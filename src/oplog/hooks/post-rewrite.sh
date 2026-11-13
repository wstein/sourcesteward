#!/bin/sh
# stw-oplog-hook v1
# git hook post-rewrite (stw spike). Appends one TAB-separated line per rewritten commit to
#   <git-common-dir>/stw/oplog/rewrites.log:   UTC-time  amend|rebase|other  old-sha  new-sha  extra  branch-or-detached
# Only SHAs and fixed words are written (extra and branch are reduced to [A-Za-z0-9._/+@-], everything else becomes ?).
# Never fails a git operation: always exit 0. A write failure is recorded as a "gap" line in stw/oplog/errors.log, or, if that
# fails too, in <git-common-dir>/stw-oplog-gaps. Remove with: stw-oplog-hook uninstall --yes
PATH="${PATH:-/usr/bin:/bin}:/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin"
LC_ALL=C; export LC_ALL
kind=${1:-}
case $kind in amend|rebase) ;; *) kind=other ;; esac
now=$(date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null) || now=unknown
common=$(git rev-parse --path-format=absolute --git-common-dir 2>/dev/null)
gitdir=$(git rev-parse --path-format=absolute --git-dir 2>/dev/null)
failed=0 bad=0
if [ -z "$common" ] || [ ! -d "$common" ]; then cat >/dev/null 2>&1; exit 0; fi
ctx=detached
if [ -n "$gitdir" ] && [ -r "$gitdir/HEAD" ]; then
  read -r head < "$gitdir/HEAD" 2>/dev/null
  case $head in "ref: refs/heads/"*) ctx=$(printf '%s' "${head#ref: refs/heads/}" | tr -c 'A-Za-z0-9._/+@-' '?' | cut -c1-80) ;; esac
fi
dir="$common/stw/oplog"
log="$dir/rewrites.log"
(umask 077; mkdir -p "$dir") 2>/dev/null
while read -r old new extra || [ -n "$old$new$extra" ]; do
  [ -n "$old$new" ] || continue
  case $old in ''|*[!0-9a-f]*) bad=$((bad+1)); continue ;; esac
  case $new in ''|*[!0-9a-f]*) bad=$((bad+1)); continue ;; esac
  case ${#old}:${#new} in 40:40|64:64) ;; *) bad=$((bad+1)); continue ;; esac
  case $extra in *[!A-Za-z0-9._/+@-]*) extra='?' ;; esac
  [ ${#extra} -le 40 ] || extra='?'
  if ! { (umask 077; printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$now" "$kind" "$old" "$new" "$extra" "$ctx" >> "$log"); } 2>/dev/null; then failed=$((failed+1)); fi
done
if [ "$failed" -gt 0 ] || [ "$bad" -gt 0 ]; then
  g=$(printf '%s\tgap\t%s\tfailed=%s\tbad=%s\t%s\n' "$now" "$kind" "$failed" "$bad" "$ctx")
  { (umask 077; printf '%s\n' "$g" >> "$dir/errors.log"); } 2>/dev/null ||
    { (umask 077; printf '%s\n' "$g" >> "$common/stw-oplog-gaps"); } 2>/dev/null
fi
exit 0
