// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func examplePath(name string) string {
	return filepath.Join("..", "..", "specs", "026-release-process", "examples", name+".release.toml")
}

func exampleBytes(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(examplePath(name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// gitEnv isolates test repositories from the user's configuration.
func gitEnv() []string {
	return append(withoutInheritedGit(os.Environ()), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@example.org", "GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@example.org", "GIT_AUTHOR_DATE=2026-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z")
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newRepo(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q", "-b", "main")
	return dir
}

func write(t *testing.T, dir, name string, data []byte) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func commitAll(t *testing.T, dir string) string {
	t.Helper()
	git(t, dir, "add", "-A", "-f")
	git(t, dir, "commit", "-q", "--allow-empty", "-m", "c")
	return git(t, dir, "rev-parse", "HEAD")
}

func repoWith(t *testing.T, files map[string][]byte) string {
	t.Helper()
	dir := newRepo(t)
	for name, data := range files {
		write(t, dir, name, data)
	}
	return dir
}

func run(t *testing.T, opts CheckOptions) CheckResult {
	t.Helper()
	return Check(context.Background(), opts)
}

func codes(r CheckResult) []string {
	var out []string
	for _, f := range r.Findings {
		out = append(out, f.Code+" "+f.File+" "+f.Pointer)
	}
	return out
}

func wantCodes(t *testing.T, r CheckResult, want ...string) {
	t.Helper()
	if strings.Join(codes(r), "|") != strings.Join(want, "|") {
		t.Fatalf("findings %q, want %q (status %s, error %+v)", codes(r), want, r.Status, r.Error)
	}
}

func TestCheckWorkingTreeAcceptsAValidContract(t *testing.T) {
	dir := repoWith(t, map[string][]byte{".stw/release.toml": exampleBytes(t, "go-cli")})
	r := run(t, CheckOptions{Path: dir})
	if r.Status != "ok" || r.Backend != "git" || r.Source != "worktree" || r.Contract == nil || len(r.Findings) != 0 || r.Error != nil {
		t.Fatalf("%+v", r)
	}
	if r.Summary.Errors != 0 || r.Summary.Infos != 0 || !r.Boundary.Complete || r.Boundary.Effects.Network || r.Boundary.Effects.Writes || r.Boundary.Effects.ProjectCommands || r.Boundary.Effects.Processes != "detection" {
		t.Fatalf("boundary %+v", r)
	}
	if len(r.Products) != 1 || r.Products[0].File != ".stw/release.toml" || len(r.Products[0].ConfigSHA256) != 64 {
		t.Fatalf("products %+v", r.Products)
	}
	if r.Boundary.Limits.MaxBytes != 65536 || r.Boundary.Limits.MaxFindings != 100 {
		t.Fatalf("limits %+v", r.Boundary.Limits)
	}
}

func TestCheckFindsTheRepositoryRootFromASubdirectory(t *testing.T) {
	dir := repoWith(t, map[string][]byte{".stw/release.toml": exampleBytes(t, "go-cli"), "deep/er/file": []byte("x")})
	r := run(t, CheckOptions{Path: filepath.Join(dir, "deep", "er")})
	if r.Status != "ok" || r.Root != dir {
		t.Fatalf("%+v", r)
	}
}

func TestCheckReportsEachProfileAndSchemaProblem(t *testing.T) {
	contract := "schema_version = 1\n[a.b]\nx = 1\n"
	dir := repoWith(t, map[string][]byte{".stw/release.toml": []byte(contract)})
	r := run(t, CheckOptions{Path: dir})
	wantCodes(t, r, "profile-violation .stw/release.toml ")
	if r.Status != "findings" || r.Contract != nil || r.Summary.Errors != 1 {
		t.Fatalf("%+v", r)
	}
}

func TestCheckMissingContract(t *testing.T) {
	r := run(t, CheckOptions{Path: newRepo(t)})
	if r.Status != "refused" || r.Error == nil || r.Error.Code != "config-missing" || r.Boundary.Complete {
		t.Fatalf("%+v", r)
	}
}

func TestCheckRefusalsOfOwnership(t *testing.T) {
	notRepo, _ := filepath.EvalSymlinks(t.TempDir())
	if r := run(t, CheckOptions{Path: notRepo}); r.Status != "refused" || r.Error.Code != "not-repository" {
		t.Fatalf("%+v", r)
	}
	bare, _ := filepath.EvalSymlinks(t.TempDir())
	git(t, bare, "init", "-q", "--bare")
	if r := run(t, CheckOptions{Path: bare}); r.Status != "refused" || r.Error.Code != "bare-repository" {
		t.Fatalf("%+v", r)
	}
	if r := run(t, CheckOptions{Path: filepath.Join(notRepo, "missing")}); r.Status != "error" {
		t.Fatalf("%+v", r)
	}
}

func TestCheckConfigAmbiguous(t *testing.T) {
	json := []byte(`{"schema_version":1}`)
	dir := repoWith(t, map[string][]byte{".stw/release.toml": exampleBytes(t, "go-cli"), ".stw/release.json": json})
	r := run(t, CheckOptions{Path: dir})
	wantCodes(t, r, "config-ambiguous .stw/release.json ")
	if r.Status != "findings" {
		t.Fatalf("%+v", r)
	}
	dir = repoWith(t, map[string][]byte{".stw/release.toml": exampleBytes(t, "go-cli"), ".stw/release.zlib.toml": exampleBytes(t, "mirror-zlib")})
	r = run(t, CheckOptions{Path: dir})
	wantCodes(t, r, "config-ambiguous .stw/release.toml ")
}

func TestCheckYAMLIsNotAContract(t *testing.T) {
	dir := repoWith(t, map[string][]byte{".stw/release.yaml": []byte("a: 1\n")})
	r := run(t, CheckOptions{Path: dir})
	wantCodes(t, r, "profile-violation .stw/release.yaml ")
	if r.Status != "findings" {
		t.Fatalf("%+v", r)
	}
}

func TestCheckJSONContract(t *testing.T) {
	// The JSON form is the same model: convert an example and lint it.
	doc := loadExample(t, "go-cli")
	raw := mustJSON(t, doc)
	dir := repoWith(t, map[string][]byte{".stw/release.json": raw})
	r := run(t, CheckOptions{Path: dir})
	if r.Status != "ok" || r.Contract == nil {
		t.Fatalf("%+v %q", r, codes(r))
	}
	dir = repoWith(t, map[string][]byte{".stw/release.json": []byte(`{"schema_version":1,"schema_version":1}`)})
	wantCodes(t, run(t, CheckOptions{Path: dir}), "profile-violation .stw/release.json /schema_version")
}

func TestCheckTwoProducts(t *testing.T) {
	files := map[string][]byte{".stw/release.zlib.toml": exampleBytes(t, "mirror-zlib"), ".stw/release.busybox.toml": exampleBytes(t, "mirror-busybox")}
	dir := repoWith(t, files)
	r := run(t, CheckOptions{Path: dir})
	if r.Status != "ok" || len(r.Products) != 2 || r.Contract != nil || r.Products[0].Product != "busybox" {
		t.Fatalf("%+v %q", r, codes(r))
	}
	if r = run(t, CheckOptions{Path: dir, Product: "zlib"}); r.Status != "ok" || r.Contract == nil || r.Contract.Project.Product != "zlib" || len(r.Products) != 1 {
		t.Fatalf("%+v", r)
	}
	if r = run(t, CheckOptions{Path: dir, Product: "nope"}); r.Status != "refused" || r.Error.Code != "product-unknown" {
		t.Fatalf("%+v", r)
	}
	overlapping := mustJSON(t, func() map[string]any {
		doc := loadExample(t, "mirror-zlib")
		setAt(t, doc, "zlibtwo", "project", "product")
		return doc
	}())
	files[".stw/release.zlibtwo.json"] = overlapping
	r = run(t, CheckOptions{Path: repoWith(t, files)})
	wantCodes(t, r, "product-tag-overlap .stw/release.zlibtwo.json /version")
	if r.Status != "findings" {
		t.Fatal(r.Status)
	}
	// With --product only that product is linted, so there is no cross-product finding.
	if r = run(t, CheckOptions{Path: repoWith(t, files), Product: "zlib"}); r.Status != "ok" {
		t.Fatalf("%+v %q", r, codes(r))
	}
}

func TestCheckRefusesSymlinksAndNonRegularFiles(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "real.toml", exampleBytes(t, "go-cli"))
	if err := os.MkdirAll(filepath.Join(dir, ".stw"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../real.toml", filepath.Join(dir, ".stw", "release.toml")); err != nil {
		t.Fatal(err)
	}
	r := run(t, CheckOptions{Path: dir})
	wantCodes(t, r, "profile-violation .stw/release.toml ")
	if r.Status != "findings" || r.Contract != nil {
		t.Fatalf("%+v", r)
	}
	dir = newRepo(t)
	if err := os.MkdirAll(filepath.Join(dir, ".stw", "release.toml"), 0o755); err != nil {
		t.Fatal(err)
	}
	wantCodes(t, run(t, CheckOptions{Path: dir}), "profile-violation .stw/release.toml ")
	dir = newRepo(t)
	write(t, dir, "elsewhere/release.toml", exampleBytes(t, "go-cli"))
	if err := os.Symlink("elsewhere", filepath.Join(dir, ".stw")); err != nil {
		t.Fatal(err)
	}
	if r := run(t, CheckOptions{Path: dir}); r.Status != "refused" || r.Error.Code != "config-missing" {
		t.Fatalf("a symlinked .stw was followed: %+v", r)
	}
}

func TestCheckLimits(t *testing.T) {
	big := []byte("# " + strings.Repeat("x", 64*1024) + "\n")
	r := run(t, CheckOptions{Path: repoWith(t, map[string][]byte{".stw/release.toml": big})})
	wantCodes(t, r, "limit-exceeded .stw/release.toml ")
	exact := append([]byte("#"), []byte(strings.Repeat("x", 64*1024-2)+"\n")...)
	r = run(t, CheckOptions{Path: repoWith(t, map[string][]byte{".stw/release.toml": exact})})
	if len(r.Findings) == 0 || r.Findings[0].Code == "limit-exceeded" {
		t.Fatalf("a file of exactly 64 KiB is within the limit: %q", codes(r))
	}
}

func TestCheckFindingLimit(t *testing.T) {
	var b strings.Builder
	b.WriteString("schema_version = 1\n")
	for i := 0; i < 150; i++ {
		b.WriteString("a.b = 1\n")
	}
	r := run(t, CheckOptions{Path: repoWith(t, map[string][]byte{".stw/release.toml": []byte(b.String())})})
	if len(r.Findings) != MaxFindings+1 || r.Findings[len(r.Findings)-1].Code != "limit-exceeded" || r.Boundary.Complete {
		t.Fatalf("%d findings, complete=%v", len(r.Findings), r.Boundary.Complete)
	}
}

func TestCheckAllowlistOverlapUsesTheWorkingTree(t *testing.T) {
	contract := string(exampleBytes(t, "go-cli"))
	contract = strings.Replace(contract, `paths = ["docs/**", "README.md", "llms.txt"]`, `paths = ["docs/**", "scripts/**"]`, 1)
	files := map[string][]byte{".stw/release.toml": []byte(contract)}
	r := run(t, CheckOptions{Path: repoWith(t, files)})
	if r.Status != "ok" {
		t.Fatalf("an argv path that does not exist is not an input: %q", codes(r))
	}
	files["scripts/release-package.sh"] = []byte("#!/bin/sh\n")
	r = run(t, CheckOptions{Path: repoWith(t, files)})
	wantCodes(t, r, "allowlist-overlaps-input .stw/release.toml /allowlist/paths/1")
	if !strings.Contains(r.Findings[0].Message, "scripts/release-package.sh") {
		t.Fatalf("message %q", r.Findings[0].Message)
	}
}

func TestCheckOutputEscapesHostileText(t *testing.T) {
	doc := loadExample(t, "go-cli")
	setAt(t, doc, "x\n\x1b[31m‮", "evil\nkey")
	raw := mustJSON(t, doc)
	r := run(t, CheckOptions{Path: repoWith(t, map[string][]byte{".stw/release.json": raw})})
	if len(r.Findings) != 1 || strings.ContainsAny(r.Findings[0].Pointer+r.Findings[0].Message, "\n\x1b‮") {
		t.Fatalf("%+v", r.Findings)
	}
}

func TestCheckCommitReadsTheCommittedBlob(t *testing.T) {
	dir := repoWith(t, map[string][]byte{".stw/release.toml": exampleBytes(t, "go-cli")})
	commit := commitAll(t, dir)
	// A later working-tree edit must not change the answer about the commit.
	write(t, dir, ".stw/release.toml", []byte("a.b = 1\n"))
	r := run(t, CheckOptions{Path: dir, Commit: commit})
	if r.Status != "ok" || r.Source != "commit" || r.Commit != commit || r.Boundary.Effects.Processes != "detection+reads" {
		t.Fatalf("%+v %q", r, codes(r))
	}
	if r := run(t, CheckOptions{Path: dir}); r.Status != "findings" {
		t.Fatalf("working tree: %+v", r)
	}
}

func TestCheckCommitRefusals(t *testing.T) {
	dir := repoWith(t, map[string][]byte{"x": []byte("x")})
	commit := commitAll(t, dir)
	if r := run(t, CheckOptions{Path: dir, Commit: commit}); r.Status != "refused" || r.Error.Code != "config-missing" {
		t.Fatalf("%+v", r)
	}
	if r := run(t, CheckOptions{Path: dir, Commit: strings.Repeat("a", 40)}); r.Status != "refused" || r.Error.Code != "commit-unresolved" {
		t.Fatalf("%+v", r)
	}
	tree := git(t, dir, "rev-parse", "HEAD^{tree}")
	if r := run(t, CheckOptions{Path: dir, Commit: tree}); r.Status != "refused" || r.Error.Code != "commit-unresolved" {
		t.Fatalf("a tree is not a commit: %+v", r)
	}
}

func TestCheckCommitTreeFindings(t *testing.T) {
	contract := string(exampleBytes(t, "go-cli"))
	contract = strings.Replace(contract, `paths = ["docs/**", "README.md", "llms.txt"]`, `paths = ["scripts/**"]`, 1)
	dir := repoWith(t, map[string][]byte{".stw/release.toml": []byte(contract)})
	first := commitAll(t, dir)
	write(t, dir, "scripts/release-package.sh", []byte("#!/bin/sh\n"))
	second := commitAll(t, dir)
	if r := run(t, CheckOptions{Path: dir, Commit: first}); r.Status != "ok" {
		t.Fatalf("%q", codes(r))
	}
	wantCodes(t, run(t, CheckOptions{Path: dir, Commit: second}), "allowlist-overlaps-input .stw/release.toml /allowlist/paths/0")
}

func TestCheckCommitSymlinkAndLargeBlob(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "real.toml", exampleBytes(t, "go-cli"))
	if err := os.MkdirAll(filepath.Join(dir, ".stw"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../real.toml", filepath.Join(dir, ".stw", "release.toml")); err != nil {
		t.Fatal(err)
	}
	commit := commitAll(t, dir)
	wantCodes(t, run(t, CheckOptions{Path: dir, Commit: commit}), "profile-violation .stw/release.toml ")
	dir = repoWith(t, map[string][]byte{".stw/release.toml": []byte("# " + strings.Repeat("x", 70000) + "\n")})
	commit = commitAll(t, dir)
	wantCodes(t, run(t, CheckOptions{Path: dir, Commit: commit}), "limit-exceeded .stw/release.toml ")
}

func TestCheckCommitTwoProductsAndHostileEntryNames(t *testing.T) {
	dir := repoWith(t, map[string][]byte{
		".stw/release.zlib.toml":    exampleBytes(t, "mirror-zlib"),
		".stw/release.busybox.toml": exampleBytes(t, "mirror-busybox"),
		".stw/odd\nname":            []byte("x"),
		".stw/release.toml.bak":     []byte("x"),
	})
	commit := commitAll(t, dir)
	r := run(t, CheckOptions{Path: dir, Commit: commit})
	if r.Status != "ok" || len(r.Products) != 2 {
		t.Fatalf("%+v %q", r, codes(r))
	}
}

func TestCheckGitButlerAndAmbiguousOwnership(t *testing.T) {
	dir := repoWith(t, map[string][]byte{".stw/release.toml": exampleBytes(t, "go-cli")})
	commit := commitAll(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, ".git", "gitbutler"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := run(t, CheckOptions{Path: dir})
	if r.Status != "ok" || r.Backend != "gitbutler" || r.Contract == nil {
		t.Fatalf("GitButler working-tree form: %+v", r)
	}
	if r = run(t, CheckOptions{Path: dir, Commit: commit}); r.Status != "refused" || r.Error.Code != "unsupported-backend" {
		t.Fatalf("GitButler --commit: %+v", r)
	}
}

func TestCheckCancellation(t *testing.T) {
	dir := repoWith(t, map[string][]byte{".stw/release.toml": exampleBytes(t, "go-cli")})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := Check(ctx, CheckOptions{Path: dir})
	if r.Status != "error" || r.Error == nil || r.Error.Code != "canceled" {
		t.Fatalf("%+v", r)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestCheckJSONArraysAreNeverNull(t *testing.T) {
	for _, r := range []CheckResult{
		run(t, CheckOptions{Path: repoWith(t, map[string][]byte{".stw/release.toml": exampleBytes(t, "go-cli")})}),
		run(t, CheckOptions{Path: newRepo(t)}),
		run(t, CheckOptions{Path: t.TempDir()}),
	} {
		raw := string(mustJSON(t, r))
		if !strings.Contains(raw, `"findings":[]`) || !strings.Contains(raw, `"products":[`) || strings.Contains(raw, `"findings":null`) || strings.Contains(raw, `"products":null`) {
			t.Errorf("%s", raw)
		}
	}
}

func TestIsFullCommitID(t *testing.T) {
	good := []string{strings.Repeat("a", 40), strings.Repeat("0", 40), strings.Repeat("f", 64), "0123456789abcdef0123456789abcdef01234567"}
	bad := []string{"", strings.Repeat("a", 39), strings.Repeat("a", 41), strings.Repeat("a", 63), strings.Repeat("a", 65), strings.Repeat("A", 40), strings.Repeat("g", 40), strings.Repeat("a", 39) + "G", "HEAD", strings.Repeat("a", 39) + "\n", strings.Repeat("a", 39) + " "}
	for _, s := range good {
		if !IsFullCommitID(s) {
			t.Errorf("%q rejected", s)
		}
	}
	for _, s := range bad {
		if IsFullCommitID(s) {
			t.Errorf("%q accepted", s)
		}
	}
}

func TestCheckEscapesPathsAndErrorsInTheResult(t *testing.T) {
	parent, _ := filepath.EvalSymlinks(t.TempDir())
	dir := filepath.Join(parent, "repo\n\x1b[31mname")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q", "-b", "main")
	write(t, dir, ".stw/release.toml", exampleBytes(t, "go-cli"))
	r := run(t, CheckOptions{Path: dir})
	if r.Status != "ok" || strings.ContainsAny(r.Root+r.InputPath, "\n\x1b") || !strings.Contains(r.Root, `repo\n\x1b[31mname`) {
		t.Fatalf("%q %q %s", r.Root, r.InputPath, r.Status)
	}
	if os.Getuid() == 0 {
		t.Skip("permissions are not enforced for root")
	}
	if err := os.Chmod(filepath.Join(dir, ".stw", "release.toml"), 0); err != nil {
		t.Fatal(err)
	}
	r = run(t, CheckOptions{Path: dir})
	if r.Status != "error" || r.Error == nil || r.Error.Code != "read-failed" || strings.ContainsAny(r.Error.Message, "\n\x1b") || !strings.Contains(r.Error.Message, `repo\n\x1b[31mname`) {
		t.Fatalf("%+v", r)
	}
}

func TestCheckOnlyTheSelectedProductIsJudged(t *testing.T) {
	files := map[string][]byte{".stw/release.zlib.toml": exampleBytes(t, "mirror-zlib"), ".stw/release.yaml": []byte("a: 1\n")}
	dir := repoWith(t, files)
	if r := run(t, CheckOptions{Path: dir}); r.Status != "findings" {
		t.Fatalf("without --product the YAML file is a finding: %+v", r)
	}
	if r := run(t, CheckOptions{Path: dir, Product: "zlib"}); r.Status != "ok" || len(r.Findings) != 0 {
		t.Fatalf("with --product only that product is linted: %+v %q", r, codes(r))
	}
}

func TestCheckProductKeepsAmbiguityFindingsAboutThatProduct(t *testing.T) {
	// Two files for the selected product.
	dir := repoWith(t, map[string][]byte{".stw/release.zlib.toml": exampleBytes(t, "mirror-zlib"), ".stw/release.zlib.json": mustJSON(t, func() map[string]any {
		doc := loadExample(t, "mirror-zlib")
		return doc
	}())})
	r := run(t, CheckOptions{Path: dir, Product: "zlib"})
	wantCodes(t, r, "config-ambiguous .stw/release.zlib.json ")
	if r.Status != "findings" {
		t.Fatalf("%+v", r)
	}
	// The default file mixed with the selected named product.
	dir = repoWith(t, map[string][]byte{".stw/release.toml": exampleBytes(t, "go-cli"), ".stw/release.zlib.toml": exampleBytes(t, "mirror-zlib")})
	r = run(t, CheckOptions{Path: dir, Product: "zlib"})
	wantCodes(t, r, "config-ambiguous .stw/release.toml ")
	// Findings about another product's files stay out.
	dir = repoWith(t, map[string][]byte{".stw/release.zlib.toml": exampleBytes(t, "mirror-zlib"), ".stw/release.busybox.toml": exampleBytes(t, "mirror-busybox"), ".stw/release.busybox.json": []byte("{}"), ".stw/release.other.yaml": []byte("a: 1\n")})
	r = run(t, CheckOptions{Path: dir, Product: "zlib"})
	if r.Status != "ok" || len(r.Findings) != 0 {
		t.Fatalf("%q", codes(r))
	}
	// The default file mixed with named files is not about a product that is not in the mix.
	dir = repoWith(t, map[string][]byte{".stw/release.zlib.toml": exampleBytes(t, "mirror-zlib"), ".stw/release.toml": exampleBytes(t, "go-cli"), ".stw/release.busybox.toml": exampleBytes(t, "mirror-busybox")})
	wantCodes(t, run(t, CheckOptions{Path: dir, Product: "busybox"}), "config-ambiguous .stw/release.toml ")
}

func TestCheckSymlinkLeavingTheRepositoryFailsClosed(t *testing.T) {
	contract := strings.Replace(string(exampleBytes(t, "go-cli")), `paths = ["docs/**", "README.md", "llms.txt"]`, `paths = ["docs/guides/*"]`, 1)
	parent, _ := filepath.EvalSymlinks(t.TempDir())
	dir := filepath.Join(parent, "repo")
	if err := os.MkdirAll(filepath.Join(parent, "outside", "elsewhere"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q", "-b", "main")
	write(t, dir, ".stw/release.toml", []byte(contract))
	write(t, dir, "scripts/release-package.sh", []byte("x"))
	if err := os.Symlink("../outside", filepath.Join(dir, "out")); err != nil {
		t.Fatal(err)
	}
	// Point the build at the link: rewrite one argv word.
	contract = strings.Replace(contract, `"sh", "scripts/release-package.sh"`, `"sh", "out/elsewhere/x.sh"`, 1)
	write(t, dir, ".stw/release.toml", []byte(contract))
	commit := commitAll(t, dir)
	for _, opts := range []CheckOptions{{Path: dir}, {Path: dir, Commit: commit}} {
		r := run(t, opts)
		wantCodes(t, r, "allowlist-overlaps-input .stw/release.toml /allowlist/paths/0")
		if !strings.Contains(r.Findings[0].Message, "cannot be proved apart") {
			t.Fatalf("%+v", r.Findings[0])
		}
	}
}

func TestCheckFollowsRepositorySymlinksToInputs(t *testing.T) {
	contract := strings.Replace(string(exampleBytes(t, "go-cli")), `paths = ["docs/**", "README.md", "llms.txt"]`, `paths = ["tools/**"]`, 1)
	dir := repoWith(t, map[string][]byte{".stw/release.toml": []byte(contract), "tools/release-package.sh": []byte("#!/bin/sh\n")})
	if err := os.Symlink("tools", filepath.Join(dir, "scripts")); err != nil {
		t.Fatal(err)
	}
	commit := commitAll(t, dir)
	for _, opts := range []CheckOptions{{Path: dir}, {Path: dir, Commit: commit}} {
		r := run(t, opts)
		wantCodes(t, r, "allowlist-overlaps-input .stw/release.toml /allowlist/paths/0")
	}
}

func TestCheckGitButlerRootIsTheRepositoryNotAStrayGitDirectory(t *testing.T) {
	dir := repoWith(t, map[string][]byte{".stw/release.toml": exampleBytes(t, "go-cli")})
	if err := os.MkdirAll(filepath.Join(dir, ".git", "gitbutler"), 0o755); err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(dir, "sub", "inner")
	if err := os.MkdirAll(filepath.Join(inner, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := run(t, CheckOptions{Path: inner})
	if r.Backend != "gitbutler" || r.Root != dir || r.Status != "ok" {
		t.Fatalf("root %q backend %q status %s %+v", r.Root, r.Backend, r.Status, r.Error)
	}
}

func TestCheckCommitInABareRepository(t *testing.T) {
	src := repoWith(t, map[string][]byte{".stw/release.toml": exampleBytes(t, "go-cli")})
	commit := commitAll(t, src)
	bare := filepath.Join(t.TempDir(), "bare.git")
	git(t, src, "clone", "-q", "--bare", src, bare)
	r := run(t, CheckOptions{Path: bare, Commit: commit})
	if r.Status != "ok" || r.Backend != "git" || r.Contract == nil {
		t.Fatalf("%+v %q", r, codes(r))
	}
	if r := run(t, CheckOptions{Path: bare}); r.Status != "refused" || r.Error.Code != "bare-repository" || !strings.Contains(r.Error.Message, "--commit") {
		t.Fatalf("working-tree form: %+v", r)
	}
	if r := run(t, CheckOptions{Path: bare, Commit: strings.Repeat("a", 40)}); r.Error == nil || r.Error.Code != "commit-unresolved" {
		t.Fatalf("%+v", r)
	}
}

func TestNewGitSourceTellsAMissingCommitFromAFailure(t *testing.T) {
	dir := repoWith(t, map[string][]byte{"x": []byte("x")})
	commitAll(t, dir)
	if _, err := newGitSource(context.Background(), dir, strings.Repeat("b", 40)); !errors.Is(err, errCommitUnresolved) {
		t.Fatalf("%v", err)
	}
	if _, err := newGitSource(context.Background(), t.TempDir(), strings.Repeat("b", 40)); err == nil || errors.Is(err, errCommitUnresolved) {
		t.Fatalf("a directory that is no repository is a read failure, not a missing commit: %v", err)
	}
}

func TestCheckBoundsTheStwDirectoryListing(t *testing.T) {
	dir := repoWith(t, map[string][]byte{".stw/release.toml": exampleBytes(t, "go-cli")})
	for i := 0; i <= maxStwEntries; i++ {
		write(t, dir, fmt.Sprintf(".stw/n%05d", i), nil)
	}
	r := run(t, CheckOptions{Path: dir})
	found := false
	for _, f := range r.Findings {
		found = found || f.Code == "limit-exceeded" && strings.Contains(f.Message, ".stw directory")
	}
	if !found {
		t.Fatalf("%q", codes(r))
	}
}

func manyUnknownKeys(t *testing.T, n int) []byte {
	doc := loadExample(t, "go-cli")
	for i := 0; i < n; i++ {
		doc[fmt.Sprintf("k%03d", i)] = 1
	}
	return mustJSON(t, doc)
}

func TestCheckCutsFindingsOnceAtTheEnd(t *testing.T) {
	for _, c := range []struct {
		n        int
		want     int
		message  string
		complete bool
	}{{100, 100, "", true}, {101, 101, "1 further findings are not shown", false}, {150, 101, "50 further findings are not shown", false}} {
		r := run(t, CheckOptions{Path: repoWith(t, map[string][]byte{".stw/release.json": manyUnknownKeys(t, c.n)})})
		if len(r.Findings) != c.want || r.Boundary.Complete != c.complete {
			t.Fatalf("%d keys: %d findings, complete=%v", c.n, len(r.Findings), r.Boundary.Complete)
		}
		last := r.Findings[len(r.Findings)-1]
		if c.message != "" && (last.Code != "limit-exceeded" || last.Message != c.message) {
			t.Fatalf("%d keys: last finding %+v", c.n, last)
		}
		if c.message == "" && last.Code == "limit-exceeded" {
			t.Fatalf("%d keys: nothing was cut", c.n)
		}
	}
}

func TestCheckSaysSoWhenTheParserStoppedEarly(t *testing.T) {
	r := run(t, CheckOptions{Path: repoWith(t, map[string][]byte{".stw/release.toml": []byte(strings.Repeat("a.b = 1\n", 150))})})
	if len(r.Findings) != MaxFindings+1 || r.Boundary.Complete {
		t.Fatalf("%d findings", len(r.Findings))
	}
	last := r.Findings[len(r.Findings)-1]
	if last.Code != "limit-exceeded" || last.Message != "more findings were not shown" {
		t.Fatalf("%+v", last)
	}
	count := 0
	for _, f := range r.Findings {
		if f.Code == "limit-exceeded" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("%d limit findings", count)
	}
}

func TestCheckContractStringsAreEscapedForTerminals(t *testing.T) {
	doc := loadExample(t, "go-cli")
	setAt(t, doc, "echo\u202ehidden\u0085x \\ ok", "build", 0, "argv", 1)
	r := run(t, CheckOptions{Path: repoWith(t, map[string][]byte{".stw/release.json": mustJSON(t, doc)})})
	if r.Contract == nil {
		t.Fatalf("%q", codes(r))
	}
	got := r.Contract.Build[0].Argv[1]
	if got != `echo\u{202e}hidden\u{85}x \ ok` {
		t.Fatalf("%q", got)
	}
	if r.Products[0].Contract.Build[0].Argv[1] != got {
		t.Fatal("the per-file model is not escaped")
	}
	raw := string(mustJSON(t, r))
	if strings.ContainsAny(raw, "\u202e\u0085") {
		t.Fatal("raw bidi or C1 character in the JSON result")
	}
}

func TestCheckEscapesHostileContractFileNames(t *testing.T) {
	dir := repoWith(t, map[string][]byte{".stw/release.\x1b[31mX\u202e.toml": exampleBytes(t, "go-cli")})
	r := run(t, CheckOptions{Path: dir})
	if len(r.Findings) != 1 || strings.ContainsAny(r.Findings[0].File, "\x1b\u202e") || !strings.Contains(r.Findings[0].File, `\x1b[31mX\u{202e}`) {
		t.Fatalf("%+v", r.Findings)
	}
}

func TestCheckReportsAnOversizedStwListingFromACommit(t *testing.T) {
	dir := repoWith(t, map[string][]byte{".stw/release.toml": exampleBytes(t, "go-cli")})
	blob := git(t, dir, "hash-object", "-w", ".stw/release.toml")
	var index strings.Builder
	for i := 0; i < 6000; i++ {
		fmt.Fprintf(&index, "100644 %s 0\t.stw/%s%05d\n", blob, strings.Repeat("n", 200), i)
	}
	cmd := exec.Command("git", "update-index", "--add", "--index-info")
	cmd.Dir, cmd.Env, cmd.Stdin = dir, gitEnv(), strings.NewReader(index.String())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	git(t, dir, "commit", "-q", "-m", "many")
	r := run(t, CheckOptions{Path: dir, Commit: git(t, dir, "rev-parse", "HEAD")})
	found := false
	for _, f := range r.Findings {
		found = found || f.Code == "limit-exceeded" && strings.Contains(f.Message, ".stw directory")
	}
	if !found {
		t.Fatalf("a truncated listing went unreported: %q", codes(r))
	}
}

func TestGitEnvironmentPinsEveryVariable(t *testing.T) {
	t.Setenv("GIT_DIR", "/nonexistent")
	t.Setenv("GIT_EXTERNAL_DIFF", "/bin/false")
	env := map[string]string{}
	for _, e := range gitEnvironment() {
		k, v, _ := strings.Cut(e, "=")
		env[k] = v
	}
	want := map[string]string{"GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": "/dev/null", "GIT_OPTIONAL_LOCKS": "0", "GIT_TERMINAL_PROMPT": "0", "GIT_NO_LAZY_FETCH": "1", "GIT_PAGER": "cat", "GIT_ATTR_NOSYSTEM": "1", "GIT_LITERAL_PATHSPECS": "1"}
	for k, v := range want {
		if env[k] != v {
			t.Errorf("%s=%q, want %q", k, env[k], v)
		}
	}
	for k := range env {
		if strings.HasPrefix(k, "GIT_") {
			if _, ok := want[k]; !ok {
				t.Errorf("unexpected %s", k)
			}
		}
	}
}
