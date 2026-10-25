# Quickstart: Stack inspection and plan

> **Owner-decision amendment — 2026-10-25:** all former OQ IDs are decided;
> [current profiles](../016-stacked-pr-coordination/contracts/owner-decisions.md) supersede earlier undecided recommendations.
> Only --online opts into reads; single repository now, batch deferred.
> This is a documentation target, not implementation or native acceptance.

**Status (2026-10-25)**: specification examples only; `stw stack` and its schema are not implemented. Embedded JSON is an UNPUBLISHED proposal; executable JSON files are unchanged.

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

Expected: three retained local layers (`depth: 3`), one proven applied parent and candidate `open_depth: 2`, so no `stack-too-deep` notice (status remains `ok`, exit 0); conditional `restack-advised` with native status `unchecked`; `layer-applied-in-base` for layer 1 (observed, patch equivalence); `stack-behind-base`; a plan of two `git -c rebase.updateRefs=false -c rerere.enabled=false rebase --onto` lines with full old-tip commit ids, as in [contracts/stack.md](contracts/stack.md). Nothing is executed. The printed lines describe the human replay boundary; the owner may choose to run them in this disposable fixture, bottom first. Spec 015 apply instead uses its own plumbing/CAS plan and never executes 012 argv. Afterwards `stw stack show --base main --tip s3` reports no drift finding.

With a merge commit instead of the cherry-pick (`git merge --no-ff s1`) the expected result is no drift finding at all; with `git -c merge.ff=true merge --squash s1 && git commit -qm squash` a single-commit layer is still patch-equivalent (`git cherry` marks it `-`) and is reported `layer-applied-in-base`; a multi-commit layer stays `pending` locally and only the pull request evidence marks the parent as merged (`rewritten`).

## jj variant (design; flag and quoting measured, the rest unverified until T002)

```sh
jj git init --colocate repo-jj && cd repo-jj
# create bookmarks s1, s2, s3 on three stacked changes, move main over the first change
stw stack show --base main --tip s3
```

Expected: the same layers with change ids, `capability-unavailable` (`jj-capability-gap`) instead of patch equivalence, the boundary limitation `jj-landed-layer-undetectable` on every jj result, and a single `jj rebase -s <change> -o '"main"'` step for a moved base; after a remote merge with head deletion, a native stack whose merged member head is in the range gives `landed-undetectable` instead (on Git too, when the merged member's branch was deleted locally), while a plain chain is unguarded (the limitation is the disclosure).

## Optional live read-only check of the pull request evidence

Requires pre-existing caller credentials and matching branches/PRs. This is an optional proposed online observation, outside the offline documentation checks; it was not run for this refresh. It makes only fixed bounded `GET` requests with `--online`, with no HTTP cache, mutation, implicit fetch or login. The explicit clone below is a separate caller setup effect, never performed by `stw stack`.

```sh
work=$(mktemp -d) && cd "$work"
git clone -q https://github.com/OWNER/NAME.git repo && cd repo
stw stack show --base origin/main --tip BRANCH --online
```

The caller first creates/updates the disposable fixture explicitly; existing local objects and remote-tracking refs are the offline cache and may be stale. Owner decision 2026-10-25 selects --online only, default offline, with old --pull-requests rejected; these target commands are unimplemented.

Expected: per layer `PR #N (open, stack K position P of S, verified)` for a native stack, `native-stack-managed`, and no `rebase` step after a merge of a native stack member (at most `fetch` and `observe` while an object is missing, otherwise one `observe` describing GitHub's rewrite); `stack-link-missing` and a printed `gh stack link --base main <PR numbers bottom first>` line for a plain chain with validated `base.trunk_ref: main`. A different known trunk is passed explicitly; an unknown/unsafe trunk blocks link instead of silently using the repository default.

## Checks to run on any output

- No output line says the stack is correct, ready, safe to merge or restacked; a result without `action` or `notice` findings says only that none was found in the evidence read.
- `stw stack plan ... --json | python3 -c 'import json,sys; r=json.load(sys.stdin); assert r["plan"]["advisory"] is True; assert all(f["reason"] is not None for f in r["findings"] if f["confidence"]=="unknown")'`.
- `git -C "$work/repo" for-each-ref`, `git status --porcelain` and `git count-objects -v` print the same before and after; nothing is fetched, and no `.git/gh-stack` file exists.
- Complete all-pending fixtures at candidate open depth 2/3/32 have no notice/notice/notice. Three local layers with one proven landed have depth 3, open_depth 2 and no notice. Unknown landing remains a qualified candidate upper bound rather than an exact open-PR count; incomplete/error has null open_depth. The 33rd local layer independently gives stack-limit/incomplete, not a depth-advice failure.
- A closed-unmerged parent reports `pull-request-closed-unmerged`; a child still based on it can report `base-mismatch`, but closing alone never proves landing or `restack-advised`. A merged parent on a stale local base produces `stale-base` rather than a rebase.
- A tip equal to the base or contained in it gives `stack-landed` with exit 0; `--base nosuch` exits 1 with `stack-ref-invalid`; `--repo OWNER/NAME` without `--online` exits 2 with no JSON.

## What the output does not prove

A stack observation says what the repository and, if requested, GitHub reported at one moment. It does not say that a pull request can merge (Spec 025), that GitHub has finished rebasing a native stack (server-side rewrites lag, unverified), that a printed command will succeed (conflicts appear only in a real rebase) or that the remote-tracking refs are fresh (nothing is fetched).

## Separately selected next steps

The observation's sampled full old-parent, new-base and child-head identities may be supplied explicitly to Spec 015, whose first Git plan also works without Spec 012 (D6). Observation does not run preflight (Spec 014), checkpoint/restore (Spec 013), local apply (Spec 015), forge effect apply (Spec 016) or human landing (Spec 043). Nothing prompts or mutates without the corresponding explicit operation.

After an actual local rewrite, Spec 016 may render a separately selected blocked lease-push plan with an exact fresh expected remote SHA. Before push, it independently applies the [actual-head/change-class policy](data-model.md#optional-downstream-evidence-handoff): docs-only checks and equal range-diff for a pure rebase, or exact-new-SHA independent review for code changes (middle-tier allowed for an equal pure rebase). Ready always needs independent exact-head CLEAR, required green CI and rebaseability. Patch equivalence, a predicted quarantine head and an old review never replace the applicable actual-head evidence. Owner resync commands are inert instructions with destructive limits; an agent never executes them on the owner's checkout.

012's designed exit codes are 0/1/2. The implemented hidden Spec 015 v1 has incomplete exit 3; the owner-decided target revision 2 maps it to 1 after the explicit 015 code task. Neither example changes the published runtime behavior of another command.

The output’s nested restack-advised finding is distinct from Spec 035’s existing restack-needed forge watch event: the watcher supplies a merge-commit new_parent and argv, while this observation references local layer indices and the explicitly selected base. Neither implies the other, and neither authorizes executing the event argv. Re-observe and create a separate effect plan; see the [namespace contract](contracts/stack.md#qualified-finding-and-watch-event-namespaces). 012 already defaults offline; this draft does not accept --offline, unlike status’s online default and explicit offline view.
