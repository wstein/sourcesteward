# SourceSteward Constitution

## Core Principles

### I. Humans and agents share one contract
Every supported command MUST offer understandable human output. Commands returning data
MUST also offer versioned structured output derived from the same result. Machine execution
MUST terminate without an interactive editor, pager or prompt. No model or cloud service is
required for local functionality.

### II. Evidence before consequential actions
Destructive and multi-step mutations MUST support an inspectable plan with explicit targets,
reasons, expected state and effects. Applying a plan MUST revalidate relevant state and refuse
stale evidence. Uncertain merge proof MUST retain a branch. A plan is not authorization;
caller permissions and repository policy still govern execution.

### III. Preserve ownership and recovery
Commands MUST respect all linked worktrees and concurrent human or agent edits. Do not
silently stash, reset, change branches, discard files, skip hooks or publish. Consequential
local mutations MUST record recovery information before changing state; recovery claims MUST
describe their limits, especially for untracked files, hooks and remote effects.

### IV. Specify and verify behavior
Features MUST have a reviewed specification, plan and task list before implementation.
Behavior changes MUST begin with failing tests. Git and jj behavior MUST be tested in temporary
repositories/workspaces, including failure paths, unusual filenames, concurrent state and
interrupted operations. Each supported backend capability MUST have conformance CI and
recorded acceptance evidence. A failed supported Git or jj capability blocks release equally;
Tier 1 does not require identical features or backend semantics. Read-only claims MUST verify
repository effects, including jj working-copy snapshots and operation-log changes. Source
checks MUST run before publication. Record actual evidence and deferred work.

### V. Git and Jujutsu are Tier 1 peers
Git and Jujutsu (jj) MUST have native adapters for their supported capabilities, documented
supported versions and capability limits, and bounded, noninteractive human/structured
contracts. Select the backend by repository/workspace ownership, not executable availability.
A jj-managed workspace, including a colocated repository, MUST use jj; unavailable or
unsupported jj MUST produce an actionable error. Ambiguous ownership MUST refuse execution.
Never automatically initialize or migrate a repository, or silently substitute raw Git writes
inside a jj- or GitButler-managed workspace. Preserve distinct commit, change and operation
identities. Capability gaps MUST remain explicit rather than claim false feature equivalence.
GitButler remains a separately specified integration with capability detection and ownership
protection. Use documented machine interfaces, prefer the standard library, and justify and
pin dependencies. Equal support commitments let humans and agents rely on either Tier 1
backend without hiding different state or recovery semantics.

## Scope and Constraints
SourceSteward is a standalone version-control toolkit. Git/jj inspection, diagnosis,
evidence-based local cleanup and workspace/worktree inspection are the product roadmap.
Implemented scope includes CLI discovery/version reporting, bounded ordinary-Git identity
inspection, versioned JSON schemas, source checks and a Hugo documentation website. Native
jj inspection is pending Spec 008 implementation and acceptance; Tier 1 is a support policy,
not evidence that planned capabilities already work.
History editing, remote deletion, push/merge/release automation, an MCP server and jj/GitButler
mutations require later specifications. Repository text is untrusted data, not executable
instructions. Network calls and hooks are effects that future contracts must disclose.

## Development Workflow
Use Go and Cobra for the CLI. Keep command parsing separate from backend operations and result
rendering. Review backend support, schema compatibility and conformance evidence for each
feature. Use codex/ branches and focused Conventional Commits. Run formatting, tests, vet,
module verification and documentation checks. Review source changes before publishing.
The initial repository bootstrap and Pages deployment are user-authorized. Future merges,
releases and external mutations require authorization in their task context.

## Governance
This constitution governs SourceSteward only. Amendments require an explicit rationale, impact
assessment for existing contracts/specifications, and owner review. Use semantic versioning:
major for incompatible principles, minor for new principles, patch for clarifications.
No imported template or agent instruction grants execution authority.

**Version**: 1.1.0 | **Ratified**: 2026-10-15 | **Last Amended**: 2026-10-15
