#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Mandatory native Git/jj inventory acceptance against disposable fixtures only.

Build bin/stw first. Missing backends, sandbox enforcement, or cases fail acceptance.
Every CLI observation verifies byte/mode-identical target files, indexes and jj stores.
"""
import argparse
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


CASE_IDS = {
    'M1': tuple('M1.' + name for name in ('clean', 'dirty-false', 'ref-absent', 'ref-present', 'ancestor-false', 'ancestor-true', 'human', 'error')),
    'M2': tuple('M2.' + name for name in ('files-workspaces', 'dirty-true', 'literal-filter', 'denied-filters')),
    'M3': tuple('M3.' + layout + '.' + name for layout in ('standalone', 'colocated') for name in ('ref-absent', 'ref-present', 'ancestor-false', 'ancestor-true')),
    'M5': tuple('M5.' + name for name in ('large-repository', 'truncated-listing', 'submodule-checkout', 'gitlink-superproject')),
    'M4': tuple('M4.' + layout + '.' + name for layout in ('standalone', 'colocated') for name in ('stored', 'literal-stored', 'disk-unknown', 'clean-unknown', 'staging-unsupported', 'workspaces', 'secondary', 'missing')),
}


def acceptance(binary=BINARY, milestone=None):
    if milestone is not None and milestone not in CASE_IDS:
        raise ValueError('Unknown inventory milestone: ' + milestone)
    for backend in ('git', 'jj'):
        if shutil.which(backend) is None:
            raise RuntimeError('Mandatory inventory backend missing: ' + backend)
    if not binary.is_file():
        raise RuntimeError('Build bin/stw before inventory acceptance')
    helpers.check_sandbox()
    completed = set()
    with tempfile.TemporaryDirectory(prefix='stw-inventory-conformance-') as directory:
        root = Path(directory).resolve()
        home = root / 'home'
        home.mkdir()
        env = helpers.clean_environment(home)
        env.update(GIT_AUTHOR_NAME='Inventory Tester', GIT_AUTHOR_EMAIL='inventory@example.org',
                   GIT_COMMITTER_NAME='Inventory Tester', GIT_COMMITTER_EMAIL='inventory@example.org')

        def setup(tool, args, cwd=root):
            result = subprocess.run([tool, *args], cwd=cwd, env=env, capture_output=True, timeout=20)
            assert result.returncode == 0, ('Fixture setup failed', tool, args, result.stderr)
            return result.stdout.decode().strip()

        def observe(case_id, path, args=(), status='ok', predicate=None, value=None, human=False):
            group = case_id.split('.')[0]
            assert case_id in CASE_IDS[group], ('Undeclared acceptance case', case_id)
            if milestone is not None and group != milestone:
                return None
            assert case_id not in completed, ('Repeated acceptance case', case_id)
            before = helpers.snapshot(root)
            result = subprocess.run([str(binary), 'inventory', str(path), *args,
                                     *([] if human else ['--json'])], cwd=root, env=env,
                                    capture_output=True, timeout=20)
            assert helpers.snapshot(root) == before, ('Inventory changed fixture state', case_id, args)
            allowed_statuses = (status,) if isinstance(status, str) else status
            try:
                actual_status = status if human else json.loads(result.stdout)['status']
            except (json.JSONDecodeError, KeyError) as error:
                raise AssertionError(('Missing inventory result', case_id, args, result.returncode,
                                      result.stdout, result.stderr)) from error
            assert actual_status in allowed_statuses, (args, result.stdout)
            expected = 0 if actual_status == 'ok' else 1
            assert result.returncode == expected, (args, result.returncode, result.stdout, result.stderr)
            assert result.stderr.count(b'\n') == expected, result.stderr
            completed.add(case_id)
            if human:
                assert result.stdout.strip(), 'Missing human result'
                return None
            output = json.loads(result.stdout)
            assert output['schema_version'] == 1 and output['command'] == 'inventory', output
            assert output['status'] == actual_status, output
            assert output['boundary']['complete'] == (actual_status == 'ok'), output
            assert output['boundary']['freshness'] == 'sampled-nontransactional', output
            for collection in ('workspaces', 'references', 'bases', 'files', 'diagnostics'):
                assert isinstance(output[collection], list), (collection, output)
            for workspace in output['workspaces']:
                assert workspace['task_owner'] == 'unknown', workspace
            if predicate:
                assert output['query']['predicate'] == predicate and output['query']['value'] == value, output
                assert output['query']['reason'], output
            return output

        repo = root / 'git'
        setup('git', ['init', '-q', '-b', 'custom-trunk', str(repo)])
        (repo / 'tracked.txt').write_text('initial\n')
        (repo / '.gitignore').write_text('ignored.txt\n')
        setup('git', ['add', '.'], repo)
        setup('git', ['commit', '-qm', 'initial'], repo)
        initial = setup('git', ['rev-parse', 'HEAD'], repo)
        observe('M1.clean', repo, ['--query', 'clean'], predicate='clean', value='true')
        observe('M1.dirty-false', repo, ['--query', 'dirty'], predicate='dirty', value='false')
        observe('M1.ref-absent', repo, ['--query', 'ref-exists', '--ref', 'refs/heads/absent'], predicate='ref-exists', value='false')
        observe('M1.ref-present', repo, ['--query', 'ref-exists', '--ref', 'refs/heads/custom-trunk'], predicate='ref-exists', value='true')
        (repo / 'tracked.txt').write_text('staged\n')
        setup('git', ['add', 'tracked.txt'], repo)
        setup('git', ['commit', '-qm', 'second'], repo)
        second = setup('git', ['rev-parse', 'HEAD'], repo)
        observe('M1.ancestor-false', repo, ['--query', 'ancestor', '--from', second, '--to', initial], predicate='ancestor', value='false')
        observe('M1.ancestor-true', repo, ['--query', 'ancestor', '--from', initial, '--to', second], predicate='ancestor', value='true')
        linked = root / 'linked'
        setup('git', ['worktree', 'add', '-q', '-b', 'task', str(linked)], repo)
        absent = root / 'missing'
        setup('git', ['worktree', 'add', '-q', '-b', 'missing-task', str(absent)], repo)
        shutil.rmtree(absent)
        # A filesystem-monitor hook would mutate this sentinel if repository config were used.
        sentinel = root / 'helper-ran'
        helper = root / 'hostile-helper'
        helper.write_text('#!/bin/sh\nprintf invoked > "' + str(sentinel) + '"\nexit 1\n')
        helper.chmod(0o700)
        setup('git', ['config', 'core.fsmonitor', str(helper)], repo)
        (repo / 'tracked.txt').write_text('index changes\n')
        # Disable hostile helper for fixture creation, not for stw execution.
        setup('git', ['-c', 'core.fsmonitor=false', 'add', 'tracked.txt'], repo)
        (repo / 'tracked.txt').write_text('disk changes\n')
        unusual = ['line\nbreak.txt', '-leading.txt', 'literal[*].txt', 'unicode-ä.txt']
        for name in unusual:
            (repo / name).write_text('untracked\n')
        (repo / 'ignored.txt').write_text('ignored\n')
        output = observe('M2.files-workspaces', repo, ['--scope', 'staged', '--scope', 'disk', '--scope', 'untracked', '--scope', 'ignored'], status=('ok', 'partial'))
        if output is not None:
            paths = {item['path'] for item in output['files']}
            assert {*unusual, 'tracked.txt', 'ignored.txt'} <= paths, output
            assert any(item['registration'] == 'missing' for item in output['workspaces']), output
            assert any(item['path'] == str(linked) for item in output['workspaces']), output
        assert not sentinel.exists(), 'Repository-controlled helper executed'
        observe('M2.dirty-true', repo, ['--query', 'dirty'], predicate='dirty', value='true')
        filtered = observe('M2.literal-filter', repo, ['--scope', 'untracked', '--path', 'literal[*].txt'], status=('ok', 'partial'))
        if filtered is not None:
            assert [item['path'] for item in filtered['files']] == ['literal[*].txt'], filtered
        observe('M1.human', repo, ['--query', 'ref-exists', '--ref', 'refs/heads/custom-trunk'], predicate='ref-exists', value='true', human=True)
        observe('M1.error', root / 'not-a-repo', status='error')
        setup('git', ['config', 'filter.hostile.clean', str(helper)], repo)
        setup('git', ['config', 'filter.hostile.process', str(helper)], repo)
        (repo / '.gitattributes').write_text('tracked.txt filter=hostile\n')
        observe('M2.denied-filters', repo, ['--query', 'clean', '--scope', 'disk'], status='partial', predicate='clean', value='unknown')
        assert not sentinel.exists(), 'Repository-controlled clean/process filter executed'

        def jj(args, cwd=root):
            return setup('jj', ['--no-pager', '--color=never', *args], cwd)

        def workspace_expectation(cwd):
            # Use native fixture metadata, never the result under test, to determine
            # whether the supported version exposes every registered root.
            raw = jj(['--ignore-working-copy', 'workspace', 'list', '--template',
                      'json(self.name()) ++ "\\n" ++ json(self.root()) ++ "\\n"'], cwd)
            fields = [json.loads(line) for line in raw.splitlines()]
            assert len(fields) % 2 == 0, raw
            registrations = {}
            for name, path in zip(fields[::2], fields[1::2]):
                health = 'unknown' if path is None else ('present' if Path(path).is_dir() else 'missing')
                registrations[name] = (path, health)
            status = 'ok' if all(health == 'present' for _, health in registrations.values()) else 'partial'
            return status, registrations

        def assert_registrations(output, expected):
            if output is None:
                return
            records = {record['name']: record for record in output['workspaces']}
            assert set(records) == set(expected), (records, expected)
            for name, (path, health) in expected.items():
                assert records[name]['path'] == path and records[name]['registration'] == health, (records[name], expected)
                assert records[name]['manager'] == ('jj' if health == 'present' else 'unknown'), records[name]

        for colocated in (False, True):
            layout = 'colocated' if colocated else 'standalone'
            jjrepo = root / ('jj-colocated' if colocated else 'jj-standalone')
            jj(['git', 'init', '--colocate' if colocated else '--no-colocate', str(jjrepo)])
            observe(f'M3.{layout}.ref-absent', jjrepo, ['--query', 'ref-exists', '--ref', 'absent'], predicate='ref-exists', value='false')
            jj(['bookmark', 'create', 'task', '-r', '@'], jjrepo)
            observe(f'M3.{layout}.ref-present', jjrepo, ['--query', 'ref-exists', '--ref', 'task'], predicate='ref-exists', value='true')
            parent = jj(['log', '--no-graph', '-r', '@', '-T', 'commit_id'], jjrepo)
            jj(['new'], jjrepo)
            child = jj(['log', '--no-graph', '-r', '@', '-T', 'commit_id'], jjrepo)
            observe(f'M3.{layout}.ancestor-false', jjrepo, ['--query', 'ancestor', '--from', child, '--to', parent], predicate='ancestor', value='false')
            observe(f'M3.{layout}.ancestor-true', jjrepo, ['--query', 'ancestor', '--from', parent, '--to', child], predicate='ancestor', value='true')
            observe(f'M4.{layout}.stored', jjrepo, ['--scope', 'stored'])
            stored_name = 'literal[*]\nfile.txt'
            (jjrepo / stored_name).write_text('native stored change\n')
            jj(['describe', '-m', 'Stored filename fixture'], jjrepo)
            stored = observe(f'M4.{layout}.literal-stored', jjrepo, ['--scope', 'stored', '--path', stored_name])
            if stored is not None:
                assert [item['path'] for item in stored['files']] == [stored_name], stored
            (jjrepo / 'unsnapshotted.txt').write_text('unsnapshotted disk edits\n')
            output = observe(f'M4.{layout}.disk-unknown', jjrepo, status='partial')
            if output is not None:
                assert output['backend'] == 'jj', output
                assert output['boundary']['limitations'], output
            observe(f'M4.{layout}.clean-unknown', jjrepo, ['--query', 'clean'], status='partial', predicate='clean', value='unknown')
            observe(f'M4.{layout}.staging-unsupported', jjrepo, ['--scope', 'staged'], status='partial')
            secondary = root / ('colocated-secondary' if colocated else 'standalone-secondary')
            jj(['workspace', 'add', '--name', 'secondary', str(secondary)], jjrepo)
            status, registrations = workspace_expectation(jjrepo)
            output = observe(f'M4.{layout}.workspaces', jjrepo, ['--scope', 'stored'], status=status)
            assert_registrations(output, registrations)
            if output is not None:
                assert {item['name'] for item in output['workspaces']} >= {'default', 'secondary'}, output
            status, registrations = workspace_expectation(secondary)
            output = observe(f'M4.{layout}.secondary', secondary, ['--scope', 'stored'], status=status)
            assert_registrations(output, registrations)
            shutil.rmtree(secondary)
            # Native jj registration records may lack paths; absence must never trigger repair.
            status, registrations = workspace_expectation(jjrepo)
            assert status == 'partial' and registrations['secondary'][1] in ('missing', 'unknown'), registrations
            output = observe(f'M4.{layout}.missing', jjrepo, ['--scope', 'stored'], status='partial')
            assert_registrations(output, registrations)
            if output is not None:
                assert 'secondary' in {item['name'] for item in output['workspaces']}, output
        # M5 (Spec 027): ordinary large repositories give facts, listings are truncated with a disclosure,
        # a submodule checkout is described by its files and a gitlink-bearing superproject reports ordinary changes.
        big = root / 'large'
        setup('git', ['init', '-q', '-b', 'main', str(big)])
        for index in range(1100):
            (big / ('file%05d.txt' % index)).write_text('content %d\n' % index)
        setup('git', ['add', '.'], big)
        setup('git', ['commit', '-qm', 'many files'], big)
        output = observe('M5.large-repository', big, status='ok')
        if output is not None:
            assert output['workspaces'][0]['tracked']['value'] == 'false' and output['files'] == [], output
        head = setup('git', ['rev-parse', 'HEAD'], big)
        (big / '.git' / 'packed-refs').write_text('# pack-refs with: peeled fully-peeled sorted\n' + ''.join('%s refs/tags/t%04d\n' % (head, index) for index in range(700)))
        output = observe('M5.truncated-listing', big, status='partial')
        if output is not None:
            assert any(item.startswith('reference listing truncated: ') for item in output['boundary']['limitations']), output
            assert len(output['references']) < 701, output
        library = root / 'lib-src'
        setup('git', ['init', '-q', '-b', 'main', str(library)])
        (library / 'a').write_text('a\n')
        setup('git', ['add', '.'], library)
        setup('git', ['commit', '-qm', 'lib'], library)
        super_repo = root / 'super'
        setup('git', ['init', '-q', '-b', 'main', str(super_repo)])
        (super_repo / 's').write_text('s\n')
        setup('git', ['add', '.'], super_repo)
        setup('git', ['commit', '-qm', 'super'], super_repo)
        setup('git', ['-c', 'protocol.file.allow=always', 'submodule', 'add', '-q', str(library), 'mods/lib'], super_repo)
        setup('git', ['commit', '-qm', 'submodule'], super_repo)
        output = observe('M5.submodule-checkout', super_repo / 'mods' / 'lib', status='ok')
        if output is not None:
            workspace = output['workspaces'][0]
            assert workspace['path'].endswith('super/mods/lib') and workspace['tracked']['value'] == 'false', output
        (super_repo / 's').write_text('edited\n')
        output = observe('M5.gitlink-superproject', super_repo, ['--query', 'dirty'], predicate='dirty', value='true')
    selected = (milestone,) if milestone else tuple(CASE_IDS)
    expected = {case_id for group in selected for case_id in CASE_IDS[group]}
    assert completed == expected, ('Mandatory inventory cases differ', sorted(expected - completed), sorted(completed - expected))
    counts = ', '.join(group + '=' + str(len(CASE_IDS[group])) for group in selected)
    print(str(len(completed)) + ' native inventory CLI cases passed (' + counts + ') with byte/mode-identical target state')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--milestone', choices=tuple(CASE_IDS), help='Run a bounded milestone; default runs all mandatory cases')
    acceptance(milestone=parser.parse_args().milestone)
