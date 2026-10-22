#!/bin/sh
# SPDX-License-Identifier: EUPL-1.2
#
# Local checks of the release workflow (specs/034-bootstrap-release).
#
# Usage:
#   sh release-verify.sh version VERSION    VERSION is a strict release version
#   sh release-verify.sh dryversion VERSION VERSION is a dry-run label: a version with a
#                                           mandatory suffix, never a release version
#   sh release-verify.sh tag TAG MAIN_REF   annotated tag on a commit reachable from MAIN_REF;
#                                           prints the tagged commit
#
# Reads local files and git objects only; no network and no writes. Exit 0 when the
# check holds, 1 when it fails, 2 on a usage error.

set -eu

fail() {
	printf 'release-verify: %s\n' "$*" >&2
	exit 1
}
usage() {
	printf 'usage: sh release-verify.sh version V | dryversion V | tag TAG MAIN_REF\n' >&2
	exit 2
}

NL='
'
release_re='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
dry_re='^v[0-9]+\.[0-9]+\.[0-9]+-[0-9A-Za-z.]+$'

# match RE VALUE: whole-value match; a newline never matches (grep works per line).
match() {
	case $2 in
	*"$NL"*) return 1 ;;
	esac
	printf '%s\n' "$2" | grep -Eq "$1"
}

[ "$#" -ge 1 ] || usage
cmd=$1
shift
case $cmd in
version)
	[ "$#" -eq 1 ] || usage
	match "$release_re" "$1" || fail "not a release version (want vMAJOR.MINOR.PATCH): $1"
	;;
dryversion)
	[ "$#" -eq 1 ] || usage
	match "$dry_re" "$1" || fail "not a dry-run version: $1"
	;;
tag)
	[ "$#" -eq 2 ] || usage
	tag=$1
	main=$2
	match "$release_re" "$tag" || fail "not a release version (want vMAJOR.MINOR.PATCH): $tag"
	kind=$(git cat-file -t "refs/tags/$tag" 2>/dev/null) || fail "tag $tag does not exist"
	[ "$kind" = tag ] || fail "tag $tag is not an annotated tag (object type: $kind)"
	commit=$(git rev-parse --verify "refs/tags/$tag^{commit}") || fail "tag $tag does not name a commit"
	git merge-base --is-ancestor "$commit" "$main" || fail "commit $commit of $tag is not an ancestor of $main"
	printf '%s\n' "$commit"
	;;
*)
	usage
	;;
esac
