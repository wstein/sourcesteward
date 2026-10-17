// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/release"
)

// releaseExamples are the six worked-example contracts of specs/026-release-process (FR-023).
var releaseExamples = []string{"go-cli", "oci-bundle", "polyglot", "skill-pack", "mirror-zlib", "mirror-busybox"}

func releaseSpecFile(t *testing.T, rel string) string {
	t.Helper()
	return filepath.Join(root(t), "specs", "026-release-process", rel)
}

// fencedBlock returns the nth ```json block that follows the heading in a markdown file.
func fencedBlock(t *testing.T, markdown, heading string, n int) string {
	t.Helper()
	_, rest, ok := strings.Cut(markdown, heading)
	if !ok {
		t.Fatalf("heading %q not found", heading)
	}
	for i := 0; ; i++ {
		_, rest, ok = strings.Cut(rest, "```json\n")
		if !ok {
			t.Fatalf("json block %d after %q not found", n, heading)
		}
		block, after, ok := strings.Cut(rest, "\n```")
		if !ok {
			t.Fatalf("unterminated json block after %q", heading)
		}
		if i == n {
			return block + "\n"
		}
		rest = after
	}
}

// tomlToJSON uses Python tomllib, the reference parser of the strict-profile contract, to
// turn an example into the JSON the schema validates.
func tomlToJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required for the tomllib reference conversion")
	}
	script := "import json,sys,tomllib\nprint(json.dumps(tomllib.load(open(sys.argv[1],'rb'))))"
	out, err := exec.Command("python3", "-I", "-c", script, path).Output()
	if err != nil {
		t.Fatalf("tomllib: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func releaseExample(t *testing.T, name string) map[string]any {
	return tomlToJSON(t, releaseSpecFile(t, "examples/"+name+".release.toml"))
}

func copyJSON(v any) any {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		panic(err)
	}
	return out
}

func TestReleaseConfigSchemaEqualsTheContractBlock(t *testing.T) {
	markdown, err := os.ReadFile(releaseSpecFile(t, "contracts/config.md"))
	if err != nil {
		t.Fatal(err)
	}
	want := fencedBlock(t, string(markdown), "## JSON Schema (source of truth", 0)
	got, err := os.ReadFile(filepath.Join(root(t), "schemas", "v1", "release-config.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatal("schemas/v1/release-config.schema.json differs from the schema block of contracts/config.md")
	}
}

func TestReleaseWorkedExamplesAreValidContracts(t *testing.T) {
	s := schema(t, "release-config")
	for _, name := range releaseExamples {
		raw, err := json.Marshal(releaseExample(t, name))
		if err != nil {
			t.Fatal(err)
		}
		if e := s.Validate(value(t, raw)); e != nil {
			t.Errorf("%s: %v", name, e)
		}
	}
}

type configMutation struct {
	name    string
	example string
	mutate  func(map[string]any)
}

func obj(v any) map[string]any { return v.(map[string]any) }
func arr(v any) []any          { return v.([]any) }
func firstBuild(d map[string]any) map[string]any {
	return obj(arr(d["build"])[0])
}
func firstTarget(d map[string]any) map[string]any {
	return obj(arr(d["target"])[0])
}

// The ten negative mutants of research.md plus one fixture per schema rule.
func configMutations() []configMutation {
	set := func(path ...any) func(any) func(map[string]any) {
		return func(v any) func(map[string]any) {
			return func(d map[string]any) {
				var cur any = d
				for _, p := range path[:len(path)-1] {
					switch k := p.(type) {
					case string:
						cur = cur.(map[string]any)[k]
					case int:
						cur = cur.([]any)[k]
					}
				}
				switch k := path[len(path)-1].(type) {
				case string:
					if v == nil {
						delete(cur.(map[string]any), k)
					} else {
						cur.(map[string]any)[k] = v
					}
				case int:
					cur.([]any)[k] = v
				}
			}
		}
	}
	del := func(path ...any) func(map[string]any) { return set(path...)(nil) }
	strs := func(items ...string) []any {
		out := make([]any, len(items))
		for i, s := range items {
			out[i] = s
		}
		return out
	}
	long := strings.Repeat("a", 5000)
	m := []configMutation{
		// the ten mutants of research.md
		{"file target with repository", "go-cli", func(d map[string]any) { firstTarget(d)["repository"] = "ghcr.io/x/y" }},
		{"shell string argv", "go-cli", set("build", 0, "argv")("sh run.sh")},
		{"tag_pattern under semver", "go-cli", set("version", "tag_pattern")("^v(?P<version>1)$")},
		{"unknown top-level key", "go-cli", set("extra")(true)},
		{"source file without file keys", "go-cli", set("version", "source")("file")},
		{"fetch without hosts", "mirror-zlib", del("build", 0, "network_hosts")},
		{"hosts without fetch", "go-cli", set("build", 0, "network_hosts")(strs("example.org"))},
		{"pattern without tag_template", "oci-bundle", del("version", "tag_template")},
		{"oci target needs result_file", "oci-bundle", del("target", 0, "result_file")},
		{"unknown nested key", "go-cli", set("project", "extra")("x")},
		// schema rules, one fixture each
		{"schema_version const", "go-cli", set("schema_version")(2)},
		{"missing schema_version", "go-cli", del("schema_version")},
		{"missing project", "go-cli", del("project")},
		{"missing version", "go-cli", del("version")},
		{"missing build", "go-cli", del("build")},
		{"missing target", "go-cli", del("target")},
		{"missing outputs", "go-cli", del("outputs")},
		{"missing reproducibility", "go-cli", del("reproducibility")},
		{"missing allowlist", "go-cli", del("allowlist")},
		{"missing ci", "go-cli", del("ci")},
		{"project name missing", "go-cli", del("project", "name")},
		{"project name charset", "go-cli", set("project", "name")("bad/name")},
		{"project name too long", "go-cli", set("project", "name")(strings.Repeat("a", 65))},
		{"project product charset", "mirror-zlib", set("project", "product")("Zlib")},
		{"scheme enum", "go-cli", set("version", "scheme")("date")},
		{"source enum", "go-cli", set("version", "source")("tag")},
		{"scheme missing", "go-cli", del("version", "scheme")},
		{"tag_prefix without trailing slash", "go-cli", set("version", "tag_prefix")("bdock")},
		{"tag_prefix charset", "go-cli", set("version", "tag_prefix")("BD/")},
		{"tag_prefix too long", "go-cli", set("version", "tag_prefix")(strings.Repeat("a/", 33))},
		{"tag_template charset", "oci-bundle", set("version", "tag_template")("tool box/{version}")},
		{"tag_pattern too long", "oci-bundle", set("version", "tag_pattern")(strings.Repeat("a", 257))},
		{"tag_pattern empty", "oci-bundle", set("version", "tag_pattern")("")},
		{"pattern without tag_pattern", "oci-bundle", del("version", "tag_pattern")},
		{"pattern without order", "oci-bundle", del("version", "order")},
		{"pattern with tag_prefix", "oci-bundle", set("version", "tag_prefix")("a/")},
		{"semver with tag_template", "go-cli", set("version", "tag_template")("v{version}")},
		{"semver with order", "go-cli", set("version", "order")(strs("major"))},
		{"semver with monotonicity", "go-cli", set("version", "monotonicity")("strict")},
		{"monotonicity enum", "oci-bundle", set("version", "monotonicity")("loose")},
		{"order empty", "oci-bundle", set("version", "order")([]any{})},
		{"order too many", "oci-bundle", set("version", "order")(strs("a", "b", "c", "d", "e", "f", "g", "h", "i"))},
		{"order name charset", "oci-bundle", set("version", "order")(strs("Year"))},
		{"file with input source", "go-cli", set("version", "file")("VERSION")},
		{"file_pattern with input source", "go-cli", set("version", "file_pattern")("(x)")},
		{"file_form with input source", "go-cli", set("version", "file_form")("with-v")},
		{"file_form enum", "polyglot", set("version", "file_form")("plain")},
		{"file path leading slash", "polyglot", set("version", "file")("/etc/mix.exs")},
		{"file path charset", "polyglot", set("version", "file")("web mix.exs")},
		{"file_pattern too long", "polyglot", set("version", "file_pattern")(strings.Repeat("a", 257))},
		{"build empty", "go-cli", set("build")([]any{})},
		{"build too many", "go-cli", func(d map[string]any) {
			b := make([]any, 17)
			for i := range b {
				b[i] = copyJSON(firstBuild(d))
			}
			d["build"] = b
		}},
		{"build id charset", "go-cli", set("build", 0, "id")("Cli")},
		{"build id missing", "go-cli", del("build", 0, "id")},
		{"build scope enum", "go-cli", set("build", 0, "scope")("each")},
		{"build scope missing", "go-cli", del("build", 0, "scope")},
		{"build argv missing", "go-cli", del("build", 0, "argv")},
		{"build argv empty", "go-cli", set("build", 0, "argv")([]any{})},
		{"build argv element empty", "go-cli", set("build", 0, "argv", 0)("")},
		{"build argv element not string", "go-cli", set("build", 0, "argv", 0)(1)},
		{"build argv element too long", "go-cli", set("build", 0, "argv", 0)(long)},
		{"build argv too many", "go-cli", func(d map[string]any) {
			a := make([]any, 65)
			for i := range a {
				a[i] = "x"
			}
			firstBuild(d)["argv"] = a
		}},
		{"env_inherit name", "go-cli", set("build", 0, "env_inherit")(strs("path"))},
		{"env_inherit too many", "go-cli", func(d map[string]any) {
			a := make([]any, 33)
			for i := range a {
				a[i] = "A"
			}
			firstBuild(d)["env_inherit"] = a
		}},
		{"env_set shape", "go-cli", set("build", 0, "env_set")(strs("lower=1"))},
		{"env_set without equals", "go-cli", set("build", 0, "env_set")(strs("ABC"))},
		{"env_set NUL", "go-cli", set("build", 0, "env_set")(strs("A=b\x00c"))},
		{"env_set too many", "go-cli", func(d map[string]any) {
			a := make([]any, 65)
			for i := range a {
				a[i] = "A=1"
			}
			firstBuild(d)["env_set"] = a
		}},
		{"working_dir charset", "polyglot", set("build", 1, "working_dir")("a b")},
		{"timeout zero", "go-cli", set("build", 0, "timeout_seconds")(0)},
		{"timeout too large", "go-cli", set("build", 0, "timeout_seconds")(7201)},
		{"timeout not integer", "go-cli", set("build", 0, "timeout_seconds")(1.5)},
		{"network enum", "go-cli", set("build", 0, "network")("full")},
		{"fetch hosts charset", "mirror-zlib", set("build", 0, "network_hosts")(strs("Zlib.NET"))},
		{"fetch hosts empty", "mirror-zlib", set("build", 0, "network_hosts")([]any{})},
		{"fetch hosts too many", "mirror-zlib", func(d map[string]any) {
			a := make([]any, 65)
			for i := range a {
				a[i] = "a.example"
			}
			firstBuild(d)["network_hosts"] = a
		}},
		{"lock_files too many", "mirror-zlib", func(d map[string]any) {
			a := make([]any, 17)
			for i := range a {
				a[i] = "x.lock"
			}
			firstBuild(d)["lock_files"] = a
		}},
		{"lock_files charset", "mirror-zlib", set("build", 0, "lock_files")(strs("a b"))},
		{"build unknown key", "go-cli", set("build", 0, "shell")("sh")},
		{"target empty", "go-cli", set("target")([]any{})},
		{"target id charset", "go-cli", set("target", 0, "id")("Linux")},
		{"target kind enum", "go-cli", set("target", 0, "kind")("docker")},
		{"target build missing", "go-cli", del("target", 0, "build")},
		{"target role enum", "skill-pack", set("target", 1, "role")("docs")},
		{"target group charset", "go-cli", set("target", 0, "group")("CLI")},
		{"file target without os", "go-cli", del("target", 0, "os")},
		{"file target without arch", "go-cli", del("target", 0, "arch")},
		{"file target without output", "go-cli", del("target", 0, "output")},
		{"os charset", "go-cli", set("target", 0, "os")("Linux")},
		{"arch too long", "go-cli", set("target", 0, "arch")(strings.Repeat("a", 17))},
		{"output charset", "go-cli", set("target", 0, "output")("dist/release/a b")},
		{"output leading slash", "go-cli", set("target", 0, "output")("/dist/release/a")},
		{"runner charset", "go-cli", set("target", 0, "runner")("ubuntu 24")},
		{"smoke_argv shell string", "go-cli", set("target", 0, "smoke_argv")("sh smoke.sh")},
		{"file target with platforms", "go-cli", set("target", 0, "platforms")(strs("linux/amd64"))},
		{"file target with staging_tag", "go-cli", set("target", 0, "staging_tag")("x")},
		{"file target with result_file", "go-cli", set("target", 0, "result_file")("dist/release/r.json")},
		{"file target with lock_file", "go-cli", set("target", 0, "lock_file")("a.lock")},
		{"file target with tool_list_file", "go-cli", set("target", 0, "tool_list_file")("a.json")},
		{"file target with source_offer", "go-cli", set("target", 0, "source_offer")("a.md")},
		{"oci target without repository", "oci-bundle", del("target", 0, "repository")},
		{"oci target without platforms", "oci-bundle", del("target", 0, "platforms")},
		{"oci target without staging_tag", "oci-bundle", del("target", 0, "staging_tag")},
		{"oci target with os", "oci-bundle", set("target", 0, "os")("linux")},
		{"oci target with arch", "oci-bundle", set("target", 0, "arch")("amd64")},
		{"oci target with output", "oci-bundle", set("target", 0, "output")("dist/release/x")},
		{"oci repository uppercase", "oci-bundle", set("target", 0, "repository")("GHCR.io/x")},
		{"oci platforms empty", "oci-bundle", set("target", 0, "platforms")([]any{})},
		{"oci platform shape", "oci-bundle", set("target", 0, "platforms")(strs("linux"))},
		{"oci platforms too many", "oci-bundle", func(d map[string]any) {
			a := make([]any, 33)
			for i := range a {
				a[i] = "linux/amd64"
			}
			firstTarget(d)["platforms"] = a
		}},
		{"staging_tag charset", "oci-bundle", set("target", 0, "staging_tag")("a b")},
		{"source_offer empty", "oci-bundle", set("target", 0, "source_offer")("")},
		{"checksums charset", "go-cli", set("outputs", "checksums")("a/b")},
		{"url_template too long", "go-cli", set("outputs", "url_template")(strings.Repeat("a", 513))},
		{"outputs unknown key", "go-cli", set("outputs", "x")(1)},
		{"reproducibility policy enum", "go-cli", set("reproducibility", "policy")("always")},
		{"reproducibility epoch enum", "go-cli", set("reproducibility", "source_date_epoch")("now")},
		{"reproducibility policy missing", "go-cli", del("reproducibility", "policy")},
		{"reproducibility epoch missing", "go-cli", del("reproducibility", "source_date_epoch")},
		{"allowlist paths missing", "go-cli", del("allowlist", "paths")},
		{"allowlist pattern charset", "go-cli", set("allowlist", "paths")(strs("docs/{a,b}"))},
		{"allowlist pattern empty", "go-cli", set("allowlist", "paths")(strs(""))},
		{"allowlist too many", "go-cli", func(d map[string]any) {
			a := make([]any, 257)
			for i := range a {
				a[i] = "docs/*"
			}
			obj(d["allowlist"])["paths"] = a
		}},
		{"ci workflows missing", "go-cli", del("ci", "workflows")},
		{"ci workflows empty", "go-cli", set("ci", "workflows")([]any{})},
		{"ci workflow extension", "go-cli", set("ci", "workflows")(strs("ci.txt"))},
		{"ci workflows too many", "go-cli", set("ci", "workflows")(strs("a.yml", "b.yml", "c.yml", "d.yml", "e.yml", "f.yml", "g.yml", "h.yml", "i.yml"))},
		{"ci release_workflow missing", "go-cli", del("ci", "release_workflow")},
		{"ci release_workflow charset", "go-cli", set("ci", "release_workflow")("a b")},
		{"readiness files missing", "skill-pack", del("readiness", "files")},
		{"readiness files empty", "skill-pack", set("readiness", "files")([]any{})},
		{"readiness files too many", "skill-pack", func(d map[string]any) {
			a := make([]any, 33)
			for i := range a {
				a[i] = "r.json"
			}
			obj(d["readiness"])["files"] = a
		}},
		{"readiness unknown key", "skill-pack", set("readiness", "x")(1)},
		{"promote argv missing", "oci-bundle", del("promote", "argv")},
		{"promote argv empty", "oci-bundle", set("promote", "argv")([]any{})},
		{"promote timeout range", "oci-bundle", set("promote", "timeout_seconds")(0)},
		{"promote unknown key", "oci-bundle", set("promote", "x")(1)},
	}
	return m
}

func TestReleaseConfigNegativeFixturesAreRejected(t *testing.T) {
	s := schema(t, "release-config")
	names := map[string]bool{}
	for _, c := range configMutations() {
		if names[c.name] {
			t.Fatalf("duplicate fixture name %q", c.name)
		}
		names[c.name] = true
		doc := releaseExample(t, c.example)
		c.mutate(doc)
		raw, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		if s.Validate(value(t, raw)) == nil {
			t.Errorf("mutant %q (%s) was accepted", c.name, c.example)
		}
	}
	if len(names) < 100 {
		t.Fatalf("only %d fixtures", len(names))
	}
}

// The linter interprets the same schema file; it must reach the same verdict as the test-only
// JSON Schema library on every example and every negative fixture.
func TestReleaseSchemaInterpreterAgreesWithTheSchemaLibrary(t *testing.T) {
	s := schema(t, "release-config")
	check := func(name string, doc map[string]any) {
		raw, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		libraryValid := s.Validate(value(t, raw)) == nil
		var decoded any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		findings := release.ValidateSchema(decoded)
		if libraryValid != (len(findings) == 0) {
			t.Errorf("%s: library valid=%v, interpreter findings=%+v", name, libraryValid, findings)
		}
	}
	for _, name := range releaseExamples {
		check("example "+name, releaseExample(t, name))
	}
	for _, c := range configMutations() {
		doc := releaseExample(t, c.example)
		c.mutate(doc)
		check(c.name, doc)
	}
}

// ---- stw release check --json (schemas/v1/release-check.schema.json) ----

func checkRepo(t *testing.T, files map[string][]byte) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init", "-q", "-b", "main")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	for name, data := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func exampleFile(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(releaseSpecFile(t, "examples/"+name+".release.toml"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func checkJSON(t *testing.T, result release.CheckResult) []byte {
	t.Helper()
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestReleaseCheckResultsMatchTheirSchema(t *testing.T) {
	s := schema(t, "release-check")
	ctx := context.Background()
	overlap := strings.Replace(string(exampleFile(t, "go-cli")), `paths = ["docs/**", "README.md", "llms.txt"]`, `paths = ["scripts/**"]`, 1)
	cases := map[string]release.CheckResult{
		"valid":      release.Check(ctx, release.CheckOptions{Path: checkRepo(t, map[string][]byte{".stw/release.toml": exampleFile(t, "go-cli")})}),
		"findings":   release.Check(ctx, release.CheckOptions{Path: checkRepo(t, map[string][]byte{".stw/release.toml": []byte("a.b = 1\n")})}),
		"overlap":    release.Check(ctx, release.CheckOptions{Path: checkRepo(t, map[string][]byte{".stw/release.toml": []byte(overlap), "scripts/release-package.sh": []byte("x")})}),
		"info only":  release.Check(ctx, release.CheckOptions{Path: checkRepo(t, map[string][]byte{".stw/release.json": withUnusedBuild(t)})}),
		"missing":    release.Check(ctx, release.CheckOptions{Path: checkRepo(t, nil)}),
		"products":   release.Check(ctx, release.CheckOptions{Path: checkRepo(t, map[string][]byte{".stw/release.zlib.toml": exampleFile(t, "mirror-zlib"), ".stw/release.busybox.toml": exampleFile(t, "mirror-busybox")})}),
		"unknown":    release.Check(ctx, release.CheckOptions{Path: checkRepo(t, map[string][]byte{".stw/release.toml": exampleFile(t, "go-cli")}), Product: "x"}),
		"not a repo": release.Check(ctx, release.CheckOptions{Path: t.TempDir()}),
		"bad path":   release.Check(ctx, release.CheckOptions{Path: filepath.Join(t.TempDir(), "missing")}),
	}
	for name, result := range cases {
		if e := s.Validate(value(t, checkJSON(t, result))); e != nil {
			t.Errorf("%s: %v\n%s", name, e, checkJSON(t, result))
		}
	}
	if cases["valid"].Status != "ok" || cases["findings"].Status != "findings" || cases["info only"].Status != "ok" || cases["missing"].Status != "refused" || cases["bad path"].Status != "error" {
		t.Fatalf("the cases no longer cover every status: valid=%s findings=%s info=%s missing=%s bad=%s %v", cases["valid"].Status, cases["findings"].Status, cases["info only"].Status, cases["missing"].Status, cases["bad path"].Status, cases["info only"].Findings)
	}
	// The normalized model is itself a valid contract.
	configSchema := schema(t, "release-config")
	for _, name := range releaseExamples {
		product := map[string]string{"oci-bundle": "toolbox", "mirror-zlib": "zlib", "mirror-busybox": "busybox"}[name]
		file := ".stw/release.toml"
		if product != "" {
			file = ".stw/release." + product + ".toml"
		}
		result := release.Check(ctx, release.CheckOptions{Path: checkRepo(t, map[string][]byte{file: exampleFile(t, name)})})
		if result.Contract == nil {
			t.Fatalf("%s: %+v", name, result.Findings)
		}
		raw, err := json.Marshal(result.Contract)
		if err != nil {
			t.Fatal(err)
		}
		if e := configSchema.Validate(value(t, raw)); e != nil {
			t.Errorf("normalized %s: %v", name, e)
		}
	}
}

func withUnusedBuild(t *testing.T) []byte {
	doc := releaseExample(t, "go-cli")
	extra := copyJSON(firstBuild(doc)).(map[string]any)
	extra["id"] = "spare"
	doc["build"] = append(arr(doc["build"]), extra)
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestReleaseCheckSchemaRejectsInconsistentResults(t *testing.T) {
	s := schema(t, "release-check")
	ctx := context.Background()
	ok := release.Check(ctx, release.CheckOptions{Path: checkRepo(t, map[string][]byte{".stw/release.toml": exampleFile(t, "go-cli")})})
	refused := release.Check(ctx, release.CheckOptions{Path: checkRepo(t, nil)})
	findings := release.Check(ctx, release.CheckOptions{Path: checkRepo(t, map[string][]byte{".stw/release.toml": []byte("a.b = 1\n")})})
	mutate := func(base release.CheckResult, edit func(map[string]any)) []byte {
		var doc map[string]any
		if err := json.Unmarshal(checkJSON(t, base), &doc); err != nil {
			t.Fatal(err)
		}
		edit(doc)
		raw, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	cases := []struct {
		name string
		base release.CheckResult
		edit func(map[string]any)
	}{
		{"unknown command", ok, func(d map[string]any) { d["command"] = "release-plan" }},
		{"wrong schema version", ok, func(d map[string]any) { d["schema_version"] = 2 }},
		{"unknown status", ok, func(d map[string]any) { d["status"] = "maybe" }},
		{"ok with an error object", ok, func(d map[string]any) { d["error"] = map[string]any{"code": "x", "message": "y"} }},
		{"refused without an error", refused, func(d map[string]any) { d["error"] = nil }},
		{"refused with a contract", refused, func(d map[string]any) { d["contract"] = map[string]any{} }},
		{"findings status without errors", findings, func(d map[string]any) { d["summary"] = map[string]any{"errors": 0, "infos": 0} }},
		{"ok with an error finding", ok, func(d map[string]any) {
			d["findings"] = []any{map[string]any{"code": "x", "severity": "error", "pointer": "", "message": "m"}}
		}},
		{"finding severity", findings, func(d map[string]any) { d["findings"].([]any)[0].(map[string]any)["severity"] = "fatal" }},
		{"finding without a pointer", findings, func(d map[string]any) { delete(d["findings"].([]any)[0].(map[string]any), "pointer") }},
		{"source enum", ok, func(d map[string]any) { d["source"] = "index" }},
		{"commit source without commit", ok, func(d map[string]any) { d["source"] = "commit" }},
		{"missing boundary", ok, func(d map[string]any) { delete(d, "boundary") }},
		{"network effect true", ok, func(d map[string]any) { d["boundary"].(map[string]any)["effects"].(map[string]any)["network"] = true }},
		{"writes effect true", ok, func(d map[string]any) { d["boundary"].(map[string]any)["effects"].(map[string]any)["writes"] = true }},
		{"project command effect true", ok, func(d map[string]any) {
			d["boundary"].(map[string]any)["effects"].(map[string]any)["project_commands"] = true
		}},
		{"processes enum", ok, func(d map[string]any) {
			d["boundary"].(map[string]any)["effects"].(map[string]any)["processes"] = "any"
		}},
		{"missing summary", ok, func(d map[string]any) { delete(d, "summary") }},
		{"negative summary", ok, func(d map[string]any) { d["summary"].(map[string]any)["errors"] = -1 }},
		{"null findings", ok, func(d map[string]any) { d["findings"] = nil }},
	}
	for _, c := range cases {
		if s.Validate(value(t, mutate(c.base, c.edit))) == nil {
			t.Errorf("%s was accepted", c.name)
		}
	}
	// Unknown properties are additive extensions of a result schema.
	extended := mutate(ok, func(d map[string]any) { d["future"] = true })
	valid(t, s, extended)
}
