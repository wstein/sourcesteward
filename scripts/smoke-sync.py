#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Real-process acceptance of `stw sync` against disposable repositories only (Spec 032, task T014).

Build bin/stw first (or pass --binary). Git is mandatory. Every fixture is local: bare remotes reached through file
paths, GIT_ALLOW_PROTOCOL=file, a private HOME and global Git configuration, so no network or user configuration is
involved. The fixture directory name contains a space, non-ASCII characters and a quote on purpose. Each case asserts
what its name says; the manifest below is checked against the cases that completed (see require_complete).
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
from types import SimpleNamespace

ROOT = Path(__file__).resolve().parents[1]
BINARY = ROOT / 'bin/stw'

CASE_IDS = tuple('S.' + name for name in (
    'plan-readonly', 'plan-json', 'fast-forward-journal', 'no-hook-installed', 'no-merge-without-flag', 'merge-flag',
    'hostile-global-config', 'submodules', 'restore', 'restore-refuses-sync-flags', 'usage-errors', 'busy', 'unsupported-path',
    'quoted-path', 'missing-binary', 'rebase', 'rebase-merged-upstream', 'prune', 'autostash', 'autostash-conflict',
    'rebase-conflict'))

# Top-level files of a Git directory that carry a repository's state and operation in progress.
STATE_FILES = ('HEAD', 'index', 'ORIG_HEAD', 'MERGE_HEAD', 'MERGE_MSG', 'MERGE_MODE', 'CHERRY_PICK_HEAD', 'REVERT_HEAD', 'packed-refs')


def require_complete(completed, expected=CASE_IDS):
    """Fail unless exactly the expected cases completed: a case that silently did not run is a failure."""
    missing, extra = sorted(set(expected) - set(completed)), sorted(set(completed) - set(expected))
    if missing or extra:
        raise AssertionError(('Mandatory sync cases differ', missing, extra))


def digest(path):
    """Relative path, mode and SHA-256 of every file below path (links are not followed)."""
    result = {}
    for parent, directories, files in os.walk(path, followlinks=False):
        directories.sort()
        for name in sorted(files):
            file = Path(parent) / name
            metadata = file.lstat()
            content = os.readlink(file) if file.is_symlink() else hashlib.sha256(file.read_bytes()).hexdigest()
            result[str(file.relative_to(path))] = (metadata.st_mode, content)
    return result


def git_state(git_dir):
    """The state files of a Git directory and every ref file below refs/ (content hashes)."""
    files = {name: hashlib.sha256((git_dir / name).read_bytes()).hexdigest() for name in STATE_FILES if (git_dir / name).is_file()}
    return files, digest(git_dir / 'refs')


def worktree_files(root):
    """The working tree without the Git directory (and without nested Git directories of submodules)."""
    result = {}
    for parent, directories, files in os.walk(root, followlinks=False):
        directories[:] = sorted(name for name in directories if name != '.git')
        for name in sorted(files):
            if name == '.git':
                continue
            file = Path(parent) / name
            result[str(file.relative_to(root))] = hashlib.sha256(file.read_bytes()).hexdigest() if not file.is_symlink() else os.readlink(file)
    return result


def acceptance(binary=BINARY):
    if shutil.which('git') is None:
        raise RuntimeError('Mandatory sync backend missing: git')
    if not binary.is_file():
        raise RuntimeError('Build bin/stw before sync acceptance')
    os.umask(0o022)
    completed = set()
    with tempfile.TemporaryDirectory(prefix="stw sync smoke ünï 'q' ") as directory:
        base = Path(directory).resolve()
        home = base / 'home'
        home.mkdir()
        gitconfig = home / 'gitconfig'
        gitconfig.write_text('')
        env = {key: value for key, value in os.environ.items() if not key.startswith(('GIT_', 'JJ_'))}
        env.update(HOME=str(home), XDG_CONFIG_HOME=str(home / 'config'), GIT_CONFIG_NOSYSTEM='1', GIT_CONFIG_GLOBAL=str(gitconfig),
                   GIT_TERMINAL_PROMPT='0', GIT_ALLOW_PROTOCOL='file', LC_ALL='C', GIT_AUTHOR_NAME='Sync Tester',
                   GIT_AUTHOR_EMAIL='sync@example.org', GIT_COMMITTER_NAME='Sync Tester', GIT_COMMITTER_EMAIL='sync@example.org')
        work = base / 'work'
        work.mkdir()

        def configured(settings):
            """An environment whose global Git configuration holds the given sections."""
            path = home / ('gitconfig-' + hashlib.sha256(settings.encode()).hexdigest()[:8])
            path.write_text(settings)
            return {**env, 'GIT_CONFIG_GLOBAL': str(path)}

        def run(tool, args, cwd=work, check=True):
            result = subprocess.run([tool, *args], cwd=cwd, env=env, capture_output=True, timeout=60)
            if check:
                assert result.returncode == 0, ('Fixture setup failed', tool, args, result.stderr)
            return result.stdout.decode().strip()

        def git(args, cwd=work):
            return run('git', ['-c', 'protocol.file.allow=always', *args], cwd)

        def commit(repo, name, text):
            (repo / name).write_text(text)
            git(['add', name], repo)
            git(['commit', '-qm', 'commit ' + name], repo)

        def stw(args, cwd=work, environment=None):
            return subprocess.run([str(binary), *args], cwd=cwd, env=environment or env, capture_output=True, timeout=120)

        def sync_json(args, expected_exit, cwd=work, environment=None):
            result = stw(['sync', *args, '--json'], cwd, environment)
            assert result.returncode == expected_exit, (args, result.returncode, result.stdout[:2000], result.stderr)
            output = json.loads(result.stdout)
            assert output['schema_version'] == 1 and output['command'] == 'sync', output
            for key in ('mode', 'run_id', 'options', 'note', 'repos', 'summary', 'error'):
                assert key in output, ('key missing', key, output)
            assert output['summary']['total'] == len(output['repos']), output
            return output

        def refused(args):
            """A usage error: exit 2, nothing on stdout, an `Error:` line on stderr."""
            result = stw(args)
            assert result.returncode == 2 and result.stdout == b'' and result.stderr.startswith(b'Error:'), (args, result.returncode, result.stdout, result.stderr)

        def by_name(output):
            return {entry['name']: entry for entry in output['repos']}

        def local_refs(repo):
            """Every ref except the remote-tracking ones, with its object (what a plan must not change)."""
            lines = git(['for-each-ref', '--format=%(refname) %(objectname)'], repo).splitlines()
            return [line for line in lines if not line.startswith('refs/remotes/')]

        def state(repo):
            """What a plan or a skipped repository must keep: local refs, HEAD, status and files."""
            return (local_refs(repo), git(['rev-parse', 'HEAD'], repo), git(['status', '--porcelain=v2', '--ignore-submodules=all'], repo),
                    worktree_files(repo))

        def make(name):
            """A fixture workspace below work/NAME: bare remotes, a clone with a submodule, a second clone that moves the upstream."""
            top = work / name
            top.mkdir()
            git(['init', '-q', '--bare', '-b', 'main', 'sub.git'], top)
            git(['init', '-q', '--bare', '-b', 'main', 'ws.git'], top)
            seed = top / 'sub-seed'
            git(['clone', '-q', str(top / 'sub.git'), str(seed)], top)
            git(['checkout', '-q', '-b', 'main'], seed)
            commit(seed, 'sub1.txt', 'one\n')
            git(['push', '-q', 'origin', 'main'], seed)
            ws = top / 'ws'
            git(['clone', '-q', str(top / 'ws.git'), str(ws)], top)
            git(['checkout', '-q', '-b', 'main'], ws)
            git(['submodule', 'add', '-q', str(top / 'sub.git'), 'sub'], ws)
            commit(ws, 'root.txt', 'root\n')  # also commits the staged submodule
            git(['push', '-q', 'origin', 'main'], ws)
            other = top / 'ws-other'
            git(['clone', '-q', str(top / 'ws.git'), str(other)], top)

            def upstream_commit(file, text=None):
                commit(other, file, file + '\n' if text is None else text)
                git(['push', '-q', 'origin', 'main'], other)

            return SimpleNamespace(top=top, ws=ws, other=other, seed=seed, upstream_commit=upstream_commit)

        def hooks(ws):
            """The hook directories of the root and of the submodule: stw must add, remove and change nothing there."""
            return {'root': digest(ws / '.git' / 'hooks'), 'sub': digest(ws / '.git' / 'modules' / 'sub' / 'hooks')}

        # --- fixture "main": the sequence of cases that build on each other -------------------------------
        fx = make('main')
        ws = fx.ws
        hooks_baseline = hooks(ws)
        assert hooks_baseline['root'] and hooks_baseline['sub'], 'fixture: no hook files to compare'

        # --- S.plan-readonly / S.plan-json -------------------------------------------------------
        fx.upstream_commit('up1.txt')
        git(['fetch', '-q', 'origin'], ws)  # the remote-tracking ref knows up1; the plan has to fetch up2
        fx.upstream_commit('up2.txt')
        before_state, before_worktrees = state(ws), git(['worktree', 'list'], ws)
        remote_before = git(['rev-parse', 'refs/remotes/origin/main'], ws)
        plan = sync_json(['--plan', str(ws)], 0)
        assert state(ws) == before_state and git(['worktree', 'list'], ws) == before_worktrees, 'a plan changed the working state'
        assert git(['rev-parse', 'refs/remotes/origin/main'], ws) != remote_before, 'the plan did not fetch'
        completed.add('S.plan-readonly')
        assert plan['mode'] == 'plan' and plan['run_id'] is None and by_name(plan)['.']['result'] == 'fast-forward', plan
        assert not (ws / '.git' / 'stw').exists() and git(['for-each-ref', 'refs/stw'], ws) == '', 'a plan wrote a journal or a recovery ref'
        completed.add('S.plan-json')

        # --- S.fast-forward-journal / S.no-hook-installed ------------------------------------------
        old_head = git(['rev-parse', 'HEAD'], ws)
        run_one = sync_json([str(ws)], 0)
        root_entry = by_name(run_one)['.']
        assert root_entry['result'] == 'fast-forwarded' and root_entry['old_oid'] == old_head, root_entry
        assert git(['rev-parse', 'HEAD'], ws) == git(['rev-parse', 'refs/remotes/origin/main'], ws) == root_entry['new_oid']
        run_id = run_one['run_id']
        assert run_id and (ws / '.git' / 'stw' / 'sync' / (run_id + '.json')).is_file(), 'no journal record'
        assert git(['rev-parse', 'refs/stw/recovery/sync/' + run_id + '/1'], ws) == old_head, 'no recovery ref at the old tip'
        completed.add('S.fast-forward-journal')
        assert hooks(ws) == hooks_baseline, 'a hook was installed or changed'
        completed.add('S.no-hook-installed')

        # --- S.no-merge-without-flag / S.hostile-global-config / S.merge-flag ----------------------
        commit(ws, 'local.txt', 'local\n')
        fx.upstream_commit('up3.txt')
        local_head = git(['rev-parse', 'HEAD'], ws)
        diverged = sync_json([str(ws)], 0)
        assert by_name(diverged)['.']['result'] == 'diverged' and 'needs a choice' in by_name(diverged)['.']['reason'], diverged
        assert git(['rev-parse', 'HEAD'], ws) == local_head and git(['rev-list', '--merges', '--count', 'HEAD'], ws) == '0'
        completed.add('S.no-merge-without-flag')
        still = sync_json([str(ws)], 0, environment=configured('[pull]\n\trebase = false\n\tff = false\n[merge]\n\tff = false\n'))
        assert by_name(still)['.']['result'] == 'diverged' and git(['rev-parse', 'HEAD'], ws) == local_head, still
        assert git(['rev-list', '--merges', '--count', 'HEAD'], ws) == '0', 'a configuration created a merge commit'
        completed.add('S.hostile-global-config')
        merged = sync_json(['--merge', str(ws)], 0)
        assert by_name(merged)['.']['result'] == 'merged' and git(['rev-list', '--merges', '--count', 'HEAD'], ws) == '1', merged
        completed.add('S.merge-flag')
        git(['fetch', '-q', 'origin'], ws)  # fixture only: realign the root with its upstream for the next cases
        git(['reset', '-q', '--hard', 'refs/remotes/origin/main'], ws)

        # --- S.submodules ---------------------------------------------------------------------------
        commit(fx.seed, 'sub2.txt', 'two\n')
        git(['push', '-q', 'origin', 'main'], fx.seed)
        git(['fetch', '-q', 'origin'], ws / 'sub')
        recorded = git(['rev-parse', 'origin/main'], ws / 'sub')
        git(['checkout', '-q', '--detach', 'HEAD'], ws / 'sub')
        git(['update-index', '--cacheinfo', '160000,' + recorded + ',sub'], ws)
        index_before = git(['ls-files', '--stage'], ws)
        plain = sync_json([str(ws)], 0)
        assert by_name(plain)['sub']['result'] == 'skipped-detached', plain
        full = sync_json(['--submodules', str(ws)], 0)
        assert by_name(full)['sub']['result'] == 'updated' and git(['rev-parse', 'HEAD'], ws / 'sub') == recorded, full
        assert git(['ls-files', '--stage'], ws) == index_before, 'a gitlink changed'
        completed.add('S.submodules')

        # --- S.restore / S.restore-refuses-sync-flags ------------------------------------------------
        fx.upstream_commit('up4.txt')
        pre_run = git(['rev-parse', 'HEAD'], ws)
        moved = sync_json([str(ws)], 0)
        restore = stw(['sync', 'restore', str(ws), '--json'])
        assert restore.returncode == 0, (restore.stdout, restore.stderr)
        restored = json.loads(restore.stdout)
        assert restored['mode'] == 'restore' and restored['run_id'] == moved['run_id'] and by_name(restored)['.']['result'] == 'restored', restored
        assert git(['rev-parse', 'HEAD'], ws) == pre_run and git(['rev-parse', 'HEAD'], ws / 'sub') == recorded
        second = stw(['sync', 'restore', str(ws), '--json'])
        assert second.returncode == 0, (second.stdout, second.stderr)
        assert by_name(json.loads(second.stdout))['.']['result'] == 'already-restored', second.stdout
        completed.add('S.restore')
        for flag in ('--submodules', '--plan', '--rebase', '--merge', '--prune'):
            result = stw(['sync', 'restore', flag, str(ws)])
            assert result.returncode == 2 and result.stdout == b'' and b'does not apply to stw sync restore' in result.stderr, (flag, result.returncode, result.stdout, result.stderr)
        completed.add('S.restore-refuses-sync-flags')

        # --- S.usage-errors ----------------------------------------------------------------------------
        for args in (['--rebase', '--merge'], ['--ff-only', '--merge'], ['--run', '20260101T000000Z-00000000'], ['--bogus'], ['--remote', 'origin']):
            refused(['sync', *args, str(ws)])
        completed.add('S.usage-errors')

        # --- S.busy: a merge in progress is skipped and untouched ------------------------------------------
        fx.upstream_commit('up5.txt')
        git(['checkout', '-q', '-b', 'side', 'HEAD~1'], ws)
        commit(ws, 'busy.txt', 'side\n')
        git(['checkout', '-q', 'main'], ws)
        commit(ws, 'busy.txt', 'main\n')
        subprocess.run(['git', 'merge', 'side'], cwd=ws, env=env, capture_output=True)
        assert (ws / '.git' / 'MERGE_HEAD').is_file(), 'fixture: no merge in progress'
        busy_state, busy_git = state(ws), git_state(ws / '.git')
        busy = sync_json([str(ws)], 0)
        assert by_name(busy)['.']['result'] == 'skipped-busy' and state(ws) == busy_state and git_state(ws / '.git') == busy_git, busy
        busy_plan = sync_json(['--plan', str(ws)], 0)
        assert by_name(busy_plan)['.']['result'] == 'skipped-busy' and state(ws) == busy_state and git_state(ws / '.git') == busy_git, busy_plan
        completed.add('S.busy')

        # --- S.unsupported-path: a jj-owned directory is refused before any effect --------------------------
        jj = work / 'jj-owned'
        (jj / '.jj').mkdir(parents=True)
        denied = sync_json([str(jj)], 1)
        assert denied['error'] is not None and denied['error']['code'] == 'sync-unsupported-backend' and denied['repos'] == [], denied
        completed.add('S.unsupported-path')
        assert hooks(ws) == hooks_baseline, 'a hook was installed or changed by a later case'

        # --- S.quoted-path: every reported path is the fixture path, byte for byte -----------------------------
        assert ' ' in str(base) and "'" in str(base) and 'ü' in str(base), base
        for output in (plan, full, busy):
            paths = {entry['name']: entry['path'] for entry in output['repos']}
            assert paths['.'] == str(ws) and paths.get('sub', str(ws / 'sub')) == str(ws / 'sub'), paths
        completed.add('S.quoted-path')

        # --- S.rebase: --rebase, and pull.rebase=true from the configuration -------------------------------
        fx = make('rebase')
        commit(fx.ws, 'local.txt', 'local\n')
        fx.upstream_commit('up.txt')
        tip = git(['rev-parse', 'refs/remotes/origin/main'], fx.ws)
        def assert_rebased_on_top(result, ws):
            assert by_name(result)['.']['result'] == 'rebased', result
            assert git(['rev-list', '--merges', '--count', 'HEAD'], ws) == '0', 'a merge commit exists'
            assert git(['rev-parse', 'HEAD~1'], ws) == git(['rev-parse', 'refs/remotes/origin/main'], ws), 'the local commit is not on top of the upstream tip'
            assert (ws / 'local.txt').read_text() == 'local\n' and (ws / 'up.txt').is_file(), 'a file is missing after the rebase'

        assert_rebased_on_top(sync_json(['--rebase', str(fx.ws)], 0), fx.ws)
        fx = make('rebase-config')
        commit(fx.ws, 'local.txt', 'local\n')
        fx.upstream_commit('up.txt')
        assert_rebased_on_top(sync_json([str(fx.ws)], 0, environment=configured('[pull]\n\trebase = true\n')), fx.ws)
        completed.add('S.rebase')

        # --- S.rebase-merged-upstream: the branch was rebase-merged upstream; the local copy catches up ------
        fx = make('rebase-merged')
        commit(fx.ws, 'x.txt', 'x\n')
        feature = git(['rev-parse', 'HEAD'], fx.ws)
        git(['push', '-q', 'origin', 'HEAD:refs/heads/feature'], fx.ws)
        fx.upstream_commit('up.txt')
        git(['fetch', '-q', 'origin'], fx.other)
        git(['cherry-pick', 'origin/feature'], fx.other)  # the rebase-merge: same change, new commit id, on top of up.txt
        git(['push', '-q', 'origin', 'main'], fx.other)
        git(['push', '-q', 'origin', ':feature'], fx.other)
        assert git(['rev-parse', 'HEAD'], fx.other) != feature, 'fixture: the commit id did not change'
        none = sync_json([str(fx.ws)], 0)
        assert by_name(none)['.']['result'] == 'diverged', none
        caught_up = sync_json(['--rebase', str(fx.ws)], 0)
        assert by_name(caught_up)['.']['result'] == 'rebased', caught_up
        assert git(['rev-parse', 'HEAD'], fx.ws) == git(['rev-parse', 'refs/remotes/origin/main'], fx.ws), 'the already-merged commit was not dropped'
        assert (fx.ws / 'x.txt').read_text() == 'x\n' and git(['rev-list', '--merges', '--count', 'HEAD'], fx.ws) == '0'
        completed.add('S.rebase-merged-upstream')

        # --- S.prune: only with --prune, only remote-tracking refs ------------------------------------------
        fx = make('prune')
        git(['push', '-q', 'origin', 'HEAD:refs/heads/topic'], fx.ws)
        git(['fetch', '-q', 'origin'], fx.ws)
        git(['branch', 'keep-me', 'refs/remotes/origin/topic'], fx.ws)
        git(['push', '-q', 'origin', ':topic'], fx.other)  # another clone deletes it: ws keeps a stale remote-tracking ref
        kept = sync_json([str(fx.ws)], 0)
        assert by_name(kept)['.']['pruned'] == 0 and git(['rev-parse', '--verify', 'refs/remotes/origin/topic'], fx.ws), kept
        pruned = sync_json(['--prune', str(fx.ws)], 0)
        assert by_name(pruned)['.']['pruned'] == 1, pruned
        assert subprocess.run(['git', 'rev-parse', '--verify', '-q', 'refs/remotes/origin/topic'], cwd=fx.ws, env=env, capture_output=True).returncode != 0
        assert git(['rev-parse', '--verify', 'refs/heads/keep-me'], fx.ws) and git(['rev-parse', '--verify', 'refs/heads/main'], fx.ws), 'a local branch was deleted'
        completed.add('S.prune')

        # --- S.autostash: a dirty tree is refused for a rebase, stashed and restored with --autostash --------
        fx = make('autostash')
        commit(fx.ws, 'local.txt', 'local\n')
        fx.upstream_commit('up.txt')
        (fx.ws / 'root.txt').write_text('dirty edit\n')
        blocked_state = state(fx.ws)
        blocked = sync_json(['--rebase', str(fx.ws)], 0)
        assert by_name(blocked)['.']['result'] == 'skipped-dirty' and state(fx.ws) == blocked_state, blocked
        assert git(['stash', 'list'], fx.ws) == '', 'a stash was made for a refused rebase'
        stashed = sync_json(['--rebase', '--autostash', str(fx.ws)], 0)
        assert by_name(stashed)['.']['result'] == 'rebased' and git(['stash', 'list'], fx.ws) == '', stashed
        assert (fx.ws / 'root.txt').read_text() == 'dirty edit\n', 'the stashed edit did not come back'
        completed.add('S.autostash')

        # --- S.autostash-conflict: the update is done, the stash is kept and named ---------------------------
        fx = make('autostash-conflict')
        fx.upstream_commit('root.txt', 'upstream edit\n')
        (fx.ws / 'root.txt').write_text('conflicting local edit\n')
        conflicted = sync_json(['--autostash', str(fx.ws)], 1)
        entry = by_name(conflicted)['.']
        assert entry['result'] == 'autostash-conflict' and entry['stash_oid'] == git(['rev-parse', 'refs/stash'], fx.ws), conflicted
        assert git(['rev-parse', 'HEAD'], fx.ws) == git(['rev-parse', 'refs/remotes/origin/main'], fx.ws) and len(git(['stash', 'list'], fx.ws).splitlines()) == 1
        completed.add('S.autostash-conflict')

        # --- S.rebase-conflict: left in progress as git leaves it, exit 1 --------------------------------------
        fx = make('rebase-conflict')
        commit(fx.ws, 'root.txt', 'local change\n')
        fx.upstream_commit('root.txt', 'upstream change\n')
        head = git(['rev-parse', 'HEAD'], fx.ws)
        stopped = sync_json(['--rebase', str(fx.ws)], 1)
        entry = by_name(stopped)['.']
        assert entry['result'] == 'conflict' and 'git rebase --continue' in entry['reason'], stopped
        assert (fx.ws / '.git' / 'rebase-merge').is_dir() or (fx.ws / '.git' / 'rebase-apply').is_dir(), 'the rebase was not left in progress'
        git(['rebase', '--abort'], fx.ws)  # fixture cleanup: Git's own abort returns to the old tip
        assert git(['rev-parse', 'HEAD'], fx.ws) == head
        completed.add('S.rebase-conflict')

    # --- S.missing-binary -------------------------------------------------------------------------------
    try:
        acceptance(Path('/nonexistent-stw-sync-binary'))
    except RuntimeError as error:
        assert 'Build bin/stw' in str(error)
        completed.add('S.missing-binary')
    require_complete(completed)
    print(str(len(completed)) + ' native sync CLI cases passed with no network and a private Git configuration')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=Path, default=BINARY, help='stw executable (default: bin/stw)')
    acceptance(parser.parse_args().binary.resolve())
