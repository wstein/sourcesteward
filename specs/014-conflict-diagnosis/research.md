# Research: Conflict diagnosis and isolated replay evidence

**Date**: 2026-10-25. Source/documentation inspection only. No new native
experiment, backend acceptance, build, product network request or product test was run.

## R1 — existing source is evidence of plumbing, not global support

**Decision:** reuse the settled Spec 015 D7 direction, with a reviewed neutral
seam only after characterization. `internal/restack/replay.go` implements
`merge-tree --write-tree` and `commit-tree` in a private object quarantine;
`internal/cli/restack.go` is hidden and retains incomplete exit 3. Existing
`schemas/v1/restack-plan.schema.json` is frozen and UNPUBLISHED. Do not change it.

**Rationale:** Spec 015 documents a historical Git 2.56.0 scratch experiment where
quarantine writes left original object content unchanged. That single measurement
does not establish supported minimum/current platforms, merge strategy parity,
pack thresholds or native jj replay. The old plan's fixed prediction committer
is not evidence about an actual apply committer or an authorized identity.

**Alternatives considered:** porcelain rebase and perform/abort in an original
worktree are excluded by D7 and FR-011/FR-012. D7 is settled, not an owner question.
A raw `merge-tree --write-tree` without quarantine can write original objects;
command naming alone cannot establish read-only effects.

## R2 — diagnosis and scratch preflight are separate effects

**Decision:** diagnosis reads existing native conflict state; preview discloses
private disposable writes, including isolation/probe setup outside originals.
Original trees, indexes, refs, objects, config and jj operation log stay read-only.

**Rationale:** constitution I–V and Specs 008/009 distinguish stored jj state from
unsnapshotted disk. An interrupted Git operation and a first-class jj conflicted
change need different evidence. Dirty originals do not imply a snapshot or stash;
a metadata observer can omit unknown disk context without claiming it clean.

**Alternatives considered:** a linked Git worktree or new jj workspace sharing the
original store is not sufficient isolation. Native jj scratch-store copying/import
and read-only source access remain unmeasured OQ-SCRATCH, not a Git fallback.

## R3 — semantic profiles must be measured and selected

**Decision:** bind full identities, explicit operation, base/mainline interpretation,
ordered moved range, native strategy/options and capability revision. Recommend
initial single-parent linear replay and a separately accepted two-head,
single-merge-base merge profile. Restack/rebase intent uses the same explicit
old-parent..source-head onto new-parent kernel, not implicit branch inference.

**Rationale:** rename rules, multiple bases, empty commits and native jj multi-parent
or descendant rewrites can change predictions. Spec 012 stack drift evidence does
not choose an integration strategy; missing objects must never cause fetch.

**Alternatives considered:** silently treating merge as replay, selecting a merge
parent heuristically, using native default strategy from repository config, or
reporting predicted-clean from a truncated replay. OQ-SEM/TOPO block unaccepted
variants, with an explicit capability-unavailable result.

### Native jj conflict evolution remains an owner decision

The [official jj conflict guide](https://www.jj-vcs.dev/latest/conflicts/)
describes stored conflict commits and descendant rewrites. The
[technical conflict model](https://github.com/jj-vcs/jj/blob/main/docs/technical/conflicts.md)
explains term simplification, including cancellation that can remove a conflict.
Those semantics differ from the Git stop-first kernel. OQ-JJCONF retains the
choice of per-revision and aggregate evidence; native version/profile measurements
must establish complete bounded graph coverage. No 014 native experiment proves
that contract, and a clean final tip is insufficient evidence for every revision.

## R4 — resolver context is a privacy capability

**Decision:** propose body-free paths/types by default and selected-path bounded
hunks only after owner policy and native measurements. Non-UTF-8 path identity
must survive machine output; unsafe terminal text is escaped. No raw native stderr,
config values, environment dumps, token, transcript or executable instruction.

**Rationale:** source text can contain credentials or hostile instructions. Hunk
locations and filenames can also be sensitive; secret scanning cannot guarantee
non-disclosure. Stored blob, disk file and scratch conflict content must never be
presented as interchangeable or as proof that a conflict is resolved.

**Alternatives considered:** default full-file context, automatic AI resolver/provider,
heuristic marker parsing as authoritative conflict state, or claiming comprehensive
redaction. All are excluded. OQ-HUNK governs the actual disclosure policy.

## R5 — toolbelt consumers own their consequential gates

**Decision:** isolated replay evidence may be consumed by Spec 015 after exact
binding comparison and fresh mutation guards. This feature creates no durable
checkpoint/journal/recovery root and retains no candidate objects. Specs 013/015
reuse the proposed typed Spec 032 recovery seam for their own mutations.

**Rationale:** Spec 011 protects reference/workspace ownership; Specs 020/022
bound explicitly selected hosting reads; Specs 025/030 explain PR/CI evidence
without merge/readiness guarantees; Specs 041/042 mode/attribution do not grant
mutation authority; Spec 043 is the owner's separate landing invocation.
Spec 040 requires pure derivation with concurrency only at the reviewed edge.

**Alternatives considered:** observation implies apply, local apply implies push,
or clean prediction supplies publication/Ready evidence. None is valid.
[Spec 012](../012-stack-inspection/spec.md) OQ-NET remains open on online-by-default versus explicit
`--pull-requests` opt-in reads; retaining explicit opt-in is its recommendation,
not a recorded owner approval. This feature does not resolve or amend that choice.
014 performs no network; supplied remote context is external sampled data without
a refresh.

## Unresolved research

The OQs in [spec.md](spec.md#needs-owner-decision) retain real unsettled
semantic/topology, native jj conflict evolution, hunk, scratch and exit choices with recommendations. There are
no invented owner answers or new approval flows. Their affected implementation
units cannot start until their decisions and measurements are recorded.
