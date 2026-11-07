#!/bin/bash
# Oracle check of the in-process patch ID against `git patch-id --stable`.
# The git binary is used ONLY here, as the reference. Usage:
#   patch_id_oracle.sh STW_BINARY ROOT [PER_REPO_COUNT]
# Samples non-merge commits (first PER_REPO_COUNT per repo, default 30, spread over the history with
# a stride) from the root and every submodule, computes both IDs and prints one line per commit
# ("same"/"DIFF") and a summary. Exit 1 on any difference.
set -u
STW=$1 ROOT=$2 COUNT=${3:-30}
same=0 diff=0 total=0
repos=("$ROOT")
for p in $(git config --file "$ROOT/.gitmodules" --get-regexp '^submodule\..*\.path$' | awk '{print $2}'); do
  [ -e "$ROOT/$p/.git" ] && repos+=("$ROOT/$p")
done
for r in "${repos[@]}"; do
  n=$(git -C "$r" rev-list --no-merges --count HEAD)
  [ "$n" -gt 0 ] || continue
  stride=$(( n / COUNT )); [ "$stride" -ge 1 ] || stride=1
  commits=$(git -C "$r" rev-list --no-merges HEAD | awk -v s="$stride" -v c="$COUNT" 'NR % s == 1 || s == 1 { if (++k <= c) print }')
  for c in $commits; do
    want=$(git -C "$r" -c core.fsmonitor=false diff-tree -p --no-renames --no-color -r --root "$c" | git patch-id --stable | awk '{print $1}')
    got=$("$STW" dev patch-id "$r" "$c" | awk '{print ($1=="none")?"":$1}')
    total=$((total+1))
    if [ "$want" = "$got" ]; then same=$((same+1)); echo "same $(basename "$r") $c ${want:-empty}"; else
      diff=$((diff+1))
      # classify: binary (git hashes abbreviated oids, not comparable), empty (no diff), text (hunk placement differs)
      patch=$(git -C "$r" diff-tree -p --no-renames --root --no-commit-id "$c")
      if [ -z "$patch" ]; then cls=empty; elif printf '%s\n' "$patch" | grep -q '^Binary files\|^GIT binary'; then cls=binary; else cls=text; fi
      echo "DIFF($cls) $(basename "$r") $c git=${want:-empty} stw=$got"
    fi
  done
done
echo "total=$total same=$same diff=$diff"
[ "$diff" -eq 0 ]
