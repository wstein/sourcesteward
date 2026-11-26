#!/bin/sh
# SPDX-License-Identifier: EUPL-1.2
set -eu

fail() {
    printf 'stw installer: %s\n' "$*" >&2
    exit 1
}

case "$(uname -s)" in
    Darwin) os=darwin ;;
    Linux) os=linux ;;
    *) fail "supported platforms are macOS and Linux" ;;
esac
case "$(uname -m)" in
    arm64|aarch64) arch=arm64 ;;
    x86_64) arch=x86_64 ;;
    *) fail "supported architectures are ARM64 and x86-64" ;;
esac

[ "$#" -le 1 ] || fail "usage: sh install.sh [vMAJOR.MINOR.PATCH]"
tag=${1:-}
if [ -n "$tag" ]; then
    printf '%s\n' "$tag" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$' ||
        fail "expected a release tag such as v0.5.0"
    manifest_path=download/$tag
else
    manifest_path=latest/download
fi

tmp=$(mktemp -d)
staging=
cleanup() {
    rm -rf -- "$tmp"
    if [ -n "$staging" ]; then rm -f -- "$staging"; fi
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
base=https://github.com/wstein/sourcesteward/releases
download() {
    curl -fsSL --proto '=https' --proto-redir '=https' --retry 3 -o "$2" "$1"
}

download "$base/$manifest_path/SHA256SUMS" "$tmp/SHA256SUMS"
asset=$(awk -v suffix="_${os}_${arch}.tar.gz" '
    NF == 2 && length($1) == 64 && $1 !~ /[^0-9a-f]/ &&
    $2 ~ /^stw_v[0-9]+\.[0-9]+\.[0-9]+_(darwin|linux)_(arm64|x86_64)\.tar\.gz$/ &&
    substr($2, length($2) - length(suffix) + 1) == suffix {
        print $2; count++
    }
    END { if (count != 1) exit 1 }
' "$tmp/SHA256SUMS") || fail "release does not list exactly one archive for $os/$arch"
asset_tag=${asset#stw_}
asset_tag=${asset_tag%%_*}
[ -z "$tag" ] || [ "$tag" = "$asset_tag" ] || fail "manifest version does not match $tag"

download "$base/download/$asset_tag/$asset" "$tmp/$asset"
expected=$(awk -v file="$asset" '$2 == file { print $1; exit }' "$tmp/SHA256SUMS")
if command -v sha256sum >/dev/null 2>&1; then
    actual=$(sha256sum "$tmp/$asset")
else
    actual=$(shasum -a 256 "$tmp/$asset")
fi
[ "${actual%% *}" = "$expected" ] || fail "SHA-256 mismatch for $asset"
tar -xOzf "$tmp/$asset" stw > "$tmp/stw"

destination=${STW_INSTALL_DIR:-"$HOME/.local/bin"}
mkdir -p -- "$destination"
[ ! -d "$destination/stw" ] || fail "$destination/stw is a directory"
staging=$(mktemp "$destination/.stw.XXXXXX")
install -m 755 "$tmp/stw" "$staging"
mv -f -- "$staging" "$destination/stw"
printf 'Installed stw %s to %s/stw\n' "${asset_tag#v}" "$destination"
