# WI1 Verification: Evidence-only Tidy Plans

## Scope and source
Native Git/jj reference planning only: explicit bases, literal selectors, ancestry proof,
protected workspace targets and explanatory retention. Stateless by default; optional private
immutable plan storage. No native mutation, deletion, snapshot, forge call or helper execution.
Existing published schemas are unchanged; tidy v1 is independent and reserves apply results.

Plan/tasks preceded independent design analysis at bb7a374. Test-first authors used separate
core, Git, jj, CLI/schema and conformance worktrees. Independent source/security review cleared
c6bc345; review fixes protect every registered workspace root, qualify canonical identities and
bound selectors before copying. Follow-up source 38cab48 adds native unreferenced jj
workspace confinement tests and uncertainty disclosure. Final review caught a false
stateless unknown-root diagnostic; d1d165f fixes it with a failing-first regression.

## Executed checks
- Integrated make check at 705c767: Go tests/vet/module verification, 48 Python tests,
  12 JavaScript tests, documentation/import checks and git diff --check passed.
- Integrated go test -race ./... at 705c767 passed with current native Git/jj and required sandbox.
- Final go test ./internal/workspace -run TestTidy -count=1 at 38cab48 passed.
- Native CLI smoke-tidy.py: all 42 cases passed in each minimum/current lane, with independent
  byte/mode-identical native state assertions including dirty/untracked/ignored jj disk edits.
- Minimum Git 2.46.0 / jj 0.45.1; current Git 2.56.0 / jj 0.46.0. Both native executables and
  enforced read-only sandbox were mandatory, not skipped. Native author tests passed both lanes.
- Real Git and standalone/colocated jj CLI results validate tidy v1; negative schema fixtures
  reject unsupported proof, missing identities and workspace-used deletion candidates.

## Limits and handoff
Local macOS evidence is not hosted Linux acceptance. Mandatory CI minimum/current lanes include
all prior conformance and tidy smoke; exact PR-head checks, owner review and merge remain gates.
Ancestry only: squash/forge proof unavailable; uncertainty retains. jj bookmarks with any remote
counterpart retain to avoid recording later push-deletion intent. Observations are sampled, not
transactions; external races and ABA changes may be missed. Unknown/noncanonical registration
roots prevent persisted plans; they never authorize workspace removal. Apply and owned retirement
remain WI2/WI3, with separate implementation and acceptance gates. No real user refs/files deleted.

WI1 handoff: [PR #36](https://github.com/wstein/sourcesteward/pull/36). Final delta
review cleared d1d165f independently; four path-safety regressions passed uncached.
Final integrated TestTidy suite also passed. PR checks and owner merge remain pending.
