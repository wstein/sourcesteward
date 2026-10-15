# Quickstart Validation: Agent Hints

Prerequisites: the repository's documented Go version, Python 3 and a checkout. No GitHub token or network access is needed to validate help/documentation behavior after dependencies are available.

1. Run `make build`, then `./bin/stw`, `./bin/stw --help`, `./bin/stw -h` and `./bin/stw help`. Each has one notice and both raw-source links.
2. Run `./bin/stw version`, `./bin/stw version --json`, `./bin/stw version --help` and `./bin/stw help version`. None has a notice; JSON remains the established single version object.
3. Run the CLI tests with `go test ./internal/cli`; writer-failure coverage must include failure after ordinary help succeeds.
4. Read `docs/agent-guide.md` and `llms.txt`. Follow the documented build/help/version examples and confirm future backend operations are labeled planned.
5. Run `python3 scripts/test_docs.py` and `python3 scripts/check-docs.py`; positive links pass and broken fixtures are rejected without HTTP requests.
6. Run `make check`, inspect `git diff --check`, then obtain independent review and record actual evidence in the feature handoff. Publication needs task-context authority.
