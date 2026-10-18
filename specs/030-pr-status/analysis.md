# Cross-artifact analysis

**Status**: Not independently reviewed; incomplete checkpoint.

Known counterexamples that the complete design must resolve:

1. An open PR has non-null `merge_commit_sha`: it is not evidence of integration.
2. A merged rebase PR has a result SHA different from its head: do not attach head review/attestation to the new SHA.
3. Adding fields to strict schema v1 breaks old validators even though JSON consumers may ignore them.
4. Recreating a 30-second context for each watched GET permits an unbounded total run.
5. A response arrives before the deadline but decoding or output blocks past it: request-only cancellation fails an inclusive deadline claim.
6. Observed A→B→A heads cannot erase the observed head movement; unobserved ABA cannot be excluded by polling.
7. Missing `merged` cannot be treated as false, and a denied/404 response cannot be treated as closed-unmerged.
8. A nullable mergeability field must neither imply closure nor trigger polling outside explicit watch.

Resume: finish the exact contract and fixtures within Spec 030 only, then return a frozen exact-SHA complete-artifact package to dispatch for an independent strongest-tier review. Runtime implementation remains prohibited.
