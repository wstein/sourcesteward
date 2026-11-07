# stw-lib.sh - shared helpers of the stw scripts (sourced, not run).
# bash 3.2 compatible. Finds git and gh itself; no sandbox, no refusals.
# SPDX-License-Identifier: EUPL-1.2

STW_SEP=$'\037'
export GIT_OPTIONAL_LOCKS=0 GIT_TERMINAL_PROMPT=0 LC_ALL=C

# stw_find NAME DIR... : PATH first, then the usual places; a tool must answer --version.
stw_find() {
  local name=$1 p d; shift
  p=$(command -v "$name" 2>/dev/null)
  if [ -n "$p" ] && "$p" --version >/dev/null 2>&1; then printf '%s\n' "$p"; return 0; fi
  for d in "$@"; do
    if [ -x "$d/$name" ] && "$d/$name" --version >/dev/null 2>&1; then printf '%s\n' "$d/$name"; return 0; fi
  done
  return 1
}
STW_GIT=$(stw_find git /usr/bin /opt/homebrew/bin /usr/local/bin /usr/local/git/bin) || {
  echo "stw: no working git found (looked on PATH, /usr/bin, /opt/homebrew/bin, /usr/local/bin)" >&2; exit 2; }
STW_GH=$(stw_find gh /opt/homebrew/bin /usr/local/bin /usr/bin) || STW_GH=

# g REPO ARGS... : read-only git in REPO without the fsmonitor daemon.
g() { local r=$1; shift; "$STW_GIT" -C "$r" -c core.fsmonitor=false "$@"; }

# js STRING : JSON string literal. jn N : number or null. jb 1|0 : bool.
js() {
  local s=$1
  case $s in *[[:cntrl:]]*)
    s=${s//\\/\\\\}; s=${s//\"/\\\"}; s=${s//$'\n'/\\n}; s=${s//$'\r'/\\r}; s=${s//$'\t'/\\t}
    s=$(printf '%s' "$s" | tr -d '\000-\010\013\014\016-\037')
    printf '"%s"' "$s"; return ;;
  esac
  s=${s//\\/\\\\}; s=${s//\"/\\\"}
  printf '"%s"' "$s"
}
jn() { case $1 in ''|*[!0-9-]*) printf 'null' ;; *) printf '%s' "$1" ;; esac; }
jb() { if [ "$1" = 1 ]; then printf 'true'; else printf 'false'; fi; }
jsn() { if [ -z "$1" ]; then printf 'null'; else js "$1"; fi; }

# stw_root_default : explicit root is handled by the caller. Order: superproject / repo with submodules of the
# current directory, $STW_ROOT, plain git top level of the current directory, else message and return 2.
stw_root_default() {
  local top sup
  top=$("$STW_GIT" rev-parse --show-toplevel 2>/dev/null) || top=
  if [ -n "$top" ]; then
    sup=$("$STW_GIT" -C "$top" rev-parse --show-superproject-working-tree 2>/dev/null)
    if [ -n "$sup" ]; then printf '%s\n' "$sup"; return; fi
    if [ -f "$top/.gitmodules" ]; then printf '%s\n' "$top"; return; fi
  fi
  if [ -n "${STW_ROOT:-}" ]; then printf '%s\n' "$STW_ROOT"; return; fi
  if [ -n "$top" ]; then printf '%s\n' "$top"; return; fi
  echo "stw: no workspace root: pass --root PATH, set STW_ROOT, or run inside a git repository" >&2
  return 2
}

# stw_repos ROOT : prints "name<SEP>path" for the root and every checked-out submodule.
stw_repos() {
  local root=$1 key path
  printf '%s%s%s\n' "$(basename "$root")" "$STW_SEP" "$root"
  if [ -f "$root/.gitmodules" ]; then
    "$STW_GIT" config --file "$root/.gitmodules" --get-regexp '^submodule\..*\.path$' 2>/dev/null |
    while read -r key path; do
      if [ -e "$root/$path/.git" ]; then printf '%s%s%s\n' "$path" "$STW_SEP" "$root/$path"; fi
    done
  fi
}

# stw_slug PATH : owner/repo of origin on GitHub, empty otherwise.
stw_slug() {
  local url; url=$(g "$1" remote get-url origin 2>/dev/null) || return 0
  case $url in
    *github.com[:/]*) url=${url#*github.com[:/]}; url=${url%.git}; url=${url%/}; printf '%s\n' "$url" ;;
  esac
}

# stw_base PATH : default branch ref of origin (refs/remotes/origin/HEAD), else origin/main, origin/master.
stw_base() {
  local b
  b=$(g "$1" symbolic-ref -q refs/remotes/origin/HEAD 2>/dev/null)
  if [ -z "$b" ]; then
    for b in refs/remotes/origin/main refs/remotes/origin/master; do
      if g "$1" rev-parse -q --verify "$b" >/dev/null 2>&1; then printf '%s\n' "$b"; return; fi
    done
    return
  fi
  printf '%s\n' "$b"
}

stw_have_gh() { [ -n "$STW_GH" ] && "$STW_GH" auth status >/dev/null 2>&1; }

# stw_shq STRING : shell-quoted for the printed next-step commands.
stw_shq() { case $1 in *[!A-Za-z0-9_./:@=+-]*|'') printf "'%s'" "${1//\'/\'\\\'\'}" ;; *) printf '%s' "$1" ;; esac; }

stw_tmp() { STW_TMP=$(mktemp -d "${TMPDIR:-/tmp}/stw.XXXXXX") || exit 2; trap 'rm -rf "$STW_TMP"' EXIT; }
