# SPDX-License-Identifier: EUPL-1.2
"""Offline tests for scripts/selfcheck.py using throwaway git repositories."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / 'scripts/selfcheck.py'
OWNER = ('Owner Test', 'owner@example.invalid')
TRAILER = '\n\nCo-Authored-By: Claude <noreply@example.invalid>'


class Repo(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.dir = Path(self.tmp.name)
        self.git('init', '-q', '-b', 'main')
        self.write('README.md', 'base\n')
        self.commit('chore: base', trailer=False)
        self.git('update-ref', 'refs/remotes/origin/main', 'HEAD')

    def git(self, *args, who=OWNER):
        env = dict(os.environ, GIT_CONFIG_GLOBAL='/dev/null', GIT_CONFIG_SYSTEM='/dev/null')
        p = subprocess.run(['git', '-c', f'user.name={who[0]}', '-c', f'user.email={who[1]}', *args],
                           cwd=self.dir, capture_output=True, text=True, env=env, check=True)
        return p.stdout

    def write(self, name, text):
        p = self.dir / name
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(text)

    def commit(self, msg='docs: change', who=OWNER, trailer=True):
        self.git('add', '-A')
        self.git('commit', '-q', '-m', msg + (TRAILER if trailer else ''), who=who)

    def check(self, *args):
        p = subprocess.run(['python3', '-B', str(SCRIPT), '--owner', OWNER[0], *args], cwd=self.dir,
                           capture_output=True, text=True, timeout=60)
        return p.returncode, p.stdout, p.stderr

    def docs(self, text, name='docs/a.md'):
        self.write(name, text)
        self.commit()


class HardChecks(Repo):
    def test_clean_change_passes(self):
        self.docs('# Title\n\nPlain prose.\n')
        code, out, _ = self.check()
        self.assertEqual(code, 0, out)
        self.assertIn('selfcheck: ok', out)

    def test_glued_word_number_fails(self):
        self.docs('The hidden015planner stays.\n')
        code, out, _ = self.check()
        self.assertEqual(code, 1)
        self.assertIn('FAIL glued: docs/a.md:1', out)

    def test_glued_keyword_number_fails(self):
        self.docs('See\nschema_version1 and revision2.\n')
        code, out, _ = self.check()
        self.assertEqual(code, 1)
        self.assertIn('FAIL glued: docs/a.md:2', out)
        self.assertIn('revision2', out)

    def test_code_span_link_url_and_fence_are_ignored(self):
        self.docs('`schema_version1` and [x](a/hidden015planner.md) https://e.org/abc123def\n'
                  '```\nhidden015planner\n```\nDone.\n')
        code, out, _ = self.check()
        self.assertEqual(code, 0, out)

    def test_spaced_words_and_numbers_pass(self):
        self.docs('Spec 015 and exit 2, version 3, sha256 and utf8 plain.\n')
        code, out, _ = self.check()
        self.assertEqual(code, 0, out)

    def test_only_added_lines_are_checked(self):
        self.write('docs/a.md', 'old hidden015planner\n')
        self.commit('docs: old')
        self.git('update-ref', 'refs/remotes/origin/main', 'HEAD')
        self.docs('old hidden015planner\nnew line\n')
        code, out, _ = self.check()
        self.assertEqual(code, 0, out)

    def test_double_blank_line_fails(self):
        self.docs('one\n\n\ntwo\n')
        code, out, _ = self.check()
        self.assertEqual(code, 1)
        self.assertIn('FAIL blank: docs/a.md:3', out)

    def test_single_blank_line_passes(self):
        self.docs('one\n\ntwo\n')
        self.assertEqual(self.check()[0], 0)

    def test_second_source_line_in_chartroom_note_fails(self):
        self.docs('# n\nSource: a\nSource: b\n', name='chartroom/notes/n.md')
        code, out, _ = self.check()
        self.assertEqual(code, 1)
        self.assertIn('FAIL source: chartroom/notes/n.md', out)

    def test_one_source_line_in_chartroom_note_passes(self):
        self.docs('# n\nSource: a\n', name='chartroom/notes/n.md')
        self.assertEqual(self.check()[0], 0)

    def test_source_lines_outside_notes_are_not_checked(self):
        self.docs('Source: a\nSource: b\n')
        self.assertEqual(self.check()[0], 0)

    def test_diff_check_failure(self):
        self.write('docs/a.md', 'trailing space \n')
        self.commit()
        code, out, _ = self.check()
        self.assertEqual(code, 1)
        self.assertIn('FAIL diff-check', out)

    def test_missing_trailer_fails(self):
        self.write('docs/a.md', 'fine\n')
        self.commit(trailer=False)
        code, out, _ = self.check()
        self.assertEqual(code, 1)
        self.assertIn('FAIL trailer', out)

    def test_non_owner_identity_fails(self):
        self.write('docs/a.md', 'fine\n')
        self.commit(who=('t', 't@t'))
        code, out, _ = self.check()
        self.assertEqual(code, 1)
        self.assertIn('FAIL identity', out)

    def test_session_link_fails(self):
        self.write('docs/a.md', 'fine\n')
        self.commit('docs: x\n\nClaude-Session: https://claude.ai/code/session_1')
        code, out, _ = self.check()
        self.assertEqual(code, 1)
        self.assertIn('FAIL session-link', out)

    def test_every_commit_is_checked(self):
        self.write('docs/a.md', 'a\n')
        self.commit()
        self.write('docs/b.md', 'b\n')
        self.commit(trailer=False)
        code, out, _ = self.check()
        self.assertEqual(code, 1)
        self.assertEqual(out.count('FAIL trailer'), 1)


class Info(Repo):
    def test_claim_lines_are_informational(self):
        self.docs('Defined in main.go:42.\nIt exits with exit status 3.\nUse `--json` here.\nPlain.\n')
        code, out, _ = self.check()
        self.assertEqual(code, 0, out)
        self.assertEqual(out.count('INFO claims: docs/a.md:'), 3)
        self.assertIn('docs/a.md:1', out)
        self.assertNotIn('docs/a.md:4', out)
        self.assertIn('verify the INFO claims', out)

    def test_tick_mismatch_is_informational(self):
        self.write('specs/001/tasks.md', '- [x] a\n- [ ] b\n')
        self.write('docs/progress.md', 'Spec 001: 2/2 done\n')
        self.commit()
        code, out, _ = self.check()
        self.assertEqual(code, 0, out)
        self.assertIn('INFO tick: docs/progress.md:1', out)

    def test_tick_match_is_silent(self):
        self.write('specs/001/tasks.md', '- [x] a\n- [ ] b\n')
        self.write('docs/progress.md', 'Spec 001: 1/2 done\n')
        self.commit()
        self.assertNotIn('INFO tick', self.check()[1])


class Usage(Repo):
    def test_missing_base_ref_is_a_clear_error(self):
        self.docs('fine\n')
        code, out, err = self.check('refs/remotes/nope/main')
        self.assertEqual(code, 2)
        self.assertIn('base ref refs/remotes/nope/main does not exist', err)
        self.assertIn('git fetch', err)

    def test_default_base_missing_is_reported(self):
        self.git('update-ref', '-d', 'refs/remotes/origin/main')
        code, _, err = self.check()
        self.assertEqual(code, 2)
        self.assertIn('refs/remotes/origin/main', err)

    def test_explicit_base_and_head(self):
        self.docs('hidden015planner\n')
        first = self.git('rev-parse', 'HEAD').strip()
        self.docs('fine\n', name='docs/b.md')
        code, out, _ = self.check('refs/remotes/origin/main', '--head', first)
        self.assertEqual(code, 1)
        self.assertIn('FAIL glued', out)

    def test_not_a_git_repository(self):
        with tempfile.TemporaryDirectory() as d:
            p = subprocess.run(['python3', '-B', str(SCRIPT)], cwd=d, capture_output=True, text=True,
                               env=dict(os.environ, GIT_CEILING_DIRECTORIES=str(Path(d).parent)))
        self.assertEqual(p.returncode, 2)

    def test_unknown_option_is_usage_error(self):
        code, _, _ = self.check('--bogus')
        self.assertEqual(code, 2)


if __name__ == '__main__':
    unittest.main()
