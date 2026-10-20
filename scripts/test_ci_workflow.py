# SPDX-License-Identifier: EUPL-1.2
"""Structure tests for the CI workflow: triggers, de-duplication, caches, tiering.

Standard library only: the workflow is inspected as text.
"""
import re
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]
WORKFLOW = (ROOT / '.github/workflows/ci.yml').read_text()


def job_text(name):
    match = re.search(r'^  %s:\n(.*?)(?=^  [a-z-]+:\n|\Z)' % name, WORKFLOW, re.S | re.M)
    assert match, name
    return match.group(1)


class TriggerTests(unittest.TestCase):
    def test_push_runs_only_for_the_default_branch(self):
        on = WORKFLOW.split('\npermissions:', 1)[0]
        self.assertRegex(on, r'(?m)^  push:\n    branches: \[main\]\n')
        self.assertNotIn('tags:', on)

    def test_pull_request_still_triggers(self):
        on = WORKFLOW.split('\npermissions:', 1)[0]
        self.assertRegex(on, r'(?m)^  pull_request:')

    def test_deployment_stays_main_push_only(self):
        self.assertIn("if: github.event_name == 'push' && github.ref == 'refs/heads/main'", job_text('pages'))


if __name__ == '__main__':
    unittest.main()
