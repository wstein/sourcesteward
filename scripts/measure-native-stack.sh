#!/bin/sh
# SPDX-License-Identifier: EUPL-1.2
# Owner-run measurement of the native stack landing sequence (Spec 043 FR-086, research.md "Native stack:
# measurement"). It WRITES to the sandbox repository you name: three branches, three pull requests, two base
# edits, ONE push to the default branch and, with --deletion-test, a branch deletion. Run it only against a
# throwaway repository that has the settings of the shape you want to record (ruleset copy, delete branch on
# merge). Agents never run it. It prints a record to paste into research.md; the allowlist entry
# (internal/land/shapes.json, "native": true) is added by hand after you read the record.
#
# usage: measure-native-stack.sh --repo OWNER/NAME --confirm OWNER/NAME [--stw PATH] [--wait SECONDS]
#                                [--deletion-test] [--dry-run]
# env:   GH (default gh), GIT (default git), MEASURE_SLEEP (default sleep)
# exit:  0 record printed, 1 a step failed or a check refused the push, 64 usage
set -eu

GH=${GH:-gh}
GIT=${GIT:-git}
SLEEP=${MEASURE_SLEEP:-sleep}

usage() {
  echo "usage: measure-native-stack.sh --repo OWNER/NAME --confirm OWNER/NAME [--stw PATH] [--wait SECONDS] [--deletion-test] [--dry-run]" >&2
  [ -z "${1:-}" ] || echo "measure-native-stack.sh: $1" >&2
  exit 64
}

repo= confirm= stw= wait=120 deletion=0 dry=0
while [ $# -gt 0 ]; do
  case $1 in
    --repo) [ $# -ge 2 ] || usage "--repo needs a value"; repo=$2; shift 2 ;;
    --confirm) [ $# -ge 2 ] || usage "--confirm needs a value"; confirm=$2; shift 2 ;;
    --stw) [ $# -ge 2 ] || usage "--stw needs a value"; stw=$2; shift 2 ;;
    --wait) [ $# -ge 2 ] || usage "--wait needs a value"; wait=$2; shift 2 ;;
    --deletion-test) deletion=1; shift ;;
    --dry-run) dry=1; shift ;;
    -h|--help) usage ;;
    *) usage "unknown argument $1" ;;
  esac
done

case $repo in
  ''|*[!A-Za-z0-9_./-]*|*/*/*|/*|*/|*..*|-*) usage "--repo must be OWNER/NAME" ;;
  */*) ;;
  *) usage "--repo must be OWNER/NAME" ;;
esac
[ "$confirm" = "$repo" ] || usage "--confirm must repeat --repo exactly (this script writes to that repository)"
case $wait in ''|*[!0-9]*) usage "--wait must be a number of seconds" ;; esac
[ "$wait" -le 900 ] || usage "--wait is at most 900"
case $repo in
  wstein/sourcesteward|wstein/cubistry|wstein/anchorage-workspace|wstein/whr-anchorage) usage "$repo is a real repository, not a sandbox" ;;
esac
case $repo in
  *sandbox*|*spike*|*scratch*|*measure*) ;;
  *) usage "the repository name must contain sandbox, spike, scratch or measure" ;;
esac

# say prints a command; do runs it unless this is a dry run.
say() { printf '+ %s\n' "$*" >&2; }
do_() { say "$@"; [ "$dry" -eq 1 ] || "$@"; }
rec() { printf '%s\n' "$*"; }

if [ "$dry" -eq 1 ]; then
  tmp=/tmp/stw-measure-DRYRUN # printed only, never created
else
  tmp=$(mktemp -d "${TMPDIR:-/tmp}/stw-measure.XXXXXX")
fi
recfile=$tmp/record.md
[ "$dry" -eq 0 ] || recfile=/dev/stdout
id=$(date +%Y%m%d%H%M%S)
ba=stw-measure-$id-a bb=stw-measure-$id-b bc=stw-measure-$id-c

if [ "$dry" -eq 1 ]; then
  say "$GH api repos/$repo --jq .default_branch"
  def=main
else
  def=$($GH api "repos/$repo" --jq '.default_branch')
  [ "$($GH api "repos/$repo" --jq '.permissions.push')" = "true" ] || { echo "no push access to $repo" >&2; exit 1; }
fi

do_ $GH repo clone "$repo" "$tmp/repo" -- --quiet

commit_on() { # branch from file
  do_ $GIT -C "$tmp/repo" checkout --quiet -b "$1" "$2"
  if [ "$dry" -eq 0 ]; then printf '%s\n' "$1" > "$tmp/repo/$3"; fi
  do_ $GIT -C "$tmp/repo" add "$3"
  do_ $GIT -C "$tmp/repo" commit --quiet -m "measure: $1"
  do_ $GIT -C "$tmp/repo" push --quiet -u origin "$1"
}
commit_on "$ba" "origin/$def" "measure-$id-a.txt"
commit_on "$bb" "$ba" "measure-$id-b.txt"
commit_on "$bc" "$bb" "measure-$id-c.txt"

pr_create() { # head base title
  say "$GH pr create --repo $repo --head $1 --base $2 --title $3 --body 'stw land native stack measurement'"
  if [ "$dry" -eq 1 ]; then echo "PR-$1"; return; fi
  url=$($GH pr create --repo "$repo" --head "$1" --base "$2" --title "$3" --body 'stw land native stack measurement')
  echo "${url##*/}"
}
na=$(pr_create "$ba" "$def" "measure $id a")
nb=$(pr_create "$bb" "$ba" "measure $id b")
nc=$(pr_create "$bc" "$bb" "measure $id c")
revparse() { # branch
  say "$GIT -C $tmp/repo rev-parse $1"
  if [ "$dry" -eq 1 ]; then echo "SHA-$1"; else $GIT -C "$tmp/repo" rev-parse "$1"; fi
}
ha=$(revparse "$ba"); hb=$(revparse "$bb"); hc=$(revparse "$bc")

view() { # number
  if [ "$dry" -eq 1 ]; then say "$GH pr view $1 --repo $repo --json number,state,baseRefName,headRefOid,mergedAt,mergeCommit,mergeable,mergeStateStatus,statusCheckRollup"; return 0; fi
  $GH pr view "$1" --repo "$repo" --json number,state,baseRefName,headRefOid,mergedAt,mergeCommit,mergeable,mergeStateStatus,statusCheckRollup
}

{
  rec "## Native stack measurement $id"
  rec "repository: $repo (default branch $def)"
  rec "settings: $([ "$dry" -eq 1 ] || $GH api "repos/$repo" --jq '"delete_branch_on_merge=\(.delete_branch_on_merge) allow_rebase_merge=\(.allow_rebase_merge)"')"
  rec "rules: $([ "$dry" -eq 1 ] || $GH api "repos/$repo/rules/branches/$def" --jq '[.[]|.type]|join(",")')"
  rec "stack: #$na ($ba) <- #$nb <- #$nc; heads $ha $hb $hc"
  rec "### before (native: b targets a, c targets b)"
  for n in "$na" "$nb" "$nc"; do rec "pr $n: $(view "$n")"; done
  if [ -n "$stw" ]; then
    rec "### stw plan before"
    rec "$(do_ "$stw" land --stack "$na,$nb,$nc" --reviewed "$na:$ha" --reviewed "$nb:$hb" --reviewed "$nc:$hc" --repo "$repo" --plan --json "$tmp/repo" || true)"
  fi

  rec "### retarget (E1 native variant)"
  do_ $GH pr edit "$nb" --repo "$repo" --base "$def" >&2
  do_ $GH pr edit "$nc" --repo "$repo" --base "$def" >&2
  for n in "$na" "$nb" "$nc"; do rec "pr $n after retarget: $(view "$n")"; done
  if [ -n "$stw" ]; then
    rec "### stw plan after the retarget (must be flat and landable except for the allowlist)"
    rec "$(do_ "$stw" land --stack "$na,$nb,$nc" --reviewed "$na:$ha" --reviewed "$nb:$hb" --reviewed "$nc:$hc" --repo "$repo" --plan --json "$tmp/repo" || true)"
  fi
  if [ "$dry" -eq 0 ]; then
    for pair in "$nb:$hb" "$nc:$hc"; do
      cur=$($GH pr view "${pair%%:*}" --repo "$repo" --json headRefOid,baseRefName --jq '.headRefOid + " " + .baseRefName')
      [ "$cur" = "${pair#*:} $def" ] || { echo "re-read refused the push: #${pair%%:*} is $cur" >&2; exit 1; }
    done
  fi

  rec "### push (E0/E1): git push --porcelain origin $hc:refs/heads/$def"
  if [ "$dry" -eq 1 ]; then say "$GIT -C $tmp/repo push --porcelain origin $hc:refs/heads/$def"; push_rc=0
  else
    set +e
    out=$($GIT -C "$tmp/repo" push --porcelain origin "$hc:refs/heads/$def" 2>&1); push_rc=$?
    set -e
  fi
  rec "push exit: $push_rc"
  rec "push output (verbatim): ${out:-}"
  if [ "$push_rc" -eq 0 ]; then
    t=0
    for step in 0 30 60 120 300 600; do
      [ "$step" -le "$wait" ] || break
      [ "$dry" -eq 1 ] || { [ "$step" -le "$t" ] || $SLEEP $((step - t)); t=$step; }
      rec "### after ${step}s"
      for n in "$na" "$nb" "$nc"; do rec "pr $n: $(view "$n")"; done
    done
    rec "### branches still on the remote (E2)"
    for b in "$ba" "$bb" "$bc"; do
      rec "$b: $([ "$dry" -eq 1 ] || { $GIT -C "$tmp/repo" ls-remote --heads origin "$b" | wc -l; })"
    done
    rec "### events (E3)"
    for n in "$na" "$nb" "$nc"; do
      rec "pr $n: $([ "$dry" -eq 1 ] || $GH api "repos/$repo/issues/$n/events" --jq '[.[]|.event]|join(",")')"
    done
  fi

  if [ "$deletion" -eq 1 ] && [ "$push_rc" -eq 0 ]; then
    rec "### deletion test (N4): child of a landed branch, base deleted by hand"
    bd=stw-measure-$id-d be=stw-measure-$id-e
    do_ $GIT -C "$tmp/repo" fetch --quiet origin
    commit_on "$bd" "origin/$def" "measure-$id-d.txt"
    commit_on "$be" "$bd" "measure-$id-e.txt"
    nd=$(pr_create "$bd" "$def" "measure $id d"); ne=$(pr_create "$be" "$bd" "measure $id e")
    hd=$(revparse "$bd")
    do_ $GIT -C "$tmp/repo" push --porcelain origin "$hd:refs/heads/$def" >&2
    $SLEEP 20
    do_ $GIT -C "$tmp/repo" push --quiet origin --delete "$bd" >&2
    for step in 5 30 60; do
      [ "$dry" -eq 1 ] || $SLEEP "$step"
      rec "after deleting $bd, ${step}s: d=$(view "$nd")"
      rec "after deleting $bd, ${step}s: e=$(view "$ne")"
    done
  fi
  rec "### end"
  rec "working copy: $tmp/repo (kept; pull requests and branches are kept for you to inspect or delete)"
} > "$recfile"
if [ "$dry" -eq 0 ]; then cat "$recfile"; echo "record also in $recfile" >&2; fi
