// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/statuscmd"
	"github.com/wstein/sourcesteward/internal/statusremote"
	"github.com/wstein/sourcesteward/internal/stwsync"
)

// block returns the lines of the block that starts with the line head (a prefix), up to the blank line.
func block(t *testing.T, out, head string) []string {
	t.Helper()
	for _, b := range strings.Split(out, "\n\n") {
		if strings.HasPrefix(b, head) {
			return strings.Split(strings.TrimRight(b, "\n"), "\n")
		}
	}
	t.Fatalf("no block %q in\n%s", head, out)
	return nil
}

// The Counts block of the fixture workspace: one aligned line per repository, then All. gone cannot be read
// (everything is ?), crewbook has its issues disabled (n/a), the missing worktree directory of the root is not
// a worktree (2 linked, 1 missing: 1), and a total with a failed repository is ?.
func TestStatusCountsBlockOfTheFixtureWorkspace(t *testing.T) {
	out := renderOnline(t, onlineReport(), statuscmd.ASCII, false, false)
	want := []string{
		"Counts",
		"  .           PRs 3 (bot 0) | Issues 5 (bug 2) | Branches 3/2 | Worktrees 1 | Stash 1",
		"  crewbook    PRs 1 (bot 0) | Issues n/a       | Branches 1/1 | Worktrees 0 | Stash 0",
		"  workharbor  PRs 2 (bot 2) | Issues 0 (bug 0) | Branches 2/0 | Worktrees 0 | Stash 4",
		"  gone        PRs ?         | Issues ?         | Branches ?/? | Worktrees ? | Stash ?",
		"  All         PRs ?         | Issues ?         | Branches ?/? | Worktrees ? | Stash ?",
	}
	if got := block(t, out, "Counts\n"); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// With every repository read the total is a sum; a full issues listing is a lower bound with a plus sign.
func TestStatusCountsTotalIsASumAndATruncatedListingIsALowerBound(t *testing.T) {
	rep := onlineReport()
	rep.Local.Repos, rep.Remote.Repos = rep.Local.Repos[:3], rep.Remote.Repos[:3]
	rep.Remote.Repos[0].Issues.Truncated = true
	out := renderOnline(t, rep, statuscmd.ASCII, false, false)
	counts := block(t, out, "Counts\n")
	if !strings.Contains(counts[1], "Issues 5+ (bug 2+)") {
		t.Errorf("a truncated listing is a lower bound: %s", counts[1])
	}
	// crewbook has its issues disabled: that does not apply and is left out, so the total is a sum of the others
	if want := "  All         PRs 6 (bot 2) | Issues 5+ (bug 2+) | Branches 6/3 | Worktrees 1 | Stash 5"; counts[4] != want {
		t.Errorf("got  %q\nwant %q", counts[4], want)
	}
}

var blockHeads = regexp.MustCompile(`(?m)^(Counts|Needs action \(\d+\)|Repositories \(\d+\)|Notices \(\d+\))$`)

// The blocks are Counts, Needs action (N), Repositories (N) and Notices (N), each after a blank line, and the
// summary line is the last line.
func TestStatusOnlineBlocksAreOrderedSeparatedAndCounted(t *testing.T) {
	out := renderOnline(t, onlineReport(), statuscmd.ASCII, false, false)
	heads := blockHeads.FindAllString(out, -1)
	if strings.Join(heads, "|") != "Counts|Needs action (16)|Repositories (4)|Notices (3)" {
		t.Fatalf("%v in\n%s", heads, out)
	}
	for _, h := range heads[1:] {
		if !strings.Contains(out, "\n\n"+h+"\n") {
			t.Errorf("no blank line before %q in\n%s", h, out)
		}
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if last := lines[len(lines)-1]; !strings.HasPrefix(last, "summary: ") || strings.Count(out, "summary: ") != 1 {
		t.Errorf("the summary is the last line: %q", last)
	}
	if !strings.Contains(out, "\n\nsummary: ") {
		t.Errorf("a blank line before the summary:\n%s", out)
	}
	// the blocks of a clean workspace
	run := runOnline(t, rootOnly(), &statusForge{}, nil)
	if !strings.Contains(run.out, "\n\nNeeds action (0)\n  nothing\n\nRepositories (1)\n") || !strings.Contains(run.out, "\n\nNotices (0)\n  none\n\nsummary") {
		t.Errorf("empty blocks:\n%s", run.out)
	}
}

// The agent-rules block sits between the Notices block and the summary, with blank lines around it.
func TestStatusOnlineRulesBlockIsBetweenTheNoticesAndTheSummary(t *testing.T) {
	rep := onlineReport()
	rep.Local.Rules = &stwsync.Rules{State: stwsync.RulesCurrent, Path: statusPtr("/ws/agent-rules")}
	out := renderOnline(t, rep, statuscmd.ASCII, false, false)
	i, j, k := strings.Index(out, "\nNotices ("), strings.Index(out, "\nagent-rules"), strings.Index(out, "\nsummary: ")
	if !(0 < i && i < j && j < k) || !strings.Contains(out, "\n\nagent-rules") || !strings.Contains(out, "\n\nsummary: ") {
		t.Errorf("order or blank lines:\n%s", out)
	}
}

// Offline nothing is read from the forge: PRs and Issues are n/a (never 0) and the local counters are numbers;
// an uninitialized submodule is not read at all.
func TestStatusOfflineCountsAreNotAvailableAndNeverZero(t *testing.T) {
	forge := &statusForge{err: fmt.Errorf("no request may be sent")}
	run := runOnline(t, statusCLIResult(), forge, nil, "--offline")
	if run.err != nil || forge.reads != 0 {
		t.Fatalf("%v %d", run.err, forge.reads)
	}
	counts := block(t, run.out, "Counts\n")
	flat := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	for i, w := range []string{
		"Counts",
		`. PRs n/a | Issues n/a | Branches 0/1 | Worktrees 0 | Stash 0`,
		`sub\x1b[31m PRs n/a | Issues n/a | Branches 4/2 | Worktrees 2 | Stash 2`,
		`gone PRs n/a | Issues n/a | Branches n/a/n/a | Worktrees n/a | Stash n/a`, // uninitialized: the line says n/a, the total leaves it out
		`All PRs n/a | Issues n/a | Branches 4/3 | Worktrees 2 | Stash 2`,
	} {
		if flat(counts[i]) != w {
			t.Errorf("line %d: %q, want %q", i, flat(counts[i]), w)
		}
	}
	if strings.Contains(run.out, "PRs 0") || strings.Contains(run.out, "Issues 0") {
		t.Errorf("not read is never 0:\n%s", run.out)
	}
	if strings.Contains(run.out, "\x1b") {
		t.Errorf("raw escape in the text: %q", run.out)
	}
	// the Counts block is the first block, then the local screen as before
	if !strings.HasPrefix(run.out, "Counts\n") || !strings.Contains(run.out, "\n\nSourceSteward status: 3 repositories in /ws\n") {
		t.Errorf("order:\n%s", run.out)
	}
}

// A failed remote read (forbidden, a page that is too large, a rate limit, the network) is a notice and exit 0:
// the counts it hit are ?, the other counts stay, and the exit status is 1 only for what cannot be read locally.
func TestStatusRemoteReadFailuresAreNoticesAndExitZero(t *testing.T) {
	for _, code := range []string{hosting.CodeForbidden, hosting.CodePageTooLarge, hosting.CodeRateLimited, hosting.CodeNetwork, hosting.CodeUnauthorized, hosting.CodeMalformed} {
		forge := &statusForge{err: &hosting.WatchError{Code: code, Message: "failed"}}
		run := runOnline(t, rootOnly(), forge, nil)
		if run.err != nil || run.errOut != "" {
			t.Errorf("%s: exit %v stderr %q", code, run.err, run.errOut)
		}
		notices := block(t, run.out, "Notices (")
		if len(notices) < 2 || notices[1] == "  none" {
			t.Errorf("%s: no notice in\n%s", code, run.out)
		}
		if flat := strings.Join(strings.Fields(block(t, run.out, "Counts\n")[1]), " "); !strings.HasPrefix(flat, ". PRs ? | Issues ? | Branches 0/1 |") {
			t.Errorf("%s: %q", code, flat)
		}
	}
	// the JSON keeps the fact: summary.failed is true, the exit status is 0
	run := runOnline(t, rootOnly(), &statusForge{err: &hosting.WatchError{Code: hosting.CodeForbidden, Message: "no"}}, nil, "--json")
	var doc struct {
		Summary struct{ Failed bool }
	}
	if err := json.Unmarshal([]byte(run.out), &doc); err != nil || run.err != nil || !doc.Summary.Failed {
		t.Errorf("%v %v %s", err, run.err, run.out)
	}
}

// Text from outside never reaches the terminal raw: a repository name (the path of a submodule) and the slug of
// the origin carry ESC, CR and a newline. This holds for the Counts block, the repository lines and the notices
// in every layout: offline, without a token, online and long.
func TestStatusTextEscapesNamesAndSlugsFromOutside(t *testing.T) {
	hostile := "sub\x1b[1mB\rX\nPWNED"
	local := rootOnly()
	local.Repos = append(local.Repos, stwsync.RepoStatus{Name: hostile, Path: "/ws/x", Kind: "submodule", State: "ok", Branch: statusPtr(hostile), Reasons: []string{}, Worktrees: stwsync.WorktreeCounts{List: []stwsync.WorktreeInfo{}}, RemoteBranches: statusInt(0)})
	local.Summary.Total = 2
	members := stwsync.Workspace{Root: "/ws", Members: []stwsync.Member{
		{Name: ".", Path: t.TempDir(), Kind: "root", Initialized: true, Remote: "https://github.com/own/\x1b[31mRED\x1b[0m\r\nPWNED"},
		{Name: hostile, Path: t.TempDir(), Kind: "submodule", Initialized: true, Remote: "https://github.com/own/\x1b[31mRED\x1b[0m"},
	}}
	run := func(open error, args ...string) string {
		cmd := workspaceStatusCommandWith(statuscmd.Params{
			Now:      func() time.Time { return time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC) },
			Local:    func(context.Context, stwsync.StatusOptions) stwsync.StatusResult { return local },
			Discover: func(context.Context, string) (stwsync.Workspace, error) { return members, nil },
			Open: func(context.Context, string, string) (statusremote.Client, func(), error) {
				return &statusForge{err: &hosting.WatchError{Code: hosting.CodeForbidden, Message: "no \x1b[31m"}}, func() {}, open
			},
		})
		var out strings.Builder
		cmd.SetOut(&out)
		cmd.SetErr(&strings.Builder{})
		cmd.SetArgs(args)
		if err := cmd.ExecuteContext(context.Background()); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return out.String()
	}
	noToken := fmt.Errorf("gh: %w", hosting.ErrAuthentication)
	for name, out := range map[string]string{
		"offline": run(nil, "--offline", "--worktrees"), "no token": run(noToken), "online": run(nil), "long": run(nil, "--long"),
	} {
		if strings.ContainsAny(out, "\x1b\r") {
			t.Errorf("%s: raw ESC or CR in %q", name, out)
		}
		for _, l := range strings.Split(out, "\n") {
			if strings.HasPrefix(l, "PWNED") || strings.HasPrefix(l, "X") {
				t.Errorf("%s: the name started a line of its own: %q", name, l)
			}
		}
		if !strings.Contains(out, `sub\x1b[1mB\rX\nPWNED`) {
			t.Errorf("%s: the escaped name is visible as text:\n%s", name, out)
		}
	}
}

// --json carries counts per repository and totals in the schema 2 document (additive, still schema 2); the
// offline document is schema 1 and stays as published.
func TestStatusJSONCarriesCountsAndTotalsOnlineAndNotOffline(t *testing.T) {
	run := runOnline(t, rootOnly(), &statusForge{}, nil, "--json")
	var doc struct {
		SchemaVersion int `json:"schema_version"`
		Repos         []struct {
			Counts map[string]any `json:"counts"`
		}
		Totals map[string]any `json:"totals"`
	}
	if err := json.Unmarshal([]byte(run.out), &doc); err != nil || doc.SchemaVersion != 2 || len(doc.Repos) != 1 {
		t.Fatalf("%v %s", err, run.out)
	}
	want := `{"branches":{"local":0,"remote":1},"issues":{"bug":0,"open":0,"state":"ok","truncated":false},"pull_requests":{"bot":0,"open":0,"state":"ok","truncated":false},"stash":0,"worktrees":0}`
	for name, got := range map[string]map[string]any{"repo": doc.Repos[0].Counts, "totals": doc.Totals} {
		raw, _ := json.Marshal(got)
		if string(raw) != want {
			t.Errorf("%s counts\n got  %s\n want %s", name, raw, want)
		}
	}
	off := runOnline(t, rootOnly(), nil, nil, "--offline", "--json")
	if strings.Contains(off.out, `"counts"`) || strings.Contains(off.out, `"totals"`) || !strings.Contains(off.out, `"schema_version":1`) {
		t.Errorf("schema 1 stays as published: %s", off.out)
	}
}
