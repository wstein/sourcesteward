# Tasks: agent attribution (Spec 042)

**Input**: [specification](spec.md). Draft: the tasks start after an independent review of the exact head. Tests first.

- [ ] T001 `internal/attribution` tests and code: `FromEnviron` (kinds, session source, unsafe id dropped) and `Gate` (reasons, no id in the message).
- [ ] T002 Workspace tests and code: `ApplyOptions.Origin`, `TaskRecord.Origin`, written once, no id in any git artifact, no secret in any state file.
- [ ] T003 CLI tests and code: `originForRun` over the existing detection, `workspace apply` passes the origin, `remoteWriteGate` exits 1 with the reason.
- [ ] T004 Wire `remoteWriteGate` into the first stw command that pushes or sets a PR Ready (outside this specification until that command exists).
- [ ] T005 Add the Codex session source once measured; until then absent.
