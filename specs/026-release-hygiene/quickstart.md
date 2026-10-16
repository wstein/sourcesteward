# Quickstart: Release hygiene

`release.py` needs Python 3.12 or newer. The Mac mini steps below use plain shell commands, not the script.

Dry-run steps change nothing outside the Go build cache. Steps marked **needs approval** build or publish and must not be run without the user's explicit authorization (AGENTS.md).

Unset `STW_RELEASE_VERSION` in normal shells; only step 2 sets it.

## 0. Order of a release (each marked step needs the named approval)

1. `plan --commit <sha>`; 2. **A1** authorization, recorded in a copy of `docs/releases/TEMPLATE-readiness.md` at the default record path on branch `codex/release-<version>` (rooted at the code commit); 3. `build`, a second `build` into another `--out` for G12, `verify`; 4. fill the record and notes and commit them on that branch (routine documentation push rules; a push never authorizes A2 or A3); 5. `plan --final`; 6. **A2**: tag at the code commit and push it; 7. **A3**: publish with `gh release create --verify-tag`; 8. verify the downloaded files by hand (steps 4 and 5 below), record the post-release verification, A3 and A4 in a second record commit and merge the branch; 9. **A4** per consumer.

## 1. Check readiness without building

```sh
make check
python3 scripts/release.py plan --version v0.1.0 --commit "$(git rev-parse HEAD)"   # --commit is required until a record exists (commit-required, exit 2)
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
python3 scripts/release.py build --version v0.1.0 --commit <code-commit> --record docs/releases/v0.1.0-readiness.md --platforms linux/amd64,linux/arm64,darwin/arm64
# record the printed SHA-256 of SHA256SUMS as the trust anchor, then:
python3 scripts/release.py verify dist/release/v0.1.0 --anchor <sha256>
```

For G12 run a second `build --out dist/release/v0.1.0-second` (another export and an empty `GOCACHE`; also a build under A1), ideally on a second host, compare the `binary_sha256` values of the two manifests and record whether both builds ran on one host.

## 4. Verify downloaded files on the Mac mini (darwin arm64, manual evidence)

Use a new empty directory and a temporary repository only.

```sh
work=$(mktemp -d) && cd "$work"
# copy or download the three release files for darwin/arm64 here
# first compare the SHA-256 of SHA256SUMS with the anchor in the readiness record
shasum -a 256 SHA256SUMS
shasum -a 256 -c SHA256SUMS --ignore-missing   # --ignore-missing is unverified on macOS; otherwise compare by eye
tar -xzf stw_v0.1.0_darwin_arm64.tar.gz
./stw version; ./stw version --json
./stw --help | head -20
git init -q repo && git -C repo -c user.name=t -c user.email=t@example.invalid commit -q --allow-empty -m one
./stw inspect repo --json
```

Record: the output of `uname -sm` and `sw_vers`, whether macOS quarantined or blocked the file (and the command used to run it), the version line, and any jj result for S5 (`jj-sandbox-unavailable` is a valid, documented outcome). Note that `stw --version` does not exist; `stw version` is the interface.

## 5. Linux arm64 (same steps on a linux/arm64 host or a hosted arm64 runner)

Same commands as step 4 with `sha256sum -c SHA256SUMS --ignore-missing` and the `linux_arm64` archive.

## 6. Final check

```sh
python3 scripts/release.py plan --version v0.1.0 --final      # before the tag, on the release branch
```

The record commit (`HEAD`) must descend from the code commit and change only `docs/releases/`. Post-release verification is recorded by hand.

## 7. Record

Fill `docs/releases/v0.1.0-readiness.md`: host, versions, outcomes, and everything not run. Evidence from a local host is `local-recorded` or `manual`, never `ci`.
