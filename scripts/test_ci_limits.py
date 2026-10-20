# SPDX-License-Identifier: EUPL-1.2
import re
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]
WORKFLOW = (ROOT / '.github/workflows/ci.yml').read_text()
MIN_TEST_MINUTES = 40
JOB_MARGIN_MINUTES = 5


def job_text(name):
    match = re.search(r'^  %s:\n(.*?)(?=^  [a-z]+:\n|\Z)' % name, WORKFLOW, re.S | re.M)
    assert match, name
    return match.group(1)


class CILimitTests(unittest.TestCase):
    def test_race_test_jobs_have_explicit_timeout_below_job_limit(self):
        for job in ('check', 'conformance'):
            text = job_text(job)
            tests = re.findall(r'go test [^\n]*-race[^\n]*', text)
            self.assertTrue(tests, job)
            job_limit = int(re.search(r'timeout-minutes: (\d+)', text).group(1))
            for command in tests:
                found = re.search(r'-timeout[= ](\d+)m\b', command)
                self.assertIsNotNone(found, command)
                minutes = int(found.group(1))
                self.assertGreaterEqual(minutes, MIN_TEST_MINUTES, command)
                self.assertGreaterEqual(job_limit, minutes + JOB_MARGIN_MINUTES, job)

    def test_make_check_tests_have_explicit_timeout(self):
        makefile = (ROOT / 'Makefile').read_text()
        commands = re.findall(r'\$\(GO\) test [^\n]*', makefile)
        self.assertTrue(commands)
        for command in commands:
            self.assertRegex(command, r'-timeout \d+m')

    def test_check_job_covers_sequential_plain_and_race_runs(self):
        limit = int(re.search(r'timeout-minutes: (\d+)', job_text('check')).group(1))
        self.assertGreaterEqual(limit, 2 * MIN_TEST_MINUTES - 20)


if __name__ == '__main__':
    unittest.main()
