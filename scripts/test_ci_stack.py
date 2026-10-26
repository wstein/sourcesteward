# SPDX-License-Identifier: EUPL-1.2
"""Tests for scripts/ci_stack.py: the stack-tip decision of the heavy conformance gate."""
import importlib.util
import io
import json
import os
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
_SPEC = importlib.util.spec_from_file_location('ci_stack', ROOT / 'scripts/ci_stack.py')
STACK = importlib.util.module_from_spec(_SPEC)
_SPEC.loader.exec_module(STACK)

REPO = 'wstein/sourcesteward'


def pr(number, base, head_repo=REPO, base_repo=REPO):
    return {'number': number, 'base_ref': base, 'base_repo': base_repo, 'head_repo': head_repo}


def decide(prs, head='feat/a', number=2, event='pull_request', head_repo=REPO, base_repo=REPO, default='main'):
    return STACK.is_stack_tip(event, number, head, head_repo, base_repo, default, prs)


class DecisionTests(unittest.TestCase):
    def test_a_pull_request_on_the_default_branch_without_children_is_a_tip(self):
        self.assertTrue(decide([pr(2, 'main')]))

    def test_a_pull_request_whose_branch_is_the_base_of_an_open_same_repo_pull_request_is_not_a_tip(self):
        self.assertFalse(decide([pr(2, 'main'), pr(3, 'feat/a')]))

    def test_the_middle_of_a_three_stack_is_not_a_tip_and_the_top_is(self):
        prs = [pr(2, 'main'), pr(3, 'feat/a'), pr(4, 'feat/b')]
        # PR 3 has head feat/b (the base of PR 4); PR 4 has head feat/c (nobody's base).
        self.assertFalse(decide(prs, 'feat/b', 3))
        self.assertTrue(decide(prs, 'feat/c', 4))

    def test_a_pull_request_on_main_with_a_child_is_not_a_tip(self):
        self.assertFalse(decide([pr(2, 'main'), pr(3, 'feat/a')]))

    def test_multiple_children_still_mean_not_a_tip(self):
        self.assertFalse(decide([pr(2, 'main'), pr(3, 'feat/a'), pr(4, 'feat/a')]))

    def test_a_child_from_a_fork_does_not_count(self):
        self.assertTrue(decide([pr(2, 'main'), pr(3, 'feat/a', head_repo='evil/sourcesteward')]))

    def test_a_child_with_a_deleted_fork_does_not_count(self):
        self.assertTrue(decide([pr(2, 'main'), pr(3, 'feat/a', head_repo=None)]))

    def test_a_child_targeting_another_repository_does_not_count(self):
        self.assertTrue(decide([pr(2, 'main'), pr(3, 'feat/a', base_repo='other/repo')]))

    def test_a_pull_request_from_a_fork_is_always_a_tip(self):
        prs = [pr(2, 'main', head_repo='evil/sourcesteward'), pr(3, 'feat/a')]
        self.assertTrue(decide(prs, head_repo='evil/sourcesteward'))

    def test_an_unknown_head_repository_is_a_tip(self):
        self.assertTrue(decide([pr(2, 'main'), pr(3, 'feat/a')], head_repo=''))

    def test_the_head_branch_equal_to_the_default_branch_is_a_tip(self):
        self.assertTrue(decide([pr(2, 'main'), pr(3, 'main')], head='main'))

    def test_the_pull_request_itself_never_counts_as_its_own_child(self):
        self.assertTrue(decide([pr(2, 'feat/a')], head='feat/a', number=2))

    def test_a_duplicate_of_the_pull_request_itself_is_not_a_child(self):
        self.assertTrue(decide([pr(2, 'main'), pr(2, 'main')]))

    def test_a_similar_branch_name_is_not_the_same_branch(self):
        self.assertTrue(decide([pr(2, 'main'), pr(3, 'feat/a/b'), pr(4, 'Feat/a'), pr(5, 'feat/a ')]))

    def test_only_pull_request_events_can_be_non_tips(self):
        for event in ('push', 'workflow_dispatch', 'schedule', ''):
            self.assertTrue(decide([pr(2, 'main'), pr(3, 'feat/a')], event=event), event)

    def test_a_failed_lookup_is_a_tip(self):
        self.assertTrue(decide(None))

    def test_an_empty_listing_or_one_without_this_pull_request_is_a_tip(self):
        self.assertTrue(decide([]))
        self.assertTrue(decide([pr(3, 'feat/a')]))

    def test_a_missing_head_ref_or_number_is_a_tip(self):
        prs = [pr(2, 'main'), pr(3, 'feat/a')]
        self.assertTrue(decide(prs, head=''))
        self.assertTrue(decide(prs, number=None))


class ParseTests(unittest.TestCase):
    def test_reads_a_stream_of_objects(self):
        text = json.dumps(pr(2, 'main')) + '\n' + json.dumps(pr(3, 'feat/a')) + '\n'
        self.assertEqual([p['number'] for p in STACK.parse_prs(text)], [2, 3])

    def test_reads_arrays_too_and_empty_input_is_an_empty_list(self):
        self.assertEqual(len(STACK.parse_prs(json.dumps([pr(2, 'main')]) + json.dumps([pr(3, 'x')]))), 2)
        self.assertEqual(STACK.parse_prs(' \n'), [])

    def test_malformed_input_raises(self):
        for text in ('{', 'nope', '[1]', '"x"', json.dumps({'number': 2}),
                     json.dumps({'number': 'two', 'base_ref': 'a', 'base_repo': 'r', 'head_repo': 'r'}),
                     json.dumps({'number': 2, 'base_ref': 5, 'base_repo': 'r', 'head_repo': 'r'})):
            with self.assertRaises(ValueError, msg=text):
                STACK.parse_prs(text)


class MainTests(unittest.TestCase):
    def run_main(self, content, **env):
        base = {'EVENT_NAME': 'pull_request', 'PR_NUMBER': '2', 'HEAD_REF': 'feat/a', 'HEAD_REPO': REPO,
                'BASE_REPO': REPO, 'DEFAULT_BRANCH': 'main'}
        base.update(env)
        with tempfile.TemporaryDirectory() as tmp:
            path = os.path.join(tmp, 'prs.json')
            if content is not None:
                Path(path).write_text(content)
            old = {k: os.environ.get(k) for k in base}
            os.environ.update(base)
            try:
                out = io.StringIO()
                with redirect_stdout(out):
                    code = STACK.main(['--prs-file', path])
            finally:
                for k, v in old.items():
                    if v is None:
                        os.environ.pop(k, None)
                    else:
                        os.environ[k] = v
        self.assertEqual(code, 0)
        return out.getvalue()

    def listing(self, *prs):
        return '\n'.join(json.dumps(p) for p in prs) + '\n'

    def test_prints_false_for_a_pull_request_with_a_child(self):
        self.assertEqual(self.run_main(self.listing(pr(2, 'main'), pr(3, 'feat/a'))), 'is_stack_tip=false\n')

    def test_prints_true_without_a_child(self):
        self.assertEqual(self.run_main(self.listing(pr(2, 'main'))), 'is_stack_tip=true\n')

    def test_a_missing_or_garbled_file_is_a_tip(self):
        self.assertEqual(self.run_main(None), 'is_stack_tip=true\n')
        self.assertEqual(self.run_main('{garbage'), 'is_stack_tip=true\n')

    def test_a_non_numeric_pull_request_number_is_a_tip(self):
        self.assertEqual(self.run_main(self.listing(pr(2, 'main'), pr(3, 'feat/a')), PR_NUMBER='x'), 'is_stack_tip=true\n')

    def test_a_push_event_is_a_tip(self):
        self.assertEqual(self.run_main(self.listing(pr(2, 'main'), pr(3, 'feat/a')), EVENT_NAME='push'), 'is_stack_tip=true\n')

    def test_branch_names_with_shell_metacharacters_are_plain_data(self):
        name = 'x$(touch pwned);`id`\n"\''
        out = self.run_main(self.listing(pr(2, 'main'), pr(3, name)), HEAD_REF=name)
        self.assertEqual(out, 'is_stack_tip=false\n')


if __name__ == '__main__':
    unittest.main()
