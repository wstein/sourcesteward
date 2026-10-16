# Research: Shared GitHub inspection

## Shared operations rather than extension code reuse

**Decision**: Implement the small read-only scope in existing Go packages; `cmd/gh-jj` is a thin entry point alongside `cmd/stw`.
**Rationale**: Both interfaces must share ownership, diagnostics, schemas and effect boundaries. Importing an independent Rust extension would duplicate those contracts.
**Alternatives considered**: Fork gh-jj; wrap jj-gh; generic gh command passthrough. All create either duplicate orchestration or effects outside this bounded scope.

## Native identity and remote discovery

**Decision**: Extend existing inspection adapters with bounded remote observation; native jj ownership always uses jj, including colocation and secondary workspaces. Explicit hosting selection still requires native identity validation.
**Rationale**: Ordinary Git repository discovery does not establish ownership of colocated jj, and gh repository-context discovery cannot be relied upon for standalone jj. Use existing configuration isolation, operation pinning, snapshot suppression and read-only sandbox.
**Alternatives considered**: Point gh at jj's backing `GIT_DIR`; inspect raw metadata/config in hosting; assume origin. These obscure ownership, bypass existing protections or misidentify forks.

## Explicit GitHub transport

**Decision**: Use `gh api --hostname HOST --method GET` with fixed REST paths and explicit owner/name/number/SHA. User auth environment/config is the sole declared credential source; no manual token parsing/storage. Run gh in private neutral cwd, disable prompting/pagers/debug output and reject repository-scoped executable selection.
**Rationale**: gh API supports explicit hostname and method; placeholders depend on repository context, while fields can change default request method. Explicit addressing avoids both. Authentication is host-specific; default host is github.com, and other hosts require explicit user selection rather than trusting arbitrary remote-host data.
**Alternatives considered**: GitHub SDK, GraphQL, gh pr context discovery, helper-driven token extraction, arbitrary request forwarding. None is needed for this scope.
**Primary source**: [gh api manual](https://cli.github.com/manual/gh_api), inspected 2026-10-16.

## Exact PR and CI evidence

**Decision**: Require positive PR number. Read `/repos/OWNER/NAME/pulls/NUMBER`; CI uses `/repos/OWNER/NAME/commits/SHA/check-runs` and `/repos/OWNER/NAME/commits/SHA/statuses`, then resamples PR head. Keep individual states rather than invent merge eligibility or aggregate required-check policy.
**Rationale**: Branch/bookmark names alone do not prove PR correspondence. GitHub exposes checks and statuses separately; a missing/403 collection is not empty success. Fork PR association is retained explicitly, and inaccessible evidence refuses completion.
**Alternatives considered**: Match PR by branch name; query latest branch CI; treat no checks as passing. These would attach evidence to an ambiguous or changed revision.
**Primary sources**: [pull requests REST](https://docs.github.com/en/rest/pulls/pulls#get-a-pull-request), [check runs REST](https://docs.github.com/en/rest/checks/runs#list-check-runs-for-a-git-reference), [commit statuses REST](https://docs.github.com/en/rest/commits/statuses#list-commit-statuses-for-a-reference), inspected 2026-10-16.

## Resource and failure limits

**Decision**: Thirty-second total operation deadline, 1 MiB subprocess/final output, manual pages of at most 100 records, at most 10 pages per collection and 1000 combined CI records. A full last allowed page cannot prove exhaustion without valid total/completion evidence; refuse instead of silently truncating. Validate JSON shape/identity and detect duplicate IDs/inconsistent totals where the endpoint exposes totals.
**Rationale**: Unbounded API pagination and buffered subprocess output could hang or consume excessive resources. Data errors must not become permission to mutate or claims CI passed.
**Alternatives considered**: gh `--paginate` unbounded, unlimited buffered output, best-effort empty result after failures. Rejected for incomplete evidence and resource risk.

## Extension delivery

**Decision**: Add the executable source and local build/documentation path, not an extension release or live user installation workflow.
**Rationale**: SourceSteward repository name does not follow gh's default gh-* extension discovery convention; local directory installation needs a correctly named executable/directory and deserves explicit docs. `make extension` may build `bin/gh-jj/gh-jj`; `gh extension install ./bin/gh-jj` is documented, and isolated temporary GH_CONFIG_DIR fixture installation verifies actual gh dispatch. Stable AMD64/ARM64 assets and distribution are later authorized work, not implied by an entry point.
**Primary source**: [creating GitHub CLI extensions](https://docs.github.com/en/github-cli/github-cli/creating-github-cli-extensions), inspected 2026-10-16. This feature makes no claim that `gh extension install wstein/sourcesteward` works.
