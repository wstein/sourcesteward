# SPDX-License-Identifier: EUPL-1.2
import hashlib
import importlib.util
import io
from pathlib import Path
import tarfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('hosting_gh_install', Path(__file__).with_name('install-hosting-gh.py'))
installer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(installer)


class InstallerTests(unittest.TestCase):
    def test_archive_checksum_and_exact_regular_executable(self):
        for linked in (False, True):
            buffer = io.BytesIO()
            with tarfile.open(fileobj=buffer, mode='w:gz') as archive:
                member = tarfile.TarInfo('gh_2.102.0_linux_amd64/bin/gh')
                if linked:
                    member.type = tarfile.SYMTYPE
                    member.linkname = '/etc/passwd'
                    archive.addfile(member)
                else:
                    member.size = 7
                    archive.addfile(member, io.BytesIO(b'fixture'))
            raw = buffer.getvalue()
            with self.assertRaises(ValueError):
                installer.extract(raw)
            with patch.object(installer, 'SHA256', hashlib.sha256(raw).hexdigest()):
                if linked:
                    with self.assertRaises(ValueError):
                        installer.extract(raw)
                else:
                    self.assertEqual(installer.extract(raw), b'fixture')
