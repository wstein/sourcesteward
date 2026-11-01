# Specification: `stw review carry-over`

**Created**: 2026-11-01 | **Status**: implemented in source, independent review pending (spec-light: this page, the schema
[review-carry-over.schema.json](../../schemas/v1/review-carry-over.schema.json) and the tests are the whole design).
Issue #345. It replaces `scripts/carry-over-check.sh` of wstein/agent-rules, which supports one base only and answers no
when the parent was rewritten.

After a rebase or restack the head of a pull request has a new hash. A review of the old head carries over to the new head
only if the change is provably the same. The rule has three equal-ranked conditions; `stw review carry-over` checks them
and prints the line a human or an agent posts. It never posts, labels, pushes or writes a reference of the user.

## Command

```
stw review carry-over --pr NUMBER OLD_BASE OLD_HEAD NEW_BASE NEW_HEAD [--remote NAME] [--checkout PATH] [--json]
```

The four arguments are full lowercase 40-hex commit ids. A ref name, a leading dash, `..`, a space, a short id or any other
spelling is a usage error (exit 2) before Git runs. `--pr` is required and goes into the printed line. The objects of both
ranges must be in the checkout (default: the current directory); a missing object is `object-missing`.

The form with the two heads and a pull request number alone needs the base of the pull request from the forge. It is not
part of this command: the four-argument form takes the bases from the caller, so a rewritten parent (restack after a
rebase-merge of the parent) is checked like any other rebase.

## Conditions

1. **patch-id**: `git diff --no-renames OLD_BASE...OLD_HEAD` and `NEW_BASE...NEW_HEAD`, each through
   `git patch-id --stable`, give the same id. An empty diff is `patch-id-empty` (no carry-over).
2. **merge commits**: `git rev-list --merges NEW_BASE..NEW_HEAD` is empty. range-diff does not show merge commits.
3. **range-diff equivalent**: the commits of `OLD_BASE..OLD_HEAD` and `NEW_BASE..NEW_HEAD` (merge commits excluded, oldest
   first) are equal pairwise at the same position, in message and in patch. The patch is compared byte for byte except the
   `index` lines (blob ids) and the hunk headers (`@@` line numbers and the function context after them), which a rebase changes. Binary content and gitlinks are included in full: `--binary`, `--ignore-submodules=none` and `--submodule=short` are pinned, so a `-diff`/`binary` attribute or a `submodule.<name>.ignore` or `diff.ignoreSubmodules` setting cannot hide a change. This is the `=` of
   `git range-diff` without running it: range-diff starts the `textconv` drivers of the repository (`.gitattributes`,
   `info/attributes`) and a driver that strips whitespace would give a false yes. A differing pair is `!`, an extra new
   commit `>`, a missing one `<`; a reordering fails at both positions. An empty range fails (`range-empty`). Changed context
   after a restack also fails (the rule then asks for a delta check). Condition 3 exists because the patch-id ignores
   whitespace.

A check that could not run is `unknown` and never a yes. With `--remote NAME` the command also fetches
`refs/pull/NUMBER/head` and refuses (`pr-head-mismatch`) unless it equals `NEW_HEAD`.

## Output and exit status

Human output (default) lists each condition, the verdict and, only for a yes, the line as the last line:

`Carried-over: from=<40-hex OLD_HEAD> to=<40-hex NEW_HEAD> patch-id=<hex> pr=<n>`

`--json` prints one document of the schema (`carry_over`, `reasons`, the three checks, `line` null unless yes). Exit 0
carry-over allowed, 1 not allowed or a read failed (the result is still printed), 2 usage error.

Not decided here: whether `from` was CLEAR (the reviewer reads the ledger), whether the new head works on its base (the
checks of the pull request), whether the label `ci-full` is set again (the reviewer who posts the line).

## Reads and effects

Git only, through one guarded runner (`internal/carryover`). The guard allows exactly the argument shapes of `rev-parse`,
`diff`, `patch-id`, `rev-list`, `log` and the one fetch, with every flag pinned and the exact length checked. A `push`,
`update-ref`, `range-diff` or a fetch into another namespace is refused. Without `--remote` nothing starts but Git itself: no
external diff, textconv, signature program, pager or hook. With `--remote` the fetch can start the transport programs of the
caller (see the limits).

Threat model: the checkout is hostile in its content (the pull request author controls commits, `.gitattributes`,
`.gitmodules`), but not in its `.git/config` or in the environment of the caller; whoever can write those already runs
programs as the caller. The `GIT_` variables that pass (`GIT_SSH`, `GIT_SSH_COMMAND`, `GIT_SSH_VARIANT`, `GIT_ASKPASS`,
`GIT_SSL_*`, `GIT_PROXY_COMMAND`) run the caller's own programs.

What decides the output is fixed, so a user's configuration cannot change a verdict:

- **Pinned by flag** (diff and log): `--no-color --no-ext-diff --no-textconv --no-renames --binary --full-index
  --ignore-submodules=none --submodule=short --no-relative -U3 --inter-hunk-context=0 --diff-algorithm=myers
  --indent-heuristic --src-prefix=a/ --dst-prefix=b/ --no-color-moved -O/dev/null`; log also `--no-show-signature --no-notes
  --encoding=UTF-8`. They cover `diff.relative` (a run from a subdirectory compares the whole tree), `diff.context` and
  `GIT_DIFF_OPTS`, `diff.algorithm`, `diff.indentHeuristic`, `diff.interHunkContext`, the prefix keys, `diff.renames`,
  `diff.orderFile`, `diff.colorMoved`, `diff.external`, `diff.*.textconv`, attributes such as `-diff` and `binary`,
  `diff.ignoreSubmodules`, `diff.submodule`, `submodule.<name>.ignore`, `log.showSignature`, notes and `i18n.logOutputEncoding`.
- **Pinned per call by `-c`** (nothing is written to a configuration): `core.hooksPath=/dev/null`, `core.fsmonitor`,
  `gc.auto`, `maintenance.auto`, `core.quotePath`, `core.bigFileThreshold`, `core.attributesFile`, `diff.suppressBlankEmpty`,
  `diff.wsErrorHighlight`, `diff.mnemonicPrefix`, `diff.noprefix`, `log.showRoot`, `log.follow`, `log.mailmap`, `core.notesRef`,
  `i18n.commitEncoding`, `core.abbrev`, `fetch.writeCommitGraph`.
- **Neutralized in the environment:** every `GIT_` variable is dropped except `GIT_SSH`, `GIT_SSH_COMMAND`, `GIT_SSH_VARIANT`,
  `GIT_ASKPASS`, `GIT_SSL_CAINFO`, `GIT_SSL_CAPATH` and `GIT_PROXY_COMMAND` (so `GIT_DIR` and the other repository selectors,
  `GIT_CONFIG_COUNT`/`KEY`/`VALUE`, `GIT_CONFIG_PARAMETERS`, `GIT_DIFF_OPTS`, `GIT_EXTERNAL_DIFF`, `GIT_PAGER`, `GIT_EXEC_PATH`,
  `GIT_TRACE*`, `GIT_ATTR_SOURCE`, `GIT_NOTES_REF` never reach Git). The global and system configuration are replaced by the null
  device (`GIT_CONFIG_GLOBAL`, `GIT_CONFIG_SYSTEM`, `GIT_CONFIG_NOSYSTEM=1`, so `~/.gitconfig`, `$XDG_CONFIG_HOME/git/config` and
  `/etc/gitconfig` are not read), `GIT_ATTR_NOSYSTEM=1`, `GIT_PAGER=cat`, `--no-pager`, `--no-replace-objects`.
- **Limits:** with `--remote`, the fetch starts the transport programs that the caller's environment and the repository's
  `.git/config` name: `GIT_SSH`, `GIT_SSH_COMMAND`, `GIT_PROXY_COMMAND`, `GIT_ASKPASS`, `core.sshCommand`, `core.gitProxy`,
  `remote.<name>.uploadpack`, an `ext::` URL (with `protocol.ext.allow` or `url.<base>.insteadOf`). The verdict is not
  affected (a failed fetch is `read-failed`), but the programs run. The repository's own configuration (`.git/config`) still applies for keys not listed above, in particular
  `remote.<name>.url`, `url.<base>.insteadOf`, `http.*` and `core.repositoryformatversion`. The fetch does not consult a credential
  helper or `http.*` setting from the global configuration; a remote that needs one fails with `read-failed` (the verdict is
  then no). A checkout owned by another user can fail the `safe.directory` check, because that setting is read only from the
  global configuration; the result is then `read-failed`. Keys that change only color or the layout of a stat are not pinned
  because the vectors print neither. The pinned keys are covered by the hostile-configuration test; the keys without a
  verdict effect on the scenarios of that test (`--full-index`, `-O`, `--diff-algorithm`, `--inter-hunk-context`,
  `--indent-heuristic`, `core.attributesFile`, `core.quotePath`, `core.bigFileThreshold`, `log.showRoot`, `diff.noprefix`,
  `diff.mnemonicPrefix`, `--no-color-moved`, `core.notesRef`, `log.follow`, `i18n.commitEncoding`, `core.abbrev`,
  `fetch.writeCommitGraph`, `gc.auto`, `maintenance.auto`, `core.fsmonitor`, `diff.wsErrorHighlight`) are pinned as a
  precaution and not proven by a mutation; both sides of a comparison are read with the same settings, so they could only hide a
  difference together with another effect.

The only local effect is the `--remote` fetch into `refs/stw/carry-over/pr-NUMBER` (`--no-tags --no-write-fetch-head
--no-recurse-submodules --refmap=`; a configured `remote.<name>.fetch` writes nothing); without `--remote` nothing is written.
No forge call, no network except that fetch.

## Verification

Tests use temporary Git repositories and write no identity into a Git configuration. They cover: pure rebase (yes, exact
line), identical heads, merge commit, patch-id change, whitespace-only change (equal patch-id, commit comparison `!`, also with a stripping textconv driver that must not run), added empty
commit, reordered commits, rename with content change, rewritten parent, missing object, no repository change, argument
injection, the guard, the private fetch, a hostile configuration (repository, global, system, `HOME`, `XDG_CONFIG_HOME`, `GIT_CONFIG_*`, `GIT_DIFF_OPTS`, a run from a subdirectory) that leaves five verdicts unchanged, a moved pull request head, JSON against the schema, exit codes. Each condition is
backed by a mutation that makes the test for that condition fail (recorded in the pull request).
