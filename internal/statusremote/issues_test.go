// SPDX-License-Identifier: EUPL-1.2

package statusremote

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/issues"
	"github.com/wstein/sourcesteward/internal/overview"
)

func okViewer() overview.SourceState { return overview.SourceState{Outcome: overview.OutcomeOK} }

// The issues of a repository are read with the cycle of `stw issues`: the rows that make an item are kept, the
// counts cover every issue read, and the login of the token is asked once for the whole run.
func TestIssuesAreReadWithOneViewerReadPerRun(t *testing.T) {
	w := newWorld()
	w.issues["o/a"] = []string{issueList(issue(1, []string{"security"}, nil), issue(2, nil, []string{"WStein"}), issue(3, []string{"bug"}, nil), issue(4, nil, nil))}
	w.issues["o/b"] = []string{issueList()}
	res := Collect(context.Background(), []Repository{repository("o/a"), repository("o/b")}, (&opener{client: w}).options())
	a, b := res.Repos[0], res.Repos[1]
	if got := sourceState(t, a, overview.SourceIssues); got != okState {
		t.Fatalf("%+v", got)
	}
	if a.Issues == nil || a.Issues.Open != 4 || a.Issues.Counts != (issues.Counts{Red: 1, Yellow: 2, Blue: 1}) || a.Issues.Truncated {
		t.Fatalf("%+v", a.Issues)
	}
	var got []string
	for _, i := range a.Remote.Issues {
		got = append(got, fmt.Sprintf("%d %s", i.Number, i.Reason))
	}
	if want := "1 security-label,2 assigned-to-you,3 bug-label"; strings.Join(got, ",") != want {
		t.Errorf("rows %v, want %s", got, want)
	}
	if b.Issues == nil || b.Issues.Open != 0 || len(b.Remote.Issues) != 0 {
		t.Errorf("%+v", b.Issues)
	}
	if res.Viewer != okViewer() || w.count("user") != 1 {
		t.Errorf("viewer %+v after %d reads", res.Viewer, w.count("user"))
	}
}

// A failed read has no data and hurts only its repository; a repository without issues is a fact, not a failure.
func TestAFailedIssuesReadHasNoDataAndAnIssuesDisabledOneIsASkip(t *testing.T) {
	w := newWorld()
	w.issues["o/a"] = []string{issueList(issue(1, []string{"security"}, nil))}
	w.issues["o/b"] = []string{issueList(issue(1, []string{"security"}, nil))}
	w.errs["issues o/a#1"] = &hosting.WatchError{Code: hosting.CodeHTTP5xx, Message: "m"}
	w.errs["issues o/c#1"] = &hosting.WatchError{Code: hosting.CodeIssuesDisabled, Message: "m"}
	res := Collect(context.Background(), []Repository{repository("o/a"), repository("o/b"), repository("o/c")}, (&opener{client: w}).options())
	a, b, c := res.Repos[0], res.Repos[1], res.Repos[2]
	if got := sourceState(t, a, overview.SourceIssues); got != failed(hosting.CodeHTTP5xx) || a.Issues != nil || a.Remote.Issues != nil {
		t.Errorf("%+v %+v", got, a.Issues)
	}
	if got := sourceState(t, b, overview.SourceIssues); got != okState || len(b.Remote.Issues) != 1 {
		t.Errorf("%+v", got)
	}
	if got := sourceState(t, c, overview.SourceIssues); got != skipped(overview.ReasonIssuesDisabled) || c.Issues != nil {
		t.Errorf("%+v", got)
	}
}

// More than ten pages are the explicit truncation of the cycle: the issues read are kept and the data says so.
func TestMoreThanTenPagesAreATruncationNotAFailure(t *testing.T) {
	w := newWorld()
	for i := 0; i < 11; i++ {
		w.issues["o/a"] = append(w.issues["o/a"], issueList(issue(i+1, nil, nil)))
	}
	res := Collect(context.Background(), []Repository{repository("o/a")}, (&opener{client: w}).options())
	a := res.Repos[0]
	if got := sourceState(t, a, overview.SourceIssues); got != okState || a.Issues == nil || !a.Issues.Truncated || a.Issues.Open != 10 {
		t.Errorf("%+v %+v", got, a.Issues)
	}
}

// The token class that cannot read `user` is a skip of the viewer only; a failed read is an error of the viewer
// only. In both cases the issues are read and the rule "assigned to you" is off.
func TestTheViewerOutcomesDoNotStopTheIssues(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want overview.SourceState
	}{
		"no identity": {&hosting.WatchError{Code: hosting.CodeForbidden, Message: "m"}, skipped(overview.ReasonNoIdentity)},
		"failure":     {&hosting.WatchError{Code: hosting.CodeHTTP5xx, Message: "m"}, failed(hosting.CodeHTTP5xx)},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld()
			w.issues["o/a"] = []string{issueList(issue(1, []string{"security"}, nil), issue(2, nil, []string{"wstein"}))}
			w.viewerErr = tc.err
			res := Collect(context.Background(), []Repository{repository("o/a")}, (&opener{client: w}).options())
			if res.Viewer != tc.want {
				t.Errorf("viewer %+v, want %+v", res.Viewer, tc.want)
			}
			if got := sourceState(t, res.Repos[0], overview.SourceIssues); got != okState || len(res.Repos[0].Remote.Issues) != 1 {
				t.Errorf("%+v %+v", got, res.Repos[0].Remote.Issues)
			}
		})
	}
}

// Without a repository on GitHub there is nothing to compare an assignee with: no `user` request is made and the
// viewer is skipped as `not-github-origin` (Spec 038), also when the workspace has no repository at all.
func TestNoRepositoryOnGitHubMeansNoViewerRead(t *testing.T) {
	elsewhere := Repository{Origin: Origin{Name: "x", Remote: "https://example.org/o/x.git", Readable: true}}
	for name, repos := range map[string][]Repository{"another host": {elsewhere}, "no repository": nil} {
		w := newWorld()
		res := Collect(context.Background(), repos, (&opener{client: w}).options())
		if res.Viewer != skipped(overview.ReasonNotGitHub) || w.count("user") != 0 {
			t.Errorf("%s: viewer %+v", name, res.Viewer)
		}
	}
	w := newWorld()
	res := Collect(context.Background(), []Repository{elsewhere}, (&opener{client: w}).options())
	if got := sourceState(t, res.Repos[0], overview.SourceIssues); got != skipped(overview.ReasonOtherHost) {
		t.Errorf("%+v", got)
	}
}

// Where no request is made the viewer carries the reason of the sources; a reader that could not be opened
// attempted nothing, so the viewer was not attempted either.
func TestTheViewerIsSkippedWhereNoRequestIsMade(t *testing.T) {
	repos := []Repository{repository("o/a")}
	if res := Collect(context.Background(), repos, Options{Offline: true}); res.Viewer != skipped(overview.ReasonOffline) {
		t.Errorf("offline: %+v", res.Viewer)
	}
	if res := Collect(context.Background(), repos, (&opener{err: hosting.ErrAuthentication}).options()); res.Viewer != skipped(overview.ReasonNoToken) {
		t.Errorf("no token: %+v", res.Viewer)
	}
	if res := Collect(context.Background(), repos, (&opener{err: fmt.Errorf("gh is missing")}).options()); res.Viewer != skipped(overview.ReasonNotAttempted) {
		t.Errorf("unavailable: %+v", res.Viewer)
	}
}

// The deadline cancels the issue reads in flight: the source and the viewer are skipped, not failed.
func TestTheDeadlineSkipsTheIssuesSource(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := newWorld()
		w.issues["o/a"] = []string{issueList(issue(1, []string{"security"}, nil))}
		w.hook = func(ctx context.Context, call string) error {
			if strings.HasPrefix(call, "issues ") {
				<-ctx.Done()
				return &hosting.WatchError{Code: hosting.CodeNetwork, Message: "canceled"}
			}
			return nil
		}
		opts := (&opener{client: w}).options()
		opts.Timeout = 7 * time.Second
		res := Collect(context.Background(), []Repository{repository("o/a")}, opts)
		if got := sourceState(t, res.Repos[0], overview.SourceIssues); got != skipped(overview.ReasonDeadline) {
			t.Errorf("%+v", got)
		}
		if res.Viewer != okViewer() {
			t.Errorf("viewer %+v", res.Viewer)
		}
	})
}

// A rate limit stops the issue reads without sleeping: the reads in flight when the evidence arrived finish, no read
// starts after it, and the repositories not read carry the rate limit. The evidence is held back until the other three
// reads of the pool of four are in flight, and they are held until the evidence has been recorded, so the count is exact
// and does not depend on the scheduler (the reads of the neighbours may legitimately complete after the evidence).
func TestARateLimitStopsTheIssuesSource(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := newWorld()
		w.errs["issues o/a#1"] = &hosting.WatchError{Code: hosting.CodeRateLimited, Message: "m"}
		release, four := make(chan struct{}), make(chan struct{})
		var started atomic.Int32
		w.hook = func(ctx context.Context, call string) error {
			if !strings.HasPrefix(call, "issues ") {
				return nil
			}
			if started.Add(1) == 4 {
				close(four)
			}
			if call == "issues o/a#1" {
				<-four // the evidence arrives while the other three reads are in flight
			} else {
				<-release // and they are in flight until it has been recorded
			}
			return nil
		}
		var repos []Repository
		for i := 0; i < 8; i++ {
			repos = append(repos, repository(fmt.Sprintf("o/%c", 'a'+i)))
		}
		done := make(chan Result)
		go func() { done <- Collect(context.Background(), repos, (&opener{client: w}).options()) }()
		synctest.Wait() // o/a has failed, the other three reads of the pool are in flight, the rest wait for a slot
		if got := started.Load(); got != 4 {
			t.Fatalf("%d reads in flight, want the pool of 4", got)
		}
		close(release)
		res := <-done
		if n := w.count("issues "); n != 4 {
			t.Errorf("%d issue reads, want the 4 that were in flight: none may start after the rate limit", n)
		}
		for i, r := range res.Repos {
			want := okState
			if i == 0 || i >= 4 {
				want = failed(hosting.CodeRateLimited)
			}
			if got := sourceState(t, r, overview.SourceIssues); got != want {
				t.Errorf("%s: %+v, want %+v", r.Name, got, want)
			}
		}
	})
}

// At most four repositories are in flight in the issues source.
func TestAtMostFourRepositoriesAreInFlightInTheIssuesSource(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := newWorld()
		var repos []Repository
		for i := 0; i < 10; i++ {
			repos = append(repos, repository(fmt.Sprintf("o/r%d", i)))
		}
		held, release := atomic.Int32{}, make(chan struct{})
		w.hook = func(_ context.Context, call string) error {
			if strings.HasPrefix(call, "issues ") {
				held.Add(1)
				<-release
			}
			return nil
		}
		done := make(chan Result)
		go func() { done <- Collect(context.Background(), repos, (&opener{client: w}).options()) }()
		synctest.Wait()
		if got := held.Load(); got != 4 {
			t.Fatalf("%d issue reads in flight, want exactly 4", got)
		}
		close(release)
		res := <-done
		for _, r := range res.Repos {
			if got := sourceState(t, r, overview.SourceIssues); got != okState {
				t.Errorf("%s: %+v", r.Name, got)
			}
		}
	})
}

// Result.Input hands the viewer to the action model.
func TestInputCarriesTheViewer(t *testing.T) {
	r := Result{Viewer: skipped(overview.ReasonNoIdentity)}
	if got := r.Input("/ws", nil).Viewer; got != r.Viewer {
		t.Errorf("%+v", got)
	}
}

// The pull request entries of the issue list are dropped and counted.
func TestPullRequestEntriesOfTheListAreCountedAsDropped(t *testing.T) {
	w := newWorld()
	pull := strings.Replace(issue(2, nil, nil), `"labels"`, `"pull_request":{"url":"x"},"labels"`, 1)
	w.issues["o/a"] = []string{issueList(issue(1, nil, nil), pull, strings.Replace(issue(3, nil, nil), `"labels"`, `"pull_request":{"url":"x"},"labels"`, 1))}
	res := Collect(context.Background(), []Repository{repository("o/a")}, (&opener{client: w}).options())
	if a := res.Repos[0].Issues; a == nil || a.Open != 1 || a.Dropped != 2 {
		t.Errorf("%+v", a)
	}
}

// The `user` read cut short by the deadline is a skip of the viewer, not a failure.
func TestTheDeadlineSkipsTheViewer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := newWorld()
		w.issues["o/a"] = []string{issueList(issue(1, nil, nil))}
		w.hook = func(ctx context.Context, call string) error {
			if call == "user" {
				<-ctx.Done()
				return &hosting.WatchError{Code: hosting.CodeNetwork, Message: "canceled"}
			}
			return nil
		}
		opts := (&opener{client: w}).options()
		opts.Timeout = 7 * time.Second
		res := Collect(context.Background(), []Repository{repository("o/a")}, opts)
		if res.Viewer != skipped(overview.ReasonDeadline) {
			t.Errorf("viewer %+v", res.Viewer)
		}
	})
}

// Only the code `network` is what a canceled request leaves; a failure the forge answered before the context ended
// stays a failure, for the viewer and for a repository.
func TestAFailureTheForgeAnsweredStaysAFailureWhenTheContextEndsAfterwards(t *testing.T) {
	for _, call := range []string{"user", "issues o/a#1"} {
		ctx, cancel := context.WithCancel(context.Background())
		w := newWorld()
		w.hook = func(_ context.Context, c string) error {
			if c == call {
				cancel()
				return &hosting.WatchError{Code: hosting.CodeHTTP5xx, Message: "m"}
			}
			return nil
		}
		res := Collect(ctx, []Repository{repository("o/a")}, (&opener{client: w}).options())
		want := failed(hosting.CodeHTTP5xx)
		if call == "user" && res.Viewer != want {
			t.Errorf("%s: viewer %+v", call, res.Viewer)
		}
		if got := sourceState(t, res.Repos[0], overview.SourceIssues); call != "user" && got != want {
			t.Errorf("%s: %+v", call, got)
		}
		cancel()
	}
}
