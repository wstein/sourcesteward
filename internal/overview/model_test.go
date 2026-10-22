// SPDX-License-Identifier: EUPL-1.2

package overview

import "testing"

// The action table of Spec 037: the number is the position, the priority the colour of the row.
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
	}
	cats := Categories()
	if len(cats) != 25 || len(spec) != 25 {
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
	if categoryNumber("nope") != 26 || rank("nope") != len(Priorities) {
		t.Error("unknown values sort last")
	}
	if got := count(nil); got != 0 {
		t.Error(got)
	}
}
