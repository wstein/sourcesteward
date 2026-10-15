#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Require actual native retirement acceptance and mandatory tools."""
import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

_spec = importlib.util.spec_from_file_location('retirement_smoke', Path(__file__).with_name('smoke-retirement.py'))
smoke = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(smoke)


class RetirementSmokeTests(unittest.TestCase):
    def test_manifest_requires_git_and_both_jj_layouts(self):
        self.assertEqual(set(smoke.CASE_IDS), {'git', 'jj-standalone', 'jj-colocated'})
        for group, cases in smoke.CASE_IDS.items():
            self.assertEqual(len(cases), len(set(cases)))
            self.assertEqual(len(cases), 12)
            for name in ('authorization', 'owned-clean-remove', 'metadata-only', 'current-workspace',
                         'wrong-owner', 'dirty', 'untracked', 'ignored', 'symlink',
                         'unknown-metadata', 'stale-files', 'stale-inode'):
                self.assertIn(group + '.' + name, cases)

    def test_missing_backend_is_failure(self):
        for missing in ('git', 'jj'):
            with self.subTest(missing=missing), patch.object(smoke.shutil, 'which', side_effect=lambda name: None if name == missing else '/bin/' + name):
                with self.assertRaisesRegex(RuntimeError, 'Mandatory retirement backend missing: ' + missing):
                    smoke.acceptance()

    def test_missing_binary_is_failure(self):
        with patch.object(smoke.shutil, 'which', return_value='/bin/native'):
            with self.assertRaisesRegex(RuntimeError, 'Build bin/stw'):
                smoke.acceptance(Path('/nonexistent-retirement-stw'))

    def test_missing_sandbox_is_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            binary = Path(directory) / 'stw'
            binary.touch()
            with patch.object(smoke.shutil, 'which', return_value='/bin/native'), patch.object(smoke.helpers, 'check_sandbox', side_effect=RuntimeError('sandbox unavailable')):
                with self.assertRaisesRegex(RuntimeError, 'sandbox unavailable'):
                    smoke.acceptance(binary)


if __name__ == '__main__':
    unittest.main()
