#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Decide whether a pull request is the tip of a stack (Spec 039, owner decision 2026-10-26).

The CI `changes` job lists the open pull requests of the repository with `gh api` into a file and runs
`ci_stack.py --prs-file FILE`, which prints `is_stack_tip=<bool>` for the job to append to GITHUB_OUTPUT. The heavy
Git/jj conformance shards run only for a tip: a pull request is NOT a tip when another open pull request of the
same repository, from a branch of the same repository, has this pull request's head branch as its base branch.
A Draft child does not count: authors push early as Draft, and a forgotten Draft must not remove the heavy gate.

Fail closed: every doubt gives `true` (run the shards): an event other than pull_request, a fork pull request, an
unknown head repository or branch, the default branch as head branch, a missing, unreadable or malformed listing, a
listing without this pull request, and a child from a fork or another repository (which would let an outsider switch
off the heavy gate). Branch names are data: they come in through the environment and are never interpolated into a
shell line.

Standard library only.
"""
import argparse
import json
import os
import sys


def parse_prs(text):
    """The records of a stream of JSON objects or arrays of objects; raises ValueError on anything else.

    A record has an integer `number`, a boolean `draft` and `base_ref`, `base_repo` and `head_repo` that are strings or null."""
    decoder = json.JSONDecoder()
    values, pos = [], 0
    while True:
        while pos < len(text) and text[pos].isspace():
            pos += 1
        if pos >= len(text):
            break
        value, pos = decoder.raw_decode(text, pos)
        values.extend(value if isinstance(value, list) else [value])
    for record in values:
        if not isinstance(record, dict) or not isinstance(record.get('number'), int) or isinstance(record['number'], bool):
            raise ValueError('bad pull request record')
        if not isinstance(record.get('draft'), bool):
            raise ValueError('bad pull request record')
        for key in ('base_ref', 'base_repo', 'head_repo'):
            if key not in record or not (record[key] is None or isinstance(record[key], str)):
                raise ValueError('bad pull request record')
    return values


def is_stack_tip(event, number, head_ref, head_repo, base_repo, default_branch, prs):
    """False only when the evidence shows an open, non-Draft same-repository child; true in every other case."""
    if event != 'pull_request' or not isinstance(number, int) or prs is None:
        return True
    if not head_ref or not base_repo or head_repo != base_repo or head_ref == default_branch:
        return True
    if not any(p['number'] == number and p['base_repo'] == base_repo and p['head_repo'] == base_repo for p in prs):
        return True  # this pull request is missing from its own listing: the lookup cannot be trusted
    for p in prs:
        if not p['draft'] and p['number'] != number and p['base_ref'] == head_ref and p['base_repo'] == base_repo and p['head_repo'] == base_repo:
            return False
    return True


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--prs-file', required=True)
    args = parser.parse_args(argv)
    try:
        with open(args.prs_file, encoding='utf-8') as handle:
            prs = parse_prs(handle.read())
    except (OSError, ValueError):
        prs = None  # json.JSONDecodeError is a ValueError
    try:
        number = int(os.environ.get('PR_NUMBER', ''))
    except ValueError:
        number = None
    env = os.environ.get
    tip = is_stack_tip(env('EVENT_NAME', ''), number, env('HEAD_REF', ''), env('HEAD_REPO', ''), env('BASE_REPO', ''),
                       env('DEFAULT_BRANCH', ''), prs)
    print('is_stack_tip=' + ('true' if tip else 'false'))
    return 0


if __name__ == '__main__':
    sys.exit(main())
