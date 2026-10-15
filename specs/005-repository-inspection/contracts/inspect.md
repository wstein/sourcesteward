# stw inspect contract

Usage: `stw inspect [PATH] [--json]`; PATH defaults to cwd. `--` permits paths beginning with a dash. Help remains local and succeeds without repository access.

| Outcome | Exit | stdout | stderr |
| --- | --- | --- | --- |
| success | 0 | human identity or one JSON object plus LF | empty |
| usage | 2 | empty | one concise `Error:` line |
| operational error, unsupported or ambiguous; human | 1 | empty | one concise `Error:` line |
| operational error, unsupported or ambiguous; JSON | 1 | one schema1 result plus LF | one concise `Error:` line |
| cancellation or output failure | 1 | no success object if detected before write; partial writes possible | one concise `Error:` line when writable |

JSON result fields and invariants are defined in [data model](../data-model.md). Human success shows backend, worktree or bare classification, quoted absolute paths, object format and attached/detached/unborn HEAD. Both renderers consume the same result. Symbolic HEAD targets outside refs/heads are invalid-metadata; unborn means absent symbolic branch only, not a missing or noncommit object. Human error includes detected backend/evidence where relevant; path controls are escaped with safe quoting. Repository content, Git stderr and config values are never interpolated into diagnostics.

Ordinary inspection reads local metadata through fixed Git built-ins; it does not refresh the index or inspect tracked/untracked files. Recognized managed workspace metadata is refused with evidence and no native backend invocation. Ancestor jj detection stops at the nearest Git boundary; same-directory jj markers still count. Marker symlinks/wrong types fail invalid-metadata. GitButler evidence is gitbutler metadata directories or refs/gitbutler/* or the exact workspace branch, including packed refs. Unknown conventions and unregistered single-branch GitButler can remain undetected. No network, hooks, writes, pager, editor or prompt is allowed.

No claim of transactional consistency: repeated HEAD observation detects only HEAD changes visible at sampling points; metadata routing/backend-evidence races and ABA changes are not covered. Unsupported cases are explicit failures, not successful incomplete Git inspection. Exit2 CLI parsing errors do not attempt JSON. JSON output failure cannot be repaired by a second object.
