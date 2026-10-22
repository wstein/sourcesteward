#!/bin/sh
# SPDX-License-Identifier: EUPL-1.2
#
# Exercise a shipped release archive on its host (specs/034-bootstrap-release).
#
# Usage: sh release-smoke.sh VERSION ARCHIVE
#
# ARCHIVE is stw_<version>_<os>_<arch>.tar.gz next to its .sha256 sidecar. Run in the
# repository root: the expected third_party/ members are read from there. Checks, in order:
# the checksum; the archive members; that `stw version` prints "SourceSteward VERSION" (and
# the same version with --json); `--help`; then, on a tiny temporary Git repository and with
# no network: `inspect` and `status`, which must leave every reference and the working tree
# unchanged. Commands that run Git inside the native sandbox (bubblewrap, sandbox-exec), such as
# `tidy plan`, are deliberately not run here: the runners need not allow the sandbox, and ci.yml
# conformance covers them. Works with POSIX tools on Linux and macOS.

set -eu

fail() {
	printf 'release-smoke: %s\n' "$*" >&2
	exit 1
}

[ "$#" -eq 2 ] || {
	printf 'usage: sh release-smoke.sh VERSION ARCHIVE\n' >&2
	exit 2
}
version=$1
archive=$2

[ -f "$archive" ] || fail "archive not found: $archive"
archive=$(cd "$(dirname "$archive")" && pwd)/$(basename "$archive")
file=$(basename "$archive")
case $file in
*.tar.gz) name=${file%.tar.gz} ;;
*) fail "not a .tar.gz: $file" ;;
esac

# Checksum written at build time (sha256sum on Linux, shasum on macOS).
[ -f "$archive.sha256" ] || fail "checksum sidecar missing: $file.sha256"
if command -v sha256sum >/dev/null 2>&1; then
	(cd "$(dirname "$archive")" && sha256sum -c "$file.sha256" >/dev/null) || fail "checksum mismatch for $file"
else
	(cd "$(dirname "$archive")" && shasum -a 256 -c "$file.sha256" >/dev/null) || fail "checksum mismatch for $file"
fi

# Physical path: macOS TMPDIR is under /var -> /private/var.
d=$(mktemp -d) || fail "cannot create a temp dir"
trap 'rm -rf "$d"' EXIT
tmp=$(cd "$d" && pwd -P) || fail "cannot resolve the temp dir"
tar -xzf "$archive" -C "$tmp" || fail "cannot extract $file"

members=$(cd "$tmp" && find . -mindepth 1 | LC_ALL=C sort | tr '\n' ' ')
expected=$(
	{
		printf '%s\n' "./$name" "./$name/stw" "./$name/LICENSE" "./$name/README.md" "./$name/agent-guide.md" "./$name/llms.txt"
		find third_party | sed "s|^|./$name/|"
	} | LC_ALL=C sort | tr '\n' ' '
)
[ "$members" = "$expected" ] || fail "unexpected archive members: $members"
bin=$tmp/$name/stw
[ -x "$bin" ] || fail "stw is not executable"

stw() { "$bin" "$@"; }

got=$(stw version) || fail "stw version failed"
[ "$got" = "SourceSteward $version" ] || fail "stw version printed '$got', want 'SourceSteward $version'"
json=$(stw version --json | tr -d ' ') || fail "stw version --json failed"
case $json in
*"\"version\":\"$version\""*) ;;
*) fail "stw version --json printed '$json', want version $version" ;;
esac
stw --help | grep -q 'Usage:' || fail "stw --help printed no usage"

# A tiny repository: identity only through the environment, nothing written to any git config.
repo=$tmp/fixture
mkdir -p "$repo"
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null GIT_CONFIG_NOSYSTEM=1
export GIT_AUTHOR_NAME=Smoke GIT_AUTHOR_EMAIL=smoke@example.test GIT_COMMITTER_NAME=Smoke GIT_COMMITTER_EMAIL=smoke@example.test
git -C "$repo" init -q -b main || fail "git init failed"
printf 'smoke\n' >"$repo/a.txt"
git -C "$repo" add a.txt
git -C "$repo" commit -q -m 'chore: fixture' || fail "git commit failed"
git -C "$repo" branch topic
printf 'untracked\n' >"$repo/b.txt"
refs_before=$(git -C "$repo" for-each-ref) || fail "cannot list refs"
tree_before=$(git -C "$repo" status --porcelain=v1) || fail "cannot read status"

stw inspect "$repo" --json | tr -d ' \n' | grep -q '"status":"ok".*"backend":"git"' || fail "inspect --json did not report an ok git repository"
stw status --offline "$repo" | grep -q '1 repository' || fail "status did not report one repository"
stw status --offline "$repo" --json | tr -d ' \n' | grep -q '"total":1' || fail "status --json did not total one repository"

[ "$(git -C "$repo" for-each-ref)" = "$refs_before" ] || fail "a read-only command changed the references"
[ "$(git -C "$repo" status --porcelain=v1)" = "$tree_before" ] || fail "a read-only command changed the working tree"
printf 'release-smoke: %s ok\n' "$file"
