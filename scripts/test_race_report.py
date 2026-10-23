# SPDX-License-Identifier: EUPL-1.2
"""Tests for the race-failure reporter (Spec 039 T4): one open issue, then comments, only matched test names."""
import importlib.util
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
_SPEC = importlib.util.spec_from_file_location('race_report', ROOT / 'scripts/race_report.py')
RR = importlib.util.module_from_spec(_SPEC)
_SPEC.loader.exec_module(RR)

REPO = 'wstein/sourcesteward'
RUN_URL = 'https://github.com/wstein/sourcesteward/actions/runs/777'
SECRET = 'ghp_SECRETTOKENVALUE0123456789'
# A simulated failed job log: timestamped lines, an injected instruction, a data race report and failures.
LOG = '\n'.join([
    '2026-10-23T01:00:00.1234567Z ##[group]Run go test',
    '2026-10-23T01:00:01.0000000Z WARNING: DATA RACE',
    '2026-10-23T01:00:01.0000001Z Write at 0x00c000 by goroutine 8: token=' + SECRET,
    '2026-10-23T01:00:02.0000000Z --- FAIL: TestAlpha (0.10s)',
    '2026-10-23T01:00:02.0000001Z     --- FAIL: TestAlpha/sub (0.10s)',
    '2026-10-23T01:00:03.0000000Z --- FAIL: TestBeta (1.00s)',
    '2026-10-23T01:00:03.0000001Z --- FAIL: TestAlpha (0.10s)',
    '2026-10-23T01:00:04.0000000Z note --- FAIL: TestNotLineStart',
    '2026-10-23T01:00:04.0000001Z --- FAIL: Benchmark_nope',
    '2026-10-23T01:00:05.0000000Z --- FAIL: TestEvil`; rm -rf / # (0.00s)',
    'FAIL\tgithub.com/wstein/sourcesteward/internal/workspace\t12.3s',
])


class FakeGh:
    """Models gh: jobs with conclusions, open and closed issues; it checks the arguments it is given."""

    def __init__(self, jobs=('11', '12'), logs=None, passed=None, fail_logs=False):
        self.calls = []
        self.issues = []  # dicts: number, state, body, comments, args
        self.jobs = list(jobs)
        self.passed = dict(passed or {})  # job id -> log of a job that succeeded
        self.logs = dict(logs) if logs is not None else {j: LOG for j in jobs}
        self.logs.update(self.passed)
        self.fail_logs = fail_logs

    def __call__(self, args, stdin=None):
        self.calls.append((list(args), stdin))
        if args[0] == 'api' and '--jq' in args:
            if 'select(.conclusion == "failure")' in args[args.index('--jq') + 1]:
                return '\n'.join(self.jobs)
            return '\n'.join(self.jobs + list(self.passed))
        if args[0] == 'api':
            if self.fail_logs:
                raise RR.GhError('api')
            return self.logs[args[1].split('/')[-2]]
        if args[:2] == ['issue', 'list']:
            wanted = args[args.index('--state') + 1] if '--state' in args else 'all'
            label = args[args.index('--label') + 1] if '--label' in args else None
            hits = [i for i in self.issues if (wanted == 'all' or i['state'] == wanted) and (label in i['args'] or label is None)]
            return str(hits[0]['number']) if hits else ''
        if args[:2] == ['issue', 'create']:
            self.issues.append({'number': len(self.issues) + 1, 'state': 'open', 'body': stdin, 'comments': [], 'args': list(args)})
            return 'https://github.com/%s/issues/%d' % (REPO, len(self.issues))
        if args[:2] == ['issue', 'comment']:
            self.issues[int(args[2]) - 1]['comments'].append(stdin)
            return ''
        if args[:2] == ['label', 'create']:
            return ''
        raise AssertionError(args)

    def writes(self):
        return [c for c in self.calls if c[0][:2] in (['issue', 'create'], ['issue', 'comment'])]


def run(gh, tier='nightly', lane='current', reason='failed'):
    return RR.report(gh, REPO, '777', RUN_URL, tier, lane, reason)


class ExtractTests(unittest.TestCase):
    def test_only_top_level_test_failures_at_line_start(self):
        self.assertEqual(RR.extract_tests(LOG), ['TestAlpha', 'TestBeta', 'TestEvil'])

    def test_names_are_deduplicated_in_order_and_bounded_to_twenty(self):
        log = '\n'.join('--- FAIL: TestN%02d (0.1s)' % i for i in range(60)) + '\n--- FAIL: TestN00 (0.1s)'
        names = RR.extract_tests(log)
        self.assertEqual(len(names), 20)
        self.assertEqual(names[0], 'TestN00')
        self.assertEqual(names[-1], 'TestN19')

    def test_unprefixed_lines_match_too(self):
        self.assertEqual(RR.extract_tests('--- FAIL: TestPlain (0.00s)'), ['TestPlain'])

    def test_a_data_race_block_with_its_fail_line_yields_the_test_name(self):
        log = '\n'.join(['==================', 'WARNING: DATA RACE', 'Read at 0x00c0 by goroutine 7:', '==================',
                         '--- FAIL: TestRacy (0.01s)', '    testing.go:1398: race detected during execution of test'])
        self.assertEqual(RR.extract_tests(log), ['TestRacy'])

    def test_no_match_gives_empty_list(self):
        self.assertEqual(RR.extract_tests('nothing\nWARNING: DATA RACE\n'), [])


class BodyTests(unittest.TestCase):
    def test_body_has_link_tier_lane_reason_and_names(self):
        body = RR.build_body(RUN_URL, 'weekly', 'minimum', 'failed', ['TestAlpha'])
        for part in (RUN_URL, 'weekly', 'minimum', 'failed', 'TestAlpha'):
            self.assertIn(part, body)

    def test_cancelled_reason_wording(self):
        body = RR.build_body(RUN_URL, 'nightly', 'current', 'cancelled', [])
        self.assertIn('failed, cancelled or timed out', body)
        self.assertIn('may be missing', body)
        self.assertNotIn('TestAlpha', body)
        self.assertIn('No test name matched, see the run.', body)

    def test_unknown_reason_is_rejected(self):
        with self.assertRaises(ValueError):
            RR.build_body(RUN_URL, 'nightly', 'current', 'x; rm', [])


class ReportTests(unittest.TestCase):
    def test_first_failure_creates_one_labelled_issue(self):
        gh = FakeGh()
        run(gh)
        self.assertEqual(len(gh.issues), 1)
        self.assertIn('race-failure', gh.issues[0]['args'])
        self.assertEqual(gh.issues[0]['comments'], [])
        self.assertIn('TestAlpha', gh.issues[0]['body'])
        self.assertIn('TestBeta', gh.issues[0]['body'])

    def test_second_failure_only_comments_on_the_open_issue(self):
        gh = FakeGh()
        run(gh)
        run(gh, tier='weekly', lane='minimum')
        run(gh, reason='cancelled')
        self.assertEqual(len(gh.issues), 1)
        self.assertEqual(len(gh.issues[0]['comments']), 2)
        self.assertIn('weekly', gh.issues[0]['comments'][0])

    def test_after_closing_a_new_issue_is_created(self):
        gh = FakeGh()
        run(gh)
        gh.issues[0]['state'] = 'closed'
        run(gh)
        self.assertEqual(len(gh.issues), 2)
        self.assertEqual(gh.issues[0]['comments'], [])
        self.assertEqual(gh.issues[1]['state'], 'open')

    def test_log_text_is_never_copied(self):
        gh = FakeGh()
        run(gh)
        run(gh)
        texts = [gh.issues[0]['body']] + gh.issues[0]['comments']
        for text in texts:
            for leaked in (SECRET, 'goroutine', 'DATA RACE', 'rm -rf', 'Benchmark_nope', 'NotLineStart', 'sub'):
                self.assertNotIn(leaked, text)

    def test_failed_job_without_a_matched_name_says_so_and_copies_no_log(self):
        gh = FakeGh(jobs=('11',), logs={'11': 'WARNING: DATA RACE\nsecret ' + SECRET + '\npanic: boom'})
        run(gh)
        body = gh.issues[0]['body']
        self.assertIn('No test name matched, see the run.', body)
        for leaked in (SECRET, 'panic', 'DATA RACE'):
            self.assertNotIn(leaked, body)

    def test_cancelled_run_without_logs_still_reports(self):
        gh = FakeGh(jobs=())
        run(gh, reason='cancelled')
        self.assertEqual(len(gh.issues), 1)
        self.assertIn('failed, cancelled or timed out', gh.issues[0]['body'])

    def test_only_failed_jobs_are_read(self):
        gh = FakeGh(jobs=('11',), logs={'11': LOG}, passed={'99': '--- FAIL: TestFromPassedJob (0s)'})
        run(gh)
        self.assertNotIn('TestFromPassedJob', gh.issues[0]['body'])
        self.assertFalse([c for c in gh.calls if c[0][-1].endswith('/jobs/99/logs')])

    def test_invalid_utf8_in_a_log_does_not_lose_the_alarm(self):
        raw = b'--- FAIL: TestBytes (0s)\n\xff\xfe broken\n'
        self.assertEqual(RR.extract_tests(raw.decode('utf-8', errors='replace')), ['TestBytes'])
        import subprocess
        from unittest import mock
        done = subprocess.CompletedProcess(['gh'], 0, stdout=raw, stderr=b'')
        with mock.patch.object(RR.subprocess, 'run', return_value=done):
            self.assertIn('TestBytes', RR._gh(['api', 'x']))

    def test_a_failed_log_fetch_still_files_the_issue_with_the_fallback(self):
        gh = FakeGh(fail_logs=True)
        run(gh)
        self.assertEqual(len(gh.issues), 1)
        self.assertIn('No test name matched, see the run.', gh.issues[0]['body'])

    def test_a_failed_job_listing_still_files_the_issue(self):
        class NoJobs(FakeGh):
            def __call__(self, args, stdin=None):
                if args[0] == 'api':
                    raise RR.GhError('api')
                return super().__call__(args, stdin)
        gh = NoJobs()
        run(gh)
        self.assertEqual(len(gh.issues), 1)

    def test_a_failed_issue_call_is_not_swallowed(self):
        class NoIssues(FakeGh):
            def __call__(self, args, stdin=None):
                if args[0] == 'issue':
                    raise RR.GhError('issue')
                return super().__call__(args, stdin)
        with self.assertRaises(RR.GhError):
            run(NoIssues())

    def test_non_numeric_job_ids_are_ignored(self):
        gh = FakeGh(jobs=('11', '../x'), logs={'11': LOG})
        run(gh)
        self.assertEqual(len(gh.issues), 1)

    def test_run_id_must_be_numeric(self):
        with self.assertRaises(ValueError):
            RR.report(FakeGh(), REPO, '777/../x', RUN_URL, 'nightly', 'current', 'failed')

    def test_names_across_jobs_stay_bounded(self):
        many = '\n'.join('--- FAIL: TestJ%02d (1s)' % i for i in range(15))
        other = '\n'.join('--- FAIL: TestK%02d (1s)' % i for i in range(15))
        gh = FakeGh(jobs=('1', '2'), logs={'1': many, '2': other})
        run(gh)
        self.assertEqual(gh.issues[0]['body'].count('`Test'), 20)


if __name__ == '__main__':
    unittest.main()
