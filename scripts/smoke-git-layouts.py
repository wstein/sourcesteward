#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Supplement each native CLI matrix with exact primary/linked Git layout cases.

Called by the existing smoke entry points after their original mandatory cases.
No CLI is built here. Every native write is confined to a disposable fixture.
"""
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
_spec = importlib.util.spec_from_file_location('layout_helpers', ROOT / 'scripts/smoke-jj-inspection.py')
helpers = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(helpers)
LAYOUTS = ('submodule', 'submodule-linked', 'separate', 'separate-linked')
NAMES = {
    'workspace': ('plan', 'apply', 'retry', 'handoff', 'stale-config', 'redirect'),
    'tidy': ('plan', 'workspace-retention', 'redirect'),
    'tidy-apply': ('plan', 'delete-recovery', 'retry', 'stale-config', 'redirect'),
    'retirement': ('plan', 'owned-remove', 'retry', 'stale-pointer', 'redirect'),
}
CASE_IDS = {family: tuple(layout + '.' + case for layout in LAYOUTS for case in names)
            for family, names in NAMES.items()}


def fixture(native, area, layout):
    """Return current, independently mapped primary, and common Git directory."""
    primary = area / 'primary'
    native(['init', '-q', '-b', 'trunk', str(primary)], area)
    native(['config', 'user.name', 'Layout Tester'], primary)
    native(['config', 'user.email', 'layout@example.invalid'], primary)
    (primary / 'tracked.txt').write_text('stored fixture\n')
    native(['add', '.'], primary)
    native(['commit', '-qm', 'initial'], primary)
    if layout.startswith('submodule'):
        superproject = area / 'super'
        native(['init', '-q', str(superproject)], area)
        native(['-c', 'protocol.file.allow=always', 'submodule', 'add', '-q', str(primary), 'member'], superproject)
        primary = superproject / 'member'
        common = superproject / '.git/modules/member'
    else:
        common = area / 'metadata'
        (primary / '.git').rename(common)
        (primary / '.git').write_text('gitdir: ' + str(common) + '\n')
    native(['config', 'user.name', 'Layout Tester'], primary)
    native(['config', 'user.email', 'layout@example.invalid'], primary)
    native(['config', 'core.worktree', os.path.relpath(primary, common)], primary)
    native(['branch', 'deletable', 'HEAD'], primary)
    native(['branch', 'keep', 'HEAD'], primary)
    native(['commit', '--allow-empty', '-qm', 'advance'], primary)
    source = primary
    if layout.endswith('-linked'):
        source = area / 'linked'
        native(['worktree', 'add', '-q', '--detach', str(source), 'HEAD'], primary)
    return source, primary, common


def acceptance(binary, family):
    if family not in CASE_IDS:
        raise ValueError('Unknown Git layout smoke family')
    if not Path(binary).is_file():
        raise RuntimeError('Build bin/stw before Git layout acceptance')
    git = shutil.which('git')
    if git is None:
        raise RuntimeError('Mandatory Git layout backend missing: git')
    helpers.check_sandbox()
    completed = set()
    with tempfile.TemporaryDirectory(prefix='stw-layout-' + family + '-') as directory:
        root = Path(directory).resolve()
        home = root / 'home'
        home.mkdir()
        env = helpers.clean_environment(home)

        def native(args, cwd):
            run = subprocess.run([git, *args], cwd=cwd, env=env, capture_output=True, timeout=20)
            assert run.returncode == 0, ('Native layout fixture failed', args, run.stderr)
            return run.stdout.decode().strip()

        for layout in LAYOUTS:
            area = root / layout
            area.mkdir()
            source, primary, common = fixture(native, area, layout)
            state = area / 'state'
            state.mkdir(mode=0o700)
            destination = area / 'task'
            expected_refs = native(['for-each-ref', '--format=%(refname) %(objectname)'], source)
            protected = {str(path): path.read_bytes() for path in
                         (source / 'tracked.txt', source / '.git', primary / 'tracked.txt', primary / '.git',
                          common / 'config', common / 'index', common / 'HEAD')}
            if layout.endswith('-linked'):
                registration = Path(native(['rev-parse', '--absolute-git-dir'], source))
                protected.update({str(path): path.read_bytes() for path in
                                  (registration / 'index', registration / 'HEAD', registration / 'gitdir', registration / 'commondir')})

            def mark(case):
                case_id = layout + '.' + case
                assert case_id in CASE_IDS[family] and case_id not in completed, case_id
                completed.add(case_id)

            def invoke(command, operation, args, ok=True, preserve=False):
                before = helpers.snapshot(area)
                run = subprocess.run([str(binary), command, operation, *map(str, args), '--json'],
                                     cwd=root, env=env, capture_output=True, timeout=75)
                value = json.loads(run.stdout)
                assert run.returncode == (0 if ok else 1), (command, operation, value, run.stderr)
                assert (value['status'] == 'ok') == ok, value
                if preserve:
                    without_state = lambda snap: {key: item for key, item in snap.items()
                                                  if key != 'state' and not key.startswith('state/')}
                    assert without_state(before) == without_state(helpers.snapshot(area)), ('Unexpected native layout write', family, layout, operation)
                return value

            def create(task, dest):
                value = invoke('workspace', 'plan', [source, '--base', 'HEAD', '--destination', dest,
                                                   '--task', task, '--owner', 'layout', '--state-dir', state], preserve=True)
                result = invoke('workspace', 'apply', ['--plan', value['plan']['plan_id'], '--state-dir', state, '--authorize'])
                return value['plan'], result['task']

            if family == 'workspace':
                plan = invoke('workspace', 'plan', [source, '--base', 'HEAD', '--destination', destination,
                                                  '--task', 'layout', '--owner', 'layout', '--state-dir', state], preserve=True)['plan']
                mark('plan')
                task = invoke('workspace', 'apply', ['--plan', plan['plan_id'], '--state-dir', state, '--authorize'])['task']
                assert task['native']['head_ref'] == '' and task['native']['repository_dir'] == str(common), task
                mark('apply')
                invoke('workspace', 'apply', ['--plan', plan['plan_id'], '--state-dir', state, '--authorize'], preserve=True)
                mark('retry')
                invoke('workspace', 'handoff', [destination, '--state-dir', state], preserve=True)
                mark('handoff')
                stale = invoke('workspace', 'plan', [source, '--base', 'HEAD', '--destination', area / 'stale',
                                                   '--task', 'stale', '--owner', 'layout', '--state-dir', state], preserve=True)['plan']
                native(['config', 'safe.layout-test', 'changed'], source)
                invoke('workspace', 'apply', ['--plan', stale['plan_id'], '--state-dir', state, '--authorize'], ok=False, preserve=True)
                native(['config', '--unset', 'safe.layout-test'], source)
                mark('stale-config')
            elif family in ('tidy', 'tidy-apply'):
                args = [source, '--base', 'HEAD', '--ref', 'refs/heads/deletable']
                plan = invoke('tidy', 'plan', [*args, '--state-dir', state], preserve=True)['plan']
                assert plan['candidates'][0]['decision'] == 'delete', plan
                mark('plan')
                if family == 'tidy':
                    retained = invoke('tidy', 'plan', [source, '--base', 'HEAD'], preserve=True)['plan']
                    assert any(c['name'] == 'refs/heads/trunk' and c['decision'] == 'retain' for c in retained['candidates']), retained
                    mark('workspace-retention')
                else:
                    before_refs = dict(line.split(' ', 1) for line in expected_refs.splitlines())
                    invoke('tidy', 'apply', ['--plan', plan['plan_id'], '--state-dir', state, '--authorize'])
                    after_refs = dict(line.split(' ', 1) for line in native(['for-each-ref', '--format=%(refname) %(objectname)'], source).splitlines())
                    original = before_refs.pop('refs/heads/deletable')
                    added = set(after_refs) - set(before_refs)
                    assert len(added) == 1 and next(iter(added)).startswith('refs/stw/recovery/')
                    assert after_refs.pop(next(iter(added))) == original and after_refs == before_refs
                    mark('delete-recovery')
                    invoke('tidy', 'apply', ['--plan', plan['plan_id'], '--state-dir', state, '--authorize'], preserve=True)
                    mark('retry')
                    stale = invoke('tidy', 'plan', [source, '--base', 'HEAD', '--ref', 'refs/heads/keep', '--state-dir', state], preserve=True)['plan']
                    native(['config', 'safe.layout-test', 'changed'], source)
                    invoke('tidy', 'apply', ['--plan', stale['plan_id'], '--state-dir', state, '--authorize'], ok=False, preserve=True)
                    native(['config', '--unset', 'safe.layout-test'], source)
                    mark('stale-config')
            else:
                _, task = create('layout', destination)
                args = [source, '--base', 'HEAD', '--task', 'layout', '--owner', 'layout', '--state-dir', state,
                        '--remove-registration', '--remove-files', destination]
                plan = invoke('tidy', 'plan', args, preserve=True)['retirement']
                mark('plan')
                invoke('tidy', 'apply', ['--plan', plan['plan_id'], '--state-dir', state, '--authorize'])
                assert not destination.exists() and not Path(task['native']['git_dir']).exists()
                mark('owned-remove')
                invoke('tidy', 'apply', ['--plan', plan['plan_id'], '--state-dir', state, '--authorize'], preserve=True)
                mark('retry')
                stale_destination = area / 'stale-task'
                create('stale', stale_destination)
                stale = invoke('tidy', 'plan', [source, '--base', 'HEAD', '--task', 'stale', '--owner', 'layout', '--state-dir', state,
                                              '--remove-registration', '--remove-files', stale_destination], preserve=True)['retirement']
                pointer = stale_destination / '.git'
                data = pointer.read_bytes()
                mode = pointer.stat().st_mode & 0o777
                pointer.rename(area / 'saved-pointer')
                pointer.write_bytes(data)
                pointer.chmod(mode)
                invoke('tidy', 'apply', ['--plan', stale['plan_id'], '--state-dir', state, '--authorize'], ok=False, preserve=True)
                mark('stale-pointer')
            # Rewriting our disposable config is fixture setup only. The product
            # must refuse redirection without changing a pointer, ref or file.
            assert (common / 'config').read_bytes() == protected[str(common / 'config')], 'Product changed configuration'
            native(['config', 'core.worktree', str(home)], source)
            if family == 'workspace':
                invoke('workspace', 'plan', [source, '--base', 'HEAD', '--destination', area / 'redirect',
                                           '--task', 'redirect', '--owner', 'layout', '--state-dir', state], ok=False, preserve=True)
            else:
                invoke('tidy', 'plan', [source, '--base', 'HEAD'], ok=False, preserve=True)
            mark('redirect')
            # Configuration was deliberately changed above; every other protected
            # source/primary/linked byte must still be exact after all CLI effects.
            for path, data in protected.items():
                if Path(path) != common / 'config':
                    assert Path(path).read_bytes() == data, ('Protected layout state changed', family, layout, path)
    assert completed == set(CASE_IDS[family]), ('Missing native Git layout cases', family, set(CASE_IDS[family]) - completed)
    return len(completed)
