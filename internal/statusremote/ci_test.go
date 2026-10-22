// SPDX-License-Identifier: EUPL-1.2
package statusremote

import (
	"context"
	"encoding/json"
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

func ciRunAt(sha, status, conclusion, created, url string) string {
	c := "null"
	if conclusion != "" {
		c = `"` + conclusion + `"`
	}
	return fmt.Sprintf(`{"status":"%s","conclusion":%s,"html_url":"%s","head_sha":"%s","created_at":"%s"}`, status, c, url, sha, created)
}

func ciRun(sha, status, conclusion, created string, n int) string {
	return ciRunAt(sha, status, conclusion, created, fmt.Sprintf("https://github.com/%s/actions/runs/%d", ciRepo, n))
}

func ciTime(hour int) string {
	return time.Date(2026, 10, 22, hour, 0, 0, 0, time.UTC).Format(time.RFC3339)
}

// ciEntry is a tag entry as the `tags` source reports it.
func ciEntry(pattern, tag, seed string, latest, unreleased bool) overview.ReleaseTag {
	return overview.ReleaseTag{Pattern: pattern, Tag: tag, SHA: ciSHA(seed), Latest: latest, Unreleased: unreleased}
}

const defaultPattern = "v*.*.*"

func TestReadCIReleaseAddsTheNewestRunsToTheGivenEntries(t *testing.T) {
	entry := ciEntry(defaultPattern, "v1.10.0", "a", true, false)
	f := newCIFake()
	f.flows["release.yml@"+entry.SHA] = ciRunsBody(ciRun(entry.SHA, "completed", "success", ciTime(9), 1), ciRun(entry.SHA, "completed", "failure", ciTime(10), 2))
	got := ReadCIRelease(context.Background(), f, CIReleaseInput{Repo: ciRepo, Tags: []overview.ReleaseTag{entry}})
	if got.State.Outcome != overview.OutcomeOK || got.Release == nil || len(got.Release.Tags) != 1 {
		t.Fatalf("%+v", got)
	}
	e := got.Release.Tags[0]
	want := []overview.Run{
		{Workflow: "release.yml", Conclusion: "failure", Status: "completed", URL: "https://github.com/" + ciRepo + "/actions/runs/2"},
		{Workflow: "release.yml", Conclusion: "success", Status: "completed", URL: "https://github.com/" + ciRepo + "/actions/runs/1"},
	}
	if e.Pattern != defaultPattern || e.Product != "" || e.Tag != "v1.10.0" || e.SHA != entry.SHA || !e.Latest || e.Unreleased || e.Workflow != "release.yml" || fmt.Sprint(e.Runs) != fmt.Sprint(want) {
		t.Fatalf("%+v", e)
	}
	if len(f.calls) != 1 || f.calls[0] != "flow release.yml@"+entry.SHA {
		t.Fatalf("requests %v", f.calls)
	}
	// the entries come back as the action model reads them, the newest run attached
	tags := got.Release.ReleaseTags()
	if len(tags) != 1 || tags[0].Run == nil || tags[0].Run.Conclusion != "failure" || tags[0].Pattern != defaultPattern || !tags[0].Latest || tags[0].SHA != entry.SHA {
		t.Fatalf("%+v", tags)
	}
}

// The entries are not recomputed: a tag that no pattern grammar of the clone would pick is read all the same.
func TestReadCIReleaseReadsTheEntriesItIsGivenAndNothingElse(t *testing.T) {
	entries := []overview.ReleaseTag{ciEntry("custom/*", "custom/odd", "a", false, true)}
	f := newCIFake()
	got := ReadCIRelease(context.Background(), f, CIReleaseInput{Repo: ciRepo, Tags: entries})
	if got.Release == nil || len(got.Release.Tags) != 1 || got.Release.Tags[0].Workflow != "release.yml" || len(f.calls) != 1 {
		t.Fatalf("%+v %v", got, f.calls)
	}
	if empty := ReadCIRelease(context.Background(), f, CIReleaseInput{Repo: ciRepo}); empty.Release == nil || len(empty.Release.Tags) != 0 || len(f.calls) != 1 {
		t.Fatalf("%+v %v", empty, f.calls)
	}
}

func ciTwoProducts(t *testing.T) ReleaseProducts {
	t.Helper()
	dir := t.TempDir()
	ciWriteContract(t, dir, "release.zlib.toml", ciContractText("zlib", "zlib-v{version}", ciProductPattern("zlib"), ".github/workflows/zlib-release.yml"))
	ciWriteContract(t, dir, "release.busybox.toml", ciContractText("busybox", "busybox-v{version}", ciProductPattern("busybox"), ".github/workflows/ship.yaml"))
	products := ReadReleaseProducts(dir)
	if products.Invalid() {
		t.Fatal("the fixture contracts are not valid")
	}
	return products
}

func TestReadReleaseProductsGivePatternsInTheOrderOfTheProductIds(t *testing.T) {
	if got := fmt.Sprint(ciTwoProducts(t).Patterns()); got != "[busybox-v* zlib-v*]" {
		t.Fatal(got)
	}
	dir := t.TempDir()
	ciWriteContract(t, dir, "release.json", `{"schema_version":1,"project":{"name":"tool"},
	"version":{"scheme":"semver","tag_prefix":"cli/","source":"input"},
	"build":[{"id":"pack","scope":"once","argv":["make","dist"],"timeout_seconds":60}],
	"target":[{"id":"archive","kind":"file","build":"pack","os":"any","arch":"any","output":"dist/release/tool-{version_number}.tar.gz"}],
	"outputs":{"checksums":"SHA256SUMS"},"reproducibility":{"policy":"required","source_date_epoch":"commit-time"},
	"allowlist":{"paths":["docs/**"]},"ci":{"workflows":["ci.yml"],"release_workflow":".github/workflows/ship.yml"}}`)
	json := ReadReleaseProducts(dir)
	if got := fmt.Sprint(json.Patterns()); got != "[cli/v*]" || json.Invalid() {
		t.Fatal(got)
	}
	for name, p := range map[string]ReleaseProducts{"no clone": ReadReleaseProducts(""), "no .stw": ReadReleaseProducts(t.TempDir())} {
		if p.Invalid() || len(p.Patterns()) != 0 {
			t.Errorf("%s: declares %v", name, p.Patterns())
		}
	}
}

// Each entry uses the workflow of its own product; the default workflow serves a pattern nobody declares.
func TestReadCIReleaseUsesTheWorkflowOfTheProductOfEachEntry(t *testing.T) {
	entries := []overview.ReleaseTag{
		ciEntry("busybox-v*", "busybox-v1.36.0", "a", true, false),
		ciEntry("zlib-v*", "zlib-v1.3.1", "b", true, false),
		ciEntry(defaultPattern, "v2.0.0", "c", true, false),
	}
	f := newCIFake()
	got := ReadCIRelease(context.Background(), f, CIReleaseInput{Repo: ciRepo, Products: ciTwoProducts(t), Tags: entries})
	want := []string{"flow ship.yaml@" + entries[0].SHA, "flow zlib-release.yml@" + entries[1].SHA, "flow release.yml@" + entries[2].SHA}
	if fmt.Sprint(f.calls) != fmt.Sprint(want) {
		t.Fatalf("requests %v", f.calls)
	}
	var seen []string
	for _, e := range got.Release.Tags {
		seen = append(seen, e.Product+":"+e.Workflow)
	}
	if fmt.Sprint(seen) != "[busybox:ship.yaml zlib:zlib-release.yml :release.yml]" {
		t.Fatal(seen)
	}
}

// The latest tag of every pattern is read, however many products there are, and at most five unreleased
// tags besides: N+5 runs reads of page 1.
func TestReadCIReleaseReadsEveryLatestTagAndAtMostFiveUnreleased(t *testing.T) {
	var entries []overview.ReleaseTag
	for i := 0; i < 8; i++ {
		entries = append(entries, ciEntry(fmt.Sprintf("p%d-v*", i), fmt.Sprintf("p%d-v1.0.0", i), string(rune('a'+i)), true, false))
	}
	for i := 0; i < 7; i++ { // the cap is the tags source's; the reader keeps it as well
		entries = append(entries, ciEntry("p0-v*", fmt.Sprintf("p0-v0.%d.0", i), string(rune('k'+i)), false, true))
	}
	f := newCIFake()
	got := ReadCIRelease(context.Background(), f, CIReleaseInput{Repo: ciRepo, Tags: entries})
	if len(f.calls) != 13 || len(got.Release.Tags) != 13 {
		t.Fatalf("%d requests: %v", len(f.calls), f.calls)
	}
	for i := 0; i < 8; i++ {
		if !got.Release.Tags[i].Latest || got.Release.Tags[i].Tag != fmt.Sprintf("p%d-v1.0.0", i) {
			t.Errorf("latest tag %d: %+v", i, got.Release.Tags[i])
		}
	}
	if last := got.Release.Tags[12]; last.Tag != "p0-v0.4.0" || !last.Unreleased {
		t.Errorf("%+v", last)
	}
}

// A tag that is the latest and unreleased uses one of the five.
func TestReadCIReleaseCountsALatestUnreleasedTagAmongTheFive(t *testing.T) {
	entries := []overview.ReleaseTag{ciEntry("a-v*", "a-v1.0.0", "a", true, true)}
	for i := 0; i < 5; i++ {
		entries = append(entries, ciEntry("a-v*", fmt.Sprintf("a-v0.%d.0", i), string(rune('k'+i)), false, true))
	}
	// the latest tag of a later pattern is read although the five unreleased tags are used up
	entries = append(entries, ciEntry("b-v*", "b-v1.0.0", "z", true, false))
	f := newCIFake()
	got := ReadCIRelease(context.Background(), f, CIReleaseInput{Repo: ciRepo, Tags: entries})
	if len(got.Release.Tags) != 6 || got.Release.Tags[4].Tag != "a-v0.3.0" || got.Release.Tags[5].Tag != "b-v1.0.0" {
		t.Fatalf("%d: %v", len(got.Release.Tags), f.calls)
	}
}

func TestReadCIReleaseKeepsAtMost20RunsNewestFirst(t *testing.T) {
	entry := ciEntry(defaultPattern, "v1.0.0", "a", true, false)
	var runs []string
	for i := 1; i <= 25; i++ { // the forge's order is not trusted: oldest first here
		runs = append(runs, ciRun(entry.SHA, "completed", "success", time.Date(2026, 10, 1, i, 0, 0, 0, time.UTC).Format(time.RFC3339), i))
	}
	f := newCIFake()
	f.flows["release.yml@"+entry.SHA] = ciRunsBody(runs...)
	got := ReadCIRelease(context.Background(), f, CIReleaseInput{Repo: ciRepo, Tags: []overview.ReleaseTag{entry}})
	kept := got.Release.Tags[0].Runs
	if len(kept) != 20 || !strings.HasSuffix(kept[0].URL, "/runs/25") || !strings.HasSuffix(kept[19].URL, "/runs/6") {
		t.Fatalf("%d runs, first %v last %v", len(kept), kept[0].URL, kept[len(kept)-1].URL)
	}
}

// A run of another commit is not a run of the tag.
func TestReadCIReleaseIgnoresRunsOfOtherCommits(t *testing.T) {
	entry := ciEntry(defaultPattern, "v1.0.0", "a", true, false)
	other := ciSHA("b")
	f := newCIFake()
	f.flows["release.yml@"+entry.SHA] = ciRunsBody(
		ciRun(other, "completed", "failure", ciTime(11), 3),
		ciRun("", "completed", "failure", ciTime(12), 4),
		ciRun(entry.SHA, "completed", "success", ciTime(9), 1))
	got := ReadCIRelease(context.Background(), f, CIReleaseInput{Repo: ciRepo, Tags: []overview.ReleaseTag{entry}})
	runs := got.Release.Tags[0].Runs
	if len(runs) != 1 || runs[0].Conclusion != "success" {
		t.Fatalf("%+v", runs)
	}
}

// Forge values are data: a page outside the repository is dropped, an unknown status or conclusion is
// information only, and a conclusion of a run that has not completed means nothing.
func TestReadCIReleaseDoesNotTrustForgeValues(t *testing.T) {
	entry := ciEntry(defaultPattern, "v1.0.0", "a", true, false)
	good := "https://github.com/" + ciRepo + "/actions/runs/1"
	for _, tc := range []struct {
		name, status, conclusion, url string
		wantStatus, wantConclusion    string
		wantURL                       string
	}{
		{"plain run", "completed", "failure", good, "completed", "failure", good},
		{"repository case is not significant", "completed", "success", "https://github.com/WStein/SourceSteward/actions/runs/1", "completed", "success", "https://github.com/WStein/SourceSteward/actions/runs/1"},
		{"http", "completed", "success", "http://github.com/" + ciRepo + "/actions/runs/1", "completed", "success", ""},
		{"other host", "completed", "success", "https://evil.example/" + ciRepo + "/actions/runs/1", "completed", "success", ""},
		{"host as user info", "completed", "success", "https://github.com@evil.example/" + ciRepo + "/actions/runs/1", "completed", "success", ""},
		{"user info", "completed", "success", "https://user:pass@github.com/" + ciRepo + "/actions/runs/1", "completed", "success", ""},
		{"javascript", "completed", "success", "javascript:alert(1)", "completed", "success", ""},
		{"other repository", "completed", "success", "https://github.com/evil/repo/actions/runs/1", "completed", "success", ""},
		{"repository prefix only", "completed", "success", "https://github.com/" + ciRepo + "-evil/actions/runs/1", "completed", "success", ""},
		{"traversal", "completed", "success", "https://github.com/" + ciRepo + "/../evil/repo", "completed", "success", ""},
		{"empty", "completed", "success", "", "completed", "success", ""},
		{"unknown conclusion", "completed", "exploded", good, "completed", "", good},
		{"unknown status", "teleporting", "failure", good, "", "", good},
		{"control characters", "completed\u001b[31m", "failure\u0007", good, "", "", good},
		{"conclusion without completion", "in_progress", "failure", good, "in_progress", "", good},
	} {
		f := newCIFake()
		body, err := json.Marshal(map[string]any{"workflow_runs": []map[string]string{{"status": tc.status, "conclusion": tc.conclusion, "html_url": tc.url, "head_sha": entry.SHA, "created_at": ciTime(9)}}})
		if err != nil {
			t.Fatal(err)
		}
		f.flows["release.yml@"+entry.SHA] = string(body)
		got := ReadCIRelease(context.Background(), f, CIReleaseInput{Repo: ciRepo, Tags: []overview.ReleaseTag{entry}})
		if got.Release == nil || len(got.Release.Tags[0].Runs) != 1 {
			t.Errorf("%s: %+v", tc.name, got)
			continue
		}
		run := got.Release.Tags[0].Runs[0]
		if run.Status != tc.wantStatus || run.Conclusion != tc.wantConclusion || run.URL != tc.wantURL {
			t.Errorf("%s: %+v", tc.name, run)
		}
	}
}

func TestReadCIReleaseFailuresAreStatesNotData(t *testing.T) {
	entries := []overview.ReleaseTag{ciEntry(defaultPattern, "v1.1.0", "a", true, false), ciEntry(defaultPattern, "v1.0.0", "b", false, true)}
	key := "flow release.yml@" + entries[1].SHA
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
		got := ReadCIRelease(context.Background(), f, CIReleaseInput{Repo: ciRepo, Tags: entries})
		if got.State != tc.want || got.Release != nil {
			t.Errorf("%s: %+v %+v", tc.name, got.State, got.Release)
		}
	}
	f := newCIFake()
	f.flows["release.yml@"+entries[1].SHA] = `{"workflow_runs":null}`
	if got := ReadCIRelease(context.Background(), f, CIReleaseInput{Repo: ciRepo, Tags: entries}); got.State != ciErrState("malformed-response") || got.Release != nil {
		t.Errorf("%+v %+v", got.State, got.Release)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f = newCIFake()
	f.errs[key] = &hosting.WatchError{Code: hosting.CodeNetwork}
	if got := ReadCIRelease(ctx, f, CIReleaseInput{Repo: ciRepo, Tags: entries}); got.State != (overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: overview.ReasonDeadline}) || got.Release != nil {
		t.Errorf("%+v %+v", got.State, got.Release)
	}
}

// An absent contract uses the defaults; one that exists but cannot be used is an error, not a fallback.
func TestReadCIReleaseAnUnusableContractIsAnErrorNotADefault(t *testing.T) {
	valid := ciContractText("", "v{version}", `^v(?P<version>(?P<major>[0-9]{1,9})\.(?P<minor>[0-9]{1,9})\.(?P<patch>[0-9]{1,9}))$`, ".github/workflows/publish.yaml")
	entries := []overview.ReleaseTag{ciEntry(defaultPattern, "v1.0.0", "a", true, false)}
	for name, files := range map[string]map[string]string{
		"not a contract":      {"release.toml": "not toml ["},
		"invalid contract":    {"release.toml": strings.Replace(valid, "schema_version = 1", "schema_version = 7", 1)},
		"yaml form":           {"release.yml": "schema_version: 1"},
		"default mixed named": {"release.toml": valid, "release.other.toml": valid},
	} {
		dir := t.TempDir()
		for file, content := range files {
			ciWriteContract(t, dir, file, content)
		}
		f := newCIFake()
		products := ReadReleaseProducts(dir)
		got := ReadCIRelease(context.Background(), f, CIReleaseInput{Repo: ciRepo, Products: products, Tags: entries})
		if !products.Invalid() || got.State != ciErrState(CodeContractInvalid) || got.Release != nil || len(f.calls) != 0 {
			t.Errorf("%s: %+v %v", name, got.State, f.calls)
		}
	}
	// a contract that is a link is never followed
	dir := t.TempDir()
	ciWriteContract(t, dir, "real.txt", valid)
	if err := os.Symlink("real.txt", filepath.Join(dir, ".stw", "release.toml")); err != nil {
		t.Fatal(err)
	}
	if !ReadReleaseProducts(dir).Invalid() {
		t.Error("a linked contract was read")
	}
	// a file of .stw that is no contract is not a contract; no .stw at all declares nothing
	ciWriteContract(t, dir, "notes.txt", "x")
	for _, p := range []ReleaseProducts{ReadReleaseProducts(t.TempDir()), {}} {
		f := newCIFake()
		got := ReadCIRelease(context.Background(), f, CIReleaseInput{Repo: ciRepo, Products: p, Tags: entries})
		if got.State.Outcome != overview.OutcomeOK || len(f.calls) != 1 || f.calls[0] != "flow release.yml@"+entries[0].SHA {
			t.Errorf("%+v %v", got.State, f.calls)
		}
	}
}

// US5: a red default branch and a failed tag run are red items of the action model; a cancelled run
// is one too, a run still in progress is not.
func TestUS5RedMainAndFailedTagRunAreRedItems(t *testing.T) {
	head := ciSHA("a")
	entries := []overview.ReleaseTag{ciEntry(defaultPattern, "v1.0.0", "b", false, false), ciEntry(defaultPattern, "v1.1.0", "c", true, true)}
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
		f.flows["release.yml@"+entries[1].SHA] = ciRunsBody(ciRun(entries[1].SHA, tc.status, tc.conclusion, ciTime(10), 7))
		main := ReadCIMain(context.Background(), f, ciRepo, "main", "")
		rel := ReadCIRelease(context.Background(), f, CIReleaseInput{Repo: ciRepo, Tags: entries})
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
