# Contract: `stw release dispatch`

## Command

```text
stw release dispatch (--commit C --version V | --tag T) [PATH] [--repo OWNER/NAME] [--host HOST] [--apply PLAN_ID] [--json]
```

Starts the CI build workflow (the contract's `ci.release_workflow`) once, as `workflow_dispatch` on `main`. Without `--apply` it is a **plan**: it evaluates the build gates online (as [`plan --online`](release-plan.md)), prints the exact request and effects and a `plan_id`, and sends nothing. With `--apply PLAN_ID` it recomputes the plan from fresh state, requires the same `plan_id`, and sends exactly one request. `--commit/--version` is input mode 1 (commit C of `main`); `--tag` is input mode 2 (an existing annotated tag; the build gates of `plan --for build`).

## Disclosed effect

`--apply`: a network **write**. One `POST /repos/{owner}/{repo}/actions/workflows/{workflow file}/dispatches` with `{"ref":"main","inputs":{"mode":"commit"|"tag","commit":"<C>","tag":"<T>","version":"<V>"}}` through the hosting transport (the pinned `go-gh/v2` client, the caller's `gh` authentication, a token with permission to run workflows). It starts a CI run that builds the declared targets **once**, attests them and keeps a **draft** release; it tags, publishes and promotes nothing. GitHub answers 204 without a run id (unverified; a newer response that returns run details would be reported). The command reports the Actions workflow URL, not a run guess.

A plan is not authorization. The caller must hold, in its own task context, the user's explicit approval for this build of this commit and version (umbrella AGENTS.md; constitution II). The tool cannot verify that approval; the output prints the sentence "a build was not authorized by this plan".

## plan_id

`plan_id` is the SHA-256 (lowercase hex) of the canonical JSON (the record canonical form) of: `repository`, `host`, `workflow` (file name), `ref: "main"`, `inputs`, the resolved commit, `main_ref.commit` as read from the remote at plan time (`GET /repos/{r}/git/ref/heads/main`), the contract's SHA-256 at C, and the gate verdict. No plan file is stored. At `--apply` the same values are recomputed from fresh reads; any difference (main moved, contract changed, a gate changed) refuses with `plan-stale` and prints the new plan.

## Exit status

| Status | Meaning |
| --- | --- |
| 0 | plan printed with verdict `pass`, or the request was accepted (204) |
| 1 | refusal or operational failure (`plan-stale`, `gate-failed` on `--apply`, `auth-unavailable`, `online-unavailable`, `remote-unresolved`, `unsupported-backend`, `ambiguous-backend`, `canceled`); an HTTP failure of the POST is `online-unavailable` with the status code and no body |
| 2 | usage error (for example `--apply` without a plan id of 64 hex characters) |
| 3 | the plan's verdict is `fail` (not applied) |

Precedence 2, 1, 3, 0. The POST is never retried.

## Backend capability

Git and jj supported (the local part is `plan --online`'s commit and contract reads, plus hosting resolution, which is native for both); GitButler-managed and ambiguous refuse. Only github.com is verified; GitHub Enterprise needs `--host` (hosting rules, unverified for dispatch).

## Output

Human: the gates, the request body (inputs escaped), `effects: network write on --apply: starts a CI build (draft release, attestations); no tag, no publication`, `plan_id: <64 hex>`, and on apply `dispatched: <workflow URL>`. JSON: `ReleaseDispatchResult` (`schemas/v1/release-dispatch.schema.json`): the plan result fields plus `request` (`method`, `endpoint`, `body`), `plan_id`, `applied`, `workflow_url`, `error`.

## Effects

Reads: as `plan --online`, plus the remote `main` ref. Writes: remote only, one request, only with `--apply`. Nothing local changes; no local lock. Processes: `gh auth token --hostname HOST` (existing hosting behavior), Git/jj reads as `plan`. Recovery: a dispatched run can be cancelled by the owner in the Actions UI; a draft release it created can be deleted by hand (a tag was never created). The remote effect is not undoable by `stw`.

## Compatibility

New command. Adds the first write to the hosting transport: a single fixed endpoint and method, tested to fail closed on any other (WI5). Existing hosting results are unchanged.
