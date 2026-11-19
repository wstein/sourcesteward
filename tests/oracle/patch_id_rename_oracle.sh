#!/bin/bash
# Oracle check of the in-process patch ID for commits with renames, against `git patch-id --stable` on
# `git diff-tree -p -M --root <commit>` (git's default for `git diff` / `git show`: renames on, copies off,
# 50 % threshold). The git binary is used ONLY here, as the reference. Usage:
#   patch_id_rename_oracle.sh fixtures STW_BINARY BASE_DIR
#       builds throwaway repositories under BASE_DIR/fixtures (BASE_DIR must be an existing, absolute, non-empty
#       directory outside any Git tree) and prints one line per case ("same"/"DIFF") and a summary
#   patch_id_rename_oracle.sh repo STW_BINARY REPO [MAX]
#       read-only: compares the commits of REPO that contain a rename (at most MAX, default 500)
# Exit 1 on any difference. Identity comes from the global git config, times from GIT_*_DATE.
set -u
MODE=${1:?usage: patch_id_rename_oracle.sh fixtures|repo STW ...}
STW=${2:?stw binary}
want_id() { # repo commit -> reference patch-id
  git -C "$1" diff-tree -p -M --no-color --no-ext-diff -r --root "$2" | git patch-id --stable | awk '{print $1}'
}
got_id() { "$STW" dev patch-id "$1" "$2" | awk '{print ($1=="none")?"":$1}'; }
same=0 diff=0 total=0
compare() { # label repo commit
  local w g
  w=$(want_id "$2" "$3"); g=$(got_id "$2" "$3")
  total=$((total+1))
  if [ "$w" = "$g" ]; then same=$((same+1)); echo "same $1 $3 ${w:-empty}"
  else diff=$((diff+1)); echo "DIFF $1 $3 git=${w:-empty} stw=${g:-empty}"; fi
}
if [ "$MODE" = repo ]; then
  R=${3:?repo path}; MAX=${4:-500}
  [ -n "$R" ] && [ -d "$R" ] || { echo "no repository: '$R'" >&2; exit 2; }
  commits=$(git -C "$R" log --no-merges -M --diff-filter=R --format=%H | head -n "$MAX")
  for c in $commits; do compare "$(basename "$R")" "$R" "$c"; done
  echo "total=$total same=$same diff=$diff"
  [ "$diff" -eq 0 ]; exit
fi
[ "$MODE" = fixtures ] || { echo "unknown mode $MODE" >&2; exit 2; }
BASE=${3:-}
case "$BASE" in /tmp/stw-prove-patchid*) ;; *) echo "BASE_DIR must be under /tmp/stw-prove-patchid: '$BASE'" >&2; exit 2;; esac
rm -rf "$BASE/fixtures"; mkdir -p "$BASE/fixtures"; cd "$BASE/fixtures" || exit 2
T=1700000000
body() { seq 1 "$1" | sed "s/^/$2 line /"; } # N lines "TAG line K"
newrepo() { # name
  mkdir "$1" && cd "$1" && git init -q -b main . && cd ..
}
commit() { # repo msg
  T=$((T+60)); (cd "$1" && git add -A && GIT_AUTHOR_DATE="$T +0000" GIT_COMMITTER_DATE="$T +0000" git commit -q --allow-empty -m "$2")
}
head_of() { git -C "$1" rev-parse HEAD; }
# 1 pure rename
newrepo pure; body 30 a >pure/old.txt; commit pure base; git -C pure mv old.txt new.txt; commit pure rename
compare pure pure "$(head_of pure)"
# 2 rename with a small change (similarity above 50 %)
newrepo changed; body 30 a >changed/old.txt; commit changed base; git -C changed mv old.txt new.txt; sed -i 's/^a line 7$/CHANGED/' changed/new.txt; commit changed rename
compare rename-change changed "$(head_of changed)"
# 3 rename with a mode change
newrepo mode; body 30 a >mode/old.txt; commit mode base; git -C mode mv old.txt new.txt; chmod +x mode/new.txt; commit mode rename
compare rename-mode mode "$(head_of mode)"
# 3b rename with a mode change and a content change
newrepo modechg; body 30 a >modechg/old.txt; commit modechg base; git -C modechg mv old.txt new.txt; chmod +x modechg/new.txt; sed -i 's/^a line 7$/CHANGED/' modechg/new.txt; commit modechg rename
compare rename-mode-change modechg "$(head_of modechg)"
# 4 copy (git without -C reports an addition)
newrepo copy; body 30 a >copy/old.txt; commit copy base; cp copy/old.txt copy/dup.txt; commit copy copy
compare copy copy "$(head_of copy)"
# 4b copy with a change plus the original changed (copy detection would need -C)
newrepo copychg; body 30 a >copychg/old.txt; commit copychg base; cp copychg/old.txt copychg/dup.txt; sed -i 's/^a line 7$/CHANGED/' copychg/dup.txt; echo more >>copychg/old.txt; commit copychg copy
compare copy-change copychg "$(head_of copychg)"
# 5 directory rename (several files, some changed)
newrepo dir; mkdir dir/d; body 30 a >dir/d/a.txt; body 30 b >dir/d/b.txt; body 30 c >dir/d/c.txt; commit dir base
git -C dir mv d e; sed -i 's/^b line 3$/B3/' dir/e/b.txt; commit dir rename
compare dir-rename dir "$(head_of dir)"
# 6 rename with a change beyond the similarity limit (below 50 %: deletion plus addition)
newrepo below; body 20 a >below/old.txt; commit below base; git -C below mv old.txt new.txt; body 20 z >below/new.txt; sed -i '1,5s/^z /a /' below/new.txt; commit below rename
compare rename-below below "$(head_of below)"
# 6b similarity close to 50 %
newrepo edge; body 20 a >edge/old.txt; commit edge base; git -C edge mv old.txt new.txt; { body 10 a; body 10 z; } >edge/new.txt; commit edge rename
compare rename-edge edge "$(head_of edge)"
# 7 binary rename, pure and with a change
newrepo bin; head -c 3000 /dev/urandom >bin/old.bin; commit bin base; git -C bin mv old.bin new.bin; commit bin rename
compare binary-rename bin "$(head_of bin)"
newrepo binchg; head -c 3000 /dev/urandom >binchg/old.bin; commit binchg base; git -C binchg mv old.bin new.bin; printf 'X' >>binchg/new.bin; commit binchg rename
compare binary-rename-change binchg "$(head_of binchg)"
newrepo binmod; head -c 3000 /dev/urandom >binmod/x.bin; commit binmod base; head -c 3000 /dev/urandom >binmod/x.bin; commit binmod change
compare binary-modify binmod "$(head_of binmod)"
# 8 empty file rename
newrepo empty; : >empty/old.txt; body 5 k >empty/other.txt; commit empty base; git -C empty mv old.txt new.txt; commit empty rename
compare empty-rename empty "$(head_of empty)"
# 9 rename over several commits (each commit compared)
newrepo multi; body 30 a >multi/one.txt; commit multi base; git -C multi mv one.txt two.txt; commit multi r1; c1=$(head_of multi)
git -C multi mv two.txt three.txt; sed -i 's/^a line 9$/N9/' multi/three.txt; commit multi r2; c2=$(head_of multi)
compare multi-1 multi "$c1"; compare multi-2 multi "$c2"
# 10 names with white space and a byte >= 0x80 (quoted in the header)
newrepo names; body 30 a >"names/a b.txt"; commit names base; git -C names mv "a b.txt" "c d é.txt"; commit names rename
compare names names "$(head_of names)"
# 11 two identical sources, one renamed: git prefers the same base name
newrepo twins; mkdir twins/x twins/y; body 30 a >twins/x/f.txt; body 30 a >twins/y/g.txt; commit twins base
git -C twins rm -q x/f.txt y/g.txt; mkdir twins/z; body 30 a >twins/z/f.txt; commit twins move
compare twins twins "$(head_of twins)"
# 12 symlink rename
newrepo link; ln -s target link/old; commit link base; git -C link mv old new; commit link rename
compare symlink-rename link "$(head_of link)"
# 13 swap of two files' contents (no rename by git when paths stay)
newrepo swap; body 30 a >swap/p.txt; body 30 b >swap/q.txt; commit swap base; mv swap/p.txt swap/t; mv swap/q.txt swap/p.txt; mv swap/t swap/q.txt; commit swap swap
compare swap swap "$(head_of swap)"
# 14 rename plus a new file with similar content
newrepo dupnew; body 30 a >dupnew/old.txt; commit dupnew base; git -C dupnew mv old.txt new.txt; body 30 a >dupnew/extra.txt; sed -i 's/^a line 2$/X2/' dupnew/extra.txt; commit dupnew rename
compare rename-and-new dupnew "$(head_of dupnew)"
# 15 CRLF content change in a rename
newrepo crlf; body 30 a | sed 's/$/\r/' >crlf/old.txt; commit crlf base; git -C crlf mv old.txt new.txt; sed -i 's/^a line 4\r$/a line four\r/' crlf/new.txt; commit crlf rename
compare rename-crlf crlf "$(head_of crlf)"
echo "total=$total same=$same diff=$diff"
[ "$diff" -eq 0 ]
