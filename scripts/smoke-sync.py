#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Real-process acceptance of `stw sync` against disposable repositories only (Spec 032, task T014).

Build bin/stw first (or pass --binary). Git is mandatory. Every fixture is local: bare remotes reached through file
paths, GIT_ALLOW_PROTOCOL=file, a private HOME and a private global Git configuration, so no network or user
configuration is involved. A plan must leave every local ref, file, HEAD, stash and worktree list unchanged (only
remote-tracking refs may move); a real run is compared with what it was told to do. The fixture directory name
contains a space, non-ASCII characters and a quote on purpose.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
BINARY = ROOT / 'bin/stw'

CASE_IDS = tuple('S.' + name for name in (
    'plan-readonly', 'plan-json', 'fast-forward-journal', 'no-hook-installed', 'no-merge-without-flag', 'merge-flag',
    'hostile-global-config', 'submodules', 'restore', 'restore-refuses-sync-flags', 'usage-errors', 'busy', 'unsupported-path',
    'quoted-path', 'missing-binary'))


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

        def run(tool, args, cwd=work, check=True, environment=None):
            result = subprocess.run([tool, *args], cwd=cwd, env=environment or env, capture_output=True, timeout=60)
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

        def by_name(output):
            return {entry['name']: entry for entry in output['repos']}

        def state(repo):
            """Everything a plan or a skipped repository must keep: heads, HEAD, stash, status, files."""
            return (git(['for-each-ref', 'refs/heads', 'refs/tags', 'refs/stash'], repo), git(['rev-parse', 'HEAD'], repo),
                    git(['status', '--porcelain=v2', '--ignore-submodules=all'], repo), worktree_files(repo))

        def hooks():
            """The hook directories of the root and of the submodule: stw must add, remove and change nothing there."""
            return {'root': digest(ws / '.git' / 'hooks'), 'sub': digest(ws / '.git' / 'modules' / 'sub' / 'hooks')}

        # --- fixture: bare remotes, a clone with a pinned submodule, an upstream that moves ----------
        git(['init', '-q', '--bare', '-b', 'main', 'sub.git'])
        git(['init', '-q', '--bare', '-b', 'main', 'ws.git'])
        seed = work / 'sub-seed'
        git(['clone', '-q', str(work / 'sub.git'), str(seed)])
        git(['checkout', '-q', '-b', 'main'], seed)
        commit(seed, 'sub1.txt', 'one\n')
        git(['push', '-q', 'origin', 'main'], seed)
        ws = work / 'ws'
        git(['clone', '-q', str(work / 'ws.git'), str(ws)])
        git(['checkout', '-q', '-b', 'main'], ws)
        git(['submodule', 'add', '-q', str(work / 'sub.git'), 'sub'], ws)
        commit(ws, 'root.txt', 'root\n')  # also commits the staged submodule
        git(['push', '-q', 'origin', 'main'], ws)
        other = work / 'ws-other'
        git(['clone', '-q', str(work / 'ws.git'), str(other)])

        def upstream_commit(name):
            commit(other, name, name + '\n')
            git(['push', '-q', 'origin', 'main'], other)

        hooks_baseline = hooks()
        assert hooks_baseline['root'] and hooks_baseline['sub'], 'fixture: no hook files to compare'

        # --- S.plan-readonly / S.plan-json -------------------------------------------------------
        upstream_commit('up1.txt')
        git(['fetch', '-q', 'origin'], ws)  # so the plan's own fetch has nothing to add to the local view of the remote
        upstream_commit('up2.txt')
        before_state, before_worktrees = state(ws), git(['worktree', 'list'], ws)
        remote_before = git(['rev-parse', 'refs/remotes/origin/main'], ws)
        plan = sync_json(['--plan', str(ws)], 0)
        assert state(ws) == before_state and git(['worktree', 'list'], ws) == before_worktrees, 'a plan changed the working state'
        completed.add('S.plan-readonly')
        assert git(['rev-parse', 'refs/remotes/origin/main'], ws) != remote_before, 'the plan did not fetch'
        assert plan['mode'] == 'plan' and plan['run_id'] is None and by_name(plan)['.']['result'] == 'fast-forward', plan
        assert not (ws / '.git' / 'stw').exists(), 'a plan wrote a journal'
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
        assert hooks() == hooks_baseline, 'a hook was installed or changed'
        completed.add('S.no-hook-installed')

        # --- S.no-merge-without-flag / S.merge-flag / S.hostile-global-config ----------------------
        commit(ws, 'local.txt', 'local\n')
        upstream_commit('up3.txt')
        local_head = git(['rev-parse', 'HEAD'], ws)
        diverged = sync_json([str(ws)], 0)
        assert by_name(diverged)['.']['result'] == 'diverged' and 'needs a choice' in by_name(diverged)['.']['reason'], diverged
        assert git(['rev-parse', 'HEAD'], ws) == local_head and git(['rev-list', '--merges', '--count', 'HEAD'], ws) == '0'
        completed.add('S.no-merge-without-flag')
        hostile = dict(env)
        hostile_config = home / 'hostile-gitconfig'
        hostile_config.write_text('[pull]\n\trebase = false\n\tff = false\n[merge]\n\tff = false\n')
        hostile['GIT_CONFIG_GLOBAL'] = str(hostile_config)
        still = sync_json([str(ws)], 0, environment=hostile)
        assert by_name(still)['.']['result'] == 'diverged' and git(['rev-parse', 'HEAD'], ws) == local_head, still
        assert git(['rev-list', '--merges', '--count', 'HEAD'], ws) == '0', 'a configuration created a merge commit'
        completed.add('S.hostile-global-config')
        merged = sync_json(['--merge', str(ws)], 0)
        assert by_name(merged)['.']['result'] == 'merged' and git(['rev-list', '--merges', '--count', 'HEAD'], ws) == '1', merged
        completed.add('S.merge-flag')

        git(['fetch', '-q', 'origin'], ws)  # fixture only: realign the root with its upstream for the next cases
        git(['reset', '-q', '--hard', 'refs/remotes/origin/main'], ws)

        # --- S.submodules ---------------------------------------------------------------------------
        commit(seed, 'sub2.txt', 'two\n')
        git(['push', '-q', 'origin', 'main'], seed)
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
        upstream_commit('up4.txt')
        pre_run = git(['rev-parse', 'HEAD'], ws)
        moved = sync_json([str(ws)], 0)
        restore = stw(['sync', 'restore', str(ws), '--json'])
        assert restore.returncode == 0, (restore.stdout, restore.stderr)
        restored = json.loads(restore.stdout)
        assert restored['mode'] == 'restore' and restored['run_id'] == moved['run_id'] and by_name(restored)['.']['result'] == 'restored', restored
        assert git(['rev-parse', 'HEAD'], ws) == pre_run and git(['rev-parse', 'HEAD'], ws / 'sub') == recorded
        again = json.loads(stw(['sync', 'restore', str(ws), '--json']).stdout)
        assert by_name(again)['.']['result'] == 'already-restored', again
        completed.add('S.restore')
        for flag in ('--submodules', '--plan', '--rebase', '--merge', '--prune'):
            refused = stw(['sync', 'restore', flag, str(ws)])
            assert refused.returncode == 2 and refused.stdout == b'', (flag, refused.returncode, refused.stdout)
        completed.add('S.restore-refuses-sync-flags')

        # --- S.usage-errors ----------------------------------------------------------------------------
        for args in (['--rebase', '--merge'], ['--ff-only', '--merge'], ['--run', '20260101T000000Z-00000000'], ['--bogus'], ['--remote', 'origin']):
            refused = stw(['sync', *args, str(ws)])
            assert refused.returncode == 2 and refused.stdout == b'', (args, refused.returncode, refused.stdout)
        completed.add('S.usage-errors')

        # --- S.busy: a merge in progress is skipped and untouched ------------------------------------------
        upstream_commit('up5.txt')
        git(['checkout', '-q', '-b', 'side', 'HEAD~1'], ws)
        commit(ws, 'busy.txt', 'side\n')
        git(['checkout', '-q', 'main'], ws)
        commit(ws, 'busy.txt', 'main\n')
        subprocess.run(['git', 'merge', 'side'], cwd=ws, env=env, capture_output=True)
        assert (ws / '.git' / 'MERGE_HEAD').is_file(), 'fixture: no merge in progress'
        busy_state, busy_git = state(ws), digest(ws / '.git' / 'refs')
        busy = sync_json([str(ws)], 0)
        assert by_name(busy)['.']['result'] == 'skipped-busy' and state(ws) == busy_state and (ws / '.git' / 'MERGE_HEAD').is_file(), busy
        busy_plan = sync_json(['--plan', str(ws)], 0)
        assert by_name(busy_plan)['.']['result'] == 'skipped-busy' and state(ws) == busy_state, busy_plan
        assert digest(ws / '.git' / 'refs') == busy_git, 'refs of a busy repository changed'
        completed.add('S.busy')

        # --- S.unsupported-path: a jj-owned directory is refused before any effect --------------------------
        jj = work / 'jj-owned'
        (jj / '.jj').mkdir(parents=True)
        refused = sync_json([str(jj)], 1)
        assert refused['error'] is not None and refused['error']['code'] == 'sync-unsupported-backend' and refused['repos'] == [], refused
        completed.add('S.unsupported-path')

        assert hooks() == hooks_baseline, 'a hook was installed or changed by a later case'

        # --- S.quoted-path: the whole run happened below a directory with a space, non-ASCII and a quote ----
        assert ' ' in str(base) and "'" in str(base) and 'ü' in str(base), base
        assert plan['repos'][0]['path'] == str(ws), plan['repos'][0]['path']
        completed.add('S.quoted-path')

    # --- S.missing-binary -------------------------------------------------------------------------------
    try:
        acceptance(Path('/nonexistent-stw-sync-binary'))
    except RuntimeError as error:
        assert 'Build bin/stw' in str(error)
        completed.add('S.missing-binary')
    assert completed == set(CASE_IDS), ('Mandatory sync cases differ', sorted(set(CASE_IDS) - completed), sorted(completed - set(CASE_IDS)))
    print(str(len(completed)) + ' native sync CLI cases passed with no network, a private Git configuration and no hook installed')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=Path, default=BINARY, help='stw executable (default: bin/stw)')
    acceptance(parser.parse_args().binary.resolve())
