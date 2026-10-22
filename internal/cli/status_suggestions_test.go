// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"io"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/overview"
	"github.com/wstein/sourcesteward/internal/stwsync"
)

// everyCategoryInput is a workspace in which every one of the 25 categories of the action table has an item.
func everyCategoryInput() overview.Input {
	sha := strings.Repeat("a", 40)
	core := stwsync.RepoStatus{
		Name: "core", Path: "core", Kind: "submodule", State: "ok", Branch: statusPtr("main"), Upstream: statusPtr("origin/main"), Head: statusPtr(sha),
		Ahead: statusInt(0), Behind: statusInt(2), Detached: true, Operation: statusPtr("rebase"), Stash: 1,
		Changes:   stwsync.Changes{Staged: 1, Unstaged: 1, Untracked: 1, Conflicts: 1},
		Branches:  stwsync.BranchCounts{Local: 2, Base: statusPtr("origin/main"), NotIntegrated: statusInt(1)},
		Worktrees: stwsync.WorktreeCounts{Linked: 2, List: []stwsync.WorktreeInfo{{Path: "wt-dirty", Dirty: true}, {Path: "wt-gone", Missing: true}}},
		Gitlink:   &stwsync.Gitlink{State: "drift", Relation: statusPtr("behind"), Recorded: statusPtr(sha), CheckedOut: statusPtr(strings.Repeat("b", 40))},
		Attention: true, Reasons: []string{"operation", "conflicts", "staged", "unstaged", "detached", "untracked", "behind", "no-upstream", "gitlink-drift"},
	}
	ahead := stwsync.RepoStatus{
		Name: "ahead", Path: "ahead", Kind: "submodule", State: "ok", Reasons: []string{},
		Gitlink: &stwsync.Gitlink{State: "drift", Relation: statusPtr("ahead"), Recorded: statusPtr(sha), CheckedOut: statusPtr(strings.Repeat("b", 40))},
	}
	broken := stwsync.RepoStatus{Name: "broken", Path: "broken", State: "error", Reasons: []string{}, Error: &stwsync.Failure{Code: "status-read-failed", Message: "no"}}
	behind := true
	failedRun := &overview.Run{Workflow: "release.yml", Conclusion: "failure", Status: "completed", URL: "https://github.com/acme/core/actions/runs/1"}
	sources := map[overview.Source]overview.SourceState{
		overview.SourcePulls:     {Outcome: overview.OutcomeError, Code: "network"},
		overview.SourceCIRelease: {Outcome: overview.OutcomeError, Code: "network"},
	}
	remote := &overview.Remote{
		Sources: sources, Visibility: overview.WriteAccess, DefaultBranch: "main",
		Pulls: []overview.PR{
			{Number: 1, Title: "a", Group: overview.GroupConflict}, {Number: 2, Title: "b", Group: overview.GroupBlocked, Reason: "checks-red"},
			{Number: 3, Title: "c", Group: overview.GroupReady}, {Number: 4, Title: "d", Group: overview.GroupChecks},
			{Number: 5, Title: "e", Group: overview.GroupBlocked, Reason: "not-read", PastBudget: true}, {Number: 6, Title: "f", Bot: true, Author: "dependabot[bot]", Group: overview.GroupReady},
		},
		Drafts: []overview.Draft{{Name: "one", Tag: "v1.0.0", Assets: 1}},
		Tags: []overview.ReleaseTag{
			{Pattern: "v*.*.*", Tag: "v2.0.0", SHA: sha, Unreleased: true},
			{Pattern: "v*.*.*", Tag: "v1.9.0", SHA: strings.Repeat("c", 40), Unreleased: true, Run: failedRun},
		},
		Main: &overview.Main{Branch: "main", Head: sha, Class: "red", BehindOrigin: &behind},
	}
	return overview.Input{Root: "/ws", Repos: []overview.Repo{{Local: core, Slug: "acme/core", Remote: remote}, {Local: ahead}, {Local: broken}}}
}

// TestEverySuggestedCommandExists parses the command of every item the action model can produce and
// resolves it against the real command tree: a `stw` command must be a command with the flags and the
// arguments it is given, anything else a gh or git command of the reviewed set.
func TestEverySuggestedCommandExists(t *testing.T) {
	ov := overview.Build(everyCategoryInput())
	seen := map[overview.Category]bool{}
	for _, it := range ov.Items {
		seen[it.Category] = true
	}
	for _, c := range overview.Categories() {
		if !seen[c] {
			t.Errorf("the fixture has no item of category %s", c)
		}
	}
	root, _ := newRoot("dev", io.Discard, io.Discard)
	commands := 0
	for _, it := range ov.Items {
		if it.Command == "" {
			if it.Effect != "" {
				t.Errorf("%s: an effect without a command", it.ID)
			}
			continue
		}
		commands++
		words := strings.Fields(it.Command)
		switch words[0] {
		case "stw":
			target, rest, err := root.Find(words[1:])
			if err != nil || target == root {
				t.Errorf("%s: %q is no stw command: %v", it.ID, it.Command, err)
				continue
			}
			if err := target.ParseFlags(rest); err != nil {
				t.Errorf("%s: %q: %v", it.ID, it.Command, err)
				continue
			}
			if err := target.ValidateArgs(target.Flags().Args()); err != nil {
				t.Errorf("%s: %q: %v", it.ID, it.Command, err)
			}
		case "gh", "git":
			if !reviewedForeignCommand(it.Command) {
				t.Errorf("%s: %q is not a reviewed gh or git command", it.ID, it.Command)
			}
		default:
			t.Errorf("%s: %q is neither stw, gh nor git", it.ID, it.Command)
		}
	}
	if commands < 20 {
		t.Errorf("only %d items have a command", commands)
	}
}

func reviewedForeignCommand(command string) bool {
	for _, prefix := range []string{"gh run list --repo ", "gh pr merge ", "gh release edit ", "gh release list -R ", "git -C "} {
		if strings.HasPrefix(command, prefix) {
			return true
		}
	}
	return false
}

func TestSuggestedCommandsAreCheckedAgainstTheTree(t *testing.T) {
	root, _ := newRoot("dev", io.Discard, io.Discard)
	for _, command := range []string{"stw status --offline core", "stw sync --plan core", "stw tidy plan core", "stw pulls --repo acme/core --bots-only"} {
		target, rest, err := root.Find(strings.Fields(command)[1:])
		if err != nil || target == root || target.ParseFlags(rest) != nil {
			t.Errorf("%q must resolve", command)
		}
	}
	for _, command := range []string{"stw status --online core", "stw tidyy plan core", "stw pulls --repos acme/core"} {
		target, rest, err := root.Find(strings.Fields(command)[1:])
		if err == nil && target != root && target.ParseFlags(rest) == nil {
			t.Errorf("%q must not resolve", command)
		}
	}
}
