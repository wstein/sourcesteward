#!/bin/bash
# Regenerates tests/vectors/*.id with the git binary (oracle only): builds the same contents as
# tests/it/patchid.rs::matches_git_patch_id_stable_on_known_vectors in a scratch repository.
set -eu
OUT=$(cd "$(dirname "$0")/../vectors" && pwd)
T=$(mktemp -d "${TMPDIR:-/tmp}/stw-vec.XXXXXX"); trap 'rm -rf "$T"' EXIT
cd "$T"; git init -q .; git config user.name T; git config user.email t@example.com
mkdir dir; printf 'alpha\nbeta\ngamma\ndelta\nepsilon\nzeta\neta\ntheta\n' > dir/f.txt; printf 'g\n' > g.txt
git add -A; git commit -q -m base
printf 'alpha\nbeta\ngamma\nDELTA\nepsilon\nzeta\neta\ntheta\niota\n' > dir/f.txt; git rm -q g.txt; printf 'n\n' > "new file.txt"
git add -A; git commit -q -m change
git diff-tree -p --no-renames -r --root HEAD | git patch-id --stable | awk '{print $1}' > "$OUT/modify_delete_add.id"
printf 'alpha\nbeta' > dir/f.txt; git add -A; git commit -q -m nonl
git diff-tree -p --no-renames -r --root HEAD | git patch-id --stable | awk '{print $1}' > "$OUT/no_newline.id"
