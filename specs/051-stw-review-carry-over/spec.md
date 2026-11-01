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

1. **patch-id**: `git diff --no-renames --binary OLD_BASE...OLD_HEAD` and `NEW_BASE...NEW_HEAD`, each through
   `git patch-id --stable`, give the same id. An empty diff is `patch-id-empty` (no carry-over).
2. **merge commits**: `git rev-list --merges NEW_BASE..NEW_HEAD` is empty. range-diff does not show merge commits.
3. **range-diff**: `git range-diff OLD_BASE..OLD_HEAD NEW_BASE..NEW_HEAD` shows `=` for every commit and each pair stands at
   the same position (a reordering shows `=` at other positions and fails). `<`, `>` and `!` fail. An empty range fails
   (`range-empty`). Every commit (merge commits excluded) must appear as exactly one row; a different row count is
   `read-failed`. Condition 3 exists because the patch-id ignores whitespace.

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

Git only, through one guarded runner (`internal/carryover`): a fixed argument vector per call, validated object ids, a clean
environment, no replace objects, a table of allowed subcommands (`cat-file`, `diff`, `patch-id`, `rev-list`, `range-diff`)
and the one fetch. Nothing else is started; a `push`, `update-ref` or a fetch into another namespace is refused by the guard.
The only local effect is the `--remote` fetch into `refs/stw/carry-over/pr-NUMBER` (`--no-tags --no-write-fetch-head`);
without `--remote` nothing is written. No forge call, no network except that fetch.

## Verification

Tests use temporary Git repositories and write no identity into a Git configuration. They cover: pure rebase (yes, exact
line), identical heads, merge commit, patch-id change, whitespace-only change (equal patch-id, range-diff `!`), added empty
commit, reordered commits, rename with content change, rewritten parent, missing object, no repository change, argument
injection, the guard, the private fetch, a moved pull request head, JSON against the schema, exit codes. Each condition is
backed by a mutation that makes the test for that condition fail (recorded in the pull request).
