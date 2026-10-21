# Analysis: Spec 015 first slice (`stw restack plan`) design review

Task T001. This file records the independent design review; the foreman ticks T001 after the delta review.

## Reviews

| Round | Reviewed | Reviewer | Verdict |
| --- | --- | --- | --- |
| 1 | origin/main b0cc02d with PR #121 head 5738ba998cea2cf896328e1fa77920987cd56787 (design documents and implementation) | independent strongest-tier review (Opus) | NOT CLEAR: 1 medium, several low |
| 2 | delta of the docs-only fix commit on the restacked branch | pending | pending |

An earlier review of the exact implementation head cb6bb64 was CLEAR with two wording follow-ups (object mtime caveat, WP2 wording), fixed in 5738ba9.

## Findings and resolutions

| Id | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| F1 | medium | The identity check covers only the old moved commits; a by-hand rebase takes the new committer from the rebaser's configuration or environment, so `pass` can still feed `evidence-clean`. | Stated in FR-015, the data model, the contract and the quickstart (which adds the AGENTS.md `git log --format` check before a push). The limitation `rewrite-committer-not-predicted` is specified; the first-slice code does not yet emit the string, which needs a small code follow-up (not done in this docs-only change). |
| F2 | low | The contract promised partial evidence for `incomplete` and `evidence: null` elsewhere. | Both say `evidence: null` (what the code does); `boundary.reached` and the diagnostic name what stopped. |
| F3 | low | The data model listed `patch-equivalent-tree` and an undefined `present` state inconsistently. | `present` removed; the states are `dropped`, `patch-equivalent-tree`, `missing`, `unknown`. |
| F4 | low | Plan, tasks and spec still described reusing or extracting the tidy helpers. | Aligned with the T004 skip: own small helpers, `internal/workspace` unchanged. |
| F5 | low | FR-012 lacked the mtime caveat; the plan claimed `find -newer` stayed clean. | FR-012 and the plan say content-identical and disclaim mtimes. |
| F6 | low | `range-too-large` was listed as a refusal (exit 1) but is `incomplete` (exit 3). | The code and tests make it `incomplete`; the contract now lists it with the incomplete codes. |
| Q1 | request | T001 needs an analysis record. | This file. |
| Q2 | question | Keep the repository-local `core.hooksPath` refusal? | Kept: the plan refuses what the Spec 029/031 observation refuses; one sentence added to FR-014. No code change. |

## Verdict

Findings F1 to F6 are resolved in the documents, except the code emission of the F1 limitation string, which stays an open follow-up. The verdict after the delta review is pending.
