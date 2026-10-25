#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Guard the sync acceptance prerequisites, case manifest and fixture comparison helpers."""
import importlib.util
import os
from pathlib import Path
import stat
import tempfile
import unittest
from unittest.mock import patch

_spec = importlib.util.spec_from_file_location('sync_smoke', Path(__file__).with_name('smoke-sync.py'))
smoke = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(smoke)


class SyncSmokeTests(unittest.TestCase):
    def test_case_manifest_is_stable_and_unique(self):
        self.assertEqual(len(smoke.CASE_IDS), 15)
        self.assertEqual(len(smoke.CASE_IDS), len(set(smoke.CASE_IDS)))
        self.assertTrue(all(case.startswith('S.') for case in smoke.CASE_IDS))

    def test_missing_git_is_failure_not_skip(self):
        with patch.object(smoke.shutil, 'which', return_value=None):
            with self.assertRaisesRegex(RuntimeError, 'Mandatory sync backend missing: git'):
                smoke.acceptance()

    def test_missing_binary_is_failure_not_skip(self):
        with self.assertRaisesRegex(RuntimeError, 'Build bin/stw'):
            smoke.acceptance(Path('/nonexistent-stw-sync-fixture'))

    def test_a_binary_that_does_nothing_fails_the_acceptance(self):
        with tempfile.TemporaryDirectory() as directory:
            fake = Path(directory) / 'stw'
            fake.write_text('#!/bin/sh\nexit 0\n')
            fake.chmod(0o755)
            with self.assertRaises(Exception):
                smoke.acceptance(fake)

    def test_digest_sees_content_and_mode_changes_and_does_not_follow_links(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'hooks').mkdir()
            hook = root / 'hooks' / 'pre-push'
            hook.write_text('a')
            outside = root / 'outside'
            outside.write_text('x')
            (root / 'hooks' / 'link').symlink_to(outside)
            before = smoke.digest(root / 'hooks')
            self.assertEqual(before, smoke.digest(root / 'hooks'))
            outside.write_text('changed')
            self.assertEqual(before, smoke.digest(root / 'hooks'), 'a link target must not be followed')
            hook.write_text('b')
            self.assertNotEqual(before, smoke.digest(root / 'hooks'))
            hook.write_text('a')
            hook.chmod(stat.S_IMODE(hook.stat().st_mode) | stat.S_IXUSR)
            self.assertNotEqual(before, smoke.digest(root / 'hooks'))

    def test_worktree_files_ignore_git_directories_and_gitlink_files(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / '.git').mkdir()
            (root / '.git' / 'index').write_text('i')
            (root / 'sub').mkdir()
            (root / 'sub' / '.git').write_text('gitdir: ../.git/modules/sub')
            (root / 'sub' / 'f.txt').write_text('f')
            (root / 'name with space ünï.txt').write_text('u')
            files = smoke.worktree_files(root)
            self.assertEqual(sorted(files), ['name with space ünï.txt', os.path.join('sub', 'f.txt')])
            (root / 'sub' / 'f.txt').write_text('changed')
            self.assertNotEqual(files, smoke.worktree_files(root))


if __name__ == '__main__':
    unittest.main()
