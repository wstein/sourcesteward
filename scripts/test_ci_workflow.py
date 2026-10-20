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


class CheckJobTests(unittest.TestCase):
    def test_check_job_runs_static_checks_and_race_suite_once(self):
        check = job_text('check')
        self.assertIn('run: make check-static', check)
        self.assertNotIn('run: make check\n', check)
        self.assertEqual(len(re.findall(r'go test\b', check)), 1, 'check must run go test exactly once')
        self.assertIn('run: go test -timeout 40m -race ./...', check)

    def test_make_check_keeps_every_step_and_check_static_omits_only_go_test(self):
        makefile = (ROOT / 'Makefile').read_text()
        check = re.search(r'^check:[^\n]*\n((?:\t[^\n]*\n)+)', makefile, re.M).group(1)
        static = re.search(r'^check-static:[^\n]*\n((?:\t[^\n]*\n)+)', makefile, re.M).group(1)
        self.assertIn('$(GO) test -timeout 40m ./...', check)
        self.assertIn('check-static', check)
        self.assertNotIn('$(GO) test', static)
        for required in ('gofmt -l', '$(GO) vet ./...', '$(GO) mod verify', 'unittest discover',
                         'scripts/test_theme.mjs', 'scripts/check-docs.py', 'scripts/check-architecture.py',
                         'git diff --check'):
            self.assertIn(required, static)


class CacheTests(unittest.TestCase):
    def test_every_action_is_pinned_by_full_commit_sha_with_version_comment(self):
        uses = re.findall(r'^\s*-?\s*uses: (\S+)(.*)$', WORKFLOW, re.M)
        self.assertTrue(uses)
        for action, rest in uses:
            self.assertRegex(action, r'^[\w.-]+/[\w./-]+@[0-9a-f]{40}$')
            self.assertRegex(rest, r'^\s+# v\d+\.\d+\.\d+$', action)

    def test_setup_go_caches_modules_and_build_with_explicit_dependency_path(self):
        for job in ('check', 'conformance'):
            text = job_text(job)
            self.assertRegex(text, r'actions/setup-go@[0-9a-f]{40} # v[\d.]+\n        with:\n          go-version-file: go.mod\n          cache: true\n          cache-dependency-path: go.sum\n')

    def test_git_build_is_cached_by_exact_version_os_and_arch(self):
        text = job_text('conformance')
        self.assertRegex(text, r'actions/cache@[0-9a-f]{40} # v\d+\.\d+\.\d+')
        self.assertIn('path: .tools/conformance/${{ matrix.lane }}/git', text)
        key = re.search(r'key: (stw-git-[^\n]+)', text).group(1)
        for part in ('runner.os', 'runner.arch', 'steps.gitpin.outputs.version', 'matrix.lane'):
            self.assertIn(part, key)

    def test_cache_hit_skips_the_git_build_but_keeps_the_version_assertion(self):
        text = job_text('conformance')
        self.assertIn('GIT_CACHE_HIT: ${{ steps.gitcache.outputs.cache-hit }}', text)
        self.assertIn('"$GIT_CACHE_HIT" = true', text)
        self.assertIn('--tool jj', text)
        self.assertLess(text.index('actions/cache@'), text.index('scripts/install-backends.py'))
        self.assertLess(text.index('scripts/install-backends.py'), text.index('--preflight "$BACKEND_LANE"'))


if __name__ == '__main__':
    unittest.main()
