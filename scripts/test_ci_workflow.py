# SPDX-License-Identifier: EUPL-1.2
"""Structure tests for the CI workflow: triggers, de-duplication, caches, tiering.

Standard library only: the workflow is inspected as text.
"""
import importlib.util
import os
import re
import shutil
import subprocess
import tempfile
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]
WORKFLOW = (ROOT / '.github/workflows/ci.yml').read_text()
CONFORMANCE = (ROOT / '.github/workflows/conformance.yml').read_text()
FULL = (ROOT / '.github/workflows/ci-full.yml').read_text()
RACE = (ROOT / '.github/workflows/race.yml').read_text()
ALL = (WORKFLOW, CONFORMANCE, FULL, RACE)
_SPEC = importlib.util.spec_from_file_location('ci_shards', ROOT / 'scripts/ci_shards.py')
SHARDS = importlib.util.module_from_spec(_SPEC)
_SPEC.loader.exec_module(SHARDS)
_CSPEC = importlib.util.spec_from_file_location('ci_changes', ROOT / 'scripts/ci_changes.py')
CHANGES = importlib.util.module_from_spec(_CSPEC)
_CSPEC.loader.exec_module(CHANGES)


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

    def test_only_pull_request_runs_are_cancelled_by_a_newer_push(self):
        group = WORKFLOW.split('\nconcurrency:', 1)[1].split('\njobs:', 1)[0]
        self.assertIn('group: ci-${{ github.workflow }}-${{ github.ref }}', group)
        self.assertIn("cancel-in-progress: ${{ github.event_name == 'pull_request' }}", group)

    def test_deployment_stays_main_push_only(self):
        self.assertIn("if: github.event_name == 'push' && github.ref == 'refs/heads/main'", job_text('pages'))


class CheckJobTests(unittest.TestCase):
    def test_check_job_runs_static_checks_and_plain_test_suite_once(self):
        check = job_text('check')
        self.assertIn('run: make check-static', check)
        self.assertNotIn('run: make check\n', check)
        runs = re.findall(r'go test\b[^\n]*', check)
        self.assertEqual(len(runs), 1, 'one test run, never a plain pass repeated')
        for run in runs:
            self.assertIn('-timeout 40m', run)
        self.assertIn('go test -timeout 40m "$@"', runs[0])

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


class NoRaceOnPullRequestTests(unittest.TestCase):
    """Owner decision 2026-10-22 (Spec 039): -race never runs on a pull request or a main push."""

    def test_no_go_test_command_uses_a_literal_race_flag(self):
        for name, text in (('ci.yml', WORKFLOW), ('conformance.yml', CONFORMANCE), ('ci-full.yml', FULL)):
            commands = re.findall(r'^[^#\n]*\bgo test\b[^\n]*', text, re.M)
            for command in commands:
                self.assertNotRegex(command, r'(^|\s)-race\b', '%s: %s' % (name, command))

    def test_no_race_option_or_race_step_remains_in_ci_yml_and_ci_full_yml(self):
        for name, text in (('ci.yml', WORKFLOW), ('ci-full.yml', FULL)):
            code = '\n'.join(line for line in text.splitlines() if not line.lstrip().startswith('#'))
            self.assertNotRegex(code, r'(?i)-race\b|name: Race tests|\brace:', name)

    def test_conformance_race_input_is_boolean_and_defaults_to_false(self):
        self.assertRegex(CONFORMANCE, r'(?m)^      race:\n        description: [^\n]*\n        required: false\n        default: false\n        type: boolean\n')

    def test_conformance_runs_race_only_when_the_input_is_true(self):
        code = '\n'.join(line for line in CONFORMANCE.splitlines() if not line.lstrip().startswith('#'))
        self.assertEqual(len(re.findall(r'-race\b', code)), 2, 'one gated assignment per go test step')
        self.assertEqual(len(re.findall(r'RACE: \$\{\{ inputs\.race \}\}', code)), 2)
        self.assertEqual(len(re.findall(r'(?m)^ *if \[ "\$RACE" = true \]; then race_flag=-race; fi$', code)), 2)
        self.assertEqual(len(re.findall(r'go test -count=1 -timeout 40m \$race_flag ', code)), 2)

    def test_no_pull_request_or_push_caller_passes_race(self):
        for name in ('ci.yml', 'ci-full.yml'):
            text = (ROOT / '.github/workflows' / name).read_text()
            self.assertNotRegex(text, r'(?m)^\s+race:', name)
        for path in (ROOT / '.github/workflows').glob('*.yml'):
            text = path.read_text()
            if path.name != 'race.yml' and 'conformance.yml' in text and path.name != 'conformance.yml':
                self.assertNotRegex(text, r'(?m)^\s+race:', path.name)


class NoForbiddenTriggerTests(unittest.TestCase):
    def test_no_workflow_uses_pull_request_target_or_an_edited_trigger(self):
        for name, text in (('ci.yml', WORKFLOW), ('ci-full.yml', FULL), ('conformance.yml', CONFORMANCE)):
            self.assertNotIn('pull_request_target', text, name)
        self.assertNotRegex(WORKFLOW.split('\npermissions:', 1)[0], r'edited')

    def test_ci_full_has_no_race_input_or_race_value(self):
        self.assertNotRegex(FULL, r'(?m)^\s*race\b')
        self.assertNotIn('race:', FULL)


class GoTestsStepExecutionTests(unittest.TestCase):
    """Runs the `Go tests` step of ci.yml with a stub `go` that records its arguments."""

    def step(self):
        text = job_text('check')
        body = text.split('- name: Go tests\n', 1)[1].split('\n      - name:', 1)[0]
        return body.split('        run: |\n', 1)[1]

    def run_step(self, reason, affected):
        real_go = shutil.which('go')
        with tempfile.TemporaryDirectory() as tmp:
            stub = Path(tmp, 'go')
            stub.write_text('#!/bin/sh\nif [ "$1" = test ]; then echo "GOTEST $*"; exit 0; fi\nexec %s "$@"\n' % real_go)
            stub.chmod(0o755)
            env = dict(os.environ, PATH=tmp + os.pathsep + os.environ['PATH'], AFFECTED=affected,
                       AFFECTED_REASON=reason, GITHUB_STEP_SUMMARY=os.devnull)
            out = subprocess.run(['bash', '-e', '-c', self.step()], cwd=ROOT, env=env, capture_output=True, text=True)
        self.assertEqual(out.returncode, 0, out.stderr)
        return [line for line in out.stdout.splitlines() if line.startswith('GOTEST')]

    def everything(self, reason, affected):
        # Everything means the shard package list: all packages except internal/workspace.
        lines = self.run_step(reason, affected)
        self.assertEqual(len(lines), 1)
        self.assertNotIn(' ./...', lines[0])
        self.assertGreater(len(lines[0].split()), 5)
        return lines[0]

    @unittest.skipUnless(shutil.which('go'), 'needs go')
    def test_an_empty_or_unknown_reason_tests_everything(self):
        for reason in ('', 'all', 'bogus'):
            self.everything(reason, 'm/internal/a')

    @unittest.skipUnless(shutil.which('go'), 'needs go')
    def test_packages_with_an_empty_list_tests_everything(self):
        self.everything('packages', '')

    def test_docs_only_runs_no_go_test(self):
        self.assertEqual(self.run_step('docs-only', ''), [])

    def test_packages_run_exactly_the_affected_list(self):
        self.assertEqual(self.run_step('packages', 'm/a m/b'), ['GOTEST test -timeout 40m m/a m/b'])

    @unittest.skipUnless(shutil.which('go'), 'needs go')
    def test_the_workspace_package_is_always_left_to_the_shards(self):
        module = subprocess.run(['go', 'list', '-m'], cwd=ROOT, capture_output=True, text=True).stdout.strip()
        ws, other = module + '/internal/workspace', module + '/internal/cli'
        self.assertEqual(self.run_step('packages', ws + ' ' + other), ['GOTEST test -timeout 40m ' + other])
        self.assertEqual(self.run_step('packages', ws), [])
        everything = self.run_step('all', '')[0]
        self.assertNotIn(ws + ' ', everything + ' ')
        self.assertIn(other, everything)


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
    def test_ci_conformance_runs_on_every_event_including_stacked_pull_requests(self):
        text = job_text('conformance')
        self.assertNotRegex(text, r'(?m)^    if:')
        self.assertIn('uses: ./.github/workflows/conformance.yml', text)
        self.assertIn(self.LANES, text)
        self.assertNotIn('lane: [minimum, current]', text)
        self.assertIn('name: Git/jj conformance (${{ matrix.lane }})', text)

    LANES = ("lane: ${{ fromJSON(github.event_name == 'pull_request' "
             "&& !contains(github.event.pull_request.labels.*.name, 'ci-full') "
             "&& '[\"current\"]' || '[\"minimum\",\"current\"]') }}")

    def test_pull_requests_run_only_the_current_lane_unless_labelled_ci_full(self):
        # The minimum lane gates main after merge, labelled PRs and the race schedule.
        import json
        match = re.search(r"lane: \$\{\{ fromJSON\((.+)\) \}\}", job_text('conformance'))
        self.assertTrue(match)
        python = match.group(1).replace('github.event_name', 'event')
        python = python.replace("!contains(github.event.pull_request.labels.*.name, 'ci-full')",
                                "('ci-full' not in labels)")
        python = python.replace('&&', ' and ').replace('||', ' or ')
        lanes = lambda event, labels=(): json.loads(
            eval(python, {}, {'event': event, 'labels': list(labels)}))
        self.assertEqual(lanes('pull_request'), ['current'])
        self.assertEqual(lanes('pull_request', ['other']), ['current'])
        self.assertEqual(lanes('pull_request', ['ci-full']), ['minimum', 'current'])
        self.assertEqual(lanes('push'), ['minimum', 'current'])

    def test_check_job_always_runs(self):
        self.assertNotRegex(job_text('check'), r'(?m)^    if: (?!\$\{\{ !cancelled\(\) \}\}$)')

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
        # Shard 0 runs the other packages, shards 1..count the workspace package, and the
        # last shard the real-CLI smoke scripts.
        self.assertEqual([int(x) for x in matrix.split(',')], list(range(count + 2)))
        self.assertIn('--shards 4', job_text('check'))

    def test_aggregate_is_per_lane_stable_and_runs_after_failures(self):
        text = job_text('suite', CONFORMANCE)
        self.assertIn('name: suite', text)
        self.assertIn('needs: [shard]', text)
        self.assertIn('if: always()', text)
        self.assertIn('SHARDS_RESULT: ${{ needs.shard.result }}', text)
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
        self.assertIn('matrix.shard != 0 && matrix.shard != 5', text)
        self.assertIn('matrix.shard == 0', text)
        self.assertIn('if: matrix.shard == 5', text)

    def test_check_verifies_shard_coverage_before_the_test_suite(self):
        text = job_text('check')
        self.assertLess(text.index('scripts/ci_shards.py check'), text.index('go test -timeout 40m'))


class LongPoleTests(unittest.TestCase):
    def test_no_workflow_level_tier_switch_is_left(self):
        self.assertNotIn('CONFORMANCE_TIER', WORKFLOW)
        self.assertNotRegex(WORKFLOW, r'github\.event\.pull_request\.base\.ref')

    def test_check_always_leaves_the_workspace_package_to_the_shards(self):
        text = job_text('check')
        self.assertNotIn('CONFORMANCE_TIER', text)
        self.assertIn('ci_shards.py packages)', text)
        self.assertIn('ci_shards.py packages --within', text)
        self.assertLess(text.index('ci_shards.py packages)'), text.index('go test -timeout 40m'))


class DocsOnlyClassifierTests(unittest.TestCase):
    def test_documentation_and_spec_prose_is_docs_only(self):
        for path in ('specs/032-x/spec.md', 'specs/032-x/checklists/requirements.md', 'docs/development.md',
                     'docs/roadmap.md', 'README.md', 'AGENTS.md'):
            self.assertTrue(CHANGES.is_docs_path(path), path)
        self.assertTrue(CHANGES.docs_only(['docs/a.md', 'specs/1/plan.md', 'README.md']))

    def test_anything_the_code_or_ci_reads_is_not_docs_only(self):
        for path in ('.github/workflows/ci.yml', '.github/CODEOWNERS', 'scripts/ci_changes.py',
                     'scripts/ci-test-weights.json', 'internal/x/x.go', 'cmd/stw/main.go', 'schemas/v2/a.json',
                     'go.mod', 'go.sum', 'Makefile', 'specs/026-release-process/examples/go-cli.record.json',
                     'specs/030-pr-status/contracts/once/once-open.json',
                     'specs/031-tidy-integration-proofs/contracts/integration-proofs.md',
                     'specs/026-release-process/examples/notes.md', 'docs/feature-status.json', 'docs/index.html',
                     'docs/assets/a.png', 'docs/sub/dir.md', 'site/hugo.toml', 'third_party/x.md',
                     'llms.txt', 'specs/x/data.json', '.specify/memory/constitution.md', 'docs/../scripts/a.md'):
            self.assertFalse(CHANGES.is_docs_path(path), path)

    def test_one_non_docs_file_makes_the_change_set_not_docs_only(self):
        self.assertFalse(CHANGES.docs_only(['docs/a.md', 'scripts/check-docs.py']))
        self.assertFalse(CHANGES.docs_only(['docs/a.md', 'go.mod']))

    def test_an_empty_change_set_is_not_docs_only(self):
        self.assertFalse(CHANGES.docs_only([]))


class DocsOnlyGitTests(unittest.TestCase):
    def git(self, cwd, *args):
        env = dict(os.environ, GIT_AUTHOR_NAME='t', GIT_AUTHOR_EMAIL='t@t', GIT_COMMITTER_NAME='t',
                   GIT_COMMITTER_EMAIL='t@t', GIT_CONFIG_GLOBAL='/dev/null', GIT_CONFIG_SYSTEM='/dev/null')
        subprocess.run(['git', '-c', 'commit.gpgsign=false', *args], cwd=cwd, env=env, check=True,
                       capture_output=True)

    def merge_repo(self, tmp, files):
        self.git(tmp, 'init', '-q', '-b', 'main')
        Path(tmp, 'a.go').write_text('package a\n')
        self.git(tmp, 'add', '.')
        self.git(tmp, 'commit', '-q', '-m', 'base')
        self.git(tmp, 'checkout', '-q', '-b', 'topic')
        for name in files:
            target = Path(tmp, name)
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text('x\n')
        self.git(tmp, 'add', '.')
        self.git(tmp, 'commit', '-q', '-m', 'topic')
        self.git(tmp, 'checkout', '-q', 'main')
        Path(tmp, 'b.go').write_text('package b\n')  # base moved on: not part of the PR
        self.git(tmp, 'add', '.')
        self.git(tmp, 'commit', '-q', '-m', 'main moved')
        self.git(tmp, 'merge', '-q', '--no-ff', '-m', 'merge', 'topic')

    def result(self, files, event='pull_request'):
        with tempfile.TemporaryDirectory() as tmp:
            self.merge_repo(tmp, files)
            return CHANGES.compute(event, cwd=tmp)

    def test_docs_only_pull_request_ignores_what_the_base_gained_meanwhile(self):
        self.assertTrue(self.result(['docs/a.md', 'specs/1/spec.md']))

    def test_mixed_pull_request_is_not_docs_only(self):
        self.assertFalse(self.result(['docs/a.md', 'scripts/x.py']))

    def test_moving_a_source_file_into_docs_is_not_docs_only(self):
        with tempfile.TemporaryDirectory() as tmp:
            self.git(tmp, 'init', '-q', '-b', 'main')
            Path(tmp, 'scripts').mkdir()
            Path(tmp, 'docs').mkdir()
            Path(tmp, 'scripts/x.md').write_text('# x\n' * 50)
            self.git(tmp, 'add', '.')
            self.git(tmp, 'commit', '-q', '-m', 'base')
            self.git(tmp, 'checkout', '-q', '-b', 'topic')
            self.git(tmp, 'mv', 'scripts/x.md', 'docs/x.md')
            self.git(tmp, 'commit', '-q', '-m', 'move')
            self.git(tmp, 'checkout', '-q', 'main')
            Path(tmp, 'other').write_text('x\n')
            self.git(tmp, 'add', '.')
            self.git(tmp, 'commit', '-q', '-m', 'main moved')
            self.git(tmp, 'merge', '-q', '--no-ff', '-m', 'merge', 'topic')
            self.assertFalse(CHANGES.compute('pull_request', cwd=tmp))

    def test_only_pull_request_events_may_be_docs_only(self):
        for event in ('push', 'workflow_dispatch', 'schedule', 'release', 'create'):
            self.assertFalse(self.result(['docs/a.md'], event), event)

    def test_a_checkout_without_a_merge_commit_fails_closed(self):
        with tempfile.TemporaryDirectory() as tmp:
            self.git(tmp, 'init', '-q', '-b', 'main')
            Path(tmp, 'docs').mkdir()
            Path(tmp, 'docs/a.md').write_text('x\n')
            self.git(tmp, 'add', '.')
            self.git(tmp, 'commit', '-q', '-m', 'only')
            self.assertFalse(CHANGES.compute('pull_request', cwd=tmp))
            Path(tmp, 'docs/b.md').write_text('y\n')
            self.git(tmp, 'add', '.')
            self.git(tmp, 'commit', '-q', '-m', 'second, linear')
            self.assertFalse(CHANGES.compute('pull_request', cwd=tmp))
        self.assertFalse(CHANGES.compute('pull_request', cwd='/nonexistent-dir-for-ci-changes'))


class DocsOnlyWorkflowTests(unittest.TestCase):
    def test_no_workflow_uses_path_filters(self):
        for text in ALL:
            self.assertNotIn('paths-ignore', text)
            self.assertNotRegex(text, r'(?m)^\s+paths:')

    def test_changes_job_classifies_with_the_script_and_exposes_docs_only(self):
        text = job_text('changes')
        self.assertNotRegex(text, r'(?m)^    if:')
        self.assertIn('fetch-depth: 2', text)
        self.assertIn('docs_only: ${{ steps.classify.outputs.docs_only }}', text)
        self.assertIn('EVENT_NAME: ${{ github.event_name }}', text)
        self.assertIn('python3 -B scripts/ci_changes.py', text)
        self.assertIn('GITHUB_OUTPUT', text)

    def test_conformance_runs_after_changes_and_receives_docs_only(self):
        text = job_text('conformance')
        self.assertIn('needs: [changes]', text)
        self.assertIn('docs_only: ${{ needs.changes.outputs.docs_only }}', text)
        self.assertNotRegex(text, r'(?m)^    if:')

    def test_check_job_never_depends_on_docs_only_and_still_runs_every_static_check(self):
        text = job_text('check')
        self.assertNotIn('docs_only', text)
        self.assertIn('make check-static', text)
        self.assertIn('scripts/check-architecture.py', text)
        self.assertIn('scripts/ci_shards.py check', text)
        for step in ('make check-static', 'go mod tidy', 'scripts/ci_shards.py check', 'scripts/check-architecture.py',
                     'scripts/package-pages.py'):
            lines = [line for line in text.splitlines() if step in line]
            self.assertTrue(lines, step)

    def test_check_waits_for_changes_but_runs_even_when_it_failed(self):
        text = job_text('check')
        self.assertIn('needs: [changes]', text)
        self.assertIn('if: ${{ !cancelled() }}', text)

    def test_changes_job_publishes_affected_and_the_reason(self):
        text = job_text('changes')
        self.assertIn('affected: ${{ steps.affected.outputs.affected }}', text)
        self.assertIn('affected_reason: ${{ steps.affected.outputs.affected_reason }}', text)
        self.assertIn('python3 -B scripts/ci_changes.py affected >> "$GITHUB_OUTPUT"', text)
        self.assertIn('actions/setup-go@', text)

    def test_the_changes_job_does_not_save_a_go_cache(self):
        text = job_text('changes')
        block = text.split('actions/setup-go@', 1)[1].split('\n      - ', 1)[0]
        self.assertIn('cache: false', block)
        self.assertNotIn('cache: true', block)
        self.assertNotIn('cache-dependency-path', block)

    def test_go_tests_honour_the_affected_set_and_fail_closed(self):
        text = job_text('check')
        step = text.split('- name: Go tests\n', 1)[1].split('\n      - name:', 1)[0]
        self.assertIn('AFFECTED: ${{ needs.changes.outputs.affected }}', step)
        self.assertIn('AFFECTED_REASON: ${{ needs.changes.outputs.affected_reason }}', step)
        self.assertIn('[ "$AFFECTED_REASON" = docs-only ]', step)
        self.assertIn('[ "$AFFECTED_REASON" = packages ] && [ -n "$AFFECTED" ]', step)
        self.assertIn("packages='./...'", step)
        self.assertIn('ci_shards.py packages --within', step)
        self.assertEqual(len(re.findall(r'go test\b', step)), 1)

    def test_sandbox_install_is_skipped_only_for_documentation_changes(self):
        text = job_text('check')
        for name in ('Install distro sandbox without relaxing host protections', 'Require sandbox capability before source checks',
                     'Read-only sandbox failure diagnostics'):
            step = text.split('- name: ' + name + '\n', 1)[1].split('\n      - ', 1)[0]
            self.assertIn("needs.changes.outputs.affected_reason != 'docs-only'", step, name)

    def test_ci_full_label_runs_never_skip(self):
        self.assertNotIn('docs_only', FULL)

    def test_shards_skip_only_for_a_docs_only_pull_request_or_one_below_the_stack_tip(self):
        text = job_text('shard', CONFORMANCE)
        self.assertIn("if: github.event_name != 'pull_request' || (inputs.docs_only != 'true' && inputs.stack_tip != 'false')\n", text)
        self.assertRegex(CONFORMANCE, r"docs_only:\n(?:\s+[a-z]+: .*\n)*?\s+default: 'false'")
        self.assertRegex(CONFORMANCE, r"stack_tip:\n(?:\s+[a-z]+: .*\n)*?\s+default: 'true'")

    def test_every_step_of_the_shard_job_stays_unconditional_on_docs_only_and_stack_tip(self):
        text = job_text('shard', CONFORMANCE)
        self.assertEqual(text.count('docs_only'), 1)
        self.assertEqual(len(re.findall(r'inputs\.docs_only', text)), 1)
        self.assertEqual(len(re.findall(r'inputs\.stack_tip', text)), 1)

    def test_the_shard_condition_for_every_case(self):
        text = job_text('shard', CONFORMANCE)
        expr = re.search(r'(?m)^    if: (.*)$', text).group(1)
        python = (expr.replace('github.event_name', 'event').replace('inputs.docs_only', 'docs_only')
                  .replace('inputs.stack_tip', 'stack_tip').replace('&&', ' and ').replace('||', ' or '))
        runs = lambda event, docs_only='false', stack_tip='true': eval(
            python, {}, {'event': event, 'docs_only': docs_only, 'stack_tip': stack_tip})
        self.assertTrue(runs('push'))
        self.assertTrue(runs('push', 'true', 'false'))  # only a pull request may skip
        self.assertTrue(runs('pull_request'))
        self.assertTrue(runs('pull_request', 'false', 'true'))
        self.assertFalse(runs('pull_request', 'true', 'true'))
        self.assertFalse(runs('pull_request', 'false', 'false'))
        self.assertFalse(runs('pull_request', 'true', 'false'))
        self.assertTrue(runs('pull_request', 'false', ''))  # anything but an explicit false runs
        self.assertTrue(runs('workflow_dispatch', 'true', 'false'))


class StackTipWorkflowTests(unittest.TestCase):
    def test_changes_job_has_exactly_the_extra_read_permission(self):
        text = job_text('changes')
        self.assertRegex(text, r'(?m)^    permissions:\n      contents: read\n      pull-requests: read\n(?!      )')
        for name in ('check', 'conformance', 'pages'):
            self.assertNotIn('pull-requests', job_text(name))
        head = WORKFLOW.split('\njobs:', 1)[0]
        self.assertNotIn('pull-requests', head)

    def test_changes_job_exposes_the_decision_and_passes_data_by_environment(self):
        text = job_text('changes')
        self.assertIn('is_stack_tip: ${{ steps.stack.outputs.is_stack_tip }}', text)
        step = text.split('- name: Detect the tip of a stack\n', 1)[1]
        env, run = step.split('        run: |\n', 1)
        for name in ('HEAD_REF: ${{ github.event.pull_request.head.ref }}', 'HEAD_REPO:', 'BASE_REPO:', 'PR_NUMBER:',
                     'DEFAULT_BRANCH:', 'EVENT_NAME: ${{ github.event_name }}'):
            self.assertIn(name, env)
        self.assertNotIn('${{', run, 'no expression, so no untrusted text, inside the shell script')
        self.assertIn('python3 -B scripts/ci_stack.py --prs-file "$prs" >> "$GITHUB_OUTPUT"', run)
        self.assertIn('|| rm -f "$prs"', run)  # a failed lookup leaves no listing: the script treats it as a tip

    def test_no_untrusted_expression_is_interpolated_into_any_run_line(self):
        for text in ALL:
            for run in re.findall(r'(?m)^\s+run: (.*)$', text):
                self.assertNotRegex(run, r'github\.event\.pull_request\.(head|title|body)|github\.head_ref')

    def test_conformance_receives_the_decision_and_the_ci_full_label_forces_the_shards(self):
        text = job_text('conformance')
        match = re.search(r'stack_tip: \$\{\{ (.+) \}\}', text)
        self.assertTrue(match)
        python = match.group(1).replace("needs.changes.outputs.is_stack_tip", 'out')
        python = python.replace("contains(github.event.pull_request.labels.*.name, 'ci-full')", "('ci-full' in labels)")
        python = python.replace('||', ' or ').replace('&&', ' and ')
        value = lambda out, labels=(): eval(python, {}, {'out': out, 'labels': list(labels)})
        self.assertEqual(value('false'), 'false')
        self.assertEqual(value('true'), 'true')
        self.assertEqual(value(''), 'true')  # an empty output of a succeeding job: fail closed
        self.assertEqual(value('false', ['ci-full']), 'true')
        self.assertEqual(value('false', ['other']), 'false')
        self.assertNotRegex(text, r'(?m)^    if:')


class SuiteAggregateTests(unittest.TestCase):
    @staticmethod
    def script():
        text = job_text('suite', CONFORMANCE)
        return re.search(r'run: \|\n((?:          .*\n?)+)', text).group(1)

    def passes(self, result, docs_only, event='pull_request', stack_tip='true'):
        env = dict(os.environ, SHARDS_RESULT=result, DOCS_ONLY=docs_only, EVENT_NAME=event, STACK_TIP=stack_tip)
        done = subprocess.run(['bash', '-e', '-c', self.script()], env=env, capture_output=True, text=True)
        return done.returncode == 0

    def test_suite_wiring(self):
        text = job_text('suite', CONFORMANCE)
        self.assertIn('if: always()', text)
        self.assertIn('needs: [shard]', text)
        self.assertIn('SHARDS_RESULT: ${{ needs.shard.result }}', text)
        self.assertIn('DOCS_ONLY: ${{ inputs.docs_only }}', text)
        self.assertIn('STACK_TIP: ${{ inputs.stack_tip }}', text)
        self.assertIn('EVENT_NAME: ${{ github.event_name }}', text)

    def test_success_passes_everywhere(self):
        for docs_only in ('true', 'false'):
            for event in ('pull_request', 'push', 'workflow_dispatch'):
                self.assertTrue(self.passes('success', docs_only, event), (docs_only, event))

    def test_failed_or_cancelled_shards_fail_even_for_docs_only(self):
        for result in ('failure', 'cancelled', '', 'bogus'):
            for docs_only in ('true', 'false'):
                self.assertFalse(self.passes(result, docs_only), (result, docs_only))

    def test_skipped_shards_pass_only_for_a_docs_only_pull_request_or_one_below_the_stack_tip(self):
        self.assertTrue(self.passes('skipped', 'true', 'pull_request'))
        self.assertTrue(self.passes('skipped', 'false', 'pull_request', 'false'))
        self.assertTrue(self.passes('skipped', 'true', 'pull_request', 'false'))
        self.assertFalse(self.passes('skipped', 'false', 'pull_request'))
        self.assertFalse(self.passes('skipped', '', 'pull_request'))
        self.assertFalse(self.passes('skipped', 'false', 'pull_request', ''))
        self.assertFalse(self.passes('skipped', 'false', 'pull_request', 'bogus'))
        for event in ('push', 'workflow_dispatch', 'schedule', 'release', 'create'):
            for stack_tip in ('true', 'false'):
                self.assertFalse(self.passes('skipped', 'true', event, stack_tip), event)
                self.assertFalse(self.passes('skipped', 'false', event, stack_tip), event)

    def test_failed_or_cancelled_shards_fail_below_the_stack_tip_too(self):
        for result in ('failure', 'cancelled', ''):
            self.assertFalse(self.passes(result, 'false', 'pull_request', 'false'), result)


class RaceWorkflowTests(unittest.TestCase):
    """race.yml (Spec 039 T4): scheduled race runs and the only place with issues: write."""

    def head(self):
        return RACE.split('\non:\n', 1)[1].split('\npermissions:', 1)[0]

    def test_triggers_are_schedule_and_dispatch_only(self):
        on = self.head()
        self.assertRegex(on, r'(?m)^  schedule:\n    - cron: ')
        self.assertRegex(on, r'(?m)^  workflow_dispatch:')
        self.assertEqual(len(re.findall(r'(?m)^    - cron: ', on)), 2, 'nightly and weekly')
        for forbidden in ('pull_request', 'pull_request_target', 'push:', 'workflow_run', 'issues:', 'issue_comment'):
            self.assertNotIn(forbidden, on)

    def test_workflow_permissions_are_read_only(self):
        top = RACE.split('\njobs:', 1)[0]
        self.assertRegex(top, r'(?m)^permissions:\n  contents: read\n(?!  )')

    def test_both_jobs_are_guarded_to_main(self):
        for job in ('race', 'report'):
            self.assertRegex(job_text(job, RACE), r"(?m)^    if: .*github\.ref == 'refs/heads/main'")

    def test_race_job_runs_the_conformance_shards_with_race_true(self):
        race = job_text('race', RACE)
        self.assertIn('uses: ./.github/workflows/conformance.yml', race)
        self.assertIn('race: true', race)
        self.assertIn('lane: ${{ matrix.lane }}', race)
        self.assertNotIn('docs_only', race)

    def test_lane_is_current_nightly_and_minimum_weekly(self):
        self.assertIn("github.event.schedule == '17 4 * * 0'", job_text('race', RACE))
        self.assertIn("&& 'minimum' || 'current'", job_text('race', RACE))
        self.assertIn("&& 'weekly' || 'nightly'", RACE.split('\nconcurrency:', 1)[1].split('\njobs:', 1)[0])

    def test_race_runs_are_never_cancelled_and_have_their_own_groups(self):
        conc = RACE.split('\nconcurrency:', 1)[1].split('\njobs:', 1)[0]
        self.assertIn('t4-', conc)
        self.assertIn('cancel-in-progress: false', conc)

    def test_report_runs_after_race_on_failure_or_cancel(self):
        report = job_text('report', RACE)
        self.assertIn('needs: [race]', report)
        self.assertIn("if: github.ref == 'refs/heads/main' && (failure() || cancelled())", report)
        self.assertNotIn('needs: [race]', job_text('race', RACE))

    def test_report_jobs_of_both_schedules_share_one_non_cancelling_group(self):
        report = job_text('report', RACE)
        self.assertRegex(report, r'(?m)^    concurrency:\n      group: race-report\n      cancel-in-progress: false\n')
        self.assertNotIn('github.event', report.split('concurrency:', 1)[1].split('permissions:', 1)[0])

    def test_report_permissions_exactly(self):
        report = job_text('report', RACE)
        self.assertRegex(report, r'(?m)^    permissions:\n      contents: read\n      actions: read\n      issues: write\n(?!      )')

    def test_issues_write_exists_only_in_the_report_job(self):
        for name, text in (('ci.yml', WORKFLOW), ('conformance.yml', CONFORMANCE), ('ci-full.yml', FULL)):
            self.assertNotRegex(text, r'issues:\s*write', name)
            self.assertNotRegex(text, r'write-all', name)
        self.assertEqual(len(re.findall(r'(?m)^[^#\n]*issues: write', RACE)), 1)
        self.assertNotIn('issues: write', job_text('race', RACE))
        self.assertNotRegex(RACE, r'write-all')
        for other in sorted((ROOT / '.github/workflows').glob('*.yml')):
            if other.name != 'race.yml':
                self.assertNotRegex(other.read_text(), r'issues:\s*write', other.name)

    def test_report_checks_out_the_default_branch_only(self):
        report = job_text('report', RACE)
        self.assertRegex(report, r'actions/checkout@[0-9a-f]{40} # v\d+\.\d+\.\d+\n        with:\n          ref: main\n          persist-credentials: false\n')
        self.assertEqual(report.count('actions/checkout@'), 1)
        self.assertNotIn('github.head_ref', report)
        self.assertNotIn('github.event.pull_request', RACE)

    def test_report_runs_the_reporter_with_token_from_env(self):
        report = job_text('report', RACE)
        self.assertIn('python3 -B scripts/race_report.py', report)
        self.assertIn('GH_TOKEN: ${{ github.token }}', report)
        self.assertNotIn('${{ github.token }}', report.split('run: |', 1)[1])  # never interpolated into the script
        self.assertNotIn('toJSON(', RACE)

    def test_the_tier_job_is_not_part_of_race_yml(self):
        self.assertNotRegex(RACE, r'(?m)^  tier:')
        self.assertNotIn('ci_tier.py', RACE)
        self.assertNotIn('docs_only', RACE)

    def test_the_reporter_is_a_script_with_unit_tests(self):
        self.assertTrue((ROOT / 'scripts/race_report.py').is_file())
        self.assertTrue((ROOT / 'scripts/test_race_report.py').is_file())


if __name__ == '__main__':
    unittest.main()
