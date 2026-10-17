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
        architecture.validate_sources({'internal/release/vcs_open_unix.go': 'package release\n\nimport "syscall"\n', 'internal/release/vcs_git.go': 'package release\n\nimport "os/exec"\n', 'internal/release/attest.go': 'package release\n\nimport "os/exec"\n'})
        architecture.validate_sources({'internal/release/x.go': 'package release\n\nimport "net/url"\n'})

    def test_net_is_absent_from_the_dependencies_of_the_local_packages(self):
        prefix = 'github.com/wstein/sourcesteward/'
        architecture.validate_dependencies({prefix + 'internal/release': ['fmt', 'os'], prefix + 'internal/cli': ['net/http']})
        for package in ('release', 'inspection', 'workspace'):
            for leaked in ('net', 'net/http', 'crypto/tls', 'golang.org/x/net/http2'):
                with self.assertRaises(ValueError, msg=package + leaked): architecture.validate_dependencies({prefix + 'internal/' + package: ['fmt', leaked]})
