#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Mandatory real-process jj acceptance; no missing-tool or sandbox skips.

Run --preflight minimum|current before the required Go conformance suite.
Run without options after building bin/stw. All repositories are disposable.
"""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import platform
import re
import shutil
import stat
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
BINARY = ROOT / 'bin/stw'
PREFIX = ['--ro-bind', '/', '/', '--unshare-net', '--die-with-parent', '--new-session', '--cap-drop', 'ALL', '--']


def clean_environment(home):
    env = {key: value for key, value in os.environ.items() if not key.startswith(('JJ_', 'GIT_', 'RUST_'))}
    env.update(HOME=str(home), XDG_CONFIG_HOME=str(home / 'config'), XDG_CACHE_HOME=str(home / 'cache'), JJ_CONFIG='', GIT_CONFIG_NOSYSTEM='1', GIT_CONFIG_GLOBAL=str(home / 'gitconfig'), GIT_TERMINAL_PROMPT='0', GIT_TEMPLATE_DIR=str(home / 'templates'), LC_ALL='C')
    return env


def sandbox_prefix():
    if platform.system() == 'Linux':
        binary = shutil.which('bwrap')
        if binary is None: raise RuntimeError('Required bubblewrap sandbox unavailable')
        return [binary, *PREFIX]
    if platform.system() == 'Darwin':
        binary = '/usr/bin/sandbox-exec'
        if not Path(binary).is_file(): raise RuntimeError('Required sandbox-exec unavailable')
        return [binary, '-p', '(version 1) (allow default) (deny file-write*) (deny network*)']
    raise RuntimeError('No supported enforced read-only sandbox on this host')


def check_sandbox():
    prefix = sandbox_prefix()
    with tempfile.TemporaryDirectory(prefix='stw-sandbox-preflight-') as directory:
        probe = Path(directory) / 'probe'
        probe.write_text('unchanged')
        env = clean_environment(Path(directory))
        # Fixed trusted shell text only in the test probe; the path is an argv value.
        denied = subprocess.run([*prefix, '/bin/sh', '-c', 'printf changed > "$1"', 'probe', str(probe)], env=env, capture_output=True, timeout=15)
        if denied.returncode == 0 or probe.read_text() != 'unchanged':
            raise RuntimeError('Required sandbox did not prevent filesystem writes')
        result = subprocess.run([*prefix, '/usr/bin/true'], env=env, capture_output=True, timeout=15)
        if result.returncode:
            raise RuntimeError('Required read-only sandbox cannot execute; install a supported host sandbox without relaxing protections')
    print('Enforced read-only sandbox capability verified')


def preflight(lane):
    spec = importlib.util.spec_from_file_location('backends', ROOT / 'scripts/install-backends.py')
    installer = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(installer)
    pins = installer.load_pins()[lane]
    for tool in ('git', 'jj'):
        executable = shutil.which(tool)
        if executable is None: raise RuntimeError('Required backend unavailable: ' + tool)
        installer.verify_version(Path(executable), tool, pins[tool].get('runtime_version', pins[tool]['version']))
    check_sandbox()
    with tempfile.TemporaryDirectory(prefix='stw-jj-preflight-') as directory:
        result = subprocess.run([*sandbox_prefix(), shutil.which('jj'), '--version'], env=clean_environment(Path(directory)), capture_output=True, timeout=15)
        if result.returncode or result.stdout.decode('utf-8') != 'jj ' + pins['jj']['runtime_version'] + '\n':
            raise RuntimeError('Required read-only sandbox cannot execute the pinned jj backend')
    print('Exact ' + lane + ' Git/jj versions and enforced read-only sandbox verified')


def snapshot(root):
    result = {}
    for parent, directories, files in os.walk(root, followlinks=False):
        for name in sorted(directories + files):
            path = Path(parent) / name
            metadata = path.lstat()
            if stat.S_ISLNK(metadata.st_mode): content = ('link', os.readlink(path))
            elif stat.S_ISREG(metadata.st_mode): content = ('file', path.read_bytes())
            elif stat.S_ISDIR(metadata.st_mode): content = ('directory',)
            else: raise AssertionError('Unexpected fixture file type: ' + str(path))
            result[str(path.relative_to(root))] = (metadata.st_mode, content)
    return result


def acceptance():
    if shutil.which('jj') is None or shutil.which('git') is None:
        raise RuntimeError('Required real Git and jj executables must be installed')
    if not BINARY.is_file(): raise RuntimeError('Build bin/stw before acceptance')
    cases = 0
    with tempfile.TemporaryDirectory(prefix='stw-jj-conformance-') as directory:
        root = Path(directory).resolve()
        home = root / 'home'
        home.mkdir()
        env = clean_environment(home)

        def jj(args, cwd=root):
            result = subprocess.run(['jj', '--no-pager', '--color=never', *args], cwd=cwd, env=env, capture_output=True, timeout=15)
            if result.returncode: raise AssertionError(('Fixture setup failed', args, result.stderr.decode('utf-8', errors='replace')))

        def inspect(path, cwd=root, human=False):
            nonlocal cases
            before = snapshot(root)
            result = subprocess.run([str(BINARY), 'inspect', str(path), *([] if human else ['--json'])], cwd=cwd, env=env, capture_output=True, timeout=20)
            assert snapshot(root) == before, 'Inspection changed fixture files or repository metadata'
            assert result.returncode == 0, (path, result.returncode, result.stderr)
            assert not result.stderr, result.stderr
            cases += 1
            if human:
                assert b'stored' in result.stdout.lower() and b'snapshotted' in result.stdout, result.stdout
                return None
            value = json.loads(result.stdout)
            assert value['schema_version'] == 2 and value['backend'] == 'jj' and value['status'] == 'ok', value
            assert value['repository'] is None and value['diagnostics'] == [], value
            identity = value['jj_repository']
            for name, pattern in [('change_id', r'[k-z]{32}'), ('commit_id', r'[0-9a-f]{40}'), ('operation_id', r'[0-9a-f]{128}')]:
                assert re.fullmatch(pattern, identity[name]), identity
            assert identity['observation']['working_copy'] == 'stored-unsnapshotted', identity
            assert identity['observation']['limitation'], identity
            return identity

        standalone = root / 'standalone'
        jj(['git', 'init', '--no-colocate', str(standalone)])
        first = inspect(standalone)
        assert Path(first['workspace_root']) == standalone, first
        nested = standalone / 'nested'
        nested.mkdir()
        inspect(nested)
        inspect(standalone, human=True)
        (standalone / 'untracked.txt').write_text('disk changes are not snapshotted\n')
        edited = inspect(standalone)
        assert (edited['change_id'], edited['commit_id'], edited['operation_id']) == (first['change_id'], first['commit_id'], first['operation_id']), edited
        colocated = root / 'colocated'
        jj(['git', 'init', '--colocate', str(colocated)])
        inspect(colocated)
        secondary = root / 'secondary'
        jj(['workspace', 'add', '--name', 'secondary', str(secondary)], standalone)
        second = inspect(secondary)
        assert second['workspace_name'] == 'secondary' and Path(second['workspace_root']) == secondary, second
        assert second['repository_dir'] == first['repository_dir'], second
    if cases != 6: raise AssertionError('Mandatory fixture count did not execute: ' + str(cases))
    print(str(cases) + ' real jj CLI cases passed with byte/mode-identical fixture state')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument('--preflight', choices=('minimum', 'current'))
    mode.add_argument('--sandbox-preflight', action='store_true')
    args = parser.parse_args()
    if args.sandbox_preflight: check_sandbox()
    elif args.preflight: preflight(args.preflight)
    else: acceptance()


if __name__ == '__main__':
    main()
