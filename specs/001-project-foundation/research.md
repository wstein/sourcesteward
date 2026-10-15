# Research: Project Foundation

## Go and Cobra
**Decision**: Go 1.27.2 with Cobra v1.10.2, initialized using go mod init and a pinned go get.
**Rationale**: User-selected stack; independently verified official stable versions.
**Alternatives**: Standard flag package lacks the requested subcommand processor; Cobra CLI
code generator adds tooling without benefit for a two-command scaffold.
**Sources**: https://go.dev/dl/ ; https://github.com/spf13/cobra/releases/tag/v1.10.2

## Command contract
**Decision**: Fresh Cobra tree per invocation, injected streams, no stdin reads, one application
error boundary. Text is default; version --json emits one newline-terminated object. Usage
errors exit 2; runtime/output errors exit 1. Preserve cancellation and writer errors.
**Rationale**: Human convenience and predictable agent execution share the same operation.
**Alternatives**: Parsing colored human output, global mutable flags, automatic editors or
agent-specific behavior modes would make the contract fragile.
**Source**: https://github.com/spf13/cobra/blob/v1.10.2/command.go

## Backend readiness
**Decision**: Ordinary Git first; explicitly model jj and GitButler capabilities/effects in the
architecture now. No adapter is implemented in foundation; unsupported means unsupported.
**Rationale**: jj has change IDs, bookmarks and operation history; working-copy snapshotting
means familiar inspection commands can write. GitButler has managed workspace state and
per-command structured-output coverage. Neither is safely interchangeable with raw Git.
**Alternatives**: A universal Git-shaped interface loses backend-specific semantics; automatic
workspace migration or raw-Git write fallback violates ownership and effects contracts.
**Sources**: https://docs.jj-vcs.dev/latest/git-compatibility/ ;
https://github.com/jj-vcs/jj/blob/main/docs/working-copy.md ;
https://github.com/jj-vcs/jj/blob/main/docs/operation-log.md ;
https://docs.gitbutler.com/cli-guides/cli-tutorial/scripting ;
https://docs.gitbutler.com/ai-agents/parallel-agents

## Static Pages and exact source checks
**Decision**: HTML/CSS/SVG/local fonts, one check-and-deploy workflow; deploy depends on
successful checks and is limited to main pushes. Use exact github.sha and a static allowlist.
**Rationale**: No site-generator dependency; same-run dependency proves checked revision.
**Alternatives**: Cross-workflow deploy needs extra source identity checks; deploying from a
folder branch loses the explicit source-check gate.
**Sources**: https://docs.github.com/en/pages/getting-started-with-github-pages/using-custom-workflows-with-github-pages ;
https://docs.github.com/en/actions/reference/security/secure-use

## Provenance
Spec Kit 1.1.3 installed its bundled Codex skills and shared infrastructure. Independent
research helper verified Go/Cobra and recommended CLI/CI contracts; main author incorporated
them. Original logo geometry and page layout are newly authored. Family font files and SIL
OFL notices are copied from BundleDock's reviewed brand assets. EUPL-1.2 follows the family
projects. No upstream Git-tool implementation has been copied.
