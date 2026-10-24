// SPDX-License-Identifier: EUPL-1.2

package tidywizard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
)

// gatedSource holds every Facts call open until released, counting the calls in flight.
type gatedSource struct {
	mu       sync.Mutex
	inFlight int
	peak     int
	started  []string
	gates    map[string]chan struct{}
	facts    func(name string) (RepoFacts, error)
}

func (g *gatedSource) Facts(ctx context.Context, name string) (RepoFacts, error) {
	g.mu.Lock()
	g.inFlight++
	g.peak = max(g.peak, g.inFlight)
	g.started = append(g.started, name)
	gate := g.gates[name]
	g.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
		}
	}
	g.mu.Lock()
	g.inFlight--
	g.mu.Unlock()
	return g.facts(name)
}

func repoNames(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("repo%02d", i)
	}
	return out
}

// Spec 041 T011a4: at most four repositories are analysed at the same time, and four are when
// there is enough work.
func TestClassifyAnalysesFourRepositoriesAtATime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		names := repoNames(9)
		g := &gatedSource{gates: map[string]chan struct{}{}, facts: func(n string) (RepoFacts, error) { return mixedRepo(n)() }}
		release := make(chan struct{})
		for _, n := range names {
			g.gates[n] = release
		}
		done := make(chan Result)
		go func() { done <- Classify(context.Background(), g, names, cfg()) }()
		synctest.Wait()
		g.mu.Lock()
		if g.inFlight != 4 {
			t.Errorf("%d repositories in flight before any finished, want 4", g.inFlight)
		}
		g.mu.Unlock()
		close(release)
		res := <-done
		if g.peak != 4 || len(res.Repositories) != 9 {
			t.Errorf("peak %d, repositories %d", g.peak, len(res.Repositories))
		}
	})
}

// The result does not depend on the completion order: repositories that finish in reverse order
// give exactly the result of the instant, in-order run.
func TestClassifyResultIsIndependentOfCompletionOrder(t *testing.T) {
	names := repoNames(8)
	facts := func(n string) (RepoFacts, error) {
		if n == "repo03" {
			return RepoFacts{}, errors.New("boom")
		}
		if n == "repo05" {
			return RepoFacts{Skip: "base-ambiguous"}, nil
		}
		return mixedRepo(n)()
	}
	want := Classify(context.Background(), &gatedSource{facts: facts}, names, cfg())
	synctest.Test(t, func(t *testing.T) {
		g := &gatedSource{gates: map[string]chan struct{}{}, facts: facts}
		for _, n := range names {
			g.gates[n] = make(chan struct{})
		}
		done := make(chan Result)
		go func() { done <- Classify(context.Background(), g, names, cfg()) }()
		// Release the started repositories highest name first, one at a time, until all are done.
		closed := map[string]bool{}
		for len(closed) < len(names) {
			synctest.Wait()
			g.mu.Lock()
			var next string
			for _, n := range g.started {
				if !closed[n] && n > next {
					next = n
				}
			}
			g.mu.Unlock()
			if next == "" {
				t.Fatal("nothing started")
			}
			closed[next] = true
			close(g.gates[next])
		}
		got := <-done
		if !reflect.DeepEqual(got, want) {
			t.Errorf("result depends on completion order:\n got %+v\nwant %+v", got.Repositories, want.Repositories)
		}
		if got.Repositories[3].State != "error" || got.Repositories[5].State != "skipped" {
			t.Errorf("per-repository failures are reported: %+v", got.Repositories)
		}
	})
}

// A cancelled context stops the analysis: repositories that were never started are reported as
// errors with the context error, never left empty, and the started ones keep their results.
func TestClassifyCancellationReportsTheRepositoriesNotStarted(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		names := repoNames(10)
		ctx, cancel := context.WithCancel(context.Background())
		g := &gatedSource{gates: map[string]chan struct{}{}, facts: func(n string) (RepoFacts, error) { return mixedRepo(n)() }}
		for _, n := range names {
			g.gates[n] = make(chan struct{})
		}
		done := make(chan Result)
		go func() { done <- Classify(ctx, g, names, cfg()) }()
		synctest.Wait()
		cancel()
		res := <-done
		if len(res.Repositories) != len(names) {
			t.Fatalf("%d repositories", len(res.Repositories))
		}
		for _, r := range res.Repositories {
			if r.Name == "" || r.State != "analysed" && (r.State != "error" || r.Reason == "") {
				t.Errorf("a repository is left unreported: %+v", r)
			}
		}
		if g.peak > 4 {
			t.Errorf("peak %d", g.peak)
		}
		analysed, errs := 0, 0
		for _, r := range res.Repositories {
			switch r.State {
			case "analysed":
				analysed++
			case "error":
				errs++
			}
		}
		for _, r := range res.Repositories {
			if r.State != "error" {
				continue
			}
			if r.Notices == nil || len(r.Notices) != 0 || r.Reason != context.Canceled.Error() {
				t.Errorf("an unstarted row carries an empty notices array and the context error: %+v", r)
			}
			b, err := json.Marshal(r)
			if err != nil || !strings.Contains(string(b), `"notices":[]`) || strings.Contains(string(b), `"notices":null`) {
				t.Errorf("json %s %v", b, err)
			}
		}
		for _, c := range res.Candidates {
			for _, r := range res.Repositories {
				if r.State == "error" && r.Name == c.Repo {
					t.Errorf("an error row has a candidate: %+v", c)
				}
			}
		}
		if analysed != 4 || errs != len(names)-4 {
			t.Errorf("the four started repositories keep their results and the other %d are reported: analysed %d, errors %d", len(names)-4, analysed, errs)
		}
	})
}

// A panic of the source follows the fanout rule: the others stop and it is re-raised unchanged.
func TestClassifyReRaisesAPanicOfTheSource(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		boom := errors.New("boom")
		var got any
		func() {
			defer func() { got = recover() }()
			g := &gatedSource{facts: func(n string) (RepoFacts, error) {
				if n == "repo01" {
					panic(boom)
				}
				return mixedRepo(n)()
			}}
			Classify(context.Background(), g, repoNames(6), cfg())
		}()
		if got != boom {
			t.Fatalf("re-raised %v", got)
		}
	})
}

// A started repository is recognised by its result, not by its name: one with an empty name keeps
// its result when the run is cancelled.
func TestClassifyCancellationKeepsAStartedRepositoryWithAnEmptyName(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		names := append([]string{""}, repoNames(9)...)
		ctx, cancel := context.WithCancel(context.Background())
		g := &gatedSource{gates: map[string]chan struct{}{}, facts: func(n string) (RepoFacts, error) { return mixedRepo(n)() }}
		for _, n := range names {
			g.gates[n] = make(chan struct{})
		}
		done := make(chan Result)
		go func() { done <- Classify(ctx, g, names, cfg()) }()
		synctest.Wait()
		cancel()
		res := <-done
		if r := res.Repositories[0]; r.Name != "" || r.State != "analysed" || r.Reason != "" {
			t.Errorf("the started empty-named repository was overwritten: %+v", r)
		}
	})
}
