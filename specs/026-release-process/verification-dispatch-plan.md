# Spec 026 T028 pure dispatch-plan source candidate

This bounded candidate implements only the stateless dispatch identity in
`internal/release/dispatch.go`. It adds no command, transport operation, state
file or workflow. T029 and T030 remain unimplemented by this candidate, and
Spec 026 remains planned. Independent exact-head review is pending.

## Executed failing-first evidence

The initial focused test run failed to compile because `DispatchPlan` and
`DispatchInputs` were absent. With the typed model and empty method stubs, the
same tests compiled and failed at runtime: canonical bytes were empty, changed
identity fields retained the same id, and malformed identities were accepted.
After implementation the focused normal and race tests passed.

A literal canonical-byte fixture pins all identity fields, nested key ordering,
two-space indentation, empty product and unused selector values, and the final
newline. The expected id is SHA-256 of those independently specified bytes.
Every identity field is mutated: valid changes produce a distinct id; fixed-ref
and conflicting-selector changes refuse. The serialized identity also binds
those fixed/refused fields. Tag mode separately checks a changed tag, Unicode
and minimal JSON escaping. Both full Git object-id widths are accepted.
Malformed destination, workflow filename, mode, selector, product, version,
commit, contract digest and verdict fixtures return neither bytes nor an id.

## Source checks and limits

Executed checks: `go test ./internal/release -run '^TestDispatchPlan' -count=1`,
`go test -race ./internal/release -run '^TestDispatchPlan' -count=1`,
`go vet ./internal/release`, `go mod verify`, source documentation checks
(258 authored documents), architecture checks, formatting and
`git diff --check` passed.
Repository-wide source checks remain pending coordinated execution; these
focused checks do not replace the required full normal and race suites.

This model validates identity shape only. The future caller must select and
validate the contract and version scheme, resolve the native subject, check tag
annotation where supported, establish ancestry against the same remote main
value and recompute fresh online gates. A digest conveys no build approval.
No real credentials, HTTP request, product or documentation build, workflow
run, tag, release, publication or consumer activation was performed.

## First independent review correction

The first independent review found that a contract-valid leading-dot workflow
basename was refused by the dispatch identity's additional first-character
restriction. A contract-to-dispatch regression first failed for `.release.yml`
and `.release.yaml` after the existing contract validator accepted each full
workflow path. The narrow alphabet correction preserves each basename in the
canonical identity and admits its digest; all focused dispatch race tests pass.

Two isolated source-overlay mutants compiled and failed behavioral assertions:
restoring the original first-character restriction fails the new contract
regression, and weakening basename confinement fails the existing malformed
workflow path and traversal refusals. No transport or contract-validation rule
was changed. Full source checks and independent exact-head review of the folded
correction remain pending.
