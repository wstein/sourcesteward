# Inventory data model

The normative field and enum contract is [inventory](contracts/inventory.md).
InventoryResult owns one immutable inspection identity sample, observation boundary, workspace/reference/base/file collections and optional predicate. All observations derive from a single selected native adapter; no mixed Git/jj identities. Complete arrays use deterministic native-name/path ordering for stable output.

Workspace records link to the shared repository identity and retain each native checkout commit/change/operation separately. Task ownership is always unknown: no path/name/reference heuristic proves ownership. Registration health differs from manager ownership and dirty state.

Reference records preserve added_targets/removed_targets native target objects (commit_id, nullable change_id) and tracked Observation; conflicted refs never arbitrarily select a target. Nested targets count toward the aggregate record bound. Reference records link upstream names and native revision identities. Reachability observations carry explicit base/revision/selected-remotes in query evidence or limitations. BaseCandidate provenance distinguishes explicit, configured and remote-default evidence. Conflicting candidates never imply an automatic base.

File records distinguish selected disk/index/stored/untracked/ignored scopes, literal paths and old_path on rename. A path can have multiple scope records. Empty successful collection differs from unsupported collection via the boundary qualifications.

Result lifecycle: initialized incomplete → native identity accepted → bounded observations sampled → identities rechecked → ok/partial/error. No target state transition is permitted. Cancellation/limit/malformed output transition only result status and retain qualified evidence; no partial collection may authorize later mutations.

Validation: schema_version is 1; command is inventory; status is ok|partial|error; arrays never null; required strings are present; native ID validity follows selected adapter; nil identities are explicit unknowns, never fake zero IDs. Record total ≤1024, serialized size ≤1048576 bytes, native command output ≤65536 bytes and total duration ≤10 seconds. Preserve literal UTF-8 names; fail invalid encoding rather than lossy conversion.
