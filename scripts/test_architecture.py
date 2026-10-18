# SPDX-License-Identifier: EUPL-1.2
import importlib.util
from pathlib import Path
import unittest
spec = importlib.util.spec_from_file_location('architecture', Path(__file__).with_name('check-architecture.py'))
architecture = importlib.util.module_from_spec(spec)
spec.loader.exec_module(architecture)
class ArchitectureTests(unittest.TestCase):
    def test_operation_boundary(self):
        prefix='github.com/wstein/sourcesteward/'
        architecture.validate([prefix+'internal/cli '+prefix+'internal/inspection'+' '+prefix+'internal/workspace'+' '+prefix+'internal/hosting'])
        for dependency in ('os', 'os/exec', 'net/http', prefix+'internal/unreviewed'):
            with self.assertRaises(ValueError): architecture.validate([prefix+'internal/cli '+dependency])

    def test_cli_may_import_the_release_operations(self):
        prefix='github.com/wstein/sourcesteward/'
        architecture.validate([prefix+'internal/cli '+prefix+'internal/release'])
        with self.assertRaises(ValueError): architecture.validate([prefix+'internal/cli '+prefix+'internal/releasex'])

    def test_release_starts_processes_only_in_reviewed_files(self):
        clean = 'package release\n\nimport "os"\n'
        spawning = 'package release\n\nimport (\n\t"os"\n\t"os/exec"\n)\n'
        for name in ('vcs_git.go', 'vcs_jj.go', 'attest.go', 'build.go'):
            architecture.validate_sources({'internal/release/' + name: spawning})
        architecture.validate_sources({'internal/release/check.go': clean, 'internal/release/check_test.go': spawning})
        for name in ('check.go', 'validate.go', 'worktree.go', 'record.go', 'vcs.go', 'vcsgit.go', 'git_vcs.go'):
            with self.assertRaises(ValueError, msg=name): architecture.validate_sources({'internal/release/' + name: spawning})
        for call in ('os.StartProcess(a, b, c)', 'syscall.ForkExec(a, b, c)', 'syscall.Exec(a, b, c)'):
            with self.assertRaises(ValueError, msg=call): architecture.validate_sources({'internal/release/check.go': 'package release\n\nimport (\n\t"os"\n\t"syscall"\n)\n\nfunc f() {\n\t' + call + '\n}\n'})
        # A mention in a comment or string is not a process start.
        architecture.validate_sources({'internal/release/check.go': 'package release\n\n// uses os/exec elsewhere\nvar s = "os/exec"\n'})

    def test_network_packages_are_imported_only_by_hosting(self):
        for imported in ('net', 'net/http', 'net/rpc'):
            source = 'package x\n\nimport "' + imported + '"\n'
            architecture.validate_sources({'internal/hosting/transport.go': source, 'internal/hosting/transport_test.go': source, 'internal/cli/x_test.go': source})
            for path in ('internal/release/check.go', 'internal/inspection/x.go', 'internal/workspace/x.go', 'internal/cli/x.go', 'cmd/stw/main.go'):
                with self.assertRaises(ValueError, msg=imported + ' in ' + path): architecture.validate_sources({path: source})
        # net/url only parses; it opens no connection.
        architecture.validate_sources({'internal/release/check.go': 'package x\n\nimport "net/url"\n'})

    def test_synthetic_evasions_are_caught(self):
        def release(text, name='check.go'):
            return {'internal/release/' + name: 'package release\n\n' + text}
        evasions = {
            'aliased os import': release('import o "os"\n\nfunc f() { o.StartProcess("a", nil, nil) }\n'),
            'dot os import': release('import . "os"\n\nfunc f() { StartProcess("a", nil, nil) }\n'),
            'aliased exec import': release('import x "os/exec"\n'),
            'blank exec import': release('import _ "os/exec"\n'),
            'syscall RawSyscall': release('import "syscall"\n\nfunc f() { syscall.RawSyscall(0, 0, 0, 0) }\n'),
            'aliased syscall': release('import sc "syscall"\n'),
            'x/sys unix': release('import "golang.org/x/sys/unix"\n'),
            'crypto/tls': release('import "crypto/tls"\n'),
            'x/net http2': release('import "golang.org/x/net/http2"\n'),
            'net/http/httptrace': release('import "net/http/httptrace"\n'),
            'net/textproto': release('import "net/textproto"\n'),
            'block import with alias': release('import (\n\t"fmt"\n\th "net/http"\n)\n'),
            'tls in inspection': {'internal/inspection/x.go': 'package inspection\n\nimport "crypto/tls"\n'},
            'plugin': release('import "plugin"\n'),
        }
        for name, sources in evasions.items():
            with self.assertRaises(ValueError, msg=name): architecture.validate_sources(sources)
        # The reviewed files may use them, and net/url remains allowed.
        architecture.validate_sources({'internal/release/vcs_git.go': 'package release\n\nimport "os/exec"\n', 'internal/release/attest.go': 'package release\n\nimport "os/exec"\n'})
        # syscall: only the O_ open flags in the file-open helpers, anything in build.go (process groups).
        opener = 'package release\n\nimport (\n\t"os"\n\t"syscall"\n)\n\nvar f = os.O_RDONLY | syscall.O_NOFOLLOW | syscall.O_NONBLOCK\n'
        architecture.validate_sources({'internal/release/openfile_unix.go': opener, 'internal/release/build.go': 'package release\n\nimport "syscall"\n\nvar _ = syscall.Kill\n'})
        for name, text in {
            'internal/release/openfile_unix.go': opener + 'var _ = syscall.RawSyscall\n',
            'internal/release/openfile_unix.go ': None,
            'internal/release/vcs_open_unix.go': opener,
            'internal/release/vcs_git.go': opener,
            'internal/release/attest.go': 'package release\n\nimport "syscall"\n',
        }.items():
            if text is None: continue
            with self.assertRaises(ValueError, msg=name): architecture.validate_sources({name: text})
        with self.assertRaises(ValueError): architecture.validate_sources({'internal/release/openfile_unix.go': 'package release\n\nimport sc "syscall"\n\nvar _ = sc.Kill\n'})
        with self.assertRaises(ValueError): architecture.validate_sources({'internal/release/openfile_unix.go': 'package release\n\nimport . "syscall"\n\nvar _ = O_NOFOLLOW\n'})
        architecture.validate_sources({'internal/release/x.go': 'package release\n\nimport "net/url"\n'})

    def test_net_is_absent_from_the_dependencies_of_the_local_packages(self):
        prefix = 'github.com/wstein/sourcesteward/'
        architecture.validate_dependencies({prefix + 'internal/release': ['fmt', 'os'], prefix + 'internal/cli': ['net/http']})
        for package in ('release', 'inspection', 'workspace'):
            for leaked in ('net', 'net/http', 'crypto/tls', 'golang.org/x/net/http2'):
                with self.assertRaises(ValueError, msg=package + leaked): architecture.validate_dependencies({prefix + 'internal/' + package: ['fmt', leaked]})

    def test_release_uses_hosting_boundary_without_network_bypass(self):
        prefix = architecture.MODULE
        release, hosting = prefix + 'internal/release', prefix + 'internal/hosting'
        helper = 'example.org/hosting-client'
        graph = {release: [hosting], hosting: [helper, 'os/exec'],
                 helper: ['net/http', 'os/exec'], 'net/http': ['net']}
        architecture.validate_dependencies(graph)
        architecture.validate_release_graph(graph)
        for bypass in ('net/http', helper):
            mutant = dict(graph)
            mutant[release] = [hosting, bypass]
            with self.assertRaises(ValueError, msg=bypass):
                architecture.validate_dependencies(mutant)
        # Local packages cannot borrow hosting's boundary or reach network via another package.
        for local in ('internal/inspection', 'internal/workspace'):
            mutant = dict(graph)
            mutant[prefix + local] = [hosting]
            with self.assertRaises(ValueError, msg=local):
                architecture.validate_dependencies(mutant)
        # Shared dependencies do not become trusted merely because hosting also uses them.
        mutant = dict(graph)
        mutant[release] = [hosting, prefix + 'internal/release/sub']
        mutant[prefix + 'internal/release/sub'] = [helper]
        with self.assertRaises(ValueError):
            architecture.validate_dependencies(mutant)
        with self.assertRaises(ValueError):
            architecture.validate_release_graph(mutant)

    def test_unsafe_and_linkname_are_refused_in_release(self):
        for name in ('zz.go', 'check.go', 'vcs_git.go', 'build.go', 'openfile_unix.go'):
            path = 'internal/release/' + name
            with self.assertRaises(ValueError, msg=name): architecture.validate_sources({path: 'package release\n\nimport _ "unsafe"\n\n//go:linkname zzfork syscall.forkExec\nfunc zzfork()\n'})
            with self.assertRaises(ValueError, msg=name): architecture.validate_sources({path: 'package release\n\nimport "unsafe"\n\nvar _ unsafe.Pointer\n'})
            with self.assertRaises(ValueError, msg=name): architecture.validate_sources({path: 'package release\n\n//go:linkname zzfork os.runtime_beforeFork\nfunc zzfork()\n'})
        # Mentions in comments and strings are not directives.
        architecture.validate_sources({'internal/release/check.go': 'package release\n\n// uses unsafe and go:linkname elsewhere\nvar s = "unsafe"\n'})
        # Other packages are outside this rule.
        architecture.validate_sources({'internal/inspection/x.go': 'package inspection\n\nimport "unsafe"\n'})

    def test_release_packages_have_no_foreign_sources(self):
        prefix = 'github.com/wstein/sourcesteward/'
        clean = {'ImportPath': prefix + 'internal/release', 'GoFiles': ['a.go']}
        architecture.validate_release_packages([clean, {'ImportPath': prefix + 'internal/release/ok', 'GoFiles': ['b.go']}])
        for key, value in {'SFiles': ['zz.s'], 'CgoFiles': ['zz.go'], 'CFiles': ['zz.c'], 'CXXFiles': ['zz.cc'], 'HFiles': ['zz.h'], 'MFiles': ['zz.m'], 'FFiles': ['zz.f'], 'SwigFiles': ['zz.swig'], 'SwigCXXFiles': ['zz.swigcxx'], 'SysoFiles': ['zz.syso']}.items():
            for name in ('internal/release', 'internal/release/zzsub', 'internal/release/zzc'):
                package = {'ImportPath': prefix + name, 'GoFiles': ['a.go'], key: value}
                with self.assertRaises(ValueError, msg=key + name): architecture.validate_release_packages([clean, package])

    def test_release_may_import_only_reviewed_module_packages(self):
        prefix = 'github.com/wstein/sourcesteward/'
        graph = {
            prefix + 'internal/release': ['fmt', 'os', 'os/exec', prefix.rstrip('/'), prefix + 'internal/inspection', prefix + 'internal/release/sub'],
            prefix + 'internal/release/sub': ['fmt'],
            prefix + 'internal/inspection': ['os/exec', 'fmt'],
            prefix.rstrip('/'): ['embed'],
        }
        architecture.validate_release_graph(graph)
        for extra in ('internal/zzexec', 'internal/workspace', 'internal/hosting/unreviewed', 'internal/cli'):
            leaked = dict(graph)
            leaked[prefix + 'internal/release'] = graph[prefix + 'internal/release'] + [prefix + extra]
            leaked[prefix + extra] = ['fmt']
            with self.assertRaises(ValueError, msg=extra): architecture.validate_release_graph(leaked)
        # os/exec reached through a package that is not reviewed.
        hidden = dict(graph)
        hidden[prefix + 'internal/release/sub'] = ['fmt', 'os/exec']
        with self.assertRaises(ValueError): architecture.validate_release_graph(hidden)
        deep = dict(graph)
        deep[prefix + 'internal/release/sub'] = ['fmt', prefix + 'internal/release/sub/deeper']
        deep[prefix + 'internal/release/sub/deeper'] = ['os/exec']
        with self.assertRaises(ValueError): architecture.validate_release_graph(deep)

        sub_leak = dict(graph)
        sub_leak[prefix + 'internal/release/sub'] = ['fmt', prefix + 'internal/zzexec']
        sub_leak[prefix + 'internal/zzexec'] = ['fmt']
        with self.assertRaises(ValueError): architecture.validate_release_graph(sub_leak)

    def test_record_writer_has_only_descriptor_relative_filesystem_exception(self):
        writer = 'internal/release/openfile_write_unix.go'
        source = 'package release\nimport "golang.org/x/sys/unix"\nfunc writeRecordExclusive() { _, _ = unix.Openat(0, "x", unix.O_RDONLY, 0); _ = unix.Mkdirat(0, "x", 0755) }\n'
        architecture.validate_sources({writer: source})
        for selector in ('ForkExec', 'Exec', 'RawSyscall', 'Syscall', 'Socket', 'Kill', 'O_TRUNC'):
            with self.assertRaises(ValueError, msg=selector):
                architecture.validate_sources({writer: source + 'var _ = unix.' + selector + '\n'})
        for function in ('Openat', 'Mkdirat'):
            for form in ('var f = unix.{}', 'func capture() {{ f := unix.{}; _ = f }}', 'func returned() any {{ return unix.{} }}', 'func consume(any) {{}}\nfunc passed() {{ consume(unix.{}) }}', 'var table = []any{{unix.{}}}'):
                with self.assertRaises(ValueError, msg=form):
                    architecture.validate_sources({writer: source + form.format(function) + '\n'})
        for wrong in ('internal/release/openfile_unix.go', 'internal/release/check.go', 'internal/release/sub/openfile_write_unix.go'):
            with self.assertRaises(ValueError): architecture.validate_sources({wrong: source})
        for alias in ('u', '.', '_'):
            with self.assertRaises(ValueError): architecture.validate_sources({writer: source.replace('import "', 'import ' + alias + ' "', 1)})

    def test_checkout_platform_metadata_exception_is_type_only(self):
        source = 'package release\nimport "syscall"\nvar _ *syscall.Stat_t\n'
        for platform in ('linux', 'darwin'):
            path = 'internal/release/build_checkout_view_' + platform + '.go'
            architecture.validate_sources({path: source})
            for selector in ('Kill', 'Exec', 'ForkExec', 'Syscall', 'RawSyscall', 'O_TRUNC'):
                with self.assertRaises(ValueError, msg=selector):
                    architecture.validate_sources({path: source + 'var _ = syscall.' + selector + '\n'})
            for alias in ('s', '.', '_'):
                with self.assertRaises(ValueError):
                    architecture.validate_sources({path: source.replace('import "', 'import ' + alias + ' "', 1)})
        for path in ('internal/release/build_checkout_view.go', 'internal/release/build_checkout_view_other.go', 'internal/release/sub/build_checkout_view_linux.go'):
            with self.assertRaises(ValueError): architecture.validate_sources({path: source})

    def test_release_foreign_helpers_cannot_forward_capabilities(self):
        prefix = 'github.com/wstein/sourcesteward/'
        release = prefix + 'internal/release'
        for target in ('syscall', 'unsafe', 'net/http', 'os/exec'):
            for owner in (release, release + '/sub', prefix + 'internal/inspection'):
                graph = {release: [owner] if owner != release else ['example.org/helper'], owner: ['example.org/helper'], 'example.org/helper': [target]}
                with self.assertRaises(ValueError): architecture.validate_release_graph(graph)
        architecture.validate_release_graph({release: ['golang.org/x/sys/unix'], 'golang.org/x/sys/unix': ['syscall', 'unsafe']})
        for file in ('internal/release/record.go', 'internal/release/sub/helper.go', 'internal/release/openfile_write_unix.go'):
            with self.assertRaises(ValueError): architecture.validate_sources({file: 'package release\nimport "example.org/helper"\n'})
