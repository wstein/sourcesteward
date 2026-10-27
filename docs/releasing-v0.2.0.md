# Release protocol: stw v0.2.0 by hand

**Status: checklist for the owner, written 2026-10-27, nothing here was run.** Decision D14 of
[Spec 045](../specs/045-release-wizard/spec.md#decided-by-the-owner) is that `stw` v0.2.0 is released now, by hand,
with the tag-first bootstrap flow of [Spec 034](../specs/034-bootstrap-release/spec.md), and that the steps are
written down so the later release wizard can reproduce them (task T029). This page is that protocol before the
run: fill the **Record** lines while you execute it. The [readiness checklist](release-readiness-v0.2.0.md)
says what must be closed first; the [release notes draft](release-notes/v0.2.0.md) is the text for step D3.

An agent never tags, runs the workflow, publishes or edits the release (`AGENTS.md`, "Pull requests and history").
Every step marked **OWNER** is yours; **read-only** steps may be run by anyone. Do not start a step before its
preconditions hold; abort points are handled in [Stop and abort](#stop-and-abort).
Placeholders: `REPO` is `wstein/sourcesteward`, `V` is `v0.2.0`, `C` the full 40-hex commit, `RUN` a workflow
run id, `DIR` an empty download directory outside the repository.

## How the wizard will map each step

The wizard of Spec 045 drives the **dispatch** flow (build, record, then tag), not this tag-first flow
([phases](../specs/045-release-wizard/spec.md#scope-and-phases)). Each **Wizard** line names the wizard step or
state ([states](../specs/045-release-wizard/spec.md#the-states)) or says the step exists only here. Summary at the end.

## A. Before the tag

### A1. Close the readiness checklist (OWNER, read-only)

Every item of [release-readiness-v0.2.0.md](release-readiness-v0.2.0.md) is closed or accepted by you.
Abort point: an open Draft that the release needs (for example #323) or a red check on `main`.
Record: the date, and the pull requests you accepted as open. Wizard: none (the wizard has no readiness list).

### A2. Check the repository settings (OWNER, read-only)

`stw` and the workflow never change settings (`docs/releasing.md`). The observed state on 2026-10-27 is in brackets.

```sh
gh variable list --repo REPO                              # STW_RELEASE_DRAFT = true   [true]
gh api repos/REPO/environments --jq '.environments[].name'   # contains release        [github-pages, release]
gh api repos/REPO/rulesets --jq '.[] | [.name,.target,.enforcement] | @tsv'
gh api repos/REPO/immutable-releases                     # {"enabled":false,...}      [false]
```

- `STW_RELEASE_DRAFT=true` makes the workflow create a **draft** (`.github/workflows/release.yml:301`); without it the
  release is published directly. Keep it `true`.
- Environment `release` is used by the publish job (`release.yml:250`); no reviewers are required by design.
- A tag ruleset for `v*` restricting creation, update and deletion to you is required before the first real
  product tag (`docs/releasing.md:54`, Spec 026 D11). **The rulesets read lists only `main` (target `branch`); no
  tag ruleset was listed. Unknown, owner to confirm** whether one exists and how to read it; create it in
  Settings, Rules, Rulesets if not.
- Release immutability was **disabled** when read. Whether to enable it before v0.2.0 is **unknown, owner to
  confirm** (it locks the tag and assets after publication; `docs/releasing.md:44`).

Record: the outputs. Abort point: `STW_RELEASE_DRAFT` is not `true`, or you do not accept the missing tag ruleset.
Wizard: tag step, "The tag ruleset at the tag step" (advisory `unknown` asks for the typed acknowledgement `ruleset ok`).

### A3. Choose C (OWNER, read-only)

C is the tip of `main` that has a successful push run of `ci.yml`. A rebase merge runs CI only on the pushed tip,
so an older commit may have no run (spike item 14); the workflow refuses a commit without one
(`.github/scripts/release-verify.sh`, command `ci-green`).

```sh
git fetch origin --tags
git rev-parse origin/main                       # this is C unless you chose an older commit; write the 40 hex characters
gh run list --repo REPO --workflow ci.yml --branch main --event push --limit 5 --json headSha,conclusion,status
git merge-base --is-ancestor C origin/main && echo ok
```

Expected: a row with `headSha` equal to C, `status` `completed`, `conclusion` `success`; `ok` printed.
The `ci-green` gate also needs the required checks `check` and `Git/jj conformance (current) / suite` to have passed
(`main` ruleset). Record: C. Abort point: no green push run for C (wait; a queued run is not green), or `main`
moved to a commit you did not review. `stw release plan` cannot be used here: this repository has no `.stw/release.toml`
(`ls .stw` fails), which is why the tag-first flow applies. Wizard: step 2 "Commit and version" (`plan C --version V --online`, state 1 `ready`).

### A4. Check the version (OWNER, read-only)

```sh
git tag --list 'v*' --sort=-version:refname | head -3      # latest is v0.1.0
gh release list --repo REPO --limit 5                      # v0.1.0 is Latest; V is absent
sh .github/scripts/release-verify.sh version V             # exit 0
gh api "repos/REPO/releases" --paginate --jq '.[] | select(.tag_name == "V") | .id'   # prints nothing
```

Expected: the tag does not exist, no release or draft named V exists, the grammar check exits 0
(`release-verify.sh` command `version`; the `verify` job runs the same, `release.yml:62`). Record: the outputs.
Abort point: V exists in any form: use the next version (`docs/releasing.md`, failure table).
Wizard: step 2 (version proposal and the gates `version-not-monotonic`, `release-exists`).

### A5. Optional dry run of the workflow (OWNER, a build: needs your explicit approval)

A dry run builds and smoke-tests all four targets from `main` and never publishes (`release.yml:68`, `dry_run`).
Earlier dry runs of the same workflow succeeded on 2026-10-22 and 2026-10-23 (`specs/034-bootstrap-release/verification.md`).
It is optional; whether to run it for v0.2.0 is **unknown, owner to confirm**.

```sh
gh workflow run release.yml --repo REPO --ref main -f version=v0.0.0-dryrun
gh run list --repo REPO --workflow release --limit 3
gh run watch RUN --repo REPO --exit-status
```

Expected: jobs `verify`, `build` (4), `smoke` (4) pass and `publish` is skipped. The run is for the tip of `main` at
dispatch time, so it is C only if `main` has not moved. Record: RUN, its head SHA, conclusion. Abort point: any
failed job (read its log, fix on `main` through a pull request, restart at A3). Wizard: none (the dispatch flow
has no dry run; the wizard's nearest equivalent is the plan gates of step 2). Duration: unknown.

## B. The tag

### B1. Freeze (OWNER, read-only, immediately before B2)

`git fetch origin && test "$(git rev-parse origin/main)" = C && echo unchanged`. Abort point: any other output
(re-run A3 and A4). Wizard: step 8 "Tag" re-verifies the tip and the gates from fresh evidence first.

### B2. Create the annotated tag (OWNER)

```sh
git tag -a V -m "stw V" C
git cat-file -t V                  # tag   (a lightweight tag prints commit and is refused by the workflow)
git rev-parse "V^{commit}"         # must print C
```

The tagger is your global git identity; agents never set one. The workflow does not read the message
(`release-verify.sh`, command `tag`, checks type, ancestry only). The wizard's own message format is in
[Spec 045](../specs/045-release-wizard/spec.md#step-details) ("Tag message") and is optional by hand.
Record: the outputs. Abort point: wrong type or commit: `git tag -d V`, redo B2. Wizard: step 8 (local part, journal `tag-created-local`).

### B3. Push the tag (OWNER; this is the publication approval)

```sh
git push origin refs/tags/V
```

This is the only effect that cannot be undone: it starts the `release` workflow, which builds, attests and creates
the release (`release.yml:10`, trigger `push` of `v[0-9]+.[0-9]+.[0-9]+`). The run is accepted only if you started it
and pushed it (`release.yml:75-84`). Never move, delete and re-create, or force-push the tag; a failed release uses the
next version. Record: the push output and the time. Wizard: step 8, typed line `tag VERSION DIGEST6`, state 10 to 11 (`tag-pushed`).

## C. The workflow run

### C1. Watch the run (OWNER, read-only)

```sh
gh run list --repo REPO --workflow release --limit 3        # a push run with headBranch V
gh run watch RUN --repo REPO --exit-status
```

Expected jobs, in order: `verify` (owner, annotated tag on `main`, `ci.yml` green for C, no release exists,
`go mod verify`, `go vet`, docs and architecture checks, `internal/releaseworkflow` tests), `build` for
`linux_amd64`, `linux_arm64`, `darwin_arm64`, `darwin_amd64` (each built twice and compared byte for byte), `smoke`
on each platform's own runner, then `publish` (environment `release`, attests `stw_*.tar.gz` and `SHA256SUMS`,
creates the release; a draft because of the variable). Record: RUN, each job's conclusion, timestamps.
Wizard: "Wait for the publish", states 11 and 12; its flow builds before the tag, so the jobs differ.

## D. Inspect the draft

### D1. List the draft (OWNER, read-only)

```sh
gh release view V --repo REPO --json isDraft,tagName,assets --jq '{isDraft,tagName,assets:[.assets[].name]}'
```

Expected: `isDraft` true, `tagName` V, five assets: `SHA256SUMS` and `stw_0.2.0_{linux,darwin}_{amd64,arm64}.tar.gz`
(as for v0.1.0, `specs/034-bootstrap-release/verification.md`). Wizard: none (tag-first draft inspection; the dispatch flow downloads and verifies before the tag, step 5).

### D2. Download and verify (OWNER, read-only)

```sh
mkdir DIR && gh release download V --repo REPO -D DIR
(cd DIR && shasum -a 256 --check SHA256SUMS)
for f in DIR/stw_0.2.0_*.tar.gz; do gh attestation verify "$f" --repo REPO; done
tar -xzf DIR/stw_0.2.0_linux_amd64.tar.gz -C DIR && DIR/stw_0.2.0_linux_amd64/stw version   # your platform
```

Expected: four `OK` lines, four successful attestation verifications, and `SourceSteward v0.2.0` (the workflow
smoke ran the same check on every platform, `.github/scripts/release-smoke.sh:66`). Downloading a draft by tag with `gh release
download` is **unmeasured for this repository (the spike runbook step A4 was written, not run): unknown, owner to
confirm**; if it fails, download the assets in the web UI. `stw release verify --published` cannot be used: a
bootstrap release has no `build-manifest.json` and no record (spike item 3). Record: the outputs. Abort point:
a checksum mismatch or a failed attestation: do not publish. Wizard: step 5 "Download and verify" (different evidence in the dispatch flow).

### D3. Release notes (OWNER, decision)

The draft body is the generated text of `.github/scripts/release-notes.sh` (header, verification instructions and
a commit-subject list). To use the curated [notes](release-notes/v0.2.0.md), edit the body:

```sh
gh release view V --repo REPO --json body --jq .body > DIR/body.md      # edit, then:
gh release edit V --repo REPO --notes-file DIR/body.md
```

Whether to replace, extend or keep the generated body is **unknown, owner to confirm**; no repository document
decides it. Keep the "Verify a download" instructions. Wizard: the wizard offers no notes assistance in phase 1 (Spec 045 phase 2).

## E. Publish and verify

### E1. Publish the draft (OWNER)

```sh
gh release edit V --repo REPO --draft=false
```

The command comes from the spike runbook (step A5); the spike did not run it for a real release, so it is
**unmeasured: unknown, owner to confirm** (the web UI "Publish release" is the alternative). Publishing is not
repeatable: from now on assets and tag are public. Wizard: none for the tag-first flow (the wizard's publish workflow does this, step 9).

### E2. Verify the published release (OWNER, read-only)

```sh
gh release view V --repo REPO --json isDraft,publishedAt,assets --jq '{isDraft,publishedAt,n:(.assets|length)}'
gh api repos/REPO/git/ref/tags/V --jq .object.type               # tag
rm -rf DIR && mkdir DIR && gh release download V --repo REPO -D DIR
(cd DIR && shasum -a 256 --check SHA256SUMS) && for f in DIR/stw_0.2.0_*.tar.gz; do gh attestation verify "$f" --repo REPO; done
```

Expected: `isDraft` false, `n` 5, the ref type `tag`, all checks pass. Record: the outputs and `publishedAt`.
Wizard: step 10 and state 13 `published` (`verify TAG --published --online`; by hand here because there is no record).

## F. After the release (OWNER, none of these is automatic)

1. Close or update tracker #263 and the readiness checklist; record this run. T029 of Spec 045
   expects the executed protocol at `specs/045-release-wizard/protocol/v0.2.0-stw.md` (`specs/045-release-wizard/tasks.md:72`);
   the filled copy's location is **unknown, owner to confirm**.
2. Update the `sourcesteward` gitlink in Anchorage, only you do (workspace `AGENTS.md`).
3. The toolbox bundle entry follows only after `whr` v0.2.0 is also finished (owner decision 2026-10-25); consumer
   image activation is a separate approval; the US-English sweep starts after the release.
4. A record commit R (`docs/releasing.md`, bootstrap section) followed no tag so far: `specs/034-bootstrap-release/verification.md`
   says no record exists for v0.1.0. Whether v0.2.0 gets one is **unknown, owner to confirm**.

## Stop and abort

| Where | What happened | What to do |
| --- | --- | --- |
| A1 to A4 | a gate is red, a setting differs, V exists | Stop. Fix it through a pull request or a setting, then restart at A3. Nothing remote has changed. |
| B2 | wrong tag object or commit | `git tag -d V`, redo B2. |
| C1 `verify` fails | owner, annotated, CI or release-exists check | No release was created. The tag exists remotely: do not move it. Next version, new tag (`docs/releasing.md`, "Wrong or previously used tag"). |
| C1 `build` or `smoke` fails | a platform differs or a test fails | Same: no release exists. Read the logs, fix on `main`, use the next version. Re-running a failed workflow does not authorize a different tag. |
| C1 `publish` fails | draft may or may not exist | Look at `gh release list`; delete a leftover draft by hand; next version. A re-run by anyone but the owner is refused (`release.yml:274-275`). |
| D2 mismatch | checksum or attestation fails | Do not publish; keep the draft for evidence; investigate; next version. |
| E1 done | a problem found later | `stw` cannot undo publication. Publish a fix as the next version and say so in its notes. |

## Wizard mapping at a glance

| Hand step | Wizard step or state | Shared? |
| --- | --- | --- |
| A3, A4 choose C and V, the gates | step 2, state 1 `ready` | yes |
| A2 settings, the ruleset | tag step ruleset screen | yes |
| A5 dry run | none | tag-first only |
| B1 to B3 tag and push | step 8, states 10 to 11 | yes |
| C1 watch the run | steps 4 and 9, states 3, 4, 11, 12 | order differs (build after tag) |
| D1, D2 inspect the draft | step 5, states 6, 7 | evidence differs |
| D3 notes, E1 publish | publish workflow (step 9) | tag-first only |
| E2 verify the release | step 10, state 13 | yes |
