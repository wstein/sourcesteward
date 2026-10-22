// SPDX-License-Identifier: EUPL-1.2

package statusremote

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
)

func rateLimited(reset time.Time) error {
	return &hosting.WatchError{Code: hosting.CodeRateLimited, Message: "m", RateLimit: hosting.RateInfo{Known: !reset.IsZero(), Reset: reset}}
}

// everyRead calls each of the thirteen reads of Client once.
func everyRead(ctx context.Context, c Client) map[string]error {
	out := map[string]error{}
	_, out["list-open"] = c.ListOpenPulls(ctx, "o/r", hosting.PullListState{}, 1)
	_, out["detail"] = c.PullDetail(ctx, "o/r", 1, "")
	_, out["branch"] = c.Branch(ctx, "o/r", "main", "")
	_, out["commit"] = c.Commit(ctx, "o/r", sha("a"), "")
	_, out["commits"] = c.PullCommits(ctx, "o/r", 1, 1, "")
	_, out["check-runs"] = c.CheckRuns(ctx, "o/r", sha("a"), 1, "")
	_, out["statuses"] = c.Statuses(ctx, "o/r", sha("a"), 1, "")
	_, out["rate-limit"] = c.RateLimit(ctx)
	_, out["items"] = c.ListOpenPullItems(ctx, "o/r", 1)
	_, out["flow"] = c.WorkflowRuns(ctx, "o/r", "release.yml", sha("a"))
	_, out["repo"] = c.RepoInfo(ctx, "o/r")
	_, out["releases"] = c.ReleaseList(ctx, "o/r")
	_, out["tags"] = c.TagList(ctx, "o/r")
	return out
}

func TestGatePassesEveryReadThroughUntilThereIsEvidence(t *testing.T) {
	w := newWorld()
	for name, err := range everyRead(context.Background(), &gate{c: w}) {
		if err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if got := len(w.requests()); got != 13 {
		t.Fatalf("%d requests: %v", got, w.requests())
	}
}

func TestGateStopsEveryReadAfterTheFirstRateLimit(t *testing.T) {
	w := newWorld()
	w.errs["repo o/r"] = rateLimited(time.Time{})
	g := &gate{c: w}
	if _, err := g.RepoInfo(context.Background(), "o/r"); err == nil {
		t.Fatal("the evidence itself is the caller's failure")
	}
	sent := len(w.requests())
	for name, err := range everyRead(context.Background(), g) {
		var we *hosting.WatchError
		if !errors.As(err, &we) || we.Code != hosting.CodeRateLimited {
			t.Errorf("%s: %v", name, err)
		}
	}
	if got := len(w.requests()); got != sent {
		t.Fatalf("requests after the evidence: %v", w.requests()[sent:])
	}
}

func TestGateOnlyRateLimitEvidenceStops(t *testing.T) {
	w := newWorld()
	w.errs["repo o/r"] = &hosting.WatchError{Code: hosting.CodeHTTP5xx, Message: "m"}
	w.errs["tags o/r"] = errors.New("not even a forge error")
	g := &gate{c: w}
	_, _ = g.RepoInfo(context.Background(), "o/r")
	_, _ = g.TagList(context.Background(), "o/r")
	for name, err := range everyRead(context.Background(), g) {
		if name != "repo" && name != "tags" && err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestGateSendsNothingOnceTheContextIsDone(t *testing.T) {
	w := newWorld()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for name, err := range everyRead(ctx, &gate{c: w}) {
		if !errors.Is(err, context.Canceled) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(w.requests()) != 0 {
		t.Fatalf("%v", w.requests())
	}
}

// A throttle that arrives while the deadline passes is not evidence: the deadline wins, as in the pulls cycle.
func TestGateDeadlineWinsOverARateLimitThatArrivesWithIt(t *testing.T) {
	w := newWorld()
	ctx, cancel := context.WithCancel(context.Background())
	w.hook = func(context.Context, string) error { cancel(); return rateLimited(time.Time{}) }
	g := &gate{c: w}
	_, _ = g.RepoInfo(ctx, "o/r")
	w.hook = nil
	if _, err := g.RepoInfo(context.Background(), "o/r"); err != nil {
		t.Fatalf("the gate stopped on a throttle that came with the deadline: %v", err)
	}
}

// Two requests in flight can both end with evidence; the first one to be recorded is the one reported.
func TestGateKeepsTheResetOfTheFirstEvidence(t *testing.T) {
	first, second := t0.Add(time.Hour), t0.Add(2*time.Hour)
	g := &gate{c: newWorld()}
	g.observe(context.Background(), rateLimited(first))
	g.observe(context.Background(), rateLimited(second))
	if got := g.resetAt(); !got.Equal(first) {
		t.Fatalf("reset %v", got)
	}
}

func TestGateWithoutResetEvidenceReportsNone(t *testing.T) {
	w := newWorld()
	w.errs["repo a/a"] = rateLimited(time.Time{})
	g := &gate{c: w}
	_, _ = g.RepoInfo(context.Background(), "a/a")
	if got := g.resetAt(); !got.IsZero() {
		t.Fatalf("reset %v", got)
	}
}
