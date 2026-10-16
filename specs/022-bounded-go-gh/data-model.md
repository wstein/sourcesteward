# Data model: bounded transport

No published data model changes. Options, Result, Repository, PullRequest, Check, CommitStatus, Boundary and diagnostics retain their existing shapes/schema versions.

## Internal client

One client owns validated selected host/API origin, context-bound HTTP middleware, selected-host token and trusted gh runner/neutral directory cleanup. Credentials are never Result fields or diagnostic payloads. Host-specific clients do not share auth state.

## Response lifecycle

Validated endpoint → context-bound fixed GET → guarded raw response (1,048,576-byte cap plus sentinel) → SDK normalization → bounded final body read → sanitized status handling → existing JSON validation. Every body closes on success/failure. Overflow, cancellation, redirect, wrong origin/path or transport failure returns an existing hosting error boundary; it never becomes a complete empty observation.

## Fixture injection

`InspectWithTransport(ctx,path,Options,http.RoundTripper)` supplies only an HTTP transport. Native identity/resolution, auth preflight, SDK middleware, response bounds and shared result generation still execute. Default production Inspect selects the guarded standard transport. No CLI option or environment endpoint override is created.
