# SPDX-License-Identifier: EUPL-1.2
"""Offline regression tests for transient Hugo release download failures."""
import importlib.util
import io
import json
from pathlib import Path
import unittest
import tempfile
from unittest.mock import patch
from urllib.error import HTTPError, URLError

spec = importlib.util.spec_from_file_location('hugo_installer', Path(__file__).with_name('install-hugo.py'))
installer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(installer)


class DownloadTests(unittest.TestCase):
    def download(self, effects):
        with patch.object(installer, 'urlopen', side_effect=effects) as request, patch.object(installer.time, 'sleep') as sleep:
            result = installer.download('https://example.invalid/hugo')
        return result, request, sleep

    def test_transient_http_errors_retry_then_succeed(self):
        for code in (500, 502, 503, 504):
            with self.subTest(code=code):
                error = HTTPError('url', code, 'temporary', {}, None)
                result, request, sleep = self.download([error, io.BytesIO(b'archive')])
                self.assertEqual(result, b'archive')
                self.assertEqual(request.call_count, 2)
                sleep.assert_called_once_with(1)

    def test_transport_errors_retry(self):
        for error in (URLError('connection reset'), TimeoutError('timed out')):
            result, request, _ = self.download([error, io.BytesIO(b'archive')])
            self.assertEqual(result, b'archive')
            self.assertEqual(request.call_count, 2)

    def test_retries_are_bounded(self):
        error = HTTPError('url', 504, 'temporary', {}, None)
        with patch.object(installer, 'urlopen', side_effect=error) as request, patch.object(installer.time, 'sleep') as sleep:
            with self.assertRaises(HTTPError):
                installer.download('url')
            self.assertEqual(request.call_count, 3)
            self.assertEqual([call.args[0] for call in sleep.call_args_list], [1, 2])

    def test_permanent_http_errors_fail_immediately(self):
        for code in (403, 404):
            with self.subTest(code=code), patch.object(installer, 'urlopen', side_effect=HTTPError('url', code, 'permanent', {}, None)) as request, patch.object(installer.time, 'sleep') as sleep:
                with self.assertRaises(HTTPError):
                    installer.download('url')
                self.assertEqual(request.call_count, 1)
                sleep.assert_not_called()

    def test_reads_remain_bounded(self):
        with patch.object(installer, 'urlopen') as request:
            installer.download('url')
            request.assert_called_once_with('url', timeout=60)
            request.return_value.__enter__.return_value.read.assert_called_once_with(150_000_001)

    def test_retry_does_not_bypass_checksum_or_replace_destination(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'scripts').mkdir()
            (root / 'scripts/hugo-release.json').write_text(json.dumps({
                'version': '0.168.0', 'artifacts': {'linux-amd64': {
                    'name': 'hugo.tar.gz', 'sha256': '0' * 64}}}))
            destination = root / 'hugo'
            destination.write_bytes(b'existing')
            with patch.object(installer, 'ROOT', root), patch.object(installer.platform, 'system', return_value='Linux'), patch.object(installer.platform, 'machine', return_value='x86_64'), patch.object(installer.time, 'sleep'), patch.object(installer, 'urlopen', side_effect=[HTTPError('url', 504, 'temporary', {}, None), io.BytesIO(b'bad archive')]):
                with self.assertRaisesRegex(ValueError, 'checksum mismatch'):
                    installer.install(destination)
            self.assertEqual(destination.read_bytes(), b'existing')
