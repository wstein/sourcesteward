# Verification: bootstrap release of `stw` (v0.1.0)

Status: implemented and run once. The owner pushed the annotated tag `v0.1.0`; the workflow `.github/workflows/release.yml` published the release. This file records what GitHub reports for that run (read with `gh` on 2026-10-25). It is not an acceptance of the Spec 026 machinery, which stays `planned`, and it authorizes no further tag, build or publication.

## The run

| Item | Observed |
| --- | --- |
| Tag | `v0.1.0`, an annotated tag object (`gh api repos/wstein/sourcesteward/git/ref/tags/v0.1.0` reports type `tag`), peeling to commit `2f246f53004e4d6268cc27f0da386c3ab80d5695` |
| Run | [39985568858](https://github.com/wstein/sourcesteward/actions/runs/39985568858), workflow `release`, event `push`, head `2f246f53004e4d6268cc27f0da386c3ab80d5695`, conclusion `success` |
| Jobs | `verify`, `build` and `smoke` for `linux_amd64`, `linux_arm64`, `darwin_amd64` and `darwin_arm64`, and `publish`: all `success` |
| Release | `v0.1.0`, published 2026-10-23T16:33:15Z, assets `SHA256SUMS` and `stw_0.1.0_{linux,darwin}_{amd64,arm64}.tar.gz` |
| Earlier dry runs | `workflow_dispatch` runs of the same workflow on `main` (the three latest succeeded on 2026-10-22 and 2026-10-23) |

## What is not covered

- The `gh attestation verify` check that the README documents was not re-run for this record.
- No record file `docs/releases/v0.1.0.json` exists; the Spec 026 record and `stw release verify --published` path (Spec 026 T047) is separate and still open.
- A second use of the bootstrap path (v0.2.0) is the owner's decision; this file does not authorize it.
