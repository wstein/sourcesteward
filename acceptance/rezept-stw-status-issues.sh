#!/usr/bin/env bash
# Usage: bash rezept-stw-status-issues.sh /absolute/path/to/stw
# Requires a GitHub token that can read the three fixture repositories.
set -euo pipefail
stw=$(realpath "${1:?Path to stw required}")
temp=$(mktemp -d)
trap 'rm -rf -- "$temp"' EXIT
export GIT_AUTHOR_NAME='Acceptance Fixture' GIT_COMMITTER_NAME='Acceptance Fixture'
export GIT_AUTHOR_EMAIL=acceptance@example.invalid GIT_COMMITTER_EMAIL=acceptance@example.invalid
make_repo() {
    git init -qb main "$1"
    git -C "$1" -c commit.gpgsign=false commit --allow-empty -qm fixture
    git -C "$1" remote add origin "https://github.com/$2.git"
}
add_submodule() {
    git -C "$temp/root" -c protocol.file.allow=always submodule add -q "$temp/$1" "$1"
    git -C "$temp/root" config -f .gitmodules "submodule.$1.url" "https://github.com/$2.git"
    git -C "$temp/root/$1" remote set-url origin "https://github.com/$2.git"
}
make_repo "$temp/root" wstein/whr-anchorage
make_repo "$temp/docs" wstein/chartscribe
make_repo "$temp/sheets" wstein/sheetmill
add_submodule docs wstein/chartscribe
add_submodule sheets wstein/sheetmill
git -C "$temp/root" add .gitmodules
git -C "$temp/root" -c commit.gpgsign=false commit -qm 'register gitlinks'
cd "$temp/root"
"$stw" status --json > "$temp/full.json"
"$stw" status > "$temp/full.txt"
missing="wstein/stw-acceptance-missing-${temp##*.}"
git config -f .gitmodules submodule.sheets.url "https://github.com/$missing.git"
git -C sheets remote set-url origin "https://github.com/$missing.git"
"$stw" status --json > "$temp/partial.json"
"$stw" status > "$temp/partial.txt"
python3 - "$temp" "$missing" <<'PY'
import json
from pathlib import Path
import re
import sys

root, missing = Path(sys.argv[1]), sys.argv[2]

def read(mode):
    doc = json.loads((root / f"{mode}.json").read_text())
    assert doc["schema_version"] == 2, doc["schema_version"]
    by_slug = {repo["slug"]: repo["open_issues"] for repo in doc["repos"]}
    text = (root / f"{mode}.txt").read_text()
    expected = sorted(issue["number"] for listed in by_slug.values() for issue in listed["issues"])
    displayed = sorted(map(int, re.findall(r"(?m)^\s+#(\d+) .*labels:", text)))
    assert expected == displayed, (mode, expected, displayed)
    return by_slug, text

full, _ = read("full")
partial, text = read("partial")
healthy = {"wstein/whr-anchorage", "wstein/chartscribe"}
assert set(full) == healthy | {"wstein/sheetmill"}, full.keys()
assert set(partial) == healthy | {missing}, partial.keys()
assert all(listed["coverage"] == "complete" for listed in full.values())
for slug in sorted(healthy):
    assert partial[slug]["coverage"] == "complete", partial[slug]
    assert partial[slug]["issues"] == full[slug]["issues"], slug
assert partial[missing]["coverage"] == "incomplete", partial[missing]
assert partial[missing]["error"] and "open issues: incomplete" in text, text
print("PASS: terminal and JSON agree for all Gitlinks; one unavailable repository leaves the others complete")
PY
