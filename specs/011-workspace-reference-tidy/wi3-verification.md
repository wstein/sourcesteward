# WI3 Verification: Owned Workspace Retirement

## Scope and design
Retire only completed owned native Git/jj tasks through explicit plans and authorization.
Full physical manifests include every admitted metadata/file node, binary content, modes and
native identities; dirty/untracked/ignored/unknown payload, aliases, symlinks, hardlinks, conflicts
and incomplete proof retain. Registration and exact file removal have distinct authority/effects.
Git requires both for native coupled removal; jj forget preserves files until separate authorization.
No native initialization, source snapshots, raw Git fallback for jj, abandonment, GC or rollback.

Plan/tasks and independent analysis cleared at 84455ef before source work; all native authors
confirmed feasibility. Six author worktrees separated core, manifest, Git, jj, CLI/schema and CI.
Existing published schemas including tidy v1 remain unchanged; owned retirement publishes v2.
Independent source/security review cleared 0fb7e0c including no-write-before-proof sequencing,
exact digest framing, readonly native completed-retry proof, missing-capability preflight and
strict combined bounds. Recovery pins preserve selected committed history, not full old metadata.

## Executed checks
- Full make check started at aa44ddc and passed after integrated fixes: Go tests/vet/module verification, 56 Python,
  12 JavaScript, documentation/import and diff checks, current Git/jj with mandatory sandbox.
- Full go test -race ./... started at 4ae8a44 and passed; actual native schema fixtures cover Git and both jj layouts.
- Final bounds/unknown-plan no-write regressions passed after the full-check run; independent
  reviewers reran those exact tests. Valid native effect paths are unchanged by these last guards.
- Native authors passed minimum/current Git retirement/sandbox and jj standalone/colocated
  lifecycle tests. Git tests actively attempt writes to protected source/config/index/siblings.
- Mandatory real CLI smoke-retirement.py passed all 36 cases in EACH minimum/current lane
  against runtime aa44ddc: Git 2.46.0/jj 0.45.1 and Git 2.56.0/jj 0.46.0, no skipped cases.
  Final invalid-input guards were independently tested after these runs; supported native paths
  are unchanged. A final failing-first CLI help correction passed workspace/discovery tests.

## Review findings and limits
Initial integrated Git effects completed but source postvalidation used an older digest framing;
a failing real public lifecycle/retry regression proved the fix. Completed retry now separately
verifies native recovery and registration absence, not merely a stored journal fingerprint.
Invalid/unknown plans and overlapping state refuse before creating state files. Native and raw
filesystem checks remain sampled, not atomic; external rename/ABA races cannot be excluded.

Local macOS evidence does not establish hosted Linux acceptance. Exact PR-head source and both
native CI lanes remain mandatory, including Linux mount/root/sibling confinement. No real user
repository references, registrations or files were deleted during tests. Owner merge, publication
and any cleanup of development worktrees remain separate actions.

Final cross-artifact analysis cleared 2998009 plus the scoped evidence/status updates.
Final source review separately cleared the no-write dispatch guard 0fb7e0c and CLI guidance 2998009.

WI3 handoff: [PR #38](https://github.com/wstein/sourcesteward/pull/38), stacked on PR37.
All local tasks are complete; exact-head hosted Linux checks and owner review/merge remain pending.
