# WI4 offline release source verification

## Candidate and scope

Corrected source/test candidate: `2cbc81c1513525bf7e468e2b542d51e17684598d`, on the
assigned `codex/release-offline-verify-record` worktree. Independent review of the
final corrected evidence head remains required. The first reviewed candidate
`a6c9c5f096474d11f1937c4256061f29a23a907d` is preserved on the author's backup branch.
It received NOT CLEAR (one High and two Medium findings); this candidate corrects
those findings as recorded below. Spec 026 remains
planned globally. No release, tag, publication, consumer activation, product artifact,
image or documentation build was performed. Temporary Git tags, fixture files and
compiler-generated test executables belong only to isolated source tests.

The author restacked only the seven owned commits from
`c06c2f4749191faff343665ac64f6a2176f3315a` onto
`40b5befec12dac96e639ea58a6fbeda15eccd360`. Both base trees were
`3d2d025089a78beb2dcf8ebbc6e68146b06ffe91`; the seven `git range-diff` rows were
all `=` against preserved pre-restack head
`924a15c46c56b0a52f603dd72e9d60fed5f1bbb5`. No foreign commit, default branch,
remote ref or umbrella gitlink was changed. Subsequent explicit run-id presence and
verify argument/diagnostic and managed tag-backend alternative fixes were folded into their owned commits and received
new source checks; the historical range-diff describes the pure base change only.

The scoped [portable writer design](record-writer-design.md) received independent
strongest-tier CLEAR at `b706d0f7f2664790082c502b6db51dbbe8f7c9c2`, after one correction
round. That is design clearance, not implementation clearance. The controlling
[plan](plan.md) incorporates the exact pinned `golang.org/x/sys v0.47.0` adapter
exception. Only its existing indirect/direct classification changed; `go.sum` did not.

## Failing-first evidence and exercised behavior

| Task | Actual failing-first trigger | Executed source coverage |
| --- | --- | --- |
| T018 | `parseRawDiff` absent | streamed raw metadata, A/M/D, mode/type/symlink/gitlink/forbidden path/UTF-8 refusals, malformed/truncated records, 20,000 entries, 4 MiB and first-20 offender truncation; native filter/textconv sentinels |
| T019 | tag planning returned unsupported | annotated/lightweight tags, tag build mode, grammar, main ancestry, missing/invalid/noncanonical/oversized/forged record, C ancestry, descendant/on-main record commit R, C equality in R mode, source/config changes and replayed documentation commits with rewritten SHAs |
| T020 | checksum/artifact APIs absent | strict text/binary-marker checksums, duplicate/path/encoding rejection, missing/extra/mismatch/symlink/directory/FIFO/device inputs, sparse oversized file and cancellation; streaming artifact cap 2 GiB |
| T021 | Verify API and CLI registration absent | actual Cobra verify/record entry points, usage before reads (including 64-digit commit refusal), correct invoked-command diagnostics, exit 1 incomplete/refusal and exit 3 failed gates, human/JSON output, strict unpublished result schemas, hostile diagnostic escaping and GitButler/jj alternatives |
| T022 | Record and exclusive writer absent; positive result schema exposed union serialization bug | deterministic canonical bytes; manifest/config/commit/repository/run agreement (including explicit zero run-id equality/mismatch); checksum/file hash agreement; named OCI record; committed binary lock/tool-list hashes despite working-tree drift; exclusive no-follow write, equal no-op, check mode and exact one-new-file sentinel |
| T023 | verification matrix added after its underlying gates existed | every SC-004 local mutation through Verify: lightweight/not-main, code/workflow/config/mode/symlink/gitlink, tampered/missing/extra artifact and wrong version; stubbed online CI/existing-release/bad-attestation gate mutations; no real online transport |

Native effect sentinels show tag plan and verify leave repository contents, index,
refs, HEAD and configuration unchanged, trigger no hook/filter/textconv/fsmonitor,
and make no network request. Record write adds only its new file (plus permitted
absent directories), stages/commits/tags nothing and preserves every existing file.

The Linux/Darwin writer uses pinned directory descriptors and atomically refuses
symlinks at each component. Failing-first tests exercise restrictive umask (the new
file is mode 0644), short writes, interruption, close failures, every named-product
directory depth, deterministic component swaps, concurrent directory/file creation,
existing-file mutation, hardlinks, FIFO/directory/oversized destinations and descriptor
cleanup. An ancestor renamed within the fixture root retains its original inode:
the write follows that pinned inode and cannot follow the substituted symlink. This
is not a lock on directory names or a claim that another actor cannot relocate an
already opened directory. Native device-destination creation was skipped because
`mknod` returned EPERM; regular-only artifact hashing of `/dev/null` was executed
and refused. No unsupported platform write fallback follows symlinks.

AST and Python guards admit only the exact writer's direct Openat/Mkdirat calls and
explicit constants. Mutants cover both functions as captures, assignments, return
values, arguments and composite literals; aliases, wrong files, parenthesized calls,
package initialization, generic forwarders, production hook injection, unrelated
external/helper import routes, and existing process/network/unsafe/foreign-source
evasions still refuse. The closed dependency guard uses Go standard-library metadata;
the pinned Unix dependency's own platform wrappers are not mislabeled project source.

## Native lanes and source checks

Environment: Linux arm64, Go 1.27.2, Git 2.56.0,
jj 0.46.0 (`7d382314f9420b4b8d932245b49c99492c812b9f`) and
jj 0.45.1 (`7c41cdeb16b6b321c64e789a966b6adf723816a5`). `umask 022` preceded checks.

Executed targeted native jj coverage on both versions: verify refusal and committed
record reads in colocated, standalone and secondary workspaces, with unchanged
operation-log, stored working-copy identity and files; binary 160 KiB committed source
hashing beyond the buffered-read bound, despite different unsnapshotted working bytes.
Hashing retains only SHA-256 state, runs under the existing operation pin/sandbox/
deadline and bounded stderr, requires successful native completion and final state
resampling, and applies no invented artifact-size cap to committed lock/tool-list blobs.
Fake-process tests reject late failure, stderr overflow, cancellation and closed/non-file
readers; they do not establish native Darwin behavior.

At the corrected source/test candidate, architecture checks, all-Go formatting,
whitespace checks and source documentation checks passed (255 authored documents).
The Python suite passed 81 tests after asserting that exactly
`test_docs.DocumentationTests.test_packaging_excludes_source_and_binds_revision` was
excluded and absent before execution. That existing test performs a real Hugo build,
which remains unauthorized; raw `make check` was therefore not run.

At the corrected candidate, full `go test ./...`, full `go test -race ./...`,
`go vet ./...` and `go mod verify` passed with `GOPROXY=off GOSUMDB=off`.
The release package completed normal tests in 68.404 seconds and race tests in
75.454 seconds. Two final target-boundary mutation probes overlapped the first race
invocation; after their restoration, `git diff --exit-code HEAD -- cmd internal
schemas go.mod go.sum` proved exact committed source/test bytes and the full race
invocation passed again (cached). No source mutation occurred during that final run.
The final evidence commit changes only Markdown progress/evidence files.

The initial candidate's full Go and race suites also passed; the independent review
nevertheless demonstrated the three defects and insufficiently orthogonal fixtures.
Those passing historical suites are not evidence that those defects were covered.

Compiler-only test fixture checks passed for Darwin arm64 and amd64 at the source
candidate. The fixtures were not run. Native Darwin, Linux amd64, Git 2.46, external
GitHub behavior, real attestations, dedicated release smoke scripts, Node/browser
checks, a product build/rehearsal, live schema publication
and publication remain unrun. New verify/record result schemas remain in
`UNPUBLISHED_SCHEMAS` until WI9. Online verification has only an injected test seam;
WI5 owns real bounded evidence acquisition. Spec 025 hosting observations are unchanged.

An offline `go mod tidy` with `GOPROXY=off GOSUMDB=off` refused uncached transitive
dependency-test modules (`heredoc`, `gock`, `x/tools`, `check.v1`). No download was allowed
or made. The one directness classification was applied manually and module verification
passed; a complete tidy check is not claimed.

## First independent review and executed corrections

The first exact-head review at `a6c9c5f096474d11f1937c4256061f29a23a907d`
reported one High and two Medium findings, with no Low findings:

- An undeclared file/image target was accepted; an undeclared image target could
  retain an attacker repository and omit lock/tool-list hashes. Failing fixtures
  reproduced acceptance, then passed after trusted target membership and compatible
  kind checks were added before assembly and during editable-record validation.
- A tag ref swapped from an annotated first commit to a lightweight second commit
  between the type and peel reads combined unrelated observations. The deterministic
  swap fixture failed first; the resolver now pins the full immutable object ID once
  and performs both later reads on that ID. The fixture asserts that the ref swap
  actually executed and that the original annotated commit is returned.
- Existing artifact tampering changed length as well as hash; trusted manifest
  fields were not independently mutated; forbidden-path samples were already outside
  the allowlist. The strengthened tests hold size constant, mutate only one of
  commit/version/config/project/product with canonical manifest/checksums regenerated,
  and prove a forbidden path matches both the provided pattern and record path before
  rejecting it.

The separate image-repository semantic question received independent strongest-tier
assessment as faithful to the existing design. The minimal clarification in
[record.md](contracts/record.md) states that each asset target names a compatible
contract target at C and each image repository equals that target's repository.
Supporting contracts are shared target meaning (record lines 24/39), target kind and
repository (config lines 55/63), and record derivation (record-command line 25).
No requirement for target completeness, target uniqueness or other asset field
equalities was added. A failing unit fixture and a real Verify fixture established
that an edited image repository passed local gates while online evidence remained
incomplete; corrected validation rejects it with `record-invalid`. This is not a
claim of successful online verification or an online bypass.

Twelve actual, compiling guard-removal mutants each failed the intended test assertion:
artifact SHA comparison; forbidden allowlisted path; each of the five manifest
agreement fields; target membership; compatible target kind; the before-assembly
check; image repository agreement; immutable tag peel.
Logs are `/tmp/stw-wi4-correction-mutant-<rule>.log` in the author environment. The
config comparator mutant initially failed compilation from an unused local; that
attempt is not counted. Its corrected compiling mutant then failed the isolated
config assertion. Fixtures also cover both file/image kind incompatibilities and
reject a known file target used by an image before asset assembly.

Corrections were folded into only the author's corresponding logical commits;
foreign/base history was not rewritten. The preserved first review SHA remains
available for the reviewer to compare. Source and effects remain within WI4.

## Remaining handoff

Obtain independent strongest-tier review of the final WI4 head, including the corrections
above, and preserve any findings until corrected and re-reviewed.
No push or PR is authorized before that exact-head CLEAR; normal hosted workflows may
build documentation/toolchains/CLI artifacts, so their separate owner build authorization
also remains pending. Owner merge, tag, publication and consumer activation stay separate.
