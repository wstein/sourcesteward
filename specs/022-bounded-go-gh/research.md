# Research: Bounded official go-gh transport

Date: 2026-10-16. All design unknowns resolved; implementation evidence is separate.

## SDK HTTP boundary

**Decision:** pin go-gh/v2 v2.16.2, construct `api.NewHTTPClient`, own status/body decoding, and cap both underlying raw and middleware-normalized bodies.
**Rationale:** the prior isolated evaluation consumed >2 MiB through REST success/error convenience APIs. Limiting returned success bodies does not limit error paths. A sentinel read detects excess without unbounded buffering.
**Alternatives:** retain gh api (bounded but owner requests SDK); REST convenience methods (rejected for buffering); SDK fork (unnecessary).

## Authentication and configuration

**Decision:** retain trusted absolute gh path/version/config/symlink checks; call bounded `gh auth token --hostname HOST` from neutral cwd under operation context. Set SDK AuthToken/Host/APIHost/Transport explicitly; disable logging/cache and environment-driven logging. Keep token state local to one client.
**Rationale:** gh honors caller auth conventions; SDK TokenForHost/ExecContext may spawn/buffer without caller bounds. Environment mutation would race concurrent inspections.
**Alternatives:** SDK auth helpers (unbounded fallback); custom gh config parser (duplicates conventions); repository-discovered SDK context (violates native ownership).

## Destination and proxy policy

**Decision:** derive API origin deterministically from validated host: github.com → api.github.com; *.ghe.com → api.<host>; other Enterprise → <host>/api/v3. Only generated PR/check/status GET paths and bounded page query pairs are accepted; deny redirects. Explicitly prevent socket/API override config from selecting another destination. A cloned standard HTTP transport may honor caller-controlled proxy infrastructure; repository data cannot select proxy configuration. Never mutate global default transport.
**Rationale:** explicit APIHost is an authenticated destination, not a security boundary by itself. Guard the exact request before credential transmission.
**Alternatives:** configured api_host (reject); generic request forwarding (out of scope); production test endpoint override (reject).

## Deterministic acceptance

**Decision:** internal exported `InspectWithTransport` retains real native operations with an injected fake RoundTripper. Adapt CLI/contracts/native remote success tests to in-process HTTP; keep actual binary/private-extension tests for local resolution and no-auth refusal.
**Rationale:** SDK HTTP bypasses fake gh api executables. No network endpoint environment escape hatch is needed. Native Git and jj PR/CI evidence remains mandatory in both pinned lanes.
**Alternatives:** fake production endpoint (unsafe configuration surface); mock entire inspection (does not prove shared operations); drop remote conformance (violates Tier 1 commitment).

## Pinned evidence and sources

Prior evaluation: `/private/tmp/sourcesteward-go-gh-evaluation/docs/evaluations/go-gh-v2.md`; six probe groups and race/vet/module verification passed, with no live API or real keyring calls. SDK source inspected at v2.16.2: [options](https://github.com/cli/go-gh/blob/v2.16.2/pkg/api/client_options.go), [HTTP](https://github.com/cli/go-gh/blob/v2.16.2/pkg/api/http_client.go), [REST](https://github.com/cli/go-gh/blob/v2.16.2/pkg/api/rest_client.go), [auth](https://github.com/cli/go-gh/blob/v2.16.2/pkg/auth/auth.go), [subprocess](https://github.com/cli/go-gh/blob/v2.16.2/gh.go). Research establishes design choices, not migration acceptance or live Enterprise/keyring compatibility.

HTTP content encoding is restricted to identity: disable automatic decompression and refuse compressed responses so raw-body bounds remain observable. Skip SDK default headers, supply fixed Accept/API-version/User-Agent headers, and keep any SDK cache directory inside the neutral temporary directory while caching remains disabled.
