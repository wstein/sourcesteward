#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Require native workspace acceptance and complete effect comparisons."""
import importlib.util
from pathlib import Path
import unittest
from unittest.mock import patch

_spec = importlib.util.spec_from_file_location('workspace_smoke', Path(__file__).with_name('smoke-workspaces.py'))
smoke = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(smoke)


class WorkspaceSmokeTests(unittest.TestCase):
    def test_case_manifest_is_unique_and_requires_all_backends(self):
        self.assertEqual(set(smoke.CASE_IDS), {'git', 'jj-standalone', 'jj-colocated'})
        for group, ids in smoke.CASE_IDS.items():
            self.assertEqual(len(ids), len(set(ids)))
            self.assertGreaterEqual(len(ids), 12)
            for required in ('plan', 'apply', 'retry', 'handoff', 'retire-plan', 'stale', 'occupied', 'symlink', 'private-store', 'foreign', 'helpers', 'incomplete-retry', 'manager-ownership'):
                self.assertIn(group + '.' + required, ids)

    def test_missing_backend_is_failure(self):
        for missing in ('git', 'jj'):
            with self.subTest(backend=missing), patch.object(smoke.shutil, 'which', side_effect=lambda name: None if name == missing else '/bin/' + name):
                with self.assertRaisesRegex(RuntimeError, 'Mandatory workspace backend missing: ' + missing):
                    smoke.acceptance()

    def test_missing_cli_is_failure(self):
        with patch.object(smoke.shutil, 'which', return_value='/bin/backend'):
            with self.assertRaisesRegex(RuntimeError, 'Build bin/stw'):
                smoke.acceptance(Path('/nonexistent-workspace-stw'))

    def test_manifest_guard_rejects_missing_extra_or_repeated_case(self):
        expected = {'one', 'two'}
        for actual in ({'one'}, {'one', 'two', 'three'}):
            with self.assertRaises(AssertionError):
                smoke.require_complete(actual, expected)
        smoke.require_complete(expected, expected)


if __name__ == '__main__':
    unittest.main()
