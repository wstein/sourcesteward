# P4b: terminal answers and same-run confirmation receipt

Scope: Spec 044 T013b and T013a, FR-017/FR-018, SC-014/SC-015. Based on
merged PRs [#320](https://github.com/wstein/sourcesteward/pull/320) and
[#321](https://github.com/wstein/sourcesteward/pull/321). This records source
checks, not release authorization or application to a real workspace. Task and
progress checkboxes are unchanged. Final repository checks and exact-head review
belong in the PR body.

## Implemented boundary

- Every human store/apply answer and typed word uses `tidyConfirmOpener`, whose
  production opener is `/dev/tty`. Questions are written to the same handle that
  supplies their answers. Selection still uses stdin, without reading ahead into
  later answers. Separate apply retains its strict answer table and explicit-ID
  `--yes` behavior.
- Both wizard views check the controlling terminal before `planset.Build`.
  Missing terminal means no planner calls, sub-plans or plan-set records. The
  janitor's own-only store question also reads the terminal.
- `Build` returns `(Set, *Receipt, error)`. Only `Build` allocates the hidden
  receipt state, after every required word was confirmed. Own-only and failed
  builds return none. Pointer copies share the full digest, exact sorted words
  and one atomic consumption bit.
- `Apply` consumes that state as its first statement, including refusals and
  interruption. Nil, zero and used receipts cover nothing. Only the existing
  confirmation loop may skip a word, after mode/scope checks, with `Authorize`
  true and matching full digest and words. Stored IDs remain the same 12-character
  prefix of the same canonical digest; stored formats are unchanged.
- The janitor handoff asks its default-no apply question on the terminal and
  passes its receipt into shared apply. The question states irreversible counts
  and, for a foreign selection, explains that the earlier words cover the plan.
  Separate apply passes nil, and `plan --save` discards the receipt result.
- Expert contract-3 and contract-4 apply gates are untouched. No receipt enters
  them. The typed terminal prompt remains the protection; markers are an
  accident guard. No expiry, nonce, process-ancestry check or extra approval is
  introduced.
- Partial apply results remain visible. A failure after effects does not claim
  nothing was applied. A declined question leaves the stored plan unapplied.

## Test-first history

The integration tests of the commits "require terminal-authorized wizard apply handoff", "exercise receipt handoff through real plan-set apply" and "preserve partial apply evidence in wizard handoff" ran and failed
against the merged print-only handoff: no apply occurred, no terminal apply
question appeared, and no partial result could be reported. They pass with the
integrated implementation.

The digest and receipt test-first commits ("pin the full canonical plan digest", "require single-use confirmation receipts") initially
failed to compile because the new APIs did not exist. These are API-absent
checkpoints, not semantic test failures. Their behavioral evidence is the
runtime assertion and mutation coverage below. Terminal changes were exercised
with compiled failing split-stream/pre-build tests before implementation.

The canceled-apply test and native-PTY fixture migration are characterization
and integration coverage, not additional claims of prior semantic RED runs.

## Reproducible focused checks

Run from the repository root:

```sh
go test ./internal/cli -run 'TestJanitor|TestTerminal|TestTidyApply' -count=1
go test ./internal/tidywizard/owner ./internal/tidywizard/planset -run 'TestPlanDigest|TestReceipt|TestBuildReturnsNoReceipt' -count=1
go test ./internal/cli -run 'TestApplyPromptsOnARealTerminalThroughRunWithStdin|TestBareTidyAsksOnARealTerminalThroughRunWithStdin' -count=1 -timeout 20s
```

These operate on temporary state and backend doubles or temporary Git fixtures.
Linux entry-point tests use different PTYs for stdin and the controlling-terminal
seam, with opposing answers. No real workspace is retired by these checks.

| Acceptance property | Test evidence |
| --- | --- |
| One typed word, then real shared apply | `TestJanitorReceiptReachesRealPlanSetApply`, `TestReceiptSkipsEachConfirmedWordExactlyOnce` |
| Default no and partial reporting | `TestJanitorApplyDeclineKeepsStoredPlanWithoutApplying`, `TestJanitorApplyReportsPartialEffectsWithoutClaimingNothingApplied` |
| Nil, zero, pointer copies and concurrency | `TestReceiptNilZeroAndCopiesCannotSkipAgain`, `TestReceiptConcurrentCopiesShareOneUse` |
| Consume before loading or refusing; canceled attempt | `TestReceiptConsumedBeforeEveryRefusal`, `TestReceiptConsumedByCanceledApply` |
| Full digest and exact word equality | `TestReceiptDigestAndWordsAreIndependentBindings`, `TestPlanDigestPreservesCanonicalBytesAndPlanIDPrefix` |
| Existing scope, mode, authorization and stale gates | `TestReceiptCannotReplaceModeScopeOrApplyAuthorization`, `TestReceiptPreservesStaleSubPlanRefusal` |
| Only Build allocates; no receipt on incomplete confirmation | `TestReceiptHasOneSharedPointerAndOneBuildAllocation`, `TestBuildReturnsNoReceiptWithoutEveryConfirmedWord` |
| Store/apply use the terminal instead of stdin | `TestTerminalStoreAnswerIgnoresStdin`, `TestTerminalRowApplyAnswerIgnoresStdin`, `TestTidyApplyPromptOnATerminal` |
| No terminal means no sub-plan or stored set | `TestTerminalUnavailableRefusesBeforeAnyPlan` (own/foreign, both views, planner calls and complete store listing) |
| No stdin read after selection | `TestTerminalNoStdinReadAfterSelection` |
| Separate apply receives no receipt | `TestTidyApplyPlanIDWithYesAuthorizesAndReportsJSON` |

## Causal mutations

Receipt author checks killed 14 runtime mutations: ignoring the receipt; removing
or truncating the digest comparison; accepting missing or extra words; ignoring
`Authorize`; consuming after lookup or mode checks; putting the consumption bit
in the copied value; honoring zero/nil receipts; bypassing mode/scope gates;
allocating state outside Build; and truncating the full digest helper. The
allocation probe fails its runtime AST assertion; it is not a compile failure.

Terminal author checks killed six compiled behavioral mutations: reading the
separate answer, store answer or row-view apply answer from stdin; bypassing the
shared typed-word opener; removing the pre-Build terminal check; and allowing
selection read-ahead. The removed precheck produced planner calls and stored
records, so the store-list assertion is not vacuous.

These are author-executed checks, not an independent security verdict. The exact
integrated head still requires the foreman's independent review and green CI.
