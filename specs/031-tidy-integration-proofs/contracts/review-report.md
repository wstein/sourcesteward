# Review report contract

Subordinate to the [specification](../spec.md) FR-031..FR-038. The report helps a human judge, and above all lets an AI evaluate and clear, every candidate that is not provable but not hopeless. It is evidence only.

## CLI

```text
stw tidy plan [PATH] --contract 3 --base REF ... [--report-dir ABS] [--consolidation-range A..B ...] [--decisions FILE] [--accept-whitespace-differences] [--report-max-entries N] [--report-max-bytes N] [--report-max-diff-bytes N] [--json]
```

- `--report-dir ABS`: absolute directory owned by the caller; files `tidy-report.md` and `tidy-report.json` are written there. Refused (usage error, exit 2, before native work) when the path is relative, inside any repository, worktree, git directory, workspace root or the state directory, a symlink, not an existing directory, or when either target exists. Files are created mode 0600 with exclusive no-follow creation, written to temporary names and renamed. A write failure exits 1, nothing partial remains, and no plan is stored.
- `--consolidation-range A..B` (repeatable, at most 4): each side resolved to a full OID before use; malformed, unresolvable or more than 256 commits is a usage error naming the selector.
- Bounds are defaults overridable within hard ceilings: `--report-max-entries` (default 256, ceiling 1,024), `--report-max-bytes` per file (default 4 MiB, ceiling 16 MiB), `--report-max-diff-bytes` per entry (default 64 KiB, ceiling 1 MiB); values above a ceiling are usage errors and the values are bound into the plan identity.
- Without `--report-dir` the v3 result still carries `review_summary` (counts per bucket and reason).
- Exit codes are those of `stw tidy plan` (0 plan produced, even with review entries; 1 operational failure; 2 usage). The report never changes an exit code.

## Entry selection and order

Every `needs-review` ref, ordered by full ref name, up to 256 entries; the remaining are listed by name and reasons only with the omitted count. `retained` refs appear only in the summary with `clearable: false`.

## Markdown rendering (humans)

Per branch, in this order:

1. Header: ref name, tip OID, base selector and OID, merge base, reasons (names from the [reason table](integration-proofs.md#review-reasons)), suggested verdict and confidence.
2. Commits: OID (12 chars), subject (at most 200 bytes, escaped), author name, date; at most 50, then `... N more commits`.
3. Proofs tried: one line per class with outcome and why it failed.
4. What the base does not yet contain: `git diff U^{tree} R`, where `R` is the result tree printed by `git merge-tree --write-tree --no-messages U B` (also for a conflicting merge: the tree with conflict markers, with the conflicted paths listed). Render as a fenced diff block. A binary path is shown as "binary file changed", old and new blob OIDs and sizes, no content (decided by `git --attr-source=<tip tree> diff-tree --numstat` printing `-`). Every changed path is listed with its file type and old to new mode (`regular -> executable`, `file -> symlink`, `gitlink: submodule pointer OLD -> NEW`); a mode-only change is listed as "mode only"; a symlink or type change is flagged. When the diff is empty under `-w` but not without, the section states "whitespace-only difference" and renders tab as `→`, trailing space as `·`, CR as `␍`. Truncated at 64 KiB, 1,000 lines or 16 files with a line `truncated: shown N of M files, K of L lines`.
5. Candidate base commits: up to 5 commits on the base side with the same default patch-id or the same subject, with the match kind.
6. Range pairing when `--consolidation-range` is named: rows `= ! < >` with branch and range commit abbreviations (up to 256), evidence label `superseded-candidate`.
7. Questions: the fixed list below.

## JSON rendering (AI)

`schema_version: 1`. No timestamps; ordering and key order are fixed so identical input gives byte-identical output.

| Field | Type | Meaning |
| --- | --- | --- |
| `summary` | object | `entries`, `omitted`, counts per bucket (`proven`, `needs_review`, `retained`) and per reason |
| `base` | object | `selector`, `oid` |
| `entries[]` | array | one per entry, below |
| `entries[].branch` | string | full ref name |
| `entries[].tip_oid`, `base_oid`, `merge_base_oid` | string | full OIDs |
| `entries[].reasons[]` | enum | review reasons |
| `entries[].commits[]` | array | `oid`, `subject`, `author`, `date`; `truncated` marker `{shown,total}` |
| `entries[].proofs_tried[]` | array | `class`, `outcome`, `why` |
| `entries[].unintegrated_diff` | object | `stat` (`files`,`insertions`,`deletions`), `conflicted_paths[]`, `whitespace_only`, `binary_files[]` (`path`, `old_blob`, `new_blob`, `old_size`, `new_size`), `files[]` (`path`, `old_mode`, `new_mode`, `type_change` (for example `regular->executable`), `kind` (`content`, `mode-only`, `symlink`, `gitlink`, `binary`), for gitlinks `old_oid`, `new_oid`), `hunks[]` (`file`, `old_start`, `old_lines`, `new_start`, `new_lines`, `lines[]`), `truncated` (`{unit,shown,total}` or null) |
| `entries[].candidate_base_commits[]` | array | `oid`, `subject`, `match` (`patch-id`, `subject`) |
| `entries[].range_pairing` | object or null | `range`, `pairs[]` (`status`, `branch_commit`, `range_commit`), `truncated` |
| `entries[].suggested_verdict` | object | `verdict`, `confidence`, `basis[]` |
| `entries[].questions[]` | array | fixed template ids and text |
| `entries[].allowed_decisions[]` | array | `integrated`, `keep`, `drop` |

An AI reviewer must be able to classify each branch from this file alone, without running git. The candidate JSON schema is validated by tests; it is published only with accepted implementation.

## Questions (fixed)

`Q1` Is every change in `unintegrated_diff` already present elsewhere on the base in another form? `Q2` Is the unintegrated diff only whitespace or formatting? `Q3` Does the range pairing show the branch's commits were consolidated? `Q4` Is any change in the diff something the owner would want to keep? A decision answers these with `integrated`, `keep` or `drop` (see the [decisions contract](decisions.md)).

## Suggested verdict

Deterministic, in order: (1) unintegrated diff empty or whitespace-only: `obviously-integrated`, confidence high (empty) or medium (whitespace-only); (2) a named range pairs every non-empty branch commit with `=` or `!`: `looks-superseded`, confidence medium (all `=`) or low; (3) reasons include `conflict`, `merge-commits-in-range`, `attribute-source`, `merge-attribute`, `diff-attribute`, `proof-limit` or any truncation: `unclear`, low; (4) unintegrated diff has non-whitespace content and is unpaired: `has-real-unmerged-content`, confidence high when no range was named and medium otherwise. The verdict is a suggestion, never a decision.

## Command bounds

Same grammar rules as the proof table; each with a bound test (at the bound accepted, one over refused, SHA-1 and SHA-256 widths).

| Command | Max records | Max bytes |
| --- | --- | --- |
| `log -n 50 --format=%H%x00%s%x00%an%x00%aI` | 50 | 50 x 4,224 |
| `diff-tree -p U^{tree} R` (no ext diff, no textconv, `--binary` off) | opaque | 1 MiB read, 64 KiB rendered |
| `diff-tree --numstat -r -z U^{tree} R` | 8,192 | 34,603,008 |
| `diff-tree -r --raw --no-renames --no-abbrev U^{tree} R` (modes) | 8,192 | 34,603,008 (8,192 x 4,224) |
| candidate commit lookup (`log` over `M..U` limited to the patch-id and subject matches already computed) | 5 per entry | 5 x 4,224 |
| `range-diff --no-color` over named range | 256 rows | 256 x 4,224 |

Totals (defaults, overridable as above): Markdown at most 4 MiB, JSON at most 4 MiB, at most 256 entries.

## Safety

Only metadata, the unintegrated diff and matching commits' subjects are emitted; no other file contents. Credentials in URLs (`scheme://user:secret@host`) are replaced by `scheme://<redacted>@host` in every emitted string. All strings are escaped for the rendering and treated as data (never commands). The unintegrated diff may contain secrets that are in the branch; the files are 0600 and sensitive. Report commands run with the planning environment and the private object directory; the report never writes into the repository.

## Golden fixtures

A fixture repository covers: a whitespace-only difference (`whitespace-differs`), a partial match, real unmerged content, consolidated work with a named range, a merge in range, a binary path, an executable-bit flip, a symlink retarget, a gitlink bump, a URL with credentials in a subject. Markdown and JSON are compared byte for byte with goldens, and the JSON alone is evaluated by a test reviewer to the expected verdict.
