#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Install the fixed official Linux AMD64 gh prerequisite for hosted conformance."""
import hashlib
import io
from pathlib import Path
import platform
import tarfile
from urllib.request import urlopen

VERSION = '2.102.0'
ASSET = 'gh_2.102.0_linux_amd64.tar.gz'
SHA256 = 'bb766f710eef8ede859c18578c72c327597cd4c8a85b06001b1f3843c6019386'
ROOT = Path(__file__).resolve().parents[1]


def extract(raw):
    if len(raw) > 100_000_000 or hashlib.sha256(raw).hexdigest() != SHA256:
        raise ValueError('gh archive checksum or size mismatch')
    with tarfile.open(fileobj=io.BytesIO(raw), mode='r:gz') as archive:
        member = archive.getmember('gh_' + VERSION + '_linux_amd64/bin/gh')
        if not member.isfile() or not 0 < member.size <= 100_000_000:
            raise ValueError('Invalid gh executable archive member')
        return archive.extractfile(member).read()


def main():
    if platform.system() != 'Linux' or platform.machine() not in ('x86_64', 'amd64'):
        raise ValueError('This CI prerequisite installer supports Linux AMD64 only; install gh 2.102.0+ with your platform package manager')
    with urlopen('https://github.com/cli/cli/releases/download/v' + VERSION + '/' + ASSET, timeout=60) as response:
        executable = extract(response.read(100_000_001))
    destination = ROOT / '.tools/hosting-gh/bin/gh'
    for path in (destination, destination.parent, destination.parent.parent, destination.parent.parent.parent):
        if path.is_symlink():
            raise ValueError('Refusing linked gh prerequisite destination')
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_bytes(executable)
    destination.chmod(0o755)
    print(destination)


if __name__ == '__main__':
    main()
