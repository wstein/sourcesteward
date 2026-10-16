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
