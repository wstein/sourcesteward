# Quickstart Validation

Prerequisites: Go 1.27.2+, Git and Python 3. From a fresh clone:
```sh
make check
go test -race ./...
go run ./cmd/stw --help
go run ./cmd/stw version
go run ./cmd/stw version --json
```
Expect help, `SourceSteward dev` (followed by ` (REVISION)` when the binary was built from a Git checkout), and a single JSON object with schema_version 1 respectively.
Unknown commands/flags/extra arguments fail; the Go command contract tests verify the actual
CLI exit classifications independently of go run's process wrapper.

Inspect the first commit:
```sh
git ls-tree -r "$(git rev-list --max-parents=0 HEAD)"
```
Expect no entries. Follow README links to the constitution, spec, plan, tasks and roadmap.

Preview the website:
```sh
python3 -m http.server 8000 --directory docs
```
Open http://localhost:8000/ at desktop and 375px widths. Confirm keyboard focus, readable
content, local assets and explicit planned-feature labels. Source/spec links target GitHub.

On GitHub, confirm CI succeeds for the pushed main SHA, deploy needs check, Pages artifact
contains revision.txt with that SHA, and PR jobs cannot deploy. Inspect the live Pages URL.
No release, destructive Git command, repository migration or backend adapter is part of this validation.
