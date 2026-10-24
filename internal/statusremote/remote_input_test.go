// SPDX-License-Identifier: EUPL-1.2

package statusremote

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/overview"
	"github.com/wstein/sourcesteward/internal/stwsync"
)

func sp(s string) *string { return &s }
func ip(i int) *int       { return &i }

// dirty is the local status of a repository with staged changes: a yellow item of the local half.
func dirty(name string) stwsync.RepoStatus {
	return stwsync.RepoStatus{Name: name, Path: name, Kind: "root", State: "ok", Branch: sp("main"), Upstream: sp("origin/main"), Ahead: ip(0), Behind: ip(0),
		Changes: stwsync.Changes{Staged: 1}, Attention: true, Reasons: []string{"staged"}}
}

func TestInputPairsTheLocalAndTheRemoteHalfByName(t *testing.T) {
	w := newWorld()
	w.addRepo("o/a", false)
	res := Collect(context.Background(), []Repository{repository("o/a"), repository("o/b")}, (&opener{client: w}).options())
	in := res.Input("/ws", []stwsync.RepoStatus{dirty("b"), dirty("a"), dirty("lonely")})
	if in.Root != "/ws" || in.Deadline != DefaultTimeout || len(in.Repos) != 3 {
		t.Fatalf("%+v", in)
	}
	if in.Repos[0].Local.Name != "b" || in.Repos[0].Slug != "o/b" || in.Repos[0].Remote != res.Repos[1].Remote {
		t.Errorf("%+v", in.Repos[0])
	}
	if in.Repos[1].Local.Name != "a" || in.Repos[1].Slug != "o/a" || in.Repos[1].Remote != res.Repos[0].Remote {
		t.Errorf("%+v", in.Repos[1])
	}
	if in.Repos[2].Slug != "" || in.Repos[2].Remote != nil {
		t.Errorf("a local repository the remote half knows nothing of: %+v", in.Repos[2])
	}
}

func TestInputCarriesTheRateLimitReset(t *testing.T) {
	reset := t0.Add(time.Hour)
	w := newWorld()
	w.addRepo("o/a", false)
	w.errs["repo o/a"] = rateLimited(reset)
	res := Collect(context.Background(), []Repository{repository("o/a")}, (&opener{client: w}).options())
	if in := res.Input("/ws", []stwsync.RepoStatus{dirty("a")}); !in.RateLimitReset.Equal(reset) {
		t.Fatalf("%v", in.RateLimitReset)
	}
}

// exit is what the exit status of the command is made of, taken from the overview a run produces.
type exit struct{ failed, degraded bool }

func overviewOf(t *testing.T, res Result, names ...string) overview.Overview {
	t.Helper()
	var locals []stwsync.RepoStatus
	for _, n := range names {
		locals = append(locals, dirty(n))
	}
	ov := overview.Build(res.Input("/ws", locals))
	local := 0
	for _, it := range ov.Items {
		if it.Category == overview.LocalChanges {
			local++
		}
	}
	if local != len(names) {
		t.Errorf("the local block must not depend on the remote half: %d of %d local items", local, len(names))
	}
	return ov
}

func hasNotice(ov overview.Overview, code string) bool {
	for _, n := range ov.Notices {
		if n.Code == code {
			return true
		}
	}
	return false
}

// The exit rule of Spec 037 (US7) as far as the orchestration decides it: it depends on what was attempted,
// never on timing.
func TestTheExitRuleDependsOnWhatWasAttempted(t *testing.T) {
	two := []Repository{repository("o/a"), repository("o/b")}
	fresh := func() *world {
		w := newWorld()
		w.addRepo("o/a", true)
		w.addRepo("o/b", true)
		return w
	}
	for name, tc := range map[string]struct {
		setup  func(w *world) Options
		repos  []Repository
		want   exit
		notice string
	}{
		"everything attempted succeeded": {func(w *world) Options { return (&opener{client: w}).options() }, two, exit{}, ""},
		"the first request fails at the transport": {func(w *world) Options {
			w.hook = func(context.Context, string) error {
				return &hosting.WatchError{Code: hosting.CodeNetwork, Message: "m"}
			}
			return (&opener{client: w}).options()
		}, two, exit{true, true}, hosting.CodeNetwork},
		"one repository answers 500": {func(w *world) Options {
			w.errs["list o/b"] = &hosting.WatchError{Code: hosting.CodeHTTP5xx, Message: "m"}
			return (&opener{client: w}).options()
		}, two, exit{true, true}, hosting.CodeHTTP5xx},
		"a releases page over 1 MiB": {func(w *world) Options {
			w.errs["releases o/a"] = &hosting.WatchError{Code: hosting.CodePageTooLarge, Message: "m"}
			return (&opener{client: w}).options()
		}, two, exit{true, true}, hosting.CodePageTooLarge},
		"a 404 for the release workflow": {func(w *world) Options {
			w.errs["flow o/a release.yml@2222"] = &hosting.WatchError{Code: hosting.CodeForbidden, Status: 404, Message: "m"}
			return (&opener{client: w}).options()
		}, two, exit{false, false}, ""}, // not applicable: no degraded run, no notice
		"no usable token": {func(*world) Options { return (&opener{err: hosting.ErrAuthentication}).options() }, two, exit{false, true}, overview.ReasonNoToken},
		"offline":         {func(*world) Options { return Options{Offline: true} }, two, exit{false, true}, ""},
		"the limit is reached": {func(w *world) Options {
			w.errs["repo o/a"] = rateLimited(time.Time{})
			return (&opener{client: w}).options()
		}, two, exit{true, true}, overview.CodeRateLimited},
		"a client that cannot be opened": {func(*world) Options { return (&opener{err: errors.New("no gh")}).options() }, two, exit{true, true}, CodeHostingUnavailable},
		"nothing to read":                {func(*world) Options { return (&opener{}).options() }, []Repository{{Origin: Origin{Name: "a", Readable: true}}}, exit{false, true}, overview.ReasonNotGitHub},
	} {
		t.Run(name, func(t *testing.T) {
			w := fresh()
			res := Collect(context.Background(), tc.repos, tc.setup(w))
			names := []string{"a", "b"}[:len(tc.repos)]
			ov := overviewOf(t, res, names...)
			if got := (exit{ov.Summary.Failed, ov.Summary.Degraded}); got != tc.want {
				t.Errorf("failed/degraded %+v, want %+v", got, tc.want)
			}
			if tc.notice != "" && !hasNotice(ov, tc.notice) {
				t.Errorf("no notice %q: %+v", tc.notice, ov.Notices)
			}
		})
	}
}

func TestAMissingReleaseWorkflowRaisesNoNotice(t *testing.T) {
	w := newWorld()
	w.addRepo("o/a", true)
	w.errs["flow o/a release.yml@2222"] = &hosting.WatchError{Code: hosting.CodeForbidden, Status: 404, Message: "m"}
	res := Collect(context.Background(), []Repository{repository("o/a")}, (&opener{client: w}).options())
	ov := overviewOf(t, res, "a")
	if len(ov.Notices) != 0 || ov.Summary.Degraded || ov.Summary.Failed {
		t.Fatalf("%+v %+v", ov.Notices, ov.Summary)
	}
}

func TestADeadlineThatCancelsInFlightWorkIsNoFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := newWorld()
		w.addRepo("o/a", true)
		w.pulls.add(human("o/a", 1))
		w.hook = func(ctx context.Context, call string) error {
			if strings.HasPrefix(call, "tags ") {
				<-ctx.Done()
				return ctx.Err()
			}
			return nil
		}
		res := Collect(context.Background(), []Repository{repository("o/a")}, (&opener{client: w}).options())
		ov := overviewOf(t, res, "a")
		if ov.Summary.Failed || !ov.Summary.Degraded || !hasNotice(ov, overview.ReasonDeadline) {
			t.Fatalf("%+v %+v", ov.Summary, ov.Notices)
		}
		for _, n := range ov.Notices {
			if n.Code == overview.ReasonDeadline && !strings.Contains(n.Message, "90") {
				t.Errorf("the notice names the deadline: %q", n.Message)
			}
		}
	})
}

// Whatever the text of a failure holds, a token included, only its code reaches the result and the notices.
func TestTheTextOfAFailureNeverReachesTheOutput(t *testing.T) {
	w := newWorld()
	w.addRepo("o/a", true)
	w.pulls.add(human("o/a", 1))
	w.errs["repo o/a"] = &hosting.WatchError{Code: hosting.CodeUnauthorized, Message: "bad credentials " + canary}
	w.errs["detail o/a#1"] = fmt.Errorf("transport: %s", canary)
	w.errs["branch o/a main"] = errors.New(canary)
	res := Collect(context.Background(), []Repository{repository("o/a")}, (&opener{client: w}).options())
	ov := overviewOf(t, res, "a")
	if got := fmt.Sprintf("%+v %+v", res, ov); strings.Contains(got, canary) {
		t.Fatalf("the token reached the output: %s", got)
	}
	if !ov.Summary.Failed {
		t.Fatal("not a failure")
	}
}

// The result is a function of what the forge answered, not of how the goroutines interleaved.
func TestTheResultDoesNotDependOnTheOrderOfTheGoroutines(t *testing.T) {
	var repos []Repository
	var names []string
	newForge := func(yield int) *world {
		w := newWorld()
		for i := 0; i < 8; i++ {
			slug := fmt.Sprintf("o/r%d", i)
			w.addRepo(slug, i%2 == 0)
			w.pulls.add(human(slug, i+1))
		}
		w.errs["tags o/r3"] = &hosting.WatchError{Code: hosting.CodeHTTP5xx, Message: "m"}
		w.errs["list o/r5"] = &hosting.WatchError{Code: hosting.CodeNetwork, Message: "m"}
		w.flows[unreleasedTagRun("o/r0")] = ciRunsBody(ciRun(sha("2"), "completed", "failure", ciTime(9), 7))
		w.hook = func(_ context.Context, call string) error {
			for n := len(call) * yield % 7; n > 0; n-- {
				runtime.Gosched()
			}
			return nil
		}
		return w
	}
	for i := 0; i < 8; i++ {
		repos = append(repos, repository(fmt.Sprintf("o/r%d", i)))
		names = append(names, fmt.Sprintf("r%d", i))
	}
	var first overview.Overview
	var firstRes Result
	for yield := 0; yield < 12; yield++ {
		res := Collect(context.Background(), repos, (&opener{client: newForge(yield)}).options())
		ov := overviewOf(t, res, names...)
		if yield == 0 {
			first, firstRes = ov, res
			continue
		}
		if !reflect.DeepEqual(res, firstRes) || !reflect.DeepEqual(ov, first) {
			t.Fatalf("run %d differs from run 0", yield)
		}
	}
}
