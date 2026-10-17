#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Real-process acceptance of `stw repos` against disposable repositories only.

Build bin/stw first (or pass --binary). Git is mandatory. The jj cases run only when jj and its
read-only sandbox are available and are reported as not exercised otherwise; a skipped lane never
counts as acceptance. Every run compares the whole fixture tree (bytes, modes and modification times,
directories included, so a lock file that appeared and vanished is seen) before and after, and a
set of effect sentinels proves that no repository-controlled helper, hook, filter, textconv,
external diff, alias, credential helper, fsmonitor or remote helper ran.
"""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import shutil
import stat
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
BINARY = ROOT / 'bin/stw'
_spec = importlib.util.spec_from_file_location('jj_smoke', ROOT / 'scripts/smoke-jj-inspection.py')
helpers = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(helpers)

CASE_IDS = {
    'G': tuple('G.' + name for name in (
        'members', 'inventories-equal', 'submodules', 'summary', 'effects', 'sentinels', 'usage', 'human', 'all-ok', 'missing-jj')),
    'J': tuple('J.' + name for name in (
        'parallel-members', 'secondary-workspace', 'inventory-equal', 'subdirectory', 'submodules-not-applicable')),
}
FOREIGN_CASE = 'F.foreign-owned'


def snapshot(root):
    """Bytes, mode and modification time of every entry below root (links are not followed)."""
    result = {}
    for parent, directories, files in os.walk(root, followlinks=False):
        for name in sorted(directories + files):
            path = Path(parent) / name
            metadata = path.lstat()
            if stat.S_ISLNK(metadata.st_mode):
                content = ('link', os.readlink(path))
            elif stat.S_ISREG(metadata.st_mode):
                content = ('file', path.read_bytes())
            elif stat.S_ISDIR(metadata.st_mode):
                content = ('directory',)
            else:
                raise AssertionError('Unexpected fixture file type: ' + str(path))
            result[str(path.relative_to(root))] = (metadata.st_mode, metadata.st_mtime_ns, content)
    return result


def recompute_summary(entries):
    """The cross-repository summary, recomputed from the entries by the derivation table."""
    summary = {'entries': len(entries), 'by_status': dict.fromkeys(('ok', 'partial', 'error', 'skipped'), 0),
               'by_backend': dict.fromkeys(('git', 'jj', 'other'), 0), 'dirty': dict.fromkeys(('true', 'false', 'unknown'), 0),
               'stale_workspaces': 0, 'foreign_workspaces': 0, 'unverified_workspaces': 0, 'conflicted_references': 0,
               'skipped_uninitialized': 0, 'discovery_not_applicable': 0}
    for entry in entries:
        summary['by_status'][entry['status']] += 1
        summary['by_backend'][entry['backend'] if entry['backend'] in ('git', 'jj') else 'other'] += 1
        if entry['origin'] == 'submodule' and entry['status'] == 'skipped':
            summary['skipped_uninitialized'] += 1
        if entry['origin'] == 'explicit' and entry['discovery']['status'] == 'not-applicable':
            summary['discovery_not_applicable'] += 1
        facts = entry['facts']
        if facts is not None:
            summary['dirty'][facts['dirty']['value']] += 1
            summary['conflicted_references'] += facts['conflicted_references']
            summary['stale_workspaces'] += facts['workspaces']['stale']
            summary['foreign_workspaces'] += facts['workspaces']['foreign']
            summary['unverified_workspaces'] += facts['workspaces']['unverified']
    return summary


def acceptance(binary=BINARY, want_jj=None):
    if shutil.which('git') is None:
        raise RuntimeError('Mandatory repos backend missing: git')
    if not binary.is_file():
        raise RuntimeError('Build bin/stw before repos acceptance')
    # Git creates directories world-writable under umask 000 and the submodule checks refuse those.
    os.umask(0o022)
    jj_lane = shutil.which('jj') is not None if want_jj is None else want_jj
    if jj_lane:
        helpers.check_sandbox()
    expected = {case for group in ('G', 'J') for case in CASE_IDS[group] if group == 'G' or jj_lane}
    completed = set()
    not_exercised = [] if jj_lane else ['J.* (jj or its read-only sandbox is not available)']
    with tempfile.TemporaryDirectory(prefix='stw-repos-conformance-') as directory:
        base = Path(directory).resolve()
        home = base / 'home'
        home.mkdir()
        sentinels = base / 'sentinels'  # outside the compared tree: any file here is an effect
        sentinels.mkdir()
        tools = base / 'tools'
        tools.mkdir()
        root = base / 'work'
        root.mkdir()
        env = helpers.clean_environment(home)
        env.update(GIT_AUTHOR_NAME='Repos Tester', GIT_AUTHOR_EMAIL='repos@example.org',
                   GIT_COMMITTER_NAME='Repos Tester', GIT_COMMITTER_EMAIL='repos@example.org',
                   PATH=str(tools) + os.pathsep + os.environ['PATH'])
        # Every member's helper writes a distinct file in the sentinel directory and fails.
        helper = tools / 'helper'
        helper.write_text('#!/bin/sh\nprintf invoked > "' + str(sentinels) + '/helper-$$"\nexit 1\n')
        helper.chmod(0o700)
        remote_helper = tools / 'git-remote-sentinel'
        remote_helper.write_text('#!/bin/sh\nprintf network > "' + str(sentinels) + '/network-$$"\nexit 1\n')
        remote_helper.chmod(0o700)

        def run(tool, args, cwd=root, check=True, environment=None):
            result = subprocess.run([tool, *args], cwd=cwd, env=environment or env, capture_output=True, timeout=30)
            if check:
                assert result.returncode == 0, ('Fixture setup failed', tool, args, result.stderr)
            return result.stdout.decode().strip()

        def git(args, cwd):
            # Fixture creation never runs a planted helper: they are configured afterwards.
            return run('git', ['-c', 'protocol.file.allow=always', '-c', 'core.fsmonitor=false', *args], cwd)

        def commit(repo, name='f.txt', text='content\n'):
            (repo / name).write_text(text)
            git(['add', '.'], repo)
            git(['commit', '-qm', 'commit ' + name], repo)

        def stw(args, environment=None):
            result = subprocess.run([str(binary), *args], cwd=root, env=environment or env, capture_output=True, timeout=120)
            return result

        def observe(args, expected_exit, case_id=None, environment=None):
            before = snapshot(base / 'work')
            result = stw(args, environment)
            assert snapshot(base / 'work') == before, ('repos changed the fixture tree', args)
            assert not list(sentinels.iterdir()), ('an effect sentinel fired', args, sorted(path.name for path in sentinels.iterdir()))
            allowed = expected_exit if isinstance(expected_exit, tuple) else (expected_exit,)
            assert result.returncode in allowed, (args, result.returncode, result.stdout[:2000], result.stderr)
            if expected_exit == 2:
                assert result.stdout == b'', ('usage error printed output', result.stdout)
                return None
            return result

        def observe_json(args, expected_exit, environment=None):
            result = observe([*args, '--json'], expected_exit, environment=environment)
            output = json.loads(result.stdout)
            assert output['schema_version'] == 1 and output['command'] == 'repos', output
            assert output['boundary']['freshness'] == 'sampled-nontransactional', output
            assert output['boundary']['effects'] == {'network': False, 'fetch': False, 'repository_writes': False, 'hooks': False, 'private_temporary_files_for_jj': True}, output
            assert [entry['index'] for entry in output['entries']] == list(range(len(output['entries']))), output
            assert recompute_summary(output['entries']).items() <= output['summary'].items(), (recompute_summary(output['entries']), output['summary'])
            return output

        def entry_of(output, path, origin='explicit'):
            matches = [entry for entry in output['entries'] if entry['path'] == str(path) and entry['origin'] == origin]
            assert matches, (path, [entry['path'] for entry in output['entries']])
            return matches[0]

        # --- fixture workspace -------------------------------------------------------------
        clean = root / 'clean'
        git(['init', '-q', '-b', 'main', str(clean)], root)
        commit(clean)
        (clean / 'subdir').mkdir()

        withwt = root / 'with-worktree'
        git(['init', '-q', '-b', 'main', str(withwt)], root)
        commit(withwt)
        linked = root / 'with-worktree-linked'
        git(['worktree', 'add', '-q', '-b', 'task', str(linked)], withwt)

        source = root / 'submodule-source'
        git(['init', '-q', '-b', 'main', str(source)], root)
        commit(source, 'lib.txt', 'lib\n')
        super_ = root / 'super'
        git(['init', '-q', '-b', 'main', str(super_)], root)
        commit(super_, 'top.txt', 'top\n')
        git(['submodule', 'add', '-q', str(source), 'vendor/lib'], super_)
        git(['commit', '-qm', 'add submodule'], super_)
        super_clone = root / 'super-clone'  # its submodule is not checked out: uninitialized
        git(['clone', '-q', str(super_), str(super_clone)], root)

        hostile = root / 'hostile'
        git(['init', '-q', '-b', 'main', str(hostile)], root)
        (hostile / '.gitattributes').write_text('*.txt filter=hostile diff=hostile\n')
        commit(hostile)
        hooks = hostile / '.githooks'
        hooks.mkdir()
        for hook in ('pre-commit', 'post-checkout', 'post-merge', 'reference-transaction', 'fsmonitor-watchman'):
            (hooks / hook).write_text('#!/bin/sh\nprintf hook > "' + str(sentinels) + '/hook-' + hook + '"\n')
            (hooks / hook).chmod(0o700)
        def plant(repo, include=False):
            """Hostile repository-controlled configuration, set after the fixture was created."""
            settings = [('core.fsmonitor', str(helper)), ('core.hooksPath', str(hooks)), ('filter.hostile.clean', str(helper)),
                        ('filter.hostile.smudge', str(helper)), ('filter.hostile.process', str(helper)),
                        ('diff.hostile.textconv', str(helper)), ('diff.external', str(helper)), ('alias.inventory', '!' + str(helper)),
                        ('credential.helper', '!' + str(helper)), ('core.pager', str(helper)), ('core.editor', str(helper)),
                        ('gpg.program', str(helper)), ('core.sshCommand', str(helper)), ('remote.origin.url', 'sentinel::origin'),
                        ('remote.origin.fetch', '+refs/heads/*:refs/remotes/origin/*')]
            if include:
                # An included file is read by Git for the superproject's own commands; it is refused
                # (path-unsafe) in a submodule's configuration, so only the superproject carries one.
                (tools / 'included.cfg').write_text('[core]\n\tfsmonitor = ' + str(helper) + '\n')
                settings.append(('include.path', str(tools / 'included.cfg')))
            (repo / '.gitattributes').write_text('*.txt filter=hostile diff=hostile\n')
            for key, value in settings:
                run('git', ['config', key, value], repo)

        plant(hostile)
        # The superproject and its initialized submodule are hostile too, so the discovery runner and the
        # inspection and inventory of the discovered member are sentinel-covered.
        plant(super_, include=True)
        plant(super_ / 'vendor' / 'lib')
        (hostile / 'f.txt').write_text('modified so that a stat-dirty file needs the filter\n')

        missing = root / 'does-not-exist'
        subdir = clean / 'subdir'
        fakejj = root / 'fake-jj'  # a jj ownership marker without a usable jj: only used with PATH lacking jj
        (fakejj / '.jj' / 'repo').mkdir(parents=True)

        # --- G: Git lane --------------------------------------------------------------------
        members = [clean, withwt, linked, super_, super_clone, hostile, missing, subdir, super_]
        output = observe_json(['repos', *map(str, members), '--submodules'], 1)
        completed.add('G.members')
        assert output['status'] == 'partial' and output['boundary']['complete'] is False, output['status']
        assert output['input_paths'] == [str(member) for member in members], output['input_paths']
        explicit = output['entries'][:len(members)]
        assert [entry['path'] for entry in explicit] == [str(member) for member in members], 'explicit entries must keep argument order'
        assert entry_of(output, clean)['status'] == 'ok' and entry_of(output, clean)['error'] is None
        assert entry_of(output, withwt)['status'] == 'ok'
        duplicate = explicit[2]
        assert duplicate['error']['code'] == 'duplicate-repository' and duplicate['duplicate_of'] == 1 and duplicate['inventory'] is None, duplicate
        assert explicit[8]['error']['code'] == 'duplicate-repository' and explicit[8]['duplicate_of'] == 3, explicit[8]
        assert explicit[6]['error']['code'] == 'invalid-path' and explicit[6]['inventory'] is None, explicit[6]
        assert explicit[7]['error']['code'] == 'not-repository-root' and explicit[7]['inventory'] is None, explicit[7]
        assert explicit[7]['backend'] == 'git'
        assert entry_of(output, super_)['status'] == 'partial', 'a repository with a registered gitlink is partial under the default scopes'
        assert entry_of(output, hostile)['status'] in ('ok', 'partial'), entry_of(output, hostile)['error']

        # Each embedded inventory equals what `stw inventory` returns for the same path (SC-001).
        for entry in output['entries']:
            if entry['inventory'] is None:
                continue
            alone = stw(['inventory', entry['resolved_path'], '--json'])
            assert json.loads(alone.stdout) == entry['inventory'], ('embedded inventory differs from stw inventory', entry['path'])
        completed.add('G.inventories-equal')

        # Discovery: the initialized submodule is an entry, the uninitialized one is skipped.
        subs = [entry for entry in output['entries'] if entry['origin'] == 'submodule']
        assert [(entry['path'], entry['status'], entry['parent']) for entry in subs] == [('vendor/lib', 'ok', 3), ('vendor/lib', 'skipped', 4)], subs
        assert subs[0]['facts']['dirty']['value'] == 'unknown' and subs[0]['notes'], subs[0]
        assert subs[1]['error']['code'] == 'submodule-uninitialized' and subs[1]['inventory'] is None
        assert output['summary']['skipped_uninitialized'] == 1
        completed.add('G.submodules')
        completed.add('G.summary')  # recompute_summary is asserted for every JSON run above

        # Effects: the fixture tree is byte, mode and mtime identical (asserted by observe), and no
        # sentinel exists: no helper, hook, filter, textconv, external diff, alias, credential helper,
        # fsmonitor, editor, pager, ssh command or remote helper (network) ran.
        completed.add('G.effects')
        assert sorted(path.name for path in sentinels.iterdir()) == [], sorted(path.name for path in sentinels.iterdir())
        completed.add('G.sentinels')

        # Usage errors exit 2 with no output; text output is the same result.
        observe(['repos'], 2)
        observe(['repos', *[str(clean)] * 17], 2)
        observe(['repos', ''], 2)
        completed.add('G.usage')
        human = observe(['repos', str(clean), str(withwt)], 0)
        assert human.stdout.startswith(b'SourceSteward repos: ok'), human.stdout
        assert b'summary:' in human.stdout and b'limitations:' in human.stdout
        completed.add('G.human')
        ok_output = observe_json(['repos', str(clean), str(withwt)], 0)
        assert ok_output['status'] == 'ok' and ok_output['boundary']['complete'] is True and all(e['status'] == 'ok' for e in ok_output['entries'])
        completed.add('G.all-ok')

        # A jj-owned entry without a usable jj fails alone, with the inventory's code; Git entries stay intact.
        without_jj = dict(env)
        git_only = base / 'git-only-bin'
        git_only.mkdir()
        (git_only / 'git').symlink_to(shutil.which('git'))
        without_jj['PATH'] = str(git_only)
        output = observe_json(['repos', str(clean), str(fakejj)], 1, environment=without_jj)
        assert output['entries'][0]['status'] == 'ok', output['entries'][0]
        assert output['entries'][1]['status'] == 'error' and output['entries'][1]['backend'] == 'jj', output['entries'][1]
        assert output['entries'][1]['error']['code'] == 'jj-unavailable', output['entries'][1]['error']
        completed.add('G.missing-jj')

        # --- F: foreign-owned member, only where the environment can create another owner ----
        foreign_exercised = False
        if hasattr(os, 'geteuid') and os.geteuid() == 0:
            foreign = root / 'foreign'
            git(['init', '-q', '-b', 'main', str(foreign)], root)
            commit(foreign)
            for parent, directories, files in os.walk(foreign):
                for name in [parent, *[os.path.join(parent, n) for n in directories + files]]:
                    os.chown(name, 65534, 65534, follow_symlinks=False)
            output = observe_json(['repos', str(clean), str(foreign)], 1)
            assert output['entries'][0]['status'] == 'ok' and output['entries'][1]['status'] == 'error', output['entries'][1]
            assert sorted(path.name for path in sentinels.iterdir()) == []
            foreign_exercised = True
            completed.add(FOREIGN_CASE)
        else:
            not_exercised.append(FOREIGN_CASE + ' (needs root to create a second owner; GIT_TEST_ASSUME_DIFFERENT_OWNER cannot be used because the runner strips GIT_*)')

        # --- J: jj lane ---------------------------------------------------------------------
        if jj_lane:
            def jj(args, cwd=root):
                return run('jj', ['--no-pager', '--color=never', *args], cwd)

            jj1 = root / 'jj-one'
            jj2 = root / 'jj-two'
            for repo in (jj1, jj2):
                jj(['git', 'init', '--no-colocate', str(repo)])
                (repo / 'a.txt').write_text('jj\n')
                jj(['describe', '-m', 'fixture'], repo)
            secondary = root / 'jj-two-secondary'
            jj(['workspace', 'add', '--name', 'secondary', str(secondary)], jj2)
            jjsub = jj1 / 'sub'
            jjsub.mkdir()
            shim_log = sentinels.parent / 'git-calls'
            shim = tools / 'git'
            real_git = shutil.which('git', path=os.environ['PATH'])
            shim.write_text('#!/bin/sh\nprintf "%s\\n" "$*" >> "' + str(shim_log) + '"\nexec "' + real_git + '" "$@"\n')
            shim.chmod(0o700)
            members = [jj1, jj2, secondary, jjsub, clean]
            output = observe_json(['repos', *map(str, members)], 1)
            jj_entries = output['entries']
            assert jj_entries[0]['backend'] == 'jj' and jj_entries[1]['backend'] == 'jj', jj_entries[:2]
            assert jj_entries[0]['facts']['dirty']['value'] == 'unknown' and jj_entries[1]['facts']['dirty']['value'] == 'unknown'
            completed.add('J.parallel-members')
            assert jj_entries[2]['error']['code'] == 'duplicate-repository' and jj_entries[2]['duplicate_of'] == 1, jj_entries[2]
            completed.add('J.secondary-workspace')
            alone = stw(['inventory', str(jj1), '--scope', 'stored', '--json'])
            assert json.loads(alone.stdout) == jj_entries[0]['inventory'], 'jj entry differs from stw inventory --scope stored'
            completed.add('J.inventory-equal')
            assert jj_entries[3]['error']['code'] == 'not-repository-root' and jj_entries[3]['inventory'] is None, jj_entries[3]
            completed.add('J.subdirectory')
            shim_log.write_text('')
            # This case asserts the discovery status and that no Git discovery ran; the exit status depends on how
            # complete the jj inventory is in the lane, which other cases already pin.
            output = observe_json(['repos', str(jj1), '--submodules'], (0, 1))
            assert output['entries'][0]['discovery']['status'] == 'not-applicable', output['entries'][0]['discovery']
            calls = shim_log.read_text()
            assert not any(word in calls for word in ('ls-tree', 'ls-files', '--blob')), ('Git discovery ran for a jj parent', calls)
            completed.add('J.submodules-not-applicable')
            assert sorted(path.name for path in sentinels.iterdir()) == []

    assert completed == expected | ({FOREIGN_CASE} if foreign_exercised else set()), ('Mandatory repos cases differ', sorted(expected - completed), sorted(completed - expected))
    print(str(len(completed)) + ' native repos CLI cases passed with byte/mode/mtime-identical fixture state and no effect sentinel')
    for item in not_exercised:
        print('NOT EXERCISED: ' + item)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=Path, default=BINARY, help='stw executable (default: bin/stw)')
    acceptance(parser.parse_args().binary.resolve())
