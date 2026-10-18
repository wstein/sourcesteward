# Complete native stream and retirement-content contract

This fixes the proof collectors, not a generic process-output cap. Normative limits are FR-017–FR-019 and FR-029–FR-032 in [spec.md](../spec.md). Every stream uses the existing canonical native binary, fixed argv, minimal environment and network-denying sandbox. No writable roots for read streams; no inventory helper overrides.

## Typed stream channels

| Channel | Fixed operation | Unit/total limits and completeness |
| --- | --- | --- |
| Base/target tree | `ls-tree -r -l -z --full-tree FULL_OID` | NUL records ≤4,224 bytes; stdout ≤4,325,376; ≤1,024 leaves; stderr ≤64 KiB; final delimiter, successful exit and EOF required. |
| Complete filter evidence | `check-attr --source=FULL_OID --stdin -z filter` | Input only complete validated tree paths, NUL-framed, ≤1,024 ×4,097 bytes; each response logical path/filter/value triple ≤4,224; complete stdout ≤4,325,376; stderr ≤64 KiB. Exact path sequence and field counts required. |
| Raw user blobs, v3 only | `cat-file --batch` | Input full hexadecimal OID plus LF per admitted file, ≤1,024 ×65 bytes; header+trailer ≤128 bytes/object; payload ≤16 MiB/file and64 MiB/sample; framing ≤128 KiB; stderr ≤64 KiB. |
| All other native outputs | Existing fixed commands | Existing 64 KiB cap remains, including index/reference/registration/configuration collectors. Their independent failure remains visible. |

Every command includes `--no-replace-objects` and the existing configuration/ownership/attribute admission. Inputs are generated from validated facts, not user rev expressions; paths are data. The filter stream validates every path, only admits existing safe sentinel results and fingerprints complete evidence. Unsupported active filters refuse without executing or suppressing helpers. Retain complete validated tree tuples within 1,024 entries and ≤4 MiB path bytes; hash raw tree/filter evidence incrementally in existing deterministic order so refactoring does not weaken stale fingerprints.

One bounded streaming runner shares launch validation with the buffered runner. Stdout is consumed by a typed parser, stderr concurrently capped, stdin fed without pipe deadlock under the same context. Each raw-content read buffer ≤64 KiB; logical records use their smaller cap. An overrun allows at most one detection byte, then cancels the child, closes pipes and waits; no unbounded drain. Total counters count bytes actually consumed, including failed reads/detection bytes; accepted maxima exclude the one detection byte. Cancellation and startup errors are never interpreted as native success. Prove no surviving children and no undeclared writes/helpers on early stop.

`cat-file --batch` responses must match the next requested OID, literal blob type and admitted size. Hash exactly the declared binary bytes, then require LF; after the final requested response close input and require EOF/success. Reject missing objects, extra headers/content, zero-progress/stalled reads and wrong ordering. Duplicate OIDs for different admitted paths may be requested repeatedly and count repeatedly toward the sample budget; deduplication cannot omit per-path manifest checks. At most one raw-object process per full observation, never one per file.

## Physical content and policy separation

Use a validated content-policy enum: legacy Git/jj v2 (64 KiB user files), Git v3 (16 MiB user files/64 MiB user bytes per pass). It is selected from the exact command/plan version and backend, not arbitrary stored numeric limits. Metadata remains64 KiB/file in both. Unknown policies/versions refuse. Stored facts, disk admission, decoded manifest validation and deletion-adjacent checks all receive the same explicit policy; old callers never inherit v3 by a global constant change. Shared jj unlink stays v2.

Git v3 retirement alone also admits the optional exact registration-root file `COMMIT_EDITMSG`, produced by a normal in-place native commit. It remains inert metadata, not completion or ownership authority: require the same owned/no-follow/single-link regular-file proof, 64 KiB cap, complete digest and exact identity/time checks at both existing registration-manifest passes. Its bytes count as metadata and its entry consumes the existing aggregate/encoded bounds; no new pass, process, buffer, budget, deadline, native effect or wire field is introduced. Nested/lookalike names and every other unknown file still refuse. Old Git/jj v2 metadata vocabulary and unlink behavior remain unchanged. Recovery does not promise to preserve the commit-message file after authorized native removal.

V3 metadata manifests add link count, owner identity, mtime and ctime to existing device/inode/type/mode/size/digest facts using native Linux/macOS timestamp precision. Require same-user regular/no-follow/single-link files and compare lstat/open-fstat/post-read-fstat/final-lstat identity/time fields. Read exactly the admitted size and test EOF; hash every byte. Preserve complete stored/index/path/mode equality and reject untracked/ignored extras, including empty directories where existing manifests reject them. Timestamp changes or same-size content mismatches refuse; timestamps never replace hashes. Same-user ABA and timestamp granularity remain limitations.

## All-pass accounting

A full Git retirement observation makes one stored-user pass, two disk-user passes, two physical-pointer metadata passes and two registration-metadata passes. Never reuse a prior hash as fresh disk evidence.

| Boundary | Full observations | User-content passes | Raw-object processes | Maximum instrumented content-proof consumption |
| --- | ---: | ---: | ---: | --- |
| Plan: initial, repeat, final under store lock | 3 | 9 | 3 | 3 × (320 MiB +320 KiB) |
| Apply: two coordinator preflights; recovery adapter before/after; coordinator after recovery and before removal; removal adapter before/after grant construction | 8 | 24 | 8 | 8 × (320 MiB +320 KiB) |

Per observation: user bytes3×64 MiB=192 MiB; registration metadata2×(1,024×64 KiB)=128 MiB before the tighter aggregate-entry check; physical Git metadata is only the `.git` pointer2×64 KiB; object framing128 KiB and stderr64 KiB. Sum320 MiB+320 KiB. User bytes alone total576 MiB per plan and1,536 MiB per apply. Admission rejects unsupported extra metadata before reading it as user content. Account bytes on failures and all repeated samples using a single operation ledger; allow only the bounded overrun detection byte before failure.

The content ledger excludes unrelated native metadata channels governed separately above and HTTP governed by the online contract; it never claims to measure Git removal's internal filesystem reads. Git removal has no additional SourceSteward per-file unlink pass. If later implementation adds a proof pass, it cannot reset the ledger or omit another guard; change the reviewed design or refuse when the approved budget is exhausted.

No deadline reset: offline plan10 seconds, online plan30 seconds; each full native observation has a subordinate10-second context; apply60 seconds total. Recovery/removal suboperations remain within their existing tighter contexts and remaining parent time. Time/cancellation/budget failure after recovery preserves it and reports partial effects. Maximum-size valid input is byte-admissible but can still fail the deadline.

## Mandatory acceptance

Use six native fixtures spanning315–358 tracked files, each including a301,117-byte SVG; include enough normal directories/metadata to exercise the actual aggregate manifest count. Prove complete tracked/staged/untracked/ignored state and successful authorized retirement, recording actual byte/process/sample/time counters. A one-file stand-in is not acceptance. Add exact/over record, per-file and aggregate boundaries, binary NUL/newline payloads, EOF/size/order errors, blocked pipes and cancellation. Inject late same-size writes/restored mtime, chmod, hardlink/symlink and directory/metadata replacement at each observation/removal boundary. Preserve actors' data on refusal and compare all siblings/source/recovery. Run minimum/current Git and unchanged jj/v2 regression; missing capability or lane remains a blocker.
