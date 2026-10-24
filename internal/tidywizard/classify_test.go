// SPDX-License-Identifier: EUPL-1.2

package tidywizard

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
)

var now = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

func cfg() Config {
	return Config{Caller: "werner", CWD: "/ws/repo/cwd-tree", Now: now}
}

var old = now.Add(-72 * time.Hour)

// stwTree is a completed, stw-created worktree: exact stw lock, matching record, clean, integrated.
func stwTree(name string) Worktree {
	return Worktree{
		Path: "/ws/wt/" + name, Branch: "feat/" + name, Lock: LockStw, Task: name, Record: RecordFound,
		TaskOwner: "werner", Proof: ProofPatchID, LastActivity: old, HasOwnCommit: true,
	}
}

// jjOwned is a jj workspace whose creation record was found by its destination (no stw lock).
func jjOwned(task string) Worktree { return jjOwnedBy(task, "werner") }

func jjOwnedBy(task, ownerLabel string) Worktree {
	return Worktree{
		Path: "/ws/wt/" + task, Task: task, JJOwned: true, TaskOwner: ownerLabel, Proof: ProofEmptyWorkingCopy,
		LastActivity: old,
	}
}

func repo(trees []Worktree, branches []Branch) RepoFacts {
	return RepoFacts{Name: "r", Worktrees: trees, Branches: branches}
}

func only(t *testing.T, r RepoFacts, c Config) Candidate {
	t.Helper()
	got := classifyRepo(r, c)
	if len(got) != 1 {
		t.Fatalf("want one candidate, got %d: %+v", len(got), got)
	}
	return got[0]
}

func TestWorktreeBehaviourTable(t *testing.T) {
	mod := func(f func(*Worktree)) Worktree { w := stwTree("a"); f(&w); return w }
	tests := []struct {
		name    string
		w       Worktree
		verdict Verdict
		reason  string
		scope   owner.Scope
		effects []string
	}{
		{"safe stw worktree", stwTree("a"), VerdictSafe, "", owner.ScopeOwn, []string{"remove-registration", "remove-files"}},
		{"unmerged", mod(func(w *Worktree) { w.Proof = ProofNone; w.Ahead = 3 }), VerdictRetain, "unmerged", owner.ScopeOwn, nil},
		{"dirty", mod(func(w *Worktree) { w.Dirty = true }), VerdictRetain, "dirty", owner.ScopeOwn, nil},
		{"ignored extras", mod(func(w *Worktree) { w.IgnoredExtras = true }), VerdictRetain, "ignored-extras", owner.ScopeOwn, nil},
		{"dirty and unmerged is dirty", mod(func(w *Worktree) { w.Dirty = true; w.Proof = ProofNone }), VerdictRetain, "dirty", owner.ScopeOwn, nil},
		{"locked-stw is a lock mismatch", mod(func(w *Worktree) { w.Record = RecordMismatch; w.TaskOwner = "" }), VerdictDecision, "locked-stw", owner.ScopeUnknown, []string{"remove-registration", "remove-files"}},
		{"lock mismatch but dirty retains", mod(func(w *Worktree) { w.Record = RecordMismatch; w.Dirty = true }), VerdictRetain, "dirty", owner.ScopeUnknown, nil},
		{"lock mismatch but unmerged retains", mod(func(w *Worktree) { w.Record = RecordMismatch; w.Proof = ProofNone }), VerdictRetain, "unmerged", owner.ScopeUnknown, nil},
		{"zero record state fails closed", mod(func(w *Worktree) { w.Record = RecordNone; w.TaskOwner = "" }), VerdictRetain, "registration-record-missing", owner.ScopeUnknown, nil},
		{"fresh lock mismatch is recently-active", mod(func(w *Worktree) { w.Record = RecordMismatch; w.TaskOwner = ""; w.LastActivity = now }), VerdictRetain, "recently-active", owner.ScopeUnknown, nil},
		{"zero activity of a foreign worktree fails closed", mod(func(w *Worktree) { w.Record = RecordMismatch; w.TaskOwner = ""; w.LastActivity = time.Time{} }), VerdictRetain, "evidence-incomplete", owner.ScopeUnknown, nil},
		{"record missing retains", mod(func(w *Worktree) { w.Record = RecordMissing; w.TaskOwner = "" }), VerdictRetain, "registration-record-missing", owner.ScopeUnknown, nil},
		{"record ambiguous retains", mod(func(w *Worktree) { w.Record = RecordAmbiguous; w.TaskOwner = "" }), VerdictRetain, "registration-record-ambiguous", owner.ScopeUnknown, nil},
		{"locked-foreign", mod(func(w *Worktree) { w.Lock = LockForeign; w.Record = RecordMissing }), VerdictRetain, "locked-foreign", owner.ScopeUnknown, nil},
		{"other owner by record", mod(func(w *Worktree) { w.TaskOwner = "codex" }), VerdictSafe, "", owner.ScopeOther, []string{"remove-registration", "remove-files"}},
		{"primary checkout", mod(func(w *Worktree) { w.Primary = true }), VerdictRetain, "primary-checkout", owner.ScopeOwn, nil},
		{"owned jj workspace is retained until a jj retirement kind exists", jjOwned("t"), VerdictRetain, "jj-retirement-unsupported", owner.ScopeOwn, nil},
		{"owned jj workspace of another owner", jjOwnedBy("t", "codex"), VerdictRetain, "jj-retirement-unsupported", owner.ScopeOther, nil},
		{"owned jj workspace that is dirty keeps the specific reason", func() Worktree { w := jjOwned("t"); w.Dirty = true; return w }(), VerdictRetain, "dirty", owner.ScopeOwn, nil},
		{"owned jj workspace that is unmerged keeps the specific reason", func() Worktree { w := jjOwned("t"); w.Proof = ProofNone; return w }(), VerdictRetain, "unmerged", owner.ScopeOwn, nil},
		{"owned jj workspace with incomplete evidence", func() Worktree { w := jjOwned("t"); w.Incomplete = "x"; return w }(), VerdictRetain, "evidence-incomplete", owner.ScopeOwn, nil},
		{"unowned jj workspace is unchanged", func() Worktree { w := jjOwned("t"); w.JJOwned, w.Task, w.TaskOwner = false, "", ""; return w }(), VerdictSafe, "unregistered", owner.ScopeUnknown, []string{"remove-registration", "remove-files"}},
		{"cwd worktree", mod(func(w *Worktree) { w.Path = "/ws/repo/cwd-tree" }), VerdictRetain, "current-worktree", owner.ScopeOwn, nil},
		{"sibling with a common prefix", mod(func(w *Worktree) { w.Path = "/ws/repo/cwd-tree-2" }), VerdictSafe, "", owner.ScopeOwn, []string{"remove-registration", "remove-files"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := only(t, repo([]Worktree{tc.w}, nil), cfg())
			if c.Verdict != tc.verdict || c.Reason != tc.reason || c.Scope != tc.scope {
				t.Fatalf("got %s/%q/%s, want %s/%q/%s", c.Verdict, c.Reason, c.Scope, tc.verdict, tc.reason, tc.scope)
			}
			if !reflect.DeepEqual(c.Effects, append([]string{}, tc.effects...)) {
				t.Fatalf("effects %v, want %v", c.Effects, tc.effects)
			}
			if c.Kind != KindWorktree || c.ID != "worktree:r:"+tc.w.Path || c.Repo != "r" {
				t.Fatalf("bad identity: %+v", c)
			}
		})
	}
}

func TestUnmergedEvidenceReportsAheadCount(t *testing.T) {
	w := stwTree("a")
	w.Proof, w.Ahead = ProofNone, 3
	c := only(t, repo([]Worktree{w}, nil), cfg())
	if !strings.Contains(c.Evidence, "3 commits ahead") {
		t.Fatalf("evidence %q", c.Evidence)
	}
}

func TestRecordMissingCarriesHint(t *testing.T) {
	w := stwTree("a")
	w.Record, w.TaskOwner = RecordMissing, ""
	c := only(t, repo([]Worktree{w}, nil), cfg())
	if !strings.Contains(c.Evidence, "pass the --state-dir that created it") {
		t.Fatalf("evidence %q", c.Evidence)
	}
}

func TestOwnerWithoutRecordIsNeverTaskRecord(t *testing.T) {
	w := stwTree("a")
	w.Record, w.TaskOwner, w.Branch = RecordMissing, "werner", "feat/a"
	c := only(t, repo([]Worktree{w}, nil), cfg())
	if c.OwnerSource == owner.SourceTaskRecord || c.Scope == owner.ScopeOwn {
		t.Fatalf("a missing record must not give task-record: %+v", c)
	}
	w.Branch = "claude/x"
	c = only(t, repo([]Worktree{w}, nil), cfg())
	if c.OwnerSource != owner.SourcePathConvention || c.Owner != "claude" {
		t.Fatalf("path convention expected: %+v", c)
	}
}

func unregistered(path, branch string) Worktree {
	return Worktree{Path: path, Branch: branch, Proof: ProofAncestor, LastActivity: old, HasOwnCommit: true}
}

func TestUnregisteredAndForeignWorktrees(t *testing.T) {
	t.Run("whr worktree is codex, safe when old, never own", func(t *testing.T) {
		c := only(t, repo([]Worktree{unregistered("/tmp/whr010-x", "")}, nil), cfg())
		if c.Verdict != VerdictSafe || c.Owner != "codex" || c.OwnerSource != owner.SourcePathConvention || c.Scope != owner.ScopeOther || c.Reason != "unregistered" || c.Evidence == "no-work" {
			t.Fatalf("%+v", c)
		}
	})
	t.Run("fresh whr worktree at the base commit is recently-active", func(t *testing.T) {
		w := unregistered("/tmp/whr010-x", "")
		w.LastActivity, w.HasOwnCommit = now.Add(-time.Hour), false
		c := only(t, repo([]Worktree{w}, nil), cfg())
		if c.Verdict != VerdictRetain || c.Reason != "recently-active" || len(c.Effects) != 0 {
			t.Fatalf("%+v", c)
		}
	})
	t.Run("activity at the window edge is still recent", func(t *testing.T) {
		w := unregistered("/tmp/whr010-x", "")
		w.LastActivity = now.Add(-RecentWindow + time.Second)
		if c := only(t, repo([]Worktree{w}, nil), cfg()); c.Reason != "recently-active" {
			t.Fatalf("%+v", c)
		}
		w.LastActivity = now.Add(-RecentWindow)
		if c := only(t, repo([]Worktree{w}, nil), cfg()); c.Verdict != VerdictSafe {
			t.Fatalf("%+v", c)
		}
	})
	t.Run("old and commitless carries no-work", func(t *testing.T) {
		w := unregistered("/tmp/whr010-x", "")
		w.HasOwnCommit = false
		c := only(t, repo([]Worktree{w}, nil), cfg())
		if c.Verdict != VerdictSafe || c.Evidence != "no-work" {
			t.Fatalf("%+v", c)
		}
	})
	t.Run("zero activity of an other worktree fails closed", func(t *testing.T) {
		w := unregistered("/tmp/whr010-x", "")
		w.LastActivity = time.Time{}
		c := only(t, repo([]Worktree{w}, nil), cfg())
		if c.Verdict != VerdictRetain || c.Reason != "evidence-incomplete" || len(c.Effects) != 0 {
			t.Fatalf("%+v", c)
		}
	})
	t.Run("a record without the stw lock is not honoured", func(t *testing.T) {
		w := stwTree("a")
		w.Lock = LockNone
		a := cfg()
		a.Agent = true
		c := only(t, repo([]Worktree{w}, nil), a)
		if c.OwnerSource == owner.SourceTaskRecord || c.Scope == owner.ScopeOwn || c.Reason != "unregistered" {
			t.Fatalf("%+v", c)
		}
	})
	t.Run("own worktree is not subject to recently-active", func(t *testing.T) {
		w := stwTree("a")
		w.LastActivity = now
		if c := only(t, repo([]Worktree{w}, nil), cfg()); c.Verdict != VerdictSafe {
			t.Fatalf("%+v", c)
		}
	})
	t.Run("unknown owner worktree is safe but scope unknown", func(t *testing.T) {
		c := only(t, repo([]Worktree{unregistered("/home/x/misc", "topic")}, nil), cfg())
		if c.Verdict != VerdictSafe || c.Scope != owner.ScopeUnknown || c.OwnerSource != owner.SourceUnknown {
			t.Fatalf("%+v", c)
		}
	})
	t.Run("author hint never makes own", func(t *testing.T) {
		w := unregistered("/home/x/misc", "topic")
		w.AuthorHint = "werner"
		c := only(t, repo([]Worktree{w}, nil), cfg())
		if c.Scope != owner.ScopeUnknown || c.OwnerSource != owner.SourceCommitAuthor {
			t.Fatalf("%+v", c)
		}
	})
	t.Run("agent mode: path convention is not own", func(t *testing.T) {
		a := cfg()
		a.Caller, a.Agent = "codex", true
		c := only(t, repo([]Worktree{unregistered("/tmp/whr010-x", "")}, nil), a)
		if c.Scope != owner.ScopeUnknown {
			t.Fatalf("%+v", c)
		}
	})
	t.Run("dirty, unmerged or foreign-locked stay retained", func(t *testing.T) {
		for reason, f := range map[string]func(*Worktree){
			"dirty":          func(w *Worktree) { w.Dirty = true },
			"unmerged":       func(w *Worktree) { w.Proof = ProofNone },
			"locked-foreign": func(w *Worktree) { w.Lock = LockForeign },
		} {
			w := unregistered("/tmp/whr010-x", "")
			f(&w)
			if c := only(t, repo([]Worktree{w}, nil), cfg()); c.Verdict != VerdictRetain || c.Reason != reason {
				t.Fatalf("%s: %+v", reason, c)
			}
		}
	})
}

func TestStaleRegistration(t *testing.T) {
	stale := func(f func(*Worktree)) Worktree {
		w := stwTree("gone")
		w.DirMissing, w.Prunable = true, true
		f(&w)
		return w
	}
	c := only(t, repo([]Worktree{stale(func(*Worktree) {})}, nil), cfg())
	// No existing plan kind removes a stale registration (T012b), so a provably prunable one is
	// retained with a stable reason and carries no effect.
	if c.Kind != KindStaleWorktree || c.Verdict != VerdictRetain || c.Reason != "stale-registration-no-plan-kind" || len(c.Effects) != 0 || c.ID != "stale-worktree:r:/ws/wt/gone" {
		t.Fatalf("%+v", c)
	}
	c = only(t, repo([]Worktree{stale(func(w *Worktree) { w.Prunable = false })}, nil), cfg())
	if c.Verdict != VerdictRetain || c.Reason != "evidence-incomplete" {
		t.Fatalf("%+v", c)
	}
	for name, f := range map[string]struct {
		mod    func(*Worktree)
		reason string
	}{
		"record missing":   {func(w *Worktree) { w.Record = RecordMissing }, "registration-record-missing"},
		"zero record":      {func(w *Worktree) { w.Record = RecordNone }, "registration-record-missing"},
		"record ambiguous": {func(w *Worktree) { w.Record = RecordAmbiguous }, "registration-record-ambiguous"},
		"lock mismatch":    {func(w *Worktree) { w.Record = RecordMismatch }, "lock-mismatch"},
		"no stw lock":      {func(w *Worktree) { w.Lock = LockNone }, "unregistered"},
	} {
		c = only(t, repo([]Worktree{stale(f.mod)}, nil), cfg())
		if c.Verdict != VerdictRetain || c.Reason != f.reason || len(c.Effects) != 0 {
			t.Fatalf("%s: %+v", name, c)
		}
		if f.reason == "registration-record-missing" && !strings.Contains(c.Evidence, "pass the --state-dir that created it") {
			t.Fatalf("%s: hint lost: %q", name, c.Evidence)
		}
	}
}

func TestBranches(t *testing.T) {
	br := func(name string, p Proof) Branch { return Branch{Name: name, Proof: p} }
	t.Run("integrated and not checked out is safe", func(t *testing.T) {
		c := only(t, repo(nil, []Branch{br("feat/x", ProofAncestor)}), cfg())
		if c.Kind != KindBranch || c.Verdict != VerdictSafe || c.ID != "branch:r:feat/x" || !reflect.DeepEqual(c.Effects, []string{"delete-branch:feat/x"}) {
			t.Fatalf("%+v", c)
		}
	})
	t.Run("not integrated is unmerged", func(t *testing.T) {
		b := br("feat/x", ProofNone)
		b.Ahead = 2
		c := only(t, repo(nil, []Branch{b}), cfg())
		if c.Verdict != VerdictRetain || c.Reason != "unmerged" || !strings.Contains(c.Evidence, "2 commits ahead") {
			t.Fatalf("%+v", c)
		}
	})
	t.Run("the base branch is never a candidate", func(t *testing.T) {
		b := br("main", ProofAncestor)
		b.IsBase = true
		if got := classifyRepo(repo(nil, []Branch{b}), cfg()); len(got) != 0 {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("attached to a retained worktree", func(t *testing.T) {
		w := stwTree("a")
		w.Dirty, w.Branch = true, "feat/x"
		got := classifyRepo(repo([]Worktree{w}, []Branch{br("feat/x", ProofAncestor)}), cfg())
		c := byKind(got, KindBranch)
		if c.Verdict != VerdictRetain || c.Reason != "branch-attached-elsewhere" || len(c.Effects) != 0 {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("attached to a safe worktree is deleted by its effect, not listed twice", func(t *testing.T) {
		w := stwTree("a")
		w.Branch = "feat/x"
		got := classifyRepo(repo([]Worktree{w}, []Branch{br("feat/x", ProofAncestor)}), cfg())
		if len(got) != 1 || !reflect.DeepEqual(got[0].Effects, []string{"remove-registration", "remove-files", "delete-branch:feat/x"}) {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("safe worktree of an unproven branch deletes no branch and lists it as unmerged", func(t *testing.T) {
		w := stwTree("a")
		w.Branch = "feat/x"
		got := classifyRepo(repo([]Worktree{w}, []Branch{br("feat/x", ProofNone)}), cfg())
		if len(got) != 2 || !reflect.DeepEqual(byKind(got, KindWorktree).Effects, []string{"remove-registration", "remove-files"}) || byKind(got, KindBranch).Reason != "unmerged" {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("a zero proof branch gets no delete effect", func(t *testing.T) {
		w := stwTree("a")
		w.Branch = "feat/x"
		got := classifyRepo(repo([]Worktree{w}, []Branch{{Name: "feat/x"}}), cfg())
		if len(got) != 2 || !reflect.DeepEqual(byKind(got, KindWorktree).Effects, []string{"remove-registration", "remove-files"}) || byKind(got, KindBranch).Reason != "unmerged" {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("a safe worktree on the base branch never deletes it", func(t *testing.T) {
		w := stwTree("a")
		w.Branch = "main"
		got := classifyRepo(repo([]Worktree{w}, []Branch{{Name: "main", IsBase: true, Proof: ProofAncestor}}), cfg())
		if len(got) != 1 || !reflect.DeepEqual(got[0].Effects, []string{"remove-registration", "remove-files"}) {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("a stale registration does not count as attached", func(t *testing.T) {
		live, gone := stwTree("a"), stwTree("gone")
		live.Branch, gone.Branch, gone.DirMissing, gone.Prunable = "feat/x", "feat/x", true, true
		got := classifyRepo(repo([]Worktree{live, gone}, []Branch{br("feat/x", ProofAncestor)}), cfg())
		if !reflect.DeepEqual(byID(got, "worktree:r:/ws/wt/a").Effects, []string{"remove-registration", "remove-files", "delete-branch:feat/x"}) {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("a branch used by two worktrees is not deleted with one", func(t *testing.T) {
		a, b := stwTree("a"), stwTree("b")
		a.Branch, b.Branch, b.Dirty = "feat/x", "feat/x", true
		got := classifyRepo(repo([]Worktree{a, b}, []Branch{br("feat/x", ProofAncestor)}), cfg())
		for _, c := range got {
			for _, e := range c.Effects {
				if strings.HasPrefix(e, "delete-branch") {
					t.Fatalf("branch deleted while attached elsewhere: %+v", got)
				}
			}
		}
	})
	t.Run("branch owner by convention", func(t *testing.T) {
		c := only(t, repo(nil, []Branch{br("codex/x", ProofAncestor)}), cfg())
		if c.Owner != "codex" || c.Scope != owner.ScopeOther {
			t.Fatalf("%+v", c)
		}
	})
	t.Run("tracking refs", func(t *testing.T) {
		r := RepoFacts{Name: "r", TrackingRefs: []TrackingRef{{Name: "origin/done", Proof: ProofAncestor}, {Name: "origin/open", Proof: ProofNone}}}
		got := classifyRepo(r, cfg())
		if len(got) != 2 || got[0].ID != "tracking-ref:r:origin/done" || got[0].Verdict != VerdictSafe || !reflect.DeepEqual(got[0].Effects, []string{"delete-tracking-ref:origin/done"}) || got[1].Reason != "unmerged" {
			t.Fatalf("%+v", got)
		}
	})
}

func byID(cs []Candidate, id string) Candidate {
	for _, c := range cs {
		if c.ID == id {
			return c
		}
	}
	return Candidate{}
}

func byKind(cs []Candidate, k Kind) Candidate {
	for _, c := range cs {
		if c.Kind == k {
			return c
		}
	}
	return Candidate{}
}

type stubSource map[string]func() (RepoFacts, error)

func (s stubSource) Facts(_ context.Context, name string) (RepoFacts, error) { return s[name]() }

func mixedRepo(name string) func() (RepoFacts, error) {
	return func() (RepoFacts, error) {
		w1, w2, w3 := stwTree("a-"+name), stwTree("b-"+name), unregistered("/tmp/whr1-"+name, "")
		w2.Dirty = true
		w3.LastActivity = now
		return RepoFacts{Name: name, Worktrees: []Worktree{w1, w2, w3},
			Branches:     []Branch{{Name: "x", Proof: ProofAncestor}, {Name: "y", Proof: ProofNone}},
			TrackingRefs: []TrackingRef{{Name: "origin/z", Proof: ProofAncestor}}}, nil
	}
}

func TestClassifyAcrossRepositories(t *testing.T) {
	src := stubSource{"a": mixedRepo("a"), "b": mixedRepo("b"), "c": mixedRepo("c")}
	want := Classify(context.Background(), src, []string{"a", "b", "c"}, cfg())
	if len(want.Repositories) != 3 || want.Truncated {
		t.Fatalf("%+v", want.Repositories)
	}
	rng := rand.New(rand.NewSource(7))
	for i := 0; i < 20; i++ {
		order := []string{"a", "b", "c"}
		rng.Shuffle(3, func(i, j int) { order[i], order[j] = order[j], order[i] })
		if got := Classify(context.Background(), src, order, cfg()); !reflect.DeepEqual(got, want) {
			t.Fatalf("order %v changed the result", order)
		}
	}
}

func TestCandidatesSortedAndIDsStableAndUnique(t *testing.T) {
	r := mixedRepo("a")
	f, _ := r()
	rng := rand.New(rand.NewSource(3))
	want := classifyRepo(f, cfg())
	for i := 0; i < 20; i++ {
		g := f
		g.Worktrees = append([]Worktree{}, f.Worktrees...)
		g.Branches = append([]Branch{}, f.Branches...)
		rng.Shuffle(len(g.Worktrees), func(i, j int) { g.Worktrees[i], g.Worktrees[j] = g.Worktrees[j], g.Worktrees[i] })
		rng.Shuffle(len(g.Branches), func(i, j int) { g.Branches[i], g.Branches[j] = g.Branches[j], g.Branches[i] })
		if got := classifyRepo(g, cfg()); !reflect.DeepEqual(got, want) {
			t.Fatalf("fact order changed the candidates")
		}
	}
	seen := map[string]bool{}
	for _, c := range want {
		if seen[c.ID] {
			t.Fatalf("duplicate id %s", c.ID)
		}
		seen[c.ID] = true
	}
}

func TestNoRetainCarriesAnEffect(t *testing.T) {
	res := Classify(context.Background(), stubSource{"a": mixedRepo("a")}, []string{"a"}, cfg())
	retained := 0
	for _, c := range res.Candidates {
		if c.Verdict == VerdictRetain {
			retained++
			if len(c.Effects) != 0 {
				t.Fatalf("retain with effects: %+v", c)
			}
		}
		if c.Effects == nil {
			t.Fatalf("effects must be an empty list, not null: %+v", c)
		}
	}
	if retained == 0 {
		t.Fatal("fixture has no retain row")
	}
}

func TestErroringRepositoryDoesNotHideTheRest(t *testing.T) {
	src := stubSource{
		"a": mixedRepo("a"),
		"b": func() (RepoFacts, error) { return RepoFacts{}, errors.New("boom") },
		"c": func() (RepoFacts, error) {
			return RepoFacts{Name: "c", Skip: "base-ambiguous", Worktrees: []Worktree{stwTree("x")}}, nil
		},
	}
	res := Classify(context.Background(), src, []string{"a", "b", "c"}, cfg())
	states := map[string]RepoResult{}
	for _, r := range res.Repositories {
		states[r.Name] = r
	}
	if states["a"].State != "analysed" || states["b"].State != "error" || states["b"].Reason != "boom" || states["c"].State != "skipped" || states["c"].Reason != "base-ambiguous" {
		t.Fatalf("%+v", res.Repositories)
	}
	for _, c := range res.Candidates {
		if c.Repo != "a" {
			t.Fatalf("candidate of a non-analysed repository: %+v", c)
		}
	}
	if len(res.Candidates) == 0 {
		t.Fatal("the healthy repository was lost")
	}
}

func TestCancelledContextReportsRemainingRepositories(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := Classify(ctx, stubSource{"a": mixedRepo("a")}, []string{"a"}, cfg())
	if len(res.Repositories) != 1 || res.Repositories[0].State != "error" || len(res.Candidates) != 0 {
		t.Fatalf("%+v", res)
	}
}

func TestBounds(t *testing.T) {
	t.Run("evidence is cut at 512 bytes on a rune boundary", func(t *testing.T) {
		w := stwTree("a")
		w.Proof, w.Ahead = ProofNone, 1
		w.Path = "/ws/" + strings.Repeat("é", 600)
		w.Branch = strings.Repeat("é", 600)
		c := only(t, repo([]Worktree{w}, nil), cfg())
		if len(c.Evidence) > MaxEvidenceBytes || !strings.HasSuffix(c.Evidence, "...") {
			t.Fatalf("evidence %d bytes", len(c.Evidence))
		}
		for _, prefix := range []string{"", "x"} {
			w.Branch = prefix + strings.Repeat("é", 600)
			if c := only(t, repo([]Worktree{w}, nil), cfg()); !utf8.ValidString(c.Evidence) {
				t.Fatalf("evidence cut inside a rune with prefix %q", prefix)
			}
		}
	})
	t.Run("4096 candidates then truncated", func(t *testing.T) {
		var trees []Worktree
		for i := 0; i < MaxCandidates+10; i++ {
			trees = append(trees, stwTree(fmt.Sprintf("w%05d", i)))
		}
		res := Classify(context.Background(), stubSource{"r": func() (RepoFacts, error) { return repo(trees, nil), nil }}, []string{"r"}, cfg())
		if len(res.Candidates) != MaxCandidates || !res.Truncated {
			t.Fatalf("%d candidates, truncated %v", len(res.Candidates), res.Truncated)
		}
		if res.Candidates[0].ID != "worktree:r:/ws/wt/w00000" {
			t.Fatalf("truncation must keep the sorted prefix: %s", res.Candidates[0].ID)
		}
	})
}

func TestDuplicateRepositoryNamesAreAnalysedOnce(t *testing.T) {
	src := stubSource{"a": mixedRepo("a")}
	one := Classify(context.Background(), src, []string{"a"}, cfg())
	two := Classify(context.Background(), src, []string{"a", "a"}, cfg())
	if !reflect.DeepEqual(one, two) {
		t.Fatalf("duplicate names changed the result: %d vs %d candidates", len(one.Candidates), len(two.Candidates))
	}
}

// An observation that could not finish is retained as evidence-incomplete for every owner scope
// and carries no effect, whatever the other facts say.
func TestIncompleteFactsAreRetainedForEveryScope(t *testing.T) {
	t.Run("own worktree", func(t *testing.T) {
		w := stwTree("a")
		w.Incomplete = "deadline-exceeded"
		c := only(t, repo([]Worktree{w}, nil), cfg())
		if c.Verdict != VerdictRetain || c.Reason != "evidence-incomplete" || len(c.Effects) != 0 || c.Scope != owner.ScopeOwn || !strings.Contains(c.Evidence, "deadline-exceeded") {
			t.Fatalf("%+v", c)
		}
	})
	t.Run("other worktree", func(t *testing.T) {
		w := unregistered("/tmp/whr010-x", "")
		w.Incomplete = "registration-mismatch"
		c := only(t, repo([]Worktree{w}, nil), cfg())
		if c.Verdict != VerdictRetain || c.Reason != "evidence-incomplete" || len(c.Effects) != 0 {
			t.Fatalf("%+v", c)
		}
	})
	t.Run("lock mismatch is not selectable when incomplete", func(t *testing.T) {
		w := stwTree("a")
		w.Record, w.TaskOwner, w.Incomplete = RecordMismatch, "", "observation-failed"
		if c := only(t, repo([]Worktree{w}, nil), cfg()); c.Verdict != VerdictRetain || c.Reason != "evidence-incomplete" {
			t.Fatalf("%+v", c)
		}
	})
	t.Run("primary and current worktree keep their reason", func(t *testing.T) {
		w := stwTree("a")
		w.Primary, w.Incomplete = true, "x"
		if c := only(t, repo([]Worktree{w}, nil), cfg()); c.Reason != "primary-checkout" {
			t.Fatalf("%+v", c)
		}
	})
	t.Run("branch", func(t *testing.T) {
		c := only(t, repo(nil, []Branch{{Name: "feat/x", Proof: ProofAncestor, Incomplete: "branch-limit"}}), cfg())
		if c.Verdict != VerdictRetain || c.Reason != "evidence-incomplete" || len(c.Effects) != 0 {
			t.Fatalf("%+v", c)
		}
	})
	t.Run("tracking reference", func(t *testing.T) {
		c := only(t, RepoFacts{Name: "r", TrackingRefs: []TrackingRef{{Name: "origin/x", Proof: ProofAncestor, Incomplete: "hosting"}}}, cfg())
		if c.Verdict != VerdictRetain || c.Reason != "evidence-incomplete" || len(c.Effects) != 0 {
			t.Fatalf("%+v", c)
		}
	})
	t.Run("an incomplete branch is not deleted by its safe worktree", func(t *testing.T) {
		w := stwTree("a")
		w.Branch = "feat/x"
		got := classifyRepo(repo([]Worktree{w}, []Branch{{Name: "feat/x", Proof: ProofAncestor, Incomplete: "x"}}), cfg())
		for _, c := range got {
			for _, e := range c.Effects {
				if strings.HasPrefix(e, "delete-branch") {
					t.Fatalf("an incomplete branch got a delete effect: %+v", got)
				}
			}
		}
	})
}

// A branch the observer keeps for a native reason is retained with that reason and carries no
// effect, whatever its proof says.
func TestBranchKeepReasonRetainsAProvenBranch(t *testing.T) {
	c := only(t, repo(nil, []Branch{{Name: "pushed", Proof: ProofAncestor, Reason: "remote-counterpart"}}), cfg())
	if c.Verdict != VerdictRetain || c.Reason != "remote-counterpart" || len(c.Effects) != 0 {
		t.Fatalf("%+v", c)
	}
	// Incomplete still outranks the reason.
	c = only(t, repo(nil, []Branch{{Name: "pushed", Proof: ProofAncestor, Reason: "remote-counterpart", Incomplete: "x"}}), cfg())
	if c.Reason != "evidence-incomplete" {
		t.Fatalf("%+v", c)
	}
	// A kept branch is not deleted with a safe worktree.
	w := stwTree("a")
	w.Branch = "kept"
	got := classifyRepo(repo([]Worktree{w}, []Branch{{Name: "kept", Proof: ProofAncestor, Reason: "recovery-reference"}}), cfg())
	for _, c := range got {
		for _, e := range c.Effects {
			if strings.HasPrefix(e, "delete-branch") {
				t.Fatalf("a kept branch got a delete effect: %+v", got)
			}
		}
	}
}
