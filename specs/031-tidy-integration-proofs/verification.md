# Verification: tidy integration proofs

Status: in progress. Only the tasks ticked in [tasks.md](tasks.md) have evidence here.

## T001 design gate

The specification had seven independent executed Opus review rounds ending CLEAR, then a Sonnet re-check CLEAR on the final content; it merged as PR #88 (merge commit `34805adf3c8ddc055daec95dde1f067a420be613`).

## T002 baseline

See "Stage2 baseline" in [research.md](research.md): paths and symbols confirmed at `37b56fbf`, schema checksums frozen by `TestPublishedV1AndV2SchemasAreByteFrozen`, stored-plan fixtures are test constructed, owners recorded.

## T003 (#80 part)

`TestTidyV1PlansHundredsOfRefsAndWorktreesOverBufferedBound` and `TestTidyV3PlansHundredsOfRefsAndWorktreesOverBufferedBound` (150 branches, 120 worktrees, for-each-ref 72,367 bytes, worktree list 118,239 bytes) failed before the change with `native Git workspace evidence exceeded the 64 KiB command output bound`. The #79 and #81 reproductions belong to later work packages.

## T004 to T006 bounded streaming reads (fixes #80)

- Added `internal/workspace/git_collection_stream.go`: `for-each-ref` and `worktree list --porcelain -z` stream through `executeNativeStream` and parse one record at a time for `Observe` digests, v1, v3 and the full reference map. `nativeOutputLimit` remains for non-collection reads.
- Boundaries tested through a helper process under the native sandbox: 1,024 records pass and 1,025 refuse `git-observation-limit` (both record kinds); 1,024 records of 4,224 bytes (4,325,376 stdout bytes, 65,536 stderr bytes) pass; 4,325,377 stdout bytes, 65,537 stderr bytes and a 4,225-byte record refuse `git-output-limit` naming stdout, stderr or the record; a truncated final record is `io.ErrUnexpectedEOF`; parsing 1,024 maximum records allocates a constant number of times and under 64 KiB (one record held). End to end: 1,023 references plan in v1 and v3, 1,024 references pass the snapshot read but refuse the aggregate Spec 029 bound, and 1,025 refuse `git-observation-limit`.
- Aggregate bound (decision): the collection reads admit 1,024 records, but the stored plan and result keep the Spec 029 aggregate 1,024-record bound (`withinTidyBounds`, `v3Bounds`, tracking evidence), so 1,024 references plus the primary registration refuse with the existing `tidy-observation-limit` (v1) or `record-limit` (v3). Raising it changes every stored-plan and result bound, so FR-025 and SC-002 were amended instead. End to end: 1,023 references plan; 1,024 pass `gitAdapter.Observe` and refuse the aggregate bound; 1,025 refuse `git-observation-limit` in the snapshot, v1 and v3.
- No buffering: `TestGitStreamRecordsDoesNotBufferStdout` bounds allocation while streaming 4,325,376 bytes at 1 MiB; replacing the parse in `gitStreamRecords` with `io.ReadAll` makes it fail (10,187,648 bytes allocated), then the mutation was reverted.
- Error codes: a truncated final record now maps to the path's own code through the production readers (`TestCollectionReadersMapTruncatedAndMalformedStreams`): `tidy-invalid-reference` (v1 refs and the reference map), `invalid-reference` (v3 refs), `tidy-invalid-registration` (v1 and v3 registrations); the snapshot digest read maps it to `git-query-refused`. Code changes against the buffered readers: more than 1,024 references or registrations in one collection was `tidy-observation-limit` (v1), `record-limit` (v3) or `tidy-invalid-registration` (v1/v3 registrations) and is now `git-observation-limit`; a collection over 4,325,376 stdout or 65,536 stderr bytes or a record over 4,224 bytes was `git-output-limit` (64 KiB) and keeps that code with a message naming the stream.
- Snapshot digests equal the SHA-256 of the buffered text (`TestGitObserveDigestsMatchBufferedOutput`); schemas v1/v2 are byte frozen.
- Measured on the 150/120 fixture (offline, so 0 requests): v1 plan 58 ms, v3 plan 66 ms.
- Results: `GOMAXPROCS=2 go test -count=1 -race ./internal/workspace` ok (449 s); `./internal/contracts ./internal/cli ./internal/hosting ./internal/inspection` ok; gofmt, `go vet ./...`, `go build ./...` clean; Python CI script tests OK.

## Known limit

`internal/workspace/retirement_git.go` (the `gitRead(..., "worktree", "list", "--porcelain", "-z")` call in the retirement apply path) still buffers at 64 KiB. Scenario: a repository whose `worktree list --porcelain -z` output exceeds 64 KiB (for example the 120-worktree long-path fixture used for T003) plans a retirement successfully with the streamed reads, but retirement apply then refuses with `git-output-limit`. No code change in this work package; a tracking issue is filed by the owner.
