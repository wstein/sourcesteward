# Implementation Plan: Release hygiene for the first `stw` release

**Branch**: `codex/026-release-hygiene` | **Date**: 2026-10-16 | **Spec**: [spec.md](spec.md)

## Summary

Move the stampable version into `internal/cli`, add a skipped-unless-requested stamp test, and add `scripts/release.py` with three subcommands: `plan` (dry run), `build` (approval-gated recipe) and `verify` (checksums, build information, smoke tests). Add the missing gates as checks (hidden commands, notices inventory, workflow sentinel), complete the notices inventory, add the arm64 conformance prerequisites, and document the release procedure and readiness record. No build, tag, publication or activation happens in this feature; the first real release is a separate, explicitly authorized step that uses these tools.

## Technical Context

**Language/Version**: Go 1.27.2 (product change is one variable and one test); Python 3 standard library for `scripts/release.py` like the other scripts.
**Primary Dependencies**: none new.
**Storage**: `dist/release/<version>/` (ignored) created only by an authorized `build`; `docs/releases/*.md` records.
**Testing**: Go test for the stamp; Python unit tests with a fake command runner; scan test for workflows; `make check`. No test builds a distributable binary.
**Target Platform**: linux amd64/arm64 and darwin arm64 (see the platform table in the spec). CI is ubuntu-24.04 amd64 only today; arm64 and darwin results are `local-recorded` until lanes exist.
**Project Type**: CLI plus release tooling scripts.
**Constraints**: no network in `plan`/`build`/`verify`; `internal/cli` starts no process (the script, not the product, runs `go`); the script never tags, pushes, publishes or activates; original code is EUPL-1.2 and third-party notices are retained.
**Scale/Scope**: one binary (`stw`), three platforms, one release at a time.

## Constitution Check

Pre-research: PASS. Post-design: PASS.

- **I. Shared contract**: `release.py` renders human and JSON output from one result, never prompts. `stw` output is unchanged.
- **II. Evidence before consequential actions**: a release is consequential. `plan` is the inspectable plan with targets, reasons and expected state; `build` re-evaluates the gates it can (see the spec's gate evaluation table) on an export of the named commit and refuses stale input. A plan and a passing dry run are not authorization.
- **III. Ownership and recovery**: no silent publish; the build writes only into a new ignored directory and never overwrites. Publication is not recoverable by this tool; this is stated and is why A3 is separate.
- **IV. Specify and verify**: stamp and script behavior are test-first; Git and jj conformance remain release gates per platform; evidence and non-runs are recorded.
- **V. Tier 1 peers**: gates require both backends' lanes on every shipped platform (FR-015); a missing lane drops the platform, never relaxes the rule. No equivalence claim.
- **Scope and Development Workflow**: "release automation requires later specifications" and "releases require authorization in their task context". This is that specification for a manual, approval-gated recipe; the build step is scripted only after A1, no push, tag or publication is automated, and nothing is amended. Git and jj: the script reads a colocated jj checkout through Git plumbing only and refuses a jj workspace without a Git view; it writes no Git state.

## Project Structure

```text
internal/cli/version.go              # Version variable (default "dev")
internal/cli/version_test.go         # stamp test, skipped unless STW_RELEASE_VERSION
cmd/stw/main.go, cmd/gh-jj/main.go   # pass cli.Version
scripts/release.py                   # plan, build, verify
scripts/test_release.py              # fake-runner unit tests, scan test, fixtures
scripts/release-gates.json           # gate ids, codes, waivable flags (single source for docs and script)
# notices closure check lives in release.py plan (DD9)
third_party/README.md, licenses/     # cobra, pflag added
docs/releasing.md                    # procedure, signing statement, approvals
docs/releases/                       # readiness record and notes per release (template first)
.github/workflows/ci.yml             # optional arm64 lanes (WI5)
scripts/backend-releases.json        # linux-arm64 jj artifacts (WI5)
```

## Design Decisions

- **DD1 Variable location.** The variable moves from `cmd/stw/main.go` (`var version`, package main) to `internal/cli.Version`. Measured: `-X main.version` was ignored by `go test` of package main and the full import path worked, while a misspelled variable is ignored silently by the linker. With the variable in `internal/cli`, the same `-X` path serves `go test` and `go build`, so the pre-build test checks exactly the flag the build uses. This also matches `smill`. `Run` keeps its `version` parameter, so the version schema and existing tests are unchanged.
- **DD2 Stamp test.** `TestReleaseStamp` skips unless `STW_RELEASE_VERSION` is set. When set it requires `Version == STW_RELEASE_VERSION` and the format. Failure message begins with `stamp-missing` when `Version == "dev"`, `stamp-mismatch` otherwise. Normal `make check` never sets the variable.
- **DD3 Stamp verification by execution.** Measured by the reviewer: under `-trimpath` the build information of a linked binary has no `-ldflags` entry, and even without it the entry would show only the flag passed, not that the variable exists. The stamp is therefore verified by running `stw version --json` (G14) on each platform; `verify` checks `GOOS`, `GOARCH`, `CGO_ENABLED=0`, `-trimpath=true` and the absence of `vcs` keys from `go version -m` for every binary, and a foreign-platform stamp stays unverified until it is executed on its host (the Mac mini for darwin arm64) before publication.
- **DD4 Recipe isolation.** `release.py` constructs the environment from an allowlist plus the recipe values and never inherits `GO*` (the author's shell has `GOFLAGS=-trimpath`, measured). It resolves absolute `GOMODCACHE` from the caller's `go env` first because this devcontainer has `GOPATH=/go` and an inferred `$HOME/go/pkg/mod` is empty (measured by the reviewer), uses a fresh `GOCACHE` for builds, resolves `go` and `git` to absolute paths, and runs Git with no user configuration, `--no-optional-locks` and `core.fsmonitor=false`.
- **DD5 Dry run without a distributable build.** `plan` uses Git plumbing (`rev-parse`, `ls-tree`, `cat-file`, `merge-base`, `diff-tree`, `tag --list` with replace objects disabled; never `git archive` or `git status`, which rewrote the index in the reviewer's measurement), then in the export: `go version`, `go env`, `go mod verify`, `go list -deps` (closure per target, cgo check) and `go vet` per target. `go vet` compiles to the build cache but links nothing, and the stamp test links a temporary test binary that is deleted; AGENTS.md classes `go vet` and `go test` as routine, and this spec decides that they are not artifact builds. Effects are listed in the contract (cache, temporary directories, lock files on a failed lookup). It cannot prove reproducibility or the effect of the stamp on a release binary, and says so.
- **DD6 Archive and manifest.** Python `tarfile` (ustar format) and `gzip` with fixed metadata. The manifest records binary hashes (the reproducibility subject) and archive hashes (the distribution subject).
- **DD7 Approvals in the record.** `build` requires an A1 entry whose quote contains the version, every requested platform and at least 12 hex characters of the code commit. The same agent could write the entry, so the check only catches accidents; the real control is the user's message and the review of the record against it. Waivers need a verbatim user quote as well.
- **DD8 No release workflow.** No `release.yml`. A later spec may add a `workflow_dispatch`-only workflow whose run still needs A1. Scan test (SC-003) enforces the absence in the meantime.
- **DD9 Notices closure check.** `plan` computes `go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}}{{end}}{{end}}' ./cmd/stw` for each shipped target (16 modules on each of the three targets, measured; Windows would add `mousetrap`) and requires each module in the union in `third_party/README.md`, plus a row for the Go standard library and runtime (BSD-3, `licenses/BSD-3-Go.txt` exists). The README title is generalized from "Hosting SDK" to the full binary closure in WI3.
- **DD10 Hidden commands.** One list, `hidden_commands` in `scripts/release-gates.json` (today `["pins"]`). A Go test in `internal/cli` asserts that the list equals the set of commands with `Hidden == true` in the real root and that neither the root help nor any listed help contains them. `plan` scans `docs/`, `README.md`, `llms.txt` and the release notes for `stw <name>` of each listed name. Unhiding a command (Spec 021 WI5) updates the list in the same change, which is the visible review point.
- **DD11 Platform evidence.** The readiness record's platform table is the only place where evidence classes are declared per release; `plan` reads it whenever the record exists.
- **DD12 Code commit and record commit.** The build exports the exact tree of the A1 code commit from tree objects (`git archive` applies `.git/config` filters and `info/attributes`, measured by the reviewer, so it is not used); the record and notes are committed afterwards, and `plan --final` checks that the record commit descends from the code commit and touches only `docs/releases/`. This removes the circularity between a record naming a commit and the commit containing the record, and makes ignored or untracked files irrelevant. The tag points at the code commit.

## Risks

- **Reproducibility is unverified** until an approved double build (G12). Go documents that `-trimpath`, a fixed toolchain and cgo off give reproducible binaries; the claim is not measured here. Mitigation: waivable gate, honest notes.
- **Archive bytes** can differ across hosts (compressor). Mitigation: claim binaries only.
- **linux/arm64 conformance** needs new jj artifact hashes from official releases; a wrong hash is caught by the installer's verification. Mitigation: drop the platform if not done.
- **macOS**: Gatekeeper, ad-hoc signature and sandbox-exec behavior on the Mac mini are unverified here.
- **Approval check is advisory**: a script cannot verify a human. Mitigation: A1 text recorded verbatim, review of the record.
- **Hosted arm64/macOS runners** are available for public repositories according to GitHub documentation (unverified here; the repository is public, measured with `gh repo view`).
