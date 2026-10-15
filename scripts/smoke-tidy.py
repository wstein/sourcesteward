#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Mandatory evidence-only tidy acceptance; no real repository is modified.

Every native invocation is confined to disposable fixtures. Each observation checks
all fixture files and metadata independently, including unsnapshotted jj disk work.
Core tests separately inject cancellation, concurrent changes and output limits.
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
NAMES = ('ancestry', 'unique', 'base-retain', 'used-retain', 'empty-retain',
         'upstream-not-proof', 'literal-selection', 'missing-selection',
         'duplicate-selection', 'explicit-base', 'human', 'helper',
         'manager-ownership', 'persisted-plan')
CASE_IDS = {group: tuple(group + '.' + name for name in NAMES)
            for group in ('git', 'jj-standalone', 'jj-colocated')}


def require_complete(completed, expected):
    assert len(completed) == len(set(completed)), 'Repeated mandatory tidy case'
    actual = set(completed)
    assert actual == expected, ('Mandatory tidy cases differ', sorted(expected - actual), sorted(actual - expected))


def acceptance(binary=BINARY):
    for backend in ('git', 'jj'):
        if shutil.which(backend) is None:
            raise RuntimeError('Mandatory tidy backend missing: ' + backend)
    if not binary.is_file():
        raise RuntimeError('Build bin/stw before tidy acceptance')
    helpers.check_sandbox()
    completed = []
    with tempfile.TemporaryDirectory(prefix='stw-tidy-conformance-') as directory:
        root = Path(directory).resolve()
        home = root / 'home'
        home.mkdir()
        env = helpers.clean_environment(home)
        env.update(GIT_AUTHOR_NAME='Tidy Tester', GIT_AUTHOR_EMAIL='tidy@example.org',
                   GIT_COMMITTER_NAME='Tidy Tester', GIT_COMMITTER_EMAIL='tidy@example.org')
        tools = {tool: shutil.which(tool) for tool in ('git', 'jj')}

        def native(tool, args, cwd=root):
            prefix = ['-c', 'maintenance.auto=false', '-c', 'gc.auto=0'] if tool == 'git' else ['--no-pager', '--color=never']
            result = subprocess.run([tools[tool], *prefix, *args], cwd=cwd, env=env, capture_output=True, timeout=20)
            assert result.returncode == 0, ('Native fixture failed', tool, args, result.stdout, result.stderr)
            return result.stdout.decode().strip()

        for group in CASE_IDS:
            area = root / group
            area.mkdir()
            source = area / 'source'
            state = area / 'state'
            state.mkdir(mode=0o700)
            if group == 'git':
                native('git', ['init', '-q', '-b', 'trunk', str(source)])
                (source / 'tracked.txt').write_text('initial\n')
                (source / '.gitignore').write_text('ignored.txt\n')
                native('git', ['add', '.'], source)
                native('git', ['commit', '-qm', 'initial'], source)
                first = native('git', ['rev-parse', 'HEAD'], source)
                native('git', ['branch', 'integrated', first], source)
                native('git', ['branch', 'literal.name', first], source)
                native('git', ['branch', 'literal.name-longer', first], source)
                (source / 'tracked.txt').write_text('trunk\n')
                native('git', ['commit', '-qam', 'trunk'], source)
                native('git', ['checkout', '-qb', 'unique', first], source)
                native('git', ['commit', '--allow-empty', '-qm', 'empty but unintegrated'], source)
                native('git', ['checkout', '-q', 'trunk'], source)
                linked = area / 'linked'
                native('git', ['worktree', 'add', '-q', '-b', 'used', str(linked), first], source)
                base, used = 'trunk', 'used'
            else:
                native('jj', ['git', 'init', '--colocate' if group == 'jj-colocated' else '--no-colocate', str(source)])
                (source / 'tracked.txt').write_text('initial\n')
                (source / '.gitignore').write_text('ignored.txt\n')
                native('jj', ['describe', '-m', 'initial'], source)
                first = native('jj', ['log', '--no-graph', '-r', '@', '-T', 'commit_id'], source)
                for name in ('integrated', 'literal.name', 'literal.name-longer'):
                    native('jj', ['bookmark', 'create', name, '-r', first], source)
                native('jj', ['new'], source)
                (source / 'tracked.txt').write_text('trunk\n')
                native('jj', ['describe', '-m', 'trunk'], source)
                native('jj', ['bookmark', 'create', 'trunk', '-r', '@'], source)
                native('jj', ['new', first], source)
                native('jj', ['bookmark', 'create', 'unique', '-r', '@'], source)
                native('jj', ['new', 'trunk'], source)
                native('jj', ['bookmark', 'create', 'used', '-r', '@'], source)
                base, used = 'trunk', 'used'
            # None of these disk changes may be snapshotted, reset or discarded.
            (source / 'tracked.txt').write_text('unsnapshotted disk edits\n')
            (source / 'untracked\nfile.txt').write_text('untracked evidence\n')
            (source / 'ignored.txt').write_text('ignored evidence\n')

            def observe(name, args=(), status='ok', mode='json', persist=False, syntax=False):
                case_id = group + '.' + name
                assert case_id in CASE_IDS[group] and case_id not in completed, case_id
                before = helpers.snapshot(area)
                result = subprocess.run([str(binary), 'tidy', 'plan', str(source),
                                         *([] if syntax else ['--base', base]), *map(str, args),
                                         *(['--json'] if mode == 'json' else [])],
                                        cwd=root, env=env, capture_output=True, timeout=25)
                after = helpers.snapshot(area)
                if persist:
                    strip_state = lambda snap: {key: value for key, value in snap.items() if key != 'state' and not key.startswith('state/')}
                    assert strip_state(before) == strip_state(after), ('Plan changed native state', case_id)
                else:
                    assert before == after, ('Observation changed fixture state', case_id)
                if syntax:
                    assert result.returncode == 2 and not result.stdout, (case_id, result.stdout, result.stderr)
                    completed.append(case_id)
                    return None
                if mode == 'human':
                    assert result.returncode == 0 and result.stdout.strip() and not result.stderr, (result.stdout, result.stderr)
                    assert b'integrated' in result.stdout and b'unique' in result.stdout, result.stdout
                    completed.append(case_id)
                    return None
                try:
                    value = json.loads(result.stdout)
                except json.JSONDecodeError as error:
                    raise AssertionError(('Missing tidy result', case_id, result.returncode, result.stdout, result.stderr)) from error
                allowed = (status,) if isinstance(status, str) else status
                assert value['status'] in allowed, (case_id, value)
                assert result.returncode == (0 if value['status'] == 'ok' else 1), (case_id, value, result.stderr)
                assert value['schema_version'] == 1 and value['command'] == 'tidy-plan', value
                assert value['backend'] == ('git' if group == 'git' else 'jj') or value['status'] != 'ok', value
                for field in ('actions', 'diagnostics', 'limitations'):
                    assert isinstance(value[field], list), value
                completed.append(case_id)
                return value

            def candidate(value, name):
                candidates = {item['name']: item for item in value['plan']['candidates']}
                assert name in candidates, (name, candidates)
                return candidates[name]

            value = observe('ancestry')
            integrated = candidate(value, 'integrated')
            assert integrated['decision'] == 'delete' and integrated['proof'] == 'native-ancestry', integrated
            assert integrated['commit_id'] == first, integrated
            assert not value['plan']['plan_id'], value
            unique = candidate(observe('unique'), 'unique')
            assert unique['decision'] == 'retain' and unique['proof'] == 'none', unique
            selected_base = candidate(observe('base-retain'), 'trunk')
            assert selected_base['decision'] == 'retain', selected_base
            workspace_used = candidate(observe('used-retain'), used)
            assert workspace_used['decision'] == 'retain' and workspace_used['workspace_paths'], workspace_used
            empty = candidate(observe('empty-retain'), 'unique')
            assert empty['decision'] == 'retain', empty
            selected = observe('literal-selection', ['--ref', 'literal.name'])
            assert [item['name'] for item in selected['plan']['candidates']] == ['literal.name'], selected
            observe('missing-selection', ['--ref', 'absent'], status=('refused', 'error'))
            observe('duplicate-selection', ['--ref', 'integrated', '--ref', 'integrated'], status=('refused', 'error'))
            observe('explicit-base', syntax=True)
            observe('human', mode='human')
            persisted = observe('persisted-plan', ['--state-dir', state], persist=True)
            record = persisted['plan']
            assert record['plan_id'] and record['state_dir'] == str(state), record
            stored = list(state.rglob(record['plan_id'] + '.json'))
            assert len(stored) == 1 and stored[0].stat().st_mode & 0o777 == 0o600, stored
            assert json.loads(stored[0].read_text()) == record, stored

            sentinel = area / 'helper-ran'
            helper = area / 'hostile-helper'
            helper.write_text('#!/bin/sh\nprintf invoked > "' + str(sentinel) + '"\nexit 1\n')
            helper.chmod(0o700)
            if group == 'git':
                hook = source / '.git/hooks/reference-transaction'
                shutil.copy2(helper, hook)
                observe('helper', status=('refused', 'error'))
                hook.unlink()
            else:
                config = source / '.jj/repo/config.toml'
                previous = config.read_bytes() if config.exists() else None
                config.write_text('[aliases]\nbookmark = ["util", "exec", "--", "' + str(helper) + '"]\n')
                observe('helper', status=('refused', 'error'))
                if previous is None:
                    config.unlink()
                else:
                    config.write_bytes(previous)
            assert not sentinel.exists(), 'Repository-controlled helper executed'
            git_marker = source / '.git'
            created = not git_marker.exists()
            if created:
                git_marker.mkdir()
            managed = git_marker / 'gitbutler'
            managed.mkdir()
            observe('manager-ownership', status=('refused', 'error'))
            managed.rmdir()
            if created:
                git_marker.rmdir()
            if group == 'git':
                native('git', ['config', 'branch.unique.remote', 'origin'], source)
                native('git', ['config', 'branch.unique.merge', 'refs/heads/gone'], source)
                upstream = candidate(observe('upstream-not-proof'), 'unique')
            else:
                # The backing Git counterpart is still a remote counterpart in jj;
                # deletion intent must not be inferred from local ancestry alone.
                native('jj', ['--ignore-working-copy', 'git', 'export'], source)
                upstream = candidate(observe('upstream-not-proof'), 'integrated')
            assert upstream['decision'] == 'retain', upstream
    expected = {case_id for cases in CASE_IDS.values() for case_id in cases}
    require_complete(completed, expected)
    print(str(len(completed)) + ' native tidy planning CLI cases passed with byte/mode-identical native state')


if __name__ == '__main__':
    acceptance()
