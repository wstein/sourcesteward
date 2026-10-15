# SPDX-License-Identifier: EUPL-1.2
"""Offline failure tests for the pinned conformance installers."""
import hashlib
import importlib.util
import io
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('backends', ROOT / 'scripts/install-backends.py')
backends = importlib.util.module_from_spec(spec)
spec.loader.exec_module(backends)


def archive(entries):
    output = io.BytesIO()
    with tarfile.open(fileobj=output, mode='w:gz') as tar:
        for name, content, kind in entries:
            member = tarfile.TarInfo(name)
            if kind == 'link':
                member.type = tarfile.SYMTYPE
                member.linkname = content
                tar.addfile(member)
            else:
                member.size = len(content)
                member.mode = 0o755
                tar.addfile(member, io.BytesIO(content))
    return output.getvalue()


class BackendInstallerTests(unittest.TestCase):
    def test_checked_download_rejects_checksum_and_size(self):
        class Response(io.BytesIO):
            def __enter__(self): return self
            def __exit__(self, *args): self.close()
        for raw, digest, limit in ((b'bad', '0'*64, 10), (b'large', hashlib.sha256(b'large').hexdigest(), 3)):
            with patch.object(backends, 'urlopen', return_value=Response(raw)):
                with self.assertRaises(ValueError): backends.download('https://example.org/checked', digest, limit)
        with patch.object(backends, 'urlopen', return_value=Response(b'ok')):
            self.assertEqual(backends.download('https://example.org/checked', hashlib.sha256(b'ok').hexdigest(), 3), b'ok')

    def test_jj_archive_requires_one_regular_exact_binary(self):
        cases = [[], [('elsewhere/jj', b'x', 'file')], [('jj', 'elsewhere', 'link')], [('jj', b'a', 'file'), ('jj', b'b', 'file')], [('jj', b'a', 'file'), ('./jj', b'b', 'file')]]
        for entries in cases:
            with self.subTest(entries=entries), tempfile.TemporaryDirectory() as directory:
                with self.assertRaises(ValueError): backends.extract_jj(archive(entries), Path(directory) / 'jj')
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'jj'
            backends.extract_jj(archive([('./jj', b'binary', 'file')]), path)
            self.assertEqual(path.read_bytes(), b'binary')
            self.assertEqual(path.stat().st_mode & 0o777, 0o755)

    def test_git_archive_rejects_traversal_links_and_duplicate_members(self):
        cases = [[('../outside', b'x', 'file')], [('git-1/file', '../../outside', 'link')], [('git-1/file', b'a', 'file'), ('git-1/file', b'b', 'file')], [('git-2/file', b'a', 'file')], [('git-1/link', 'file', 'link'), ('git-1/link/child', b'a', 'file')]]
        for entries in cases:
            with self.subTest(entries=entries), tempfile.TemporaryDirectory() as directory:
                with self.assertRaises(ValueError): backends.extract_git(archive(entries), Path(directory), '1')
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            backends.extract_git(archive([('git-1/file', b'ok', 'file'), ('git-1/RelNotes', 'file', 'link')]), root, '1')
            self.assertEqual((root / 'git-1/RelNotes').read_bytes(), b'ok')

    def test_version_mismatch_and_missing_binary_fail(self):
        for output in (b'jj 0.1.0\n', b'jj 0.46.0-extra\n'):
            with patch.object(backends.subprocess, 'check_output', return_value=output):
                with self.assertRaises(ValueError): backends.verify_version(Path('/jj'), 'jj', '0.46.0')
        with patch.object(backends.subprocess, 'check_output', side_effect=FileNotFoundError):
            with self.assertRaises(FileNotFoundError): backends.verify_version(Path('/jj'), 'jj', '0.46.0')
        with patch.object(backends.subprocess, 'check_output', return_value=b'jj 0.46.0\n'):
            backends.verify_version(Path('/jj'), 'jj', '0.46.0')

    def test_destination_symlink_refused(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'link').symlink_to(root, target_is_directory=True)
            with self.assertRaises(ValueError): backends.safe_destination(root / 'link' / 'jj')

    def test_manifest_pins_two_toolchain_lanes(self):
        pins = backends.load_pins()
        self.assertEqual(set(pins), {'minimum', 'current'})
        for lane in pins.values():
            self.assertRegex(lane['git']['sha256'], r'^[a-f0-9]{64}$')
            for artifact in lane['jj']['artifacts'].values():
                self.assertRegex(artifact['sha256'], r'^[a-f0-9]{64}$')
                self.assertTrue(artifact['name'].endswith('.tar.gz'))

    def test_ci_requires_both_lanes_and_exact_artifact_gate(self):
        workflow = (ROOT / '.github/workflows/ci.yml').read_text()
        for required in ('lane: [minimum, current]', 'STW_REQUIRE_JJ:', 'scripts/smoke-jj-inspection.py', 'needs: [check, conformance]'):
            self.assertIn(required, workflow)
        for forbidden in ('--privileged', 'apparmor_restrict_unprivileged_userns=0', 'chmod u+s'):
            self.assertNotIn(forbidden, workflow)
