# Unit C hosting evidence handoff

This private source handoff covers T013–T014, T018–T019 and T032–T033 only.
It does not establish integrated native proof, acceptance or release readiness.
Corrected focused checks pass; corrected aggregate checks and fresh independent
review remain pending.

## Frozen typed API

```go
type TidySessionOptions struct {
    Online, Apply bool
    Remote, Repo string
}
func OpenTidySession(context.Context, string, TidySessionOptions) (*TidySession, error)
func OpenTidySessionWithTransport(context.Context, string, TidySessionOptions,
    http.RoundTripper) (*TidySession, error)
func (*TidySession) Repository() TidyRepositoryIdentity
func (*TidySession) Budget() TidyHTTPBudget
func (*TidySession) Close()
func (*TidySession) RevalidateRepository(context.Context) error
func (*TidySession) DiscoverPullRequests(context.Context, []string,
    map[string]int) ([]TidyHeadEvidence, error)
func (*TidySession) ReadPullRequest(context.Context, int) (TidyPullRequest, error)
func (*TidySession) RevalidatePullRequest(context.Context, TidyPullRequest) error
func (*TidySession) ReadBranches(context.Context) (TidyBranchAdvertisement, error)
func (*TidySession) RevalidateBranches(context.Context, TidyBranchAdvertisement) error
```

Types are defined in `internal/hosting/tidy_evidence_session.go`,
`tidy_evidence_pr.go` and `tidy_evidence_branches.go`. Repository identity binds
host, numeric ID, node ID, canonical full name and default branch. PR evidence
binds that identity, PR numeric/node IDs and number, state/merged/merged-at/draft,
nullable head/base repository identities, literal head/base refs, full head/base
OIDs and nullable full integration OID. Branch advertisements bind complete
sorted name/OID pairs, repository identity and SHA-256 fingerprint.

The caller completes native layout/base/config/protection qualification before
opening the session. Session creation additionally requalifies ownership through
existing native inspection and repeated remote reads before any credential call.
It requires explicit online selection, one named remote with exactly one fetch
URL, Git/SHA-1 ownership and a GitHub.com repository matching optional Repo.
Git/SHA-1 and GitHub.com restrictions follow FR-005 and the online contract's
full SHA-1 requirement. Offline Git SHA-256 and all existing v1/v2/jj paths remain
unchanged. Unsupported online ownership/format/selection returns
`ErrTidyUnsupported`, never a successful no-match.

Discovery accepts 1–1,024 unique full lowercase native SHA-1 OIDs. Hint map keys
must be selected OIDs and values positive PR numbers; conflicting number mappings
refuse. Hinted heads bypass only list discovery; all other heads share one
complete closed-PR collection with no head/base filter. One result row per input
OID preserves input order. A nonnil PullRequest and reason `github-merged-head`
means the provider tuple qualifies for subsequent native merge-result ancestry.
A nil PullRequest has a complete negative reason: `integration-not-proven`,
`pr-head-mismatch`, `hosting-proof-unsupported` or `pr-ambiguous`. No observation
error returns a successful prefix. Errors mean incomplete/unavailable evidence;
`ErrTidyChanged` requires replanning. ReadPullRequest exposes complete typed facts,
including unmerged/deleted-repository observations, without making a native proof.

Native ancestry into the independently chosen base belongs to unit D. There is
no original-base-equality parameter, live intermediate branch lookup, patch
similarity fallback, native fetch or server mutation. Callers must resample
selected tuples before recovery and destructive boundaries. Apply uses stored
PR identity and never rescans the closed collection. Tracking callers collect
and compare two complete planning advertisements and resample at each required
boundary, including prepared native reference locks.

Every method and repeated sample shares the original 30-second plan/60-second
apply context, 128-request and 32 MiB consumed-raw-byte ledger. Parent context
cancellation also cancels the operation. Per-response/raw/normalized/result caps
remain 1 MiB; authentication channels are 16 KiB. Budgets never reset per head,
collection or method. Session creation returns a closed session on failure so
Budget remains available: authentication/native refusal has no attempted HTTP;
any request passed to the underlying transport sets NetworkAttempted. Close is
idempotent and callers must close successful sessions.

## Dependency integration and first regression

Parent was `9fc680cc2cb66c33127971368ca7e92f39c11281`. The reviewed release
transport revision `e8f48dc738c241ecf38fac25967a288a62b03b6d` was not assumed an
ancestor. Dependency commit `a42d4e1` copied exactly six reviewed files, before
any tidy changes. Every copied blob matched its source revision:

| File under internal/hosting | Git blob |
| --- | --- |
| release.go | 0c261ac1386a4eceb13986f970abedc02be81811 |
| release_review_test.go | 91312dcf2f83b5718587f8ab411a76061e26b3fd |
| release_rules_test.go | 030ad2e2c441ebd091ccdb7624316a9d59b99a04 |
| release_test.go | 2668a1c85a5244ec2ca4180c13a6debdf6205dde |
| release_transport_test.go | 2831f949aeb7da564f45859ae8253c585eb78910 |
| transport.go | 588312e1e1a0a6d18864f35e697f54153309352f |

Focused `go test ./internal/hosting` passed on that byte-identical dependency.
The first meaningful RED was TestTidyFixedReadRoutes: the original guard refused
all three required new forms (repository, unfiltered closed PRs and branches).
It passed after the narrow tidy GET predicate was added. Existing release and
inspection predicates remain unchanged; transport explicitly combines them
with the new predicate and confines new tidy routes to GitHub.com.

Nine exact-head PR fixtures include seven intermediate stack bases. PR and
branch tests were added before their methods; their initial runs reported the
missing methods, followed by passing focused fixtures. Complete negative rows,
identity drift, pagination/budget failures and source-only boundaries require
full fixture review before this unit is accepted. No real credentials, GitHub
traffic, user cleanup, builds, installation or publication are claimed.

## Review correction batch

Independent review of `d0b6b9c9683e293281576ba46d7c6ce52b17bb8a` was NOT CLEAR:
one High fixture-isolation finding, one Medium JSON-member finding and one Low
missing-Git capability finding. All three were corrected test first by the same
author, without changing the frozen API or expanding feature scope.

The first redirected regression run failed for the intended reasons. Aliases of
every admitted repository/discovery/PR/branch member were accepted, including
nested identities, alias-only missing exact fields and mixed-case duplicates.
`merged:false` plus `MERGED:true` could produce a merge proof; a `NAME` alias
could overwrite a still-live branch name. A shared exact-member validator now
checks typed proof members before Go struct decoding, including Unicode case
fold aliases. Exact duplicate rejection remains in place. Unrelated provider
fields, including nested metadata, remain accepted and cannot affect proof.

An actual test-binary regression also reproduced inherited `GIT_DIR` and
combined Git controls modifying a separate disposable repository. Inherited
HOME/XDG configuration broke fixture isolation. The native fixture now removes
all inherited Git control variables and isolates HOME, XDG and gh configuration
before its first Git setup command. It suppresses system/global Git config and
uses the resolved Git binary. The child-process regressions now succeed while
every file in the unrelated disposable repository remains byte-identical.
Missing Git produces an explicit ordinary fixture skip before setup; mandatory
native conformance requirements and scripts are unchanged.

Original redirected corrected receipts:

- `red-review-regressions.log`: meaningful pre-fix failures.
- `green-review-regressions.log`: all correction regressions and unrelated-field
  acceptance passed.
- `hosting-normal.log` and `hosting-race.log`: complete focused hosting suites
  passed (3.565 and 11.023 seconds respectively), including the actual test-binary
  isolation and missing-Git probes.
- `mutant-json-casefold.log`, `mutant-fixture-controls.log` and
  `mutant-missing-git-guard.log`: isolated source mutants each failed at their
  intended regression. `mutant-manifest.txt` binds their corrected source and
  changed file hashes. No working source was weakened for the mutant runs.
- Focused hosting vet, module verification, docs, architecture, formatting and
  whitespace checks passed in their separate original redirected logs.

The corrected full Go normal/race aggregate is pending the coordinator's quiet
native-test slot. Historical aggregate receipts below do not establish that the
new corrections passed those aggregates. No build, installation, real hosting
traffic, user cleanup or publication was performed.

## Preserved history and correction fold

The original candidate remains preserved as
`codex/tidy-hosting-evidence-before-fold-460c069` at
`460c069c0aadc10c8944920b5e311329f5fb29e8`. Its first Close-fix fold preserved
complete tree `0a20cddec86b50635ac78a8dbdfd64a25646e829`; the subsequent evidence
record was the reviewed `d0b6b9c` candidate. That reviewed candidate is now also
preserved as `codex/tidy-hosting-evidence-before-review-fixes-d0b6b9c`.

Review corrections were folded into the owning session, PR and branch commits.
Corrected pre-fold branch `codex/tidy-hosting-evidence-review-fixes-before-fold`
and folded head `862fdff6fc83dc0db8b5a24b14d2eaefcc0f3013` had the same complete
tree `31158c346d31962f7f002e2e5cd7f3a1ad8de9ef`, with an empty diff. This final
evidence refresh changes only this document; it does not change those corrected
runtime files. The exact shared parent
`9fc680cc2cb66c33127971368ca7e92f39c11281` and reviewed dependency
`a42d4e19be11c2459c1b52b39453d24df67de494` remain unchanged ancestors. Five focused
commits follow the shared parent: dependency, session including its fixes, PR,
branches and evidence. Fresh independent review must evaluate the final corrected
head; the prior NOT CLEAR is not a clearance.

## Historical source receipts before review corrections

Executed pre-fold source `c7e9d8c31363c4e3fcc0fafa278ff33df6ef67f3` includes the
session, exact-head PR and complete advertisement commits plus the lifecycle
regression fix. On that source, with `umask 022`:

- `go test ./...` passed, followed sequentially by `go test -race ./...`.
  Unchanged packages reused valid Go test-cache entries; changed hosting tests
  executed (3.454 seconds normal, 6.716 seconds race). The full normal suite also
  executed contracts (43.167 seconds) and workspace (50.484 seconds).
- `go vet ./...`, `go mod verify`, formatting, documentation checks
  (267 authored documents), architecture checks and `git diff --check` passed.
- Python source checks passed 82 tests. The selection asserted exactly one
  excluded case:
  `test_docs.DocumentationTests.test_packaging_excludes_source_and_binds_revision`.
  It invokes actual documentation packaging, which was not authorized. Synthetic
  fixture validation still ran; it does not establish actual Hugo packaging.
- Node was absent. Theme runtime tests were not run and nothing was installed.

The earlier exact source `93839b5a4987a9f9a0a6fda95e6edf567cb89d35` also passed
full normal and full race checks before the additional lifecycle regression.
These pre-correction receipts are historical and do not substitute for corrected
aggregate checks or independent re-review.

Additional meaningful failing regressions preceded their fixes: changed
pagination last-page totals were accepted; full terminal pages omitted their
already proven last-page link and were refused; a conflicting selected-remote
push repository reached authentication; and Close waited on the operation lock
instead of canceling an in-flight request. Each now passes, including the Close
regression under the race detector. Close cancels before waiting for serialized
operation cleanup, preserving immediate cancellation and idempotent cleanup.

The fixtures cover all required PR fields missing/null/wrong-type, all tuple
identity/ref/OID changes, forks/deleted repositories, unmerged/draft/amended heads,
ambiguity and hint validation; nine merged heads with seven original intermediate
bases; duplicate/truncated/malicious/changed pages; explicit 1,000-row exhaustion
versus a full unproven final page; complete fingerprints, branch reappearance,
unrelated branch changes, repository reuse, malformed/compressed/redirected/error
responses, cancellation, raw/normalized/result limits, request exhaustion and
exact 32 MiB consumption plus one overrun detection byte. Offline/unsupported
qualification makes zero auth/network calls. Fake gh processes exercise 16 KiB
authentication channel limits and redaction. One disposable native Git fixture
executes the actual session entry point and preserves its native identity; no
user repository or real credentials are used.

The five reviewed release files remain byte-identical to the handed-off source.
`transport.go` changes only the scoped tidy predicate combination and GitHub.com
restriction for those routes. Published v1/v2 schemas, old stored-plan fixtures,
old hosting/workspace results, dependencies and backend pins are unchanged.

Independent exact-head review remains required. This unit does not execute
unit D's native integration-result ancestry or prepared-reference deletion,
unit E's retirement/content acceptance, minimum/current native conformance
lanes, binary smoke tests, an actual site build, GitHub reads, user cleanup,
CLI/artifact/image builds, installation, publication or consumer activation.
The caller must use its original whole-operation context and complete native
configuration/origin/rewrite/protection preflight before opening one session;
the session's additional native ownership/remote qualification does not replace
that broader workspace authority. Selected push URLs must also identify the
same repository as the single selected fetch URL before authentication.
