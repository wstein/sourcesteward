#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Validate mandatory layout manifests and real disposable fixture mappings."""
import importlib.util
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


def load(name):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(name + '.py'))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


smoke = load('smoke-git-layouts')


class GitLayoutSmokeTests(unittest.TestCase):
    def test_each_entrypoint_requires_layout_acceptance(self):
        counts = {'workspace': 24, 'tidy': 12, 'tidy-apply': 20, 'retirement': 20}
        for filename, family in (('smoke-workspaces', 'workspace'), ('smoke-tidy', 'tidy'),
                                 ('smoke-tidy-apply', 'tidy-apply'), ('smoke-retirement', 'retirement')):
            entry = load(filename)
            self.assertEqual(entry.LAYOUT_CASE_IDS, smoke.CASE_IDS[family])
            self.assertEqual(len(entry.LAYOUT_CASE_IDS), counts[family])
            self.assertEqual(len(set(entry.LAYOUT_CASE_IDS)), counts[family])
            for layout in ('submodule', 'submodule-linked', 'separate', 'separate-linked'):
                self.assertIn(layout + '.plan', entry.LAYOUT_CASE_IDS)
                self.assertIn(layout + '.redirect', entry.LAYOUT_CASE_IDS)

    def test_native_fixtures_prove_primary_and_current_separately(self):
        git = shutil.which('git')
        self.assertIsNotNone(git, 'Mandatory Git layout backend missing')
        for layout in smoke.LAYOUTS:
            with self.subTest(layout=layout), tempfile.TemporaryDirectory(prefix='stw-layout-fixture-test-') as directory:
                root = Path(directory).resolve()
                home = root / 'home'
                home.mkdir()
                env = smoke.helpers.clean_environment(home)

                def native(args, cwd):
                    run = subprocess.run([git, *args], cwd=cwd, env=env, capture_output=True, timeout=20)
                    self.assertEqual(run.returncode, 0, (args, run.stderr))
                    return run.stdout.decode().strip()

                source, primary, common = smoke.fixture(native, root, layout)
                self.assertEqual(native(['rev-parse', '--show-toplevel'], source), str(source))
                self.assertEqual(native(['rev-parse', '--path-format=absolute', '--git-common-dir'], source), str(common))
                value = native(['config', '--get', 'core.worktree'], source)
                self.assertEqual((common / value).resolve(), primary)
                pointer = (primary / '.git').read_text()
                self.assertTrue(pointer.startswith('gitdir: ') and pointer.endswith('\n'))
                self.assertEqual((primary / pointer.removeprefix('gitdir: ').removesuffix('\n')).resolve(), common)
                if layout.endswith('-linked'):
                    self.assertNotEqual(primary, source)
                    gitdir = Path(native(['rev-parse', '--absolute-git-dir'], source))
                    self.assertEqual(gitdir.parent, common / 'worktrees')
                    self.assertEqual((gitdir / 'gitdir').read_text(), str(source / '.git') + '\n')
                self.assertEqual(native(['status', '--porcelain'], source), '')

    def test_missing_binary_and_unknown_family_fail_closed(self):
        with self.assertRaisesRegex(RuntimeError, 'Build bin/stw'):
            smoke.acceptance(Path('/nonexistent-layout-stw'), 'workspace')
        with self.assertRaisesRegex(ValueError, 'Unknown Git layout'):
            smoke.acceptance(Path('/nonexistent-layout-stw'), 'other')


if __name__ == '__main__':
    unittest.main()
