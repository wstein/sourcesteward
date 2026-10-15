#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""The tidy acceptance manifest must fail closed when evidence is absent."""
import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

_spec = importlib.util.spec_from_file_location('tidy_smoke', Path(__file__).with_name('smoke-tidy.py'))
smoke = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(smoke)


class TidySmokeTests(unittest.TestCase):
    def test_manifest_requires_both_native_jj_layouts_and_git(self):
        self.assertEqual(set(smoke.CASE_IDS), {'git', 'jj-standalone', 'jj-colocated'})
        for group, ids in smoke.CASE_IDS.items():
            self.assertEqual(len(ids), len(set(ids)))
            self.assertEqual(len(ids), 14)
            for name in ('ancestry', 'unique', 'base-retain', 'used-retain', 'empty-retain',
                         'upstream-not-proof', 'literal-selection', 'missing-selection',
                         'duplicate-selection', 'explicit-base', 'human', 'helper',
                         'manager-ownership', 'persisted-plan'):
                self.assertIn(group + '.' + name, ids)

    def test_missing_backend_is_failure(self):
        for missing in ('git', 'jj'):
            with self.subTest(missing=missing), patch.object(smoke.shutil, 'which', side_effect=lambda name: None if name == missing else '/bin/' + name):
                with self.assertRaisesRegex(RuntimeError, 'Mandatory tidy backend missing: ' + missing):
                    smoke.acceptance()

    def test_missing_binary_is_failure(self):
        with patch.object(smoke.shutil, 'which', return_value='/bin/native'):
            with self.assertRaisesRegex(RuntimeError, 'Build bin/stw'):
                smoke.acceptance(Path('/nonexistent-tidy-stw'))

    def test_missing_sandbox_is_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            binary = Path(directory) / 'stw'
            binary.touch()
            with patch.object(smoke.shutil, 'which', return_value='/bin/native'), patch.object(smoke.helpers, 'check_sandbox', side_effect=RuntimeError('sandbox unavailable')):
                with self.assertRaisesRegex(RuntimeError, 'sandbox unavailable'):
                    smoke.acceptance(binary)

    def test_manifest_rejects_missing_extra_and_duplicate_cases(self):
        expected = {'one', 'two'}
        for actual in (['one'], ['one', 'two', 'extra'], ['one', 'two', 'two']):
            with self.assertRaises(AssertionError):
                smoke.require_complete(actual, expected)
        smoke.require_complete(['one', 'two'], expected)


if __name__ == '__main__':
    unittest.main()
