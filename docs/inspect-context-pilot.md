# Diff-first context pilot

Work package: [Anchorage #28](https://github.com/wstein/whr-anchorage/issues/28).
Source: [Cursor Codebase and STW context](../../../chartroom/sources/stw-agent-workflow/Cursor%20Codebase%20and%20STW%20context.md).

## First comparison

On November 25, 2026, GitHub's pull files endpoint for [Anchorage PR #24](https://github.com/wstein/whr-anchorage/pull/24) and a local `git diff --name-status --find-renames BASE...HEAD` agreed on nine paths. Both used published base `12c2703bfbb5a28be5088875517cf470b94b530f` and head `592ee06a2969fd4e815baed4ab0c0620004d5797`. Git resolved merge base `137997dfae174ef2606bceb8a3466a6b1c450c15`. Local objects were fetched first without checking out either revision. The subsequent local diff used no forge requests. This PR has no renames. The nine paths cover CLI and hosting code, the readiness implementation and test, documentation, and a CLI snapshot.

Starting with those paths leads to the Anchorage `AGENTS.md` and two immediate test entry points, `cargo test --locked --test ready_check` and `cargo test --locked --test snapshots_cli`, from `projects/sourcesteward`. This was a manual comparison. Automated rule discovery, time to the first useful test, and any reduction in search calls remain unmeasured.

## Next validation

- Report requested and resolved revisions, comparison basis, changed paths with both sides of each rename, data source, and any truncation or missing evidence. Exercise a local-only revision and a rename with fixtures.
- Resolve applicable repository rules for each changed path and cite their paths. Include a fixture that crosses project boundaries. Test suggestions should cite their source.
- Compare a normal inspection with the compact report. Count extra searches until the agent identifies a useful test. Check whether either path misses a rule or input that affects that test.
