# WI3 Retirement Contract and Pending Interface Gate

Public selection: `stw tidy plan [PATH] --base REF --state-dir ABS --task ID --owner OWNER
--remove-registration [--remove-files ABS] [--json]`; --ref and retirement flags mutually exclusive.
Exact files path must equal completed owned task destination. Registration-only jj may complete
with files retained; Git present-tree registration-only retains unsupported. Both authorizations are
required for native Git combined remove. Plan output lists each distinct effect, proof and blocker.

Publish a separate tidy retirement schema v2; preserve tidy v1 bytes/fixtures. Backend identities,
base/operation/safety, task/native creation ownership, canonical destination inode, full manifest,
separate registration/files action selections, recovery root and action outcomes must be explicit.
Final concrete Go structures are frozen after WI2 integration, before independent WI3 analyze;
this document is a draft design gate, NOT source authorization. Do not reinterpret old task records
or weaken their strict decoders; new retirement journals refer to immutable creation ownership.

Before registration action: stale/owner/integration/disk proof, prepared durable journal, durable
native recovery pin and exact recovery record. Before files action: revalidate owned physical path
and complete disk manifest again; forget success alone never authorizes deletion. An interrupted
operation preserves remaining files and reports known metadata effects. Recovering deleted files
requires native stored tree recovery; no promise of untracked file restoration (these blocked apply).
No automatic rollback, broad prune/GC, abandon, remote intent changes or blind retry.

Git sandbox grants protect every existing sibling regular file/directory under writable parents;
symlink/special entries or list overflow refuse. jj deletion is descriptor-confined and only removes
verified entries, never recursive path-derived broad removal. Same-user external races/ABA cannot
be excluded, are sampled and disclosed, and uncertain effects remain partial/manual recovery.
