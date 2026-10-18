# WI5 attestation process input proposal

This is a bounded implementation interpretation awaiting independent assessment;
it does not amend the specification or claim real GitHub CLI compatibility.

## Owning requirements

[Verify procedure step 7](contracts/verify.md#procedure-normative-order-every-step-is-a-gate-later-steps-still-run-after-a-failure-so-all-failures-are-reported-except-that-steps-that-depend-on-a-missing-record-or-tag-are-not-run-with-the-reason-depends-on-code)
requires fixed repository, signer workflow, source-ref and self-hosted-runner pins,
an external fresh empty working directory, verified subject-digest agreement and
manifest binding. The [effects contract](contracts/verify.md#effects) requires
regular file reads without following symlinks, no writes, a filtered process
environment, 60 seconds and one MiB. [T026](tasks.md#wi5---online-reads-and-attestation-verification-read-only-network)
keeps the process output shape unverified until the authorized rehearsal.

## Proposed process input

Open each file with the existing no-follow adapter and require the opened
descriptor itself to be regular before any read. Stream its
SHA-256 with the existing two-GiB bound, reject size/time changes, compare it with
the record, then rewind. Inherit this exact descriptor as fd3 using Go's
`exec.Cmd.ExtraFiles`; the fixed subject argument is `/dev/fd/3`. Pass the native ownership root to each later launch and independently require
a canonical existing absolute boundary before checking executable/configuration
containment. Run gh in the existing external empty directory. Rehash the pinned descriptor after completion
and compare the digest again before accepting the structured verification result.
The ordinary subject path is never reopened by gh. OCI subjects retain the fixed
`oci://repository@sha256:digest` form derived from the validated record/contract.
No content snapshot is written and no fallback to a repository path is allowed.
Descriptor or `/dev/fd` incompatibility fails closed as unavailable verification.
A pinned descriptor preserves inode identity, not immutable content: digest and
size/time checks sample the bytes before and after the child read. A writer may
change the file after the final sample; this is no transaction or future-state
guarantee. The child must still independently verify the recorded digest.

This preserves the stated no-write effects. A disk snapshot was considered but
rejected because it would add an unmentioned content write of up to two GiB.

## Executed and remaining evidence

Linux fake-gh tests execute the inherited descriptor path, read its bytes in the
child, assert the working directory is empty and outside the target, and assert
fixed argv/environment. Tests reject symlinks, digest mismatch, cancellation and
content mutation during verification; a rename-and-link fixture proves the opened
descriptor retains the original inode. All executable fixtures are synthetic.

Real gh seeking/hashing behavior through `/dev/fd/3`, real attestation JSON shape,
trust-root network/cache behavior and real GitHub attestations remain UNVERIFIED
until an explicitly authorized rehearsal. Darwin is compiler-only in this source
candidate; no Darwin process compatibility is claimed. The implementation refuses
missing/unknown JSON shapes and has no less strict platform fallback. Independent
assessment must decide whether this process input interpretation is acceptable
before this candidate is represented as satisfying T026.
