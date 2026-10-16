# Pre-implementation analysis

## Scope reduction applied

Source: round 5 part B (independent Opus review of revision 9ce514c). The maintainer decided to apply it, including the reduced conformance procedure (J0, J2, J2b, J3, J4, J7; deciding sentinels `pre-commit`, `commit-msg`, `pre-push`). Applied in commits 0c7b52d (spec), beb6e14 (plan, research, data model, contract) and the tasks commit that follows; the proposal text below is kept as the record of what was cut. Differences from the proposal: FR numbers were renumbered (FR-001 to FR-016); `effective_scope` replaces the level list; the kept finding set is the one in the spec's findings table (15 codes); tasks were renumbered T001 to T024 and every task keeps its FR, US or SC tags. Rounds 1 to 5 below refer to the pre-reduction numbering. Findings of rounds 1 to 5 about cut scope are closed by removal.

**Recommendation.** Reduce to the smallest feature that answers the motivating question: for a WorkHarbor-shaped repository, which commit and push hooks would Git run from the effective `core.hooksPath`, and do `jj commit` and `jj git push` run them, at which evidence level. Keep the jj claims `jj-commit: no` at `documented` and `jj-push: unknown` at `unverified`, ship with a reduced conformance procedure, and defer hook hygiene (safety, provenance, inventory) to a later spec.

**Retained core.**
- FR-001 without `--strict`; FR-002, FR-005, FR-006 (ownership dispatch, ownership gate, discovered-directory check, jj probe and `--version` only).
- FR-003 reduced to the effective value and its scope from the one `--get-regexp` read, plus `rev-parse --git-path hooks`.
- FR-004 reduced to `lstat`, `readlink` and `access` on the six operation hooks (`pre-commit`, `prepare-commit-msg`, `commit-msg`, `post-commit`, `pre-push`, `reference-transaction`).
- FR-007 classification order; FR-009 with states `active`, `ignored`, `not-runnable`, `disabled-by-path`, `absent` (no `unknown`, no `source`); FR-010, FR-011, FR-012 (isolation), FR-013 (reduced limits), FR-014, FR-015, FR-016 with statuses 0, 1 and 2, FR-018, FR-019.
- Findings kept: `guards-bypassed-on-backend`, `guards-may-be-bypassed` (active guard under an `unknown` claim only), `backend-hook-behavior-unverified`, `hooks-dir-absent`, `hooks-path-null-device`, `hooks-path-empty`, `hooks-dir-unusable`, `hooks-path-invalid`, `hook-ignored`, `hook-not-runnable`, `hooks-limit`, `hooks-state-changed`, `workspace-has-no-git-configuration`, `no-active-guards` (only when `guards_evaluated`), plus one new `config-hooks-not-evaluated`.

**Cut or defer.**
- FR-008 whole (configuration-hook engine, claim `git-config-hooks`, state `unknown`, source `both`, precedence, the unknown-engine definition). Replace with one key-names read of `hook.*.event`; if a value names one of the six operation hooks, raise `config-hooks-not-evaluated` and suppress `no-active-guards`. This removes the area where rounds 3 to 5 kept finding defects.
- `--strict` and exit status 3 (removes the Spec 021 dependency and the shared error type); caller-environment reporting (replace with a fixed note); level reporting (`levels`, origins, 16-record cap, `overridden`, `extensions.worktreeConfig`); the 28-name catalog, confined listing and its open helper, unrecognized names, confinement roots, directory ownership, permission and symlink-outside facts; tracking.
- Stories and scenarios: US1-6, US1-7, US3-3, US3-4, US3-6, US4-2; US2-4 reduced (non-colocated gives `guards_evaluated: false` and `workspace-has-no-git-configuration`; no special secondary measurement).
- Findings removed: `hook-symlink-outside`, `hook-unsafe-permissions`, `hook-unsafe-owner`, `hooks-path-overridden`, `config-hooks-present`, `config-hook-incomplete`, `hooks-outside-repository`, `hooks-tracked`, `hook-name-unrecognized`, `caller-environment-overrides`.
- Data-model fields removed: HooksPath `levels`, `levels_truncated`, `overridden` (keep `effective_scope`); Configuration `worktree_config_enabled`, `caller_environment` (ConfigHooks becomes `config_hooks_detected`); Directory facts beyond `path` and `kind`; HookEntry `hook_scope`, `size`, owner, writability, `symlink.target_inside_repository`, `tracked`; OperationHook `source` and state `unknown`; limits `directory_entries`, `unrecognized_names`, `config_hooks`, `level_records`. With no directory listing, the source scan can forbid every open in `hooks_*.go`.
- Tasks: cut T010, T013, T027; replace T011 with the one-read presence check; reduce T005 (no strict, no `root.go` change), T007 (effective value, scope and gate; keep valueless/empty, include and global fixtures), T008, T009, T012, T015, T017, T018, T022, T024. 27 tasks become 23, with WI2 about half its size.

**Minimum conformance.** Shipping the two claims at `documented` and `unverified` is honest without measurement. The constitution still requires a smoke step on the minimum and current Git and jj lanes: in a colocated WorkHarbor fixture it asserts the three guards active, both claims at exactly those levels, `bypassed` true and null, and the effect sentinels (no hook ran, nothing written, jj working copy and operation log unchanged). To ever state `measured`, add the reduced procedure J0 (plain-Git control, also measuring the Git `yes` claims), J2 and J2b (relative and absolute `core.hooksPath`), J3 (`jj commit`), J4 (`jj git push` to a local bare remote with the remote-holds-the-commit precondition) and J7 (in-fixture control); deciding sentinels `pre-commit`, `commit-msg` (J3) and `pre-push` (J4); exit 2 for an invalid control or precondition, exit 1 only when a deciding outcome contradicts a `yes` or `no` claim, otherwise record and exit 0; promotion is a manual T024 edit. Drop J1, J5, J6, the `--no-verify` matrix and the configuration-hook sentinel. The reviewer recommends including this reduced procedure because `jj-push` is the most decision-relevant unknown.

## Round 0

Author draft, 2026-10-16. No independent review has run yet. Facts the draft relies on were measured with Git 2.56.0 in scratch repositories (research.md, Git measurements 1 to 14) and read from the official jj documentation (research.md, documentation evidence). Nothing about jj behavior was measured: jj is not installed in the author's environment, and the jj claims are `documented` or `unverified` by construction.

## Round 1

Independent reviewer: read-only Opus review, revision 4983930e, 2026-10-16, with scratch repositories on Git 2.56.0; jj not installed and nothing about jj measured. Verdict: NOT READY, six high, twelve medium and ten low findings. The author re-measured findings 3, 7 and 8 (the first two as stated, the third for a directory entry) before changing the artifacts; the reviewer's measurements of findings 2, 9(a), 13 and 15 and its reading of the jj `workspace add` documentation (finding 4) are accepted from the report and marked as not re-measured in research.md.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | High | jj effects statement false: `Inspect` runs many jj queries and extra failure codes | Plan DD1 and DD2, FR-006, contract Effects, T004, T006: jj ownership from the filesystem-only detection, only the sandbox probe and `--version` run |
| 2 | High | Linked worktree default hooks live in the common directory; confinement too narrow | FR-004, plan DD8, data model `common_dir`, T008 fixture |
| 3 | High | `git config` does not apply the ownership check | Re-measured (research 13); FR-005, plan DD4, T007 gate bracketing every read |
| 4 | High | Secondary jj workspaces are Git worktrees by default | FR-005, US2-4, plan DD3, J6, T016 (colocated means a `.git` entry, directory or gitfile) |
| 5 | High | Non-colocated push may read global config and store hooks; J5 vacuous | `guards_evaluated: false` and `workspace-has-no-git-configuration` wording; J5 installs store and global hooks; research |
| 6 | High | Conformance procedure not deterministic | Spec procedure: identities, per-step sentinels, push precondition, in-fixture control J7, jj-spawned git version, exit statuses 0, 1 and 2; T022 |
| 7 | Medium | Valueless `hooksPath` and `~nosuchuser` misclassified | Re-measured (research 15); state `invalid`, `hooks-path-invalid`; FR-003, DD5 |
| 8 | Medium | Kind-only classification wrong | Re-measured (research 16); evaluation order in FR-007 and DD7 |
| 9 | Medium | Configuration hooks under-specified | `command_present`, per-key scopes, `config-hook-incomplete`, claim `git-config-hooks`, engine unknown not counted, example corrected |
| 10 | Medium | No Git version source | `git version` and minimum check on the colocated path; refusal table rows |
| 11 | Medium | Process allowlist incomplete | Research allowlist rewritten; runner-recording test in T004 |
| 12 | Medium | Refusal table omits `Inspect` codes | One pass-through row; HEAD instability blocks, stated |
| 13 | Medium | `not-directory` and `unreadable` without finding | `hooks-dir-unusable`; `disabled-by-path` extended |
| 14 | Medium | Symlink and outside-directory rules tautological | Findings table and DD8 define both |
| 15 | Medium | Listing conflicts with the source scan | Single allowlisted directory-open helper, `ReadDir(257)`; wording of "never opened" |
| 16 | Medium | Evidence level semantics ambiguous | One level per claim, backend tuples, disagreeing lanes, version parsing |
| 17 | Medium | No proof method for "not opened" | inotify on Linux plus FIFO; macOS recorded as unproven |
| 18 | Medium | Man page on host is 2.47.3 | Spec and research say so; task T027 |
| 19 | Low | Levels not a limit | 16-record cap with truncation flag |
| 20 | Low | `ls-files` fragile | Run from the worktree root with literal catalog pathspecs |
| 21 | Low | Command text read into process | `--name-only` read for `.command` |
| 22 | Low | Real versus effective uid | Real user, `syscall.Access` |
| 23 | Low | Root-owned hooks flagged | Root counts as safe |
| 24 | Low | Privacy claim false | Claim restated in research and FR-003 |
| 25 | Low | Environment list incomplete | Extended; `HOME` and `XDG_CONFIG_HOME` applied |
| 26 | Low | Text example inconsistent with JSON | Example rewritten |
| 27 | Low | Edge cases without a task | Assigned in T008, T010; inert default hooks not examined, stated |
| 28 | Low | Untagged tasks | Tagged |

## Round 2

Independent reviewer: read-only Opus review, revision f1074da, 2026-10-16, with scratch repositories on Git 2.56.0 and the jj CLI reference; jj not run. Verdict: NOT READY (one high, six medium, eight low findings). The reviewer confirmed the ownership gate, the record layout of `--show-scope --show-origin -z --get-regexp` (valueless key `key NUL`, empty value `key LF NUL`), the linked-worktree paths, `--literal-pathspecs ls-files` from the worktree root, the 28-name catalog count, and that the jj path runs only the probe and `--version`.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | High | Conformance decision rule undefined for `unknown`, cross-fixture flips, relative `core.hooksPath` false negative | Spec procedure: per-fixture, per-claim rule (`unknown` recorded, never fails), only colocated fixtures J1 to J3 and J2b decide the claims, disagreement leaves `unverified`, absolute-path variant J2b |
| 2 | Medium | Colocated missing or old Git: `hooks-failed` versus `git-unavailable` | `git-unavailable` or `git-failed` everywhere |
| 3 | Medium | Limits key for level records missing | `level_records: 16` in data model and both examples |
| 4 | Medium | `jj-output-limit` unlisted | Added to FR-006 and the refusal table |
| 5 | Medium | `git-config-hooks` level inconsistent | One level, `unverified`; example agrees |
| 6 | Medium | T011 and research still read command text; `--name-only` lacked `--show-scope` | Rewritten to the two reads; scope added |
| 7 | Medium | No Git version source on the Git-owned path | One `version` read on every Git-using path |
| 8 | Medium | Unknown-engine configuration hooks vanish from guard logic | State `unknown` in operations, `guards-may-be-bypassed` raised, `no-active-guards` suppressed |
| 9 | Low-medium | Nested `.jj` markers and `managed` sorting | Innermost marker captured first; T016 test |
| 10 | Low | jj-push Git version is stw's `PATH` Git | Stated in the plan and notes |
| 11 | Low | Quickstart J-range; SC-006 | J0 to J7; J0 and J7 required |
| 12 | Low | Derivation wording | Aligned ("a claim used in the result") |
| 13 | Low | `directory` null list; `config_hooks` for `not-applicable` | Both defined |
| 14 | Low | Round-1 passages not updated (plan summary, research listing roots, status line) | Updated |
| 15 | Low | Sentinel path delivery | Absolute path baked into hooks |

## Round 3

Independent reviewer: narrow read-only Opus re-check, revision e558e58, 2026-10-16 (a first attempt was cut off by a rate limit and re-run). Verdict: NOT READY (two high, three medium, six low findings). Round-2 items 2, 3, 6, 9, 11, 13, 14 and 15 were confirmed landed; limits keys and the colocated JSON example were confirmed field by field. The author applied the resolutions below; per the guide's three-round limit they were NOT re-reviewed, so the verdict of record is NOT READY with these resolutions pending an independent check.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | High | Unknown-engine configuration hooks not carried into FR-009, contract rules, findings table, T015 | FR-009 (state `unknown`, `bypassed: null`), findings table, contract derivation rules, T015; both bypass findings may co-occur |
| 2 | High | Conformance rule self-contradictory; `jj-push` has no deciding step; `unknown` never resolves | Deciding steps J3 and J4 in J1, J2, J2b; disagreement exit 1 and `unverified`; consistent outcomes resolve `unknown` in T024; T022 and T024 carry it |
| 3 | Medium | Error codes in data model incomplete | Refers to the refusal table `error` rows |
| 4 | Medium | Directory and configuration hook sharing an event | Source `both`; active beats `disabled-by-path` |
| 5 | Medium | `backend` on jj path for Git-prefixed codes | `jj` on the jj path stated in the refusal table |
| 6 | Low | T006 code list | Extended |
| 7 | Low | Note about PATH Git absent | Added to data model and example |
| 8 | Low | Refusal example notes | Notes empty for refused and error results |
| 9 | Low | Null operation sort order | Null sorts last |
| 10 | Low | `git-config-hooks` range when minimum lane lacks it | Range starts at the lowest supporting lane version |
| 11 | Low | Double `version` read | Accepted and stated |

## Round 4

Independent reviewer: narrow read-only Opus re-check, revision 7727c5d, 2026-10-16 (one round beyond the three-round limit, authorized by the coordinator). Verdict: NOT READY (one high, four medium, five low findings). The reviewer confirmed the two JSON examples, exit statuses, limits, refusal rows and summary counts field by field. The author applied the resolutions below; they were NOT re-reviewed, so the verdict of record is NOT READY.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | High | Disagreement rule keeps the lane red forever once the claim is `unverified` | Disagreement exits 1 unless the catalog already states `unverified`/`unknown`; spec, plan DD16, T022/T024 |
| 2 | Medium | Deciding sentinel set undefined; observers could flip a claim | Deciding sentinels are blocking guards only; partial outcomes go to the disagreement rule |
| 3 | Medium | Contract bypass rule contradicts the unknown-engine rule | Rewritten: at most one finding of each code per operation |
| 4 | Medium | US2-6 contradicts FR-008 | Scenario amended |
| 5 | Medium | No precedence for state `unknown`; data-model note | Precedence in FR-009; data-model note qualified |
| 6 | Low | Dangling `guards_evaluated` clause | Separate sentence |
| 7 | Low | Unknown-engine hook defined three ways | One definition in FR-009 used everywhere |
| 8 | Low | T024 and DD16 lacked the resolution rule | Added |
| 9 | Low | `backend: jj` too broad | Limited to codes after a `.jj` marker |
| 10 | Low | Text output notes | Marked abbreviated |

## Round 5

Independent reviewer: read-only Opus review, revision 9ce514c, 2026-10-16, part A (narrow verification) and part B (scope reduction, recorded at the top of this file). Part A verdict: NOT READY (no critical or high; five medium and nine low findings; the reviewer withheld READY because round-4 #7 was recorded as resolved but had not landed everywhere). The author applied the resolutions below; they were NOT re-reviewed, so the verdict of record is NOT READY. The reviewer notes that under a strict reading of "READY unless critical or high" the verdict would be READY with five mediums to fix.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | Medium | Unknown-engine trigger defined loosely in FR-008, contract rules, data model, DD10, T011 | One definition (FR-009) referenced; contract line merged |
| 2 | Medium | T022 lacked the exit-0 escape clause and exit 2 for a failed control | Added |
| 3 | Medium | "Any sentinel" versus deciding sentinels | "deciding sentinel" |
| 4 | Medium | Disabled configuration hooks counted as guards | Only enabled entries count; disabled is `absent` |
| 5 | Medium | `no-active-guards` fired for unevaluated workspaces | Requires `guards_evaluated` |
| 6 | Low | FR-009 sentence broken by an edit | Rewritten |
| 7 | Low | `source` with non-winning contributors | Lists winning contributors only |
| 8 | Low | Duplicated phrase in T024 | Removed |
| 9 | Low | US5-1 ignored the exit-0 rules | Exception added |
| 10 | Low | Notes list broken and incomplete | Rewritten with conditions |
| 11 | Low | J2b missing from J7 | J7 names J2b |
| 12 | Low | J5 sources indistinguishable | Split into J5a and J5b |
| 13 | Low | Lane pairing for `jj-push` | Pairs stated |
| 14 | Low | Meaning of `tracked` in colocated jj | Stated in contract and data model |

## Round 6 (reduced scope)

Independent reviewer: read-only Opus review of the reduced artifacts, revision 8d4c652, 2026-10-16, with Git 2.56.0 re-measurements and the jj documentation (git-compatibility, CLI reference, changelog). Verdict: READY (no critical or high; six medium and seventeen low findings). The reviewer re-confirmed the record layouts, the `rev-parse --git-path hooks` cases, the ownership gate behavior, the classification order, the `hook.*.event` output, that the two JSON examples match the data model field by field, and that the 15 finding codes agree across artifacts. The author applied the resolutions below.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | Medium | Nested `.jj` markers: `managed` sorts `Evidence[0]` | Plan DD1 and T004: probe-only session gets innermost-first evidence; test |
| 2 | Medium | Cross-lane rule not checkable by the script | Script rules are within-lane; cross-lane comparison is the manual T022 step |
| 3 | Medium | J0 both control and deciding step | J0 is a control and a measurement; failure is exit 2 only |
| 4 | Medium | `hooks-failed` unreachable on the Git path | Refusal row, T007 clarify per path |
| 5 | Medium | `jj-push` coverage with null Git version or cross pairs | FR-008: null member or unmeasured pair is not covered |
| 6 | Medium | Cut-scope leftovers in research.md | Four passages rewritten |
| 7 to 23 | Low | `-z` in FR-004; path validation before `ancestors`; race between `ancestors` and `Inspect`; temp directory disclosure; `git_processes` rule; valueless event key; FR-010 tag; unfixtured edge cases; `skippable_by`; scan glob; gate output equality; quickstart warning; fresh-repository `hooks-dir-absent`; notes condition; status line; checklist wording; pre-release versions | Applied in the spec, plan, data model, contract, tasks, quickstart and checklist |
