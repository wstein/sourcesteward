# Hosting CLI and operation contract

## Commands

```text
stw hosting repo [PATH] [--repo OWNER/NAME] [--host HOST] [--remote NAME] [--json]
stw hosting pr NUMBER [PATH] [--repo OWNER/NAME] [--host HOST] [--remote NAME] [--json]
stw hosting ci NUMBER [PATH] [--repo OWNER/NAME] [--host HOST] [--remote NAME] [--json]
gh jj repo [PATH] [--repo OWNER/NAME] [--host HOST] [--remote NAME] [--json]
gh jj pr NUMBER [PATH] [--repo OWNER/NAME] [--host HOST] [--remote NAME] [--json]
gh jj ci NUMBER [PATH] [--repo OWNER/NAME] [--host HOST] [--remote NAME] [--json]
```

PATH defaults to current directory. Extension process receives arguments after `gh jj`; `cmd/gh-jj` calls shared CLI with RequireJJ policy. Equivalent jj commands share operation/result schema; no extension-specific repository or API implementation exists. Only the extension requires jj; `stw hosting` uses native Git or jj according to ownership.

Repository resolution is local. Host defaults github.com and explicit alternative hostname is validated; remote data cannot choose an arbitrary authentication host. OWNER/NAME is explicit override, not ownership override. Remote selects exact candidate; no origin/main convention. Multiple distinct identities require selection; duplicate references to one identity do not create ambiguity. Unsupported/malformed discovery that prevents completeness refuses.

PR/CI require NUMBER positive decimal. No automatic PR guesses, pushes, fetching, CI reruns, mutation command forwarding or authentication setup. The hosting schema is separate from all published inspection/inventory/workspace/tidy/version schemas.

## Operation interface

```go
ValidateOptions(Options) error
Inspect(context.Context, string, Options) Result
```

Concrete exported types and JSON fields are defined in [data-model](../data-model.md). CLI imports hosting/inspection result types only; no os/exec, filesystem inspection or network adapter in parsing/rendering. `cmd` owns cancellation and exit status. Structured output is one bounded JSON object/newline; successful results exit 0. Operational errors exit 1 with schema-valid error observation plus concise stderr. Usage errors exit 2 with no JSON. Writer/process failures may leave partial output; consumers must check both JSON validity and exit status.

Human output renders control characters safely. Raw repository/remote/API/subprocess strings are never shell snippets or untrusted error bodies. JSON preserves data using normal escaping, with no silent truncation of successful results.

## Effects and limitations

Repository resolution performs native bounded identity/configuration reads and disclosed temporary isolation setup. PR/CI add authenticated host-specific GitHub REST reads. No target metadata/files/ref changes, jj snapshots/operation changes, Git transport, hooks, credential helper, editor or pager effects are allowed. Current-user gh credential config and auth environment are declared inputs; no token extraction or new credential store.

Thirty-second whole-operation deadline; existing tighter native bounds retained. Maximum 1 MiB per subprocess and final result, 10 pages per collection, 1000 combined CI records. Cancellation or exceeded/inconsistent/missing evidence yields incomplete error. CI samples exact PR head, reads both collections, resamples head and refuses moved identity. Successful sampled evidence cannot prove a transaction, future CI outcome, required-check satisfaction, review approval or merge eligibility.

No release artifact or installation claim is made by implementing this source entry point. Users already using another `gh jj` extension must choose their installed implementation; no automatic replacement is performed.

## Selection rules after independent analysis

- Native ownership and complete native remote enumeration are required in every mode. Failure to enumerate is not bypassed by `--repo`.
- `--repo OWNER/NAME` without `--remote` may select a repository absent from local remotes. Its provenance is explicit caller selection; no local association is claimed.
- Combined `--repo` and `--remote` must agree with the selected remote's unambiguous host/owner/name. Missing, malformed or conflicting selected remote evidence refuses.
- Well-formed remotes for a different host are ignored for selected-host resolution. Unsupported/malformed URLs are excluded with a bounded generic limitation, never echoed with credentials. Automatic or remote-based resolution refuses malformed selected-host URLs rather than asserting complete selection. An explicit repository without a remote selector does not depend on URL interpretation, after complete native enumeration.
- Distinct fetch/push repository identities remain distinct candidates; duplicate identities do not create ambiguity. No credential-bearing URL is included in result data.
