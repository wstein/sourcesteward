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
the SPIKE release made with it.

**What this specification adds.**

1. **A flagless wizard.** `stw release` in human mode (both streams on terminals, no agent marker) starts it. The existing
   subcommands (`check`, `plan`, `verify`, `record`, `build`, `dispatch`) stay as expert commands, unchanged. New: a read-only
   `stw release status [--json]`. In agent mode `stw release` prints the status and the **wizard** refuses every write (the expert
   commands are a separate question, see [Security analysis](#security-analysis) and D15).
2. **A state machine** (states 0 to 14 and a stop state `conflict`) derived from evidence: git (tags, ancestry, the record file), GitHub
   (workflow runs, drafts, releases, attestations; the existing bounded reads) and a small local **journal**. The journal may only
   **restrict**: it can block repeating an effect, never advance a step. Every combination of evidence maps to a state or to `conflict`,
   which stops and offers no effect. A tag is never re-created.
3. **Plain-language steps** with one screen each: product, commit and version (every gate explained), plan and effects, dispatch the build
   (one fixed POST, typed approval), wait, download and verify, write the record, wait for the owner's merge, **tag with a typed approval and
   push the tag**, wait for the publish workflow, verify the published release.
4. **The tag step.** Setting an annotated tag and pushing it is the same human act whether typed by hand or run by `stw`: the owner's explicit
   run is the approval (Spec 026 D4). The protection is the typed word bound to the exact commit and record, fresh re-verification right before the
   tag, the refusal in agent mode, and the tag ruleset **against other accounts** (an agent on the owner's own account is not stopped by it).
5. **Gap fixes** and a [coverage table](#spike-coverage-every-gap-a-task-and-a-criterion-or-a-deferral) for every spike finding.

**Phases (honest).** Phase **1a** = what the SPIKE needs: the wizard against the spike repository, where the three Spec 026 workflows are written
for the spike. Phase **1b** = the dependencies for real projects: Spec 026 WI8 (the build and publish workflows, T035 to T039) and the
self-hosting switch (T047) for `stw`, and a contract plus workflows for `whr`. Until 1b is done, the `stw` and `whr` v0.2.0 releases can use
only today's tag-first flow with a hand-set tag; whether to wait for the wizard is [decision D14](#open-decisions-for-the-owner). Phase 2 = polish.

**What does not change.** The wizard never builds, tags or publishes without the owner's typed words in an owner-run terminal; no
auto-publish; no standing approval; no new GitHub write beyond the one fixed dispatch POST; the tag push is the only other remote write; tags are
never moved or re-created; `stw` never changes repository settings.

**Open decisions** ([all](#open-decisions-for-the-owner)), the two that decide the schedule: **D14 the v0.2.0 release path** (hand-set tag now, or wait
for the wizard and its dependencies) and **D15 whether expert `dispatch --apply` refuses in agent mode**.

## Decided by the owner

| When | Decision (owner's words are quoted, German) |
| --- | --- |
| 2026-10-26 | "Wenn ich STW-Release aufrufe, ja, dann würde ich mir wünschen, dass wieder ein Wizard, also ohne zusätzliche CLI-Flags, ... dass ein Wizard startet, der mit mir die Punkte durchgeht, in einem leicht verständlichen Ablauf und auch nach Möglichkeit, wenn das STW-Release aus mehreren Schritten besteht, also ich das nochmal aufrufen muss, dass dann mein Tool erkennt, wo wir jetzt sind und dann eben den Wizard entsprechend fortsetzt bzw. neu aufsetzt." Consequence: [State machine](#the-state-machine) and [Wizard](#the-wizard) |
| 2026-10-26 | The wizard includes the tag step with a typed approval, never in agent mode (reasoning accepted by the owner: a manual `git tag -a` plus `git push` is the same human act as a `stw` command; the owner's explicit run is the approval, Spec 026 D4; the protection is the tag ruleset D11 and the refusal in agent mode). Tags are never re-created: a failed release uses the next version |
| 2026-10-26 | The SPIKE release is done with the wizard, not by hand. The spike repository is `wstein/stw-release-spike` (public, throwaway); the owner merges its pull requests and runs the real approvals |
| 2026-10-26 | Schedule as decided: first the spike with the existing flow (to find the gaps), then this specification and the wizard, then the v0.2.0 releases "with the wizard". This specification finds that last step depends on work that does not exist yet (phase 1b) and asks the owner again (D14) |
| 2026-10-26, earlier in the same thread (owner-decision log) | The owner accepted the idea of a `stw release tag` command (an owner-run command is the approval; the tag ruleset D11 and the agent-mode refusal are the protection). See D2 |

## Why: what the spike found

Spike findings (the foreman's `spike-release-notes.md`, items 1 to 14 and S1 to S11) that concern the release flow, with the proof in the source. The
[coverage table](#spike-coverage-every-gap-a-task-and-a-criterion-or-a-deferral) maps every item, the stacking ones included.

| # | Finding | Proof in the source |
| --- | --- | --- |
| G1 | `stw` has no `.stw/release.toml` of its own; the contract exists only as examples | `ls .stw` is empty on main (`852467c1`); examples in `specs/026-release-process/examples/` |
| G2 | The contract's `ci.release_workflow` must name a dispatchable build workflow (inputs `mode`, `commit`, `version`, optional `product`, `tag`), but the only workflow of `stw` is the bootstrap tag workflow with one dispatch input, a dry-run label | `specs/026-release-process/contracts/workflow.md` (release-build.yml inputs), `.github/workflows/release.yml:9-16` |
| G3 | `check` does not look at the workflow: the path is only a declared build input | `internal/release/validate.go:389` |
| G4 | Two build-script signatures (contract argv `OS ARCH VERSION OUTPUT`, bootstrap script `VERSION TARGET BINARY OUTDIR`) and two evidence formats (`.sha256` sidecars and `SHA256SUMS` plus `build-manifest.json`) | Spike notes item 3; `.github/scripts/release-package.sh`; `specs/026-release-process/contracts/build.md` |
| G5 | A bootstrap release has no manifest, so `record` and `verify` need `--signer-workflow-file` for it | `specs/026-release-process/contracts/bootstrap.md` (Record row) |
| G6 | The draft depends on the repository variable `STW_RELEASE_DRAFT`, which is not in the workflow file | `.github/workflows/release.yml:301` |
| G7 | The `release` group is hidden from root help; `stw release` prints help | `internal/cli/release.go:35-38`, Screen 0 |
| G8 | A dispatch answers 204 without a run id, and an error of the POST may still have started a run | `specs/026-release-process/contracts/dispatch.md` ("Disclosed effect"); `internal/release/dispatch_command.go:136-140` |
| G9 | The tag-ruleset advisory stays `unknown` although the ruleset exists and the token is admin: `RulesetsKnown` is false whenever the rulesets read fails | `internal/release/online.go:233`; `internal/hosting/release.go:208`; spike notes item 9 |
| G10 | The publish gates cannot be checked before the tag exists | `specs/026-release-process/spec.md` (command table, `--for`) |

## Scope and phases

| Phase | Content | Needs |
| --- | --- | --- |
| **1a** | The wizard for the **dispatch flow** (Spec 026), run on the spike repository `wstein/stw-release-spike`: shell, journal, states, `status`, every step, the gap fixes G1 to G10 that the wizard needs. The spike gets its own `release-build.yml` and `release-publish.yml` from the Spec 026 workflow contract | the packages R1 to R6 |
| **1b** | Making real projects releasable by the wizard: `stw` needs Spec 026 WI8 (T035 to T039: `release-build.yml`, `release-publish.yml`, `release-repro.yml`, the workflow rule tests) and T047 (the self-hosting switch: the `v0.1.0` record and deleting the bootstrap workflow, which would otherwise **also build on the wizard's tag push**); `whr` needs a `.stw/release.toml` and the two workflows (it is a Spec 026 wave 2 polyglot project; today it has only a tag-first `release.yml` that triggers on `v*` tags, `workharbor/.github/workflows/release.yml:10`) | WI8, T047, a `whr` contract |
| 2 | Polish: the expert `tag` command if the owner wants it (D2), more products per screen, release notes assistance; support for `oci-image` artifacts and for the tag-first flow if a project still needs them | |

**What v0.2.0 of `stw` and `whr` can use today:** the tag-first flow they have (the owner hand-sets an annotated tag on `main`; the bootstrap workflow of
Spec 034 builds, attests and drafts; the owner publishes the draft). **What only comes with the wizard:** one flagless walk-through, the journal,
the record and verification by the dispatch flow, and the self-hosted build. D14 asks which path v0.2.0 takes.

## The wizard

**Entry.** `stw release` with no subcommand and no flag:

| Situation | Behavior |
| --- | --- |
| Human mode (both streams terminals, `STW_AGENT` unset, no agent marker; `internal/tidywizard/owner/mode.go:80`) | The wizard starts. Mode detection is an **accident guard, not a boundary** (041 FR-006g, 044) |
| Agent mode, or no terminal | Prints the offline status and one message: "release steps need a person at a terminal; nothing was changed". The wizard refuses every effect. Exit codes as `status` |
| `stw release status [--json]` | The same read-only status in any mode, one `release-status` JSON document |
| `stw release SUBCOMMAND ...` | Exactly as today; the wizard never wraps them |

**Network reads.** The human wizard reads GitHub (bounded, authenticated, github.com only, no writes besides the two named effects) by default, falling back to
offline with a one-line notice, as Spec 044 D13. `status` and the bare command in agent mode follow the Spec 026 convention: **offline unless `--online`**; an
offline status says which parts it could not read ("not checked: needs --online") and derives only what git and the journal alone can show.

**`status` exit codes.** 0: the state was derived from all evidence the state needs; 1: needed evidence could not be read (offline, authentication, a limit); 2: usage
error; 3: the derived state is `conflict` (so a script can stop on it).

The wizard uses the working directory's repository (ownership detection as `check` does). It is read-only until the first step that names an
effect, and every effect step is a screen of its own with a typed line. Answers to `y/N` questions and the typed lines are read from `/dev/tty`
as in Spec 044 (D16); stdin is never an approval; no controlling terminal means nothing is changed. Progress lines go to stderr like the tidy progress
(`internal/cli/tidy_progress.go`); the screens go to stdout.

**Typed lines.** One plain line read from `/dev/tty`, compared exactly:

| Effect | Typed line | What the six characters bind |
| --- | --- | --- |
| dispatch one build | `build VERSION PLAN6` | the first 6 hex characters of the `plan_id` (repository, workflow, inputs incl. product, commit and version, main tip, contract digest, gate verdict; `dispatch.md`). The `plan_id` is stateless, so a retry of the same version has the same line: repeating is guarded by the journal and the evidence, not by the word |
| set and push the tag | `tag VERSION DIGEST6` | the first 6 hex characters of the SHA-256 over the canonical (product, version, commit D, SHA-256 of the record file at D), so a different commit, version, product or record gives a different line |
| tag step only, when the ruleset advisory is `unknown` | `ruleset ok` | nothing; see [the ruleset](#the-tag-ruleset-at-the-tag-step) |

A bare `y`, another version, a stale prefix, input on stdin or no terminal refuses. The six characters (24 bits) are a confirmation that the owner read
the screen, not a cryptographic commitment: no adversary chooses the state they bind; what matters is that the line is typed fresh for the
exact state shown. No typed line is ever stored.

## The state machine

A release is identified by (repository, product, version). Its state is **derived on every call** from evidence; the journal adds only what the evidence cannot show,
and it can only **restrict** (below).

### Evidence

| Axis | Values |
| --- | --- |
| Tag `T` | `none`; `local-only` (a local tag object, not on the remote); `lightweight` (remote, not annotated); `at-D` (remote annotated tag whose peeled target is the commit D approved or, before approval, the tip of `main` that holds the record); `elsewhere` (remote tag, another commit); `vanished` (the journal says it was pushed, the remote has none) |
| Release `R` | `none`; `draft` (exactly one draft with the tag name); `drafts` (two or more drafts with that name); `published` |
| Runs `U` | the runs of `ci.release_workflow` **bound to this release** (below): `none`; `queued`; `running`; `success`; `failure`; `several` (more than one bound run) |
| Record `M` | `none`; `on-branch` (in the working tree of a topic branch); `on-main` and equal to the verified record; `on-main-different` |
| Artifacts `A` | the draft's assets equal the record (read now) or not; the private directory verified or not |
| Journal `J` | the events present (a hint that restricts) |

**Run binding.** A run belongs to the release only if all hold: its workflow path is `ci.release_workflow`; its event is `workflow_dispatch`; its branch is `main`; its
triggering actor is the repository owner; its run name is `release-build PRODUCT VERSION COMMIT` (the build workflow sets `run-name` with exactly these inputs, G8); it was created
no earlier than 5 minutes before the journal line `dispatching` (or, without a journal, it is still the only such run); and only the latest attempt counts. Two or more
bound runs make `runs = several`.

### The states

| # | State | Evidence |
| --- | --- | --- |
| 0 | `nothing` | `T none`, `R none`, `U none`, `M none`, no journal effect line |
| 1 | `ready` | state 0 and `plan <C> --version V --online` has verdict `pass` (recomputed, not durable) |
| 2 | `dispatch-unseen` | `U none`, `T none`, `R none`, and the journal has `dispatching` (with or without `dispatched`): a POST may have started a run that is not visible yet |
| 3 | `build-queued` | one bound run `queued` |
| 4 | `build-running` | one bound run `running` |
| 5 | `build-failed` | one bound run `failure`, and no draft with assets; with a draft: `conflict` |
| 6 | `artifacts-built` | one bound run `success` and exactly one draft whose assets include `SHA256SUMS`, `build-manifest.json` and the files |
| 7 | `artifacts-verified` | state 6 and the private directory re-hashes equal to the record candidate and the attestation read passes (re-derived, the journal line is only a pointer) |
| 8 | `record-written` | state 7 and `M on-branch` |
| 9 | `record-merged` | state 7 and `M on-main` (remote-tracking `main`; the owner fetched) |
| 10 | `tag-ready` | state 9 and the prospective publish gates for the tip D pass (G10) and `T none`; `T local-only` is the same state with the sub-state `tag-local-only` |
| 11 | `tag-pushed` | `T at-D`, `R draft`, and the publish run not seen yet or queued |
| 12 | `publishing` | `T at-D` and a publish run `running` |
| 13 | `published` | `T at-D`, `R published`, and `verify TAG --published --online` passes |
| 14 | `abandoned` | the journal says abandoned (a restriction the owner typed), or a publish run `failure` with `T at-D`: only the next version continues |
| 15 | `conflict` | **everything else**: see the rules below |

State 1 is not durable. States 2, 8 and the `dispatching` line exist because of the journal or the working tree.

### Mapping rules (first match wins)

1. `R published` and `T at-D` and verification passes: 13. `R published` otherwise (no tag, another commit, verification fails): `conflict`.
2. `T lightweight`, `elsewhere` or `vanished`: `conflict` (a tag is never moved or re-created; a vanished tag blocks the version for good).
3. `R drafts`, or `U several`, or `M on-main-different`: `conflict`.
4. `T at-D`: `R draft` gives 11 or 12 by the publish run; a publish run `failure` gives 14; `R none` (no draft left) gives `conflict`.
5. `U none`, `R none`, `T none`, `M none`: 2 if the journal has `dispatching`, else 0 or 1.
6. `T none` or `local-only`: `U success` and no draft gives `conflict`; `U failure` with a draft gives `conflict`; otherwise by `U`/`R`/`M`/`A` as the table above (3, 4, 5, 6 to 10).
7. Any other vector: `conflict`.

**The journal only restricts.** A journal line never advances a step. The effect lines (`dispatching`, `dispatched`, `tag-created-local`, `tag-pushed`) **block repeating**
that effect for this version: after `dispatching` the wizard offers no second dispatch until a bound run is visible (then it waits), or until the owner has checked
by hand that no run exists and types `no-run VERSION` (journal `dispatch-released`); after a tag line the version can never be tagged again by the wizard. A line the
evidence contradicts keeps its restriction and prints a notice; it never produces an effect.

### What the wizard does in each state

| State | The wizard offers |
| --- | --- |
| 0, 1 | Start at the product, commit and version screen |
| 2 | Wait for the bound run for up to 30 minutes (queues are slow, spike item 12); after that show "no run is visible; check the Actions page" and offer `no-run VERSION` (a new dispatch needs it) or to wait longer. It never dispatches on its own |
| 3, 4 | Wait (never dispatch again) |
| 5 | Show the failure (run URL, failed job), then **the next version** (the default, as Spec 026 quickstart: delete the draft by hand, use the next version). Retrying the same version is possible only if the owner accepted D13 and no tag, release or draft exists |
| 6, 7 | Download and verify, then the record |
| 8 | Show the record, its digest and the comparison against the draft; the owner commits, pushes and merges (the wizard never does, D3) |
| 9, 10 | The tag screen. `tag-local-only`: offer to push the existing local tag after the same fresh checks and a new typed line |
| 11, 12 | Wait for the publish run |
| 13 | "Nothing to do." with the release URL and the verification result |
| 14 | "This version cannot continue. A tag is never re-created." and offer the next version, starting at state 0 |
| 15 | **Stop.** Show both sides (what the journal says, what the evidence shows, the rule that matched), explain what each side means, and offer **no effect step**; the owner resolves it by hand and the next call derives again. Exit 3 for `status` |
| a higher version exists | Say so and offer the next greater version |

**Restart** always means a new version; it never reuses a tag. The previous release's journal stays as history.

## The journal

A small append-only file per release, so the wizard knows facts the evidence cannot show (that a typed line was accepted, that a POST was about to be or was sent, where the
verified artifacts are).

- **Location:** `<state base>/release/<repo key>/<product or main>/<version>.jsonl`, where the state base is the `stw` state directory of Spec 041
  (`${XDG_STATE_HOME:-~/.local/state}/stw`, mode 0700, user-owned, not a symlink, outside every repository; the same checks as
  `internal/tidywizard/derive/state.go:71-112`); the repo key is the SHA-256 of the resolved `host/owner/name`. Directories are created with an explicit
  `chmod 0700`, so a process umask of `0000` (spike item 13) cannot leave them open.
- **Content:** one JSON object per line: `seq`, `at` (UTC), `event` (closed set: `planned`, `approval-observed-build`, `dispatching` (written **before** the POST),
  `dispatched`, `dispatch-released`, `artifacts-verified`, `record-written`, `approval-observed-tag`, `tag-created-local`, `tag-pushed`, `abandoned`, `restarted-as`), the facts of that
  event (commit, version, `plan_id`, digests, the artifact directory path, the run URL) and `prev` (the SHA-256 of the previous line). The names, never the values, of the
  agent markers are recorded. **No token, no secret, no approval word.** The `approval-observed-*` events say that a typed line was accepted at that time; they are facts,
  they grant nothing and are never read as an approval.
- **Bounds:** at most 64 lines and 64 KiB per file, 256 files per repository. At the 256-file limit the wizard refuses to start a new release with `journal-limit` and tells the
  owner to remove old journals by hand; `stw` never deletes them. A file over its own bound is `journal-limit` for that release: evidence only, and the effect restrictions of
  that release cannot be read, so the wizard asks for `no-run VERSION` style acknowledgements where a restriction would have applied.
- **Lock:** while a wizard holds an effect screen (from the typed line until the journal line of that effect is written) it holds an exclusive advisory `flock` on
  `<version>.lock` in the journal directory; a second wizard (another terminal, same machine) that finds the lock held starts **read-only** ("another stw release is running
  for this version"). The lock is the kernel's, so a crash leaves none. It does not cover two machines: there the evidence decides, which is why the build-in-flight guard reads
  the bound runs and a double dispatch from two machines remains possible in the seconds before a run is visible (disclosed).
- **Integrity:** the hash chain detects accidental damage and naive edits, not a malicious same-user process; the journal therefore grants nothing and every effect step re-derives its
  preconditions from evidence. A broken chain is `journal-corrupt`: the file is ignored for restrictions too, with a notice, and the wizard falls back to the stricter evidence rules
  and the explicit acknowledgements.
- **Writes:** only the wizard shell writes it. `stw release` subcommands never write it, so `dispatch --apply` stays "nothing local changes" (Spec 026 dispatch contract).

## The steps (screens)

Every screen says what happens, which gate or fact it rests on in plain words, what it will change, and what undoes it. Gate codes appear only in details (`d`) and
`--json`. Screens 1 to 6 are **mockups** (see [Screens](#screens)).

| Step | State before | Effect | Existing command used |
| --- | --- | --- | --- |
| 1 Product | 0 | none (reads contracts) | `check` |
| 2 Commit and version | 0 | none (reads) | `plan C --version V --online` |
| 3 Plan and approval, then dispatch | 1 | journal `dispatching`, then **remote write**: one `POST .../dispatches`, then journal `dispatched` | `dispatch` plan, then `dispatch --apply PLAN_ID` |
| 4 Wait for the build | 2 to 4 | none (reads runs) | hosting reads |
| 5 Download and verify | 6 | local write: the assets into the private directory | `verify --artifacts DIR --online` |
| 6 Record | 7 | local write: `docs/releases/V.json` on a topic branch | `record --write` |
| 7 Wait for the merge | 8 | none | `plan D --for publish` (G10) |
| 8 Tag | 10 | **remote write**: create the annotated tag, push `refs/tags/V` | new (the tag step) |
| 9 Wait for the publish | 11, 12 | none | hosting reads |
| 10 Verify the release | 13 | none | `verify TAG --published --online` |

### Step details

**1 Product.** One contract: used. Several (`.stw/release.<product>.toml`): a numbered list with each product's last release and tag grammar. A
repository without a valid contract stops with the `check` findings in plain words. `oci-image` artifacts are not supported in phase 1 (message
`artifact-kind-unsupported`).

**2 Commit and version.** The wizard proposes the tip of `main` that CI ran on and the next version by the scheme (greater than every existing tag
and release of the product, drafts included when the token sees them); both can be typed. Then the gates, each as one line of plain language with its state (`ok`,
`blocked`, `waiting`, `not seen`), for example "CI has not run on this commit". The gates are the rows of the Spec 026 gate table; a test requires a sentence for every
code (SC-009). A `ci-not-green` with reason `ci-run-pending` is shown as **waiting** and the wizard polls (spike items 8 and 12); `ci-run-absent` says "no CI run exists for
this exact commit (rebase merges run CI only on the pushed tip, spike item 14); choose the merged tip or wait" and never continues. It never continues past a `fail`.

**3 Plan and approval.** The wizard prints the exact request (workflow file, ref `main`, inputs), the declared network effect of fetch builds, and "This starts one CI
build. It creates a draft release and attestations. It tags and publishes nothing. A started build can be canceled in the Actions UI; a draft can be deleted by hand."
Then the typed line `build VERSION PLAN6`. On a match, with the lock held, the wizard writes `dispatching` **before** anything is sent, recomputes the plan from fresh state
(`dispatch --apply PLAN_ID` does that and refuses `plan-stale` on any change), sends the one POST, and writes `dispatched`. **A POST error, a timeout or a cancellation is not
"nothing happened"** (the code says an attempted write may have started a workflow, `dispatch_command.go:136-140`): the wizard goes to state 2 and waits for the bound run;
it never dispatches again on its own. A `main-ref-stale` refusal says "fetch main first" and waits.

**4 Wait.** Reads the bound runs every 10 seconds with a bounded budget (at most 360 reads per wait; a pause on rate limits), shows the job names and states, and offers `q`
to leave; the run continues without the wizard and the next `stw release` resumes. A failure goes to state 5.

**5 Download and verify.** Downloads the draft's assets by id into the private directory (`<state base>/release/.../artifacts/V/`; bounded count and bytes by the Spec 026
limits table), recomputes sizes and hashes, runs the attestation verification, and shows "all N files match their checksums and are attested by the build workflow of this
repository". A mismatch stops with the file name.

**6 Record.** Requires a clean checkout on a topic branch (not `main`): writes `docs/releases/V.json` with the existing `record --write` (refuses to overwrite a different file)
and shows the record's digest and the table "file, size, hash: record against draft". It then prints the commit, push and pull request commands for the owner and **does not
run them** (D3). The pull request must contain the record and, optionally, release notes; nothing else (the allowlist, Spec 026 FR-006).

**7 Wait for the merge.** After the owner merged and fetched, the wizard re-reads the remote-tracking `main` (never fetches) and evaluates the prospective publish gates of the tip D
(G10). It explains each blocker, for example "`main` moved on with a code change since the build (diff-not-allowlisted); release the next version from a newer commit".

**8 Tag.** The most careful screen. With the lock held, before it asks anything the wizard re-verifies, from fresh evidence: no tag of that name exists locally or remotely (and the
journal has no tag line); D is the tip of the remote-tracking `main` and contains the record whose digest was shown; the draft's assets still equal the record (a fresh list);
the publish gates pass; the **effective push URL** names the same host, owner and repository as the repository whose API evidence was read. It shows the tag message, the effect
("an annotated tag `V` on D is created and pushed to `origin`. That starts the publish workflow, which verifies and publishes the draft. It cannot be undone: a tag is never
re-created, a failed release uses the next version") and the ruleset check, and asks the typed line `tag VERSION DIGEST6`. Then:

- the tag **object** is created for the commit id D (never a branch name) with the owner's configured identity as tagger (read from the global `user.name` and `user.email`;
  refuse `tagger-unknown` if absent), by the native runner under the planning policy: no global or system configuration (so `tag.gpgSign` and signing are never applied: an
  **unsigned** annotated tag, D5), no hooks;
- the push is `git -c core.hooksPath=<empty directory> push --no-verify` of the explicit refspec `<tag-object-id>:refs/tags/V` (never `--force`, never a branch, one ref), over the
  **effective push URL** (`remote.origin.pushurl` and `url.*.pushInsteadOf` included) after the check above; it runs with the owner's own git configuration because the
  credential helper lives there, and it is the one git write outside the read-only sandbox: T004 reviews exactly this;
- the wizard verifies through the API that `refs/tags/V` is an annotated tag whose peeled target is **exactly D** (not a descendant), and writes the journal lines
  `tag-created-local` (after the object exists) and `tag-pushed`.

**Tag message** (data, never trusted for the version, Spec 026 edge case):

```text
stw release V

product: NAME
commit: D
built-from: C
record: docs/releases/V.json sha256:<64 hex>
```

**9 Wait for the publish** and **10 Verify the release** as above; the end screen shows the release URL and "Consumer activation is a separate step with its own
authorization" (Spec 026 quickstart step 7).

### The tag ruleset at the tag step

Spec 026 D11 makes the tag ruleset a precondition of the first tag, and the advisory has four values (`present`, `partial`, `absent`, `unknown`). At the tag screen:

| Advisory | The wizard |
| --- | --- |
| `present` | continues, with the reminder that `present` cannot prove the bypass list is only the owner (`docs/releasing.md:57`) |
| `partial`, `absent` | **refuses** the tag step: the protection is missing (positive evidence); the owner fixes the setting by hand |
| `unknown` (cannot be read) | asks for the acknowledgement `ruleset ok` **at every tag attempt**, kept in memory only, never stored or journaled; Spec 026 requires the owner to confirm the setting by hand (`quickstart.md:48`) |

Today the advisory is `unknown` even where the ruleset exists and the token is admin (spike item 9): `RulesetsKnown` is false whenever the rulesets read returns an error
(`internal/hosting/release.go:208`). The real cause is a task (T016a): capture the real response of the spike, find why the read fails, fix or explain it, so that `present`
is reachable and the acknowledgement becomes rare.

### Idempotency and interruption

Every step is safe to run again: the wizard derives the state first.

| Interrupted at | What exists | What the next call does |
| --- | --- | --- |
| before the typed line | nothing | start the step again |
| after the line, before `dispatching` | journal `approval-observed-build` only | recompute the plan and ask again (the old line never applies) |
| after `dispatching`, before or during the POST | `dispatching`, maybe a run | state 2: wait for the bound run, **no second dispatch** without `no-run VERSION` |
| the POST returned an error or timed out | `dispatching` | the same: state 2 |
| after the POST, before `dispatched` | `dispatching`, a run | the bound run shows `build-queued` or `build-running`; the journal is repaired |
| during the build | a running run | wait |
| during the download | a partial directory | the directory is removed and the download repeated |
| after `record --write`, before the pull request | the file on a topic branch | state 8; shows the commands again |
| after the typed tag line, before the local tag | `approval-observed-tag` | fresh checks, new typed line |
| local tag created, push failed or not made | `tag-local-only` | fresh checks, new typed line, then push; if the remote has a tag of that name at another commit: `conflict` |
| tag pushed, journal line missing | the remote tag | evidence shows `tag-pushed`; the journal is repaired |
| tag pushed, then deleted by someone | journal `tag-pushed`, no remote tag | `conflict` (tag `vanished`): the wizard never re-creates it |
| publish run failed | a tag, no published release | state 14: next version only |

## What is new and what is unchanged

| Item | Status |
| --- | --- |
| `check`, `plan`, `verify`, `record`, `build`, `dispatch` | **unchanged** in flags, behavior, exit codes and JSON, except the additive items listed in [Gaps](#gaps-to-fix-so-the-wizard-can-work) (and D15 if the owner decides it) |
| `stw release` bare | **changed**: help today (`internal/cli/release.go:35-37`), the wizard in human mode, offline status in agent mode |
| `stw release status [--json]` | new, read-only, any mode |
| the journal, the lock, the wizard shell | new (`internal/releasewizard`, importing `internal/release`; the CLI imports no `os` or network package, as the tidy wizard) |
| the tag step | new: the only place `stw` creates a tag and the only remote write besides the dispatch POST |
| the download of draft assets | new bounded read (D4) |

## Gaps to fix so the wizard can work

Each is a change to a contract or workflow with its own test; the wizard depends on them.

| # | Fix | Where |
| --- | --- | --- |
| G1 | Add `.stw/release.toml` to `stw` and run `check` in `make check` (Spec 026 T043 intended this) | repository root, `Makefile` |
| G2, G3 | The build workflow contract: `ci.release_workflow` names a `workflow_dispatch` workflow with inputs `mode`, `commit`, `version` and, for multi-product, `product` and `tag`. `check` gains a **text-rule cross-check** of the workflow file at the commit: a new bounded line scan inside `internal/release` (`internal/releaseworkflow` is test-only, Spec 026 T035, so nothing is reused from it at run time), no YAML dependency: findings `release-workflow-not-dispatchable` and `release-workflow-missing-input`, additive, exit 3 | `contracts/check.md`, `internal/release` |
| G4 | One evidence format: `SHA256SUMS` plus `build-manifest.json`; the bootstrap `.sha256` sidecars stop being a second format for new projects. One package-script signature: the contract argv | `contracts/build.md`, workflow templates |
| G5 | `record` and `verify` derive the signer workflow from `ci.release_workflow` and accept `--signer-workflow-file` only as an override | `contracts/record-command.md`, `verify.md` |
| G6 | The build workflow of the dispatch flow always creates a draft; the repository variable `STW_RELEASE_DRAFT` is not read by it (the bootstrap workflow keeps it, `.github/workflows/release.yml:301`) | `contracts/workflow.md` |
| G7 | Unhide the `release` group when the owner accepts the spike (D11); document `status` | `internal/cli/release.go:35`, `docs/release.md`, `docs/agent-guide.md:227` |
| G8 | The build workflow sets `run-name: release-build PRODUCT VERSION COMMIT`; the run binding above and the `build-in-flight` guard depend on it | `contracts/workflow.md`, dispatch plan |
| G9 | The rulesets read: find and fix why `RulesetsKnown` is false (T016a); the acknowledgement rule above | `internal/hosting/release.go`, `release-plan.md` |
| G10 | `plan D --for publish --version V` evaluates the publish gates for a tag that does not exist yet (the annotation gate is `not-applicable`) | `contracts/release-plan.md` |
| G11 | The doubled message `tag-unresolved: tag-unresolved` (spike item 10) prints the code once | `internal/cli/release_verify.go` |

## Spike coverage: every gap, a task and a criterion, or a deferral

| Spike item | Gap | Task | Criterion | Or deferred (reason) |
| --- | --- | --- | --- | --- |
| 1 | no contract in `stw` (G1) | T003a | SC-013 | |
| 2, 3 | workflow vs contract, no cross-check (G2, G3) | T001 | SC-014 | |
| 3 (second part) | two script signatures, two evidence formats (G4) | T003b | SC-015 | |
| 3 (bootstrap manifest) | `--signer-workflow-file` for bootstrap (G5) | T003c | SC-016 | |
| 4 | workflows need manual edits when copied | | | Phase 2: a workflow template generator; T001 catches the dispatchable-contract part now |
| 5 | draft depends on `STW_RELEASE_DRAFT` (G6) | T003d | SC-017 | |
| 6, 7 | `stw land` missing, `stw restack` hidden | | | Specs 043 and 015: not release steps; the owner decides unhiding separately |
| 7 (hidden group) | the `release` group hidden (G7) | T018 | SC-018 | |
| 8 | a pending CI run reported as a failed gate | T010a | SC-019 (the wizard waits on `ci-run-pending`) | |
| 9 | ruleset advisory `unknown` (G9) | T016a | SC-020 | |
| 10 | doubled code in `tag-unresolved` (G11) | T003e | SC-021 | |
| 11 | dispatch plan never observed | T019 (round 1 observes it on `release-build.yml`) | SC-012 | |
| 12 | slow runner queues | T011 (waits up to 30 minutes, polls) | SC-019 | |
| 13 | `0700` under umask `0000` | T005 | SC-007 (a journal directory is 0700 after an `umask 0000` run) | `stw watch --state-dir` has the same issue: deferred to its owner spec |
| 14 | push run needed on C (`ci-run-absent`) | T010 | SC-019 | |
| S1 to S5 | `pulls` stack awareness, `gh stack link` bodies, doubled checks, watch events, writes capability-off | | | Specs 016 and 043 (stacking), not release steps |
| S6 to S8 | restack plan details | | | Spec 015 follow-up |
| S9 to S11 | GitHub's automatic restack can leave the new head without the required check (close and reopen fixed it); the CI start delay | | | Spec 043 (`stw land`): it matters for landing stacks; it also means the **record pull request** may need a close and reopen before its check runs: the wizard's step 7 mentions it as advice when `main` does not move for a long time |

## Amendments to Spec 026, 034 and the docs

Where the listed text says otherwise, the new wording applies after this specification is accepted; the old text is not rewritten in place except by the
implementing pull requests. Every place that talks about the tag, the record, the ruleset or the release flow was swept (`rg` for tag, ruleset, record, draft, hidden over
Specs 026 and 034, `docs/release.md`, `docs/releasing.md`, `docs/agent-guide.md`).

| Text | Today | New wording |
| --- | --- | --- |
| Spec 026 D4 (`spec.md:17`) | "Every tag and release needs the owner's explicit approval each time." | Unchanged in substance. Added: the owner's typed approval in the wizard's tag step, or a manual `git tag -a` and push, is that approval; both are the same human act |
| Spec 026 FR-012 (`spec.md:156`) | "the owner's tag push ... need explicit authorization in their task context" | "the owner's tag push, by hand or by the wizard's tag step (typed line on `/dev/tty`)" |
| Spec 026 `plan.md:26` (constitution III row) | "the only local mutation is one new file"; "no ... publication by `stw`" | "the only local mutations are the record file, and, from the wizard only, the journal, the private asset directory and one local tag object; `stw` still publishes nothing" |
| Spec 026 quickstart step 3, 4 and 5 (`quickstart.md:49-53`) | the owner runs `dispatch`, downloads the assets, runs `git tag -a` and `git push` | the owner may instead run `stw release` and take the steps; the manual route stays valid |
| Spec 026 quickstart step 0 (`quickstart.md:48`) and bootstrap contract "Tag ruleset (D11)" (`contracts/bootstrap.md:40`) | "`partial`, `absent` and `unknown` mean: check the setting by hand and do not tag until it is confirmed" | Unchanged. The wizard **refuses** on `partial` and `absent` and, on `unknown`, requires the owner's typed `ruleset ok` at every attempt, which is the confirmation by hand |
| Spec 026 quickstart (`quickstart.md:57`) | "If something fails before step 5, delete the draft by hand and use the next version" | The wizard's default is the same (next version); retrying the same version needs D13 and requires that no tag, release or draft exists |
| Spec 026 commands table (`spec.md`, "Commands (normative)") | six subcommands | adds bare `stw release` (wizard or status) and `stw release status` |
| Spec 026 dispatch contract, "Effects" (`contracts/dispatch.md`) | "Nothing local changes; no local lock" | Still true for the command; the wizard shell around it writes the journal and holds the lock |
| Spec 026 dispatch contract, "Compatibility" ("first write to the hosting transport") | one fixed POST is the only write | the tag push is a second remote write, by the git transport and only from the wizard's tag step; no new hosting write |
| Spec 026 workflow contract (`contracts/workflow.md`, `release-build.yml`) | inputs listed, no run name | adds `run-name` (G8) and the dispatchable-workflow cross-check (G2) |
| Spec 026 `check` contract and findings table | findings of the contract only | adds the two workflow findings of G2 |
| Spec 026 release-plan contract | `plan <commit\|tag>`; publish gates need a tag | adds `--for publish` for a commit (G10) |
| Spec 026 bootstrap contract, "Authority" and Spec 034 "Authority" ("Agents never tag, run the workflow or publish") | agents never tag | Unchanged; the wizard refuses in agent mode |
| Constitution, Development Workflow (5) | the owner's explicit approval of each tag and release in the task context | Unchanged; the typed line in an owner-run terminal is that approval, and the constitution's disclosure rule is met by the screen of each effect |
| Workspace and `sourcesteward/AGENTS.md` ("Agents never merge, tag or release") | as written | Unchanged |
| `docs/release.md` (first paragraph and lines 13, 158 to 165: "The owner pushes an annotated tag on D with explicit approval") | the owner pushes the tag by hand | "The owner pushes an annotated tag on D, by hand or with the wizard's tag step, with explicit approval for both the tag and the triggered publication" |
| `docs/releasing.md:15` ("A tag push that triggers publication needs explicit approval for both effects") and `:57` (ruleset `present` cannot prove membership) | as written | Unchanged; the tag screen states both effects and repeats the membership reminder |
| `docs/release.md` hidden-group paragraph and `docs/agent-guide.md:227` | the group is hidden, commands listed | updated with the wizard, `status`, the journal and the unhidden group (G7) |

## Security analysis

**Who may do what, and what protects it.**

| Actor | Can | What stops the rest |
| --- | --- | --- |
| The owner at a terminal (human mode) | everything the wizard offers, each effect behind its typed line | the typed line, the fresh re-verification, `conflict` for every unclear case |
| An agent, same OS user, agent mode | read the status; run the **expert commands** exactly as today: `check`, `plan`, `verify`, `record --write` (a local file), `dispatch` plan **and `dispatch --apply`** (no mode check, `internal/cli/release_dispatch.go:24-83`); `git tag` and `git push` by hand | for the wizard: its refusal in agent mode (an accident guard). For the expert commands and manual git: the harness permission system the agent runs under, the repository rules (AGENTS.md: agents never tag), and for tags the ruleset against **other accounts only** |
| An agent that drives a pty or sets environment variables | what a human at that terminal could do, the wizard's typed lines included | nothing in `stw`: mode detection is an accident guard (Spec 041 FR-006g, 044); the typed line is read from `/dev/tty`, which a pty-driving agent can also write |
| Any other GitHub account | nothing | the workflows require the repository owner as actor; the tag ruleset restricts tag creation to the owner |

**Honest limits.** (1) The tag ruleset's bypass is the repository admin role (the spike's ruleset: bypass actor `RepositoryRole 5`), and an agent uses the same account as the
owner, so the ruleset separates the owner's account from other accounts, not the owner from the owner's agent. (2) `dispatch --apply` is today reachable in agent mode; whether it
should refuse there is the owner's decision D15. (3) With the owner's `gh` credentials and git access an agent can run `git tag -a` and `git push` by hand, with or without
`stw`; `stw` neither adds nor removes that power. The owner can narrow it by giving the agent a credential without `contents: write` on tags and without `actions: write` (a
token decision outside `stw`). (4) What the wizard adds is: one place where the three irreversible acts happen only after a fresh, bound, typed approval, with an explicit stop
whenever the evidence is unclear.

**Token scopes.** Reads need `contents: read` and `actions: read` (drafts need a token that sees them); dispatch needs `actions: write`; the tag push uses the owner's git
credential for `origin` (`contents: write`). The wizard reads the token only through the existing `gh auth token` path of the hosting transport and never prints, stores or
journals it.

**TOCTOU between plan and tag.** Right before the typed line and again right before creating the tag object (with the lock held) the wizard re-verifies from fresh reads: tag
absent, D the tip of `main`, record digest, draft assets, publish gates, push URL. The tag object names the **commit id** D. The remaining window is the seconds between the second
check and the push; the publish workflow re-verifies everything at the tag and the ruleset decides who may push at all.

**Replay of an old approval.** No approval is stored. The typed line contains the version and a digest of the exact state approved; an old answer, a screenshot or text on stdin
cannot match a new state. A retry of the same version produces the same dispatch line, which is why repeating a dispatch is guarded by the journal and the evidence.

**Journal tampering.** The journal only restricts: forging it can block the wizard or make it ask for an acknowledgement, never make it advance or repeat an effect.

**Never auto-publish.** The wizard never publishes, edits or deletes a release or a draft; publication is the publish workflow's job after the owner's tag. It never changes
settings, never pushes a branch, never merges, never opens a pull request.

**New attack surface.** One new local write path (the journal, the lock and the private asset directory: 0700, bounded), one new read (draft asset download), one new remote
write (the tag push, outside the read-only sandbox). The tag step and the dispatch step get an independent security review before code (T004).

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

- **FR-001** `stw release` with no subcommand and no flag starts the wizard in human mode; in agent mode or without a terminal it prints the offline status and a one-line message and
  the wizard changes nothing. `stw release status [--json]` is read-only in every mode, offline unless `--online`, with the exit codes above.
- **FR-002** The state is derived from evidence (git, GitHub reads, the working tree); the journal adds only facts and **only restricts**: it never advances a step and never makes
  an effect step available.
- **FR-003** The states 0 to 15, the mapping rules and the decision table above: every evidence vector maps to a state or to `conflict`; `conflict` offers no effect step; a tag is
  never re-created, a vanished tag blocks the version for good.
- **FR-004** Every effect step (dispatch, tag) requires its typed line read from `/dev/tty`, bound as the table states; stdin or no terminal refuses.
- **FR-005** Dispatch writes `dispatching` before the POST, uses `dispatch` plan and `--apply PLAN_ID` unchanged and sends exactly one POST; an error or timeout of the POST goes to
  state 2; no second dispatch happens without a bound run being visible or the owner's `no-run VERSION`.
- **FR-006** The tag step re-verifies from fresh reads before asking and before creating the tag, creates the annotated (unsigned, hook-free) tag object for the commit id D with the
  message above, checks the effective push URL, pushes only the explicit refspec `<tag-object-id>:refs/tags/V` without force and with `--no-verify`, and verifies through the API that
  the remote tag peels to exactly D.
- **FR-007** The wizard never commits, pushes a branch, merges, opens a pull request, publishes, edits or deletes a release or draft, or changes a setting.
- **FR-008** The journal location, bounds, closed event set, hash chain, the `0700` directories and the `flock` as specified; no secret is ever written; a corrupt or over-limit journal
  degrades to evidence only with the stricter acknowledgements.
- **FR-009** All repository and GitHub text is escaped (`esc`), bounded and treated as data; hostile tag, run, release and file names are shown escaped.
- **FR-010** The existing subcommands keep their flags, exit codes and JSON; additions are the findings and flags of G2, G5, G8, G10, G11 only (and D15 if decided).
- **FR-011** Progress goes to stderr, screens to stdout; `--json` of `status` is one schema-valid document.
- **FR-012** The wizard supports the dispatch flow and `file` artifacts; a contract naming a tag-triggered workflow or an `oci-image` target gets `flow-unsupported` or
  `artifact-kind-unsupported` with the manual route of Spec 034 (D1).
- **FR-013** The ruleset rule of "The tag ruleset at the tag step": `partial` and `absent` refuse; `unknown` needs the typed `ruleset ok` at every attempt, never stored.
- **FR-014** `ci-run-pending` is waiting, not a failure of the wizard's flow; `ci-run-absent` and every other `fail` stop.

### Success criteria

Each is testable with temporary repositories and a fake hosting transport; the end-to-end target is the spike repository, where the owner runs the real approvals.

- **SC-001** State table test: one fixture per state 0 to 15 and one per `conflict` rule (lightweight tag, tag elsewhere, vanished tag, two drafts, two bound runs, successful run with
  no draft, failed run with a draft, published without a tag, record on main different) yields the stated state and next step; **a journal line never moves a state forward**
  (mutation: a journal-only `artifacts-verified` or `tag-pushed` line with no evidence changes nothing).
- **SC-002** Interruption table test: each row of the interruption table is replayed (kill at that point) and the next call reaches the stated state with **no second effect**, in
  particular: kill after `dispatching` and after the POST error, a run invisible for 2 minutes then visible, a tag pushed then deleted.
- **SC-003** Agent mode and no terminal: no wizard effect is possible (the hosting double fails on any write method, the git push is refused); status prints; exit codes as stated.
- **SC-004** Typed lines: a bare `y`, input on stdin, a wrong version, a stale digest prefix, a line typed for another commit D or record, an old answer after the state changed, and no
  terminal each refuse and change nothing.
- **SC-005** Dispatch: exactly one POST to the fixed endpoint; a second wizard run during an in-flight or unseen build sends none; `plan-stale` after a changed main refuses; two
  wizards on one version: the second is read-only (`flock`); the journal chain survives concurrent starts.
- **SC-006** Tag: the tag object is annotated, unsigned, names the commit id D, carries the message, is pushed as one non-forced ref with `--no-verify`; a configured `tag.gpgSign`, a
  `core.hooksPath` hook, and a `pushurl` or `pushInsteadOf` to another repository each leave the push unchanged or refuse (`push-url-mismatch`); a main that moved, a replaced draft
  asset, an existing remote tag, a missing or changed record refuse before the word and again after it; a local-only tag resumes with a new word; a remote tag naming a
  descendant of D is `conflict`.
- **SC-007** Journal: bounds, hash chain, corruption, over-limit, the 256-file limit, no secret (a sentinel token in the environment appears in no journal line, stdout or stderr), and
  the directories are `0700` after a run under `umask 0000`.
- **SC-008** Read-only claims: a before and after snapshot of every repository, worktree and ref is equal for every step except the three effect steps (record file, dispatch, tag).
- **SC-009** Every gate code, state and finding has a plain-language sentence; a new code without one fails.
- **SC-010** Existing `check`, `plan`, `verify`, `record`, `build`, `dispatch` tests and goldens pass unchanged except the additions of G2, G5, G8, G10, G11.
- **SC-011** Hostile names (ESC, CR, NUL, bidi) in tags, runs, releases and files reach no terminal byte unescaped.
- **SC-012** End to end on the spike repository (phase 1a): states 0 to 13 are walked once with the real approvals (including the first observation of the dispatch plan on the
  spike's `release-build.yml`); the same run is interrupted after the dispatch and after the tag and resumed; a deliberately failed build moves to the next version without
  re-creating a tag. Evidence goes into the pull request body.
- **SC-013** `make check` runs `stw release check` over the repository's own contract (G1).
- **SC-014** `check` reports `release-workflow-not-dispatchable` and `release-workflow-missing-input` for the bootstrap `release.yml` and accepts a conforming `release-build.yml` (G2).
- **SC-015** A project contract with the contract argv and `SHA256SUMS` plus `build-manifest.json` passes; the sidecar-only format is rejected for new projects (G4).
- **SC-016** `record` and `verify` need no `--signer-workflow-file` for a dispatch-flow release (G5).
- **SC-017** The build workflow template creates a draft without the repository variable (G6).
- **SC-018** `stw --help` lists `release` once the owner unhides it, and `status` is documented (G7).
- **SC-019** A pending CI run is shown as waiting and the wizard polls; `ci-run-absent` stops with its sentence; a queue delay of 30 minutes is tolerated (spike items 8, 12, 14).
- **SC-020** With a captured real rulesets response the advisory is `present` or `partial` or `absent` as the response says; `unknown` only when the read fails (G9).
- **SC-021** `verify` of an unresolved tag prints the code once (G11).
- **SC-022** The ruleset rule: `partial` and `absent` refuse the tag step; `unknown` requires `ruleset ok` at every attempt; the word is never stored or journaled.

## Edge cases

| Case | Behavior |
| --- | --- |
| Several products | numbered list; the tag selects the product when resuming |
| A higher version exists | the wizard says so and offers the next greater version |
| Drafts invisible to the token | the draft part of the checks is `not seen`; the wizard says why and does not guess; state 6 needs a visible draft |
| Rate limit or GitHub error | the wizard stops waiting, keeps the state, says "try again later"; nothing is retried automatically except waiting reads |
| The build workflow was run from another machine | the bound run is visible; the journal is missing; the wizard continues from evidence (the run binding then relies on the run name and actor only) |
| Two wizards at once on one machine | the second is read-only (`flock`); two machines: evidence decides, with the disclosed seconds-wide window |
| Shallow clone, missing remote-tracking `main` | `plan` refuses as today (`main-ref-stale`, ancestry unavailable); the wizard says "fetch main first" |
| Jujutsu workspace | commit-level steps work as `plan` and `record` do; `verify` of a tag and the tag step are `unsupported-backend` (Spec 026 capability table) |
| `core.hooksPath`, `tag.gpgSign`, `pushurl` | the tag object ignores signing and hooks; the push uses no hooks and refuses a push URL for another repository |
| The tag exists remotely at another commit, as a lightweight tag, or has vanished | `conflict` |
| Offline, no authentication | the wizard shows what can be shown offline and stops before the first online step |
| Narrow terminal | screens wrap at the terminal width, the typed line is never truncated |
| The record pull request's check does not start (GitHub restack, spike S9) | step 7 suggests "close and reopen the pull request" when `main` does not move for long |

## Test plan: the spike repository as the real target

The spike `wstein/stw-release-spike` is a throwaway public repository the owner created for this. It gets its own `release-build.yml` and `release-publish.yml` written from
the Spec 026 workflow contract (phase 1a); `stw` and `whr` get theirs only in phase 1b. The wizard is exercised there in three rounds, each with the owner running the real
approvals: (1) walk states 0 to 13 for the spike's `v0.1.0`, interrupting after the dispatch and after the tag and resuming, then release a second version `v0.2.0` to see the
"last release" screens; (2) a deliberately failing build (a script that exits 1) to reach state 5 and the next version; (3) a second product to test the product list. Settings the test
needs (tag ruleset for the owner, environment `release`, Actions permissions, branch protection) are set only in the spike by the agent as listed in the spike's settings file; they
are never set in `stw` or `whr`. Unit and component tests use temporary repositories and the hosting double; nothing real is touched.

## Non-goals

- No auto-publish, no deletion or editing of releases or drafts, no setting changes, no pull request creation, no commit, no branch push, no merge.
- No tag signing in phase 1 (D5), no tag moves or re-creation, no force push, no hooks.
- No standing approval, no stored approval, no schedule.
- No wizard for the tag-first flow in phase 1 (D1); no `oci-image` artifacts (the BundleDock toolbox bundle, Spec 026 wave 2); no package-manager channels; no consumer activation.
- No new dependency; no YAML parser (G2 uses a bounded text scan inside `internal/release`).
- No change to the owner's repository rules: agents never tag.

## Open decisions for the owner

| # | Question | Recommendation | Consequence of the recommendation | Consequence of the alternative |
| --- | --- | --- | --- | --- |
| D1 | Phase 1 drives only the dispatch flow, not the tag-first flow of Spec 034 | Yes | `stw` v0.2.0 can be the first self-hosted release only after phase 1b; other repositories adopt the dispatch flow with their own contracts | Driving the tag-first flow too means a second flow in the wizard and a second set of states to test |
| D2 | A separate expert command `stw release tag`. The owner accepted the idea earlier in the same thread (owner-decision log); this specification recommends keeping the tag step inside the wizard | Keep it in the wizard only in phase 1; offer `stw release tag` in phase 2 if wanted | One approval surface to review; a manual `git tag -a` stays possible | An expert command is callable by scripts and agents (refused in agent mode), a second surface to secure and test |
| D3 | The wizard writes the record file on a topic branch but never commits, pushes or opens the pull request | Yes | The wizard's write set stays small; commit and PR follow the usual rules | The wizard would need git write authority and PR creation |
| D4 | The wizard downloads the draft's assets itself (a new bounded read) | Yes | The verified state can be shown and re-derived | The owner downloads by hand and the verified-artifacts state cannot be derived |
| D5 | Tag signing | Not in phase 1: an unsigned annotated tag | The ruleset and the verification are the protection | A signing key policy and a way to sign without the owner's global config are needed first |
| D6 | May the wizard publish a draft | No: never | Publication stays the publish workflow's job after the tag | A third remote write and review surface |
| D7 | The `run-name` convention and the run binding (G8) | Yes | Runs can be matched; double dispatch is guarded | Runs cannot be recognized; state 2 can never resolve |
| D8 | `check` cross-checks the workflow by bounded text rules, no YAML dependency (G2) | Yes | No new dependency, a conservative scan | A YAML parser dependency with its review cost |
| D9 | The journal as specified (location, 64 lines, hash chain, `flock`, no secrets) | Yes | Resume works; the journal can only restrict | Evidence-only resume cannot show approvals or a sent POST |
| D10 | One typed line per effect (`build ...`, `tag ...`) | Yes | Two approvals for the two irreversible acts | More words, more friction, no more safety |
| D11 | Unhide the `release` group in root help | Yes, in the release that ships the wizard, after the spike round 1 | The feature is discoverable | Stays hidden; the wizard can only be found by name |
| D12 | The ruleset rule: refuse on `partial` and `absent`; `ruleset ok` typed at every attempt only for `unknown`; fix the read (G9) | Yes | Consistent with Spec 026 D11; the prompt becomes rare once the read works | The weaker override of the first draft contradicts Spec 026 |
| D13 | Same-version retry after a failed build (no tag, no release, no draft) | Allow only then; the default stays the next version | Version numbers are not burnt by failed builds | Never reuse a version: simpler, wastes numbers |
| D14 | **The v0.2.0 release path of `stw` and `whr`.** (A) Release them by hand with the tag-first flow they have today (the owner sets an annotated tag; the bootstrap workflow builds and drafts; the owner publishes the draft); the wizard follows after the spike. (B) Wait for the wizard and phase 1b: `stw` needs Spec 026 WI8 (T035 to T039) and T047 (and the bootstrap workflow removed, because it would also build on the wizard's tag push), and `whr` needs a contract and two workflows | **(A)** for v0.2.0 of both; move `stw` to the dispatch flow with the first release after the spike rounds | v0.2.0 ships on a proven path soon; it is not self-hosted and has no `stw` record; the first self-hosted release (the first untested draft and publish path, Spec 026 "Risks") comes later and is rehearsed first | The releases wait for WI8, T047, a `whr` contract and the wizard; the first real release exercises an unrehearsed publish path |
| D15 | Should expert `dispatch --apply` refuse in agent mode (a change of Spec 026 FR-010 and of "an authorized agent may dispatch") | Yes: the owner dispatches through the wizard or by hand; an agent keeps `plan` | The accident guard covers the build effect too | Agents with the owner's authorization may keep dispatching; the only protection stays the harness permissions |

## Docs impact

`docs/release.md` (wizard first, states, journal, steps, the manual route), `docs/releasing.md` (the typed line as approval), `docs/commands.md` (`stw release`, `status`),
`docs/json-contracts.md` and a `release-status` schema, `docs/agent-guide.md` (agents: status only; the expert commands as they are), `docs/workspaces.md` (state base), the contracts
named in the amendment table, `docs/progress.md` and `docs/roadmap.md` (status lines only, by the last pull request of a wave), `docs/feature-status.json` (status `planned`). Specs 026
and 034 get a pointer to this specification.

## Risks

- The tag step is an irreversible remote write: mitigated by fresh re-verification, the typed bound line, no force and no hooks, the `conflict` stop, the independent security review and the spike rehearsal.
- The wizard could be mistaken for approval-free automation: it is human-mode only, every effect needs its line, and nothing is stored as approval.
- The journal could mislead: mitigated by "the journal only restricts".
- Two machines can still double-dispatch in a seconds-wide window: disclosed, bounded by the run binding and the release-exists gate once a run or draft appears.
- Dependence on unverified GitHub behavior (draft visibility, run discovery, attestation action): the spike rounds are the measurement, and unmeasured shapes fail closed as Spec 026 states.
- Phase 1b depends on work that does not exist yet (WI8, T047, a `whr` contract): D14 decides whether v0.2.0 waits for it.
