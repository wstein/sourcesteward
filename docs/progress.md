# Progress and handoff

## Current milestone
008-jj-inspection implements native standalone, colocated and secondary workspace identity
with enforced read-only Linux/macOS sandboxing, isolated configuration and schema v2.
Published Git/version v1 contracts remain unchanged. Independently reviewed native source
949c082 passes both macOS jj versions and mandatory minimum/current Linux conformance at
3b152f6 (Actions run38777280084). Final documentation/integration review and latest-head CI
remain pending. See [Spec 008 evidence](../specs/008-jj-inspection/verification.md).

007-hugo-docs are merged to main through
[PR #7](https://github.com/wstein/sourcesteward/pull/7),
[PR #6](https://github.com/wstein/sourcesteward/pull/6) and
[PR #8](https://github.com/wstein/sourcesteward/pull/8). Their recorded acceptance covers
bounded ordinary-Git identity inspection, versioned JSON contracts and pinned Hugo guides/
command reference. This is merged-source status; deployment evidence remains in each feature's
verification record. Native jj inspection remains pending Spec 008.

The Tier 1 policy amendment is prepared for owner review: Git and jj receive equal support
commitments for explicitly supported capabilities. See [amendment evidence](tier1-policy.md).

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
- Git and jj are Tier 1 peers by policy; bounded ordinary-Git identity reads execute. Native jj
  inspection is pending Spec 008; GitButler remains a future separately specified integration.
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
Finish exact-revision documentation/integration analysis and review for Spec 008, then confirm
latest-head CI before handoff. PR #9 carries the Tier 1 policy; PR #10 carries native inspection.
No merge, deployment or live schema publication is claimed here. Broader status, mutation,
recovery, repository migration and GitButler integration require separate specifications.
