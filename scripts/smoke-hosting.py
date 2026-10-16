#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Mandatory native hosting conformance; only disposable fixtures and fake API data.

Build bin/stw and bin/gh-jj/gh-jj first. Actual gh dispatch is exercised through a
private extension directory; no user installation, authentication or network call.
"""
import importlib.util
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[1]
_spec = importlib.util.spec_from_file_location('jj_smoke', ROOT / 'scripts/smoke-jj-inspection.py')
helpers = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(helpers)


def acceptance():
    tools = {name: shutil.which(name) for name in ('git', 'jj', 'gh')}
    if not all(tools.values()):
        raise RuntimeError('Mandatory Git/jj/gh tooling missing: ' + str(tools))
    binaries = [ROOT / 'bin/stw', ROOT / 'bin/gh-jj/gh-jj']
    if not all(path.is_file() for path in binaries):
        raise RuntimeError('Build both SourceSteward entry points first')
    version = subprocess.run([tools['gh'], '--version'], capture_output=True, text=True, check=True).stdout
    match = re.search(r'gh version (\d+)\.(\d+)\.(\d+)', version)
    if not match or tuple(map(int, match.groups())) < (2, 102, 0):
        raise RuntimeError('Hosting dispatch requires tested gh >= 2.102.0: ' + version)
    helpers.check_sandbox()
    cases = 0
    with tempfile.TemporaryDirectory(prefix='stw-hosting-conformance-') as directory:
        root = Path(directory).resolve()
        fixtures = root / 'fixtures'
        fixtures.mkdir()
        home = root / 'home'
        home.mkdir()
        env = helpers.clean_environment(home)
        for key in tuple(env):
            if key.startswith(('GH_', 'GITHUB_')):
                del env[key]
        env.update(GIT_AUTHOR_NAME='Hosting Tester', GIT_AUTHOR_EMAIL='hosting@example.org',
                   GIT_COMMITTER_NAME='Hosting Tester', GIT_COMMITTER_EMAIL='hosting@example.org',
                   GH_CONFIG_DIR=str(home / 'gh'), GH_PROMPT_DISABLED='1')
        extension = home / 'gh/extensions/gh-jj'
        extension.mkdir(parents=True)
        shutil.copy2(binaries[1], extension / 'gh-jj')
        fake = root / 'fake-tools'
        fake.mkdir()
        audit = root / 'api-audit.jsonl'
        fakegh = fake / 'gh'
        fakegh.write_text('#!' + sys.executable + '\n' + '''import json,sys,os
args=sys.argv[1:]
if args==['--version']:
 print('gh version 2.102.0');sys.exit(0)
assert args[0]=='api' and '--method' in args and args[args.index('--method')+1]=='GET', args
assert '--hostname' in args and args[args.index('--hostname')+1]=='github.com', args
endpoint=next(x for x in args if x.startswith('repos/'))
assert endpoint.startswith('repos/example/project/'),endpoint
with open(os.environ['STW_TEST_API_AUDIT'],'a') as f:f.write(json.dumps(args)+'\\n')
if '/pulls/' in endpoint:
 value={'number':42,'title':'Fixture PR','html_url':'https://github.com/example/project/pull/42','state':'open','draft':False,'head':{'sha':'a'*40,'ref':'feature','repo':{'full_name':'example/fork'}},'base':{'sha':'b'*40,'ref':'main','repo':{'full_name':'example/project'}}}
elif '/check-runs?' in endpoint:
 value={'total_count':1,'check_runs':[{'id':11,'name':'test','status':'queued','conclusion':None,'head_sha':'a'*40,'html_url':''}]}
elif '/statuses?' in endpoint:
 value=[{'id':12,'context':'lint','state':'success','description':'Done','target_url':''}]
else:raise AssertionError(endpoint)
print(json.dumps(value))
''')
        fakegh.chmod(0o755)
        api_env = dict(env, PATH=str(fake) + os.pathsep + env['PATH'], GH_TOKEN='fixture-only-not-a-secret', STW_TEST_API_AUDIT=str(audit))

        def setup(tool, args, cwd=fixtures):
            result = subprocess.run([tools[tool], *args], cwd=cwd, env=env, capture_output=True, timeout=30)
            assert result.returncode == 0, ('setup', tool, args, result.stderr)
            return result.stdout.decode().strip()

        def observe(command, path, options=(), expected=0, network=False, actual_dispatch=False):
            nonlocal cases
            before = helpers.snapshot(fixtures)
            prior_calls = audit.read_bytes() if audit.exists() else b''
            if actual_dispatch:
                args = [tools['gh'], 'jj', *command, str(path), *options, '--json']
            else:
                args = [str(binaries[0]), 'hosting', *command, str(path), *options, '--json']
            result = subprocess.run(args, cwd=root, env=api_env, capture_output=True, timeout=45)
            if not network:
                assert (audit.read_bytes() if audit.exists() else b'') == prior_calls, ('local resolution launched gh API', args)
            assert helpers.snapshot(fixtures) == before, ('hosting changed native state', args)
            assert result.returncode == expected, (args, result.returncode, result.stdout, result.stderr)
            output = json.loads(result.stdout)
            assert output['schema_version'] == 1 and output['status'] == ('ok' if expected == 0 else 'error'), output
            assert output['boundary']['complete'] == (expected == 0), output
            assert output['boundary']['network'] == network, output
            assert all(isinstance(output[key], list) for key in ('candidates', 'checks', 'statuses', 'diagnostics')), output
            if expected == 0:
                assert output['repository']['owner'] == 'example' and output['repository']['name'] == 'project', output
            cases += 1
            return output

        git = fixtures / 'git'
        setup('git', ['init', '-q', '-b', 'custom-trunk', str(git)])
        setup('git', ['-C', str(git), 'commit', '--allow-empty', '-qm', 'initial'])
        setup('git', ['-C', str(git), 'remote', 'add', 'custom', 'https://github.com/example/project.git'])
        linked = fixtures / 'linked'
        setup('git', ['-C', str(git), 'worktree', 'add', '-b', 'task', str(linked)])
        for path in (git, linked):
            observe(['repo'], path)
            observe(['pr', '42'], path, network=True)
            observe(['ci', '42'], path, network=True)
        observe(['repo'], git, expected=1, actual_dispatch=True)
        standalone = fixtures / 'standalone'
        colocated = fixtures / 'colocated'
        for path, flag in ((standalone, '--no-colocate'), (colocated, '--colocate')):
            setup('jj', ['git', 'init', flag, str(path)])
            setup('jj', ['git', 'remote', 'add', 'custom', 'https://github.com/example/project.git'], path)
            (path / 'unsnapshotted.txt').write_text('unrecorded disk work\n')
        secondary = fixtures / 'secondary'
        setup('jj', ['workspace', 'add', '--name', 'secondary', str(secondary)], standalone)
        for path in (standalone, colocated, secondary):
            regular = observe(['repo'], path)
            dispatched = observe(['repo'], path, actual_dispatch=True)
            for key in ('backend', 'repository', 'candidates'):
                assert regular[key] == dispatched[key], (key, regular, dispatched)
            assert regular['backend'] == 'jj', regular
            assert regular['identity']['jj_repository']['observation']['working_copy'] == 'stored-unsnapshotted', regular
            observe(['pr', '42'], path, network=True)
            ci = observe(['ci', '42'], path, network=True)
            assert ci['pull_request']['head_sha'] == 'a' * 40 and len(ci['checks']) == len(ci['statuses']) == 1, ci
        setup('git', ['-C', str(git), 'remote', 'add', 'fork', 'https://github.com/another/fork.git'])
        observe(['repo'], git, expected=1)
        observe(['repo'], git, options=('--remote', 'custom'))
        observe(['repo'], fixtures / 'missing', expected=1)
        calls = [json.loads(line) for line in audit.read_text().splitlines()]
        assert calls and all('--method' in call and call[call.index('--method') + 1] == 'GET' for call in calls), calls
    assert cases == 22, ('Mandatory hosting case count changed', cases)
    print(str(cases) + ' native hosting/extension cases passed with byte/mode-identical fixture state and GET-only fake API reads')


if __name__ == '__main__':
    acceptance()
