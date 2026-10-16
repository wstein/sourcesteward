# Quickstart: Release hygiene

Dry-run steps change nothing outside the Go build cache. Steps marked **needs approval** build or publish and must not be run without the user's explicit authorization (AGENTS.md).

## 1. Check readiness without building

```sh
make check
python3 scripts/release.py plan --version v0.1.0
python3 scripts/release.py plan --version v0.1.0 --json | python3 -m json.tool
```

Expected: every gate is listed; a gate that cannot be evaluated without a binary is `not-run` with the reason; the last lines say that no distributable binary was linked. Until WI3 completes, G07 fails with `notices-incomplete` for Cobra and pflag.

## 2. Stamp test (routine `go test`)

```sh
STW_RELEASE_VERSION=v0.1.0 go test -count=1 -run TestReleaseStamp \
  -ldflags "-X github.com/wstein/sourcesteward/internal/cli.Version=v0.1.0" ./internal/cli
STW_RELEASE_VERSION=v0.1.0 go test -count=1 -run TestReleaseStamp ./internal/cli; echo "status $?"
```

Expected: the first passes, the second fails with `stamp-missing`.

## 3. Build and double build (needs approval A1)

```sh
go mod download            # network; separate and visible
python3 scripts/release.py build --version v0.1.0 --record docs/releases/v0.1.0-readiness.md --platforms linux/amd64,linux/arm64,darwin/arm64
python3 scripts/release.py verify dist/release/v0.1.0
```

For G12 repeat the build from a second clone in another directory and compare `binary_sha256` values in the two manifests.

## 4. Verify downloaded files on the Mac mini (darwin arm64, manual evidence)

Use a new empty directory and a temporary repository only.

```sh
work=$(mktemp -d) && cd "$work"
# copy or download the three release files for darwin/arm64 here
shasum -a 256 -c SHA256SUMS --ignore-missing
tar -xzf stw_v0.1.0_darwin_arm64.tar.gz
./stw version; ./stw version --json
./stw --help | head -20
git init -q repo && git -C repo commit -q --allow-empty -m one
./stw inspect repo --json
```

Record: the output of `uname -sm` and `sw_vers`, whether macOS quarantined or blocked the file (and the command used to run it), the version line, and any jj result for S5 (`jj-sandbox-unavailable` is a valid, documented outcome). Note that `stw --version` does not exist; `stw version` is the interface.

## 5. Linux arm64 (same steps on a linux/arm64 host or a hosted arm64 runner)

Same commands as step 4 with `sha256sum -c SHA256SUMS --ignore-missing` and the `linux_arm64` archive.

## 6. Record

Fill `docs/releases/v0.1.0-readiness.md`: host, versions, outcomes, and everything not run. Evidence from a local host is `local-recorded` or `manual`, never `ci`.
