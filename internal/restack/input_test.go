// SPDX-License-Identifier: EUPL-1.2
package restack

import (
	"context"
	"strings"
	"testing"
)

func TestPlanResolvesTheExplicitInput(t *testing.T) {
	s := rebaseMerged(t)
	res := s.plan(t)
	if res.Status != StatusOK || res.Backend != "git" || res.Identity == nil {
		t.Fatalf("status %s backend %s diagnostics %+v", res.Status, res.Backend, res.Diagnostics)
	}
	in := res.Input
	if in.Branch.Ref != "refs/heads/child" || in.Branch.Short != "child" || in.Branch.OldHead == nil || *in.Branch.OldHead != s.oldHead {
		t.Fatalf("branch %+v", in.Branch)
	}
	if in.OldParent.OID == nil || *in.OldParent.OID != s.oldParent || in.NewParent.OID == nil || *in.NewParent.OID != s.newParent {
		t.Fatalf("parents %+v %+v", in.OldParent, in.NewParent)
	}
	if in.Moved.Count != 2 || len(in.Moved.Commits) != 2 || in.Moved.Commits[0].OID != s.childCommits[0] || in.Moved.Commits[1].OID != s.childCommits[1] {
		t.Fatalf("moved range is not exactly old_parent..old_head: %+v", in.Moved)
	}
	if in.Moved.Commits[0].Subject != "C1" || in.Moved.Commits[0].Author != ownerIdentity || len(in.Moved.Commits[0].Parents) != 1 {
		t.Fatalf("moved commit %+v", in.Moved.Commits[0])
	}
	if in.Base == nil || in.Base.Old != s.oldParent || in.Base.New != s.base {
		t.Fatalf("base %+v", in.Base)
	}
}

func TestPlanResolvesFullyQualifiedRefs(t *testing.T) {
	s := rebaseMerged(t)
	o := s.options()
	o.OldParent, o.NewParent = "refs/heads/parent", "refs/heads/main"
	res := Plan(context.Background(), o)
	if res.Status != StatusOK || *res.Input.OldParent.OID != s.oldParent || *res.Input.NewParent.OID != s.newParent {
		t.Fatalf("%+v", res.Diagnostics)
	}
}

func TestPlanRefusals(t *testing.T) {
	s := rebaseMerged(t)
	missing := strings.Repeat("a", 40)
	cases := []struct {
		name   string
		code   string
		mutate func(*Options)
	}{
		{"branch missing", "ref-not-found", func(o *Options) { o.Branch = "refs/heads/nope" }},
		{"old parent ref missing", "ref-not-found", func(o *Options) { o.OldParent = "refs/heads/nope" }},
		{"old parent object missing", "object-missing", func(o *Options) { o.OldParent = missing }},
		{"new parent object missing", "object-missing", func(o *Options) { o.NewParent = missing }},
		{"head changed", "head-changed", func(o *Options) { o.ExpectHead = s.oldParent }},
		{"empty moved range", "empty-moved-range", func(o *Options) { o.Branch = "refs/heads/parent" }},
		{"old parent is the new parent descendant side", "old-parent-not-ancestor", func(o *Options) { o.OldParent = s.newParent; o.NewParent = s.oldParent }},
		{"tag object is not a commit id", "ref-not-commit", func(o *Options) { o.OldParent = "refs/tags/blob" }},
	}
	s.git("tag", "blob", s.git("hash-object", "-w", "--stdin"))
	for _, tc := range cases {
		o := s.options()
		tc.mutate(&o)
		res := Plan(context.Background(), o)
		if res.Status != StatusError || firstCode(res) != tc.code || res.Evidence != nil || res.Outcome != OutcomeNone {
			t.Errorf("%s: status %s code %q evidence %v", tc.name, res.Status, firstCode(res), res.Evidence)
		}
	}
}

func TestOldParentMustBeAnAncestorOfTheBranch(t *testing.T) {
	s := rebaseMerged(t)
	s.checkout("-b", "sibling", s.base)
	sibling := s.commit("S1", map[string]string{"sib.txt": "s\n"})
	o := s.options()
	o.OldParent = sibling // not reachable from child: its own commits would be moved
	res := Plan(context.Background(), o)
	if firstCode(res) != "old-parent-not-ancestor" || res.Status != StatusError {
		t.Fatalf("%s %+v", res.Status, res.Diagnostics)
	}
	o = s.options()
	o.OldParent = s.childCommits[0] // an ancestor inside the branch
	res = Plan(context.Background(), o)
	if res.Status != StatusOK || res.Input.Moved.Count != 1 {
		t.Fatalf("an ancestor inside the branch must move only the tail: %s %+v", res.Status, res.Diagnostics)
	}
}

func TestUnrelatedHistoriesAreRefused(t *testing.T) {
	s := rebaseMerged(t)
	s.git("checkout", "-q", "--orphan", "other")
	s.commit("O1", map[string]string{"o.txt": "o\n"})
	o := s.options()
	o.NewParent = "refs/heads/other"
	res := Plan(context.Background(), o)
	if firstCode(res) != "unrelated-histories" {
		t.Fatalf("%s %+v", res.Status, res.Diagnostics)
	}
}

func TestMergeCommitInRangeIsRefused(t *testing.T) {
	s := rebaseMerged(t)
	s.checkout("child")
	s.checkout("-b", "side", s.oldParent)
	s.commit("X", map[string]string{"x.txt": "x\n"})
	s.checkout("child")
	s.git("merge", "-q", "--no-ff", "-m", "merge side", "side")
	res := s.plan(t)
	if firstCode(res) != "merge-commit-in-range" || res.Status != StatusError {
		t.Fatalf("%s %+v", res.Status, res.Diagnostics)
	}
}

func TestRangeTooLargeIsIncomplete(t *testing.T) {
	s := rebaseMergedWith(t, func(r *repo) { r.commit("B", map[string]string{"b": "b\n"}) }, func(r *repo) {
		// 201 commits written in one fast-import stream keep the fixture quick.
		var b strings.Builder
		b.WriteString("reset refs/heads/child\nfrom refs/heads/parent\n")
		for i := 0; i < MaxCommits+1; i++ {
			b.WriteString("commit refs/heads/child\ncommitter F <f@x> " + itoa(1_800_000_000+i) + " +0000\ndata 1\nc\nM 100644 inline f.txt\ndata " + itoa(len(itoa(i))+1) + "\n" + itoa(i) + "\n\n")
		}
		if out, code := r.gitIn(r.dir, nil, b.String(), "fast-import", "--quiet"); code != 0 {
			t.Fatal(out)
		}
		r.checkout("-f", "child")
	}, func(r *repo) { r.commit("M", map[string]string{"m": "m\n"}) })
	res := s.plan(t)
	if res.Status != StatusIncomplete || firstCode(res) != "range-too-large" || res.Boundary.Reached == nil || *res.Boundary.Reached != "max_commits" || res.Outcome != OutcomeNone {
		t.Fatalf("%s %+v %v", res.Status, res.Diagnostics, res.Boundary.Reached)
	}
}

func TestAnnotatedTagRefPeelsToItsCommit(t *testing.T) {
	s := rebaseMerged(t)
	s.git("tag", "-a", "-m", "release", "v1", s.oldParent)
	o := s.options()
	o.OldParent = "refs/tags/v1"
	res := Plan(context.Background(), o)
	if res.Status != StatusOK || *res.Input.OldParent.OID != s.oldParent {
		t.Fatalf("%s %+v", res.Status, res.Diagnostics)
	}
}

func TestACommitIdNamedLikeARefIsAmbiguous(t *testing.T) {
	s := rebaseMerged(t)
	s.git("update-ref", "refs/heads/"+s.oldParent, s.base)
	res := s.plan(t)
	if firstCode(res) != "ref-ambiguous" {
		t.Fatalf("%s %+v", res.Status, res.Diagnostics)
	}
}
