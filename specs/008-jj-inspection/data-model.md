# Native jj Result Model

Existing Git/version schema1 result fields and serialization remain unchanged. Selected jj results use schema_version2; pre-selection, GitButler and ambiguous outcomes remain schema1.

`jj result`: schema_version=2; status ok/error; backend=jj; input_path string; evidence array; repository=null; optional jj_repository native identity; diagnostics array. Failure omits jj_repository and has at least one diagnostic; success contains complete jj_repository identity and zero diagnostics. Unknown object properties/diagnostic codes remain additive.

`jj identity`: workspace_root, metadata_dir, repository_dir and workspace_name are nonempty valid UTF-8 strings; paths are absolute. `change_id`, `commit_id`, `operation_id` are distinct full native identifier strings validated against supported jj output. No abbreviations, synthetic Git HEAD, bookmark-as-branch or interchangeable identifier fields. Supported output: change_id is 32 lowercase reverse-hex letters k-z, commit_id 40 lowercase hexadecimal characters, operation_id 128 lowercase hexadecimal characters.

`jj identity.observation`: working_copy='stored-unsnapshotted'; limitation explains that disk edits/untracked files are not included and no freshness/transaction guarantee exists. This is a success qualification, not a diagnostic implying failure. Native stale-workspace/refusal failures use jj-failed; no update is attempted.

Evidence retains existing kind/location semantics and deterministic ordering. Operational diagnostic additions: jj-unavailable, jj-version-unsupported, jj-sandbox-unavailable, jj-failed, jj-timeout, jj-output-limit, changed-repository, invalid-metadata, metadata-unreadable, canceled. Diagnostics contain fixed actionable descriptions without raw stderr/config/content. Missing tools after recognized jj ownership remain backend jj/schema2 failures.

There are no inspected-target state transitions or writes. Private outside-target temporary isolation/probe files may be created and removed as disclosed in the CLI contract. All results are observations. Invalid UTF-8 cannot be lossily serialized as identity; terminate with a structured error where representable.
