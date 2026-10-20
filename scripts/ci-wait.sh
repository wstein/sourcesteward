#!/bin/sh
# SPDX-License-Identifier: EUPL-1.2
# Interim bounded, read-only wait for the CI runs of a pull request head (issue #93).
# Observes only: it never re-runs, cancels or dispatches anything.
#
# usage: ci-wait.sh [-R OWNER/REPO] [--head SHA] [--timeout SECONDS]
#                   [--appear-timeout SECONDS] [--interval SECONDS] [--json] PR_NUMBER
# env:   GH (default gh), CI_WAIT_SLEEP (default sleep), CI_WAIT_NOW (default: date +%s)
# exit:  0 success, 1 failure, 2 deadline, 3 no-runs, 4 head-changed, 5 unavailable, 64 usage
set -eu

GH=${GH:-gh}
SLEEP=${CI_WAIT_SLEEP:-sleep}
NOW=${CI_WAIT_NOW:-}
LOG_BYTES=204800
MAX_TESTS=10
BACKOFF_CAP=60

usage() {
  echo "usage: ci-wait.sh [-R OWNER/REPO] [--head SHA] [--timeout SECONDS] [--appear-timeout SECONDS] [--interval SECONDS] [--json] PR_NUMBER" >&2
  [ -z "${1:-}" ] || echo "ci-wait.sh: $1" >&2
  exit 64
}

now() {
  if [ -n "$NOW" ]; then $NOW; else date +%s; fi
}

repo=
expected=
timeout=2700
appear=120
interval=20
json=0
pr=

is_uint() { case $1 in ''|*[!0-9]*) return 1 ;; esac; }

while [ $# -gt 0 ]; do
  case $1 in
    -R|--head|--timeout|--appear-timeout|--interval)
      [ $# -ge 2 ] || usage "missing value for $1"
      case $1 in
        -R) repo=$2 ;;
        --head) expected=$2 ;;
        --timeout) timeout=$2 ;;
        --appear-timeout) appear=$2 ;;
        --interval) interval=$2 ;;
      esac
      shift 2 ;;
    --json) json=1; shift ;;
    -h|--help) usage ;;
    -*) usage "unknown option $1" ;;
    *) [ -z "$pr" ] || usage "only one PR number"; pr=$1; shift ;;
  esac
done
[ -n "$pr" ] || usage "PR_NUMBER required"
is_uint "$pr" || usage "PR_NUMBER must be a number"
is_uint "$timeout" || usage "--timeout must be a number"
is_uint "$appear" || usage "--appear-timeout must be a number"
is_uint "$interval" && [ "$interval" -gt 0 ] || usage "--interval must be a positive number"
if [ -n "$expected" ]; then
  case $expected in *[!0-9a-fA-F]*) usage "--head must be a hex SHA" ;; esac
  [ "${#expected}" -ge 7 ] || usage "--head must be a hex SHA"
fi
command -v jq >/dev/null 2>&1 || { echo "ci-wait.sh: jq is required" >&2; exit 5; }

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT HUP INT TERM

start=$(now)
deadline=$((start + timeout))
appear_deadline=$((start + appear))
[ "$appear_deadline" -le "$deadline" ] || appear_deadline=$deadline
cap=$BACKOFF_CAP
[ "$interval" -le "$cap" ] || cap=$interval

head=
runs='[]'
nruns=0
outcome=
code=0

fetch_head() {
  out=$("$GH" pr view "$pr" ${repo:+-R "$repo"} --json headRefOid 2>"$work/err") || return 1
  head=$(printf '%s' "$out" | jq -er '.headRefOid') || return 1
  [ -n "$head" ] || return 1
}

fetch_runs() {
  out=$("$GH" run list ${repo:+-R "$repo"} --commit "$1" --limit 100 \
    --json databaseId,workflowName,status,conclusion,startedAt,updatedAt 2>"$work/err") || return 1
  printf '%s' "$out" | jq -e 'type == "array"' >/dev/null || return 1
  runs=$out
  nruns=$(printf '%s' "$runs" | jq 'length')
}

finish() {
  outcome=$1
  code=$2
  elapsed=$(( $(now) - start ))
  [ "$elapsed" -ge 0 ] || elapsed=0
  short=$(printf '%.7s' "${expected:-$head}")
  report
  exit "$code"
}

# failed test names from a bounded, filtered excerpt of the failed-step log
failed_tests() {
  "$GH" run view "$1" ${repo:+-R "$repo"} --log-failed 2>/dev/null | head -c "$LOG_BYTES" \
    | grep -a -- '--- FAIL: Test' | cut -c1-200 \
    | sed -n 's/.*--- FAIL: \(Test[^ ]*\).*/\1/p' | tr -cd '[:print:]\n' \
    | awk '!seen[$0]++' | head -n "$MAX_TESTS" || true
}

report() {
  : > "$work/rows"
  printf '%s' "$runs" | jq -c '.[]' > "$work/runs.jsonl"
  while IFS= read -r row; do
    id=$(printf '%s' "$row" | jq -r '.databaseId')
    concl=$(printf '%s' "$row" | jq -r 'if .status != "completed" then .status else (.conclusion // "unknown") end')
    tests=
    case $concl in
      success|skipped|neutral|queued|in_progress|waiting|pending|requested) ;;
      *) tests=$(failed_tests "$id") ;;
    esac
    printf '%s' "$row" | jq -c --arg c "$concl" --arg t "$tests" '
      def clean: gsub("[^ -~]"; "?");
      def secs: (try (((.updatedAt | fromdateiso8601) - (.startedAt | fromdateiso8601)) | if . < 0 then 0 else . end) catch null);
      {workflow: ((.workflowName // "unknown") | clean), conclusion: $c, duration_seconds: secs,
       failed_tests: ($t | split("\n") | map(select(length > 0)))}' >> "$work/rows"
  done < "$work/runs.jsonl"
  if [ "$json" = 1 ]; then
    jq -s --arg o "$outcome" --arg h "${expected:-$head}" --arg cur "$head" --argjson e "$elapsed" \
      '{outcome: $o, head: $h, current_head: $cur, elapsed: $e, runs: .}' "$work/rows"
    return
  fi
  jq -r '
    def dur: if . == null then "-" elif . >= 60 then "\(. / 60 | floor)m\(. % 60 | tostring | if length < 2 then "0" + . else . end)s" else "\(.)s" end;
    "\(.workflow) | \(.conclusion) | \(.duration_seconds | dur)",
    (.failed_tests[] | "  failed: \(.)")' "$work/rows"
  [ "$outcome" != head-changed ] || echo "current-head=$head"
  echo "outcome=$outcome head=$short elapsed=$elapsed"
}

unavailable() {
  runs='[]'
  expected=${expected:-$head}
  msg=$(head -c 200 "$work/err" 2>/dev/null | tr -cd '[:print:]' || true)
  [ -z "$msg" ] || echo "ci-wait.sh: evidence unavailable: $msg" >&2
  finish unavailable 5
}

sleep_for() { # limit-timestamp
  remaining=$(( $1 - $(now) ))
  [ "$remaining" -gt 0 ] || return 0
  step=$interval
  [ "$step" -le "$remaining" ] || step=$remaining
  "$SLEEP" "$step"
  interval=$(( interval + interval / 2 + 1 ))
  [ "$interval" -le "$cap" ] || interval=$cap
}

seen=0
while :; do
  fetch_head || unavailable
  [ -n "$expected" ] || expected=$head
  case $head in "$expected"*) ;; *)
    case $expected in "$head"*) ;; *) finish head-changed 4 ;; esac ;;
  esac
  fetch_runs "$head" || unavailable
  if [ "$nruns" -gt 0 ]; then
    seen=1
    active=$(printf '%s' "$runs" | jq '[.[] | select(.status != "completed")] | length')
    if [ "$active" -eq 0 ]; then
      bad=$(printf '%s' "$runs" | jq '[.[] | select((.conclusion // "") as $c | ["success","skipped","neutral"] | index($c) | not)] | length')
      if [ "$bad" -eq 0 ]; then finish success 0; else finish failure 1; fi
    fi
    [ "$(now)" -lt "$deadline" ] || finish deadline 2
    sleep_for "$deadline"
  else
    if [ "$seen" -eq 1 ]; then
      # runs vanished after being seen: not evidence of anything
      unavailable
    fi
    [ "$(now)" -lt "$appear_deadline" ] || finish no-runs 3
    sleep_for "$appear_deadline"
  fi
done
