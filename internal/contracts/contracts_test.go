// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const base = "https://wstein.github.io/sourcesteward/schemas/v1/"

type denyLoader struct{}

func (denyLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("external schema loading forbidden: %s", url)
}
func root(t *testing.T) string {
	t.Helper()
	p, e := filepath.Abs("../..")
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func compiler() *jsonschema.Compiler {
	c := jsonschema.NewCompiler()
	c.UseLoader(denyLoader{})
	return c
}
func schema(t *testing.T, name string) *jsonschema.Schema {
	t.Helper()
	raw, e := os.ReadFile(filepath.Join(root(t), "schemas/v1", name+".schema.json"))
	if e != nil {
		t.Fatal(e)
	}
	doc, e := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	obj := doc.(map[string]any)
	if obj["$id"] != base+name+".schema.json" || obj["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
		t.Fatal("incorrect schema identity or dialect")
	}
	checkLocalReferences(t, doc)
	c := compiler()
	if e = c.AddResource(base+name+".schema.json", doc); e != nil {
		t.Fatal(e)
	}
	s, e := c.Compile(base + name + ".schema.json")
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func checkLocalReferences(t *testing.T, value any) {
	t.Helper()
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if key == "$ref" || key == "$dynamicRef" {
				ref, ok := child.(string)
				if !ok || !strings.HasPrefix(ref, "#") {
					t.Fatalf("nonlocal schema reference: %v", child)
				}
			}
			checkLocalReferences(t, child)
		}
	case []any:
		for _, child := range v {
			checkLocalReferences(t, child)
		}
	}
}
func value(t *testing.T, raw []byte) any {
	t.Helper()
	v, e := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func valid(t *testing.T, s *jsonschema.Schema, raw []byte) {
	t.Helper()
	if e := s.Validate(value(t, raw)); e != nil {
		t.Fatalf("invalid output: %s\n%s", raw, e)
	}
}
func TestSchemasCompileOffline(t *testing.T) { schema(t, "version"); schema(t, "inspect") }
func TestCompilerRejectsBadSchemasAndExternalReferences(t *testing.T) {
	for _, doc := range []any{map[string]any{"type": 42}, map[string]any{"$ref": "https://invalid.example/schema"}, map[string]any{"$ref": "#/$defs/missing"}} {
		c := compiler()
		if e := c.AddResource("urn:test", doc); e != nil {
			t.Fatal(e)
		}
		if _, e := c.Compile("urn:test"); e == nil {
			t.Fatalf("accepted invalid schema %v", doc)
		}
	}
}
func TestVersionContract(t *testing.T) {
	s := schema(t, "version")
	valid(t, s, []byte(`{"schema_version":1,"name":"SourceSteward","version":"dev","future":true}`))
	for _, raw := range []string{`{}`, `{"schema_version":2,"name":"SourceSteward","version":"dev"}`, `{"schema_version":1,"name":"other","version":"dev"}`, `{"schema_version":1,"name":"SourceSteward","version":""}`} {
		if s.Validate(value(t, []byte(raw))) == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}
func TestInspectionInvariants(t *testing.T) {
	s := schema(t, "inspect")
	const seed = `{"schema_version":1,"status":"ok","backend":"git","input_path":"/tmp/repo","evidence":[],"repository":{"git_dir":"/tmp/repo/.git","common_dir":"/tmp/repo/.git","worktree":"/tmp/repo","bare":false,"linked_worktree":false,"object_format":"sha1","head":{"state":"attached","ref":"refs/heads/main","oid":"1111111111111111111111111111111111111111"}},"diagnostics":[]}`
	valid(t, s, []byte(seed))
	cases := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"version", func(v map[string]any) { v["schema_version"] = 2 }},
		{"status", func(v map[string]any) { v["status"] = "maybe" }},
		{"backend", func(v map[string]any) { v["backend"] = "future" }},
		{"missing-required", func(v map[string]any) { delete(v, "diagnostics") }},
		{"null-array", func(v map[string]any) { v["evidence"] = nil }},
		{"ok-without-repository", func(v map[string]any) { v["repository"] = nil }},
		{"error-with-repository", func(v map[string]any) { v["status"] = "error" }},
		{"unsupported-git", func(v map[string]any) {
			v["status"] = "unsupported"
			v["repository"] = nil
			v["diagnostics"] = []any{map[string]any{"code": "unsupported-backend", "message": "unsupported"}}
		}},
		{"managed-success", func(v map[string]any) { v["backend"] = "jj" }},
		{"bare-worktree", func(v map[string]any) { v["repository"].(map[string]any)["bare"] = true }},
		{"nonbare-null-worktree", func(v map[string]any) { v["repository"].(map[string]any)["worktree"] = nil }},
		{"head-ref", func(v map[string]any) { head(v)["ref"] = "main" }},
		{"detached-with-ref", func(v map[string]any) { head(v)["state"] = "detached" }},
		{"unborn-with-oid", func(v map[string]any) { head(v)["state"] = "unborn" }},
		{"attached-without-oid", func(v map[string]any) { head(v)["oid"] = nil }},
		{"oid-case", func(v map[string]any) { head(v)["oid"] = strings.Repeat("A", 40) }},
		{"oid-newline", func(v map[string]any) { head(v)["oid"] = strings.Repeat("a", 40) + "\n" }},
		{"sha256-length", func(v map[string]any) { v["repository"].(map[string]any)["object_format"] = "sha256" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := value(t, []byte(seed)).(map[string]any)
			tc.mutate(v)
			if s.Validate(v) == nil {
				t.Fatal("invalid invariant accepted")
			}
		})
	}
	v := value(t, []byte(seed)).(map[string]any)
	v["future"] = true
	v["repository"].(map[string]any)["future"] = true
	head(v)["future"] = true
	v["evidence"] = []any{map[string]any{"kind": "gitbutler-reference", "location": "refs/gitbutler/x", "future": true}}
	if e := s.Validate(v); e != nil {
		t.Fatal(e)
	}
	for _, backend := range []string{"unknown", "git", "jj", "gitbutler", "ambiguous"} {
		v := value(t, []byte(seed)).(map[string]any)
		v["status"] = "error"
		v["backend"] = backend
		v["input_path"] = ""
		v["repository"] = nil
		v["diagnostics"] = []any{map[string]any{"code": "future-code", "message": "failure", "future": true}}
		if e := s.Validate(v); e != nil {
			t.Fatal(e)
		}
		v["diagnostics"] = []any{}
		if s.Validate(v) == nil {
			t.Fatal("empty error diagnostics accepted")
		}
	}
}
func head(v map[string]any) map[string]any {
	return v["repository"].(map[string]any)["head"].(map[string]any)
}
func TestRealCLIOutputs(t *testing.T) {
	version, inspect := schema(t, "version"), schema(t, "inspect")
	binary := filepath.Join(t.TempDir(), "stw")
	build := exec.Command("go", "build", "-o", binary, "./cmd/stw")
	build.Dir = root(t)
	if raw, e := build.CombinedOutput(); e != nil {
		t.Fatalf("build: %v %s", e, raw)
	}
	home := t.TempDir()
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "TMPDIR=" + t.TempDir(), "LC_ALL=C", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + filepath.Join(home, "config"), "GIT_TERMINAL_PROMPT=0", "GIT_TEMPLATE_DIR=" + t.TempDir()}
	run := func(name string, want int, s *jsonschema.Schema, args ...string) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command(binary, args...)
			cmd.Env = env
			var out, errout bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &errout
			err := cmd.Run()
			exit := 0
			if err != nil {
				ee, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatal(err)
				}
				exit = ee.ExitCode()
			}
			if exit != want {
				t.Fatalf("exit %d want %d: %s", exit, want, errout.String())
			}
			if want == 0 && errout.Len() != 0 {
				t.Fatal(errout.String())
			}
			if want == 1 && !strings.HasPrefix(errout.String(), "Error:") {
				t.Fatal("missing error diagnostic")
			}
			if s != nil {
				if !bytes.HasSuffix(out.Bytes(), []byte("\n")) {
					t.Fatal("JSON result lacks final newline")
				}
				valid(t, s, out.Bytes())
				if s == inspect {
					status := value(t, out.Bytes()).(map[string]any)["status"]
					if (want == 0) != (status == "ok") {
						t.Fatalf("status %v contradicts exit %d", status, want)
					}
				}
			} else if out.Len() != 0 {
				t.Fatal("usage error unexpectedly emitted JSON")
			}
		})
	}
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = env
		if raw, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("git %v: %v %s", args, e, raw)
		}
	}
	run("version", 0, version, "version", "--json")
	repo := t.TempDir()
	git(repo, "init", "-b", "main")
	run("unborn", 0, inspect, "inspect", repo, "--json")
	git(repo, "-c", "user.name=Tester", "-c", "user.email=test@example.org", "commit", "--allow-empty", "-m", "initial")
	run("attached", 0, inspect, "inspect", repo, "--json")
	git(repo, "checkout", "--detach")
	run("detached", 0, inspect, "inspect", repo, "--json")
	linked := filepath.Join(t.TempDir(), "linked")
	git(repo, "worktree", "add", "-b", "linked", linked)
	run("linked", 0, inspect, "inspect", linked, "--json")
	bare := filepath.Join(t.TempDir(), "bare")
	git(repo, "clone", "--bare", repo, bare)
	run("bare", 0, inspect, "inspect", bare, "--json")
	run("not-repository", 1, inspect, "inspect", home, "--json")
	run("missing-path", 1, inspect, "inspect", filepath.Join(home, "missing"), "--json")
	for _, marker := range []string{".jj", ".git/gitbutler"} {
		managed := t.TempDir()
		git(managed, "init", "-b", "main")
		if e := os.MkdirAll(filepath.Join(managed, marker), 0700); e != nil {
			t.Fatal(e)
		}
		run(marker, 1, inspect, "inspect", managed, "--json")
	}
	sha256 := t.TempDir()
	git(sha256, "init", "--object-format=sha256", "-b", "main")
	git(sha256, "-c", "user.name=Tester", "-c", "user.email=test@example.org", "commit", "--allow-empty", "-m", "initial")
	run("sha256", 0, inspect, "inspect", sha256, "--json")
	ambiguous := t.TempDir()
	git(ambiguous, "init", "-b", "main")
	for _, marker := range []string{".jj", ".git/gitbutler"} {
		if err := os.MkdirAll(filepath.Join(ambiguous, marker), 0700); err != nil {
			t.Fatal(err)
		}
	}
	run("ambiguous", 1, inspect, "inspect", ambiguous, "--json")
	unusual := filepath.Join(t.TempDir(), "repo with space\nand unicode-ä")
	if err := os.Mkdir(unusual, 0700); err != nil {
		t.Fatal(err)
	}
	git(unusual, "init", "-b", "main")
	run("unusual-path", 0, inspect, "inspect", unusual, "--json")
	run("invalid-utf8-path", 1, inspect, "inspect", string([]byte{0xff}), "--json")
	run("usage", 2, nil, "inspect", "--json", "one", "two")
	deps := exec.Command("go", "list", "-deps", "./cmd/stw")
	deps.Dir = root(t)
	raw, e := deps.Output()
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(raw, []byte("santhosh-tekuri")) {
		t.Fatal("schema validator entered runtime graph")
	}
}

// Keep encoding/json exercised with the standard decoder as well as validator decoding.
func TestJSONNumbersAreNotStrings(t *testing.T) {
	var v any
	if e := json.Unmarshal([]byte(`{"schema_version":"1","name":"SourceSteward","version":"dev"}`), &v); e != nil {
		t.Fatal(e)
	}
	if schema(t, "version").Validate(v) == nil {
		t.Fatal("string schema version accepted")
	}
}
