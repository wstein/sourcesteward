// SPDX-License-Identifier: EUPL-1.2
package statusremote

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/overview"
	"github.com/wstein/sourcesteward/internal/stwsync"
	"github.com/wstein/sourcesteward/internal/watchcmd"
)

const ciRepo = "wstein/sourcesteward"

func ciSHA(seed string) string { return strings.Repeat(seed, 40)[:40] }

// ciFake is an injected forge reader: canned bodies and errors by request, every request recorded. The
// embedded nil reader makes any read the CI sources must not use panic.
type ciFake struct {
	watchcmd.Reader
	mu       sync.Mutex
	branches map[string]string // branch name -> body
	runs     map[string]string // sha -> check_runs body
	stats    map[string]string // sha -> statuses body
	flows    map[string]string // workflow@sha -> workflow runs body
	errs     map[string]error  // "branch NAME", "flow WORKFLOW@sha"
	calls    []string
}

func newCIFake() *ciFake {
	return &ciFake{branches: map[string]string{}, runs: map[string]string{}, stats: map[string]string{}, flows: map[string]string{}, errs: map[string]error{}}
}

func (f *ciFake) note(call string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
	return f.errs[call]
}

func (f *ciFake) Branch(_ context.Context, repo, name, etag string) (hosting.WatchResponse, error) {
	if err := f.note("branch " + name); err != nil {
		return hosting.WatchResponse{}, err
	}
	if etag != "" {
		return hosting.WatchResponse{}, errors.New("a status read keeps no validators")
	}
	body, ok := f.branches[name]
	if !ok {
		return hosting.WatchResponse{}, &hosting.WatchError{Code: hosting.CodeForbidden, Status: 404}
	}
	return hosting.WatchResponse{Status: 200, Body: []byte(body)}, nil
}

func (f *ciFake) CheckRuns(_ context.Context, repo, sha string, page int, _ string) (hosting.WatchResponse, error) {
	if err := f.note("check-runs " + sha); err != nil {
		return hosting.WatchResponse{}, err
	}
	body := f.runs[sha]
	if body == "" {
		body = `{"total_count":0,"check_runs":[]}`
	}
	return hosting.WatchResponse{Status: 200, Body: []byte(body)}, nil
}

func (f *ciFake) Statuses(_ context.Context, repo, sha string, page int, _ string) (hosting.WatchResponse, error) {
	if err := f.note("statuses " + sha); err != nil {
		return hosting.WatchResponse{}, err
	}
	body := f.stats[sha]
	if body == "" {
		body = `[]`
	}
	return hosting.WatchResponse{Status: 200, Body: []byte(body)}, nil
}

func (f *ciFake) WorkflowRuns(_ context.Context, repo, workflow, sha string) (hosting.WatchResponse, error) {
	key := "flow " + workflow + "@" + sha
	if err := f.note(key); err != nil {
		return hosting.WatchResponse{}, err
	}
	body, ok := f.flows[workflow+"@"+sha]
	if !ok {
		body = `{"total_count":0,"workflow_runs":[]}`
	}
	return hosting.WatchResponse{Status: 200, Body: []byte(body)}, nil
}

func (f *ciFake) count(prefix string) int {
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func ciBranch(sha string) string { return `{"name":"main","commit":{"sha":"` + sha + `"}}` }

func ciCheckRun(status, conclusion string) string {
	c := "null"
	if conclusion != "" {
		c = `"` + conclusion + `"`
	}
	return `{"total_count":1,"check_runs":[{"name":"test","status":"` + status + `","conclusion":` + c + `,"output":{}}]}`
}

func ciErrState(code string) overview.SourceState {
	return overview.SourceState{Outcome: overview.OutcomeError, Code: code}
}

func TestReadCIMainClasses(t *testing.T) {
	head := ciSHA("a")
	for _, tc := range []struct {
		name, runs, stats, class string
	}{
		{"red", ciCheckRun("completed", "failure"), "", "red"},
		{"red commit status", ciCheckRun("completed", "success"), `[{"context":"ci","state":"failure"}]`, "red"},
		{"cancelled check run is red", ciCheckRun("completed", "cancelled"), "", "red"},
		{"pending", ciCheckRun("in_progress", ""), "", "pending"},
		{"pending commit status", "", `[{"context":"ci","state":"pending"}]`, "pending"},
		{"green", ciCheckRun("completed", "success"), "", "green"},
		{"none", "", "", "none"},
	} {
		f := newCIFake()
		f.branches["main"] = ciBranch(head)
		f.runs[head], f.stats[head] = tc.runs, tc.stats
		got := ReadCIMain(context.Background(), f, ciRepo, "main", "")
		if got.State.Outcome != overview.OutcomeOK || got.Main == nil || got.Main.Class != tc.class || got.Main.Head != head || got.Main.Branch != "main" || got.Main.BehindOrigin != nil {
			t.Errorf("%s: %+v %+v", tc.name, got.State, got.Main)
		}
	}
}

func TestReadCIMainFailuresAreStatesNotData(t *testing.T) {
	head := ciSHA("a")
	rate := &hosting.WatchError{Code: hosting.CodeRateLimited}
	for _, tc := range []struct {
		name  string
		setup func(*ciFake)
		want  overview.SourceState
	}{
		{"branch not readable", func(f *ciFake) { f.errs["branch main"] = &hosting.WatchError{Code: hosting.CodeForbidden, Status: 404} }, ciErrState("forbidden")},
		{"rate limited", func(f *ciFake) { f.errs["branch main"] = rate }, ciErrState("rate-limited")},
		{"checks fail", func(f *ciFake) { f.errs["check-runs "+head] = &hosting.WatchError{Code: hosting.CodeHTTP5xx} }, ciErrState("http-5xx")},
		{"branch body is not usable", func(f *ciFake) { f.branches["main"] = `{"commit":{"sha":"x"}}` }, ciErrState("malformed-response")},
		{"not a watch error is a network error", func(f *ciFake) { f.errs["branch main"] = errors.New("boom") }, ciErrState("network")},
	} {
		f := newCIFake()
		f.branches["main"] = ciBranch(head)
		tc.setup(f)
		got := ReadCIMain(context.Background(), f, ciRepo, "main", "")
		if got.State != tc.want || got.Main != nil {
			t.Errorf("%s: %+v %+v", tc.name, got.State, got.Main)
		}
	}
}

func TestReadCIMainDeadlineIsASkipNotAFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := newCIFake()
	f.errs["branch main"] = &hosting.WatchError{Code: hosting.CodeNetwork, Message: "request canceled or exceeded its deadline"}
	got := ReadCIMain(ctx, f, ciRepo, "main", "")
	if got.State != (overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: overview.ReasonDeadline}) || got.Main != nil {
		t.Fatalf("%+v %+v", got.State, got.Main)
	}
	// a failure with its own code stays a failure even when the run's time is also up
	f.errs["branch main"] = &hosting.WatchError{Code: hosting.CodeRateLimited}
	if limited := ReadCIMain(ctx, f, ciRepo, "main", ""); limited.State != ciErrState("rate-limited") {
		t.Fatalf("%+v", limited.State)
	}
	f.errs["branch main"] = &hosting.WatchError{Code: hosting.CodeNetwork}
	// a request that timed out on its own while the run still has time is a failed read
	live := ReadCIMain(context.Background(), f, ciRepo, "main", "")
	if live.State != ciErrState("network") {
		t.Fatalf("%+v", live.State)
	}
}

// ciGit runs Git in dir with a neutral configuration and identity.
func ciGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func ciCommit(t *testing.T, dir, content string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	ciGit(t, dir, "add", "f.txt")
	ciGit(t, dir, "commit", "-q", "-m", content)
	return ciGit(t, dir, "rev-parse", "HEAD")
}

func TestReadCIMainBehindOriginComesFromTheLocalStore(t *testing.T) {
	dir := t.TempDir()
	ciGit(t, dir, "init", "-q", "-b", "main")
	have := ciCommit(t, dir, "one")
	missing := ciSHA("e") // origin has a commit the clone has never seen
	for _, tc := range []struct {
		name, head string
		want       bool
	}{
		{"origin head is in the local store", have, false},
		{"origin head is not in the local store", missing, true},
	} {
		f := newCIFake()
		f.branches["main"] = ciBranch(tc.head)
		got := ReadCIMain(context.Background(), f, ciRepo, "main", dir)
		if got.Main == nil || got.Main.BehindOrigin == nil || *got.Main.BehindOrigin != tc.want {
			t.Errorf("%s: %+v", tc.name, got.Main)
		}
		if f.count("flow") != 0 || f.count("branch") != 1 {
			t.Errorf("%s: requests %v", tc.name, f.calls)
		}
	}
	// no local clone, or no such local branch: undecided, never a guess
	f := newCIFake()
	f.branches["main"] = ciBranch(have)
	for name, d := range map[string]string{"no clone": "", "empty directory": t.TempDir()} {
		got := ReadCIMain(context.Background(), f, ciRepo, "main", d)
		if got.Main == nil || got.Main.BehindOrigin != nil {
			t.Errorf("%s: %+v", name, got.Main)
		}
	}
}

func ciContractText(product, template, pattern, workflow string) string {
	line := ""
	if product != "" {
		line = "product = \"" + product + "\"\n"
	}
	return `schema_version = 1

[project]
name = "` + product + `x"
` + line + `
[version]
scheme = "pattern"
source = "input"
tag_template = "` + template + `"
tag_pattern = '` + pattern + `'
order = ["major", "minor", "patch"]

[[build]]
id = "pack"
scope = "once"
argv = ["make", "dist"]
timeout_seconds = 60

[[target]]
id = "archive"
kind = "file"
build = "pack"
os = "any"
arch = "any"
output = "dist/release/` + product + `x-{version_number}.tar.gz"

[outputs]
checksums = "SHA256SUMS"

[reproducibility]
policy = "required"
source_date_epoch = "commit-time"

[allowlist]
paths = ["docs/**"]

[ci]
workflows = ["ci.yml"]
release_workflow = "` + workflow + `"
`
}

func ciProductPattern(prefix string) string {
	return `^` + prefix + `-v(?P<version>(?P<major>[0-9]{1,9})\.(?P<minor>[0-9]{1,9})\.(?P<patch>[0-9]{1,9}))$`
}

func ciWriteContract(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".stw"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".stw", name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func ciRunsBody(runs ...string) string {
	return fmt.Sprintf(`{"total_count":%d,"workflow_runs":[%s]}`, len(runs), strings.Join(runs, ","))
}

func ciRun(status, conclusion, created string, n int) string {
	c := "null"
	if conclusion != "" {
		c = `"` + conclusion + `"`
	}
	return fmt.Sprintf(`{"status":"%s","conclusion":%s,"html_url":"https://github.com/%s/actions/runs/%d","created_at":"%s"}`, status, c, ciRepo, n, created)
}

func ciTime(hour int) string {
	return time.Date(2026, 10, 22, hour, 0, 0, 0, time.UTC).Format(time.RFC3339)
}

func ciTags(names ...string) []CITag {
	tags := make([]CITag, len(names))
	for i, n := range names {
		tags[i] = CITag{Name: n, SHA: ciSHA(string(rune('a' + i)))}
	}
	return tags
}

func TestReadCIReleaseDefaultsToReleaseYmlAndTheLatestSemverTag(t *testing.T) {
	f := newCIFake()
	tags := ciTags("v1.9.0", "v1.10.0", "v0.2.0", "nightly", "1.2", "v1.10.0-rc1")
	latest := tags[1]
	f.flows["release.yml@"+latest.SHA] = ciRunsBody(ciRun("completed", "failure", ciTime(10), 2), ciRun("completed", "success", ciTime(9), 1))
	got := ReadCIRelease(context.Background(), f, CIReleaseInput{Repo: ciRepo, Tags: tags})
	if got.State.Outcome != overview.OutcomeOK || got.Release == nil {
		t.Fatalf("%+v", got.State)
	}
	want := CIReleaseTag{Tag: "v1.10.0", SHA: latest.SHA, Released: true, Runs: []overview.Run{
		{Workflow: "release.yml", Conclusion: "failure", Status: "completed", URL: "https://github.com/" + ciRepo + "/actions/runs/2"},
		{Workflow: "release.yml", Conclusion: "success", Status: "completed", URL: "https://github.com/" + ciRepo + "/actions/runs/1"},
	}}
	if got.Release.Workflow != "release.yml" || len(got.Release.Tags) != 1 || fmt.Sprint(got.Release.Tags[0]) != fmt.Sprint(want) {
		t.Fatalf("%+v", got.Release)
	}
	if len(f.calls) != 1 || f.calls[0] != "flow release.yml@"+latest.SHA {
		t.Fatalf("requests %v", f.calls)
	}
}

func TestReadCIReleaseEqualVersionsResolveWhateverTheOrderRead(t *testing.T) {
	for _, names := range [][]string{{"1.0.0", "v1.0.0"}, {"v1.0.0", "1.0.0"}} {
		tags := ciTags(names...)
		got := ReadCIRelease(context.Background(), newCIFake(), CIReleaseInput{Repo: ciRepo, Tags: tags})
		if got.Release == nil || len(got.Release.Tags) != 1 || got.Release.Tags[0].Tag != "v1.0.0" {
			t.Errorf("%v: %+v", names, got.Release)
		}
	}
}

func TestReadCIReleaseUsesOnlyTheDeclaredWorkflow(t *testing.T) {
	dir := t.TempDir()
	ciWriteContract(t, dir, "release.toml", ciContractText("", "v{version}", `^v(?P<version>(?P<major>[0-9]{1,9})\.(?P<minor>[0-9]{1,9})\.(?P<patch>[0-9]{1,9}))$`, ".github/workflows/publish.yaml"))
	f := newCIFake()
	tags := ciTags("v1.0.0")
	f.flows["publish.yaml@"+tags[0].SHA] = ciRunsBody(ciRun("completed", "success", ciTime(9), 1))
	got := ReadCIRelease(context.Background(), f, CIReleaseInput{Repo: ciRepo, Dir: dir, Tags: tags})
	if got.Release == nil || got.Release.Workflow != "publish.yaml" || got.Release.Tags[0].Runs[0].Workflow != "publish.yaml" {
		t.Fatalf("%+v %+v", got.State, got.Release)
	}
	if len(f.calls) != 1 || f.calls[0] != "flow publish.yaml@"+tags[0].SHA {
		t.Fatalf("a workflow that is not declared was read: %v", f.calls)
	}
}

func TestReadCIReleaseReadsAJSONContract(t *testing.T) {
	dir := t.TempDir()
	ciWriteContract(t, dir, "release.json", `{"schema_version":1,"project":{"name":"tool"},
	"version":{"scheme":"semver","source":"input"},
	"build":[{"id":"pack","scope":"once","argv":["make","dist"],"timeout_seconds":60}],
	"target":[{"id":"archive","kind":"file","build":"pack","os":"any","arch":"any","output":"dist/release/tool-{version_number}.tar.gz"}],
	"outputs":{"checksums":"SHA256SUMS"},"reproducibility":{"policy":"required","source_date_epoch":"commit-time"},
	"allowlist":{"paths":["docs/**"]},"ci":{"workflows":["ci.yml"],"release_workflow":".github/workflows/ship.yml"}}`)
	tags := ciTags("v1.0.0", "1.2.0")
	f := newCIFake()
	got := ReadCIRelease(context.Background(), f, CIReleaseInput{Repo: ciRepo, Dir: dir, Tags: tags})
	// the declared semver grammar requires the `v`: the default grammar's optional `v` does not apply
	if got.Release == nil || got.Release.Workflow != "ship.yml" || len(got.Release.Tags) != 1 || got.Release.Tags[0].Tag != "v1.0.0" {
		t.Fatalf("%+v %+v", got.State, got.Release)
	}
}

func TestReadCIReleaseAContractThatDeclaresNothingUsableFallsBackToTheDefault(t *testing.T) {
	broken := ciContractText("", "v{version}", `^v(?P<version>(?P<major>[0-9]{1,9})\.(?P<minor>[0-9]{1,9})\.(?P<patch>[0-9]{1,9}))$`, ".github/workflows/publish.yaml")
	for name, files := range map[string]map[string]string{
		"not a contract":     {"release.toml": "not toml ["},
		"invalid contract":   {"release.toml": strings.Replace(broken, "schema_version = 1", "schema_version = 7", 1)},
		"yaml is not a form": {"release.yml": "schema_version: 1"},
		"unrelated file":     {"notes.txt": broken},
	} {
		dir := t.TempDir()
		for file, content := range files {
			ciWriteContract(t, dir, file, content)
		}
		tags := ciTags("v1.0.0")
		f := newCIFake()
		got := ReadCIRelease(context.Background(), f, CIReleaseInput{Repo: ciRepo, Dir: dir, Tags: tags})
		if got.Release == nil || got.Release.Workflow != "release.yml" || len(f.calls) != 1 || f.calls[0] != "flow release.yml@"+tags[0].SHA {
			t.Errorf("%s: %+v %v", name, got.Release, f.calls)
		}
	}
	// a contract that is a link is never followed
	dir := t.TempDir()
	ciWriteContract(t, dir, "real.txt", broken)
	if err := os.Symlink("real.txt", filepath.Join(dir, ".stw", "release.toml")); err != nil {
		t.Fatal(err)
	}
	f := newCIFake()
	if got := ReadCIRelease(context.Background(), f, CIReleaseInput{Repo: ciRepo, Dir: dir, Tags: ciTags("v1.0.0")}); got.Release == nil || got.Release.Workflow != "release.yml" {
		t.Errorf("a linked contract was read: %+v", got.Release)
	}
}

func TestReadCIReleaseLatestTagPerDeclaredProduct(t *testing.T) {
	dir := t.TempDir()
	ciWriteContract(t, dir, "release.zlib.toml", ciContractText("zlib", "zlib-v{version}", ciProductPattern("zlib"), ".github/workflows/release.yml"))
	ciWriteContract(t, dir, "release.busybox.toml", ciContractText("busybox", "busybox-v{version}", ciProductPattern("busybox"), ".github/workflows/release.yml"))
	tags := ciTags("zlib-v1.2.9", "zlib-v1.2.10", "busybox-v1.36.0", "busybox-v1.35.0", "v9.9.9")
	f := newCIFake()
	got := ReadCIRelease(context.Background(), f, CIReleaseInput{Repo: ciRepo, Dir: dir, Tags: tags})
	if got.Release == nil || len(got.Release.Tags) != 2 {
		t.Fatalf("%+v %+v", got.State, got.Release)
	}
	// products in the order of their ids, the default pattern is not used when products are declared
	if a, b := got.Release.Tags[0], got.Release.Tags[1]; a.Product != "busybox" || a.Tag != "busybox-v1.36.0" || b.Product != "zlib" || b.Tag != "zlib-v1.2.10" || !a.Released || !b.Released {
		t.Fatalf("%+v", got.Release.Tags)
	}
	if f.count("flow") != 2 {
		t.Fatalf("requests %v", f.calls)
	}
}

func TestReadCIReleaseAddsUnreleasedTagsOnceAndCapsThemAtFive(t *testing.T) {
	names := []string{"v1.0.0", "v1.1.0", "v1.2.0", "v1.3.0", "v1.4.0", "v1.5.0", "v1.6.0", "v1.7.0"}
	tags := ciTags(names...)
	summary := func(in CIReleaseInput) ([]string, *ciFake) {
		f := newCIFake()
		got := ReadCIRelease(context.Background(), f, in)
		if got.Release == nil {
			t.Fatalf("%+v", got.State)
		}
		var seen []string
		for _, e := range got.Release.Tags {
			seen = append(seen, fmt.Sprintf("%s:%v", e.Tag, e.Released))
		}
		return seen, f
	}
	// the latest tag is also unreleased: one entry, not released, and five unreleased tags in all
	seen, f := summary(CIReleaseInput{Repo: ciRepo, Tags: tags, Unreleased: []string{"v1.7.0", "v1.6.0", "v1.5.0", "v1.4.0", "v1.3.0", "v1.2.0", "v1.1.0", "ghost"}})
	if got, want := strings.Join(seen, " "), "v1.7.0:false v1.6.0:false v1.5.0:false v1.4.0:false v1.3.0:false"; got != want || f.count("flow") != 5 {
		t.Fatalf("%v %v", seen, f.calls)
	}
	// the latest tag is released: it and five unreleased older tags make the six of a repository
	seen, f = summary(CIReleaseInput{Repo: ciRepo, Tags: tags, Unreleased: []string{"v1.6.0", "v1.5.0", "v1.4.0", "v1.3.0", "v1.2.0", "v1.1.0", "v1.0.0"}})
	if got, want := strings.Join(seen, " "), "v1.7.0:true v1.6.0:false v1.5.0:false v1.4.0:false v1.3.0:false v1.2.0:false"; got != want || f.count("flow") != 6 {
		t.Fatalf("%v %v", seen, f.calls)
	}
}

func TestReadCIReleaseKeepsAtMost20RunsNewestFirst(t *testing.T) {
	var runs []string
	for i := 1; i <= 25; i++ { // the forge's order is not trusted: oldest first here
		runs = append(runs, ciRun("completed", "success", time.Date(2026, 10, 1, i, 0, 0, 0, time.UTC).Format(time.RFC3339), i))
	}
	tags := ciTags("v1.0.0")
	f := newCIFake()
	f.flows["release.yml@"+tags[0].SHA] = ciRunsBody(runs...)
	got := ReadCIRelease(context.Background(), f, CIReleaseInput{Repo: ciRepo, Tags: tags})
	kept := got.Release.Tags[0].Runs
	if len(kept) != 20 || !strings.HasSuffix(kept[0].URL, "/runs/25") || !strings.HasSuffix(kept[19].URL, "/runs/6") {
		t.Fatalf("%d runs, first %v last %v", len(kept), kept[0].URL, kept[len(kept)-1].URL)
	}
}

func TestReadCIReleaseWithoutTagsReadsNothing(t *testing.T) {
	f := newCIFake()
	got := ReadCIRelease(context.Background(), f, CIReleaseInput{Repo: ciRepo, Tags: ciTags("nightly", "1.2")})
	if got.State.Outcome != overview.OutcomeOK || got.Release == nil || len(got.Release.Tags) != 0 || len(f.calls) != 0 {
		t.Fatalf("%+v %+v %v", got.State, got.Release, f.calls)
	}
}

func TestReadCIReleaseFailuresAreStatesNotData(t *testing.T) {
	tags := ciTags("v1.0.0", "v1.1.0")
	key := "flow release.yml@" + tags[1].SHA
	notFound := &hosting.WatchError{Code: hosting.CodeForbidden, Status: 404}
	for _, tc := range []struct {
		name string
		err  error
		want overview.SourceState
	}{
		{"missing workflow is skipped, not failed", notFound, overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: overview.ReasonNotFound}},
		{"forbidden", &hosting.WatchError{Code: hosting.CodeForbidden, Status: 403}, ciErrState("forbidden")},
		{"rate limited", &hosting.WatchError{Code: hosting.CodeRateLimited}, ciErrState("rate-limited")},
		{"malformed", &hosting.WatchError{Code: hosting.CodeMalformed}, ciErrState("malformed-response")},
	} {
		f := newCIFake()
		f.errs[key] = tc.err
		got := ReadCIRelease(context.Background(), f, CIReleaseInput{Repo: ciRepo, Tags: tags, Unreleased: []string{"v1.0.0"}})
		if got.State != tc.want || got.Release != nil {
			t.Errorf("%s: %+v %+v", tc.name, got.State, got.Release)
		}
	}
	f := newCIFake()
	f.flows["release.yml@"+tags[1].SHA] = `{"workflow_runs":null}`
	if got := ReadCIRelease(context.Background(), f, CIReleaseInput{Repo: ciRepo, Tags: tags}); got.State != ciErrState("malformed-response") || got.Release != nil {
		t.Errorf("%+v %+v", got.State, got.Release)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f = newCIFake()
	f.errs[key] = &hosting.WatchError{Code: hosting.CodeNetwork}
	if got := ReadCIRelease(ctx, f, CIReleaseInput{Repo: ciRepo, Tags: tags}); got.State != (overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: overview.ReasonDeadline}) || got.Release != nil {
		t.Errorf("%+v %+v", got.State, got.Release)
	}
}

// US5: a red default branch and a failed tag run are red items of the action model; a cancelled run
// is one too, a run still in progress is not.
func TestUS5RedMainAndFailedTagRunAreRedItems(t *testing.T) {
	head := ciSHA("a")
	tags := ciTags("v1.0.0", "v1.1.0")
	for _, tc := range []struct {
		name, conclusion, status string
		wantRun                  bool
	}{
		{"failure", "failure", "completed", true},
		{"cancelled", "cancelled", "completed", true},
		{"timed out", "timed_out", "completed", true},
		{"in progress", "", "in_progress", false},
		{"success", "success", "completed", false},
	} {
		f := newCIFake()
		f.branches["main"] = ciBranch(head)
		f.runs[head] = ciCheckRun("completed", "failure")
		f.flows["release.yml@"+tags[1].SHA] = ciRunsBody(ciRun(tc.status, tc.conclusion, ciTime(10), 7))
		main := ReadCIMain(context.Background(), f, ciRepo, "main", "")
		rel := ReadCIRelease(context.Background(), f, CIReleaseInput{Repo: ciRepo, Tags: tags, Unreleased: []string{"v1.1.0"}})
		repo := overview.Repo{
			Local: stwsync.RepoStatus{Name: "sourcesteward", Path: "sourcesteward", Kind: "root", State: "ok", Reasons: []string{}},
			Slug:  ciRepo,
			Remote: &overview.Remote{
				Sources: map[overview.Source]overview.SourceState{overview.SourceCIMain: main.State, overview.SourceCIRelease: rel.State},
				Main:    main.Main,
				Tags:    rel.Release.ReleaseTags(),
			},
		}
		got := map[overview.Category]overview.Priority{}
		for _, it := range overview.Build(overview.Input{Root: "/ws", Repos: []overview.Repo{repo}}).Items {
			got[it.Category] = it.Priority
		}
		if got[overview.MainRed] != overview.Red {
			t.Errorf("%s: main-red missing: %v", tc.name, got)
		}
		_, failed := got[overview.ReleaseRunFailed]
		if failed != tc.wantRun || (failed && got[overview.ReleaseRunFailed] != overview.Red) {
			t.Errorf("%s: release-run-failed %v: %v", tc.name, failed, got)
		}
		if _, unreleased := got[overview.TagWithoutRelease]; unreleased == failed {
			t.Errorf("%s: one item per tag: %v", tc.name, got)
		}
	}
}
