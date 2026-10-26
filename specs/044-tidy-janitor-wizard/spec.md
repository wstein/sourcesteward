# Specification: `stw tidy` as a janitor, the human tidy wizard

**Created**: 2026-10-26 | **Status**: Draft specification, documentation only (no code, schema or script changes). Needs an independent
review of the exact head before tasks start; all owner decisions D1 to D16 are recorded below, none is open. The single typed word of D12 needs an independent security review before any code. Owner direction of 2026-10-26 (quoted in
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
   `scope-not-authorized` stay exactly as they are. The Spec 041 texts this contradicts are listed one by one
   ([Amendments](#amendments-to-spec-041-and-the-online-contract)).
2. **Risk model.** Every choosable row and every group gets a level, **green, yellow, orange or red**, from fixed rules and with a
   one-line reason ("why orange"). Red (possible unique work) is never offered for deletion.
3. **Forge evidence, online by default for the human wizard** (decided, D13). The human wizard reads GitHub with the existing
   contract-3 reads (github.com only, bounded budget, no writes) when it is authenticated and falls back to offline with a one-line
   notice; `--offline` opts out. A merged pull request whose head equals the branch tip is the strongest evidence (green). Agent mode
   and `stw tidy plan` keep offline as the default.
4. **Grouped overview first.** A short summary by kind and risk with human sentences and the consequence of each group.
   Internal codes stay in `--json`, `--verbose` and `--rows`.
5. **Wizard steps by group** with single keys (Enter or `y` take the group, `s` skip, `l` list, `r` review each, `q` quit),
   numbers, ranges and selectors as expert input, exclusions (`except 85-86`), then **one final summary that states how many
   directories will be deleted for good**, directly before the **unchanged strict typed prompt** read from `/dev/tty`; the store and apply answers are read from `/dev/tty` too (D16).
6. **Retain explained.** A table per reason with the count and one "what to do" sentence; rows only on request.
7. **Phase 2:** `stw workspace done TASK` (an agent says "finished, merged"), an **adopt** option for legacy unlocked worktrees, and an
   optional TUI checklist.

**Phases (accepted by the owner, 2026-10-26).** Phase 1 = v0.2.0: items 1 to 6 and human wording. Phase 2 = v0.3.0: item 7.

**What does not change.** No auto-apply; nothing unmerged is ever deleted; no agent-mode loosening; no new GitHub write; no new
dependency in phase 1; the typed `/dev/tty` confirmation stays plain; the proofs, plan ids, stale checks, journals, effects and recovery
manifests are untouched. The one change to the shared apply entry is the single-confirmation receipt of D12, which only the wizard
passes; `stw tidy apply` and agent mode never do.

## Decided by the owner (2026-10-26)

Owner answers, verbatim (D16: "Ja, D16 ist in Ordnung, macht es so, ja"): "D1j D2Nein, die strenge Abfrage genügt. D3nie D4j D5j"; on the online default: "online std? offline optin?". D16 followed the same day. The remaining decisions D6 to D12, D14 and D15 were answered the same day (about 15:40 UTC): D6 72 hours (the owner's own value), D7 to D11, D14 and D15 as recommended, D12 "only ask once", clarified as one typed word per wizard run.

| # | Decision | Consequence in this spec |
| --- | --- | --- |
| D1 yes | Enter takes green groups of any owner (green only) | Group steps; [FR-002](#functional-phase-1-unless-marked) |
| D2 no | No second typed confirmation for removing worktree directories; the strict typed prompt suffices | Condition: the summary line directly before the typed prompt states how many directories are deleted for good and how many branches; see [Final summary](#final-summary-and-the-typed-prompt) |
| D3 never | Red rows are never selectable | Red rows have no number and match no selector |
| D4 yes | Owner `unknown` is choosable with the typed word `unknown` | As in Spec 041 `ConfirmWord` |
| D5 yes | Group defaults follow the risk (green takes, yellow and orange skip) | Replaces Spec 041 "default none" for group steps only |
| Phases | Phase 1 = v0.2.0, phase 2 = v0.3.0 | [Scope and phases](#scope-and-phases) |
| D6 72 hours | R1 (recent activity) looks at worktrees whose activity is at least 24 hours and at most 72 hours old (the owner's 72-hour window; this spec had proposed 7 days. The 24-hour lower boundary is the existing classifier's retain window, rows younger than 24 hours are retained; the inclusive wording is a foreman clarification, F-2, not a change of the window) | Risk ladder R1; it only raises a level, never blocks |
| D7 no label | No `you` label; keep "hint: NAME" | Owner as information |
| D8 not in phase 1 | A merged pull request head does not become a deletion proof in phase 1 | Online evidence only lifts to green (C2); the `proof` enum is unchanged |
| D9 read-only | `stw watch` and `stw pulls` stay read-only; no automatic done marking | Phase 2 `done` is explicit only |
| D10 phase 2 | A real TUI checklist in phase 2, after the line wizard has been used | T022; the line wizard stays the fallback |
| D11 with phase 2 | The agent workflow sentence goes into the workspace `AGENTS.md` with phase 2 | T021 |
| D12 once per run | **One** typed word per wizard run: the plan built and applied at once with `y` in the same run asks once; a later separate `stw tidy apply PLAN-ID` asks again; the agent path is unchanged. Replaces "twice, as today" | [Single confirmation](#single-confirmation-per-wizard-run-d12); needs a security review (T012c) before code |
| D14 yes | `a` takes all green rows of every owner, the same rule as Enter | FR-002 |
| D15 yes | The grouped text overview replaces the owner grouping of decision #255; `--rows` keeps the old view | Amendment table |
| D16 yes | In human mode the answers to "Store this plan set?", "Apply it now?" and the prompt of a separate `stw tidy apply PLAN-ID` (`internal/cli/tidy_planset.go:73-82`) are read from `/dev/tty`, not stdin; with no controlling terminal nothing is stored or applied (exit 1) | FR-018, SC-015, T013b; `--yes` with an explicit plan id is unchanged |
| D13 yes | Online is the default in the human wizard; offline is opt-in (`--offline`) | [Online by default](#online-by-default-in-the-human-wizard-d13) |

## Foreman decisions for phase 1 (owner may reverse)

Found by the independent review of the P1 code (`stw` #308) as gaps or ambiguities of this text; resolved conservatively by the foreman on
2026-10-26 and written into the sections named; the owner may reverse any of them.

| # | Decision | Where |
| --- | --- | --- |
| F-1 | Group key = (effect class, family); the group carries its effect class; the live data gives 15 groups; the mockups show them | [Overview first](#overview-first), Screens 1 to 3 |
| F-2 | R1 is inclusive at both ends (at least 24 hours and at most 72 hours); the pure ladder treats an unknown, zero or future activity time as R1 even though the classifier retains such rows today (zero as `evidence-incomplete`, future as `recently-active`), because P2 supplies the time and the rule must stay testable; R1 applies to every scope other than `own` (an empty or unrecognized scope value included) and R2 treats such a value as `unknown`; the classifier's `recently-active` (under 24 hours) stays a retain | [ladder](#the-ladder-exact-all-inputs-are-existing-evidence-unless-marked) step 3 |
| F-3 | An unknown kind or proof word is red (`not-judged`) | [Levels](#levels) |
| F-4 | Stale row with incomplete evidence, primary checkout with observed dirt, tracking reference with proof `none` and a reason other than `unmerged`: red; the jj proof word is yellow; unknown levels count as red in group counts | [Levels](#levels) |
| F-5 | `proof-github-branch-absent` is a risk reason word; the words are an exported enumerable set | [Reasons](#reasons-why-orange) |
| F-6 | Group keys use `[a-z]` of the lexically cleaned path only; `no-work` never cancels a red cause of a retained row | [Overview first](#overview-first), [Levels](#levels) |

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
| 1 | v0.2.0 | Janitor mode, risk model, online by default with `--offline`, grouped overview, group steps, selectors, final summary, retain table, human wording, the `--rows` and `--offline` flags |
| 2 | v0.3.0 | `stw workspace done`, tidy use of the done mark, adopt for unlocked legacy worktrees, the TUI checklist (D10), a later look at the PR-attested proof class (D8) |

Phase 1 adds no dependency and no new write effect. Everything the wizard deletes is deleted by the existing apply path of Spec 041.

## Janitor mode

**Definition.** Janitor mode is Spec 041 human mode, nothing more: both standard streams are terminals, `STW_AGENT` is unset, no agent
marker is in the environment, and `--mine` is not given (`internal/tidywizard/owner/mode.go:80`). It remains an **accident guard,
not a boundary** (041 FR-006g): an agent that drives a pty is not stopped by it. The deletion safety stays with the proofs (041
SI2), the stale checks, the recovery manifest and the typed prompt.

**Rule.** In janitor mode a row is choosable iff its verdict is `safe`, or `decision` (by its number only, see FR-001). The owner label,
`owner_source` and `scope` no longer influence which rows may be chosen, selected by a group, or taken by `a`. They are shown as
information and feed the risk rules R2 and R3.

**What the human path already allowed.** A human may already choose any safe row by number (`owner.Plannable` returns every entry
in human mode, `internal/tidywizard/owner/scope.go:56`), and `--by-owner` already narrows only the view. What blocks the owner
today is the shortcuts: `a` takes `own` rows only, `o` asks per owner label, and the layout groups by owner. Those change.

**New meaning of "explicit choice".** Spec 041 requires an explicit human choice for work of other owners and forbids preselecting it.
In janitor mode a **selection** is made by number, selector, a group answer (Enter on a green group, `y`) or `a`; each counts as the
explicit choice. A selection applies nothing. What protects the owner is unchanged and sits after it: the final summary, the typed
`/dev/tty` words, the stored plan and the apply path (changed only by the D12 receipt, below). In agent mode there is no selection at all.

### Amendments to Spec 041 and the online contract

Every Spec 041 text that letting Enter, `a` or a group take rows of other owners contradicts, with its new wording (line numbers are
those of `specs/041-tidy-wizard/spec.md` at main `6d4c75ed`):

| 041 text | Today | New wording (human path) |
| --- | --- | --- |
| US4a (line 56) | foreign items "never preselected and need the same proof" | Foreign items are shown with their owner as information; a selection (see above) is the explicit choice; the same proof is required |
| Non-goals, line 34-36 | work of other owners "removable only by explicit choice with the full proof" | Unchanged in substance: a selection is the explicit choice; an agent-run command never touches it |
| Candidate table, line 222 (unregistered or foreign worktree, `safe`, `scope` `other` or `unknown`) | "human mode: shown, never preselected, removable only by explicit choice" | "human mode: shown with its owner and risk, choosable by number, selector, group answer or `a` (green only); agent mode: not shown and not plannable" |
| Ownership section, human mode, line 398 | "No candidate with `scope` `other` or `unknown` is ever preselected: `a` ... selects `own` items only" | "No selection is ever stored or applied without the summary and the typed words. `a` takes all green rows of every owner; a green group takes its rows on Enter; yellow and orange groups need `y`" |
| FR-006a (line 577) | "all candidates ... visible ..., grouped by owner" | Visible without any flag; the text overview is grouped by kind and risk with the owner as a column; `--rows` prints the by-owner view; JSON order is unchanged |
| FR-006b (line 578) | "never preselected (`a` selects `own` only)" | As the ownership row above; red rows are never selectable and `decision` rows only by number |
| SC-011 | `a` selects own safe items only | Replaced for the human path by SC-001 and SC-002 here; the agent-mode half stays |
| SI4 (line 563) | "other owners' work is touched only by a human's explicit choice with the full proof, never preselected, behind a typed `/dev/tty` confirmation, and never by agent-run mode without that human authorization" | "... touched only by a human's explicit choice with the full proof (a number, selector, group answer or `a`), behind the typed `/dev/tty` confirmation; never in agent mode without that human authorization; a selection applies nothing" |
| Decision 14 (line 708), the legacy completion | "exact path, declared complete by the human's explicit choice" | The selection covering that path is the declaration; the final summary lists the paths (counts) before the typed words. All 122 safe worktrees of the live data are unregistered, so all go through this path |
| FR-006f and "Typed confirmation for foreign items" (the confirmation of a chosen foreign item, and of `apply` of a plan with `authorized_scopes`) | the typed word is read when the plan set is built and again at every apply | D12: **one** typed word per wizard run (the plan applied at once with `y` in the same run); a later separate `stw tidy apply PLAN-ID` and the agent path ask as today ([Single confirmation](#single-confirmation-per-wizard-run-d12)) |
| Wizard apply question (line 671): "Foreign items still need the typed `/dev/tty` confirmation inside the apply path" | the typed words are asked inside the apply path | In a janitor run the words are asked once, when the plan set is built, and the receipt covers the apply of that same plan; a separate apply asks inside the apply path as today (D12) |
| `authorized_scopes` and the plan id (lines 405 to 407): "only the human path ... applies `authorized_scopes`"; "the id typed at apply" | the human path applies; the id is typed at apply | Unchanged for a separate apply. In a janitor run the plan id is matched in memory by the receipt instead of being typed; editing `authorized_scopes` changes the id, so a receipt for the old id never matches |
| Plan set and apply, "on a TTY the prompt names the plan id and the effect counts"; SC-012 (a plan with `authorized_scopes` is applied only by the human path with the typed confirmation) | the apply prompt names the plan id; the typed confirmation is required at apply | Still true: the typed confirmation happened in the same human run and is bound to that plan id; a separate apply of a stored plan asks again |
| Wizard interaction, Rules | "the default answer is always none/no" | Group steps only: default by risk (D5). The questions that store or apply, and the typed prompt, keep the default no |
| Prompt text (`prompt.Select`) | one question over all rows | Overview, group steps and the expert line; strict parsing (3 invalid answers end the run, end of input chooses nothing, 256-byte lines) is kept |
| Grouping of the text overview, decision 2026-10-25 (#255), rules 1 to 6 | owner, then repository | Replaced for text output by kind and risk (D15, decided); `--rows` prints the old view |
| Risks, line 726 | "mitigated by default none, a summary with effect counts, `decision` rows never included in `a`" | "mitigated by green-only defaults, a summary with the count of directories deleted for good, the typed words, and `decision` rows never taken by a group, `y`, `a` or a selector" |
| OQ13 (owner of an unregistered worktree) | `commit-author` is a hint, never `own` | Unchanged; there is no `you` label (D7, decided) |
| 041 SI9 (line 565) and FR-016 (line 595) | "no network unless `--online`"; "`--online` is the only way to network; default offline" | Human wizard only: online by default, `--offline` opts out ([D13](#online-by-default-in-the-human-wizard-d13)). Agent mode, `stw tidy plan`, `--json`, `--save` and the expert layer: unchanged |
| 041 non-goal (line 37) | "no fetch beyond what `--online` already allows in contract 3; no GitHub write" | Same, with "in the human wizard, what the default online reads allow"; still no fetch and no write |
| 041 `--online` flag row (line 133) | "allow the contract-3 authenticated reads (default off, as today)" | "default on in the human wizard, off with `--offline`; opt-in elsewhere" |
| 041 observer options (line 241) | "`Online` and `Remote` are set only by `--online`; without `Online` no network is attempted" | `Online` and `Remote` are set by `--online`, or by the human wizard's default when the run is online; without `Online` no network is attempted (unchanged) |
| 041 Decision 8, tracking refs (line 703) | "only with `--online` (contract 3 needs it); offline wizard skips them with a notice" | In the human wizard tracking references are analysed whenever the run is online (default); with `--offline` they are skipped with the notice `tracking-requires-online`, as today. Elsewhere unchanged |
| Spec 029 FR-001 (line 87), the Spec 029 online contract; Spec 031 FR-011 (line 105) | "online evidence requires explicit `--online` selection"; "forge-free proofs run without `--online` and without network" | 029 FR-001 is unchanged for the expert layer, agents and `plan`; for the human wizard the contract-3 reads run without the flag under the budget in D13. 031 FR-011 stays true (forge-free proofs need no network); the reads, endpoints, bounds, token handling and "no write" rules are those documents' own. Spec 031 and 029 texts are not rewritten; this table is the amendment |

Swept and **not** changed (they describe offline facts that stay true): 041 lines 149-150 (base and freshness are read locally, the wizard
never fetches), 041 line 340 (jj has no tracking evidence), 041 line 645 (offline observation reads no remote), 029 lines 14 and 18, 031
lines 40 and 165 (offline fixtures).

### What stays exactly as it is

- **Agent mode** (non-terminal, `STW_AGENT`, a marker, or `--mine`): no prompt, no group steps. `plan` and `plan --save` list and
  plan `own` items from a task record only (041 FR-006c, `owner.Plannable`, `planset.SelectSafe` at `internal/tidywizard/planset/planset.go:300`).
  `apply` refuses `other` and `unknown` items without `authorized_scopes` (`scope-not-authorized`, 041 FR-006d), and
  `authorized_scopes` is written by the human path only (`owner.Authorize`, `scope.go:67`) and bound into the plan id.
- **Typed confirmation** (041 FR-006f): the confirmation word is the owner label (scope `other`, one word) or the literal word
  `unknown`, followed by the first six characters of the plan id, read from `/dev/tty`, asked once per word in sorted order
  (`internal/tidywizard/owner/confirm.go:45`, `:59`; `planset.go:468-494`). The words, their text and the plan-id binding are unchanged.
  What changes is how often they are asked in one run (D12, below): once, where today they are asked when the plan set is built and
  again at apply (`apply.go:300`). Items of scope `own` need no word.
- Scopes, `owner_source`, the owner derivation order, the proofs (ancestor, patch-id, merge-noop, no-net-change), the recently-active
  retain rule (24 h, `classify.go:33`, `:413`), the plan set, the dependent branch leg, journals and recovery roots.
- Exit codes 0, 1 and 2, the no-terminal degradation (US7, FR-009), `esc` and the 120-character bound (FR-042).

## Risk model

Each choosable row gets one level, each group the worst level of its rows (and the counts per level). Rows that are kept get a level only
when they may hold unique work.

### Levels

| Level | Meaning | Offered for deletion |
| --- | --- | --- |
| green | The work is provably on the base, or nothing unique exists, and what is deleted is a clean copy or a restorable reference | yes |
| yellow | Integrated by equivalence (same patches, not the same commits) | yes |
| orange | Integrated by equivalence **and** one more fact makes a mistake likelier (recent activity, nobody can vouch, another owner's unfinished task, a decision row) | yes, never a default |
| red | Possible unique work: see the list below | never |
| kept | Kept for a structural reason and nothing known says unique work is there | never |

**Red** is any kept row, whatever the first reason the classifier found (it checks `evidence-incomplete`, `locked-foreign` and
`registration-record-missing` before the content reasons, `internal/tidywizard/classify.go:396-409`), for which at least one of
these holds: proof `none` on a branch or an existing directory (`unmerged`, and also `registration-record-missing` rows that are not
integrated); `dirty` or `ignored-extras` observed; or the row is **not judged** (`evidence-incomplete`), because then unique work cannot
be ruled out. Primary checkouts, stale registrations whose directory is gone, and rows that are integrated (proof not `none`) and
kept for `registration-record-missing`, `branch-attached-elsewhere`, `recently-active`, `locked-foreign` or `protected` are **kept**:
their dirt is not necessarily known in this view, and the text says so ("integrated; not checked for uncommitted files"). The
risk function reads the observed facts (proof, `Dirty`, `IgnoredExtras`, `Incomplete`), not only the reason; where the adapter does not
expose a fact, T001 adds it.

**Foreman clarifications for the red and kept edges** (2026-10-26, conservative; the owner may reverse any of them): (a) a row whose kind or
proof word is not one the ladder knows is **red** (`not-judged`), before any strength rule runs, for safe, decision and retained rows alike;
(b) a stale-worktree row with incomplete evidence is **red** (not judged), a stale row without it stays kept; (c) a primary checkout with
observed dirt is **red**; (d) a tracking reference with proof `none` and a reason other than `unmerged` is **red**; (e) the jj proof
`empty-working-copy-integrated-parent` is **equivalent** (yellow), never strong; (f) `no-work` evidence only lifts strength on a safe row:
it never cancels a red cause of a retained row (a record-missing row with proof `none` stays red); (g) a level the code does not know counts as **red** when group counts and the worst level are computed.

### The ladder (exact, all inputs are existing evidence unless marked)

1. **Strength of the proof, S.**
   - **strong** if the proof word is `ancestor`, `no-net-change` or `github-branch-absent` (a tracking reference); or the evidence is
     `no-work` (no commit of its own; 041 FR-006h); or the tip OID equals the head of a pull request merged into the base (C2,
     forge evidence, see D13).
   - **equivalent** otherwise: `patch-id` or `merge-noop`. Whitespace-only differences are never `safe` (the wizard has no
     `--accept-whitespace-differences`; those rows stay `retain` `unmerged`, hence red).
2. **Base level.** strong gives **green**; equivalent gives **yellow**. The effect type is shown, not scored: a `delete-branch` effect is
   restorable with `stw tidy restore` from its recovery ref (`docs/tidy.md:566`); the removal of a worktree directory is not restorable
   (`docs/tidy.md:584`, `:348-351`), but `safe` guarantees a clean tree with no ignored extras and a proven HEAD that stays pinned.
3. **Raises**, each adds one step (green, yellow, orange), at most orange, each counted once:
   - **R1 recent activity**: a worktree whose scope is not `own` and whose newest HEAD, index or tracked-file time (`LastActivity`, already
     observed) is **at least 24 hours and at most 72 hours** old, **both ends inclusive** (the window is D6, decided). An **unknown, zero or
     future** activity time counts as R1 as a defensive rule of the pure ladder (it cannot be shown to be old; the classifier retains such rows today, P2 supplies the time). A row younger than 24 hours is already `recently-active` and kept
     by the classifier (`classify.go:413`, `<` 24 hours), so it never reaches the ladder. Every scope other than `own` counts, including an
     empty or unrecognized scope value.
   - **R2 nobody vouches**: a worktree, S equivalent, with no creation record and an owner of scope `unknown`; an empty or unrecognized
     scope value counts as `unknown` (no task record, no path convention).
   - **R3 (phase 2) another owner's unfinished task**: a creation record exists, its owner is not the caller, and it has no done mark.
   - **R4 decision rows**: a `decision` row (`lock-mismatch`) is at least orange.
4. **Weak corroboration never makes green.** Two facts only add text and, where stated, remove a raise; neither lifts `equivalent` to
   `strong`, because neither is integration evidence:
   - **C1 upstream gone** (a branch whose remote-tracking branch no longer exists, read from the local reference database, no
     network): adds the text "upstream branch is gone". A pull request closed unmerged, a manual delete or a prune produce the same
     state, so it changes no level.
   - **C3 done mark** (phase 2): a claim by the task's owner. It removes R3 and R2 for that row and adds the text "agent says done". A
     done mark never lifts a row above **yellow**; only forge evidence (C2) lifts it to green. Who may set the mark: see Phase 2.
5. **Red rows** have no number a human could type and match no selector; the row shows its reason and, where known, the consequence
   ("2 commits on this branch exist nowhere else").

### Reasons ("why orange")

Every level carries `risk_reasons[]`: stable kebab-case words in JSON (`proof-ancestor`, `proof-no-net-change`, `no-own-commit`,
`pr-merged-head`, `proof-equivalent`, `recent-activity`, `nobody-vouches`, `task-not-done`, `decision-row`, `upstream-gone`,
`done-mark`, `proof-github-branch-absent` (a tracking reference proof), and for red `not-integrated`, `uncommitted-files`, `extra-files`, `not-judged`) and one human sentence per word in text. The words are an exported, enumerable set (a list the sentence test walks, SC-010).
Example, orange: "Why orange: the work is on main as equal patches (a rebase merge), not as the same commits; the directory is deleted for
good; no record says whose it is."

### Consequence facts (shown with every group, never scored)

Kind and effect, the number of items and repositories, the recovery location (the state directory of the run, recovery refs
`refs/stw/recovery/v3/...`), the undo command where one exists (branches only: one `stw tidy restore PATH --contract 3 --state-dir DIR
--plan ID` per repository, the ids printed by `stw tidy history`, `docs/tidy.md:571-587`), and, when known, an upper bound of size: the
tracked-file count that the activity scan reads (at most 8,192 per worktree; 041 Read-only observers). No new filesystem walk.

### Measured example

Applied to the live data (R1, C1 and C2 are not evaluable from the file, so they change nothing here; the mockups show the offline
ladder): **green 6, yellow 93, orange 66** of the 165 safe rows. Of the 964 retained rows **394 are red** (301 `unmerged`, 47
`registration-record-missing` worktrees with proof `none`, 23 `ignored-extras`, 17 `evidence-incomplete`, 6 `dirty`) and **570 are
kept** (299 record-missing worktrees that are integrated, 17 stale record-missing, 217 `branch-attached-elsewhere`, 27
`recently-active`, 7 `primary-checkout`, 3 `unregistered` stale). The dirt of the 299 integrated record-missing rows is not in the file.

## Online by default in the human wizard (D13)

**Decision (owner, 2026-10-26): "online std? offline optin?" answered yes.** In janitor mode the wizard performs the existing
contract-3 authenticated reads without a flag, and `--offline` opts out. Everything else about `--online` stays: github.com only,
read-only GET requests, the Spec 031 closed endpoint set, the token never in output (041 SI10), no write call.

| Situation | Behavior |
| --- | --- |
| Human wizard (`stw tidy` on terminals), authenticated, remote is github.com | Online reads run; one line "GitHub: checked N of M repositories" |
| Not authenticated, remote not github.com, an error or deadline of one member's session, that member's own 128-request or 30-second budget | **That member only** is judged offline; one notice line names the reason; exit code unchanged; levels as offline |
| Rate limit exhausted (403 or 429), a 5xx, the per-run cap of 512 requests, `--timeout` expiry | **Every member that has not finished its reads** is judged offline; members that finished keep their online levels; one notice line; nothing is retried |
| `--offline` | No network, as today; `--online` and `--offline` together are a usage error (exit 2) |
| `stw tidy plan`, `stw tidy --json`, `--save`, agent mode | Offline by default; `--online` opts in, behavior as today; `--offline` is accepted (wizard layer) and changes nothing |
| The expert layer (a `PATH` or any expert flag) | Offline unless `--online`, as today; `--offline` is **not accepted** there (exit 2, like every wizard-only flag) |
| An explicit `--online` | Behavior as today (the contract-3 failure rules); only the implicit default falls back, and it says so in its one notice line |

**Phase 1 use of the reads.** Spec 031 discovery (FR-013 to FR-018) runs per explicitly resolved repository, closed endpoint set, up to 10
pages of 100 closed pull requests, 128 requests, 32 MiB and one 30-second deadline per run, and targets unproven tips. The wizard extends
it, read-only, to the `safe` tips whose proof is only `equivalent`, so that a merged head makes them green, and to `unmerged` tips for
a hint. Positive evidence is usable whatever the coverage; absence is inconclusive (031 FR-017).

**Budget across the workspace (F6).** The live workspace has 7 members and about 466 distinct tips (165 safe plus 301 unmerged), more than
one session can cover. The allocation:

- **Per member:** Spec 031 FR-018 states 128 requests, 32 MiB and 30 s "per run" for one explicitly resolved repository; the wizard reads
  that **per member**: one session per member with those limits unchanged, members through the existing fan-out (width 4).
- **Per run:** at most 512 requests in all; the members that have not finished when the cap is reached are judged offline for the remainder (the run-wide rule in the table above). `--timeout`, when given,
  bounds the whole analysis including every online session; a session ends at that deadline.
- **Tips per member:** at most 64 distinct tips, taken in this order: `safe` rows with an `equivalent` proof (by name), then `unmerged`
  rows (by name). The rest are "not checked"; the overview says "N of M rows not checked against GitHub". On a workspace like the live
  one coverage is often incomplete; that is expected and harmless (offline levels apply).
- **Rate limit or server trouble:** a 403 or 429 that reports an exhausted rate limit, a 5xx, or the expiry of `--timeout` stops all further online
  calls of the run (a timeout of one member's own 30-second session ends that member only, see the table above), the unchecked rows are judged offline, nothing is retried; one notice line.
- **Bookkeeping:** requests are counted before they are sent and failures count (031 FR-018).

**Phase 1 effects.** A `safe` row whose tip equals a merged head is strong (green, evidence "merged as #N"). An `unmerged` row whose tip
equals a merged head stays red with the added text "merged as #N, but the local proofs disagree: review it" (D8, decided: this does not become a proof in phase 1). A tracking reference with `github-branch-absent` (contract 3) now appears in the human wizard by default, as a group
"Tracking references" (local reference deletion; strong). The latency of the reads shows on the stderr progress line (#297).

**Phase 2.** D8 only: whether `github-merged-head` becomes a deletion proof (it changes the frozen `proof` enum).

## Overview first

The grouped overview replaces the row wall as the first screen of `stw tidy` and of the text of `stw tidy plan`. It is built from the
same candidates the JSON carries; `--json` is unchanged except the additive fields in [Interfaces](#interfaces).

**Groups.** The group key is (**effect class**, **family**). Effect classes: `branch` (delete-branch only), `directory` (remove registration and
files) and `directory-with-branch` (the directory and its branch leg); tracking references form their own class (online only). The family of
a **branch** group is its repository (the undo command is per repository); the family of a directory is its place and name family below.
On the live data this gives **15 groups** (7 branch groups and 8 directory groups, computed from `overview.json` with this rule; Screen 1), not the 7 of the first mockup: the 55 `whr-*` directories are
two groups (40 without and 15 with a branch), the 43 branches are seven repository groups, and `crewbook-*` is two groups. A group carries its
effect class so the renderer can tell groups of one family apart. Details of the places:

- **Branch deletions** (`delete-branch` only): one group per repository.
- **Tracking references** (online only).
- **Worktree directories**, by place: *agent scratchpads* (a path component equal to `scratchpad`), *`/tmp`* by **family** (the leading
  ASCII lowercase letters of the first path component below `/tmp`; for `/tmp/workharbor-baseline-0jbykfta/checkout` that is
  `workharbor`, for `/tmp/whr007-ci-binding` and `/tmp/whr-006-t030-apply` it is `whr`; a family needs at least 3 members, otherwise the
  items go to "other in /tmp"), and *in the workspace* (anything else, by parent directory).
- **Kept** rows are not groups; they are the [retain table](#retain-explained).

Paths are cleaned lexically (`path.Clean`, no resolution, no I/O) before the family and the `/tmp/` test, so `/tmp/../home/x` is not in
`/tmp` and `/tmp//whr-a` has the family `whr`. A family has at least 3 members **in all effect classes together**, otherwise its items go to
"other in /tmp". The key is built only from `[a-z]` of the cleaned name, so a hostile name (for example one that starts with letters and carries ESC) cannot
inject bytes into a key; all names displayed pass `esc` and the 120-character bound.

Groups are ordered by worst risk (green first), then by size. Each line says what happens, how many, how risky, and what undoes it.

**Wording.** Text uses sentences, not codes. Codes (`registration-record-missing`, `patch-id`, `branch-attached-elsewhere`, ...) appear
only in `--json`, `--verbose`, `--rows` and `d N` details. A table in `internal/tidywizard/render` maps every reason, proof and risk word
to a sentence and an action, and a test fails when a code has none (SC-010).

**Owner as information.** The owner is a column of the group step and a count line (for the whole overview "owner: 101 hint Werner Stein, 57 codex, 7 claude"; in a group step, for example, "owner: codex 40"); the label is
the derived one (`codex`, `claude`, `author:NAME` shown as "hint: NAME", `unknown`). There is no `you` label: agents commit under the
owner's Git identity, so a match with that identity would show foreign agent work as the owner's (D7).

## Wizard steps

Human mode with a terminal only. After the overview the wizard walks the groups in the order shown. Nothing is changed by any of
these answers; they build a selection.

**One screen per group:** header (what, how many, level counts), the consequence lines, the "why" of the worst level, three example
rows, then the question. Answers are one line followed by Enter (no raw mode in phase 1).

### Keys and grammar (one table)

| Input | Where | Meaning |
| --- | --- | --- |
| Enter | group step | the default of the group: **take** for a green group, **skip** for yellow and orange (D1, D5) |
| `y` | group step | take the whole group's choosable rows (also yellow and orange); never a `decision` row |
| `s` | group step | skip the group |
| `l` | group step, retain table | list every row of the group (20 at a time) with its number |
| `r` | group step | review each row: `y`, `n` or `q` per row (the only place a number-less row choice is possible) |
| `d N` | any list | show the details and the codes of row N |
| `a` | overview, final summary | take **all green rows** of every owner (D14); never yellow, orange or `decision` rows |
| `e` | final summary | edit the selection with a line of terms (below) |
| `q` | any question | quit; nothing is stored |
| a term line | group question, `e` | numbers, ranges, selectors and `except` |

```
line      = term { (" " | ",") term } [ " except " term { (" " | ",") term } ]
term      = NUMBER | NUMBER "-" NUMBER | selector
selector  = "a"                all green rows
          | "b"                all branch deletions
          | "w"                all worktree directories
          | "t"                all worktree directories whose path starts with /tmp/ (this includes the agent scratchpads)
          | "o:" LABEL         all rows of that owner label, exact as shown; a label with a space is quoted: o:"author:Werner Stein"
          | "m:" REPO          all rows of that repository
```

The result is the union of the terms before `except`, minus the union after it, intersected with the choosable rows. Red and kept rows
have no number and match no selector. **`decision` rows are taken by their number only**, each with the Spec 041 per-row second
confirmation, and never by `y`, `a`, Enter or any other selector. A word that does not parse, a number that is not choosable, an
empty `except`, an unterminated quote, or a line over 256 bytes is an invalid answer (three end the run, as today). Example:
`w t except 85-86`. Numbers are the display numbers printed by `l`; they are stable within the run.

### Final summary and the typed prompt

After the last group, one summary lists the selection by group with counts, level counts, effects, the typed words that will be asked,
and the undo lines (screen 3). **Its last line, directly before the first typed prompt, states the irreversible effect: "This deletes N
directories for good and M branches (restorable)."** (Owner condition for D2.) The order:

1. the summary and that line;
2. if foreign items are chosen, the typed confirmation per word, sorted (`claude`, `codex`, `unknown` in the mockup), read from
   `/dev/tty`, plain, while the plan set is built (`planset.go:492`); if all chosen items are own, the new question "Store this plan set?
   [y/N]" (default no) follows the line;
3. the stored plan id and the question "Apply it now?" (default no), whose text now repeats the counts;
4. on `y`, the apply runs **without a second typed word** (D12): the words of step 2 already covered this very plan.

**What changes against today's flow** (`internal/cli/tidy_interactive.go:48-93`, where a choice is built and stored at once, then "Apply it
now?", and the apply path asks the typed words again): the grouped steps and the summary replace the single "Apply which?" question; the
summary's last line is new; the store question for an own-only selection is new; the apply question gains the counts; **the typed word
is asked once per run, not twice** (D12). **Unchanged**: the words and their text, the plan set built by `planset.Build` (which additionally returns the receipt), the store, the stale checks, the
default no on every storing and applying question, and the whole path of a separate `stw tidy apply PLAN-ID`.

### Single confirmation per wizard run (D12)

**Decision (owner, 2026-10-26): one typed word per wizard run.** This changes the apply path and therefore **requires an independent
security review of the design and of the exact code head before any code is merged** (T012c; the review also covers the cases below).

**Mechanism.**

- **Creation.** `planset.Build` (`planset.go:463`, which asks the words at `:491-492`) returns, next to the set, a **confirmation
  receipt** when and only when it just obtained the typed word for every foreign word of this set from `/dev/tty` (a set of own items
  only has no words and gets no receipt). The receipt is a value of an opaque type of package `planset` with unexported fields that
  nothing else can construct: the full 12-character plan id, the sorted words that were confirmed, and a used flag. It is never written
  to the state directory, a file, the environment or an argument and has no serialized form. (Build already refuses agent mode for foreign
  words, so a receipt exists only for a human run; no separate mode or nonce field is kept.)
- **Use.** `ApplyRequest` (`apply.go:198-205`) gains one field, the receipt. Apply skips the typed prompt for a word only if **all**
  hold: the receipt is present and unused; its plan id equals the plan id of the set being applied (content-addressed over the items,
  `authorized_scopes` and the mode evidence, so another plan, an edited plan or a plan with an added foreign word never matches); and
  every word the set requires now (`confirmWords`, `apply.go:235-243`) is in the receipt's list. Otherwise the typed prompt is asked as
  today. A mismatch never skips a prompt and never refuses by itself.
- **Who passes it.** The shared entry `runTidyApplyPlanSet` (`internal/cli/tidy_planset.go:57`) gets one parameter for the receipt.
  Its two callers are `stw tidy apply` (`internal/cli/tidy.go:111`), which always passes none, and the wizard
  (`internal/cli/tidy_interactive.go:93`), which passes the receipt from its own `Build`. Both files are in work package P4.
- **Single use.** The receipt is **consumed when apply starts** (after the plan id check, before the first effect), whether the apply then
  succeeds, fails or is interrupted; a second apply attempt in the same process asks the words. It dies with the run: answering `n`,
  Enter or end of input at "Apply it now?", an interrupt or any error before apply discards it, and it is never kept for a later `apply`.
- **Nothing else is relaxed**: the stored plan is re-verified (store mode, owner, content-addressed id) and every stale check runs at
  apply exactly as today; a stale plan refuses with `evidence-changed` and changes nothing; `--yes` is still not honored for plans with
  foreign items.

**Residual risk, stated precisely.** Today, applying a plan with foreign items needs a typed word read from `/dev/tty` at the apply as
well. With the receipt, within one run the apply rests on the `y` answer to "Apply it now?", which is read from **stdin**
(`internal/cli/tidy_interactive.go:74`), after the words were typed at build. What a stdin writer gains: it can approve the apply of exactly
the plan whose words the human just typed on `/dev/tty`, not another plan and not a later one. The same stdin `y` is already the only gate
before applying an own-only plan. D16 (decided) reads this answer from `/dev/tty` too, which removes this residual risk.

**Other runs.** A plan stored by an earlier run, or applied by a separate `stw tidy apply PLAN-ID` (with or without `--yes`), has no
receipt, so the typed words are asked at apply exactly as today. The agent path is unchanged: agent mode never builds a receipt and its
`apply` still refuses `other` and `unknown` items without `authorized_scopes` and still needs the human path for them.

**Security review questions** (T012c): can any code outside `planset.Build` construct or copy a receipt; whether consuming it when apply starts is enough; can a receipt
authorize a plan other than the one confirmed (including a plan with the same six-character prefix); what a same-user process can do that it could not
do before (it already can drive a pty, 041 FR-006g); the window between the confirmation and the apply, and the stale checks across it;
behavior on interrupt, concurrent wizard runs and a plan store changed in between; and regression of the separate apply and the agent path.

A wrong word, no terminal, an interrupt or `q` stores or applies nothing more. The typed prompt stays **plain text** (no color, no menu);
it is the protection, and the environment markers are only the accident guard in front of it.

## Retain explained

After the groups the wizard prints one table of kept rows: count, a human reason and one "what to do" sentence. Rows are shown only on
request (`l` at the table, or `--rows`). Red rows are counted separately because they may hold unique work.

| Reason (code stays in JSON) | Count | Level | What it means | What to do |
| --- | --- | --- | --- | --- |
| Created by stw, record not found, integrated (`registration-record-missing`) | 299 worktrees, 17 stale | kept | The worktree is locked `stw:TASK`, no task record is in a known state directory; not checked for uncommitted files here | Re-run with the `--state-dir` that created them (agent scratch stores are separate directories). Nothing else frees them: neither phase offers another way |
| Created by stw, record not found, **not integrated** | 47 | red | As above, and its commits are not on the base | Do not delete. Find the right `--state-dir` and judge it with the review report |
| Not merged (`unmerged`) | 301 | red | The branch or worktree has commits the base lacks | Nothing is deleted. Judge them with the review report: expert command `stw tidy plan REPO --contract 3 ... --report-dir DIR` (Spec 031) |
| Branch is checked out elsewhere (`branch-attached-elsewhere`) | 217 | kept | A kept worktree has this branch checked out | Decide on that worktree first (it is kept for its own reason and may hold unmerged work), then run again |
| Recently active (`recently-active`) | 27 | kept | A job may still be running there (changed within 24 hours) | Wait, then run again |
| Untracked or ignored files (`ignored-extras`) | 23 | red | The directory holds files Git does not track | Look inside and remove them yourself if they are junk; stw never deletes them |
| Not judged (`evidence-incomplete`) | 17 | red | A deadline or proof budget ran out, or Git refused the tree | Run again with a larger window, for example `--timeout 15m`; until then assume unique work |
| Repository checkout (`primary-checkout`) | 7 | kept | The main checkout of a repository | Never offered |
| Uncommitted changes (`dirty`) | 6 | red | Tracked files differ from the commit | Commit or discard them yourself |
| Registration without directory (`unregistered`, stale) | 3 | kept | A worktree entry whose directory is gone | No stw action yet (Spec 041 T012b) |

The counts are those of the live workspace and sum to 964 (394 red, 570 kept); the sentences are fixed text per code (SC-010).

## Phase 2

### Agents say "done" (`stw workspace done`)

`stw workspace done TASK [--merged PR] [--state-dir DIR]` records in the **existing task record** (the file `stw workspace apply`
wrote) that the task is finished and, optionally, which pull request merged it.

- **Not the state field.** The record's `state` is `complete` when *creation* completed (`internal/workspace/workspace.go:462`), so the
  mark is a new additive optional block, like `origin` (Spec 042 D1, D3): `done` with `at` (UTC), `by` (the origin kind) and
  `merged_pr` (number, optional). The frozen v1 workspace schema must allow it; T017 verifies that as Spec 042 did.
- **State directory only.** No network, no remote write, no change to any repository or worktree. The record is rewritten atomically,
  only the `done` block is added, an existing `done` is returned and not overwritten.
- **Who may mark.** The command takes the Spec 042 D5 gate, which refuses another session's task (`attribution-mismatch`) and
  never blocks a human run; it does **not** gate a task without a record or without a recorded session (042 D5), so any agent on such a
  task, or any process of the user, can set the mark. It is therefore **a claim by whoever ran it, not evidence**.
- **Effect on tidy.** The mark never makes anything deletable and never lifts a row above **yellow** (ladder step 4); it removes R2 and R3
  and adds the text "agent says done". Only forge evidence (C2) makes green. Without a proof the row stays red.
- **Agent step.** The workspace `AGENTS.md` workflow would gain: "After the owner reports the merge, run `stw workspace done TASK
  --merged PR`." (D11, decided: with phase 2; the owner's file.) Agents that never run it lose nothing: the online reads (C2) find the merged pull request
  from the branch tip.
- **Automatic marking.** Not done (D9, decided): `stw watch` and `stw pulls` are read-only by contract (Specs 035 and 036), and C2 already
  recognizes a merged head at tidy time without writing a record.

### Adopt legacy worktrees

For **unlocked** worktrees without a creation record, `stw tidy adopt PATTERN=OWNER ...` (example `/tmp/whr*=codex`) supplies an owner
label the built-in path table does not know. It removes R2 for them and nothing else. **It does not help the 363
`registration-record-missing` rows**: those are all locked `stw:TASK` (`classify.go:380-383`, `:401`), owned retirement binds the creation
record, and only the `--state-dir` that holds it frees them. Safeguards: human mode and a terminal only; the pattern is an absolute path glob
under `/tmp` or the workspace, never `*` alone, matched only against existing linked worktrees of known members (never the primary or
current checkout); the matches are listed and need `y`; the result is an **adoption record** in the state directory (new `adoptions`
namespace: path, repository identity, the HEAD at adoption, label, time), not a task record. It sets `owner` and a new `owner_source`
value `adopted`, which never counts as `task-record` for agent mode, and **never changes a verdict**.

### Real TUI checklist

An optional checklist (arrow keys, space) needs a terminal library and therefore a pinned dependency; Spec 041 OQ10 chose plain lines.
Decided (D10): in phase 2, after the line wizard has been used. The line wizard stays the fallback in every case (no terminal, narrow terminal, `NO_COLOR`).

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
real `overview.json` of 2026-10-26 (counts, owners, paths and proofs are real; levels follow the offline ladder with R1 and C1 not
evaluable; the numbering and the undo ids are invented; the GitHub line shows the offline case).

### Screen 1 (mockup): grouped overview

```text
Workspace /workspaces/Anchorage   7 repositories analysed   GitHub: offline in this mockup

Can be cleaned (165 of 1,129 candidates in 15 groups)                         risk
   1    2 branches        .              reversible                          green
   2   40 directories     /tmp/whr-*     files deleted, codex                yellow  (2 green, 38 yellow)
   3   24 branches        sourcesteward  reversible                          yellow
   4   15 directories +   /tmp/whr-*     files deleted, branch too, codex    yellow  (1 green, 14 yellow)
   5    9 branches        bundledock     reversible                          yellow
   6    5 branches        crewbook       reversible                          yellow
   7    3 branches        chartscribe, sheetmill, workharbor (1 each)        yellow
   8   51 directories     /tmp/workharbor-*  files deleted, hint Werner      orange  (1 green, 50 orange)
   9    7 directories     agent scratchpads  files deleted, hint Werner      orange
  10    4 directories     other in /tmp  files deleted, hint Werner          orange
  11    3 directories +   /tmp/crewbook-*  files deleted, branch too         orange
  12    2 directories     crewbook, in the workspace  files deleted          orange

Kept (964): nothing here is offered for deletion
  394 may hold unique work (red): not merged, uncommitted or untracked files, not judged
  570 kept for another reason (record missing, branch in use, recently active, ...)

Next: one question per group. Nothing changes until the final summary and your typed confirmation.
Enter = continue   q = quit
```

The mockup folds the three one-item branch groups (chartscribe, sheetmill, workharbor) into one line and the two one-item directory groups (crewbook
and the workspace) into the last line, to fit a screen: they are still separate groups of the wizard (15 in all), and `l` shows them. Totals: 43
branches (2, 24, 9, 5, 3), 122 directories (40, 15, 51, 7, 4, 3, 2).

### Screen 2 (mockup): one group step

```text
Step 2 of 15   40 directories in /tmp:  whr-*                                 risk yellow
  Owner     codex (40), found by path convention
  Evidence  2 already on main, 38 on main as equal patches (a rebase merge)
  Effect    40 directories are deleted for good (their branches stay)
  Recover   committed history stays pinned under refs/stw/recovery/v3/...;
            deleted files are not restored
  Why yellow  the work is on main as equal patches, not as the same commits

      1  yellow  /tmp/whr-006-t030-apply-20261020      codex/006-t030-apply-20261020
      2  yellow  /tmp/whr-006-t030-cli-20261020        codex/006-t030-cli-20261020
      3  yellow  /tmp/whr-006-t030-proposal-20261020   codex/006-t030-proposal-20261020
     ... 37 more (l = list)

Take this group?  [y] take  [s] skip  [l] list  [r] review each  [q] quit   (Enter = skip) :
```

### Screen 3 (mockup): final summary and typed prompt

```text
Summary: nothing has been changed yet
  group                              items   risk     effect
  40 directories /tmp/whr-*             40   yellow   delete 40 directories
  15 directories + branch /tmp/whr-*    15   yellow   delete 15 directories and 15 branches
  43 branches (groups 1, 3, 5 to 7)     43   yellow   delete 43 branches (7 claude, 2 codex, 34 hint Werner)
  51 directories /tmp/workharbor-*      51   orange   delete 51 directories
  skipped: 5 groups (16 directories)
  total: 149 items in 7 repositories; 964 kept rows untouched
Undo: branches only, one restore per repository:
      stw tidy restore REPO --contract 3 --state-dir DIR --plan ID   (ids: stw tidy history)
Typed words that will be asked (on /dev/tty): claude, codex, unknown
This deletes 106 directories for good and 58 branches (restorable).
Foreign items (claude). Type "claude 3f9c2a" to confirm:
```

The last line is the real prompt text (`internal/tidywizard/owner/confirm.go:75`); the words follow in sorted order (`planset.go:468-494`).

### Screen 4 (mockup): retain table

```text
Kept: 964 candidates. Nothing here is offered for deletion.
  count  level  why                                         what to do
    316  kept   created by stw, record not found            re-run with the --state-dir that created them
    301  RED    not merged                                  nothing is deleted; review with the report
    217  kept   branch is checked out in a kept worktree    decide on that worktree first, then run again
     47  RED    created by stw, record not found, not on main   do not delete; find the right --state-dir
     27  kept   recently active (within 24 hours)           wait, then run again
     23  RED    untracked or ignored files                  look inside; remove them yourself if junk
     17  RED    not judged (deadline or Git refused)        run again with --timeout 15m
      7  kept   main checkout of a repository               never offered
      6  RED    uncommitted changes                         commit or discard them yourself
      3  kept   worktree entry whose directory is gone      no stw action yet
Rows: l = list a reason, or run  stw tidy --rows
```

## Interfaces

- **Flags (wizard layer).** New: `--rows` (print the row view grouped by owner, as before the change, with all rows) and `--offline` (no
  network reads; exclusive with `--online`, exit 2). Both are classified as wizard-layer flags (not accepted with a `PATH` or an expert
  flag, as `internal/cli/tidy_mode.go:31` classifies the others; the flag test lists them) and `--offline` is accepted on `stw tidy` and `stw tidy plan`, where it is a no-op in agent mode, `plan` and `--json` (they are offline by default), and refused in the expert layer. Existing and unchanged: `--verbose`, `--quiet`, `--progress`, `--trace` (progress and timings go to
  stderr, nothing to stdout; #297, merged; `internal/cli/tidy_wizard.go:109`).
- **JSON.** Additive optional fields on each candidate of `tidy-overview`: `risk` (`green|yellow|orange|red|kept`),
  `risk_reasons[]`, `group` (a stable group key), and `pr` (merged pull request number, online only); `summary` gains counts per level;
  `derived[]` gains `online` (`default`, `flag`, `off`) with a reason when it fell back. The schema is frozen but unpublished
  (`UNPUBLISHED_SCHEMAS`), so it is amended with its freeze digest in the same change. Phase 2 adds the `done` block (task record,
  workspace schema v1), the `adoptions` namespace and the `owner_source` value `adopted` (overview schema enum).
- **No new write effect.** The wizard stores a plan set exactly as today; `--save` for agents is unchanged.
- **Exit codes** unchanged; an online fallback is never an error.

## Requirements

### Functional (phase 1 unless marked)

- **FR-001** In janitor mode every row with verdict `safe` is choosable by number, group answer, `a` or selector regardless of `owner`,
  `owner_source` and `scope`; a `decision` row is choosable **by its number only**, with the 041 per-row second confirmation, never by
  a group, `y`, `a`, Enter or a selector; agent mode is unchanged (041 FR-006c, FR-006d).
- **FR-002** Enter on a green group takes it; `a` takes all green rows of every owner (D14); neither takes a yellow, orange or `decision`
  row.
- **FR-003** Each row has a `risk` and `risk_reasons[]`; each group has the worst level and counts per level. Red rows (possible unique
  work, see the risk model) have no number and match no selector.
- **FR-004** The ladder reads only evidence of the analysis (proof word, evidence word, observed dirt facts, activity time, creation
  record, owner source, upstream state read from the local reference database) and the online reads. No other input.
- **FR-005** Online reads follow D13: default in the human wizard, off with `--offline`; the budget (per member 128 requests, 32 MiB, 30 s;
  per run 512 requests; at most 64 tips per member; `--timeout` bounds everything; a rate limit stops the run's online calls) and the
  offline fallback with one notice line; no write, no other host.
- **FR-006** The text overview is the grouped overview; every code is replaced by a sentence; `--json`, `--verbose`, `--rows` and `d N`
  keep the codes.
- **FR-007** Each group shows the count, level counts, effect, recovery location, the undo command (one restore per repository, branches
  only) and an upper bound of size only when already known.
- **FR-008** One question per group with the keys and defaults of the table; the strict parsing of Spec 041 is kept.
- **FR-010** Selection grammar, `except`, quoting and selectors as specified; red and kept rows match nothing.
- **FR-011** The final summary ends with the line stating the directories deleted for good and the branches, directly before the first
  typed prompt (or the store question); the typed prompt text is unchanged, read from `/dev/tty`, plain, once per word, refused without a
  terminal or with a wrong word; within one wizard run the words are asked once (FR-017).
- **FR-018** (D16) In human mode the answers to the store question, the apply question and the prompt of a separate `stw tidy apply PLAN-ID` are read from `/dev/tty`; without a controlling terminal nothing is stored or applied (exit 1); `--yes` with an explicit plan id and agent mode are unchanged.
- **FR-017** Single confirmation (D12): a receipt as specified covers the apply of the plan set built in the same run and nothing else;
  a separate or later `stw tidy apply PLAN-ID` and agent mode ask as today.
- **FR-012** The retain table lists every reason with count, level, sentence and action; counts sum to the retained total.
- **FR-013** Output obeys `esc`, the 120-character bound and the row bounds (FR-042, FR-041); color is decoration only (the level word is
  always printed; `NO_COLOR` is honored as today).
- **FR-014** No terminal: the wizard prints the grouped overview offline, asks nothing, stores nothing, exit 0 (041 US7).
- **FR-015 (phase 2)** `stw workspace done TASK [--merged PR]` as specified; the done block is additive and idempotent; it is a claim and
  never lifts a row above yellow.
- **FR-016 (phase 2)** `adopt` as specified.

(FR-009 of the first draft, the `you` label, is dropped; the number is not reused.)

### Success criteria

Each is testable with temporary repositories and a fake hosting transport (no real repository, no network).

- **SC-001** Janitor mode: a fixture with items of `codex`, `claude`, `unknown` and own owners, all proven integrated, shows every
  `safe` row choosable and `a` takes exactly the green rows of all owners; a `decision` row is taken only by its number.
- **SC-002** Agent mode on the same fixture: no prompt, only `own` items from a task record listed and planned; `apply` of a plan
  with a foreign item and no `authorized_scopes` exits 1 `scope-not-authorized`; results byte-identical to Spec 041 SC-012.
- **SC-003** Ladder table test: one row per case (strong and equivalent proofs, `no-work`, C1 changes nothing, each raise, the cap at
  orange, each red cause including a record-missing row with proof `none` and a not-judged row); a mutation of any threshold or of "at
  most orange" fails a test.
- **SC-004** Red rows never get a number, never match a selector, and forcing one into a plan makes apply refuse (as 041 SC-002).
- **SC-005** Online double with **7 members and about 466 tips**: at most 128 requests and 64 tips per member, at most 512 in all, every
  request a read (the double fails on any write method); a merged head makes a `safe` row green with `pr` and leaves an `unmerged` row
  red; not authenticated, a non-github.com remote, 403 with exhausted rate limit, 429, 5xx and `--timeout` expiry each stop the unfinished members, and an error or 30-second timeout of one member's session ends only that member; each falls back
  to offline levels with one notice and exit code unchanged; `--offline` makes no request; `--online --offline` is exit 2.
- **SC-006** Grammar table: numbers, ranges, every selector, quoted label, `except`, a selector that matches nothing, a red number, a
  `decision` number and a `decision` hit by `y`, `a` and a selector, junk, 257 bytes; `w t except 85-86` selects the expected set; `t` includes
  the scratchpads.
- **SC-007** Transcript test of the screens and the retain table from the fixture: group order, wording, undo lines and counts equal the
  golden; the retain counts sum to the retained total; the summary's last line states the directory and branch counts, the typed words
  are sorted (`claude`, `codex`, `unknown`) and the prompt text is the real one.
- **SC-014** Single confirmation (D12): in one run the typed words are asked once, then the apply runs; with a receipt for plan A, applying
  plan B, A with an edited `authorized_scopes`, or A with an added foreign word **asks the typed words** (a receipt never skips a prompt
  it does not exactly cover); a second apply attempt after the first started asks the words (single use, consumed when apply starts, also
  after a failed or interrupted first attempt); `n`, Enter or end of input at "Apply it now?" discards the receipt; a separate `stw tidy
  apply PLAN-ID` of the stored plan asks the words again and passes no receipt (`tidy.go:111`); agent-mode apply of a foreign plan without
  `authorized_scopes` still exits 1 `scope-not-authorized`; stale checks still refuse with `evidence-changed`; the separate-apply and
  agent-mode tests of Spec 041 pass unchanged.
- **SC-015** (D16, decided) In human mode the answers to "Store this plan set?", "Apply it now?" and the `stw tidy apply PLAN-ID`
  prompt are read from `/dev/tty`: a `y` on stdin alone stores and applies nothing; with both streams on terminals but **no controlling
  terminal** the question is not asked, nothing is stored or applied, and the run ends with exit 1 and a message that names the missing
  terminal and the safe alternatives (`stw tidy plan --save`, then apply from a terminal); `--yes` with an explicit plan id behaves as before
  (foreign items still refuse it); agent mode is unaffected.
- **SC-008** The typed prompt: a bare `y`, the wrong word, stdin input and no controlling terminal refuse and store or apply nothing; the
  prompt text is unchanged byte for byte.
- **SC-009** Hostile names (ESC, CR, NUL, bidi, a name starting with letters then ESC) in branch, path and owner reach no terminal byte
  unescaped in any screen, group key or example row; the family key contains only `[a-z]`; `--json` carries them as data.
- **SC-010** Every reason, proof and risk word has a sentence and an action; a new code without one fails the test.
- **SC-011** `stw tidy --json`, the expert layer and every Spec 041 test and golden other than the text overview pass unchanged; the
  tidy flags are all classified (`--rows` and `--offline` included).
- **SC-012** With at least 960 retained rows and 165 choosable rows the first screen has at most 24 lines and no row of a retained code.
- **SC-013 (phase 2)** `workspace done`: idempotent, state directory only (a before and after snapshot of every repository and worktree
  is equal), refused for another session's task, accepted on a task without a record (and so documented as a claim), additive in the
  schema; a done mark never makes a row choosable and never lifts it above yellow.

## Edge cases

| Case | Behavior |
| --- | --- |
| Name with ESC, CR, NUL or bidi controls | escaped by `esc`, bounded at 120; the family key is `[a-z]` only |
| No terminal on stdin or stdout, or agent marker | grouped overview only, offline, no question, nothing stored, exit 0 |
| Narrow terminal (below 60 columns) | rows drop the branch column, then the example rows; the level word and counts never wrap away; width from the terminal, 80 if unknown |
| 960 or more retained rows | one table, no rows until asked; the first screen stays within 24 lines |
| Interrupted run (Ctrl-C, `--timeout`) | exit 1 naming the cause; before the plan set is stored nothing exists, after it the id is named and nothing was applied (as today, `tidy_interactive.go:98`) |
| All rows red or kept | summary says "nothing can be cleaned"; the retain table follows; no questions |
| Nothing to clean | one line "Nothing to clean."; exit 0 |
| A group of 1 item | the step is still shown; one line header |
| Mixed levels in a group | `y` takes all choosable rows; `r` reviews each; Enter follows the worst level (skip if any yellow or orange) |
| Selection includes a `decision` row | only its number takes it, with the per-row second confirmation |
| `--by-owner`, `--only`, `--member` | narrow the view first; groups and counts are computed on the narrowed set |
| Online fails, rate limit, not authenticated | levels as offline; one notice; exit code unchanged |
| Stale state between the selection and apply | the existing stale checks refuse with `evidence-changed`; nothing changed |

## Composition with other work

- **#297 (merged).** Progress on stderr (`internal/cli/tidy_progress.go`), `--verbose` timings, `--quiet`, `--progress` and `--trace`
  are unchanged; the grouped overview and the steps write to stdout, progress never does. The analysis and the online reads run before
  the first screen.
- **The strict prompt.** The janitor flow only builds the selection and the summary; it calls `planset.Build` (`internal/cli/tidy_interactive.go:60`), which now also returns the opaque confirmation receipt (D12), the
  same typed prompt, and `runTidyApplyPlanSet` (`:93`) with a new receipt parameter; `stw tidy apply` (`tidy.go:111`) calls the same function and
  passes none.
- **Spec 042.** The done command uses the attribution gate (D5) with the limits stated above. No session id is printed or stored in the
  new records.
- **Spec 043.** `stw land` is unrelated except that a landed pull request becomes C2 evidence for tidy.

## Non-goals

- No automatic apply, no `--yes` for janitor plans, no scheduled cleaning.
- No deletion of unmerged or changed work, ever; red rows have no number.
- No loosening of agent mode, scopes, `authorized_scopes` or `scope-not-authorized`.
- No new write call to GitHub (reads only, github.com only, the Spec 031 bounds); the `done` command writes only the state directory.
- No new dependency in phase 1; no raw-mode keyboard handling in phase 1.
- No new integration proof; the proofs of Specs 011 and 031 are unchanged.
- No guess about ownership beyond the existing derivation; no `you` label.

## Open decisions for the owner

None open. D1 to D16 are decided, see [Decided by the owner](#decided-by-the-owner-2026-10-26). A later question (for example
whether `github-merged-head` may become a proof in phase 2 after dogfood, D8) is raised when it arises.

## Measured data: the live workspace of 2026-10-26

All numbers come from `overview.json` (`stw tidy plan --json`, agent mode, offline, main `6d4c75ed`), queried with `jq`. Groups use the
family rule above. Not in the file and therefore not claimed: activity times, upstream state, sizes, the dirt of kept rows. The 122 safe
worktree directories are 121 under `/tmp` (55 `whr-*`, 51 `workharbor-*`, 7 under `scratchpad`, 4 `crewbook-*`, 4 other) and 1 in the
workspace. The owner's earlier estimate (63 under `/tmp`, 57 baseline) differs from the file; the file is used.

## Docs impact

`docs/tidy.md` (wizard section: janitor mode, the amendment list against Spec 041, risk ladder, steps, grammar, online default, retain
table), `docs/commands.md` (`--rows`, `--offline`; phase 2 `workspace done`, `tidy adopt`), `docs/json-contracts.md` and
`schemas/v2/tidy-overview.schema.json` with fixtures and freeze digest (additive fields; phase 2 `owner_source` value), the workspace v1
schema and `docs/workspaces.md` (phase 2 `done` block), `docs/agent-guide.md` (agents unchanged; phase 2 done step), `docs/progress.md`
(status lines only, set by the last pull request of a wave), `docs/roadmap.md`, `docs/feature-status.json` (status `planned`). Spec 041
gets a pointer to this spec; Specs 031 and 029 get a pointer to the online amendment; their text is not rewritten.

## Risks

- A one-key "take" tempts: mitigated by green-only defaults, the summary with the count of directories deleted for good, the typed words
  and the unchanged apply refusals.
- Levels can be mistaken for proof: the text says "risk" and the proof word is in the details; a level never makes a row choosable.
- Online by default adds latency and a network dependency to the human wizard: bounded budget, one fallback rule, progress on stderr.
- The family rule may group badly: the group is a view; `l` and `r` always show the rows and numbers still work.
- Grouped text changes the golden of Spec 041's text overview: it is unreleased; the JSON is additive only.
