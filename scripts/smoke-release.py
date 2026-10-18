#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Opt-in release conformance using an already supplied stw binary.

This harness creates disposable native fixtures. It never compiles a CLI or runs a
declared project build. Run it separately in each pinned jj lane after authorization
to provide the binary. Source tests exercise the assertions without running it.
"""
import importlib.util
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('release_jj_helpers', ROOT / 'scripts/smoke-jj-inspection.py')
helpers = importlib.util.module_from_spec(spec)
spec.loader.exec_module(helpers)


def assert_result(result, command, status):
    assert result['command'] == command, result
    assert result['status'] == status, result
    effects = result['boundary']['effects']
    assert effects['network'] is False, result
    assert effects['writes'] is False, result
    assert effects['project_commands'] is False, result
    if command == 'release-plan':
        assert result['network'] == [], result
        if status == 'ok':
            assert result['verdict'] in ('fail', 'incomplete'), result
            assert all(g['outcome'] == 'not-run' for g in result['gates'][-3:]), result


def acceptance(binary):
    binary = Path(binary).resolve(strict=True)
    tools = {name: shutil.which(name) for name in ('git', 'jj')}
    if not all(tools.values()):
        raise RuntimeError('Mandatory native Git/jj tooling missing')
    helpers.check_sandbox()
    cases = 0
    with tempfile.TemporaryDirectory(prefix='stw-release-conformance-') as temporary:
        root = Path(temporary).resolve()
        home = root / 'home'
        home.mkdir()
        env = helpers.clean_environment(home)
        env.update(GIT_AUTHOR_NAME='Release Tester', GIT_AUTHOR_EMAIL='release@example.org',
                   GIT_COMMITTER_NAME='Release Tester', GIT_COMMITTER_EMAIL='release@example.org')
        # An accidental project invocation would fail and leave a visible marker.
        fixtures = root / 'fixtures'
        fixtures.mkdir()

        def setup(tool, args, directory):
            completed = subprocess.run([tools[tool], *args], cwd=directory, env=env,
                                       capture_output=True, timeout=30, check=True)
            return completed.stdout.decode().strip()

        def observe(directory, args, status, expected, code=None):
            nonlocal cases
            # snapshot covers the working-copy commit and operation log bytes as well.
            before = helpers.snapshot(fixtures)
            completed = subprocess.run([str(binary), 'release', *args, '--json'],
                                       cwd=directory, env=env, capture_output=True, timeout=30)
            assert completed.returncode == expected, (args, completed.stderr)
            result = json.loads(completed.stdout)
            assert_result(result, 'release-' + args[0], status)
            if code:
                assert result['error']['code'] == code, result
            assert helpers.snapshot(fixtures) == before, args
            cases += 1
            return result

        for backend in ('git', '--colocate', '--no-colocate'):
            directory = fixtures / backend.lstrip('-')
            directory.mkdir()
            if backend == 'git':
                setup('git', ['init', '-q', '-b', 'main'], directory)
            else:
                setup('jj', ['git', 'init', backend, str(directory)], directory)
            (directory / '.stw').mkdir()
            (directory / '.stw/release.toml').write_bytes(
                (ROOT / 'specs/026-release-process/examples/skill-pack.release.toml').read_bytes())
            if backend == 'git':
                setup('git', ['add', '.'], directory)
                setup('git', ['commit', '-qm', 'fixture'], directory)
                commit = setup('git', ['rev-parse', 'HEAD'], directory)
                setup('git', ['remote', 'add', 'origin', 'https://github.com/example/skills.git'], directory)
                setup('git', ['update-ref', 'refs/remotes/origin/main', commit], directory)
            else:
                setup('jj', ['commit', '-m', 'fixture'], directory)
                commit = setup('jj', ['--ignore-working-copy', 'log', '--no-graph', '-r', '@-', '-T', 'commit_id'], directory)
                setup('jj', ['git', 'remote', 'add', 'origin', 'https://github.com/example/skills.git'], directory)
            (directory / '.stw/release.toml').write_text('uncommitted broken config\n')
            (directory / 'unsnapshotted').write_text('not part of the commit\n')
            observe(directory, ['check', '--commit', commit], 'ok', 0)
            result = observe(directory, ['plan', commit, '--version', 'v1.2.3'], 'ok', 3)
            assert result['gates'][4]['code'] == 'readiness-missing', result
            assert result['gates'][4]['outcome'] == 'fail', result
            if backend != 'git':
                assert result['gates'][0]['reason'] == 'main-ref-absent', result
                observe(directory, ['plan', 'v1.2.3'], 'refused', 1, 'unsupported-backend')
    print(f'{cases} release read-only conformance cases passed')


if __name__ == '__main__':
    acceptance(sys.argv[1] if len(sys.argv) == 2 else ROOT / 'bin/stw')
