# Validation Quickstart

Use disposable temporary repositories only. Prerequisites: Go from go.mod and Git; no network or service required.

1. `make check` establishes source/contract checks.
2. `go test ./internal/inspection -v` exercises normal, linked, bare, detached/unborn, managed markers, ownership/error paths, unusual filenames, cancellation, concurrent HEAD and no-write snapshots.
3. `go test -race ./...` checks shared-state handling.
4. `make build`, then `./bin/stw inspect --json` demonstrates one structured identity result; `./bin/stw inspect --help` explains bounded effects.
5. In a disposable empty directory, inspect fails1 with not-repository. With --json, stdout is a schema1 error result; stderr has exactly one concise diagnostic.
6. Add a recognized `.jj` fixture marker in a disposable directory: inspect refuses unsupported backend, records evidence and does not execute jj.

Record commands and observed results in docs/inspection-verification.md after implementation, not before. Independent review must examine source and acceptance evidence before external handoff.
