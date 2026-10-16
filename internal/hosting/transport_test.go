// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"io"
	"net/http"
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
[ "$1 $2 $3 $4" = "auth token --hostname github.com" ] || exit 9
echo testsecret
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
	api, cleanup, err := newGHWithTransport(context.Background(), target, "github.com", roundTripFunc(func(r *http.Request) (*http.Response, error) { return response(200, `{"ok":true}`), nil }))
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
	_, _, err = newGH(context.Background(), target, "github.com")
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
	_, cleanup, e := newGH(context.Background(), t.TempDir(), "github.com")
	if e == nil {
		cleanup()
		t.Fatal("oversized auth output accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, e = newGH(ctx, t.TempDir(), "github.com"); e == nil {
		t.Fatal("canceled read accepted")
	}
}
func TestGHTransportIgnoresTargetTemporaryDirectoryAndSuppressesNotifiers(t *testing.T) {
	d := t.TempDir()
	exe := filepath.Join(d, "gh")
	body := `#!/bin/sh
[ "$GH_NO_UPDATE_NOTIFIER" = "1" ] || exit 7
[ "$GH_NO_EXTENSION_UPDATE_NOTIFIER" = "1" ] || exit 8
[ "$GH_TELEMETRY" = "0" ] || exit 9
[ -z "$DEBUG" ] || exit 10
if [ "$1" = "--version" ];then echo 'gh version 2.102.0';else echo fixturetoken;fi
`
	os.WriteFile(exe, []byte(body), 0700)
	t.Setenv("GH_PATH", exe)
	target := t.TempDir()
	t.Setenv("TMPDIR", target)
	t.Setenv("DEBUG", "api")
	before, _ := os.ReadDir(target)
	api, cleanup, e := newGH(context.Background(), target, "github.com")
	if e != nil {
		t.Fatal(e)
	}
	defer cleanup()
	after, _ := os.ReadDir(target)
	if len(before) != len(after) || inside(api.dir, target) {
		t.Fatal("temporary execution wrote target")
	}
}
func TestGHTransportRejectsRepositoryAuthConfiguration(t *testing.T) {
	d := t.TempDir()
	exe := filepath.Join(d, "gh")
	os.WriteFile(exe, []byte("#!/bin/sh\necho 'gh version 2.102.0'\n"), 0700)
	t.Setenv("GH_PATH", exe)
	target := t.TempDir()
	t.Setenv("GH_CONFIG_DIR", target)
	if _, cleanup, e := newGH(context.Background(), target, "github.com"); e == nil {
		cleanup()
		t.Fatal("repository configuration accepted")
	}
}
func TestGHTransportRejectsLinkedCredentialFilesBeforeExecution(t *testing.T) {
	for _, which := range []string{"default-root", "xdg-root", "hosts.yml", "config.yml", "missing-parent"} {
		t.Run(which, func(t *testing.T) {
			d := t.TempDir()
			target := filepath.Join(d, "target")
			os.Mkdir(target, 0700)
			home := filepath.Join(d, "home")
			os.Mkdir(home, 0700)
			marker := filepath.Join(d, "executed")
			exe := filepath.Join(d, "gh")
			os.WriteFile(exe, []byte("#!/bin/sh\ntouch '"+marker+"'\necho 'gh version 2.102.0'\n"), 0700)
			t.Setenv("GH_PATH", exe)
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", "")
			t.Setenv("GH_CONFIG_DIR", "")
			switch which {
			case "default-root":
				os.Mkdir(filepath.Join(home, ".config"), 0700)
				os.Symlink(target, filepath.Join(home, ".config", "gh"))
			case "xdg-root":
				t.Setenv("XDG_CONFIG_HOME", home)
				os.Symlink(target, filepath.Join(home, "gh"))
			case "hosts.yml", "config.yml":
				t.Setenv("GH_CONFIG_DIR", home)
				os.Symlink(filepath.Join(target, "secret"), filepath.Join(home, which))
			case "missing-parent":
				os.Symlink(target, filepath.Join(home, "linked"))
				t.Setenv("GH_CONFIG_DIR", filepath.Join(home, "linked", "not-created"))
			}
			if _, cleanup, e := newGH(context.Background(), target, "github.com"); e == nil {
				cleanup()
				t.Fatal("linked config accepted")
			}
			if _, e := os.Stat(marker); !os.IsNotExist(e) {
				t.Fatal("gh ran before config refusal")
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}
func TestSDKResponseBoundsAndRedaction(t *testing.T) {
	for _, tc := range []struct {
		name, body, encoding string
		code                 int
	}{
		{"raw", strings.Repeat("x", maxBytes+1), "", 200},
		{"normalized", strings.Repeat("\x00", maxBytes/2+1), "", 200},
		{"error", strings.Repeat("secret", maxBytes), "", 403},
		{"compressed", "secret", "gzip", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client, err := newSDKClient("github.com", "fixturetoken", t.TempDir(), roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				res := response(tc.code, tc.body)
				if tc.encoding != "" {
					res.Header.Set("Content-Encoding", tc.encoding)
				}
				return res, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			api := &ghAPI{client: client, host: "github.com"}
			_, err = api.get(context.Background(), "repos/a/b/pulls/1")
			if err == nil || strings.Contains(err.Error(), "secret") || calls != 1 {
				t.Fatalf("unbounded or unredacted %v calls%d", err, calls)
			}
		})
	}
}

func TestSDKDestinationsAndFixedReadScope(t *testing.T) {
	for _, tc := range []struct{ host, want string }{
		{"github.com", "https://api.github.com/"}, {"company.ghe.com", "https://api.company.ghe.com/"}, {"github.company.test", "https://github.company.test/api/v3/"},
	} {
		t.Run(tc.host, func(t *testing.T) {
			calls := 0
			client, e := newSDKClient(tc.host, "fixturetoken", t.TempDir(), roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.String() != tc.want+"repos/a/b/pulls/1" || r.Method != "GET" || r.Header.Get("Authorization") != "token fixturetoken" || r.Header.Get("Accept") != "application/vnd.github+json" || r.Header.Get("X-GitHub-Api-Version") != "2022-11-28" {
					t.Fatalf("incorrect request %s %s", r.Method, r.URL)
				}
				return response(200, `{"ok":true}`), nil
			}))
			if e != nil {
				t.Fatal(e)
			}
			api := &ghAPI{client: client, host: tc.host}
			for i := 0; i < 2; i++ {
				if _, e = api.get(context.Background(), "repos/a/b/pulls/1"); e != nil {
					t.Fatal(e)
				}
			}
			if calls != 2 {
				t.Fatal("unexpected caching")
			}
			for _, bad := range []string{"https://evil.test/repos/a/b/pulls/1", "repos/a/b/pulls/1?extra=1", "repos/a/b/pulls/0", "repos/a/b/../../pulls/1", "repos/a/b/commits/" + strings.Repeat("a", 40) + "/statuses?per_page=100&page=11"} {
				if _, e = api.get(context.Background(), bad); e == nil {
					t.Fatalf("accepted %q", bad)
				}
			}
			for _, bad := range []struct{ method, url string }{{"POST", tc.want + "repos/a/b/pulls/1"}, {"GET", "https://evil.test/repos/a/b/pulls/1"}, {"GET", strings.Replace(tc.want, "https:", "http:", 1) + "repos/a/b/pulls/1"}, {"GET", tc.want + "repos/a/b/pulls/1#fragment"}, {"GET", tc.want + "repos/a/b/pulls/1?extra=1"}} {
				req, _ := http.NewRequest(bad.method, bad.url, nil)
				if _, e = client.Do(req); e == nil {
					t.Fatalf("request scope accepted %s", bad.url)
				}
			}
			if calls != 2 {
				t.Fatal("rejected requests reached transport")
			}
		})
	}
}
func TestSDKRedirectAndCancellation(t *testing.T) {
	calls := 0
	client, e := newSDKClient("github.com", "fixturetoken", t.TempDir(), roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		res := response(302, "secret")
		res.Header.Set("Location", "https://evil.test/secret")
		return res, nil
	}))
	if e != nil {
		t.Fatal(e)
	}
	api := &ghAPI{client: client, host: "github.com"}
	if _, e = api.get(context.Background(), "repos/a/b/pulls/1"); e == nil || strings.Contains(e.Error(), "secret") || calls != 1 {
		t.Fatalf("redirect %v %d", e, calls)
	}
	client, e = newSDKClient("github.com", "fixturetoken", t.TempDir(), roundTripFunc(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() }))
	if e != nil {
		t.Fatal(e)
	}
	api.client = client
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = api.get(ctx, "repos/a/b/pulls/1"); e == nil {
		t.Fatal("cancellation lost")
	}
}
func TestAuthenticationBoundedSingleLineAndRedacted(t *testing.T) {
	for _, tc := range []struct{ name, output string }{
		{"empty", ""}, {"multiline", "first\nsecret"}, {"space", "secret token"}, {"control", "secret\r"}, {"overflow", strings.Repeat("s", 16*1024+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := t.TempDir()
			out := filepath.Join(d, "output")
			os.WriteFile(out, []byte(tc.output), 0600)
			exe := filepath.Join(d, "gh")
			os.WriteFile(exe, []byte("#!/bin/sh\nif [ \"$1\" = \"--version\" ];then echo 'gh version 2.102.0';else cat '"+out+"';fi\n"), 0700)
			t.Setenv("GH_PATH", exe)
			calls := 0
			_, cleanup, e := newGHWithTransport(context.Background(), t.TempDir(), "github.com", roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return response(200, "{}"), nil }))
			defer cleanup()
			if e == nil || strings.Contains(e.Error(), "secret") || calls != 0 {
				t.Fatalf("auth failure leaked or requested: %v %d", e, calls)
			}
		})
	}
}
func TestSDKRawLimitBeforeShrinkingNormalization(t *testing.T) {
	body := strings.Repeat(`\u001b`, maxBytes/6+1)
	client, e := newSDKClient("github.com", "fixturetoken", t.TempDir(), roundTripFunc(func(*http.Request) (*http.Response, error) { return response(200, body), nil }))
	if e != nil {
		t.Fatal(e)
	}
	api := &ghAPI{client: client, host: "github.com"}
	if _, e = api.get(context.Background(), "repos/a/b/pulls/1"); e == nil {
		t.Fatal("raw cap lost when SDK shrank response")
	}
}
