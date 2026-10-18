# Research and decisions: Spec 029

Status: design rationale for stacked-integration specification/plan re-review; no new runtime behavior or live API investigation.

## Native stream and transaction boundaries

**Decision:** Factor the existing workspace sandbox launch policy and add narrowly typed read/transaction sessions. Preserve canonical executable/configuration checks, fixed argv, minimal environment, read-only grants for observations and network denial. Do not import Spec 027's helper-neutralizing inventory runner.

**Rationale:** Existing `nativeCapture` caps complete output; both tree metadata and stored blobs fail before semantic completeness can be established. Existing retirement has three full observations in planning and eight in apply; each includes one stored and two disk user-content passes. Changing only the first collector leaves the other size checks and repeated costs intact.

**Alternatives:** Raising `nativeOutputLimit` globally changes all command bounds and buffers. Per-file processes preserve the measured performance problem. Skipping repeat reads loses freshness. All are rejected.

Use one fixed `cat-file --batch` process per stored-content sample, input full validated OIDs only; object length frames raw binary content, with a validated trailing newline and EOF after the final request. No filters, mailmap, symlink following or all-object scan. Git documents ordered request/response content framing. [Git cat-file](https://git-scm.com/docs/git-cat-file)

Use a native `update-ref --stdin -z` transaction with explicit no-dereference option: start, queue old-OID deletion, prepare, inspect the locked target as a direct ref, revalidate protections, then commit or abort. `prepare` locks queued references; a transaction ending without commit aborts. This permits detection of a symbolic transition without ever writing its referent. The native-minimum fixture must prove these semantics; unsupported capability refuses, never falls back to unchecked deletion. [Git update-ref 2.46.0](https://git-scm.com/docs/git-update-ref/2.46.0)

## Provider discovery and proof

**Decision:** One explicitly selected remote/repository and exact local base per online run. Discover through the bounded closed-PR collection without an original-base-name filter, compare exact head OIDs, and individually read selected candidates. Optional OID-to-PR-number hints avoid scanning unrelated history. Neither list ordering nor branch-name equality is proof.

**Rationale:** The measured repositories have dozens of PRs, so this is a bounded practical batch, unlike one manually supplied PR per plan. Seven of nine merged-head SheetMill cases targeted intermediate stack branches; filtering by the final base name excludes them despite native integration-result ancestry. The same reported merge-result OID reaching the chosen base establishes the required transitive integration under the explicit provider attribution trust; a PR-chain traversal or patch-equivalence claim does not. Full selected-PR reads establish merged status and integration commit; stable repository/PR IDs and repeat reads bind it to the chosen local base. Multiple exact candidates need a hint. GitHub's commit-associated-PR endpoint is useful discovery but is not a universal completeness guarantee for rewritten historical heads, so it is not required by this initial algorithm.

**Alternatives:** Patch equivalence alone, PR titles/names, local upstream absence and generic search are rejected as deletion authority. Fork/deleted-head-repository ambiguity and non-GitHub providers retain explicitly.

GitHub defines post-merge `merge_commit_sha` for merge, squash and rebase methods. The provider's reported exact head is the selected trust input, not a cryptographic history certificate. [GitHub pull requests](https://docs.github.com/en/rest/pulls/pulls#get-a-pull-request). Repository PR listing permits omitting the optional base-name filter; bounded exhaustion remains mandatory. [GitHub list pull requests](https://docs.github.com/en/rest/pulls/pulls#list-pull-requests)

## Completed legacy worktrees

**Decision:** One-operation explicit completion claim, fresh apply acknowledgment and complete same-repository physical/native proof; no fabricated historical ownership. Retire the clean selected attached worktree through native combined removal without force or unlock; keep its branch for a fresh reference plan.

**Rationale:** The six supplied examples are clean including ignored scope but contain 301,117-byte SVGs. New content streaming is necessary; tree metadata alone is insufficient. Native locks and active/protected/dirty/foreign state remain blockers even with completion intent.

**Alternative:** Same-commit detachment preserves files but creates new HEAD/hook/index/recovery semantics and does not remove completed registrations. It is deferred and never a fallback when full retirement fails. Existing stw-owned v3 keeps original-journal/lock rules; legacy does not borrow them.

## Tracking-reference absence

**Decision:** A complete unfiltered GitHub branch collection, read twice during plan and freshly at apply boundaries, is the declared remote advertisement. Require exact standard fetch mapping and repository identity. Missing source permits only a recoverable local tracking-ref action.

**Rationale:** GitHub's unfiltered branch collection includes protected and unprotected branches. Single-resource 404 conflates absence and access; PR state is unrelated. Multiple URLs/custom/negative mappings are unsupported initially. [GitHub branches](https://docs.github.com/en/rest/branches/branches#list-branches)

**Alternatives:** `fetch --prune`, automatic Git transport and server deletion introduce additional effects and are rejected. Provider reappearance after a final sample remains a disclosed external race; native old-OID validation still protects local concurrency.

## Versioning and scope

**Decision:** Explicit `--contract 3` selects new result/store/policy semantics. Existing flag-free v1/v2 selection and saved records remain unchanged; new online flags cannot accidentally turn an old command into an online read. The v3 coordinator imports typed hosting reads; hosting imports no workspace mutation types. No schema publication build runs during source implementation.

**Resolved assumptions:** GitHub online OIDs are SHA-1 for this capability; offline native format handling remains as admitted. OS identity/time capability is required for v3 content proof; unsupported platforms retain. No new library, scheduler, process-activity inference or model-review engine is needed.
