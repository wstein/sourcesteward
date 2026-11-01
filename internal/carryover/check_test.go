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
	if res.RangeDiff.NotEqual != 2 || res.RangeDiff.Deviations[0].Mark != "=" {
		t.Fatalf("both rows show = at another position: %+v", res.RangeDiff)
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

// fakeGit scripts every answer of the Git interface.
type fakeGit struct {
	has                bool
	patch              map[string]string
	merges             []string
	count              int
	rangeOut           string
	errPatch, errMerge error
	errCount, errRange error
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
func (f *fakeGit) Count(context.Context, string, string) (int, error) { return f.count, f.errCount }
func (f *fakeGit) RangeDiff(context.Context, string, string, string, string) (string, error) {
	return f.rangeOut, f.errRange
}

var (
	sha = func(c string) string { return strings.Repeat(c, 40) }
	req = Request{PR: 3, OldBase: sha("1"), OldHead: sha("2"), NewBase: sha("3"), NewHead: sha("4")}
)

func okFake() *fakeGit {
	return &fakeGit{has: true, patch: map[string]string{sha("2"): sha("9"), sha("4"): sha("9")}, count: 1,
		rangeOut: "1:  abc1234 = 1:  def5678 subject\n    @@ file\n    +x\n"}
}

func TestReadFailuresNeverBecomeAYes(t *testing.T) {
	if res := Check(t.Context(), okFake(), req); !res.CarryOver {
		t.Fatalf("baseline must be a yes: %+v", res)
	}
	for name, mutate := range map[string]func(f *fakeGit){
		"patch-id":   func(f *fakeGit) { f.errPatch = errors.New("boom") },
		"merges":     func(f *fakeGit) { f.errMerge = errors.New("boom") },
		"count":      func(f *fakeGit) { f.errCount = errors.New("boom") },
		"range-diff": func(f *fakeGit) { f.errRange = errors.New("boom") },
		"no rows":    func(f *fakeGit) { f.rangeOut = "unexpected\n" },
		"row count":  func(f *fakeGit) { f.count = 2 },
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
	f.count = 0
	wantReasons(t, Check(t.Context(), f, req), ReasonRangeEmpty)
}

func TestRangeDiffRowParsing(t *testing.T) {
	f := okFake()
	f.count = 3
	f.rangeOut = "1:  aaaaaaa = 1:  bbbbbbb one\n" +
		"    @@ x\n     context\n" +
		"2:  ccccccc ! 2:  ddddddd two\n" +
		"-:  ------- > 3:  eeeeeee three\n"
	res := Check(t.Context(), f, req)
	if res.RangeDiff.Total != 3 || res.RangeDiff.NotEqual != 2 || res.CarryOver {
		t.Fatalf("%+v", res.RangeDiff)
	}
	d := res.RangeDiff.Deviations
	if len(d) != 2 || d[0].Mark != "!" || *d[0].Old != 2 || d[1].Mark != ">" || d[1].Old != nil || *d[1].New != 3 {
		t.Fatalf("%+v", d)
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
