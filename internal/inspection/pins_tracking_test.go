// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func (c *publishCase) track(t *testing.T, pin string, branch, superBranch *string) pinsTracking {
	t.Helper()
	facts, err := pinsReadFacts(c.run, c.dirs)
	if err != nil {
		t.Fatal(err)
	}
	tr, err := pinsTrack(c.run, c.repo, facts, pinsTrackInput{Pin: &pin, Branch: branch, SuperBranch: superBranch, Remote: pinsSoleRemote(facts)})
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func sp(s string) *string { return &s }

func TestPinsTrackingRelations(t *testing.T) {
	c := newPublishCase(t)
	wt := c.repo.WorkTree
	tr := c.track(t, c.f.Pin, nil, nil)
	if tr.Relation != "equal" || tr.Ref == nil || *tr.Ref != "refs/remotes/origin/main" || *tr.Ahead != 0 || *tr.Behind != 0 {
		t.Fatalf("default ref from origin/HEAD: %+v", tr)
	}
	c2 := commitIn(t, wt, "two")
	c3 := commitIn(t, wt, "three")
	git(t, wt, "update-ref", "refs/remotes/origin/main", c3)
	tr = c.track(t, c.f.Pin, nil, nil)
	if tr.Relation != "behind" || *tr.Ahead != 0 || *tr.Behind != 2 || tr.Capped {
		t.Fatalf("behind: %+v", tr)
	}
	git(t, wt, "update-ref", "refs/remotes/origin/main", c.f.Pin)
	tr = c.track(t, c3, nil, nil)
	if tr.Relation != "ahead" || *tr.Ahead != 2 || *tr.Behind != 0 {
		t.Fatalf("ahead: %+v", tr)
	}
	git(t, wt, "update-ref", "refs/remotes/origin/main", c2)
	git(t, wt, "checkout", "-q", "-b", "side", c.f.Pin)
	side := commitIn(t, wt, "side")
	tr = c.track(t, side, nil, nil)
	if tr.Relation != "diverged" || *tr.Ahead != 1 || *tr.Behind != 1 {
		t.Fatalf("diverged: %+v", tr)
	}
}

func TestPinsTrackingBranchFromGitmodules(t *testing.T) {
	c := newPublishCase(t)
	wt := c.repo.WorkTree
	topic := commitIn(t, wt, "topic")
	git(t, wt, "update-ref", "refs/remotes/origin/topic", topic)
	tr := c.track(t, c.f.Pin, sp("topic"), nil)
	if tr.Ref == nil || *tr.Ref != "refs/remotes/origin/topic" || tr.Relation != "behind" || *tr.Behind != 1 {
		t.Fatalf("%+v", tr)
	}
	// "." follows the superproject branch.
	tr = c.track(t, c.f.Pin, sp("."), sp("topic"))
	if tr.Ref == nil || *tr.Ref != "refs/remotes/origin/topic" || tr.Relation != "behind" {
		t.Fatalf("%+v", tr)
	}
	tr = c.track(t, c.f.Pin, sp("."), nil)
	if tr.Relation != "unknown" || tr.Reason == "" || tr.Ref != nil {
		t.Fatalf("detached superproject: %+v", tr)
	}
	// A configured branch the repository has not fetched: the ref is named, the relation unknown.
	tr = c.track(t, c.f.Pin, sp("never-fetched"), nil)
	if tr.Ref == nil || *tr.Ref != "refs/remotes/origin/never-fetched" || tr.Relation != "unknown" || tr.Reason == "" || tr.Ahead != nil {
		t.Fatalf("%+v", tr)
	}
}

func TestPinsTrackingUnknownWithoutAUsableRemoteOrDefault(t *testing.T) {
	c := newPublishCase(t)
	wt := c.repo.WorkTree
	git(t, wt, "remote", "add", "second", "https://example.invalid/second.git")
	tr := c.track(t, c.f.Pin, nil, nil)
	if tr.Relation != "unknown" || tr.Ref != nil || !strings.Contains(tr.Reason, "remote") {
		t.Fatalf("two remotes and no match: %+v", tr)
	}
	git(t, wt, "remote", "remove", "second")
	git(t, wt, "remote", "remove", "origin")
	if tr = c.track(t, c.f.Pin, nil, nil); tr.Relation != "unknown" || tr.Ref != nil {
		t.Fatalf("no remote: %+v", tr)
	}
}

func TestPinsTrackingNoDefaultRefRecorded(t *testing.T) {
	c := newPublishCase(t)
	mustOS(t, os.Remove(filepath.Join(c.repo.GitDir, "refs", "remotes", "origin", "HEAD")))
	tr := c.track(t, c.f.Pin, nil, nil)
	if tr.Relation != "unknown" || tr.Ref != nil || tr.Reason == "" {
		t.Fatalf("%+v", tr)
	}
}

func TestPinsTrackingNeverPassesUntrustedBranchNamesToGit(t *testing.T) {
	c := newPublishCase(t)
	gitLog, _ := pinsSentinels(t)
	for _, b := range []string{"../x", "-x", "a b", "x^{tree}", "x@{1}", "a..b", "x:y", "x~1", "x\ny", "", "x.lock", "x/", "/x", "refs/../x", "a\\b", "x?", "x*", "x["} {
		tr := c.track(t, c.f.Pin, sp(b), nil)
		if tr.Relation != "unknown" || tr.Ref != nil || tr.Reason == "" {
			t.Errorf("branch %q: %+v", b, tr)
		}
	}
	for _, l := range gitLog() {
		for _, bad := range []string{"../x", "-x", "x^{tree}", "@{1}", "x~1", "x:y"} {
			if strings.Contains(l, "refs/remotes/origin/"+bad) {
				t.Errorf("an untrusted branch reached Git: %s", l)
			}
		}
	}
}

func TestPinsTrackingBranchWithSlashAndDots(t *testing.T) {
	c := newPublishCase(t)
	git(t, c.repo.WorkTree, "update-ref", "refs/remotes/origin/release/1.2-x_y+z", c.f.Pin)
	tr := c.track(t, c.f.Pin, sp("release/1.2-x_y+z"), nil)
	if tr.Relation != "equal" {
		t.Fatalf("%+v", tr)
	}
}

func TestPinsTrackingMissingPinObjectIsUnknown(t *testing.T) {
	c := newPublishCase(t)
	tr := c.track(t, strings.Repeat("ab", 20), nil, nil)
	if tr.Relation != "unknown" || tr.Reason == "" || tr.Ahead != nil {
		t.Fatalf("%+v", tr)
	}
}

func TestPinsTrackingStartsOnlyAllowlistedGitCommands(t *testing.T) {
	c := newPublishCase(t)
	gitLog, _ := pinsSentinels(t)
	c.track(t, c.f.Pin, nil, nil)
	requireAllowlisted(t, gitLog(), "symbolic-ref -q refs/remotes/<REMOTE>/HEAD", "rev-parse --verify --quiet refs/remotes/<REMOTE>/<BRANCH>^{commit}")
}

func TestPinsTrackingCappedCountsAreFlagged(t *testing.T) {
	c := newPublishCase(t)
	wt := c.repo.WorkTree
	fastImport(t, wt, "long", c.f.Pin, 10005)
	git(t, wt, "update-ref", "refs/remotes/origin/main", gitIn(t, wt, "rev-parse", "long"))
	tr := c.track(t, c.f.Pin, nil, nil)
	if tr.Relation != "behind" || *tr.Behind != 10000 || !tr.Capped {
		t.Fatalf("%+v", tr)
	}
}
