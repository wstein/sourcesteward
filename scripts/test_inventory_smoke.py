#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Guard mandatory acceptance prerequisites and effect-comparison coverage."""
import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

_spec = importlib.util.spec_from_file_location('inventory_smoke', Path(__file__).with_name('smoke-inventory.py'))
smoke = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(smoke)


class InventorySmokeTests(unittest.TestCase):
    def test_milestone_manifests_have_stable_unique_case_ids(self):
        self.assertEqual({name: len(ids) for name, ids in smoke.CASE_IDS.items()},
                         {'M1': 8, 'M2': 4, 'M3': 8, 'M4': 16})
        all_ids = [case_id for ids in smoke.CASE_IDS.values() for case_id in ids]
        self.assertEqual(len(all_ids), 36)
        self.assertEqual(len(set(all_ids)), 36)
        for group, ids in smoke.CASE_IDS.items():
            self.assertTrue(all(case_id.startswith(group + '.') for case_id in ids))

    def test_unknown_milestone_is_rejected_before_backend_execution(self):
        with patch.object(smoke.shutil, 'which') as backend:
            with self.assertRaisesRegex(ValueError, 'Unknown inventory milestone'):
                smoke.acceptance(milestone='M5')
            backend.assert_not_called()

    def test_missing_backend_is_failure_not_skip(self):
        for missing in ('git', 'jj'):
            with self.subTest(backend=missing), patch.object(smoke.shutil, 'which', side_effect=lambda name: None if name == missing else '/bin/' + name):
                with self.assertRaisesRegex(RuntimeError, 'Mandatory inventory backend missing: ' + missing):
                    smoke.acceptance()

    def test_missing_cli_is_failure_not_skip(self):
        with patch.object(smoke.shutil, 'which', return_value='/bin/backend'):
            with self.assertRaisesRegex(RuntimeError, 'Build bin/stw'):
                smoke.acceptance(Path('/nonexistent-stw-inventory-fixture'))

    def test_snapshot_detects_index_operation_store_and_mode_changes(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for relative in ('.git/index', '.jj/repo/op_store/operations/fixture', 'dirty.txt'):
                target = root / relative
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(b'original')
            before = smoke.helpers.snapshot(root)
            for relative in ('.git/index', '.jj/repo/op_store/operations/fixture', 'dirty.txt'):
                with self.subTest(path=relative):
                    target = root / relative
                    target.write_bytes(b'changed')
                    self.assertNotEqual(before, smoke.helpers.snapshot(root))
                    target.write_bytes(b'original')
                    self.assertEqual(before, smoke.helpers.snapshot(root))
            target = root / 'dirty.txt'
            target.chmod(0o700 if target.stat().st_mode & 0o777 != 0o700 else 0o600)
            self.assertNotEqual(before, smoke.helpers.snapshot(root))


if __name__ == '__main__':
    unittest.main()
