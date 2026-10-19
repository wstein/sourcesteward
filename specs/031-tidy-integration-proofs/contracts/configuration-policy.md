# Planning configuration policy

Subordinate to the [specification](../spec.md) FR-019..FR-023.

## Planning environment

One constant argument list `planningConfigArgs`, used by planning and by apply re-evaluation (a test asserts equality): `-c core.hooksPath=/dev/null -c core.fsmonitor=false -c submodule.recurse=false -c core.untrackedCache=false -c merge.renormalize=false -c merge.renames=true -c merge.directoryRenames=conflict -c merge.renameLimit=1000 -c diff.renames=true -c diff.renameLimit=1000 -c core.autocrlf=false -c gc.auto=0 -c maintenance.auto=false -c protocol.ext.allow=never`, with `--no-optional-locks`, `--no-replace-objects`. Environment: the existing sandbox environment with no inherited `GIT_*` override (explicitly including `GIT_CONFIG_PARAMETERS`, `GIT_CONFIG_COUNT`, `GIT_CONFIG_KEY_*`, `GIT_CONFIG_VALUE_*`, `GIT_EXTERNAL_DIFF`, `GIT_PAGER`, `GIT_ASKPASS`), `GIT_CONFIG_NOSYSTEM=1`, global configuration replaced by an empty private file for content-evaluating commands, `GIT_NO_LAZY_FETCH=1`, `GIT_TERMINAL_PROMPT=0`. One admission read lists effective configuration with origins (global and system included) for classification and notes; it runs no helper.

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
| `core.pager`, `core.editor`, `core.sshCommand`, `credential.*`, `gpg.*`, `core.askPass` | unreachable, noted | unchanged |
| `diff.external`, `diff.<d>.textconv` | neutralized (`--no-ext-diff --no-textconv`) | unchanged |
| executable `filter.*`, `merge.*.driver` | `merge-noop` proof-not-evaluated | existing stage2 behavior retained |
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
| C7 | `merge.x.driver=helper` with matching attribute | `merge-noop` not-evaluated | stage2 behavior |
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
