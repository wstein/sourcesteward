# SPDX-License-Identifier: EUPL-1.2
"""Offline tests for scripts/ci-wait.sh using a fake gh and a fake clock."""
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / 'scripts/ci-wait.sh'
SHA = 'a' * 40
OTHER = 'b' * 40

FAKE_GH = r'''#!/bin/sh
# Fake gh: replays $FIX/state.json. Every call advances the fake clock by GH_COST.
set -eu
echo "$*" >> "$FIX/calls.log"
now=$(cat "$FIX/clock")
echo $((now + ${GH_COST:-0})) > "$FIX/clock"
n=$(cat "$FIX/n_$1_$2" 2>/dev/null || echo 0)
n=$((n + 1))
echo "$n" > "$FIX/n_$1_$2"
if [ -f "$FIX/fail" ]; then
  cat "$FIX/fail" >&2
  exit 1
fi
pick() { # file-prefix: use prefix.N if present, else the last one
  f="$FIX/$1.$n"
  [ -f "$f" ] || f=$(ls "$FIX/$1".* | sort -t. -k2 -n | tail -1)
  cat "$f"
}
case "$1 $2" in
  "pr view") pick head ;;
  "run list")
    lim=100
    while [ $# -gt 0 ]; do [ "$1" = --limit ] && lim=$2; shift; done
    pick runs | jq -c ".[:$lim]" ;;
  "run view") cat "$FIX/log" ;;
  *) echo "unexpected $*" >&2; exit 9 ;;
esac
'''

FAKE_SLEEP = '''#!/bin/sh
now=$(cat "$FIX/clock")
echo $((now + $1)) > "$FIX/clock"
echo "$1" >> "$FIX/sleeps.log"
'''

FAKE_NOW = '''#!/bin/sh
cat "$FIX/clock"
'''


def run(id_, wf, status='completed', conclusion='success', secs=60):
    return {'databaseId': id_, 'workflowName': wf, 'status': status, 'conclusion': conclusion,
            'startedAt': '2026-01-01T00:00:00Z',
            'updatedAt': '2026-01-01T00:%02d:%02dZ' % divmod(secs, 60)}


class Base(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.fix = Path(self.tmp.name)
        for name, body in (('gh', FAKE_GH), ('sleep', FAKE_SLEEP), ('now', FAKE_NOW)):
            p = self.fix / name
            p.write_text(body)
            p.chmod(0o755)
        (self.fix / 'clock').write_text('1000')
        self.head(SHA)
        self.log('')

    def head(self, *shas):
        for i, sha in enumerate(shas, 1):
            (self.fix / f'head.{i}').write_text(json.dumps({'headRefOid': sha}))

    def runs(self, *samples):
        for i, runs in enumerate(samples, 1):
            (self.fix / f'runs.{i}').write_text(json.dumps(runs))

    def log(self, text):
        (self.fix / 'log').write_text(text)

    def call(self, *args, env=None):
        e = dict(os.environ, FIX=str(self.fix), GH=str(self.fix / 'gh'),
                 CI_WAIT_SLEEP=str(self.fix / 'sleep'), CI_WAIT_NOW=str(self.fix / 'now'))
        e.update(env or {})
        p = subprocess.run(['sh', str(SCRIPT), *args], capture_output=True, text=True, env=e, timeout=60)
        return p.returncode, p.stdout, p.stderr

    def calls(self):
        return (self.fix / 'calls.log').read_text().splitlines()

    def elapsed(self, out):
        return int(re.search(r'elapsed=(\d+)', out).group(1))


class OutcomeTests(Base):
    def test_all_green(self):
        self.runs([run(1, 'ci', secs=125), run(2, 'pages')])
        code, out, _ = self.call('7')
        self.assertEqual(code, 0, out)
        self.assertIn('ci | success | 2m05s', out)
        self.assertTrue(out.splitlines()[-1].startswith('outcome=success head=aaaaaaa elapsed='))

    def test_skipped_is_neutral_and_listed(self):
        self.runs([run(1, 'ci'), run(2, 'conformance', conclusion='skipped')])
        code, out, _ = self.call('7')
        self.assertEqual(code, 0)
        self.assertIn('conformance | skipped |', out)

    def test_failure_lists_failed_tests(self):
        self.runs([run(1, 'ci', conclusion='failure'), run(2, 'pages')])
        self.log('x\n--- FAIL: TestAlpha (0.1s)\n--- FAIL: TestBeta/sub (0.2s)\nnoise secret=hunter2\n')
        code, out, _ = self.call('7')
        self.assertEqual(code, 1)
        self.assertIn('ci | failure |', out)
        self.assertIn('TestAlpha', out)
        self.assertIn('TestBeta/sub', out)
        self.assertNotIn('hunter2', out)
        self.assertIn('outcome=failure', out.splitlines()[-1])

    def test_failed_test_names_capped_at_ten(self):
        self.runs([run(1, 'ci', conclusion='failure')])
        self.log(''.join('--- FAIL: TestN%02d (0s)\n' % i for i in range(30)))
        _, out, _ = self.call('7')
        self.assertEqual(len(re.findall(r'TestN\d\d', out)), 10)

    def test_failed_log_read_is_bounded(self):
        self.runs([run(1, 'ci', conclusion='failure')])
        self.log('A' * 1_000_000 + '\n' + '--- FAIL: TestLate (0s)\n' + ('--- FAIL: Test' + 'Z' * 500 + '\n'))
        code, out, _ = self.call('7')
        self.assertEqual(code, 1)
        self.assertNotIn('TestLate', out)
        self.assertLess(len(out), 2000)
        self.assertNotIn('Z' * 201, out)

    def test_cancelled_timed_out_action_required_fail(self):
        for conclusion in ('cancelled', 'timed_out', 'action_required'):
            with self.subTest(conclusion):
                self.runs([run(1, 'ci', conclusion=conclusion)])
                code, out, _ = self.call('7')
                self.assertEqual(code, 1)
                self.assertIn(conclusion, out)

    def test_deadline_with_active_run(self):
        self.runs([run(1, 'ci', status='in_progress', conclusion=None)])
        code, out, _ = self.call('--timeout', '100', '--interval', '20', '7')
        self.assertEqual(code, 2)
        self.assertIn('ci | in_progress |', out)
        self.assertIn('outcome=deadline', out.splitlines()[-1])

    def test_deadline_is_not_reset_per_poll(self):
        self.runs([run(1, 'ci', status='queued', conclusion=None)])
        code, out, _ = self.call('--timeout', '100', '--interval', '20', '7', env={'GH_COST': '15'})
        self.assertEqual(code, 2)
        self.assertLessEqual(self.elapsed(out), 100 + 40)
        sleeps = [int(s) for s in (self.fix / 'sleeps.log').read_text().split()]
        self.assertTrue(all(s <= 60 for s in sleeps))

    def test_backoff_grows_to_cap(self):
        self.runs([run(1, 'ci', status='queued', conclusion=None)])
        self.call('--timeout', '1000', '--interval', '20', '7')
        sleeps = [int(s) for s in (self.fix / 'sleeps.log').read_text().split()]
        self.assertEqual(sleeps[0], 20)
        self.assertEqual(max(sleeps), 60)
        self.assertEqual(sleeps[:-1], sorted(sleeps[:-1]))

    def test_no_runs_appeared(self):
        self.runs([])
        code, out, _ = self.call('--appear-timeout', '60', '--interval', '20', '7')
        self.assertEqual(code, 3)
        self.assertIn('outcome=no-runs', out.splitlines()[-1])

    def test_phase_one_resolves_after_a_few_polls(self):
        # The motivating bug: no runs yet must not read as done.
        self.runs([], [], [], [run(1, 'ci', status='in_progress', conclusion=None)],
                  [run(1, 'ci', secs=30)])
        code, out, _ = self.call('--appear-timeout', '120', '--interval', '10', '7')
        self.assertEqual(code, 0, out)
        self.assertGreaterEqual(len(self.calls()), 5)
        self.assertIn('outcome=success', out)

    def test_head_changed_before_wait(self):
        self.head(OTHER)
        self.runs([run(1, 'ci')])
        code, out, _ = self.call('--head', SHA, '7')
        self.assertEqual(code, 4)
        self.assertIn(OTHER, out)
        self.assertIn('outcome=head-changed head=aaaaaaa', out.splitlines()[-1])

    def test_head_changed_during_wait(self):
        self.head(SHA, SHA, OTHER)
        self.runs([run(1, 'ci', status='in_progress', conclusion=None)])
        code, out, _ = self.call('--head', SHA, '--interval', '5', '7')
        self.assertEqual(code, 4)
        self.assertIn(OTHER, out)

    def test_head_changed_during_wait_without_flag(self):
        self.head(SHA, OTHER)
        self.runs([run(1, 'ci', status='in_progress', conclusion=None)])
        code, _, _ = self.call('--interval', '5', '7')
        self.assertEqual(code, 4)

    def test_gh_error_is_unavailable(self):
        (self.fix / 'fail').write_text('HTTP 401: Bad credentials')
        code, out, err = self.call('7')
        self.assertEqual(code, 5)
        self.assertIn('outcome=unavailable', out.splitlines()[-1])

    def test_rate_limit_is_unavailable(self):
        (self.fix / 'fail').write_text('API rate limit exceeded (HTTP 403)')
        code, out, _ = self.call('7')
        self.assertEqual(code, 5)
        self.assertNotIn('outcome=success', out)

    def test_garbage_evidence_is_unavailable(self):
        (self.fix / 'runs.1').write_text('not json')
        code, _, _ = self.call('7')
        self.assertEqual(code, 5)

    def test_full_page_raises_limit_then_succeeds(self):
        self.runs([run(i, 'w%d' % i) for i in range(1, 151)])
        code, out, _ = self.call('7')
        self.assertEqual(code, 0, out)
        self.assertEqual(out.count(' | success | '), 150)
        self.assertIn('--limit 200', ' '.join(self.calls()))

    def test_exactly_100_runs_is_retried_with_higher_limit(self):
        self.runs([run(i, 'w') for i in range(1, 101)])
        code, _, _ = self.call('7')
        self.assertEqual(code, 0)
        self.assertIn('--limit 200', ' '.join(self.calls()))

    def test_runs_at_hard_cap_are_incomplete_evidence(self):
        self.runs([run(i, 'w') for i in range(1, 301)])
        code, out, err = self.call('7')
        self.assertEqual(code, 5)
        self.assertIn('evidence incomplete', err)
        self.assertNotIn('outcome=success', out)

    def test_control_characters_stripped_non_ascii_kept(self):
        self.runs([run(1, 'ci\x1b[31m-\u00fcber \u2713', conclusion='failure')])
        code, out, _ = self.call('7')
        self.assertEqual(code, 1)
        self.assertNotIn('\x1b', out)
        self.assertIn('ci[31m-\u00fcber \u2713', out)

    def test_runs_vanishing_after_being_seen_is_unavailable(self):
        self.runs([run(1, 'ci', status='in_progress', conclusion=None)], [])
        code, out, _ = self.call('--interval', '5', '7')
        self.assertEqual(code, 5)
        self.assertIn('outcome=unavailable', out.splitlines()[-1])

    def test_head_changes_after_run_fetch(self):
        self.head(SHA, OTHER)
        self.runs([run(1, 'ci')])
        code, out, _ = self.call('--head', SHA, '7')
        self.assertEqual(code, 4, out)

    def test_usage_errors(self):
        for args in ([], ['--bogus', '7'], ['abc'], ['--timeout', 'x', '7'], ['--interval', '0', '7'],
                     ['--head', 'zz', '7'], ['7', '8'], ['--timeout']):
            with self.subTest(args=args):
                code, _, err = self.call(*args)
                self.assertEqual(code, 64)
                self.assertIn('usage', err.lower())

    def test_json_output(self):
        self.runs([run(1, 'ci', conclusion='failure', secs=5), run(2, 'x', conclusion='skipped')])
        self.log('--- FAIL: TestJ (0s)\n')
        code, out, _ = self.call('--json', '7')
        self.assertEqual(code, 1)
        data = json.loads(out)
        self.assertEqual(data['outcome'], 'failure')
        self.assertEqual(data['head'], SHA)
        self.assertEqual(data['runs'][0]['workflow'], 'ci')
        self.assertEqual(data['runs'][0]['failed_tests'], ['TestJ'])
        self.assertEqual(data['runs'][1]['conclusion'], 'skipped')
        self.assertIn('elapsed', data)

    def test_repo_flag_is_forwarded(self):
        self.runs([run(1, 'ci')])
        self.call('-R', 'o/r', '7')
        self.assertTrue(all('-R o/r' in c for c in self.calls()))


class ReadOnlyTests(unittest.TestCase):
    def test_script_never_mutates(self):
        text = SCRIPT.read_text()
        code = '\n'.join(l for l in text.splitlines() if not l.lstrip().startswith('#'))
        for forbidden in ('run rerun', 'run cancel', 'workflow run', 'pr ready', 'pr merge', 'pr close',
                          'api ', 'POST', 'PATCH', 'DELETE', '--method', '-X '):
            self.assertNotIn(forbidden, code, forbidden)
        for used in re.findall(r'"\$GH"\s+(\w+\s+\w+)', code):
            self.assertIn(used, ('pr view', 'run list', 'run view'))


if __name__ == '__main__':
    unittest.main()
