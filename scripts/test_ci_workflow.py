# SPDX-License-Identifier: EUPL-1.2
"""Structure tests for the CI workflow: triggers, de-duplication, caches, tiering.

Standard library only: the workflow is inspected as text.
"""
import importlib.util
import re
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]
WORKFLOW = (ROOT / '.github/workflows/ci.yml').read_text()
CONFORMANCE = (ROOT / '.github/workflows/conformance.yml').read_text()
FULL = (ROOT / '.github/workflows/ci-full.yml').read_text()
ALL = (WORKFLOW, CONFORMANCE, FULL)
_SPEC = importlib.util.spec_from_file_location('ci_shards', ROOT / 'scripts/ci_shards.py')
SHARDS = importlib.util.module_from_spec(_SPEC)
_SPEC.loader.exec_module(SHARDS)


def job_text(name, text=WORKFLOW):
    match = re.search(r'^  %s:\n(.*?)(?=^  [a-z-]+:\n|\Z)' % name, text, re.S | re.M)
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
        runs = re.findall(r'go test\b[^\n]*', check)
        self.assertEqual(len(runs), 2, 'one race run per tier branch, never both')
        for run in runs:
            self.assertIn('-timeout 40m -race', run)
        self.assertIn('go test -timeout 40m -race ./...', runs[1])

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
        uses = [u for text in ALL for u in re.findall(r'^\s*-?\s*uses: (\S+)(.*)$', text, re.M)]
        self.assertTrue(uses)
        for action, rest in uses:
            if action.startswith('./'):
                continue  # local reusable workflow, same commit
            self.assertRegex(action, r'^[\w.-]+/[\w./-]+@[0-9a-f]{40}$')
            self.assertRegex(rest, r'^\s+# v\d+\.\d+\.\d+$', action)

    def test_setup_go_caches_modules_and_build_with_explicit_dependency_path(self):
        for text in (job_text('check'), job_text('shard', CONFORMANCE)):
            self.assertRegex(text, r'actions/setup-go@[0-9a-f]{40} # v[\d.]+\n        with:\n          go-version-file: go.mod\n          cache: true\n          cache-dependency-path: go.sum\n')

    def test_git_build_is_cached_by_exact_version_os_and_arch(self):
        text = job_text('shard', CONFORMANCE)
        self.assertRegex(text, r'actions/cache@[0-9a-f]{40} # v\d+\.\d+\.\d+')
        self.assertIn('path: .tools/conformance/${{ inputs.lane }}/git', text)
        key = re.search(r'key: (stw-git-[^\n]+)', text).group(1)
        for part in ('runner.os', 'runner.arch', 'steps.gitpin.outputs.version', 'inputs.lane',
                     "hashFiles('scripts/install-backends.py', 'scripts/backend-releases.json')"):
            self.assertIn(part, key)
        self.assertNotIn('restore-keys', text)

    def test_cache_hit_skips_the_git_build_but_keeps_the_version_assertion(self):
        text = job_text('shard', CONFORMANCE)
        self.assertIn('GIT_CACHE_HIT: ${{ steps.gitcache.outputs.cache-hit }}', text)
        self.assertIn('"$GIT_CACHE_HIT" = true', text)
        self.assertIn('--tool jj', text)
        self.assertLess(text.index('actions/cache@'), text.index('scripts/install-backends.py'))
        self.assertLess(text.index('scripts/install-backends.py'), text.index('--preflight "$BACKEND_LANE"'))


class TieringTests(unittest.TestCase):
    CONDITION = ("github.event_name != 'pull_request' || "
                 "github.event.pull_request.base.ref == github.event.repository.default_branch || "
                 "contains(github.event.pull_request.labels.*.name, 'ci-full')")

    def test_ci_conformance_is_tiered_by_base_branch_or_ci_full_label(self):
        text = job_text('conformance')
        self.assertIn('if: ' + self.CONDITION + '\n', text)
        self.assertIn('uses: ./.github/workflows/conformance.yml', text)
        self.assertIn('lane: [minimum, current]', text)
        self.assertIn('name: Git/jj conformance (${{ matrix.lane }})', text)

    def test_check_job_always_runs(self):
        self.assertNotRegex(job_text('check'), r'(?m)^    if:')

    def test_ci_does_not_react_to_label_events(self):
        on = WORKFLOW.split('\npermissions:', 1)[0]
        self.assertNotIn('labeled', on)
        self.assertNotIn('types:', on)

    def test_pages_waits_for_check_and_conformance(self):
        self.assertIn('needs: [check, conformance]', job_text('pages'))


class CiFullWorkflowTests(unittest.TestCase):
    def test_runs_only_on_the_label_event_for_ci_full(self):
        on = FULL.split('\npermissions:', 1)[0]
        self.assertRegex(on, r'(?m)^  pull_request:\n    types: \[labeled\]\n')
        self.assertNotIn('push:', on)
        self.assertIn("if: github.event.label.name == 'ci-full'", job_text('conformance', FULL))

    def test_permissions_are_read_only_and_match_ci(self):
        for text in (FULL, CONFORMANCE, WORKFLOW):
            head = text.split('\njobs:', 1)[0]
            self.assertRegex(head, r'(?m)^permissions:\n  contents: read\n(?!  )')

    def test_calls_the_reusable_suite_for_both_lanes(self):
        text = job_text('conformance', FULL)
        self.assertIn('uses: ./.github/workflows/conformance.yml', text)
        self.assertIn('lane: [minimum, current]', text)
        self.assertIn('lane: ${{ matrix.lane }}', text)

    def test_reusable_workflow_is_call_only_with_a_stable_job_name(self):
        head = CONFORMANCE.split('\npermissions:', 1)[0]
        self.assertIn('workflow_call:', head)
        for forbidden in ('push:', 'pull_request:', 'workflow_dispatch:', 'schedule:'):
            self.assertNotIn(forbidden, head)
        self.assertIn('name: suite', job_text('suite', CONFORMANCE))


class ShardedConformanceTests(unittest.TestCase):
    def test_shard_matrix_matches_script_default(self):
        count = int(re.search(r"SHARD_COUNT: '(\d+)'", CONFORMANCE).group(1))
        self.assertEqual(count, SHARDS.DEFAULT_SHARDS)
        matrix = re.search(r'shard: \[([\d, ]+)\]', CONFORMANCE).group(1)
        self.assertEqual([int(x) for x in matrix.split(',')], list(range(count + 1)))
        self.assertIn('--shards 4', job_text('check'))

    def test_aggregate_is_per_lane_stable_and_runs_after_failures(self):
        text = job_text('suite', CONFORMANCE)
        self.assertIn('name: suite', text)
        self.assertIn('needs: [shard]', text)
        self.assertIn('if: always()', text)
        self.assertIn('test "$SHARDS_RESULT" = success', text)
        self.assertNotIn('matrix', text)

    def test_command_substitutions_are_assigned_before_go_test(self):
        for text in (job_text('shard', CONFORMANCE), job_text('check')):
            for line in re.findall(r'go test[^\n]*', text):
                self.assertNotIn('$(', line)
            self.assertNotRegex(text, r'go test[^\n]*\$\(python3')
        shard = job_text('shard', CONFORMANCE)
        self.assertRegex(shard, r'regex="\$\(python3 -B scripts/ci_shards.py regex[^\n]*\)"\n\s+test -n "\$regex"\n')
        self.assertRegex(shard, r'packages="\$\(python3 -B scripts/ci_shards.py packages\)"\n\s+set -- \$packages\n\s+test "\$#" -gt 1\n')
        self.assertRegex(job_text('check'), r'packages="\$\(python3 -B scripts/ci_shards.py packages\)"\n\s+set -- \$packages\n\s+test "\$#" -gt 1\n')

    def test_only_the_workspace_package_is_sharded(self):
        text = job_text('shard', CONFORMANCE)
        self.assertIn('-run "$regex" ./internal/workspace', text)
        self.assertIn('matrix.shard != 0', text)
        self.assertIn('matrix.shard == 0', text)

    def test_check_verifies_shard_coverage_before_the_race_suite(self):
        text = job_text('check')
        self.assertLess(text.index('scripts/ci_shards.py check'), text.index('go test -timeout 40m -race'))


class LongPoleTests(unittest.TestCase):
    def evaluate(self, expression, name, base=None, labels=()):
        python = expression.replace('||', ' or ')
        python = re.sub(r"contains\(github\.event\.pull_request\.labels\.\*\.name, '([^']+)'\)", r"('\1' in labels)", python)
        python = python.replace('github.event.pull_request.base.ref', 'base')
        python = python.replace('github.event.repository.default_branch', "'main'")
        python = python.replace('github.event_name', 'event')
        return eval(python, {}, {'event': name, 'base': base, 'labels': list(labels)})

    def tier(self):
        return re.search(r'(?m)^  CONFORMANCE_TIER: \$\{\{ (.+) \}\}$', WORKFLOW).group(1)

    def test_tier_expression_is_the_conformance_condition(self):
        self.assertEqual(self.tier(), TieringTests.CONDITION)
        self.assertIn('if: ' + self.tier() + '\n', job_text('conformance'))

    def test_tier_per_event(self):
        tier = self.tier()
        self.assertTrue(self.evaluate(tier, 'push', None))
        self.assertTrue(self.evaluate(tier, 'pull_request', 'main'))
        self.assertFalse(self.evaluate(tier, 'pull_request', 'codex/parent'))
        self.assertTrue(self.evaluate(tier, 'pull_request', 'codex/parent', ['ci-full']))
        self.assertFalse(self.evaluate(tier, 'pull_request', 'codex/parent', ['other']))

    def test_check_skips_the_workspace_package_only_in_the_tier(self):
        text = job_text('check')
        self.assertIn('if [ "$CONFORMANCE_TIER" = true ]; then', text)
        full = text.index('else\n')
        self.assertIn('ci_shards.py packages', text[:full])
        self.assertIn('go test -timeout 40m -race ./...', text[full:])


if __name__ == '__main__':
    unittest.main()
