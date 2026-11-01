// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/conflicts"
	"github.com/wstein/sourcesteward/internal/pulls"
	"github.com/wstein/sourcesteward/internal/pullscmd"
)

const pullsSearch = "search/issues?q=user%3Awstein+is%3Apr+is%3Aopen+archived%3Afalse&per_page=100&page=1&sort=created&order=asc"

// pullsForge serves two ready-looking pull requests in o/r (one by a bot) and, for the owner
// mode, one in wstein/lab found by search.
func pullsForge(t *testing.T) *watchForge {
	t.Helper()
	f := &watchForge{t: t, docs: map[string]string{}}
	head1, head2, head3 := strings.Repeat("1", 40), strings.Repeat("2", 40), strings.Repeat("3", 40)
	item := func(n int, user, kind, head string) string {
		return `{"number":` + string(rune('0'+n)) + `,"title":"PR ` + string(rune('0'+n)) + `","user":{"login":"` + user + `","type":"` + kind + `"},"labels":[],"draft":false,"created_at":"2026-10-20T09:00:00Z","html_url":"https://github.com/x/y/pull/` + string(rune('0'+n)) + `","requested_reviewers":[],"requested_teams":[],"head":{"sha":"` + head + `","ref":"feat"},"base":{"sha":"` + strings.Repeat("b", 40) + `","ref":"main"}}`
	}
	detail := func(repo, head string) string {
		return `{"mergeable":true,"mergeable_state":"clean","rebaseable":true,"head":{"sha":"` + head + `","repo":{"full_name":"` + repo + `"}},"base":{"repo":{"full_name":"` + repo + `"}}}`
	}
	green := `{"total_count":1,"check_runs":[{"name":"ci","status":"completed","conclusion":"success","output":{}}]}`
	f.docs["repos/o/r/pulls?state=open&per_page=25&page=1"] = "[" + item(1, "alice", "User", head1) + "," + item(2, "dependabot[bot]", "Bot", head2) + "]"
	f.docs["repos/o/r/pulls/1"], f.docs["repos/o/r/pulls/2"] = detail("o/r", head1), detail("o/r", head2)
	f.docs["repos/wstein/lab/pulls?state=open&per_page=25&page=1"] = "[" + item(3, "bob", "User", head3) + "]"
	f.docs["repos/wstein/lab/pulls/3"] = detail("wstein/lab", head3)
	for _, h := range []struct{ repo, head string }{{"o/r", head1}, {"o/r", head2}, {"wstein/lab", head3}} {
		f.docs["repos/"+h.repo+"/commits/"+h.head+"/check-runs?per_page=100&page=1"] = green
		f.docs["repos/"+h.repo+"/commits/"+h.head+"/statuses?per_page=100&page=1"] = `[]`
	}
	// Spec 046: the default branch, the comments of every pull request and the head commit date.
	for _, repo := range []string{"o/r", "wstein/lab"} {
		f.docs["repos/"+repo] = `{"default_branch":"main","html_url":"https://github.com/` + repo + `"}`
	}
	clear1 := `[{"id":9,"html_url":"https://github.com/o/r/pull/1#issuecomment-9","created_at":"2026-10-21T09:00:00Z","updated_at":"2026-10-21T09:00:00Z","author_association":"OWNER","body":"Review: CLEAR head=` + head1 + ` patch-id=` + strings.Repeat("a", 40) + ` pr=1\nok"}]`
	f.docs["repos/o/r/issues/1/comments?per_page=50&page=1"] = clear1
	f.docs["repos/o/r/issues/2/comments?per_page=50&page=1"] = `[]`
	f.docs["repos/wstein/lab/issues/3/comments?per_page=50&page=1"] = `[]`
	f.docs["repos/o/r/commits/"+head1] = `{"commit":{"committer":{"date":"2026-10-20T08:00:00Z"}}}`
	f.docs[pullsSearch] = `{"total_count":1,"incomplete_results":false,"items":[{"repository_url":"https://api.github.com/repos/wstein/lab","number":3}]}`
	return f
}

func pullsRun(t *testing.T, forge http.RoundTripper, args ...string) (int, string, string) {
	t.Helper()
	pullsTransport = forge
	pullsHook = func(p *pullscmd.Params) {}
	t.Cleanup(func() { pullsTransport, pullsHook = nil, nil })
	var stdout, stderr bytes.Buffer
	status := Run(context.Background(), "test", append([]string{"pulls"}, args...), &stdout, &stderr)
	return status, stdout.String(), stderr.String()
}

func TestPullsStrangersReviewLineDoesNotCountOverTheGuardedReader(t *testing.T) {
	for _, assoc := range []string{`,"author_association":"NONE"`, `,"author_association":"FIRST_TIME_CONTRIBUTOR"`, `,"author_association":null`, ``} {
		watchEnv(t, true)
		forge := pullsForge(t)
		key := "repos/o/r/issues/1/comments?per_page=50&page=1"
		forge.docs[key] = strings.Replace(forge.docs[key], `,"author_association":"OWNER"`, assoc, 1)
		status, out, errOut := pullsRun(t, forge, "--repo", "o/r", "--json")
		if status != 0 || errOut != "" {
			t.Fatalf("%s: %d %q", assoc, status, errOut)
		}
		var doc struct {
			Groups map[string][]map[string]any `json:"groups"`
		}
		if err := json.Unmarshal([]byte(out), &doc); err != nil {
			t.Fatal(err)
		}
		first := doc.Groups["ready"][0]
		rv, _ := first["review"].(map[string]any)
		hints := fmt.Sprint(first["hints"])
		if rv == nil || rv["state"] != "none" || rv["association"] != nil || rv["verdict"] != nil || strings.Contains(hints, "ready-mergeable") {
			t.Errorf("%q: a stranger's CLEAR line counted: %v", assoc, first)
		}
		if rv != nil && rv["ignored"] != 1.0 || !strings.Contains(hints, "foreign-review-ignored") {
			t.Errorf("%q: the ignored line is not reported: %v", assoc, first)
		}
	}
}

func TestPullsEndToEndJSONOverTheGuardedReader(t *testing.T) {
	watchEnv(t, true)
	forge := pullsForge(t)
	status, out, errOut := pullsRun(t, forge, "--repo", "o/r", "--json")
	if status != 0 || errOut != "" {
		t.Fatalf("%d %q", status, errOut)
	}
	var doc struct {
		Groups  map[string][]map[string]any `json:"groups"`
		Summary map[string]any              `json:"summary"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil || len(doc.Groups["ready"]) != 2 || doc.Summary["bots"] != 1.0 || doc.Summary["ready"] != 2.0 {
		t.Fatalf("%v %s", err, out)
	}
	if doc.Groups["ready"][0]["number"] != 1.0 { // equal created_at: repository, then number
		t.Fatalf("%v", doc.Groups["ready"])
	}
	first := doc.Groups["ready"][0]
	if rv, _ := first["review"].(map[string]any); rv == nil || rv["state"] != "clear" || rv["basis"] != "comment" || rv["association"] != "OWNER" || len(first["hints"].([]any)) != 1 || first["hints"].([]any)[0] != "ready-mergeable" {
		t.Fatalf("review of #1: %v", first)
	}
	if rv, _ := doc.Groups["ready"][1]["review"].(map[string]any); rv == nil || rv["state"] != "none" {
		t.Fatalf("review of #2: %v", doc.Groups["ready"][1])
	}
	var shapes []string
	for _, uri := range forge.log {
		if strings.Contains(uri, "/comments") || uri == "repos/o/r" || strings.HasPrefix(uri, "repos/o/r/commits/"+strings.Repeat("1", 40)) && !strings.Contains(uri, "check-runs") && !strings.Contains(uri, "statuses") {
			shapes = append(shapes, uri)
		}
	}
	if len(shapes) != 4 {
		t.Fatalf("review reads over the guarded reader: %v", shapes)
	}
	for _, uri := range forge.log {
		if strings.HasPrefix(uri, "search/") || strings.Contains(uri, "branches/") || uri == "rate_limit" || strings.Contains(uri, "state=closed") {
			t.Errorf("unexpected read %s", uri)
		}
	}
	if strings.Contains(out+errOut, watchToken) {
		t.Fatal("token in output")
	}
}

func TestPullsEndToEndOwnerModeUsesTheSearchShape(t *testing.T) {
	watchEnv(t, true)
	forge := pullsForge(t)
	status, out, errOut := pullsRun(t, forge, "--repo", "o/r", "--owner", "wstein", "--json")
	if status != 0 || errOut != "" {
		t.Fatalf("%d %q %s", status, errOut, out)
	}
	var doc struct {
		Repos   []map[string]any `json:"repos"`
		Options map[string]any   `json:"options"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil || len(doc.Repos) != 2 || doc.Repos[0]["source"] != "repo" || doc.Repos[1]["source"] != "owner" || doc.Options["scope"] != "mixed" {
		t.Fatalf("%v %s", err, out)
	}
	found := false
	for _, uri := range forge.log {
		found = found || uri == pullsSearch
	}
	if !found {
		t.Fatalf("search not requested: %v", forge.log)
	}
}

func TestPullsHumanOutput(t *testing.T) {
	watchEnv(t, true)
	status, out, _ := pullsRun(t, pullsForge(t), "--repo", "o/r", "--human", "--no-bots")
	if status != 0 || !strings.Contains(out, "ready (1)\n") || !strings.Contains(out, "o/r#1  ") || strings.Contains(out, "dependabot") || !strings.HasSuffix(out, "review: 1 clear\nhints: 1 ready-mergeable\n1 open pull request in 1 repository, 1 ready\n") {
		t.Fatalf("%d %q", status, out)
	}
}

func TestPullsExitCodes(t *testing.T) {
	watchEnv(t, true)
	forge := pullsForge(t)
	for name, args := range map[string][]string{
		"no-bots and bots-only":  {"--repo", "o/r", "--no-bots", "--bots-only"},
		"human and json":         {"--repo", "o/r", "--human", "--json"},
		"limit 0":                {"--repo", "o/r", "--limit", "0"},
		"limit 501":              {"--repo", "o/r", "--limit", "501"},
		"bad owner":              {"--owner", "a--b"},
		"bad author":             {"--repo", "o/r", "--author", "a b"},
		"bad repo":               {"--repo", "nope"},
		"unknown flag":           {"--repo", "o/r", "--nonsense"},
		"path without workspace": {"--repo", "o/r", "/tmp"},
		"path alone":             {"/nonexistent-workspace-path"},
		"two paths":              {"--workspace", "a", "b"},
	} {
		if status, out, errOut := pullsRun(t, forge, args...); status != 2 || out != "" || !strings.HasPrefix(errOut, "Error:") {
			t.Errorf("%s: %d %q %q", name, status, out, errOut)
		}
	}
	if len(forge.log) != 0 {
		t.Fatalf("a usage error reads nothing: %v", forge.log)
	}
	// a repository the forge does not know: exit 1, the others are printed
	status, out, errOut := pullsRun(t, forge, "--repo", "o/r", "--repo", "o/missing", "--json")
	if status != 1 || !strings.Contains(out, `"repos_failed":1`) || !strings.Contains(errOut, "o/missing") {
		t.Fatalf("%d %q %q", status, out, errOut)
	}
	// no token: exit 1 before any request, nothing printed
	watchEnv(t, false)
	forge = pullsForge(t)
	status, out, errOut = pullsRun(t, forge, "--repo", "o/r")
	if status != 1 || out != "" || len(forge.log) != 0 || !strings.Contains(errOut, "hosting-api-unavailable") {
		t.Fatalf("%d %q %q %v", status, out, errOut, forge.log)
	}
}

func TestPullsIsListedInHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if status := Run(context.Background(), "test", []string{"--help"}, &stdout, &stderr); status != 0 || !strings.Contains(stdout.String(), "pulls ") {
		t.Fatalf("%d %q", status, stdout.String())
	}
	stdout.Reset()
	if status := Run(context.Background(), "test", []string{"pulls", "--help"}, &stdout, &stderr); status != 0 || !strings.Contains(stdout.String(), "--bots-only") || !strings.Contains(stdout.String(), "--detail-drafts") {
		t.Fatalf("%d %q", status, stdout.String())
	}
}

func TestPullsOutsideAWorkspaceIsAUsageError(t *testing.T) {
	watchEnv(t, true)
	t.Chdir(t.TempDir())
	forge := pullsForge(t)
	status, out, errOut := pullsRun(t, forge)
	if status != 2 || out != "" || !strings.HasPrefix(errOut, "Error:") || len(forge.log) != 0 {
		t.Fatalf("%d %q %q %v", status, out, errOut, forge.log)
	}
}

func TestPullsLimitDefaultsTo200(t *testing.T) {
	watchEnv(t, true)
	status, out, _ := pullsRun(t, pullsForge(t), "--repo", "o/r", "--json")
	var doc struct {
		Options map[string]any `json:"options"`
	}
	if status != 0 || json.Unmarshal([]byte(out), &doc) != nil || doc.Options["limit"] != 200.0 {
		t.Fatalf("%d %s", status, out)
	}
}

func TestPullsMergeableOverTheGuardedReader(t *testing.T) {
	watchEnv(t, true)
	status, out, errOut := pullsRun(t, pullsForge(t), "--repo", "o/r", "--mergeable", "--human")
	if status != 0 || errOut != "" {
		t.Fatalf("%d %q", status, errOut)
	}
	if !strings.HasPrefix(out, "merge now (1)\n1. o/r#1  PR 1\n   CLEAR for head 1111111 per OWNER comment 2026-10-21T09:00Z, checks green, mergeable clean, base main\n") ||
		!strings.HasSuffix(out, "1 can be merged now\n") {
		t.Fatalf("%q", out)
	}
	if status, _, _ := pullsRun(t, pullsForge(t), "--repo", "o/r", "--mergeable", "--no-review"); status != 2 {
		t.Fatalf("exit %d", status)
	}
}

func TestPullsStackOverTheGuardedReader(t *testing.T) {
	watchEnv(t, true)
	status, out, errOut := pullsRun(t, pullsForge(t), "--repo", "o/r", "--stack", "--human")
	if status != 0 || errOut != "" || out != "no stacked pull requests\n\n2 single pull requests not shown\n" {
		t.Fatalf("%d %q %q", status, out, errOut)
	}
	if status, _, _ := pullsRun(t, pullsForge(t), "--repo", "o/r", "--stack", "--mergeable"); status != 2 {
		t.Fatalf("exit %d", status)
	}
}

func TestPullsHelpNamesTheConflictFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if status := Run(context.Background(), "test", []string{"pulls", "--help"}, &stdout, &stderr); status != 0 {
		t.Fatalf("%d %q", status, stderr.String())
	}
	for _, want := range []string{"--conflicts", "--restack-plan", "--checkout", "--remote", "refs/stw/conflicts/", "rebases and pushes nothing"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("help lacks %q", want)
		}
	}
}

func TestPullsConflictsNeedsOneRepository(t *testing.T) {
	watchEnv(t, true)
	forge := pullsForge(t)
	for _, args := range [][]string{
		{"--repo", "o/r", "--repo", "o/s", "--conflicts"},
		{"--repo", "o/r", "--owner", "wstein", "--restack-plan"},
		{"--repo", "o/r", "--checkout", "/tmp"},
	} {
		status, out, errOut := pullsRun(t, forge, args...)
		if status != 2 || out != "" || !strings.HasPrefix(errOut, "Error:") || len(forge.log) != 0 {
			t.Errorf("%v: %d %q %q %v", args, status, out, errOut, forge.log)
		}
	}
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestPullsConflictsEndToEnd probes a real temporary repository over the guarded reader: pull
// request 1 changes a line that main changed afterwards, pull request 2 is stacked on it.
func TestPullsConflictsEndToEnd(t *testing.T) {
	watchEnv(t, true)
	root := t.TempDir()
	origin, work := filepath.Join(root, "origin.git"), filepath.Join(root, "work")
	gitIn(t, root, "init", "-q", "--bare", "-b", "main", origin)
	gitIn(t, root, "init", "-q", "-b", "main", work)
	gitIn(t, work, "remote", "add", "origin", origin)
	text := func(m map[int]string) string {
		var b strings.Builder
		for i := 1; i <= 8; i++ {
			if v, ok := m[i]; ok {
				b.WriteString(v + "\n")
			} else {
				b.WriteString("line\n")
			}
		}
		return b.String()
	}
	put := func(name, body string) {
		if err := os.WriteFile(filepath.Join(work, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put("rules.md", text(nil))
	put("own.md", text(nil))
	gitIn(t, work, "add", ".")
	gitIn(t, work, "commit", "-q", "-m", "base")
	gitIn(t, work, "push", "-q", "origin", "main")
	gitIn(t, work, "checkout", "-q", "-b", "one")
	put("rules.md", text(map[int]string{3: "PR"}))
	gitIn(t, work, "commit", "-q", "-am", "pr one")
	head1 := gitIn(t, work, "rev-parse", "HEAD")
	gitIn(t, work, "push", "-q", "origin", "one", "one:refs/pull/1/head")
	gitIn(t, work, "checkout", "-q", "-b", "two")
	put("own.md", text(map[int]string{3: "PR two"}))
	gitIn(t, work, "commit", "-q", "-am", "pr two")
	head2 := gitIn(t, work, "rev-parse", "HEAD")
	gitIn(t, work, "push", "-q", "origin", "two", "two:refs/pull/2/head")
	gitIn(t, work, "checkout", "-q", "main")
	put("rules.md", text(map[int]string{3: "MAIN"}))
	gitIn(t, work, "commit", "-q", "-am", "main")
	mainCommit := gitIn(t, work, "rev-parse", "HEAD")
	gitIn(t, work, "push", "-q", "origin", "main")

	f := &watchForge{t: t, docs: map[string]string{}}
	item := func(n int, head, headRef, base string) string {
		return fmt.Sprintf(`{"number":%d,"title":"PR %d","user":{"login":"alice","type":"User"},"labels":[],"draft":false,"created_at":"2026-10-20T09:00:00Z","html_url":"https://github.com/o/r/pull/%d","requested_reviewers":[],"requested_teams":[],"head":{"sha":%q,"ref":%q},"base":{"sha":%q,"ref":%q}}`,
			n, n, n, head, headRef, strings.Repeat("b", 40), base)
	}
	detail := func(head string) string {
		return `{"mergeable":true,"mergeable_state":"clean","rebaseable":true,"head":{"sha":"` + head + `","repo":{"full_name":"o/r"}},"base":{"repo":{"full_name":"o/r"}}}`
	}
	f.docs["repos/o/r/pulls?state=open&per_page=25&page=1"] = "[" + item(1, head1, "one", "main") + "," + item(2, head2, "two", "one") + "]"
	f.docs["repos/o/r/pulls/1"], f.docs["repos/o/r/pulls/2"] = detail(head1), detail(head2)
	for _, h := range []string{head1, head2} {
		f.docs["repos/o/r/commits/"+h+"/check-runs?per_page=100&page=1"] = `{"total_count":1,"check_runs":[{"name":"ci","status":"completed","conclusion":"success","output":{}}]}`
		f.docs["repos/o/r/commits/"+h+"/statuses?per_page=100&page=1"] = `[]`
	}
	f.docs["repos/o/r"] = `{"default_branch":"main","html_url":"https://github.com/o/r"}`
	f.docs["repos/o/r/pulls?state=closed&sort=updated&direction=desc&per_page=100&page=1"] = `[]`
	f.docs["repos/o/r/issues/1/comments?per_page=50&page=1"], f.docs["repos/o/r/issues/2/comments?per_page=50&page=1"] = `[]`, `[]`

	pullsTransport = f
	pullsHook = func(p *pullscmd.Params) {
		p.NewProber = func(context.Context, pullscmd.ProberRequest) (pulls.ConflictProber, func(), error) {
			r := conflicts.NewRunner(work)
			return conflicts.Prober{Repo: "o/r", Remote: "origin", Git: r}, r.Close, nil
		}
	}
	t.Cleanup(func() { pullsTransport, pullsHook = nil, nil })
	var stdout, stderr bytes.Buffer
	status := Run(context.Background(), "test", []string{"pulls", "--repo", "o/r", "--conflicts", "--restack-plan", "--json"}, &stdout, &stderr)
	if status != 0 || stderr.String() != "" {
		t.Fatalf("%d %q %v", status, stderr.String(), f.log)
	}
	var doc struct {
		Groups  map[string][]map[string]any `json:"groups"`
		Summary map[string]any              `json:"summary"`
		Conf    struct {
			Plans []struct {
				Steps []map[string]any `json:"steps"`
				Brief string           `json:"brief"`
			} `json:"plans"`
		} `json:"conflicts"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Groups["ready"]) != 0 || len(doc.Groups["conflict"]) != 1 || len(doc.Groups["blocked"]) != 1 || doc.Summary["base_conflicts"] != 1.0 {
		t.Fatalf("a conflicting pull request and its child must not be ready: %s", stdout.String())
	}
	bc := doc.Groups["conflict"][0]["base_conflict"].(map[string]any)
	file := bc["files"].([]any)[0].(map[string]any)
	if bc["state"] != "conflicts-with-base" || file["path"] != "rules.md" || file["base_commit"] != mainCommit || bc["base_sha"] != mainCommit {
		t.Fatalf("%v", bc)
	}
	if len(doc.Conf.Plans) != 1 || len(doc.Conf.Plans[0].Steps) != 2 || !strings.Contains(doc.Conf.Plans[0].Brief, "--force-with-lease=one:"+head1) {
		t.Fatalf("%+v", doc.Conf)
	}
	for _, uri := range f.log {
		if strings.Contains(uri, "/merges") || strings.Contains(uri, "compare/") {
			t.Errorf("a forge read was added: %s", uri)
		}
	}
	if got := gitIn(t, work, "rev-parse", "refs/stw/conflicts/pull/1"); got != head1 {
		t.Fatalf("private ref %s", got)
	}
}
