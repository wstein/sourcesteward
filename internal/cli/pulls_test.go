// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

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
	for _, uri := range forge.log {
		if strings.HasPrefix(uri, "search/") || strings.Contains(uri, "branches/") || uri == "rate_limit" {
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
	if status != 0 || !strings.Contains(out, "ready (1)\n") || !strings.Contains(out, "o/r#1  ") || strings.Contains(out, "dependabot") || !strings.HasSuffix(out, "1 open pull request in 1 repository, 1 ready\n") {
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
