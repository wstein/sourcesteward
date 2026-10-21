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

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/wstein/sourcesteward/internal/hosting"
)

func validateSchema2(t *testing.T, name string, raw []byte) {
	t.Helper()
	id := "https://wstein.github.io/sourcesteward/schemas/v2/" + name
	file, err := os.ReadFile(filepath.Join("..", "..", "schemas", "v2", name))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	if err = c.AddResource(id, doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile(id)
	if err != nil {
		t.Fatal(err)
	}
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Validate(value); err != nil {
		t.Fatalf("printed JSON violates %s: %s\n%v", name, raw, err)
	}
}

// Real native discovery and the real operations in both entry points; only the
// HTTP response is a fixture. The token must never be printed.
func TestHostingStatusAndLookupEntryPoints(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GH_CONFIG_DIR", filepath.Join(home, "gh"))
	t.Setenv("XDG_CONFIG_HOME", home)
	for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN"} {
		t.Setenv(name, "")
	}
	t.Setenv("JJ_CONFIG", "")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_TEMPLATE_DIR", t.TempDir())
	fake := filepath.Join(t.TempDir(), "gh")
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
	type target struct {
		name, path string
		extension  bool
	}
	targets := []target{{"stw-git", gitRepo, false}}
	if _, err := exec.LookPath("jj"); err == nil {
		jjRepo := filepath.Join(t.TempDir(), "jj")
		run("jj", "git", "init", "--no-colocate", jjRepo)
		run("jj", "--repository", jjRepo, "git", "remote", "add", "custom", "https://github.com/example/project.git")
		targets = append(targets, target{"stw-jj", jjRepo, false}, target{"gh-jj", jjRepo, true})
	} else if os.Getenv("STW_REQUIRE_JJ") == "1" {
		t.Fatal("mandatory jj unavailable")
	}
	head, result := strings.Repeat("a", 40), strings.Repeat("d", 40)
	prBody := func(merged bool) string {
		extra := `"merged":false,"merged_at":null,"closed_at":null,"merge_commit_sha":"` + strings.Repeat("c", 40) + `"`
		state := "open"
		if merged {
			state = "closed"
			extra = `"merged":true,"merged_at":"2026-10-18T12:00:00Z","closed_at":"2026-10-18T12:00:00Z","merge_commit_sha":"` + result + `"`
		}
		return `{"number":42,"title":"Fixture\u001b[31m","html_url":"https://github.com/example/project/pull/42","state":"` + state + `","draft":false,` + extra + `,"head":{"sha":"` + head + `","ref":"task","repo":{"full_name":"example/fork"}},"base":{"sha":"` + strings.Repeat("b", 40) + `","ref":"trunk","repo":{"full_name":"example/project"}}}`
	}
	listBody := func() string {
		return "[" + strings.Replace(strings.Replace(prBody(true), `"merged":true,`, "", 1), "}}}", "}}}", 1) + "]"
	}
	var requests []string
	status := 200
	hostingTransport = hostingHTTPFixture(func(r *http.Request) (*http.Response, error) {
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		if r.Method != "GET" || r.URL.Host != "api.github.com" || r.Header.Get("Authorization") != "token fixture-token" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL)
		}
		body := prBody(strings.Contains(r.URL.Path, "merged"))
		if strings.Contains(r.URL.Path, "/commits/") {
			body = listBody()
		}
		if status != 200 {
			body = `{"message":"fixture-token must never be shown"}`
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	t.Cleanup(func() { hostingTransport = nil })
	execute := func(tc target, args ...string) (int, string) {
		requests = nil
		full := args
		if !tc.extension {
			full = append([]string{"hosting"}, args...)
		}
		var out, diag bytes.Buffer
		code := runCLIWithHosting(context.Background(), "test", full, &out, &diag, tc.extension, func(ctx context.Context, p string, o hosting.Options) hosting.Result {
			return hosting.InspectWithTransport(ctx, p, o, hostingTransport)
		})
		if strings.Contains(out.String()+diag.String(), "fixture-token") {
			t.Fatal("credential or error body leaked")
		}
		return code, out.String()
	}
	for _, tc := range targets {
		t.Run(tc.name, func(t *testing.T) {
			status = 200
			code, out := execute(tc, "pr", "42", tc.path, "--json", "--json-version", "2", "--expect-head", head)
			if code != 0 || len(requests) != 1 || requests[0] != "GET /repos/example/project/pulls/42" {
				t.Fatalf("exit%d %v %s", code, requests, out)
			}
			validateSchema2(t, "hosting-pr.schema.json", []byte(out))
			var r struct {
				Observation struct{ Expectation string }
				PullRequest struct{ Lifecycle string } `json:"pull_request"`
			}
			if json.Unmarshal([]byte(out), &r) != nil || r.Observation.Expectation != "matched" || r.PullRequest.Lifecycle != "open" {
				t.Fatal(out)
			}
			// A mismatch is exit 1 with a schema-valid error observation.
			code, out = execute(tc, "pr", "42", tc.path, "--json", "--json-version", "2", "--expect-head", strings.Repeat("e", 40))
			if code != 1 || len(requests) != 1 || !strings.Contains(out, `"expectation-mismatch"`) {
				t.Fatalf("exit%d %s", code, out)
			}
			validateSchema2(t, "hosting-pr.schema.json", []byte(out))
			// Compact human output: one line, no raw control bytes.
			code, out = execute(tc, "pr", "42", tc.path, "--compact")
			if code != 0 || strings.Count(out, "\n") != 1 || strings.ContainsRune(out, 0x1b) {
				t.Fatalf("exit%d %q", code, out)
			}
			// Rate limit or denial: one request, no retry, exit 1, valid error JSON.
			for _, st := range []int{403, 429} {
				status = st
				code, out = execute(tc, "pr", "42", tc.path, "--json", "--json-version", "2")
				if code != 1 || len(requests) != 1 {
					t.Fatalf("status %d: exit%d requests %v", st, code, requests)
				}
				validateSchema2(t, "hosting-pr.schema.json", []byte(out))
				code, out = execute(tc, "prs", head, tc.path, "--json")
				if code != 1 || len(requests) != 1 {
					t.Fatalf("lookup status %d: exit%d requests %v", st, code, requests)
				}
				validateSchema2(t, "hosting-prs.schema.json", []byte(out))
			}
			status = 200
			if code, _ = execute(tc, "pr", "42", tc.path, "--watch"); code != 2 || len(requests) != 0 {
				t.Fatalf("--watch must be a usage error: exit%d %v", code, requests)
			}
			// Lookup: one GET of the exact route.
			code, out = execute(tc, "prs", head, tc.path, "--json")
			if code != 0 || len(requests) != 1 || requests[0] != "GET /repos/example/project/commits/"+head+"/pulls?per_page=30&page=1" {
				t.Fatalf("exit%d %v %s", code, requests, out)
			}
			validateSchema2(t, "hosting-prs.schema.json", []byte(out))
			// Legacy JSON stays schema 1.
			code, out = execute(tc, "pr", "42", tc.path, "--json")
			if code != 0 || !strings.Contains(out, `"schema_version":1`) {
				t.Fatalf("exit%d %s", code, out)
			}
		})
	}
	// The extension still refuses Git ownership before any HTTP attempt.
	requests = nil
	code, _ := execute(target{"x", gitRepo, true}, "prs", head, gitRepo, "--json")
	if code != 1 || len(requests) != 0 {
		t.Fatalf("exit%d %v", code, requests)
	}
}
