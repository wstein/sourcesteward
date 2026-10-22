// SPDX-License-Identifier: EUPL-1.2

package overview

import (
	"reflect"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/stwsync"
)

func sp(s string) *string { return &s }
func ip(i int) *int       { return &i }

// repoOK is a clean repository in the shape stwsync reports it.
func repoOK(name string, mod func(*stwsync.RepoStatus)) stwsync.RepoStatus {
	rp := stwsync.RepoStatus{Name: name, Path: name, Kind: "root", State: "ok", Branch: sp("main"), Upstream: sp("origin/main"),
		Ahead: ip(0), Behind: ip(0), Head: sp("0123456789abcdef0123456789abcdef01234567"), Reasons: []string{}}
	if mod != nil {
		mod(&rp)
	}
	return rp
}

func local(rp stwsync.RepoStatus) []Item { return localItems("/ws", rp) }

func ids(items []Item) []string {
	out := []string{}
	for _, it := range items {
		out = append(out, it.ID)
	}
	return out
}

func TestLocalRules(t *testing.T) {
	cases := []struct {
		name     string
		mod      func(*stwsync.RepoStatus)
		id       string
		priority Priority
		cat      Category
		kind     SubjectKind
		command  string
		effect   Effect
	}{
		{"1 repo error", func(r *stwsync.RepoStatus) {
			r.State = "error"
			r.Reasons = []string{"error"}
			r.Error = &stwsync.Failure{Code: "git-failed", Message: "boom"}
		}, "repo-error:r:-", Red, RepoError, KindRepository, "stw status --offline r", Read},
		{"2 operation", func(r *stwsync.RepoStatus) { r.Operation = sp("rebase"); r.Reasons = []string{"operation"} },
			"local-operation:r:operation", Red, LocalOperation, KindFact, "git -C r status", Read},
		{"2 conflicts", func(r *stwsync.RepoStatus) { r.Changes.Conflicts = 2; r.Reasons = []string{"conflicts"} },
			"local-operation:r:conflicts", Red, LocalOperation, KindFact, "git -C r status", Read},
		{"6 staged", func(r *stwsync.RepoStatus) { r.Changes.Staged = 1; r.Reasons = []string{"staged"} },
			"local-changes:r:dirty", Yellow, LocalChanges, KindFact, "git -C r status", Read},
		{"6 unstaged", func(r *stwsync.RepoStatus) { r.Changes.Unstaged = 1; r.Reasons = []string{"unstaged"} },
			"local-changes:r:dirty", Yellow, LocalChanges, KindFact, "git -C r status", Read},
		{"7 detached", func(r *stwsync.RepoStatus) { r.Detached = true; r.Branch = nil; r.Reasons = []string{"detached"} },
			"local-detached:r:detached", Yellow, LocalDetached, KindFact, "git -C r status", Read},
		{"8 untracked", func(r *stwsync.RepoStatus) { r.Changes.Untracked = 3; r.Reasons = []string{"untracked"} },
			"local-untracked:r:untracked", Yellow, LocalUntracked, KindFact, "git -C r status", Read},
		{"9 behind", func(r *stwsync.RepoStatus) { r.Behind = ip(2); r.Reasons = []string{"behind"} },
			"local-behind:r:behind", Yellow, LocalBehind, KindFact, "stw sync --plan r", Read},
		{"9 diverged", func(r *stwsync.RepoStatus) {
			r.Behind = ip(2)
			r.Ahead = ip(1)
			r.Reasons = []string{"behind", "diverged"}
		},
			"local-behind:r:diverged", Yellow, LocalBehind, KindFact, "stw sync --plan r", Read},
		{"10 no upstream", func(r *stwsync.RepoStatus) { r.Upstream = nil; r.Reasons = []string{"no-upstream"} },
			"local-no-upstream:r:no-upstream", Yellow, LocalNoUpstream, KindFact, "git -C r branch -vv", Read},
		{"12 gitlink drift", func(r *stwsync.RepoStatus) {
			r.Kind = "submodule"
			r.Path = "libs/sub"
			r.Gitlink = &stwsync.Gitlink{State: "drift", Relation: sp("behind")}
			r.Reasons = []string{"gitlink-drift"}
		}, "gitlink-drift:r:libs/sub", Yellow, GitlinkDrift, KindGitlink, "stw status --offline libs/sub", Read},
		{"12 gitlink drift, relation unknown", func(r *stwsync.RepoStatus) {
			r.Path = "libs/sub"
			r.Gitlink = &stwsync.Gitlink{State: "drift"}
			r.Reasons = []string{"gitlink-drift"}
		}, "gitlink-drift:r:libs/sub", Yellow, GitlinkDrift, KindGitlink, "stw status --offline libs/sub", Read},
		{"13 worktree dirty", func(r *stwsync.RepoStatus) {
			r.Worktrees.List = []stwsync.WorktreeInfo{{Path: "/wt/a", Dirty: true}}
		}, "worktree-dirty:r:/wt/a", Yellow, WorktreeDirty, KindWorktree, "git -C /wt/a status", Read},
		{"21 stash", func(r *stwsync.RepoStatus) { r.Stash = 2 },
			"local-stash:r:stash", Blue, LocalStash, KindFact, "git -C r stash list", Read},
		{"22 gitlink ahead", func(r *stwsync.RepoStatus) {
			r.Path = "libs/sub"
			r.Gitlink = &stwsync.Gitlink{State: "drift", Relation: sp("ahead")}
		}, "gitlink-ahead:r:libs/sub", Blue, GitlinkAhead, KindGitlink, "git -C /ws add libs/sub", Owner},
		{"23 branches unmerged", func(r *stwsync.RepoStatus) { r.Branches.NotIntegrated = ip(3) },
			"branches-unmerged:r:branches", Blue, BranchesUnmerged, KindFact, "stw tidy plan r", Read},
		{"24 worktree prunable", func(r *stwsync.RepoStatus) {
			r.Worktrees.List = []stwsync.WorktreeInfo{{Path: "/wt/b", Prunable: true}}
		}, "worktree-stale:r:/wt/b", Blue, WorktreeStale, KindWorktree, "stw tidy plan r", Read},
		{"24 worktree missing", func(r *stwsync.RepoStatus) {
			r.Worktrees.List = []stwsync.WorktreeInfo{{Path: "/wt/b", Missing: true}}
		}, "worktree-stale:r:/wt/b", Blue, WorktreeStale, KindWorktree, "stw tidy plan r", Read},
		{"24 worktree locked", func(r *stwsync.RepoStatus) {
			r.Worktrees.List = []stwsync.WorktreeInfo{{Path: "/wt/b", Locked: sp("")}}
		}, "worktree-stale:r:/wt/b", Blue, WorktreeStale, KindWorktree, "stw tidy plan r", Read},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			items := local(repoOK("r", c.mod))
			if len(items) != 1 {
				t.Fatalf("items = %v", ids(items))
			}
			it := items[0]
			if it.ID != c.id || it.Priority != c.priority || it.Category != c.cat || it.Subject.Kind != c.kind {
				t.Errorf("item = %+v", it)
			}
			if it.Command != c.command || it.Effect != c.effect {
				t.Errorf("command %q (%q), want %q (%q)", it.Command, it.Effect, c.command, c.effect)
			}
			if it.Repo != "r" || it.Title == "" || it.URL != "" {
				t.Errorf("repo %q title %q url %q", it.Repo, it.Title, it.URL)
			}
			if strings.ContainsAny(it.Title, "\n\x1b") {
				t.Errorf("title not escaped: %q", it.Title)
			}
		})
	}
}

func TestLocalCleanRepositoryHasNoItems(t *testing.T) {
	if got := local(repoOK("r", nil)); len(got) != 0 {
		t.Fatalf("items = %v", ids(got))
	}
	// Information the spec keeps out of the table: commits ahead, a gone upstream, an uninitialized or in-sync gitlink.
	got := local(repoOK("r", func(r *stwsync.RepoStatus) {
		r.Ahead = ip(4)
		r.UpstreamGone = true
		r.Gitlink = &stwsync.Gitlink{State: "uninitialized"}
		r.Branches.NotIntegrated = ip(0)
		r.Worktrees.List = []stwsync.WorktreeInfo{{Path: "/wt/ok"}}
	}))
	if len(got) != 0 {
		t.Fatalf("items = %v", ids(got))
	}
}

// Every attention reason of Spec 033 maps to an item. The reasons come from stwsync, the source of truth: a new
// reason has no fixture here and fails this test until it is mapped.
func TestEveryAttentionReasonMapsToARow(t *testing.T) {
	type row struct {
		mod func(*stwsync.RepoStatus)
		cat Category
	}
	rows := map[string]row{
		"error":       {func(r *stwsync.RepoStatus) { r.State = "error" }, RepoError},
		"operation":   {func(r *stwsync.RepoStatus) { r.Operation = sp("merge") }, LocalOperation},
		"conflicts":   {func(r *stwsync.RepoStatus) { r.Changes.Conflicts = 1 }, LocalOperation},
		"staged":      {func(r *stwsync.RepoStatus) { r.Changes.Staged = 1 }, LocalChanges},
		"unstaged":    {func(r *stwsync.RepoStatus) { r.Changes.Unstaged = 1 }, LocalChanges},
		"untracked":   {func(r *stwsync.RepoStatus) { r.Changes.Untracked = 1 }, LocalUntracked},
		"behind":      {func(r *stwsync.RepoStatus) { r.Behind = ip(1) }, LocalBehind},
		"diverged":    {func(r *stwsync.RepoStatus) { r.Behind = ip(1); r.Ahead = ip(1) }, LocalBehind},
		"no-upstream": {func(r *stwsync.RepoStatus) { r.Upstream = nil }, LocalNoUpstream},
		"detached":    {func(r *stwsync.RepoStatus) { r.Detached = true; r.Branch = nil }, LocalDetached},
		"gitlink-drift": {func(r *stwsync.RepoStatus) {
			r.Gitlink = &stwsync.Gitlink{State: "drift", Relation: sp("diverged")}
		}, GitlinkDrift},
	}
	reasons := stwsync.AttentionReasons()
	for _, reason := range reasons {
		row, ok := rows[reason]
		if !ok {
			t.Errorf("attention reason %q of stwsync has no row in the action model: map it", reason)
			continue
		}
		rp := repoOK("r", row.mod)
		rp.Reasons = []string{reason}
		items := local(rp)
		if len(items) != 1 || items[0].Category != row.cat {
			t.Errorf("reason %s: %v", reason, ids(items))
		}
	}
	if len(rows) != len(reasons) {
		t.Errorf("%d fixtures for %d reasons: a reason was removed from stwsync", len(rows), len(reasons))
	}
	// The same repository with all its reasons at once loses none; staged+unstaged and behind+diverged
	// collapse per subject.
	rp := repoOK("r", func(r *stwsync.RepoStatus) {
		for reason, row := range rows {
			if reason != "error" {
				row.mod(r)
			}
		}
		r.Branch, r.Upstream = nil, nil
	})
	rp.Reasons = reasons[1:]
	if got := len(local(rp)); got != 8 {
		t.Errorf("all reasons: %d items, want 8", got)
	}
}

func TestLocalDistinctFactsGiveDistinctItems(t *testing.T) {
	got := local(repoOK("r", func(r *stwsync.RepoStatus) {
		r.Changes.Staged, r.Changes.Unstaged, r.Behind = 1, 1, ip(1)
		r.Reasons = []string{"staged", "unstaged", "behind"}
	}))
	if len(got) != 2 || got[0].ID != "local-changes:r:dirty" || got[1].ID != "local-behind:r:behind" {
		t.Fatalf("dirty and behind: %v", ids(got))
	}
}

func TestLocalPrecedenceOnlyWithinOneSubject(t *testing.T) {
	// behind and diverged are one fact kind: diverged wins, one item.
	got := local(repoOK("r", func(r *stwsync.RepoStatus) {
		r.Behind, r.Ahead, r.Reasons = ip(1), ip(1), []string{"behind", "diverged"}
	}))
	if len(got) != 1 || got[0].Subject.Name != "diverged" {
		t.Fatalf("%v", ids(got))
	}
	// one worktree that is dirty and prunable: the dirty row (13) wins; another worktree stays separate.
	got = local(repoOK("r", func(r *stwsync.RepoStatus) {
		r.Worktrees.List = []stwsync.WorktreeInfo{{Path: "/a", Dirty: true, Prunable: true}, {Path: "/b", Prunable: true}}
	}))
	if len(got) != 2 || got[0].Category != WorktreeDirty || got[1].ID != "worktree-stale:r:/b" {
		t.Fatalf("%v", ids(got))
	}
}

func TestLocalErrorStateIsTheOnlyItem(t *testing.T) {
	rp := repoOK("r", func(r *stwsync.RepoStatus) {
		r.State = "error"
		r.Reasons = []string{"error"}
		r.Changes.Staged = 5
		r.Stash = 1
		r.Error = &stwsync.Failure{Code: "x", Message: "a\nb\x1b[31m"}
	})
	got := local(rp)
	if len(got) != 1 || got[0].Category != RepoError || strings.ContainsAny(got[0].Title, "\n\x1b") {
		t.Fatalf("%+v", got)
	}
	rp.Error = nil
	if got := local(rp); len(got) != 1 || got[0].Category != RepoError {
		t.Fatalf("without failure: %+v", got)
	}
}

func TestLocalHostileNamesAreEscapedAndNeverInjectedIntoCommands(t *testing.T) {
	rp := repoOK("r", func(r *stwsync.RepoStatus) {
		r.Path = "-evil"
		r.Branch = sp("b\x1b[31m\nx")
		r.Upstream = nil
		r.Reasons = []string{"no-upstream"}
	})
	got := local(rp)
	if len(got) != 1 || got[0].Command != "" || got[0].Effect != "" {
		t.Fatalf("%+v", got)
	}
	if strings.ContainsAny(got[0].Title, "\n\x1b") {
		t.Errorf("title %q", got[0].Title)
	}
}

func TestLocalEvidence(t *testing.T) {
	got := local(repoOK("r", func(r *stwsync.RepoStatus) {
		r.Changes.Staged, r.Changes.Unstaged = 2, 3
		r.Reasons = []string{"staged", "unstaged"}
	}))
	ev := got[0].Evidence
	if ev["staged"] != 2 || ev["unstaged"] != 3 {
		t.Fatalf("%v", ev)
	}
}

func TestLocalIDsUseValidatedComponents(t *testing.T) {
	hostile := []string{"a:b", "x\ny", "x\x1b[31m", strings.Repeat("p/", 4096), "-rf", ""}
	seen := map[string]bool{}
	for _, h := range hostile {
		rp := repoOK("r"+h, func(r *stwsync.RepoStatus) {
			r.Path = "libs/" + h
			r.Gitlink = &stwsync.Gitlink{State: "drift", Relation: sp("behind")}
			r.Reasons = []string{"gitlink-drift"}
			r.Worktrees.List = []stwsync.WorktreeInfo{{Path: "/wt/" + h, Dirty: true}, {Path: "/wt2/" + h, Missing: true}}
		})
		items := local(rp)
		if len(items) != 3 {
			t.Fatalf("%q: %v", h, ids(items))
		}
		for _, it := range items {
			if strings.ContainsAny(it.ID, "\n\x1b") || strings.Count(it.ID, ":") != 2 && h != "" || len(it.ID) > 300 {
				t.Errorf("id %q for %q", it.ID, h)
			}
			if seen[it.ID] {
				t.Errorf("duplicate id %q", it.ID)
			}
			seen[it.ID] = true
		}
		again := local(rp)
		if !reflect.DeepEqual(ids(items), ids(again)) {
			t.Errorf("ids are not stable for %q", h)
		}
	}
	// Plain names stay readable.
	got := local(repoOK("crewbook", func(r *stwsync.RepoStatus) { r.Changes.Staged = 1; r.Reasons = []string{"staged"} }))
	if got[0].ID != "local-changes:crewbook:dirty" {
		t.Error(got[0].ID)
	}
}
