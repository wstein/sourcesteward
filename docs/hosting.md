# GitHub inspection and the gh jj extension

SourceSteward provides shared read-only hosting operations through `stw hosting`
and the optional `gh jj` entry point. `stw hosting` supports native Git and jj;
`gh jj` requires a jj-owned workspace, including colocated repositories. Both use
the same Go operations and [hosting JSON contract](../schemas/v1/hosting.schema.json).
Jujutsu is a supported runtime CLI dependency, not a linked Rust library.

```sh
stw hosting repo PATH --json
stw hosting pr 42 PATH --repo owner/project --json
stw hosting ci 42 PATH --repo owner/project --json

gh jj repo JJ_PATH --json
gh jj pr 42 JJ_PATH --repo owner/project --json
gh jj ci 42 JJ_PATH --repo owner/project --json
```

Paths default to the current directory. `repo` reads local identity and active
native fetch/push remotes without gh or network. It reports a local association,
not confirmation that the hosted repository exists. Distinct candidates refuse
selection unless `--remote NAME` or `--repo OWNER/NAME` selects explicitly. Combined
selectors must agree. There is no assumption about origin, main or master.
Well-formed remotes for other hosts are ignored. jj's native interface lists
active remotes and omits empty sections and non-UTF-8 remote names; this is not a
raw configuration inventory. Unsupported visible records refuse native discovery.

The host defaults to github.com; GitHub Enterprise requires explicit `--host HOST`.
Remote text never selects an authentication host. Explicit `--repo` may target a
repository absent from local remotes after ownership/native discovery succeeds.
Fork head/base repositories remain separate identities. PR numbers are explicit;
there is no automatic branch/bookmark-to-PR inference in this feature.

PR/CI reads require GitHub CLI 2.102.0 or newer and the caller's existing
host-specific authentication. SourceSteward invokes fixed `gh api` GET requests
outside the inspected repository. No editor, pager, git credential helper, fetch,
push, API mutation or CI rerun is requested. Authentication configuration linked
inside the inspected target is refused. Tokens and raw subprocess/API error bodies
are not included in diagnostics. Native jj retains enforced read-only sandboxing,
operation pinning and stored-unsnapshotted working-copy qualifications.

CI samples checks and commit statuses at the PR's exact head SHA, then rechecks
head/base identities. Missing permissions, malformed or incomplete pages, moved
heads and exhausted bounds produce incomplete error results. No aggregate passing
CI, required-check satisfaction, review approval or merge eligibility is inferred.
Evidence is sampled rather than transactional. API text reflects gh's response processing and is not a byte-for-byte raw HTTP capture. Limits are 30 seconds total,
1 MiB per subprocess output/final JSON result, 10 pages per collection and 1000
combined CI records. Native inspection retains its tighter bounds.

All data commands offer human and `--json` output. Success exits 0, operational
failure exits 1 with a structured error observation, and usage errors exit 2.
Pre-dispatch cancellation may return only the process diagnostic, and writer/process failures may leave partial output. Consumers must check both exit status and JSON validity. Network-attempt and
completeness fields distinguish early local failures from remote observations.

## Optional local extension installation

After reviewing/building this source:

```sh
make extension
gh extension install ./bin/gh-jj
gh jj --help
```

The directory and executable are both named gh-jj, as required by gh's local
extension interface. `stw` need not be separately installed for this entry point.
The repository is still named sourcesteward; `gh extension install
wstein/sourcesteward` is not the documented installation route. An existing
`gh jj` extension occupies the same command name; choose your preferred
implementation explicitly. SourceSteward never replaces an installed extension.
Source tests install only in disposable private configuration directories.

Release distribution, ARM64 release artifacts and automatic installation remain
separate work. Hosting mutations and stacked PR publication remain [Spec 016](../specs/016-stacked-pr-coordination/spec.md).
