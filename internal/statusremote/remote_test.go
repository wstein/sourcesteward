// SPDX-License-Identifier: EUPL-1.2

package statusremote

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/overview"
)

// canary stands for a token: it must never reach a result, whatever fails.
const canary = "ghp_canary0123456789abcdef"

// repository is a readable workspace repository whose origin is a GitHub clone URL of slug.
func repository(slug string) Repository {
	return Repository{Origin: Origin{Name: slug[strings.Index(slug, "/")+1:], Remote: "https://github.com/" + slug + ".git", Readable: true}}
}

// opener is the Options.Open of a test: it counts the opens and the closes of the client it hands out.
type opener struct {
	client Client
	err    error
	opens  atomic.Int32
	closes atomic.Int32
}

func (o *opener) open(context.Context) (Client, func(), error) {
	o.opens.Add(1)
	if o.err != nil {
		return nil, nil, o.err
	}
	return o.client, func() { o.closes.Add(1) }, nil
}

func (o *opener) options() Options { return Options{Open: o.open} }

func sourceState(t *testing.T, r RepoResult, src overview.Source) overview.SourceState {
	t.Helper()
	if r.Remote == nil {
		t.Fatalf("%s has no remote", r.Name)
	}
	st, ok := r.Remote.Sources[src]
	if !ok {
		t.Fatalf("%s: source %s has no state", r.Name, src)
	}
	return st
}

func skipped(reason string) overview.SourceState {
	return overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: reason}
}

func failed(code string) overview.SourceState {
	return overview.SourceState{Outcome: overview.OutcomeError, Code: code}
}

// allSources checks the state of every source of one repository.
func allSources(t *testing.T, r RepoResult, want overview.SourceState) {
	t.Helper()
	for _, src := range overview.Sources {
		if got := sourceState(t, r, src); got != want {
			t.Errorf("%s %s: %+v, want %+v", r.Name, src, got, want)
		}
	}
}

func TestOfflineSendsNothingAndReadsNoCredential(t *testing.T) {
	w := newWorld()
	op := &opener{client: w}
	res := Collect(context.Background(), []Repository{repository("o/a"), repository("o/b")}, Options{Offline: true, Open: op.open})
	if op.opens.Load() != 0 || len(w.requests()) != 0 {
		t.Fatalf("opens %d, requests %v", op.opens.Load(), w.requests())
	}
	for _, r := range res.Repos {
		allSources(t, r, skipped(ReasonOffline))
	}
}

func TestARepositoryThatCannotBeReadKeepsItsOwnSkipReason(t *testing.T) {
	other := Repository{Origin: Origin{Name: "gl", Remote: "https://gitlab.com/o/gl.git", Readable: true}}
	nothing := Repository{Origin: Origin{Name: "bare", Readable: true}}
	unread := Repository{Origin: Origin{Name: "broken", Remote: "https://github.com/o/broken.git"}}
	for name, opts := range map[string]Options{"offline": {Offline: true}, "no token": {Open: (&opener{err: hosting.ErrAuthentication}).open}} {
		res := Collect(context.Background(), []Repository{other, nothing, unread, repository("o/a")}, opts)
		allSources(t, res.Repos[0], skipped(overview.ReasonOtherHost))
		allSources(t, res.Repos[1], skipped(overview.ReasonNotGitHub))
		allSources(t, res.Repos[2], skipped(overview.ReasonNotReadLocally))
		want := skipped(ReasonOffline)
		if name == "no token" {
			want = skipped(overview.ReasonNoToken)
		}
		allSources(t, res.Repos[3], want)
		if res.Repos[0].Slug != "" || res.Repos[3].Slug != "o/a" {
			t.Errorf("%s: slugs %q %q", name, res.Repos[0].Slug, res.Repos[3].Slug)
		}
	}
}

func TestNothingToReadMeansNoCredentialIsLookedUp(t *testing.T) {
	op := &opener{client: newWorld()}
	res := Collect(context.Background(), []Repository{{Origin: Origin{Name: "gl", Remote: "https://gitlab.com/o/gl.git", Readable: true}}}, op.options())
	if op.opens.Load() != 0 {
		t.Fatal("the credential route was used although no repository can be read")
	}
	allSources(t, res.Repos[0], skipped(overview.ReasonOtherHost))
}

func TestNoUsableTokenIsASkipAndSendsNoRequest(t *testing.T) {
	w := newWorld()
	op := &opener{client: w, err: fmt.Errorf("gh authentication unavailable for %s: %w", canary, hosting.ErrAuthentication)}
	res := Collect(context.Background(), []Repository{repository("o/a")}, op.options())
	allSources(t, res.Repos[0], skipped(overview.ReasonNoToken))
	if len(w.requests()) != 0 || res.Repos[0].Remote.Pulls != nil {
		t.Fatal("a request or data without a token")
	}
	if got := fmt.Sprintf("%+v", res); strings.Contains(got, canary) {
		t.Fatalf("the token reached the result: %s", got)
	}
}

// Any other reason the reader cannot be opened (no gh, an old gh) is a failed attempt at reading, with a
// code of its own, never a skip; the text of the error is not part of the result.
func TestAReaderThatCannotBeOpenedIsAFailureWithoutItsText(t *testing.T) {
	op := &opener{err: errors.New("cannot run " + canary)}
	res := Collect(context.Background(), []Repository{repository("o/a")}, op.options())
	allSources(t, res.Repos[0], failed(CodeHostingUnavailable))
	if got := fmt.Sprintf("%+v", res); strings.Contains(got, canary) {
		t.Fatalf("the error text reached the result: %s", got)
	}
}

func TestAReaderThatIsOpenedOnlyAfterTheDeadlineIsASkip(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	op := &opener{err: errors.New("canceled")}
	open := func(ctx context.Context) (Client, func(), error) { cancel(); return op.open(ctx) }
	res := Collect(ctx, []Repository{repository("o/a")}, Options{Open: open})
	allSources(t, res.Repos[0], skipped(overview.ReasonDeadline))
}

func TestPullsAreReadAndTheClientIsClosedOnce(t *testing.T) {
	w := newWorld()
	w.pulls.add(human("o/a", 1))
	w.pulls.add(pr{repo: "o/a", n: 2, user: "bot", utype: "Bot", age: time.Hour, mergeable: "true", state: "clean", class: "green", head: sha("b")})
	op := &opener{client: w}
	res := Collect(context.Background(), []Repository{repository("o/a"), repository("o/b")}, op.options())
	a, b := res.Repos[0], res.Repos[1]
	if got := sourceState(t, a, overview.SourcePulls); got != okState {
		t.Fatalf("%+v", got)
	}
	if len(a.Remote.Pulls) != 2 || a.Pulls == nil || a.Pulls.Open == nil || *a.Pulls.Open != 2 || len(a.Pulls.PRs) != 2 {
		t.Fatalf("%+v %+v", a.Remote.Pulls, a.Pulls)
	}
	if got := sourceState(t, b, overview.SourcePulls); got != okState || len(b.Remote.Pulls) != 0 || *b.Pulls.Open != 0 {
		t.Fatalf("%+v %+v", got, b.Pulls)
	}
	if op.opens.Load() != 1 || op.closes.Load() != 1 {
		t.Fatalf("opens %d, closes %d", op.opens.Load(), op.closes.Load())
	}
}

// A repository whose read failed has no data: the rows that were read before the failure are not
// reported (the action model gives such a repository one read-error), and its neighbour is untouched.
func TestAFailedPullsReadHasNoDataAndHurtsOnlyItsRepository(t *testing.T) {
	w := newWorld()
	w.pulls.add(human("o/a", 1))
	w.pulls.add(human("o/b", 2))
	w.errs["detail o/a#1"] = &hosting.WatchError{Code: hosting.CodeHTTP5xx, Message: "m"}
	res := Collect(context.Background(), []Repository{repository("o/a"), repository("o/b")}, (&opener{client: w}).options())
	a, b := res.Repos[0], res.Repos[1]
	if got := sourceState(t, a, overview.SourcePulls); got != failed(hosting.CodeHTTP5xx) {
		t.Fatalf("%+v", got)
	}
	if a.Remote.Pulls != nil || a.Pulls != nil {
		t.Fatalf("data of a failed source: %+v %+v", a.Remote.Pulls, a.Pulls)
	}
	if got := sourceState(t, b, overview.SourcePulls); got != okState || len(b.Remote.Pulls) != 1 {
		t.Fatalf("%+v", got)
	}
}

// The deadline cancels the request in flight; the source is skipped, not failed, and the run took the
// time of the deadline and not a nanosecond more. The bubble's clock is fake, so nothing really waits.
func TestTheDeadlineCancelsWhatIsInFlight(t *testing.T) {
	for name, tc := range map[string]struct {
		timeout time.Duration
		want    time.Duration
	}{"the default is 90 s": {0, DefaultTimeout}, "the option": {7 * time.Second, 7 * time.Second}} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				w := newWorld()
				w.pulls.add(human("o/a", 1))
				w.hook = func(ctx context.Context, call string) error {
					if strings.HasPrefix(call, "list ") {
						<-ctx.Done()
						return ctx.Err()
					}
					return nil
				}
				start := time.Now()
				opts := (&opener{client: w}).options()
				opts.Timeout = tc.timeout
				res := Collect(context.Background(), []Repository{repository("o/a")}, opts)
				if got := time.Since(start); got != tc.want {
					t.Errorf("the run took %v, want %v", got, tc.want)
				}
				if got := sourceState(t, res.Repos[0], overview.SourcePulls); got != skipped(overview.ReasonDeadline) {
					t.Errorf("%+v", got)
				}
				if res.Timeout != tc.want {
					t.Errorf("timeout %v", res.Timeout)
				}
			})
		})
	}
}

// At most four repositories are in flight in the pulls source, proved with a gate that holds every list read.
func TestAtMostFourRepositoriesAreInFlightInThePullsSource(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := newWorld()
		var repos []Repository
		for i := 0; i < 10; i++ {
			repos = append(repos, repository(fmt.Sprintf("o/r%d", i)))
		}
		held, release := atomic.Int32{}, make(chan struct{})
		w.hook = func(_ context.Context, call string) error {
			if strings.HasPrefix(call, "list ") {
				held.Add(1)
				<-release
			}
			return nil
		}
		done := make(chan Result)
		go func() { done <- Collect(context.Background(), repos, (&opener{client: w}).options()) }()
		synctest.Wait()
		if got := held.Load(); got != 4 {
			t.Fatalf("%d list reads in flight, want exactly 4", got)
		}
		close(release)
		res := <-done
		if got := w.count("list "); got != 10 {
			t.Fatalf("%d list reads", got)
		}
		for _, r := range res.Repos {
			if got := sourceState(t, r, overview.SourcePulls); got != okState {
				t.Errorf("%s: %+v", r.Name, got)
			}
		}
	})
}
