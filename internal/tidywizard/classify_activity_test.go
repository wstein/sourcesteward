// SPDX-License-Identifier: EUPL-1.2

package tidywizard

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// The activity time the classifier already reads is kept on the candidate for the risk ladder
// (Spec 044 R1). It is never part of the JSON document and changes no verdict.
func TestCandidateKeepsTheObservedActivityTime(t *testing.T) {
	w := stwTree("a")
	w.LastActivity = now.Add(-100 * time.Hour)
	c := only(t, repo([]Worktree{w}, nil), cfg())
	if !c.LastActivity.Equal(w.LastActivity) {
		t.Fatalf("activity time = %v, want %v", c.LastActivity, w.LastActivity)
	}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(b)), "activity") {
		t.Fatalf("the activity time must not reach the JSON document: %s", b)
	}
}

func TestActivityTimeIsKeptOnRetainedWorktreesAndAbsentOnBranches(t *testing.T) {
	// A retained recently-active row keeps its time too; a branch has none.
	w := Worktree{Path: "/ws/wt/x", Lock: LockStw, Task: "x", Record: RecordFound, TaskOwner: "codex", Proof: ProofPatchID, LastActivity: now.Add(-time.Hour)}
	if c := only(t, repo([]Worktree{w}, nil), cfg()); c.Reason != "recently-active" || !c.LastActivity.Equal(w.LastActivity) {
		t.Fatalf("recently-active row: %+v", c)
	}
	b := Branch{Name: "feat/b", Proof: ProofAncestor}
	if c := only(t, repo(nil, []Branch{b}), cfg()); !c.LastActivity.IsZero() {
		t.Fatalf("a branch carries no activity time: %v", c.LastActivity)
	}
	// A stale registration has no directory and no activity.
	s := Worktree{Path: "/ws/wt/gone", DirMissing: true, LastActivity: now}
	if c := only(t, repo([]Worktree{s}, nil), cfg()); !c.LastActivity.IsZero() {
		t.Fatalf("a stale registration carries no activity time: %v", c.LastActivity)
	}
}
