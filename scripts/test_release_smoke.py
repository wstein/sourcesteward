# SPDX-License-Identifier: EUPL-1.2
"""Source checks for the opt-in release binary conformance harness; no real build."""
import ast
import copy
import importlib.util
from pathlib import Path
import unittest


class ReleaseSmokeTests(unittest.TestCase):
    def test_effect_assertions_reject_single_rule_mutants(self):
        path = Path(__file__).with_name('smoke-release.py')
        spec = importlib.util.spec_from_file_location('release_smoke', path)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        valid = {'command': 'release-plan', 'status': 'ok',
                 'boundary': {'effects': {'network': False, 'writes': False,
                                          'project_commands': False}},
                 'network': [], 'verdict': 'incomplete',
                 'gates': [{'outcome': 'not-run'}] * 3}
        module.assert_result(valid, 'release-plan', 'ok')
        for field in ('network', 'writes', 'project_commands'):
            mutant = copy.deepcopy(valid)
            mutant['boundary']['effects'][field] = True
            with self.assertRaises(AssertionError):
                module.assert_result(mutant, 'release-plan', 'ok')
        mutant = copy.deepcopy(valid)
        mutant['gates'][0]['outcome'] = 'pass'
        with self.assertRaises(AssertionError):
            module.assert_result(mutant, 'release-plan', 'ok')

    def test_harness_checks_read_only_contracts(self):
        path = Path(__file__).with_name('smoke-release.py')
        source = path.read_text()
        ast.parse(source)
        for marker in ('--ignore-working-copy', 'operation', 'snapshot',
                       'release-plan', "'check', '--commit'", 'unsupported-backend',
                       'main-ref-absent', '--no-colocate', '--colocate'):
            self.assertIn(marker, source)
        self.assertNotIn('go build', source)
        self.assertNotIn('make build', source)


if __name__ == '__main__':
    unittest.main()
