// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGHTransportNeutralBoundedAndRedacted(t *testing.T) {
	d := t.TempDir()
	target := filepath.Join(d, "target")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(d, "gh")
	body := `#!/bin/sh
if [ "$1" = "--version" ];then echo 'gh version 2.102.0';exit 0;fi
[ "$PWD" != "$TARGET" ] || exit 5
[ -z "$GH_REPO$GH_HOST$GH_DEBUG$GIT_DIR" ] || exit 6
[ "$GH_PROMPT_DISABLED" = "1" ] || exit 7
[ "$GH_TOKEN" = "testsecret" ] || exit 8
[ "$1 $2 $3 $4 $5 $6" = "api --hostname github.com --method GET repos/a/b/pulls/1" ] || exit 9
echo '{"ok":true}'
`
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GH_PATH", script)
	t.Setenv("TARGET", target)
	t.Setenv("GH_REPO", "evil/x")
	t.Setenv("GH_HOST", "evil.test")
	t.Setenv("GH_DEBUG", "api")
	t.Setenv("GIT_DIR", "bad")
	t.Setenv("GH_TOKEN", "testsecret")
	api, cleanup, err := newGH(context.Background(), target, "github.com")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	out, err := api.get(context.Background(), "repos/a/b/pulls/1")
	if err != nil || strings.TrimSpace(string(out)) != "{\"ok\":true}" {
		t.Fatalf("%s %v", out, err)
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho testsecret >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	_, err = api.get(context.Background(), "repos/a/b/pulls/1")
	if err == nil || strings.Contains(err.Error(), "testsecret") {
		t.Fatalf("unredacted: %v", err)
	}
}
func TestGHTransportRejectsTargetExecutableAndUnsupportedVersion(t *testing.T) {
	d := t.TempDir()
	exe := filepath.Join(d, "gh")
	os.WriteFile(exe, []byte("#!/bin/sh\necho 'gh version 2.101.0'\n"), 0700)
	t.Setenv("GH_PATH", exe)
	if _, cleanup, err := newGH(context.Background(), d, "github.com"); err == nil {
		cleanup()
		t.Fatal("target executable accepted")
	}
	if _, cleanup, err := newGH(context.Background(), t.TempDir(), "github.com"); err == nil {
		cleanup()
		t.Fatal("old version accepted")
	}
}
func TestGHTransportBoundsAndCancellation(t *testing.T) {
	d := t.TempDir()
	exe := filepath.Join(d, "gh")
	body := "#!/bin/sh\nif [ \"$1\" = \"--version\" ];then echo 'gh version 2.102.0';exit 0;fi\nhead -c 1048577 /dev/zero\n"
	if e := os.WriteFile(exe, []byte(body), 0700); e != nil {
		t.Fatal(e)
	}
	t.Setenv("GH_PATH", exe)
	api, cleanup, e := newGH(context.Background(), t.TempDir(), "github.com")
	if e != nil {
		t.Fatal(e)
	}
	defer cleanup()
	if _, e = api.get(context.Background(), "repos/a/b/pulls/1"); e == nil {
		t.Fatal("oversized output accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = api.get(ctx, "repos/a/b/pulls/1"); e == nil {
		t.Fatal("canceled read accepted")
	}
}
