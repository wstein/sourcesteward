# SPDX-License-Identifier: EUPL-1.2
"""Offline tests for scripts/measure-native-stack.sh with a fake gh and a fake git. Nothing here
touches GitHub: the fakes only log their calls and answer from files."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / 'scripts/measure-native-stack.sh'
REPO = 'wstein/stw-native-sandbox'

FAKE_GH = r'''#!/bin/sh
set -eu
echo "gh $*" >> "$FIX/calls.log"
case "$1 $2" in
  "api repos/$REPO") case "$*" in *default_branch*) echo main ;; *permissions.push*) echo "${PUSH:-true}" ;; *) echo "delete_branch_on_merge=true allow_rebase_merge=true" ;; esac ;;
  "api repos/$REPO/rules/branches/main") echo "required_linear_history" ;;
  "api repos/$REPO/issues/"*) echo "head_ref_force_pushed" ;;
  "repo clone") mkdir -p "$4" ;;
  "pr create")
    n=$(cat "$FIX/n" 2>/dev/null || echo 100); n=$((n + 1)); echo "$n" > "$FIX/n"
    [ "${FAIL_PR_CREATE:-0}" != "$((n - 100))" ] || { echo "boom" >&2; exit 1; }
    head=; base=
    while [ $# -gt 0 ]; do case $1 in --head) head=$2 ;; --base) base=$2 ;; esac; shift; done
    echo "sha-$head" > "$FIX/head_$n"; echo "$base" > "$FIX/base_$n"
    echo "https://github.com/$REPO/pull/$n" ;;
  "pr edit") n=$3; shift 3; while [ $# -gt 0 ]; do case $1 in --base) echo "$2" > "$FIX/base_$n" ;; esac; shift; done ;;
  "pr view")
    n=$3
    case "$*" in
      *headRefOid,baseRefName*) head=$(cat "$FIX/head_$n"); [ -z "${MOVED:-}" ] || [ "$n" != 103 ] || head=moved; echo "$head $(cat "$FIX/base_$n")" ;;
      *) echo "{\"number\":$n,\"state\":\"OPEN\"}" ;;
    esac ;;
  *) echo "unexpected gh call: $*" >&2; exit 9 ;;
esac
'''

FAKE_GIT = r'''#!/bin/sh
set -eu
echo "git $*" >> "$FIX/calls.log"
case "$*" in
  *"rev-parse "*) for a; do :; done; echo "sha-$a" ;;
  *"ls-remote"*) echo "x" ;;
  *"push --porcelain"*) echo "${PUSH_OUT:-To github.com:$REPO}"; exit "${PUSH_RC:-0}" ;;
esac
'''


class MeasureNativeStack(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        d = Path(self.tmp.name)
        self.fix = d / 'fix'
        self.fix.mkdir()
        for name, body in (('gh', FAKE_GH), ('git', FAKE_GIT)):
            (d / name).write_text(body)
            (d / name).chmod(0o755)
        self.env = dict(os.environ, GH=str(d / 'gh'), GIT=str(d / 'git'), FIX=str(self.fix), REPO=REPO,
                        MEASURE_SLEEP='true', TMPDIR=str(d))

    def run_script(self, *args, **extra):
        env = dict(self.env, **extra)
        return subprocess.run(['sh', str(SCRIPT), *args], capture_output=True, text=True, env=env, timeout=60)

    def calls(self):
        p = self.fix / 'calls.log'
        return p.read_text().splitlines() if p.exists() else []

    def ok_args(self, *more):
        return ['--repo', REPO, '--confirm', REPO, *more]

    def test_usage_errors_exit_64_and_call_nothing(self):
        bad = [
            [],
            ['--repo', REPO],
            ['--repo', REPO, '--confirm', 'wstein/other-sandbox'],
            ['--repo', 'wstein/cubistry', '--confirm', 'wstein/cubistry'],
            ['--repo', 'wstein/sourcesteward', '--confirm', 'wstein/sourcesteward'],
            ['--repo', 'wstein/plain', '--confirm', 'wstein/plain'],
            ['--repo', 'measure-org/prod', '--confirm', 'measure-org/prod'],
            ['--repo', 'sandbox-inc/product', '--confirm', 'sandbox-inc/product'],
            ['--repo', '../x', '--confirm', '../x'],
            self.ok_args('--wait', 'x'),
            self.ok_args('--wait', '901'),
            self.ok_args('--nope'),
        ]
        for args in bad:
            r = self.run_script(*args)
            self.assertEqual(r.returncode, 64, (args, r.stderr))
        self.assertEqual(self.calls(), [])

    def test_deny_list_ignores_case(self):
        for name in ('WStein/Cubistry', 'wstein/SourceSteward', 'WSTEIN/Anchorage-Workspace'):
            r = self.run_script('--repo', name, '--confirm', name)
            self.assertEqual(r.returncode, 64, name)
            self.assertIn('real repository', r.stderr, name)

    def test_the_name_pattern_is_case_insensitive_on_the_name(self):
        r = self.run_script(*self.ok_args('--dry-run'))
        self.assertEqual(r.returncode, 0, r.stderr)
        r = self.run_script('--repo', 'wstein/STW-Native-SANDBOX', '--confirm', 'wstein/STW-Native-SANDBOX', '--dry-run')
        self.assertEqual(r.returncode, 0, r.stderr)

    def test_dry_run_never_sleeps(self):
        log = Path(self.tmp.name) / 'sleep.log'
        fake = Path(self.tmp.name) / 'fakesleep'
        fake.write_text('#!/bin/sh\necho "$*" >> %s\n' % log)
        fake.chmod(0o755)
        r = self.run_script(*self.ok_args('--dry-run', '--deletion-test', '--wait', '600'), MEASURE_SLEEP=str(fake))
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertFalse(log.exists(), 'a dry run must not sleep')

    def test_a_midway_failure_lists_what_it_left_behind(self):
        r = self.run_script(*self.ok_args(), FAIL_PR_CREATE='2')
        self.assertNotEqual(r.returncode, 0)
        self.assertIn('left behind', r.stderr)
        self.assertRegex(r.stderr, r'branch: stw-measure-\d+-a')
        self.assertRegex(r.stderr, r'branch: stw-measure-\d+-b')
        self.assertIn('pull request: #101', r.stderr)
        self.assertNotIn('pull request: #102', r.stderr)

    def test_dry_run_calls_nothing_and_prints_the_writes_in_order(self):
        r = self.run_script(*self.ok_args('--dry-run', '--deletion-test'))
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(self.calls(), [])
        said = [l for l in r.stderr.splitlines() if l.startswith('+ ')]
        edits = [i for i, l in enumerate(said) if ' pr edit ' in l]
        pushes = [i for i, l in enumerate(said) if 'push --porcelain origin' in l and 'refs/heads/main' in l]
        self.assertEqual(len(edits), 2)
        self.assertTrue(pushes and pushes[0] > edits[1], 'the push must follow both retargets')

    def test_full_run_retargets_bottom_first_then_pushes_once(self):
        r = self.run_script(*self.ok_args('--wait', '30'))
        self.assertEqual(r.returncode, 0, r.stderr)
        calls = self.calls()
        edits = [c for c in calls if c.startswith('gh pr edit')]
        self.assertEqual([c.split()[3] for c in edits], ['102', '103'])
        self.assertTrue(all(c.endswith('--base main') for c in edits), edits)
        pushes = [i for i, c in enumerate(calls) if c.startswith('git') and 'push --porcelain' in c]
        self.assertEqual(len(pushes), 1)
        last_edit = max(i for i, c in enumerate(calls) if c.startswith('gh pr edit'))
        self.assertGreater(pushes[0], last_edit)
        self.assertIn('refs/heads/main', calls[pushes[0]])
        self.assertIn('## Native stack measurement', r.stdout)
        self.assertIn('push exit: 0', r.stdout)

    def test_a_moved_head_after_the_retarget_refuses_the_push(self):
        r = self.run_script(*self.ok_args(), MOVED='1')
        self.assertEqual(r.returncode, 1)
        self.assertFalse([c for c in self.calls() if 'push --porcelain' in c])

    def test_a_refused_push_is_recorded_verbatim_without_polling(self):
        r = self.run_script(*self.ok_args(), PUSH_RC='1', PUSH_OUT='remote: error: GH013 refused')
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn('push exit: 1', r.stdout)
        self.assertIn('GH013 refused', r.stdout)
        self.assertNotIn('### after', r.stdout)

    def test_no_push_access_stops_before_any_write(self):
        r = self.run_script(*self.ok_args(), PUSH='false')
        self.assertEqual(r.returncode, 1)
        self.assertFalse([c for c in self.calls() if c.startswith('gh pr') or ' push' in c])


if __name__ == '__main__':
    unittest.main()
