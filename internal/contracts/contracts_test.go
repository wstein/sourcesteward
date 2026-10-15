// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"bytes"
	"crypto/sha256"
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
func schema(t *testing.T, name string) *jsonschema.Schema { return schemaAt(t, "v1", name) }
func schemaAt(t *testing.T, version, name string) *jsonschema.Schema {
	t.Helper()
	raw, e := os.ReadFile(filepath.Join(root(t), "schemas", version, name+".schema.json"))
	if e != nil {
		t.Fatal(e)
	}
	doc, e := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	base := "https://wstein.github.io/sourcesteward/schemas/" + version + "/"
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
func TestSchemasCompileOffline(t *testing.T) {
	schema(t, "version")
	schema(t, "inspect")
	schemaAt(t, "v2", "inspect")
}
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
		selected := inspect
		if marker == ".jj" {
			selected = schemaAt(t, "v2", "inspect")
		}
		run(marker, 1, selected, "inspect", managed, "--json")
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

// Published v1 contracts are immutable even when the native jj contract evolves.
func TestPublishedV1Bytes(t *testing.T) {
	for name, want := range map[string]string{
		"inspect": "8e12ee38a3e065e1f2e47b88ec4b1668cd8a32d8879e4803870e8994a9cfcfae",
		"version": "886864837547ded4992af89e8e8e04d38fe7bf77724f91a8960a184c51a0659f",
	} {
		raw, err := os.ReadFile(filepath.Join(root(t), "schemas/v1", name+".schema.json"))
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(raw)); got != want {
			t.Fatalf("published %s v1 bytes changed: %s", name, got)
		}
	}
}
func jjSeed() string {
	return `{"schema_version":2,"status":"ok","backend":"jj","input_path":"/tmp/repo","evidence":[{"kind":"jj-metadata","location":"/tmp/repo/.jj"}],"repository":null,"diagnostics":[],"jj_repository":{"workspace_root":"/tmp/repo","metadata_dir":"/tmp/repo/.jj","repository_dir":"/tmp/repo/.jj/repo","workspace_name":"default","change_id":"` + strings.Repeat("k", 32) + `","commit_id":"` + strings.Repeat("a", 40) + `","operation_id":"` + strings.Repeat("b", 128) + `","observation":{"working_copy":"stored-unsnapshotted","limitation":"Stored identity does not certify disk freshness or concurrent state."}}}`
}
func TestJJInspectionInvariants(t *testing.T) {
	s := schemaAt(t, "v2", "inspect")
	valid(t, s, []byte(jjSeed()))
	cases := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"schema", func(v map[string]any) { v["schema_version"] = 1 }},
		{"backend", func(v map[string]any) { v["backend"] = "git" }},
		{"status", func(v map[string]any) { v["status"] = "maybe" }},
		{"git-repository", func(v map[string]any) { v["repository"] = map[string]any{} }},
		{"missing-jj", func(v map[string]any) { delete(v, "jj_repository") }},
		{"null-jj", func(v map[string]any) { v["jj_repository"] = nil }},
		{"error-with-identity", func(v map[string]any) {
			v["status"] = "error"
			v["diagnostics"] = []any{map[string]any{"code": "jj-failed", "message": "failed"}}
		}},
		{"success-diagnostic", func(v map[string]any) { v["diagnostics"] = []any{map[string]any{"code": "x", "message": "x"}} }},
	}
	for _, field := range []string{"workspace_root", "metadata_dir", "repository_dir", "workspace_name", "change_id", "commit_id", "operation_id", "observation"} {
		field := field
		cases = append(cases, struct {
			name   string
			mutate func(map[string]any)
		}{"missing-" + field, func(v map[string]any) { delete(v["jj_repository"].(map[string]any), field) }})
	}
	for _, field := range []string{"workspace_root", "metadata_dir", "repository_dir", "workspace_name"} {
		field := field
		cases = append(cases, struct {
			name   string
			mutate func(map[string]any)
		}{"empty-" + field, func(v map[string]any) { v["jj_repository"].(map[string]any)[field] = "" }})
	}
	for _, pair := range [][2]string{{"change_id", strings.Repeat("a", 32)}, {"change_id", strings.Repeat("k", 31)}, {"commit_id", strings.Repeat("k", 40)}, {"commit_id", strings.Repeat("a", 40) + "\n"}, {"operation_id", strings.Repeat("B", 128)}, {"operation_id", strings.Repeat("b", 127)}} {
		pair := pair
		cases = append(cases, struct {
			name   string
			mutate func(map[string]any)
		}{"invalid-" + pair[0] + pair[1][:1], func(v map[string]any) { v["jj_repository"].(map[string]any)[pair[0]] = pair[1] }})
	}
	cases = append(cases, struct {
		name   string
		mutate func(map[string]any)
	}{"snapshot", func(v map[string]any) {
		v["jj_repository"].(map[string]any)["observation"].(map[string]any)["working_copy"] = "fresh"
	}}, struct {
		name   string
		mutate func(map[string]any)
	}{"empty-limitation", func(v map[string]any) {
		v["jj_repository"].(map[string]any)["observation"].(map[string]any)["limitation"] = ""
	}})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := value(t, []byte(jjSeed())).(map[string]any)
			tc.mutate(v)
			if s.Validate(v) == nil {
				t.Fatal("invalid native invariant accepted")
			}
		})
	}
	v := value(t, []byte(jjSeed())).(map[string]any)
	v["future"] = true
	v["jj_repository"].(map[string]any)["future"] = true
	v["jj_repository"].(map[string]any)["observation"].(map[string]any)["future"] = true
	if err := s.Validate(v); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"error"} {
		v := value(t, []byte(jjSeed())).(map[string]any)
		v["status"] = status
		delete(v, "jj_repository")
		v["diagnostics"] = []any{map[string]any{"code": "future-code", "message": "failure", "future": true}}
		if err := s.Validate(v); err != nil {
			t.Fatal(err)
		}
		v["status"] = "unsupported"
		if s.Validate(v) == nil {
			t.Fatal("native unsupported status accepted")
		}
		v["status"] = "error"
		v["diagnostics"] = []any{}
		if s.Validate(v) == nil {
			t.Fatal("empty error diagnostics accepted")
		}
	}
}

func TestRealJJCLIOutputs(t *testing.T) {
	if _, err := exec.LookPath("jj"); err != nil {
		if os.Getenv("STW_REQUIRE_JJ") == "1" {
			t.Fatal("mandatory native jj schema fixtures require jj on PATH")
		}
		t.Skip("jj not installed; STW_REQUIRE_JJ=1 makes native schema fixtures mandatory")
	}
	s := schemaAt(t, "v2", "inspect")
	binary := filepath.Join(t.TempDir(), "stw")
	build := exec.Command("go", "build", "-o", binary, "./cmd/stw")
	build.Dir = root(t)
	if raw, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, raw)
	}
	home := t.TempDir()
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "TMPDIR=" + t.TempDir(), "LC_ALL=C", "JJ_CONFIG=" + filepath.Join(home, "config.toml"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + filepath.Join(home, "gitconfig")}
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("[user]\nname = \"Tester\"\nemail = \"test@example.org\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	jj := func(args ...string) {
		t.Helper()
		cmd := exec.Command("jj", args...)
		cmd.Env = env
		if raw, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("jj %v: %v %s", args, err, raw)
		}
	}
	run := func(name, path string, want int, runenv []string) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command(binary, "inspect", path, "--json")
			cmd.Env = runenv
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
				t.Fatalf("exit %d want %d: %s %s", exit, want, out.String(), errout.String())
			}
			if !bytes.HasSuffix(out.Bytes(), []byte("\n")) {
				t.Fatal("JSON result lacks newline")
			}
			valid(t, s, out.Bytes())
			v := value(t, out.Bytes()).(map[string]any)
			if (want == 0) != (v["status"] == "ok") {
				t.Fatal("exit/result disagreement")
			}
			if want == 0 && errout.Len() != 0 {
				t.Fatal(errout.String())
			}
			if want == 1 && !strings.HasPrefix(errout.String(), "Error:") {
				t.Fatal("missing Error diagnostic")
			}
		})
	}
	standalone := filepath.Join(t.TempDir(), "standalone")
	jj("git", "init", "--no-colocate", standalone)
	run("standalone", standalone, 0, env)
	colocated := filepath.Join(t.TempDir(), "colocated")
	jj("git", "init", "--colocate", colocated)
	run("colocated", colocated, 0, env)
	secondary := filepath.Join(t.TempDir(), "secondary")
	jj("-R", standalone, "workspace", "add", secondary)
	run("secondary", secondary, 0, env)
	nested := filepath.Join(secondary, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	run("nested", nested, 0, env)
	if err := os.WriteFile(filepath.Join(standalone, "unsnapshotted"), []byte("disk edit"), 0600); err != nil {
		t.Fatal(err)
	}
	run("unsnapshotted", standalone, 0, env)
	malformed := t.TempDir()
	if err := os.Mkdir(filepath.Join(malformed, ".jj"), 0700); err != nil {
		t.Fatal(err)
	}
	run("malformed", malformed, 1, env)
	noBackend := []string{"PATH=" + t.TempDir(), "HOME=" + home, "TMPDIR=" + t.TempDir()}
	run("missing-jj", standalone, 1, noBackend)
}
