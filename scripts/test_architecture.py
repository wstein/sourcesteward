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
            with self.assertRaises(ValueError, msg=call): architecture.validate_sources({'internal/release/check.go': 'package release\n\nfunc f() {\n\t' + call + '\n}\n'})
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
