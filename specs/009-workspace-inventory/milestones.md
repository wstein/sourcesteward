# Spec 009 implementation and acceptance milestones

These are bounded implementation/review work items within Spec 009, not changes to
its functional scope. Each work item uses focused Conventional Commits and receives
its own PR. The shared contract, plan, tasks and analysis remain prerequisites.
Git and jj retain the same Tier 1 acceptance requirements.

## Delivery sequence

| Milestone | Work item and review boundary | Native CLI cases | Acceptance gate |
| --- | --- | ---: | --- |
| M1 | Inventory envelope, CLI/schema integration and Git state/reference predicates | 8 | Known true/false exit 0; operational error exits 1; human output and machine contract agree. |
| M2 | Git registered worktrees, file scopes and helper suppression | 4 | Missing registration retained, exact unusual filenames, selected dirty state, denied clean/process filters with unknown result. |
| M3 | Native jj reference predicates in standalone and colocated layouts | 8 | Absent/existing bookmarks and false/true full-identity ancestry; no Git fallback or snapshots. |
| M4 | Native jj stored files, workspace registration and qualified disk observations | 16 | Literal stored paths, secondary/missing workspace registrations, explicit unknown disk state and unsupported staging. |
| Integrated handoff | Independent review, full source/schema/race checks and minimum/current conformance | **36 per lane** | All milestones together, plus adapter/unit/schema coverage and every mapped requirement; missing execution blocks acceptance. |

Milestones help review and troubleshooting; they do not permit partial coverage to
be marked as completed Spec 009. PRs may be stacked where the shared model/CLI is a
dependency. A milestone is verified only after its actual integrated native tests
pass and evidence is recorded in the verification record. At this document's
creation the integrated command was not available and none of these native cases
had passed; the pre-implementation CLI failed the first query as expected.

## Stable native case IDs

The authoritative executable manifest is `CASE_IDS` in
[scripts/smoke-inventory.py](../../scripts/smoke-inventory.py). Each case checks
byte/mode-identical disposable fixture state, including Git index, jj operation
stores and dirty files. Missing backends or required sandbox enforcement fail;
there is no acceptance skip. Expected and completed case-ID sets must match.

- M1: `M1.clean`, `M1.dirty-false`, `M1.ref-absent`, `M1.ref-present`,
  `M1.ancestor-false`, `M1.ancestor-true`, `M1.human`, `M1.error`.
- M2: `M2.files-workspaces`, `M2.dirty-true`, `M2.literal-filter`,
  `M2.denied-filters`.
- M3: for each of `standalone` and `colocated`,
  `M3.<layout>.ref-absent`, `M3.<layout>.ref-present`,
  `M3.<layout>.ancestor-false`, `M3.<layout>.ancestor-true`.
- M4: for each of `standalone` and `colocated`,
  `M4.<layout>.stored`, `M4.<layout>.literal-stored`,
  `M4.<layout>.disk-unknown`, `M4.<layout>.clean-unknown`,
  `M4.<layout>.staging-unsupported`, `M4.<layout>.workspaces`,
  `M4.<layout>.secondary`, `M4.<layout>.missing`.

The earlier 33-case count gained three cases: one literal stored-path case per jj
layout and one Git clean/process-filter denial case. The total is now 36, with no
case deferred or removed.

## Running a milestone

After building the current source CLI and installing the pinned backend lane:

```sh
python3 -B scripts/smoke-inventory.py --milestone M1
python3 -B scripts/smoke-inventory.py --milestone M2
python3 -B scripts/smoke-inventory.py --milestone M3
python3 -B scripts/smoke-inventory.py --milestone M4
python3 -B scripts/smoke-inventory.py
```

A selected milestone runs only that group's observations/assertions. Disposable
fixture preparation still uses both native backends and requires the sandbox;
selection does not weaken prerequisites. CI deliberately omits `--milestone` and
requires all 36 cases in each minimum/current backend lane. Milestone runs alone
do not substitute for full source tests, schemas, race checks, native adapter
failure/concurrency/ownership coverage, or integrated review.
