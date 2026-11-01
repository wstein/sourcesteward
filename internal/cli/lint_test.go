// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// lintForge serves pull request 7 of o/r with one clean commit and one comment; the body can be replaced.
func lintForge(t *testing.T, body string) *watchForge {
	t.Helper()
	f := &watchForge{t: t, docs: map[string]string{}}
	head := strings.Repeat("a", 40)
	bodyJSON, _ := json.Marshal(body)
	f.docs["repos/o/r/pulls/7"] = `{"number":7,"title":"feat: x","body":` + string(bodyJSON) + `,"head":{"sha":"` + head + `"}}`
	f.docs["repos/o/r/pulls/7/commits?per_page=100&page=1"] = `[{"sha":"` + strings.Repeat("1", 40) + `","commit":{"message":"feat: x\n\nCo-Authored-By: C <c@d.e>","author":{"name":"Octo Owner","email":"octo@example.com"},"committer":{"name":"Octo Owner","email":"octo@example.com"}}}]`
	f.docs["repos/o/r/issues/7/comments?per_page=30&page=1"] = `[{"id":5,"body":"hello"}]`
	return f
}

func lintRun(t *testing.T, forge *watchForge, args ...string) (int, string, string) {
	t.Helper()
	lintTransport = forge
	t.Cleanup(func() { lintTransport = nil })
	var stdout, stderr bytes.Buffer
	status := Run(context.Background(), "test", append([]string{"lint", "pr"}, args...), &stdout, &stderr)
	return status, stdout.String(), stderr.String()
}

const lintOK = "Purpose.\nBlocker: none\nAuthor agent: Claude"

func TestLintPREndToEndOverTheGuardedReader(t *testing.T) {
	watchEnv(t, true)
	forge := lintForge(t, lintOK)
	status, out, errOut := lintRun(t, forge, "7", "--repo", "o/r", "--owner-identity", "Octo Owner <octo@example.com>", "--json")
	if status != 0 || errOut != "" {
		t.Fatalf("%d %q %q", status, out, errOut)
	}
	var doc struct {
		Summary struct {
			Passed   bool `json:"passed"`
			Commits  int  `json:"commits"`
			Comments int  `json:"comments"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil || !doc.Summary.Passed || doc.Summary.Commits != 1 || doc.Summary.Comments != 1 {
		t.Fatalf("%v %s", err, out)
	}
	want := []string{"repos/o/r/pulls/7", "repos/o/r/pulls/7/commits?per_page=100&page=1", "repos/o/r/issues/7/comments?per_page=30&page=1"}
	if strings.Join(forge.log, "|") != strings.Join(want, "|") {
		t.Fatalf("requests %v", forge.log)
	}
}

func TestLintPRViolationExitsThree(t *testing.T) {
	watchEnv(t, true)
	status, out, errOut := lintRun(t, lintForge(t, "no structure"), "7", "--repo", "o/r", "--owner-identity", "Octo Owner <octo@example.com>", "--json")
	if status != 3 || !strings.Contains(out, `"pr-blocker-line"`) || !strings.Contains(errOut, "error") {
		t.Fatalf("%d %q %q", status, out, errOut)
	}
}

func TestLintPRUsageAndReadFailures(t *testing.T) {
	watchEnv(t, true)
	for name, args := range map[string][]string{
		"no number":   {},
		"not number":  {"x", "--repo", "o/r"},
		"zero":        {"0", "--repo", "o/r", "--owner-identity", "A <a@b.c>"},
		"bad repo":    {"7", "--repo", "nope", "--owner-identity", "A <a@b.c>"},
		"bad ident":   {"7", "--repo", "o/r", "--owner-identity", "nope"},
		"both output": {"7", "--repo", "o/r", "--owner-identity", "A <a@b.c>", "--human", "--json"},
	} {
		if status, _, _ := lintRun(t, lintForge(t, lintOK), args...); status != 2 {
			t.Errorf("%s: status %d", name, status)
		}
	}
	forge := lintForge(t, lintOK)
	delete(forge.docs, "repos/o/r/pulls/7")
	status, out, _ := lintRun(t, forge, "7", "--repo", "o/r", "--owner-identity", "A <a@b.c>", "--json")
	if status != 1 || out != "" {
		t.Fatalf("%d %q", status, out)
	}
	watchEnv(t, false)
	if status, out, _ := lintRun(t, lintForge(t, lintOK), "7", "--repo", "o/r", "--owner-identity", "A <a@b.c>"); status != 1 || out != "" {
		t.Fatalf("no token: %d %q", status, out)
	}
}
