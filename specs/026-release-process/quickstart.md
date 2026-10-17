# Quickstart: release process v2

Everything in sections 1 to 3 runs in temporary repositories only, needs no network and no GitHub access, and builds nothing. The `stw release` commands exist only after the work items named in each section; the Git commands in section 2 were run while writing this specification (Git 2.56.0). Sections 4 and 5 describe the owner's real procedure; nothing there is run by the specification, and each step needs the owner's explicit approval.

## 1. Lint a contract (after WI1)

```sh
work=$(mktemp -d) && cd "$work" && git init -q -b main repo && cd repo
mkdir .stw && cp <worktree>/specs/026-release-process/examples/go-cli.release.toml .stw/release.toml
stw release check            # exit 0, prints the normalized model
stw release check --json     # one object, schema release-check
printf '[a.b]\nx = 1\n' > .stw/release.toml
stw release check; echo "exit $?"   # profile-violation, exit 3
```

Expected: the first run exits 0 and lists three targets and one build; the last prints `profile-violation` for the dotted table and exits 3. Replace the file with `release.json` while `release.toml` still exists to see `config-ambiguous`.

## 2. See why binding is by ancestry and an allowlist diff (Git only, runs today)

```sh
work=$(mktemp -d) && cd "$work" && git init -q -b main r && cd r
git config user.email you@example.org && git config user.name You
mkdir -p docs/releases src && echo a > docs/a.md && echo code > src/x.go && git add -A && git commit -qm c0
C=$(git rev-parse HEAD)                       # the built commit
git checkout -qb pr && echo b >> docs/a.md && echo '{}' > docs/releases/v1.json && git add -A && git commit -qm "docs: record"
git checkout -q main && echo e > e.txt && git add -A && git commit -qm "chore: other change" && git checkout -q pr && git rebase -q main
D=$(git rev-parse HEAD)                       # new SHA after the rebase
git merge-base --is-ancestor "$C" "$D"; echo "ancestor: $?"          # 0
git diff-tree -r --raw -z --no-renames --no-ext-diff --no-textconv "$C" "$D" | tr '\0' ' '; echo
```

The diff lists `docs/a.md`, `docs/releases/v1.json` and `e.txt`; with an allowlist of `docs/**` the last entry (`e.txt`) is a build input, which is exactly what `plan <tag>` and `verify` would report as `diff-not-allowlisted`. Add `.gitattributes` with a `filter`/`diff` driver that touches a file: the file is never created, because only tree objects are compared.

## 3. Plan and verify in a temporary repository (after WI3 and WI4)

```sh
stw release plan "$C" --version v0.2.0            # offline: local gates, online gates not-run, exit 1 (incomplete)
git tag -a v0.2.0 -m "stw v0.2.0" "$D"
stw release plan v0.2.0                           # publish gates, exit 3 if the diff has a non-allowlisted path
stw release verify v0.2.0 --artifacts ./assets    # offline file checks; incomplete without --online
stw release record --commit "$C" --version v0.2.0 --from ./assets    # prints the canonical record
```

Fixtures for the files in `./assets` are produced by the test helper (`SHA256SUMS`, `build-manifest.json`, small files); a real build is never part of this walkthrough. In a jj workspace `plan <commit>` and `record` work; `plan <tag>` and `verify` refuse with `unsupported-backend`.

## 4. The owner's release (needs explicit approval at every numbered step, none is automated)

0. Readiness: the tag ruleset exists (`stw release plan ... --online` reports `advisory tag-ruleset: present`; `absent` or `unknown` means check the setting by hand and do not tag).
1. Choose C: a commit on `main` that CI ran on (normally the tip after a merge) and a version greater than every existing tag and release of the product.
2. `stw release plan C --version V --online` must show verdict `pass`.
3. Approve the build in your task context, then `stw release dispatch --commit C --version V` (prints the plan and `plan_id`), then `stw release dispatch ... --apply <plan_id>`. This starts one CI build, attests the files and keeps a **draft** release. Nothing is tagged or published. Do not publish the draft in the web UI (it would create a lightweight tag).
4. Download the draft assets into a directory; `stw release record --commit C --version V --from DIR --write`; commit `docs/releases/V.json` plus the release notes as commit D on a branch; open a pull request; review the hashes against the draft; merge (a rebase merge is fine).
5. Approve the tag (approving the tag push approves the publication it triggers), then push an **annotated** tag `V` on D yourself: `git tag -a V -m "..." D && git push origin V`. The publish workflow verifies (`stw release verify V --online` on the downloaded draft) and publishes; there is no build.
6. Verify a download (assets and attestation of a published release): `stw release verify V --published [--record-commit R] --artifacts DIR --online` (for the bootstrap release add `--signer-workflow-file .github/workflows/release-bootstrap.yml --source-ref refs/tags/V`), or by hand: `gh attestation verify <file> --repo <owner/name> --signer-workflow <owner/name>/.github/workflows/release-build.yml`, and `sha256sum -c SHA256SUMS`.
7. Consumer activation (devcontainer pin, catalog, lock) is a separate step with its own authorization.

If something fails before step 5, delete the draft by hand and use the next version; never delete and re-create a tag. In input mode 2 (build once on an existing tag) the tag precedes the record: merge the record commit R, then run the publish workflow with `workflow_dispatch` (inputs `tag` and `record_commit`). The bootstrap release publishes itself; add its record commit R and check it with `verify --published` (step 6). R must follow the tag on `main` before any code lands, otherwise the diff gate fails.

Adding a product to a multi-product repository is an owner step before its first tag: add its tag pattern to the publish workflow's triggers and its prefix to the tag ruleset; release products one at a time, because another product's contract change between C and D fails the repository-wide diff.

For a repository with several products add `--product NAME` to every command (or let the tag select it); the mirror example shows `zlib-v1.3.2` selecting the `zlib` product.

## Owner settings click path

Documentation only; the tool never changes settings.

- Tag ruleset (**required** before the first release): Settings, Rules, Rulesets, New ruleset, New tag ruleset. Name it for the release tags, Enforcement `Active`, target `Include by pattern` with each product's tag pattern (`v*`; `zlib-v*`, ...), enable "Restrict creations", "Restrict updates", "Restrict deletions" and "Block force pushes", and put yourself in the bypass list so only you can create release tags. The publish workflow runs the workflow file of the pushed tag commit, so this ruleset (not the workflow) keeps a non-owner from tagging a crafted commit. Create it before the first tag; a tag cannot be protected retroactively.
- Immutable releases: Settings, General, Releases, enable "Immutable releases" (the setting's name and effects with drafts are unverified; check it before relying on it).
- `release` environment: Settings, Environments. Decision D4: no required reviewer rule; the workflows name `environment: release` and GitHub creates it unprotected on first use.
- Optional: a repository variable is not used by these workflows.

## Rehearsal checklist for the unverified GitHub behavior (before the first real release)

Do this in a **disposable repository**, before WI10, and only with the owner's explicit approval (it dispatches a build there).

Run once with a throwaway version label (a draft is deleted by hand afterward; nothing is tagged): dispatch the build workflow for a commit that is not the tip of `main`; confirm the draft exists with the assets, that the web view shows it as untagged, that `releases/tags/{tag}` does not list it, that the releases list does, that assets download by id, and that `gh attestation verify` verifies a downloaded file against the build workflow. Record the observations in the verification note; if any contradicts research items 1 to 3, switch to the workflow-artifact fallback before WI10.
