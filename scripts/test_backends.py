# SPDX-License-Identifier: EUPL-1.2
"""Offline failure tests for the pinned conformance installers."""
import hashlib
import importlib.util
import io
from pathlib import Path
import tarfile
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('backends', ROOT / 'scripts/install-backends.py')
backends = importlib.util.module_from_spec(spec)
spec.loader.exec_module(backends)
smoke_spec = importlib.util.spec_from_file_location('smoke_jj', ROOT / 'scripts/smoke-jj-inspection.py')
smoke = importlib.util.module_from_spec(smoke_spec)
smoke_spec.loader.exec_module(smoke)


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

    def test_git_install_refuses_existing_prefix_before_build(self):
        with tempfile.TemporaryDirectory() as directory:
            with patch.object(backends, 'download', return_value=b'unused'):
                with self.assertRaises(ValueError):
                    backends.install_git({'version': '1', 'sha256': '0'*64}, Path(directory).resolve())

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
        self.assertLess(workflow.index('scripts/smoke-jj-inspection.py --sandbox-preflight'), workflow.index('scripts/install-backends.py'))

    def test_every_native_test_job_installs_and_requires_sandbox_before_tests(self):
        workflow = (ROOT / '.github/workflows/ci.yml').read_text()
        for name, following, test_command in (('check', 'conformance', 'run: make check-static'),
                                              ('conformance', 'pages', 'run: go test -count=1 -timeout 40m -race ./...')):
            with self.subTest(job=name):
                job = workflow.split('  ' + name + ':\n', 1)[1].split('\n  ' + following + ':\n', 1)[0]
                self.assertIn("STW_REQUIRE_NATIVE_SANDBOX: '1'", job)
                setup = job.index('sudo apt-get install --no-install-recommends -y bubblewrap apparmor apparmor-profiles')
                profile = job.index('python3 -B scripts/activate-sandbox-profile.py')
                preservation = job.index('test "$before" = "$(sandbox_settings)"')
                preflight = job.index('scripts/smoke-jj-inspection.py --sandbox-preflight')
                tests = job.index(test_command)
                self.assertLess(setup, profile)
                self.assertLess(profile, preservation)
                self.assertLess(preservation, preflight)
                self.assertLess(preflight, tests)
                for forbidden in ('--privileged', 'apparmor_restrict_unprivileged_userns=0', 'chmod u+s'):
                    self.assertNotIn(forbidden, job)

    def test_sandbox_preflight_requires_denied_write_and_successful_read(self):
        for write_code, read_code, passed in ((0, 0, False), (1, 1, False), (1, 0, True)):
            responses = [SimpleNamespace(returncode=write_code, stderr=b'denied'), SimpleNamespace(returncode=read_code, stderr=b'namespace denied')]
            with self.subTest(write=write_code, read=read_code), patch.object(smoke.platform, 'system', return_value='Darwin'), patch.object(smoke, 'sandbox_prefix', return_value=['/bwrap']), patch.object(smoke.subprocess, 'run', side_effect=responses):
                if passed: smoke.check_sandbox()
                else:
                    with self.assertRaises(RuntimeError): smoke.check_sandbox()

    def test_linux_payload_requires_zero_capabilities_enforced_child_and_private_network(self):
        valid = {'cap_eff': '0000000000000000', 'profile': 'bwrap//&unpriv_bwrap (enforce)', 'netns': 'net:[2]'}
        smoke.validate_linux_payload(valid, 'net:[1]')
        for field, invalid in [('cap_eff', '0000000000001000'), ('cap_eff', ''), ('profile', 'unconfined'), ('profile', 'unpriv_bwrap (complain)'), ('profile', 'bwrap (enforce)'), ('netns', 'net:[1]'), ('netns', 'missing')]:
            case = dict(valid)
            case[field] = invalid
            with self.subTest(field=field, value=invalid), self.assertRaises(RuntimeError):
                smoke.validate_linux_payload(case, 'net:[1]')

    def test_namespace_diagnostics_never_execute_jj_or_target_commands(self):
        variants = smoke.diagnostic_variants('/usr/bin/bwrap')
        self.assertEqual(len(variants), 3)
        for command in variants.values():
            self.assertEqual(command[-1], '/usr/bin/true')
            self.assertIn('--unshare-net', command)
            self.assertEqual(command[1:4], ['--ro-bind', '/', '/'])
        self.assertIn('--unshare-user', variants['explicit-user'])
        self.assertIn('--cap-drop', variants['explicit-user'])
        self.assertNotIn('--cap-drop', variants['setup-capability-diagnostic'])

    def test_distro_profile_digest_and_local_overrides_are_strict(self):
        spec = importlib.util.spec_from_file_location('profile', ROOT / 'scripts/activate-sandbox-profile.py')
        profile = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(profile)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / 'source'
            source.write_bytes(b'checked distro profile')
            digest = hashlib.sha256(source.read_bytes()).hexdigest()
            override = root / 'local'
            profile.validate_profile(source, digest, [override])
            with self.assertRaises(ValueError): profile.validate_profile(source, '0'*64, [override])
            override.write_text('allow capability,')
            with self.assertRaises(ValueError): profile.validate_profile(source, digest, [override])
            override.write_text('')
            profile.validate_profile(source, digest, [override])
            override.unlink()
            override.symlink_to(source)
            with self.assertRaises(ValueError): profile.validate_profile(source, digest, [override])
            source.unlink()
            source.symlink_to(override)
            with self.assertRaises(ValueError): profile.validate_profile(source, digest, [])
