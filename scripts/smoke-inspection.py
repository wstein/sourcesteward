#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Real-process acceptance against disposable repositories only. Build bin/stw first."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
ROOT=Path(__file__).resolve().parents[1]
BINARY=ROOT/'bin/stw'
def run(args,cwd):
    return subprocess.run([str(BINARY),*args],cwd=cwd,capture_output=True,text=True)
def check(args,cwd,code,status=None,schema_version=1):
    p=run(args,cwd)
    assert p.returncode==code,(args,p.returncode,p.stdout,p.stderr)
    assert p.stderr.count('\n')==(0 if code==0 else 1),(args,p.stderr)
    if status:
        result=json.loads(p.stdout)
        assert result['schema_version']==schema_version and result['status']==status,result
    elif code: assert not p.stdout,p.stdout
    return p
with tempfile.TemporaryDirectory() as directory:
    root=Path(directory)
    check(['inspect','--json'],root,1,'error')
    check(['inspect'],root,1)
    check(['inspect','--json','a','b'],root,2)
    check(['inspect','--bogus'],root,2)
    check(['inspect','--help'],root,0)
    subprocess.run(['git','init','-q','-b','main',str(root/'repo')],check=True)
    repo=root/'repo'
    check(['inspect','--json'],repo,0,'ok')
    check(['inspect'],repo,0)
    (repo/'.jj').mkdir()
    check(['inspect','--json'],repo,1,'error',schema_version=2)
    check(['inspect'],repo,1)
    (repo/'.git/gitbutler').mkdir()
    result=json.loads(check(['inspect','--json'],repo,1,'unsupported').stdout)
    assert result['backend']=='ambiguous',result
    if os.name == 'posix':
        unusual=os.fsencode(root)+b'/invalid-\xff'
        try: os.mkdir(unusual)
        except OSError: print('Invalid UTF-8 cwd fixture unavailable on this filesystem.')
        else:
            result=json.loads(check(['inspect','--json'],unusual,1,'error').stdout)
            assert result['input_path']=='' and result['diagnostics'][0]['code']=='invalid-path',result
print('10 inspection process cases passed; optional invalid UTF-8 cwd checked where supported.')
