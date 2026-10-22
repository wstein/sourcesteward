#!/bin/sh
# SPDX-License-Identifier: EUPL-1.2
#
# Package one release binary (specs/034-bootstrap-release).
#
# Usage: SOURCE_DATE_EPOCH=N sh release-package.sh VERSION TARGET BINARY OUTDIR
#
# TARGET is the archive platform, <os>_<arch>, for the shipped operating systems linux and
# darwin only (stw drives native Git/jj and a native sandbox; see the plan). Run in the repository
# root (LICENSE, README.md, llms.txt, docs/agent-guide.md and third_party/ are read from there).
# Writes OUTDIR/stw_<version without v>_<target>.tar.gz and its .sha256 sidecar. The archive
# holds one top directory with stw (0755), LICENSE, README.md, llms.txt, agent-guide.md and
# the third_party/ notices (files 0644, directories 0755), in sorted order, owner 0, mtime
# SOURCE_DATE_EPOCH, no gzip name or time, so the same inputs always give the same bytes.
# Needs GNU tar.

set -eu

fail() {
	printf 'release-package: %s\n' "$*" >&2
	exit 1
}

[ "$#" -eq 4 ] || {
	printf 'usage: SOURCE_DATE_EPOCH=N sh release-package.sh VERSION TARGET BINARY OUTDIR\n' >&2
	exit 2
}
version=$1
target=$2
binary=$3
out=$4

case $version$target in
*'
'*) fail "newline in an argument" ;;
esac
printf '%s\n' "$version" | grep -Eq '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.]+)?$' ||
	fail "version must look like v1.2.3 or v1.2.3-dryrun: $version"
printf '%s\n' "$target" | grep -Eq '^(linux|darwin)_[a-z0-9]+$' || fail "bad target (want linux_<arch> or darwin_<arch>): $target"
: "${SOURCE_DATE_EPOCH:?release-package: SOURCE_DATE_EPOCH is required}"
printf '%s\n' "$SOURCE_DATE_EPOCH" | grep -Eq '^[0-9]+$' || fail "SOURCE_DATE_EPOCH must be digits"
[ -f "$binary" ] || fail "binary not found: $binary"
for f in LICENSE README.md llms.txt docs/agent-guide.md third_party/README.md; do
	[ -f "$f" ] || fail "$f not found in $(pwd)"
done

tar --version 2>/dev/null | grep -q 'GNU tar' || fail "GNU tar is required"

name=stw_${version#v}_${target}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

mkdir "$work/$name"
cp "$binary" "$work/$name/stw"
cp LICENSE README.md llms.txt "$work/$name/"
cp docs/agent-guide.md "$work/$name/agent-guide.md"
cp -R third_party "$work/$name/third_party"
find "$work/$name" -type d -exec chmod 0755 {} +
find "$work/$name" -type f -exec chmod 0644 {} +
chmod 0755 "$work/$name/stw"

mkdir -p "$out"
out=$(cd "$out" && pwd)
archive=$out/$name.tar.gz
rm -f "$archive"
tar --sort=name --owner=0 --group=0 --numeric-owner --mtime="@$SOURCE_DATE_EPOCH" \
	--format=ustar -C "$work" -cf - "$name" | gzip -n -9 >"$archive"
(cd "$out" && sha256sum "$name.tar.gz" >"$name.tar.gz.sha256")
printf '%s\n' "$archive"
