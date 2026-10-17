#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Enforce the CLI/inspection import and effects boundary."""
from pathlib import Path
import re
import subprocess

ROOT = Path(__file__).resolve().parents[1]
MODULE = 'github.com/wstein/sourcesteward/'


def validate(lines):
    for line in lines:
        package, *imports = line.split()
        if package == MODULE + 'internal/cli':
            for dependency in imports:
                if dependency in ('os', 'os/exec', 'syscall') or dependency == 'net' or dependency.startswith('net/') or (dependency.startswith(MODULE) and dependency not in (MODULE + 'internal/inspection', MODULE + 'internal/workspace', MODULE + 'internal/hosting', MODULE + 'internal/release')):
                    raise ValueError('CLI may access only reviewed inspection/workspace/hosting operations: ' + dependency)
        if package.startswith(MODULE + 'cmd/'):
            for dependency in imports:
                if dependency.startswith(MODULE) and dependency != MODULE + 'internal/cli':
                    raise ValueError('Command entry point must delegate through internal/cli: ' + dependency)


# Spec 026: project and Git processes start only in reviewed files of internal/release, and
# nothing but internal/hosting imports a network package.
PROCESS_FILES = re.compile(r'^internal/release/(vcs_[a-z0-9_]+|attest|build)\.go$')
IMPORT_BLOCK = re.compile(r'^import\s*\((.*?)^\)', re.S | re.M)
IMPORT_LINE = re.compile(r'^\s*(?:[A-Za-z_.][A-Za-z0-9_]*\s+)?"([^"]+)"', re.M)
IMPORT_SINGLE = re.compile(r'^import\s+(?:[A-Za-z_.][A-Za-z0-9_]*\s+)?"([^"]+)"', re.M)
PROCESS_CALL = re.compile(r'\b(os\.StartProcess|syscall\.ForkExec|syscall\.Exec)\s*\(')
NETWORK_IMPORTS = ('net', 'net/http', 'net/rpc', 'net/smtp', 'net/textproto', 'net/http/httputil')


def imports(text):
    found = IMPORT_SINGLE.findall(text)
    for block in IMPORT_BLOCK.findall(text):
        found += IMPORT_LINE.findall(block)
    return found


def strip_comments(text):
    return re.sub(r'//[^\n]*|/\*.*?\*/', '', text, flags=re.S)


def validate_sources(sources):
    for path, text in sources.items():
        if path.endswith('_test.go') or not path.endswith('.go'):
            continue
        imported = imports(text)
        if not path.startswith('internal/hosting/'):
            for dependency in imported:
                if dependency in NETWORK_IMPORTS:
                    raise ValueError('Only internal/hosting may use the network: ' + path + ' imports ' + dependency)
        if path.startswith('internal/release/') and not PROCESS_FILES.match(path):
            if 'os/exec' in imported or PROCESS_CALL.search(strip_comments(text)):
                raise ValueError('internal/release starts processes only in vcs_*.go, attest.go and build.go: ' + path)


def go_sources():
    sources = {}
    for top in ('internal', 'cmd'):
        for path in sorted((ROOT / top).rglob('*.go')):
            sources[path.relative_to(ROOT).as_posix()] = path.read_text()
    return sources


if __name__ == '__main__':
    validate_sources(go_sources())
    result = subprocess.run(['go', 'list', '-f', '{{.ImportPath}} {{join .Imports " "}}', './...'], cwd=ROOT, text=True, capture_output=True, check=True)
    validate(result.stdout.splitlines())
    print('Architecture boundary checks passed.')
