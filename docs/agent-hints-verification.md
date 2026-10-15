# Agent hints acceptance — 2026-10-15

## Scope and design

Adapted the discovery pattern of [SheetMill PR #19](https://github.com/wstein/sheetmill/pull/19)
to SourceSteward: conditional root-help guidance, an agent guide and llms.txt troubleshooting
index. Only help/version remain implemented. No dependency, backend, network operation or
Pages packaging changes were introduced. Spec Kit artifacts are in
[003-agent-hints](../specs/003-agent-hints/spec.md).

The specification, plan, tasks and help contract were independently reviewed before
implementation. The specification-quality checklist passed all eleven criteria.
Post-implementation consistency analysis found no ambiguities, duplications, constitution
conflicts or coverage gaps: seven requirements and four success criteria map to thirteen tasks.

## Tests and measured results

- Baseline make check passed before implementation.
- CLI tests first failed because all four root-help forms lacked the notice and a
  notice-only write failure incorrectly returned success. They pass after the minimal append.
- Offline link fixtures first failed because the checker ignored owned raw GitHub URLs.
  Missing paths, traversal, symlink escapes and raw-directory targets are now rejected;
  ordinary local directory links remain supported. No links are fetched during checks.
- make check passed: formatting, Go tests/vet/module verification, eight Python tests,
  documentation checks, architecture boundary checks and git diff --check.
- go test -race ./... passed.
- make build succeeded. Thirteen real-process smoke cases passed with the binary invoked
  outside any Git repository: four root-help forms, six version/subcommand help forms,
  and three usage-error forms. Version JSON remained byte-for-byte unchanged.
- Independent implementation review cleared 9b9ef59e7d93e1c1dc8357f7838204e072b3f58c
  against base 41553403643494a79c32e9753b8af5c22431f1c3 with no actionable findings.
  The reviewer separately ran CLI tests, all eight Python tests, documentation validation
  and whitespace checks successfully.

## Handoff

The feature uses focused Conventional Commits on codex/003-agent-hints. Work was moved
into a Git worktree of the existing SourceSteward repository after the user's explicit
worktree preference. The main checkout remains unchanged. This final documentation-only
record follows the reviewed implementation.

Remote publication and merge are pending; neither is claimed as acceptance evidence.
The main-branch raw URLs in help become available after the feature reaches main; the local
files already exist and pass offline validation. No HTTP availability check is claimed.
