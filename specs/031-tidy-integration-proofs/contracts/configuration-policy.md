# Planning configuration policy

Subordinate to the [specification](../spec.md) FR-019..FR-023.

## Planning environment

One constant argument list `planningConfigArgs`, used by planning and by apply re-evaluation (a test asserts equality): `-c core.hooksPath=/dev/null -c core.fsmonitor=false -c submodule.recurse=false -c core.untrackedCache=false -c merge.renormalize=false -c merge.renames=true -c merge.directoryRenames=conflict -c merge.renameLimit=1000 -c diff.renames=true -c diff.renameLimit=1000 -c core.autocrlf=false -c gc.auto=0 -c maintenance.auto=false -c protocol.ext.allow=never`, with `--no-optional-locks`, `--no-replace-objects`. Environment: the existing sandbox environment with no inherited `GIT_*` override (explicitly including `GIT_CONFIG_PARAMETERS`, `GIT_CONFIG_COUNT`, `GIT_CONFIG_KEY_*`, `GIT_CONFIG_VALUE_*`, `GIT_EXTERNAL_DIFF`, `GIT_PAGER`, `GIT_ASKPASS`), `GIT_CONFIG_NOSYSTEM=1`, `GIT_ATTR_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL` and `GIT_CONFIG_SYSTEM` set to the platform null file, and a private empty `HOME` and `XDG_CONFIG_HOME` (temporary directories the process owns) for every proof command, `GIT_NO_LAZY_FETCH=1`, `GIT_TERMINAL_PROMPT=0`. Attribute-dependent commands pin `--attr-source` to the tip tree (spec FR-010a). One admission read lists effective configuration with origins (global and system included) for classification and notes; it runs no helper.

Why these keys change (versus Spec 029's blanket refusal): only `core.hooksPath`, `core.fsmonitor` and `submodule.*` become neutralized, because no read-only planning command runs hooks, fsmonitor or `git submodule` and the arguments disable them on every command (the precedent in inventory and release reads). `hook.*` and `core.attributesFile` stay refused (a command-line `core.hooksPath` does not neutralize config-based hooks; attributes change content evaluation). The default `.git/hooks` fingerprint stays bound into the safety digest.

## Classification

Dispositions: **neutralized** (plan succeeds, plan note), **review reason** (plan succeeds, the named reason of the [reason table](integration-proofs.md#review-reasons)), **hard stop** (all proofs stop), **refused** (no plan, named class), **retained** (unchanged behavior).

| Key or condition | Planning | Apply |
| --- | --- | --- |
| `core.hooksPath` | neutralized | refuses a hook-running effect |
| `hook.*` | refused (`git-unsafe-configuration`) | refused |
| default executable `.git/hooks/*` | fingerprint retained, noted | refuses a hook-running effect |
| `core.fsmonitor` | neutralized | status checks neutralized, noted |
| `submodule.*` | neutralized | retirement retains `submodule-present` |
| `core.pager`, `core.editor`, `core.sshCommand`, `core.askPass`, `credential.*`, `gpg.*` | neutralized (never invoked, environment stripped), noted when set | unchanged |
| `diff.external`, `diff.<d>.textconv` | neutralized (`--no-ext-diff --no-textconv`) | unchanged |
| executable `filter.*`, `merge.*.driver` | review reason `merge-driver` (`merge-noop` only) | existing stage2 behavior retained |
| sentinel `filter.unspecified|unset|set.*` | refused | refused |
| `include.path` | evaluated by Git, classified by effective keys | same |
| `includeIf.*`, `core.attributesFile` | refused | refused |
| `extensions.worktreeConfig`, `extensions.partialclone`, `remote.*.promisor`, `core.alternateRefsCommand`, `core.sparseCheckout*` | refused | refused |
| tree `.gitattributes` defining `merge` or `binary` | review reason `merge-attribute` | re-evaluation reads the same sources |
| `$GIT_DIR/info/attributes` with content, symlink, special file or over 1 MiB | review reason `attribute-source` | same |
| `diff` attribute other than unspecified on a touched path | review reason `diff-attribute` | same |
| `info/grafts`, `shallow` | hard stop `history-override` | re-evaluation retains |
| `objects/info/alternates` | hard stop `unsupported-metadata` | retained |
| `refs/replace/*` | ignored by `--no-replace-objects`, note | same |

## Test matrix

Each row is a temporary-repository fixture; hooks, filters, drivers, pagers and similar helpers append to a marker file, which must be absent in every row.

| ID | Condition | Planning | Apply |
| --- | --- | --- | --- |
| C1 | local `core.hooksPath` with a `reference-transaction` hook | neutralized, plan succeeds | refuses ref deletion, class named |
| C2 | global `core.hooksPath` | neutralized | same |
| C3 | `submodule.x.url`, `submodule.x.update=!helper`, a gitlink | neutralized | unchanged |
| C4 | `submodule.recurse=true` | neutralized | unchanged |
| C5 | `core.fsmonitor=helper` | neutralized | status neutralized |
| C6 | `filter.x.clean=helper` with matching attribute | `merge-driver` for `merge-noop`; other classes run | stage2 behavior |
| C7 | `merge.x.driver=helper` with matching attribute | `merge-driver` | stage2 behavior |
| C8 | `diff.external=helper`, `diff.x.textconv=helper` | neutralized, proofs run | unchanged |
| C9 | `includeIf` | refused, class named | refused |
| C10 | `extensions.partialclone` | refused | refused |
| C11 | umbrella: three gitlinks, `submodule.*` set | plan succeeds | unchanged |
| C12 | `hook.<name>.command` | refused | refused |
| C13 | `include.path` file defining `core.hooksPath` | neutralized via effective key | as C1 |
| C14 | `core.pager`, `core.editor`, `credential.helper`, `core.sshCommand`, `gpg.program`, `core.askPass` set to helpers | plan succeeds, notes | unchanged |
| C15 | `core.attributesFile` | refused | refused |
| C16 | `extensions.worktreeConfig` | refused | refused |
| C17 | `core.sparseCheckout` | refused | refused |
| C18 | `core.alternateRefsCommand=helper` | refused, marker absent | refused |
| C19 | sentinel `filter.unspecified.clean`, `filter.unset.*`, `filter.set.*` | refused | refused |
| C20 | non-reserved executable `filter.evil.*`, `filter.fixture.*`, `filter.reviewer.*` with matching attributes | `merge-driver` | stage2 behavior |
| C21 | executable `.git/hooks/reference-transaction` | note, hooks fingerprint bound | refuses ref deletion |
| C22 | replace refs present | note `replace-refs-ignored` | same |
| C23 | `info/grafts` present | hard stop `history-override` | retained |
| C24 | shallow clone | hard stop `history-override` | retained |
| C25 | `objects/info/alternates` present | hard stop `unsupported-metadata` | retained |
| C26 | tip with a gitlink difference | `gitlink-change`, no submodule repository opened | unchanged |
| C27 | planning versus apply argument list | identical `planningConfigArgs` | identical |
| C28 | union repro: branch deletes a line the base changed, `merge=union` in a tree `.gitattributes` (base only, branch only, merge base only); control without the attribute exits 1 `conflict` | `merge-attribute` (never proven) | re-evaluation retains |
| C29 | `* binary` macro, `-merge`, `merge=binary`, user macro `[attr]m merge=union` | `merge-attribute` | same |
| C30 | clean tree without merge attributes (other attributes allowed) | `merge-noop` evaluates and can prove; a worktree `.gitattributes` with `merge=union` is ignored by `--attr-source` | same |
| C31 | `$GIT_DIR/info/attributes` with any non-blank, non-comment line (including a macro use), as a symlink, a special file, over 1 MiB, and through a linked worktree's common git directory; comment-only or absent file | `attribute-source` for `patch-id` and `merge-noop` (absent or comment-only: ignored) | same |
| C32 | `$XDG_CONFIG_HOME/git/attributes` with `merge=union` (real Git) | verdict unchanged, never falsely proven | same |
| C33 | tree attribute `*.dat diff=d` (also plain `diff` and `-diff`) on a touched path, with or without `diff.d.binary=false` or `diff.d.textconv=helper` | `diff-attribute` for `patch-id`, helper marker absent | same |
| C34 | unit test of the exact environment the executor builds: private empty `HOME`/`XDG_CONFIG_HOME`, null `GIT_CONFIG_GLOBAL`/`GIT_CONFIG_SYSTEM`, `GIT_CONFIG_NOSYSTEM=1`, `GIT_ATTR_NOSYSTEM=1`; no variable can redirect Git's system attributes path, so the real-Git assertion covers global and XDG only | environment equals the expected list | same |
| C35 | binary file changed on the branch (blobs differ) and a different binary change on the base | not matched on path alone; report shows "binary file changed" with blob OIDs and sizes, never a content diff | same |
| C36 | binary change that is identical on both sides, and a text commit with whitespace-only difference | binary: exact `patch-id` match, no `whitespace-differs`; text: `whitespace-differs` | same |
| M1 | mutant: drop `-c core.hooksPath` | C1 must fail | n/a |
| M2 | mutant: inherit global config | C2 must fail | n/a |
| M3 | mutant: inherit `GIT_CONFIG_PARAMETERS` / `GIT_CONFIG_COUNT` with a hooksPath value | C1 must fail | n/a |
| M4 | mutant: drop `merge.renormalize=false` or `core.autocrlf=false` | a renormalizing fixture changes `merge-noop` and must fail | n/a |
| M5 | mutant: omit the attribute scan or `--attr-source` | C28..C30 must fail | n/a |
| M6 | mutant: inherit the caller's `HOME`/`XDG_CONFIG_HOME` or drop `GIT_ATTR_NOSYSTEM` | C32 or C34 must fail | n/a |
| M7 | mutant: ignore `info/attributes` | C31 must fail | n/a |
