# Quickstart: restack evidence and proposed local apply

**2026-10-25 scope:** hidden v1 plan exists in source; its schema is UNPUBLISHED
and minimum/current Git plus exact-head acceptance remain pending. Local apply,
restore and jj below are proposals, not available commands. This amendment runs
no product command/test/build. Use temporary repositories for later acceptance,
never a real owner's checkout as a destructive test.

## Existing hidden Git plan: validation scenario

The existing test `TestRestackPlanEndToEnd` and
`TestTheMotivatingScenarioIsEvidenceClean` supply temporary fixtures. Arrange
main=A, parent=B1 B2, child=C1 C2; make the new parent contain equivalent B1/B2
under rewritten SHAs. Capture the exact old parent and child head first. With an
already authorized source binary, the existing invocation shape is:

```text
stw restack plan PATH --branch refs/heads/child --old-parent OLD_PARENT_FULL_SHA
  --new-parent NEW_PARENT_FULL_SHA --expect-head OLD_CHILD_FULL_SHA
  --identity "Owner Name <owner@example.org>" --json
```

This is a syntax sketch with inert placeholders, not a pasteable shell command.
Complete clean evidence exits0, including all `=` pairing, qualified parent proof
and identity pass only with the explicit flag. An overlapping-path indicator is
risk evidence, never correctness. No identity flag gives unknown; conflict
prediction is a complete observation (exit0), refusal exit1, syntax exit2,
incomplete/bound/cancellation exit3. No ref/index/worktree/config/object-content
change, hook, helper or network occurs; the documented existing-object mtime
caveat remains. The prediction committer never predicts an actual persisted
rewritten committer; T024's reserved limitation is not yet emitted.

Later validation must cover dirty/occupied/unsupported/merge/unrelated and wrong
ancestor fixtures, conflict paths, empty replay commits, hostile names/config,
201-commit/2001-path/result-byte/deadline edges and cancellation. Compare content
sentinels over every native/worktree/neighbour boundary, not object mtimes.

## Human reference semantics (data only)

The manual reference sequence below explains the exact replay/proof intent; stw
uses D7 plumbing/quarantine/CAS and never invokes porcelain rebase:

```text
git rebase --onto NEW_PARENT_FULL_SHA OLD_PARENT_FULL_SHA BRANCH
git range-diff OLD_PARENT_FULL_SHA..OLD_HEAD_FULL_SHA NEW_PARENT_FULL_SHA..NEW_HEAD_FULL_SHA
git cherry NEW_PARENT_FULL_SHA OLD_PARENT_FULL_SHA
```

For a supported pure replay, every range row must be `=`, own diffs equal and
parent proof qualified. Clean replay can still break tests/semantics; equality
and an old identity pass never mean safe to push. Actual author/committer fields
must be checked on the new commits, as required by the workspace policy.

## Proposed local apply/recovery walkthrough (not runnable)

1. Resolve [owner choices](spec.md#needs-owner-decision), review the complete
   [apply contract](contracts/restack-apply.md), then implement and accept its
   red-first/native tasks. `--contract 2` and authorization spellings are proposed.
2. Choose exactly one owned topic ref, exact old parent/head/new parent, explicit
   actual committer/allowed identities and declared effects. The recommended
   first capability requires an unoccupied ref; an attached author worktree is
   explicitly OQ-6, not silently updated. Default is no mutation and no prompt.
3. Store/review the immutable future apply plan. Apply only with that id and
   explicit local authorization. Replay stays quarantined, computes the actual
   candidate, validates one-to-one all `=` rows and actual author/committer ids,
   prepares durable journal/root, revalidates and CAS-updates only the target.
4. Inspect the receipt's actual new SHA/mapping, partial effects, recovery pointer
   and derived change-class/pure-rebase policy input. Before push, docs-only
   rewrites require green local checks and pure-rebase equality without review;
   code requires independent exact-new-SHA review (middle tier allowed for equal
   pure rebase). Ready always requires independent exact-head CLEAR and green
   required checks. Prediction or prior-head CLEAR never covers a new head.
5. Inject crash/write/ref races in temporary fixtures at each boundary. Retry
   compares old/new/third value; a third value is changed-since, never reset.
   A separate [restore plan](contracts/recovery.md) authorizes only new→old with
   fresh guards. Recovery does not restore untracked work or remote effects.
6. Exercise [native jj capability refusals](contracts/jj-restack.md) until the
   operation/bookmark/recovery spike and both native versions pass. No raw Git
   fallback or original working-copy snapshot is allowed.

## Remote handoff and owner resync

D8: Spec 016 owns separately selected push/link/retarget plans and authorization.
Its agent pre-push gate follows the class-dependent table in the apply contract:
docs-only checks/equality without pre-push review, code independent exact-new-SHA
CLEAR (middle tier allowed for equal pure rebase), plus applicable checks, fresh
attribution/identity and full observed **remote** old SHA. The push identity
check covers observed remote-default..actual-new-head including open-parent
commits, trusted owner only (no caller-widened identity flags), with local/global
user-config origins reported and any foreign user identity refused; no config
write occurs. Ready always requires
independent exact-head CLEAR and all required checks green. Local old head
and a v1 inert command do not supply that advertisement. A changed head requires
new review; no local mutation authorizes a remote write.

Owner resync alternatives are output DATA only: fetch/prune, a named old-SHA
`rebase --onto` if the owner has local commits, or `switch` plus `reset --hard`
when the owner chooses to discard local-only/uncommitted work (destructive).
SourceSteward never executes these resync/reset commands. Spec 043 landing is
another human invocation; agents never merge, push default, tag/release or change
settings through this local workflow.

## Checks for this documentation amendment

Run only `python3 -I -B scripts/check-docs.py` and `git diff --check`, plus typos
if configured. Do not use the old Go test sketch as evidence that source tests
ran on this documentation revision. Actual new product/native fixtures,
minimum/current lanes, builds and runtime acceptance remain unexecuted here;
[verification](verification.md) preserves the historical results separately.
