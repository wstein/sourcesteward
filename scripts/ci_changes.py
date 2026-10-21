#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Decide whether a pull request changes documentation only.

The CI `changes` job runs this on the checked-out merge commit of a pull request and prints
`docs_only=<bool>` for the job to append to GITHUB_OUTPUT. `docs_only=true` lets the conformance shards be skipped; it is
true only for a pull_request event whose every changed file is on the conservative allowlist
below. Anything else, and any error, yields `false` (fail closed: run everything).

The allowlist is prose only. Files that Go tests, scripts or the site read (specs contracts and
examples, docs data and assets, schemas, workflows, scripts, source) are never on it.

Standard library only.
"""
import argparse
import os
import re
import subprocess
import sys

ALLOWED = (
    re.compile(r'[^/]+\.md'),                                  # README.md, AGENTS.md, ...
    re.compile(r'docs/[^/]+\.md'),                             # top-level docs prose only
    re.compile(r'specs/[^/]+/(?:checklists/)?[^/]+\.md'),      # spec prose, not contracts/ or examples/
)


def is_docs_path(path):
    return any(pattern.fullmatch(path) for pattern in ALLOWED)


def docs_only(paths):
    return bool(paths) and all(is_docs_path(path) for path in paths)


def changed_paths(cwd=None):
    """Files the pull request changes relative to the base tip, from the merge commit.

    The checkout of a pull_request event is a merge commit: HEAD^1 is the base tip and HEAD^2 the
    pull request head. Anything else raises, which callers treat as not docs-only.
    """
    parents = subprocess.run(['git', 'rev-list', '--parents', '-n', '1', 'HEAD'], cwd=cwd, check=True,
                             capture_output=True, text=True).stdout.split()
    if len(parents) != 3:
        raise ValueError('HEAD is not a merge commit')
    out = subprocess.run(['git', 'diff', '--name-only', '--no-renames', '-z', 'HEAD^1', 'HEAD'], cwd=cwd,
                         check=True, capture_output=True).stdout
    return [p.decode('utf-8', 'surrogateescape') for p in out.split(b'\0') if p]


def compute(event, cwd=None):
    if event != 'pull_request':
        return False
    try:
        return docs_only(changed_paths(cwd))
    except (OSError, ValueError, subprocess.CalledProcessError):
        return False


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--event', default=os.environ.get('EVENT_NAME', ''))
    args = parser.parse_args(argv)
    value = 'true' if compute(args.event) else 'false'
    print('docs_only=' + value)
    return 0


if __name__ == '__main__':
    sys.exit(main())
