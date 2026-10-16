// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/wstein/sourcesteward/internal/cli"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
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
	tools := t.TempDir()
	fake := filepath.Join(tools, "gh")
	// Independent fixed transport fixture: no authentication, real network or writes.
	script := `#!/bin/sh
case "$*" in
 --version) printf '%s\n' 'gh version 2.102.0' ;;
 *'/pulls/42'*) printf '%s\n' '{"number":42,"title":"Fixture","html_url":"https://github.com/example/project/pull/42","state":"open","draft":false,"head":{"sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","ref":"task","repo":{"full_name":"example/fork"}},"base":{"sha":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","ref":"trunk","repo":{"full_name":"example/project"}}}' ;;
 *'/check-runs?'*) printf '%s\n' '{"total_count":0,"check_runs":[]}' ;;
 *'/statuses?'*) printf '%s\n' '[]' ;;
 *) exit 1 ;;
esac
`
	if err := os.WriteFile(fake, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GH_TOKEN", "fixture-token-not-a-secret")
	for _, kind := range []string{"pr", "ci"} {
		var out, diag bytes.Buffer
		code := cli.Run(context.Background(), "test", []string{"hosting", kind, "42", repo, "--json"}, &out, &diag)
		if code != 0 {
			t.Fatalf("%s exit=%d: %s %s", kind, code, &out, &diag)
		}
		valid(t, s, out.Bytes())
	}
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nprintf '%s\\n' 'not-json'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	var out, diag bytes.Buffer
	code := cli.Run(context.Background(), "test", []string{"hosting", "ci", "42", repo, "--json"}, &out, &diag)
	if code != 1 {
		t.Fatalf("malformed API exit=%d: %s %s", code, &out, &diag)
	}
	valid(t, s, out.Bytes())
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
