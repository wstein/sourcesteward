// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/hosting"
)

type hostingHTTPFixture func(*http.Request) (*http.Response, error)

func (f hostingHTTPFixture) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Both entry points run real native discovery and the SDK operation. Only the
// remote HTTP response is replaced; option parsing, rendering and exits are real.
func TestHostingSDKEntryPoints(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GH_CONFIG_DIR", filepath.Join(home, "gh"))
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("JJ_CONFIG", "")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_TEMPLATE_DIR", t.TempDir())
	tools := t.TempDir()
	fake := filepath.Join(tools, "gh")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\ncase \"$*\" in\n --version) echo 'gh version 2.102.0';;\n 'auth token --hostname github.com') echo fixture-token;;\n *) exit 1;;\nesac\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GH_PATH", fake)
	run := func(tool string, args ...string) {
		t.Helper()
		if out, e := exec.Command(tool, args...).CombinedOutput(); e != nil {
			t.Fatalf("fixture %s: %v %s", tool, e, out)
		}
	}
	gitRepo := filepath.Join(t.TempDir(), "git")
	run("git", "init", "-q", "-b", "main", gitRepo)
	run("git", "-C", gitRepo, "remote", "add", "custom", "https://github.com/example/project.git")
	cases := []struct {
		name, path string
		extension  bool
	}{{"stw-git", gitRepo, false}}
	if _, err := exec.LookPath("jj"); err == nil {
		jjRepo := filepath.Join(t.TempDir(), "jj")
		run("jj", "git", "init", "--no-colocate", jjRepo)
		run("jj", "--repository", jjRepo, "git", "remote", "add", "custom", "https://github.com/example/project.git")
		cases = append(cases, struct {
			name, path string
			extension  bool
		}{"stw-jj", jjRepo, false}, struct {
			name, path string
			extension  bool
		}{"gh-jj", jjRepo, true})
	} else if os.Getenv("STW_REQUIRE_JJ") == "1" {
		t.Fatal("mandatory jj unavailable")
	}
	const pr = `{"number":42,"title":"Fixture","html_url":"https://github.com/example/project/pull/42","state":"open","draft":false,"head":{"sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","ref":"task","repo":{"full_name":"example/fork"}},"base":{"sha":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","ref":"trunk","repo":{"full_name":"example/project"}}}`
	for _, tc := range cases {
		for _, kind := range []string{"pr", "ci"} {
			for _, fail := range []bool{false, true} {
				for _, human := range []bool{false, true} {
					t.Run(tc.name+"/"+kind+map[bool]string{false: "/ok", true: "/error"}[fail]+map[bool]string{false: "/json", true: "/human"}[human], func(t *testing.T) {
						calls := 0
						rt := hostingHTTPFixture(func(r *http.Request) (*http.Response, error) {
							calls++
							if r.Method != "GET" || r.URL.Host != "api.github.com" || r.Header.Get("Authorization") != "token fixture-token" {
								t.Fatalf("unexpected request: %s %s", r.Method, r.URL)
							}
							body := pr
							status := 200
							if strings.Contains(r.URL.Path, "check-runs") {
								body = `{"total_count":0,"check_runs":[]}`
							} else if strings.Contains(r.URL.Path, "statuses") {
								body = `[]`
							}
							if fail {
								status = 403
								body = `{"message":"fixture-token must never be shown"}`
							}
							return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
						})
						operation := func(ctx context.Context, p string, o hosting.Options) hosting.Result {
							return hosting.InspectWithTransport(ctx, p, o, rt)
						}
						args := []string{kind, "42", tc.path}
						if !tc.extension {
							args = append([]string{"hosting"}, args...)
						}
						if !human {
							args = append(args, "--json")
						}
						var out, diag bytes.Buffer
						code := runCLIWithHosting(context.Background(), "test", args, &out, &diag, tc.extension, operation)
						wantCode := 0
						if fail {
							wantCode = 1
						}
						if code != wantCode || calls == 0 {
							t.Fatalf("exit%d calls%d: %s %s", code, calls, &out, &diag)
						}
						if strings.Contains(out.String()+diag.String(), "fixture-token") {
							t.Fatal("credential/error body leaked")
						}
						if human {
							if !strings.Contains(out.String(), "Hosting:") {
								t.Fatal(out.String())
							}
							return
						}
						var result hosting.Result
						if err := json.Unmarshal(out.Bytes(), &result); err != nil {
							t.Fatal(err)
						}
						if result.SchemaVersion != 1 || result.Kind != kind || result.Boundary.Complete == fail || !result.Boundary.Network || result.Identity == nil {
							t.Fatalf("incompatible result: %#v", result)
						}
						wantBackend := "jj"
						if tc.name == "stw-git" {
							wantBackend = "git"
						}
						if result.Backend != wantBackend {
							t.Fatal(result.Backend)
						}
					})
				}
			}
		}
	}
	// The extension still refuses Git ownership before any HTTP attempt.
	calls := 0
	op := func(ctx context.Context, p string, o hosting.Options) hosting.Result {
		return hosting.InspectWithTransport(ctx, p, o, hostingHTTPFixture(func(*http.Request) (*http.Response, error) {
			calls++
			t.Fatal("Git extension performed HTTP")
			return nil, nil
		}))
	}
	var out, diag bytes.Buffer
	if code := runCLIWithHosting(context.Background(), "test", []string{"pr", "42", gitRepo, "--json"}, &out, &diag, true, op); code != 1 || calls != 0 {
		t.Fatalf("exit%d calls%d", code, calls)
	}
}
