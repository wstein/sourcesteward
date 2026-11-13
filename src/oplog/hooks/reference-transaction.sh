#!/bin/sh
# stw-oplog-refhook v1
# git hook reference-transaction (stw spike). git calls it up to three times per ref transaction: prepared, committed or aborted.
# Danger: in the phase `prepared` an exit code other than 0 aborts the ref transaction. This script therefore always exits 0,
# does nothing in `aborted`, and in `prepared` only the one thing described below.
# committed: appends one TAB-separated line per changed ref to <git-common-dir>/stw/oplog/refs.log:
#   UTC-time  old  new  refname  ref-transaction  main|worktrees-id
# old/new: 40 or 64 hex digits, or ref:<target> for a symbolic ref. refname and target are percent-encoded: every byte outside
# [A-Za-z0-9._/+@:-] becomes %XX (upper case hex, so % itself is %25); a name longer than 200 bytes is cut at 200 bytes and ends in
# %00 plus 8 hex digits (djb2 hash of the whole name, so cut names stay distinct).
# Only HEAD and refs/* are logged; refs/remotes/* only with `remotes=on` in <git-common-dir>/stw/oplog/config or
# STW_OPLOG_REMOTES=on. Not logged: pseudo refs (ORIG_HEAD, AUTO_MERGE, ...), old = new, HEAD lines that repeat a branch line,
# the pruning of loose refs that `git pack-refs` does (the same value is in packed-refs). The packing itself is logged as 0 -> value
# with a `pre-same` marker before it; the reader hides both.
# git reports old = 0 whenever the caller gave no expected old value (git tag -d, git branch -D, git branch -f, update-ref without
# an old value, ...), also for refs that exist. In `prepared` the hook therefore looks up the current value of every ref reported with
# old = 0 and writes a marker line (context `pre-old`, old = the current value) when the ref exists and differs from the new value, or
# `pre-same` when it equals the new value (packing by pack-refs/gc, update to the same value); the reader joins the marker to the
# committed line (pre-same: the line is no change and is hidden). Per-worktree refs (refs/bisect, refs/worktree, refs/rewritten) get no marker.
# A transaction without such a line (commit, ordinary updates with an old value) does no work in `prepared`.
# A write error is recorded as a "gap" line in stw/oplog/errors.log, or, if that fails too, in <git-common-dir>/stw-oplog-gaps.
# Only regular files are appended to (a FIFO or device as log counts as a write error). The hook changes no ref.
# Remove with: stw-oplog-refhook uninstall --yes
PATH="${PATH:-/usr/bin:/bin}:/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin"
LC_ALL=C; export LC_ALL
GIT_OPTIONAL_LOCKS=0; export GIT_OPTIONAL_LOCKS
umask 077
phase=${1:-}
case $phase in prepared|committed) ;; *) exit 0 ;; esac
Z40=0000000000000000000000000000000000000000
Z64=0000000000000000000000000000000000000000000000000000000000000000
app() { { [ ! -e "$2" ] || [ -f "$2" ]; } && printf '%s\n' "$1" 2>/dev/null >> "$2"; }
first=
if [ "$phase" = prepared ]; then
  while read -r old new ref _rest || [ -n "$old$new$ref" ]; do
    case $old in "$Z40"|"$Z64") ;; *) continue ;; esac
    case $ref in refs/*) first="$old $new $ref"; break ;; esac
  done
  [ -n "$first" ] || exit 0
fi
now=$(date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null) || now=unknown
common=$(git rev-parse --path-format=absolute --git-common-dir 2>/dev/null)
if [ -z "$common" ] || [ ! -d "$common" ]; then cat >/dev/null 2>&1; exit 0; fi
wt=-
if [ "$phase" = committed ]; then
  wt=main
  gitdir=
  [ -z "${GIT_DIR:-}" ] || gitdir=$(git rev-parse --path-format=absolute --git-dir 2>/dev/null)
  if [ -n "$gitdir" ] && [ "$gitdir" != "$common" ]; then
    wt=${gitdir#"$common"/worktrees/}
    case $wt in */*) wt=other ;; esac
    case $wt in *[!A-Za-z0-9._-]*) wt=$(printf '%s' "$wt" | tr -c 'A-Za-z0-9._-' '?') ;; esac
    [ ${#wt} -le 40 ] || wt=$(printf '%s' "$wt" | cut -c1-40)
  fi
fi
remotes=off
case ${STW_OPLOG_REMOTES:-} in 1|on|yes|true) remotes=on ;; esac
dir="$common/stw/oplog"
log="$dir/refs.log"
if [ -f "$dir/config" ]; then
  cfg=$(head -c 1024 "$dir/config" 2>/dev/null)
  while IFS='=' read -r k v || [ -n "$k" ]; do
    case $k in remotes) case $v in 1|on|yes|true) remotes=on ;; esac ;; esac
  done <<EOF
$cfg
EOF
fi
[ -d "$dir" ] || mkdir -p "$dir" 2>/dev/null
failed=0 bad=0
if [ "$phase" = prepared ]; then
  out=$({ printf '%s\n' "$first"; cat; } | STW_COMMON=$common STW_REMOTES=$remotes STW_NOW=$now awk '
BEGIN { for (i = 1; i < 256; i++) ord[sprintf("%c", i)] = i }
function enc(s,  i, c, o, cut, h) {
  cut = 0
  if (length(s) > 200) {
    h = 5381; for (i = 1; i <= length(s); i++) h = (h * 33 + ord[substr(s, i, 1)]) % 4294967296
    cut = sprintf("%%00%08x", h); s = substr(s, 1, 200)
  }
  for (i = 1; i <= length(s); i++) { c = substr(s, i, 1); o = o ((c ~ "[A-Za-z0-9._/+@:-]") ? c : sprintf("%%%02X", ord[c])) }
  return o (cut ? cut : "")
}
BEGIN { cm = ENVIRON["STW_COMMON"] "/"; pk = cm "packed-refs" }
{ if (NF != 3 || $1 !~ /^0+$/ || $3 !~ /^refs\// || $3 ~ /^refs\/(bisect|worktree|rewritten)\//) next
  nw = $2; ref = $3
  if (ref ~ /^refs\/remotes\// && ENVIRON["STW_REMOTES"] != "on") next
  cur = ""; f = cm ref
  if ((getline v < f) > 0) { if (v ~ /^[0-9a-f]+$/ && (length(v) == 40 || length(v) == 64)) cur = v; else if (v ~ /^ref:/) cur = "-" }
  close(f)
  if (cur == "") {
    if (!loaded) { loaded = 1; while ((getline l < pk) > 0) if (l ~ /^[0-9a-f]+ refs\//) { split(l, a, " "); pd[a[2]] = a[1] }; close(pk) }
    if (ref in pd) cur = pd[ref]
  }
  if (cur != "" && cur != "-") { z = cur; gsub(/./, "0", z); printf "%s\t%s\t%s\t%s\t%s\t-\n", ENVIRON["STW_NOW"], cur, z, enc(ref), (cur == nw ? "pre-same" : "pre-old") }
}' 2>/dev/null) || failed=$((failed+1))
else
  out=$(STW_PACKED="$common/packed-refs" STW_REMOTES=$remotes STW_NOW=$now STW_WT=$wt awk '
BEGIN { for (i = 1; i < 256; i++) ord[sprintf("%c", i)] = i }
function isoid(s) { return (s ~ /^[0-9a-f]+$/ && (length(s) == 40 || length(s) == 64)) }
function iszero(s) { return (s ~ /^0+$/) }
function val(s) { return (isoid(s) || s ~ /^ref:[^ ]+$/) }
function enc(s,  i, c, o, cut, h) {
  cut = 0
  if (length(s) > 200) {
    h = 5381; for (i = 1; i <= length(s); i++) h = (h * 33 + ord[substr(s, i, 1)]) % 4294967296
    cut = sprintf("%%00%08x", h); s = substr(s, 1, 200)
  }
  for (i = 1; i <= length(s); i++) { c = substr(s, i, 1); o = o ((c ~ "[A-Za-z0-9._/+@:-]") ? c : sprintf("%%%02X", ord[c])) }
  return o (cut ? cut : "")
}
function encv(s) { return (s ~ /^ref:/) ? "ref:" enc(substr(s, 5)) : s }
function loadpk(  pk, l, a) {
  if (loaded) return
  loaded = 1; pk = ENVIRON["STW_PACKED"]
  if (pk != "") { while ((getline l < pk) > 0) if (l ~ /^[0-9a-f]+ refs\//) { split(l, a, " "); pd[a[2]] = a[1] }; close(pk) }
}
{ if (NF != 3 || !val($1) || !val($2)) { bad++; next }
  n++; o[n] = $1; w[n] = $2; r[n] = $3
  if ($3 ~ /^refs\/heads\// && isoid($2)) hn[$2] = 1 }
END {
  for (i = 1; i <= n; i++) {
    if (r[i] != "HEAD" && r[i] !~ /^refs\//) continue
    if (r[i] ~ /^refs\/remotes\// && ENVIRON["STW_REMOTES"] != "on") continue
    if (o[i] == w[i] && !iszero(o[i])) continue
    if (r[i] == "HEAD" && iszero(o[i]) && isoid(w[i]) && (w[i] in hn)) continue
    if (isoid(o[i]) && !iszero(o[i]) && iszero(w[i])) { loadpk(); if (r[i] in pd) continue }
    printf "%s\t%s\t%s\t%s\tref-transaction\t%s\n", ENVIRON["STW_NOW"], encv(o[i]), encv(w[i]), enc(r[i]), ENVIRON["STW_WT"]
  }
  if (bad > 0) printf "#bad\t%d\n", bad
}' 2>/dev/null) || failed=$((failed+1))
fi
if [ -n "$out" ]; then
  while IFS= read -r line; do
    case $line in "#bad"*) n=${line#"#bad"}; n=${n#?}; case $n in ''|*[!0-9]*) n=1 ;; esac; bad=$((bad+n)); continue ;; esac
    app "$line" "$log" || failed=$((failed+1))
  done <<EOF
$out
EOF
fi
if [ "$failed" -gt 0 ] || [ "$bad" -gt 0 ]; then
  g=$(printf '%s\tgap\t%s\tfailed=%s\tbad=%s\t%s' "$now" "$phase" "$failed" "$bad" "$wt")
  app "$g" "$dir/errors.log" || app "$g" "$common/stw-oplog-gaps"
fi
exit 0
