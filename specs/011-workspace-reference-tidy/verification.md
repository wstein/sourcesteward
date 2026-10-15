# Spec 011 Acceptance and Handoff

Three separately analyzed, implemented and reviewed WIs use atomic Conventional Commits and
isolated worktrees. [WI1 evidence](wi1-verification.md) covers plans and PR36 (owner merged);
[WI2 evidence](wi2-verification.md) covers local references and PR37; [WI3 evidence](wi3-verification.md)
covers owned retirement and PR38 (stacked on PR37). Global implementation is bounded by native
capabilities; exact-head hosted acceptance is recorded per WI, not assumed from local checks.

The CI matrix requires minimum/current Git and jj, mandatory sandbox capability and complete
case manifests: 42 tidy-plan, 24 reference-apply and 36 retirement CLI cases per lane, plus the
existing inspection/inventory/workspace gates. Missing native capabilities or cases fail, never skip.
Existing schema bytes remain unchanged; retirement adds tidy v2. Live schema/Pages publication
is a separate exact-main-revision workflow; no CLI release or real repository tidy is performed here.
