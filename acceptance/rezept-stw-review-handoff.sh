#!/usr/bin/env bash
# Usage: bash rezept-stw-review-handoff.sh /absolute/path/to/stw
set -euo pipefail
stw=$(realpath "${1:?Path to stw required}")
scratch=$(mktemp -d)
trap 'rm -rf -- "$scratch"' EXIT
repo="$scratch/repo"
git init -qb main "$repo"
cd "$repo"
git config user.name 'Handoff Fixture'
git config user.email handoff@example.invalid
git config commit.gpgsign false
git config maintenance.auto false
printf 'base\n' > selected
printf 'other base\n' > other
git add selected other
git commit -qm base
printf 'staged\n' > selected
printf 'other staged\n' > other
git add selected other
printf 'worktree\n' > selected
"$stw" workspace commit-plan --source index selected --json >"$scratch/plan.json"
"$stw" workspace commit-apply "$scratch/plan.json" --message 'selected' --json
test "$(git show HEAD:selected)" = staged
test "$(git diff --cached --name-only)" = other
test "$(cat selected)" = worktree
"$stw" workspace commit-plan --source worktree selected --json >"$scratch/stale.json"
before=$(git rev-parse HEAD)
cp .git/index "$scratch/index-before"
printf 'newer\n' > selected
code=0
"$stw" workspace commit-apply "$scratch/stale.json" --message 'stale' --json >"$scratch/refusal.json" || code=$?
test "$code" = 1
jq -e '.schema_version == 1 and .outcome == "refused"' "$scratch/refusal.json" >/dev/null
test "$(git rev-parse HEAD)" = "$before"
cmp .git/index "$scratch/index-before"
test "$(cat selected)" = newer
printf '%s\n' 'PASS: selective commit and stale refusal preserve unrelated staging'

repo="$scratch/conflict"
git init -qb main "$repo"
cd "$repo"
git config user.name 'Handoff Fixture'
git config user.email handoff@example.invalid
git config commit.gpgsign false
git config maintenance.auto false
printf 'base\n' > shared
git add shared
git commit -qm base
git branch lower
printf 'main\n' > shared
git commit -qam main
git switch -q lower
printf 'lower\n' > independent
git add independent
git commit -qm lower
git switch -qc conflict
printf 'incoming\n' > shared
git commit -qam conflict
git switch -qc blocked
printf 'blocked\n' > descendant
git add descendant
git commit -qm blocked
git switch -q main
mkdir "$scratch/store"
"$stw" restack conflict-report --onto main --store "$scratch/store" lower conflict blocked >"$scratch/report.json"
id=$(jq -er .id "$scratch/report.json")
"$stw" restack show-conflict --store "$scratch/store" "$id" >"$scratch/reopened.json"
jq -e '.stale == [] and (.plan.levels | map(.state)) == ["clean","conflict","inherited-conflict"]' "$scratch/reopened.json" >/dev/null
for pair in 'base base' 'current main' 'incoming incoming'; do
    read -r side expected <<<"$pair"
    blob=$(jq -er --arg side "$side" '.conflict_versions[] | select(.path == "shared") | .[$side].file' "$scratch/reopened.json")
    test "$(cat "$scratch/store/$blob")" = "$expected"
done
old=$(git rev-parse HEAD)
git update-ref refs/heads/main refs/heads/lower "$old"
new=$(git rev-parse HEAD)
code=0
"$stw" restack show-conflict --store "$scratch/store" "$id" >"$scratch/stale-conflict.json" || code=$?
test "$code" = 1
jq -e '.stale | length > 0' "$scratch/stale-conflict.json" >/dev/null
test "$(git rev-parse HEAD)" = "$new"
printf '%s\n' 'PASS: conflict report reopens and preserves a moved ref'
