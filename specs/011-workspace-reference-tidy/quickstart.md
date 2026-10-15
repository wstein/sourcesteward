# Tidy Validation Guide

Use disposable fixtures only; never test destructive operations in the development checkout.
Run `go test ./...`, `go vet ./...`, `go mod verify`, `make check`, race tests and both mandatory
pinned minimum/current native lanes. Existing workspace/inventory/inspection conformance stays enabled.

WI1: create temporary Git branches/jj bookmarks that are integrated and unique; plan with explicit
base/private state. Verify native state byte/mode identical, all decisions/limits visible, same human
and JSON evidence, schemas valid, missing base/backend and selected base/workspace-used refs retained.

WI2: plan eligible local refs, apply without authorization (refuse), then authorized apply in the
disposable fixture. Verify only targets deleted, durable recovery roots point to exact originals,
journals precede action. Move target/base/op/registration between plan/apply and verify refusal.
Inject persistence failure/cancel/interruption; completed effects remain reported, uncertain retry
retains. jj remote counterparts/conflicted bookmarks and Git symbolic/hooks retain/refuse.

WI3: create owned tasks through Spec010. Plan registration-only and exact file removal separately;
verify jj forget preserves bytes and Git registration-only reports unsupported. Combined Git and
explicit jj files removal require proven clean exact owned paths. Dirty, untracked, ignored extras,
foreign owner, active workspace, replaced inode, symlink/submodule/sparse/conflict or oversized state
retains. Native jj empty working-copy case proves single integrated parent plus tree/disk equality.
Sentinels in siblings/parent/source/store/unrelated registrations survive every success/failure.

Each WI's analysis and verification record must cite actual cases/revisions/commands. Missing
mandatory lane/case/sandbox evidence blocks acceptance, never counts as skipped success. PR handoff
requires independent source review; owner merge and hosted publication remain separate actions.
