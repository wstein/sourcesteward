# Quickstart: Stack inspection and plan

Acceptance never needs the network: the Go tests use real temporary repositories and an in-process fake HTTP transport. The commands below do not exist until work item WI4 lands; the expected output is the design, not a recorded result. Everything runs in temporary directories and changes no real repository. The jj variant is unverified until task T002 has measured the jj forms.

## Offline reproduction of the rebase-merge drift (Git)

```sh
work=$(mktemp -d) && cd "$work"
git init -q -b main repo && cd repo
git config user.email you@example.invalid && git config user.name you
echo base > f && git add f && git commit -qm base
for i in 1 2 3; do
  git switch -q -c s$i $([ $i = 1 ] && echo main || echo s$((i-1)))
  echo $i > f$i && git add f$i && git commit -qm "s$i"
done
# Simulate GitHub's rebase merge of layer 1: the same patch with a new commit id.
git switch -q main
GIT_COMMITTER_DATE="2030-01-01T00:00:00Z" git cherry-pick s1 > /dev/null

stw stack show --base main --tip s3
stw stack plan --base main --tip s3
stw stack plan --base main --tip s3 --json
```

Expected: three layers; `layer-applied-in-base` for layer 1 (observed, patch equivalence); `stack-behind-base`; a plan of two `git rebase --onto` lines with full old-tip commit ids, as in [contracts/stack.md](contracts/stack.md). Nothing is executed. To repair, you run the printed lines yourself, bottom first; afterwards `stw stack show --base main --tip s3` reports no drift finding.

With a merge commit instead of the cherry-pick (`git merge --no-ff s1`) the expected result is no drift finding at all; with `git merge --squash s1 && git commit -qm squash` the layer stays `pending` locally (no patch equivalent) and only the pull request evidence marks the parent as merged (`rewritten`).

## jj variant (design; unverified until T002)

```sh
jj git init --colocate repo-jj && cd repo-jj
# create bookmarks s1, s2, s3 on three stacked changes, move main over the first change
stw stack show --base main --tip s3
```

Expected: the same layers with change ids, `capability-unavailable` (`jj-capability-gap`) instead of patch equivalence, and, with `--pull-requests` showing the parent merged, a single `jj rebase -s <change> -d main` step.

## Optional live read-only check of the pull request evidence

Needs `gh auth login` and a repository whose branches match open pull requests; it makes only the fixed `GET` requests of the Remote sources table and writes nothing.

```sh
work=$(mktemp -d) && cd "$work"
git clone -q https://github.com/OWNER/NAME.git repo && cd repo
stw stack show --base origin/main --tip BRANCH --pull-requests
```

Expected: per layer `PR #N (open, stack K position P of S, verified)` for a native stack, `native-stack-managed`, and no `rebase` step after a merge of a native stack member (only `fetch` and `observe`); `stack-link-missing` and a printed `gh stack link` line for a plain chain.

## Checks to run on any output

- No output line says the stack is correct, ready, safe to merge or restacked; a result without `action` or `notice` findings says only that none was found in the evidence read.
- `stw stack plan ... --json | python3 -c 'import json,sys; r=json.load(sys.stdin); assert r["plan"]["advisory"] is True; assert all(f["reason"] is not None for f in r["findings"] if f["confidence"]=="unknown")'`.
- `git -C "$work/repo" for-each-ref`, `git status --porcelain` and `git count-objects -v` print the same before and after; nothing is fetched, and no `.git/gh-stack` file exists.
- A tip equal to the base or contained in it gives `stack-landed` with exit 0; `--base nosuch` exits 1 with `stack-ref-invalid`; `--repo OWNER/NAME` without `--pull-requests` exits 2 with no JSON.

## What the output does not prove

A stack observation says what the repository and, if requested, GitHub reported at one moment. It does not say that a pull request can merge (Spec 025), that GitHub has finished rebasing a native stack (server-side rewrites lag, unverified), that a printed command will succeed (conflicts appear only in a real rebase) or that the remote-tracking refs are fresh (nothing is fetched).
