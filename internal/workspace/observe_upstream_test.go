// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"testing"
)

// upstreamFixture has a branch with a vanished remote-tracking branch (gone), one whose
// remote-tracking branch exists (present), one without an upstream (none), one that tracks a
// local branch that does not exist (local) and one whose configuration names a remote that has no
// reference at all (norefs is gone too: the remote-tracking branch does not exist).
func upstreamFixture(t *testing.T) string {
	t.Helper()
	repo := observeRepo(t)
	gitFixture(t, repo, "remote", "add", "origin", "https://github.com/owner/repo.git")
	for _, name := range []string{"gone", "present", "none", "local"} {
		gitFixture(t, repo, "branch", name)
	}
	for _, name := range []string{"gone", "present"} {
		gitFixture(t, repo, "update-ref", "refs/remotes/origin/"+name, "main")
		gitFixture(t, repo, "config", "branch."+name+".remote", "origin")
		gitFixture(t, repo, "config", "branch."+name+".merge", "refs/heads/"+name)
	}
	gitFixture(t, repo, "update-ref", "-d", "refs/remotes/origin/gone")
	gitFixture(t, repo, "config", "branch.local.remote", ".")
	gitFixture(t, repo, "config", "branch.local.merge", "refs/heads/missing")
	return repo
}

func branchByName(o WorktreeObservation) map[string]BranchEvidence {
	by := map[string]BranchEvidence{}
	for _, b := range o.Branches {
		by[b.Name] = b
	}
	return by
}

// Spec 044 T004: the upstream-gone fact comes from the local reference database only.
func TestObserveUpstreamGoneIsReadFromTheLocalReferences(t *testing.T) {
	repo := upstreamFixture(t)
	by := branchByName(observe(t, repo))
	want := map[string]UpstreamState{
		"refs/heads/gone": UpstreamGone, "refs/heads/present": UpstreamNotGone, "refs/heads/none": UpstreamNotGone,
		// A branch that tracks a local branch is not a vanished remote-tracking branch.
		"refs/heads/local": UpstreamNotGone, "refs/heads/main": UpstreamNotGone,
	}
	for name, state := range want {
		if got := by[name].Upstream; got != state {
			t.Errorf("%s: upstream %q, want %q", name, got, state)
		}
	}
}

// An unreadable state is "not known", never "not gone": the fact stays empty.
func TestObserveUpstreamUnreadableIsNotKnown(t *testing.T) {
	repo := upstreamFixture(t)
	obs := defaultWorktreeObserver()
	real := obs.read
	obs.read = func(ctx context.Context, bin, dir string, args ...string) (string, error) {
		for _, a := range args {
			if a == "for-each-ref" {
				return "", failure("git-query-refused", "fixture failure")
			}
		}
		return real(ctx, bin, dir, args...)
	}
	o, err := obs.observe(context.Background(), ObserveOptions{Path: repo, Base: observeBase})
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range o.Branches {
		if b.Upstream != UpstreamUnknown || b.Incomplete != "" || b.Proof == "" {
			t.Fatalf("%s: %+v (the fact is unknown, the rest of the evidence is unchanged)", b.Name, b)
		}
	}
}

// The fact adds no network call, no write and no other evidence: the same observation with the
// upstream configuration removed differs only in the fact.
func TestObserveUpstreamChangesNoOtherEvidence(t *testing.T) {
	repo := upstreamFixture(t)
	with := observe(t, repo)
	gitFixture(t, repo, "config", "--unset", "branch.gone.remote")
	gitFixture(t, repo, "config", "--unset", "branch.gone.merge")
	without := observe(t, repo)
	a, b := branchByName(with)["refs/heads/gone"], branchByName(without)["refs/heads/gone"]
	if a.Upstream != UpstreamGone || b.Upstream != UpstreamNotGone {
		t.Fatalf("with %q without %q", a.Upstream, b.Upstream)
	}
	a.Upstream, b.Upstream = "", ""
	if a != b {
		t.Fatalf("other evidence changed: %+v vs %+v", a, b)
	}
}
