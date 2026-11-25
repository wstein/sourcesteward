#!/usr/bin/env bash
# Usage: bash rezept-stw-absorb-apply.sh /absolute/path/to/stw
set -euo pipefail
stw=$(realpath "${1:?Path to stw required}")
temp=$(mktemp -d)
trap 'rm -rf -- "$temp"' EXIT
git init --quiet -b main "$temp/repo"
cd "$temp/repo"
git config user.name 'Acceptance Fixture'
git config user.email acceptance@example.invalid
git config commit.gpgsign false
awk 'BEGIN {for(i=1;i<=20;i++) print "line " i}' > a.txt
git add a.txt
git commit -qm base
git branch keep
sed '1s/.*/topic/' a.txt > "$temp/topic"
cp "$temp/topic" a.txt
git commit -qam topic
old=$(git rev-parse HEAD)
sed '1s/.*/corrected/' a.txt > "$temp/expected-head"
awk 'NR==12 {print "extra"} {print}' "$temp/expected-head" > "$temp/expected-index"
cp "$temp/expected-index" a.txt
git add a.txt
printf 'unstaged\n' >> a.txt
printf 'untracked\n' > local.txt
cp a.txt "$temp/worktree"
cp local.txt "$temp/untracked"
cp .git/index "$temp/index"
"$stw" absorb --json > "$temp/plan.json"
sum=$(python3 -c 'import json,sys; d=json.load(open(sys.argv[1])); assert d["schema_version"] == 1 and d["summary"]["absorbed"] == d["summary"]["skipped"] == 1, d; print(d["plan_sum"])' "$temp/plan.json")
"$stw" absorb --apply --plan-sum "$sum" --json | tee "$temp/apply.json"
new=$(git rev-parse HEAD)
test "$old" != "$new"
python3 - "$temp/apply.json" "$old" "$new" <<'PY'
import json
import subprocess
import sys
path, old, new = sys.argv[1:]
with open(path) as stream:
    result = json.load(stream)
assert result["old_head"] == old and result["new_head"] == new, result
assert subprocess.check_output(["git", "rev-parse", result["backup_ref"]], text=True).strip() == old
PY
test "$(git diff --name-only "$old" "$new")" = a.txt
git show HEAD:a.txt | cmp "$temp/expected-head" -
git show :a.txt | cmp "$temp/expected-index" -
cmp "$temp/index" .git/index
cmp "$temp/worktree" a.txt
cmp "$temp/untracked" local.txt
git diff --cached -- a.txt
git diff -- a.txt
echo 'PASS: selected hunk committed; skipped hunk staged; index and worktree byte-identical'
