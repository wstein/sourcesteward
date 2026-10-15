# WI3 Tasks: Owned Workspace Retirement

## Setup and foundation
- [ ] T301 Freeze concrete retirement v2 result/action/manifest/native interfaces after WI2 integration and obtain independent WI3 analyze in specs/011-workspace-reference-tidy/contracts/wi3.md and wi3-analysis.md.
- [ ] T302 Write failing exactcompletedownership/task-owner/nativeidentity/pathreplacement/oldrecordcompatibility tests in internal/workspace/tidy_retire_test.go.
- [ ] T303 Implement strict retirement model/journal binding creation evidence and separate authorization/expected identities in internal/workspace/tidy_retire.go; preserve old schemas and strict task records.

## US1 — Review retirement candidates
Independent test: useful native clean cases eligible; uncertain/foreign/dirty paths retained with precise evidence.
- [ ] T304 [P] [US1] Write failing Git native-metadata-extra-payload/pointer-swap/rawdisk/index/storedtree/ignoredextra/transforms/sparse/symlink/gitlink/active/integration tests in internal/workspace/retirement_git_test.go.
- [ ] T305 [P] [US1] Write failing jj native-metadata-extra-payload/symlink-metadata/complete disk/binary/literalpath/ignoredextra/sparse/conflict/single-integrated-parent versus emptyalone/multiparent tests in internal/inspection/jj_retirement_test.go.
- [ ] T306 [P] [US1] Implement bounded Git full cleanliness/exact admitted native metadata manifests/registration/taskownership evidence and distinct combined-effect eligibility in internal/workspace/retirement_git.go.
- [ ] T307 [P] [US1] Implement native jj full tree/rawbyte, fixed minimum/current metadata allowlists and confined fresh disk proof, one-parent integrated equivalent tree case and exact ownership in internal/inspection/jj_retirement.go and internal/workspace/retirement_jj.go.

## US2 — Apply reviewed retirement
Independent test: exact authorized native effects only, recovery durable first, siblings/source preserved even on failure.
- [ ] T308 [P] [US2] Write failing sandbox parentgrant regularfile/directory sibling protections, symlink/special sibling refusal/listlimit and Linux rootremoval cases in internal/workspace/native_sandbox_test.go.
- [ ] T309 [US2] Implement canonical regularfile+directory readonly protections for exact narrow parentgrants in internal/workspace/native_sandbox.go; revalidate parentidentities/siblinglists, never broadfallback.
- [ ] T310 [P] [US2] Write failing jj confined manifest deletion/newfile/type/inode/pathswap/cancel/partial cases in internal/workspace/retirement_manifest_test.go.
- [ ] T311 [US2] Implement os.Root nofollow verified manifest file removal and bottom-up empty directories/root identity checks in internal/workspace/retirement_manifest.go; no RemoveAll/following symlinks.
- [ ] T312 [P] [US2] Implement native Git exactowned lock/removal with both permissions and recoverypin before combined action in internal/workspace/retirement_git.go; registrationonly retains, no prune/force dirty removal.
- [ ] T313 [P] [US2] Implement native jj exactworkspace forget preserving disk and separate files action/recoverybookmark in internal/inspection/jj_retirement.go and internal/workspace/retirement_jj.go.
- [ ] T314 [US2] Integrate per-action ownership/freshness revalidation, durable recovery/outcomes, persistencefailure/cancellation and conservative retries in internal/workspace/tidy_retire.go and tidy_retire_test.go.

## US3 — Bounded human/agent contract
Independent test: separate effects and partial outcomes agree human/JSON; old and new schemas validate.
- [ ] T315 [P] [US3] Write failing retirement flagmix/exactdestination/authorization/v2/schema/oldv1 regression fixtures in internal/cli/tidy_test.go and internal/contracts/tidy_test.go.
- [ ] T316 [US3] Expose distinct registration/files selection and safelyquoted shared rendering in internal/cli/tidy.go; publish independent schemas/v2/tidy.schema.json without altering v1.
- [ ] T317 [P] [US3] Extend mandatory native exactcase manifest with real clean-retirement/sibling/source preservation, races/partial/cancel and missing-capability failures in scripts/smoke-retirement.py and scripts/test_retirement_smoke.py; both supported lanes mandatory.

## Review and handoff
- [ ] T318 [P] Document native effect differences/freshness/capability limits/manual recovery and versioned schema publication in docs/tidy.md, docs/backends.md and docs/json-contracts.md.
- [ ] T319 Obtain independent source/sandbox review and final artifact analysis, run gofmt/make check/race/all mandatory native lanes and record exact evidence in specs/011-workspace-reference-tidy/wi3-verification.md.
- [ ] T320 Update all tasks/progress/featurestatus based on actual three-WI evidence, atomic Conventional Commits and open/attach WI3 PR in docs/progress.md and specs/011-workspace-reference-tidy/wi3-tasks.md.

## Dependencies and parallel work
T301 sourcegate then T302→T303 corefoundation. Git T304→T306 and jj T305→T307 parallelworktrees.
Sandbox T308→T309 and confinedfiles T310→T311 are separatefiles; T312 needsGitproof+sandbox,
T313 needsjjproof+files. T314 integration. CLI T315→T316 and conformance T317 prepareparallel;
T318 docsparallel; all precede T319→T320. 20 tasks: US1 four, US2 seven, US3 three,
foundation three, handoff three. Every declared capability must actually pass both native lanes.
