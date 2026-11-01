// SPDX-License-Identifier: EUPL-1.2
package pulls

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/reviewstate"
)

const repoA = "o/a"

// reviewed adds a green, clean PR whose head commit is dated t0 and returns its head.
func reviewed(f *fakeForge, repo string, n int) string {
	p := okPR(repo, n)
	f.add(p)
	f.commits[p.head] = t0
	return p.head
}

func runReview(t *testing.T, f *fakeForge, o Options) *Result {
	t.Helper()
	o.Review = true
	if o.Repos == nil {
		o.Repos = []string{repoA}
	}
	return run(t, f, o)
}

func TestReviewStatesInRows(t *testing.T) {
	f := newForge()
	other := sha("9")
	h1, h2, h3, h4, h5, h6 := reviewed(f, repoA, 1), reviewed(f, repoA, 2), reviewed(f, repoA, 3), reviewed(f, repoA, 4), reviewed(f, repoA, 5), reviewed(f, repoA, 6)
	f.say(repoA, 1, 1, 5, reviewLine("CLEAR", h1, 1))
	f.say(repoA, 2, 1, 5, reviewLine("FINDINGS", h2, 2))
	f.say(repoA, 3, 1, -30, reviewLine("CLEAR", other, 3))
	f.say(repoA, 3, 2, 5, fmt.Sprintf("Carried-over: from=%s to=%s patch-id=%s pr=3", other, h3, strings.Repeat("1", 40)))
	f.say(repoA, 4, 1, 5, reviewLine("CLEAR", other, 4))
	f.say(repoA, 5, 1, 5, "Verdict: CLEAR")
	_ = h4
	_ = h5
	_ = h6
	res := runReview(t, f, Options{})
	want := map[int]string{1: reviewstate.StateClear, 2: reviewstate.StateFindings, 3: reviewstate.StateClearCarried, 4: reviewstate.StateClearStale, 5: reviewstate.StateUnparsed, 6: reviewstate.StateNone}
	for n, state := range want {
		r := row(t, res, repoA, n)
		if r.Review == nil || r.Review.State != state {
			t.Errorf("#%d: %+v want %s", n, r.Review, state)
		}
	}
	r := row(t, res, repoA, 1)
	if r.Review.Verdict != "CLEAR" || r.Review.Head != h1 || r.Review.URL == "" || !r.Review.At.Equal(t0.Add(5*time.Minute)) {
		t.Fatalf("evidence %+v", r.Review)
	}
	if got := strings.Join(r.Hints, ","); got != reviewstate.HintReadyMergeable {
		t.Fatalf("hints %q", got)
	}
	if got := strings.Join(row(t, res, repoA, 6).Hints, ","); got != reviewstate.HintNoFinalReview {
		t.Fatalf("hints of #6 %q", got)
	}
	s := res.Summary.Review
	if s.None != 1 || s.Findings != 1 || s.Clear != 1 || s.ClearCarried != 1 || s.ClearStale != 1 || s.Unparsed != 1 || s.NotRead != 0 {
		t.Fatalf("summary %+v", s)
	}
	if h := res.Summary.Hints; h.ReadyMergeable != 2 || h.NoFinalReview != 3 || h.ClearNotReady != 0 || h.BaseNotDefault != 0 {
		t.Fatalf("hint counts %+v", h)
	}
}

func TestReviewTrustRulesUseTheHeadCommitDate(t *testing.T) {
	f := newForge()
	h := reviewed(f, repoA, 1)
	f.commits[h] = t0.Add(10 * time.Minute) // the head commit is newer than the comment
	f.say(repoA, 1, 1, 5, reviewLine("CLEAR", h, 1))
	res := runReview(t, f, Options{})
	if got := row(t, res, repoA, 1).Review.State; got != reviewstate.StateNone {
		t.Fatalf("a comment before the head commit counted: %s", got)
	}
	if f.count("commit/") != 1 {
		t.Fatalf("calls %v", f.calls)
	}
}

func TestReviewCommitIsReadOnlyWhenACommentNamesTheHead(t *testing.T) {
	f := newForge()
	reviewed(f, repoA, 1)
	reviewed(f, repoA, 2)
	f.say(repoA, 1, 1, 5, reviewLine("CLEAR", sha("9"), 1)) // an older head
	f.say(repoA, 2, 1, 5, "Verdict: CLEAR")
	runReview(t, f, Options{})
	if n := f.count("commit/"); n != 0 {
		t.Fatalf("commit reads %d: %v", n, f.calls)
	}
}

func TestReviewDraftsAreReadWithoutDetail(t *testing.T) {
	f := newForge()
	p := okPR(repoA, 1)
	p.draft = true
	f.add(p)
	f.commits[p.head] = t0
	f.say(repoA, 1, 1, 5, reviewLine("CLEAR", p.head, 1))
	res := runReview(t, f, Options{})
	r := row(t, res, repoA, 1)
	if r.Detail || r.Group != GroupDraft || r.Review == nil || r.Review.State != reviewstate.StateClear {
		t.Fatalf("%+v", r)
	}
	if got := strings.Join(r.Hints, ","); got != reviewstate.HintClearNotReady {
		t.Fatalf("hints %q", got)
	}
	if f.count("detail/") != 0 {
		t.Fatal("a draft got a detail read")
	}
}

func TestReviewOffReadsNothingAndShowsNothing(t *testing.T) {
	f := newForge()
	reviewed(f, repoA, 1)
	res := run(t, f, Options{Repos: []string{repoA}})
	r := row(t, res, repoA, 1)
	if r.Review != nil || len(r.Hints) != 0 {
		t.Fatalf("%+v", r)
	}
	if res.Summary.Review != (ReviewCounts{}) || res.Summary.Hints != (HintCounts{}) {
		t.Fatalf("counts without reviews: %+v %+v", res.Summary.Review, res.Summary.Hints)
	}
	for _, c := range []string{"comments/", "commit/", "closed/", "repoinfo/"} {
		if f.count(c) != 0 {
			t.Fatalf("%s read: %v", c, f.calls)
		}
	}
}

func TestReviewReaderWithoutSupportLeavesRowsAlone(t *testing.T) {
	f := newForge()
	reviewed(f, repoA, 1)
	res, err := Run(context.Background(), plainReader{f}, Options{Repos: []string{repoA}, Review: true, Now: func() time.Time { return t0 }})
	if err != nil {
		t.Fatal(err)
	}
	r := row(t, res, repoA, 1)
	if r.Review != nil || len(r.Hints) != 0 || res.Summary.Review.NotRead != 1 {
		t.Fatalf("%+v %+v", r, res.Summary.Review)
	}
	if _, err := Run(context.Background(), plainReader{f}, Options{Repos: []string{repoA}, Review: true, ReviewStates: []string{"clear"}}); err == nil {
		t.Fatal("a review filter needs a reader that reads reviews")
	}
}

func TestReviewBudgetIsTheLimit(t *testing.T) {
	f := newForge()
	for n := 1; n <= 3; n++ {
		p := okPR(repoA, n)
		p.age = time.Duration(n) * time.Hour // #1 is the newest
		f.add(p)
		f.commits[p.head] = t0.Add(-100 * time.Hour)
		f.say(repoA, n, 1, 5, reviewLine("CLEAR", p.head, n))
	}
	res := runReview(t, f, Options{Limit: 2})
	if row(t, res, repoA, 1).Review == nil || row(t, res, repoA, 2).Review == nil || row(t, res, repoA, 3).Review != nil {
		t.Fatalf("the two newest rows are read: %+v", res.Rows)
	}
	if res.Summary.Review.NotRead != 1 || res.Summary.Review.Clear != 2 {
		t.Fatalf("%+v", res.Summary.Review)
	}
	if f.count("comments/") != 2 {
		t.Fatalf("calls %v", f.calls)
	}
}

func TestReviewBaseHints(t *testing.T) {
	f := newForge()
	child := okPR(repoA, 2)
	child.base = "codex/parent"
	f.add(child)
	f.commits[child.head] = t0
	f.say(repoA, 2, 1, 5, reviewLine("CLEAR", child.head, 2))
	unmerged := okPR(repoA, 3)
	unmerged.base = "codex/closed-unmerged"
	f.add(unmerged)
	fork := okPR(repoA, 4)
	fork.base = "codex/fork"
	f.add(fork)
	f.closed[repoA] = `[
	 {"number":10,"merged_at":"2026-10-30T10:00:00Z","head":{"ref":"codex/parent","repo":{"full_name":"o/a"}},"base":{"repo":{"full_name":"o/a"}}},
	 {"number":11,"merged_at":null,"head":{"ref":"codex/closed-unmerged","repo":{"full_name":"o/a"}},"base":{"repo":{"full_name":"o/a"}}},
	 {"number":12,"merged_at":"2026-10-30T10:00:00Z","head":{"ref":"codex/fork","repo":{"full_name":"x/a"}},"base":{"repo":{"full_name":"o/a"}}}]`
	res := runReview(t, f, Options{})
	hints := func(n int) string { return strings.Join(row(t, res, repoA, n).Hints, ",") }
	if got := hints(2); got != "base-not-default,parent-merged-retarget" {
		t.Fatalf("#2 %q", got)
	}
	if got := hints(3); !strings.Contains(got, "base-not-default") || strings.Contains(got, "parent-merged") {
		t.Fatalf("#3 %q", got)
	}
	if got := hints(4); !strings.Contains(got, "base-not-default") || strings.Contains(got, "parent-merged") {
		t.Fatalf("#4 %q", got)
	}
	if n := f.count("closed/"); n != 1 {
		t.Fatalf("one closed-list read per repository: %v", f.calls)
	}
	if n := f.count("repoinfo/"); n != 1 {
		t.Fatalf("one repository read: %v", f.calls)
	}
}

func TestReviewDefaultBranchOfTheRepositoryDecidesTheBase(t *testing.T) {
	f := newForge()
	reviewed(f, repoA, 1)
	f.defaultRef[repoA] = "develop"
	res := runReview(t, f, Options{})
	if got := strings.Join(row(t, res, repoA, 1).Hints, ","); !strings.Contains(got, "base-not-default") {
		t.Fatalf("%q", got)
	}
}

func TestReviewNoClosedListWhenEveryBaseIsDefault(t *testing.T) {
	f := newForge()
	reviewed(f, repoA, 1)
	runReview(t, f, Options{})
	if f.count("closed/") != 0 {
		t.Fatalf("calls %v", f.calls)
	}
}

func TestReviewFilters(t *testing.T) {
	f := newForge()
	h1, h2 := reviewed(f, repoA, 1), reviewed(f, repoA, 2)
	reviewed(f, repoA, 3)
	f.say(repoA, 1, 1, 5, reviewLine("CLEAR", h1, 1))
	f.say(repoA, 2, 1, 5, reviewLine("FINDINGS", h2, 2))
	nums := func(res *Result) string {
		var out []string
		for _, r := range res.Rows {
			out = append(out, fmt.Sprint(r.Number))
		}
		return strings.Join(out, ",")
	}
	if got := nums(runReview(t, f, Options{ReviewStates: []string{"clear"}})); got != "1" {
		t.Fatalf("clear: %s", got)
	}
	if got := nums(runReview(t, f, Options{ReviewStates: []string{"clear", "findings"}})); got != "1,2" {
		t.Fatalf("clear or findings: %s", got)
	}
	if got := nums(runReview(t, f, Options{Hints: []string{"no-final-review"}})); got != "3" {
		t.Fatalf("hint: %s", got)
	}
	if got := nums(runReview(t, f, Options{ReviewStates: []string{"clear", "none"}, Hints: []string{"no-final-review"}})); got != "3" {
		t.Fatalf("both flags are ANDed: %s", got)
	}
	res := runReview(t, f, Options{ReviewStates: []string{"clear"}})
	if res.Summary.Total != 1 || res.Summary.Review.Clear != 1 || res.Summary.Review.Findings != 0 || res.Repos[0].Listed != 1 {
		t.Fatalf("the summary counts the rows that remain: %+v %+v", res.Summary, res.Repos[0])
	}
	if _, err := Run(context.Background(), f, Options{Repos: []string{repoA}, ReviewStates: []string{"clear"}}); err == nil {
		t.Fatal("a review filter without Review is an error")
	}
	if _, err := Run(context.Background(), f, Options{Repos: []string{repoA}, Review: true, ReviewStates: []string{"clear-ish"}}); err == nil {
		t.Fatal("unknown state accepted")
	}
	if _, err := Run(context.Background(), f, Options{Repos: []string{repoA}, Review: true, Hints: []string{"nope"}}); err == nil {
		t.Fatal("unknown hint accepted")
	}
}

func TestReviewFilterDropsUnreadRows(t *testing.T) {
	f := newForge()
	for n := 1; n <= 2; n++ {
		p := okPR(repoA, n)
		p.age = time.Duration(n) * time.Hour
		f.add(p)
	}
	res := runReview(t, f, Options{Limit: 1, ReviewStates: []string{"none"}})
	if len(res.Rows) != 1 || res.Rows[0].Number != 1 {
		t.Fatalf("the unread row #2 must not match: %+v", res.Rows)
	}
}

func TestReviewReadFailureLeavesRowUnreadAndIsReported(t *testing.T) {
	f := newForge()
	reviewed(f, repoA, 1)
	reviewed(f, repoA, 2)
	f.errAt["comments/o/a#1"] = &hosting.WatchError{Code: hosting.CodeHTTP5xx, Message: "server error"}
	res := runReview(t, f, Options{})
	if row(t, res, repoA, 1).Review != nil || row(t, res, repoA, 2).Review == nil {
		t.Fatalf("%+v", res.Rows)
	}
	if len(res.Errors) != 1 || res.Errors[0].Code != hosting.CodeHTTP5xx || res.Repos[0].Error != hosting.CodeHTTP5xx {
		t.Fatalf("%+v", res.Errors)
	}
	if res.Summary.Review.NotRead != 1 {
		t.Fatalf("%+v", res.Summary.Review)
	}
}

func TestReviewRateLimitStopsFurtherReviewReads(t *testing.T) {
	f := newForge()
	for n := 1; n <= 3; n++ {
		reviewed(f, repoA, n)
	}
	f.errAt["comments/o/a#1"] = &hosting.WatchError{Code: hosting.CodeRateLimited, Message: "rate limit reached"}
	res := runReview(t, f, Options{Parallel: 1})
	if f.count("comments/") != 1 {
		t.Fatalf("reads after the rate limit: %v", f.calls)
	}
	if len(res.Errors) == 0 || res.Errors[0].Code != hosting.CodeRateLimited {
		t.Fatalf("%+v", res.Errors)
	}
}

func TestReviewCommitFailureIsReported(t *testing.T) {
	f := newForge()
	h := reviewed(f, repoA, 1)
	f.say(repoA, 1, 1, 5, reviewLine("CLEAR", h, 1))
	f.errAt["commit/"+h[:4]] = &hosting.WatchError{Code: hosting.CodeNetwork, Message: "x"}
	res := runReview(t, f, Options{})
	if row(t, res, repoA, 1).Review != nil || len(res.Errors) != 1 {
		t.Fatalf("%+v %+v", res.Rows, res.Errors)
	}
}

func TestReviewRepoInfoFailureOnlyDropsBaseHints(t *testing.T) {
	f := newForge()
	h := reviewed(f, repoA, 1)
	f.say(repoA, 1, 1, 5, reviewLine("CLEAR", h, 1))
	f.repoInfoErr[repoA] = &hosting.WatchError{Code: hosting.CodeHTTP5xx, Message: "x"}
	res := runReview(t, f, Options{})
	r := row(t, res, repoA, 1)
	if r.Review == nil || r.Review.State != reviewstate.StateClear || len(r.Hints) != 0 {
		t.Fatalf("%+v", r)
	}
	if len(res.Errors) != 1 {
		t.Fatalf("%+v", res.Errors)
	}
}

func TestReviewTruncatedAndEditedAreEvidence(t *testing.T) {
	f := newForge()
	h := reviewed(f, repoA, 1)
	f.say(repoA, 1, 1, 5, reviewLine("CLEAR", h, 1))
	f.comments[repoA+"#1"][0].UpdatedAt = t0.Add(time.Hour)
	f.commentsTr[repoA+"#1"] = true
	r := row(t, runReview(t, f, Options{}), repoA, 1)
	if !r.Review.Edited || !r.Review.Truncated {
		t.Fatalf("%+v", r.Review)
	}
}

func TestReviewMergeableHintNeedsGroupReady(t *testing.T) {
	f := newForge()
	p := okPR(repoA, 1)
	p.class = "red"
	f.add(p)
	f.commits[p.head] = t0
	f.say(repoA, 1, 1, 5, reviewLine("CLEAR", p.head, 1))
	r := row(t, runReview(t, f, Options{}), repoA, 1)
	if r.Group != GroupBlocked || len(r.Hints) != 0 || r.Review.State != reviewstate.StateClear {
		t.Fatalf("%+v", r)
	}
}

func TestReviewInvalidListedHeadIsNotRead(t *testing.T) {
	f := newForge()
	reviewed(f, repoA, 1)
	f.lists[repoA][0].HeadSHA = "abc"
	res := runReview(t, f, Options{})
	if row(t, res, repoA, 1).Review != nil || f.count("comments/") != 0 {
		t.Fatalf("%+v %v", res.Rows, f.calls)
	}
}

func TestReviewHintFilterSkipsRowsWithoutReview(t *testing.T) {
	f := newForge()
	for n := 1; n <= 3; n++ {
		p := okPR(repoA, n)
		if n != 3 {
			p.base = "feat" // base-not-default does not need the comments
		}
		f.add(p)
		f.commits[p.head] = t0
	}
	f.errAt["comments/o/a#1"] = &hosting.WatchError{Code: hosting.CodeHTTP5xx, Message: "server error"}
	f.errAt["comments/o/a#3"] = &hosting.WatchError{Code: hosting.CodeHTTP5xx, Message: "server error"}
	nums := func(res *Result) string {
		var out []string
		for _, r := range res.Rows {
			out = append(out, fmt.Sprint(r.Number))
		}
		return strings.Join(out, ",")
	}
	all := runReview(t, f, Options{})
	if got := strings.Join(row(t, all, repoA, 1).Hints, ","); row(t, all, repoA, 1).Review != nil || got != reviewstate.HintBaseNotDefault {
		t.Fatalf("setup: #1 is unread and has the hint base-not-default: %+v", row(t, all, repoA, 1))
	}
	if got := nums(runReview(t, f, Options{Hints: []string{"base-not-default"}})); got != "2" {
		t.Fatalf("an unread row matches no hint, even one it carries: %s", got)
	}
	if got := nums(runReview(t, f, Options{Hints: []string{"no-final-review"}})); got != "2" {
		t.Fatalf("an unread row without hints matches no hint: %s", got)
	}
}

func TestReviewIgnoresCommentsOfOtherAuthors(t *testing.T) {
	f := newForge()
	h1, h2, h3 := reviewed(f, repoA, 1), reviewed(f, repoA, 2), reviewed(f, repoA, 3)
	f.sayAs(repoA, 1, 1, 5, reviewLine("CLEAR", h1, 1), "NONE")
	f.sayAs(repoA, 2, 1, 5, reviewLine("CLEAR", h2, 2), "")
	f.sayAs(repoA, 3, 1, 5, reviewLine("CLEAR", h3, 3), "MEMBER")
	f.sayAs(repoA, 3, 2, 6, reviewLine("FINDINGS", h3, 3), "FIRST_TIME_CONTRIBUTOR")
	res := runReview(t, f, Options{})
	for _, n := range []int{1, 2} {
		r := row(t, res, repoA, n)
		if r.Review.State != reviewstate.StateNone || r.Review.Ignored != 1 || r.Review.Association != "" || r.Review.URL != "" {
			t.Errorf("#%d: %+v", n, r.Review)
		}
		if got := strings.Join(r.Hints, ","); got != reviewstate.HintNoFinalReview+","+reviewstate.HintForeignReviewIgnored {
			t.Errorf("#%d hints %q", n, got)
		}
	}
	r := row(t, res, repoA, 3)
	if r.Review.State != reviewstate.StateClear || r.Review.Association != "MEMBER" || r.Review.Ignored != 1 {
		t.Fatalf("#3 %+v", r.Review)
	}
	if got := strings.Join(r.Hints, ","); got != reviewstate.HintForeignReviewIgnored+","+reviewstate.HintReadyMergeable {
		t.Fatalf("#3 hints %q", got)
	}
	if h := res.Summary.Hints; h.ForeignReviewIgnored != 3 || h.ReadyMergeable != 1 || h.NoFinalReview != 2 {
		t.Fatalf("hint counts %+v", h)
	}
	if got := len(runReview(t, f, Options{Hints: []string{"foreign-review-ignored"}}).Rows); got != 3 {
		t.Fatalf("filter by foreign-review-ignored: %d rows", got)
	}
}

func TestReviewTruncatedThreadIsNotReadyMergeable(t *testing.T) {
	f := newForge()
	h1, h2 := reviewed(f, repoA, 1), reviewed(f, repoA, 2)
	f.say(repoA, 1, 1, 5, reviewLine("CLEAR", h1, 1))
	f.say(repoA, 2, 1, 5, reviewLine("CLEAR", h2, 2))
	f.commentsTr["o/a#1"] = true
	res := runReview(t, f, Options{})
	r := row(t, res, repoA, 1)
	if r.Review.State != reviewstate.StateClear || !r.Review.Truncated {
		t.Fatalf("%+v", r.Review)
	}
	if got := strings.Join(r.Hints, ","); got != reviewstate.HintTruncatedReviewUnknown {
		t.Fatalf("hints %q", got)
	}
	if got := strings.Join(row(t, res, repoA, 2).Hints, ","); got != reviewstate.HintReadyMergeable {
		t.Fatalf("#2 hints %q", got)
	}
	if h := res.Summary.Hints; h.TruncatedReviewUnknown != 1 || h.ReadyMergeable != 1 {
		t.Fatalf("hint counts %+v", h)
	}
}
