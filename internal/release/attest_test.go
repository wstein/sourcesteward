// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAttestationProcessPinnedAndStrict(t *testing.T) {
	d := t.TempDir()
	exe := filepath.Join(d, "gh")
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte("fixture")))
	script := `#!/bin/sh
if [ "$1" = "--version" ];then echo 'gh version 2.102.0';exit 0;fi
[ "$1 $2" = 'attestation verify' ] || exit 11
[ "$3" = /dev/fd/3 ] || exit 18
[ "$(cat "$3")" = fixture ] || exit 19
[ "$PWD" != "$TARGET" ] || exit 12
[ -z "$(ls -A)" ] || exit 17
[ -z "$GH_DEBUG$GH_REPO$GIT_DIR" ] || exit 13
[ "$GH_PROMPT_DISABLED" = 1 ] || exit 14
[ "$4 $5 $6 $7 $8 $9" = '--repo o/r --signer-workflow o/r/.github/workflows/release.yml --source-ref refs/heads/main' ] || exit 15
shift 9
[ "$1 $2 $3" = '--deny-self-hosted-runners --format json' ] || exit 16
[ -z "$MUTATE_SUBJECT" ] || printf changed > "$ORIGINAL_SUBJECT"
printf '%s\n' '` + `[{"verificationResult":{"statement":{"subject":[{"name":"asset","digest":{"sha256":"` + hash + `"}}]}}}]` + `'
`
	if err := os.WriteFile(exe, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	asset := filepath.Join(d, "asset")
	if e := os.WriteFile(asset, []byte("fixture"), 0600); e != nil {
		t.Fatal(e)
	}
	target := t.TempDir()
	t.Setenv("GH_PATH", exe)
	t.Setenv("TARGET", target)
	t.Setenv("GH_DEBUG", "api")
	t.Setenv("GH_REPO", "evil/x")
	t.Setenv("GIT_DIR", "bad")
	if err := verifyAttestation(context.Background(), target, "o/r", "o/r/.github/workflows/release.yml", "refs/heads/main", asset, hash, ""); err != nil {
		t.Fatal(err)
	}
	if err := verifyAttestation(context.Background(), target, "o/r", "o/r/.github/workflows/release.yml", "refs/heads/main", asset, strings.Repeat("b", 64), ""); err == nil {
		t.Fatal("digest mismatch accepted")
	}
	t.Setenv("MUTATE_SUBJECT", "1")
	t.Setenv("ORIGINAL_SUBJECT", asset)
	if err := verifyAttestation(context.Background(), target, "o/r", "o/r/.github/workflows/release.yml", "refs/heads/main", asset, hash, ""); err == nil {
		t.Fatal("subject changed during verification accepted")
	}
}
func TestAttestationJSONFailsClosed(t *testing.T) {
	hash := strings.Repeat("a", 64)
	for _, raw := range []string{`null`, `[]`, `[{"verificationResult":{}}]`, `[{"verificationResult":{"statement":{"subject":[{"digest":{"sha256":"` + hash + `"},"digest":{"sha256":"` + hash + `"}}]}}}]`, `[{"verificationResult":{"statement":{"subject":[{"digest":{"sha256":"` + hash + `"}}]}}}] true`} {
		if attestationDigest([]byte(raw), hash) == nil {
			t.Fatal(raw)
		}
	}
}

func TestAttestationDescriptorRejectsLinksAndDigestMismatch(t *testing.T) {
	d := t.TempDir()
	name := filepath.Join(d, "file")
	if err := os.WriteFile(name, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte("fixture")))
	f, err := openAttestationSubject(context.Background(), name, digest)
	if err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(d, "moved")
	if err := os.Rename(name, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("unrelated", name); err != nil {
		t.Fatal(err)
	}
	raw, e := io.ReadAll(f)
	f.Close()
	if e != nil || string(raw) != "fixture" {
		t.Fatalf("%s %v", raw, e)
	}
	if f, err := openAttestationSubject(context.Background(), name, digest); err == nil {
		f.Close()
		t.Fatal("symlink accepted")
	}
	if f, err := openAttestationSubject(context.Background(), moved, strings.Repeat("b", 64)); err == nil {
		f.Close()
		t.Fatal("different digest accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if f, err := openAttestationSubject(ctx, moved, digest); err == nil {
		f.Close()
		t.Fatal("canceled read accepted")
	}
}

func TestAttestationOutputBoundAndCancellation(t *testing.T) {
	d := t.TempDir()
	asset := filepath.Join(d, "asset")
	if err := os.WriteFile(asset, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(d, "gh")
	script := "#!/bin/sh\nif [ \"$1\" = --version ];then echo 'gh version 2.102.0';exit 0;fi\nhead -c 1048577 /dev/zero\n"
	if err := os.WriteFile(exe, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GH_PATH", exe)
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte("fixture")))
	if err := verifyAttestation(context.Background(), t.TempDir(), "o/r", "o/r/.github/workflows/release.yml", "refs/heads/main", asset, digest, filepath.Join(d, "synthetic.bundle")); !errors.Is(err, errReleaseLimit) {
		t.Fatalf("unbounded output: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := verifyAttestation(ctx, t.TempDir(), "o/r", "o/r/.github/workflows/release.yml", "refs/heads/main", asset, digest, ""); err == nil {
		t.Fatal("canceled verifier accepted")
	}
}
