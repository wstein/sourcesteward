#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""CI-only activation of the reviewed, unmodified Ubuntu bubblewrap profile.

Never changes sysctl settings, creates an unconfined policy, or edits the
packaged profile. The distro profile allows sandbox setup and denies child
capabilities; the runtime still requires read-only bindings and network denial.
"""
import hashlib
import json
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[1]
SOURCE = Path('/usr/share/apparmor/extra-profiles/bwrap-userns-restrict')
DESTINATION = Path('/etc/apparmor.d/bwrap-userns-restrict')
OVERRIDES = [Path('/etc/apparmor.d/local/bwrap-userns-restrict'), Path('/etc/apparmor.d/local/unpriv_bwrap')]


def validate_profile(source, digest, overrides):
    if source.is_symlink() or not source.is_file() or source.stat().st_size > 16_384:
        raise ValueError('Expected a bounded regular distro sandbox profile')
    if hashlib.sha256(source.read_bytes()).hexdigest() != digest:
        raise ValueError('Distro sandbox profile differs from the reviewed digest')
    for path in overrides:
        if path.is_symlink() or (path.exists() and (not path.is_file() or path.stat().st_size != 0)):
            raise ValueError('Local sandbox profile overrides are not permitted in conformance CI')


def activate():
    pins = json.loads((ROOT / 'scripts/backend-releases.json').read_text())
    pin = pins['minimum']['sandbox_profile']
    if pin != pins['current']['sandbox_profile']:
        raise ValueError('Both conformance lanes must use the same reviewed sandbox policy')
    version = subprocess.check_output(['dpkg-query', '-W', '-f=${Version}', 'apparmor-profiles'], timeout=15).decode('utf-8')
    if version != pin['package_version']:
        raise ValueError('Unexpected distro sandbox profile package version')
    validate_profile(SOURCE, pin['sha256'], OVERRIDES)
    if DESTINATION.exists() or DESTINATION.is_symlink():
        validate_profile(DESTINATION, pin['sha256'], OVERRIDES)
    subprocess.run(['sudo', 'install', '-m', '0644', str(SOURCE), str(DESTINATION)], check=True, timeout=15)
    validate_profile(DESTINATION, pin['sha256'], OVERRIDES)
    # No complain flag is present in the checked distro profile; default is enforce.
    subprocess.run(['sudo', 'apparmor_parser', '--replace', str(DESTINATION)], check=True, timeout=30)
    loaded = subprocess.check_output(['sudo', 'cat', '/sys/kernel/security/apparmor/profiles'], timeout=15).decode('utf-8').splitlines()
    if 'bwrap (enforce)' not in loaded or 'unpriv_bwrap (enforce)' not in loaded:
        raise ValueError('Both distro sandbox setup and child profiles must be enforced')
    print('Reviewed distro bubblewrap setup/child profiles loaded in enforce mode')


if __name__ == '__main__':
    activate()
