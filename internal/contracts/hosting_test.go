// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/cli"
	"github.com/wstein/sourcesteward/internal/hosting"
)

func hostingFixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root(t), "internal/cli/testdata/hosting-repo.json"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestHostingSchemaOfflineFixturesAndInvariants(t *testing.T) {
	s := schema(t, "hosting")
	paths, err := filepath.Glob(filepath.Join(root(t), "internal/cli/testdata/hosting*.json"))
	if err != nil || len(paths) < 4 {
		t.Fatalf("missing hosting observations: %v", err)
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		valid(t, s, raw)
	}
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"version", func(v map[string]any) { v["schema_version"] = 2 }},
		{"null-array", func(v map[string]any) { v["checks"] = nil }},
		{"incomplete-success", func(v map[string]any) { v["boundary"].(map[string]any)["complete"] = false }},
		{"missing-repository", func(v map[string]any) { v["repository"] = nil }},
		{"missing-identity", func(v map[string]any) { v["identity"] = nil }},
		{"missing-pr", func(v map[string]any) { v["kind"] = "pr"; v["boundary"].(map[string]any)["network"] = true }},
		{"error-without-diagnostic", func(v map[string]any) { v["status"] = "error"; v["boundary"].(map[string]any)["complete"] = false }},
		{"wrong-limit", func(v map[string]any) { v["boundary"].(map[string]any)["max_pages"] = 11 }},
		{"identity-backend-mismatch", func(v map[string]any) { v["backend"] = "jj" }},
		{"repo-network", func(v map[string]any) { v["boundary"].(map[string]any)["network"] = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := value(t, hostingFixture(t)).(map[string]any)
			tc.mutate(v)
			if s.Validate(v) == nil {
				t.Fatal("invalid hosting observation accepted")
			}
		})
	}
	v := value(t, hostingFixture(t)).(map[string]any)
	v["future"] = true
	raw, _ := json.Marshal(v)
	valid(t, s, raw)
}

// Validate real shared CLI envelopes against the published offline schema.
func TestRealHostingCLIOutputs(t *testing.T) {
	s := schema(t, "hosting")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_TEMPLATE_DIR", t.TempDir())
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "main", repo}, {"-C", repo, "remote", "add", "custom", "https://github.com/example/project.git"}} {
		c := exec.Command("git", args...)
		if raw, err := c.CombinedOutput(); err != nil {
			t.Fatalf("setup: %v %s", err, raw)
		}
	}
	for _, tc := range []struct {
		args []string
		code int
	}{
		{[]string{"hosting", "repo", repo, "--json"}, 0},
		{[]string{"hosting", "repo", filepath.Join(home, "missing"), "--json"}, 1},
		{[]string{"hosting", "pr", "42", filepath.Join(home, "missing"), "--json"}, 1},
		{[]string{"hosting", "ci", "42", filepath.Join(home, "missing"), "--json"}, 1},
		{[]string{"hosting", "pr", "0", repo, "--json"}, 2},
	} {
		var out, diag bytes.Buffer
		code := cli.Run(context.Background(), "test", tc.args, &out, &diag)
		if code != tc.code {
			t.Fatalf("%v exit=%d want=%d: %s %s", tc.args, code, tc.code, &out, &diag)
		}
		if code != 2 {
			valid(t, s, out.Bytes())
		} else if out.Len() != 0 {
			t.Fatal("usage emitted JSON")
		}
	}
}

func TestRealHostingPRAndCIContracts(t *testing.T) {
	s := schema(t, "hosting")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_TEMPLATE_DIR", t.TempDir())
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "main", repo}, {"-C", repo, "remote", "add", "custom", "https://github.com/example/project.git"}} {
		c := exec.Command("git", args...)
		if raw, err := c.CombinedOutput(); err != nil {
			t.Fatalf("setup: %v %s", err, raw)
		}
	}
	rt, malformed, calls := hostingHTTPFixture(t, home)
	// Observe both metadata roots: the primary Git directory and linked worktree.
	root := t.TempDir()
	linked := filepath.Join(root, "linked")
	cmd := exec.Command("git", "-C", repo, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.org", "commit", "--allow-empty", "-qm", "initial")
	if raw, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit setup: %v %s", err, raw)
	}
	cmd = exec.Command("git", "-C", repo, "worktree", "add", "-b", "task", linked)
	if raw, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("worktree setup: %v %s", err, raw)
	}
	for _, path := range []string{repo, linked} {
		before := hostingTreeSnapshot(t, repo)
		linkedBefore := hostingTreeSnapshot(t, root)
		for _, kind := range []string{"pr", "ci"} {
			for _, broken := range []bool{false, true} {
				*malformed = broken
				result := hosting.InspectWithTransport(context.Background(), path, hosting.Options{Kind: kind, Number: 42}, rt)
				raw, err := json.Marshal(result)
				want := "ok"
				if broken {
					want = "error"
				}
				if err != nil || result.Status != want || result.Backend != "git" || !result.Boundary.Network || result.Boundary.Complete == broken {
					t.Fatalf("Git %s malformed=%t result=%s err=%v", kind, broken, raw, err)
				}
				valid(t, s, raw)
				if !broken {
					hostingSDKOutcome(t, result)
				}
				if !reflect.DeepEqual(before, hostingTreeSnapshot(t, repo)) || !reflect.DeepEqual(linkedBefore, hostingTreeSnapshot(t, root)) {
					t.Fatal("SDK inspection changed Git bytes, refs, metadata or modes")
				}
			}
		}
	}
	if *calls != 14 {
		t.Fatalf("expected exact successful and malformed PR/CI reads, got %d", *calls)
	}

}

func TestRealJJHostingContracts(t *testing.T) {
	if _, err := exec.LookPath("jj"); err != nil {
		if os.Getenv("STW_REQUIRE_JJ") == "1" {
			t.Fatal("mandatory jj unavailable")
		}
		t.Skip("jj unavailable outside mandatory conformance")
	}
	s := schema(t, "hosting")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("JJ_CONFIG", "")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	jj := func(args ...string) {
		t.Helper()
		c := exec.Command("jj", args...)
		if raw, err := c.CombinedOutput(); err != nil {
			t.Fatalf("jj setup: %v %s", err, raw)
		}
	}
	root := t.TempDir()
	standalone := filepath.Join(root, "standalone")
	colocated := filepath.Join(root, "colocated")
	secondary := filepath.Join(root, "secondary")
	jj("git", "init", "--no-colocate", standalone)
	jj("git", "init", "--colocate", colocated)
	for _, path := range []string{standalone, colocated} {
		jj("-R", path, "git", "remote", "add", "custom", "https://github.com/example/project.git")
	}
	jj("-R", standalone, "workspace", "add", "--name", "secondary", secondary)
	rt, malformed, calls := hostingHTTPFixture(t, home)
	for _, path := range []string{standalone, colocated, secondary} {
		if err := os.WriteFile(filepath.Join(path, "unsnapshotted.txt"), []byte("unrecorded work\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{standalone, colocated, secondary} {
		var out, diag bytes.Buffer
		code := cli.Run(context.Background(), "test", []string{"hosting", "repo", path, "--json"}, &out, &diag)
		if code != 0 {
			t.Fatalf("jj hosting exit=%d: %s %s", code, &out, &diag)
		}
		valid(t, s, out.Bytes())
		v := value(t, out.Bytes()).(map[string]any)
		if v["backend"] != "jj" {
			t.Fatalf("wrong native identity: %s", &out)
		}
		before := hostingTreeSnapshot(t, root)
		for _, kind := range []string{"pr", "ci"} {
			for _, broken := range []bool{false, true} {
				*malformed = broken
				result := hosting.InspectWithTransport(context.Background(), path, hosting.Options{Kind: kind, Number: 42, RequireJJ: true}, rt)
				raw, err := json.Marshal(result)
				want := "ok"
				if broken {
					want = "error"
				}
				if err != nil || result.Status != want || result.Backend != "jj" || !result.Boundary.Network || result.Boundary.Complete == broken {
					t.Fatalf("jj %s malformed=%t result=%s err=%v", kind, broken, raw, err)
				}
				valid(t, s, raw)
				if !broken {
					hostingSDKOutcome(t, result)
				}
				if !reflect.DeepEqual(before, hostingTreeSnapshot(t, root)) {
					t.Fatalf("SDK %s changed jj stored working copy, operation log, contents or modes", kind)
				}
			}
		}
	}
	if *calls != 21 {
		t.Fatalf("expected exact native jj SDK reads, got %d", *calls)
	}
}

func TestHostingEarlyErrorsMayHaveNoNetworkEffects(t *testing.T) {
	s := schema(t, "hosting")
	for _, kind := range []string{"pr", "ci"} {
		v := value(t, hostingFixture(t)).(map[string]any)
		v["kind"] = kind
		v["status"] = "error"
		v["boundary"].(map[string]any)["complete"] = false
		v["diagnostics"] = []any{map[string]any{"code": "hosting-native-failed", "message": "Native ownership could not be established."}}
		raw, _ := json.Marshal(v)
		valid(t, s, raw)
	}
}

func hostingSDKOutcome(t *testing.T, result hosting.Result) {
	t.Helper()
	if result.PullRequest == nil || result.PullRequest.Number != 42 || result.PullRequest.HeadSHA != strings.Repeat("a", 40) || result.PullRequest.BaseRepository != "example/project" || result.PullRequest.HeadRepository != "example/fork" {
		t.Fatalf("wrong SDK PR identity: %+v", result.PullRequest)
	}
	if result.Kind == "ci" && (len(result.Checks) != 1 || result.Checks[0].ID != 11 || result.Checks[0].Status != "queued" || len(result.Statuses) != 1 || result.Statuses[0].ID != 12 || result.Statuses[0].State != "success") {
		t.Fatalf("wrong SDK CI evidence: %+v %+v", result.Checks, result.Statuses)
	}
}

func hostingHTTPFixture(t *testing.T, home string) (http.RoundTripper, *bool, *int) {
	t.Helper()
	tools := t.TempDir()
	fake := filepath.Join(tools, "gh")
	// Independent gh authentication fixture. The SDK receives fixed HTTP data,
	// never API output from a subprocess or credentials from the user's profile.
	script := `#!/bin/sh
case "$*" in
 --version) printf '%s\n' 'gh version 2.102.0' ;;
 'auth token --hostname github.com') printf '%s\n' 'fixture-token-not-a-secret' ;;
 *) exit 1 ;;
esac
`
	if err := os.WriteFile(fake, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GH_PATH", fake)
	t.Setenv("GH_CONFIG_DIR", filepath.Join(home, "gh"))
	for _, key := range []string{"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN"} {
		t.Setenv(key, "")
	}
	malformed := new(bool)
	calls := new(int)
	rt := hostingContractTransport(func(req *http.Request) (*http.Response, error) {
		(*calls)++
		if req.Method != "GET" || req.URL.Scheme != "https" || req.URL.Host != "api.github.com" || req.Header.Get("Authorization") != "token fixture-token-not-a-secret" {
			t.Fatalf("unexpected SDK request: %s %s", req.Method, req.URL)
		}
		body := ""
		switch req.URL.Path {
		case "/repos/example/project/pulls/42":
			body = `{"number":42,"title":"Fixture","html_url":"https://github.com/example/project/pull/42","state":"open","draft":false,"head":{"sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","ref":"task","repo":{"full_name":"example/fork"}},"base":{"sha":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","ref":"trunk","repo":{"full_name":"example/project"}}}`
		case "/repos/example/project/commits/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/check-runs":
			body = `{"total_count":1,"check_runs":[{"id":11,"name":"test","status":"queued","conclusion":null,"head_sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","html_url":""}]}`
		case "/repos/example/project/commits/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/statuses":
			body = `[{"id":12,"context":"lint","state":"success","description":"Done","target_url":""}]`
		default:
			t.Fatalf("unexpected fixed endpoint: %s", req.URL)
		}
		if *malformed {
			body = "not-json"
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})
	return rt, malformed, calls
}

// Snapshot all native metadata, working-copy contents, symlinks and modes. No
// snapshot-producing native command is run as an observation sentinel.
func hostingTreeSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	state := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		content := ""
		if info.Mode()&os.ModeSymlink != 0 {
			content, err = os.Readlink(path)
		} else if info.Mode().IsRegular() {
			var data []byte
			data, err = os.ReadFile(path)
			content = string(data)
		}
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		state[rel] = fmt.Sprintf("%v:%s", info.Mode(), content)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return state
}

// hostingContractTransport injects only HTTP I/O. Native discovery, auth preflight,
// SDK construction, request guards and decoding remain production operations.
type hostingContractTransport func(*http.Request) (*http.Response, error)

func (f hostingContractTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
