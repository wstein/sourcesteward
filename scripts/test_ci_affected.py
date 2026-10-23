# SPDX-License-Identifier: EUPL-1.2
"""Tests for the `affected` mode of scripts/ci_changes.py (Spec 039 WP-1).

Synthetic package graphs cover the closure and path mapping; a temporary Git repository with a real Go
module covers the merge-commit mode end to end. Standard library only (the Go tests need `go` on PATH).
"""
import importlib.util
import io
import contextlib
import re
import unittest.mock as mock
import json
import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
_SPEC = importlib.util.spec_from_file_location('ci_changes', ROOT / 'scripts/ci_changes.py')
CH = importlib.util.module_from_spec(_SPEC)
_SPEC.loader.exec_module(CH)

M = 'example.com/m'


def pkg(rel, imports=(), tests=(), xtests=()):
    return {'ImportPath': M + ('/' + rel if rel != '.' else ''), 'Dir': '/repo/' + rel if rel != '.' else '/repo',
            'Imports': list(imports), 'TestImports': list(tests), 'XTestImports': list(xtests)}


GRAPH = [
    pkg('.', [M + '/internal/a']),
    pkg('internal/a', ['fmt']),
    pkg('internal/b', [M + '/internal/a']),
    pkg('internal/c', [], [M + '/internal/b']),            # test-only edge: c's test imports b
    pkg('internal/d', [], [], [M + '/internal/c']),        # external test package edge
    pkg('internal/e', ['os']),
    pkg('cmd/x', [M + '/internal/d']),
]


class ClosureTests(unittest.TestCase):
    def affected(self, paths):
        return CH.affected_from_paths(paths, GRAPH, '/repo', [])

    def test_reverse_closure_follows_imports_test_imports_and_external_tests(self):
        reason, pkgs = self.affected(['internal/a/a.go'])
        self.assertEqual(reason, 'packages')
        self.assertEqual(pkgs, sorted(M + s for s in ('', '/cmd/x', '/internal/a', '/internal/b', '/internal/c', '/internal/d')))

    def test_an_unrelated_leaf_selects_only_itself(self):
        self.assertEqual(self.affected(['internal/e/e_test.go']), ('packages', [M + '/internal/e']))

    def test_testdata_and_nested_files_map_to_the_nearest_package(self):
        self.assertEqual(self.affected(['internal/e/testdata/deep/x.json']), ('packages', [M + '/internal/e']))

    def test_a_root_go_file_maps_to_the_root_package(self):
        self.assertEqual(self.affected(['schemas.go']), ('packages', [M]))

    def test_docs_beside_code_are_ignored_and_docs_alone_are_docs_only(self):
        self.assertEqual(self.affected(['docs/a.md', 'internal/e/e.go']), ('packages', [M + '/internal/e']))
        self.assertEqual(self.affected(['docs/a.md', 'specs/1/spec.md']), ('docs-only', []))

    def test_every_non_package_non_prose_path_gives_all(self):
        for path in ('go.mod', 'go.sum', 'Makefile', 'schemas/v2/a.json', 'docs/feature-status.json', 'scripts/x.py',
                     '.github/workflows/ci.yml', 'specs/1/contracts/c.md', 'site/hugo.toml', 'LICENSE',
                     'internal/README.md', 'internal/new/x.go', 'cmd/other/main.go', 'third_party/x/y.go'):
            self.assertEqual(self.affected(['internal/e/e.go', path]), ('all', []), path)

    def test_an_empty_path_list_gives_all_not_docs_only(self):
        self.assertEqual(self.affected([]), ('all', []))

    def test_a_deleted_package_directory_gives_all(self):
        self.assertEqual(self.affected(['internal/gone/gone.go']), ('all', []))

    def test_a_path_that_escapes_the_tree_gives_all(self):
        self.assertEqual(self.affected(['internal/e/../../go.mod']), ('all', []))
        self.assertEqual(self.affected(['/etc/passwd']), ('all', []))

    def test_cycles_in_the_test_graph_terminate(self):
        graph = [pkg('internal/p', [], [M + '/internal/q']), pkg('internal/q', [M + '/internal/p'])]
        reason, pkgs = CH.affected_from_paths(['internal/p/p.go'], graph, '/repo', [])
        self.assertEqual((reason, pkgs), ('packages', [M + '/internal/p', M + '/internal/q']))

    def test_go_list_json_stream_is_parsed(self):
        text = json.dumps(GRAPH[1]) + '\n' + json.dumps(GRAPH[2], indent=2)
        self.assertEqual([p['ImportPath'] for p in CH.parse_go_list(text)], [M + '/internal/a', M + '/internal/b'])
        with self.assertRaises(ValueError):
            CH.parse_go_list('{"ImportPath": ')


class CleanPathTests(unittest.TestCase):
    def test_only_normalised_relative_paths_are_accepted(self):
        for ok in ('a', 'a/b.go', 'internal/x/y.go'):
            self.assertEqual(CH.clean_path(ok), ok)
        for bad in ('', '/a', 'a//b', 'a/./b', 'a/b/', 'a/../b', '../a', '.', '..', './a', 'a\0b'):
            self.assertIsNone(CH.clean_path(bad), repr(bad))

    def test_unnormalised_paths_give_all_in_both_modes(self):
        self.assertEqual(CH.classify_by_path(['internal/x/./y.go']), 'all')
        self.assertEqual(CH.affected_from_paths(['internal/e//e.go'], GRAPH, '/repo', []), ('all', []))


class ReadsTests(unittest.TestCase):
    """The declared cross-package reads (scripts/ci-test-reads.json)."""

    def test_the_committed_file_loads_and_every_dependent_is_a_real_package(self):
        reads = CH.load_reads()
        listed = {p['ImportPath'] for p in CH.go_list(str(ROOT))} if shutil.which('go') else None
        for entry in reads:
            self.assertIn(entry['kind'], ('read', 'build'))
            self.assertTrue(('prefix' in entry) != ('pattern' in entry))
            for dependent in entry['dependents']:
                self.assertTrue((ROOT / dependent).is_dir(), dependent)
                if listed is not None:
                    self.assertTrue(any(i.endswith('/' + dependent) for i in listed), dependent)

    def test_a_cli_change_selects_release_which_reads_the_cli_sources(self):
        graph = [pkg('internal/cli', ['fmt']), pkg('internal/release', ['fmt']), pkg('internal/other')]
        reads = [{'kind': 'read', 'prefix': 'internal/cli/', 'dependents': ['internal/release']}]
        self.assertEqual(CH.affected_from_paths(['internal/cli/cli.go'], graph, '/repo', reads),
                         ('packages', [M + '/internal/cli', M + '/internal/release']))
        self.assertEqual(CH.affected_from_paths(['internal/other/o.go'], graph, '/repo', reads),
                         ('packages', [M + '/internal/other']))

    def test_with_the_committed_mapping_a_cli_source_change_selects_release_and_the_build_tests(self):
        graph = [pkg(d) for d in ('internal/cli', 'internal/release', 'internal/contracts', 'internal/releaseworkflow',
                                  'internal/watch', 'internal/watchcmd', 'internal/workspace', 'cmd/stw')]
        reason, pkgs = CH.affected_from_paths(['internal/cli/cli.go'], graph, '/repo')
        self.assertEqual(reason, 'packages')
        for name in ('internal/release', 'internal/contracts', 'internal/releaseworkflow'):
            self.assertIn(M + '/' + name, pkgs)
        self.assertNotIn(M + '/internal/workspace', pkgs)
        reason, pkgs = CH.affected_from_paths(['internal/watch/w.go'], graph, '/repo')
        self.assertIn(M + '/internal/cli', pkgs)
        self.assertIn(M + '/internal/release', pkgs)  # through the cli entry

    def test_test_files_do_not_trigger_the_build_entries(self):
        graph = [pkg(d) for d in ('internal/cli', 'internal/contracts', 'internal/releaseworkflow', 'internal/release',
                                  'internal/hosting')]
        self.assertEqual(CH.affected_from_paths(['internal/hosting/x_test.go'], graph, '/repo')[1], [M + '/internal/hosting'])
        self.assertIn(M + '/internal/contracts', CH.affected_from_paths(['internal/hosting/x.go'], graph, '/repo')[1])

    def test_a_declared_dependent_that_is_not_a_package_fails_closed(self):
        reads = [{'kind': 'read', 'prefix': 'internal/e/', 'dependents': ['internal/missing']}]
        with self.assertRaises(ValueError):
            CH.affected_from_paths(['internal/e/e.go'], GRAPH, '/repo', reads)

    def test_malformed_reads_files_are_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            for text in ('{', '{"reads": [{"prefix": "internal/x/", "dependents": ["a"]}]}',
                         '{"reads": [{"kind": "read", "prefix": "internal/x/", "dependents": []}]}',
                         '{"reads": [{"kind": "read", "dependents": ["a"]}]}',
                         '{"reads": [{"kind": "other", "prefix": "a/", "dependents": ["a"]}]}',
                         '{"reads": [{"kind": "read", "prefix": "a/", "pattern": "a", "dependents": ["a"]}]}',
                         '{"reads": [{"kind": "read", "pattern": "(", "dependents": ["a"]}]}'):
                path = Path(tmp, 'r.json')
                path.write_text(text)
                with self.assertRaises(Exception):
                    CH.load_reads(str(path))


RUN = re.compile(r'"[\w./*+-]*"(?:\s*,\s*"[\w./*+-]*")*|`[\w./*+-]+`')
CALL = re.compile(r'ReadFile|ReadDir|Glob|Walk|filepath\.Join|Command|Stat|Open|\.Dir\b')


def prose_files(root):
    found = []
    for base, names, files in os.walk(root):
        names[:] = [n for n in names if not n.startswith('.') and n != 'node_modules']
        for name in files:
            rel = os.path.relpath(os.path.join(base, name), root).replace(os.sep, '/')
            if CH.is_docs_path(rel):
                found.append(rel)
    return found


def undeclared(root, reads, packages):
    """Cross-package reads, builds and prose reads of the Go tests under `root` without a matching entry."""
    root = str(root)
    dirs = CH.package_dirs(packages, root)
    prose = prose_files(root)
    missing = set()
    tests = [os.path.relpath(os.path.join(base, n), root).replace(os.sep, '/')
             for top in ('internal', 'cmd') for base, _, files in os.walk(os.path.join(root, top))
             for n in files if n.endswith('_test.go')]
    for rel in sorted(tests):
        own = os.path.dirname(rel)
        if own not in dirs:
            continue
        text = Path(root, rel).read_text(errors='replace')
        lines = text.splitlines()
        literals, candidates = set(), []
        for m in re.finditer(r'"((?:[^"\\\n]|\\.)*)"|`([^`\n]*)`', text):
            value = m.group(1) if m.group(1) is not None else m.group(2)
            literals.add(value)
            literals.update(value.split('/'))
        for m in RUN.finditer(text):
            parts = [x.strip().strip('"`') for x in re.split(r'\s*,\s*', m.group(0))]
            raw = '/'.join(x for x in parts if x)
            if not raw:
                continue
            cand = os.path.normpath(os.path.join(own, raw)) if raw.startswith('..') else os.path.normpath(raw)
            line = text.count('\n', 0, m.start())
            window = ' '.join(lines[max(0, line - 1):line + 3])
            candidates.append((cand, window))
        referenced = {c for c, _ in candidates}
        for cand, window in candidates:
            if not cand.startswith(('internal/', 'cmd/')) or (cand + '/').startswith(own + '/'):
                continue
            probe = cand
            while probe and not os.path.exists(os.path.join(root, probe)):
                probe = os.path.dirname(probe)
            if not probe.startswith(('internal/', 'cmd/')) or (probe + '/').startswith(own + '/'):
                continue
            build = '"go"' in window and ('"build"' in window or '"list"' in window)
            if not build and not CALL.search(window):
                continue
            kind = 'build' if build else 'read'
            target = probe + ('/x.go' if build else '/x')
            ok = any(e['kind'] == kind and CH.entry_matches(e, target) and own in e['dependents'] for e in reads)
            if not ok and kind == 'read':
                owner = CH.owner_package(probe + '/x.go', dirs)
                ok = (owner is not None and owner != dirs[own] and dirs[own] in CH.reverse_closure(packages, [owner])
                      and 'testdata' not in cand)
            if not ok:
                missing.add('%s %ss %s' % (rel, kind, probe))
        for path in prose:
            if path in referenced or all(x in literals for x in path.split('/')):
                if not any(e['kind'] == 'read' and CH.entry_matches(e, path) and own in e['dependents'] for e in reads):
                    missing.add('%s reads prose %s' % (rel, path))
    return sorted(missing)


class ScanFixtureTests(unittest.TestCase):
    """The scan itself, on a synthetic tree: an undeclared read, build or prose read must be reported."""

    def tree(self, files):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        for name, text in files.items():
            target = Path(tmp.name, name)
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text(text)
        packages = [{'ImportPath': M + '/' + d, 'Dir': str(Path(tmp.name, d)), 'Imports': [], 'TestImports': [], 'XTestImports': []}
                    for d in ('internal/a', 'internal/b')]
        return tmp.name, packages

    BASE = {'internal/a/a.go': 'package a\n', 'internal/b/b.go': 'package b\n', 'internal/b/testdata/x.json': '{}',
            'README.md': '# r\n', 'docs/guide.md': '# g\n'}

    def scan(self, extra, reads):
        root, packages = self.tree(dict(self.BASE, **extra))
        return undeclared(root, reads, packages)

    def test_an_undeclared_sibling_read_fails_and_a_read_entry_fixes_it(self):
        test = {'internal/a/a_test.go': 'package a\n\nvar _ = os.ReadFile("../b/b.go")\n'}
        self.assertEqual(self.scan(test, []), ['internal/a/a_test.go reads internal/b/b.go'])
        read = [{'kind': 'read', 'prefix': 'internal/b/', 'dependents': ['internal/a']}]
        self.assertEqual(self.scan(test, read), [])

    def test_a_broad_build_entry_does_not_declare_a_file_read(self):
        test = {'internal/a/a_test.go': 'package a\n\nvar _ = filepath.Join(root(t), "internal/b/testdata/x.json")\n'}
        build = [{'kind': 'build', 'prefix': 'internal/', 'dependents': ['internal/a']}]
        self.assertEqual(self.scan(test, build), ['internal/a/a_test.go reads internal/b/testdata/x.json'])
        read = [{'kind': 'read', 'prefix': 'internal/b/testdata/', 'dependents': ['internal/a']}]
        self.assertEqual(self.scan(test, read), [])

    def test_a_go_build_of_another_package_needs_a_build_entry(self):
        test = {'internal/a/a_test.go': 'package a\n\nvar _ = exec.Command("go", "build", "-o", bin, "./internal/b")\n'}
        self.assertEqual(self.scan(test, []), ['internal/a/a_test.go builds internal/b'])
        read = [{'kind': 'read', 'prefix': 'internal/b/', 'dependents': ['internal/a']}]
        self.assertEqual(self.scan(test, read), ['internal/a/a_test.go builds internal/b'])
        build = [{'kind': 'build', 'prefix': 'internal/', 'dependents': ['internal/a']}]
        self.assertEqual(self.scan(test, build), [])

    def test_a_prose_file_read_by_a_test_needs_a_read_entry(self):
        for source in ('os.ReadFile("../../README.md")', 'os.ReadFile(filepath.Join(root, "docs", "guide.md"))'):
            test = {'internal/a/a_test.go': 'package a\n\nvar _ = %s\n' % source}
            name = 'README.md' if 'README' in source else 'docs/guide.md'
            self.assertEqual(self.scan(test, []), ['internal/a/a_test.go reads prose ' + name], source)
            entry = [{'kind': 'read', 'prefix': name, 'dependents': ['internal/a']}]
            self.assertEqual(self.scan(test, entry), [], source)
            wrong = [{'kind': 'build', 'prefix': name, 'dependents': ['internal/a']}]
            self.assertEqual(self.scan(test, wrong), ['internal/a/a_test.go reads prose ' + name], source)

    def test_a_test_reading_only_its_own_files_needs_nothing(self):
        test = {'internal/a/a_test.go': 'package a\n\nvar _ = os.ReadFile("testdata/x.json")\nvar _ = os.ReadFile("a.go")\n'}
        self.assertEqual(self.scan(test, []), [])


@unittest.skipUnless(shutil.which('go'), 'needs go')
class ReadsScanTests(unittest.TestCase):
    def test_every_cross_package_read_build_and_prose_read_of_the_committed_tests_is_declared(self):
        packages = CH.go_list(str(ROOT))
        self.assertEqual(undeclared(ROOT, CH.load_reads(), packages), [], 'add an entry to scripts/ci-test-reads.json')


class ProseReadsTests(unittest.TestCase):
    """Prose that a Go test reads is not docs-only: its readers run (Spec 039 T1)."""

    GRAPH = [pkg(d) for d in ('internal/cli', 'internal/release', 'internal/contracts', 'internal/releaseworkflow',
                              'internal/watch', 'internal/watchcmd', 'internal/workspace', 'cmd/stw')] + [pkg('.')]

    def affected(self, paths):
        return CH.affected_from_paths(paths, self.GRAPH, '/repo')

    def test_a_readme_only_change_selects_exactly_its_readers(self):
        self.assertEqual(self.affected(['README.md']),
                         ('packages', [M + '/internal/release', M + '/internal/releaseworkflow']))

    def test_a_spec_prose_change_selects_its_reader(self):
        self.assertEqual(self.affected(['specs/027-inventory-limits/data-model.md']), ('packages', [M + '/internal/contracts']))
        self.assertEqual(self.affected(['specs/034-bootstrap-release/spec.md']), ('packages', [M + '/internal/releaseworkflow']))

    def test_other_prose_stays_docs_only(self):
        self.assertEqual(self.affected(['CONTRIBUTING.md', 'docs/commands.md', 'specs/1/spec.md']), ('docs-only', []))

    def test_prose_beside_code_adds_its_readers_to_the_closure(self):
        reason, pkgs = self.affected(['specs/034-bootstrap-release/spec.md', 'internal/watchcmd/w.go'])
        self.assertEqual(reason, 'packages')
        self.assertIn(M + '/internal/releaseworkflow', pkgs)

    def test_files_from_marks_read_prose_as_packages(self):
        self.assertEqual(CH.classify_by_path(['README.md']), 'packages')
        self.assertEqual(CH.classify_by_path(['docs/commands.md', 'README.md']), 'packages')
        self.assertEqual(CH.classify_by_path(['docs/commands.md']), 'docs-only')

    def test_the_git_mode_runs_the_readers_for_a_readme_only_pull_request(self):
        helper = AffectedGitTests()
        reads = [{'kind': 'read', 'prefix': 'README.md', 'dependents': ['internal/e']}]
        with tempfile.TemporaryDirectory() as tmp:
            helper.merged(tmp, {'README.md': 'changed\n'})
            self.assertEqual(CH.compute_affected('pull_request', cwd=tmp, reads=reads),
                             ('packages', ['example.com/m/internal/e']))
            self.assertEqual(CH.compute_affected('pull_request', cwd=tmp, reads=[]), ('docs-only', []))

    def test_root_go_files_select_the_binary_build_tests(self):
        for path in ('schemas.go', 'release_record_schemas.go'):
            reason, pkgs = self.affected([path])
            self.assertEqual(reason, 'packages', path)
            self.assertIn(M + '/internal/releaseworkflow', pkgs, path)
            self.assertIn(M + '/internal/contracts', pkgs, path)
        self.assertNotIn(M + '/internal/releaseworkflow', self.affected(['schemas_test.go'])[1])

    def test_exact_file_and_pattern_entries_match_only_what_they_name(self):
        exact = {'kind': 'read', 'prefix': 'README.md', 'dependents': ['x']}
        self.assertTrue(CH.entry_matches(exact, 'README.md'))
        self.assertFalse(CH.entry_matches(exact, 'README.md.bak'))
        self.assertFalse(CH.entry_matches(exact, 'docs/README.md'))
        pattern = {'kind': 'build', 'pattern': r'[^/]+\.go', 'dependents': ['x']}
        self.assertTrue(CH.entry_matches(pattern, 'a.go'))
        self.assertFalse(CH.entry_matches(pattern, 'internal/a.go'))


class FilesFromTests(unittest.TestCase):
    def classify(self, paths):
        return CH.classify_by_path(paths)

    def test_prose_only_is_docs_only(self):
        self.assertEqual(self.classify(['docs/a.md', 'CONTRIBUTING.md', 'specs/1/spec.md']), 'docs-only')

    def test_package_paths_give_packages(self):
        self.assertEqual(self.classify(['internal/x/x.go', 'cmd/stw/main.go', 'schemas.go', 'docs/a.md']), 'packages')

    def test_github_and_scripts_paths_give_all_even_beside_packages(self):
        for path in ('.github/workflows/ci.yml', '.github/CODEOWNERS', 'scripts/ci_changes.py', 'scripts/x/y.py'):
            self.assertEqual(self.classify(['internal/x/x.go', path]), 'all', path)

    def test_any_other_path_and_an_empty_list_give_all(self):
        for paths in ([], ['go.mod'], ['Makefile'], ['schemas/a.json'], ['docs/a.json'], ['internal/x/../../go.mod'], ['/abs.go'],
                      ['internal/README.md']):
            self.assertEqual(self.classify(paths), 'all', paths)

    def test_main_reads_a_file_and_prints_only_the_reason(self):
        with tempfile.TemporaryDirectory() as tmp:
            for content, expect in (('docs/a.md\nCONTRIBUTING.md\n', 'docs-only'), ('internal/x/x.go\n', 'packages'),
                                    ('scripts/ci_changes.py\ninternal/x/x.go\n', 'all'), ('', 'all'), ('\n\n', 'all')):
                path = Path(tmp, 'files')
                path.write_text(content)
                self.assertEqual(self.run_main(['affected', '--files-from', str(path)]), 'affected_reason=%s\n' % expect)

    def test_any_error_while_classifying_fails_closed_with_exit_zero(self):
        import unittest.mock as mock
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp, 'files')
            path.write_text('docs/a.md\n')
            for error in (KeyError('boom'), ValueError('boom'), RuntimeError('boom')):
                with mock.patch.object(CH, 'classify_by_path', side_effect=error):
                    self.assertEqual(self.run_main(['affected', '--files-from', str(path)]), 'affected_reason=all\n')
            with mock.patch.object(CH, 'load_reads', side_effect=KeyError('kind')):
                self.assertEqual(self.run_main(['affected', '--files-from', str(path)]), 'affected_reason=all\n')

    def test_an_unreadable_file_fails_closed_with_exit_zero(self):
        out = self.run_main(['affected', '--files-from', '/nonexistent/list'])
        self.assertEqual(out, 'affected_reason=all\n')

    def run_main(self, argv):
        buf = io.StringIO()
        with contextlib.redirect_stdout(buf):
            self.assertEqual(CH.main(argv), 0)
        return buf.getvalue()


@unittest.skipUnless(shutil.which('go') and shutil.which('git'), 'needs go and git')
class AffectedGitTests(unittest.TestCase):
    FILES = {
        'go.mod': 'module example.com/m\n\ngo 1.21\n',
        'internal/a/a.go': 'package a\n\nfunc A() int { return 1 }\n',
        'internal/b/b.go': 'package b\n\nimport "example.com/m/internal/a"\n\nfunc B() int { return a.A() }\n',
        'internal/c/c.go': 'package c\n',
        'internal/c/c_test.go': 'package c\n\nimport (\n\t"testing"\n\n\t"example.com/m/internal/b"\n)\n\nfunc TestC(t *testing.T) { _ = b.B() }\n',
        'internal/e/e.go': 'package e\n',
        'docs/a.md': '# a\n',
    }

    def git(self, cwd, *args):
        env = dict(os.environ, GIT_AUTHOR_NAME='t', GIT_AUTHOR_EMAIL='t@t', GIT_COMMITTER_NAME='t',
                   GIT_COMMITTER_EMAIL='t@t', GIT_CONFIG_GLOBAL='/dev/null', GIT_CONFIG_SYSTEM='/dev/null')
        subprocess.run(['git', '-c', 'commit.gpgsign=false', *args], cwd=cwd, env=env, check=True, capture_output=True)

    def write(self, tmp, files):
        for name, text in files.items():
            target = Path(tmp, name)
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text(text)

    def merged(self, tmp, changes, remove=()):
        self.git(tmp, 'init', '-q', '-b', 'main')
        self.write(tmp, self.FILES)
        self.git(tmp, 'add', '.')
        self.git(tmp, 'commit', '-q', '-m', 'base')
        self.git(tmp, 'checkout', '-q', '-b', 'topic')
        self.write(tmp, changes)
        for name in remove:
            Path(tmp, name).unlink()
        self.git(tmp, 'add', '-A')
        self.git(tmp, 'commit', '-q', '-m', 'topic')
        self.git(tmp, 'checkout', '-q', 'main')
        self.write(tmp, {'internal/e/moved.go': 'package e\n'})  # the base moved on: not part of the PR
        self.git(tmp, 'add', '.')
        self.git(tmp, 'commit', '-q', '-m', 'main moved')
        self.git(tmp, 'merge', '-q', '--no-ff', '-m', 'merge', 'topic')

    def result(self, changes, event='pull_request', remove=()):
        with tempfile.TemporaryDirectory() as tmp:
            self.merged(tmp, changes, remove)
            return CH.compute_affected(event, cwd=tmp, reads=[])

    def test_a_changed_package_selects_itself_and_its_reverse_dependencies_including_test_importers(self):
        self.assertEqual(self.result({'internal/a/a.go': 'package a\n\nfunc A() int { return 2 }\n'}),
                         ('packages', ['example.com/m/internal/a', 'example.com/m/internal/b', 'example.com/m/internal/c']))

    def test_a_leaf_selects_only_itself_and_the_base_side_change_is_ignored(self):
        self.assertEqual(self.result({'internal/c/c.go': 'package c\n\nvar X = 1\n'}),
                         ('packages', ['example.com/m/internal/c']))

    def test_docs_only_and_mixed_changes(self):
        self.assertEqual(self.result({'docs/b.md': 'x\n'}), ('docs-only', []))
        self.assertEqual(self.result({'docs/b.md': 'x\n', 'internal/e/e.go': 'package e\n\nvar Y = 1\n'}),
                         ('packages', ['example.com/m/internal/e']))

    def test_non_package_changes_and_deleted_packages_give_all(self):
        self.assertEqual(self.result({'go.mod': 'module example.com/m\n\ngo 1.22\n'}), ('all', []))
        self.assertEqual(self.result({'scripts/ci_changes.py': 'x\n'}), ('all', []))
        self.assertEqual(self.result({}, remove=['internal/c/c.go', 'internal/c/c_test.go']), ('all', []))

    def test_every_other_event_and_a_non_merge_checkout_give_all(self):
        for event in ('push', 'workflow_dispatch', 'schedule', ''):
            self.assertEqual(self.result({'docs/b.md': 'x\n'}, event), ('all', []), event)
        with tempfile.TemporaryDirectory() as tmp:
            self.git(tmp, 'init', '-q', '-b', 'main')
            self.write(tmp, self.FILES)
            self.git(tmp, 'add', '.')
            self.git(tmp, 'commit', '-q', '-m', 'only')
            self.assertEqual(CH.compute_affected('pull_request', cwd=tmp, reads=[]), ('all', []))
        self.assertEqual(CH.compute_affected('pull_request', cwd='/nonexistent-dir-for-ci-changes'), ('all', []))

    def test_a_package_that_does_not_compile_to_a_graph_fails_closed(self):
        self.assertEqual(self.result({'internal/e/e.go': 'package e\n\nimport "example.com/m/internal/missing"\n'}), ('all', []))

    def test_any_unexpected_error_fails_closed_with_exit_zero(self):
        broken = ['{"ImportPath": "example.com/m/internal/e"}',  # no Dir
                  '{"ImportPath": ', '', '[1, 2]']
        for output in broken:
            with mock.patch.object(CH, 'go_list', side_effect=lambda cwd=None, out=output: CH.parse_go_list(out)):
                self.assertEqual(self.result({'internal/e/e.go': 'package e\n\nvar Q = 1\n'}), ('all', []), output)
        with mock.patch.object(CH, 'go_list', return_value=[{'ImportPath': 'x'}]):
            self.assertEqual(self.result({'internal/e/e.go': 'package e\n\nvar Q = 1\n'}), ('all', []))
        with mock.patch.object(CH, 'changed_paths', side_effect=KeyError('boom')):
            self.assertEqual(self.result({'internal/e/e.go': 'package e\n\nvar Q = 1\n'}), ('all', []))

    def test_main_prints_the_two_lines_and_never_a_failure_status(self):
        with tempfile.TemporaryDirectory() as tmp:
            self.merged(tmp, {'internal/e/e.go': 'package e\n\nvar Z = 1\n'})
            Path(tmp, 'reads.json').write_text('{"reads": []}')
            cwd = os.getcwd()
            os.chdir(tmp)
            try:
                buf = io.StringIO()
                with contextlib.redirect_stdout(buf), mock.patch.object(CH, 'READS_FILE', str(Path(tmp, 'reads.json'))):
                    self.assertEqual(CH.main(['affected', '--event', 'pull_request']), 0)
                    self.assertEqual(CH.main(['affected', '--event', 'push']), 0)
                    self.assertEqual(CH.main(['--event', 'pull_request']), 0)
            finally:
                os.chdir(cwd)
        self.assertEqual(buf.getvalue(), 'affected=example.com/m/internal/e\naffected_reason=packages\n'
                         'affected=./...\naffected_reason=all\ndocs_only=false\n')


if __name__ == '__main__':
    unittest.main()
