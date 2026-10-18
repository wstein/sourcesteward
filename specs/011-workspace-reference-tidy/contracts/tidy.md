# Tidy CLI Contract v1

```text
stw tidy plan [PATH] --base REF [--state-dir ABS] [--ref NAME ...] [--json]
stw tidy apply --plan PLAN_ID --state-dir ABS --authorize [--json]
stw tidy plan [PATH] --base REF --state-dir ABS --task ID --owner OWNER --remove-registration [--remove-files ABS] [--json]
```
PATH defaults to dot. WI1 exposes reference planning only; WI2 adds apply; WI3 adds owned retirement
selection. Repeat --ref selects literal local refs (Git branch or jj bookmark), omitted selects bounded
local references. Workspace flags and --ref are mutually exclusive. Retirement requires exact task,
owner and registration selection; optional --remove-files must equal the recorded owned destination.
Reject unrelated flags, extra operands, malformed/beyond-bound input, arbitrary native arguments or
stdin plans. Base is explicit; no main/master/origin inference. If supplied, state dir must already exist privately; omitted means stateless observation.

Plan optionally persists immutable private records but changes no native refs, registrations, files or jj
operations. Unsupported squash/forge evidence retains; no network is contacted. Authorization flag
is apply-only, required and never implied by a plan. Apply loads the exact immutable plan, validates
all current relevant state per action, records durable recovery, creates native recovery roots and
executes only eligible listed local effects. No remote refs, GC, abandon, stash/reset or hooks.

Exit0: complete observation/apply (including complete retained candidates or verified completed retry).
Exit1: refusal, partial/unknown/incomplete evidence or operational failure. Exit2: CLI syntax errors.
All operational success/error/refusal/partial JSON results validate independent tidy schema v1;
help/syntax usage remain Cobra text. Human output safely quotes names/paths and shows plan ID,
backend/base, every decision/proof/blocker, explicit effects, action outcomes and recovery location.

Git deletion preserves a private native recovery ref then performs target old-OID CAS. jj purely
local bookmark deletion preserves a native recovery bookmark and uses pinned/sampled operations;
any remote counterpart retains. Neither backend promises an external transaction. Private recovery
roots remain after apply and may require later explicitly reviewed manual removal.

WI3: registration removal and filesystem removal have separate actions/authority. jj forget preserves
files. Native Git cannot forget a present checkout alone, so registration-only retains unsupported;
both exact flags permit the declared native combined effect when safety is proven. Never remove
current/foreign/dirty/untracked/ignored-extra/unknown state. Full stored/disk tree proof is bounded;
unsupported node types retain. jj one-parent empty WC proof additionally requires parent integration
and tree equivalence, never empty alone. No automatic restoration/cleanup after partial failure.

Reads10seconds/apply60seconds/nativeoutput64KiB/recordresult1MiB/aggregate1024 entries.
Paths/selectors4096bytes, IDs128bytes. Missing mandatory capability, sandbox, command output or
freshness refuses; no unbounded fallback. Cancellation after start reports known partial effects.

The v1 command discriminant reserves tidy-plan and tidy-apply from WI1 publication; WI1 does not
expose apply until WI2 acceptance. WI3 uses a separate v2 retirement contract rather than expanding
strict v1 candidate enums/fields. Published v1 bytes and prior fixtures remain unchanged.

## Issue #62: Git checkout layout admission

Primary and linked Git sources use the
[Spec 010 layout contract](../../010-isolated-task-workspaces/contracts/workspace.md#git-checkout-layout-admission-issue-62)
through observation, persisted-plan validation, recovery and apply. A validated shared
`core.worktree` names the primary checkout; it does not replace the current linked identity
or authorize primary writes. Incomplete mapping/workspace protection or changed fingerprint
refuses before the next effect. Reference/retirement schemas, action authorization, retention
and recovery semantics remain unchanged. This amendment is pending implementation and
independent acceptance; existing native jj bookmark deletion retains remote counterparts,
including untracked remote targets, and never abandons changes.
