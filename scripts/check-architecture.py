#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Enforce the CLI/inspection import and effects boundary."""
from pathlib import Path
from functools import lru_cache
import json
import re
import subprocess

ROOT = Path(__file__).resolve().parents[1]
MODULE = 'github.com/wstein/sourcesteward/'


def validate(lines):
    for line in lines:
        package, *imports = line.split()
        if package == MODULE + 'internal/cli':
            for dependency in imports:
                if dependency in ('os', 'os/exec', 'syscall') or dependency == 'net' or dependency.startswith('net/') or (dependency.startswith(MODULE) and dependency not in (MODULE + 'internal/inspection', MODULE + 'internal/workspace', MODULE + 'internal/hosting', MODULE + 'internal/release', MODULE + 'internal/restack', MODULE + 'internal/stwsync', MODULE + 'internal/watchcmd', MODULE + 'internal/pullscmd', MODULE + 'internal/issuescmd', MODULE + 'internal/untrusted', MODULE + 'internal/statuscmd', MODULE + 'internal/statusremote', MODULE + 'internal/overview', MODULE + 'internal/tidywizard/derive', MODULE + 'internal/tidywizard/owner', MODULE + 'internal/tidywizard/planset')):
                    raise ValueError('CLI may access only reviewed inspection/workspace/hosting operations: ' + dependency)
        if package.startswith(MODULE + 'cmd/'):
            for dependency in imports:
                if dependency.startswith(MODULE) and dependency != MODULE + 'internal/cli':
                    raise ValueError('Command entry point must delegate through internal/cli: ' + dependency)


# Spec 026: project and Git processes start only in reviewed files of internal/release, and
# network use is confined to the internal/hosting operation boundary.
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
NETWORK_FREE_PACKAGES = ('internal/release', 'internal/inspection', 'internal/workspace', 'internal/restack', 'internal/untrusted', 'internal/overview', 'internal/tidywizard/derive', 'internal/tidywizard/planset')


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
        if path.startswith('internal/restack/'):
            validate_restack_file(path, imported)


# Spec 015: the read-only restack plan starts Git only in its one reviewed runner and may import
# from the module only the read-only inspection.
RESTACK_PROCESS_FILE = 'internal/restack/git.go'
RESTACK_MODULE_IMPORTS = (MODULE + 'internal/inspection',)


def validate_restack_file(path, imported):
    for dependency in imported:
        if is_process_import(dependency) and not (path == RESTACK_PROCESS_FILE and dependency == 'os/exec'):
            raise ValueError('internal/restack starts Git only in ' + RESTACK_PROCESS_FILE + ': ' + path + ' imports ' + dependency)
        if dependency.startswith(MODULE) and dependency not in RESTACK_MODULE_IMPORTS:
            raise ValueError('internal/restack may import only the read-only inspection from the module: ' + path + ' imports ' + dependency)


@lru_cache(maxsize=1)
def standard_packages():
    result = subprocess.run(['go', 'list', 'std'], cwd=ROOT, text=True, capture_output=True, check=True)
    return set(result.stdout.split())


def release_project_import(path):
    return path in RELEASE_IMPORT_ALLOWLIST or path == RELEASE_PREFIX or path.startswith(RELEASE_PREFIX + '/')


def validate_release_file(path, text, imported):
    if 'unsafe' in imported or LINKNAME.search(text):
        raise ValueError('internal/release must not use unsafe or go:linkname: ' + path)
    code = strip_comments(text)
    unix_writer = path == 'internal/release/openfile_write_unix.go'
    darwin_observer = path == 'internal/release/build_checkout_view_darwin.go'
    for dependency in imported:
        if dependency not in standard_packages() and not release_project_import(dependency) and not ((unix_writer or darwin_observer) and dependency == 'golang.org/x/sys/unix'):
            raise ValueError('release import outside the closed dependency boundary: ' + path + ' imports ' + dependency)
    if unix_writer and 'golang.org/x/sys/unix' in imported:
        if not re.search(r'^\s*(?:import\s+)?"golang.org/x/sys/unix"', text, re.M):
            raise ValueError('writer unix import must have no alias, dot or blank name: ' + path)
        constants = {'O_RDONLY', 'O_WRONLY', 'O_CREAT', 'O_EXCL', 'O_NOFOLLOW', 'O_DIRECTORY', 'O_CLOEXEC', 'O_NONBLOCK'}
        for selector in re.finditer(r'\bunix\.([A-Za-z_][A-Za-z0-9_]*)', code):
            name = selector.group(1)
            if name in constants:
                continue
            if name in ('Openat', 'Mkdirat') and re.match(r'\s*\(', code[selector.end():]):
                continue
            raise ValueError('writer unix permits only exact constants and direct filesystem calls: ' + path)
    if OPEN_FILES.match(path):
        # The file-open helpers may use the syscall.O_ open flags and nothing else from syscall.
        for dependency in imported:
            if is_process_import(dependency) and dependency != 'syscall' and not (unix_writer and dependency == 'golang.org/x/sys/unix'):
                raise ValueError('the file-open helpers import only syscall: ' + path + ' imports ' + dependency)
        if 'syscall' in imported:
            plain = re.search(r'^\s*(import\s+)?"syscall"', text, re.M)
            if not plain or SYSCALL_OPEN_FLAGS.search(code):
                raise ValueError('the file-open helpers may use only the syscall.O_ constants (no alias, no dot import): ' + path)
    elif path in ('internal/release/build_checkout_view_linux.go', 'internal/release/build_checkout_view_darwin.go'):
        # The two platform observers currently read only Stat_t ctime fields.
        # This is not a wildcard process-capability exception for build_* files.
        for dependency in imported:
            if is_process_import(dependency) and dependency != 'syscall' and not (darwin_observer and dependency == 'golang.org/x/sys/unix'):
                raise ValueError('checkout observers may import only reviewed metadata operations: ' + path)
        if darwin_observer and 'golang.org/x/sys/unix' in imported:
            if not re.search(r'^\s*(?:import\s+)?"golang.org/x/sys/unix"', text, re.M):
                raise ValueError('checkout observer unix import must be plain: ' + path)
            for selector in re.finditer(r'\bunix\.([A-Za-z_][A-Za-z0-9_]*)', code):
                name = selector.group(1)
                if name in ('KinfoProc', 'SizeofKinfoProc'):
                    continue
                if name == 'SysctlRaw' and re.match(r'\s*\(\s*"kern\.proc\.pgrp"\s*,', code[selector.end():]):
                    continue
                raise ValueError('Darwin checkout observer permits only direct owned-group read and ABI types: ' + path)
        if 'syscall' in imported:
            if not re.search(r'^\s*(?:import\s+)?"syscall"', text, re.M):
                raise ValueError('checkout observer syscall import must be plain: ' + path)
            for selector in re.finditer(r'\bsyscall\.([A-Za-z_][A-Za-z0-9_]*)', code):
                name = selector.group(1)
                if name == 'Stat_t' or (not darwin_observer and name == 'ESRCH'):
                    continue
                if not darwin_observer and name == 'Getpgid' and re.match(r'\s*\(', code[selector.end():]):
                    continue
                raise ValueError('checkout observers may use only metadata types/direct owned-group observations: ' + path)
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


def reachable(graph, root, boundary=None):
    """Follow direct edges, excluding only the named operation boundary, never shared deps."""
    seen, pending = set(), [root]
    while pending:
        package = pending.pop()
        if package == boundary or package in seen:
            continue
        seen.add(package)
        pending.extend(graph.get(package, ()))
    return seen


def validate_dependencies(graph):
    """Reviewed release/workspace operations delegate through the exact hosting boundary.
    Every path outside that boundary, including all inspection paths, stays network-free.
    """
    for local in NETWORK_FREE_PACKAGES:
        package = MODULE + local
        boundary = MODULE + 'internal/hosting' if local in ('internal/release', 'internal/workspace', 'internal/tidywizard/derive', 'internal/tidywizard/planset') else None
        for dependency in reachable(graph, package, boundary):
            if is_network_import(dependency):
                raise ValueError(package + ' reaches network outside internal/hosting: ' + dependency)


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


def run_release_ast_check():
    """The syntax-tree rules of internal/release (syscall use, cgo, assembly, unsafe, linkname)."""
    result = subprocess.run(['go', 'test', './internal/release', '-run', '^TestReleaseSourceArchitecture', '-count=1'], cwd=ROOT, text=True, capture_output=True)
    if result.returncode != 0:
        raise ValueError('internal/release architecture rules failed:\n' + result.stdout + result.stderr)


FOREIGN_FILE_LISTS = ('CgoFiles', 'CFiles', 'CXXFiles', 'MFiles', 'HFiles', 'FFiles', 'SFiles', 'SwigFiles', 'SwigCXXFiles', 'SysoFiles')
# Module packages internal/release may import: the root package (embedded schema), the existing
# inspection, hosting operations and its own subpackages. Extend only when actually needed.
RELEASE_IMPORT_ALLOWLIST = (MODULE.rstrip('/'), MODULE + 'internal/inspection', MODULE + 'internal/hosting')
RELEASE_PREFIX = MODULE + 'internal/release'
EXEC_ALLOWED_PACKAGES = (RELEASE_PREFIX, MODULE + 'internal/inspection')


def validate_release_packages(packages):
    """`go list -json` of internal/release/...: no cgo, assembly or other foreign sources."""
    for package in packages:
        path = package['ImportPath']
        if path != RELEASE_PREFIX and not path.startswith(RELEASE_PREFIX + '/'):
            continue
        for key in FOREIGN_FILE_LISTS:
            if package.get(key):
                raise ValueError(path + ' has ' + key + ' ' + ', '.join(package[key]) + '; internal/release is pure Go')


def validate_release_graph(graph):
    """graph maps a module package to its direct imports (internal/release/... and its closure)."""
    outside_hosting = reachable(graph, RELEASE_PREFIX, MODULE + 'internal/hosting')
    for package in outside_hosting:
        if package not in standard_packages() and not release_project_import(package) and package != 'golang.org/x/sys/unix':
            raise ValueError('release reaches an unreviewed external/helper package outside hosting: ' + package)
    for package, imports in graph.items():
        in_release = package == RELEASE_PREFIX or package.startswith(RELEASE_PREFIX + '/')
        if in_release:
            for dependency in imports:
                module_internal = dependency.startswith(MODULE) or dependency == MODULE.rstrip('/')
                inside = dependency == RELEASE_PREFIX or dependency.startswith(RELEASE_PREFIX + '/')
                if module_internal and not inside and dependency not in RELEASE_IMPORT_ALLOWLIST:
                    raise ValueError(package + ' imports ' + dependency + ', which is not on the internal/release import allowlist')
        if package in outside_hosting and 'os/exec' in imports and package not in EXEC_ALLOWED_PACKAGES:
            raise ValueError(package + ' imports os/exec; only internal/release (reviewed files) and internal/inspection may')


def go_release_packages():
    result = subprocess.run(['go', 'list', '-json', './internal/release/...'], cwd=ROOT, text=True, capture_output=True, check=True)
    decoder, text, packages, index = json.JSONDecoder(), result.stdout, [], 0
    while index < len(text):
        while index < len(text) and text[index].isspace():
            index += 1
        if index >= len(text):
            break
        package, index = decoder.raw_decode(text, index)
        packages.append(package)
    return packages


def go_release_graph():
    result = subprocess.run(['go', 'list', '-deps', '-f', '{{.ImportPath}} {{join .Imports " "}}', './internal/release/...'], cwd=ROOT, text=True, capture_output=True, check=True)
    graph = {}
    for line in result.stdout.splitlines():
        package, *imports = line.split()
        graph[package] = imports
    return graph


def go_dependencies():
    result = subprocess.run(['go', 'list', '-deps', '-f', '{{.ImportPath}} {{join .Imports " "}}'] + ['./' + p for p in NETWORK_FREE_PACKAGES], cwd=ROOT, text=True, capture_output=True, check=True)
    graph = {}
    for line in result.stdout.splitlines():
        package, *imports = line.split()
        graph[package] = imports
    return graph


if __name__ == '__main__':
    sources = go_sources()
    validate_sources(sources, go_file_imports(sources))
    validate_dependencies(go_dependencies())
    validate_release_packages(go_release_packages())
    validate_release_graph(go_release_graph())
    run_release_ast_check()
    result = subprocess.run(['go', 'list', '-f', '{{.ImportPath}} {{join .Imports " "}}', './...'], cwd=ROOT, text=True, capture_output=True, check=True)
    validate(result.stdout.splitlines())
    print('Architecture boundary checks passed.')
