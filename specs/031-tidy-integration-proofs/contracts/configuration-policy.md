# Planning configuration policy

Subordinate to the [specification](../spec.md) FR-019..FR-023.

## Planning environment

One constant argument list `planningConfigArgs`, used by planning and by apply re-evaluation (a test asserts equality): `-c core.hooksPath=/dev/null -c core.fsmonitor=false -c submodule.recurse=false -c core.untrackedCache=false -c merge.renormalize=false -c merge.renames=true -c merge.directoryRenames=conflict -c merge.renameLimit=1000 -c diff.renames=true -c diff.renameLimit=1000 -c core.autocrlf=false -c gc.auto=0 -c maintenance.auto=false -c protocol.ext.allow=never`, with `--no-optional-locks`, `--no-replace-objects`. Environment: the existing sandbox environment with no inherited `GIT_*` override (explicitly including `GIT_CONFIG_PARAMETERS`, `GIT_CONFIG_COUNT`, `GIT_CONFIG_KEY_*`, `GIT_CONFIG_VALUE_*`, `GIT_EXTERNAL_DIFF`, `GIT_PAGER`, `GIT_ASKPASS`), `GIT_CONFIG_NOSYSTEM=1`, `GIT_ATTR_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL` and `GIT_CONFIG_SYSTEM` set to the platform null file, and a private empty `HOME` and `XDG_CONFIG_HOME` (temporary directories the process owns) for every proof command, `GIT_NO_LAZY_FETCH=1`, `GIT_TERMINAL_PROMPT=0`. For `merge-noop`, `merge` attributes from every source are checked (spec FR-010a) and `--attr-source=U^{tree}` is passed. One admission read lists effective configuration with origins (global and system included) for classification and notes; it runs no helper.

Why these keys change (versus Spec 029's blanket refusal): only `core.hooksPath`, `core.fsmonitor` and `submodule.*` become neutralized, because no read-only planning command runs hooks, fsmonitor or `git submodule` and the arguments disable them on every command (the precedent in inventory and release reads). `hook.*` and `core.attributesFile` stay refused (a command-line `core.hooksPath` does not neutralize config-based hooks; attributes change content evaluation). The default `.git/hooks` fingerprint stays bound into the safety digest.

## Classification

Disposition values: **neutralized** (plan succeeds, plan note), **proof-not-evaluated** (plan succeeds, named proof `not-evaluated`), **refused** (no plan, named class), **retained** (unchanged behavior).

| Key or condition | Planning | Apply |
| --- | --- | --- |
| `core.hooksPath` | neutralized | refuses a hook-running effect |
| `hook.*` | refused (`git-unsafe-configuration`) | refused |
| default executable `.git/hooks/*` | fingerprint retained, noted | refuses a hook-running effect |
| `core.fsmonitor` | neutralized | status checks neutralized, noted |
| `submodule.*` | neutralized | retirement retains `submodule-present` |
| `core.pager`, `core.editor`, `core.sshCommand`, `core.askPass`, `credential.*`, `gpg.*` | neutralized (never invoked, environment stripped), noted when set | unchanged |
| `diff.external`, `diff.<d>.textconv` | neutralized (`--no-ext-diff --no-textconv`) | unchanged |
| executable `filter.*`, `merge.*.driver` | `merge-noop` proof-not-evaluated | existing stage2 behavior retained |
| any `merge` attribute (`merge`, `-merge`, `merge=union`, macro `binary`) in `.gitattributes` of the `U`, `B` or `M` trees | `merge-noop` proof-not-evaluated, class `merge-attributes-present` | re-evaluation reads the same sources and retains |
| `$GIT_DIR/info/attributes` that is a symlink, a special file, over 1 MiB or has any non-blank, non-comment line | binary classification of patch classes and `merge-noop` proof-not-evaluated, class `info-attributes-present` | same |
| `diff=<driver>` attribute with a named driver, `diff.<d>.binary`, `diff.<d>.textconv` | patch classes `not-evaluated: diff-driver-attribute` | same |
| sentinel `filter.unspecified|unset|set.*` | refused | refused |
| `include.path` | evaluated by Git, classified by effective keys | same |
| `includeIf.*` | refused | refused |
| `core.attributesFile` | refused | refused |
| `extensions.worktreeConfig`, `extensions.partialclone`, `remote.*.promisor`, `core.alternateRefsCommand`, `core.sparseCheckout*` | refused | refused |
| `info/grafts`, `shallow` | all proofs `history-override` | re-evaluation retains |
| `objects/info/alternates` | all proofs `unsupported-metadata` (existing refusal retained) | retained |
| `refs/replace/*` | ignored by `--no-replace-objects`, note | same |

## Test matrix

Each row is a temporary-repository fixture; hook, filter, driver, pager and similar helpers append to a marker file. Expected result is the dispositions above; the marker must be absent in every row.

| ID | Condition | Planning | Apply |
| --- | --- | --- | --- |
| C1 | local `core.hooksPath` with `reference-transaction` hook | neutralized, plan succeeds | refuses ref deletion, class named |
| C2 | global `core.hooksPath` | neutralized | same |
| C3 | `submodule.x.url`, `submodule.x.update=!helper`, a gitlink | neutralized | unchanged |
| C4 | `submodule.recurse=true` | neutralized | unchanged |
| C5 | `core.fsmonitor=helper` | neutralized | status neutralized |
| C6 | `filter.x.clean=helper` with matching attribute | `merge-noop` not-evaluated, other proofs run | stage2 behavior |
| C7 | `merge.x.driver=helper` with matching attribute | `merge-noop` not-evaluated (`git-unsafe-configuration`) | stage2 behavior |
| C28 | union repro: branch deletes a line the base changed, `merge=union` in `info/attributes` | `info-attributes-present`, not proven although `merge-tree` would exit 0; control without the attribute: exit 1, `conflict` | re-evaluation retains |
| C29 | same repro with `merge=union` in an in-tree `.gitattributes` of the base only, the branch only, the merge base only | each `merge-attributes-present` | same |
| C30 | `* binary` macro, `-merge` or `merge=binary` | `merge-attributes-present` | same |
| C31 | user macro `[attr]m merge=union` used by a path | `merge-attributes-present` | same |
| C32 | clean tree without merge attributes (other attributes such as `text`, `eol` allowed) | `merge-noop` evaluates and can prove; a checked-out worktree `.gitattributes` with `merge=union` is ignored by `--attr-source` | same sources, same result |
| C8 | `diff.external=helper`, `diff.x.textconv=helper` | neutralized, all proofs run | unchanged |
| C9 | `includeIf` | refused, class named | refused |
| C10 | `extensions.partialclone` | refused | refused |
| C11 | umbrella: three gitlinks, `submodule.*` set | plan succeeds | unchanged |
| C12 | `hook.<name>.command` | refused | refused |
| C13 | `include.path` file defining `core.hooksPath` | neutralized via effective key | as C1 |
| C14 | `core.pager`, `core.editor`, `credential.helper`, `core.sshCommand`, `gpg.program` set to helpers | plan succeeds, notes, markers absent | unchanged |
| C15 | `core.attributesFile` | refused | refused |
| C16 | `extensions.worktreeConfig` | refused | refused |
| C17 | `core.sparseCheckout` | refused | refused |
| C18 | `core.alternateRefsCommand=helper` | refused, marker absent | refused |
| C19 | sentinel `filter.unspecified.clean`, `filter.unset.*`, `filter.set.*` | refused | refused |
| C20 | non-reserved executable `filter.evil.*`, `filter.fixture.*`, `filter.reviewer.*` with matching attributes | `merge-noop` not-evaluated | stage2 behavior |
| C21 | executable `.git/hooks/reference-transaction` | note, hooks fingerprint bound | refuses ref deletion |
| C22 | replace refs present | note `replace-refs-ignored`, proofs use true graph | same |
| C23 | `info/grafts` present | proofs `history-override` | re-evaluation retains |
| C24 | shallow clone (`shallow` file) | proofs `history-override` | retained |
| C25 | `objects/info/alternates` present | proofs `unsupported-metadata` | retained |
| C26 | tip with gitlink difference | `merge-noop` `gitlink-change`, no submodule repository opened | unchanged |
| C27 | planning versus apply argument list | identical `planningConfigArgs` | identical |
| M1 | mutant: drop `-c core.hooksPath` | C1 must fail | n/a |
| M2 | mutant: inherit global config | C2 must fail | n/a |
| M3 | mutant: inherit `GIT_CONFIG_PARAMETERS` / `GIT_CONFIG_COUNT` with a hooksPath value | C1 must fail | n/a |
| M4 | mutant: drop `merge.renormalize=false` or `core.autocrlf=false` | a renormalizing fixture changes `merge-noop` and must fail | n/a |
| C33 | file marked by `* binary` or `*.bin -diff` in the branch tip tree, differing by one byte | retained, `patch-exact` only; identical patch is `patch-exact` | same |
| C34 | binary by NUL content, no attributes | numstat `-`: verbatim only | same |
| C35 | text file with `-text` set, differing only by CRLF | treated as binary: retained | same |
| C36 | `binary` attribute only in the checked-out worktree `.gitattributes`, not in the pinned tree | ignored: path judged by tree and content | same |
| C37a | untracked `.gitattributes` with `e.raw diff` on a NUL file, and with `-diff` on a text file | classification and patch text unchanged (judged by the pinned tree only) | same |
| C37b | `$GIT_DIR/info/attributes` with any non-blank, non-comment line (including a macro use such as `*.dat tm` with `[attr]tm diff` defined in a tree `.gitattributes`), as a symlink, a special file, over 1 MiB, and resolved through a linked worktree's common git directory | binary classification and `merge-noop` `not-evaluated: info-attributes-present`; `ancestor` and `no-net-change` unaffected; absent or comment-only file ignored | same |
| C37c | `$XDG_CONFIG_HOME/git/attributes` with `merge=union`, real Git | verdict unchanged, never falsely proven | same |
| C37d | tree attribute `*.dat diff=d` with `diff.d.binary=false` on a NUL file | patch classes `not-evaluated: diff-driver-attribute` | same |
| C37e | tree attribute `*.dat diff=d` with `diff.d.textconv=helper` | `not-evaluated: diff-driver-attribute`, helper marker absent | same |
| C37f | unit test of the exact environment the executor builds: private empty `HOME`/`XDG_CONFIG_HOME`, null `GIT_CONFIG_GLOBAL`/`GIT_CONFIG_SYSTEM`, `GIT_CONFIG_NOSYSTEM=1`, `GIT_ATTR_NOSYSTEM=1`; no variable can redirect Git's system attributes path, so the real-Git assertion covers global and XDG only | environment equals the expected list | same |
| M5 | mutant: omit the attribute-source scan or `--attr-source` | C28..C31 must fail | n/a |
| M6 | mutant: inherit the caller's `HOME`/`XDG_CONFIG_HOME` or drop `GIT_ATTR_NOSYSTEM` | C37c or C37f must fail | n/a |
| M7 | mutant: ignore `info/attributes` | C37b must fail | n/a |
