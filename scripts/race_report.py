#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Report a failed or cancelled scheduled race run (Spec 039 T4) as the one open `race-failure` issue.

Runs in race.yml's `report` job from the default branch only. The job logs are untrusted: the only text
taken from them is a test name matched by `^--- FAIL: (Test\\w+)`, at most 20 names, never any other line.
If an open issue with the label exists the report is a comment on it, otherwise it creates the issue,
so at most one `race-failure` issue is open.
"""
import argparse
import os
import re
import subprocess
import sys

LABEL = 'race-failure'
LABEL_COLOR = 'B60205'


class GhError(Exception):
    """A gh call failed; `detail` is the stderr of gh, used only to tell a 404 from other failures."""

    def __init__(self, message, detail=''):
        super().__init__(message)
        self.detail = detail
MAX_NAMES = 20
# A timed-out job normally reaches `needs.race.result` as failure, so it is not told apart from a failure.
REASONS = {'failed': 'failed or timed out (job result: failure); a timed-out job also ends as failure and its log may be missing',
           'cancelled': 'cancelled (job result: cancelled); the log may be missing'}
_LABEL = re.compile(r'[A-Za-z0-9][A-Za-z0-9._-]{0,49}')
TIERS = ('nightly', 'weekly')
LANES = ('minimum', 'current')
_PREFIX = re.compile(r'^\d{4}-\d\d-\d\dT[\d:.]+Z ')  # the timestamp GitHub puts in front of each log line
_FAIL = re.compile(r'^--- FAIL: (Test\w+)')


def extract_tests(text):
    """Return the distinct top-level failing test names in order of appearance, bounded to MAX_NAMES."""
    names = []
    for line in text.splitlines():
        match = _FAIL.match(_PREFIX.sub('', line))
        if match and match.group(1) not in names:
            names.append(match.group(1))
            if len(names) == MAX_NAMES:
                break
    return names


def build_body(run_url, tier, lane, reason, tests):
    if reason not in REASONS:
        raise ValueError('reason must be one of %s' % ', '.join(REASONS))
    if tier not in TIERS or lane not in LANES:
        raise ValueError('unknown tier or lane')
    lines = ['Scheduled race run (T4) did not pass.', '',
             '- Run: %s' % run_url, '- Tier: %s' % tier, '- Lane: %s' % lane, '- Reason: %s' % REASONS[reason], '']
    if tests:
        lines += ['Failing tests (matched from the job logs, at most %d):' % MAX_NAMES, ''] + ['- `%s`' % t for t in tests]
    else:
        lines.append('No test name matched, see the run.')
    return '\n'.join(lines) + '\n'


def _label_exists(gh, repo, label):
    """True if the label exists, False only for a 404; any other failure raises GhError."""
    try:
        gh(['api', 'repos/%s/labels/%s' % (repo, label)])
    except GhError as err:
        if 'HTTP 404' in err.detail:
            return False
        raise
    return True


def report(gh, repo, run_id, run_url, tier, lane, reason, label=LABEL):
    """Create the issue or comment on the open one; `gh(args, stdin=None)` returns stdout."""
    if not re.fullmatch(r'\d+', run_id):
        raise ValueError('run id must be numeric')
    if not _LABEL.fullmatch(label):
        raise ValueError('label must be 1-50 characters of letters, digits, dot, underscore or hyphen')
    tests = []
    try:  # best effort: logs may be expired, not ready or missing (a timed-out job); the alarm is still raised
        jobs = gh(['api', '--paginate', 'repos/%s/actions/runs/%s/jobs' % (repo, run_id), '--jq',
                   '.jobs[] | select(.conclusion == "failure") | .id']).split()
    except GhError:
        jobs = []
    for job in jobs:
        if not re.fullmatch(r'\d+', job):
            continue
        try:
            log = gh(['api', 'repos/%s/actions/jobs/%s/logs' % (repo, job)])
        except GhError:
            continue
        for name in extract_tests(log):
            if name not in tests:
                tests.append(name)
    tests = tests[:MAX_NAMES]
    body = build_body(run_url, tier, lane, reason, tests)
    number = gh(['issue', 'list', '--repo', repo, '--label', label, '--state', 'open', '--limit', '1',
                 '--json', 'number', '--jq', '.[0].number // empty']).strip()
    if number:
        gh(['issue', 'comment', number, '--repo', repo, '--body-file', '-'], body)
        return 'comment', number
    if not _label_exists(gh, repo, label):  # an existing label (colour, description) is left untouched
        gh(['label', 'create', label, '--repo', repo, '--color', LABEL_COLOR, '--description', 'Scheduled race run failed'])
    gh(['issue', 'create', '--repo', repo, '--label', label, '--title', 'Scheduled race run failed (%s, %s lane)' % (tier, lane),
        '--body-file', '-'], body)
    return 'create', ''


def _gh(args, stdin=None):
    done = subprocess.run(['gh'] + args, input=None if stdin is None else stdin.encode(), capture_output=True, check=False)
    if done.returncode:
        detail = done.stderr.decode('utf-8', errors='replace').strip()[:200]
        sys.stderr.write('gh %s failed (%d)%s\n' % (' '.join(args[:2]), done.returncode, ': ' + detail if detail else ''))
        raise GhError(' '.join(args[:2]), detail)
    return done.stdout.decode('utf-8', errors='replace')  # a log may hold invalid UTF-8


def _label_arg(value):
    if not _LABEL.fullmatch(value):
        raise argparse.ArgumentTypeError('label must be 1-50 characters of letters, digits, dot, underscore or hyphen')
    return value


def main(argv=None):
    p = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    p.add_argument('--repo', required=True)
    p.add_argument('--run-id', required=True)
    p.add_argument('--run-url', required=True)
    p.add_argument('--tier', required=True, choices=TIERS)
    p.add_argument('--lane', required=True, choices=LANES)
    p.add_argument('--label', default=LABEL, type=_label_arg)
    p.add_argument('--reason', required=True, choices=sorted(REASONS))
    a = p.parse_args(argv)
    try:
        action, number = report(_gh, a.repo, a.run_id, a.run_url, a.tier, a.lane, a.reason, a.label)
    except GhError as err:
        sys.stderr.write('race_report: gh %s failed%s\n' % (err, ': ' + err.detail if err.detail else ''))
        raise SystemExit(1)  # an issue, label or comment call failed: the report job must go red
    print('%s %s' % (action, number) if number else action)


if __name__ == '__main__':
    main()
