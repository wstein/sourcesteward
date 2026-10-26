# Prune limit and digest follow-up evidence

Base: merged PR #290, main `9e13078828193eda1df29c2504e27ffe32971e73`.
The [Opus delta](https://github.com/wstein/sourcesteward/pull/290#pullrequestreview-5646789916)
closed F1-F5 and withdrew F6 on the accepted head `426a4ce2`. This follow-up
adds the owner's explicitly requested maximum-size evidence and records the
recovery-limit digest effect. It changes no product behavior, accepted bounds,
published schema or completion checkbox.

## Actual accepted byte limits

`TestRegistrationPruneV4RemovalBudgetAtAcceptedByteLimits` creates a real
registration with a 64 MiB metadata file and exactly 256 MiB of total metadata.
It uses admitted `index`, `logs/HEAD`, `ORIG_HEAD` and `config.worktree` files;
other required metadata remains present. The normal planner must accept that
exact total, and apply must create and verify complete private recovery.

The existing measurement seam models a slow full verification pass without
sleeping or depending on host disk timing. Budget admission must then return
`prune-metadata-limit` before the first unlink. A fresh complete metadata
inventory must retain every original digest and physical identity, and the
full recovery payload must still verify. The prepared journal and history pin
may exist; zero unlinks is not a claim of zero local effects.

`TestRegistrationPruneV4RemovalBudgetAtEntryLimit` exercises the 1,024-entry
budget boundary directly: a fitting measured pass is admitted, an insufficient
budget refuses, and entry 1,025 refuses even with zero measured cost. The current
native metadata allowlist is smaller than 1,024, so this unit case does not claim
that a fabricated 1,024-path native registration is admissible.

These characterize the already accepted budget guard. Later disk stalls and
same-user races remain documented limitations; no new deadline or recovery
policy is introduced.

## Recovery-limit wording is plan data

The original `96503f8d01db0d4e860d61ff6db04929a90b552f`, replayed as
`216991d0` in merged main, changed both the human apply guidance and the first
`recovery_limits` sentence. It was therefore a runtime/data change despite its
`docs(cli)` type. The owner had already merged it before this follow-up began;
accepted history remains unchanged under the repository's history rule.

The `recovery_limits` field participates in
`pruneV4Digest("stw-registration-plan-v4", plan, "digest")`. Changing that text
changes the digest for an otherwise identical plan. New plan bytes and their
digest must agree; stored immutable plans are not rewritten, relabeled or
silently migrated to the new wording.

`TestRegistrationPruneV4RecoveryLimitTextIsDigestBound` substitutes the exact
historical sentence, checks the digest changes, and requires
`prune-plan-refused` with the exact reason when the original digest is reused.
It also checks the original stored bytes and reader remain unchanged. This
isolates the wording's digest effect; other witness/version checks remain
independent.

## Verification

Run `go test ./internal/workspace -run
'^TestRegistrationPruneV4(RemovalBudgetAt|RecoveryLimitText)' -count=1` and the
full `go test -timeout 40m ./...` before publication. The tests characterize
existing behavior, rather than claiming a new RED-to-GREEN product change.
The PR body records the measured results and required exact-head review/CI.
No real-user prune, manual restore, standalone build or owner-host acceptance
is claimed. T506-T508 remain separate work.
