# Feature Specification: Tidy git-native integration proofs, PR discovery and bounded planning

**Branch**: `codex/spec-tidy-integration-proofs` | **Created**: 2026-10-19
**Status**: Draft specification; no runtime implementation. Requires independent review before tasks start.
**Input**: Dogfooding `stw tidy` (Spec 029 contract 3) on this repository (149 local branches, 111 worktrees, GitHub rebase-merge) and on the Anchorage umbrella (submodules). Forge issues: #79 (plan refused for `submodule.*` or `core.hooksPath` configuration), #80 (v1 plan fails at the 64 KiB `nativeOutputLimit` around 146 branches/86 worktrees), #81 (v3 online plan needs explicit `--pr OID=N`; duplicate OIDs rejected as duplicate hints; offline rebase-merge integration is not detected: 93 of 149 refs `integration-not-proven`).

## Scope and decision status

This Spec extends Spec 029 (local tidy with explicit integration evidence); it does not replace it. Spec 029 stays the owner of contract 3 commands, stored plans, recovery roots, apply effects and the online budget model. This Spec adds, behind the same explicit contract selection and with no new write effect:

1. forge-free, bounded **integration proof kinds** beyond ancestry (patch-id equivalence, merge-noop);
2. **merged-PR discovery** that is keyed by distinct head OID, with `--pr` as an override;
3. **bounded streaming** read paths so planning no longer fails at 64 KiB;
4. an explicit **configuration policy** (refuse versus neutralize) for read-only planning;
5. a **real-repository acceptance** task with before/after counts.

The exact numeric bounds below are proposed design decisions (marked *proposed*), derived from existing Spec 029 constants where one exists. They need owner confirmation through independent review of this Spec; none is existing policy beyond the cited Spec 029 values.

Nothing here authorizes a build, a release, a tag, an installation, an `apply` against a real repository or any activation. See [owner-gated work](#owner-gated-work).

## Dogfood evidence (measured on this repository, 2026-10-19)

Of 131 non-main local branches: 5 are **merge-noop** (merging them into the base changes nothing), 21 are **patch-id equivalent** (overlapping the 5), 33 would add content to the base, and 99 **conflict** with the base: their content was consolidated and reworked into other commits, so no forge-free proof exists. Offline v3 reported 93 of 149 refs `integration-not-proven` (rebase-merge rewrites SHAs so ancestry fails).

A per-commit cherry-pick simulation was evaluated and **rejected**: 1,087 of 1,750 commits conflict because stacked commits depend on their predecessors, so a conflict says nothing about integration. See [research](research.md#rejected-per-commit-cherry-pick-simulation).

## User scenarios and acceptance

### US1 — Recognize rebase- and squash-merged work without a forge (P1)

A maintainer plans a tidy offline. A branch whose commits were rebase-merged is reported `integration-proven` by `patch-id` evidence; a branch whose cumulative content landed unchanged (rebase-merge or squash-merge) is proven by `merge-noop`; an ancestor still proves by `ancestor`. Each reports its proof kind, strength and bounded counts.

1. Fixture: rebase-merge of a three-commit branch yields `patch-id` (3 of 3 commits equivalent) and `merge-noop`.
2. Fixture: squash-merge of the same branch yields `merge-noop` and not `patch-id`.
3. Fixture: base later reverts the integrated change. `patch-id` still matches (history equivalence) while `merge-noop` does not; the plan reports exactly that.
4. Fixture: branch with one unmatched commit, or one that would add content, is retained with evidence `partial` and the counts; nothing is deleted on a heuristic.
5. A branch that conflicts with the base is retained `integration-not-proven` with the conflict count only, never a deletion proof.

### US2 — Online evidence without per-branch hints (P1)

With `--online --remote NAME`, merged-PR evidence is discovered for every unproven distinct head OID without `--pr`. Several refs sharing one OID share one hint. `--pr OID=N` overrides discovery for that OID. Errors name the offending selector (`--pr` value, OID or ref).

### US3 — Plan large repositories (P1)

A repository with 149 branches/111 worktrees (and up to 1,024 of each) plans successfully in v1 and v3 within bounded memory. One record above a bound refuses with a named limit, never a truncated plan.

### US4 — Plan in repositories with submodules or hooks configuration (P1)

A repository or umbrella with `submodule.*`, `core.hooksPath` or `core.fsmonitor` configuration plans read-only; configured helpers provably never execute. Configuration that cannot be evaluated or that would execute under a planning command is refused with the key class named. Apply keeps its existing refusals.

### US5 — Honest evidence for consolidated work (P3, deferred)

A non-destructive `superseded-candidate` evidence aid (range-diff pairing) is described but deferred; see [plan](plan.md#decision-superseded-candidate-evidence-aid-deferred).

### Edge cases

- Merge commits in `M..B`; empty patches; a branch equal to the base; an unborn or symbolic ref; detached heads; a ref that is also a workspace-used ref (always retained, proof annotates only); more than 1,024 refs; SHA-256 repositories (patch-id and merge-tree are supported by native Git; PR discovery stays SHA-1/GitHub.com as in Spec 029).
- An object-database write by `merge-tree --write-tree` must never touch the inspected repository (see FR-009).
- Hostile branch names, config values and `.gitattributes` are data; they never become argv, endpoints or commands.

## Requirements

### Integration proofs (forge-free)

- **FR-001**: Define the closed set of proof kinds `ancestor`, `patch-id`, `merge-noop`, `github-merged-head` (Spec 029). Each proven ref records the kind, a strength label (`ancestry`, `content-present`, `history-equivalent`, `forge-attested`), the base selector and OID, the tip OID and bounded counts. Multiple kinds may be recorded; the strongest present decides the headline, in the order above. Proof never overrides protection: selected bases, symbolic refs, workspace-used refs and uncertain targets retain regardless of proof (Spec 029 FR-010).
- **FR-002**: `ancestor`: the tip is reachable from the base OID (`git merge-base --is-ancestor TIP BASE` semantics). Unchanged from Spec 029.
- **FR-003**: `patch-id` has `git cherry` semantics. Let `M` be the merge base of base `U` and tip `B`. Branch side is every non-merge commit in `M..B`; base side is every non-merge commit in `M..U`. The proof holds when the branch side is non-empty and every branch-side commit has a base-side commit with an equal stable patch-id, independent of order; matching is by set membership of the patch-id, so two branch-side commits with the same patch-id both match one base-side commit with that patch-id. Merge commits are skipped and counted. A commit with an empty patch, or a patch-id that cannot be computed, never matches. Any unmatched commit makes the result `partial`, retained with `matched`, `unmatched`, `skipped-merges` counts.
- **FR-004**: `merge-noop` holds when `git merge-tree --write-tree --no-messages U B` exits 0 (clean merge) and prints a tree OID equal to the tree OID of `U` (applying the branch to the base changes nothing). Exit 1 (conflict) or any other status is `not-proven` and records `conflict` or the failure class only. A tree that differs from `U^{tree}` records `would-add-content`. A tip that is an ancestor, or equal to the base, is reported as `ancestor` and merge-noop is not evaluated.
- **FR-005**: Nothing is proven by per-commit cherry-pick simulation, subject similarity, branch names, dates, authors or PR titles. A ref that is not provable is retained `integration-not-proven` with its evidence (counts, conflict/would-add classification), never selected for deletion by a heuristic.
- **FR-006**: A new `--integration-proof KINDS` option (comma-separated subset of `ancestor,patch-id,merge-noop`) narrows which forge-free kinds may make a ref eligible. The default is the full set (open question OQ-1 in [plan](plan.md#open-questions)). Unselected kinds are neither run nor reported.
- **FR-007**: Proof order per distinct tip OID is ancestor, then patch-id, then merge-noop, then (online only) PR evidence. Evaluation stops at the first kind that yields eligibility only when `--integration-proof` selects a single kind; otherwise all selected kinds run so evidence is complete, within the budget.
- **FR-008**: Bounds (*proposed*). Per plan: at most 1,024 evaluated tips (Spec 029 observation limit); per tip at most 1,024 branch-side commits and 4,096 base-side commits (`git rev-list --count --no-merges` pre-check, exceeding refuses that tip's `patch-id` with `proof-limit`, keeps other kinds); each native proof command stdout at most 64 KiB and stderr at most 64 KiB; one aggregate forge-free proof deadline of 60 seconds over the planning operation. Budget exhaustion retains remaining tips `integration-not-proven` with evidence `proof-budget-exhausted`; the plan is still valid and says so. Counters are never reset per tip.
- **FR-009**: Proofs are read-only for the inspected repository. `merge-tree --write-tree` writes tree objects, so it runs with a private temporary object directory (`GIT_OBJECT_DIRECTORY` set to a fresh private directory and `GIT_ALTERNATE_OBJECT_DIRECTORIES` naming the repository's object directories), removed on every exit path. A test asserts the repository's object directory and refs are byte-identical before and after planning. The same environment applies to every proof command. Failure to create the private directory refuses `merge-noop` only, with a named class.
- **FR-010**: Proof commands use fixed typed argv, `--` separators, full OIDs resolved by an earlier native read, the sanitized environment of FR-019, no pager, no external diff, no textconv, no merge driver. `merge-noop` is refused for a repository whose effective configuration defines `merge.*.driver`, `filter.*.clean|smudge|process` or `diff.external` (class `git-unsafe-configuration`, key class named); `patch-id` needs no such refusal under FR-019 environment.
- **FR-011**: Forge-free proofs also run without `--online` and without any network, authentication or state-directory requirement. Offline plan output and plan identity include proof policy version and the selected kinds, so a stored plan is bound to the proof meaning (Spec 029 FR-027).
- **FR-012**: Apply re-evaluates the recorded proof kind for each selected deletion immediately before the effect, with fresh native state, and retains on any change. Deletion keeps Spec 029 recovery roots; this Spec adds no new effect class.

### Merged-PR discovery (fixes #81)

- **FR-013**: Online planning discovers merged-PR evidence by head OID with no `--pr` hint, for every distinct unproven tip OID of the selected refs. Discovery is read-only GET against one explicitly resolved repository and uses only the closed endpoint set in [online discovery](contracts/pr-discovery.md#endpoint-set): the repository record, the closed-PR collection (up to 10 pages of 100) and the per-PR read already allowed by Spec 029. No write, no redirect following to another host, no compression (Spec 029 online contract).
- **FR-014**: Hints and evidence are keyed by distinct OID. Several refs with the same OID share one hint and one evidence record; the result lists every ref it covers. A repeated `--pr OID=N` with the same pair is accepted as idempotent; the same OID with a different number is a usage error naming the OID and both numbers.
- **FR-015**: `--pr OID=N` overrides discovery for that OID only and still passes full tuple validation (Spec 029 FR-007). A hint for an OID that no selected ref carries is a usage error naming the OID, not silently ignored.
- **FR-016**: Every usage or discovery refusal names the offending selector: the full flag text (`--pr <value>`), the OID and the affected refs, truncated to the Spec 029 4,096-byte selector bound with escaping of control bytes.
- **FR-017**: Discovery coverage is explicit. When the closed collection ends within the page and record bounds, coverage is `complete` and OIDs without a match are `integration-not-proven` with `no-merged-pr`. When a bound is reached, coverage is `truncated`: the plan still completes, OIDs found are used, others are `integration-not-proven` with `discovery-truncated`; no absence is claimed. Multiple admissible PRs for one OID require an explicit hint (Spec 029 FR-008).
- **FR-018**: A research task measures, on this repository, whether the closed-PR collection covers the 149 refs and why #81 required hints, before implementation chooses between keeping that collection and adding a second read-only endpoint (see [plan](plan.md#decision-discovery-endpoint)). Any added endpoint is added to the closed endpoint set, the transport route allow-list and its tests together; none is added by default.

### Configuration policy (fixes #79)

- **FR-019**: Native commands used by planning (reads, proofs and the observation of Spec 029) run with a sanitized environment and command-line configuration that neutralize configured helpers: `GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL` unset to an empty file for execution, no `GIT_*` override inheritance, plus `-c core.hooksPath=/dev/null -c core.fsmonitor=false -c submodule.recurse=false -c core.untrackedCache=false`, `--no-optional-locks`, `--no-replace-objects` and `GIT_NO_LAZY_FETCH=1`. Global and system configuration are still *read once* by a separate admission read for classification and evidence; they are excluded only from commands that evaluate content.
- **FR-020**: Classification table ([configuration policy](contracts/configuration-policy.md)). **Neutralized for planning** (recorded as a plan note, plan succeeds): `core.hooksPath`, `hook.*`, `core.fsmonitor`, `submodule.*` (including `submodule.recurse`, `submodule.<name>.update`), `core.pager`, `core.editor`, `core.sshCommand`, `credential.*`, `gpg.*`, `diff.external` and `diff.<driver>.textconv`. **Refused** (named class, no plan): `include.*`/`includeIf.*` whose result cannot be evaluated consistently, `extensions.worktreeConfig`, `extensions.partialclone`, `remote.*.promisor`, `core.alternateRefsCommand`, `core.sparseCheckout*`, `core.attributesFile` pointing outside the admitted tree, executable `filter.*` (for kinds that evaluate content: FR-010), `merge.*.driver` (for `merge-noop`), reserved attribute-sentinel `filter.*` names (Spec 029 sentinel rule). Unknown executable-looking keys are refused by class, never guessed safe.
- **FR-021**: Gitlink entries in a base tree are data; no command descends into a submodule or runs `git submodule`. A worktree retirement target containing an initialized submodule retains `submodule-present` (Spec 029 retirement boundary unchanged).
- **FR-022**: Apply keeps every existing refusal. Neutralization is planning-only: a plan records the neutralized configuration digest, and apply refuses mutating commands whose hooks or helpers would execute (for example a `reference-transaction` hook for a ref deletion), naming the key class, rather than silently skipping hooks. A repository whose plan noted neutralized keys is therefore plannable but may be non-appliable; the plan says which.
- **FR-023**: A sentinel test proves non-execution: for every neutralized key class a configured helper script writes a marker file; after planning the marker is absent and the plan notes name the key class. Mutating the neutralization (mutant) makes the sentinel fail.

### Bounded planning (fixes #80)

- **FR-024**: The v1 (and v3) reference and registration reads stream through the existing bounded native stream executor and parse record by record; whole-output buffering at 64 KiB (`nativeOutputLimit`) no longer applies to `for-each-ref` and `worktree list --porcelain -z` reads. Memory is bounded by one record plus the parsed set.
- **FR-025**: Hard bounds remain refusals (*proposed*, reusing Spec 029 constants): at most 1,024 references and 1,024 worktree registrations (`observationLimit`); each record at most 4,224 bytes; each stream at most 4,325,376 stdout bytes and 65,536 stderr bytes. One record or byte above refuses with `git-observation-limit` (or `git-output-limit` for byte overflow) naming the stream, never a truncated or partial plan.
- **FR-026**: Documentation of v3 as the only supported large-repository path is **not** chosen; v1 is repaired by FR-024 so v1/v2 callers keep working, with v1/v2 published bytes and result shape unchanged. 64 KiB stays the bound for single-value reads that are not collections.

### Acceptance and effects

- **FR-027**: Real-repository acceptance: after reviewed source exists and a build is separately authorized, run `stw tidy plan` (never `apply`) on this repository and on the Anchorage umbrella (and its pinned submodules that tidy supports) with a temporary state directory, and record counts before (current stage2 baseline) and after per proof kind and disposition in a verification note. Missing lanes are gaps.
- **FR-028**: All new proof, discovery and configuration behavior is first covered by failing tests using temporary repositories and deterministic transport doubles (constitution IV). No test touches a real user repository.
- **FR-029**: Schema: new fields are additive to the unpublished v3 result/plan ([data model](data-model.md)); v1 and v2 schemas and fixtures stay byte-identical; the v3 schema is published only with accepted implementation, as Spec 029 already requires.
- **FR-030**: No new network write, no new native write effect, no new remote ref effect. Native effects of proofs are limited to the private temporary object directory of FR-009.

## Success criteria

- **SC-001**: US1 fixtures (rebase-merge, squash-merge, revert-after-merge, partial, conflict) produce exactly the stated proof kinds, strengths and counts offline, with zero network and zero repository object/ref changes.
- **SC-002**: On a generated 150-branch/120-worktree fixture, v1 and v3 plans succeed; fixtures at 1,024 refs/registrations pass and at 1,025 refuse with the named limit; peak buffered stdout in the parser is one record.
- **SC-003**: Every neutralized key class has a sentinel test where the helper marker file is absent; every refused class has a refusal test naming the class; mutants (remove the `-c` neutralization, rerun with global config) are killed.
- **SC-004**: Online fixtures with 40 refs sharing 12 OIDs plan with no `--pr`; duplicate/mismatched hints fail with selector-naming messages; truncated discovery is reported `truncated`, never as absence.
- **SC-005**: Published v1/v2 schemas, fixtures and old stored-plan bytes are unchanged; v3 results validate against the candidate schema and agree with human output.
- **SC-006**: Recorded before/after counts for this repository (baseline 93 of 149 `integration-not-proven` offline) and the Anchorage umbrella exist in a verification note, with every unexplained retention listed. No cleanup was applied.

## Owner-gated work

The author of this Spec and its implementation may not: build or install a CLI artifact, run acceptance with a built `stw`, run `apply` against any real repository, tag, release, publish, change repository/branch/ruleset settings, merge, or activate a consumer image. Each needs explicit owner authorization naming the one operation (workspace `AGENTS.md`). Source tests, formatting, vet and test-compiler verification may run automatically. `--integration-proof` default (OQ-1), the proposed numeric bounds and the deferral of the evidence aid need owner confirmation.

## Key entities

- Proof evidence: kind, strength, base selector/OID, tip OID, counts, policy version, classification (`proven`, `partial`, `would-add-content`, `conflict`, `not-evaluated`).
- Discovery record: OID, hint number and identities, covered refs, coverage (`complete`, `truncated`), source (`discovered`, `hint`).
- Configuration note: key class, neutralized or refused, digest, effect on apply.

## Assumptions and scope

Same-user cooperative repositories (Spec 029). Native Git minimum/current lanes as Spec 029 (2.46.0 / 2.56.0); `merge-tree --write-tree` requires Git 2.38+. jj and GitButler backends are out of scope: no Git writes for them, no proof kinds are claimed for them. Patch-id equality proves that equivalent changes landed at some point, not that they remain present; `merge-noop` proves present content. Consolidated work remains retained.

## References

[Spec 029](../029-tidy-dogfood-safety/spec.md), [online contract](../029-tidy-dogfood-safety/contracts/online.md), [streams contract](../029-tidy-dogfood-safety/contracts/streams.md), [tidy v3 contract](../029-tidy-dogfood-safety/contracts/tidy-v3.md), [git cherry](https://git-scm.com/docs/git-cherry), [git patch-id](https://git-scm.com/docs/git-patch-id), [git merge-tree](https://git-scm.com/docs/git-merge-tree), [git range-diff](https://git-scm.com/docs/git-range-diff).
