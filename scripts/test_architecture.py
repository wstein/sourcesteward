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
