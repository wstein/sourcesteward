// SPDX-License-Identifier: EUPL-1.2
package restack

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// planKeeping plans and, before the quarantine is removed, hands the planner to look.
func planKeeping(t *testing.T, o Options, look func(ctx context.Context, p *planner)) Result {
	t.Helper()
	return planWith(context.Background(), o, time.Now, look)
}

func TestCleanReplayMatchesARealRebase(t *testing.T) {
	s := rebaseMerged(t)
	res := s.plan(t)
	if res.Status != StatusOK || res.Evidence == nil {
		t.Fatalf("%s %+v", res.Status, res.Diagnostics)
	}
	rp := res.Evidence.Replay
	if rp.State != "clean" || rp.CommitsReplayed != 2 || rp.FinalTree == nil || rp.TreeEqualsOneShot == nil || !*rp.TreeEqualsOneShot {
		t.Fatalf("replay %+v", rp)
	}
	copyDir := filepath.Join(filepath.Dir(s.dir), "copy")
	s.git("clone", "-q", "--no-local", s.dir, copyDir)
	if out, code := s.gitIn(copyDir, nil, "", "rebase", "--onto", s.newParent, s.oldParent, s.oldHead); code != 0 {
		t.Fatalf("real rebase: %s", out)
	}
	real, _ := s.gitIn(copyDir, nil, "", "rev-parse", "HEAD^{tree}")
	if *rp.FinalTree != real {
		t.Fatalf("predicted tree %s differs from the real rebase %s", *rp.FinalTree, real)
	}
}

func TestReplayCommitsCarryTheFixedIdentityNotTheRepositoryIdentity(t *testing.T) {
	s := rebaseMerged(t)
	s.git("config", "user.name", "Leaked Placeholder")
	s.git("config", "user.email", "t@t")
	var seen int
	res := planKeeping(t, s.options(), func(ctx context.Context, p *planner) {
		for _, oid := range p.replayed {
			raw, err := p.git.must(ctx, "cat-file", "commit", oid)
			if err != nil {
				t.Error(err)
				continue
			}
			seen++
			text := string(raw)
			if !strings.Contains(text, "\ncommitter stw-restack-plan <restack-plan@invalid> 0 +0000\n") {
				t.Errorf("committer is not the fixed identity:\n%s", text)
			}
			if strings.Contains(text, "Leaked Placeholder") || strings.Contains(text, "t@t") {
				t.Errorf("the repository identity leaked into the replay commit:\n%s", text)
			}
			if !strings.Contains(text, "\nauthor Fixture Owner <owner@example.org> ") {
				t.Errorf("the author is not preserved:\n%s", text)
			}
		}
	})
	if res.Status != StatusOK || seen != 2 {
		t.Fatalf("%s %+v seen %d", res.Status, res.Diagnostics, seen)
	}
}

func TestPredictedConflictStopsTheReplayAndNamesThePaths(t *testing.T) {
	s := rebaseMergedWith(t, func(r *repo) { r.commit("B1", map[string]string{"parent1.txt": "p1\n"}) },
		func(r *repo) {
			r.commit("C1", map[string]string{"shared.txt": "line1\nchild\nline3\n"})
			r.commit("C2", map[string]string{"child2.txt": "c2\n"})
			r.commit("C3", map[string]string{"child3.txt": "c3\n"})
		},
		func(r *repo) { r.commit("M", map[string]string{"shared.txt": "line1\nmain\nline3\n"}) })
	res := s.plan(t)
	if res.Status != StatusOK || res.Outcome != OutcomeConflicts {
		t.Fatalf("%s %s %+v", res.Status, res.Outcome, res.Diagnostics)
	}
	e := res.Evidence
	if e.Replay.State != "conflict" || e.Replay.CommitsReplayed != 0 || e.Replay.FinalTree != nil {
		t.Fatalf("replay %+v", e.Replay)
	}
	c := e.Conflicts
	if !c.Predicted || c.Commit == nil || *c.Commit != s.childCommits[0] || len(c.Items) != 1 || c.Items[0].Path != "shared.txt" || c.Items[0].Kind != "content" {
		t.Fatalf("conflicts %+v", c)
	}
	if c.ReplayStoppedAt == nil || *c.ReplayStoppedAt != s.childCommits[1] {
		t.Fatalf("replay_stopped_at %v", c.ReplayStoppedAt)
	}
}

func TestConflictKindsAreNamed(t *testing.T) {
	addAdd := rebaseMergedWith(t, func(r *repo) { r.commit("B1", map[string]string{"parent1.txt": "p1\n"}) },
		func(r *repo) { r.commit("C1", map[string]string{"both.txt": "child\n"}) },
		func(r *repo) { r.commit("M", map[string]string{"both.txt": "main\n"}) })
	if k := addAdd.plan(t).Evidence.Conflicts.Items; len(k) != 1 || k[0].Kind != "add/add" {
		t.Fatalf("add/add: %+v", k)
	}
	modDelete := rebaseMergedWith(t, func(r *repo) { r.commit("B1", map[string]string{"parent1.txt": "p1\n"}) },
		func(r *repo) { r.commit("C1", map[string]string{"shared.txt": "line1\nchanged\nline3\n"}) },
		func(r *repo) {
			r.git("rm", "-q", "shared.txt")
			r.git("commit", "-q", "-m", "delete shared")
		})
	if k := modDelete.plan(t).Evidence.Conflicts.Items; len(k) != 1 || k[0].Kind != "modify/delete" {
		t.Fatalf("modify/delete: %+v", k)
	}
}

func TestACommitWhoseChangeTheNewParentContainsIsListedAsEmpty(t *testing.T) {
	s := rebaseMergedWith(t, func(r *repo) { r.commit("B1", map[string]string{"parent1.txt": "p1\n"}) },
		func(r *repo) {
			r.commit("C1", map[string]string{"shared.txt": "line1\nsame\nline3\n"})
			r.commit("C2", map[string]string{"child2.txt": "c2\n"})
		},
		func(r *repo) { r.commit("M", map[string]string{"shared.txt": "line1\nsame\nline3\n"}) })
	res := s.plan(t)
	e := res.Evidence
	if res.Status != StatusOK || len(e.EmptyAfterReplay) != 1 || e.EmptyAfterReplay[0] != s.childCommits[0] {
		t.Fatalf("%s %+v %+v", res.Status, res.Diagnostics, e.EmptyAfterReplay)
	}
	if res.Outcome != OutcomeAttention {
		t.Fatalf("outcome %s", res.Outcome)
	}
}

func TestTheQuarantineIsRemovedOnEveryPath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	s := rebaseMerged(t)
	t.Setenv("TMPDIR", tmp) // the fixture uses its own t.TempDir; plans use TMPDIR
	left := func() []string {
		entries, _ := os.ReadDir(tmp)
		var names []string
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "stw-restack-") {
				names = append(names, e.Name())
			}
		}
		return names
	}
	s.plan(t) // clean
	o := s.options()
	o.Branch = "refs/heads/missing"
	Plan(context.Background(), o) // refusal
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	Plan(ctx, s.options()) // cancellation
	if names := left(); len(names) != 0 {
		t.Fatalf("temporary plan directories left behind: %v", names)
	}
}
