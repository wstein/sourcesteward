# Data Model

`Result` contains schema_version=1, status (`ok`, `unsupported`, `error`), backend (`git`, `jj`, `gitbutler`, `ambiguous`, `unknown`), input_path (absolute resolved path, or empty string if unavailable/invalid), evidence (ordered array), repository (nullable), diagnostics (ordered array).

`Evidence` contains kind (`jj-metadata`, `gitbutler-metadata`, `gitbutler-reference`) and location. Exact reference evidence includes its full reference name; marker location is an absolute path. Evidence sorting is deterministic.

`Repository` contains git_dir, common_dir, worktree (nullable), bare (bool), linked_worktree (bool), object_format (`sha1` or `sha256`) and head.

`Head` contains state (`attached`, `detached`, `unborn`), ref (nullable full `refs/heads/...` name), oid (nullable full lowercase object ID). No abbreviation or inferred revision/change identity.

`Diagnostic` contains code and message; message excludes subprocess stderr and configuration/file content. Initial codes: invalid-path, not-repository, git-unavailable, git-failed, unsupported-backend, ambiguous-backend, metadata-unreadable, invalid-metadata, changed-repository, canceled. Writer failures use existing runtime diagnostic outside JSON.

Only status=ok contains repository. Unsupported/ambiguous may retain backend evidence, never partial ordinary-Git success identity. All slices serialize as arrays, nullable values as null; unknown fields may be added within schema1, incompatible changes require a new version. JSON strings must preserve valid UTF-8 paths exactly; invalid UTF-8 identity is rejected.
