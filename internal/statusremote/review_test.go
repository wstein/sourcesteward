// SPDX-License-Identifier: EUPL-1.2

package statusremote

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/overview"
	"github.com/wstein/sourcesteward/internal/pulls"
)

// reviewing is the forge plus the review reads of Spec 046, so that it is a pulls.ReviewSource.
type reviewing struct {
	*forge
	comments map[string][]hosting.IssueComment
}

func (r reviewing) ReadIssueComments(_ context.Context, repo string, n int) ([]hosting.IssueComment, bool, error) {
	if err := r.note(fmt.Sprintf("comments/%s#%d", repo, n)); err != nil {
		return nil, false, err
	}
	return r.comments[fmt.Sprintf("%s#%d", repo, n)], false, nil
}

func (r reviewing) ClosedPulls(context.Context, string) (hosting.WatchResponse, error) {
	return hosting.WatchResponse{Status: 200, Body: []byte(`[]`)}, nil
}

func (r reviewing) RepoInfo(context.Context, string) (hosting.RepoInfo, error) {
	return hosting.RepoInfo{DefaultBranch: "main", HTMLURL: "https://github.com/x"}, nil
}

func (r reviewing) Commit(context.Context, string, string, string) (hosting.WatchResponse, error) {
	return hosting.WatchResponse{Status: 200, Body: []byte(`{"commit":{"committer":{"date":"2026-10-01T00:00:00Z"}}}`)}, nil
}

func comment(repo string, n int, body string) (string, []hosting.IssueComment) {
	at := t0.Add(time.Hour)
	return fmt.Sprintf("%s#%d", repo, n), []hosting.IssueComment{{ID: 1, CreatedAt: at, UpdatedAt: at, Body: body, Association: "OWNER", URL: "https://github.com/x#c"}}
}

func TestReadPullsDoesNotCountCommentsOfOtherAuthors(t *testing.T) {
	f := newForge()
	a, b := human(repoA, 1), human(repoA, 2)
	f.add(a)
	f.add(b)
	r := reviewing{forge: f, comments: map[string][]hosting.IssueComment{}}
	for n, p := range map[int]string{1: a.head, 2: b.head} {
		k, cs := comment(repoA, n, fmt.Sprintf("Review: CLEAR head=%s patch-id=%s pr=%d", p, strings.Repeat("1", 40), n))
		if n == 1 {
			cs[0].Association = "CONTRIBUTOR"
		}
		r.comments[k] = cs
	}
	got := ReadPulls(context.Background(), r, targets(repoA))[repoA]
	if got.Counts.Review.Clear != 1 || got.Counts.Review.None != 1 || got.Counts.Hints.ForeignReviewIgnored != 1 || got.Counts.Hints.NoFinalReview != 1 || got.Counts.Hints.ReadyMergeable != 1 {
		t.Fatalf("a stranger's CLEAR must not count: %+v", got.Counts)
	}
}

func TestReadPullsCarriesTheReviewCountsOfTheCycle(t *testing.T) {
	f := newForge()
	a, b := human(repoA, 1), human(repoA, 2)
	f.add(a)
	f.add(b)
	r := reviewing{forge: f, comments: map[string][]hosting.IssueComment{}}
	k, cs := comment(repoA, 1, fmt.Sprintf("Review: CLEAR head=%s patch-id=%s pr=1", a.head, strings.Repeat("1", 40)))
	r.comments[k] = cs
	got := ReadPulls(context.Background(), r, targets(repoA))[repoA]
	if !got.ReviewRead || got.Counts.Review.Clear != 1 || got.Counts.Review.None != 1 || got.Counts.Hints.ReadyMergeable != 1 || got.Counts.Hints.NoFinalReview != 1 {
		t.Fatalf("%+v", got.Counts)
	}
	// the counts are those of the cycle for the same fixture
	res, err := pulls.Run(context.Background(), r, pulls.Options{Repos: []string{repoA}, Review: true})
	if err != nil || res.Summary.Review != got.Counts.Review || res.Summary.Hints != got.Counts.Hints {
		t.Fatalf("%v %+v", err, res.Summary)
	}
}

func TestReadPullsWithoutReviewSupportReadsNoReviews(t *testing.T) {
	f := newForge()
	f.add(human(repoA, 1))
	got := read(t, f, targets(repoA))[repoA]
	if got.ReviewRead || got.Counts.Review != (pulls.ReviewCounts{}) {
		t.Fatalf("%+v", got)
	}
}

func TestGateGivesThePullsCycleReviewReadsOnlyWhenTheClientHasThem(t *testing.T) {
	plain := &gate{c: newWorld()}
	if _, ok := plain.pullsReader().(pulls.ReviewSource); ok {
		t.Fatal("a client without review reads must not get a review source")
	}
	g := &gate{c: reviewWorld{newWorld()}}
	rs, ok := g.pullsReader().(pulls.ReviewSource)
	if !ok {
		t.Fatal("review source missing")
	}
	if _, _, err := rs.ReadIssueComments(context.Background(), "o/r", 1); err != nil {
		t.Fatal(err)
	}
	// after the first rate limit no review read is sent either
	g.observe(context.Background(), rateLimited(time.Time{}))
	if _, _, err := rs.ReadIssueComments(context.Background(), "o/r", 1); err == nil {
		t.Fatal("a review read was sent after the rate limit")
	}
	if _, err := rs.ClosedPulls(context.Background(), "o/r"); err == nil {
		t.Fatal("the closed list was read after the rate limit")
	}
}

type reviewWorld struct{ *world }

func (w reviewWorld) ReadIssueComments(ctx context.Context, repo string, n int) ([]hosting.IssueComment, bool, error) {
	return nil, false, w.enter(ctx, fmt.Sprintf("comments %s#%d", repo, n))
}

func (w reviewWorld) ClosedPulls(ctx context.Context, repo string) (hosting.WatchResponse, error) {
	return hosting.WatchResponse{Status: 200, Body: []byte(`[]`)}, w.enter(ctx, "closed "+repo)
}

var _ = overview.GroupReady

func TestCollectReadsReviewsWhenTheClientCanAndNotOtherwise(t *testing.T) {
	for name, tc := range map[string]struct {
		client   func(w *world) Client
		reviewed bool
	}{
		"client with review reads": {func(w *world) Client { return reviewWorld{w} }, true},
		"client without":           {func(w *world) Client { return w }, false},
	} {
		w := newWorld()
		w.addRepo("o/a", false)
		w.pulls.add(human("o/a", 1))
		op := &opener{client: tc.client(w)}
		res := Collect(context.Background(), []Repository{repository("o/a")}, op.options())
		p := res.Repos[0].Pulls
		if p == nil || p.ReviewRead != tc.reviewed {
			t.Fatalf("%s: %+v", name, p)
		}
		if got := w.count("comments o/a#1") > 0; got != tc.reviewed {
			t.Errorf("%s: comment reads %v, requests %v", name, got, w.requests())
		}
		if tc.reviewed && p.Counts.Review.None != 1 {
			t.Errorf("%s: %+v", name, p.Counts.Review)
		}
	}
}
