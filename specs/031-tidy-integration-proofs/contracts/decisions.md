# Decision file contract

Subordinate to the [specification](../spec.md) FR-039..FR-041.

A decision file is written by a human or an AI reviewer after reading the [review report](review-report.md). It is evidence input to `stw tidy plan --decisions FILE`, never authorization.

## Format

```json
{"schema_version": 1,
 "decisions": [
  {"branch": "refs/heads/topic", "tip_oid": "<full OID>", "base_oid": "<full OID>", "decision": "integrated",
   "reason": "all hunks already present in main as commit abc", "reviewer": "claude", "kind": "ai"}
 ]}
```

| Field | Rule |
| --- | --- |
| `branch` | full ref under `refs/heads/` |
| `tip_oid` | full OID; must equal the ref's current tip |
| `base_oid` | full OID; must equal the plan's base OID (a different base makes the decision stale) |
| `decision` | `integrated`, `keep` or `drop` |
| `reason` | non-empty for `integrated` and `drop`, at most 1,024 bytes (data, escaped on output) |
| `reviewer` | non-empty, at most 128 bytes |
| `kind` | `human` or `ai` |

## Limits and refusals

File at most 1 MiB, at most 1,024 decisions, absolute path to a regular file (no symlink, no special file). Usage error (exit 2, before any native work, message names the selector and the offending decision index): unreadable or oversized file, malformed JSON, unknown `schema_version`, unknown field (anything but the fixed fields is refused, so report content cannot smuggle data), missing field, invalid enum, duplicate `branch`, missing reason where required.

## Consumption

- `integrated` on a `needs-review` ref: deletable, class `reviewed-integrated`.
- `drop` on a `needs-review` ref: deletable, class `reviewed-drop`.
- `keep`: `retained`, disposition `reviewed-keep`.
- A decision whose `tip_oid` differs from the current tip or whose `base_oid` differs from the plan's base OID: ignored, plan note `stale-decision` naming branch and both OIDs.
- A decision for a ref whose report entry was omitted by the entry bound, or whose commits or unintegrated diff were truncated by a bound (the reviewer did not see all of it): ignored with `decision-not-applicable: not-fully-reviewed`; split the review per `--ref` or per named consolidation range, or raise the overridable `--report-max-*` bounds up to their ceilings (the 50-commit, 1,000-line and 16-file entry bounds are fixed; see Known limits in the spec).
- A decision for a protected ref, a hard stop (`history-override`, `unsupported-metadata`) or a ref that is `proven` or absent from the plan: ignored with plan note `decision-not-applicable` and the reason.
- The decision (reviewer, kind, decision, reason, tip OID) is recorded in the plan and in the recovery journal next to the Spec 029 recovery ref; `kind: ai` is marked so the owner can see which decisions were not human. The plan identity includes the decision file digest.

## Limits of a decision

A decision never authorizes anything. `stw tidy apply` still requires the existing explicit owner authorization and recovery journal, re-checks the exact tip OID and all protections, and keeps the recovery limits: the recovery ref preserves the tip commit history only, not the reflog or `branch.<name>.*` configuration; restoring is manual; the ref exists only in this clone.
