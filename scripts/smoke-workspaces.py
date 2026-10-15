#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Mandatory native isolated task workspace acceptance in disposable repositories.

Missing backend, sandbox, fixture or declared acceptance case fails the run. Plans,
handoff, refusal and retirement preparation preserve native state byte for byte.
"""
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
BINARY = ROOT / 'bin/stw'
_spec = importlib.util.spec_from_file_location('jj_smoke', ROOT / 'scripts/smoke-jj-inspection.py')
helpers = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(helpers)
NAMES = ('plan', 'apply', 'retry', 'handoff', 'retire-plan', 'stale', 'occupied',
         'symlink', 'private-store', 'foreign', 'helpers', 'incomplete-retry', 'authorization', 'human', 'manager-ownership')
CASE_IDS = {group: tuple(group + '.' + name for name in NAMES)
            for group in ('git', 'jj-standalone', 'jj-colocated')}


def require_complete(completed, expected):
    assert completed == expected, ('Mandatory workspace cases differ', sorted(expected - completed), sorted(completed - expected))


def acceptance(binary=BINARY):
    for backend in ('git', 'jj'):
        if shutil.which(backend) is None:
            raise RuntimeError('Mandatory workspace backend missing: ' + backend)
    if not binary.is_file():
        raise RuntimeError('Build bin/stw before workspace acceptance')
    helpers.check_sandbox()
    completed = set()
    with tempfile.TemporaryDirectory(prefix='stw-workspaces-conformance-') as directory:
        root = Path(directory).resolve()
        home = root / 'home'
        home.mkdir()
        env = helpers.clean_environment(home)
        env.update(GIT_AUTHOR_NAME='Workspace Tester', GIT_AUTHOR_EMAIL='workspace@example.org',
                   GIT_COMMITTER_NAME='Workspace Tester', GIT_COMMITTER_EMAIL='workspace@example.org')
        real_tools = {tool: shutil.which(tool) for tool in ('git', 'jj')}

        def native(tool, args, cwd=root):
            result = subprocess.run([real_tools[tool], *args], cwd=cwd, env=env, capture_output=True, timeout=20)
            assert result.returncode == 0, ('Fixture setup failed', tool, args, result.stderr)
            return result.stdout.decode().strip()

        def invoke(operation, args, status='ok', mode='json', native_roots=(), run_env=None):
            before = {str(path): helpers.snapshot(path) for path in native_roots}
            result = subprocess.run([str(binary), 'workspace', operation, *map(str, args),
                                     *(['--json'] if mode == 'json' else [])],
                                    cwd=root, env=run_env or env, capture_output=True, timeout=75)
            for path in native_roots:
                assert helpers.snapshot(path) == before[str(path)], ('Unexpected native mutation', operation, path, result.stdout, result.stderr)
            if mode == 'human':
                assert result.returncode == 0 and result.stdout.strip() and not result.stderr, (result.stdout, result.stderr)
                return None
            try:
                value = json.loads(result.stdout)
            except json.JSONDecodeError as error:
                raise AssertionError(('Missing structured workspace result', operation, result.returncode, result.stdout, result.stderr)) from error
            assert value['schema_version'] == 1 and value['command'] == 'workspace-' + operation, value
            allowed = (status,) if isinstance(status, str) else status
            assert value['status'] in allowed, value
            assert result.returncode == (0 if value['status'] == 'ok' else 1), (value, result.returncode, result.stderr)
            for name in ('actions', 'diagnostics', 'limitations'):
                assert isinstance(value[name], list), value
            return value

        def mark(group, name):
            case_id = group + '.' + name
            assert case_id in CASE_IDS[group] and case_id not in completed, case_id
            completed.add(case_id)

        for group in CASE_IDS:
            area = root / group
            area.mkdir()
            source = area / 'source'
            state = area / 'state'
            state.mkdir(mode=0o700)
            destination = area / 'task'
            if group == 'git':
                native('git', ['init', '-q', '-b', 'trunk', str(source)])
                native('git', ['config', 'user.name', 'Workspace Tester'], source)
                native('git', ['config', 'user.email', 'workspace@example.org'], source)
                (source / 'tracked.txt').write_text('stored fixture\n')
                native('git', ['add', '.'], source)
                native('git', ['commit', '-qm', 'initial'], source)
                base = 'HEAD'
            else:
                native('jj', ['git', 'init', '--colocate' if group == 'jj-colocated' else '--no-colocate', str(source)])
                (source / 'tracked.txt').write_text('stored fixture\n')
                native('jj', ['describe', '-m', 'initial fixture'], source)
                native('jj', ['bookmark', 'create', 'trunk', '-r', '@'], source)
                base = 'trunk'
            (source / 'untracked\nfile.txt').write_text('source work must survive\n')
            if group == 'git':
                (source / 'tracked.txt').write_text('unstaged source work\n')
            else:
                (source / 'unsnapshotted.txt').write_text('do not snapshot source\n')

            def source_content():
                snap = helpers.snapshot(source)
                # Native creation may update only shared registration/object/operation stores.
                # Source checkout files, config, refs, HEAD/index and jj working-copy stay exact.
                if group == 'git':
                    return {key: val for key, val in snap.items() if key != '.git/worktrees' and not key.startswith('.git/worktrees/')}
                return {key: val for key, val in snap.items()
                        if (not key.startswith('.jj/repo/') and key != '.jj/repo' or key == '.jj/repo/config.toml')
                        and not (group == 'jj-colocated' and (key == '.git/objects' or key.startswith('.git/objects/')
                        or key == '.git/refs/jj' or key.startswith('.git/refs/jj/')))}

            def source_identity():
                if group == 'git':
                    return native('git', ['rev-parse', 'HEAD'], source), native('git', ['symbolic-ref', 'HEAD'], source)
                return native('jj', ['--ignore-working-copy', 'log', '--no-graph', '-r', '@', '-T', 'commit_id ++ " " ++ change_id'], source)

            before_content, before_identity = source_content(), source_identity()

            def plan(dest, task, supplied_state=state, run_env=None, status='ok'):
                value = invoke('plan', [source, '--base', base, '--destination', dest, '--task', task,
                                       '--owner', 'conformance', '--state-dir', supplied_state], status=status,
                               native_roots=(source,), run_env=run_env)
                return value

            planned = plan(destination, 'task')
            mark(group, 'plan')
            mutation = planned['plan']
            assert mutation['backend'] == ('git' if group == 'git' else 'jj') and mutation['effects'] and mutation['recovery_limits'], mutation
            assert mutation['destination'] == str(destination) and mutation['owner'] == 'conformance', mutation
            plan_id = mutation['plan_id']
            assert not destination.exists(), 'Planning created destination'
            plan_path = state / 'plans' / (plan_id + '.json')
            assert plan_path.is_file() and plan_path.stat().st_mode & 0o777 == 0o600
            invoke('apply', ['--plan', plan_id, '--state-dir', state], status=('refused', 'error'), native_roots=(source,))
            assert not destination.exists()
            mark(group, 'authorization')
            applied = invoke('apply', ['--plan', plan_id, '--state-dir', state, '--authorize'])
            assert destination.is_dir() and applied['task']['owner'] == 'conformance', applied
            assert (destination / 'tracked.txt').read_text() == 'stored fixture\n'
            assert not (destination / 'untracked\nfile.txt').exists()
            task_path = state / 'tasks/task.json'
            assert task_path.is_file() and task_path.stat().st_mode & 0o777 == 0o600
            assert source_content() == before_content and source_identity() == before_identity, ('Source checkout changed', group)
            if group == 'git':
                assert native('git', ['rev-parse', 'HEAD'], destination) == mutation['expected']['base_commit']
                result = subprocess.run([real_tools['git'], 'symbolic-ref', '-q', 'HEAD'], cwd=destination, env=env, capture_output=True)
                assert result.returncode == 1, 'Task Git worktree is not detached'
            else:
                names = native('jj', ['--ignore-working-copy', 'workspace', 'list', '-T', 'name ++ "\\n"'], source).splitlines()
                assert applied['task']['native']['workspace_name'] in names, applied
            journal_path = state / 'operations' / (plan_id + '.json')
            journal = json.loads(journal_path.read_text())
            assert journal['phase'] == 'complete' and journal_path.stat().st_mode & 0o777 == 0o600, journal
            mark(group, 'apply')
            invoke('apply', ['--plan', plan_id, '--state-dir', state, '--authorize'], native_roots=(source, destination))
            mark(group, 'retry')
            for operation in ('handoff', 'retire-plan'):
                value = invoke(operation, [destination, '--state-dir', state], native_roots=(source, destination, state))
                assert value['task']['task_id'] == 'task' and value['task']['owner'] == 'conformance', value
                mark(group, operation)
            invoke('handoff', [destination, '--state-dir', state], mode='human', native_roots=(source, destination, state))
            mark(group, 'human')

            def refusal_case(name, arrange):
                dest = area / name
                value = plan(dest, name)
                arrange(dest)
                before = helpers.snapshot(area)
                invoke('apply', ['--plan', value['plan']['plan_id'], '--state-dir', state, '--authorize'], status=('refused', 'error'))
                # Refusal may persist diagnostic store records, but never changes source/destination.
                after = helpers.snapshot(area)
                retained = lambda snap: {key: val for key, val in snap.items() if key != 'state' and not key.startswith('state/')}
                assert retained(before) == retained(after), ('Refusal changed native state', group, name)
                mark(group, name)

            refusal_case('occupied', lambda dest: dest.mkdir())
            refusal_case('symlink', lambda dest: dest.symlink_to(source, target_is_directory=True))
            stale = plan(area / 'stale', 'stale')
            if group == 'git':
                native('git', ['commit', '--allow-empty', '-qm', 'base moved'], source)
            else:
                native('jj', ['--ignore-working-copy', 'new', '--no-edit', 'trunk'], source)
            invoke('apply', ['--plan', stale['plan']['plan_id'], '--state-dir', state, '--authorize'], status=('refused', 'error'), native_roots=(source,))
            assert not (area / 'stale').exists()
            mark(group, 'stale')
            unsafe = area / 'unsafe-store'
            unsafe.mkdir(mode=0o755)
            unsafe.chmod(0o755)
            plan(area / 'unsafe-target', 'unsafe', supplied_state=unsafe, status=('refused', 'error'))
            assert not (area / 'unsafe-target').exists()
            mark(group, 'private-store')
            foreign = area / 'foreign'
            foreign.mkdir()
            invoke('handoff', [foreign, '--state-dir', state], status=('refused', 'error'), native_roots=(source, foreign, state))
            mark(group, 'foreign')

            helper = area / 'hostile-helper'
            sentinel = area / 'helper-ran'
            helper.write_text('#!/bin/sh\nprintf invoked > "' + str(sentinel) + '"\nexit 1\n')
            helper.chmod(0o700)
            if group == 'git':
                hook = source / '.git/hooks/post-checkout'
                shutil.copy2(helper, hook)
                plan(area / 'helper-target', 'helpers', status=('refused', 'error'))
                hook.unlink()
            else:
                config = source / '.jj/repo/config.toml'
                prior = config.read_bytes() if config.exists() else None
                config.write_text('[aliases]\nworkspace = ["util", "exec", "--", "' + str(helper) + '"]\n')
                plan(area / 'helper-target', 'helpers', status=('refused', 'error'))
                if prior is None:
                    config.unlink()
                else:
                    config.write_bytes(prior)
            assert not sentinel.exists(), 'Repository-controlled helper executed'
            mark(group, 'helpers')

            git_marker = source / '.git'
            created_git_marker = not git_marker.exists()
            if created_git_marker:
                git_marker.mkdir()
            owner_marker = git_marker / 'gitbutler'
            owner_marker.mkdir()
            managed = plan(area / 'managed-target', 'managed', status=('refused', 'error'))
            assert managed['backend'] in ('gitbutler', 'ambiguous'), managed
            owner_marker.rmdir()
            if created_git_marker:
                git_marker.rmdir()
            mark(group, 'manager-ownership')

            # Simulated crash record after native completion: deliberately downgrade
            # this trusted fixture's durable journal. Native subprocess interruption
            # is independently fault-injected by core tests. Replay must not adopt,
            # repair or reexecute effects merely because the registration exists.
            journal['phase'] = 'incomplete'
            journal_path.write_text(json.dumps(journal))
            invoke('apply', ['--plan', plan_id, '--state-dir', state, '--authorize'],
                   status='partial', native_roots=(source, destination))
            mark(group, 'incomplete-retry')
    expected = {case for ids in CASE_IDS.values() for case in ids}
    require_complete(completed, expected)
    print(str(len(completed)) + ' native workspace CLI cases passed with independently checked source and declared effects')


if __name__ == '__main__':
    acceptance()
