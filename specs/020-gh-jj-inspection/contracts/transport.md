# Bounded GitHub transport

Transport mechanism and bounds are superseded by [Spec 022](../../022-bounded-go-gh/spec.md) when the SDK migration lands. Native behavior, CLI options and published JSON contracts remain unchanged. The original implementation decisions below are historical.

Allowed operations are `gh api --hostname HOST --method GET ENDPOINT` with fixed API headers as needed. No response-provided URL, caller-provided endpoint, placeholders, fields, aliases, extension calls, browser, GraphQL or mutation methods are supported.

| Purpose | Explicit endpoint |
| --- | --- |
| Numbered PR | `repos/OWNER/NAME/pulls/NUMBER` |
| Check runs | `repos/OWNER/NAME/commits/SHA/check-runs?per_page=100&page=N` |
| Commit statuses | `repos/OWNER/NAME/commits/SHA/statuses?per_page=100&page=N` |

Repository command makes zero API requests. PR request verifies returned number and base repository identity. CI obtains PR, pins all reads to its full head SHA, validates check-run SHA identity, bounds/validates every page and resamples PR head/base identity before success. Failures retain valid evidence only with complete=false.

The executable comes from trusted caller PATH; repository-relative executable/PATH injection must be rejected. gh runs outside target in a private neutral cwd with stdin closed, noninteractive auth, pager/browser/editor/debug/repository-context overrides removed or explicitly disabled. Auth tokens and current-user credential config are accepted inputs for the selected host; they must not be echoed. Do not read repository-linked gh configuration or execute helpers. No gh `--paginate` unbounded reads: pages and decoded records are counted by hosting.

Each collection is at most 10 pages; all CI collections combined are at most 1000 records. Missing/invalid collections, inconsistent totals, duplicate IDs, unexpected shape/identity and inability to prove exhaustion are errors. Output and stderr are independently bounded to 1 MiB; final serialized result is bounded to 1 MiB. Hosting deadline is 30 seconds including native discovery, API calls and revalidation, respecting earlier caller cancellation.

403, 404, authentication failure, rate limit and malformed response are operational failures, not empty observations. Diagnostics are stable/sanitized and do not include raw subprocess stdout/stderr or tokens. API data is untrusted display/JSON data; URLs are not followed and terminal control bytes are escaped in human rendering.
