# SPDX-License-Identifier: EUPL-1.2
import importlib.util
import json
import random
import re
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('ci_shards', ROOT / 'scripts/ci_shards.py')
shards = importlib.util.module_from_spec(spec)
spec.loader.exec_module(shards)

WEIGHTS = {'TestTidyV3RetirementA': 50, 'TestTidyV3SixRealisticB': 49, 'TestMid': 20}
TESTS = ['TestTidyV3RetirementA', 'TestTidyV3SixRealisticB', 'TestMid'] + ['TestSmall%02d' % i for i in range(30)]


class PartitionTests(unittest.TestCase):
    def plan(self, tests=TESTS, weights=WEIGHTS, count=4):
        return shards.partition(tests, weights, 1, count)

    def test_deterministic_regardless_of_input_order(self):
        expected = self.plan()
        for seed in range(5):
            shuffled = list(TESTS)
            random.Random(seed).shuffle(shuffled)
            self.assertEqual(self.plan(shuffled), expected)

    def test_complete_without_duplicates(self):
        flat = [name for shard in self.plan() for name in shard]
        self.assertEqual(sorted(flat), sorted(TESTS))
        self.assertEqual(len(flat), len(set(flat)))

    def test_input_duplicates_are_collapsed(self):
        flat = [name for shard in self.plan(TESTS + TESTS) for name in shard]
        self.assertEqual(sorted(flat), sorted(TESTS))

    def test_two_heaviest_tests_land_in_different_shards(self):
        where = {name: i for i, shard in enumerate(self.plan()) for name in shard}
        self.assertNotEqual(where['TestTidyV3RetirementA'], where['TestTidyV3SixRealisticB'])

    def test_unlisted_test_is_still_assigned_with_default_weight(self):
        plan = self.plan(TESTS + ['TestBrandNew'])
        self.assertEqual(sum('TestBrandNew' in shard for shard in plan), 1)

    def test_shards_are_sorted_by_name_for_stable_output(self):
        for shard in self.plan():
            self.assertEqual(shard, sorted(shard))

    def test_loads_are_balanced_within_the_heaviest_test(self):
        plan = self.plan()
        loads = [sum(WEIGHTS.get(name, 1) for name in shard) for shard in plan]
        self.assertLessEqual(max(loads) - min(loads), max(WEIGHTS.values()))

    def test_more_shards_than_tests_leaves_empty_shards_that_match_nothing(self):
        plan = shards.partition(['TestA'], {}, 1, 3)
        self.assertEqual(sum(len(s) for s in plan), 1)
        self.assertEqual(shards.regex([]), '^$')

    def test_regex_matches_exactly_the_shard(self):
        for shard in self.plan():
            pattern = re.compile(shards.regex(shard))
            for name in TESTS:
                self.assertEqual(bool(pattern.search(name)), name in shard)

    def test_no_prefix_test_is_swallowed_by_a_shorter_name(self):
        plan = shards.partition(['TestA', 'TestAB'], {}, 1, 1)
        self.assertEqual(re.compile(shards.regex(plan[0])).fullmatch('TestAB') is not None, True)
        self.assertIsNone(re.compile(shards.regex(['TestA'])).search('TestAB'))


class VerifyTests(unittest.TestCase):
    def test_exact_cover_passes(self):
        shards.verify(['TestA', 'TestB'], [['TestA'], ['TestB']])

    def test_missing_test_fails(self):
        with self.assertRaisesRegex(SystemExit, 'not run by any shard.*TestB'):
            shards.verify(['TestA', 'TestB'], [['TestA'], []])

    def test_extra_test_fails(self):
        with self.assertRaisesRegex(SystemExit, 'unknown.*TestZ'):
            shards.verify(['TestA'], [['TestA', 'TestZ']])

    def test_empty_full_list_fails(self):
        with self.assertRaisesRegex(SystemExit, 'no tests'):
            shards.verify([], [[], []])

    def test_test_run_twice_fails(self):
        with self.assertRaisesRegex(SystemExit, 'more than one shard.*TestA'):
            shards.verify(['TestA'], [['TestA'], ['TestA']])


class PackagesTests(unittest.TestCase):
    def test_exclusion_removes_only_the_sharded_package(self):
        listed = ['m', 'm/internal/a', 'm/internal/workspace']
        self.assertEqual(shards.exclude(listed, 'm/internal/workspace'), ['m', 'm/internal/a'])

    def test_exclusion_fails_closed_when_the_sharded_package_is_not_listed(self):
        with self.assertRaisesRegex(SystemExit, 'not in the package list'):
            shards.exclude(['m', 'm/internal/a'], 'm/internal/workspace')

    def test_exclusion_fails_when_nothing_remains(self):
        with self.assertRaisesRegex(SystemExit, 'no other packages'):
            shards.exclude(['m/internal/workspace'], 'm/internal/workspace')


class WeightsFileTests(unittest.TestCase):
    def test_weights_file_is_well_formed(self):
        data = json.loads((ROOT / 'scripts/ci-test-weights.json').read_text())
        self.assertEqual(data['package'], './internal/workspace')
        self.assertGreater(data['default'], 0)
        for name, weight in data['weights'].items():
            self.assertRegex(name, r'^Test\w*$')
            self.assertGreater(weight, 0)

    def test_list_parser_keeps_only_test_names(self):
        output = 'TestA\nTestB\nok  \texample/pkg\t0.012s\n'
        self.assertEqual(shards.parse_list(output), ['TestA', 'TestB'])


if __name__ == '__main__':
    unittest.main()
