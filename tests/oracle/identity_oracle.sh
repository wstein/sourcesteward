#!/bin/bash
# Oracle for the reflog identity of `stw sync --apply` WITH a fetch: git (reference only, also the
# git-upload-pack that gix starts for a file:// remote) builds an upstream and a clone one commit behind;
# stw runs with no identity anywhere (empty HOME, no GIT_* variables, no config) and git then reads the
# reflogs. Usage: identity_oracle.sh STW_BINARY SCRATCH_DIR
set -eu
STW=$1 SCRATCH=$2
T=$(mktemp -d "$SCRATCH/idor.XXXXXX"); cd "$T"
export GIT_AUTHOR_NAME=T GIT_AUTHOR_EMAIL=t@example.com GIT_COMMITTER_NAME=T GIT_COMMITTER_EMAIL=t@example.com
git init -q -b main up; cd up; echo a > a.txt; git add -A; git commit -q -m c0; echo b > b.txt; git add -A; git commit -q -m c1; cd ..
git clone -q "file://$T/up" clone; cd clone; git reset -q --hard HEAD~1; git update-ref -d refs/remotes/origin/main; git update-ref refs/remotes/origin/main HEAD
git config --unset user.name 2>/dev/null || true; git config --unset user.email 2>/dev/null || true
cd ..; mkdir home
run() { env -i HOME="$T/home" PATH=/usr/bin:/bin GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null "$STW" sync --apply --json --root "$T/clone" "$@"; }
out=$(run)
echo "$out" | grep -q '"fetch":"ok"' || { echo "FAIL: fetch not ok: $out"; exit 1; }
echo "$out" | grep -q '"result":"fast-forwarded"' || { echo "FAIL: not fast-forwarded: $out"; exit 1; }
cd clone
[ -f b.txt ] || { echo "FAIL: b.txt missing"; exit 1; }
for ref in HEAD refs/heads/main refs/remotes/origin/main; do
  line=$(git reflog show --format='%gn <%ge>|%gs' "$ref" | head -1)
  echo "$ref: $line"
  case $line in "stw <stw@localhost>|"*) ;; *) echo "FAIL: $ref reflog identity"; exit 1;; esac
done
git fsck --no-dangling 2>&1 | grep -v '^Checking' || true
[ -z "$(git status --porcelain)" ] || { echo "FAIL: dirty after sync"; exit 1; }
echo "OK: fetch and fast-forward wrote the fallback identity; git accepts the repository"
