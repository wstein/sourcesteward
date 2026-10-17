# Catalog: later stack effects (not part of this feature)

Nothing in this catalog is implemented, authorized or accepted by Spec 012. It records the effects that the owner wants as separate, optional, explicitly authorized work items, so that `stack plan` can print them as advisory steps without the specification being silent about who owns them. Each item needs its own specification amendment (Spec 015 or Spec 016), its own plan and analysis, tests-first work items and explicit authorization at execution time (constitution II and III; AGENTS.md: pushes, merges and releases need the user's authorization). The common rules below apply to every item.

## Common rules

1. Each item is its own command with a `plan` that is inspectable, bounded and revalidated before apply; stale evidence refuses. A plan is not authorization.
2. A stack observation of Spec 012 is an input, never proof: every item rereads the relevant local and remote state, compares it with the plan's `expected` values and refuses on any difference.
3. Remote effects go through the bounded transport of Spec 022 (extended by an amendment that allows a closed list of write shapes) or are left to the maintainer as printed commands; they never go through an unreviewed shell-out, a `gh` extension subprocess, GraphQL or a response-provided URL.
4. Native stack semantics are GitHub's. An item may mirror what `gh stack` does (M6 in [research.md](../research.md)) but must not invent behavior, must not choose a merge method for the maintainer and must treat the maintainer's merge method as an input.
5. Per-action outcome records, duplicate prevention on retry, partial-failure reporting and explicit remote recovery limits follow Spec 016 FR-004 and FR-005.
6. Tier 1: each item states its Git and jj mechanism, or a stated capability gap.

## Items

| Id | Effect | Owner spec | Delegation and mechanism | Preconditions before specification |
| --- | --- | --- | --- | --- |
| L1 `stack link` | Create a native stack from the layers' open pull requests, or extend an existing one (existing members are never removed) | Spec 016 | `gh stack link` semantics (bottom to top; pushes branches; creates missing pull requests) | Measure the REST write endpoints for stack creation and extension in a disposable repository with the owner's approval (U7); decide whether creating missing pull requests is part of L1 or a separate item; refuse `stack-split`, ambiguous pull requests and fork heads; idempotent on retry |
| L2 `stack push` | Push the layer branches, in order | Spec 016 | Git: push with `--force-with-lease` and `--atomic` semantics; jj: `jj git push` of the named bookmarks; Spec 024 hook disclosure for any `pre-push` hook | Lease values come from `Layer.upstream.commit_id` and the evidence is recomputed at execution time with merge commits counted (`git cherry` omits them); 012 gives no push advice. Needs an amendment of Spec 016 FR-006, which forbids force-push in the first slice (as for L5, which needs the merge amendment); lease values from the plan's `expected` commit ids; never a plain force-push; no push of a layer with `layer-conflicted`; remote-tracking evidence is stale unless fetched |
| L3 `stack sync` | Fetch, restack, push and link as one reviewed sequence | Specs 015 and 016 | Composition of the `fetch` step, the local rewrite of Spec 015, L2 and L1, each separately authorizable; for a native stack only fetch and observe (GitHub rewrites) | L2 and L1 specified; Spec 015 apply with recovery evidence; the lock-only updater rule of AGENTS.md does not apply because nothing here is a bot |
| L4 `stack unstack` | Dissolve a native stack on GitHub (pull requests stay open) | Spec 016 | `gh stack unstack` semantics: pull requests that are queued or have auto-merge enabled stay stacked | Measure the write endpoint; report the pull requests that remained stacked |
| L5 `stack merge` | Merge a native stack up to a chosen pull request, all or nothing | Spec 016 (needs an amendment of its FR-006, which forbids merges in the first slice) | `gh stack merge` semantics: GitHub evaluates branch protection and rulesets, bypass is unsupported, a merge queue is respected, the method is the maintainer's choice | Explicit merge authorization at execution; Spec 025 explanation as pre-read; measure `gh stack merge` effects first (the spike did not) |
| L6 apply a printed `rebase` plan locally | Rewrite the local layers with recovery evidence | Spec 015 | The same `git rebase --onto` / `jj rebase -s` forms of the Plan rules; Spec 015 adds recovery roots, interruption handling and conflict reporting | Spec 015 plan and analysis; revalidation against the Spec 012 `expected` ids |

`stack plan` prints `fetch`, `rebase` and `link` steps as text only. A step names the item that would perform it (`performed_by`), but no code in Spec 012 starts a process, writes a ref or writes to GitHub.
