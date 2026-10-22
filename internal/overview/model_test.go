// SPDX-License-Identifier: EUPL-1.2

package overview

import (
	"testing"

	"github.com/wstein/sourcesteward/internal/pulls"
)

// The action table of Spec 037 (rows 1 to 25) and Spec 038 (rows 26 to 28): the number is the position, the priority the colour of the row.
func TestCategoryTableMatchesTheSpec(t *testing.T) {
	spec := []struct {
		id       Category
		priority Priority
	}{
		{"repo-error", Red}, {"local-operation", Red}, {"main-red", Red}, {"release-run-failed", Red},
		{"read-error", Yellow}, {"local-changes", Yellow}, {"local-detached", Yellow}, {"local-untracked", Yellow},
		{"local-behind", Yellow}, {"local-no-upstream", Yellow}, {"main-behind-origin", Yellow}, {"gitlink-drift", Yellow},
		{"worktree-dirty", Yellow}, {"pr-conflict", Yellow}, {"pr-blocked", Yellow}, {"tag-without-release", Yellow},
		{"pr-ready", Green}, {"release-draft", Green},
		{"pr-checks-pending", Blue}, {"pr-unread", Blue}, {"local-stash", Blue}, {"gitlink-ahead", Blue},
		{"branches-unmerged", Blue}, {"worktree-stale", Blue},
		{"pr-bot", Violet},
		{"issue-security", Red}, {"issue-assigned", Yellow}, {"issue-bug", Yellow},
	}
	cats := Categories()
	if len(cats) != 28 || len(spec) != 28 {
		t.Fatalf("%d categories", len(cats))
	}
	for i, row := range spec {
		p, ok := PriorityOf(row.id)
		if cats[i] != row.id || !ok || p != row.priority || categoryNumber(row.id) != i+1 {
			t.Errorf("row %d %s: got %s %s %v", i+1, row.id, cats[i], p, ok)
		}
	}
	if _, ok := PriorityOf("nope"); ok {
		t.Error("unknown category has a priority")
	}
	if categoryNumber("nope") != 29 || rank("nope") != len(Priorities) {
		t.Error("unknown values sort last")
	}
	if got := count(nil); got != 0 {
		t.Error(got)
	}
}

// The verdict strings of the model are those of the pulls cycle (Spec 036), which this package must not import.
func TestVerdictStringsMatchThePullsCycle(t *testing.T) {
	pairs := [][2]string{
		{GroupReady, pulls.GroupReady}, {GroupChecks, pulls.GroupChecks}, {GroupConflict, pulls.GroupConflict},
		{GroupDraft, pulls.GroupDraft}, {GroupBlocked, pulls.GroupBlocked}, {PRReasonNotRead, pulls.ReasonNotRead},
	}
	for _, p := range pairs {
		if p[0] != p[1] {
			t.Errorf("%q != %q", p[0], p[1])
		}
	}
}
