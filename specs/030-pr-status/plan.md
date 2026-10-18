# Implementation Plan: PR status

**Status**: Incomplete design checkpoint, not implementation authorization.

## Existing code evidence

Inspected base `2e9b9fc99888b605bcce08afa6ef98846359e06a`:

- `internal/hosting/result.go`: `PullRequest` lacks merge evidence; `Result` already includes `observed_at`. `Options` serves repo/pr/ci.
- `internal/hosting/operations.go`: `readPR` is shared by PR and CI including CI resampling. `fullSHA` accepts exactly 40 lowercase hex. `inspect` starts a single 30-second context before native ownership and authentication; JSON output checking is internal, but CLI delivery happens afterward.
- `internal/cli/hosting.go`: shared `stw hosting`/`gh jj` registration, detailed rendering, one JSON object, runtime failure after result rendering. No implicit polling.
- `schemas/v1/hosting.schema.json`: strict published shape; an additive field is not compatible with existing validators. Expanded PR output requires a new version or explicit format selection, not an unversioned additive change.
- `internal/hosting/transport.go`: fixed GET endpoint guard, caller-owned gh authentication, one MiB raw and normalized body caps, request context, redirect/compression refusal. Per-client timeouts must not replace the operation's global deadline.

## Initial design direction

Retain 30 seconds rather than silently raising established bounds. Prefer one final result in both one-shot and watch modes rather than adding an implicit NDJSON stream. Watching reuses one resolution/authentication session and the fixed numbered PR GET only. Settle final schema/CLI compatibility and writer deadline behavior in the next design batch; these are proposals, not frozen contracts.

## Dependency and ownership ledger

| Item | Owned now | Before implementation |
| --- | --- | --- |
| Spec 030 design | This worktree, `specs/030-pr-status/**` only | Complete artifacts and independent strong review |
| Shared transport / PR reader | No edit ownership | Dispatch grants named files after reconciling current F tidy and Ready changes |
| Published schema / CLI | No edit ownership | Explicit compatibility choice and handoff after design gate |
| Specs 028 and 029 | Other active work | Do not reserve or edit their paths |

No runtime, transport, public docs, status registry or gitlink edits in this checkpoint. No source/artifact/site/image/CLI build, installation, CI dispatch, push or outward write is authorized by this handoff.
