# Specification: `stw tidy` as a janitor, the human tidy wizard

**Created**: 2026-10-26 | **Status**: Draft specification, documentation only (no code, schema or script changes). Needs an independent
review of the exact head and the owner decisions below before tasks start. Owner direction of 2026-10-26 (quoted in
[Why](#why)). It **amends** [Spec 041](../041-tidy-wizard/spec.md) for the human path only and builds on
[Spec 011](../011-workspace-reference-tidy/spec.md), [Spec 029](../029-tidy-dogfood-safety/spec.md),
[Spec 031](../031-tidy-integration-proofs/spec.md) and [Spec 042](../042-agent-attribution/spec.md). Plan: [plan.md](plan.md);
tasks: [tasks.md](tasks.md).

## One-page summary

**What the owner asked for.** Tidying is cleaning, like a cleaner who does not only remove her own dirt. The wizard must be
clearer, more interactive, tell how dangerous each step is, use better criteria, and agents should be able to say "done, it is
merged". Today it "feels like an AI tool": 1,129 candidates on the live workspace, 964 of them retained rows with internal codes,
numeric selection over 165 rows in raw order, and "0 own" because the caller label is the OS user `vscode` while the owner's
own branches carry only a commit-author hint.

**What this specification changes (human path only).**

1. **Janitor mode** (Spec 041 human mode, both streams on terminals, no agent marker): every integrated item can be chosen, whoever
   it belongs to. The owner label stays **information** and feeds the risk level; it no longer decides what a human may choose.
   Agent mode, `stw tidy plan`/`--save`/`apply` for agents, scopes `own`/`other`/`unknown`, `authorized_scopes` and
   `scope-not-authorized` stay exactly as they are.
2. **Risk model.** Every choosable row and every group gets a level, **green, yellow, orange or red**, from fixed rules and with a
   one-line reason ("why orange"). Red (unmerged or dirty work) is never offered for deletion.
3. **Better criteria with the forge.** With `--online`, a merged pull request whose head equals the branch tip becomes the
   strongest evidence (green). Offline everything still works, only with fewer green rows. No GitHub write call is added.
4. **Grouped overview first.** A short summary by kind and risk with human sentences ("43 branches, reversible", "122 worktree
   directories, files are deleted: 55 in `whr-*`, 51 in `workharbor-*`, 7 agent scratchpads ...") and the consequence of each
   group, including the exact undo command where one exists. Internal codes stay in `--json` and `--verbose`.
5. **Wizard steps by group** with single keys (Enter or `y` take the group, `s` skip, `l` list, `r` review each, `q` quit),
   numbers and ranges as the expert input, group selectors (`b`, `w`, `t`, `o`, `m`, `g`) and exclusions (`except 85-86`), then
   **one final summary** before the **unchanged strict typed prompt** read from `/dev/tty`.
6. **Retain explained.** A table per reason with the count and one "what to do" sentence; rows only on request.
7. **Phase 2:** `stw workspace done TASK` (an agent says "finished, merged"), an **adopt** option for legacy worktrees without a
   creation record, and an optional real TUI checklist.

**Phases.** Phase 1 = v0.2.0: items 1, 2, 4, 5, 6, human wording, and item 3 as display. Phase 2 = v0.3.0: item 7.

**What does not change.** No auto-apply; nothing unmerged is ever deleted; no agent-mode loosening; no new GitHub write; no new
dependency in phase 1; the typed `/dev/tty` confirmation stays plain; the apply code path, proofs, plan ids and recovery
manifests are untouched.

**Top decisions for the owner** ([full list](#decisions-for-the-owner)): D1 may Enter take green groups of any owner (recommended:
yes, green only); D2 does removing worktree directories need a second typed confirmation (recommended: no, the existing typed
words stay); D3 do red rows ever become selectable (recommended: never); D4 the rule for owner `unknown` (recommended: choosable,
typed word `unknown` as today); D5 are the group steps' defaults by level acceptable (recommended: yes).

## Why

Owner direction, 2026-10-26 (German, verbatim): "Hier geht es ums Aufräumen, ums Putzen. Das ist wie bei einer Putzfrau. Die putzt
ja auch nicht nur ihren eigenen Dreck weg. Es muss deutlich übersichtlicher werden. Mehr Wizard, mehr Interaktivität, mehr
Einschätzung der Gefährlichkeit, besseres Kriterien, ja, vielleicht auch, dass die Agents einfach irgendwo sagen, okay, sind
fertig, es ist gemerged. Wir haben alle Möglichkeiten, wir haben GitHub-Zugriff, wir haben unser Repo selber, wir können
irgendwelche Sachen schreiben."

The real workspace of that day (`stw tidy plan --json`, 7 repositories, [claims](#measured-data-the-live-workspace-of-2026-10-26)):

| | Count |
| --- | --- |
| Candidates | 1,129 (165 safe, 964 retain) |
| Safe: branch deletions | 43 |
| Safe: worktree directory removals | 122 (19 of them also delete their branch) |
| Safe by owner label | 101 `author:Werner Stein` (hint, scope `unknown`), 57 `codex`, 7 `claude`; 0 `own` |
| Retain by reason | 363 `registration-record-missing` (346 worktrees, 17 stale), 301 `unmerged`, 217 `branch-attached-elsewhere`, 27 `recently-active`, 23 `ignored-extras`, 17 `evidence-incomplete`, 7 `primary-checkout`, 6 `dirty`, 3 `unregistered` |

Three causes make this unusable for a human: the text is machine-shaped (one dense line per candidate, internal codes, 120 columns);
`a` selects `own` rows only, which are none here (`internal/tidywizard/prompt/prompt.go:110-113`, label from
`internal/tidywizard/owner/owner.go:73`); and the 165 choosable rows are numbered in the order of the analysis, so a human cannot say
"all the old baseline checkouts in /tmp".

## Scope and phases

| Phase | Release | Content |
| --- | --- | --- |
| 1 | v0.2.0 | Janitor mode, risk model, grouped overview, group steps, selectors, final summary, retain table, human wording, `--online` display of merged-PR evidence where Spec 031 already provides it, the `--rows` flag |
| 2 | v0.3.0 | `stw workspace done`, tidy use of the done mark, adopt for legacy worktrees, optional TUI checklist, PR-attested proof class decision (D8) |

Phase 1 adds no dependency and no new write effect. Everything the wizard deletes is deleted by the existing apply path of Spec 041.

## Janitor mode

**Definition.** Janitor mode is Spec 041 human mode, nothing more: both standard streams are terminals, `STW_AGENT` is unset, no agent
marker is in the environment, and `--mine` is not given (`internal/tidywizard/owner/mode.go:80`). It remains an **accident guard,
not a boundary** (041 FR-006g): an agent that drives a pty is not stopped by it. The deletion safety stays with the proofs (041
SI2), the stale checks, the recovery manifest and the typed prompt.

**Rule.** In janitor mode a row is choosable iff its verdict is `safe` or `decision` (a proof exists and every precondition holds).
The owner label, `owner_source` and `scope` no longer influence which rows may be chosen, selected by a group, or taken by `a`. They
are shown as information and feed two risk rules (R2 and R3 below).

**What the human path already allowed.** A human may already choose any safe row by number (`owner.Plannable` returns every entry
in human mode, `internal/tidywizard/owner/scope.go:56`), and `--by-owner` already narrows only the view. What blocks the owner
today is the shortcuts: `a` takes `own` rows only, `o` asks per owner label, and the layout groups by owner. Those change.

### Amendments to Spec 041

| 041 id | Text today | In janitor mode (human path) |
| --- | --- | --- |
| FR-006b, US4a ("never preselected"), Scope of ownership is a view (human mode) | `other` and `unknown` are never preselected; `a` selects `own` only | Amended: the group step may take a group whose default is yes (D1; green only by default). `a` becomes "all green rows of every owner". Nothing is applied by a selection; applying needs the final summary and the typed prompt |
| SC-011 | `a` selects own safe items only; foreign items only by number or `o` | Replaced by SC-001 and SC-002 of this spec for the human path; the agent-mode half of SC-011 stays |
| Wizard interaction, Rules ("the default answer is always none/no") | one default for every prompt | Amended for the group steps only: the default depends on the level of the group (green: take, yellow and orange: skip). The question before the plan is stored, the apply question and the typed prompt keep the default no |
| Wizard interaction (prompt text `[a]ll own safe ... [o]wner`), `prompt.Select` | one question over all rows | Replaced by overview, group steps and the expert line (below); the strict parsing rules (3 invalid answers end the run, EOF means nothing chosen, 256-byte lines) are kept |
| Grouping of the text overview, decision 2026-10-25 (#255), rules 1 to 6 | owner, then repository | Replaced for text output by kind and risk. The JSON order is unchanged. `--rows` prints the old per-owner row view |
| OQ13 (owner of an unregistered worktree) | `commit-author` is a hint, never `own` | Kept. Added: a display label `you` when the tip author equals the caller's global Git identity (FR-009). It is information, scope stays `unknown` |

### What stays exactly as it is

- **Agent mode** (non-terminal, `STW_AGENT`, a marker, or `--mine`): no prompt, no group steps. `plan` and `plan --save` list and
  plan `own` items from a task record only (041 FR-006c, `owner.Plannable`, `planset.SelectSafe` at `internal/tidywizard/planset/planset.go:300`).
  `apply` refuses `other` and `unknown` items without `authorized_scopes` (`scope-not-authorized`, 041 FR-006d), and
  `authorized_scopes` is written by the human path only (`owner.Authorize`, `scope.go:67`) and bound into the plan id.
- **Typed confirmation** (041 FR-006f): the confirmation word is the owner label, or the literal word `unknown`, followed by the
  first six characters of the plan id, read from `/dev/tty`, asked once per word (`internal/tidywizard/owner/confirm.go:45`,
  `:59`); it runs when the plan set is built (`planset.go:492`) and again at apply (`apply.go:300`). Unchanged.
- Scopes, `owner_source`, the owner derivation order, the proofs (ancestor, patch-id, merge-noop, no-net-change), the recently-active
  retain rule (24 h, `classify.go:33`), the plan set, the dependent branch leg, journals and recovery roots.
- Exit codes 0, 1 and 2, the no-terminal degradation (US7, FR-009), `esc` and the 120-character bound (FR-042).

## Risk model

Each choosable row gets one level, each group the worst level of its rows (and the counts per level). Retain rows never get a
level of "safe-ness"; they are *kept* (see [Retain explained](#retain-explained)), except those that guard real work, which are
**red**.

### Levels

| Level | Meaning | Offered for deletion |
| --- | --- | --- |
| green | The work is provably on the base, or nothing unique exists, and the effect is restorable or removes only a clean copy | yes |
| yellow | Integrated by equivalence (same patches, not the same commits) or restorable only with effort | yes |
| orange | Integrated by equivalence **and** one more fact makes a mistake likelier (recent activity, nobody can vouch for the item, another owner's unfinished task) | yes, never a default |
| red | Unique work is at stake: `unmerged`, `dirty`, `ignored-extras` | never |

Rows kept for another reason (`primary-checkout`, `current-worktree`, `registration-record-missing`, `branch-attached-elsewhere`,
`recently-active`, `evidence-incomplete`, `locked-foreign`, `protected`, `base-*`, `jj-retirement-unsupported`) have no level; the
row says "kept" and why. `evidence-incomplete` means "not judged", not "dangerous".

### The ladder (exact, all inputs are existing evidence unless marked)

1. **Strength of the proof, S.**
   - **strong** if the proof word is `ancestor` or `no-net-change`; or the evidence is `no-work` (no commit of its own; 041 FR-006h);
     or, with `--online`, the tip OID equals the head of a pull request merged into the base (C2); or the proof is `patch-id` or
     `merge-noop` and a corroboration holds: the branch's upstream is gone (C1, a branch whose remote-tracking branch was
     deleted; read from the local ref database without network) or, in phase 2, the task record carries a done mark with a
     merged pull request number (C3).
   - **equivalent** otherwise (`patch-id` or `merge-noop`, whitespace-only differences are never `safe`; the wizard has no
     `--accept-whitespace-differences`, so those rows stay `retain` `unmerged`).
2. **Base level.** strong gives **green**; equivalent gives **yellow**. (Effect type is shown, not scored: a `delete-branch`
   effect is restorable with `stw tidy restore` from its recovery ref (`docs/tidy.md:566`), the removal of a worktree directory is
   not restorable (`docs/tidy.md:584`, `:348-351`) but the verdict `safe` already guarantees a clean tree with no ignored extras, so
   the committed state is pinned and nothing but a copy of it is deleted.)
3. **Raises**, each adds one step (green, yellow, orange), at most orange, each counted once:
   - **R1 recent activity**: a worktree of scope `other` or `unknown` whose newest HEAD, index or tracked-file time is older than
     the 24-hour retain window but within 7 days (`LastActivity`, already observed). Window: proposed.
   - **R2 nobody vouches**: a worktree, S equivalent, with no creation record and an owner of scope `unknown` or an
     `author:` hint (no task record, no path convention). Nobody can say whose it is or whether it still matters.
   - **R3 (phase 2) another owner's unfinished task**: a creation record exists, its owner is not the caller, and it has no done mark.
   - **R4 decision rows**: a `decision` row (`lock-mismatch`, legacy completion) is at least orange.
4. **Red** is the verdict `retain` with reason `unmerged`, `dirty` or `ignored-extras`. It is shown with its reason and a consequence
   ("2 commits on this branch exist nowhere else") and has no number a human could type.

### Reasons ("why orange")

Every level carries `risk_reasons[]`: stable kebab-case words in JSON (`proof-ancestor`, `proof-no-net-change`, `no-own-commit`,
`pr-merged-head`, `upstream-gone`, `done-mark`, `proof-equivalent`, `recent-activity`, `nobody-vouches`, `task-not-done`,
`decision-row`) and one human sentence per word in text. Example, orange: "Why orange: the work is on main as equal patches (a
rebase merge), not as the same commits; the directory is deleted for good; no record says whose it is."

### Effect and consequence facts (shown with every group, never scored)

Kind and effect (`delete-branch`: restorable; `remove-registration` plus `remove-files`: not restorable), the number of items and
repositories, the recovery location (the state directory of the run, recovery refs `refs/stw/recovery/v3/...`), the exact undo command
where one exists, and, when known, an upper bound of size: the tracked-file count that the activity scan reads (at most 8,192 per
worktree, `retirement-file-limit` above that; 041 Read-only observers). No new filesystem walk is added; no size is shown when the
count is not known.

### Measured example

Applied to the live workspace data (R1 cannot be evaluated: `overview.json` carries no activity times, so it is shown as 0; C1 and C2
are not evaluable offline): **green 6, yellow 93, orange 66** of 165 safe rows, and **red 330** (301 `unmerged`, 23 `ignored-extras`,
6 `dirty`) of the 964 retained rows.

## Better criteria with the forge (`--online`)

**Today.** `--online` allows the contract-3 authenticated reads (default off); in the wizard it fills tracking references only
(041 Read-only observers, `TrackingSkipped: tracking-requires-online`). Spec 031 already has merged-pull-request discovery keyed by
distinct head OID (031 FR-013 to FR-018: closed endpoint set, up to 10 pages of 100, 128 requests and one 30-second deadline per run,
github.com only, no write) and the proof class `github-merged-head` for local reference plans.

**Phase 1 (display and risk only).** With `--online` the wizard reuses that discovery, read-only, for every distinct tip OID of a
row that is `safe` or kept as `unmerged`:

- a `safe` row whose tip equals a merged pull request head is **strong** (C2): green, with the evidence "merged as #N";
- an `unmerged` row whose tip equals a merged pull request head is **not** made selectable: it stays red with the added text
  "merged as #N, but the local proofs disagree: review it" (D8 asks whether this may ever become a proof);
- discovery failure, an exceeded bound or an incomplete collection never raises or lowers a level; the row is judged offline and the
  overview says "N rows could not be checked against GitHub" (031 FR-017: absence is inconclusive, presence is positive evidence).

No new endpoint, no write, no other host, no new flag (`--online` and `--timeout` exist). Offline is the default and stays complete
without it.

**Phase 2.** D8: whether `github-merged-head` becomes a deletion proof inside the wizard (it changes the frozen
`tidy-overview` `proof` enum). Recommended: not before phase 2 and only after a measured dogfood run.

## Overview first

The grouped overview replaces the row wall as the first screen of `stw tidy` and of the text of `stw tidy plan`. It is built from the
same candidates the JSON carries; `--json` is unchanged except the additive fields in [Interfaces](#interfaces).

**Groups.** One group per (kind and effect, place):

- **Branch deletions** (`delete-branch` only): one group, with counts per repository.
- **Worktree directories**, by place: *agent scratchpads* (path contains `/scratchpad/`), *`/tmp`* by **family** (the leading lowercase
  letters of the directory name, for example `whr` for `whr-006-t030-apply` and `whr007-ci-binding`, `workharbor` for
  `workharbor-baseline-0jbykfta`; a family needs at least 3 members, otherwise the item goes to "other in /tmp"), and
  *in the workspace* (anything else, by parent directory). A hostile name has no letters, so it falls into "other" and is shown
  escaped.
- **Kept** rows are not groups; they are the [retain table](#retain-explained).

Groups are ordered by risk (green first), then by size. Each line says what happens, how many, how risky, and what undoes it.

**Wording.** Text uses sentences, not codes. Codes (`registration-record-missing`, `patch-id`, `branch-attached-elsewhere`, ...) appear
only in `--json`, `--verbose`, `--rows` and `d N` details. A table in `internal/tidywizard/render` maps every reason and proof word to a
sentence and a test fails when a code has none (SC-010).

**Owner as information.** The owner appears as a column of the group step and as a count line ("owner: 51 yourself, 55 codex"); the label
`you` is shown for tip authors equal to the caller's global Git identity, otherwise the label as derived (`codex`, `claude`,
`author:NAME`, `unknown`).

## Wizard steps

Human mode with a terminal only. After the overview the wizard walks the groups in the order shown. Nothing is changed by any of
these answers; they build a selection.

**One screen per group:** header (what, how many, level counts), the consequence lines, the "why" of the worst level, three example
rows, then the question. Keys (a single line answer followed by Enter, no raw mode in phase 1):

| Key | Meaning |
| --- | --- |
| Enter | the default of this group: **take** for green, **skip** for yellow and orange (D1, D5) |
| `y` | take the whole group (also yellow and orange) |
| `s` | skip the group |
| `l` | list every row of the group (paged, 20 rows at a time) |
| `r` | review each row of the group: one line each, `y`, `n` or `q` |
| `q` | quit; nothing is stored |

**Expert input** (kept): at any group question, and at the final summary via `e`, the line may be numbers, ranges and selectors
(grammar below). Numbers are the display numbers of the listing; they are stable within the run and printed by `l`.

### Selection grammar

```
line      = term { (" " | ",") term } [ " except " term { (" " | ",") term } ]
term      = NUMBER | NUMBER "-" NUMBER | selector
selector  = "b"            all branch deletions
          | "w"            all worktree directories
          | "t"            all worktree directories under /tmp
          | "g"            all green rows
          | "o:" LABEL     all rows of that owner label (exact, as shown)
          | "m:" REPO      all rows of that repository
```

The result is the union of the terms before `except`, minus the union after it, intersected with the choosable rows (red and kept
rows have no number and match no selector). A letter, number or word that does not parse, a number that is not choosable, an empty
`except`, or a line over 256 bytes is an invalid answer (three end the run, as today). Example: `w t except 85-86`. Selectors never
select a `decision` row; those keep the per-row second confirmation of Spec 041.

### Final summary and the typed prompt

After the last group, one summary lists the selection by group with counts, level counts, effects, the typed words that will be
asked, and the undo lines (screen 3 below). Then, in this order, all as today: the question "Store this plan set?" (default no), the
typed confirmation per word on `/dev/tty` while the plan set is built, the stored plan id, the question "Apply it now?" (default no),
and the typed confirmation again inside the unchanged apply path. A wrong word, no terminal, an interrupt or `q` stores or applies
nothing more. The typed prompt stays **plain text** (no color, no menu); it is the protection, and the environment markers are only
the accident guard in front of it.

## Retain explained

After the groups the wizard prints one table of kept rows: count, a human reason and one "what to do" sentence. Rows are shown only on
request (`l` at the table, or `--rows`).

| Reason (code stays in JSON) | Count | What it means | What to do |
| --- | --- | --- | --- |
| Created by stw, record not found (`registration-record-missing`) | 346 worktrees, 17 stale | The worktree is locked `stw:TASK` but no task record is in a known state directory | Re-run with the `--state-dir` that created them (agent scratch stores are separate directories). Records that no longer exist: phase 2 adopt |
| Not merged (`unmerged`) | 301 | The branch or worktree has commits the base lacks | Nothing is deleted. To judge them, make the review report per repository with the expert command `stw tidy plan REPO --contract 3 ... --report-dir DIR` (Spec 031) |
| Branch is checked out elsewhere (`branch-attached-elsewhere`) | 217 | A kept worktree has this branch checked out | Clean up that worktree first, then run again |
| Recently active (`recently-active`) | 27 | A job may still be running there (changed within 24 hours) | Wait, then run again |
| Untracked or ignored files (`ignored-extras`) | 23 | The directory holds files Git does not track | Look inside and remove them yourself if they are junk; stw never deletes them |
| Not judged (`evidence-incomplete`) | 17 | A deadline or proof budget ran out | Run again with a larger window, for example `--timeout 15m` |
| Repository checkout (`primary-checkout`) | 7 | The main checkout of a repository | Never offered |
| Uncommitted changes (`dirty`) | 6 | Tracked files differ from the commit | Commit or discard them yourself |
| Registration without directory (`unregistered`, stale) | 3 | A worktree entry whose directory is gone | No stw action yet (Spec 041 T012b) |

The counts are those of the live workspace; the sentences are fixed text per code (SC-010). The table is computed from the same
candidates, so the numbers always add up to the retained total.

## Phase 2

### Agents say "done" (`stw workspace done`)

`stw workspace done TASK [--merged PR] [--state-dir DIR]` records in the **existing task record** (the file `stw workspace apply`
wrote) that the owner of the task considers it finished and, optionally, which pull request merged it.

- **Not the state field.** The record's `state` is `complete` when *creation* completed (`internal/workspace/workspace.go:462`), so the
  mark is a new additive optional block, like `origin` (Spec 042 D1, D3): `done` with `at` (UTC), `by` (the origin kind) and
  `merged_pr` (number, optional). The frozen v1 workspace schema must allow it; T-tasks verify that as Spec 042 did.
- **State directory only.** No network, no remote write, no change to any repository or worktree. The record is rewritten atomically and
  only the `done` block is added; an existing `done` is returned, not overwritten (idempotent, as `origin` is written once).
- **Attribution gate.** The command calls the Spec 042 D5 gate: another session's task is refused (`attribution-mismatch`); a human run
  is never blocked.
- **It is a claim, not a proof.** The mark never makes anything deletable. It is corroboration C3 (lowers equivalent to strong, only for
  a row that already has a proof), removes R3, and adds the line "agent says done" to the reason. Without a proof the row stays red.
- **Agent step.** Add to the workspace `AGENTS.md` workflow (owner's file, owner's approval; this spec proposes the sentence): "After the owner
  reports the merge, run `stw workspace done TASK --merged PR`." Agents that never run it lose nothing: `--online` (C2) finds the
  merged pull request from the branch tip anyway.
- **Automatic marking.** Not recommended (D9): `stw watch` and `stw pulls` are read-only by contract (Spec 035 and 036), and C2 already
  recognizes a merged head at tidy time without writing a record.

### Adopt legacy worktrees

For worktrees without a creation record, `stw tidy adopt PATTERN=OWNER ...` (example `/tmp/whr*=codex`) supplies an owner label the
built-in path table does not know. Safeguards: human mode and a terminal only; the pattern is an absolute path glob under `/tmp` or the
workspace, never `*` alone, matched only against existing linked worktrees of known members (never the primary or current checkout);
the matches are listed and need `y`; the result is an **adoption record** in the state directory (new `adoptions` namespace, path,
repository identity, the HEAD at adoption, label, time), not a task record. It changes only `owner` and `owner_source` (`adopted`, which
never counts as `task-record` for agent mode) and **never a verdict**: a worktree locked `stw:TASK` without record keeps needing the
`--state-dir` that created it, because owned retirement binds the creation record.

### Real TUI checklist

An optional checklist (arrow keys, space) needs a terminal library and therefore a pinned dependency; Spec 041 OQ10 chose plain lines.
Decision D10. The line wizard stays the fallback in every case (no terminal, narrow terminal, `NO_COLOR`).

## Screens

### Screen 0: today (real output)

`stw tidy plan --human` of the installed `stw` (main `6d4c75ed`, read-only) on a throwaway workspace with two members. The derived,
mode and notice lines are omitted and paths shortened to `/fx`; exit status is 1 because the workspace repository itself has no
derivable base. Note "0 own", the owner-first grouping and the codes.

```text
Workspace: /fx/ws
Analysed 3 repositories (1 skipped, 0 with errors)

Repositories:
.   skipped: base-ambiguous
ra   base refs/heads/main
rb   base refs/heads/main

owner claude (other)
  ra
    [1] safe     branch       claude/old  owner claude (path-convention) [other]  integrated (ancestor)  effects delete-branch:claude/old  not preselected

owner codex (other)
  ra
    [-] retain   branch       codex/done-1  owner codex (path-convention) [other]  branch-attached-elsewhere: checked out in a worktree that is retained or kept
    [-] retain   worktree     /fx/wt-codex-1  branch codex/done-1  owner codex (path-convention) [other]  recently-active: HEAD, index or a tracked file changed within the last 24 hours

owner unknown
  ra
    [-] retain   branch       feat/unmerged  owner author:t (commit-author) [unknown]  unmerged: 1 commit ahead, not in the base (feat/unmerged)
    [-] retain   worktree     /fx/ws/ra  branch main  owner unknown (unknown) [unknown]  primary-checkout
    [-] retain   worktree     /fx/wt-detached  owner author:t (commit-author) [unknown]  recently-active: HEAD, index or a tracked file changed within the last 24 hours
  rb
    [2] safe     branch       tmp/ci-wait  owner author:t (commit-author) [unknown]  integrated (ancestor)  effects delete-branch:tmp/ci-wait  not preselected
    [3] safe     branch       work/x  owner author:t (commit-author) [unknown]  integrated (ancestor)  effects delete-branch:work/x  not preselected
    [-] retain   worktree     /fx/ws/rb  branch main  owner unknown (unknown) [unknown]  primary-checkout

summary: 3 safe (0 own, 3 other), 6 retain
```

The next three screens and the retain table are **MOCKUPS**: the program cannot produce them yet. Their numbers are computed from the
real `overview.json` of 2026-10-26 (counts, owners, paths and proofs are real; levels follow the ladder above with R1, C1 and C2 not
evaluable; the numbering and the undo ids are invented).

### Screen 1 (mockup): grouped overview

```text
Workspace /workspaces/Anchorage   7 repositories analysed   mode: you (terminal, no agent)

Can be cleaned (165 of 1,129 candidates)                        risk
  1  55 directories  /tmp/whr-*          files deleted, codex   yellow  (3 green, 52 yellow)
  2  43 branches                         reversible             yellow  (2 green, 41 yellow)
  3  51 directories  /tmp/workharbor-*   files deleted, you     orange  (1 green, 50 orange)
  4   7 directories  agent scratchpads   files deleted, you     orange
  5   4 directories  /tmp/crewbook-*      files deleted, you     orange
  6   4 directories  other in /tmp        files deleted, you     orange
  7   1 directory    in the workspace     files deleted, you     orange
     19 of the directories also delete their branch (reversible)

Kept (964): nothing is deleted, nothing unmerged is ever offered
  330 unmerged or changed work (red)   634 other reasons, see the table at the end

Next: one question per group. Nothing changes until the final summary and your typed confirmation.
Enter = continue   q = quit
```

### Screen 2 (mockup): one group step

```text
Step 1 of 7   55 directories in /tmp:  whr-*                                  risk yellow
  Owner     codex (55), found by path convention
  Evidence  3 already on main, 52 on main as equal patches (a rebase merge)
  Effect    55 directories are deleted for good, 15 of them also delete their branch
  Recover   committed history stays pinned under refs/stw/recovery/v3/...;
            deleted files are not restored; branches: stw tidy restore --contract 3
  Why yellow  the work is on main as equal patches, not as the same commits

      1  yellow  /tmp/whr-006-t030-apply-20261020      codex/006-t030-apply-20261020
      2  yellow  /tmp/whr-006-t030-cli-20261020        codex/006-t030-cli-20261020
      3  yellow  /tmp/whr-006-t030-proposal-20261020   codex/006-t030-proposal-20261020
     ... 52 more (l = list)

Take this group?  [y] take  [s] skip  [l] list  [r] review each  [q] quit   (Enter = skip) :
```

### Screen 3 (mockup): final summary and typed prompt

```text
Summary: nothing has been changed yet
  group                         items   risk     effect
  55 directories /tmp/whr-*        55   yellow   delete 55 directories, 15 branches
  43 branches                      43   yellow   delete 43 branches (reversible)
  51 directories /tmp/workharbor-* 51   orange   delete 51 directories
  skipped: 4 groups (16 directories)
  total: 149 items in 7 repositories; 964 kept rows untouched

Undo: branches   stw tidy restore --contract 3 --state-dir DIR --plan <printed after apply>
      directories  not restorable; committed history stays pinned
Typed words that will be asked (on /dev/tty): codex   unknown
Store this plan set? [y/N]: y
Foreign items (codex). Type "codex 3f9c2a" to confirm:
```

The last line is the real prompt text (`internal/tidywizard/owner/confirm.go:75`).

### Screen 4 (mockup): retain table

```text
Kept: 964 candidates. Nothing here is offered for deletion.
  count  why                                        what to do
    363  created by stw, record not found           re-run with the --state-dir that created them
    301  not merged (red)                           nothing is deleted; review with the report, see docs/tidy.md
    217  branch is checked out in a kept worktree   clean up that worktree first, then run again
     27  recently active (within 24 hours)          wait, then run again
     23  untracked or ignored files (red)           look inside; remove them yourself if junk
     17  not judged (deadline)                      run again with --timeout 15m
      7  main checkout of a repository              never offered
      6  uncommitted changes (red)                  commit or discard them yourself
      3  worktree entry whose directory is gone     no stw action yet
Rows: l = list a reason, or run  stw tidy --rows
```

## Interfaces

- **Flags (wizard layer).** New: `--rows` (print the row view grouped by owner, as before the change, with all rows). Existing and
  unchanged: `--verbose`, `--quiet`, `--progress`, `--trace` (progress and timings go to stderr, nothing to stdout; #297,
  merged; `internal/cli/tidy_mode.go:31`, `internal/cli/tidy_wizard.go:109`). The flag classification test must list `--rows`.
- **JSON.** Additive optional fields on each candidate of `tidy-overview`: `risk` (`green|yellow|orange|red|none`),
  `risk_reasons[]`, `group` (a stable group key), and `pr` (merged pull request number, `--online` only). The schema is frozen but
  unpublished (`UNPUBLISHED_SCHEMAS`), so it is amended with its freeze digest in the same change; `summary` gains counts per level.
- **No new write effect.** The wizard stores a plan set exactly as today; `--save` for agents is unchanged.
- **Exit codes** unchanged.

## Requirements

### Functional (phase 1 unless marked)

- **FR-001** In janitor mode every row with verdict `safe` or `decision` is choosable by number, group or selector regardless of
  `owner`, `owner_source` and `scope`; agent mode is unchanged (041 FR-006c, FR-006d).
- **FR-002** `a` takes all green rows of every owner; it never takes a yellow, orange or `decision` row.
- **FR-003** Each choosable row has a `risk` computed by the ladder, with `risk_reasons[]`; each group has the worst level and counts
  per level. Rows with `unmerged`, `dirty` or `ignored-extras` are red and have no number.
- **FR-004** The ladder reads only evidence of the analysis (proof word, evidence word, activity time, creation record, owner source,
  upstream state read from the local reference database) and, with `--online`, the Spec 031 pull request discovery. No other input.
- **FR-005** `--online` marks a tip equal to a merged pull request head as strong (C2). Discovery failure never changes a level and is
  counted in a notice. No new endpoint, no write, no new flag.
- **FR-006** The text overview is the grouped overview; every code is replaced by a sentence; `--json`, `--verbose`, `--rows` and `d N`
  keep the codes.
- **FR-007** Each group shows the count, level counts, effect, recovery location, the exact undo command where one exists, and an upper
  bound of size only when already known.
- **FR-008** The wizard asks one question per group with the keys and defaults of [Wizard steps](#wizard-steps); the strict parsing
  of Spec 041 (three invalid answers, EOF means nothing chosen, 256 bytes) is kept.
- **FR-009** The label `you` is shown for a tip author equal to the global Git identity (`user.name` or `user.email`) of the caller,
  read once with two bounded `git config --global --get` calls (the only read of global Git configuration; the observers keep ignoring it); unreadable means no label. It never changes `scope`.
- **FR-010** Selection grammar, `except`, and selectors as specified; red and kept rows match nothing.
- **FR-011** The final summary precedes the unchanged typed prompt, which is read from `/dev/tty`, plain, once per word, and refused
  without a terminal or with a wrong word.
- **FR-012** The retain table lists every reason with count, sentence and one action sentence; counts sum to the retained total.
- **FR-013** Output obeys `esc`, the 120-character bound and the row bounds (FR-042, FR-041); color is decoration only (the level word is
  always printed; `NO_COLOR` is honored as today).
- **FR-014** No terminal: the wizard prints the grouped overview, asks nothing, stores nothing, exit 0 (041 US7, FR-009 there).
- **FR-015 (phase 2)** `stw workspace done TASK [--merged PR]` as specified; the done block is additive and idempotent.
- **FR-016 (phase 2)** Tidy treats a done mark as C3 only together with a proof; `adopt` as specified.

### Success criteria

Each is testable with temporary repositories and a fake hosting transport (no real repository, no network).

- **SC-001** Janitor mode: a fixture with items of `codex`, `claude`, `unknown` and own owners, all proven integrated, shows every row
  choosable and `a` takes exactly the green rows of all owners.
- **SC-002** Agent mode on the same fixture: no prompt, only `own` items from a task record listed and planned; `apply` of a plan
  with a foreign item and no `authorized_scopes` exits 1 `scope-not-authorized`; byte-identical to Spec 041 SC-012 results.
- **SC-003** Ladder table test: one row per case (strong and equivalent proofs, each corroboration, each raise, caps at orange, red
  reasons) with the expected level and `risk_reasons`; a mutation of any threshold or of "at most orange" fails a test.
- **SC-004** Red rows (`unmerged`, `dirty`, `ignored-extras`) never get a number, never match a selector, and forcing one into a plan
  makes apply refuse (as 041 SC-002).
- **SC-005** `--online` with the fake transport: a tip equal to a merged head is green with `pr`; an `unmerged` row with a merged head
  stays red; failure, 403 and an incomplete collection leave levels unchanged and add a notice; the request count stays within 031 FR-018;
  no request is a write (the double records methods).
- **SC-006** Grammar table: numbers, ranges, every selector, `except`, a selector that matches nothing, a red number, junk, 257 bytes;
  `w t except 85-86` selects the expected set.
- **SC-007** Transcript test of the three screens and the retain table from the fixture: the group order, wording, undo lines and
  counts equal the golden; the counts of the retain table sum to the retained total.
- **SC-008** The typed prompt: a bare `y`, the wrong word, stdin input and no controlling terminal refuse and store or apply nothing; the
  prompt text is unchanged byte for byte.
- **SC-009** Hostile names (ESC, CR, NUL, bidi) in branch, path and owner reach no terminal byte unescaped in any screen, group key
  or example row; `--json` carries them as data.
- **SC-010** Every reason and proof word has a sentence and an action; a new code without one fails the test.
- **SC-011** `stw tidy --json`, the expert layer and every Spec 041 test and golden other than the text overview pass unchanged; the
  tidy flags are all classified (`--rows` included).
- **SC-012** With 960 retained rows and 165 choosable rows the first screen has at most 24 lines and no row of a retained code.
- **SC-013 (phase 2)** `workspace done`: idempotent, state directory only (a before and after snapshot of every repository and
  worktree is equal), refused for another session's task, additive in the schema; a done mark without proof never makes a row choosable.

## Edge cases

| Case | Behavior |
| --- | --- |
| Name with ESC, CR, NUL or bidi controls | escaped by `esc`, bounded at 120; the family function sees only letters, so it falls to "other" |
| No terminal on stdin or stdout, or agent marker | grouped overview only, no question, nothing stored, exit 0 |
| Narrow terminal (below 60 columns) | rows drop the branch column, then the example rows; the level word and counts never wrap away; columns come from the terminal size, fixed 80 if unknown |
| 960 retained rows | one table, no rows until asked; the first screen stays within 24 lines |
| Interrupted run (Ctrl-C, `--timeout`) | exit 1 naming the cause; before the plan set is stored nothing exists, after it the id is named and nothing was applied (as today, `tidy_interactive.go:98`) |
| All rows red or kept | summary says "nothing can be cleaned"; the retain table follows; no questions |
| Nothing to clean | one line "Nothing to clean."; exit 0 |
| A group of 1 item | the step is still shown; one line header |
| Mixed levels in a group | `y` takes all; `r` reviews each; Enter follows the worst level (default skip if any yellow or orange) |
| Selection includes a `decision` row | selectors skip it; only its number asks the per-row second confirmation (041) |
| `--by-owner`, `--only`, `--member` | narrow the view first; groups and counts are computed on the narrowed set |
| Discovery fails online | levels unchanged, notice counts the rows not checked |
| Stale state between the selection and apply | the existing stale checks refuse with `evidence-changed`; nothing changed |

## Composition with other work

- **#297 (merged).** Progress on stderr (`internal/cli/tidy_progress.go`), `--verbose` timings, `--quiet`, `--progress` and `--trace`
  are unchanged; the grouped overview and the steps write to stdout, progress never does. The analysis time is spent before the first screen.
- **The strict prompt.** The janitor flow only builds the selection and the summary; it calls the same `planset.Build`, typed prompt and
  `runTidyApplyPlanSet` (`internal/cli/tidy_interactive.go:60`, `:93`).
- **Spec 042.** The done command uses the attribution gate (D5). No session id is printed or stored in the new records.
- **Spec 043.** `stw land` is unrelated except that a landed pull request becomes C2 evidence for tidy.

## Non-goals

- No automatic apply, no `--yes` for janitor plans, no scheduled cleaning.
- No deletion of unmerged or changed work, ever; red rows have no number.
- No loosening of agent mode, scopes, `authorized_scopes` or `scope-not-authorized`.
- No new write call to GitHub (reads only, github.com only, the Spec 031 bounds); the `done` command writes only the state directory.
- No new dependency in phase 1; no raw-mode keyboard handling in phase 1.
- No new integration proof; the proofs of Specs 011 and 031 are unchanged.
- No guess about ownership beyond the existing derivation; `you` is a display label.

## Decisions for the owner

| # | Question | Recommendation |
| --- | --- | --- |
| D1 | May a group step take green rows of other and unknown owners with Enter? | Yes, green only. Enter skips yellow and orange; `y` takes them. Nothing is applied by Enter: the final summary and the typed prompt follow |
| D2 | Does removing worktree directories need a second typed confirmation beyond the existing words? | No. The rows are clean and pinned; the existing typed word per owner stays. Revisit after the first dogfood run |
| D3 | May red rows ever become selectable? | Never. A human who wants them gone uses Git or the review report and decides there |
| D4 | The rule for owner `unknown` | Choosable like every other owner; the typed word is `unknown` as today; no extra flag |
| D5 | Are group defaults by level (green take, others skip) acceptable against 041 "default is always none"? | Yes; the questions that store or apply keep the default no |
| D6 | The "recent activity" window for R1 | 7 days (proposed); only raises a level, never blocks |
| D7 | Is `you` from the global Git identity acceptable as a display label (FR-009)? | Yes; display only |
| D8 | May a merged pull request head become a deletion proof inside the wizard (changes the frozen `proof` enum)? | Not in phase 1; decide after dogfood in phase 2 |
| D9 | Should `stw watch` or `stw pulls` mark tasks done automatically? | No; both stay read-only, `--online` already finds merged heads |
| D10 | A real TUI checklist (needs a pinned dependency)? | Decide in phase 2, after the line wizard has been used; the line wizard stays |
| D11 | The workflow sentence for agents ("after the owner reports the merge, run `stw workspace done`") in the workspace `AGENTS.md` | Add it with phase 2; it is the owner's file |
| D12 | Is one typed word asked twice (at store and at apply, as today) acceptable? | Yes for phase 1; asking once is a later change to the apply path |

## Measured data: the live workspace of 2026-10-26

All numbers come from `overview.json` (`stw tidy plan --json`, agent mode, offline, main `6d4c75ed`), queried with `jq`. Groups use the
family rule above. Not in the file and therefore not claimed: activity times, upstream state, sizes. The 122 safe worktree directories are
121 under `/tmp` (55 `whr-*`, 51 `workharbor-*`, 7 agent scratchpads, 4 `crewbook-*`, 4 other) and 1 in the workspace. The owner's
earlier estimate (63 under `/tmp`, 57 baseline) differs from the file; the file is used.

## Docs impact

`docs/tidy.md` (wizard section: janitor mode, risk ladder, steps, grammar, retain table; amendments listed against Spec 041), `docs/commands.md`
(`--rows`, later `workspace done`, `tidy adopt`), `docs/json-contracts.md` and `schemas/v2/tidy-overview.schema.json` with fixtures and
freeze digest (additive fields), `docs/agent-guide.md` (agents are unchanged; phase 2 done step), `docs/workspaces.md` (phase 2 `done`),
`docs/progress.md`, `docs/roadmap.md`, `docs/feature-status.json` (status `planned`). Spec 041 gets a pointer to this spec; its text is
not rewritten.

## Risks

- A one-key "take" tempts: mitigated by green-only default, the summary, the typed words and the unchanged apply refusals.
- Levels can be mistaken for proof: the text says "risk" and always lists the proof word in details; a level never makes a row choosable.
- The family rule may group badly: the group is a view; `l` and `r` always show the rows and numbers still work.
- Grouped text changes the golden of Spec 041's text overview: it is unreleased; the JSON is additive only.
