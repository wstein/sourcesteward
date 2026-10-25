# SPDX-License-Identifier: EUPL-1.2
"""Offline tests for scripts/selfcheck.py using throwaway git repositories.

Every git call (fixture and tool) runs with the global and system git configuration disabled,
so the results do not depend on the host's settings; the hostile-configuration test sets its own.
"""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / 'scripts/selfcheck.py'
OWNER = ('Owner Test', 'owner@example.invalid')
OWNER_STR = '%s <%s>' % OWNER
TRAILER = '\n\nCo-Authored-By: Claude <noreply@example.invalid>'
ISOLATED = {'GIT_CONFIG_GLOBAL': '/dev/null', 'GIT_CONFIG_SYSTEM': '/dev/null', 'GIT_CONFIG_NOSYSTEM': '1'}


def env_for(**extra):
    e = {k: v for k, v in os.environ.items()
         if not k.startswith(('GIT_AUTHOR', 'GIT_COMMITTER', 'GIT_DIR', 'GIT_WORK_TREE', 'GIT_INDEX'))}
    e.update(ISOLATED)
    e.update(extra)
    return e


class Repo(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.dir = Path(self.tmp.name) / 'repo'
        self.dir.mkdir()
        self.git('init', '-q', '-b', 'main')
        self.write('README.md', 'base\n')
        self.commit('chore: base', trailer=False)
        self.git('update-ref', 'refs/remotes/origin/main', 'HEAD')

    def git(self, *args, who=OWNER, committer=None):
        c = committer or who
        env = env_for(GIT_AUTHOR_NAME=who[0], GIT_AUTHOR_EMAIL=who[1],
                      GIT_COMMITTER_NAME=c[0], GIT_COMMITTER_EMAIL=c[1])
        p = subprocess.run(['git', *args], cwd=self.dir, capture_output=True, text=True, env=env, check=True)
        return p.stdout

    def write(self, name, text):
        p = self.dir / name
        p.parent.mkdir(parents=True, exist_ok=True)
        if isinstance(text, bytes):
            p.write_bytes(text)
        else:
            p.write_bytes(text.encode())

    def commit(self, msg='docs: change', who=OWNER, committer=None, trailer=True):
        self.git('add', '-A')
        self.git('commit', '-q', '-m', msg + (TRAILER if trailer else ''), who=who, committer=committer)

    def check(self, *args, owner=OWNER_STR, env=None, cwd=None):
        cmd = ['python3', '-B', str(SCRIPT)]
        if owner:
            cmd += ['--owner', owner]
        p = subprocess.run(cmd + list(args), cwd=cwd or self.dir, capture_output=True, text=True,
                           timeout=60, env=env_for(**(env or {})))
        return p.returncode, p.stdout, p.stderr

    def docs(self, text, name='docs/a.md'):
        self.write(name, text)
        self.commit()


class GluedChecks(Repo):
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

    def test_glued_in_json_fails(self):
        self.docs('{"a": "hidden015planner"}\n', name='docs/a.json')
        self.assertIn('FAIL glued: docs/a.json:1', self.check()[1])

    def test_table_of_rejected_patterns(self):
        for text in ('Exit0', 'exit0', 'Hidden015Planner', 'hidden1planner', 'schema_version1',
                     'revision2', 'Version3', 'rows2', 'codes7', 'ghi12jkl', 'xyz9abc'):
            with self.subTest(text):
                self.write('docs/a.md', 'lead %s tail\n' % text)
                self.commit()
                self.assertEqual(self.check()[0], 1, text)

    def test_table_of_accepted_patterns(self):
        for text in ('sha256sum', 'base64url', 'deadbeef12abcdef', 'code128', 'row2', 'utf8',
                     'version 3', 'exit 0', 'Spec 015', 'sha256', 'v2', 'go1.27', 'md5sum'):
            with self.subTest(text):
                self.write('docs/a.md', 'lead %s tail\n' % text)
                self.commit()
                code, out, _ = self.check()
                self.assertEqual(code, 0, out)

    def test_code_span_link_url_reference_and_fence_are_ignored(self):
        self.docs('`schema_version1` and [x](a/hidden015planner.md) https://e.org/abc123def\n'
                  '[ref]: docs/spec015draft.md\n'
                  '```\nhidden015planner\n```\nDone.\n')
        code, out, _ = self.check()
        self.assertEqual(code, 0, out)

    def test_only_added_lines_are_checked(self):
        self.write('docs/a.md', 'old hidden015planner\n')
        self.commit('docs: old')
        self.git('update-ref', 'refs/remotes/origin/main', 'HEAD')
        self.docs('old hidden015planner\nnew line\n')
        code, out, _ = self.check()
        self.assertEqual(code, 0, out)

    def test_content_line_starting_with_plusplus_is_not_a_header(self):
        self.docs('++ hidden015planner\n+ also\n')
        out = self.check()[1]
        self.assertIn('FAIL glued: docs/a.md:1', out)

    def test_removed_line_starting_with_dashes_does_not_confuse(self):
        self.write('docs/a.md', '-- old\nkeep\n')
        self.commit()
        self.git('update-ref', 'refs/remotes/origin/main', 'HEAD')
        self.write('docs/a.md', 'hidden015planner\nkeep\n')
        self.commit()
        self.assertIn('FAIL glued: docs/a.md:1', self.check()[1])


class PathAndConfig(Repo):
    def test_space_in_name_is_checked(self):
        self.docs('x hidden015planner\n', name='docs/My Note.md')
        self.assertIn('FAIL glued: docs/My Note.md:1', self.check()[1])

    def test_unicode_name_is_checked(self):
        self.docs('x hidden015planner\n', name='docs/Überblick.md')
        self.assertIn('FAIL glued: docs/Überblick.md:1', self.check()[1])

    def test_note_with_spaces_gets_the_source_check(self):
        self.docs('# n\nSource: a\nSource: b\n', name='chartroom/notes/Map - Name.md')
        self.assertIn('FAIL source: chartroom/notes/Map - Name.md', self.check()[1])

    def test_hostile_host_configuration_does_not_hide_findings(self):
        cfg = Path(self.tmp.name) / 'hostile.gitconfig'
        cfg.write_text('[diff]\n\tnoprefix = true\n\texternal = /bin/false\n\trenames = copies\n'
                       '\tinterHunkContext = 5\n\tmnemonicPrefix = true\n[color "diff"]\n\tnew = red\n'
                       '[color]\n\tdiff = always\n\tui = always\n[core]\n\tquotepath = true\n')
        self.write('docs/a.md', 'a\n\n\nb\n\nc\nd\ne\nf\n')
        self.commit()
        self.git('update-ref', 'refs/remotes/origin/main', 'HEAD')
        self.write('docs/a.md', 'a\n\n\n\nb\n\nc\nd hidden015planner\ne\nf\n')
        self.write('docs/Ü b.md', 'x hidden015planner\n')
        self.commit()
        code, out, _ = self.check(env={'GIT_CONFIG_GLOBAL': str(cfg)})
        self.assertEqual(code, 1)
        self.assertIn('FAIL glued: docs/a.md:8', out)
        self.assertIn('FAIL glued: docs/Ü b.md:1', out)
        self.assertIn('FAIL blank: docs/a.md:4', out)


class StructureChecks(Repo):
    def test_double_blank_line_fails(self):
        self.docs('one\n\n\ntwo\n')
        code, out, _ = self.check()
        self.assertEqual(code, 1)
        self.assertIn('FAIL blank: docs/a.md:3', out)

    def test_single_blank_line_passes(self):
        self.docs('one\n\ntwo\n')
        self.assertEqual(self.check()[0], 0)

    def test_blank_line_after_existing_blank_line_fails(self):
        self.write('docs/a.md', 'one\n\ntwo\n')
        self.commit()
        self.git('update-ref', 'refs/remotes/origin/main', 'HEAD')
        self.write('docs/a.md', 'one\n\n\ntwo\n')
        self.commit()
        self.assertIn('FAIL blank: docs/a.md:3', self.check()[1])

    def test_blank_lines_inside_a_fence_are_fine(self):
        self.docs('```\none\n\n\ntwo\n```\n')
        self.assertEqual(self.check()[0], 0)

    def test_form_feed_and_unicode_separators_do_not_shift_lines(self):
        self.docs('a\x0cb c\x1c\nline two\n\n\nhidden015planner\n')
        out = self.check()[1]
        self.assertIn('FAIL blank: docs/a.md:4', out)
        self.assertIn('FAIL glued: docs/a.md:5', out)
        self.assertNotIn('docs/a.md:3', out)

    def test_crlf_files(self):
        self.write('docs/a.md', b'one\r\n\r\n\r\nhidden015planner\r\n```\r\nhidden015planner\r\n```\r\n')
        self.commit()
        out = self.check()[1]
        self.assertIn('FAIL blank: docs/a.md:3', out)
        self.assertIn('FAIL glued: docs/a.md:4', out)
        self.assertNotIn('docs/a.md:6', out)

    def test_fence_rules(self):
        cases = {
            'tilde fence': '~~~\nhidden015planner\n~~~\n',
            'tilde inside backtick fence': '```\n~~~\nhidden015planner\n~~~\n```\n',
            'backticks inside longer fence': '````\n```\nhidden015planner\n```\n````\n',
            'indented fence': '- item\n\n  ```\n  hidden015planner\n  ```\n',
            'info string': '```go\nhidden015planner\n```\n',
            'unclosed fence': '```\nhidden015planner\n',
        }
        for name, text in cases.items():
            with self.subTest(name):
                self.docs(text)
                code, out, _ = self.check()
                self.assertEqual(code, 0, out)

    def test_short_closing_fence_does_not_close(self):
        self.docs('````\n```\nhidden015planner\n````\nreal hidden016planner\n')
        out = self.check()[1]
        self.assertNotIn('docs/a.md:3', out)
        self.assertIn('FAIL glued: docs/a.md:5', out)

    def test_second_source_line_in_chartroom_note_fails(self):
        self.docs('# n\nSource: a\nSource: b\n', name='chartroom/notes/n.md')
        code, out, _ = self.check()
        self.assertEqual(code, 1)
        self.assertIn('FAIL source: chartroom/notes/n.md', out)

    def test_zero_or_one_source_line_passes(self):
        for text in ('# n\nno source\n', '# n\nSource: a\n'):
            with self.subTest(text):
                self.docs(text, name='chartroom/notes/Map - x.md')
                self.assertEqual(self.check()[0], 0)

    def test_source_lines_in_fences_are_ignored(self):
        self.docs('# n\nSource: a\n```\nSource: b\n```\n', name='chartroom/notes/n.md')
        self.assertEqual(self.check()[0], 0)

    def test_source_lines_outside_note_markdown_are_not_checked(self):
        self.docs('Source: a\nSource: b\n')
        self.docs('Source: a\nSource: b\n', name='chartroom/notes/n.txt')
        self.docs('Source: a\nSource: b\n', name='chartroom/other/n.md')
        self.assertEqual(self.check()[0], 0)

    def test_diff_check_failure(self):
        self.write('docs/a.md', 'trailing space \n')
        self.commit()
        code, out, _ = self.check()
        self.assertEqual(code, 1)
        self.assertIn('FAIL diff-check', out)
        self.assertIn('trailing whitespace', out)

    def test_diverged_base_reports_only_the_branch_side(self):
        self.docs('fine\n', name='docs/b.md')
        self.git('switch', '-q', '-c', 'side', 'refs/remotes/origin/main')
        self.write('docs/c.md', 'hidden015planner\n')
        self.commit()
        self.git('update-ref', 'refs/remotes/origin/main', 'HEAD')
        self.git('switch', '-q', 'main')
        code, out, _ = self.check()
        self.assertEqual(code, 0, out)


class CommitChecks(Repo):
    def test_trailer_is_opt_in(self):
        self.write('docs/a.md', 'fine\n')
        self.commit(trailer=False)
        self.assertEqual(self.check()[0], 0)

    def test_missing_trailer_fails_when_required(self):
        self.write('docs/a.md', 'fine\n')
        self.commit(trailer=False)
        code, out, _ = self.check('--require-trailer')
        self.assertEqual(code, 1)
        self.assertIn('FAIL trailer', out)

    def test_trailer_must_be_in_the_trailer_block(self):
        self.write('docs/a.md', 'fine\n')
        self.commit('docs: x\n\nThe co-authored-by: line is explained here.\n\nMore prose.', trailer=False)
        self.assertEqual(self.check('--require-trailer')[0], 1)

    def test_real_trailer_passes_when_required(self):
        self.write('docs/a.md', 'fine\n')
        self.commit()
        self.assertEqual(self.check('--require-trailer')[0], 0)

    def test_every_commit_is_checked(self):
        self.write('docs/a.md', 'a\n')
        self.commit(trailer=False)
        self.write('docs/b.md', 'b\n')
        self.commit()
        code, out, _ = self.check('--require-trailer')
        self.assertEqual(code, 1)
        self.assertEqual(out.count('FAIL trailer'), 1)

    def test_session_link_variants_fail(self):
        for msg in ('docs: x\n\nsee https://claude.ai/code/session_1', 'docs: x\n\nClaude-Session: abc'):
            with self.subTest(msg):
                self.git('reset', '-q', '--hard', 'refs/remotes/origin/main')
                self.write('docs/a.md', 'fine\n')
                self.commit(msg)
                code, out, _ = self.check()
                self.assertEqual(code, 1)
                self.assertIn('FAIL session-link', out)

    def test_exact_identity_comparison(self):
        for who in (('Owner Testberg', OWNER[1]), (OWNER[0], 'other@example.invalid'), ('t', 't@t')):
            with self.subTest(who):
                self.git('reset', '-q', '--hard', 'refs/remotes/origin/main')
                self.write('docs/a.md', 'fine\n')
                self.commit(who=who)
                code, out, _ = self.check()
                self.assertEqual(code, 1)
                self.assertIn('FAIL identity', out)

    def test_author_only_and_committer_only_mismatch_fail(self):
        other = ('Other', 'o@example.invalid')
        for who, committer in ((other, OWNER), (OWNER, other)):
            with self.subTest(who):
                self.git('reset', '-q', '--hard', 'refs/remotes/origin/main')
                self.write('docs/a.md', 'fine\n')
                self.commit(who=who, committer=committer)
                self.assertEqual(self.check()[0], 1)

    def test_default_owner_is_the_read_only_git_identity(self):
        cfg = Path(self.tmp.name) / 'owner.gitconfig'
        cfg.write_text('[user]\n\tname = %s\n\temail = %s\n' % OWNER)
        before = cfg.read_text()
        self.docs('fine\n')
        code, out, _ = self.check(owner=None, env={'GIT_CONFIG_GLOBAL': str(cfg)})
        self.assertEqual(code, 0, out)
        self.assertNotIn('INFO identity', out)
        self.git('reset', '-q', '--hard', 'refs/remotes/origin/main')
        self.write('docs/a.md', 'fine\n')
        self.commit(who=('Else', 'e@example.invalid'))
        self.assertEqual(self.check(owner=None, env={'GIT_CONFIG_GLOBAL': str(cfg)})[0], 1)
        self.assertEqual(cfg.read_text(), before)

    def test_explicit_owner_overrides_the_git_identity(self):
        cfg = Path(self.tmp.name) / 'else.gitconfig'
        cfg.write_text('[user]\n\tname = Else\n\temail = e@example.invalid\n')
        self.docs('fine\n')
        self.assertEqual(self.check(env={'GIT_CONFIG_GLOBAL': str(cfg)})[0], 0)

    def test_no_identity_skips_the_check_with_info(self):
        self.docs('fine\n')
        code, out, _ = self.check(owner=None)
        self.assertEqual(code, 0, out)
        self.assertIn('INFO identity', out)


class Info(Repo):
    def test_claim_lines_are_informational(self):
        self.docs('Defined in main.go:42.\nIt exits with exit status 3.\nUse `--json` here.\nPlain.\n'
                  '| main.go:42 | table |\n')
        code, out, _ = self.check()
        self.assertEqual(code, 0, out)
        self.assertEqual(out.count('INFO claims: docs/a.md:'), 3)
        self.assertIn('docs/a.md:1', out)
        self.assertNotIn('docs/a.md:4', out)
        self.assertNotIn('docs/a.md:5', out)
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

    def test_invalid_utf8_does_not_crash_and_is_reported(self):
        self.write('docs/a.md', b'x\xe9y hidden015planner\n')
        self.commit()
        code, out, _ = self.check()
        self.assertEqual(code, 1)
        self.assertIn('FAIL glued: docs/a.md:1', out)
        self.assertIn('INFO encoding: docs/a.md', out)

    def test_invalid_utf8_alone_passes_with_info(self):
        self.write('docs/a.md', b'x\xe9y\n')
        self.commit()
        code, out, _ = self.check()
        self.assertEqual(code, 0, out)
        self.assertIn('INFO encoding', out)


class Usage(Repo):
    def test_missing_base_ref_is_a_clear_error(self):
        self.docs('fine\n')
        code, out, err = self.check('refs/remotes/nope/main')
        self.assertEqual(code, 2)
        self.assertIn('base ref refs/remotes/nope/main does not exist', err)
        self.assertIn('git fetch', err)
        self.assertEqual(out, '')

    def test_default_base_missing_is_reported(self):
        self.git('update-ref', '-d', 'refs/remotes/origin/main')
        code, _, err = self.check()
        self.assertEqual(code, 2)
        self.assertIn('refs/remotes/origin/main', err)

    def test_missing_head_ref(self):
        code, _, err = self.check('--head', 'refs/heads/nope')
        self.assertEqual(code, 2)
        self.assertIn('head refs/heads/nope does not exist', err)

    def test_no_merge_base_is_an_error_not_ok(self):
        self.git('switch', '-q', '--orphan', 'other')
        self.write('docs/z.md', 'hidden015planner\n')
        self.commit()
        code, out, err = self.check()
        self.assertEqual(code, 2)
        self.assertIn('no merge base', err)
        self.assertNotIn('selfcheck: ok', out)

    def test_explicit_base_and_head(self):
        self.docs('hidden015planner\n')
        first = self.git('rev-parse', 'HEAD').strip()
        self.docs('fine\n', name='docs/b.md')
        code, out, _ = self.check('refs/remotes/origin/main', '--head', first)
        self.assertEqual(code, 1)
        self.assertIn('FAIL glued', out)

    def test_not_a_git_repository(self):
        with tempfile.TemporaryDirectory() as d:
            code, _, err = self.check(cwd=d, env={'GIT_CEILING_DIRECTORIES': str(Path(d).parent)})
        self.assertEqual(code, 2)
        self.assertIn('not inside a git repository', err)

    def test_unknown_option_is_usage_error(self):
        self.assertEqual(self.check('--bogus')[0], 2)


if __name__ == '__main__':
    unittest.main()
