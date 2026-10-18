# Pre-implementation analysis

## Round 0

Author draft, 2026-10-17. Not an independent review. This round rewrote the 2026-10-16 "release hygiene" draft (five earlier analysis rounds, recorded in git history of `specs/026-release-hygiene/analysis.md`) because the owner's decisions D1 to D8 replaced its design: a Python script with Go-specific recipe, a markdown readiness record, four approvals and a per-release double build. What was reused and dropped is in [research.md](research.md#reuse-and-drops-from-the-release-hygiene-draft).

Inputs received while writing, all applied before the first review:

| Input | Where applied |
| --- | --- |
| D6: artifact kinds `file` and `oci-image` from v1, per-project version scheme (`semver`, `calver`, pattern), promote means tagging the same digest, activation outside the release | spec D6, FR-003, FR-004, version table; contracts/config.md and record.md (both kinds in the schemas); data-model; build `--mode promote`; examples 2 and 3 |
| D7: bootstrap with the existing tag-workflow pattern, `stw` self-hosts from the next release, `stw` v0.1 implemented right after READY and used for all releasable repositories | spec D7 and FR-024, contracts/bootstrap.md, tasks WI10 and WI11 |
| D8: waves (SheetMill, SourceSteward, BundleDock CLI; BundleDock toolbox bundle, WorkHarbor; CrewBook), several `[[build]]` and `[[target]]` per project, optional `readiness` gate | spec D8, FR-021, FR-022, gate `readiness-missing`; config `[[build]]`, `scope`, `group`, `role`; examples 3 and 4; WorkHarbor release policy and CrewBook criteria recorded as unverified (not read) |

Measurements made in this round are in research.md (rebase ancestry, filter-free raw diff, tomllib leniency, schema validation of four contracts and four records with seven negative mutants, `gh` flag inventory). Not verified: everything about jj (not installed), GitHub draft and attestation behavior, WorkHarbor and CrewBook.

Known weak points the author wants the reviewer to attack: the unverified GitHub behavior list; whether `build` as a `stw` command is the right home for execution (open question 7); attestation binding to C through the attested manifest; the allowlist's reliance on project declaration; the jj capability table; the exit status table (3 for a failed verdict); the readiness gate's honesty; the plan/verify split for tags.

Additional work done in this round after measurement: the four example contracts were found to violate the profile rules the author had just written once (readiness files under an allowlisted `docs/**`), which is why the readiness files in example 4 live outside the allowlist; the regular expression `\u0000` and repeat counts above 1000 in the first schema draft were rejected by the schema library (RE2) and corrected.

## Round 1

Independent reviewer: read-only Opus review (general-purpose agent that executed checks in scratch directories), revision 9a810a2, 2026-10-17. Verdict: NOT READY, 8 high, 17 medium, 14 low findings. Verified by the reviewer by running: schemas versus examples (all valid; seven forgery probes also validated, see finding 5), canonical encoding equals `json.dumps`, `tomllib` leniency, rebase ancestry and the net diff, diff classification (mode, symlink as `T`, gitlink, renames as D plus A, newline paths, no filter or textconv run by `diff-tree`), annotated versus lightweight tags, `info/grafts` spoofing ancestry, regex behavior, `gh` 2.102.0 flags (`--source-ref`, `--cert-identity`, `--signer-digest`, `--deny-self-hosted-runners` exist), the 64 KiB Git runner cap, the GET-only hosting transport, `ci.yml` triggers, the siblings' `event=push` CI query and BundleDock's documented counter restart, quay secret and aliases. Not verifiable: jj, GitHub draft, attestation, dispatch and immutable behavior (marked unverified).

Two further binding inputs arrived while the review ran and were applied together with the findings: **D9** (multiple products per repository with per-product tag prefix and monotonic version; fetch/assemble builds with a declared network effect; a fifth worked example for `wstein/bundledock-third-party-sources`), recorded in spec D9, FR-025, FR-026, config.md "Products and files", examples.md example 5, data-model, tasks T004a and others; and the waves/readiness input recorded in Round 0.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | High | `pattern` scheme cannot derive a tag from a version | `tag_template` added (required for `pattern`); `tag_pattern` must have a literal prefix and suffix equal to the template's around one top-level `version` group, which makes the round trip the identity (config.md, schema, spec FR-004, version table, T009) |
| 2 | High | Existing-tag build mode can never be published | `verify`/`plan --record-commit R` and a `workflow_dispatch` publish path (`tag`, `record_commit`) specified; the subject tag is excluded from monotonicity; the bootstrap release uses the same path (spec edge case, FR-027, verify.md, workflow.md, T019, T037) |
| 3 | High | Draft gates cannot be evaluated by read-only jobs | Draft-sensitive checks run in write-permission jobs (`draft` re-runs `plan --online`; `verify-and-publish` has `contents: write`); a token that does not see drafts yields `not-run` (`drafts-invisible`), never `pass` (spec edge case, verify.md step 5, workflow.md, data-model reasons) |
| 4 | High | Publish can be raced after verification | One job `verify-and-publish`; re-list of asset ids, sizes and update times just before publishing; publish with `--verify-tag`; residual window stated; immutable releases freeze afterward (workflow.md, DD32, T037) |
| 5 | High | Attestation not pinned to `main`; trusted inputs come from the editable record | Repository from hosting, signer from the contract at C, `--source-ref refs/heads/main`, `--deny-self-hosted-runners`; record fields repository, signer, project, product, `url_template`, `tags` recomputed and compared (`record-invalid`) (FR-011, verify.md, record.md, DD28, T012, T026) |
| 6 | High | Publish workflow trusts the tag commit; verifier built from it | Verifier built from the `main` tip in a separate checkout; tag ruleset restricting creation to the owner is a required setting and a documented limitation (workflow.md, quickstart, FR-013) |
| 7 | High | Bootstrap release cannot produce its record | The bootstrap build runs `stw release build --mode ci`, producing a schema-valid manifest; attestation in a separate job; `v0.1.0` verified through `--record-commit R` (bootstrap.md, T044, T047) |
| 8 | High | Smoke step cannot run as written | Smoke runs at the root of the C checkout with `{artifact}`; no extraction by `stw`; full synopsis incl. `--smoke`, `--files-only`; `stw` hand-off via the `gates` job artifact, pinned release for other projects (build.md, workflow.md, spec placeholder table, example 1) |
| 9 | Medium | Placeholder rules contradict each other | One normative per-field placeholder table in the spec; config.md and record.md reference it (spec "Placeholders") |
| 10 | Medium | Exit-status drift and `release-limit` double use | One exit table with every command; `release-limit` is a refusal (exit 1) everywhere except `check`'s `limit-exceeded`; removed from the gate table |
| 11 | Medium | `incomplete` versus `fail` precedence | One verdict rule: any `fail` gives `fail` (exit 3); `boundary.complete` renamed in meaning (data-model) |
| 12 | Medium | CI gate lacks event/branch | `event=push`, `head_branch=main`, conclusion `success` (gate table, verify.md) |
| 13 | Medium | Main ref underspecified | Remote-tracking `main` of the hosting-resolved remote; jj revset `C & ::main@<remote>` (unverified); dispatch `main-ref-stale` makes gate and `plan_id` use one value |
| 14 | Medium | Build job preconditions and `stw` source | `fetch-depth: 0` checkout; gate-tool artifact hand-off; adopters need a pinned `stw` (workflow.md) |
| 15 | Medium | False "no process" claims for `check` | Reworded everywhere to "no process other than ownership detection and fixed `cat-file` reads" (spec US1, commands table, check.md) |
| 16 | Medium | Runner cap claim false | Streaming diff parser with 20,000 entries / 4 MiB; other reads keep the 64 KiB runner cap (spec limits, DD31, T018) |
| 17 | Medium | Pattern tracks split on optional groups | Track notion removed; the monotonicity set is the product's full grammar (version table, data-model) |
| 18 | Medium | Cut order removes owner decisions | Only `local-verify` and the SBOM role are cuttable without owner re-decision (tasks) |
| 19 | Medium | Commands table and FR-008 drift | Complete command table incl. `--product`, `--record-commit`, `--tag`, `--smoke`, `--files-only`, `promote`; contract flags declared normative; FR-008 and FR-016 corrected |
| 20 | Medium | `record --write` path safety | No-follow opens, directories created as specified, `output-exists` for symlinks (record-command.md, T022) |
| 21 | Medium | `{out_dir}` with `working_dir` | Substituted as an absolute path (config.md, placeholder table) |
| 22 | Medium | BundleDock fit overclaimed | Counter restart, registry secret and moving aliases recorded as wave-2 blockers (open question 3); SC-007 reworded to fields only |
| 23 | Medium | Repro workflow fixed version | Version is a dispatch input; offline build gates apply, release gates do not (workflow.md, DD25 superseded) |
| 24 | Medium | Argument classification circular | Syntax-only usage errors before any read; tags resolved as `refs/tags/<arg>` (`tag-unresolved` refusal); contract read at the tag commit in both plan and verify |
| 25 | Medium | Coverage gaps | FR tags added to T001, T040 to T042, T046, T048; new tasks T004a and extended T004, T014, T021, T022, T025, T030, T032, T033; verify/dispatch sentinels, Go-free plan test, `config-ambiguous` test, FR-007/013/019/020/021/022 mapped |
| 26 | Low | `SHA256SUMS` limit off by one | 65 lines |
| 27 | Low | Signer workflow name drift | Aligned to `release-build.yml` |
| 28 | Low | Broken anchor and references | Quickstart heading and anchor fixed; section references corrected; T045 pointer |
| 29 | Low | bootstrap.md versus tasks | Aligned (WI10, T043) |
| 30 | Low | Grafts fake ancestry | `grafts-present` and shallow clones make ancestry gates `not-run` (DD18, T013) |
| 31 | Low | calver date, pattern group digits | Calendar-date rule; `[0-9]{1,9}` for `order` groups |
| 32 | Low | Record `tags` unconstrained | `tags == [version]` is a `record-invalid` rule |
| 33 | Low | Promote details | Digest and version from the record; idempotent for the same digest, refuses another; failure after promote documented (build.md, workflow.md) |
| 34 | Low | Environment deny-list and wording | Wider deny-list; "no secrets passed" reworded (spec build table) |
| 35 | Low | Tag approval is publication approval | Stated in quickstart step 5 |
| 36 | Low | Bootstrap build job holds attestation rights | Separate `attest` job (bootstrap.md) |
| 37 | Low | Interleaved arrays ambiguity | Defined as forbidden (contiguous groups); measured that `tomllib` accepts them (config.md, T003) |
| 38 | Low | Canonical encoder gaps | Hand-written schema-ordered encoder stated (record.md, record-command.md) |
| 39 | Low | Result-shape inconsistencies | `effects` keys aligned (`network`, `writes`, `processes`, `project_commands`); lowercase commit ids |

Author re-validation after the edits: the six example contracts and six records validate against the updated schemas; ten negative mutants fail; the strict-profile line check passes for all six contract files. Not re-reviewed yet: this resolution is the subject of round 2.

## Round 2

Independent reviewer: read-only Opus confirmation review (executed checks), revision f7751e4, 2026-10-17. Verdict: NOT READY, 3 high, 8 medium, 12 low. Of the 39 round-1 resolutions, 31 were confirmed, 7 partially (2, 3, 8, 9, 17, 26, 39) and 1 not (7). Mechanically verified by the reviewer: six contracts and six records valid against the extracted schemas, each record consistent with its contract, strict-profile line check, Go regexp behavior of the mirror pattern (including `zlib-ng-v` and a trailing newline), `check-docs.py` and `git diff --check`, sibling permissions.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| H1 | High | The bootstrap release cannot be verified as specified (publication gates, signer, source ref) | The bootstrap workflow publishes `v0.1.0` itself; the spec no longer says the publish workflow does. New `verify --published` mode: `already-published` and `draft-missing` are `not-applicable`, and `--signer-workflow-file`/`--source-ref` are given explicitly (never read from the record); spec FR-027 and edge cases, verify.md, bootstrap.md, quickstart step 6, T047 |
| H2 | High | Draft visibility rule contradicts itself and cannot be detected | `stw` cannot detect it, so the draft part of `release-exists` runs only with the caller's assertion `--with-drafts` (otherwise `not-applicable`, `drafts-not-checked`, printed); `draft-missing` fails closed; `drafts-invisible` removed (spec edge case, release-plan.md, verify.md, data-model, workflow.md, T025) |
| H3 | High | `draft` and `verify-and-publish` lack `actions: read` | Added to both (and `attestations: read` flagged as possibly needed, unverified); rule tests extended in T036 |
| M1 | Medium | `--product` missing in workflow steps, T006, `check --commit` | Passed in every workflow `stw` step; T006 and check.md updated |
| M2 | Medium | "Literal" prefix/suffix of `tag_pattern` unsound | Defined on the parsed RE2 tree (literal nodes only); members must satisfy `tag == tag_for(version_of(tag))` (config.md) |
| M3 | Medium | Placeholder table not applied everywhere | config.md and build.md now reference the spec table |
| M4 | Medium | `verify` cannot verify a published release | `--published` (see H1) |
| M5 | Medium | Smoke job cannot meet `ci` preconditions | Smoke checks out with full history; any `fail` or `not-run` offline gate refuses `gate-failed` in `build` |
| M6 | Medium | Multi-product consequences unstated | Workflow "Multi-product consequences" section and a quickstart owner step; the repository-wide diff rule is kept |
| M7 | Medium | Registry authentication unspecified | Pinned registry-login step with `github.token` named for the image-capable variants (workflow.md) |
| M8 | Medium | Fetch disclosure missing from dispatch | Added to dispatch output, JSON and effects; T030 |
| L1 | Low | Stale task/requirement text (64 lines, track-aware, four records, FR-023, checklist) | Updated |
| L2 | Low | Example 2 is a default product though BundleDock is multi-product | `product = "toolbox"`; record carries `product` |
| L3 | Low | release-plan example subject incomplete | Completed |
| L4 | Low | Manifest and OCI result schemas prose only | Two schema blocks added to record.md; samples `go-cli.manifest.json`, `mirror-zlib.manifest.json`, `oci-bundle.result.json` validated; T010 extracts all three blocks |
| L5 | Low | `--repository` versus `--repo`; "verified manifest" | `--repo` everywhere; wording corrected |
| L6 | Low | Loose tag arguments | Ref-name character rules in the classification rule |
| L7 | Low | `ci-not-green` wording | Rewritten |
| L8 | Low | Mixed finding and refusal lists | Clarified in config.md, check.md and the spec limits row |
| L9 | Low | Build job "no write permission" versus `packages: write` | Reworded ("no repository write") |
| L10 | Low | `tag-version-mismatch` without a record | Defined against the tag-derived version in `--for build` |
| L11 | Low | `dispatch --tag` for jj | `unsupported-backend` in jj |
| L12 | Low | Leading zeros | Equal tuples are refused by strict monotonicity (stated) |

Author re-validation: six contracts, six records, two manifests and one OCI result validate against the three-schema set; profile line check passes. Not re-reviewed yet.

## Round 3

Independent reviewer: read-only Opus confirmation review (executed checks), revision ddde764, 2026-10-17. Verdict: **READY** (no critical or high finding). All 23 round-2 items were checked; 17 confirmed, 6 partially (H1, H2, M3, M4, L5, with leftovers). Mechanically: the four schemas (three in record.md, one in config.md) compile and resolve the manifest's cross-reference; 6 contracts, 6 records, 2 manifests and 1 OCI result valid; 25 negative probes rejected; all 9 JSON fixtures canonical; each record consistent with its contract; the strict-profile line check, `check-docs.py` and `git diff --check` clean.

The eight medium and nine low findings were all resolved in the commit after this review and are **not re-reviewed**:

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | Medium | Three places still sent the bootstrap release through the publish workflow | workflow.md, plan DD33 and quickstart limited to input mode 2; the bootstrap release publishes itself and is checked with `verify --published` |
| 2 | Medium | Bootstrap record named a signer that did not sign | `record` and `verify` take the same explicit `--signer-workflow-file`, replacing the recomputation (record-command.md, verify.md, record.md) |
| 3 | Medium | `--published` could not audit an older release | `version-not-monotonic` is `not-applicable` under `--published` (spec, verify.md, T025) |
| 4 | Medium | Outcome of `release-exists` without `--with-drafts` undefined | Gate outcome comes from the tag/release part (`pass`/`fail`) with reason `drafts-not-checked`; never `not-applicable` as a whole (spec, release-plan.md, T025) |
| 5 | Medium | `drafts-invisible` leftover; dispatch asserted draft visibility | T024 corrected; dispatch runs `plan --online` without `--with-drafts` and reports `drafts-not-checked`; the `draft` job is authoritative |
| 6 | Medium | Empty `--product` unspecified | Empty value is treated as absent by the CLI (spec rule, T006 test); workflows pass it only when non-empty |
| 7 | Medium | config.md `smoke_argv`/`promote.argv` rows stale | Aligned with build.md and the spec placeholder table |
| 8 | Medium | Sample manifest/result disagreed with their record pairs | Samples regenerated to agree (repository, host order, platforms) |
| 9-17 | Low | `verified manifest` wording; `--with-drafts` on `verify` defined (detail only); `--signer-workflow-file`/`--source-ref` usage rules; optional `--record-commit` in quickstart step 6; config.md heading anchor broken (heading made a real heading); manifest `builds` in the prose list; SCHEMA_PATHS task number; plan example `readiness-missing` row and "bundle product"; dispatch exit codes and the "R follows the tag" ordering sentence | Applied |

This is the third round; the verdict is READY. The final resolution is an author edit that has not been independently re-checked, which the handoff states.

## Round 4

Coordinator confirmation review: READY. One medium and six low findings, fixed by the author without a further review:

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| M1 | Medium | The bootstrap `record` step lacked the signer override | `record --signer-workflow-file .github/workflows/release-bootstrap.yml` in bootstrap.md and T047 |
| L1 | Low | verify.md `--published` row omitted `version-not-monotonic` | Added (matches step 5, FR-027, T025) |
| L2 | Low | workflow.md `gates` said the gate is `not-applicable` | Draft part skipped with reason `drafts-not-checked`; the gate is `pass`/`fail` |
| L3 | Low | spec `record` row lacked `--signer-workflow-file` | Added |
| L4 | Low | `--published`, `--signer-workflow-file`, `--source-ref` shown as nested | Shown as independently optional in the spec row and verify.md synopsis |
| L5 | Low | verify.md steps 2 and 7 and the flag row garbled | Rewritten; override stated alongside the defaults |
| L6 | Low | workflow.md showed `--product "$PRODUCT"` unconditionally | Shown as `[--product "$PRODUCT"]` with a note: passed only when non-empty |

## Decision log: owner decisions of 2026-10-17

Recorded from the owner's reply on pull request sourcesteward#56. **No further review round was run on these edits**; they are author edits applied after round 4 and have not been independently re-checked.

| # | Decision | Where applied |
| --- | --- | --- |
| 1 | *Superseded by D10 (a): the owner then decided the amendment, applied in PR #58.* Constitution amendment **not decided**: the owner reads the text first; it stays a proposal; WI6 to WI11 stay blocked | spec notice and D10 (a), constitution-amendment-proposal.md header, bootstrap.md preconditions, tasks authorization boundary |
| 2 | `tag_commit` stays omitted from the record; `verify --json` reports it | spec D10 (b), DD14 unchanged |
| 3 | Toolbox-bundle blockers solved **before wave 2** (binding): restartable calver counter via a `pattern` product (new `monotonicity = "non-decreasing"`), registry auth as a workflow-owned login step with no secret in the contract, moving aliases `stable`/`latest` removed from the record | spec D10 (c), FR-028, version table, config.md field and schema, example 2 (now `pattern`), examples.md, workflow.md posture (one named secret, login step only), bootstrap.md wave table and preconditions, tasks T049 to T052, plan DD36, research DD36 |
| 4 | Reproducibility workflow dispatch-only; a schedule only with explicit owner approval | spec D10 (d), FR-017 unchanged |
| 5 | Retention: draft release for the single build, workflow artifacts as fallback; rehearsal in a disposable repository before WI10, only with explicit owner approval | spec D10 (e), quickstart rehearsal, bootstrap.md, T045 |
| 6 | Owner-only actor checks fail closed in organizations: accepted, recorded as a known limit | spec D10 (f), workflow.md known limitations (unchanged) |
| 7 | Keep `build` (modes `ci`, `promote`, `local-verify`) and `verify --published` | spec D10 (g); `local-verify` stays the first cut candidate only with an owner re-decision |
| 8 | Tag ruleset (creation restricted to the owner) is a REQUIRED precondition before the first tag; an owner-applied setting that `stw` may only report | spec D11, FR-028, release-plan.md advisory (`tag-ruleset`), bootstrap.md preconditions, quickstart readiness step 0, T043, T045, T052 |

Mechanical re-check by the author only: example 2 (now a `pattern` product) validates against the updated config schema together with the other five contracts, and its tag pattern was run in Go (`toolbox/2026.10.15-b98.1`, `-b1` and the bare date match; extra components and another prefix do not). Open questions 3 to 7 of the earlier handoff are closed; the amendment and the unverified GitHub behavior remain open.

## Round 5

Review of revision e532c53 (the owner-decision commits after the merge of the specification as pull request #56): NOT READY, 0 high, 2 medium, 9 low findings. **Author fixes, no further independent round** (unless the owner asks for one); the fixes are not independently re-checked. The owner-decision commits of 2026-10-17 (non-decreasing monotonicity, wave-2 preconditions, tag-ruleset advisory) were rebased onto `origin/main` and come before these fixes.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| M1 | Medium | Normative gate text still said "strictly greater" (verify.md step 5, version-not-monotonic row, D1, leading-zero sentence, quickstart step 1, T025) | All made mode-aware: strictly greater (`strict`), or an `order` tuple not below the maximum (`non-decreasing`), the full tag not yet existing (`release-exists`); verify.md states that `toolbox/2026.10.15-b1` is accepted when `-b98.1` exists under `non-decreasing` |
| M2 | Medium | Registry secret posture inconsistent (ghcr.io versus quay.io, repository versus environment secret, T050 counting, credential exposure) | New credential model table in workflow.md: no secret for file-only and `ghcr.io` (`github.token`), one environment-scoped secret of `release` for a non-GitHub registry, counted per workflow file (T050); FR-018, D10 P2, research and example 2 aligned; honest limit added: the login writes the credential (a base64 form not covered by masking) under the runner `HOME` read by project commands in the same job; workflow-owned push and promote steps are named as unspecified hardening |
| 1 | Low | Example 2 pattern lacked calendar validation and conflicted with the digit-width rule | Month, day and build expressions tightened; rule stated precisely (an `order` group can match only 1 to 9 decimal digits, checked on the parsed tree); no calendar validation for `pattern` stated (`2026.02.31` still matches) |
| 2 | Low | Order within an equal tuple undefined | Stated in the spec, config.md and the version table: consumers must not infer recency from the counter |
| 3 | Low | Rulesets GET undisclosed; `present` undefined; advisory not in the examples | Effects, plan.md endpoint list, human and JSON examples updated; `present`/`absent`/`unknown` defined with their limits (glob versus RE2, enforcement, bypass actors unverifiable, endpoint unverified) |
| 4 | Low | Absent versus unknown inconsistent | Real bootstrap and first tags block on `absent` and `unknown`; the disposable-repository rehearsal does not need the ruleset (quickstart, bootstrap.md, T043, T045) |
| 5 | Low | Tasks header said "implemented in the BundleDock repository" | Reworded: T049 to T052 are `stw` tasks; adoption evidence lives in the BundleDock repository |
| 6 | Low | plan.md risk said the blockers are not removed | Reworded to match D10 |
| 7 | Low | P3 checkability overstated | `check` rule `promote-argv-invalid` (`{digest}` and `{version}`/`{version_number}` required, literal aliases refused) plus the record rule and stw's own invocation; the tool's behavior beyond argv is stated as unverified (spec FR-028, config.md, T004, T051) |
| 8 | Low | Constitution amendment listed as both closed and open | Unified: decided by the owner, applied by pull request sourcesteward#58; WI6 to WI11 start only after the merge (spec notice, D10, open questions, proposal header, plan, tasks, bootstrap, checklist) |
| 9 | Low | Typo `[[--product "$PRODUCT"]]` | Fixed |

Author re-validation: the six contracts (example 2 with the tightened pattern), six records and samples validate against the schemas; the tightened toolbox pattern was run in Go.

## Round 6

Re-check of revision dfddca3 (pull request sourcesteward#59): NOT READY, 0 high, 2 medium, 11 low findings. **Author fixes; no further independent round, by owner decision.** Not independently re-checked.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| M1 | Medium | verify step 5 and the gate row required "full tag not yet existing" although `verify` has no `release-exists` and the subject tag exists | The new-tag condition applies to build mode only; in `verify` and `plan <tag>` the subject is excluded and a replay shows as `already-published` or the subject itself (verify.md, spec gate row) |
| M2 | Medium | ghcr.io variant exposure understated: in `verify-and-publish` the login token carries `contents: write`, `attestations: write`, `packages: write` and `promote.argv` runs in the same job | Exposure stated honestly in the credential model and Known limitations; the "better" claim replaced; hardening named but unspecified (promote in a job holding only `packages: write`, giving up the single-job property unless publish re-lists assets) |
| 1 | Low | T025 "leading-zero equal tuples refused" contradicted `non-decreasing` | Made mode-aware |
| 2 | Low | config.md node list omitted concatenation | Concatenations added |
| 3 | Low | check.md lacked `promote-argv-invalid` | Added |
| 4 | Low | plan DD36 not variant-aware | Reworded per the credential model |
| 5 | Low | `packages: write` granted to "the image-capable variant" in general | Limited to the `ghcr.io` variant in the job rows |
| 6 | Low | `present` required only the creation rule, D11 requires creation, update and deletion | `present` needs all three; new state `partial` with the missing rules named; every state list and the block-on rule updated (anything but `present` blocks a real tag) |
| 7 | Low | Stale amendment wording in the proposal title/status and task headers | Proposal retitled and restated as decided and applied by PR #58; task headers and the WI7 dependency say "until PR #58 is merged" |
| 8 | Low | Decision-log row 1 "not decided" | Marked superseded by D10 (a) |
| 9 | Low | Round 5 recorded as READY | Corrected to NOT READY (0 high, 2 medium, 9 low), reviewed revision e532c53 |
| 10 | Low | data-model lacked a `non-decreasing` example | Added (accepted restart, refused regression, existing full tag) |
| 11 | Low | D1 wording awkward | Tidied |


## WI3 owner clarification H5 (2026-10-18)

The owner explicitly approved option (a): native jj may read and discard readiness-file bytes solely to distinguish zero from nonzero, with no returned contents, content judgment, new file-size cap, writes or network. This corrects the former metadata-only effects wording: measured jj 0.45.1 exposes tree path/type/conflict/executable but no blob identity or size; the `normal_value()` method available in the installed 0.46 build is absent at the supported minimum.

FR-022, the limits table, T015 and the release-plan effects now agree. The fixed read uses the existing operation-pinned read-only runner and ten-second deadline, retains one stdout boolean and the existing bounded stderr, waits for successful completion and requires final state resampling. A prefix followed by failure, timeout, cancellation, stderr overflow or stale state cannot prove readiness. Git keeps metadata-only emptiness checks. This approval authorizes the stated read effect and source amendment only; it authorizes no build, run, tag, publication or consumer activation.

These are author amendments awaiting independent cross-artifact analysis with the exact WI3 implementation revision; no final completeness or independent CLEAR is claimed here.

### WI3 allowlist disclosure reconciliation

The config contract and plan mitigation require plan-time pattern reach disclosure, independently of WI4 net-diff classification. `allowlist_matches` contains every declared pattern and conservative case-insensitive tracked-leaf count in contract order, including zero, or null when exact enumeration is unavailable. Symlinks, gitlinks and hostile tracked names remain in these metadata counts; counts do not grant publication eligibility. Git enumerates paths incrementally under its existing deadline, retaining one bounded path and the existing maximum 256 counters, with clean EOF and successful completion required. The buffered-output and raw-diff volume/count cuts do not apply to this metadata stream. Native jj reuses already admitted entries. This closes the missing human/JSON disclosure without reading contents or the working tree and without introducing a total repository-size cut.

## WI3 source review round 2: native symlink capability

Independent review of cce88564b4af020c56de5e80a4e62af001e23cf3 was NOT CLEAR: the five first-round findings were fixed, but one High found that native committed input symlinks could hide allowlisted build inputs. Both measured pinned jj binaries (0.45.1 and 0.46.0) return successful empty `file show` stdout with a not-a-file warning for a symlink; the old adapter incorrectly used that empty output as a target. The exact reviewer regression and expanded native direct/chained/directory/escape/loop/empty-allowlist regressions failed before correction.

The narrow FR-014 correction admits blob reads only for regular files and returns a typed capability-unavailable error when declared input resolution requests a committed native symlink. Public checks and plans refuse `unsupported-backend` (exit 1, incomplete) with an actionable detail; they do not judge valid configuration invalid. Unrelated symlinks remain admitted and counted. Generic known empty targets also fail before lexical path joining. No new diff/debug parser, raw Git fallback, snapshot, command, effect or size limit is added: native patch-target decoding has not been designed or verified for hostile names, multiline targets and conflicts. Full jj planning acceptance for symlink-dependent inputs remains explicitly unclaimed. Exact corrected-head independent review is required before a CLEAR claim.
