#!/bin/bash
# Oracle of `stw tidy plan` against real repositories (package 4): the keys of gix against `git diff -U0` (hunk oracle),
# the same commits against every `diff.algorithm` (matrix), the merged pull requests of the repositories against the plan
# (rebase oracle), and the cost of `stw tidy plan` (seconds, peak memory). Writes the report OUTDIR/bericht.md.
# Usage: tidy_oracle.sh OUTDIR
# OUTDIR: absolute, outside a Git tree or ignored by it. Everything the script writes is below OUTDIR (clones/, out/, raw/);
# nothing is written to the repository, nothing is pushed, a clone is only read (the pull request heads are fetched as
# local branches `oracle-pr-<n>` of the clone). Variables (all optional):
#   ORACLE_HUNK           slug (cloned to OUTDIR/clones) or the path of an existing clone, read only [flix/flix]
#   ORACLE_REBASE         slugs separated by commas, cloned to OUTDIR/clones [wstein/dotagents,wstein/sourcesteward,wstein/whr-anchorage]
#   ORACLE_RAWDIR         directory with <owner>_<name>.json of GitHub (merged pull requests, their commits, the commits on the
#                         base); a missing file is collected with `gh api` from the last ORACLE_PRS merged pull requests [OUTDIR/raw]
#   ORACLE_PRS            number of pull requests to collect [15]
#   ORACLE_HUNK_PRS       pull request heads of the hunk repository fetched as branches for the cost table, 0 for none [10]
#   ORACLE_STW            the stw binary for the cost table (release build); without it the table is left out
#   ORACLE_HUNK_COMMITS   [1000]    ORACLE_MATRIX_COMMITS [500]
#   ORACLE_MIN_RATE       stop below this many remaining GitHub requests [500]
#   ORACLE_CLEAN          1: delete OUTDIR/clones when done [0]
# Needs: cargo, git, gh (login or GH_TOKEN; the token is never printed or written), jq, python3. flock when present.
set -u
die() { echo "tidy_oracle.sh: $*" >&2; exit 2; }
[ $# -eq 1 ] && [ -n "$1" ] || die "usage: tidy_oracle.sh OUTDIR (absolute path)"
case $1 in /*) ;; *) die "OUTDIR must be an absolute path: $1" ;; esac
case $1 in *..*) die "OUTDIR must not contain '..': $1" ;; esac
[ "$1" != / ] || die "OUTDIR must not be /"
probe=$1
while [ ! -d "$probe" ]; do probe=$(dirname "$probe"); done
if [ "$(git -C "$probe" rev-parse --is-inside-work-tree 2> /dev/null)" = true ]; then
  git -C "$probe" check-ignore -q "$1" || die "OUTDIR is inside a Git tree that does not ignore it: $1"
fi
for t in cargo git gh jq python3; do command -v "$t" > /dev/null || die "needs $t"; done
mkdir -p "$1" || die "cannot create $1"
OUT=$(cd "$1" && pwd -P)
HERE=$(cd "$(dirname "$0")" && pwd)
CRATE=$(cd "$HERE/../.." && pwd)
[ "$OUT" != "$CRATE" ] || die "OUTDIR is the crate"
mkdir -p "$OUT/clones" "$OUT/out" "$OUT/raw"
HUNK=${ORACLE_HUNK:-flix/flix}
REBASE=${ORACLE_REBASE:-wstein/dotagents,wstein/sourcesteward,wstein/whr-anchorage}
RAWDIR=${ORACLE_RAWDIR:-$OUT/raw}
PRS=${ORACLE_PRS:-15}
MIN_RATE=${ORACLE_MIN_RATE:-500}
STW=${ORACLE_STW:-}
LOCK=$(cd "$CRATE/../.." && pwd)/scratch/cargo-queue.lock

rate() { gh api rate_limit --jq .resources.core.remaining 2> /dev/null || echo 0; }
check_rate() {
  r=$(rate)
  [ "$r" -ge "$MIN_RATE" ] || die "GitHub rate limit: $r requests left, below $MIN_RATE; stopped"
}
name_of() { echo "$1" | tr / _; }

# The clone of a slug below OUTDIR/clones (an existing one is fetched, not cloned again).
clone() {
  d=$OUT/clones/$(name_of "$1")
  if [ -d "$d/.git" ]; then
    git -C "$d" fetch -q origin || die "fetch of $1 failed"
  else
    gh repo clone "$1" "$d" > "$OUT/out/clone-$(name_of "$1").log" 2>&1 || die "clone of $1 failed (see out/clone-$(name_of "$1").log)"
  fi
  echo "$d"
}

# Raw data in the shape of scratch/stw-tidy-rebase/raw: number, pr, pr_commits, main_commits.
collect_raw() {
  slug=$1 out=$2
  check_rate
  nums=$(gh api "repos/$slug/pulls?state=closed&per_page=100" --jq "[.[] | select(.merged_at != null)] | .[0:$PRS] | .[].number") || die "cannot list pull requests of $slug"
  : > "$out.tmp"
  for n in $nums; do
    pr=$(gh api "repos/$slug/pulls/$n" --jq '{merged, merged_at, merge_commit_sha, commits, additions, deletions, title, state, head_sha: .head.sha, base: .base.ref}') || die "pulls/$n failed"
    pc=$(gh api "repos/$slug/pulls/$n/commits?per_page=100") || die "pulls/$n/commits failed"
    k=$(echo "$pr" | jq .commits)
    sha=$(echo "$pr" | jq -r .merge_commit_sha)
    mc=$(gh api "repos/$slug/commits?sha=$sha&per_page=$((k + 3))") || die "commits of $sha failed"
    jq -n --argjson number "$n" --argjson pr "$pr" --argjson pc "$pc" --argjson mc "$mc" \
      '{number: $number, pr: $pr, pr_commits: $pc, main_commits: $mc}' >> "$out.tmp"
  done
  jq -s . "$out.tmp" > "$out" && rm -f "$out.tmp"
}

# --- repositories ---
# Fetch the heads of the merged pull requests of a raw file into the clone as local branches oracle-pr-<n>.
fetch_heads() {
  d=$1 raw=$2
  args=()
  for n in $(jq -r '.[] | select(.pr.merged == true) | .number' "$raw"); do
    args+=("+refs/pull/$n/head:refs/heads/oracle-pr-$n")
  done
  [ "${#args[@]}" -gt 0 ] || die "no merged pull request in $raw"
  git -C "$d" fetch -q origin "${args[@]}" || die "fetch of the pull request heads into $d failed"
}

if [ -d "$HUNK" ]; then
  HUNK_DIR=$(cd "$HUNK" && pwd -P)
else
  check_rate
  HUNK_DIR=$(clone "$HUNK")
  # branches for the cost table (a repository with branches that are not merged); a squash repository: no rebase verdict
  if [ "${ORACLE_HUNK_PRS:-10}" -gt 0 ]; then
    raw=$RAWDIR/$(name_of "$HUNK").json
    PRS_SAVE=$PRS; PRS=${ORACLE_HUNK_PRS:-10}
    [ -s "$raw" ] || collect_raw "$HUNK" "$raw"
    PRS=$PRS_SAVE
    fetch_heads "$HUNK_DIR" "$raw"
    HUNK_BRANCHES=1
  fi
fi
repos=""
for slug in $(echo "$REBASE" | tr ',' ' '); do
  check_rate
  d=$(clone "$slug")
  raw=$RAWDIR/$(name_of "$slug").json
  [ -s "$raw" ] || collect_raw "$slug" "$raw"
  fetch_heads "$d" "$raw"
  repos="${repos:+$repos,}$d:$raw"
done

# --- the tests ---
export STW_ORACLE_OUT=$OUT/out STW_ORACLE_HUNK_REPO=$HUNK_DIR STW_ORACLE_REBASE_REPOS=$repos
export STW_ORACLE_HUNK_COMMITS=${ORACLE_HUNK_COMMITS:-1000} STW_ORACLE_MATRIX_COMMITS=${ORACLE_MATRIX_COMMITS:-500}
rm -f "$OUT/out/hunk.md" "$OUT/out/matrix.md" "$OUT/out/rebase.md"
run_tests() {
  cd "$CRATE" && cargo test --test it oracle_tidy -- --ignored --nocapture --test-threads=1
}
if command -v flock > /dev/null && [ -d "$(dirname "$LOCK")" ]; then
  flock "$LOCK" bash -c "$(declare -f run_tests); CRATE='$CRATE'; run_tests" > "$OUT/out/tests.log" 2>&1
else
  run_tests > "$OUT/out/tests.log" 2>&1
fi
tests_rc=$?
tail -5 "$OUT/out/tests.log"

# --- the cost of `stw tidy plan` ---
PYWRAP='
import resource, subprocess, sys, time
t = time.time()
p = subprocess.run(sys.argv[1:], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
secs = time.time() - t
rss = resource.getrusage(resource.RUSAGE_CHILDREN).ru_maxrss
if sys.platform == "darwin":
    rss //= 1024
print("%.2f %d %d" % (secs, rss // 1024, p.returncode))
'
cost() { # label clone args...
  label=$1 dir=$2; shift 2
  best=""
  for i in 1 2; do
    res=$(cd "$dir" && python3 -I -c "$PYWRAP" "$STW" tidy plan --json --root "$dir" "$@")
    if [ -z "$best" ] || [ "$(echo "${res%% *} ${best%% *}" | awk '{print ($1 < $2)}')" = 1 ]; then best=$res; fi
  done
  set -- $best
  echo "| $(basename "$dir") | $label | $1 | $2 | $3 |"
}
COST=""
if [ -n "$STW" ] && [ -x "$STW" ]; then
  COST="| repository | mode | seconds (best of 2) | peak memory MB | exit |
|---|---|---|---|---|
"
  for d in "$HUNK_DIR" $(echo "$repos" | tr ',' '\n' | sed 's/:.*//'); do
    COST="$COST$(cost 'offline, similarity and rebase evidence' "$d" --offline)
$(cost 'offline, --no-rebase-evidence' "$d" --offline --no-rebase-evidence)
$(cost 'offline, --no-similarity --no-rebase-evidence' "$d" --offline --no-similarity --no-rebase-evidence)
"
  done
  online=$(echo "$repos" | tr ',' '\n' | sed 's/:.*//')
  [ "${HUNK_BRANCHES:-0}" = 1 ] && online="$HUNK_DIR $online"
  for d in $online; do
    check_rate
    COST="$COST$(cost 'online (GitHub read), similarity and rebase evidence' "$d")
"
    (cd "$d" && "$STW" tidy plan --json --root "$d" > "$OUT/out/plan-$(basename "$d")-online.json" 2> /dev/null)
    jq -r '[.repos[0].branches[] | select(.branch | startswith("oracle-pr-")) | "\(.landed_via.kind)/\(.landed_via.basis)/\(.landed_via.strength), proof \(.proof)"] | group_by(.) | map("  " + .[0] + ": " + (length | tostring)) | .[]' \
      "$OUT/out/plan-$(basename "$d")-online.json" > "$OUT/out/landed-$(basename "$d")-online.txt" 2> /dev/null
  done
fi

# --- the report ---
{
  echo "# stw tidy oracle"
  echo
  echo "Run: $(date -u +%Y-%m-%dT%H:%M:%SZ), tests exit code $tests_rc (0 = the harness compared; findings are results)."
  echo "git: $(git --version), stw: ${STW:-not given}"
  echo
  echo "## A. Hunk oracle: keys of gix (Myers, no slider heuristic) against git diff -U0 --no-renames"
  echo
  echo '```'; cat "$OUT/out/hunk.md" 2> /dev/null || echo "not run"; echo '```'
  echo
  echo "## C. Algorithm matrix: the same commits against every diff.algorithm"
  echo
  echo '```'; cat "$OUT/out/matrix.md" 2> /dev/null || echo "not run"; echo '```'
  echo
  echo "## B. Rebase oracle: merged pull requests against the plan (offline, from the commits)"
  echo
  echo '```'; cat "$OUT/out/rebase.md" 2> /dev/null || echo "not run"; echo '```'
  for f in "$OUT"/out/landed-*-online.txt; do
    [ -f "$f" ] || continue
    echo
    echo "Online plan, landed via per pull request head of $(basename "$f" .txt | sed 's/^landed-//; s/-online$//'):"
    echo '```'; cat "$f"; echo '```'
  done
  echo
  echo "## Cost"
  echo
  if [ -n "$COST" ]; then printf '%s\n' "$COST"; else echo "not measured (set ORACLE_STW to a release build of stw)"; fi
} > "$OUT/bericht.md"
echo "report: $OUT/bericht.md"

if [ "${ORACLE_CLEAN:-0}" = 1 ]; then
  case $OUT/clones in "$OUT"/clones) rm -rf "$OUT/clones" ;; esac
fi
exit "$tests_rc"
