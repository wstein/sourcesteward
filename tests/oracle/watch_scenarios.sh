#!/bin/bash
# Exports the scenarios of `stw watch` (port corpus, tests/fixtures/watch/FORMAT.md) from the Go sources.
# Usage: watch_scenarios.sh GO_SRC OUTDIR
#   GO_SRC   absolute path of the Go source tree (the module github.com/wstein/sourcesteward, for example legacy/sourcesteward);
#            it is only READ: the script copies it (without .git) into a private work directory, lays the exporter of
#            tests/oracle/watch_scenarios/ into the copy (internal/ packages are importable only inside the module),
#            and runs `go test` there. The Go tests and sources of the copy are not changed, only files are added.
#   OUTDIR   absolute path where <name>.json and index.json are written. Refused: empty, relative, '..', '/', and a
#            directory inside a Git tree that does not ignore it (a scratch directory is ignored). The one exception is
#            the tracked tests/fixtures/watch of this crate, with WATCH_SCENARIOS_WRITE_FIXTURES=1 (the deliberate export).
#            OUTDIR must be empty or hold an index.json of an earlier export; its *.json files are then replaced.
# Variables: TMPDIR (absolute, default /tmp; the work directory is created below it and removed at the end, also on failure;
#            it must allow running binaries), WATCH_SCENARIOS_KEEP=1 keeps the work directory, GO (the go binary).
# Needs: go (1.27), tar. Nothing outside the work directory and OUTDIR is written; nothing is deleted outside them.
set -u
die() { echo "watch_scenarios.sh: $*" >&2; exit 2; }
[ $# -eq 2 ] && [ -n "$1" ] && [ -n "$2" ] || die "usage: watch_scenarios.sh GO_SRC OUTDIR (absolute paths)"
HERE=$(cd "$(dirname "$0")" && pwd -P)
GO_SRC=$1 OUT=$2

# A directory the script may write to: absolute, without '..', not /, and where it lies in a Git work tree (after
# resolving symlinks) ignored by that tree. The directory need not exist yet. Nothing is created here.
guard_dir() {
  gname=$1 gpath=$2
  [ -n "$gpath" ] || die "$gname is empty"
  case $gpath in /*) ;; *) die "$gname must be an absolute path: $gpath" ;; esac
  case $gpath in *..*) die "$gname must not contain '..': $gpath" ;; esac
  [ "$gpath" != / ] || die "$gname must not be /"
  gprobe=$gpath gtail=""
  while [ ! -d "$gprobe" ]; do
    gtail="/${gprobe##*/}$gtail"
    gprobe=$(dirname "$gprobe")
  done
  gprobe=$(cd "$gprobe" && pwd -P)
  if [ "$(git -C "$gprobe" rev-parse --is-inside-work-tree 2> /dev/null)" = true ]; then
    # the trailing slash: a pattern such as out/ matches a directory that is not there yet
    git -C "$gprobe" check-ignore -q "$gprobe$gtail/" || die "$gname is inside a Git tree that does not ignore it: $gpath"
  fi
}

case $GO_SRC in /*) ;; *) die "GO_SRC must be an absolute path: $GO_SRC" ;; esac
case $GO_SRC in *..*) die "GO_SRC must not contain '..': $GO_SRC" ;; esac
[ -d "$GO_SRC" ] || die "GO_SRC is not a directory: $GO_SRC"
grep -q '^module github.com/wstein/sourcesteward$' "$GO_SRC/go.mod" 2> /dev/null || die "GO_SRC is not the sourcesteward Go module: $GO_SRC"
[ -d "$GO_SRC/internal/watch" ] || die "GO_SRC has no internal/watch: $GO_SRC"

FIXTURES=$(cd "$HERE/.." && pwd -P)/fixtures/watch
if [ "${WATCH_SCENARIOS_WRITE_FIXTURES:-}" = 1 ] && [ "$OUT" = "$FIXTURES" ]; then
  :
else
  guard_dir OUTDIR "$OUT"
fi
if [ -e "$OUT" ]; then
  [ -d "$OUT" ] || die "OUTDIR is not a directory: $OUT"
  if [ -n "$(ls -A "$OUT" 2> /dev/null)" ] && [ ! -f "$OUT/index.json" ]; then
    case $(ls -A "$OUT") in
      FORMAT.md) ;; # the tracked directory before the first export
      *) die "OUTDIR is not empty and has no index.json of an earlier export: $OUT" ;;
    esac
  fi
fi

tmp=${TMPDIR-/tmp}
[ -n "$tmp" ] || die "TMPDIR is set but empty"
case $tmp in /*) ;; *) die "TMPDIR must be an absolute path: $tmp" ;; esac
[ -d "$tmp" ] || die "TMPDIR is not a directory: $tmp"
guard_dir TMPDIR "$tmp"
WORK=$(mktemp -d "$tmp/stw-watch-scn.XXXXXX") || die "cannot create a work directory below $tmp"
[ -n "$WORK" ] && [ -d "$WORK" ] || die "no work directory"
cleanup() {
  if [ "${WATCH_SCENARIOS_KEEP:-}" = 1 ]; then echo "work directory kept: $WORK" >&2; else rm -rf "$WORK"; fi
}
trap cleanup EXIT

GOBIN=${GO:-}
[ -n "$GOBIN" ] || GOBIN=$(command -v go) || GOBIN=$(mise which go 2> /dev/null) || die "no go binary found (set GO)"
[ -x "$GOBIN" ] || die "not executable: $GOBIN"

mkdir "$WORK/go" "$WORK/out"
(cd "$GO_SRC" && tar --exclude=./.git --exclude=./.worktrees -cf - .) | (cd "$WORK/go" && tar -xf -) || die "copy of GO_SRC failed"
cp -R "$HERE/watch_scenarios/internal/." "$WORK/go/internal/" || die "cannot lay the exporter into the copy"

cd "$WORK/go" || die "no copy"
export STW_EXPORT_DIR="$WORK/out"
"$GOBIN" test -count=1 -run '^TestExportScenarios$' ./internal/watch/ ./internal/watch/watchhost/ ./internal/watchcmd/ ./internal/hosting/ || die "the export failed"
"$GOBIN" test -count=1 -run '^TestExportIndex$' ./internal/watchexport/ || die "the index failed"
[ -f "$WORK/out/index.json" ] || die "no index.json was written"

mkdir -p "$OUT" || die "cannot create $OUT"
rm -f "$OUT"/*.json
cp "$WORK/out"/*.json "$OUT"/ || die "cannot write to $OUT"
echo "exported $(ls "$OUT"/*.json | grep -vc '/index.json$') scenarios to $OUT"
