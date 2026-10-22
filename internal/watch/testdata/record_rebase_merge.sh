#!/bin/sh
# Records the rebase-merge fixture shape into an empty directory and prints the interesting SHAs.
# The identity is passed through the environment of this script only; no git config is written.
set -eu
dir=$1
export GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@t GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@t
export GIT_AUTHOR_DATE=2026-10-22T09:00:00Z GIT_COMMITTER_DATE=2026-10-22T09:00:00Z
git init -q -b main "$dir"
cd "$dir"
echo base >a && git add a && git commit -qm base
git switch -qc parent && echo p1 >p && git add p && git commit -qm p1 && echo p2 >>p && git commit -qam p2
git switch -qc child && echo c1 >c && git add c && git commit -qm c1 && echo c2 >>c && git commit -qam c2
git switch -q main && echo m >m && git add m && GIT_COMMITTER_DATE=2026-10-22T09:05:00Z git commit -qm "main moves"
old=$(git rev-parse parent)
git switch -q parent && GIT_COMMITTER_DATE=2026-10-22T09:10:00Z git rebase -q main
git switch -q main && git merge -q --ff-only parent
echo "old_parent_head $old"
echo "merge_commit $(git rev-parse main)"
echo "rewritten $(git rev-list --reverse main~2..main | tr '\n' ' ')"
echo "child_retargeted $(git rev-list --reverse main..child | tr '\n' ' ')"
echo "child_on_parent $(git rev-list --reverse "$old"..child | tr '\n' ' ')"
