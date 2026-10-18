# Spec 026 WI6 dispatch source candidate

T028 and T029 have independent exact-source clearance at `39507ce` and
`c1194ae`, respectively. T030 adds the command, fresh native/online gate
recomputation, stale-id refusal and the unpublished dispatch result schema.
The feature remains planned. Full final-candidate checks and independent T030
review remain pending; this source candidate is not live dispatch acceptance.

## Executed failing-first evidence

The T030 typed operation stub compiled but failed native plan/apply, stale
main/contract/gate, failed-gate apply, tag-annotation and effect-accounting
fixtures. The command skeleton then failed parser, selector-routing, rendering
and exit-status tests. The result schema initially failed because its file was
absent; its staging test failed until the new schema was explicitly excluded
from publication. Typed read-authentication and cancellation regressions also
failed before correction. Credential error text remains byte-identical.

Focused tests now pass for plan then apply, exactly one writer invocation,
no writer without apply, locally unchanged bytes and metadata, exact committed
contract-byte hashing, main-ref equality, changed main/contract/gate ids,
matching failed-gate refusal, annotated/lightweight tags, named-product
inference and selection errors, fetch hosts/lock-file disclosure, explicit-host
native repository identity, cancellation, authentication, status-only write
errors, CLI validation before operations, real-command JSON refusal, human
rendering and schema mutations. No plan file is stored.

Seven isolated compiling mutants failed behavioral assertions: implicit apply,
stale-id admission, failed-gate admission, missing remote-main equality,
substituted contract hash, lost host identity and lost inferred tag product.

## Native source fixtures

The existing executables report jj 0.45.1 and jj 0.46.0; local Git reports
2.56.0. Focused native dispatch tests passed using each available jj lane and
its existing colocated, standalone and secondary-workspace fixture helpers.
Each seeds remote main from a separate Git-owned temporary repository using
native jj local fetch, changes the configured URL without fetching it, then
uses fake online/read/write adapters. Native commit plan/apply retains the
qualified jj root and commit. Tag mode refuses before online reads. Files,
operation log and working-copy identity are compared before and afterward.
GitButler refusal also precedes online access. No raw Git write runs inside a
jj-owned workspace. This is not a pinned minimum-Git lane claim.

## Limits and later actions

The remaining full normal/race/source gates and exact-head independent review
are recorded in the coordinator handoff. Node/theme and actual Hugo packaging
retain the existing environment/authorization limits. The schema remains on
the unpublished staging list; no documentation artifact was built.

All release HTTP and dispatch operations in acceptance fixtures are fake. No
real gh authentication, external request, workflow dispatch, project command,
product or image build, installation, publication, tag, release or consumer
activation was performed. Real GitHub dispatch and Enterprise behavior remain
unverified. Each future build requires its own explicit task-context approval;
a printed plan grants none.

## First independent T030 review correction

The original T030 candidate passed full source checks, but independent review
found that explicit hexadecimal tag names were rejected by positional argument
validation or inferred as commit selectors. Failing regressions reproduced this
for short, 40-character and 64-character names, including the wrong jj refusal.

The correction preserves explicit tag mode from `--tag` through native selection
and online gates. It bypasses only the positional validator's hexadecimal
ambiguity rule; ordinary positional planning is unchanged. Annotated native Git
fixtures keep the subject tag present in remote evidence, verify its exclusion
from build existence/monotonicity checks, then plan and apply once. Commit mode
retains those existing-tag gates. Both jj versions and all three layouts refuse
explicit tag mode before online reads and preserve native state. Focused race
checks pass. Four isolated compiling mutants detect CLI heuristic reuse, lost
selector mode, and lost remote-tag exclusions in each online gate. Final checks
and the same independent reviewer's assessment of the folded head remain pending.
