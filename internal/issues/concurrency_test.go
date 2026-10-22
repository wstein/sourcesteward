// SPDX-License-Identifier: EUPL-1.2
package issues

import (
	"fmt"
	"reflect"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
)

func TestRunReadsAtMostFourRepositoriesAtATime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newForge()
		var repos []string
		for i := 0; i < 9; i++ {
			r := fmt.Sprintf("o/r%d", i)
			repos = append(repos, r)
			f.pages[r] = []string{body(raw(1))}
		}
		var mu sync.Mutex
		inFlight, peak := 0, 0
		gate := make(chan struct{})
		f.before = func(string, int) {
			mu.Lock()
			inFlight++
			peak = max(peak, inFlight)
			mu.Unlock()
			<-gate
			mu.Lock()
			inFlight--
			mu.Unlock()
		}
		done := make(chan *Result)
		go func() { done <- run(t, f, Options{Repos: repos}) }()
		synctest.Wait()
		mu.Lock()
		if inFlight != 4 {
			t.Errorf("%d repositories in flight before any finished, want 4", inFlight)
		}
		mu.Unlock()
		close(gate)
		res := <-done
		if peak != 4 || len(res.Rows) != 9 {
			t.Errorf("peak %d rows %d", peak, len(res.Rows))
		}
	})
}

func TestRunReadsOneRepositorySequentially(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newForge()
		f.pages["o/r"] = []string{body(raw(1)), body(raw(2)), body(raw(3))}
		var mu sync.Mutex
		inFlight, peak := 0, 0
		f.before = func(string, int) {
			mu.Lock()
			inFlight++
			peak = max(peak, inFlight)
			mu.Unlock()
			time.Sleep(time.Second)
			mu.Lock()
			inFlight--
			mu.Unlock()
		}
		run(t, f, Options{Repos: []string{"o/r"}})
		if peak != 1 {
			t.Errorf("%d reads of one repository at the same time", peak)
		}
	})
}

func TestResultIsIndependentOfCompletionOrder(t *testing.T) {
	repos := []string{"a/x", "b/y", "c/z", "d/w"}
	build := func() *fakeForge {
		f := newForge()
		for i, r := range repos {
			f.pages[r] = []string{body(raw(i+1), raw(i+11, withLabels("bug")))}
		}
		return f
	}
	want := run(t, build(), Options{Repos: repos})
	synctest.Test(t, func(t *testing.T) {
		f := build()
		gates := map[string]chan struct{}{}
		for _, r := range repos {
			gates[r] = make(chan struct{})
		}
		f.before = func(repo string, _ int) { <-gates[repo] }
		done := make(chan *Result)
		go func() { done <- run(t, f, Options{Repos: repos}) }()
		for _, r := range slices.Backward(repos) { // finish in the reverse order
			synctest.Wait()
			close(gates[r])
		}
		if got := <-done; !reflect.DeepEqual(got, want) {
			t.Errorf("completion order changed the result:\n%+v\n%+v", got, want)
		}
	})
}

func TestRateLimitStopsTheRunWithoutSleepingOrRetrying(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		f := newForge()
		repos := []string{"a/1", "b/2", "c/3", "d/4", "e/5", "f/6"}
		for _, r := range repos {
			f.pages[r] = []string{body(raw(1)), body(raw(2))}
		}
		f.errAt["a/1#1"] = &hosting.WatchError{Code: hosting.CodeRateLimited, Status: 403, Message: "rate limit reached", RetryAfter: 24 * time.Hour}
		gate := make(chan struct{})
		f.before = func(repo string, _ int) {
			if repo != "a/1" {
				<-gate // b, c and d are in flight when a fails; e and f have not started
			}
		}
		done := make(chan *Result)
		go func() { done <- run(t, f, Options{Repos: repos}) }()
		synctest.Wait()
		close(gate)
		res := <-done
		if time.Since(start) != 0 {
			t.Errorf("the run waited %v", time.Since(start))
		}
		for _, r := range []string{"a/1", "e/5", "f/6"} {
			if st := repoStatus(t, res, r); st.State != StateError || st.Error != hosting.CodeRateLimited {
				t.Errorf("%s: %+v", r, st)
			}
		}
		if f.count("e/5") != 0 || f.count("f/6") != 0 || f.count("a/1") != 1 {
			t.Errorf("requests after the rate limit: %v", f.callList())
		}
		// b, c and d were already reading their first page; their next page is not requested.
		for _, r := range []string{"b/2", "c/3", "d/4"} {
			if f.count(r+"#2") != 0 {
				t.Errorf("%s read a second page after the rate limit", r)
			}
			if st := repoStatus(t, res, r); st.State != StateError || st.Error != hosting.CodeRateLimited {
				t.Errorf("%s: %+v", r, st)
			}
		}
		if len(res.Rows) != 0 || res.Summary.ReposFailed != 6 {
			t.Errorf("rows %d summary %+v", len(res.Rows), res.Summary)
		}
		if len(res.Errors) != 6 {
			t.Errorf("errors %+v", res.Errors)
		}
	})
}
