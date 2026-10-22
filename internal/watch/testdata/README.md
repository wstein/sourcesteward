# Fixtures of the restack tests

`rebase_merge.json` was recorded from a real `git rebase` of a stacked pair (main, parent with commits p1 and p2,
child on parent with c1 and c2, then main moves) in a throwaway repository, replaying GitHub's rebase merge: the parent
is rebased onto main and main fast-forwarded to it. `merge_commit_sha` is the rewritten tip, `child_retargeted` is
`git rev-list main..child` after that (what `pulls/N/commits` lists once GitHub retargets the child to main), and
`child_on_parent` is `git rev-list <old parent head>..child`. Reproduce with `record_rebase_merge.sh <empty dir>`;
the SHAs differ per run only through dates, which the script pins, so the output is stable.
