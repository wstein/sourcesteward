# Specification: agent attribution for task records

**Created**: 2026-10-24 | **Status**: Draft (owner decision 2026-10-24). Needs an independent review of the exact head before the
tasks start. The code in the first pull request is the test-first realisation of D1 to D6 and is reviewed together with this text.

## Problem

Several agent sessions work in one workspace under one OS user. A task record (`stw workspace plan` and `apply`) names an owner label
but not the session that created it, so a session can push or set Ready a branch that another session created, by mistake.

## Scope

Attribution, not a security boundary: the agents share one OS user, and the session id is an environment value set by the harness,
not a secret. The mechanism only has to catch honest mix-ups.

## Decisions

- **D1 Origin block.** `stw workspace apply` writes `origin` into the task record: `kind` (`claude`, `codex`, `agy`, `agent`,
  `unknown`, `human`), `session_id` (optional) and `created_at` (UTC, RFC 3339). Kind comes from the existing marker detection
  (`internal/tidywizard/owner`): `CLAUDECODE` or `CLAUDE_CODE_*` is `claude`, `CODEX_*` is `codex`, `AGY_*` is `agy`, only
  `STW_AGENT` is `agent`, an agent run with no marker is `unknown`, a human run is `human` without a session id.
- **D2 Session source.** Only `CLAUDE_CODE_SESSION_ID` is read, for a `claude` run, and only if it is a plain token of at most 128
  bytes. The Codex source is not measured yet: until it is, a Codex session id stays absent, never guessed. No other variable value is read
  or stored; `CLAUDE_CODE_MESSAGING_TOKEN` and any secret-looking variable are never read.
- **D3 Storage.** Only in the stw state store, in the task record file. Never in git config, commits, branch names, PR text or the
  new checkout. Command output (workspace apply and handoff, text and JSON) shows the origin kind and time only, never the session
  id. The field is additive and optional (`omitempty`); the frozen v1 workspace schema does not forbid it.
- **D4 Written once.** The origin belongs to the creation. A retry or a second apply of the same plan returns the stored origin and
  never overwrites it.
- **D5 Remote-write gate.** Every stw command that pushes or sets a PR Ready for a task calls the gate first. The gate refuses with the
  typed reason `attribution-mismatch` when the recorded session id differs from the current one, and `attribution-unknown` when a
  session id is recorded and the current run has none (an unreadable task record also refuses an agent run). The exit status is 1, as for
  the other typed refusals. A human run is never blocked, a task without a record or without a recorded session is not gated, and the
  existing agent-mode rules are unchanged. The refusal message never carries a session id.
- **D6 Display marker.** A marker in a PR body or commit (agent kind and task number, no session id) is a later convention decision
  for the owner and is not part of this change.

## Status of the gate

No stw command pushes or sets a PR Ready today, so the first change ships the gate function and its tests; the first such command
must call it. Wiring it is a task of that command's specification.

## Residual risk

Same OS user: any process of the user can read the state store, edit a task record, set the variable or run as a human. The
environment value is set by the harness and is not a secret, so it identifies a session for accident prevention only. A session that
the harness restarts with a new id is refused until the owner acts.

## Verification

Tests: same session allowed; different session refused with the reason; agent without a session id on an attributed task refused;
human allowed; origin written once; session id absent from every git object, commit and the new checkout and present only in the task
record; a sentinel in `CLAUDE_CODE_MESSAGING_TOKEN` absent from every state file. The session id is also absent from the JSON of apply, retry and
handoff. No PR body generator exists in stw, so the PR-body check is a placeholder task (T006), not a green test.
