# Research: PR status

## Primary API semantics (2026-10-18)

[GitHub REST: Get a pull request, API version 2022-11-28](https://docs.github.com/en/rest/pulls/pulls?apiVersion=2022-11-28#get-a-pull-request) documents nullable `mergeable` while background computation runs, and a test-merge `merge_commit_sha` before merging. After merge its meaning depends on the method: merge commit, squash result, or the commit to which rebasing updated the base. This supports reporting separate raw/result/head identities; it does not establish current ancestry or review validity. The response example includes merged, merged_at, closed_at and merge_commit_sha. Missing/inconsistent required members are conservatively refused by this design, rather than given undocumented defaults.

[GitHub: Merging a pull request](https://docs.github.com/en/pull-requests/how-tos/merge-and-close-pull-requests/merging-a-pull-request) documents new commit identities for GitHub rebase-and-merge. This supports the inference that review of an old head cannot simply be attached to its different post-merge result. No live GitHub/Enterprise/permission/keyring timing was measured. The existing client pins 2022-11-28; no API-version change is proposed.

## Local implementation evidence

At base `2e9b9fc99888b605bcce08afa6ef98846359e06a`, `readPR` is also CI's reader and resample. It requires head/base repositories and 40 lowercase hex SHAs. V1 accepts extra properties: no additionalProperties restriction; a contract test explicitly accepts `future:true`. The initial checkpoint's strict-v1 statement is corrected in this revision. New guarantees motivate the proposed explicit v2; current validator strictness does not.

## Bounded delivery experiment

[Go1.27.2 os documentation](https://pkg.go.dev/os@go1.27.2#File.SetWriteDeadline) limits deadline support to suitable files and allows partial writes on timeout. An arbitrary io.Writer has no cancellation API. A writer goroutine with a timeout would therefore retain a possibly blocked goroutine.

A temporary test-only file executed with `go test -count=1 -v /tmp/stw-spec030-output-*/sink_test.go` on this session's Go1.27.2/linux/arm64 measured:

| Effective sink | SetWriteDeadline result |
| --- | --- |
| inherited captured stdout | unsupported |
| owned os.Pipe writer | supported, reset succeeded |
| ordinary temporary file | unsupported |
| /dev/null | unsupported |
| terminal | unavailable: /dev/tty could not be opened |

The probe only set/reset a deadline; it did not establish a complete watcher or blocked-pipe cancellation implementation. It produced no CLI artifact. Linux terminal, macOS sinks and ordinary inherited terminal/pipe adaptation remain unverified. Sink names alone cannot establish capability. These measured results block claiming generally usable watch under delivery option A. No inheritance/flag manipulation workaround is authorized or validated by this experiment.

## Round 1 independent review and proposed final-delivery C

Independent review of `09274122cd12857b340135d46f2089cbb1c0bd12` was NOT CLEAR: seven expressible schema contradictions, non-standalone external references, missing commit issue references and PR ref/repository grammar were reported, together with the delivery implementation gate. Original reviewer evidence is `/tmp/stw-review-spec030/schema_test.go` (SHA256 `2bb4dd99d89ae8a8670f70298fd0187330a316d7483f7aff93ead6e9a92bc8d6`) and `/tmp/stw-review-spec030/sink_test.go` (SHA256 `fa687cb3d292e31c79b78876aae290365990c1378a59701b7c925a2c88344488`). The author reproduced all nine new negative schema cases failing plus standalone compilation failing before corrections; the adapted original test now passes all cases and standalone compilation with external loading refused.

The reviewer additionally demonstrated a cmd-owned final-delivery watchdog prototype. This author evaluated the narrowed C alternative with `/tmp/stw-spec030-round2/final_guard_test.go` using disposable source-only test processes on linux/arm64. Blocked stdout/stderr/root-error stderr exited 1 at about 151–155ms for a 150ms deadline; parent cancellation exited 1 at ~66ms. Ordinary file and /dev/null completed exit 0 at ~23ms, after an actual disposable child Wait and cleanup sentinel. A five-case pure exit-decision table plus 100 both-ready completion/cancellation cases verified cancellation/deadline precedence, one callback and model monitor join.

The expired-before-arming model took ~22ms to finish cleanup despite a deliberately shorter 10ms deadline, then exited 1 without output. This proves safety ordering only and demonstrates why a process guard cannot substitute for finite native/auth/child/cleanup bounds. The actual CLI cleanup paths, entry wiring, inherited terminal/macOS behavior and production completion races remain unverified; this is neither a CLI build nor production acceptance. No library os.Exit, live provider request or privilege/descriptor mutation was exercised. The normal model uses a sole command-owned exit arbiter, avoiding an interval where a joined/disarmed monitor no longer covers pending process exit; callback-only tests can join because their injected terminal function returns.

## Scoped authentication ownership evidence

The current candidate and primary-source attribution are in [auth-lifetime.md](auth-lifetime.md). Source-only schema checks pass the original21 decisions, eight positive fixtures, standalone offline compilation, unchanged reused definitions/local refs and six extra decisions. The finite Linux owned-auth prototype passes normal, two-fallback and one-hop-escape controls, ownership refusal predicates and independent control EOF. The private cleanup/connection-abort prototype and original direct-child orphaning counterexample both pass when run separately. An initial attempt to combine their different Go packages failed setup; it was corrected without source changes. Documentation checker passes269 authored documents; whitespace passes. No full Go/race suite, production build, install, real keyring/provider or Darwin/terminal acceptance was run. Evidence paths and hashes are retained in the private final-candidate manifest.

## Third review and finite native-session assessment

Round3 exact bc59132 was DESIGN NOT CLEAR. The bounded follow-on [assessment](round3-disposition.md) retains that result and leaves round4 unspent. Official bubblewrap source confirms the monitor/setsid/parent-death boundary; no sandbox flag changed. Source-only `/tmp/stw-spec030-round4-assessment/native_session_test.go` passes the normal and abnormal-parent controls with an independently registered Linux pidfd, separately identifying actual parent Wait versus worker nonexecution. This finite known-PID evidence cannot close production child discovery or observation-failure ownership. Docs/whitespace checks pass; no production/full Go/race suite, native sandbox run, credentials/provider, build/install or Darwin execution occurred.
