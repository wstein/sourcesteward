// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"reflect"
	"testing"
)

func TestAttentionReasonsAreTheOneList(t *testing.T) {
	one, two := 1, 2
	rel := "behind"
	branch := "main"
	all := RepoStatus{
		Operation: &branch, Branch: &branch, Detached: true, Ahead: &one, Behind: &two,
		Changes: Changes{Staged: 1, Unstaged: 1, Untracked: 1, Conflicts: 1},
		Gitlink: &Gitlink{State: "drift", Relation: &rel},
	}
	want := []string{"operation", "conflicts", "staged", "unstaged", "untracked", "behind", "diverged", "no-upstream", "detached", "gitlink-drift"}
	if got := attention(all); !reflect.DeepEqual(got, want) {
		t.Fatalf("attention = %v", got)
	}
	all.State = "error"
	if got := attention(all); got[0] != "error" || len(got) != len(want)+1 {
		t.Fatalf("with error: %v", got)
	}
	if got := AttentionReasons(); !reflect.DeepEqual(got, append([]string{"error"}, want...)) {
		t.Fatalf("AttentionReasons = %v", got)
	}
	got := AttentionReasons()
	got[0] = "changed"
	if AttentionReasons()[0] != "error" {
		t.Fatal("the list must be a copy")
	}
	if got := attention(RepoStatus{State: "ok"}); len(got) != 0 || got == nil {
		t.Fatalf("clean repository: %#v", got)
	}
}
