#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Mandatory native Git/jj inventory acceptance against disposable fixtures only.

Build bin/stw first. Missing backends, sandbox enforcement, or cases fail acceptance.
Every CLI observation verifies byte/mode-identical target files, indexes and jj stores.
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


def acceptance(binary=BINARY):
    for backend in ('git', 'jj'):
        if shutil.which(backend) is None:
            raise RuntimeError('Mandatory inventory backend missing: ' + backend)
    if not binary.is_file():
        raise RuntimeError('Build bin/stw before inventory acceptance')
    helpers.check_sandbox()
    cases = 0
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

        def observe(path, args=(), status='ok', predicate=None, value=None, human=False):
            nonlocal cases
            before = helpers.snapshot(root)
            result = subprocess.run([str(binary), 'inventory', str(path), *args,
                                     *([] if human else ['--json'])], cwd=root, env=env,
                                    capture_output=True, timeout=20)
            assert helpers.snapshot(root) == before, ('Inventory changed fixture state', args)
            allowed_statuses = (status,) if isinstance(status, str) else status
            actual_status = status if human else json.loads(result.stdout)['status']
            assert actual_status in allowed_statuses, (args, result.stdout)
            expected = 0 if actual_status == 'ok' else 1
            assert result.returncode == expected, (args, result.returncode, result.stdout, result.stderr)
            assert result.stderr.count(b'\n') == expected, result.stderr
            cases += 1
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
        observe(repo, ['--query', 'clean'], predicate='clean', value='true')
        observe(repo, ['--query', 'dirty'], predicate='dirty', value='false')
        observe(repo, ['--query', 'ref-exists', '--ref', 'refs/heads/absent'], predicate='ref-exists', value='false')
        observe(repo, ['--query', 'ref-exists', '--ref', 'refs/heads/custom-trunk'], predicate='ref-exists', value='true')
        (repo / 'tracked.txt').write_text('staged\n')
        setup('git', ['add', 'tracked.txt'], repo)
        setup('git', ['commit', '-qm', 'second'], repo)
        second = setup('git', ['rev-parse', 'HEAD'], repo)
        observe(repo, ['--query', 'ancestor', '--from', second, '--to', initial], predicate='ancestor', value='false')
        observe(repo, ['--query', 'ancestor', '--from', initial, '--to', second], predicate='ancestor', value='true')
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
        output = observe(repo, ['--scope', 'staged', '--scope', 'disk', '--scope', 'untracked', '--scope', 'ignored'], status=('ok', 'partial'))
        paths = {item['path'] for item in output['files']}
        assert {*unusual, 'tracked.txt', 'ignored.txt'} <= paths, output
        assert any(item['registration'] == 'missing' for item in output['workspaces']), output
        assert any(item['path'] == str(linked) for item in output['workspaces']), output
        assert not sentinel.exists(), 'Repository-controlled helper executed'
        observe(repo, ['--query', 'dirty'], predicate='dirty', value='true')
        filtered = observe(repo, ['--scope', 'untracked', '--path', 'literal[*].txt'], status=('ok', 'partial'))
        assert [item['path'] for item in filtered['files']] == ['literal[*].txt'], filtered
        observe(repo, ['--query', 'ref-exists', '--ref', 'refs/heads/custom-trunk'], predicate='ref-exists', value='true', human=True)
        observe(root / 'not-a-repo', status='error')

        def jj(args, cwd=root):
            return setup('jj', ['--no-pager', '--color=never', *args], cwd)

        for colocated in (False, True):
            jjrepo = root / ('jj-colocated' if colocated else 'jj-standalone')
            jj(['git', 'init', '--colocate' if colocated else '--no-colocate', str(jjrepo)])
            observe(jjrepo, ['--query', 'ref-exists', '--ref', 'absent'], predicate='ref-exists', value='false')
            observe(jjrepo, ['--scope', 'stored'])
            (jjrepo / 'unsnapshotted.txt').write_text('unsnapshotted disk edits\n')
            output = observe(jjrepo, status='partial')
            assert output['backend'] == 'jj', output
            assert output['boundary']['limitations'], output
            observe(jjrepo, ['--query', 'clean'], status='partial', predicate='clean', value='unknown')
            observe(jjrepo, ['--scope', 'staged'], status='partial')
            secondary = root / ('colocated-secondary' if colocated else 'standalone-secondary')
            jj(['workspace', 'add', '--name', 'secondary', str(secondary)], jjrepo)
            output = observe(jjrepo, ['--scope', 'stored'])
            assert {item['name'] for item in output['workspaces']} >= {'default', 'secondary'}, output
            observe(secondary, ['--scope', 'stored'])
            shutil.rmtree(secondary)
            # Native jj registration records may lack paths; absence must never trigger repair.
            output = observe(jjrepo, ['--scope', 'stored'])
            assert 'secondary' in {item['name'] for item in output['workspaces']}, output
    if cases != 27:
        raise AssertionError('Mandatory inventory case count differs: ' + str(cases))
    print(str(cases) + ' native inventory CLI cases passed with byte/mode-identical target state')


if __name__ == '__main__':
    acceptance()
