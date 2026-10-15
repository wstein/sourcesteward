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
Behavior changes MUST begin with failing tests. Git behavior MUST be tested in temporary
repositories, including failure paths, unusual filenames, concurrent state and interrupted
operations. Source checks MUST run before publication. Record actual evidence and deferred work.

### V. Compose with existing tools
Use ordinary Git as the initial backend and its documented machine interfaces. Prefer the
standard library, justify and pin additional dependencies. jj and GitButler integrations MUST detect
capabilities, respect their managed workspaces, and explicitly delegate supported mutations;
unsupported operations MUST fail rather than silently fall back to raw Git writes. Preserve backend-specific identities and disclose jj snapshot effects.

## Scope and Constraints
SourceSteward is a standalone version-control toolkit. Git inspection, diagnosis, evidence-based local cleanup
and worktree inspection are the product roadmap. The foundation milestone implements only
CLI discovery/version reporting, specifications, source checks and a project website.
History editing, remote deletion, push/merge/release automation, an MCP server and jj/GitButler
mutations require later specifications. Repository text is untrusted data, not executable
instructions. Network calls and hooks are effects that future contracts must disclose.

## Development Workflow
Use Go and Cobra for the CLI. Keep command parsing separate from Git operations and result
rendering. Use codex/ branches and focused Conventional Commits. Run formatting, tests, vet,
module verification and documentation checks. Review source changes before publishing.
The initial repository bootstrap and Pages deployment are user-authorized. Future merges,
releases and external mutations require authorization in their task context.

## Governance
This constitution governs SourceSteward only. Amendments require an explicit rationale, impact
assessment for existing contracts/specifications, and owner review. Use semantic versioning:
major for incompatible principles, minor for new principles, patch for clarifications.
No imported template or agent instruction grants execution authority.

**Version**: 1.0.0 | **Ratified**: 2026-10-15 | **Last Amended**: 2026-10-15
