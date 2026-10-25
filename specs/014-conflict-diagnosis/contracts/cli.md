# CLI and result contract

**UNPUBLISHED Markdown proposal.** No command below is implemented by this
artifact set; no JSON/schema file is added or changed. Implementation and native
conformance must precede publication. Hidden restack-plan v1 stays unchanged.

## Explicit operation selection

```text
stw conflict
stw conflict diagnose [PATH] [--revision FULL_NATIVE_COMMIT_ID] [--json]
stw conflict preflight [PATH] --semantics merge|rebase|restack --source REV --destination REV [--old-parent REV] [--json]
```

The group prints help and performs no observation, prompt or scratch write.
Diagnosis selects native existing conflict evidence. Git diagnoses current
unmerged index/operation metadata; `--revision` is a usage error on Git.
Native jj may select one full commit id or use its stored working-copy commit at
the sampled operation, with snapshot suppression. No revset expression or prefix
is accepted. The selector grammar and stored-state meaning must be confirmed by
the native capability study before exposure.

Preflight explicitly selects semantics. Git REV is a full object id of the
repository object format or a fully qualified local ref that resolves uniquely
to one commit; short names, abbreviations, `HEAD`, revision expressions and remote
URLs refuse as usage. Native jj REV is one full native commit id; the operation
and change ids are sampled/bound separately. `--old-parent` is required for
rebase/restack and forbidden for merge. All needed objects must already exist
locally: never fetch, lazy-fetch a promisor object or contact credentials.

No strategy/default inferred from repository configuration. The accepted native
semantic profile and its options are recorded; OQ-SEM/TOPO/SCRATCH gate profiles
before replay. This proposal does not silently select an owner answer. No
`--apply`, `--authorize`, `--yes`, provider or arbitrary-command pass-through flag.
No `gh jj` entry point is proposed by this feature.

A future accepted hunk capability may add repeatable exact-byte-safe selected
path input and an explicit context switch. The final selector/encoding syntax
belongs to OQ-HUNK; no placeholder CLI flag is advertised as accepted. Default
output has paths/types/participant identities only. An unrequested body is never
read solely for rendering. Source reads required by replay do not imply permission
to emit those bodies.

## Shared flag vocabulary and noninteractive use

Text output is the draft default; `--json` selects structured output. Neither
mode requires a TTY, reads stdin or introduces a prompt. Relative to the shared
vocabulary in Specs 041/043, `--human` and `--timeout` are not proposed accepted
flags here pending [OQ-FLAGS](../spec.md#needs-owner-decision); the current draft instead has fixed
60-second operation and 10-second native-process bounds. If an explicit text or
timeout selector is later adopted, recommend those shared names rather than
new aliases; that recommendation is not an owner decision or an accepted flag.

`--state-dir` is not applicable: diagnosis/preflight creates no saved plan,
checkpoint or evidence store. Disclosed private scratch is disposable execution
state, not a plan store. `--online` is not applicable and is refused: this feature
is offline and never refreshes supplied remote evidence. Specs 033/037 use
`--offline` for their existing online-default commands; those commands remain
unchanged. Since 014 is already offline, `--offline` is not proposed as an accepted
flag here either; any future interface choice needs explicit alignment rather
than an invented owner answer. OQ-FLAGS records recommendations and the unchanged
current proposal; it grants no flag admission. Under the current draft, all five unproposed or
inapplicable flags are usage errors (exit 2) before native reads or scratch effects,
including when no TTY is present. A later accepted flag alignment requires contract
and test updates rather than implicit inheritance.

## Outcomes and exit status

| Status | Prediction | Proposed exit and meaning |
| --- | --- | --- |
| complete | not-applicable | 0: diagnosis completed, including native unresolved conflicts |
| complete | predicted-clean | 0: selected supported kernel completed with no predicted conflict; fixed limitation applies |
| complete | predicted-conflicted | 0: Git first conflict or supported merge conflict observed; Git replay suffix explicitly not-evaluated; native jj aggregate remains OQ-JJCONF-gated |
| incomplete | unknown or qualified predicted-conflicted | 1: bound, cancellation, failed resample, missing optional selected evidence or scratch residue |
| error | unknown or not-applicable | 1: admission/refusal/unsupported capability; no usable prediction |
| usage | no result | 2: malformed selectors/flag combinations before reads/effects |

A failed output write exits 1; a broken stdout cannot promise a final complete
JSON document. Do not emit a second competing JSON error object. Existing hidden
Spec 015 `restack plan` incomplete exit 3 is implemented historical behavior, not
changed by this new 0/1/2 proposal (OQ-EXIT).

### Shared diagnostic vocabulary

Reuse Spec 015's implemented diagnostic names for the same shared replay facts;
do not create conflict-prefixed synonyms. Codes identify facts, while this
operation's outcome table controls complete/incomplete/error and prediction.
A refusal before a usable observation is error; an observed head change during
final resampling is incomplete/unknown even though the code is head-changed.
Existing ownership/admission codes also retain their meanings.

| Diagnostic | 014 fact and disposition; relation to 015 |
| --- | --- |
| repository-hazard | Mandatory configuration/metadata/attribute refusal; replaces proposed conflict-hazard. Same shared safety fact and no helper execution. |
| object-missing | Required local native object or history is missing/unreadable; replaces proposed conflict-object-missing. No fetch or invented identity. |
| head-changed | Expected Git head or selected ref target differs, including a resample; use this shared fact instead of conflict-changed for a head/ref move. |
| replay-failed | Shared Git plumbing cannot predict or create a scratch replay object; replaces proposed conflict-native-failed for that Git replay fact. |
| merge-commit-in-range | Initial Git linear replay contains an unsupported merge commit; no guessed mainline. |
| empty-moved-range | Git source-head equals old-parent, leaving no selected moved commits; no fabricated replay. |
| old-parent-not-ancestor | Git old-parent is not an ancestor of source-head; never widen the range. |
| unrelated-histories | Shared Git replay cannot establish the required common history; no invented base. |
| operation-in-progress | Reuse only if a selected capability explicitly refuses the observed operation state. Unlike the 015 target gate, diagnosis reads supported interrupted operations as normal evidence, not an automatic refusal. |
| symlink-hazard | Original repository/admin path or required metadata violates canonical/no-follow admission; no fallback path traversal. |

New 014-only facts use proposed names: conflict-topology-unsupported for a shape
outside an accepted native profile, conflict-input-invalid for a resolved
semantic inconsistency not covered above, conflict-isolation-unavailable,
conflict-limit, conflict-canceled, conflict-context-omitted and
conflict-cleanup-failed. Conflict-changed is reserved for layout/ownership/native
operation changes, never a duplicate head/ref-move code. Conflict-native-failed
is reserved for non-shared native observation/jj failures, never a synonym for
Git replay-failed. These names are UNPUBLISHED proposals, not a frozen ABI.
A refused capability names backend/version and a bounded actionable reason,
never a suggested fallback write or an instruction to disable confinement.

## Bounds and failure

| Bound | Proposed finite value and response |
| --- | --- |
| Operation | 60 seconds total including owned cleanup; native work stops early enough for cleanup; deadline during cleanup yields residue/incomplete |
| Native process | 10 seconds and 8 MiB captured stdout/stderr combined; read one extra sentinel byte to detect excess, stop/reap process, no raw diagnostic dump |
| Replay | at most 200 moved commits; exceeded input is incomplete, never a clean 200-commit prefix |
| Paths | at most 2,000 paths, at most 16 KiB per raw path; retain counted/omitted coverage without claiming unseen paths clean |
| Scratch | 512 MiB scratch writes enforced by quota/accounting boundary, stop before further writes; inability to enforce refuses capability |
| Context | at most 32 hunks, 4 KiB per hunk, 64 KiB total hunk content; requested excess incomplete with omission reasons |
| Diagnostics | at most 100 diagnostics, each sanitized message at most 2 KiB |
| Final output | 1 MiB total; reserve room for incomplete/omission fields before serialization; inability to fit minimal envelope is output failure |

Output is deterministic by native path byte ordering, then native participant
order; replay follows explicit oldest-first order. All limits are checked during
reads/allocation, not after unbounded collection. Cancellation starts no further
native step, stops/reaps in-flight work and attempts owned cleanup. No automatic
retry, background worker, prompt, editor, pager, hook, helper, network or model.

## Shared human and machine evidence

One typed model renders both modes. Human text escapes control/bidi bytes and
labels untrusted content DATA. JSON has exactly one final result conforming to a
future separate schema; error/incomplete results are covered as well as complete
results. Paths preserve bytes as described in [data-model](../data-model.md).
Never echo raw stdout/stderr, credential/config values, environment dumps or
agent transcripts. No field asserts verified, safe, ready, mergeable or authorized.

Every admitted result includes this fixed notice:

> Prediction is sampled conflict evidence only. It does not verify semantics, tests, readiness, future integration success or permission for any local or remote action.

This is proposed result structure, with an abbreviated empty diagnosis rather
than a successful preflight example; repository/binding values require their own
full native definitions in implementation:

```json
{
  "schema_version": 1,
  "kind": "conflict-evidence",
  "operation": "diagnose",
  "status": "error",
  "repository": null,
  "binding": null,
  "observations": [],
  "replay": null,
  "prediction": "not-applicable",
  "boundary": {
    "complete": false,
    "notice": "Prediction is sampled conflict evidence only. It does not verify semantics, tests, readiness, future integration success or permission for any local or remote action.",
    "qualifications": ["backend capability was not admitted"]
  },
  "scratch": {"state": "not-created", "handle": null},
  "diagnostics": [{"code": "capability-unavailable", "reason": "native capability unmeasured"}]
}
```

Fields in this Markdown example are UNPUBLISHED proposals, not a validator or
fixture. No existing schema, fixture, freeze digest or restack exit mapping changes.
