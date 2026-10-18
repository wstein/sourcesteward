# Constitution amendment for release automation (decided by the owner; applied in PR #58, in effect after merge)

> **Decided by the owner on 2026-10-17; applied in pull request sourcesteward#58. Until that PR is merged the constitution text is unchanged and WI6 to WI11 stay blocked.**
>
> **Status: decided by Werner Stein on 2026-10-17; the text below is the proposal that pull request sourcesteward#58 applies. It takes effect in the constitution when #58 is merged; this feature itself does not edit the constitution.** The rationale and impact assessment are kept for the record.

## Rationale

Constitution 1.1.0, Scope and Constraints, lists "push/merge/release automation" among things that "require later specifications", and Development Workflow says "Future merges, releases and external mutations require authorization in their task context". Spec 026 is such a later specification for one narrow kind of release automation: a CI build started by an explicit, disclosed dispatch, and a tag-triggered verification and publication of what that build produced. The two sentences leave open whether a reviewed specification is sufficient or whether the principle itself must name the exception, and the umbrella `AGENTS.md` independently requires explicit approval for builds, tags, publication and activation. The amendment states, once, the conditions under which release automation is allowed so that no per-feature exception is needed and no automation can quietly broaden.

## Proposed text

**Scope and Constraints** - replace the sentence "History editing, remote deletion, push/merge/release automation, an MCP server and GitButler writes require later specifications." with:

> History editing, remote deletion, push/merge automation, an MCP server and GitButler writes require later specifications. Release automation requires a reviewed specification that keeps building, tagging, publishing and consumer activation separately authorized each time, with no standing approval, and that satisfies the release rules of the Development Workflow. A plan, schedule, bot, push, merge or commit never authorizes a release step.

**Development Workflow** - append:

> Release steps: (1) commands that inspect, plan, verify or record a release MUST NOT execute project scripts, and project commands run only in CI on a commit that passed the declared gates; (2) every network write, remote mutation and project command execution is a disclosed effect of its command or workflow; (3) a release build runs once, and promotion verifies the recorded artifacts and attestations before publication and never rebuilds; (4) release tags are annotated, never moved or re-created; (5) the owner's explicit approval of each tag and release is required in the task context of that release; (6) consumer activation (pinning a released artifact in another repository) is a separate step outside the release.

**Governance** - bump **Version** to `1.2.0` (minor: a new principle-level constraint on a previously unspecified area) and add to the footer: "Release automation conditions added 2026-10-17 for Spec 026; core principles I to V and the Tier 1 rules are unchanged."

## Impact assessment

- **Existing contracts and specifications**: none changes. Principles I to V are unchanged. Specs 001 to 025 and 027 describe no release behavior. The older Spec 026 draft ("release hygiene") is superseded by this specification.
- **Umbrella `AGENTS.md`**: stricter or equal; builds, tags, publication and activation still need explicit approval; schedules and bots still authorize nothing. The sourcesteward `AGENTS.md` sentence "otherwise do not merge, publish, release or change remote refs" stays true: `stw` never pushes or tags, and the dispatch is a disclosed remote effect needing authorization.
- **Sibling repositories**: `sheetmill` and `bundledock` have their own recorded owner decisions for tag-driven workflows; this amendment governs SourceSteward only (Governance: "This constitution governs SourceSteward only").
- **Alternatives considered**: (a) no amendment, treat the specification as the exception - weaker and invites per-feature drift; (b) a patch-level clarification only - the new conditions are constraints, not clarifications; (c) amend only the Scope sentence - leaves the release rules unspecified for later features (package channels, separate follow-up specifications).

## Decision

The owner decided on 2026-10-17 to apply the amendment; pull request sourcesteward#58 applies it. The alternatives that were offered (apply with changes, or reject and re-plan WI6 to WI11 as a non-automated recipe) were not chosen.
