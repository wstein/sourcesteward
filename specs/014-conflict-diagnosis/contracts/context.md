# Conflict diagnosis and resolver context contract

> **Owner-decision amendment — 2026-10-25:** all former OQ IDs are decided;
> [current profiles](../../016-stacked-pr-coordination/contracts/owner-decisions.md) supersede earlier undecided recommendations.
> Shared human/shortening-timeout flags adopted; preflight remains offline/helper-free.
> This is a documentation target, not implementation or native acceptance.

**Status**: UNPUBLISHED Markdown proposal; hunk policy/capability remains OQ-HUNK.

Diagnosis never resolves or stages paths, snapshots a jj working copy, rewrites
conflict markers, continues/aborts an operation or changes refs/index/files.
Git index stages and operation participants are native facts; absent/malformed
participants stay unknown. Native jj first-class conflicts retain their native
terms, commit/change identity and sampled operation. An interrupted Git operation
is not presented as the same state as a jj conflicted change.

Every path records existing-native or predicted-scratch origin, native type,
participants and completeness. Binary, rename/delete, directory/file, executable
mode, symlink and unknown kinds remain distinct when supported. If the native
interface cannot classify a kind, report unknown, not text conflict by guess.
A file that merely contains marker-like text is not authoritative conflict state.

Default handoff contains no blob body, diff excerpt, environment/config dump,
credential or transcript. No content is sent to a provider/model or copied to an
external application. Suggestions are bounded inert explanatory DATA, not shell
commands chosen from repository text or an automatic resolver invocation.

An accepted hunk capability must require explicit selected paths and disclose
stored-blob/disk/scratch origin. Never follow a symlink outside the selected native
path scope. Record participant/blob id, reliable byte/line span or unknown, and
observed disk byte identity/freshness where available. Never label disk edits as
stored conflict terms. A concurrent disk change makes that context incomplete;
metadata diagnosis may remain useful but the aggregate cannot conceal the loss.

Proposed hunk limits are at most 32 hunks, 4 KiB per hunk and 64 KiB aggregate;
line counts are bounded by those byte caps and must be reported with the excerpt.
Binary bodies are omitted. Sensitive-policy, unknown-origin, unsupported-native,
unavailable and limit omissions are explicit. These are disclosure controls,
not proof that all secrets are identified. OQ-HUNK must settle whether optional
path/range suppression is required before context is enabled.

Git path bytes survive as base64 identity with an escaped UTF-8 display. Terminal
rendering escapes ANSI, CR/LF injection and bidi controls; native diagnostics are
sanitized bounded data. Newlines in a path are not record delimiters. User-selected
content containing instructions remains DATA and never triggers hooks, helpers,
editor/pager, scripts, prompts, network or another command.

A resolver handoff is ephemeral output, not a saved checkpoint or durable resume
token. A caller that stores it owns its privacy/lifetime; stw adds no evidence cache.
A clean-looking hunk, edited marker or no selected path never proves resolution.
