# Pre-implementation analysis

## Round 1

Independent reviewer: read-only Opus review, revision 651bc36, 2026-10-16, with read-only `gh api` and anonymous requests against public repositories. Verdict: NOT READY (6 high, 15 medium, 11 low). The author re-verified finding 6 (compare `page=2` returns the counts without `files`: 177,292 bytes become 15,733 bytes for `cli/cli` 14451) and that pull request 112 merged (13:11:59Z). Findings 1, 2, 17 and 18 rest on the reviewer's measurements of other pull requests and are recorded as reviewer-measured in research.md.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | High | Latest-review counting loses an approval after a later `COMMENTED` review | Items table `review-required`: only `APPROVED`, `CHANGES_REQUESTED`, `DISMISSED` count; mutually exclusive ordered rows; R of 0 and R unknown rows; fixtures in T007 |
| 2 | High | No rule for status history, same-app duplicate runs or mixed sources | `required-check` row: newest status per context, newest run per app, worst state wins and is `inferred`; pinned pairs; statuses never satisfy a pinned app |
| 3 | High | A partially known required set treated as complete | Required-set rule; `required-checks-unknown` emitted beside known contexts |
| 4 | High | Degraded rows silent | Degradation rule (normative); explicit rows for `head-behind-base`, `review-required`, `classic-protection-unreadable` reasons; T007, T011 |
| 5 | High | `merge-methods` observed while repository settings are hidden | Row rewritten: unknown when any field is null; `rule_limit` reported as a constraint only |
| 6 | High | `compare` page 1 can exceed 1 MiB | Allowlisted shape is `per_page=1&page=2` in spec, contract, research, T003 |
| 7 | Medium | Allowlist is an undeclared change of Spec 022 | Scope states the explicit extension; T003 updates the 022 transport contract, 020 table and `docs/hosting.md` |
| 8 | Medium | Literal slash makes `x/protection` ambiguous | FR-008 refuses a component named `protection`; T003 refusal test |
| 9 | Medium | Unrecognized auto-merge method: item vs failure | Non-fatal: `method` null, `unknown`, reason `unrecognized-value` |
| 10 | Medium | Two null-field rules for `error` | One normative rule in the data model, quoted by the contract |
| 11 | Medium | Deleted head repository | `head_repository` nullable; T007 fixture |
| 12 | Medium | When strict is `false` observed | Strict rule in `strict-required-status-checks`; `protection.enabled` false proves classic absence and skips the protection read |
| 13 | Medium | Secondary rate limit classified `forbidden` | 403 with `Retry-After` is `rate-limited` (FR-007, plan, T002) |
| 14 | Medium | Resample read failure undefined | FR-010: `merge-state-source-failed`; lazy fields excluded from the comparison; T010 |
| 15 | Medium | No reason for dependency skips and failed sources | Reasons `dependency-unavailable`; `failed` maps to `source-failed` (data model Source) |
| 16 | Medium | Oversized response had two diagnostics | FR-009 and status table: `too-large` is a source failure; `merge-state-limit` only for count bounds |
| 17 | Medium | M2 overstated; check-suites evidence | M2 reworded and extended with the reviewer's timeline; cause stays hypothesis-only; check-suites source recorded as an open question |
| 18 | Medium | Combined status rejection mis-rated | research re-rated; newest-per-context rule for raw statuses |
| 19 | Medium | Edge cases without tasks; no-poll test | Added by name to T007, T010, T011, T014 |
| 20 | Medium | Wording deny-list | Summary templates are normative (data model); human text is an allowlist; JSON scan excludes `github_reported`; T013 |
| 21 | Medium | Fail-first impossible for guards | Tasks preamble exempts characterization and byte-equality guards |
| 22 | Low | 50 rule entries vs 50 rulesets | One unit: 50 distinct rulesets |
| 23 | Low | T015 cites nonexistent unit tests | Reworded; adds `scripts/test_hosting_smoke.py` and a separate observer |
| 24 | Low | Prelude extraction collateral changes | Plan decision 2 and T004 list them |
| 25 | Low | `gh api` cannot provide header-based classes | Research and plan qualify the claim |
| 26 | Low | Unused fields in the Sources table | `permissions` and `enforcement_level` removed |
| 27 | Low | Which read fills `github_reported` | First read (FR-010, data model) |
| 28 | Low | `rule-not-evaluated` for disabled classic fields | Only when `enabled` is true |
| 29 | Low | `merge-queue-required` and `blocked-unexplained` | May-wait and info items never suppress it |
| 30 | Low | Review with null `user` | Shape failure of the `reviews` source |
| 31 | Low | Example evidence incomplete | Contract example adds the `base-branch` entry and `rule_limit` |
| 32 | Low | Quickstart example stale | Quickstart uses a placeholder pull request |

## Round 2

Independent reviewer: read-only Opus review, revision 5fa4c12, 2026-10-16, with fresh read-only measurements on `cli/cli` and `cli/go-gh` (compare `page=2` correct for ahead 3, ahead 1, identical and behind-only; ruleset-only branches report `protected` true and `protection.enabled` false). Verdict: NOT READY (3 high, 5 medium, 9 low). Round-1 resolutions were confirmed present except 17 and 22 (partial) and 4, 19, 20, which later findings showed contradicted or undecidable.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | High | Contract examples did not follow the normative summary templates; human output had non-allowlisted lines | Data model gains the fixed human lines table and `branch-rules-unreadable` template; both contract examples regenerated from the templates |
| 2 | High | One 404 had two status outcomes | Decided: any optional-source 404 is outcome `not-found`, reason `not-found-ambiguous`, result stays complete; only the pull request reads treat 404 as failure; Degradation rule |
| 3 | High | Degradation rule unsatisfiable for rows with no unknown variant | Per-source absorbing-item table; new item `branch-rules-unreadable`; information rows over a degraded source are not produced |
| 4 | Medium | Worst-state order and competition counting undefined | Total order `failing`, `pending`, `missing`, `neutral-or-skipped`, `successful`; competition counted after the per-context and per-app reductions |
| 5 | Medium | Detail keys not nullable for degraded items | `behind_by`, `ahead_by`, `required`, `approvals`, `changes_requested` nullable; null renders as `unknown` |
| 6 | Medium | `ref-unsupported` vs `dependency-unavailable` | `ref-unsupported` takes precedence for sources carrying the base ref and the items they feed; T007 row |
| 7 | Medium | R incomplete when classic or rules unreadable | R complete rule mirrors the Required-set rule; lower-bound shortfall stays conclusive; `required_complete` detail key |
| 8 | Medium | Plan order contradicted the protection skip and repository dependency | Plan decision 4 reordered: `repository` always, `base-branch` before `branch-protection` with the skip |
| 9 | Low | Plan "50 rule entries" | 50 distinct rulesets |
| 10 | Low | `files` is absent on page 2, not empty | research M11; decoder must not require `files` |
| 11 | Low | check-suites open question unlisted | Added to Unverified and untestable |
| 12 | Low | Equal-subject ordering | Tie-break by `detail.app_id`, null first |
| 13 | Low | Status line stale | Updated |
| 14 | Low | Review row order downgraded a conclusive shortfall | Shortfall row precedes `changes_requested` |
| 15 | Low | `observed` definition too narrow | Covers combinations the row names |
| 16 | Low | CI read with no known context | Not read (`not-needed`); `required-checks-unknown` carries the gap |
| 17 | Low | `merged` missing from resample list | Added to FR-010 |

## Round 3

Independent reviewer: narrow read-only Opus re-check, revision fb46ecd, 2026-10-16 (artifact reading and comparison with the hosting schema and fixtures; no live API calls). Verdict: NOT READY (3 high, 4 medium, 6 low). Round-2 resolutions 1, 4, 5, 6, 8, 9, 10, 11, 13, 14, 15 and 16 confirmed; 2, 3 and 7 introduced new defects; 12 and 17 were only partly carried into plan and tasks. The contract JSON example was confirmed valid against the data model (arithmetic, order, request sum, summaries equal templates). All findings were resolved in the artifacts after the review; the resolutions below were **not** re-reviewed because the loop ends after three rounds.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | High | `review-required`: first row fired when `reviews` was skipped; bound 0 with R incomplete undecided | Row order: bound 0 and R complete first (no item, `reviews` skipped); unknown row only when `reviews` was needed and not `ok` or no bound; bound 0 with R incomplete counts as no bound |
| 2 | High | Degradation rule suppressed items that readable sources prove | Rewritten: every other row is still produced from readable sources; only the degraded source's contribution is omitted |
| 3 | High | Resample failure vs 404 rule; single outcome for two-read sources | Data model: first `ok` then failed resample gives outcome `failed`, `source-failed`, class kept, `incomplete`; Degradation rule names the exceptions; T010 |
| 4 | Medium | `classic-protection-unreadable` wording, reasons, example fields | Template "may apply"; all reasons listed; contract example lists the seven fields |
| 5 | Medium | `rule_limit` null for a known absence | Empty array (`none`) when `branch-rules` is `ok` with no limit; null only when not `ok`; examples updated |
| 6 | Medium | `unreadable` vs a decisive readable record | A readable `failing` or `pending` record wins (`waits`, `inferred`); `unreadable` only when nothing readable is worse than `successful` |
| 7 | Medium | No human form for error results or null parameters in fixed lines | Error line set added; null renders `unknown` in fixed lines too |
| 8 | Low | `required-checks-unknown` reason picked a skipped source | First source in table order whose outcome prevents completeness |
| 9 | Low | `dependency-unavailable` impossible for `branch-rules-unreadable` | Removed from its reasons |
| 10 | Low | Template and detail table order | Moved to the Items order |
| 11 | Low | Plan and T010 omitted tie-break and `merged` | Added |
| 12 | Low | Plan summary "no existing contract changes" | Reworded |
| 13 | Low | US5-2 ignored other review rows | Qualified |

## Round 4

Independent reviewer: read-only Opus review, revision 95b8c4d, 2026-10-16. Verdict: READY (0 critical, 0 high, 3 medium, 9 low). The round-3 resolutions were confirmed. All twelve findings below were resolved in the artifacts (not only here) after the review and committed afterwards; those edits were not re-reviewed.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | Medium | `canceled` inside the failed-resample rule | FR-010 and the data model's two-read rule exclude `canceled`; cancellation at any point is `error` with `hosting-canceled`; T010 case |
| 2 | Medium | Resample of an unreadable `base-branch` | Requested only when its first read was `ok`; otherwise the first outcome stays; T010 case |
| 3 | Medium | Guard vs builder responsibilities | Plan decision 3 and T003 limit the guard to a structured parser and coarse classes; ref grammar belongs to the builder (T009 ref-grammar table) |
| 4 | Low | Degradation rule scope | First sentence scoped to the absorbing-item rows; T007 asserts other rows are still produced |
| 5 | Low | Resample sentence in the Source field list | Moved into its own two-read rule |
| 6 | Low | `dependency-unavailable` for classic protection | Removed; reason comes from `branch-protection` (or `base-branch` when it was not `ok`) |
| 7 | Low | `review-required.detail.required` null | Data model says null when no bound is known; T007 rows |
| 8 | Low | merge-methods sources and field count | `branch-rules` added to examples; spec uses the three merge-method fields consistently |
| 9 | Low | `head-behind-base` template with unknown counts | Conditional wording in the data model; T007 |
| 10 | Low | Stream of the `Error:` line | Named: standard error; standard output also carries it in text mode only |
| 11 | Low | Null `head.repo` in decoder table | Added to T009 |
| 12 | Low | US2-3 wording | "a `401` or a non-rate-limit `403`" |

Also recorded (coordinator request): T002 and plan decision 3 state that the body is read under its cap before the status is mapped, so an oversized error body on a `404` is classed `too-large`.
