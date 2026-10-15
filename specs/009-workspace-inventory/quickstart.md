# Inventory validation guide

Planned commands below become executable only after implementation. Run against throwaway repositories, never user work.

1. Run `make check` and `go test -race ./...`; require no published schema regressions.
2. Create temporary Git repository, commit a tracked file, add linked worktree, make staged/disk/untracked edits. Run `go run ./cmd/stw inventory PATH --json`, then `--query dirty`; check independent workspace identities and distinct scopes. `--query ref-exists --ref refs/heads/absent` must return known false with exit0.
3. Add custom remote-tracking refs and conflicting remote HEAD candidates without network; exercise `--remote custom --base FULL_OID --revision FULL_OID`. Confirm no conventional main/origin assumption and ancestry differs from publication proof.
4. Query files with literal newlines, leading dashes, glob characters and rename/deletion; use `--path` and both anchor modes. Check exact JSON round trip and scope exclusions.
5. In standalone, colocated and secondary jj fixtures run same inventory/query cases with native IDs. Modify disk without snapshot, compare operation heads and metadata hashes before/after; requested disk freshness remains unknown. `--scope stored` exposes stored evidence; `--scope staged` explicitly unsupported.
6. Exercise missing registrations, foreign ownership, cancellation, malformed/truncated native output and changing identity with helper/network/write sentinels. No target changes or helper execution may occur; partial/error cannot claim complete.
7. CI must execute smoke-inventory and Go tests on both pinned minimum/current Git/jj lanes; missing mandatory executable/sandbox is failure, not skipped acceptance.

See [contract](contracts/inventory.md), [tasks](tasks.md) and future verification.md for actual execution evidence. No runtime acceptance is claimed by this guide.
