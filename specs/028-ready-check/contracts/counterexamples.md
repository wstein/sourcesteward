# Adversarial design vectors for the plan corrections

These vectors are required future regression tests. No publisher or Ready implementation exists yet; schema and byte-comparison checks below do not establish runtime or hosted acceptance.

## R1: a prior success must not survive an observed invalidation

All rows start with an existing successful owned check at `(repository 123, head e…e, review-evidence, App 789)` and a reachable write channel unless stated otherwise. Before reevaluation, revoke that success and confirm blocking state. Only a complete passing sample may produce a new success.

| Input after prior success | Required outcome |
| --- | --- |
| Selected report edited without matching status digest | Previous success revoked; completed/failure; report-binding-mismatch |
| Selected report deleted | Previous success revoked; completed/failure; report unavailable |
| Newest trusted review status becomes failure or pending | Previous success revoked; completed/failure; review-not-clear or pending |
| Local lock gains an unauthenticated transition | Previous success revoked; completed/failure; activation-mismatch |
| Review/status/activation read fails | Previous success revoked if writes remain usable; completed/failure with unknown/error reason, not neutral/skipped |
| Cancellation after confirmed blocking state | Retain blocking state or failure; never restore old success |
| Revocation write/readback fails | No later success write; nonzero enforcement-unknown with affected key/ID; do not claim remote revocation succeeded |
| Stale attempt resumes after a newer writer | Cannot write without reacquiring shared ownership and reevaluating; never replay old success/failure payload over the newer result |

A simulator or test that only checks "no new success was emitted" is insufficient: assert the old successful run became blocking, or that the explicit unconfirmed-enforcement path was taken when writes could not succeed.

## R2: the host's check identity is shared across PRs

The association reader enumerates all open repository PRs; filtering only a branch, selected PR number or possibly incomplete commit-association endpoint is not proof.

| Before / observed change | Required outcome |
| --- | --- |
| PR42 alone at head H | Singleton prerequisite can pass for PR42, subject to all other evidence |
| PR42 and PR43 open at H, different bases or branches | Shared key fails; invalidate its success; neither PR receives a v1 success |
| PR43 opened or reopened at H after PR42 success | Invalidation event revokes the existing shared success before evaluation; collision fails |
| PR43 retargeted while remaining open at H | Still a collision; retargeting does not give it a different check identity |
| Only PR42 before evaluation; PR43 appears before success write | Final complete association resample detects change; failure, no success |
| PR43 appears between success write and fixed post-write resample | Observed collision revokes just-written success; failure |
| Association page missing, malformed or at a full final bound | Unknown is blocking; never infer singleton from collected prefix |
| Two events for PR42/PR43 execute concurrently at H | Both map to the same repository/H/context/App writer lock |
| PR43 appears after the last resample and event is lost | Disclosed residual platform race; not a claim of atomic isolation or a successful observed revocation |

Assert that adding PR number to JSON/external_id does not change the controller's shared serialization key or the host's required-check matching. CLI remains read-only and reports the same singleton predicate failure; only publisher performs revocation.

## R3: authenticating a field subset is insufficient

[activation.json](examples/activation.json) represents fictitious authenticated baseline bytes with transition=null. [lock-with-transition.json](examples/lock-with-transition.json) preserves every other field and adds a well-shaped owner-transition claim. Both validate against [lock.schema.json](lock.schema.json). Their complete bytes/digests differ, so the latter must not authenticate against the former despite matching generation, policy and checker.

Starting from a hypothetical authenticated lock-with-transition, separately change only each transition field (repository_id, number, head_sha, old_generation, old_policy_sha256, new_policy_commit, new_policy_sha256, new_checker_source_sha, new_checker_artifact_sha256). Every shape-valid mutation must fail full activation equality. Replace transition with null: shape-valid but unequal. Omit transition: schema-invalid. Change only whitespace: raw-byte unequal. Alter the candidate channel/environment: refuse against the independently accepted bootstrap rather than reading the attacker-chosen channel.

Repeat these mutations between the initial and final samples: reject sample drift and revoke an earlier published success through R1. No matching policy generation shortcut may bypass comparison. These vectors test the intended comparison rule; actual reader/parser behavior must be implemented test-first later.
