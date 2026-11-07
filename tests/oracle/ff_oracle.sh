#!/bin/bash
# Oracle check of `stw sync --apply`: git (reference only) builds a throwaway repository, stw fast-forwards
# it, and git then verifies index, working tree, objects and reflog. Usage: ff_oracle.sh STW_BINARY SCRATCH_DIR
set -eu
STW=$1 SCRATCH=$2
T=$(mktemp -d "$SCRATCH/ffo.XXXXXX"); cd "$T"
git init -q -b main repo; cd repo; git config user.name T; git config user.email t@example.com
seq 1 10 > a.txt; echo old > old.txt; echo keep > keep.txt; echo staged > staged.txt; mkdir sub; echo g > sub/gone.txt
mkdir -p tool; printf '#!/bin/sh\necho hi\n' > tool/run.sh; chmod +x tool/run.sh; ln -s a.txt link
git add -A; git commit -q -m c0
git switch -q -c up
sed -i.bak 's/^5$/FIVE/' a.txt; rm a.txt.bak; git rm -q old.txt; chmod -x tool/run.sh; git rm -q link; ln -s keep.txt link2; git add -A; git commit -q -m c1
mkdir -p dir; echo new > dir/new.txt; git rm -q sub/gone.txt; git add -A; git commit -q -m c2
UP=$(git rev-parse up); git switch -q main; git branch -q -D up
git update-ref refs/remotes/origin/main "$UP"; git symbolic-ref refs/remotes/origin/HEAD refs/remotes/origin/main
git remote add origin https://github.com/test/fixture; git config branch.main.remote origin; git config branch.main.merge refs/heads/main
echo "local edit" >> keep.txt; echo "staged edit" >> staged.txt; git add staged.txt
BEFORE=$(git rev-parse HEAD)
"$STW" sync --offline --apply --root "$T/repo"
echo "--- git verifies"
[ "$(git rev-parse HEAD)" = "$UP" ] || { echo "FAIL: HEAD not at upstream"; exit 1; }
git fsck --no-dangling 2>&1 | grep -v '^Checking' || true
echo "status (expect only the two local edits):"; git status --porcelain
[ "$(git status --porcelain)" = "$(printf ' M keep.txt\nM  staged.txt')" ] || { echo "FAIL: unexpected status"; exit 1; }
git diff HEAD --stat -- a.txt dir tool link link2 old.txt sub | grep . && { echo "FAIL: files differ from HEAD"; exit 1; }
[ -x tool/run.sh ] && { echo "FAIL: exec bit not removed"; exit 1; }
[ -L link2 ] || { echo "FAIL: symlink missing"; exit 1; }
git reflog -1 refs/heads/main
git reflog -1 HEAD
git log --oneline -1 --format=%H main | grep -q "$UP"
echo "OK: git accepts the result"
