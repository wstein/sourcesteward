# Release protocol: stw v0.2.0 by hand

**Status: checklist for the owner, written 2026-10-27, nothing here was run.** Decision D14 of
[Spec 045](../specs/045-release-wizard/spec.md#decided-by-the-owner) is that `stw` v0.2.0 is released now, by hand,
with the tag-first bootstrap flow of [Spec 034](../specs/034-bootstrap-release/spec.md), and that the steps are
written down so the later release wizard can reproduce them (task T029). Fill the **Record** lines while you execute it. The [readiness checklist](release-readiness-v0.2.0.md)
says what must be closed first; the [release notes draft](release-notes/v0.2.0.md) is the text for step D3.

An agent never tags, runs the workflow, publishes or edits the release (`AGENTS.md`, "Pull requests and history").
**OWNER** steps are yours; **read-only** steps may be run by anyone. Abort points: [Stop and abort](#stop-and-abort).
Each **Wizard** line names the wizard step or
state ([states](../specs/045-release-wizard/spec.md#the-states)) that corresponds; the wizard drives the **dispatch** flow (build,
record, then tag), not this tag-first flow ([phases](../specs/045-release-wizard/spec.md#scope-and-phases)).
Placeholders: `REPO` is `wstein/sourcesteward`, `V` is `v0.2.0`, `C` the full 40-hex commit, `RUN` a workflow
run id, `DIR` an empty download directory outside the repository, `ID` the ruleset id printed by the third command of A2. Run the commands in your own clone of `REPO`.

## Owner decisions needed before the tag

This page decides none of these; each is open.

| # | Decision | Where it bites |
| --- | --- | --- |
| 1 | Create the `v*` tag ruleset first (the rule of `docs/releasing.md:54-57`), or record an explicit written waiver | A2, hard stop |
| 2 | Release immutability on or off before V (read: off) | A2 |
| 3 | Dry run of the workflow before the tag, yes or no (a build) | A5 |
| 4 | After a failed run on an unchanged tag: re-run it once for the same tag, or always the next version | Stop table |
| 5 | Release body: generated, curated notes, or both (v0.1.0 body was generated) | D3 |
| 6 | Publish with the web UI or `gh release edit --draft=false` (how v0.1.0 was published is not recorded) | E1 |
| 7 | Tracker #70: step 1 (full run in a throwaway clone) and step 2 (one safe element on the live workspace), required or dropped | readiness |
| 8 | #326 (`y` plus end of input at `stw tidy apply`): fix before V or list as a known limit | readiness |
| 9 | Where the filled protocol lives: `specs/045-release-wizard/protocol/v0.2.0-stw.md` (T029) or this page | F |
| 10 | Whether the older open issues gate the tag. Proposal only: #269 (lock-taking writers rely on a 1 s wait for sandbox descendants, follow-up of the safety fix #217) is the one that touches tidy locks, so the owner may want it decided; #218, #216, #195, #108 are Low test or message follow-ups; #98, #93, #74, #72, #64 are later features | readiness |

Not open: #323 (Spec 044 P4b) and T016 are required (owner decision 2026-10-26); a record commit is Spec 026 T047.

## A. Before the tag

### A1. Close the readiness checklist (OWNER, read-only)

Every item of [release-readiness-v0.2.0.md](release-readiness-v0.2.0.md) is closed. Abort point: #323 or T016
not done, or a red check on `main`. Record: the date. Wizard: none (the wizard has no readiness list).

### A2. Check the repository settings (OWNER, read-only, HARD STOP)

`stw` and the workflow never change settings (`docs/releasing.md`). Observed 2026-10-27 in brackets.

```sh
gh variable list --repo REPO                              # STW_RELEASE_DRAFT = true   [true]
gh api repos/REPO/environments --jq '.environments[].name'   # contains release        [github-pages, release]
gh api repos/REPO/rulesets --jq '.[] | select(.target=="tag") | [.id,.name,.enforcement] | @tsv'   # [prints nothing]
gh api repos/REPO/rulesets/ID --jq '{inc:.conditions.ref_name.include,rules:[.rules[].type],bypass:.bypass_actors}'
gh api repos/REPO/immutable-releases                     # {"enabled":false,...}      [false]
```

- `STW_RELEASE_DRAFT=true` makes the workflow create a **draft** (`.github/workflows/release.yml:301`); environment
  `release` is used by the publish job (`release.yml:250`), no reviewers by design.
- **The tag ruleset is absent.** The rulesets read succeeded and lists only `main` (target `branch`, id 25683357);
  the repository belongs to a user, so no organization ruleset applies. `docs/releasing.md:54-57` says `absent`
  blocks the first real tag, and Spec 034 lists the ruleset as an owner action before the tag
  (`specs/034-bootstrap-release/spec.md:93`). **Stop here.** Proceed only when the third command lists an active
  ruleset and the fourth shows `refs/tags/v*`, `creation`, `update`, `deletion` and `non_fast_forward`, with a bypass
  list that you confirm by hand is owner-only (`docs/releasing.md:37-40`: an admin role may not be owner-only).
  Create it in Settings, Rules, Rulesets (owner decision 1). A waiver is only valid if you write it down with
  the reason; v0.1.0 appears to have been tagged without a ruleset, which is a precedent, not the rule.
- Release immutability is **off** (decision 2); when on it locks tag and assets after publication (`docs/releasing.md:44`).

Record: the outputs, and the ruleset id or the waiver text. Abort point: no ruleset and no waiver, or the variable
is not `true`. Wizard: `absent` **refuses**, only `unknown` asks `ruleset ok`
([table](../specs/045-release-wizard/spec.md#the-tag-ruleset-at-the-tag-step)).

### A3. Choose C (OWNER, read-only)

C is the tip of `origin/main` (as in Spec 045 step 2) and must have a successful push run of `ci.yml`; a rebase
merge runs CI only on the pushed tip, so an older commit may have none (spike item 14). The workflow refuses a commit
without one (`.github/scripts/release-verify.sh`, command `ci-green`, `:83`).

```sh
git fetch origin --tags
git rev-parse origin/main                       # this is C; write the 40 hex characters
gh run list --repo REPO --workflow ci.yml --branch main --event push --limit 5 --json headSha,conclusion,status
```
Expected: a row with `headSha` equal to C, `completed`, `success`; that means the `check` job and both conformance
lanes passed (`release.yml:106-107`). Record: C. Abort point: no green push run for C (queued is not green; wait).
`stw release plan` cannot be used: there is no `.stw/release.toml` (`config-missing`), hence the tag-first flow.
Wizard: step 2 "Commit and version" (`plan C --version V --online`, state 1 `ready`).

### A4. Check the version (OWNER, read-only)

```sh
git tag --list 'v*' --sort=-version:refname | head -3      # latest is v0.1.0
gh release list --repo REPO --limit 5                      # v0.1.0 is Latest; V is absent
sh .github/scripts/release-verify.sh version V             # exit 0
gh api "repos/REPO/releases" --paginate --jq '.[] | select(.tag_name == "V") | .id'   # prints nothing
```
Expected: no tag, release or draft named V; the grammar check exits 0 (`release.yml:62`). Record: the outputs.
Abort point: V exists in any form: use the next version (`docs/releasing.md`, failure table).
Wizard: step 2 (version proposal; gates `version-not-monotonic`, `release-exists`).

### A5. Optional dry run of the workflow (OWNER, a build: needs your explicit approval)

A dry run builds and smoke-tests all four targets from `main` and never publishes (`release.yml:68`). It
skips the tag, CI and release-exists gates (`release.yml:86`, `:99`, `:111`), so it does not exercise them. The
latest dry run (2026-10-23, `specs/034-bootstrap-release/verification.md`) took about 3 minutes 46 seconds. Decision 3.

```sh
gh workflow run release.yml --repo REPO --ref main -f version=v0.0.0-dryrun
gh run list --repo REPO --workflow release --limit 3
gh run watch RUN --repo REPO --exit-status
```

Expected: `verify`, `build` (4), `smoke` (4) pass, `publish` is skipped; it runs on the tip of `main` at dispatch.
Record: RUN, head SHA, conclusion. Abort point: a failed job (fix via pull request, restart at A3). Wizard: none.

## B. The tag

### B1. Freeze (OWNER, read-only, immediately before B2)

```sh
git fetch origin && test "$(git rev-parse origin/main)" = C && echo unchanged
git ls-remote --tags origin "refs/tags/V*"        # prints nothing
git remote get-url --push origin                  # github.com/wstein/sourcesteward (https or ssh form)
git config --get-regexp '^url\..*insteadof$|^remote\.origin\.pushurl$'   # nothing that redirects to another repository
git tag --list V                                  # prints nothing (no local tag either)
```

Abort point: any other output (re-run A3 and A4; an existing remote tag of that name is never reused).
Record: the outputs and time. Wizard: step 8 "Tag" re-verifies the same facts before it asks anything.

### B2. Create the annotated tag (OWNER)

```sh
git tag -a V -m "stw V" C
git cat-file -t V                  # tag   (a lightweight tag prints commit and is refused by the workflow)
git rev-parse "V^{commit}"         # must print C
```

The tagger is your global git identity. The workflow does not read the message (`release-verify.sh`, command
`tag`, checks type and ancestry only); the wizard's message format ([Spec 045](../specs/045-release-wizard/spec.md#step-details)) is optional by hand.
Record: the outputs. Wizard: step 8 (local part, journal `tag-created-local`).

### B3. Push the tag (OWNER; this is the publication approval)

```sh
git push origin refs/tags/V
```
One explicit ref: never `--tags`, `--follow-tags` or `--force`. This is the first effect that cannot be undone: it
starts the `release` workflow, which builds, attests and creates the release (`release.yml:10`). The run is accepted
only if you started and pushed it (`release.yml:75-84`). Never move, delete and re-create the tag. Record: the push
output and the time. Wizard: step 8, typed line `tag VERSION DIGEST6`, state 10 to 11 (`tag-pushed`).

## C. The workflow run

### C1. Watch the run (OWNER, read-only)

`gh run list --repo REPO --workflow release --limit 3` shows a push run with headBranch V; then
`gh run watch RUN --repo REPO --exit-status`.

Expected jobs: `verify` (owner, annotated tag on `main`, `ci.yml` green for C, no release exists, vet, docs and
architecture checks, `internal/releaseworkflow` tests), `build` for the four targets (each built twice and compared
byte for byte), `smoke` on each platform's own runner, then `publish` (attests the archives and `SHA256SUMS`, creates
a draft because of the variable). The v0.1.0 run took about 3 minutes 53 seconds (`gh api repos/REPO/actions/runs/39985568858`).
Record: RUN, each job's conclusion, timestamps. Wizard: "Wait for the publish", states 11 and 12; its flow builds before the tag.

## D. Inspect the draft

### D1. List the draft (OWNER, read-only)

```sh
gh release view V --repo REPO --json isDraft,tagName,assets --jq '{isDraft,tagName,assets:[.assets[].name]}'
```

Expected: `isDraft` true, `tagName` V, five assets: `SHA256SUMS` and `stw_0.2.0_{linux,darwin}_{amd64,arm64}.tar.gz`
(as for v0.1.0). v0.1.0 was a draft first (assets 16:21Z, published 16:33Z, 2026-10-23); V is unmeasured. Record: the output. Wizard: none here (the dispatch flow verifies before the tag, step 5).

### D2. Download and verify (OWNER, read-only)

```sh
mkdir DIR && gh release download V --repo REPO -D DIR
for f in DIR/stw_0.2.0_*.tar.gz; do gh attestation verify "$f" --repo REPO; done
(cd DIR && shasum -a 256 --check SHA256SUMS)    # macOS; on Linux without shasum use the coreutils checker
tar -xzf DIR/stw_0.2.0_darwin_arm64.tar.gz -C DIR && DIR/stw_0.2.0_darwin_arm64/stw version   # Apple silicon
tar -xzf DIR/stw_0.2.0_linux_amd64.tar.gz -C DIR && DIR/stw_0.2.0_linux_amd64/stw version     # Linux amd64
```

Run the checksum line and the one `stw version` line for your platform. Expected: four `OK` lines, four successful
attestation verifications, `SourceSteward v0.2.0` (the workflow smoke ran the same check on every platform,
`.github/scripts/release-smoke.sh:66`). `stw release verify --published` cannot be used: a bootstrap release has no
`build-manifest.json` and no record (spike item 3). Record: the outputs. Abort point: a mismatch or failed attestation: do not publish. Wizard: step 5 "Download and verify" (different evidence in the dispatch flow).

### D3. Release notes (OWNER, decision 5)

The draft body is the generated text of `.github/scripts/release-notes.sh` (header, verification instructions and a
commit-subject list). To use the curated [notes](release-notes/v0.2.0.md), edit the body and keep the verification
instructions:

```sh
gh release view V --repo REPO --json body --jq .body > DIR/body.md      # edit, then:
gh release edit V --repo REPO --notes-file DIR/body.md   # then: shasum -a 256 DIR/body.md
```
Record: which option, and the SHA-256 of the final body. Wizard: none (notes assistance is phase 2).

## E. Publish and verify

### E1. Publish the draft (OWNER, decision 6)

`gh release edit V --repo REPO --draft=false`, or the web UI "Publish release". How v0.1.0 was published is not
recorded. Publishing is not repeatable: assets and tag become public. Record: the command or click, output and
time. If it fails, use the other method. Wizard: none here (the dispatch flow's publish workflow, step 9).

### E2. Verify the published release (OWNER, read-only)

```sh
gh release view V --repo REPO --json isDraft,publishedAt,assets --jq '{isDraft,publishedAt,n:(.assets|length)}'
gh api repos/REPO/git/ref/tags/V --jq .object.type               # tag
rm -rf DIR && mkdir DIR && gh release download V --repo REPO -D DIR
# then the checksum, attestation and version lines of D2
```
Expected: `isDraft` false, `n` 5, ref type `tag`, all checks pass. Record: the outputs. Wizard: step 10 and state 13 `published` (`verify TAG --published --online`; by hand here because there is no record).
## F. After the release (OWNER, none of these is automatic)

Update tracker #263 and record this run (decision 9); update the gitlink in Anchorage (only you do); the toolbox
bundle follows only after `whr` v0.2.0 is finished; consumer image activation is a separate approval; the
US-English sweep starts after the release.

## Stop and abort

| Where | What happened | What to do |
| --- | --- | --- |
| A1 to B1 | a gate is red, a setting differs, V exists, tip moved, remote tag or odd push URL | Stop. Fix it through a pull request or a setting, restart at A3. Nothing remote has changed. |
| B2 | wrong tag object or commit | `git tag -d V`, redo B2. |
| B3 | push rejected, or the outcome is unknown (timeout, lost answer) | `git ls-remote origin refs/tags/V`. If it names your tag object, continue at C1. If absent, push the same local object again. Never create a second tag object. |
| after B3 | no `release` run appears | Do not push again. Check Actions settings and the run list; investigate. |
| C1 `verify`, `build` or `smoke` fails | a gate, a platform or a test | No release exists. The tag is remote: never move it. Read the logs, fix on `main`, next version (state 14 `abandoned` in the wizard). |
| C1 transient failure | runner loss, API flake in `ci-green` | Owner decision 4: re-run once for the same unchanged tag (`gh run rerun RUN --failed`; the workflow re-checks owner, tag commit and absence of a release, `release.yml:274-281`), or the next version. |
| C1 `publish` fails | a draft may or may not exist | Look at `gh release list`; delete a leftover draft by hand; then decision 4. Another actor's re-run is refused (`release.yml:274-275`). |
| D2 mismatch | checksum or attestation fails | Do not publish; keep the draft for evidence; next version. |
| D3, E1, later | `gh release edit` fails; a problem found after publication | Use the web UI, never re-tag. `stw` cannot undo publication: publish a fix as the next version. |

## Wizard mapping at a glance

| Hand step | Wizard step or state | Shared? |
| --- | --- | --- |
| A3, A4 choose C and V, the gates | step 2, state 1 `ready` | yes |
| A2 the tag ruleset | tag step ruleset screen: `absent` refuses, `unknown` asks `ruleset ok` | yes |
| A5 dry run | none | tag-first only |
| B1 to B3 tag and push | step 8, states 10 to 11 | yes |
| C1 watch the run | steps 4 and 9, states 3, 4, 11, 12 | differs: build after the tag |
| D1, D2 inspect the draft | step 5, states 6, 7 | differs: evidence |
| D3 notes, E1 publish | publish workflow (step 9) | tag-first only |
| E2 verify the release | step 10, state 13 | yes |
