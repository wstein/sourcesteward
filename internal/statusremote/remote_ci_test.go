// SPDX-License-Identifier: EUPL-1.2

package statusremote

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/overview"
	"github.com/wstein/sourcesteward/internal/stwsync"
)

// unreleasedTagRun is the key of the release workflow read for the unreleased tag v1.1.0 that addRepo adds.
func unreleasedTagRun(repo string) string { return repo + " release.yml@" + sha("2") }

func TestCIMainIsReadForTheDefaultBranchOfTheRepositoryRead(t *testing.T) {
	w := newWorld()
	w.addRepo("o/a", false)
	w.addRepo("o/b", false)
	w.pulls.runs[sha("a")] = ciCheckRun("completed", "failure")
	res := Collect(context.Background(), []Repository{repository("o/a")}, (&opener{client: w}).options())
	a := res.Repos[0]
	if got := sourceState(t, a, overview.SourceCIMain); got != okState {
		t.Fatalf("%+v", got)
	}
	if a.Remote.Main == nil || a.Remote.Main.Branch != "main" || a.Remote.Main.Head != sha("a") || a.Remote.Main.Class != "red" || a.Remote.Main.BehindOrigin != nil {
		t.Fatalf("%+v", a.Remote.Main)
	}
	if w.count("branch o/a main") != 1 {
		t.Fatalf("%v", w.requests())
	}
}

func TestCIMainKnowsWhetherTheLocalCloneLagsOrigin(t *testing.T) {
	dir := t.TempDir()
	ciGit(t, dir, "init", "-q", "-b", "main")
	have := ciCommit(t, dir, "one")
	for name, tc := range map[string]struct {
		head string
		want bool
	}{"present": {have, false}, "missing": {sha("e"), true}} {
		w := newWorld()
		w.addRepo("o/a", false)
		w.heads["o/a"] = tc.head
		r := repository("o/a")
		r.Dir = dir
		res := Collect(context.Background(), []Repository{r}, (&opener{client: w}).options())
		if m := res.Repos[0].Remote.Main; m == nil || m.BehindOrigin == nil || *m.BehindOrigin != tc.want {
			t.Errorf("%s: %+v", name, m)
		}
	}
}

// The runs of the release workflow are added to the tag entries of the tags source, never in their place:
// the original entries stay, the entries with a run follow, and the overview merges them per tag.
func TestCIReleaseTagsAreAppendedToTheTagsNeverReplaced(t *testing.T) {
	w := newWorld()
	w.addRepo("o/a", true)
	w.flows[unreleasedTagRun("o/a")] = ciRunsBody(ciRun(sha("2"), "completed", "failure", ciTime(9), 7), ciRun(sha("2"), "completed", "success", ciTime(8), 6))
	res := Collect(context.Background(), []Repository{repository("o/a")}, (&opener{client: w}).options())
	a := res.Repos[0]
	if got := sourceState(t, a, overview.SourceCIRelease); got != okState {
		t.Fatalf("%+v", got)
	}
	tags := a.Remote.Tags
	if len(tags) != 2 || tags[0].Run != nil || tags[1].Run == nil || tags[1].Run.Conclusion != "failure" || tags[0].Tag != "v1.1.0" || tags[1].Tag != "v1.1.0" {
		t.Fatalf("%+v", tags)
	}
	if a.CIRelease == nil || len(a.CIRelease.Tags) != 1 || len(a.CIRelease.Tags[0].Runs) != 2 || a.CIRelease.Tags[0].Workflow != "release.yml" {
		t.Fatalf("the runs and the workflow must stay available to the output: %+v", a.CIRelease)
	}
	items := overview.Build(overview.Input{Repos: []overview.Repo{{Slug: "o/a", Remote: a.Remote}}}).Items
	if len(items) != 1 || items[0].Category != overview.ReleaseRunFailed {
		t.Fatalf("one item per tag, the red one: %+v", items)
	}
}

func TestCIReleaseOfAWorkflowTheForgeDoesNotKnowIsASkip(t *testing.T) {
	w := newWorld()
	w.addRepo("o/a", true)
	w.errs["flow "+"o/a release.yml@2222"] = &hosting.WatchError{Code: hosting.CodeForbidden, Status: 404, Message: "m"}
	res := Collect(context.Background(), []Repository{repository("o/a")}, (&opener{client: w}).options())
	a := res.Repos[0]
	if got := sourceState(t, a, overview.SourceCIRelease); got != skipped(overview.ReasonNotFound) {
		t.Fatalf("%+v", got)
	}
	if a.CIRelease != nil || len(a.Remote.Tags) != 1 {
		t.Fatalf("%+v %+v", a.CIRelease, a.Remote.Tags)
	}
}

// ci-main needs the default branch of the repo source, ci-release the tags of the tags source: when the
// source they depend on failed they are not attempted and are skipped as not-attempted (the failure is
// reported once, by the source that failed), and when it was skipped they carry the same skip.
func TestCISourcesCarryTheStateOfTheSourceTheyDependOn(t *testing.T) {
	w := newWorld()
	w.addRepo("o/a", true)
	w.addRepo("o/b", true)
	w.errs["repo o/a"] = &hosting.WatchError{Code: hosting.CodeHTTP5xx, Message: "m"}
	w.errs["tags o/b"] = &hosting.WatchError{Code: hosting.CodeNetwork, Message: "m"}
	res := Collect(context.Background(), []Repository{repository("o/a"), repository("o/b")}, (&opener{client: w}).options())
	a, b := res.Repos[0], res.Repos[1]
	if got := sourceState(t, a, overview.SourceCIMain); got != skipped(overview.ReasonNotAttempted) || a.Remote.Main != nil {
		t.Errorf("ci-main of a: %+v %+v", got, a.Remote.Main)
	}
	if got := sourceState(t, a, overview.SourceCIRelease); got != okState {
		t.Errorf("ci-release of a: %+v", got)
	}
	if got := sourceState(t, b, overview.SourceCIRelease); got != skipped(overview.ReasonNotAttempted) || b.CIRelease != nil || b.Remote.Tags != nil {
		t.Errorf("ci-release of b: %+v %+v", got, b.CIRelease)
	}
	if got := sourceState(t, b, overview.SourceCIMain); got != okState {
		t.Errorf("ci-main of b: %+v", got)
	}
	if w.count("branch o/a") != 0 || w.count("flow o/b") != 0 {
		t.Fatalf("requests for data that was not there: %v", w.requests())
	}
	// one error notice per failed read, none repeated for the sources that were never attempted
	ov := overview.Build(res.Input("/ws", []stwsync.RepoStatus{dirty("a"), dirty("b")}))
	var codes []string
	for _, n := range ov.Notices {
		codes = append(codes, n.Code+"/"+n.Source)
	}
	sort.Strings(codes)
	if fmt.Sprint(codes) != "[http-5xx/repo network/tags]" {
		t.Errorf("notices %v", codes)
	}
}

func TestACISourceIsSkippedWithItsInputWhenTheInputWasSkipped(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := newWorld()
		w.addRepo("o/a", true)
		w.hook = func(ctx context.Context, call string) error {
			if call == "repo o/a" {
				<-ctx.Done()
				return ctx.Err()
			}
			return nil
		}
		res := Collect(context.Background(), []Repository{repository("o/a")}, (&opener{client: w}).options())
		if got := sourceState(t, res.Repos[0], overview.SourceCIMain); got != skipped(overview.ReasonDeadline) {
			t.Fatalf("%+v", got)
		}
	})
}

func TestAnUnusableContractIsAnErrorOfCIReleaseOnlyAndCostsNoRunRead(t *testing.T) {
	dir := t.TempDir()
	ciWriteContract(t, dir, "release.toml", "not toml [")
	w := newWorld()
	w.addRepo("o/a", true)
	r := repository("o/a")
	r.Dir = dir
	res := Collect(context.Background(), []Repository{r}, (&opener{client: w}).options())
	a := res.Repos[0]
	if got := sourceState(t, a, overview.SourceCIRelease); got != failed(CodeContractInvalid) {
		t.Fatalf("%+v", got)
	}
	for _, src := range []overview.Source{overview.SourceRepo, overview.SourceReleases, overview.SourceTags, overview.SourceCIMain} {
		if got := sourceState(t, a, src); got != okState {
			t.Errorf("%s: %+v", src, got)
		}
	}
	if w.count("flow") != 0 {
		t.Fatal("a run read although the contract is unusable")
	}
}

// The unusable contract is reported even when the tags could not be read: it is a fact of the clone.
func TestAnUnusableContractWinsOverAFailedTagsRead(t *testing.T) {
	dir := t.TempDir()
	ciWriteContract(t, dir, "release.toml", "not toml [")
	w := newWorld()
	w.addRepo("o/a", true)
	w.errs["tags o/a"] = &hosting.WatchError{Code: hosting.CodeNetwork, Message: "m"}
	r := repository("o/a")
	r.Dir = dir
	res := Collect(context.Background(), []Repository{r}, (&opener{client: w}).options())
	if got := sourceState(t, res.Repos[0], overview.SourceCIRelease); got != failed(CodeContractInvalid) {
		t.Fatalf("%+v", got)
	}
}

func TestTheDeadlineSkipsTheCISourcesThatWereNotFinished(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := newWorld()
		w.addRepo("o/a", true)
		w.hook = func(ctx context.Context, call string) error {
			if strings.HasPrefix(call, "flow ") {
				<-ctx.Done()
				return ctx.Err()
			}
			return nil
		}
		res := Collect(context.Background(), []Repository{repository("o/a")}, (&opener{client: w}).options())
		a := res.Repos[0]
		if sourceState(t, a, overview.SourceCIMain) != okState || sourceState(t, a, overview.SourceTags) != okState {
			t.Errorf("finished sources are kept: %+v", a.Remote.Sources)
		}
		if got := sourceState(t, a, overview.SourceCIRelease); got != skipped(overview.ReasonDeadline) || a.CIRelease != nil {
			t.Errorf("%+v %+v", got, a.CIRelease)
		}
	})
}

// All sources but the pull requests and the issues share one pool: at most four repositories are in flight for them
// together, however many sources a repository still has to read.
func TestAtMostFourRepositoriesAreInFlightForAllSourcesButPullRequestsAndIssues(t *testing.T) {
	for _, prefix := range []string{"repo ", "branch ", "flow "} {
		t.Run(strings.TrimSpace(prefix), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				w := newWorld()
				var repos []Repository
				for i := 0; i < 10; i++ {
					slug := fmt.Sprintf("o/r%d", i)
					w.addRepo(slug, true)
					repos = append(repos, repository(slug))
				}
				var inFlight, peak atomic.Int32
				release := make(chan struct{})
				w.hook = func(_ context.Context, call string) error {
					if strings.HasPrefix(call, "list ") || strings.HasPrefix(call, "detail ") || strings.HasPrefix(call, "issues ") || call == "user" {
						return nil // the pull requests and the issues have a pool of their own
					}
					n := inFlight.Add(1)
					for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
					}
					if strings.HasPrefix(call, prefix) {
						<-release
					}
					inFlight.Add(-1)
					return nil
				}
				done := make(chan Result)
				go func() { done <- Collect(context.Background(), repos, (&opener{client: w}).options()) }()
				synctest.Wait()
				if got := peak.Load(); got != 4 {
					t.Fatalf("%d repositories in flight, want exactly 4", got)
				}
				close(release)
				res := <-done
				if got := peak.Load(); got != 4 {
					t.Fatalf("peak %d after the run, want 4", got)
				}
				for _, r := range res.Repos {
					if got := sourceState(t, r, overview.SourceCIRelease); got != okState {
						t.Errorf("%s: %+v", r.Name, got)
					}
				}
			})
		})
	}
}

// The first rate-limit evidence of any source stops every further request of every source. Requests
// already in flight finish; nothing starts after the evidence, and the run never sleeps: the clock of the
// bubble is fake, so a sleep or a wait for the reset would show as elapsed time.
func TestTheFirstRateLimitStopsEverySourceWithoutSleeping(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reset := time.Now().Add(37 * time.Minute)
		w := newWorld()
		var repos []Repository
		for _, slug := range []string{"o/a", "o/b", "o/c"} {
			w.addRepo(slug, true)
			w.pulls.add(human(slug, 1))
			repos = append(repos, repository(slug))
		}
		w.errs["repo o/a"] = rateLimited(reset)
		evidence, proceed := make(chan struct{}), make(chan struct{})
		w.hook = func(_ context.Context, call string) error {
			if call == "repo o/a" {
				<-evidence
			} else {
				<-proceed // every other request is in flight when the evidence arrives
			}
			return nil
		}
		start := time.Now()
		done := make(chan Result)
		go func() { done <- Collect(context.Background(), repos, (&opener{client: w}).options()) }()
		synctest.Wait()
		close(evidence)
		synctest.Wait()
		sent := w.requests()
		close(proceed)
		res := <-done
		if got := w.requests(); !reflect.DeepEqual(got, sent) {
			t.Fatalf("requests after the evidence: %v", got[len(sent):])
		}
		for _, prefix := range []string{"detail ", "branch ", "flow ", "releases ", "tags "} {
			if w.count(prefix) != 0 {
				t.Errorf("%s requests were sent although the limit was reached: %v", prefix, w.requests())
			}
		}
		for _, r := range res.Repos {
			for _, src := range overview.Sources {
				if st := sourceState(t, r, src); st != okState && st != failed(hosting.CodeRateLimited) && st != skipped(overview.ReasonNotAttempted) {
					t.Errorf("%s %s: %+v", r.Name, src, st)
				}
			}
			if got := sourceState(t, r, overview.SourcePulls); got != failed(hosting.CodeRateLimited) {
				t.Errorf("%s pulls: %+v", r.Name, got)
			}
		}
		if got := sourceState(t, res.Repos[0], overview.SourceRepo); got != failed(hosting.CodeRateLimited) {
			t.Errorf("the evidence itself: %+v", got)
		}
		if !res.RateLimitReset.Equal(reset) {
			t.Errorf("reset %v, want %v", res.RateLimitReset, reset)
		}
		if elapsed := time.Since(start); elapsed != 0 {
			t.Errorf("the run waited %v", elapsed)
		}
	})
}
