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
