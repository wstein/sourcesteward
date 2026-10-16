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
