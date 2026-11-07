Fixtures for `tests/github_reads.rs`.

- `issue_comments.json`, `pr_reviews.json`, `check_runs.json`: recorded on 2026-11-07 from the public repository
  `cli/cli` (issue 14963 comments, PR 14972 reviews, check runs of commit 3988a274..., unauthenticated GET),
  reduced to the fields the reader uses. They contain real CRLF, zero-width characters and bot bodies.
- `commit_statuses.json`, `forbidden_checks.json`: written by hand after the documented GitHub schema; no public
  repository with commit statuses was found, and a 403 for the Checks permission cannot be recorded without a token.
  The status descriptions contain hostile characters on purpose.
