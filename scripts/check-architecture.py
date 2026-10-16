#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Enforce the CLI/inspection import and effects boundary."""
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[1]
MODULE = 'github.com/wstein/sourcesteward/'


def validate(lines):
    for line in lines:
        package, *imports = line.split()
        if package == MODULE + 'internal/cli':
            for dependency in imports:
                if dependency in ('os', 'os/exec', 'syscall') or dependency == 'net' or dependency.startswith('net/') or (dependency.startswith(MODULE) and dependency not in (MODULE + 'internal/inspection', MODULE + 'internal/workspace', MODULE + 'internal/hosting')):
                    raise ValueError('CLI may access only reviewed inspection/workspace/hosting operations: ' + dependency)
        if package.startswith(MODULE + 'cmd/'):
            for dependency in imports:
                if dependency.startswith(MODULE) and dependency != MODULE + 'internal/cli':
                    raise ValueError('Command entry point must delegate through internal/cli: ' + dependency)


if __name__ == '__main__':
    result = subprocess.run(['go', 'list', '-f', '{{.ImportPath}} {{join .Imports " "}}', './...'], cwd=ROOT, text=True, capture_output=True, check=True)
    validate(result.stdout.splitlines())
    print('Architecture boundary checks passed.')
