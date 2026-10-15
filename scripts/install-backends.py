#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Install reviewed conformance toolchains from checked official artifacts.

Git is built locally from checked source, without HTTP/GUI/Perl/Python helpers.
No installer scripts, package managers, remote shell code or cached binaries run.
"""
import argparse
import hashlib
import io
import json
from pathlib import Path, PurePosixPath
import platform
import posixpath
import shutil
import subprocess
import tarfile
import tempfile
from urllib.request import urlopen

ROOT = Path(__file__).resolve().parents[1]
LIMIT = 150_000_000


def load_pins():
    return json.loads((ROOT / 'scripts/backend-releases.json').read_text())


def download(url, sha256, limit=LIMIT):
    with urlopen(url, timeout=60) as response:
        raw = response.read(limit + 1)
    if len(raw) > limit or hashlib.sha256(raw).hexdigest() != sha256:
        raise ValueError('Backend release size/checksum mismatch')
    return raw


def safe_destination(path):
    path = path.absolute()
    for parent in (path, *path.parents):
        if parent.is_symlink():
            raise ValueError('Backend destination must not contain symlinks')
    return path


def verify_version(binary, tool, version):
    expected = ('git version ' if tool == 'git' else 'jj ') + version
    actual = subprocess.check_output([str(binary), '--version'], timeout=30).decode('utf-8')
    if actual != expected + '\n':
        raise ValueError('Unexpected backend runtime version: ' + actual)


def extract_jj(raw, destination):
    with tarfile.open(fileobj=io.BytesIO(raw), mode='r:gz') as archive:
        members = [member for member in archive if member.name in ('jj', './jj')]
        if len(members) != 1 or not members[0].isfile() or members[0].size > LIMIT:
            raise ValueError('Expected one regular jj archive executable')
        destination.write_bytes(archive.extractfile(members[0]).read())
    destination.chmod(0o755)


def extract_git(raw, destination, version):
    """Extract bounded source with validated internal symlinks, created last."""
    prefix = 'git-' + version
    with tarfile.open(fileobj=io.BytesIO(raw), mode='r:*') as archive:
        members = archive.getmembers()
        if len(members) > 20_000 or sum(member.size for member in members) > LIMIT:
            raise ValueError('Git source archive exceeds extraction budget')
        seen, links = set(), set()
        for member in members:
            path = PurePosixPath(member.name)
            if path.is_absolute() or '..' in path.parts or not path.parts or path.parts[0] != prefix or member.name.rstrip('/') != str(path) or member.name in seen:
                raise ValueError('Unexpected Git source archive path')
            seen.add(member.name)
            if member.issym():
                target = posixpath.normpath(posixpath.join(str(path.parent), member.linkname))
                if member.linkname.startswith('/') or not target.startswith(prefix + '/'):
                    raise ValueError('Git source symlink escapes source root')
                links.add(member.name)
            elif not (member.isfile() or member.isdir()):
                raise ValueError('Unexpected Git source archive member type')
        for member in members:
            if any(str(parent) in links for parent in PurePosixPath(member.name).parents):
                raise ValueError('Git source member descends through symlink')
            target = destination / member.name
            if member.isdir():
                target.mkdir(parents=True, exist_ok=True)
            elif member.isfile():
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(archive.extractfile(member).read())
                target.chmod(member.mode & 0o777)
        for member in members:
            if member.issym():
                target = destination / member.name
                target.parent.mkdir(parents=True, exist_ok=True)
                target.symlink_to(member.linkname)
    return destination / prefix


def install_jj(pin, destination):
    architecture = {'x86_64': 'amd64', 'aarch64': 'arm64', 'arm64': 'arm64'}.get(platform.machine())
    key = platform.system().lower() + '-' + str(architecture)
    if key not in pin['artifacts']:
        raise ValueError('No checked jj artifact for this platform: ' + key)
    artifact = pin['artifacts'][key]
    url = 'https://github.com/jj-vcs/jj/releases/download/v' + pin['version'] + '/' + artifact['name']
    raw = download(url, artifact['sha256'])
    destination = safe_destination(destination)
    destination.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='stw-jj-install-') as directory:
        binary = Path(directory) / 'jj'
        extract_jj(raw, binary)
        verify_version(binary, 'jj', pin['runtime_version'])
        target = safe_destination(destination / 'jj')
        shutil.copyfile(binary, target)
        target.chmod(0o755)
    print(target)


def install_git(pin, destination):
    version = pin['version']
    raw = download('https://www.kernel.org/pub/software/scm/git/git-' + version + '.tar.xz', pin['sha256'])
    destination = safe_destination(destination)
    destination.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='stw-git-build-') as directory:
        source = extract_git(raw, Path(directory), version)
        flags = ['prefix=' + str(destination), 'NO_CURL=YesPlease', 'NO_EXPAT=YesPlease', 'NO_OPENSSL=YesPlease', 'NO_GETTEXT=YesPlease', 'NO_TCLTK=YesPlease', 'NO_PERL=YesPlease', 'NO_PYTHON=YesPlease']
        subprocess.run(['make', '-j2', *flags, 'install'], cwd=source, check=True, timeout=900)
    verify_version(destination / 'bin/git', 'git', version)
    print(destination / 'bin/git')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('lane', choices=('minimum', 'current'))
    parser.add_argument('--tool', choices=('git', 'jj', 'both'), default='both')
    args = parser.parse_args()
    pin = load_pins()[args.lane]
    destination = ROOT / '.tools/conformance' / args.lane
    if args.tool in ('git', 'both'): install_git(pin['git'], destination / 'git')
    if args.tool in ('jj', 'both'): install_jj(pin['jj'], destination / 'jj')


if __name__ == '__main__':
    main()
