#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Decide whether a pull request changes documentation only.

The CI `changes` job runs this on the checked-out merge commit of a pull request and prints
`docs_only=<bool>` for the job to append to GITHUB_OUTPUT. `docs_only=true` lets the conformance shards be skipped; it is
true only for a pull_request event whose every changed file is on the conservative allowlist
below. Anything else, and any error, yields `false` (fail closed: run everything).

The allowlist is prose only. Files that Go tests, scripts or the site read (specs contracts and
examples, docs data and assets, schemas, workflows, scripts, source) are never on it.

`ci_changes.py affected` (Spec 039) prints `affected=<import paths or ./...>` and
`affected_reason=docs-only|packages|all`: the changed Go packages plus every package that imports them, also
through test imports. `affected --files-from FILE` classifies a path list (the pull request file list from the
API) by path only and prints just `affected_reason`. Both fail closed to `all` (exit status 0, so a classifier error
runs more tests, never fewer); any path under `.github/` or `scripts/`, any other non-package non-prose path, an
empty list and an unreadable input give `all`.

Standard library only.
"""
import argparse
import json
import os
import posixpath
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


def clean_path(path):
    """The normalised repository relative path, or None when it is empty, absolute or leaves the tree."""
    if not path or path.startswith('/') or '\0' in path:
        return None
    norm = posixpath.normpath(path)
    if norm in ('.', '..') or norm.startswith('../') or norm != path:
        return None
    return norm


def is_package_path(path):
    """Path-only guess that a file belongs to a Go package: source under internal/ or cmd/, or a root .go file."""
    if path.startswith(('internal/', 'cmd/')):
        return path.count('/') >= 2
    return '/' not in path and path.endswith('.go')


def classify_by_path(paths, reads=None):
    """`docs-only`, `packages` or `all` from the paths alone (no Go package graph).

    Prose that a Go test reads (scripts/ci-test-reads.json) is not `docs-only`: it gives `packages`."""
    cleaned = [clean_path(path) for path in paths]
    if not cleaned or None in cleaned:
        return 'all'
    code = [path for path in cleaned if not is_docs_path(path)]
    if not code:
        reads = load_reads() if reads is None else reads
        return 'packages' if any(matches_any(path, reads) for path in cleaned) else 'docs-only'
    return 'packages' if all(is_package_path(path) for path in code) else 'all'


def parse_go_list(text):
    """The package objects of a `go list -json` stream (concatenated JSON values)."""
    decoder = json.JSONDecoder()
    packages, pos = [], 0
    while True:
        while pos < len(text) and text[pos].isspace():
            pos += 1
        if pos >= len(text):
            return packages
        value, pos = decoder.raw_decode(text, pos)
        if not isinstance(value, dict) or 'ImportPath' not in value:
            raise ValueError('unexpected go list output')
        packages.append(value)


def go_list(cwd=None):
    out = subprocess.run(['go', 'list', '-json=ImportPath,Dir,Imports,TestImports,XTestImports', './...'], cwd=cwd,
                         check=True, capture_output=True, text=True).stdout
    return parse_go_list(out)


def package_dirs(packages, root):
    """Map the repository relative directory of every package to its import path."""
    dirs = {}
    for package in packages:
        rel = os.path.relpath(package['Dir'], root)
        if rel == '..' or rel.startswith('../'):
            raise ValueError('package outside the repository')
        dirs['.' if rel == '.' else rel.replace(os.sep, '/')] = package['ImportPath']
    return dirs


def owner_package(path, dirs):
    """The package that owns a path: Go source or data under internal/, cmd/ or a root .go file; else None."""
    if '/' not in path:
        return dirs.get('.') if path.endswith('.go') else None
    if not path.startswith(('internal/', 'cmd/')):
        return None
    parent = posixpath.dirname(path)
    while parent:
        if parent in dirs:
            return dirs[parent]
        parent = posixpath.dirname(parent)
    return None


def reverse_closure(packages, seeds):
    importers = {}
    for package in packages:
        for name in package.get('Imports', []) + package.get('TestImports', []) + package.get('XTestImports', []):
            importers.setdefault(name, set()).add(package['ImportPath'])
    seen, queue = set(seeds), list(seeds)
    while queue:
        for importer in importers.get(queue.pop(), ()):
            if importer not in seen:
                seen.add(importer)
                queue.append(importer)
    return sorted(seen)


READS_FILE = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'ci-test-reads.json')


KINDS = ('read', 'build')


def load_reads(path=None):
    """The declared test reads and builds (scripts/ci-test-reads.json); raises on any malformed entry.

    An entry has a `kind` (`read`: a test reads those files; `build`: a test compiles them), a `prefix` (a directory
    ending in `/`, or one exact file path) or a regular expression `pattern`, and the `dependents` package directories."""
    with open(path or READS_FILE, encoding='utf-8') as handle:
        entries = json.load(handle)['reads']
    for entry in entries:
        if entry['kind'] not in KINDS or ('prefix' in entry) == ('pattern' in entry):
            raise ValueError('bad kind or matcher')
        if 'prefix' in entry and (not isinstance(entry['prefix'], str) or not entry['prefix']):
            raise ValueError('bad prefix')
        if 'pattern' in entry:
            re.compile(entry['pattern'])
        if not entry['dependents'] or not all(isinstance(d, str) for d in entry['dependents']):
            raise ValueError('bad dependents')
    return entries


def entry_matches(entry, path):
    if 'pattern' in entry:
        return re.fullmatch(entry['pattern'], path) is not None
    prefix = entry['prefix']
    return path.startswith(prefix) if prefix.endswith('/') else path == prefix


def matches_any(path, reads):
    """True when a declared test read covers the path (a prose file read by a Go test must not be docs-only)."""
    return any(entry['kind'] == 'read' and entry_matches(entry, path) for entry in reads)


def read_dependents(path, reads, dirs, build=True):
    """Import paths of the packages whose tests read or build what `path` is part of (`build` False skips the build entries)."""
    found = set()
    for entry in reads:
        if not entry_matches(entry, path):
            continue
        if entry['kind'] == 'build' and (not build or not path.endswith('.go') or path.endswith('_test.go')):
            continue
        for dependent in entry['dependents']:
            if dependent not in dirs:
                raise ValueError('declared dependent %s is not a package' % dependent)
            found.add(dirs[dependent])
    return found


def affected_from_paths(paths, packages, root, reads=None):
    """(`docs-only`|`packages`|`all`, sorted import paths); the list is empty unless the reason is `packages`.

    `reads` is the declared cross-package read list (default: scripts/ci-test-reads.json). A change of prose only
    selects just the packages whose tests read it."""
    cleaned = [clean_path(path) for path in paths]
    if not cleaned or None in cleaned:
        return 'all', []
    reads = load_reads() if reads is None else reads
    code = [path for path in cleaned if not is_docs_path(path)]
    if not code and not any(matches_any(path, reads) for path in cleaned):
        return 'docs-only', []
    dirs = package_dirs(packages, root)
    if not code:
        readers = set()
        for path in cleaned:
            readers |= read_dependents(path, reads, dirs)
        return 'packages', sorted(readers)
    seeds = {owner_package(path, dirs) for path in code}
    if None in seeds:
        return 'all', []
    for path in cleaned:
        seeds |= read_dependents(path, reads, dirs)
    by_import = {name: rel for rel, name in dirs.items()}
    while True:  # an added dependent brings the readers of its own sources and its importers
        closure = set(reverse_closure(packages, seeds))
        more = set()
        for name in closure:
            rel = by_import[name]
            more |= read_dependents('x.go' if rel == '.' else rel + '/x.go', reads, dirs, build=False)
        if more <= closure:
            return 'packages', sorted(closure)
        seeds = closure | more


def compute_affected(event, cwd=None, reads=None):
    if event != 'pull_request':
        return 'all', []
    try:
        paths = changed_paths(cwd)
        reads = load_reads() if reads is None else reads
        if not paths or (all(is_docs_path(path) for path in paths) and not any(matches_any(path, reads) for path in paths)):
            return affected_from_paths(paths, [], cwd or os.getcwd(), reads)
        return affected_from_paths(paths, go_list(cwd), os.path.realpath(cwd or os.getcwd()), reads)
    except Exception:  # fail closed on any error, never fewer tests
        return 'all', []


def print_affected(reason, packages):
    print('affected=' + ('./...' if reason == 'all' else ' '.join(packages)))
    print('affected_reason=' + reason)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('command', nargs='?', choices=('affected',))
    parser.add_argument('--event', default=os.environ.get('EVENT_NAME', ''))
    parser.add_argument('--files-from', help='newline separated path list (affected only)')
    args = parser.parse_args(argv)
    if args.command == 'affected':
        if args.files_from is not None:
            try:
                with open(args.files_from, encoding='utf-8', errors='surrogateescape') as handle:
                    reason = classify_by_path([line for line in handle.read().split('\n') if line])
            except Exception:
                reason = 'all'
            print('affected_reason=' + reason)
        else:
            print_affected(*compute_affected(args.event))
        return 0
    if args.files_from is not None:
        parser.error('--files-from needs the affected command')
    value = 'true' if compute(args.event) else 'false'
    print('docs_only=' + value)
    return 0


if __name__ == '__main__':
    sys.exit(main())
