# Pre-implementation analysis

## Round 0

Author draft, 2026-10-16. No independent review has run yet. Facts the draft relies on were measured with Git 2.56.0 in scratch repositories (research.md, Git measurements 1 to 14) and read from the official jj documentation (research.md, documentation evidence). Nothing about jj behavior was measured: jj is not installed in the author's environment, and the jj claims are `documented` or `unverified` by construction.

## Round 1

Independent reviewer: read-only Opus review, revision 4983930e, 2026-10-16, with scratch repositories on Git 2.56.0; jj not installed and nothing about jj measured. Verdict: NOT READY, six high, twelve medium and ten low findings. The author re-measured findings 3, 7 and 8 (the first two as stated, the third for a directory entry) before changing the artifacts; the reviewer's measurements of findings 2, 9(a), 13 and 15 and its reading of the jj `workspace add` documentation (finding 4) are accepted from the report and marked as not re-measured in research.md.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | High | jj effects statement false: `Inspect` runs many jj queries and extra failure codes | Plan DD1 and DD2, FR-006, contract Effects, T004, T006: jj ownership from the filesystem-only detection, only the sandbox probe and `--version` run |
| 2 | High | Linked worktree default hooks live in the common directory; confinement too narrow | FR-004, plan DD8, data model `common_dir`, T008 fixture |
| 3 | High | `git config` does not apply the ownership check | Re-measured (research 13); FR-005, plan DD4, T007 gate bracketing every read |
| 4 | High | Secondary jj workspaces are Git worktrees by default | FR-005, US2-4, plan DD3, J6, T016 (colocated means a `.git` entry, directory or gitfile) |
| 5 | High | Non-colocated push may read global config and store hooks; J5 vacuous | `guards_evaluated: false` and `workspace-has-no-git-configuration` wording; J5 installs store and global hooks; research |
| 6 | High | Conformance procedure not deterministic | Spec procedure: identities, per-step sentinels, push precondition, in-fixture control J7, jj-spawned git version, exit statuses 0, 1 and 2; T022 |
| 7 | Medium | Valueless `hooksPath` and `~nosuchuser` misclassified | Re-measured (research 15); state `invalid`, `hooks-path-invalid`; FR-003, DD5 |
| 8 | Medium | Kind-only classification wrong | Re-measured (research 16); evaluation order in FR-007 and DD7 |
| 9 | Medium | Configuration hooks under-specified | `command_present`, per-key scopes, `config-hook-incomplete`, claim `git-config-hooks`, engine unknown not counted, example corrected |
| 10 | Medium | No Git version source | `git version` and minimum check on the colocated path; refusal table rows |
| 11 | Medium | Process allowlist incomplete | Research allowlist rewritten; runner-recording test in T004 |
| 12 | Medium | Refusal table omits `Inspect` codes | One pass-through row; HEAD instability blocks, stated |
| 13 | Medium | `not-directory` and `unreadable` without finding | `hooks-dir-unusable`; `disabled-by-path` extended |
| 14 | Medium | Symlink and outside-directory rules tautological | Findings table and DD8 define both |
| 15 | Medium | Listing conflicts with the source scan | Single allowlisted directory-open helper, `ReadDir(257)`; wording of "never opened" |
| 16 | Medium | Evidence level semantics ambiguous | One level per claim, backend tuples, disagreeing lanes, version parsing |
| 17 | Medium | No proof method for "not opened" | inotify on Linux plus FIFO; macOS recorded as unproven |
| 18 | Medium | Man page on host is 2.47.3 | Spec and research say so; task T027 |
| 19 | Low | Levels not a limit | 16-record cap with truncation flag |
| 20 | Low | `ls-files` fragile | Run from the worktree root with literal catalog pathspecs |
| 21 | Low | Command text read into process | `--name-only` read for `.command` |
| 22 | Low | Real versus effective uid | Real user, `syscall.Access` |
| 23 | Low | Root-owned hooks flagged | Root counts as safe |
| 24 | Low | Privacy claim false | Claim restated in research and FR-003 |
| 25 | Low | Environment list incomplete | Extended; `HOME` and `XDG_CONFIG_HOME` applied |
| 26 | Low | Text example inconsistent with JSON | Example rewritten |
| 27 | Low | Edge cases without a task | Assigned in T008, T010; inert default hooks not examined, stated |
| 28 | Low | Untagged tasks | Tagged |
