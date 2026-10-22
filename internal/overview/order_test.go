// SPDX-License-Identifier: EUPL-1.2

package overview

import (
	"math/rand"
	"reflect"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/pulls"
	"github.com/wstein/sourcesteward/internal/stwsync"
)

func TestNaturalCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v1.9.0", "v1.10.0", -1},
		{"v1.10.0", "v1.9.0", 1},
		{"v1.2.3", "v1.2.3", 0},
		{"v2", "v10", -1},
		{"a007", "a7", -1}, // equal value: bytewise, so the order is total
		{"bdock/v1.0.0", "claude/v0.1.0", -1},
		{"v1", "v1.0", -1},
		{"", "a", -1},
		{"v99999999999999999999999", "v100000000000000000000000", -1},
	}
	for _, c := range cases {
		if got := naturalCompare(c.a, c.b); got != c.want {
			t.Errorf("naturalCompare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func mk(c Category, repo string, s Subject) Item {
	p, _ := PriorityOf(c)
	return Item{ID: string(c) + ":" + repo + ":" + subjectID(s), Priority: p, Category: c, Repo: repo, Subject: s}
}

func TestSortOrderPriorityCategoryRepoSubject(t *testing.T) {
	want := []Item{
		mk(MainRed, "b/z", Subject{Kind: KindBranch, Name: "main"}),
		mk(ReleaseRunFailed, "A/a", Subject{Kind: KindTag, Tag: "v1.10.0"}),
		mk(ReleaseRunFailed, "A/a", Subject{Kind: KindTag, Tag: "v1.9.0"}), // tags by version, descending
		mk(ReadError, "a/a", Subject{Kind: KindSource, Name: "pulls"}),
		mk(ReadError, "a/b", Subject{Kind: KindSource, Name: "tags"}),
		mk(LocalChanges, "crewbook", Subject{Kind: KindFact, Name: "dirty"}),
		mk(LocalBehind, "crewbook", Subject{Kind: KindFact, Name: "behind"}),
		mk(PRConflict, "o/r", Subject{Kind: KindPR, Number: 9}), // numeric, not textual
		mk(PRConflict, "o/r", Subject{Kind: KindPR, Number: 10}),
		mk(PRReady, "o/r", Subject{Kind: KindPR, Number: 2}),
		mk(ReleaseDraft, "o/r", Subject{Kind: KindRelease, Tag: "v2.0.0"}),
		mk(PRChecksPending, "o/r", Subject{Kind: KindPR, Number: 1}),
		mk(WorktreeStale, "o/r", Subject{Kind: KindWorktree, Name: "/a"}),
		mk(WorktreeStale, "o/r", Subject{Kind: KindWorktree, Name: "/b"}),
		mk(PRBot, "o/r", Subject{Kind: KindPR, Number: 3}),
	}
	for seed := int64(0); seed < 50; seed++ {
		got := append([]Item(nil), want...)
		rand.New(rand.NewSource(seed)).Shuffle(len(got), func(i, j int) { got[i], got[j] = got[j], got[i] })
		got = sortItems(got)
		if !reflect.DeepEqual(ids(got), ids(want)) {
			t.Fatalf("seed %d:\n got %v\nwant %v", seed, ids(got), ids(want))
		}
	}
}

func TestSortDedupesTheSameID(t *testing.T) {
	a := mk(PRReady, "o/r", Subject{Kind: KindPR, Number: 1})
	b := a
	b.Title = "other"
	for _, in := range [][]Item{{a, b}, {b, a}} {
		got := sortItems(in)
		if len(got) != 1 || got[0].Title != "" {
			t.Fatalf("%+v", got)
		}
	}
}

func TestSortRepositoriesDifferingInCaseStayGrouped(t *testing.T) {
	a := mk(PRReady, "O/r", Subject{Kind: KindPR, Number: 5})
	b := mk(PRReady, "o/r", Subject{Kind: KindPR, Number: 1})
	c := mk(PRReady, "O/r", Subject{Kind: KindPR, Number: 1})
	got := sortItems([]Item{a, b, c})
	if !reflect.DeepEqual(ids(got), []string{"pr-ready:O/r:1", "pr-ready:O/r:5", "pr-ready:o/r:1"}) {
		t.Fatalf("%v", ids(got))
	}
}

// richInput has every kind of finding in several repositories.
func richInput() Input {
	failed := &Run{Conclusion: "failure", URL: "https://github.com/wstein/a/actions/runs/1"}
	yes := true
	repoA := Repo{Local: repoOK("a", func(r *stwsync.RepoStatus) {
		r.Changes.Staged, r.Behind, r.Stash = 1, ip(2), 2
		r.Reasons = []string{"staged", "behind"}
		r.Worktrees.List = []stwsync.WorktreeInfo{{Path: "/w1", Dirty: true}, {Path: "/w2", Prunable: true}}
	}), Slug: "wstein/a", Remote: &Remote{
		Sources:    map[Source]SourceState{SourceTags: {Outcome: OutcomeError, Code: "network"}},
		Pulls:      []PR{pr(9, pulls.GroupReady, "", nil), pr(10, pulls.GroupConflict, "", nil), pr(2, pulls.GroupReady, "", func(p *PR) { p.Bot = true }), pr(3, pulls.GroupDraft, "", func(p *PR) { p.Draft = true })},
		Visibility: WriteAccess,
		Drafts:     []Draft{{Tag: "v0.2.0", CreatedAt: time.Unix(100, 0)}, {Tag: "v0.10.0", CreatedAt: time.Unix(50, 0)}},
		Tags:       []ReleaseTag{{Tag: "v1.0.0", SHA: sha40, Unreleased: true, Run: failed}, {Tag: "v1.1.0", SHA: sha40, Unreleased: true}},
		Main:       &Main{Branch: "main", Class: "red", BehindOrigin: &yes},
	}}
	repoB := Repo{Local: repoOK("B", func(r *stwsync.RepoStatus) {
		r.State = "error"
		r.Reasons = []string{"error"}
	}), Slug: "wstein/B"}
	repoC := Repo{Local: repoOK("c", func(r *stwsync.RepoStatus) { r.Operation = sp("rebase"); r.Reasons = []string{"operation"} }), Slug: "wstein/c",
		Remote: &Remote{Sources: map[Source]SourceState{}, Pulls: []PR{pr(1, pulls.GroupChecks, "", nil), pr(4, pulls.GroupBlocked, pulls.ReasonNotRead, func(p *PR) { p.PastBudget = true })}}}
	return Input{Root: "/ws", Repos: []Repo{repoA, repoB, repoC}}
}

func TestItemsAreInvariantUnderShuffles(t *testing.T) {
	base := richInput()
	want := collect(base)
	if len(want) < 15 {
		t.Fatalf("fixture too small: %v", ids(want))
	}
	for seed := int64(0); seed < 100; seed++ {
		in := richInput()
		rng := rand.New(rand.NewSource(seed))
		rng.Shuffle(len(in.Repos), func(i, j int) { in.Repos[i], in.Repos[j] = in.Repos[j], in.Repos[i] })
		for i := range in.Repos {
			r := in.Repos[i].Remote
			if r == nil {
				continue
			}
			rng.Shuffle(len(r.Pulls), func(a, b int) { r.Pulls[a], r.Pulls[b] = r.Pulls[b], r.Pulls[a] })
			rng.Shuffle(len(r.Drafts), func(a, b int) { r.Drafts[a], r.Drafts[b] = r.Drafts[b], r.Drafts[a] })
			rng.Shuffle(len(r.Tags), func(a, b int) { r.Tags[a], r.Tags[b] = r.Tags[b], r.Tags[a] })
			w := in.Repos[i].Local.Worktrees.List
			rng.Shuffle(len(w), func(a, b int) { w[a], w[b] = w[b], w[a] })
		}
		if got := collect(in); !reflect.DeepEqual(got, want) {
			t.Fatalf("seed %d:\n got %v\nwant %v", seed, ids(got), ids(want))
		}
	}
}

func TestIDsDoNotDependOnTimeOrRunData(t *testing.T) {
	a, b := richInput(), richInput()
	for i := range b.Repos[0].Remote.Drafts {
		b.Repos[0].Remote.Drafts[i].CreatedAt = time.Unix(int64(9000-i), 0)
	}
	b.Repos[0].Remote.Tags[0].Run.Workflow = "other.yml"
	b.Repos[0].Remote.Tags[0].Run.Status = "completed"
	if !reflect.DeepEqual(ids(collect(a)), ids(collect(b))) {
		t.Fatal("ids or order changed with times or run data")
	}
	for _, it := range collect(a) {
		if want := string(it.Category) + ":" + it.Repo + ":" + subjectID(it.Subject); it.ID != want {
			t.Errorf("id %q, want %q", it.ID, want)
		}
	}
}

func TestEachRepositoryContributesItsOwnItems(t *testing.T) {
	got := ids(collect(richInput()))
	for _, id := range []string{"repo-error:B:-", "local-operation:c:operation", "main-red:wstein/a:main", "release-run-failed:wstein/a:v1.0.0", "tag-without-release:wstein/a:v1.1.0", "pr-unread:wstein/c:4"} {
		found := false
		for _, g := range got {
			found = found || g == id
		}
		if !found {
			t.Errorf("missing %s in %v", id, got)
		}
	}
}

func TestErrorRepositoryHasNoRemoteItems(t *testing.T) {
	r := richInput().Repos[0]
	r.Local.State = "error"
	r.Local.Reasons = []string{"error"}
	for _, it := range collect(Input{Repos: []Repo{r}}) {
		if it.Category != RepoError {
			t.Errorf("unexpected %s", it.ID)
		}
	}
}

func collect(in Input) []Item { return sortItems(allItems(in)) }

func TestSortRepositoryNamesCaseInsensitively(t *testing.T) {
	a := mk(PRReady, "a/zeta", Subject{Kind: KindPR, Number: 1})
	b := mk(PRReady, "B/alpha", Subject{Kind: KindPR, Number: 1})
	c := mk(PRReady, "c/x", Subject{Kind: KindPR, Number: 1})
	if got := ids(sortItems([]Item{c, b, a})); !reflect.DeepEqual(got, []string{a.ID, b.ID, c.ID}) {
		t.Fatalf("%v", got)
	}
}
