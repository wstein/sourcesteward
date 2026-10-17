# Verification: Inventory limits and checkout description

Status: implemented on branch `codex/027-inventory-limits`; hosted CI, the binary acceptance run and the Git 2.46.0 lane are **not run**. The feature stays `planned` in docs/feature-status.json until that evidence exists. No tag, release, publication, image or documentation build was run.

## Environment

linux/arm64 container, Go 1.27.2, Git 2.56.0 (`/usr/local/bin/git`) and Git 2.47.3 (`/usr/bin/git`), jj not installed. CI evidence (ubuntu-24.04 amd64, Git minimum and current lanes) does not exist yet; every result below is local evidence.

## What ran, and the outcome

| Check | Outcome |
| --- | --- |
| New tests observed failing on the code before the change | Scale fixtures (400, 1,030, 5,000 files, long paths, stored root commits, 700 tags, real-shaped repository) failed with the diagnostics of research Measurement 1; the stream tests failed to compile until `inventory_stream.go` existed |
| `gofmt -l cmd internal` | clean |
| `go vet ./...` | clean |
| `go test ./...` (Git 2.56.0) | passes except the pre-existing `scripts/test_docs.py` packaging test, which needs the Hugo tool that is not installed (environmental, unchanged by this work) |
| `go test -race -count=3 -shuffle=on ./...` | passes |
| Same Go tests under `PATH=/usr/bin:$PATH` (Git 2.47.3) | passes |
| `GOARCH=amd64 go test ./internal/...` (emulated on the arm64 host) | passes |
| `TMPDIR` set to a symlink to the real temporary directory | passes |
| `umask 077` | passes |
| `scripts/smoke-repos.py --binary <scratch binary built outside bin/ and dist/>` | 10 Git cases passed with byte/mode/mtime-identical fixture state and no sentinel; the jj cases and the foreign-owner case are not exercisable here |
| M5 steps of `scripts/smoke-inventory.py` | the script needs jj for its fixtures and cannot run here; the large-repository and truncated-listing steps were run by hand against the scratch binary (ok / partial with 511 references and the TXT-2 note); the checkout and gitlink steps are covered by the Go tests |
| `python3 -B scripts/check-docs.py`, `scripts/check-architecture.py`, `git diff --check` | pass |
| `python3 -B -m unittest scripts.test_inventory_smoke` (manifest now has M5) | passes |
| Schema bytes | `schemas/v1/inventory.schema.json` byte-identical to main (SHA-256 pinned in `TestPublishedV1Bytes`); real outputs of the new behaviors and the data-model example validate (`TestRealInventoryOutputsOfLimitsAndCheckouts`, `TestSpec027ExampleValidates`) |

## Measured after the change (Git 2.56.0, same fixtures as research.md Measurement 1)

| Fixture | Before | After | Time after |
| --- | --- | --- | --- |
| r150, r400 | ok / `error` | ok, `tracked` false | 33 ms, 46 ms |
| r1030, long1030 | `error` | ok, `tracked` false | 85 ms, 123 ms |
| r5000 | `error` | ok | 229 ms |
| r50000 | `error` | ok | 2.5 s (two comparison passes) |
| r1030/r5000/r50000 `--scope stored` | `error` | `partial`, 1,021 records, "file listing truncated: 9 / 3979 / 48979 file records omitted" | 31-89 ms |
| tags700, tags1100 | `inventory-limit` / `git-inventory-failed` | `partial`, tags omitted first, "reference listing truncated: 190 / 590 references omitted" | about 50 ms |
| big (100,000-byte tracked file) | `partial`, `tracked` unknown | ok, `tracked` false | 36 ms |
| clean submodule checkout | `tracked` true, path = Git directory | `tracked` false, path = checkout | 51 ms |
| superproject, clean | partial | partial (unchanged, TXT-9) | 47 ms |
| superproject, modified ordinary file | partial, `tracked` unknown | partial, `tracked` true; `--query dirty` ok/true | tests |

## Review 6 fixes (independent review of 3d429d5) and mutation checks

Fixed with failing tests first: the 023 acceptance script asserted an `ok` discovered submodule (now `partial`/`inventory-partial`, dirty true, run against the scratch binary); the index path-byte limit counted whole `ls-files` records (now path bytes only); linked worktrees of bare repositories (the `bare` registration is kept per entry, CK-3, result `ok` instead of a `changed-repository` error); a content-budget test used an unresolved temporary path; retained candidates lowered to 20,000 records and 8 MiB of paths (measured peak resident set 31 MiB for 100,000 untracked files, 53 MiB for 100,000 modified tracked files, 92 MiB with `--scope stored --scope disk`; the earlier 100,000-record cap peaked at 126 to 163 MiB); TXT-1 counts the records dropped by the 1 MiB trim and the stored count is exact; trimming never drops branches or remote references (`inventory-limit` instead); streamed commands run in their own process group and the group is killed on cancel (a forking wrapper no longer leaves an orphan and the stop returns immediately); the deadline test no longer depends on the shim starting before the deadline (a deterministic cancel test requires the process); COMPAT-13 to COMPAT-16 and the 023 embedding-budget statement added.

Mutation spot-checks (hand-applied to the source, targeted tests): 33 mutants of the stream bounds, index and candidate limits, checkout candidate checks and the query guard, the gitlink rule, fit trimming, bare entries, process group, hash length, symlink and sparse rules, budget accounting and note handling. All killed except two equivalent ones (`files+refCost < budget` and removing `candidate == entry` in checkout selection, which change no result) and one that needed a stronger test and was killed afterwards (conflict de-duplication).

## Not run

- The end-to-end M5 cases of `scripts/smoke-inventory.py` (needs jj) and acceptance with a `bin/stw` built by the maintainer.
- Git 2.46.0 (the stated minimum): not available; `check-attr --stdin -z` and `ls-files --stage -t -z` were verified on 2.47.3 and 2.56.0 only.
- jj: not installed; the jj adapter is unchanged and its limits unverified.
- darwin, amd64, cold-cache I/O times, hosted CI.
- The `safe.directory` refusal for the CK path needs a second user.

## Deviations from the specification

- The behavior commit is larger than the work-item split in tasks.md because the stream, checkout and gitlink rules share one workspace loop.
- Retained candidates are capped at 20,000 records and 8 MiB of paths per pass; reference-name bytes are bounded only by the scan allowance and the deadline.
