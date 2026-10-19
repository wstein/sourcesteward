# Planning configuration policy

Subordinate to the [specification](../spec.md) FR-019..FR-023.

## Planning environment

Every native command in planning: argv built from typed constants and full OIDs; environment of the existing sandbox environment (no inherited `GIT_*` overrides, no credential/loader/editor variables); `GIT_CONFIG_NOSYSTEM=1`; `GIT_CONFIG_GLOBAL` and `XDG_CONFIG_HOME` pointing at an empty private file/dir for content-evaluating commands; `-c core.hooksPath=/dev/null -c core.fsmonitor=false -c submodule.recurse=false -c core.untrackedCache=false -c protocol.ext.allow=never`; `--no-optional-locks`, `--no-replace-objects`; `GIT_NO_LAZY_FETCH=1`. One separate admission read lists effective configuration with origins (global/system included) for classification and the plan note; it executes no helper.

## Classification

| Key class | Planning | Apply | Reason |
| --- | --- | --- | --- |
| `core.hooksPath`, `hook.*` | neutralized, noted | refuse if the declared effect runs hooks (ref delete: `reference-transaction`) | hooks never run in read-only planning |
| `core.fsmonitor` (any value) | neutralized (`false`), noted | neutralized for status checks, noted | hook-based monitor would execute |
| `submodule.*`, `submodule.recurse` | neutralized, noted; gitlinks are data | refuse retirement when an initialized submodule is present | tidy never runs `git submodule` |
| `core.pager`, `core.editor`, `core.sshCommand`, `credential.*`, `gpg.*`, `core.askPass` | unreachable; noted when set | unchanged | no planning command invokes them |
| `diff.external`, `diff.<d>.textconv` | neutralized; refuses `merge-noop` when `diff.external` set | unchanged | content evaluation |
| `filter.<n>.clean|smudge|process` | refuse content-evaluating proofs (`merge-noop`); other planning unchanged | existing refusal | executes during content evaluation |
| `merge.<n>.driver` | refuses `merge-noop` | existing refusal | merge driver executes |
| `include.*`, `includeIf.*` | refuse | refuse | effective value not reproducible under neutralized environment |
| `extensions.worktreeConfig`, `extensions.partialclone`, `remote.*.promisor`, `core.alternateRefsCommand`, `core.sparseCheckout*` | refuse | refuse | unchanged Spec 029 refusals |
| reserved sentinel `filter.unspecified|unset|set.*` | refuse | refuse | unchanged |

## Test matrix

Each row is one temporary-repository fixture with a helper script that appends to a marker file:

| ID | Configuration | Planning expectation | Marker | Apply expectation |
| --- | --- | --- | --- | --- |
| C1 | local `core.hooksPath` to hook dir with `reference-transaction` | plan succeeds, note `hooks` | absent | refuses ref deletion, names class |
| C2 | global `core.hooksPath` | plan succeeds, note | absent | same |
| C3 | `submodule.x.url`, `submodule.x.update=!helper` with a gitlink | plan succeeds, note | absent | unchanged |
| C4 | `submodule.recurse=true` | plan succeeds, neutralized | absent | unchanged |
| C5 | `core.fsmonitor=helper` | plan succeeds, note | absent | status neutralized |
| C6 | `filter.x.clean=helper` with matching attribute | plan succeeds except `merge-noop` not-evaluated | absent | existing refusal |
| C7 | `merge.x.driver=helper` with matching attribute | `merge-noop` not-evaluated | absent | existing refusal |
| C8 | `diff.external=helper` | `merge-noop` not-evaluated | absent | unchanged |
| C9 | `includeIf` | plan refused, class named | absent | refused |
| C10 | `extensions.partialclone` | refused | absent | refused |
| C11 | Anchorage-like umbrella: three gitlinks, `submodule.*` set | plan succeeds | absent | unchanged |
| M1 | mutant: drop `-c core.hooksPath` | C1 marker appears: test must fail | n/a | n/a |
| M2 | mutant: inherit global config | C2 marker appears or digest differs: fail | n/a | n/a |
