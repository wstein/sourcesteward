// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/land"
	"github.com/wstein/sourcesteward/internal/landcmd"
)

func landSHA(i int) string { return strings.Repeat(fmt.Sprint(i), 40) }

type landGit struct{ calls []string }

func (g *landGit) RemoteRepo(context.Context, string) (string, error) { return "o/r", nil }
func (g *landGit) DefaultTip(context.Context, string, string) (string, error) {
	g.calls = append(g.calls, "ls-remote")
	return landSHA(0), nil
}
func (g *landGit) Fetch(context.Context, string, string, []int) error {
	g.calls = append(g.calls, "fetch")
	return nil
}
func (g *landGit) Resolve(_ context.Context, n int) (string, error) { return landSHA(n - 10), nil }
func (g *landGit) IsAncestor(_ context.Context, a, b string) (land.Ancestry, error) {
	if a <= b {
		return land.AncestorYes, nil
	}
	return land.AncestorNo, nil
}
func (g *landGit) Range(_ context.Context, from, to string, _ int) ([]land.Commit, error) {
	var out []land.Commit
	for c := from[0] + 1; c <= to[0]; c++ {
		out = append(out, land.Commit{SHA: strings.Repeat(string(c), 40), AuthorName: "Werner Stein", AuthorEmail: "w@example.org", CommitterName: "Werner Stein", CommitterEmail: "w@example.org"})
	}
	return out, nil
}

func landForge(t *testing.T) *watchForge {
	t.Helper()
	f := &watchForge{t: t, docs: map[string]string{
		"repos/o/r": `{"default_branch":"main","delete_branch_on_merge":true}`,
		"user":      `{"login":"wstein"}`,
		"repos/o/r/pulls?state=open&per_page=100&page=1":    `[]`,
		"repos/o/r/rules/branches/main?per_page=100&page=1": `[{"type":"required_status_checks","ruleset_id":7,"parameters":{"required_status_checks":[{"context":"ci"}]}},{"type":"pull_request","ruleset_id":7}]`,
		"repos/o/r/rulesets/7":                              `{"bypass_actors":[{"bypass_mode":"always"}]}`,
	}}
	for n := 11; n <= 13; n++ {
		head := landSHA(n - 10)
		f.docs[fmt.Sprintf("repos/o/r/pulls/%d", n)] = fmt.Sprintf(`{"number":%d,"state":"open","head":{"sha":%q,"ref":"feat-%d","repo":{"full_name":"o/r"}},"base":{"ref":"main","repo":{"full_name":"o/r"}},"mergeable":true,"mergeable_state":"blocked","rebaseable":true}`, n, head, n)
		f.docs[fmt.Sprintf("repos/o/r/commits/%s/check-runs?per_page=100&page=1", head)] = `{"check_runs":[{"name":"ci","status":"completed","conclusion":"success"}]}`
		f.docs[fmt.Sprintf("repos/o/r/commits/%s/statuses?per_page=100&page=1", head)] = `[]`
		f.docs[fmt.Sprintf("repos/o/r/pulls/%d/reviews?per_page=100&page=1", n)] = `[]`
	}
	return f
}

func landRun(t *testing.T, forge *watchForge, env []string, tty bool, args ...string) (int, string, string, *landGit) {
	t.Helper()
	git := &landGit{}
	landTransport = forge
	landHook = func(p *landcmd.Params) {
		p.Git, p.Environ, p.TTY = git, append([]string{}, env...), func() (bool, bool) { return tty, tty }
		p.Policy = func(context.Context, string) land.Policy {
			return land.Policy{Loaded: true, OwnerName: "Werner Stein", OwnerEmail: "w@example.org"}
		}
	}
	t.Cleanup(func() { landTransport, landHook = nil, nil })
	var stdout, stderr bytes.Buffer
	status := Run(context.Background(), "test", append([]string{"land"}, args...), &stdout, &stderr)
	return status, stdout.String(), stderr.String(), git
}

func landArgs(extra ...string) []string {
	return append([]string{"--stack", "11,12,13", "--reviewed", "11:" + landSHA(1), "--reviewed", "12:" + landSHA(2), "--reviewed", "13:" + landSHA(3), "--repo", "o/r", "--json"}, extra...)
}

func TestLandPlanOverTheGuardedReader(t *testing.T) {
	watchEnv(t, true)
	forge := landForge(t)
	status, out, errOut, git := landRun(t, forge, nil, true, landArgs("--plan")...)
	// the shape is not recorded in the embedded (empty) allowlist: the plan is refused with exit 1
	if status != 1 || !strings.Contains(out, `"land-unmeasured"`) || !strings.Contains(out, `"landable":false`) {
		t.Fatalf("status %d\n%s\n%s", status, out, errOut)
	}
	if len(git.calls) == 0 || len(forge.log) == 0 {
		t.Fatalf("no reads: %v %v", git.calls, forge.log)
	}
	for _, uri := range forge.log {
		if strings.Contains(uri, "merge") || strings.Contains(uri, "comments") {
			t.Errorf("unexpected endpoint %s", uri)
		}
	}
}

func TestLandApplyByAnAgentReadsNothing(t *testing.T) {
	watchEnv(t, true)
	forge := landForge(t)
	status, out, _, git := landRun(t, forge, []string{"STW_AGENT=1"}, true, landArgs()...)
	if status != 1 || !strings.Contains(out, `"agent-mode"`) {
		t.Fatalf("status %d\n%s", status, out)
	}
	if len(git.calls) != 0 || len(forge.log) != 0 {
		t.Fatalf("an agent apply reached a read: git=%v forge=%v", git.calls, forge.log)
	}
}

func TestLandUsageErrorsExitTwo(t *testing.T) {
	watchEnv(t, true)
	for name, args := range map[string][]string{
		"yes":           landArgs("--yes"),
		"no stack":      {"--plan"},
		"single number": {"--stack", "11", "--reviewed", "11:" + landSHA(1), "--plan"},
		"pr with stack": landArgs("--pr", "11"),
		"pr alone":      {"--pr", "11", "--head", landSHA(1), "--reviewed", "11:" + landSHA(1)},
		"json human":    landArgs("--human"),
	} {
		status, out, errOut, git := landRun(t, landForge(t), nil, true, args...)
		if status != 2 || out != "" || errOut == "" || len(git.calls) != 0 {
			t.Errorf("%s: status %d out=%q err=%q", name, status, out, errOut)
		}
	}
}

func TestLandIsListedInRootHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if status := Run(context.Background(), "test", []string{"--help"}, &stdout, &stderr); status != 0 || !strings.Contains(stdout.String(), "land ") {
		t.Fatalf("%d %q", status, stdout.String())
	}
	stdout.Reset()
	if status := Run(context.Background(), "test", []string{"land", "--help"}, &stdout, &stderr); status != 0 || !strings.Contains(stdout.String(), "Owner-only") || strings.Contains(stdout.String(), "\n      --yes") {
		t.Fatalf("%d %q", status, stdout.String())
	}
}
