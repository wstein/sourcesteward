#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Mandatory authorized local-reference cleanup in disposable native repositories.

Interrupted-native/cancel/persistence fault injection remains in required Go tests.
This CLI matrix independently checks refs, native history and undeclared effects.
"""
import importlib.util
import json
from pathlib import Path
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
BINARY = ROOT / 'bin/stw'
_spec = importlib.util.spec_from_file_location('jj_smoke', ROOT / 'scripts/smoke-jj-inspection.py')
helpers = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(helpers)
NAMES = ('authorization', 'delete-recovery', 'retry', 'stale-tip', 'stale-base',
         'workspace-use', 'remote-preservation', 'human')
CASE_IDS = {group: tuple(group + '.' + name for name in NAMES)
            for group in ('git', 'jj-standalone', 'jj-colocated')}


def acceptance(binary=BINARY):
    for backend in ('git', 'jj'):
        if shutil.which(backend) is None:
            raise RuntimeError('Mandatory tidy apply backend missing: ' + backend)
    if not binary.is_file():
        raise RuntimeError('Build bin/stw before tidy apply acceptance')
    helpers.check_sandbox()
    completed = []
    with tempfile.TemporaryDirectory(prefix='stw-tidy-apply-conformance-') as directory:
        root = Path(directory).resolve()
        home = root / 'home'
        home.mkdir()
        env = helpers.clean_environment(home)
        env.update(GIT_AUTHOR_NAME='Tidy Tester', GIT_AUTHOR_EMAIL='tidy@example.org',
                   GIT_COMMITTER_NAME='Tidy Tester', GIT_COMMITTER_EMAIL='tidy@example.org')
        tools = {tool: shutil.which(tool) for tool in ('git', 'jj')}

        def native(tool, args, cwd=root):
            prefix = ['-c', 'maintenance.auto=false', '-c', 'gc.auto=0'] if tool == 'git' else ['--no-pager', '--color=never']
            run = subprocess.run([tools[tool], *prefix, *args], cwd=cwd, env=env, capture_output=True, timeout=20)
            assert run.returncode == 0, ('Native fixture failed', tool, args, run.stdout, run.stderr)
            return run.stdout.decode().strip()

        for group in CASE_IDS:
            area = root / group
            area.mkdir()
            source, state = area / 'source', area / 'state'
            state.mkdir(mode=0o700)
            is_git = group == 'git'

            def name(logical):
                return 'refs/heads/' + logical if is_git else logical + '?'

            if is_git:
                native('git', ['init', '-q', '-b', 'trunk', str(source)])
                (source / 'tracked.txt').write_text('initial\n')
                native('git', ['add', '.'], source)
                native('git', ['commit', '-qm', 'initial'], source)
                first = native('git', ['rev-parse', 'HEAD'], source)
                for logical in ('foo', 'foobar', 'stale', 'base-stale', 'used'):
                    native('git', ['branch', logical, first], source)
                native('git', ['update-ref', 'refs/remotes/fixture/keep', first], source)
                (source / 'tracked.txt').write_text('trunk\n')
                native('git', ['commit', '-qam', 'trunk'], source)
                base = 'refs/heads/trunk'
            else:
                native('jj', ['git', 'init', '--colocate' if group == 'jj-colocated' else '--no-colocate', str(source)])
                (source / 'tracked.txt').write_text('initial\n')
                native('jj', ['describe', '-m', 'initial'], source)
                first = native('jj', ['log', '--no-graph', '-r', '@', '-T', 'commit_id'], source)
                for logical in ('foo', 'foobar', 'stale', 'base-stale', 'used'):
                    native('jj', ['bookmark', 'create', json.dumps(name(logical)), '-r', first], source)
                native('jj', ['bookmark', 'create', 'remote-test', '-r', first], source)
                native('jj', ['new'], source)
                (source / 'tracked.txt').write_text('trunk\n')
                native('jj', ['describe', '-m', 'trunk'], source)
                native('jj', ['bookmark', 'create', 'trunk', '-r', '@'], source)
                native('jj', ['new', 'trunk'], source)
                native('jj', ['--ignore-working-copy', 'git', 'export'], source)
                base = 'trunk'
            (source / 'tracked.txt').write_text('source disk edits must survive\n')
            (source / 'untracked\nfile.txt').write_text('untracked work\n')

            def refs():
                if is_git:
                    out = native('git', ['for-each-ref', '--format=%(refname) %(objectname)'], source)
                    return dict(line.split(' ', 1) for line in out.splitlines())
                out = native('jj', ['--ignore-working-copy', 'bookmark', 'list', '--all-remotes', '-T',
                                   'json(self.name()) ++ "\\n" ++ json(self.remote()) ++ "\\n" ++ if(self.normal_target(), json(self.normal_target().commit_id()), "null") ++ "\\n"'], source)
                rows = [json.loads(line) for line in out.splitlines()]
                assert len(rows) % 3 == 0, rows
                return {(rows[i], rows[i + 1]): rows[i + 2] for i in range(0, len(rows), 3)}

            def invoke(operation, args, status='ok', human=False):
                run = subprocess.run([str(binary), 'tidy', operation, *map(str, args), *([] if human else ['--json'])],
                                     cwd=root, env=env, capture_output=True, timeout=75)
                if human:
                    assert run.returncode == 0 and run.stdout.strip() and not run.stderr, (run.stdout, run.stderr)
                    return None
                try:
                    value = json.loads(run.stdout)
                except json.JSONDecodeError as error:
                    raise AssertionError(('Missing tidy result', operation, run.returncode, run.stdout, run.stderr)) from error
                allowed = (status,) if isinstance(status, str) else status
                assert value['status'] in allowed and value['command'] == 'tidy-' + operation and value['schema_version'] == 1, value
                assert run.returncode == (0 if value['status'] == 'ok' else 1), (value, run.stderr)
                return value

            def plan(target):
                before = helpers.snapshot(source)
                value = invoke('plan', [source, '--base', base, '--state-dir', state, '--ref', target])
                assert helpers.snapshot(source) == before, 'Planning changed native state'
                return value['plan']

            def mark(case):
                case_id = group + '.' + case
                assert case_id in CASE_IDS[group] and case_id not in completed, case_id
                completed.append(case_id)

            def apply(record, authorize=True, status='ok', preserve=False, human=False):
                before = helpers.snapshot(source)
                value = invoke('apply', ['--plan', record['plan_id'], '--state-dir', state,
                                        *(['--authorize'] if authorize else [])], status, human)
                if preserve:
                    assert helpers.snapshot(source) == before, 'Refused/retried apply changed native state'
                return value

            record = plan(name('foo'))
            assert record['candidates'][0]['decision'] == 'delete', record
            apply(record, authorize=False, status=('refused', 'error'), preserve=True)
            mark('authorization')
            before_refs, before_files = refs(), helpers.snapshot(source)
            apply(record)
            after_refs, after_files = refs(), helpers.snapshot(source)
            target = name('foo') if is_git else (name('foo'), None)
            assert target not in after_refs, after_refs
            assert {key: value for key, value in before_refs.items() if key != target}.items() <= after_refs.items(), (before_refs, after_refs)
            added = set(after_refs) - set(before_refs)
            assert len(added) == 1 and after_refs[next(iter(added))] == first, (before_refs, after_refs)
            recovery = next(iter(added))
            if is_git:
                assert recovery.startswith('refs/stw/'), recovery
                native('git', ['cat-file', '-e', first + '^{commit}'], source)
                allowed = ('.git/refs/stw', '.git/refs/heads/foo', '.git/logs/refs/heads/foo')
                def protected(snap):
                    return {key: value for key, value in snap.items() if not any(key == prefix or key.startswith(prefix + '/') for prefix in allowed)}
            else:
                assert recovery[1] is None, recovery
                restored = native('jj', ['--ignore-working-copy', 'log', '--no-graph', '-r', 'commit_id(' + json.dumps(first) + ')', '-T', 'commit_id'], source)
                assert restored == first, restored
                def protected(snap):
                    return {key: value for key, value in snap.items()
                            if not (key == '.jj/repo' or key.startswith('.jj/repo/'))
                            and not (group == 'jj-colocated' and (key == '.git/objects' or key.startswith('.git/objects/') or key == '.git/refs/jj' or key.startswith('.git/refs/jj/')))}
            assert protected(before_files) == protected(after_files), ('Apply changed undeclared source state', group)
            mark('delete-recovery')
            apply(record, preserve=True)
            mark('retry')
            apply(record, preserve=True, human=True)
            mark('human')
            # A distinct literal sibling and every remote counterpart must survive.
            assert (name('foobar') if is_git else (name('foobar'), None)) in after_refs
            remote_refs = {key: value for key, value in before_refs.items() if (key.startswith('refs/remotes/') if is_git else key[1] is not None)}
            assert remote_refs and all(after_refs.get(key) == value for key, value in remote_refs.items()), (remote_refs, after_refs)
            if not is_git:
                remote_plan = plan('remote-test')
                assert remote_plan['candidates'][0]['decision'] == 'retain', remote_plan
            mark('remote-preservation')
            stale = plan(name('stale'))
            if is_git:
                native('git', ['update-ref', name('stale'), base], source)
            else:
                native('jj', ['--ignore-working-copy', 'bookmark', 'set', json.dumps(name('stale')), '-r', base], source)
            apply(stale, status=('refused', 'error', 'partial'), preserve=True)
            mark('stale-tip')
            stale_base = plan(name('base-stale'))
            if is_git:
                native('git', ['update-ref', base, first], source)
            else:
                native('jj', ['--ignore-working-copy', 'bookmark', 'set', 'trunk', '-r', first, '--allow-backwards'], source)
            apply(stale_base, status=('refused', 'error', 'partial'), preserve=True)
            mark('stale-base')
            # Restore base before the final new-registration stale-proof fixture.
            if is_git:
                trunk = before_refs[base]
                native('git', ['update-ref', base, trunk], source)
            else:
                native('jj', ['--ignore-working-copy', 'bookmark', 'set', 'trunk', '-r', '@-'], source)
            used = plan(name('used'))
            linked = area / 'linked'
            if is_git:
                native('git', ['worktree', 'add', '-q', str(linked), 'used'], source)
            else:
                native('jj', ['workspace', 'add', '--name', 'new-owner', '-r', json.dumps(name('used')), str(linked)], source)
            before_linked = helpers.snapshot(linked)
            apply(used, status=('refused', 'error', 'partial'), preserve=True)
            assert helpers.snapshot(linked) == before_linked, 'Apply changed another workspace'
            mark('workspace-use')
    expected = {case_id for cases in CASE_IDS.values() for case_id in cases}
    assert len(completed) == len(set(completed)) and set(completed) == expected, ('Missing/extra apply cases', expected - set(completed), set(completed) - expected)
    print(str(len(completed)) + ' native tidy apply CLI cases passed with exact reference and protected-state checks')


if __name__ == '__main__':
    acceptance()
