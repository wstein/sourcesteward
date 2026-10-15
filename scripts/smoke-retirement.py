#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Mandatory owned Git/jj retirement acceptance, exclusively in disposable fixtures.

Real Spec010 plans establish ownership. Registration and filesystem authority are
separate. Required Go tests inject cancellation, races and partial native failures.
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
NAMES = ('authorization', 'owned-clean-remove', 'metadata-only', 'current-workspace',
         'wrong-owner', 'dirty', 'untracked', 'ignored', 'symlink',
         'unknown-metadata', 'stale-files', 'stale-inode')
CASE_IDS = {group: tuple(group + '.' + name for name in NAMES)
            for group in ('git', 'jj-standalone', 'jj-colocated')}


def acceptance(binary=BINARY):
    for backend in ('git', 'jj'):
        if shutil.which(backend) is None:
            raise RuntimeError('Mandatory retirement backend missing: ' + backend)
    if not binary.is_file():
        raise RuntimeError('Build bin/stw before retirement acceptance')
    helpers.check_sandbox()
    completed = []
    with tempfile.TemporaryDirectory(prefix='stw-retirement-conformance-') as directory:
        root = Path(directory).resolve()
        home = root / 'home'
        home.mkdir()
        env = helpers.clean_environment(home)
        env.update(GIT_AUTHOR_NAME='Retirement Tester', GIT_AUTHOR_EMAIL='retire@example.org',
                   GIT_COMMITTER_NAME='Retirement Tester', GIT_COMMITTER_EMAIL='retire@example.org')
        tools = {tool: shutil.which(tool) for tool in ('git', 'jj')}

        def native(tool, args, cwd=root):
            prefix = ['-c', 'maintenance.auto=false', '-c', 'gc.auto=0'] if tool == 'git' else ['--no-pager', '--color=never']
            run = subprocess.run([tools[tool], *prefix, *args], cwd=cwd, env=env, capture_output=True, timeout=20)
            assert run.returncode == 0, ('Native fixture failed', tool, args, run.stdout, run.stderr)
            return run.stdout.decode().strip()

        def invoke(family, operation, args, statuses=('ok',), cwd=root):
            run = subprocess.run([str(binary), family, operation, *map(str, args), '--json'], cwd=cwd, env=env, capture_output=True, timeout=75)
            try:
                value = json.loads(run.stdout)
            except json.JSONDecodeError as error:
                raise AssertionError(('Missing structured result', family, operation, run.returncode, run.stdout, run.stderr)) from error
            assert value['status'] in statuses and value['command'] == family + '-' + operation, value
            assert run.returncode == (0 if value['status'] == 'ok' else 1), (value, run.stderr)
            return value

        for group in CASE_IDS:
            area = root / group
            area.mkdir()
            source, state = area / 'source', area / 'state'
            state.mkdir(mode=0o700)
            is_git = group == 'git'
            if is_git:
                native('git', ['init', '-q', '-b', 'trunk', str(source)])
            else:
                native('jj', ['git', 'init', '--colocate' if group == 'jj-colocated' else '--no-colocate', str(source)])
            (source / 'tracked.txt').write_bytes(b'stored binary\x00content\xff\n')
            (source / '.gitignore').write_text('ignored.txt\n')
            if is_git:
                native('git', ['add', '.'], source)
                native('git', ['commit', '-qm', 'initial'], source)
                base = 'refs/heads/trunk'
            else:
                native('jj', ['describe', '-m', 'initial'], source)
                native('jj', ['bookmark', 'create', 'trunk', '-r', '@'], source)
                base = 'trunk'

            def create(task):
                destination = area / task
                record = invoke('workspace', 'plan', [source, '--base', base, '--destination', destination,
                                                      '--task', task, '--owner', 'conformance', '--state-dir', state])['plan']
                value = invoke('workspace', 'apply', ['--plan', record['plan_id'], '--state-dir', state, '--authorize'])
                assert destination.is_dir() and value['task']['destination'] == str(destination), value
                return destination, value['task']

            clean, clean_task = create('clean-task')
            metadata, metadata_task = create('metadata-task')
            retained, retained_task = create('retained-task')
            sibling_file = area / 'sibling.txt'
            sibling_file.write_text('sibling file must survive\n')
            sibling_dir = area / 'sibling-directory'
            sibling_dir.mkdir()
            (sibling_dir / 'content.txt').write_text('sibling directory must survive\n')
            # Source work is neither retirement target nor authorization to snapshot.
            (source / 'tracked.txt').write_bytes(b'unsnapshotted source edits\x00\xff')
            (source / 'source-untracked.txt').write_text('source work\n')

            def mark(case):
                case_id = group + '.' + case
                assert case_id in CASE_IDS[group] and case_id not in completed, case_id
                completed.append(case_id)

            def plan(task, destination, files=True, owner='conformance', cwd=root, blocked=False):
                before = helpers.snapshot(area)
                value = invoke('tidy', 'plan', [source, '--base', base, '--state-dir', state, '--task', task,
                                               '--owner', owner, '--remove-registration',
                                               *(['--remove-files', destination] if files else [])],
                               statuses=('ok', 'refused', 'error', 'partial') if blocked else ('ok',), cwd=cwd)
                after = helpers.snapshot(area)
                without_state = lambda snap: {key: val for key, val in snap.items() if key != 'state' and not key.startswith('state/')}
                assert without_state(before) == without_state(after), 'Retirement planning changed native/filesystem state'
                assert value['schema_version'] == 2, value
                if blocked and value.get('retirement') is not None:
                    assert all(action['decision'] != 'delete' for action in value['retirement']['expected']['actions']), value
                return value.get('retirement')

            def apply(record, authorize=True, statuses=('ok',), preserve=False):
                before = helpers.snapshot(area)
                value = invoke('tidy', 'apply', ['--plan', record['plan_id'], '--state-dir', state,
                                               *(['--authorize'] if authorize else [])], statuses)
                assert value['schema_version'] == 2, value
                if preserve:
                    strip_state = lambda snap: {key: val for key, val in snap.items() if key != 'state' and not key.startswith('state/')}
                    assert strip_state(before) == strip_state(helpers.snapshot(area)), 'Refused retirement changed native/filesystem state'
                return value

            record = plan('clean-task', clean)
            apply(record, authorize=False, statuses=('refused', 'error'), preserve=True)
            mark('authorization')
            before_source, before_retained = helpers.snapshot(source), helpers.snapshot(retained)
            before_metadata = helpers.snapshot(metadata)
            before_sibling_file, before_sibling_dir = sibling_file.read_bytes(), helpers.snapshot(sibling_dir)
            apply(record)
            assert not clean.exists(), 'Authorized clean task files survived unexpectedly'
            if is_git:
                registrations = native('git', ['worktree', 'list', '--porcelain'], source)
                assert 'worktree ' + str(clean) + '\n' not in registrations + '\n', registrations
                root_refs = native('git', ['for-each-ref', '--format=%(objectname)', 'refs/stw/recovery/'], source).splitlines()
                assert clean_task['native']['commit_id'] in root_refs, root_refs
                registration = Path(clean_task['native']['git_dir']).name
                def source_protected(snap):
                    allowed = ('.git/refs/stw', '.git/worktrees/' + registration)
                    return {key: val for key, val in snap.items() if not any(key == prefix or key.startswith(prefix + '/') for prefix in allowed)}
            else:
                registrations = native('jj', ['--ignore-working-copy', 'workspace', 'list', '-T', 'name ++ "\\n"'], source).splitlines()
                assert clean_task['native']['workspace_name'] not in registrations, registrations
                history = native('jj', ['--ignore-working-copy', 'log', '--no-graph', '-r',
                                        'commit_id(' + json.dumps(clean_task['native']['commit_id']) + ')', '-T', 'commit_id'], source)
                assert history == clean_task['native']['commit_id'], history
                def source_protected(snap):
                    return {key: val for key, val in snap.items()
                            if (not (key == '.jj/repo' or key.startswith('.jj/repo/')) or key in ('.jj/repo/config-id', '.jj/repo/config.toml'))
                            and not (group == 'jj-colocated' and (key == '.git/objects' or key.startswith('.git/objects/') or key == '.git/refs/jj' or key.startswith('.git/refs/jj/')))}
            assert source_protected(before_source) == source_protected(helpers.snapshot(source)), 'Retirement changed protected source state'
            assert helpers.snapshot(retained) == before_retained and helpers.snapshot(metadata) == before_metadata, 'Retirement changed another task'
            assert sibling_file.read_bytes() == before_sibling_file and helpers.snapshot(sibling_dir) == before_sibling_dir, 'Retirement changed siblings'
            mark('owned-clean-remove')
            metadata_record = plan('metadata-task', metadata, files=False, blocked=is_git)
            if is_git:
                assert metadata_record is not None, 'Git registration-only retention omitted evidence'
                assert all(action['decision'] == 'retain' for action in metadata_record['expected']['actions']), metadata_record
            else:
                before_files = helpers.snapshot(metadata)
                apply(metadata_record)
                assert metadata.is_dir() and helpers.snapshot(metadata) == before_files, 'jj forget deleted or changed files'
                registrations = native('jj', ['--ignore-working-copy', 'workspace', 'list', '-T', 'name ++ "\\n"'], source).splitlines()
                assert metadata_task['native']['workspace_name'] not in registrations, registrations
            mark('metadata-only')
            plan('retained-task', retained, cwd=retained, blocked=True)
            mark('current-workspace')
            plan('retained-task', retained, owner='someone-else', blocked=True)
            mark('wrong-owner')
            tracked = retained / 'tracked.txt'
            stored = tracked.read_bytes()
            tracked.write_bytes(b'dirty\x00disk')
            plan('retained-task', retained, blocked=True)
            mark('dirty')
            tracked.write_bytes(stored)
            for case, path in (('untracked', retained / 'untracked\nfile.txt'), ('ignored', retained / 'ignored.txt')):
                path.write_text('retain this data\n')
                plan('retained-task', retained, blocked=True)
                path.unlink()
                mark(case)
            link = retained / 'extra-link'
            link.symlink_to(sibling_file)
            plan('retained-task', retained, blocked=True)
            link.unlink()
            mark('symlink')
            metadata_root = Path(retained_task['native']['git_dir']) if is_git else retained / '.jj'
            payload = metadata_root / 'unknown-payload'
            payload.write_bytes(b'unknown manager metadata\x00')
            plan('retained-task', retained, blocked=True)
            payload.unlink()
            mark('unknown-metadata')
            stale = plan('retained-task', retained)
            tracked.write_bytes(b'new work after planning')
            apply(stale, statuses=('refused', 'error', 'partial'), preserve=True)
            tracked.write_bytes(stored)
            mark('stale-files')
            stale = plan('retained-task', retained)
            renamed = area / 'retained-original'
            retained.rename(renamed)
            shutil.copytree(renamed, retained)
            apply(stale, statuses=('refused', 'error', 'partial'), preserve=True)
            assert renamed.is_dir() and retained.is_dir(), 'Stale physical ownership was deleted'
            mark('stale-inode')
    expected = {case for cases in CASE_IDS.values() for case in cases}
    assert len(completed) == len(set(completed)) and set(completed) == expected, ('Missing retirement cases', expected - set(completed), set(completed) - expected)
    print(str(len(completed)) + ' native retirement CLI cases passed with separate effects and protected-state checks')


if __name__ == '__main__':
    acceptance()
