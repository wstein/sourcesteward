#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Guard the sync acceptance prerequisites, case manifest and fixture comparison helpers."""
import importlib.util
import json
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
        self.assertEqual(len(smoke.CASE_IDS), 21)
        self.assertEqual(len(smoke.CASE_IDS), len(set(smoke.CASE_IDS)))
        self.assertTrue(all(case.startswith('S.') for case in smoke.CASE_IDS))

    def test_missing_git_is_failure_not_skip(self):
        with patch.object(smoke.shutil, 'which', return_value=None):
            with self.assertRaisesRegex(RuntimeError, 'Mandatory sync backend missing: git'):
                smoke.acceptance()

    def test_missing_binary_is_failure_not_skip(self):
        with self.assertRaisesRegex(RuntimeError, 'Build bin/stw'):
            smoke.acceptance(Path('/nonexistent-stw-sync-fixture'))

    def test_a_binary_that_prints_nothing_fails_at_the_first_result(self):
        with tempfile.TemporaryDirectory() as directory:
            fake = Path(directory) / 'stw'
            fake.write_text('#!/bin/sh\nexit 0\n')
            fake.chmod(0o755)
            with self.assertRaises(json.JSONDecodeError):
                smoke.acceptance(fake)

    def test_a_binary_with_an_incomplete_result_fails_with_the_missing_key(self):
        with tempfile.TemporaryDirectory() as directory:
            fake = Path(directory) / 'stw'
            fake.write_text('#!/bin/sh\necho \'{"schema_version":1,"command":"sync"}\'\nexit 0\n')
            fake.chmod(0o755)
            with self.assertRaisesRegex(AssertionError, 'key missing'):
                smoke.acceptance(fake)

    def test_require_complete_fails_on_a_missing_or_an_unexpected_case(self):
        smoke.require_complete(set(smoke.CASE_IDS))
        with self.assertRaisesRegex(AssertionError, 'S.busy'):
            smoke.require_complete(set(smoke.CASE_IDS) - {'S.busy'})
        with self.assertRaisesRegex(AssertionError, 'S.unknown'):
            smoke.require_complete(set(smoke.CASE_IDS) | {'S.unknown'})

    def test_the_acceptance_checks_that_every_case_completed(self):
        import inspect
        self.assertIn('require_complete(completed)', inspect.getsource(smoke.acceptance))

    def test_git_state_sees_operation_files_packed_refs_and_ref_files(self):
        with tempfile.TemporaryDirectory() as directory:
            git_dir = Path(directory)
            (git_dir / 'refs' / 'heads').mkdir(parents=True)
            (git_dir / 'HEAD').write_text('ref: refs/heads/main\n')
            (git_dir / 'MERGE_HEAD').write_text('a' * 40)
            (git_dir / 'refs' / 'heads' / 'main').write_text('b' * 40)
            before = smoke.git_state(git_dir)
            self.assertEqual(before, smoke.git_state(git_dir))
            for name in ('MERGE_HEAD', 'ORIG_HEAD', 'packed-refs'):
                (git_dir / name).write_text('changed')
                self.assertNotEqual(before, smoke.git_state(git_dir), name)
                (git_dir / name).unlink() if name != 'MERGE_HEAD' else (git_dir / name).write_text('a' * 40)
            (git_dir / 'refs' / 'heads' / 'other').write_text('c' * 40)
            self.assertNotEqual(before, smoke.git_state(git_dir))

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
