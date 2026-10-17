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
# nothing but internal/hosting reaches a network package.
PROCESS_FILES = re.compile(r'^internal/release/(vcs_[a-z0-9_]+|attest|build)\.go$')
IMPORT_BLOCK = re.compile(r'^import\s*\((.*?)^\)', re.S | re.M)
IMPORT_LINE = re.compile(r'^\s*(?:[A-Za-z_.][A-Za-z0-9_]*\s+)?"([^"]+)"', re.M)
IMPORT_SINGLE = re.compile(r'^import\s+(?:[A-Za-z_.][A-Za-z0-9_]*\s+)?"([^"]+)"', re.M)
OS_IMPORT = re.compile(r'^\s*(?:import\s+)?(\.|[A-Za-z_][A-Za-z0-9_]*)\s+"os"', re.M)
PROCESS_FUNCTIONS = ('StartProcess',)
NETWORK_ALLOWED = ('net/url', 'net/netip')
PROCESS_IMPORTS = ('os/exec', 'syscall', 'plugin')
# syscall is allowed only for the open flags of the file-open helpers and, for process groups, in build.go.
OPEN_FILES = re.compile(r'^internal/release/openfile_[a-z0-9_]+\.go$')
SYSCALL_OPEN_FLAGS = re.compile(r'\bsyscall\.(?!O_[A-Z]+\b)[A-Za-z_]+')
LINKNAME = re.compile(r'^\s*//go:linkname\b', re.M)
NETWORK_FREE_PACKAGES = ('internal/release', 'internal/inspection', 'internal/workspace')


def is_network_import(path):
    if path in NETWORK_ALLOWED:
        return False
    return path == 'net' or path.startswith('net/') or path == 'crypto/tls' or path.startswith('golang.org/x/net')


def is_process_import(path):
    return path in PROCESS_IMPORTS or path.startswith('golang.org/x/sys')


def source_imports(text):
    found = IMPORT_SINGLE.findall(text)
    for block in IMPORT_BLOCK.findall(text):
        found += IMPORT_LINE.findall(block)
    return found


def strip_comments(text):
    return re.sub(r'//[^\n]*|/\*.*?\*/', '', text, flags=re.S)


def validate_sources(sources, imports=None):
    """Check non-test Go sources. imports maps a path to its import list (from `go list`); without
    it the imports are read from the source text, which handles aliased, blank and dot imports."""
    for path, text in sources.items():
        if path.endswith('_test.go') or not path.endswith('.go'):
            continue
        imported = imports[path] if imports and path in imports else source_imports(text)
        if not path.startswith('internal/hosting/'):
            for dependency in imported:
                if is_network_import(dependency):
                    raise ValueError('Only internal/hosting may use the network: ' + path + ' imports ' + dependency)
        if path.startswith('internal/release/'):
            validate_release_file(path, text, imported)


def validate_release_file(path, text, imported):
    if 'unsafe' in imported or LINKNAME.search(text):
        raise ValueError('internal/release must not use unsafe or go:linkname: ' + path)
    code = strip_comments(text)
    if OPEN_FILES.match(path):
        # The file-open helpers may use the syscall.O_ open flags and nothing else from syscall.
        for dependency in imported:
            if is_process_import(dependency) and dependency != 'syscall':
                raise ValueError('the file-open helpers import only syscall: ' + path + ' imports ' + dependency)
        if 'syscall' in imported:
            plain = re.search(r'^\s*(import\s+)?"syscall"', text, re.M)
            if not plain or SYSCALL_OPEN_FLAGS.search(code):
                raise ValueError('the file-open helpers may use only the syscall.O_ constants (no alias, no dot import): ' + path)
    elif path == 'internal/release/build.go':
        pass  # process groups need syscall; build.go is a reviewed process file
    else:
        for dependency in imported:
            if is_process_import(dependency) and not (dependency == 'os/exec' and PROCESS_FILES.match(path)):
                raise ValueError('internal/release starts processes only in vcs_*.go, attest.go and build.go and uses syscall only in openfile_*.go and build.go: ' + path + ' imports ' + dependency)
    if not PROCESS_FILES.match(path):
        for alias in OS_IMPORT.findall(code) + ['os']:
            for function in PROCESS_FUNCTIONS:
                pattern = r'\b' + function + r'\s*\(' if alias == '.' else r'\b' + re.escape(alias) + r'\.' + function + r'\b'
                if re.search(pattern, code):
                    raise ValueError('internal/release starts processes only in reviewed files: ' + path + ' calls os.' + function)


def validate_dependencies(dependencies):
    """No network package may be in the dependency closure of the local-only packages."""
    for package, deps in dependencies.items():
        if package.removeprefix(MODULE) in NETWORK_FREE_PACKAGES:
            for dependency in deps:
                if is_network_import(dependency):
                    raise ValueError(package + ' depends on the network package ' + dependency)


def go_sources():
    sources = {}
    for top in ('internal', 'cmd'):
        for path in sorted((ROOT / top).rglob('*.go')):
            sources[path.relative_to(ROOT).as_posix()] = path.read_text()
    return sources


def go_file_imports(sources):
    """Imports of each non-test file as `go list` reports them for that file alone."""
    imports = {}
    for path in sources:
        if path.endswith('_test.go'):
            continue
        result = subprocess.run(['go', 'list', '-f', '{{join .Imports " "}}', './' + path], cwd=ROOT, text=True, capture_output=True, check=True)
        imports[path] = result.stdout.split()
    return imports


def go_dependencies():
    result = subprocess.run(['go', 'list', '-deps', '-f', '{{.ImportPath}}'] + ['./' + p for p in NETWORK_FREE_PACKAGES], cwd=ROOT, text=True, capture_output=True, check=True)
    closure = result.stdout.split()
    return {MODULE + p: closure for p in NETWORK_FREE_PACKAGES}


if __name__ == '__main__':
    sources = go_sources()
    validate_sources(sources, go_file_imports(sources))
    validate_dependencies(go_dependencies())
    result = subprocess.run(['go', 'list', '-f', '{{.ImportPath}} {{join .Imports " "}}', './...'], cwd=ROOT, text=True, capture_output=True, check=True)
    validate(result.stdout.splitlines())
    print('Architecture boundary checks passed.')
