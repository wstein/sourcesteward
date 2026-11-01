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

Git only, through one guarded runner (`internal/carryover`): a fixed argument vector per call, validated object ids, a clean
environment, no replace objects, hooks switched off (`core.hooksPath` is `/dev/null` per call, no configuration is written), the `GIT_DIR`
family removed from the environment, and a guard that allows only fixed argument shapes (`rev-parse`, `diff`,
`patch-id`, `rev-list`, `log`) and the one fetch, with every flag pinned (`--no-ext-diff`, `--no-textconv`, `--no-renames`,
`--no-show-signature`, `--no-tags`). No external diff, textconv, signature program or hook starts; a `push`, `update-ref`,
`range-diff` or a fetch into another namespace is refused by the guard.
The only local effect is the `--remote` fetch into `refs/stw/carry-over/pr-NUMBER` (`--no-tags --no-write-fetch-head
--no-recurse-submodules --refmap=`; a configured `remote.<name>.fetch` writes nothing);
without `--remote` nothing is written. No forge call, no network except that fetch.

## Verification

Tests use temporary Git repositories and write no identity into a Git configuration. They cover: pure rebase (yes, exact
line), identical heads, merge commit, patch-id change, whitespace-only change (equal patch-id, commit comparison `!`, also with a stripping textconv driver that must not run), added empty
commit, reordered commits, rename with content change, rewritten parent, missing object, no repository change, argument
injection, the guard, the private fetch, a moved pull request head, JSON against the schema, exit codes. Each condition is
backed by a mutation that makes the test for that condition fail (recorded in the pull request).
