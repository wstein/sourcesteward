#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Install a checked official Hugo binary locally; never run installer scripts."""
import hashlib
import io
import json
from pathlib import Path
import platform
import re
import shutil
import subprocess
import tarfile
import tempfile
from urllib.request import urlopen

ROOT = Path(__file__).resolve().parents[1]

def install(destination):
    pin = json.loads((ROOT / 'scripts/hugo-release.json').read_text())
    architecture = {'x86_64': 'amd64', 'aarch64': 'arm64', 'arm64': 'arm64'}.get(platform.machine())
    key = platform.system().lower() + '-' + str(architecture)
    if key not in pin['artifacts']:
        raise ValueError('Unsupported platform; install official Hugo ' + pin['version'] + ' and set HUGO')
    artifact = pin['artifacts'][key]
    url = 'https://github.com/gohugoio/hugo/releases/download/v' + pin['version'] + '/' + artifact['name']
    with urlopen(url, timeout=60) as response:
        raw = response.read(150_000_001)
    if len(raw) > 150_000_000 or hashlib.sha256(raw).hexdigest() != artifact['sha256']:
        raise ValueError('Hugo release checksum mismatch')
    with tempfile.TemporaryDirectory(prefix='stw-hugo-install-') as directory:
        temporary = Path(directory)
        if artifact['name'].endswith('.pkg'):
            package = temporary / 'hugo.pkg'
            package.write_bytes(raw)
            # pkgutil extracts payloads; it does not run pre/post-install scripts.
            subprocess.run(['/usr/sbin/pkgutil', '--expand-full', str(package), str(temporary / 'expanded')], check=True)
            executable = temporary / 'expanded/Payload/hugo'
            if not executable.is_file() or executable.is_symlink(): raise ValueError('Unexpected Hugo package payload')
        else:
            with tarfile.open(fileobj=io.BytesIO(raw),mode='r:gz') as archive:
                member = archive.getmember('hugo')
                if not member.isfile() or member.size > 150_000_000: raise ValueError('Unexpected Hugo archive member')
                executable = temporary / 'hugo'
                executable.write_bytes(archive.extractfile(member).read())
        executable.chmod(0o755)
        version = subprocess.check_output([str(executable),'version'],text=True)
        if not re.match(r'^hugo v' + re.escape(pin['version']) + r'(?:-[0-9a-f]+)? ',version): raise ValueError('Unexpected Hugo runtime version: ' + version)
        if destination.is_symlink(): raise ValueError('Hugo destination must not be a symlink')
        destination.parent.mkdir(parents=True,exist_ok=True)
        shutil.copyfile(executable,destination)
        destination.chmod(0o755)
    print(destination)

if __name__ == '__main__':
    install(ROOT / '.tools/hugo')
