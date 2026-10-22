Recorded GitHub `search/issues` responses for the Spec 036 owner mode.

`search_user.json` (user:octocat) and `search_org.json` (user:github) were recorded once on 2026-10-22 from the public API,
unauthenticated, query `q=user:OWNER+is:pr+is:open+archived:false&sort=created&order=asc&per_page=3`, and trimmed to 3 items (public data only).
The other three are minimal edits of them: `search_empty.json` (total_count 0, items empty), `search_incomplete.json`
(incomplete_results true), `search_truncated.json` (total_count 1500, above the 1000 result cap).

The issue owner search (Spec 038) has its own recordings, `issue_search_user.json` (user:octocat) and `issue_search_org.json`
(user:github), recorded once on 2026-10-22 from the public API with curl, unauthenticated, query
`q=user:OWNER+is:issue+is:open+archived:false&sort=created&order=asc&per_page=3`, three items each (public data only).
`issue_search_empty.json` (total_count 0, items empty), `issue_search_incomplete.json` (incomplete_results true, total_count 0)
and `issue_search_truncated.json` (the organisation response with total_count 1500) are derived from them with jq.
