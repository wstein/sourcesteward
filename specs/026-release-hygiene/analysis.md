# Pre-implementation analysis

## Round 0

Author draft, 2026-10-16. Not an independent review. Independent analysis rounds are recorded below as they happen.

## Round 1

Independent reviewer: read-only Opus review, revision 47070ae, 2026-10-16, with measurements in a copy of the repository (Go 1.27.2 linux/arm64; `go test`, `go vet`, `go list`, `go version -m` only). Verdict: NOT READY, 2 critical, 5 high, 9 medium and several low findings. Measurements the author accepted from the reviewer: under `-trimpath` the build information has no `-ldflags` entry; `git status` rewrote the index while `--no-optional-locks` did not; an empty module cache with `GOPROXY=off` fails and may create `GOMODCACHE/cache`; the devcontainer has `GOPATH=/go`, so an inferred `$HOME/go/pkg/mod` is empty; Python `$` accepts a trailing newline.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | Critical | Record naming the commit cannot be inside that commit | Code commit and record commit (spec edge case, DD12); build from `git archive` of the A1 commit; tag at the code commit; CI evidence refers to the code commit |
| 2 | Critical | `-ldflags` not in build information under `-trimpath` | Stamp verified only by execution (DD3, data model `stamp_verified`, G14, T015); research corrected |
| 3 | High | No rule for which gate each command evaluates | Gate evaluation table; `build` re-checks G01, G04 to G07, G09, G13; SC-007 and FR-008 reconciled |
| 4 | High | "Writes nothing but the cache" false; fakes cannot prove it | Git plumbing only, no `git status`; effects restated; real-run snapshot test (T009); `unsupported-checkout` for jj without Git |
| 5 | High | `verify` executes untrusted downloads without a trust anchor | `--anchor`, `trust-anchor-missing`, exact member extraction with limits, allowlisted environment (FR-013, contract, T015) |
| 6 | High | G05 unmapped and undecidable | Feature id list in the notes, T011a, one fixture per gate |
| 7 | High | Approval loopholes | Quote check (version, platforms, 12 hex), waivers need quotes, A2 covers tag push, `--verify-tag`, script never tags |
| 8 | Medium | Allowlist drops GOPATH | Absolute `GOMODCACHE` from the caller's `go env`; fresh `GOCACHE` (DD4) |
| 9 | Medium | Code vocabulary drift | One code table with gate, exit; usage errors exit 2 without JSON |
| 10 | Medium | Pattern differs Go vs Python | Whole-string matching, newline test case (T004) |
| 11 | Medium | Hidden command scope vs published schema | Edge case accepts the already-published schema; help of a hidden command on request is allowed |
| 12 | Medium | Rebuild not independent | Separate export, directory and empty `GOCACHE`; hosts recorded (FR-012) |
| 13 | Medium | Ignored files in the build | Superseded by finding 1: export from the commit |
| 14 | Medium | arm64 job cannot reuse the existing jobs | Separate `conformance-arm64` job without Hugo, gh and Pages (T018) |
| 15 | Medium | Notices miss stdlib and per-target closures | Union over shipped targets plus a Go standard library row (G07, DD9, T007, T012) |
| 16 | Medium | Git inherits user configuration | Isolated Git invocation in the recipe |
| 17 | Medium | G01 tested only with fakes | Real temporary-repository tests (T006) |
| 18 | Low | Smaller items | Fixed: cgo wording, archive wording, smill name row, T021 tag, T022 status rule, manifest `env`, quickstart identity and `shasum`, S4 method, `STW_RELEASE_VERSION` note, scan wording, quote sanitizing. Not changed: `TestVersionCommandUsesStamp` stays parameter-level; the `cmd/stw` wiring is covered by S1 and S2 (executed) |

## Round 2

Independent reviewer: read-only Opus review, revision 6870cf8 (rebased on origin/main), 2026-10-16, with probes of `git archive` and of an export under the recipe environment. Verdict: NOT READY, 3 high, 9 medium, 7 low. Measured by the reviewer: `git archive` ran a repository-local smudge filter, honored `export-ignore` and rewrote `export-subst`; an export without `.git` passes `go vet ./cmd/stw` and `go test ./internal/cli` under the recipe environment; `check-docs.py` does not validate anchors (checked by hand).

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | High | `git archive` applies filters and attributes | Export from tree objects (`ls-tree`, `cat-file --batch`), `export-unsafe` code, recipe, DD12, T006 fixtures |
| 2 | High | Evaluated commit not tied to the record | G01 requires `--commit` (default: record header) equal to header, A1 and every `ci` entry; tests in T008 |
| 3 | High | Notes and record not in the export | Notes and record read from the checkout (`--notes`), docs scans on the export, record commit ancestry and `docs/releases/`-only diff in G11 |
| 4 | Medium | Manifest not covered by the anchor | `SHA256SUMS` lists the manifest; tampered-manifest test |
| 5 | Medium | G02/G03 self-attested | Declared record-asserted with a reviewer cross-check; "code commit" wording |
| 6 | Medium | G09 before A1 | A1 and `release-exists` first, before any `go` command |
| 7 | Medium | `verify --rebuild` rows and output | Evaluation table rows, `.rebuild` directory, effects |
| 8 | Medium | G04 dropped outcome | `pass` with the dropped list; notes must name them |
| 9 | Medium | Missing record | `not-run` with reason `no record`; `record-incomplete` under `--final` |
| 10 | Medium | Stamp code ownership | `stamp-*` codes under G08 (test) and G14 (execution) |
| 11 | Medium | Quickstart order | Section 0 and a `plan --final` step |
| 12 | Medium | arm64 lanes | `lane` matrix; hosting smokes excluded on arm64 in G04 |
| 13-19 | Low | Synopsis `[--anchor]`; T002 code; `plan` GOCACHE; DD order and duplicate risk; extra assertions in T008/T014/T016; record-commit push stated as routine documentation push that never authorizes A2/A3; constructed environment for every export process | Applied |

## Round 3

Independent reviewer: read-only Opus confirmation review, revision b2bd3cb, 2026-10-16. Verdict: NOT READY (2 high, 8 medium, several low). All Round 2 resolutions were found applied; the findings below are new or residual. They were resolved in the following commit. The three-round limit is reached, so **this resolution has not been re-reviewed**; the coordinator should run one more confirmation pass.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | High | `plan --final` before the tag cannot pass G13, G11 and G14 | Split: `--final` (before the tag: A1 and A2, pre-publication smokes, post-release items may be pending) and `--complete` (after publication: A3, section 8); quickstart order, R5, T022 |
| 2 | High | Replace refs and grafts defeat the tree export and ancestry | `GIT_NO_REPLACE_OBJECTS=1`, `info/grafts` gives `export-unsafe`, fixtures in T006 |
| 3 | Medium | `build` gate set differs between table, contract and T014 | One set: G01 to G07, G09, G13 (A1), G02 and G03 from the record |
| 4 | Medium | Stale `archive` in DD5 and Effects | Replaced; Effects list completed; `git archive` and `git status` never run |
| 5 | Medium | `--commit` default inconsistent | Header commit when a record exists, otherwise `--commit` required |
| 6 | Medium | Record commit definition ambiguous | `HEAD`; record and notes read from `HEAD`'s tree; differing working-tree file is `record-incomplete` |
| 7 | Medium | Notes absent during `plan`/`build` | `not-run` with reason `no notes`; `release-notes-missing` under `--final` |
| 8 | Medium | Commit agreement only for `ci` entries | All evidence classes; `manual` records the anchor |
| 9 | Medium | Hostile tree entries and bounds | `export-unsafe` for `..`, `.git`, absolute, duplicate or case-colliding names, 20,000 entries, 256 MiB; `O_EXCL` |
| 10 | Medium | `stamp_verified` in an anchored manifest cannot be set | Removed from the manifest; `stamp` in the verify result |
| Lows | Low | Status line, R5 pointer, `wrote` wording, G08 under final, T002 wording, misspelled-variable and `modules-unverified`/`compile-failed` fixtures (T014a), quote escaping, quickstart record path | Applied. Not changed: S4 creates its repository with the smoke environment, which the verify design already limits to an allowlist; add `GIT_CONFIG_*` isolation during implementation |

## Round 4 and narrow review of the cuts

Round 4 (coordinator, confirmation of revision 76e9449): READY, 0 critical or high, 5 medium and 12 low findings. The medium findings were: undefined pre-publication evidence for `plan --final`; A4 not checked by any mode; no release branch rule; constitution IV conformance wording; open questions and the dry-run classification. Resolved by the pre-publication verification record section, A3 and A4 as reviewer-checked approvals, branch `codex/release-<version>`, FR-015 and FR-005, and the spec's Resolved decisions and Open questions. The coordinator also directed cuts (no `verify --rebuild`, no `--complete`, no JSON or `tool_schema` or line bound for `release.py`, no spec-table parser, the `smill` table reduced to one paragraph, a JSON machine block in the record, no export entry or size bounds), and recorded decisions by the maintainer on 2026-10-16.

Superseded by these cuts: round 1 `stamp_verified` in the manifest and `verify --rebuild`; round 3 `plan --complete`, the 20,000-entry and 256 MiB export bounds, and the PlanResult JSON. Task numbers cited in rounds 1 to 3 (T011a, T014a, T015, T018, T022) refer to the earlier numbering.

Narrow independent review (read-only Opus) of revision e404404: NOT READY, 2 high, 6 medium, 8 low. All were resolved in the following commit and **not re-reviewed**.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | High | `plan --final` needed A2, which was obtained only after it | A2 is recorded in a record commit before `plan --final`; the tag is created only after it passes (approvals, quickstart, R2, R3) |
| 2 | High | A3 recorded after publication | A3 and A4 are recorded before their steps; only post-release verification is after the fact (G13, record contract, R4 to R6) |
| 3 | Medium | Round 4 not recorded | This section |
| 4 | Medium | Branch rule fails with a detached `HEAD` in colocated jj | G11 checks `refs/heads/codex/release-<version>` resolves to `HEAD`; creation method left to the maintainer |
| 5 | Medium | No pre-publication procedure for linux hosts | Quickstart step 3b and R2: built files copied by hand, `local-recorded` |
| 6 | Medium | T023 inside WI6 conflicted with G11 | T023 limited to `implemented`; R7 marks `verified` on `main` after R5 |
| 7 | Medium | Code commit need not be reviewed or on `main` | G01 requires an ancestor of `origin/main` (`commit-not-on-main`); merging the branch needs its own authorization |
| 8 | Medium | Quote duplicated in prose and JSON | The JSON `quote` is canonical |
| 9-16 | Low | Checklist wording, research fallback bullet, "record header" wording, `--out` vs FR-006, `status: ready`, computed vs recorded gates, existing tag check, SC tags on tasks, quickstart effects wording, old task numbers | Applied |

## Round 5

Independent confirmation of revision adee7d1 (coordinator): READY, 0 critical or high, 6 medium and 10 low findings, all applied afterwards and not re-reviewed.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| M1 | Medium | Mac mini manual smoke listed as a G04 condition | It is G14 pre-publication evidence (platform table, FR-001) |
| M2 | Medium | `build` gate preconditions unclear | `build` refuses (exit 1) unless G02, G03 and G04 per requested platform are recorded `pass`; only the notes parts of G05 and G06 may be `not-run` |
| M3 | Medium | Order: CI evidence and A1 before the build | Quickstart step 0 to 3 and R1 commit them on the release branch before `build` |
| M4 | Medium | Machine block lacked CI fields | EvidenceEntry with `gate`, `commit`, `workflow`, `job`, `conclusion`; G02 and G03 point at it |
| M5 | Medium | Decision 6 scope | Verbatim reply "1-5 ja, 6 routine", scoped to `codex/release-*` branches; never authorizes A2, A3, A4, tag, release or merge |
| M6 | Medium | Record edited after `--final` | Quickstart order completes the record before step 5; the trailing "Record" section removed |
| L7-L16 | Low | "record header" wording; `origin/main` missing; A3/A4 absent at `--final`; Approval fields; `released`/`abandoned` status; R-numbering and R7 dependency; Status line; macOS-safe comparison and `pins` check | Applied |

Cuts applied on the maintainer's direction (Werner, "im Zweifelsfall aufweichen"): no `make release-plan`; `plan --final` reduced to G01, G09, G11 and the A1 and A2 quote checks, with G02 to G04 evidence, G05 ids, G10, G12 and the recorded G14 values moved to a reviewer checklist in the record contract (so the round 5 suggestion to require `stamp: execution` by script is a checklist item instead); hostile-tree fixtures in T006 collapsed to one test per class; the `smill` paragraph moved from `docs/releasing.md` to a follow-up note in research.md; the reproducibility comparison task removed (G12 is a checklist item). Tasks were renumbered again (T001 to T020, R1 to R7); earlier rounds cite older numbering.
