#!/bin/sh
# SPDX-License-Identifier: EUPL-1.2
#
# Release notes for a tag on stdout (specs/034-bootstrap-release). There is no CHANGELOG file:
# the change list is generated from the commit subjects.
#
# Usage: sh release-notes.sh TAG COMMIT GO_VERSION
#
# Run in a repository with full history and tags. The change list is the non-merge
# commit subjects between the nearest earlier strict release tag (by version order)
# and COMMIT, grouped by Conventional Commit type, or just "First release." when there is
# no earlier tag.

set -eu

[ "$#" -eq 3 ] || {
	printf 'usage: sh release-notes.sh TAG COMMIT GO_VERSION\n' >&2
	exit 2
}
tag=$1
commit=$2
gover=$3
plain=${tag#v}

prev=
if git rev-parse -q --verify "$commit^" >/dev/null; then
	prev=$(git tag --merged "$commit^" --list 'v*' --sort=-version:refname |
		grep -E '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$' | grep -vx "$tag" | head -n 1 || true)
fi

cat <<TEXT
SourceSteward $tag

Commit: $commit
Built with Go $gover, CGO disabled, -trimpath, version stamped as $tag.

Archives are named stw_${plain}_<os>_<arch>.tar.gz. Each holds stw, LICENSE, README.md,
llms.txt, agent-guide.md and the third_party/ license notices.

- Run natively on a GitHub runner before release: linux/amd64, linux/arm64, darwin/arm64,
  darwin/amd64.
- Windows, the BSDs and illumos are not shipped: stw needs Linux or macOS native sandboxing
  for jj (bubblewrap on Linux, sandbox-exec on macOS), and its release machinery does not
  build for them. Other Linux architectures compile but are not shipped.
- stw runs the native git (and jj, 0.45.1 or later, for jj repositories) found on PATH.

Verify a download in the directory that holds the archives and SHA256SUMS:

    sha256sum --check --ignore-missing SHA256SUMS                       # Linux
    grep ' ARCHIVE\$' SHA256SUMS | shasum -a 256 --check               # macOS, one archive
    gh attestation verify ARCHIVE --repo wstein/sourcesteward

then extract and run \`stw version\`, which prints \`SourceSteward $tag\`.

The binaries are not signed with a certificate or notarized. The build provenance
attestation shows that GitHub Actions built the archive from this repository;
SHA256SUMS detects a damaged or wrong download.

TEXT
if [ -z "$prev" ]; then
	printf '## Changes\n\nFirst release.\n'
	exit 0
fi
printf '## Changes since %s\n\n' "$prev"
range=$prev..$commit
subjects=$(git log --no-merges --format='%s (%h)' "$range")
if [ -z "$subjects" ]; then
	printf 'No changes.\n'
	exit 0
fi
section() { # section TITLE ERE
	lines=$(printf '%s\n' "$subjects" | grep -E "$2" || true)
	if [ -n "$lines" ]; then
		printf '### %s\n\n' "$1"
		printf '%s\n' "$lines" | sed 's/^/- /'
		printf '\n'
	fi
}
section Features '^feat(\(.+\))?!?:'
section Fixes '^fix(\(.+\))?!?:'
section Documentation '^docs(\(.+\))?!?:'
rest=$(printf '%s\n' "$subjects" | grep -Ev '^(feat|fix|docs)(\(.+\))?!?:' || true)
if [ -n "$rest" ]; then
	printf '### Other\n\n'
	printf '%s\n' "$rest" | sed 's/^/- /'
fi
