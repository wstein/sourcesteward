#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Guard the repos acceptance prerequisites, case manifest, effect comparison and summary recomputation."""
import importlib.util
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

_spec = importlib.util.spec_from_file_location('repos_smoke', Path(__file__).with_name('smoke-repos.py'))
smoke = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(smoke)


def entry(status, backend='git', origin='explicit', discovery='not-requested', dirty=None, stale=0, foreign=0, unverified=0, conflicts=0):
    facts = None if dirty is None else {'dirty': {'value': dirty, 'reason': ''}, 'conflicted_references': conflicts,
                                       'workspaces': {'total': 1, 'stale': stale, 'foreign': foreign, 'unverified': unverified}}
    return {'status': status, 'backend': backend, 'origin': origin, 'discovery': {'status': discovery}, 'facts': facts}


class ReposSmokeTests(unittest.TestCase):
    def test_case_manifest_is_stable_and_unique(self):
        self.assertEqual({name: len(ids) for name, ids in smoke.CASE_IDS.items()}, {'G': 10, 'J': 5})
        all_ids = [case_id for ids in smoke.CASE_IDS.values() for case_id in ids]
        self.assertEqual(len(all_ids), len(set(all_ids)))
        for group, ids in smoke.CASE_IDS.items():
            self.assertTrue(all(case_id.startswith(group + '.') for case_id in ids))

    def test_missing_git_is_failure_not_skip(self):
        with patch.object(smoke.shutil, 'which', return_value=None):
            with self.assertRaisesRegex(RuntimeError, 'Mandatory repos backend missing: git'):
                smoke.acceptance()

    def test_missing_cli_is_failure_not_skip(self):
        with patch.object(smoke.shutil, 'which', return_value='/bin/git'):
            with self.assertRaisesRegex(RuntimeError, 'Build bin/stw'):
                smoke.acceptance(Path('/nonexistent-stw-repos-fixture'))

    def test_snapshot_detects_content_mode_and_mtime_changes(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            target = root / '.git' / 'index'
            target.parent.mkdir()
            target.write_bytes(b'original')
            before = smoke.snapshot(root)
            self.assertEqual(before, smoke.snapshot(root))
            stat = target.stat()
            os.utime(target, ns=(stat.st_atime_ns, stat.st_mtime_ns + 1_000_000))
            self.assertNotEqual(before, smoke.snapshot(root), 'an mtime-only change of the index must be seen')
            os.utime(target, ns=(stat.st_atime_ns, stat.st_mtime_ns))
            self.assertEqual(before, smoke.snapshot(root))
            (root / '.git' / 'index.lock').write_bytes(b'')
            (root / '.git' / 'index.lock').unlink()
            self.assertNotEqual(before, smoke.snapshot(root), 'a lock file that appeared and vanished changes the directory mtime')

    def test_summary_recomputation(self):
        entries = [entry('ok', dirty='true', stale=1), entry('partial', dirty='unknown', foreign=2, conflicts=3),
                   entry('skipped', backend='unknown', origin='submodule'), entry('error', backend='unknown', discovery='not-applicable'),
                   entry('ok', backend='jj', dirty='unknown', unverified=1)]
        summary = smoke.recompute_summary(entries)
        self.assertEqual(summary['entries'], 5)
        self.assertEqual(summary['by_status'], {'ok': 2, 'partial': 1, 'error': 1, 'skipped': 1})
        self.assertEqual(summary['by_backend'], {'git': 2, 'jj': 1, 'other': 2})
        self.assertEqual(summary['dirty'], {'true': 1, 'false': 0, 'unknown': 2})
        self.assertEqual((summary['stale_workspaces'], summary['foreign_workspaces'], summary['unverified_workspaces'], summary['conflicted_references']), (1, 2, 1, 3))
        self.assertEqual((summary['skipped_uninitialized'], summary['discovery_not_applicable']), (1, 1))


if __name__ == '__main__':
    unittest.main()
