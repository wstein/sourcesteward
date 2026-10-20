#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Deterministic CI shards for the heavy internal/workspace test package.

Top-level tests are listed with `go test -list`, assigned to shards by greedy
longest-processing-time using scripts/ci-test-weights.json (seconds; unlisted tests
get the file's default) and emitted as `-run` regular expressions. `check` fails when
the shards do not cover the full list exactly once.

  ci_shards.py plan [--shards N]            print the shards with their weights
  ci_shards.py regex --shard K [--shards N] print the -run regex of shard K (1-based)
  ci_shards.py packages                     print every package except the sharded one
  ci_shards.py check [--shards N]           verify the shards via go test -list
"""
import argparse
import json
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
WEIGHTS_FILE = ROOT / 'scripts/ci-test-weights.json'
DEFAULT_SHARDS = 4
NAME = re.compile(r'^Test\w*$')


def parse_list(output):
    return [line.strip() for line in output.splitlines() if NAME.match(line.strip())]


def go_list(package, run='^Test'):
    result = subprocess.run(['go', 'test', '-list', run, package], cwd=ROOT, text=True,
                            capture_output=True, check=False)
    if result.returncode != 0:
        sys.exit('go test -list failed:\n' + result.stdout + result.stderr)
    return parse_list(result.stdout)


def partition(tests, weights, default, count):
    """Greedy LPT: heaviest first (ties by name), each to the lightest shard (ties by index)."""
    ordered = sorted(set(tests), key=lambda name: (-weights.get(name, default), name))
    loads = [0] * count
    shards = [[] for _ in range(count)]
    for name in ordered:
        index = min(range(count), key=lambda i: (loads[i], i))
        shards[index].append(name)
        loads[index] += weights.get(name, default)
    return [sorted(shard) for shard in shards]


def regex(names):
    return '^(' + '|'.join(sorted(names)) + ')$' if names else '^$'


def verify(full, shard_lists):
    seen = {}
    for index, names in enumerate(shard_lists, 1):
        for name in names:
            seen.setdefault(name, []).append(index)
    problems = []
    twice = sorted(name for name, where in seen.items() if len(where) > 1)
    if twice:
        problems.append('tests in more than one shard: ' + ', '.join(twice))
    missing = sorted(set(full) - set(seen))
    if missing:
        problems.append('tests not run by any shard: ' + ', '.join(missing))
    unknown = sorted(set(seen) - set(full))
    if unknown:
        problems.append('unknown tests selected by a shard: ' + ', '.join(unknown))
    if problems:
        sys.exit('shard coverage check failed: ' + '; '.join(problems))


def load_weights():
    data = json.loads(WEIGHTS_FILE.read_text())
    return data['package'], data['weights'], data['default']


def plan(count):
    package, weights, default = load_weights()
    tests = go_list(package)
    return package, tests, weights, default, partition(tests, weights, default, count)


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('command', choices=('plan', 'regex', 'packages', 'check'))
    parser.add_argument('--shards', type=int, default=DEFAULT_SHARDS)
    parser.add_argument('--shard', type=int)
    args = parser.parse_args()
    if args.shards < 1:
        parser.error('--shards must be positive')
    if args.command == 'packages':
        package = load_weights()[0]
        listed = subprocess.run(['go', 'list', './...'], cwd=ROOT, text=True, capture_output=True, check=True).stdout.split()
        module = subprocess.run(['go', 'list', '-m'], cwd=ROOT, text=True, capture_output=True, check=True).stdout.strip()
        heavy = module + '/' + package.removeprefix('./')
        print(' '.join(p for p in listed if p != heavy))
        return
    package, tests, weights, default, shards = plan(args.shards)
    if args.command == 'regex':
        if not args.shard or not 1 <= args.shard <= args.shards:
            parser.error('--shard must be between 1 and --shards')
        print(regex(shards[args.shard - 1]))
    elif args.command == 'plan':
        for index, names in enumerate(shards, 1):
            load = sum(weights.get(n, default) for n in names)
            print('shard %d: %d tests, weight %s' % (index, len(names), load))
            heavy = sorted(names, key=lambda n: (-weights.get(n, default), n))[:3]
            print('  heaviest: ' + ', '.join('%s (%s)' % (n, weights.get(n, default)) for n in heavy))
    else:
        verify(tests, [go_list(package, regex(names)) for names in shards])
        print('Shard coverage verified: %d tests in %d shards, each exactly once' % (len(tests), len(shards)))


if __name__ == '__main__':
    main()
