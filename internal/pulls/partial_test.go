// SPDX-License-Identifier: EUPL-1.2
package pulls

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
)

// cancelForge cancels the run's context when the n-th pulls/N read starts and fails that
// read the way a real reader does once its context is done.
type cancelForge struct {
	*fakeForge
	cancel context.CancelFunc
	at     int
	seen   int
}

func (c *cancelForge) PullDetail(ctx context.Context, repo string, n int, etag string) (hosting.WatchResponse, error) {
	c.fakeForge.mu.Lock()
	c.seen++
	hit := c.seen == c.at
	c.fakeForge.mu.Unlock()
	if hit {
		c.cancel()
		return hosting.WatchResponse{}, ctx.Err()
	}
	return c.fakeForge.PullDetail(ctx, repo, n, etag)
}

func TestExpiryReturnsTheRowsReadSoFarWithDeadlineMarks(t *testing.T) {
	f := newForge()
	threePRs(f)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	res, err := Run(ctx, &cancelForge{fakeForge: f, cancel: cancel, at: 2}, Options{Repos: []string{"o/r"}, Parallel: 1, Now: func() time.Time { return t0 }})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("the context error is still reported: %v", err)
	}
	if res == nil || len(res.Rows) != 3 || !res.Deadline {
		t.Fatalf("rows read so far and the mark must survive: %+v", res)
	}
	if r := row(t, res, "o/r", 1); !r.Detail || r.Deadline || r.Group != GroupReady {
		t.Fatalf("the finished read is kept: %+v", r)
	}
	for _, n := range []int{2, 3} {
		r := row(t, res, "o/r", n)
		if r.Detail || !r.Deadline || r.PastBudget || r.Group != GroupBlocked || r.Reason != ReasonNotRead {
			t.Fatalf("#%d not-read by deadline, inside the budget: %+v", n, r)
		}
	}
	if len(res.Errors) != 0 || res.Repos[0].Error != "" || !res.Repos[0].Deadline || res.Repos[0].Open == nil {
		t.Fatalf("a deadline is not a failure: %+v %+v", res.Errors, res.Repos)
	}
	if s := res.Summary; s.ReposFailed != 0 || s.NotRead != 0 || s.Total != 3 {
		t.Fatalf("%+v", s)
	}
}

func TestExpiryBeforeTheListIsASkippedRepositoryNotAFailure(t *testing.T) {
	f := newForge()
	threePRs(f)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := Run(ctx, f, Options{Repos: []string{"o/r", "p/q"}, Now: func() time.Time { return t0 }})
	if !errors.Is(err, context.Canceled) || res == nil {
		t.Fatalf("%v %v", res, err)
	}
	if len(res.Rows) != 0 || len(res.Errors) != 0 || f.count("") != 0 {
		t.Fatalf("nothing is read or reported as an error: %+v calls=%v", res, f.calls)
	}
	for _, st := range res.Repos {
		if !st.Deadline || st.Error != "" || st.Open != nil {
			t.Fatalf("%+v", st)
		}
	}
	if s := res.Summary; s.ReposFailed != 0 || s.ReposRead != 0 || s.ReposDeadline != 2 {
		t.Fatalf("%+v", s)
	}
}

func TestExpiryDuringTheReReadKeepsTheRowsAndIsNoFailure(t *testing.T) {
	f := newForge()
	f.lazy("o/r", 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// read 1 is the first pulls/N, read 2 the re-read.
	res, err := Run(ctx, &cancelForge{fakeForge: f, cancel: cancel, at: 2}, Options{Repos: []string{"o/r"}, Now: func() time.Time { return t0 }})
	if !errors.Is(err, context.Canceled) || res == nil {
		t.Fatalf("%v %v", res, err)
	}
	if r := row(t, res, "o/r", 1); r.Group != GroupBlocked || r.Reason != ReasonUnknown || !r.Detail {
		t.Fatalf("%+v", r)
	}
	if len(res.Errors) != 0 || res.Repos[0].Error != "" || !res.Repos[0].Deadline {
		t.Fatalf("%+v %+v", res.Errors, res.Repos)
	}
}

func TestDeadlineIsNeverSetOnACompleteRun(t *testing.T) {
	f := newForge()
	threePRs(f)
	res := run(t, f, Options{Repos: []string{"o/r"}, Limit: 1})
	if res.Deadline || res.Repos[0].Deadline {
		t.Fatalf("%+v", res.Repos)
	}
	for _, r := range res.Rows {
		if r.Deadline {
			t.Fatalf("%+v", r)
		}
	}
}

func TestExpiryKeepsPastBudgetRowsDistinctFromDeadlineRows(t *testing.T) {
	f := newForge()
	threePRs(f)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	res, _ := Run(ctx, &cancelForge{fakeForge: f, cancel: cancel, at: 1}, Options{Repos: []string{"o/r"}, Limit: 2, Parallel: 1, Now: func() time.Time { return t0 }})
	if r := row(t, res, "o/r", 2); !r.Deadline || r.PastBudget {
		t.Fatalf("inside the budget, cut by the deadline: %+v", r)
	}
	if r := row(t, res, "o/r", 3); r.Deadline || !r.PastBudget {
		t.Fatalf("past the budget stays past the budget: %+v", r)
	}
	if res.Summary.NotRead != 1 {
		t.Fatalf("%+v", res.Summary)
	}
}
