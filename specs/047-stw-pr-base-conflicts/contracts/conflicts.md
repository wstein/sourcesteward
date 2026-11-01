# Contract: base conflict members of `stw pulls --json` (Spec 047)

Additive optional members of the unpublished `schemas/v1/pulls.schema.json`; no key is removed or retyped, `schema_version` stays 1, the
digest in `internal/contracts/schema_freeze_test.go` changes in the commit that edits the schema. Without `--conflicts` the new members
are `false`, `0` or `null`.

- `options.conflicts`, `options.restack_plan` (bool).
- `summary.base_conflicts` (int): rows whose `base_conflict.state` is `conflicts-with-base`.
- Row member `base_conflict`: `null` when not probed, else `{state, reason, base, base_sha, merge_base, files, overlap, truncated,
  blocked_by}`: `state` `clean|conflicts-with-base|unknown`; `reason` null or one of `fetch-failed|head-moved|ref-invalid|
  past-probe-limit|probe-failed|no-merge-base`; `base` the base branch; `base_sha` and `merge_base` sha or null; `files` array of
  `{path, base_commit}` (`base_commit` sha or null); `overlap` array of paths; `truncated` bool; `blocked_by` the number of the lowest
  conflicting ancestor or null.
- Row `reason` gains `parent-conflict` (group `blocked`).
- Document member `conflicts` (`null` without `--conflicts`): `{remote, stacks, plans}`. `stacks`: array of `{repo, root, levels}`, a level
  is `{number, depth, parent, head, clear, ci, conflict, status}` (`parent` null for a root; `clear` `valid|invalid|none|unknown`;
  `ci` `none|pending|green|red|-`; `conflict` as `state`; `status` null, `blocked: restack needed` or `blocked by #n`). `plans`
  (filled with `--restack-plan`, else `[]`): array of `{repo, remote, root, steps, gh_stack, brief}`, a step is `{number, head_ref,
  old_head, why, skip, rebase, verify, push}` (`skip` and `push` null when absent; `rebase` and `verify` string arrays); `brief` is Markdown.

## Human output

A `base conflicts` section lists per conflicting pull request its files and base commits, an `early warning` section the overlaps, a
`stacks` section the tree with one line per level (`#n  head  clear  ci  conflict  status`), and with `--restack-plan` the brief of each
plan. The row line of a conflicting pull request ends with `conflicts-with-base`. Untrusted text is escaped as before.
