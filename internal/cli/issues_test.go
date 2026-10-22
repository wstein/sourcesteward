// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const (
	issuesListOR   = "repos/o/r/issues?state=open&per_page=100&sort=created&direction=asc&page=1"
	issuesListLab  = "repos/wstein/lab/issues?state=open&per_page=100&sort=created&direction=asc&page=1"
	issuesUserRead = "user"
	issuesSearch   = "search/issues?q=user%3Awstein+is%3Aissue+is%3Aopen+archived%3Afalse&per_page=100&page=1&sort=created&order=asc"
)

// issuesForge serves three issues in o/r (one security, one assigned to the viewer, one by a bot
// that is a plain bug), a pull request entry that must be dropped, and for the owner mode one
// issue in wstein/lab found by search.
func issuesForge(t *testing.T) *watchForge {
	t.Helper()
	f := &watchForge{t: t, docs: map[string]string{}}
	item := func(n, user, kind, labels, assignees string) string {
		return `{"number":` + n + `,"title":"Issue ` + n + `","user":{"login":"` + user + `","type":"` + kind + `"},"labels":[` + labels + `],"assignees":[` + assignees + `],"created_at":"2026-10-20T09:00:00Z","html_url":"https://github.com/o/r/issues/` + n + `","comments":0}`
	}
	f.docs[issuesListOR] = "[" + item("1", "alice", "User", `{"name":"security"}`, "") + "," + item("2", "bob", "User", "", `{"login":"Wstein"}`) + "," +
		item("3", "dependabot[bot]", "Bot", `{"name":"bug"}`, "") + `,{"number":4,"title":"a pull request","pull_request":{},"user":{"login":"x","type":"User"}}]`
	f.docs[issuesListLab] = "[" + strings.Replace(item("7", "erin", "User", "", ""), "/o/r/", "/wstein/lab/", 1) + "]"
	f.docs[issuesUserRead] = `{"login":"wstein"}`
	f.docs[issuesSearch] = `{"total_count":1,"incomplete_results":false,"items":[{"repository_url":"https://api.github.com/repos/wstein/lab","number":7}]}`
	return f
}

func issuesRun(t *testing.T, forge http.RoundTripper, args ...string) (int, string, string) {
	t.Helper()
	issuesTransport = forge
	t.Cleanup(func() { issuesTransport = nil })
	var stdout, stderr bytes.Buffer
	status := Run(context.Background(), "test", append([]string{"issues"}, args...), &stdout, &stderr)
	return status, stdout.String(), stderr.String()
}

func TestIssuesEndToEndJSONOverTheGuardedReader(t *testing.T) {
	watchEnv(t, true)
	forge := issuesForge(t)
	status, out, errOut := issuesRun(t, forge, "--repo", "o/r", "--json")
	if status != 0 || errOut != "" {
		t.Fatalf("%d %q", status, errOut)
	}
	var doc struct {
		Issues  map[string][]map[string]any `json:"issues"`
		Summary map[string]any              `json:"summary"`
		Viewer  map[string]any              `json:"viewer"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil || len(doc.Issues["red"]) != 1 || len(doc.Issues["yellow"]) != 1 || len(doc.Issues["violet"]) != 1 || doc.Summary["total"] != 3.0 || doc.Viewer["login"] != "wstein" {
		t.Fatalf("%v %s", err, out)
	}
	if doc.Issues["yellow"][0]["number"] != 2.0 || doc.Issues["yellow"][0]["reason"] != "assigned-to-you" || doc.Issues["red"][0]["command"] != "gh issue view 1 -R o/r" {
		t.Fatalf("%s", out)
	}
	if strings.Join(forge.log, ",") != issuesUserRead+","+issuesListOR && strings.Join(forge.log, ",") != issuesListOR+","+issuesUserRead {
		t.Fatalf("exactly the list and one user read: %v", forge.log)
	}
	if strings.Contains(out+errOut, watchToken) {
		t.Fatal("token in output")
	}
}

func TestIssuesEndToEndOwnerModeUsesTheIssueSearchShape(t *testing.T) {
	watchEnv(t, true)
	forge := issuesForge(t)
	status, out, errOut := issuesRun(t, forge, "--repo", "o/r", "--owner", "wstein", "--json")
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
		found = found || uri == issuesSearch
		if strings.Contains(uri, "is%3Apr") || strings.Contains(uri, "labels=") || strings.Contains(uri, "assignee=") {
			t.Errorf("unexpected shape %s", uri)
		}
	}
	if !found {
		t.Fatalf("search not requested: %v", forge.log)
	}
}

func TestIssuesFiltersNeverReachTheForge(t *testing.T) {
	watchEnv(t, true)
	forge := issuesForge(t)
	status, out, _ := issuesRun(t, forge, "--repo", "o/r", "--label", "BUG", "--assignee", "@me", "--author", "alice", "--json")
	if status != 0 || !strings.Contains(out, `"total":0`) {
		t.Fatalf("%d %s", status, out)
	}
	for _, uri := range forge.log {
		if strings.ContainsAny(uri, "&") && uri != issuesListOR {
			t.Errorf("unexpected request %s", uri)
		}
	}
}

func TestIssuesHumanOutput(t *testing.T) {
	watchEnv(t, true)
	status, out, _ := issuesRun(t, issuesForge(t), "--repo", "o/r", "--human")
	if status != 0 || !strings.HasPrefix(out, "RED (1)\no/r#1  ") || !strings.Contains(out, "dependabot[bot]") || strings.Contains(out, "[bot][bot]") ||
		!strings.HasSuffix(out, "3 open issues in 1 repository, 1 red, 1 yellow\n") {
		t.Fatalf("%d %q", status, out)
	}
}

func TestIssuesExitCodes(t *testing.T) {
	watchEnv(t, true)
	forge := issuesForge(t)
	for name, args := range map[string][]string{
		"human and json":         {"--repo", "o/r", "--human", "--json"},
		"limit 0":                {"--repo", "o/r", "--limit", "0"},
		"limit 501":              {"--repo", "o/r", "--limit", "501"},
		"bad owner":              {"--owner", "a--b"},
		"bad author":             {"--repo", "o/r", "--author", "a b"},
		"bad assignee":           {"--repo", "o/r", "--assignee", "@you"},
		"bad label":              {"--repo", "o/r", "--label", ""},
		"bad repo":               {"--repo", "nope"},
		"unknown flag":           {"--repo", "o/r", "--nonsense"},
		"pulls flag":             {"--repo", "o/r", "--bots-only"},
		"path without workspace": {"--repo", "o/r", "/tmp"},
		"path alone":             {"/nonexistent-workspace-path"},
		"two paths":              {"--workspace", "a", "b"},
	} {
		if status, out, errOut := issuesRun(t, forge, args...); status != 2 || out != "" || !strings.HasPrefix(errOut, "Error:") {
			t.Errorf("%s: %d %q %q", name, status, out, errOut)
		}
	}
	if len(forge.log) != 0 {
		t.Fatalf("a usage error reads nothing: %v", forge.log)
	}
	// a repository the forge does not know: exit 1, the others are printed
	status, out, errOut := issuesRun(t, forge, "--repo", "o/r", "--repo", "o/missing", "--json")
	if status != 1 || !strings.Contains(out, `"repos_failed":1`) || !strings.Contains(errOut, "o/missing") {
		t.Fatalf("%d %q %q", status, out, errOut)
	}
	// the token cannot read `user` (403 without rate-limit evidence is answered 404 by the forge here): a skip, exit 0
	delete(forge.docs, issuesUserRead)
	status, out, _ = issuesRun(t, forge, "--repo", "o/r", "--json")
	if status != 0 || !strings.Contains(out, `"reason":"no-identity"`) {
		t.Fatalf("%d %q", status, out)
	}
	// no token: exit 1 before any request, nothing printed
	watchEnv(t, false)
	forge = issuesForge(t)
	status, out, errOut = issuesRun(t, forge, "--repo", "o/r")
	if status != 1 || out != "" || len(forge.log) != 0 || !strings.Contains(errOut, "hosting-api-unavailable") {
		t.Fatalf("%d %q %q %v", status, out, errOut, forge.log)
	}
}

func TestIssuesIsListedInHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if status := Run(context.Background(), "test", []string{"--help"}, &stdout, &stderr); status != 0 || !strings.Contains(stdout.String(), "issues ") {
		t.Fatalf("%d %q", status, stdout.String())
	}
	stdout.Reset()
	if status := Run(context.Background(), "test", []string{"issues", "--help"}, &stdout, &stderr); status != 0 || !strings.Contains(stdout.String(), "--assignee") || !strings.Contains(stdout.String(), "--label") {
		t.Fatalf("%d %q", status, stdout.String())
	}
}

func TestIssuesOutsideAWorkspaceIsAUsageError(t *testing.T) {
	watchEnv(t, true)
	t.Chdir(t.TempDir())
	forge := issuesForge(t)
	status, out, errOut := issuesRun(t, forge)
	if status != 2 || out != "" || !strings.HasPrefix(errOut, "Error:") || len(forge.log) != 0 {
		t.Fatalf("%d %q %q %v", status, out, errOut, forge.log)
	}
}

func TestIssuesLimitDefaultsTo200(t *testing.T) {
	watchEnv(t, true)
	status, out, _ := issuesRun(t, issuesForge(t), "--repo", "o/r", "--json")
	var doc struct {
		Options map[string]any `json:"options"`
	}
	if status != 0 || json.Unmarshal([]byte(out), &doc) != nil || doc.Options["limit"] != 200.0 {
		t.Fatalf("%d %s", status, out)
	}
}
