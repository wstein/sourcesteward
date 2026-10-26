# Specification: `stw release` as a wizard

**Created**: 2026-10-26 | **Status**: Draft specification, documentation only (no code, schema, workflow or script changes). Needs an
independent review of the exact head and the owner's answers to the [open decisions](#open-decisions-for-the-owner) before tasks start; the
tag and dispatch steps additionally need an independent security review before any code ([T004](tasks.md)). It builds on
[Spec 026](../026-release-process/spec.md) (release process v2), [Spec 034](../034-bootstrap-release/spec.md) (bootstrap release) and
reuses the conventions of [Spec 044](../044-tidy-janitor-wizard/spec.md) (wizard, typed prompt on `/dev/tty`, mode detection as an
accident guard). Plan: [plan.md](plan.md); tasks: [tasks.md](tasks.md).

## One-page summary

**What the owner asked for.** Calling `stw release` should start a wizard, without extra flags, that walks through the release in an easy
sequence. A release has several steps and is called more than once; the tool must recognize where the release stands and continue the
wizard there, or start over. The owner also wants the **tag step** inside the wizard (a typed approval, never in agent mode) and wants
the SPIKE release, and then `stw` and `whr` v0.2.0, made with it.

**What this specification adds.**

1. **A flagless wizard.** `stw release` in human mode (both streams on terminals, no agent marker) starts it. The existing
   subcommands (`check`, `plan`, `verify`, `record`, `build`, `dispatch`) stay as expert commands, unchanged. New: a read-only
   `stw release status [--json]`. In agent mode `stw release` prints the status and refuses every write.
2. **A state machine** with fourteen states, each detected from evidence: git (tags, ancestry, the record file), GitHub (workflow runs,
   drafts, releases, attestations; the existing bounded reads) and a small local **journal** per release. Evidence beats the journal;
   the journal never grants anything. The wizard answers "continue here", "start over with the next version" (a tag is never
   re-created) or "nothing to do".
3. **Plain-language steps** with one screen each: choose the product, commit and version (every gate explained), see the plan and its
   effects, dispatch the build (one fixed POST, typed approval), wait with progress, download and verify, write the record, wait for
   the owner to merge it, **tag with a typed approval and push the tag**, wait for the publish workflow, verify the published release.
4. **The tag step.** Setting an annotated tag and pushing it is the same human act whether typed by hand or run by `stw`: the owner's
   explicit run is the approval (Spec 026 D4). The protection is the tag ruleset (Spec 026 D11), the refusal in agent mode, the typed word
   bound to the record digest, and fresh re-verification right before the tag.
5. **Gap fixes** the spike found, listed in [Gaps](#gaps-to-fix-so-the-wizard-can-work): a dispatchable build workflow contract that
   `check` cross-checks, one evidence format, a run name so a dispatched run can be found, the draft variable, and unhiding the group.

**Phases.** Phase 1 = what the SPIKE release needs, then `stw` and `whr` v0.2.0 (dispatch flow only). Phase 2 = polish. The tag-first
bootstrap flow (Spec 034, SheetMill, BundleDock today) is not driven by the wizard in phase 1.

**What does not change.** No build, tag or publication without the owner's typed words in an owner-run terminal; no auto-publish; no
standing approval; no new GitHub write beyond the one fixed dispatch POST; the tag push is the only other remote write; tags are never
moved or re-created; agent mode is read-only; `stw` never changes repository settings.

**Top open decisions** ([all](#open-decisions-for-the-owner)): dispatch flow only in phase 1 (recommended yes); no separate `stw release tag`
command (recommended: the tag step lives in the wizard only); the wizard downloads the draft assets (recommended yes); until the tag
ruleset can be read as `present`, an extra typed acknowledgement before every tag (recommended yes); the journal as specified.

## Decided by the owner

| When | Decision (owner's words are quoted, German) |
| --- | --- |
| 2026-10-26 | "Wenn ich STW-Release aufrufe, ja, dann würde ich mir wünschen, dass wieder ein Wizard, also ohne zusätzliche CLI-Flags, ... dass ein Wizard startet, der mit mir die Punkte durchgeht, in einem leicht verständlichen Ablauf und auch nach Möglichkeit, wenn das STW-Release aus mehreren Schritten besteht, also ich das nochmal aufrufen muss, dass dann mein Tool erkennt, wo wir jetzt sind und dann eben den Wizard entsprechend fortsetzt bzw. neu aufsetzt." Consequence: sections [State machine](#the-state-machine) and [Wizard](#the-wizard) |
| 2026-10-26 | The wizard includes the tag step with a typed approval, never in agent mode (reasoning accepted by the owner: a manual `git tag -a` plus `git push` is the same human act as a `stw` command; the owner's explicit run is the approval, Spec 026 D4; the protection is the tag ruleset D11 and the refusal in agent mode). Tags are never re-created: a failed release uses the next version |
| 2026-10-26 | The SPIKE release is done with the wizard, not by hand; then `stw` and `whr` v0.2.0 are released with it. The spike repository is `wstein/stw-release-spike` (public, throwaway); the owner merges its pull requests and runs the real approvals |
| 2026-10-26 | Schedule: first the spike with the existing flow (to find the gaps), then this specification and the wizard, then the v0.2.0 releases |

## Why: the spike found the flow is not walkable yet

Findings of the spike so far ([spike notes](https://github.com/wstein/stw-release-spike), the foreman's `spike-release-notes.md`, items 1 to 7)
and what the source says today:

| # | Finding | Proof in the source |
| --- | --- | --- |
| G1 | `stw` has no `.stw/release.toml` of its own; the contract exists only as examples | `ls .stw` is empty on main (`852467c1`); examples in `specs/026-release-process/examples/` |
| G2 | The contract's `ci.release_workflow` must name a dispatchable build workflow (inputs `mode`, `commit`, `version`, optional `product`, `tag`), but the only workflow of `stw` is the bootstrap tag workflow with one dispatch input, a dry-run label | `specs/026-release-process/contracts/workflow.md` (release-build.yml inputs), `.github/workflows/release.yml:9-16` |
| G3 | `check` does not look at the workflow: the path is only a declared build input that must be a valid repository path | `internal/release/validate.go:389` |
| G4 | Two build-script signatures (contract argv `OS ARCH VERSION OUTPUT`, bootstrap script `VERSION TARGET BINARY OUTDIR`) and two evidence formats (`.sha256` sidecars and `SHA256SUMS` plus `build-manifest.json`) | Spike notes item 3; `.github/scripts/release-package.sh`; `specs/026-release-process/contracts/build.md` |
| G5 | A bootstrap release has no manifest, so `record` and `verify` need `--signer-workflow-file` for it | `specs/026-release-process/contracts/bootstrap.md` (Record row) |
| G6 | The draft depends on the repository variable `STW_RELEASE_DRAFT`, which is not in the workflow | `.github/workflows/release.yml:301` |
| G7 | The `release` group is hidden from root help; `stw release` prints help | `internal/cli/release.go:35` (`Hidden: true`), Screen 0 below |
| G8 | A dispatch answers 204 without a run id, so a started run cannot be found from the response | `specs/026-release-process/contracts/dispatch.md` ("Disclosed effect") |
| G9 | The tag-ruleset advisory is `unknown` unless rulesets are known; the owner must check by hand | `internal/release/online.go:233` |
| G10 | There is no way to check a tag before it exists: `plan` takes a tag or a commit, and the publish gates need the tag | `specs/026-release-process/spec.md` (command table, `--for`) |

## Scope and phases

| Phase | Content |
| --- | --- |
| 1 | The wizard for the **dispatch flow** (Spec 026): shell, journal, states, `status`, every step, the gap fixes G1 to G10 that the wizard needs; run on the spike repository, then `stw` and `whr` v0.2.0 |
| 2 | Polish: expert `tag` command if the owner wants it (D2), more products per screen, release notes assistance, a progress bar for slow builds, wizard support for the tag-first flow if a project still needs it |

## The wizard

**Entry.** `stw release` with no subcommand and no flag:

| Situation | Behavior |
| --- | --- |
| Human mode (both streams terminals, `STW_AGENT` unset, no agent marker; `internal/tidywizard/owner/mode.go:80`) | The wizard starts. Mode detection is an **accident guard, not a boundary** (041 FR-006g, 044) |
| Agent mode, or no terminal | Prints the status (below) and one message: "release steps need a person at a terminal; nothing was changed". Every write is refused. Exit 0 for a complete status, 1 if the evidence could not be read |
| `stw release status [--json]` | The same read-only status in any mode, one `release-status` JSON document for tools |
| `stw release SUBCOMMAND ...` | Exactly as today; the wizard never wraps them |

The wizard uses the working directory's repository (ownership detection as `check` does). It is read-only until the first step that names an
effect, and every effect step is a screen of its own with a typed word (below). Answers to `y/N` questions and the typed words are read
from `/dev/tty` as in Spec 044 (D16); stdin is never an approval; no controlling terminal means nothing is changed. Progress lines go to
stderr like the tidy progress (`internal/cli/tidy_progress.go`); the screens go to stdout.

**Typed approvals.** One plain line read from `/dev/tty`, compared exactly:

| Effect | Typed line | Bound to |
| --- | --- | --- |
| dispatch one build | `build VERSION PLAN6` | the first 6 hex characters of the `plan_id` of the fresh dispatch plan |
| set and push the tag | `tag VERSION DIGEST6` | the first 6 hex characters of the SHA-256 of the record file at the tag commit |
| (until the ruleset reads `present`) acknowledge the ruleset | `ruleset ok` | once per release, recorded in the journal |

A bare `y`, another version, a stale prefix, input on stdin or no terminal refuses. Because the words contain the digest of what is approved,
an answer typed for an earlier state can never approve a later one.

## The state machine

A release is identified by (repository, product, version). Its state is **derived** on every call from evidence; the journal adds only
what the evidence cannot show. Order of trust: git and GitHub evidence first, the journal last, and a journal claim the evidence
contradicts is ignored with a notice.

| # | State | Evidence that shows it |
| --- | --- | --- |
| 0 | `nothing` | no tag (local or remote), no release or draft, no run for the version, no record on `main`, no journal |
| 1 | `ready` | `plan <C> --version V --online` has verdict `pass` for the chosen commit and version; nothing started |
| 2 | `dispatch-approved` | journal: the typed build line was accepted but no POST was recorded (interrupted between approval and effect) |
| 3 | `build-queued` | a run of `ci.release_workflow` with the release's run name exists with status queued, or the journal records the POST and no run is visible yet (up to 2 minutes) |
| 4 | `build-running` | such a run is in progress |
| 5 | `build-failed` | such a run concluded `failure`, `cancelled` or `timed_out`, and no draft holds the assets |
| 6 | `artifacts-built` | such a run concluded `success` and a draft release for the version holds `SHA256SUMS`, `build-manifest.json` and the files (read with a token that sees drafts) |
| 7 | `artifacts-verified` | the wizard downloaded the draft assets into its private directory, recomputed hashes, and `verify --artifacts` offline checks and the attestation read passed for the commit; the journal holds the digests |
| 8 | `record-written` | `docs/releases/V.json` exists in the working tree of a topic branch, equals `record --check`, and is not yet on `main` |
| 9 | `record-merged` | the record is in the tree of the remote-tracking `main` (the owner merged and fetched; `stw` never fetches) and its canonical bytes equal the written record |
| 10 | `tag-ready` | the prospective publish gates (G10) pass for `main`'s tip D: C is an ancestor of D, the diff C..D is allowlisted, the record at D is valid, the draft's assets still equal the record |
| 11 | `tag-pushed` | an annotated tag for the version exists on the remote and names D (or a descendant on `main`); a local tag not on the remote is shown as `tag-local-only` |
| 12 | `publishing` | the publish workflow run of the tag is queued or in progress |
| 13 | `published` | a non-draft release exists for the tag and `verify <tag> --published --online` passes |
| 14 | `abandoned` or `failed` | the journal marks abandonment, or a publish run failed after the tag exists, or a tag exists and no version path is left; terminal: only the next version continues |

State 1 is not durable: it is recomputed. States 2 and 8 exist only because of the journal or the working tree. Each state names the
step the wizard resumes at.

### Decisions the wizard takes

| Detected | The wizard offers |
| --- | --- |
| 0, 1 | Start at the product, commit and version screen |
| 2, 3, 4 | Continue: wait for the build (never dispatch again: a guard refuses when a run with this run name is queued or in progress, `build-in-flight`) |
| 5 | Show the failure (run URL, failed job), then: **use the next version** (default), or, only if no tag and no draft exists for this version, retry the same version after the owner deleted the failed draft by hand; the wizard never deletes anything |
| 6, 7 | Continue at download and verify, then record |
| 8 | Show the record, its digest and the hash comparison against the draft; tell the owner to commit, push and merge it (the wizard never commits or pushes, D3) |
| 9, 10 | Continue at the tag screen |
| 11 (`tag-local-only`) | Offer to push the existing local tag after the same fresh checks and a new typed approval |
| 11, 12 | Wait for the publish run |
| 13 | "Nothing to do." with the release URL and the verification result |
| 14 | "This version cannot continue. A tag is never re-created." and offer the next version, starting at state 0 |
| a tag or release of a **higher** version exists | Say so and offer the next greater version |
| the journal disagrees with the evidence | Use the evidence, print one notice, and ignore the journal entry |

**Restart** always means a new version; it never reuses a tag. The previous release's journal stays as history.

## The journal

A small append-only file per release, so the wizard knows the facts the evidence cannot show (that the owner approved, that a POST was sent,
where the verified artifacts are).

- **Location:** `<state base>/release/<repo key>/<product or main>/<version>.jsonl`, where the state base is the `stw` state directory of Spec 041
  (`${XDG_STATE_HOME:-~/.local/state}/stw`, mode 0700, user-owned, not a symlink, outside every repository; the same checks as
  `internal/tidywizard/derive/state.go:71-112`); the repo key is the SHA-256 of the resolved `host/owner/name`.
- **Content:** one JSON object per line: `seq`, `at` (UTC), `event` (closed set: `planned`, `approved-build`, `dispatched`, `artifacts-verified`,
  `record-written`, `ruleset-acknowledged`, `approved-tag`, `tag-created-local`, `tag-pushed`, `abandoned`, `restarted-as`), the facts of that event (commit,
  version, `plan_id`, digests, the artifact directory path, the run URL) and `prev` (the SHA-256 of the previous line). The names, never the values, of
  the agent markers of the mode evidence are recorded. **No token, no secret, no approval word.**
- **Bounds:** at most 64 lines and 64 KiB per file, 256 files per repository; a longer file is `journal-limit` and the wizard continues from
  evidence only.
- **Integrity:** the hash chain detects accidental damage and naive edits, not a malicious same-user process (which can rewrite the whole chain);
  therefore the journal grants nothing: every effect step re-derives its preconditions from evidence and needs its typed word. A broken
  chain is `journal-corrupt`: ignored with a notice.
- **Writes:** only the wizard shell writes it, after the effect it records (a crash between effect and journal is recovered from evidence,
  see below). `stw release` subcommands never write it, so `dispatch --apply` stays "nothing local changes" (Spec 026 dispatch contract).

## The steps (screens)

Every screen says what happens, which gate or fact it rests on in plain words, what it will change, and what undoes it. Gate codes appear
only in details (`d`) and `--json`. Screens 1 to 5 are **mockups** (see [Screens](#screens)).

| Step | State before | Effect | Existing command used |
| --- | --- | --- | --- |
| 1 Product | 0 | none (reads contracts) | `check` |
| 2 Commit and version | 0 | none (reads) | `plan C --version V --online` |
| 3 Plan and approval, then dispatch | 1 | **remote write**: one `POST .../dispatches` | `dispatch` plan, then `dispatch --apply PLAN_ID` |
| 4 Wait for the build | 3, 4 | none (reads runs) | hosting reads |
| 5 Download and verify | 6 | local write: the assets into the private directory | `verify --artifacts DIR --online` |
| 6 Record | 7 | local write: `docs/releases/V.json` on a topic branch | `record --write` |
| 7 Wait for the merge | 8 | none | `plan D --for publish` (G10) |
| 8 Tag | 10 | **remote write**: create the annotated tag, push `refs/tags/V` | new (the tag step) |
| 9 Wait for the publish | 11, 12 | none | hosting reads |
| 10 Verify the release | 13 | none | `verify TAG --published --online` |

### Step details

**1 Product.** One contract: used. Several (`.stw/release.<product>.toml`): a numbered list with each product's last release and tag grammar. A
repository without a valid contract stops with the `check` findings in plain words.

**2 Commit and version.** The wizard proposes the tip of `main` that CI ran on and the next version by the scheme (greater than every existing tag
and release of the product, drafts included when the token sees them); both can be typed. Then the gates, each as one line of
plain language with its state (`ok`, `blocked`, `not checked`), for example "CI has not run on this commit (ci-not-green)". The gates are the rows
of the Spec 026 gate table; a test requires a sentence for every code (SC-009). The wizard offers to change the commit or version when a gate
blocks. It never continues past a `fail`.

**3 Plan and approval.** The wizard prints the exact request (workflow file, ref `main`, inputs), the declared network effect of fetch builds, and
"This starts one CI build. It creates a draft release and attestations. It tags and publishes nothing. A started build can be canceled in
the Actions UI; a draft can be deleted by hand." Then the typed line `build VERSION PLAN6`. On a match the wizard recomputes the plan from
fresh state (`dispatch --apply PLAN_ID` does that and refuses `plan-stale` on any change), sends the one POST, writes the journal line
`dispatched`, and moves on. A `main-ref-stale` refusal says "fetch main first" and waits.

**4 Wait.** Reads the runs of `ci.release_workflow` that carry the release's run name (G8) every 10 seconds with a bounded budget (at most 360 reads
per wait, a ten-minute pause between waits on `rate limit`), shows the job names and states, and offers `q` to leave; the run continues without
the wizard and the next `stw release` resumes. A failure goes to state 5.

**5 Download and verify.** Downloads the draft's assets by id into the private directory (`<state base>/release/.../artifacts/V/`; bounded count and
bytes by the Spec 026 limits table), recomputes sizes and hashes, runs the attestation verification, and shows "all N files match their
checksums and are attested by the build workflow of this repository". A mismatch stops with the file name.

**6 Record.** Requires a clean checkout on a topic branch (not `main`): writes `docs/releases/V.json` with the existing `record --write` (refuses
to overwrite a different file) and shows the record's digest and the table "file, size, hash: record against draft". It then prints the
commit, push and pull request commands for the owner and **does not run them** (D3). The wizard states what the pull request must contain: the
record and, optionally, release notes; nothing else (the allowlist, Spec 026 FR-006).

**7 Wait for the merge.** After the owner merged and fetched, the wizard re-reads the remote-tracking `main` (never fetches) and evaluates the
prospective publish gates of the tip D (G10). It explains each blocker, for example "`main` moved on with a code change since the build
(diff-not-allowlisted); release the next version from a newer commit".

**8 Tag.** The most careful screen. Before it asks anything the wizard re-verifies, from fresh evidence: the tag does not exist locally or
remotely; D is the tip of the remote-tracking `main` and contains the record whose digest was shown; the draft's assets still equal the
record (a fresh list); the publish gates pass; the checkout the tag is created in is irrelevant (the tag object is created for the
commit id D). It shows the tag message built from the record (below), the effect ("an annotated tag `V` on D is created and pushed to `origin`.
That starts the publish workflow, which verifies and publishes the draft. It cannot be undone: a tag is never re-created, a failed release
uses the next version"), the ruleset advisory, and asks the typed line `tag VERSION DIGEST6`. Then it creates the annotated tag object for the
commit id D with the owner's configured identity as tagger, pushes `refs/tags/V` (a plain push, never forced, atomic with respect to the
single ref), verifies the remote tag names D, and writes the journal lines `tag-created-local` and `tag-pushed`.

**Tag message** (data, never trusted for the version, Spec 026 edge case):

```text
stw release V

product: NAME
commit: D
built-from: C
record: docs/releases/V.json sha256:<64 hex>
```

**9 Wait for the publish** and **10 Verify the release** as above; the end screen shows the release URL and "Consumer activation is a separate step
with its own authorization" (Spec 026 quickstart step 7).

### Idempotency and interruption

Every step is safe to run again: the wizard derives the state first.

| Interrupted at | What exists | What the next call does |
| --- | --- | --- |
| before the typed word | nothing | start the step again |
| after the word, before the POST | journal `approved-build` only | recompute the plan, ask for the word again (the old one never applies) |
| after the POST, before the journal line | a run with the run name | the evidence shows `build-queued` or `build-running`; the journal is behind and is repaired by a line `dispatched (recovered)` |
| during the build | a running run | wait |
| during the download | a partial directory | the directory is removed and the download repeated |
| after `record --write`, before the pull request | the file on a topic branch | state 8; shows the commands again |
| after the typed tag word, before the local tag | journal `approved-tag` | fresh checks, new typed word |
| local tag created, push failed or not made | `tag-local-only` | fresh checks, new typed word, then push; if the remote has a tag of that name at another commit: refuse, the version is abandoned |
| tag pushed, journal line missing | the remote tag | evidence shows `tag-pushed`; the journal is repaired |
| publish run failed | a tag, no published release | state 14: next version only |

## What is new and what is unchanged

| Item | Status |
| --- | --- |
| `check`, `plan`, `verify`, `record`, `build`, `dispatch` | **unchanged** in flags, behavior, exit codes and JSON, except the additive items listed in [Gaps](#gaps-to-fix-so-the-wizard-can-work) |
| `stw release` bare | **changed**: help today (`internal/cli/release.go:35-37`), the wizard in human mode, status in agent mode |
| `stw release status [--json]` | new, read-only, any mode |
| the journal, the wizard shell | new (`internal/releasewizard`, importing `internal/release`; the CLI imports no `os` or network package, as the tidy wizard) |
| the tag step | new: the only place `stw` creates a tag and the only remote write besides the dispatch POST |
| the download of draft assets | new bounded read (D4) |

## Gaps to fix so the wizard can work

Each is a change to a contract or workflow with its own test; the wizard depends on them.

| # | Fix | Where |
| --- | --- | --- |
| G1 | Add `.stw/release.toml` to `stw` and run `check` in `make check` (Spec 026 T043 intended this) | repository root, `Makefile` |
| G2, G3 | The build workflow contract: `ci.release_workflow` names a `workflow_dispatch` workflow with inputs `mode`, `commit`, `version` and, for multi-product, `product` and `tag`. `check` gains a **text-rule cross-check** of the workflow file at the commit (the same bounded line scan `internal/releaseworkflow` uses, no YAML dependency): findings `release-workflow-not-dispatchable` and `release-workflow-missing-input`. Additive findings, exit 3 as the others | `contracts/check.md`, `internal/release` |
| G4 | One evidence format: `SHA256SUMS` plus `build-manifest.json`; the bootstrap `.sha256` sidecars stop being a second format for new projects. One package-script signature: the contract argv; the bootstrap scripts become wrappers or are replaced | `contracts/build.md`, workflow templates |
| G5 | `record` and `verify` derive the signer workflow from `ci.release_workflow` and accept `--signer-workflow-file` only as an override, so the wizard never needs the flag | `contracts/record-command.md`, `verify.md` |
| G6 | The draft state is part of the workflow contract: the build workflow always creates a draft (the repository variable `STW_RELEASE_DRAFT` is removed from the dispatch flow); the tag-first bootstrap keeps it | `contracts/workflow.md`, `.github/workflows/release.yml:301` |
| G7 | Unhide the `release` group when the owner accepts the spike (D11); document `status` | `internal/cli/release.go:35`, `docs/release.md`, `docs/agent-guide.md:227` |
| G8 | The build workflow sets `run-name: release-build PRODUCT VERSION COMMIT` so a dispatched run can be found; the wizard also refuses a second dispatch while one is in flight (`build-in-flight`) | `contracts/workflow.md`, dispatch plan |
| G9 | The tag-ruleset advisory stays advisory; the wizard adds the typed acknowledgement `ruleset ok` while the advisory is not `present` (D12) | wizard, `release-plan.md` |
| G10 | `plan D --for publish --version V` evaluates the publish gates for a tag that does not exist yet (the annotation gate is `not-applicable`) | `contracts/release-plan.md` |

## Amendments to Spec 026, 034 and the docs

Where the listed text says otherwise, the new wording applies after this specification is accepted; the old text is not rewritten in place
except by the implementing pull requests.

| Text | Today | New wording |
| --- | --- | --- |
| Spec 026 D4 (`spec.md:17`) | "Every tag and release needs the owner's explicit approval each time." | Unchanged in substance. Added: the owner's typed approval in the wizard's tag step, or a manual `git tag -a` and push, is that approval; both are the same human act |
| Spec 026 FR-012 (`spec.md:156`) | "the owner's tag push ... need explicit authorization in their task context" | "the owner's tag push, by hand or by the wizard's tag step (typed approval on `/dev/tty`)" |
| Spec 026 quickstart step 3 and 5 (`quickstart.md`, "The owner's release") | the owner runs `dispatch`, downloads the assets, runs `git tag -a` and `git push` | the owner may instead run `stw release` and take the steps; the manual route stays valid |
| Spec 026 commands table (`spec.md`, "Commands (normative)") | six subcommands | adds bare `stw release` (wizard or status) and `stw release status` |
| Spec 026 dispatch contract, "Effects" (`contracts/dispatch.md`) | "Nothing local changes; no local lock" | Still true for the command; the wizard shell around it writes the journal |
| Spec 026 dispatch contract, "Compatibility" ("first write to the hosting transport") | one fixed POST is the only write | the tag push is a second remote write, by the git transport and only from the wizard's tag step; no new hosting write |
| Spec 026 workflow contract (`contracts/workflow.md`, `release-build.yml`) | inputs listed, no run name | adds `run-name` (G8) and the dispatchable-workflow cross-check (G2) |
| Spec 026 `check` contract and findings table | findings of the contract only | adds the two workflow findings of G2 |
| Spec 026 release-plan contract | `plan <commit\|tag>`; publish gates need a tag | adds `--for publish` for a commit (G10) |
| Spec 026 bootstrap contract, "Authority" and Spec 034 "Authority" ("Agents never tag, run the workflow or publish") | agents never tag | Unchanged; the wizard refuses in agent mode |
| Constitution, Development Workflow (5) ("the owner's explicit approval of each tag and release is required in the task context of that release") | as written | Unchanged; the typed word in an owner-run terminal is that approval, and the constitution's "Network calls ... must disclose" is met by the screen of each effect |
| Workspace and `sourcesteward/AGENTS.md` ("Agents never merge, tag or release") | as written | Unchanged |
| `docs/release.md` (hidden group, "current source and planned process") and `docs/agent-guide.md:227` | the group is hidden, commands listed | updated with the wizard, `status`, the journal and the unhidden group (G7) |

## Security analysis

**Who may do what.**

| Actor | Can | Cannot (by `stw`) |
| --- | --- | --- |
| The owner at a terminal (human mode) | everything the wizard offers, each effect behind its typed line | skip a gate, force a tag, re-create a tag |
| An agent, same OS user, agent mode | read status, run the read-only subcommands, `record --write`, `dispatch` plan | take any wizard effect step: the wizard refuses; `dispatch --apply` stays as Spec 026 states (authorization in the caller's task context, owner-only in the workflow) |
| An agent that drives a pty or sets environment variables | what a human at that terminal could do | nothing stops it: mode detection is an accident guard (Spec 041 FR-006g, 044); the word is typed on `/dev/tty`, which an agent can also drive |
| Any GitHub user | nothing: dispatch and publish workflows require the repository owner as actor, the tag ruleset restricts tag creation | |

**Honest limit.** An agent with the owner's `gh` credentials and git access can run `git tag -a` and `git push` by hand, with or without
`stw`; `stw` neither adds nor removes that power. The real protections are: the tag ruleset (owner-only creation; D11), the repository rules
that agents never tag (AGENTS.md), the wizard's refusal in agent mode, and the verification the publish workflow runs on every tag (annotation,
ancestry, allowlisted diff, record, hashes, attestations). The owner can narrow the agents' power by giving them a credential without
`contents: write` on tags and without `actions: write` (a token decision, outside `stw`).

**Token scopes.** Reads need `contents: read` and `actions: read` (drafts need a token that sees them); dispatch needs `actions: write`; the tag push uses
the owner's git credential for `origin` (`contents: write`). The wizard reads the token only through the existing `gh auth token` path of the
hosting transport and never prints, stores or journals it.

**TOCTOU between plan and tag.** The wizard re-verifies from fresh reads immediately before the typed word and again before creating the tag object
for the **commit id** D (never a branch name), so a main that moved, a draft asset that was replaced, a tag that appeared, or a record that
changed refuses. The remaining window is the seconds between the second check and the push; the publish workflow re-verifies everything at the tag
(Spec 026 workflow contract) and the ruleset decides who may push at all.

**Replay of an old approval.** There is no stored approval. The typed line contains the version and a digest prefix of the exact object approved; an old
answer, a screenshot, or text on stdin cannot match a new state.

**Journal tampering.** See [The journal](#the-journal): it grants nothing; at worst it shows a wrong "where we are", which evidence overrides, or
makes the wizard ask again.

**Never auto-publish.** The wizard never publishes, edits or deletes a release or a draft; publication is the publish workflow's job after the owner's
tag. It never changes repository settings, never pushes a branch, never merges, never opens a pull request.

**New attack surface.** One new local write path (the journal and the private asset directory: 0700, bounded), one new read (draft asset download), one
new remote write (tag push). The tag step and the dispatch step get an independent security review before code (T004).

## Screens

### Screen 0: today (real output)

`stw release` and `stw release check` of the installed `stw` (main `6d4c75ed`) on a throwaway repository with the example Go CLI contract. Bare
`stw release` prints help; the group is not in root help.

```text
$ stw release
Release process commands. check lints the project release contract (.stw/release.toml) without writes, network or project commands.

Usage:
  stw release [flags]
  stw release [command]

Available Commands:
  build       Execute declared release commands in CI or an explicit local verification
  check       Lint the project release contract without writes
  dispatch    Plan or explicitly apply one CI workflow dispatch
  plan        Explain release gates without execution
  record      Create a canonical release record from existing files
  verify      Verify a release without building or publishing
$ stw release check
SourceSteward release check: /fx/repo (git)
effects: read-only, no network, no project command
contract: .stw/release.toml (schema 1, scheme semver, 3 targets, 1 build)
findings: none
```

The screens 1 to 5 are **MOCKUPS**: the program cannot produce them yet. Product, repository and numbers are those of the spike target
(`wstein/stw-release-spike`); hashes, ids and times are invented.

### Screen 1 (mockup): where are we

```text
stw release   wstein/stw-release-spike   product: stw-spike   (you are the owner, terminal)

Last release: v0.1.0 published 2026-10-26
This run:     nothing is in progress for the next version.

Next version  v0.2.0   (greater than v0.1.0)
Commit        9be4a1c   tip of main, CI is green on it

What would you like to do?
  [1] check the gates for v0.2.0 at 9be4a1c, then build
  [2] choose another commit or version
  [q] quit
```

### Screen 2 (mockup): the gates in plain words

```text
Can v0.2.0 be built from 9be4a1c?
  ok        the commit is on main
  ok        CI is green on this exact commit
  ok        v0.2.0 is greater than every existing version
  ok        no tag, release or draft exists for v0.2.0
  ok        the contract is valid
  not seen  the tag ruleset (GitHub does not let stw read it): you must have set it, see docs/release.md
Result: all checks that can be made here pass.
  [c] continue   [d] details and codes   [b] back   [q] quit
```

### Screen 3 (mockup): approve the build

```text
This starts ONE build on GitHub: workflow release-build.yml on main, commit 9be4a1c, version v0.2.0.
It creates a draft release and attestations. It does not tag and does not publish.
You can cancel the run in the Actions page; a draft can be deleted by hand.
plan id 3f9c2a41...
Type the line below to approve (read from the terminal, nothing else is accepted):
  build v0.2.0 3f9c2a
>
```

### Screen 4 (mockup): coming back later

```text
stw release   wstein/stw-release-spike   product: stw-spike

v0.2.0 is in progress.
  built     the build finished, 4 files and the checksums are in the draft
  verified  you checked them on 2026-10-26 14:02 (4 of 4 match, attested)
  record    docs/releases/v0.2.0.json is on main (merged)
  tag       not set yet

Next: tag v0.2.0 on 7c11d2e (main) and push it.   [c] continue   [q] quit
```

### Screen 5 (mockup): approve the tag

```text
Tag v0.2.0 on 7c11d2e (tip of main, contains the record, checked just now):
  record  docs/releases/v0.2.0.json  sha256 b7d41f...
  built from 9be4a1c; the draft's 4 files still match the record
  ruleset  not readable by stw: type "ruleset ok" first if you have set it and checked it

This creates the annotated tag v0.2.0 and pushes it to origin. The publish workflow then verifies and
publishes the draft. It cannot be undone: a tag is never re-created; a failed release uses the next version.
Type the line below to approve:
  tag v0.2.0 b7d41f
>
```

### Screen 6 (mockup): agent mode

```text
$ stw release          (agent mode)
v0.2.0 is in progress: build finished, record merged, tag not set.
Release steps need a person at a terminal; nothing was changed.
```

## Requirements

### Functional (phase 1)

- **FR-001** `stw release` with no subcommand and no flag starts the wizard in human mode; in agent mode or without a terminal it prints the
  status and a one-line message and changes nothing. `stw release status [--json]` is read-only in every mode.
- **FR-002** The state is derived from evidence (git, GitHub reads, the working tree); the journal adds only approvals, the sent POST and the
  artifact directory; a contradiction is resolved for the evidence with a notice.
- **FR-003** The states and decisions of the tables above, including "continue", "next version", "nothing to do"; a tag is never re-created.
- **FR-004** Every effect step (dispatch, tag) requires its typed line read from `/dev/tty`, bound to the digest of what is approved; stdin
  or no terminal refuses.
- **FR-005** Dispatch uses `dispatch` plan and `--apply PLAN_ID` unchanged and sends exactly one POST; a run with the release's run name that is
  queued or in progress refuses a second dispatch.
- **FR-006** The tag step re-verifies from fresh reads before asking and before creating the tag, creates the annotated tag for the commit id D with the
  message above and the owner's configured tagger identity, pushes only `refs/tags/V` without force, and verifies the remote tag.
- **FR-007** The wizard never commits, pushes a branch, merges, opens a pull request, publishes, edits or deletes a release or draft, or changes a setting.
- **FR-008** The journal location, bounds, closed event set and hash chain as specified; no secret is ever written; a corrupt or over-limit
  journal degrades to evidence only.
- **FR-009** All repository and GitHub text is escaped (`esc`), bounded, and treated as data; hostile tag, run, release and file names are shown escaped.
- **FR-010** The existing subcommands keep their flags, exit codes and JSON; additions are the findings and flags of G2, G5, G8, G10 only.
- **FR-011** Progress goes to stderr, screens to stdout; `--json` of `status` is one schema-valid document.
- **FR-012** The wizard supports the dispatch flow; a repository whose contract names a tag-triggered workflow gets the message
  `flow-unsupported` with the manual route of Spec 034 (D1).

### Success criteria

Each is testable with temporary repositories and a fake hosting transport; the end-to-end target is the spike repository, where the owner runs the real approvals.

- **SC-001** State table test: one fixture per state 0 to 14 (git, a fake hosting double and a journal) yields the stated state and next step; evidence overrides a contradicting journal.
- **SC-002** Interruption table test: each row of the interruption table is replayed (kill at that point) and the next call reaches the stated state with no second effect.
- **SC-003** Agent mode and no terminal: no effect is possible (the hosting double fails on any write method, the git push is refused); status prints; exit 0 or 1 as stated.
- **SC-004** Typed approvals: a bare `y`, input on stdin, a wrong version, a stale digest prefix, an old answer after the state changed, and no terminal each refuse and change nothing; the accepted word needs the exact current prefix.
- **SC-005** Dispatch: exactly one POST to the fixed endpoint; a second wizard run during an in-flight build sends none (`build-in-flight`); `plan-stale` after a changed main refuses.
- **SC-006** Tag: the tag object is annotated, names the commit id D, carries the message, is pushed as one non-forced ref; a main that moved, a replaced draft asset, an existing remote tag, a missing record or a changed record refuses before the word and again after it; a local-only tag resumes with a new word.
- **SC-007** Journal: bounds, hash chain, corruption, over-limit, no secret (a sentinel token in the environment appears in no journal line, stdout or stderr).
- **SC-008** Read-only claims: a before and after snapshot of every repository, worktree and ref is equal for every step except the three effect steps (record file, dispatch, tag).
- **SC-009** Every gate code, state and finding has a plain-language sentence; a new code without one fails.
- **SC-010** Existing `check`, `plan`, `verify`, `record`, `build`, `dispatch` tests and goldens pass unchanged except the additions of G2, G5, G8, G10.
- **SC-011** Hostile names (ESC, CR, NUL, bidi) in tags, runs, releases and files reach no terminal byte unescaped.
- **SC-012** End to end on the spike repository: states 0 to 13 are walked once with the real approvals; the same run is interrupted after the dispatch and after the tag and resumed; a deliberately failed build moves to the next version without re-creating a tag. Evidence goes into the pull request body.

## Edge cases

| Case | Behavior |
| --- | --- |
| Several products | numbered list; the tag selects the product when resuming |
| A higher version exists | the wizard says so and offers the next greater version |
| Drafts invisible to the token | the draft part of the checks is `not seen`; the wizard says why and does not guess |
| Rate limit or GitHub error | the wizard stops the waiting, keeps the state, and says "try again later"; nothing is retried automatically except waiting reads |
| The build workflow was run from another machine | evidence shows the run; the journal is missing; the wizard continues from evidence |
| Two wizards at once | the second sees `build-in-flight` or the tag; a lock file in the journal directory is not used (no local lock); evidence decides |
| Shallow clone, missing remote-tracking `main` | `plan` refuses as today (`main-ref-stale`, ancestry unavailable); the wizard says "fetch main first" |
| Jujutsu workspace | commit-level steps work as `plan` and `record` do; `verify` of a tag and the tag step are `unsupported-backend` (Spec 026 capability table) |
| `core.hooksPath`, tag signing configuration | the tag object is created by `stw` without running hooks or signing (D5) |
| The tag exists remotely at another commit | refuse; the version is abandoned |
| Offline, no authentication | the wizard shows what can be shown offline and stops before the first online step |
| Narrow terminal | screens wrap at the terminal width, the typed line is never truncated |

## Test plan: the spike repository as the real target

The spike `wstein/stw-release-spike` is a throwaway public repository the owner created for this. The wizard is exercised there in three rounds, each
with the owner running the real approvals: (1) after the gap fixes G1 to G10 land in the spike's copy of the contract and workflows: walk states
0 to 13 for `v0.1.0` of the spike, interrupting after dispatch and after the tag; (2) a deliberately failing build (a script that exits 1) to reach
state 5 and the next version; (3) a second product to test the product list. Settings the test needs (tag ruleset for the owner, environment `release`,
Actions permissions, branch protection) are set only in the spike by the agent as listed in the spike's settings file; they are never set in `stw`
or `whr`. Unit and component tests use temporary repositories and the hosting double; nothing real is touched.

## Non-goals

- No auto-publish, no deletion or editing of releases or drafts, no setting changes, no pull request creation, no commit, no branch push, no merge.
- No tag signing in phase 1 (D5), no tag moves or re-creation, no force push.
- No standing approval, no stored approval, no schedule.
- No wizard for the tag-first bootstrap flow in phase 1 (D1); no package-manager channels; no consumer activation.
- No new dependency; no YAML parser (G2 uses text rules).
- No change to the owner's repository rules: agents never tag.

## Open decisions for the owner

| # | Question | Recommendation |
| --- | --- | --- |
| D1 | Phase 1 drives only the dispatch flow of Spec 026, not the tag-first bootstrap flow of Spec 034 | Yes: `stw` v0.2.0 is the first self-hosted release (Spec 026 D7, bootstrap contract "Self-hosting switch"); the other repositories adopt the dispatch flow with their own contracts |
| D2 | A separate expert command `stw release tag` | No: the tag step lives only in the wizard, so there is one approval surface to review; a manual `git tag -a` stays possible |
| D3 | The wizard writes the record file on a topic branch but never commits, pushes or opens the pull request | Yes: those are ordinary owner or agent actions under their own rules and keep the wizard's write set small |
| D4 | The wizard downloads the draft's assets itself (a new bounded read of the release assets) | Yes: otherwise the owner downloads by hand and the verified-artifacts state cannot be shown |
| D5 | Tag signing | Not in phase 1: an unsigned annotated tag; the ruleset and the verification are the protection; decide again when the owner has a signing key policy |
| D6 | May the wizard publish a draft in the tag-first flow or a failed-retry flow | No: never; publication is the publish workflow after the tag |
| D7 | The `run-name` convention and the `build-in-flight` guard (G8) | Yes |
| D8 | `check` cross-checks the workflow by bounded text rules, no YAML dependency (G2) | Yes |
| D9 | The journal as specified (location, 64 lines, hash chain, no secrets) | Yes |
| D10 | One typed line per effect (`build ...`, `tag ...`) | Yes; the tag push needs no extra word because the tag step is one effect |
| D11 | Unhide the `release` group in root help | Yes, in v0.2.0, after the spike round 1 |
| D12 | While the ruleset advisory is not `present`, require the extra typed `ruleset ok` before every tag | Yes: Spec 026 D11 makes the ruleset a precondition and `stw` cannot read it (`internal/release/online.go:233`) |
| D13 | What `abandoned` means for the version: never reuse, even when no tag exists | Yes when a tag or release exists; allow reuse only when neither a tag nor a draft exists (state 5) |

## Docs impact

`docs/release.md` (wizard first, states, journal, steps, the manual route kept), `docs/commands.md` (`stw release`, `status`), `docs/json-contracts.md` and
a `release-status` schema, `docs/agent-guide.md` (agents: status only), `docs/workspaces.md` (state base), the contracts named in the amendment
table, `docs/progress.md` and `docs/roadmap.md` (status lines only, by the last pull request of a wave), `docs/feature-status.json`
(status `planned`). Spec 026 and 034 get a pointer to this specification.

## Risks

- The tag step is an irreversible remote write: mitigated by fresh re-verification, the typed digest-bound line, the ruleset, no force, the independent security review and the spike rehearsal.
- The wizard could be mistaken for approval-free automation: it is human-mode only, every effect needs its word, and nothing is stored as approval.
- The journal could mislead: mitigated by "evidence beats journal".
- Dependence on unverified GitHub behavior (draft visibility, run discovery, attestation action): the spike rounds are the measurement, and unmeasured shapes fail closed as Spec 026 states.
