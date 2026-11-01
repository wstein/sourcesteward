// SPDX-License-Identifier: EUPL-1.2
package carryover

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func wantReasons(t *testing.T, res Result, want ...string) {
	t.Helper()
	if want == nil {
		want = []string{}
	}
	if !reflect.DeepEqual(res.Reasons, want) {
		t.Fatalf("reasons = %v, want %v\n%+v", res.Reasons, want, res)
	}
	if res.CarryOver != (len(want) == 0) {
		t.Fatalf("carry_over = %v with reasons %v", res.CarryOver, res.Reasons)
	}
	if (res.Line != nil) != res.CarryOver {
		t.Fatalf("line %v with carry_over %v", res.Line, res.CarryOver)
	}
}

func TestPureRebaseIsCarriedOverWithTheExactLine(t *testing.T) {
	s := newStack(t)
	head := s.rebased("new")
	res := s.check(t, s.m2, head)
	wantReasons(t, res)
	if res.PatchID.State != StateEqual || res.MergeCommits.State != StateNone || res.RangeDiff.State != StateEqual || res.RangeDiff.Total != 2 {
		t.Fatalf("%+v", res)
	}
	want := "Carried-over: from=" + s.c2 + " to=" + head + " patch-id=" + res.PatchID.New + " pr=12"
	if *res.Line != want {
		t.Fatalf("line %q, want %q", *res.Line, want)
	}
	if strings.Count(*res.Line, "\n") != 0 || len(res.PatchID.New) != 40 {
		t.Fatalf("line %q", *res.Line)
	}
}

func TestIdenticalHeadsAreCarriedOver(t *testing.T) {
	s := newStack(t)
	wantReasons(t, s.check(t, s.m1, s.c2))
}

func TestMergeCommitRefusesAndOnlyThatCondition(t *testing.T) {
	s := newStack(t)
	s.r.git("checkout", "-q", "-b", "new", "feature")
	s.r.git("merge", "-q", "--no-edit", "main")
	res := s.check(t, s.m2, s.r.head())
	wantReasons(t, res, ReasonMergeCommits)
	if res.MergeCommits.Count != 1 || res.MergeCommits.State != StateFound || len(res.MergeCommits.Commits) != 1 {
		t.Fatalf("%+v", res.MergeCommits)
	}
	if res.PatchID.State != StateEqual || res.RangeDiff.State != StateEqual {
		t.Fatalf("the other two conditions hold: %+v", res)
	}
}

func TestPatchIDChangeRefuses(t *testing.T) {
	s := newStack(t)
	s.r.git("checkout", "-q", "-b", "new", "feature")
	s.r.git("rebase", "-q", "main")
	s.r.commit("b.txt", "x = 1\ny = 4\n", "tweak")
	s.r.git("reset", "-q", "--soft", "HEAD~1")
	s.r.git("commit", "-q", "--amend", "--no-edit")
	res := s.check(t, s.m2, s.r.head())
	wantReasons(t, res, ReasonPatchIDDiffers, ReasonRangeDiff)
	if res.PatchID.State != StateDiffers || res.PatchID.Old == res.PatchID.New {
		t.Fatalf("%+v", res.PatchID)
	}
}

func TestWhitespaceOnlyChangeKeepsThePatchIDButRangeDiffRefuses(t *testing.T) {
	s := newStack(t)
	s.r.git("checkout", "-q", "-b", "new", "feature")
	s.r.git("rebase", "-q", "main")
	// Amend the first commit with trailing whitespace: edit it by an interactive-free rebase.
	s.r.git("reset", "-q", "--hard", "HEAD~2")
	s.r.git("cherry-pick", s.c1)
	s.r.write("a.txt", strings.Replace(c1File, "TWO", "TWO ", 1))
	s.r.git("commit", "-q", "-a", "--amend", "--no-edit")
	s.r.git("cherry-pick", s.c2)
	res := s.check(t, s.m2, s.r.head())
	if res.PatchID.State != StateEqual {
		t.Fatalf("git patch-id --stable ignores whitespace, so the patch-id is equal: %+v", res.PatchID)
	}
	wantReasons(t, res, ReasonRangeDiff)
	if res.RangeDiff.NotEqual != 1 || res.RangeDiff.Deviations[0].Mark != "!" {
		t.Fatalf("%+v", res.RangeDiff)
	}
}

func TestAddedEmptyCommitRefuses(t *testing.T) {
	s := newStack(t)
	s.rebased("new")
	s.r.git("commit", "-q", "--allow-empty", "-m", "empty")
	res := s.check(t, s.m2, s.r.head())
	wantReasons(t, res, ReasonRangeDiff)
	if res.PatchID.State != StateEqual || res.RangeDiff.NotEqual != 1 || res.RangeDiff.Deviations[0].Mark != ">" || res.RangeDiff.Deviations[0].Old != nil {
		t.Fatalf("%+v", res)
	}
}

func TestReorderedCommitsRefuseByPosition(t *testing.T) {
	s := newStack(t)
	s.r.git("checkout", "-q", "-b", "new", "main")
	s.r.git("cherry-pick", s.c2)
	s.r.git("cherry-pick", s.c1)
	res := s.check(t, s.m2, s.r.head())
	if res.PatchID.State != StateEqual {
		t.Fatalf("the combined diff is the same: %+v", res.PatchID)
	}
	wantReasons(t, res, ReasonRangeDiff)
	if res.RangeDiff.NotEqual != 2 || res.RangeDiff.Deviations[0].Mark != "!" {
		t.Fatalf("both positions differ: %+v", res.RangeDiff)
	}
}

func TestRenameWithContentChangeRefuses(t *testing.T) {
	s := newStack(t)
	s.r.git("checkout", "-q", "-b", "new", "main")
	s.r.git("cherry-pick", s.c1)
	s.r.git("mv", "b.txt", "d.txt")
	s.r.write("d.txt", "x = 1\ny = 3\nz = 9\n")
	s.r.git("add", "-A")
	s.r.git("commit", "-q", "-m", "c2")
	res := s.check(t, s.m2, s.r.head())
	wantReasons(t, res, ReasonPatchIDDiffers, ReasonRangeDiff)
}

func TestRewrittenParentIsCarriedOverWithTheFourArgumentForm(t *testing.T) {
	r := newRepo(t)
	r.write("a.txt", aFile)
	r.write("m.txt", "m\n")
	r.git("add", "-A")
	r.git("commit", "-q", "-m", "m1")
	r.git("checkout", "-q", "-b", "parent")
	p1 := r.commit("p.txt", "parent\n", "p1")
	r.git("checkout", "-q", "-b", "child")
	r.commit("a.txt", strings.Replace(aFile, "two", "TWO", 1), "k1")
	k2 := r.commit("b.txt", "child\n", "k2")
	// The parent is rebase-merged: main moves on and gets a rewritten p1.
	r.git("checkout", "-q", "main")
	r.commit("m.txt", "m\nm2\n", "m2")
	r.git("cherry-pick", p1)
	p1New := r.head()
	r.git("checkout", "-q", "-b", "child-new", "child")
	r.git("rebase", "-q", "--onto", p1New, p1)
	res := Check(t.Context(), NewRunner(r.dir), Request{PR: 5, OldBase: p1, OldHead: k2, NewBase: p1New, NewHead: r.head()})
	wantReasons(t, res)
	if p1 == p1New {
		t.Fatal("the parent was not rewritten")
	}
}

func TestMissingObjectRefuses(t *testing.T) {
	s := newStack(t)
	res := s.check(t, s.m2, strings.Repeat("a", 40))
	wantReasons(t, res, ReasonObjectMissing)
	if res.PatchID.State != StateUnknown {
		t.Fatalf("%+v", res)
	}
}

func TestCheckWritesNothing(t *testing.T) {
	s := newStack(t)
	head := s.rebased("new")
	snapshot := func() string {
		return s.r.git("for-each-ref") + "|" + s.r.git("status", "--porcelain=v1", "--branch") + "|" + s.r.git("count-objects", "-v")
	}
	before := snapshot()
	wantReasons(t, s.check(t, s.m2, head))
	if after := snapshot(); after != before {
		t.Fatalf("repository changed:\n%s\n%s", before, after)
	}
}

// fakeGit scripts every answer of the Git interface; commits are keyed by the head.
type fakeGit struct {
	has                bool
	patch              map[string]string
	merges             []string
	commits            map[string][]CommitPatch
	errPatch, errMerge error
	errCommits         error
	pullTip            string
	errFetch           error
}

func (f *fakeGit) FetchPull(context.Context, string, int) (string, error) {
	return f.pullTip, f.errFetch
}
func (f *fakeGit) HasCommit(context.Context, string) (bool, error) { return f.has, nil }
func (f *fakeGit) PatchID(_ context.Context, _, head string) (string, error) {
	return f.patch[head], f.errPatch
}
func (f *fakeGit) Merges(context.Context, string, string) ([]string, error) {
	return f.merges, f.errMerge
}
func (f *fakeGit) Commits(_ context.Context, _, head string) ([]CommitPatch, error) {
	return f.commits[head], f.errCommits
}

var (
	sha = func(c string) string { return strings.Repeat(c, 40) }
	req = Request{PR: 3, OldBase: sha("1"), OldHead: sha("2"), NewBase: sha("3"), NewHead: sha("4")}
)

func cp(msg, body string) CommitPatch {
	return CommitPatch{Message: msg, Patch: "\ndiff --git a/f b/f\nindex 111..222 100644\n--- a/f\n+++ b/f\n@@ -1,2 +1,3 @@ func\n" + body}
}

func okFake() *fakeGit {
	c := []CommitPatch{cp("one\n", " ctx\n+x\n")}
	return &fakeGit{has: true, patch: map[string]string{sha("2"): sha("9"), sha("4"): sha("9")},
		commits: map[string][]CommitPatch{sha("2"): c, sha("4"): c}}
}

func TestReadFailuresNeverBecomeAYes(t *testing.T) {
	if res := Check(t.Context(), okFake(), req); !res.CarryOver {
		t.Fatalf("baseline must be a yes: %+v", res)
	}
	for name, mutate := range map[string]func(f *fakeGit){
		"patch-id": func(f *fakeGit) { f.errPatch = errors.New("boom") },
		"merges":   func(f *fakeGit) { f.errMerge = errors.New("boom") },
		"commits":  func(f *fakeGit) { f.errCommits = errors.New("boom") },
	} {
		f := okFake()
		mutate(f)
		res := Check(t.Context(), f, req)
		if res.CarryOver || res.Line != nil || len(res.Reasons) == 0 || res.Reasons[0] != ReasonReadFailed {
			t.Errorf("%s: %+v", name, res)
		}
	}
}

func TestEmptyPatchIDAndEmptyRangeRefuse(t *testing.T) {
	f := okFake()
	f.patch = map[string]string{}
	wantReasons(t, Check(t.Context(), f, req), ReasonPatchIDEmpty)
	f = okFake()
	f.commits = map[string][]CommitPatch{}
	wantReasons(t, Check(t.Context(), f, req), ReasonRangeEmpty)
}

func TestCommitComparisonIgnoresOnlyBlobIDsAndLineNumbers(t *testing.T) {
	f := okFake()
	shifted := cp("one\n", " ctx\n+x\n")
	shifted.Patch = strings.Replace(strings.Replace(shifted.Patch, "index 111..222", "index 333..444", 1), "@@ -1,2 +1,3 @@ func", "@@ -40,2 +41,3 @@ other", 1)
	f.commits[sha("4")] = []CommitPatch{shifted}
	if res := Check(t.Context(), f, req); !res.CarryOver || res.RangeDiff.Total != 1 {
		t.Fatalf("a shifted hunk and other blob ids stay equal: %+v", res)
	}
	for name, changed := range map[string]CommitPatch{
		"whitespace":      cp("one\n", " ctx\n+x \n"),
		"message":         cp("one changed\n", " ctx\n+x\n"),
		"changed context": cp("one\n", " other\n+x\n"),
	} {
		f := okFake()
		f.commits[sha("4")] = []CommitPatch{changed}
		res := Check(t.Context(), f, req)
		wantReasons(t, res, ReasonRangeDiff)
		if d := res.RangeDiff.Deviations; res.RangeDiff.NotEqual != 1 || d[0].Mark != "!" || *d[0].Old != 1 || *d[0].New != 1 {
			t.Errorf("%s: %+v", name, res.RangeDiff)
		}
	}
}

func TestCommitComparisonAddedRemovedAndReordered(t *testing.T) {
	a, b := cp("a\n", "+a\n"), cp("b\n", "+b\n")
	f := okFake()
	f.commits[sha("2")] = []CommitPatch{a}
	f.commits[sha("4")] = []CommitPatch{a, b}
	res := Check(t.Context(), f, req)
	if d := res.RangeDiff.Deviations; res.CarryOver || len(d) != 1 || d[0].Mark != ">" || d[0].Old != nil || *d[0].New != 2 || res.RangeDiff.Total != 2 {
		t.Fatalf("%+v", res.RangeDiff)
	}
	f.commits[sha("2")], f.commits[sha("4")] = []CommitPatch{a, b}, []CommitPatch{a}
	if d := Check(t.Context(), f, req).RangeDiff.Deviations; len(d) != 1 || d[0].Mark != "<" || d[0].New != nil {
		t.Fatalf("%+v", d)
	}
	f.commits[sha("2")], f.commits[sha("4")] = []CommitPatch{a, b}, []CommitPatch{b, a}
	if res := Check(t.Context(), f, req); res.CarryOver || res.RangeDiff.NotEqual != 2 {
		t.Fatalf("%+v", res.RangeDiff)
	}
}

func TestPullHeadComparison(t *testing.T) {
	f := okFake()
	r := req
	r.Remote = "origin"
	f.pullTip = r.NewHead
	if res := Check(t.Context(), f, r); !res.CarryOver || res.PRHead.State != StateMatch {
		t.Fatalf("%+v", res)
	}
	f.pullTip = sha("5")
	res := Check(t.Context(), f, r)
	wantReasons(t, res, ReasonHeadMismatch)
	if res.PRHead.State != StateMismatch {
		t.Fatalf("%+v", res.PRHead)
	}
	f.errFetch = errors.New("no remote")
	res = Check(t.Context(), f, r)
	if res.CarryOver || res.PRHead.State != StateUnknown {
		t.Fatalf("%+v", res)
	}
	if res := Check(t.Context(), okFake(), req); res.PRHead.State != StateNotChecked {
		t.Fatalf("%+v", res.PRHead)
	}
}
