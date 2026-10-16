# Spec 020 verification and handoff

## Scope and provenance

Plan → tasks → independent analysis preceded implementation. Native remote, shared hosting, schema/conformance and independent review lanes used Git worktrees. Original Go implementation; no Rust extension code imported. The branch was rebased onto main fe4e11f while retaining its tidy/retirement conformance and v2 tidy schema.

## Test-first and independent review

Recorded failing behavior tests before native remote, hosting types, transport, resolution/PR/CI and CLI implementation. Review regressions covered private temp/config redirection, malformed UTF-8 API responses, selection rules, early non-network error envelopes and native jj human qualifications. Reviewer hosting_review reported no critical/high design conflicts and 100% requirement/task coverage. Its three actionable source findings were auth-config symlink redirection, early-error schema mismatch and missing native human qualifications; focused fixes and regressions were integrated. Independent re-review cleared a75f81d after all three findings were closed. Final independent review cleared 99fb59b, including network-preflight reporting, pagination bounds, identity-drift regressions, schema and native human qualifications.

## Actual execution

- Baseline make check passed on the starting main with native Homebrew Git, jj 0.46.0 and repository-pinned Hugo. Apple Git launcher and missing Hugo initially failed prerequisites; no weakening or skipped acceptance substituted.
- Native remote tests passed with jj 0.45.1 and 0.46.0; full inspection suite and vet passed in the native author lane.
- Shared CLI/hosting tests and actual native hosting schema outputs passed; transport and CI negative fixtures passed race checks in the core lane.
- Current jj 0.46.0 native/private-extension conformance: 22 cases passed with byte/mode-identical fixture state and fixed GET-only fake API reads. Actual gh dispatch used disposable private config/data/state, never the user's installed extension profile.
- Minimum jj 0.45.1 native/private-extension conformance also passed all 22 cases. Local Git was Homebrew Git 2.56.0; minimum Git remains a hosted CI gate.
- Integrated make check and go test -race ./... passed with native current jj and required sandbox; after the final small network-attempt reporting correction, focused hosting race tests and final integrated make check passed. Refreshed binaries passed all 22 cases again on each supported jj version. CI workflow actionlint passed. Documentation checks, all seven Hugo/schema-publication fixtures and twelve documentation tests passed.

## Limits

No live authenticated GitHub API smoke, push, fetch, PR mutation, CI rerun, extension installation in the user profile, release artifact upload or BundleDock live deletion was performed. Local test builds were explicitly approved after automatic approval review initially rejected them. `jj` remains a version-checked native runtime dependency; no jj Rust library/FFI or new Go library dependency is added. Native jj exposes active remotes and omits empty sections/non-UTF-8 remote names; this is not raw configuration inventory. Config/path sampling and read evidence are not transactional and cannot exclude every concurrent/ABA change.

Linux hosted minimum/current conformance remains required acceptance evidence until the PR CI executes. Source implementation and local test success do not constitute a release/distribution claim. ARM64 release binaries and remote publication plans remain separate work.

## PR handoff

Opened [PR #40](https://github.com/wstein/sourcesteward/pull/40) for owner review. All 13 implementation tasks are completed locally; hosted minimum/current acceptance is still a separate pending CI gate. No merge or release was performed. The owner explicitly chose to retain bounded gh api and evaluate the official SDK separately.
