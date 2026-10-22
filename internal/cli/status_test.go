// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/stwsync"
)

func statusPtr(s string) *string { return &s }
func statusInt(i int) *int       { return &i }

func statusCLIResult() stwsync.StatusResult {
	long := strings.Repeat("a", 40)
	return stwsync.StatusResult{
		SchemaVersion: 1, Command: "status", Root: statusPtr("/ws"),
		Repos: []stwsync.RepoStatus{
			{Name: ".", Path: "/ws", Kind: "root", State: "ok", Branch: statusPtr("main"), Upstream: statusPtr("origin/main"), Ahead: statusInt(0), Behind: statusInt(0), Reasons: []string{}, Worktrees: stwsync.WorktreeCounts{List: []stwsync.WorktreeInfo{}}},
			{Name: "sub\x1b[31m", Path: "/ws/sub", Kind: "submodule", State: "ok", Branch: statusPtr("feat"), Upstream: statusPtr("origin/feat"), Ahead: statusInt(2), Behind: statusInt(1),
				Changes: stwsync.Changes{Staged: 1, Unstaged: 2, Untracked: 3, Conflicts: 1}, Operation: statusPtr("rebase"), Stash: 2,
				Branches: stwsync.BranchCounts{Local: 4, Base: statusPtr("origin/main"), Integrated: statusInt(1), NotIntegrated: statusInt(2)},
				Worktrees: stwsync.WorktreeCounts{Linked: 2, Dirty: 1, Locked: 1, List: []stwsync.WorktreeInfo{
					{Path: "/wt/one", Branch: statusPtr("one"), Dirty: true},
					{Path: "/wt/two", Detached: true, Locked: statusPtr("usb")},
				}},
				Gitlink:   &stwsync.Gitlink{Recorded: &long, CheckedOut: statusPtr(strings.Repeat("b", 40)), State: "drift", Relation: statusPtr("ahead")},
				Attention: true, Reasons: []string{"operation", "staged"}},
			{Name: "gone", Path: "/ws/gone", Kind: "submodule", State: "uninitialized", Reasons: []string{}, Gitlink: &stwsync.Gitlink{Recorded: &long, State: "uninitialized"}},
		},
		Summary: stwsync.StatusSummary{Total: 3, Attention: 1},
	}
}

func runStatus(result stwsync.StatusResult, args ...string) (stwsync.StatusOptions, int, string, error) {
	var got stwsync.StatusOptions
	calls := 0
	cmd := workspaceStatusCommandWith(func(_ context.Context, o stwsync.StatusOptions) stwsync.StatusResult {
		calls++
		got = o
		return result
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return got, calls, out.String(), err
}

func TestStatusFlagsMapToOptions(t *testing.T) {
	got, calls, _, err := runStatus(statusCLIResult(), "--fetch", "--worktrees", "some path")
	if err != nil || calls != 1 || !got.Fetch || !got.Worktrees || got.Path != "some path" {
		t.Fatalf("%v %+v", err, got)
	}
	got, _, _, _ = runStatus(statusCLIResult())
	if got.Fetch || got.Worktrees || got.Path != "." {
		t.Fatalf("%+v", got)
	}
}

func TestStatusUsageErrorsBeforeTheOperation(t *testing.T) {
	for _, args := range [][]string{{"a", "b"}, {"--bogus"}, {"--json=maybe"}, {"a\x00b"}, {"\xff"}} {
		_, calls, out, err := runStatus(statusCLIResult(), args...)
		var failure runtimeFailure
		if err == nil || errors.As(err, &failure) || calls != 0 || out != "" {
			t.Errorf("%q: %v %d %q", args, err, calls, out)
		}
	}
}

func TestStatusTextIsOneBlockPerRepositoryWithASummary(t *testing.T) {
	_, _, out, err := runStatus(statusCLIResult())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"ahead 2, behind 1", "origin/feat", "1 staged, 2 unstaged, 3 untracked, 1 conflict", "rebase in progress", "stash 2",
		"2 not integrated into origin/main", "2 linked worktrees (1 dirty, 1 locked)", "aaaaaaa", "bbbbbbb", "checked out ahead (bbbbbbb)", "not initialized",
		"1 of 3 repositories need attention",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b") || strings.Contains(out, "/wt/one") {
		t.Errorf("no colour off a terminal, no worktree list without --worktrees:\n%s", out)
	}
	if !strings.Contains(out, `sub\x1b[31m`) && !strings.Contains(out, `sub\u001b`) && !strings.Contains(out, `\x1b`) {
		t.Errorf("repository text must be escaped:\n%s", out)
	}
}

func TestStatusWorktreeListWithFlag(t *testing.T) {
	_, _, out, err := runStatus(statusCLIResult(), "--worktrees")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/wt/one", "one", "dirty", "/wt/two", "detached", "locked: usb"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}

func TestStatusAllClearSummary(t *testing.T) {
	r := statusCLIResult()
	r.Repos = r.Repos[:1]
	r.Summary = stwsync.StatusSummary{Total: 1}
	_, _, out, err := runStatus(r)
	if err != nil || !strings.Contains(out, "nothing needs attention") {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestStatusJSONIsTheModel(t *testing.T) {
	_, _, out, err := runStatus(statusCLIResult(), "--json")
	if err != nil {
		t.Fatal(err)
	}
	var back stwsync.StatusResult
	if json.Unmarshal([]byte(out), &back) != nil || back.Command != "status" || len(back.Repos) != 3 || strings.Count(out, "\n") != 1 {
		t.Fatalf("%s", out)
	}
}

func TestStatusExitStatus(t *testing.T) {
	var failure runtimeFailure
	r := statusCLIResult()
	if _, _, _, err := runStatus(r); err != nil {
		t.Fatalf("attention is not a failure: %v", err)
	}
	r.Repos[2].State, r.Repos[2].Error = "error", &stwsync.Failure{Code: "status-read-failed", Message: "cannot read the repository status"}
	r.Summary.Errors = 1
	_, _, out, err := runStatus(r)
	if !errors.As(err, &failure) || !strings.Contains(out, "cannot read the repository status") {
		t.Fatalf("%v %s", err, out)
	}
	failed := stwsync.StatusResult{Repos: []stwsync.RepoStatus{}, Error: &stwsync.Failure{Code: "status-not-a-repository", Message: "no"}}
	if _, _, out, err = runStatus(failed, "--json"); !errors.As(err, &failure) || !strings.Contains(out, "status-not-a-repository") {
		t.Fatalf("%v %s", err, out)
	}
	if _, _, out, err = runStatus(failed); !errors.As(err, &failure) || out != "" {
		t.Fatalf("%v %q", err, out)
	}
}

func TestStatusColourOnlyWhenEnabled(t *testing.T) {
	var b bytes.Buffer
	if err := renderStatusText(&b, statusCLIResult(), false, true); err != nil || !strings.Contains(b.String(), "\x1b[") {
		t.Fatalf("%v %q", err, b.String())
	}
	if strings.Contains(b.String(), "\x1b[31m\x1b") || strings.Count(b.String(), "\x1b") != strings.Count(b.String(), "\x1b[") {
		t.Fatalf("raw escape from repository text: %q", b.String())
	}
}

// statusGolden compares got with testdata/NAME; STW_UPDATE_GOLDEN=1 rewrites the file after review.
func statusGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if os.Getenv("STW_UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("%s differs from its golden (STW_UPDATE_GOLDEN=1 rewrites it after review)\n--- got\n%s\n--- want\n%s", name, got, want)
	}
}

// The offline output is the Spec 033 output byte for byte (Spec 037, US2): these goldens were taken
// before the online default existed and must never change.
func TestStatusOfflineOutputIsPinned(t *testing.T) {
	for name, args := range map[string][]string{
		"status-offline.txt":           {},
		"status-offline-worktrees.txt": {"--worktrees"},
		"status-offline.json":          {"--json"},
	} {
		_, _, out, err := runStatus(statusCLIResult(), args...)
		if err != nil {
			t.Fatal(err)
		}
		statusGolden(t, name, out)
	}
	var b bytes.Buffer
	if err := renderStatusText(&b, statusCLIResult(), true, true); err != nil {
		t.Fatal(err)
	}
	statusGolden(t, "status-offline-colour.txt", b.String())
}
