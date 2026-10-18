# Spec029 implementation verification

## T002 ownership and typed interface freeze

Baseline: `5bf3da530450f1cc556d5fc1cbf73a7b28d94c11`, following the executed
[T001 design gate](analysis.md). This is an implementation contract, not evidence
that the signatures or behaviors below are implemented or accepted.

- Unit A owns `internal/workspace/native_sandbox.go`, `native_stream*`,
  `native_blob_stream*`, `native_ref_transaction*`, `tidy_v3_budget.go`,
  `git.go`, `git_stream*`, and narrow setup/verification ledger edits.
- Unit B owns new v3 model/options/result/schema/store/journal files and tests;
  it reuses existing secure-store primitives, without changing old public types.
- Unit C owns typed hosting tidy evidence session/PR/branch files and tests.
  `transport.go` and `transport_test.go` require the separate release reader
  author's exclusive handoff; its unrelated branch is not assumed an ancestor.
- Later native planning/apply/retirement/CLI authors consume these primitives
  after explicit handoff. Existing manifest shared files retain one author.

No A/B/C author changes another unit's owned files. Existing result/schema bytes
and v1/v2 behavior remain frozen. New v3 public operation/result names follow
[data model](data-model.md) and [v3 contract](contracts/tidy-v3.md); CLI exposure
waits for integration. Unit B can start directly from this baseline without a
runtime dependency on unfinished A/C code.

Unit A freezes these package-private consumer signatures:

```go
type gitBlobRequest struct {
    Path, OID string
    Size int64
    Executable bool
}
func gitStoredBlobSample(ctx context.Context, bin, dir, format string,
    requests []gitBlobRequest, budget *tidyV3ProofBudget) ([]RetirementStoredFile, error)
func newTidyV3ProofBudget(apply bool) *tidyV3ProofBudget
func (b *tidyV3ProofBudget) beginObservation() error
func (b *tidyV3ProofBudget) consumeUserBytes(n int64) error
func (b *tidyV3ProofBudget) consumeMetadataBytes(n int64) error
func (b *tidyV3ProofBudget) consumeFramingBytes(n int64) error
func (b *tidyV3ProofBudget) consumeStderrBytes(n int64) error
func prepareGitRefDeletion(ctx context.Context, bin string, id NativeIdentity,
    ref, oldOID string, writeRoots, readOnlyRoots []string) (*gitRefTransaction, error)
func (t *gitRefTransaction) Commit(ctx context.Context,
    revalidate func(context.Context) error) (gitRefTransactionOutcome, error)
func (t *gitRefTransaction) Abort() error
type gitRefTransactionOutcome struct {
    Prepared, CommitAttempted, Committed, Uncertain bool
}
```

The ledger is operation-scoped and non-resettable: three plan/eight apply full
observations, including failures and detection bytes. Callers account both disk
passes and metadata passes through the same ledger; raw samples additionally
account their own bytes/processes/framing/stderr. Commit performs locked direct
readback before caller revalidation. The original context/deadline remains in
force; callbacks expose no arbitrary native/HTTP command route. Internal stream
parsers and launch factoring remain A-owned implementation details. Changes to
these signatures require coordinator handoff before dependent authors edit.

## Immutable compatibility baseline

Old stored-plan fixtures are constructed inline, so fixture-bearing source tests
are checksummed alongside old schemas and result types. Hashes below are SHA-256.

| File | SHA-256 |
| --- | --- |
| `schemas/v1/hosting.schema.json` | `5cf8bd2e67e209ee5f39befc2a0b0765a1f0fa1bb8082a940d1993e360133ff7` |
| `schemas/v1/inspect.schema.json` | `8e12ee38a3e065e1f2e47b88ec4b1668cd8a32d8879e4803870e8994a9cfcfae` |
| `schemas/v1/inventory.schema.json` | `8a8aeb281ca24c91809ea8fc95e83655d5a459afce51b1993965c9fa25a805f8` |
| `schemas/v1/pins.schema.json` | `1ae773a4fa0c4d08a9948ff050600505165323865a44b213ae91b205204c055b` |
| `schemas/v1/release-build-manifest.schema.json` | `ca9fc9e2087d421d7bc0be1bc341fcb76248af6668f7719b3260bd9d949a84fd` |
| `schemas/v1/release-check.schema.json` | `41a55b82bccc3efbdaaa542a4195eb0fe9c2f16eb4bba539d633f42c23054b73` |
| `schemas/v1/release-config.schema.json` | `73ef31f244adfc57d32f80d0f67295a58efd96b1841547d5961e28f5e99ff7ee` |
| `schemas/v1/release-oci-result.schema.json` | `9286b63a73c9eb341c0546bacc3ef5372bf30603d650d84021f160068cf8ed6a` |
| `schemas/v1/release-plan.schema.json` | `cdeca1858414b8cc7ad214c200e3465d598c8c84eae7d2a1babe1aada7b81d23` |
| `schemas/v1/release-record.schema.json` | `bd1c94371b3cf7489fdf1cce2f38b896317a0a3aecd054e1a1fbbd15bfb8192e` |
| `schemas/v1/repos.schema.json` | `05b1ef5a78cb3b3dbc87a32c720188c0fd46f574cd742d4df9f5cad6d09c6182` |
| `schemas/v1/tidy.schema.json` | `7b32f169e81b5351c9b542af7b09025a068279de98f3441ec8966d1239ec1161` |
| `schemas/v1/version.schema.json` | `886864837547ded4992af89e8e8e04d38fe7bf77724f91a8960a184c51a0659f` |
| `schemas/v1/workspace.schema.json` | `fb4d4623c7b3f329e596e7a3608116d10d3bb3c6801aaece6a073e154ccfd825` |
| `schemas/v2/inspect.schema.json` | `3824dbf58ee99eaa184218c292a8eff0e7c7ee1b7f455a77d9c1977f1478f595` |
| `schemas/v2/tidy.schema.json` | `c964a85de0941f41ceef84b6cc57235de984c7084ea1b4bacc8ebd799de19f2d` |
| `internal/workspace/tidy_result.go` | `62961450d864c8513f09729b8f63c624ef3a7187cac5f610fc0960aa00dca1a8` |
| `internal/workspace/tidy_apply_result.go` | `3271883139c1d3994524c650c7196165a769748442057ad41c822f37eba5ac71` |
| `internal/workspace/tidy_retire_result.go` | `bb995c066659c13eeccc911626ff84108eeddf6d83eb96138b65e4a97ff6ffff` |
| `internal/hosting/result.go` | `f07033f643736bea1dbfb49d66b186f6e7c1822b32051535a375cbed53deffb1` |
| `internal/workspace/state_test.go` | `2516b4cd4a0722e48e6e05fcc3bfcfbc697e8de075f995c2e20a58a9ae28e3f3` |
| `internal/workspace/tidy_apply_test.go` | `83ca162b8dfe09947a2244c5f80ba868d302cc3469bc2711494925e4d7332741` |
| `internal/workspace/tidy_retire_test.go` | `746c3ade73171f5a0417ba708832195297974a079fadf01fc9ccab92912773de` |
| `scripts/backend-releases.json` | `e1584597248048d845934f51ee2bd19949d1854d18fbc106c74b3df90c761e9f` |

## Backend pins and observed environment

`scripts/backend-releases.json` declares Git 2.46.0/2.56.0 and jj
0.45.1/0.46.0, with exact jj runtime identities. Pins are unchanged. Initial
available commands report Git 2.56.0 and jj
0.46.0-7d382314f9420b4b8d932245b49c99492c812b9f.
Minimum-version execution remains unverified until its declared lane runs.
No build, download, install, publication, activation or real tidy execution is
part of this source slice.

## First source increment: T003–T004 and T015–T017

Author candidate runtime/test revision:
`d87be8d488ad1b4e7783e1b7d7f3628b5d5b075c` (R1 corrected). The final verification-only
commit leaves those source bytes unchanged. Independent exact-source review is
pending; this record does not award runtime CLEAR or complete Spec029.

The coordinator explicitly handed Unit A only `gitRetirementStoredFiles` in
`internal/workspace/retirement_git.go` plus necessary import cleanup. Its tree
capture now consumes the same complete validated collector. Other retirement
functions, old per-file raw-content reads, index/registration metadata caps and
manifest policy are unchanged. New v3 content/retirement consumers remain later
work, not part of T017.

Meaningful RED: before runtime changes, the disposable exact 695-leaf,
67,877-byte fixture failed `TestGitCompleteMetadataAdmitsMeasuredOrdinaryBase`
at actual `gitAdapter.Observe` with `native Git workspace evidence exceeded the
64 KiB command output bound`. The same test now passes. The collector uses
fixed full-OID `ls-tree` and complete stdin `check-attr` streams, validates final
records/triples/EOF/native exit, and fingerprints complete raw evidence in the
previous deterministic order. A native digest-equivalence fixture passes.
Only these tree/filter channels receive their reviewed 4,224-byte logical
record and 4,325,376-byte stdout limits. Other buffered channels retain 64 KiB.

Executed native Git 2.56.0 acceptance on disposable repositories:

- Exact 695-leaf/67,877-byte base passes observation, creation plan/apply,
  existing v1 reference tidy, old v2 owned-target/base retirement observation
  and retirement planning; actual CLI planning retains schema v1.
- That full retirement positive configures native index-v4 compression only
  inside the fixture: its linked registration index is 45,359 bytes, below the
  unchanged 65,536-byte metadata cap. This is no product configuration change.
- A separate actual linked-worktree fixture using the default index produces
  a 72,315-byte index. Complete tree/stored-content/index collectors pass;
  independent registration metadata admission refuses. No cap is bypassed.
- Final unsafe/aliased/OID-invalid records, active final-path filters, changed
  final-path safe attribute evidence, incomplete delimiters/triples, extra
  fields and exact/over path/leaf/blob/aggregate/record/stdout bounds are covered.
  Stale creation refuses before effects and leaves the destination untouched.
- Concurrent stdin/stdout/stderr, independently capped stderr, a single
  detection byte, parser failure, blocked stdin/stdout, cancellation and direct
  child reaping are exercised through canonical sandbox launch admission.
  Existing sandbox write/network/environment regressions pass unchanged.

### Independent review correction and aggregate-check history

Independent review of `e98e22f02f5c40252b642802609924643a8d5f41`
reported Medium R1: checking the returned field length did not constrain
`bufio` read-ahead across a filter triple. The counter regression failed before
implementation: 8,329 underlying bytes consumed against a 4,225-byte permitted
record-plus-detection bound. The corrected reader bounds every underlying read
by the current logical record's absolute end plus one detection byte, including
all three filter fields. It advances that budget only after the full logical
record, accounting for bytes already buffered. The same regression now consumes
exactly 4,225 bytes; buffered-prefix/reset and tree-channel counter regressions
also pass. No stream/record limit or other guard changed. The correction is
folded into its owning metadata commit; shared setup `9fc680c` and stream
admission `18bbba9` remain unchanged. Independent correction review is pending.

Earlier aggregate race failures remain evidence. One new test helper initially
collided with the Go race runtime's one-second shutdown delay; only its test
deadline became three seconds. A later full run at `e98e22f` failed the large
retirement fixture after 16.00s with `retirement-stale-source`. That old error
collapses either a failed native source query or unequal snapshots into the
same message; its hidden cause cannot be recovered from that log. A temporary
compiler-overlay diagnostic of the unchanged pre-R1 runtime then passed in a
quiet window (fixture 5.24s, race command 6.254s): all four observations had
nil native errors, nil context errors and equal final source snapshots, taking
1.098–1.144s each. Concurrent-load/deadline causation remains an inference,
not a confirmed root cause. No additional production change or deadline increase
was justified or made. Original failed and diagnostic logs/receipts are retained.

Executed source checks on the frozen R1-corrected candidate, sequentially in the
coordinator's quiet window on 2026-10-18:

- `go test ./...`: PASS, exit 0; workspace 64.978s, aggregate 90.860s.
- `go test -race ./...`: PASS, exit 0; workspace 75.987s, aggregate 83.074s.
  The receipt confirms the head stayed exact and the worktree stayed clean
  throughout both full commands. No race was reported.
- `go vet ./...`, `go mod verify`, documentation (266 authored documents),
  architecture and `git diff --check`: PASS.
- Python source suite: 82 PASS. Exactly
  `test_docs.DocumentationTests.test_packaging_excludes_source_and_binds_revision`
  was excluded because it executes an actual documentation build, which lacks
  authorization. Other synthetic artifact-validation tests use source fixtures.
- Node is absent, so `node --test scripts/test_theme.mjs` remains unexecuted;
  no install was attempted. Consequently the aggregate `make check` is not
  claimed as passed. No build/install/CI dispatch/push/PR/release/publication,
  consumer activation or actual user-repository tidy was executed.

Compatibility SHA-256 baselines above remain unchanged. Minimum Git 2.46.0 and
jj 0.45.1 native lanes, macOS and the broader Spec029 acceptance remain pending;
installed current-version source regressions do not stand in for those lanes.
T005–T008 raw-object/budget/prepared-transaction primitives and all new v3
runtime consumers remain unimplemented in this first source increment.

### Retained check-output fingerprints

SHA-256 binds these locally retained raw outputs and receipts without publishing
private workspace paths. The coordinator has their exact local locations.

| Evidence | SHA-256 |
| --- | --- |
| R1-corrected full source log | `ad81877e99834f683ddadbc4fedfd14522474aaca111e0a795c65d0d6a42fcfd` |
| R1-corrected full race log | `097b33a48aa7f3977f29d182b893cde711314724ef6e2a6528032cdd3630e332` |
| R1-corrected frozen-head receipt | `017c04a20a72b2cf22bde455267b0e80148fd943ffa9e6be273b060837006163` |
| Earlier e98 full-race failure log | `08af66ff85416c7492cc17e6603dae7a89ac610bcb3c02ee02b6098e9ee0d750` |
| Earlier e98 failure receipt | `68e894394c769b5fb5b9cebf7cab67a2ccb758d76982b4caa119a4d20de88a3c` |
| Quiet pre-R1 compiler-overlay diagnostic | `4439f13b476e617129b03ed00e39f81dc93359acb8e4b4540bbaee5747ab972a` |


## Unit A continuation: raw-object and prepared-reference primitives

This increment implements T005–T008 as private primitives only. It exposes no
CLI mode and does not integrate new v3 reference/retirement consumers. The
cleared metadata revision `d0ece2344db4b1ea43adb553fe8c0833ee580f69` is preserved
unchanged as the parent. Minimum Git 2.46.0/jj 0.45.1 and macOS remain gaps;
current native Git 2.56.0 tests do not substitute for them.

### Raw-object/budget handoff

The frozen `gitBlobRequest`, `gitStoredBlobSample` and budget constructor/
consumption signatures remain unchanged. A raw sample validates all requests,
then starts one fixed `--no-replace-objects cat-file --batch` process. Each
response must have the next exact OID, literal blob type, admitted decimal size,
exact binary payload, LF trailer and final EOF/success. Duplicate OIDs for
separate paths are requested and hashed repeatedly. Headers read without
prefetch into binary payload; their 127-byte allowance plus one detection byte
preserves the per-object 128-byte header/trailer bound. User reads are at most
64 KiB. One sample remains limited to 1,024 files, 16 MiB/file and 64 MiB/user
payload; old v2/jj per-file 64 KiB paths are unchanged and call no new primitive.

The operation ledger reserves each channel and global byte allowance before
underlying reads, including concurrent stderr. A shared detection reservation
serializes EOF/overrun probes at exhausted bounds. Actual failed bytes count;
no sample, observation or failed launch resets the ledger. `RawProcesses`
charges launch attempts, including failed starts, conservatively before launch.
All-pass limits remain 3/8 observations, 9/24 user passes, 3/8 raw launches and
3/8 times (320 MiB + 320 KiB), with the reviewed per-channel maxima.

Additive package-private APIs for later D/E consumers:

```go
func (b *tidyV3ProofBudget) beginUserPass() error
func (b *tidyV3ProofBudget) beginMetadataPass() error
func (b *tidyV3ProofBudget) reader(context.Context, int, io.Reader) io.Reader
func (b *tidyV3ProofBudget) snapshot() tidyV3ProofCounters
func (t *gitRefTransaction) outcome() gitRefTransactionOutcome
```

Call `beginObservation` once per full observation. Raw sampling reserves its
one native launch and begins its stored-user pass itself. Later disk helpers
must explicitly begin each of the other two fresh user passes and four physical
pointer/registration metadata passes per observation. Wrap reads with the same
ledger's `reader` using `proofUser` or `proofMetadata`; retain separate validated
per-file/per-pass bounds, no-follow identity checks and the original context.
The `consume*` methods record already-consumed bytes; they do not replace
pre-read reservation and must not double-charge bytes the reader already counts.
`snapshot` reports actual channel/total bytes, maximum read request, observation/
pass counters and conservative raw launch attempts. None are stored authority.

### Prepared-reference handoff

`prepareGitRefDeletion` validates current native identity, layout/config/hooks,
full target/old-OID syntax and exact existing sibling protections before starting
one fixed sandboxed `update-ref --stdin -z` session. It sends start, no-deref,
old-OID delete and prepare; only native protocol phases that emit acknowledgments
(start/prepare/commit/abort) are expected to acknowledge. Bounded exact phase
responses, complete EOF/exit and independently bounded stderr are required.
The prepared lock is followed by direct-ref readback, repeated again before
mandatory caller revalidation in `Commit`. Commit retains the constructor's
original deadline and any tighter caller cancellation/deadline cause.

The caller must durably create/confirm committed-history recovery before this
primitive; it does not create or remove recovery. A started-but-failed prepare
may return a nonnull session with an error: inspect `outcome()` for confirmed
preparation and uncertain locks/effects. A lost commit response never implies
no mutation or rollback. Abort and close/reap are bounded by the original
context; uncertain locks remain for manual inspection, never blind removal.
Successful aborts preserve actors' changed targets and unrelated/recovery refs.
Native cooperating writers are locked out; raw filesystem/ABA races after
sampling retain the existing disclosed limitation.

### Author-side behavior evidence so far

Meaningful REDs preceded implementation: a valid native 262,153-byte binary
sample with duplicate OID paths refused as unimplemented; an empty ledger
admitted a fourth plan observation; all four loose/packed commit/abort cases
refused without the prepared primitive. The implemented focused race suite
passes, including binary NUL/LF/non-UTF8 hashing, exact 16/64 MiB bounds, native
1,024-object input and one process, wrong order/type/OID/size/framing/EOF/read
errors, zero progress, pre-read channel/global detection bounds, concurrent exact
EOF, 3/8 observations and 9/24 passes. Transaction fixtures prove native prepared
locks, competing writer refusal, old-OID CAS, symbolic/OID readback refusal,
abort, helper/write-grant admission, deadline propagation, cancellation and lost
commit acknowledgment with recovery preserved. Final focused native/ledger/
stream race command passed (6.873s). Full required source/race commands for this
new increment remain pending the coordinator's quiet window; no aggregate pass
or independent runtime clearance is claimed by this handoff.


### Unit A continuation: first review corrections

Independent review of initial foundation head
`32f12a35de97e5104909e43b1e8fbeb19c8f08a1` returned NOT CLEAR with exactly
Medium R2, Medium R3 and Low R4. Its ordinary/full race commands passed, but
independent deterministic probes found gaps those suites had not covered.
The original head and complete raw check receipts remain preserved.

All three probes were added as failing owned regressions before correction:

- R2: a held 10-byte reservation and two concurrent probes consumed 12 bytes,
  exceeding cap plus one. Both global and same-channel cases reproduced it.
  The ledger now waits for reservations occupying the limiting capacity to
  settle before allocating an overrun probe. Waiting honors cancellation and
  performs no source read. Corrected probes consume zero while capacity is
  reserved; the completed read accounts 10, then one final detection byte makes
  11 and permanently refuses further reads.
- R3: a tighter caller deadline after native commit acknowledgment returned an
  uncertain outcome while losing `DeadlineExceeded`. The final EOF/exit error
  now preserves the linked caller's context error/cause. The test confirms
  commit-attempted=true, committed=false, uncertain=true, unchanged recovery and
  an unexpired parent context; no timeout was increased.
- R4: a final `(1, bound-error)` EOF probe counted its detection byte but masked
  the bound error as a generic protocol violation. Non-EOF read errors now
  retain their identity before extra-data classification. Both operation-budget
  and native-stream bounds remain identifiable; a 49-byte frame plus its one
  detection byte accounts exactly 50 bytes.

The final focused raw/ledger/transaction/inherited-stream race command passes
(7.066s). Three independent temporary compiler-overlay guard mutants each
remove only the relevant correction; every mutant fails its expected runtime
regression, rather than failing compilation. This is author-side correction
verification, not independent clearance. The corrected full normal/race commands
remain pending the next coordinated quiet slot.

Retained original aggregate and guard-mutant evidence, SHA-256:

| Evidence | SHA-256 |
| --- | --- |
| Initial foundation full source log | `26dcac2ad185663d5d9cc18473dcd93c67ec259b294a655c6a16d9da91ddfe2b` |
| Initial foundation full race log | `4a9db912383e841f6e35f018a8fb03acdbb10c5bd2a8f4c009bc4f5068e3d140` |
| Initial foundation frozen-head receipt | `373bc7cdfe7831c88804f50f9989bf397b4ab5556fd20283f055a281858b415b` |
| R2 guard-mutant runtime failure | `68d6a39d384f58c7daa3d6a62435da20c8e89f674ae7d4d86e5f70c6d43eadec` |
| R3 guard-mutant runtime failure | `7e72fdb47f0d13763d4e36b1fbe1c6e412041ba6d66283997cd872bef45eb043` |
| R4 guard-mutant runtime failure | `2d55b2f7702abda5e022f4c6cf58cef9306e937a780a114f10b7e403a27268d0` |
| All-three mutant receipt | `7486abcfe52e84b281367dcdfbd0a0ada2a8abbcc08dd09ae799f23c9214b1fe` |
