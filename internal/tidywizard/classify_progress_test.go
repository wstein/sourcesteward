// SPDX-License-Identifier: EUPL-1.2

package tidywizard

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"sync"
	"testing"

	"github.com/wstein/sourcesteward/internal/workspace"
)

// The classifier tells the progress sink which repository starts and how it ended, with its
// position in the sorted order; the source sees the same repository in its context.
func TestClassifyReportsEveryRepositoryStartAndEnd(t *testing.T) {
	var mu sync.Mutex
	var events []workspace.ObserveEvent
	ctx := workspace.WithObserveProgress(context.Background(), func(e workspace.ObserveEvent) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, e)
	})
	src := stubSource{
		"a": mixedRepo("a"),
		"b": func() (RepoFacts, error) { return RepoFacts{}, errors.New("boom") },
		"c": func() (RepoFacts, error) { return RepoFacts{Name: "c", Skip: "base-ambiguous"}, nil },
	}
	Classify(ctx, src, []string{"c", "a", "b"}, cfg())
	type end struct {
		Repo string
		Idx  int
		Note string
	}
	var starts []end
	var ends []end
	for _, e := range events {
		if e.Count != 3 {
			t.Fatalf("count: %+v", e)
		}
		switch e.Phase {
		case workspace.ObservePhaseStart:
			starts = append(starts, end{e.Repo, e.Index, ""})
		case workspace.ObservePhaseDone:
			ends = append(ends, end{e.Repo, e.Index, e.Note})
		}
	}
	sort.Slice(starts, func(i, j int) bool { return starts[i].Idx < starts[j].Idx })
	sort.Slice(ends, func(i, j int) bool { return ends[i].Idx < ends[j].Idx })
	if want := []end{{"a", 1, ""}, {"b", 2, ""}, {"c", 3, ""}}; !reflect.DeepEqual(starts, want) {
		t.Fatalf("starts %+v", starts)
	}
	if want := []end{{"a", 1, "analysed"}, {"b", 2, "error"}, {"c", 3, "skipped"}}; !reflect.DeepEqual(ends, want) {
		t.Fatalf("ends %+v", ends)
	}
}

// The repository of the source's context is the one it is asked about.
func TestClassifyGivesTheSourceTheRepositoryOfItsContext(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]bool{}
	src := ctxSource(func(ctx context.Context, name string) (RepoFacts, error) {
		workspace.ReportObserve(ctx, workspace.ObserveEvent{Phase: workspace.ObservePhaseWorktrees, Done: 1, Total: 2})
		return RepoFacts{Name: name, Skip: "base-ambiguous"}, nil
	})
	ctx := workspace.WithObserveProgress(context.Background(), func(e workspace.ObserveEvent) {
		if e.Phase == workspace.ObservePhaseWorktrees {
			mu.Lock()
			seen[e.Repo] = true
			mu.Unlock()
		}
	})
	Classify(ctx, src, []string{"x", "y"}, cfg())
	if !seen["x"] || !seen["y"] || len(seen) != 2 {
		t.Fatalf("seen %v", seen)
	}
}

type ctxSource func(context.Context, string) (RepoFacts, error)

func (s ctxSource) Facts(ctx context.Context, name string) (RepoFacts, error) { return s(ctx, name) }
