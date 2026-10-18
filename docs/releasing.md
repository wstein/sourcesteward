# Owner release preparation

**Preparation for planned, unreleased Spec 026.** Current source commands and
their incomplete evidence are described in the [release guide](release.md).
There is no completed self-hosted release procedure in this source slice.
Online evidence/dispatch, WI8 workflows, WI10 bootstrap and WI11 acceptance remain
separate dependencies; WI9 cannot finish before WI8. Consult the
[task list](../specs/026-release-process/tasks.md) before relying on the design.

## Authority and readiness evidence

The owner retains merges, tags, releases, builds, publication, deployment,
repository/environment/ruleset settings and consumer gitlinks. A plan, commit,
push, merge, bot or schedule grants no build or release authority. Each approval
covers the named operation and its disclosed effects. A tag push that triggers
publication needs explicit approval for both effects. Local verification and
reproducibility runs are builds requiring separate approval too; source tests,
formatting, vet and temporary compiler fixtures do not authorize artifact, image
or Hugo/documentation builds, installation or deployment.

Before proposing a release, prepare the exact product, full C, version/tag,
contract hash, repository/remote, successful CI push runs on C, review/conformance
evidence, notices/source-offer references and all outstanding gaps. An illustrative
fixture or non-empty readiness file is not acceptance evidence. Record installed
versions and the source/workflow revisions actually exercised.

## Settings the owner must establish

`stw` never changes these settings. The following click paths were checked in
official GitHub documentation on 2026-10-18; repository configuration itself has
not been measured by this guide.

- In repository **Settings**, open **Rulesets**, then **New ruleset → New tag
  ruleset**. Select **Active** enforcement, add each product pattern under
  **Target tags**, and enable creation/update/deletion restrictions and force-push
  blocking under **Tag protections** before **Create**. In **Bypass list → Add
  bypass**, verify effective membership meets owner-only release-tag control.
  GitHub documents roles, teams and apps as choices; do not assume an individual
  owner selector or that an admin role contains only the owner. If the available
  controls cannot express owner-only access, keep the real tag blocked.
  Patterns use `fnmatch`; `*` does not cross `/`. See
  [ruleset creation](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/creating-rulesets-for-a-repository)
  and [available protections](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/available-rules-for-rulesets).
- Under **Settings → General → Releases**, select **Enable release immutability**.
  It applies to future releases and locks associated tags/assets after publication;
  it does not protect the editable draft window. See
  [enabling immutability](https://docs.github.com/en/code-security/how-tos/secure-your-supply-chain/establish-provenance-and-integrity/prevent-release-changes)
  and [immutable release behavior](https://docs.github.com/en/code-security/concepts/supply-chain-security/immutable-releases).
- Inspect **Settings → Environments → release**, or explicitly create it with
  **New environment**. The selected design has no required-reviewer pause.
  A workflow can create an absent environment without protection rules. See
  [environment management](https://docs.github.com/en/actions/how-tos/deploy/configure-and-manage-deployments/manage-environments).

Before any first real product tag, including bootstrap, require the planned
ruleset advisory `present` and manually confirm active coverage and owner-only
bypass access. `partial`, `absent` and `unknown` block the tag until resolved;
`present` alone cannot prove membership. The tagged workflow runs on tag push,
so protection must exist beforehand. Current offline source reports `unknown`;
it cannot establish this readiness condition. Registry login and credentials
belong to reviewed workflow setup under the
[credential model](../specs/026-release-process/contracts/workflow.md#registry-credential-model-per-variant),
never the project contract.

## Rehearsal and bootstrap still required

The owner must explicitly approve a rehearsal build and its remote effects in a
disposable repository before WI10. The draft rehearsal creates no real product
tag or publication and does not require the real-tag ruleset. Record whether a
gated non-tip C builds once, whether draft listing/visibility and downloads by
asset ID work, and whether downloaded files verify against the expected signer,
source ref and attested manifest commit. Measure attestation output/permissions,
not just the presence of flags. See the
[rehearsal checklist](../specs/026-release-process/quickstart.md#rehearsal-checklist-for-the-unverified-github-behavior-before-the-first-real-release).

If those observations contradict the draft-retention design, prepare and review
the workflow-artifact fallback before relying on it; do not silently substitute
it. The planned fallback retention is one day. The owner cancels runs or removes
disposable drafts only within the approved cleanup scope. A draft rehearsal does
not exercise real publication, and organization actor checks remain fail-closed.

The separately approved bootstrap dry run uses a suffixed version and does not
publish. The first `stw` release uses the bootstrap tag-workflow pattern rather
than self-hosting: it builds/publishes once, then a record commit R follows the
tag on `main` before new code lands. Auditing that release needs explicit
bootstrap signer/source-ref overrides. Removing the bootstrap workflow and the
first self-hosted release procedure belong to WI11, not this documentation slice.
See the [bootstrap contract](../specs/026-release-process/contracts/bootstrap.md).

## Planned execution and failure handling

After implementation and acceptance, the normal design is: review a passing online
plan for C/version; separately approve and dispatch one build; download and review
its attested assets; merge only allowlisted notes/record as D; explicitly approve
the annotated tag and triggered publication; verify the published download; then
separately authorize any consumer activation. Detailed commands remain in the
[workflow contract](../specs/026-release-process/contracts/workflow.md) until that
procedure has actual evidence. No current offline result clears this sequence.

| Failure | Owner response |
| --- | --- |
| Failed/incomplete gate or exceeded bound | Stop the consequential step; resolve evidence and review a fresh plan. `not-run` is not success. |
| Uncertain dispatch result | Inspect Actions before considering another request; uncertainty does not authorize a duplicate build. |
| Build/draft failure before tagging | Preserve logs/hashes. Owner-authorized cancellation/deletion is manual; a new release attempt uses the next version and separate build approval. |
| Build inputs changed between C and D | Keep the draft unpublished and select a newer candidate; do not broaden the allowlist to hide changes. |
| Artifact/provenance disagreement | Keep publication blocked and investigate. A rewritten record cannot legitimize substituted bytes. |
| Image promotion succeeds but publication fails | A version tag may exist without a published release. The owner may authorize retrying the same publication; promotion is idempotent for the same digest. Otherwise abandon the version. |
| Wrong or previously used tag | Never move or re-create it; use the next version. Deleted tags without releases are invisible to monotonicity checks. |

Immutable publication and final asset re-listing limit different windows; neither
is task-context approval. `stw` does not undo publication or promoted registry
tags. Retain observed results and unrun cases instead of claiming global release
readiness from source checks or reviewed documentation.
