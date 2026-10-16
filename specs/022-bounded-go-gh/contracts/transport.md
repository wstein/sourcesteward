# Hosting transport contract

The Spec 020 CLI and published JSON contracts are unchanged, except authentication refusal now reports `hosting-api-unavailable` with `boundary.network=false` (before any request); HTTP read failures use new messages. This contract replaces only its API subprocess implementation.

## Selection and authentication

Native ownership and explicit repository selection execute before network initialization. Local `repo` resolution never authenticates. Remote reads retain trusted gh >=2.102.0 preflight, configuration/symlink refusal and external neutral cwd. Authentication is `gh auth token --hostname HOST`, executed with 16 KiB-capped stdout/stderr and inherited cancellation; raw errors/tokens are never rendered. Tokens must be nonempty visible ASCII on one line, allowing an optional trailing newline.

## SDK configuration and request allowance

Pin official go-gh/v2 v2.16.2. Construct api.NewHTTPClient with explicit Host, APIHost, AuthToken, guarded Transport, deadline-aware requests and redirect refusal; cache/logging/environment debug are disabled. API routing is github.com → api.github.com, *.ghe.com → api.<host>, other Enterprise → <host>/api/v3. Request method is GET. Allowed suffixes are exactly generated `repos/OWNER/NAME/pulls/NUMBER`, `repos/OWNER/NAME/commits/SHA/check-runs?per_page=100&page=N` and corresponding statuses; identities and bounded page N are validated. No userinfo, fragments, alternate schemes/ports, response-provided URLs, placeholders or arbitrary forwarding.

## Bounds and errors

Each raw and normalized body is at most 1,048,576 bytes, plus at most one sentinel byte consumed to detect excess; applies equally to HTTP success and failure. Bodies close on all exits. HTTP errors are safe generic messages within existing hosting diagnostics; API body, auth token and raw transport detail are not exposed. Existing overall 30-second deadline, 10-page/1000-record policy and final JSON result limit stay unchanged. Clients never mutate global environment or shared transport settings.

## Fixture seam

`InspectWithTransport(context.Context,string,Options,http.RoundTripper) Result` is internal-package test integration, used with real native operations and shared CLI rendering. Nil selects production transport. The adapter wraps injected transports with the same destination/body guards. No production endpoint override is added. Real-process conformance covers local/dispatch/auth-refusal; in-process native Git/jj conformance covers successful PR/CI HTTP behavior and schemas.

HTTP content encoding is restricted to identity: disable automatic decompression and refuse compressed responses so raw-body bounds remain observable. Skip SDK default headers, supply fixed Accept/API-version/User-Agent headers, and keep any SDK cache directory inside the neutral temporary directory while caching remains disabled.
