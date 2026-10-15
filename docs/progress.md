# Progress and handoff

## Current milestone
006-json-schemas is implemented on codex/006-json-schemas, stacked on inspection005.
Version and inspection results have JSON Schema2020-12 contracts, real-output validation,
negative invariant fixtures and verbatim static publication. Source checks and race tests
pass on the final005 source revision. Independent review cleared006 source at98f65df;
[PR #6](https://github.com/wstein/sourcesteward/pull/6) is open; schema URLs await merge and Pages deployment. See [JSON acceptance](json-schema-verification.md).

005-repository-inspection is implemented and independently verified on codex/005-repository-inspection.
Identity inspection and conservative managed-backend refusal are bounded by the reviewed
contract. Source checks, race tests and 10 process cases pass. Independent review cleared source
revision 1d8d7c5; coordinator publication remains pending. See [inspection acceptance](inspection-verification.md).

004-site-themes is merged to main through [PR #5](https://github.com/wstein/sourcesteward/pull/5).
The README site link and system/light/dark Pages selector passed source, browser and
independent review checks. This records merged source; real OS dark preference/change
remains untested. See [theme evidence](../specs/004-site-themes/verification.md).

003-agent-hints is merged to main through [PR #4](https://github.com/wstein/sourcesteward/pull/4).
Root help links to task-specific agent guidance and llms.txt; version/JSON and error
contracts are preserved. Independent review cleared the implementation. See
[agent hints acceptance](agent-hints-verification.md).

002-wide-tie-brand is complete and published.
The wide tie asset set, navy/orange site, useful README badges and local make build target
pass source/browser checks and independent review. CI/Pages run 38751223604 passed;
live revision 63e5f9b matched the checked HTML/CSS, logo, favicons and social preview. See [branding evidence](../brand/verification.md).

001-project-foundation is complete. The public SourceSteward repository starts with an empty
commit and focused Conventional Commits for specifications, CLI, community, branding, site,
harness and CI. A durable local checkout is available. See [verification](verification.md).

## Decisions

- Repository/module: wstein/sourcesteward; product: SourceSteward; standalone short CLI: stw.
- Go 1.27.2 and Cobra v1.10.2; EUPL-1.2 original source/artwork; font licenses retained.
- Git-first and jj-ready, with optional GitButler adapters planned; bounded ordinary-Git identity reads execute; native managed adapters remain planned.
- Wide navy tie, orange knot and white branch; local family fonts with preserved OFL notices.
- Static Pages with local assets and same-run exact-SHA checks; no CLI release automation.
- Weekly Dependabot, community standards, private security reporting and owner review.
- Spec Kit workflow and small repository-local harness; no agent scheduler or auto-merge.

## Measured state

make check, race tests and six real-process smoke cases passed. Independent source review
cleared fd4d652ce402621bf07b4d83ee5a5da7b70f4fd3 after two regression fixes. Browser inspection
passed at 1280×800 and 375×812 with no horizontal overflow. Initial Linux CI and Pages run
38743805161 succeeded; live revision and static bytes matched the checked source. The final
documentation-only evidence commit will go through the same CI/Pages gate.

## Next bounded action
Review inspection and schema contracts at the final integrated revision; publish only after independent review. Hugo documentation generation preserves existing branding and versioned schema URLs in feature007.
Broader status and native backend adapters require separate specifications.
Cleanup, mutation/recovery, repository migration and MCP remain outside foundation scope.
