// SPDX-License-Identifier: EUPL-1.2
package restack

import (
	"context"
	"strings"
	"testing"
)

const tenLines = "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\n"

func marks(rows []PairRow) string {
	var m []string
	for _, r := range rows {
		m = append(m, r.Mark)
	}
	return strings.Join(m, " ")
}

func TestRangePairingMarksEqualChangedAndUnpairedCommits(t *testing.T) {
	s := rebaseMerged(t)
	e := s.plan(t).Evidence
	if marks(e.RangePairing.Rows) != "= =" || !e.RangePairing.AllEqual || e.RangePairing.Rows[0].Old != s.childCommits[0] {
		t.Fatalf("%+v", e.RangePairing)
	}

	// The parent changed the context of the branch's hunk: the patch differs, the mark is `!`.
	changed := rebaseMergedWith(t, func(r *repo) { r.commit("B0", map[string]string{"ctx.txt": tenLines}) },
		func(r *repo) {
			r.commit("C1", map[string]string{"ctx.txt": strings.Replace(tenLines, "l5\n", "child5\n", 1)})
		},
		func(r *repo) {
			r.commit("M", map[string]string{"ctx.txt": strings.Replace(tenLines, "l3\n", "main3\n", 1)})
		})
	res := changed.plan(t)
	if res.Status != StatusOK || res.Evidence.Replay.State != "clean" || marks(res.Evidence.RangePairing.Rows) != "!" || res.Evidence.RangePairing.AllEqual {
		t.Fatalf("%s %+v %+v", res.Status, res.Diagnostics, res.Evidence.RangePairing)
	}

	// A conflict leaves every later commit unpaired; an already contained change is unpaired.
	conflict := rebaseMergedWith(t, func(r *repo) { r.commit("B1", map[string]string{"parent1.txt": "p1\n"}) },
		func(r *repo) {
			r.commit("C1", map[string]string{"shared.txt": "line1\nchild\nline3\n"})
			r.commit("C2", map[string]string{"c2.txt": "c2\n"})
		},
		func(r *repo) { r.commit("M", map[string]string{"shared.txt": "line1\nmain\nline3\n"}) })
	e = conflict.plan(t).Evidence
	if marks(e.RangePairing.Rows) != "unpaired unpaired" || e.RangePairing.AllEqual {
		t.Fatalf("%+v", e.RangePairing)
	}
	contained := rebaseMergedWith(t, func(r *repo) { r.commit("B1", map[string]string{"parent1.txt": "p1\n"}) },
		func(r *repo) {
			r.commit("C1", map[string]string{"shared.txt": "line1\nsame\nline3\n"})
			r.commit("C2", map[string]string{"c2.txt": "c2\n"})
		},
		func(r *repo) { r.commit("M", map[string]string{"shared.txt": "line1\nsame\nline3\n"}) })
	e = contained.plan(t).Evidence
	if marks(e.RangePairing.Rows) != "unpaired =" || e.RangePairing.AllEqual {
		t.Fatalf("%+v", e.RangePairing)
	}
}

func TestParentCommitsAreClassifiedByCherry(t *testing.T) {
	s := rebaseMerged(t)
	d := s.plan(t).Evidence.ParentDropped
	if !d.AllDropped || d.State != "dropped" || len(d.Rows) != 2 || d.Rows[0].Mark != "-" || d.Rows[0].State != "dropped" || d.Rows[1].OID != s.parentCommits[1] {
		t.Fatalf("%+v", d)
	}

	// A different patch with the same resulting content: `+`, tree equivalent for the touched paths.
	equivalent := rebaseMergedWith(t, func(r *repo) { r.commit("B1", map[string]string{"x.txt": "c\n"}) },
		func(r *repo) { r.commit("C1", map[string]string{"c1.txt": "c1\n"}) },
		func(r *repo) {})
	equivalent.git("reset", "-q", "--hard", equivalent.base)
	equivalent.commit("X1", map[string]string{"x.txt": "b\n"})
	equivalent.commit("X2", map[string]string{"x.txt": "c\n"})
	opts := equivalent.options()
	opts.NewParent = equivalent.oid("HEAD")
	d = Plan(context.Background(), opts).Evidence.ParentDropped
	if !d.AllDropped || d.Rows[0].Mark != "+" || d.Rows[0].State != "patch-equivalent-tree" {
		t.Fatalf("%+v", d)
	}

	// The same shape where the new parent does not carry the content: `+` and missing.
	missing := rebaseMergedWith(t, func(r *repo) { r.commit("B1", map[string]string{"x.txt": "c\n"}) },
		func(r *repo) { r.commit("C1", map[string]string{"c1.txt": "c1\n"}) },
		func(r *repo) {})
	missing.git("reset", "-q", "--hard", missing.base)
	missing.commit("X1", map[string]string{"x.txt": "b\n"})
	opts = missing.options()
	opts.NewParent = missing.oid("HEAD")
	d = Plan(context.Background(), opts).Evidence.ParentDropped
	if d.AllDropped || d.Rows[0].Mark != "+" || d.Rows[0].State != "missing" {
		t.Fatalf("%+v", d)
	}
}

func TestAnEmptyParentRangeIsUnknownNeverAPass(t *testing.T) {
	s := rebaseMerged(t)
	opts := s.options()
	opts.NewParent = s.oldParent // the parent is already part of the new parent's chain
	res := Plan(context.Background(), opts)
	d := res.Evidence.ParentDropped
	if res.Status != StatusOK || d.AllDropped || d.State != "unknown" || d.Reason != "empty-parent-range" || len(d.Rows) != 0 {
		t.Fatalf("%s %+v", res.Status, d)
	}
	if res.Outcome == OutcomeClean {
		t.Fatal("an empty parent range must not be clean")
	}
}

func TestOwnDiffsAreComparedByPatchId(t *testing.T) {
	s := rebaseMerged(t)
	o := s.plan(t).Evidence.OwnDiffEquality
	if !o.AllEqual || len(o.Rows) != 2 || o.Rows[0].State != "equal" || o.Rows[0].OldPatchID == "" || o.Rows[0].OldPatchID != o.Rows[0].PredictedPatchID {
		t.Fatalf("%+v", o)
	}
	// The parent renamed the file the branch edits: the replay follows the rename, the patch differs.
	renamed := rebaseMergedWith(t, func(r *repo) { r.commit("B0", map[string]string{"old.txt": tenLines}) },
		func(r *repo) {
			r.commit("C1", map[string]string{"old.txt": strings.Replace(tenLines, "l5\n", "child5\n", 1)})
		},
		func(r *repo) {
			r.git("mv", "old.txt", "new.txt")
			r.git("commit", "-q", "-m", "rename")
		})
	res := renamed.plan(t)
	o = res.Evidence.OwnDiffEquality
	if res.Evidence.Replay.State != "clean" || o.AllEqual || o.Rows[0].State != "differs" {
		t.Fatalf("%+v %+v", res.Evidence.Replay, o)
	}
	// Unreplayed commits are unknown.
	conflict := rebaseMergedWith(t, func(r *repo) { r.commit("B1", map[string]string{"parent1.txt": "p1\n"}) },
		func(r *repo) { r.commit("C1", map[string]string{"shared.txt": "line1\nchild\nline3\n"}) },
		func(r *repo) { r.commit("M", map[string]string{"shared.txt": "line1\nmain\nline3\n"}) })
	o = conflict.plan(t).Evidence.OwnDiffEquality
	if o.AllEqual || o.Rows[0].State != "unknown" {
		t.Fatalf("%+v", o)
	}
}

func TestIdentityComesOnlyFromTheFlag(t *testing.T) {
	s := rebaseMerged(t)
	id := s.plan(t).Evidence.Identity
	if id.State != "pass" || id.Source != "flag" || len(id.Mismatches) != 0 || len(id.Expected) != 1 {
		t.Fatalf("%+v", id)
	}
	opts := s.options()
	opts.Identities = []string{"Someone Else <else@example.org>"}
	id = Plan(context.Background(), opts).Evidence.Identity
	if id.State != "fail" || len(id.Mismatches) != 4 || id.Mismatches[0].Role != "author" || id.Mismatches[0].Commit != s.childCommits[0] || id.Mismatches[0].Value != ownerIdentity {
		t.Fatalf("%+v", id)
	}
	// Without the flag the check is unknown, even when the environment carries a placeholder
	// identity that equals the commits' identity.
	t.Setenv("GIT_AUTHOR_NAME", "Fixture Owner")
	t.Setenv("GIT_AUTHOR_EMAIL", "owner@example.org")
	t.Setenv("GIT_COMMITTER_NAME", "Fixture Owner")
	t.Setenv("GIT_COMMITTER_EMAIL", "owner@example.org")
	opts.Identities = nil
	res := Plan(context.Background(), opts)
	id = res.Evidence.Identity
	if id.State != "unknown" || id.Source != "none" || id.Reason != "no-expected-identity" || res.Outcome != OutcomeAttention {
		t.Fatalf("%s %+v", res.Outcome, id)
	}
}

func TestAPlaceholderIdentityIsAMismatchNotExpected(t *testing.T) {
	r := rebaseMergedWith(t, func(r *repo) { r.commit("B1", map[string]string{"p": "p\n"}) }, func(r *repo) {
		out, code := r.gitIn(r.dir, []string{"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t"}, "", "commit", "-q", "--allow-empty", "-m", "leaked")
		if code != 0 {
			t.Fatal(out)
		}
	}, func(r *repo) {})
	id := r.plan(t).Evidence.Identity
	if id.State != "fail" || id.Mismatches[0].Value != placeholder {
		t.Fatalf("%+v", id)
	}
}

func TestOverlapIsOnlyARiskIndicator(t *testing.T) {
	s := rebaseMerged(t)
	res := s.plan(t)
	o := res.Evidence.Overlap
	if o.Indicator != "none" || o.Count != 0 || len(o.Paths) != 0 || o.Note == "" {
		t.Fatalf("%+v", o)
	}
	s2 := rebaseMergedWith(t, func(r *repo) { r.commit("B0", map[string]string{"ctx.txt": tenLines}) },
		func(r *repo) {
			r.commit("C1", map[string]string{"ctx.txt": strings.Replace(tenLines, "l10\n", "child10\n", 1)})
		},
		func(r *repo) {
			r.commit("M", map[string]string{"ctx.txt": strings.Replace(tenLines, "l1\n", "main1\n", 1)})
		})
	res = s2.plan(t)
	o = res.Evidence.Overlap
	if res.Evidence.Replay.State != "clean" || o.Indicator != "some" || o.Count != 1 || o.Paths[0] != "ctx.txt" {
		t.Fatalf("%+v %+v", res.Evidence.Replay, o)
	}
	if res.Outcome == OutcomeClean {
		t.Fatal("an overlap is a risk indicator that keeps the outcome out of evidence-clean")
	}
}

func TestSemanticBreaksAreNeverReported(t *testing.T) {
	// The parent changes a signature that the branch calls in another file: a clean replay, no
	// overlap, and the result still says that semantic breaks are not detected.
	s := rebaseMergedWith(t, func(r *repo) { r.commit("B0", map[string]string{"lib.go": "func f(a int)\n", "use.go": "f(1)\n"}) },
		func(r *repo) { r.commit("C1", map[string]string{"call.go": "f(2)\n"}) },
		func(r *repo) { r.commit("M", map[string]string{"lib.go": "func f(a, b int)\n"}) })
	res := s.plan(t)
	if res.Evidence.Replay.State != "clean" || res.Evidence.Overlap.Indicator != "none" {
		t.Fatalf("%+v", res.Evidence)
	}
	found := false
	for _, l := range res.Limitations {
		found = found || l == "semantic-breaks-not-detected"
	}
	if !found || !strings.Contains(res.Notice, "does not assert that the restacked branch builds") {
		t.Fatalf("limitations %v notice %q", res.Limitations, res.Notice)
	}
}
