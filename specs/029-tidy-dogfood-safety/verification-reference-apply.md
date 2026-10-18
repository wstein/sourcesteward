# Reference apply author evidence

This bounded T022–T023 candidate remains internal: it adds local-reference apply,
recovery and journal coordination. It does not activate v3 CLI commands, tracking
apply or workspace retirement. Exact source review and final integrated source
checks remain required before downstream handoff.

## Reviewed foundations

The cleared T020–T021 helper is preserved byte for byte from
`03c54111f5f056dbcc1c4588383a97a200acd6ba`. The initial 52-path dependency receipt
and earlier five-path correction remain intact. The separate
[journal correction receipt](journal-dependency-corrections.json) imports only
three reviewed B blobs from `9a420f679de4ecc0f8f8c3895e324a2ac15c2125`.
A native and C hosting dependencies retain their reviewed bytes.

## Executed behavior evidence

Native fixtures isolate Git environment, HOME and XDG configuration before their
first Git invocation and skip explicitly if the native tool is unavailable.
Temporary native repositories exercise exact recovery/deletion, nine provider
heads including seven stacked PR bases, three-action write-ahead ordering,
completed read-only retry, stale/tampered/unauthorized refusals, provider failure
before/after recovery and while prepared, symbolic/OID races, lost native commit
acknowledgment, durable-write failures and a partially completed batch.

The original cancellation and live post-intent refusal probes distinguish A's
actual outcomes. Uncertain cancellation remains unknown. An acknowledged abort
with no COMMIT send persists terminal incomplete/aborted exactly once, retaining
recovery and prior native deltas without possible deletion. Retry preserves that
diagnostic and does not replay terminal records or invent uncertain historical
locks. These changes were exercised RED before GREEN.

Focused apply tests pass (34.316 seconds). Original raw receipt:
`/tmp/stw-unit-d-apply-focused-7gtg4fst/receipt.json`.
Six targeted safety mutants were rejected; original source bytes were restored.
Raw receipt: `/tmp/stw-unit-d-apply-mutations-4r2dhgki/receipt.json`.
Original native journal integration RED/GREEN receipts are under
`/tmp/stw-unit-d-journal-integration-zcpnba87/`.

## Acceptance limits

These fixtures use available Linux native tools. Minimum-supported Git/jj and
macOS execution remain separate acceptance gaps. No product build, installation,
real repository cleanup, fetch, remote mutation or publication is authorized by
these tests. The actual Hugo packaging test and Node checks retain the previously
recorded environment/approval limits; no tool installation is implied.

## Exact review correction

The initial frozen candidate `89aafe6804260f902a21650bf1bce770dce66a49`
passed full normal (102.658 seconds) and race (133.705 seconds) checks; its
original receipt remains `/tmp/stw-unit-d-apply-full-yub8a5vi/receipt.json`.
Independent review found one Low disclosure gap: a nil-session prepare failure
omitted the exact action ID and full ref from the durable diagnostic. The sole
runtime correction adds those two identifiers to that message. Its regression
exercises the actual failure path and incomplete retry, retaining recovery and
last acknowledged transaction state without native replay or journal writes.
Original targeted RED/GREEN receipts:
`/tmp/stw-unit-d-prepare-disclosure-9uga3qpc/`.

The identifier-concatenation candidate `e30b6c26145f0db4b26b856caecd72c5f776b194`
was not run through full aggregate checks: independent review first found that
an admitted 3,901-byte native ref overflowed the unchanged 4,096-byte message
cap. The correction keeps the action ID and uncertainty in
`native-prepare-unconfirmed`, with the exact full ref as the message of a linked
`native-prepare-target:<action-id>` diagnostic. Both persist in one terminal
write and survive unchanged incomplete retry. No text cap, schema, admission,
transaction or replay rule changes.

The actual short/native-3,901-byte cases pass. Maximum admitted 128-byte IDs and
4,096-byte refs produce a 150-byte target diagnostic code and 322-byte uncertainty
summary. Existing aggregate-entry boundaries and encoded journal bounds were
exercised with surrounding context: 1,046,932 encoded bytes admitted;
1,071,546 bytes refused. Original RED/GREEN and measured boundary log:
`/tmp/stw-unit-d-prepare-bounds-ezgxg4lx/`.

A subsequent consolidated terminal-message audit found that current target and
recovery preservation was not established after a final observation failed.
Only three failure-message literals change: confirmed abort, unknown transaction
outcome and abort-journal update failure now distinguish acknowledged transaction
facts and prior journal evidence from current roots requiring manual inspection.
The earlier successfully sampled recovery/deletion actions remain historical
observations. No state, effects, schema, budgets or transaction rules change.

Prepared OID/symbolic/recovery actors and an actual lost-abort-acknowledgment path
exercise this distinction RED before GREEN. Targeted raw receipts:
`/tmp/stw-unit-d-terminal-history-pqytx24q/`.
The unchanged prior candidate `35788b2d0f2bedd30473c7ec2faa2f997c2348c3`
passed normal (99.337 seconds) and race (127.566 seconds); its original receipt
remains `/tmp/stw-unit-d-apply-final-full-7vesc4ov/receipt.json`.
