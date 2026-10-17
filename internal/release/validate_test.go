// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

var exampleProducts = map[string]string{"oci-bundle": "toolbox", "mirror-zlib": "zlib", "mirror-busybox": "busybox"}

func loadExample(t *testing.T, name string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "specs", "026-release-process", "examples", name+".release.toml"))
	if err != nil {
		t.Fatal(err)
	}
	doc, findings := ParseTOML(raw)
	if len(findings) != 0 {
		t.Fatalf("%s: %+v", name, findings)
	}
	return doc
}

// fakeTree maps a path to "file", "dir" or "symlink:<target>".
type fakeTree map[string]string

func (f fakeTree) Entries(paths []string) (map[string]TreeEntry, error) {
	out := map[string]TreeEntry{}
	for _, p := range paths {
		switch v := f[p]; {
		case v == "":
		case strings.HasPrefix(v, "symlink:"):
			out[p] = TreeEntry{Kind: "symlink", Target: strings.TrimPrefix(v, "symlink:")}
		default:
			out[p] = TreeEntry{Kind: v}
		}
	}
	return out, nil
}

// navigate walks a document by object keys and array indexes.
func navigate(t *testing.T, doc any, path ...any) any {
	t.Helper()
	cur := doc
	for _, p := range path {
		switch k := p.(type) {
		case string:
			cur = cur.(map[string]any)[k]
		case int:
			cur = cur.([]any)[k]
		}
	}
	return cur
}

// setAt replaces the value at path; a nil value deletes an object key.
func setAt(t *testing.T, doc map[string]any, value any, path ...any) {
	t.Helper()
	parent := navigate(t, doc, path[:len(path)-1]...)
	switch k := path[len(path)-1].(type) {
	case string:
		if value == nil {
			delete(parent.(map[string]any), k)
		} else {
			parent.(map[string]any)[k] = value
		}
	case int:
		parent.([]any)[k] = value
	}
}

func strs(items ...string) []any {
	out := make([]any, len(items))
	for i, s := range items {
		out[i] = s
	}
	return out
}

func lint(t *testing.T, name string, tree Tree, mutate func(*testing.T, map[string]any)) (*Contract, []string) {
	t.Helper()
	contract, findings := lintFindings(t, name, tree, mutate)
	var got []string
	for _, f := range findings {
		got = append(got, f.Code+" "+f.Pointer)
	}
	sort.Strings(got)
	return contract, got
}

func lintFindings(t *testing.T, name string, tree Tree, mutate func(*testing.T, map[string]any)) (*Contract, []Finding) {
	t.Helper()
	doc := loadExample(t, name)
	if mutate != nil {
		mutate(t, doc)
	}
	contract, findings, err := ValidateContract(doc, ValidateOptions{Product: exampleProducts[name], Tree: tree})
	if err != nil {
		t.Fatal(err)
	}
	return contract, findings
}

func expectFindings(t *testing.T, got []string, want ...string) {
	t.Helper()
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("findings:\n got  %q\n want %q", got, want)
	}
}

func TestWorkedExamplesAreCleanContracts(t *testing.T) {
	for _, name := range []string{"go-cli", "oci-bundle", "polyglot", "skill-pack", "mirror-zlib", "mirror-busybox"} {
		contract, got := lint(t, name, nil, nil)
		if len(got) != 0 || contract == nil {
			t.Errorf("%s: %q", name, got)
		}
	}
}

type semanticCase struct {
	name    string
	example string
	tree    fakeTree
	mutate  func(*testing.T, map[string]any)
	want    []string
	msg     string // a substring one finding message must contain, to tell rules apart
}

func set(value any, path ...any) func(*testing.T, map[string]any) {
	return func(t *testing.T, doc map[string]any) { setAt(t, doc, value, path...) }
}

func both(fs ...func(*testing.T, map[string]any)) func(*testing.T, map[string]any) {
	return func(t *testing.T, doc map[string]any) {
		for _, f := range fs {
			f(t, doc)
		}
	}
}

func appendTo(value any, path ...any) func(*testing.T, map[string]any) {
	return func(t *testing.T, doc map[string]any) {
		parent := navigate(t, doc, path[:len(path)-1]...).(map[string]any)
		key := path[len(path)-1].(string)
		parent[key] = append(parent[key].([]any), value)
	}
}

func copyOf(v any) any {
	raw, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(raw, &out)
	return out
}

func placeholderCases() []semanticCase {
	return []semanticCase{
		{name: "unknown placeholder in argv", example: "go-cli", mutate: set("{bogus}", "build", 0, "argv", 2), want: []string{"placeholder-unknown /build/0/argv/2"}},
		{name: "unknown placeholder in env_set value", example: "go-cli", mutate: set("A={nope}", "build", 0, "env_set", 0), want: []string{"placeholder-unknown /build/0/env_set/0"}},
		{name: "known placeholder in env_set value", example: "go-cli", mutate: set("A={commit12}", "build", 0, "env_set", 0)},
		{name: "per-target placeholder in a once build argv", example: "skill-pack", mutate: set("{os}", "build", 0, "argv", 1), want: []string{"placeholder-unknown /build/0/argv/1"}},
		{name: "output placeholder in a once build env_set", example: "skill-pack", mutate: set(strs("X={output}"), "build", 0, "env_set"), want: []string{"placeholder-unknown /build/0/env_set/0"}},
		{name: "target placeholder in a once build", example: "skill-pack", mutate: set("{target}", "build", 0, "argv", 1), want: []string{"placeholder-unknown /build/0/argv/1"}},
		{name: "arch placeholder in a once build", example: "skill-pack", mutate: set("{arch}", "build", 0, "argv", 1), want: []string{"placeholder-unknown /build/0/argv/1"}},
		{name: "artifact placeholder is not a build placeholder", example: "go-cli", mutate: set("{artifact}", "build", 0, "argv", 2), want: []string{"placeholder-unknown /build/0/argv/2"}},
		{name: "digest placeholder is not a build placeholder", example: "go-cli", mutate: set("{digest}", "build", 0, "argv", 2), want: []string{"placeholder-unknown /build/0/argv/2"}},
		{name: "source_date_epoch and out_dir are build placeholders", example: "go-cli", mutate: set("{source_date_epoch}:{out_dir}", "build", 0, "argv", 2)},
		{name: "commit placeholder in output", example: "go-cli", mutate: set("dist/release/{commit}.tgz", "target", 0, "output"), want: []string{"placeholder-unknown /target/0/output"}},
		{name: "out_dir placeholder in output", example: "go-cli", mutate: set("dist/release/{out_dir}", "target", 0, "output"), want: []string{"placeholder-unknown /target/0/output"}},
		{name: "commit placeholder in result_file", example: "oci-bundle", mutate: set("dist/release/{commit}.json", "target", 0, "result_file"), want: []string{"placeholder-unknown /target/0/result_file"}},
		{name: "commit12 placeholder in result_file", example: "oci-bundle", mutate: set("dist/release/{commit12}.json", "target", 0, "result_file")},
		{name: "version placeholder in staging_tag", example: "oci-bundle", mutate: set("staging-{version}", "target", 0, "staging_tag"), want: []string{"placeholder-unknown /target/0/staging_tag"}},
		{name: "commit placeholder in staging_tag", example: "oci-bundle", mutate: set("staging-{commit}", "target", 0, "staging_tag")},
		{name: "commit12 placeholder in smoke_argv", example: "go-cli", mutate: set("{commit12}", "target", 0, "smoke_argv", 1), want: []string{"placeholder-unknown /target/0/smoke_argv/1"}},
		{name: "artifact placeholder in smoke_argv", example: "go-cli", mutate: set("{artifact}", "target", 0, "smoke_argv", 1)},
		{name: "output placeholder in smoke_argv", example: "go-cli", mutate: set("{output}", "target", 0, "smoke_argv", 1), want: []string{"placeholder-unknown /target/0/smoke_argv/1"}},
		{name: "commit placeholder in promote argv", example: "oci-bundle", mutate: set("{commit}", "promote", "argv", 0), want: []string{"placeholder-unknown /promote/argv/0"}},
		{name: "os placeholder in promote argv", example: "oci-bundle", mutate: set("{os}", "promote", "argv", 0), want: []string{"placeholder-unknown /promote/argv/0"}},
		{name: "version placeholder in url_template", example: "go-cli", mutate: set("https://example.org/{version}/{name}", "outputs", "url_template"), want: []string{"placeholder-unknown /outputs/url_template"}},
		{name: "repository tag name in url_template", example: "go-cli", mutate: set("https://github.com/{repository}/releases/download/{tag}/{name}", "outputs", "url_template")},
		{name: "unclosed brace", example: "go-cli", mutate: set("{version", "build", 0, "argv", 2), want: []string{"placeholder-unknown /build/0/argv/2"}},
		{name: "stray closing brace", example: "go-cli", mutate: set("version}", "build", 0, "argv", 2), want: []string{"placeholder-unknown /build/0/argv/2"}},
		{name: "empty braces", example: "go-cli", mutate: set("{}", "build", 0, "argv", 2), want: []string{"placeholder-unknown /build/0/argv/2"}},
		{name: "spaced placeholder", example: "go-cli", mutate: set("{ version }", "build", 0, "argv", 2), want: []string{"placeholder-unknown /build/0/argv/2"}},
		{name: "doubled braces", example: "go-cli", mutate: set("{{version}}", "build", 0, "argv", 2), want: []string{"placeholder-unknown /build/0/argv/2"}},
		{name: "uppercase placeholder", example: "go-cli", mutate: set("{VERSION}", "build", 0, "argv", 2), want: []string{"placeholder-unknown /build/0/argv/2"}},
	}
}

func referenceCases() []semanticCase {
	secondBuild := func(t *testing.T, doc map[string]any) {
		appendTo(copyOf(navigate(t, doc, "build", 0)), "build")(t, doc)
	}
	return []semanticCase{
		{name: "duplicate build id", example: "go-cli", mutate: secondBuild, want: []string{"duplicate-id /build/1/id"}},
		{name: "duplicate target id", example: "go-cli", mutate: set("linux-amd64", "target", 1, "id"), want: []string{"duplicate-id /target/1/id"}},
		{name: "unknown build reference", example: "go-cli", mutate: set("missing", "target", 0, "build"), want: []string{"unknown-reference /target/0/build"}},
		{name: "unused build is information", example: "go-cli", mutate: both(secondBuild, set("other", "build", 1, "id")), want: []string{"unused-build /build/1"}},
		{name: "build and target ids are separate namespaces", example: "skill-pack", mutate: set("pack", "target", 0, "id")},
	}
}

func argvCases() []semanticCase {
	return []semanticCase{
		{name: "shell string for build argv", example: "go-cli", mutate: set("sh run.sh", "build", 0, "argv"), want: []string{"argv-not-array /build/0/argv"}},
		{name: "empty build argv", example: "go-cli", mutate: set([]any{}, "build", 0, "argv"), want: []string{"argv-not-array /build/0/argv"}},
		{name: "number for build argv", example: "go-cli", mutate: set(int64(5), "build", 0, "argv"), want: []string{"argv-not-array /build/0/argv"}},
		{name: "shell string for smoke_argv", example: "go-cli", mutate: set("sh smoke.sh", "target", 0, "smoke_argv"), want: []string{"argv-not-array /target/0/smoke_argv"}},
		{name: "shell string for promote argv", example: "oci-bundle", mutate: set("crane tag", "promote", "argv"), want: []string{"argv-not-array /promote/argv"}},
		{name: "non-string argv element stays a schema finding", example: "go-cli", mutate: set(int64(1), "build", 0, "argv", 0), want: []string{"schema-violation /build/0/argv/0"}},
	}
}

func envCases() []semanticCase {
	var cases []semanticCase
	for _, name := range []string{"GITHUB_TOKEN", "NPM_TOKEN", "MY_SECRET", "API_KEY", "DB_PASSWORD", "SSH_AUTH_SOCK", "AUTH_HEADER", "AWS_CREDENTIALS", "ACTIONS_RUNTIME_TOKEN", "GITHUB_SHA", "ACTIONS_X", "SOURCE_DATE_EPOCH", "TZ", "LC_ALL"} {
		cases = append(cases,
			semanticCase{name: "env_inherit " + name, example: "go-cli", mutate: set(strs("PATH", name), "build", 0, "env_inherit"), want: []string{"env-name-forbidden /build/0/env_inherit/1"}},
			semanticCase{name: "env_set " + name, example: "go-cli", mutate: set(name+"=1", "build", 0, "env_set", 0), want: []string{"env-name-forbidden /build/0/env_set/0"}})
	}
	cases = append(cases,
		semanticCase{name: "lower-case credential name in env_inherit", example: "go-cli", mutate: set(strs("github_token"), "build", 0, "env_inherit"), want: []string{"schema-violation /build/0/env_inherit/0", "env-name-forbidden /build/0/env_inherit/0"}},
		semanticCase{name: "env_set name with braces is a schema problem only", example: "go-cli", mutate: set("{bogus}=1", "build", 0, "env_set", 0), want: []string{"schema-violation /build/0/env_set/0"}},
	)
	for _, name := range []string{"PATH", "HOME", "TMPDIR", "DOCKER_HOST", "GOFLAGS", "LANG", "CGO_ENABLED", "ACTION"} {
		cases = append(cases, semanticCase{name: "env_inherit allows " + name, example: "go-cli", mutate: set(strs(name), "build", 0, "env_inherit")})
	}
	return cases
}

func pathCases() []semanticCase {
	bad := []string{"../x", "a/../b", "..", "a//b", "a/", "./a", "a/./b"}
	var cases []semanticCase
	for _, p := range bad {
		cases = append(cases,
			semanticCase{name: "working_dir " + p, example: "polyglot", mutate: set(p, "build", 1, "working_dir"), want: []string{"schema-violation /build/1/working_dir"}},
			semanticCase{name: "version file " + p, example: "polyglot", mutate: set(p, "version", "file"), want: []string{"schema-violation /version/file"}},
			semanticCase{name: "lock file " + p, example: "mirror-zlib", mutate: set(strs(p), "build", 0, "lock_files"), want: []string{"schema-violation /build/0/lock_files/0"}},
			semanticCase{name: "readiness file " + p, example: "skill-pack", mutate: set(strs(p), "readiness", "files"), want: []string{"schema-violation /readiness/files/0"}},
			semanticCase{name: "release workflow " + p, example: "go-cli", mutate: set(p, "ci", "release_workflow"), want: []string{"schema-violation /ci/release_workflow"}},
			semanticCase{name: "oci lock_file " + p, example: "oci-bundle", mutate: set(p, "target", 0, "lock_file"), want: []string{"schema-violation /target/0/lock_file"}},
			semanticCase{name: "oci tool_list_file " + p, example: "oci-bundle", mutate: set(p, "target", 0, "tool_list_file"), want: []string{"schema-violation /target/0/tool_list_file"}})
	}
	for _, p := range []string{".", "a/."} {
		cases = append(cases,
			semanticCase{name: "version file " + p, example: "polyglot", mutate: set(p, "version", "file"), want: []string{"schema-violation /version/file"}},
			semanticCase{name: "readiness file " + p, example: "skill-pack", mutate: set(strs(p), "readiness", "files"), want: []string{"schema-violation /readiness/files/0"}})
	}
	cases = append(cases,
		semanticCase{name: "absolute path in a lock file", example: "mirror-zlib", mutate: set(strs("/etc/passwd"), "build", 0, "lock_files"), want: []string{"schema-violation /build/0/lock_files/0"}},
		semanticCase{name: "working_dir dot is the default", example: "polyglot", mutate: set(".", "build", 1, "working_dir")},
		semanticCase{name: "working_dir below the root", example: "polyglot", mutate: set("web/app", "build", 1, "working_dir")},
		semanticCase{name: "output outside the out dir", example: "go-cli", mutate: set("out/x.tgz", "target", 0, "output"), want: []string{"schema-violation /target/0/output"}},
		semanticCase{name: "output at the repository root", example: "go-cli", mutate: set("x.tgz", "target", 0, "output"), want: []string{"schema-violation /target/0/output"}},
		semanticCase{name: "output is the out dir itself", example: "go-cli", mutate: set("dist/release/", "target", 0, "output"), want: []string{"schema-violation /target/0/output"}},
		semanticCase{name: "output with dot-dot after the prefix", example: "go-cli", mutate: set("dist/release/../x", "target", 0, "output"), want: []string{"schema-violation /target/0/output"}},
		semanticCase{name: "output near the out dir prefix", example: "go-cli", mutate: set("dist/releases/x", "target", 0, "output"), want: []string{"schema-violation /target/0/output"}},
		semanticCase{name: "result_file outside the out dir", example: "oci-bundle", mutate: set("x/r.json", "target", 0, "result_file"), want: []string{"schema-violation /target/0/result_file"}},
		semanticCase{name: "result_file in a subdirectory of the out dir", example: "oci-bundle", mutate: set("dist/release/sub/r.json", "target", 0, "result_file")},
	)
	return cases
}

func fetchAndPromoteCases() []semanticCase {
	return []semanticCase{
		{name: "fetch under required reproducibility needs lock files", example: "mirror-zlib", mutate: set(nil, "build", 0, "lock_files"), want: []string{"fetch-unpinned /build/0"}},
		{name: "fetch with empty lock files under required reproducibility", example: "mirror-zlib", mutate: set([]any{}, "build", 0, "lock_files"), want: []string{"fetch-unpinned /build/0"}},
		{name: "fetch without lock files is fine when best-effort", example: "mirror-zlib", mutate: both(set(nil, "build", 0, "lock_files"), set("best-effort", "reproducibility", "policy"))},
		{name: "fetch without lock files is fine when none", example: "mirror-zlib", mutate: both(set(nil, "build", 0, "lock_files"), set("none", "reproducibility", "policy"))},
		{name: "oci target without promote", example: "oci-bundle", mutate: set(nil, "promote"), want: []string{"schema-violation /promote"}},
		{name: "promote without an oci target", example: "go-cli", mutate: set(map[string]any{"argv": strs("crane", "{digest}", "{version}")}, "promote"), want: []string{"schema-violation /promote"}},
		{name: "promote without digest", example: "oci-bundle", mutate: set(strs("crane", "tag", "{repository}", "{version}"), "promote", "argv"), want: []string{"promote-argv-invalid /promote/argv"}},
		{name: "promote without version", example: "oci-bundle", mutate: set(strs("crane", "tag", "{repository}@{digest}", "v1"), "promote", "argv"), want: []string{"promote-argv-invalid /promote/argv"}},
		{name: "promote with version_number", example: "oci-bundle", mutate: set(strs("crane", "tag", "{repository}@{digest}", "{version_number}"), "promote", "argv")},
		{name: "promote alias latest", example: "oci-bundle", mutate: appendTo("latest", "promote", "argv"), want: []string{"promote-argv-invalid /promote/argv/4"}},
		{name: "promote alias stable", example: "oci-bundle", mutate: appendTo("stable", "promote", "argv"), want: []string{"promote-argv-invalid /promote/argv/4"}},
		{name: "promote alias edge", example: "oci-bundle", mutate: appendTo("edge", "promote", "argv"), want: []string{"promote-argv-invalid /promote/argv/4"}},
		{name: "promote alias main", example: "oci-bundle", mutate: appendTo("main", "promote", "argv"), want: []string{"promote-argv-invalid /promote/argv/4"}},
		{name: "promote alias master", example: "oci-bundle", mutate: appendTo("master", "promote", "argv"), want: []string{"promote-argv-invalid /promote/argv/4"}},
		{name: "promote element merely containing an alias word", example: "oci-bundle", mutate: appendTo("latest-build", "promote", "argv")},
		{name: "promote with timeout", example: "oci-bundle", mutate: set(int64(60), "promote", "timeout_seconds")},
	}
}

func productCases() []semanticCase {
	return []semanticCase{
		{name: "default file with a product", example: "go-cli", mutate: set("x", "project", "product"), want: []string{"schema-violation /project/product"}},
		{name: "mismatched product", example: "mirror-zlib", mutate: set("busybox", "project", "product"), want: []string{"schema-violation /project/product"}},
		{name: "named file without a product", example: "mirror-zlib", mutate: set(nil, "project", "product"), want: []string{"schema-violation /project"}},
	}
}

func TestContractSemanticRules(t *testing.T) {
	var groups [][]semanticCase
	groups = append(groups, placeholderCases(), referenceCases(), argvCases(), envCases(), pathCases(), fetchAndPromoteCases(), productCases(), schemeCases(), allowlistCases())
	for _, group := range groups {
		for _, c := range group {
			t.Run(c.name, func(t *testing.T) {
				var tree Tree
				if c.tree != nil {
					tree = c.tree
				}
				contract, findings := lintFindings(t, c.example, tree, c.mutate)
				var got []string
				hasError := false
				for _, f := range findings {
					got = append(got, f.Code+" "+f.Pointer)
					hasError = hasError || f.Severity == severityError
				}
				if (contract != nil) == hasError {
					t.Fatalf("contract returned=%v although error findings=%v", contract != nil, hasError)
				}
				sort.Strings(got)
				expectFindings(t, got, c.want...)
				if c.msg != "" {
					found := false
					for _, f := range findings {
						found = found || strings.Contains(f.Message, c.msg)
					}
					if !found {
						t.Fatalf("no message contains %q: %+v", c.msg, findings)
					}
				}
			})
		}
	}
}

func TestNormalizedContractCarriesDefaults(t *testing.T) {
	contract, got := lint(t, "go-cli", nil, nil)
	if len(got) != 0 {
		t.Fatal(got)
	}
	b := contract.Build[0]
	if strings.Join(b.EnvInherit, ",") != "PATH,HOME,TMPDIR" || b.WorkingDir != "." || b.Network != "none" || b.TimeoutSeconds != 900 {
		t.Fatalf("build defaults: %+v", b)
	}
	if contract.Target[0].Role != "artifact" || contract.Outputs.Checksums != "SHA256SUMS" || contract.Outputs.URLTemplate != "https://github.com/{repository}/releases/download/{tag}/{name}" {
		t.Fatalf("defaults: %+v %+v", contract.Target[0], contract.Outputs)
	}
	toolbox, _ := lint(t, "oci-bundle", nil, nil)
	if toolbox.Version.Monotonicity != "non-decreasing" || toolbox.Build[0].TimeoutSeconds != 3600 {
		t.Fatalf("%+v", toolbox.Version)
	}
	zlib, _ := lint(t, "mirror-zlib", nil, nil)
	if zlib.Version.Monotonicity != "strict" || zlib.Build[0].Network != "fetch" {
		t.Fatalf("%+v", zlib.Version)
	}
	skill, _ := lint(t, "skill-pack", nil, nil)
	if skill.Readiness == nil || len(skill.Readiness.Files) != 3 {
		t.Fatal("readiness lost")
	}
	raw, err := json.Marshal(contract)
	if err != nil || !strings.Contains(string(raw), `"schema_version":1`) {
		t.Fatalf("%s %v", raw, err)
	}
}

func TestSchemaFindingsKeepTheirPointersThroughValidation(t *testing.T) {
	_, got := lint(t, "go-cli", nil, set("extra", "bogus"))
	expectFindings(t, got, "schema-violation /bogus")
	_, got = lint(t, "go-cli", nil, set(int64(2), "schema_version"))
	expectFindings(t, got, "schema-violation /schema_version")
}

func TestOneFindingPerCodeAndPointer(t *testing.T) {
	_, findings, err := ValidateContract(func() map[string]any {
		doc := loadExample(t, "go-cli")
		setAt(t, doc, "{a}{b}{c}", "build", 0, "argv", 2)
		return doc
	}(), ValidateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, f := range findings {
		if f.Code == "placeholder-unknown" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("want one placeholder-unknown, got %d", count)
	}
}

func TestMoreThanOneHundredFindingsAreCut(t *testing.T) {
	doc := loadExample(t, "go-cli")
	argv := make([]any, 0, 64)
	for i := 0; i < 64; i++ {
		argv = append(argv, "{bad}")
	}
	setAt(t, doc, argv, "build", 0, "argv")
	envs := make([]any, 0, 64)
	for i := 0; i < 64; i++ {
		envs = append(envs, "A={bad}")
	}
	setAt(t, doc, envs, "build", 0, "env_set")
	_, findings, err := ValidateContract(doc, ValidateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != MaxFindings+1 || findings[len(findings)-1].Code != "limit-exceeded" {
		t.Fatalf("got %d findings, last %+v", len(findings), findings[len(findings)-1])
	}
}

func TestDefaultsAreAppliedOnlyWhereAbsent(t *testing.T) {
	polyglot, got := lint(t, "polyglot", nil, nil)
	if len(got) != 0 {
		t.Fatal(got)
	}
	if polyglot.Build[2].TimeoutSeconds != 1800 || polyglot.Build[1].TimeoutSeconds != 2400 || polyglot.Build[1].WorkingDir != "." {
		t.Fatalf("%+v", polyglot.Build)
	}
	if polyglot.Target[0].Role != "artifact" || polyglot.Outputs.Checksums != "SHA256SUMS" {
		t.Fatalf("%+v", polyglot.Target[0])
	}
	skill, _ := lint(t, "skill-pack", nil, nil)
	if skill.Target[0].Role != "artifact" || skill.Target[1].Role != "manifest" {
		t.Fatalf("roles %q %q", skill.Target[0].Role, skill.Target[1].Role)
	}
	custom, got := lint(t, "go-cli", nil, both(
		set(map[string]any{"checksums": "SUMS", "url_template": "https://example.org/{tag}/{name}"}, "outputs"),
		set("best-effort", "reproducibility", "policy")))
	if len(got) != 0 || custom.Outputs.Checksums != "SUMS" || custom.Outputs.URLTemplate != "https://example.org/{tag}/{name}" {
		t.Fatalf("%v %+v", got, custom.Outputs)
	}
	bare, _ := lint(t, "go-cli", nil, set(map[string]any{}, "outputs"))
	if bare.Outputs.Checksums != "SHA256SUMS" || !strings.Contains(bare.Outputs.URLTemplate, "{repository}/releases/download/{tag}/{name}") {
		t.Fatalf("%+v", bare.Outputs)
	}
	withEnv, _ := lint(t, "go-cli", nil, set(strs("PATH"), "build", 0, "env_inherit"))
	if len(withEnv.Build[0].EnvInherit) != 1 {
		t.Fatalf("an explicit env_inherit was replaced: %v", withEnv.Build[0].EnvInherit)
	}
	explicitNetwork, _ := lint(t, "mirror-zlib", nil, nil)
	if explicitNetwork.Build[0].Network != "fetch" || explicitNetwork.Build[0].TimeoutSeconds != 1800 {
		t.Fatalf("%+v", explicitNetwork.Build[0])
	}
	semver, _ := lint(t, "go-cli", nil, nil)
	if semver.Version.Monotonicity != "" {
		t.Fatalf("monotonicity belongs to pattern products only: %q", semver.Version.Monotonicity)
	}
}

func manyPlaceholderErrors(t *testing.T, count int) map[string]any {
	doc := loadExample(t, "go-cli")
	argv, env := []any{}, []any{}
	for i := 0; i < count && i < 64; i++ {
		argv = append(argv, "{bad}")
	}
	for i := 64; i < count; i++ {
		env = append(env, "A={bad}")
	}
	setAt(t, doc, argv, "build", 0, "argv")
	setAt(t, doc, env, "build", 0, "env_set")
	return doc
}

func TestFindingLimitBoundary(t *testing.T) {
	for _, c := range []struct{ count, want int }{{99, 99}, {100, 100}, {101, MaxFindings + 1}} {
		_, findings, err := ValidateContract(manyPlaceholderErrors(t, c.count), ValidateOptions{})
		if err != nil || len(findings) != c.want {
			t.Errorf("%d errors: got %d findings, want %d", c.count, len(findings), c.want)
			continue
		}
		if cut := findings[len(findings)-1].Code == "limit-exceeded"; cut != (c.count > MaxFindings) {
			t.Errorf("%d errors: limit finding present=%v", c.count, cut)
		}
	}
}

func TestSemanticAndSchemaFindingsAreOrderedTogether(t *testing.T) {
	doc := loadExample(t, "go-cli")
	setAt(t, doc, "{bad}", "build", 0, "argv", 2)
	setAt(t, doc, int64(2), "schema_version")
	_, findings, _ := ValidateContract(doc, ValidateOptions{})
	if len(findings) != 2 || findings[0].Pointer != "/build/0/argv/2" || findings[1].Pointer != "/schema_version" {
		t.Fatalf("%+v", findings)
	}
}

func TestAllowlistFindingsAreDeterministic(t *testing.T) {
	tree := fakeTree{"scripts": "dir"}
	for i := 0; i < 40; i++ {
		tree[fmt.Sprintf("scripts/s%02d.sh", i)] = "file"
	}
	mutate := func(t *testing.T, doc map[string]any) {
		argv := []any{"sh"}
		for i := 39; i >= 0; i-- {
			argv = append(argv, fmt.Sprintf("scripts/s%02d.sh", i))
		}
		setAt(t, doc, argv, "build", 0, "argv")
		setAt(t, doc, strs("scripts/*.sh", "scripts/**", "**"), "allowlist", "paths")
	}
	var first string
	for run := 0; run < 25; run++ {
		_, findings := lintFindings(t, "go-cli", tree, mutate)
		var out []string
		for _, f := range findings {
			out = append(out, f.Pointer+f.Message)
		}
		got := strings.Join(out, "\n")
		if run == 0 {
			first = got
			if len(findings) != 3 {
				t.Fatalf("%q", out)
			}
		} else if got != first {
			t.Fatalf("run %d differs:\n%s\n--\n%s", run, got, first)
		}
	}
}
