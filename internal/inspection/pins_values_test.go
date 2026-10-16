// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func intIs(p *int, want int) bool { return p != nil && *p == want }

func memberOf(f pinsFixture) string { return filepath.Join(f.Super, "member") }

func TestPinsAuditCheckoutValuesAreReportedExactly(t *testing.T) {
	f := newPinsFixture(t)
	commitIn(t, memberOf(f), "two")
	commitIn(t, memberOf(f), "three")
	settleIndexOf(t, f)
	c := subOf(t, auditOf(t, f.Super), "member").Checkout
	if c.RelationToPin != "ahead" || !intIs(c.Ahead, 2) || !intIs(c.Behind, 0) || c.Capped || c.Detached || c.Branch == nil || *c.Branch != "main" || c.HeadCommit == nil {
		t.Fatalf("%+v", c)
	}
	git(t, memberOf(f), "checkout", "-q", "--detach")
	c = subOf(t, auditOf(t, f.Super), "member").Checkout
	if !c.Detached || c.Branch != nil || c.HeadCommit == nil {
		t.Fatalf("detached: %+v", c)
	}
}

func TestPinsAuditCappedCountsAreReported(t *testing.T) {
	f := newPinsFixture(t)
	fastImport(t, memberOf(f), "long", f.Pin, 10005)
	git(t, memberOf(f), "checkout", "-q", "long")
	r := auditOf(t, f.Super)
	c := subOf(t, r, "member").Checkout
	if c.RelationToPin != "ahead" || !intIs(c.Ahead, 10000) || !c.Capped {
		t.Fatalf("%+v", c)
	}
	requireFinding(t, r, "checkout-differs-from-pin", "warning", "member")
}

func TestPinsAuditTrackingValuesAreReportedExactly(t *testing.T) {
	f := newPinsFixture(t)
	wt := memberOf(f)
	commitIn(t, wt, "two")
	c3 := commitIn(t, wt, "three")
	git(t, wt, "update-ref", "refs/remotes/origin/main", c3)
	git(t, wt, "reset", "-q", "--hard", f.Pin)
	r := auditOf(t, f.Super)
	tr := subOf(t, r, "member").Tracking
	if tr.Ref == nil || *tr.Ref != "refs/remotes/origin/main" || tr.Relation != "behind" || !intIs(tr.Ahead, 0) || !intIs(tr.Behind, 2) || tr.Capped {
		t.Fatalf("%+v", tr)
	}
	requireFinding(t, r, "pin-behind-tracking", "info", "member")
}

func TestPinsAuditDivergedTrackingIsNotBehind(t *testing.T) {
	f := newPinsFixture(t)
	wt := memberOf(f)
	up := commitIn(t, wt, "upstream")
	git(t, wt, "update-ref", "refs/remotes/origin/main", up)
	git(t, wt, "checkout", "-q", "-b", "side", f.Pin)
	side := commitIn(t, wt, "side")
	gitlink(t, f.Super, side, "member")
	git(t, f.Super, "commit", "-qm", "pin side")
	r := auditOf(t, f.Super)
	if tr := subOf(t, r, "member").Tracking; tr.Relation != "diverged" || !intIs(tr.Ahead, 1) || !intIs(tr.Behind, 1) {
		t.Fatalf("%+v", tr)
	}
	requireNoFinding(t, r, "pin-behind-tracking")
}

func TestPinsAuditCappedTrackingIsFlagged(t *testing.T) {
	f := newPinsFixture(t)
	wt := memberOf(f)
	fastImport(t, wt, "long", f.Pin, 10005)
	git(t, wt, "update-ref", "refs/remotes/origin/main", gitIn(t, wt, "rev-parse", "long"))
	tr := subOf(t, auditOf(t, f.Super), "member").Tracking
	if tr.Relation != "behind" || !intIs(tr.Behind, 10000) || !tr.Capped {
		t.Fatalf("%+v", tr)
	}
}

func editGitmodules(t *testing.T, f pinsFixture, args ...string) {
	t.Helper()
	git(t, f.Super, append([]string{"config", "-f", ".gitmodules"}, args...)...)
	git(t, f.Super, "add", ".gitmodules")
	git(t, f.Super, "commit", "-qm", "gitmodules")
}

func TestPinsAuditDotBranchFollowsTheSuperprojectBranch(t *testing.T) {
	f := newPinsFixture(t)
	git(t, memberOf(f), "update-ref", "refs/remotes/origin/work", f.Pin)
	git(t, f.Super, "checkout", "-q", "-b", "work")
	editGitmodules(t, f, "submodule.member.branch", ".")
	tr := subOf(t, auditOf(t, f.Super), "member").Tracking
	if tr.Ref == nil || *tr.Ref != "refs/remotes/origin/work" || tr.Relation != "equal" {
		t.Fatalf("%+v", tr)
	}
	git(t, f.Super, "checkout", "-q", "--detach")
	if tr = subOf(t, auditOf(t, f.Super), "member").Tracking; tr.Ref != nil || tr.Relation != "unknown" {
		t.Fatalf("detached: %+v", tr)
	}
}

func TestPinsAuditTrackingUsesTheOnlyRemoteWhenNoURLIsRegistered(t *testing.T) {
	f := newPinsFixture(t)
	editGitmodules(t, f, "--unset", "submodule.member.url")
	s := subOf(t, auditOf(t, f.Super), "member")
	if s.URL.Configured != nil || s.URL.Match != "unknown" || s.Tracking.Ref == nil || *s.Tracking.Ref != "refs/remotes/origin/main" || s.Tracking.Relation != "equal" {
		t.Fatalf("%+v %+v", s.URL, s.Tracking)
	}
}

func TestPinsAuditPublicationValues(t *testing.T) {
	f := newPinsFixture(t)
	wt := memberOf(f)
	for i := 0; i < 20; i++ {
		git(t, wt, "update-ref", fmt.Sprintf("refs/remotes/origin/b%02d", i), f.Pin)
	}
	fh := filepath.Join(f.Super, ".git", "modules", "member", "FETCH_HEAD")
	writeFile(t, fh, "x\n")
	when := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	mustOS(t, os.Chtimes(fh, when, when))
	p := subOf(t, auditOf(t, f.Super), "member").Publication
	if p.Status != "reachable" || len(p.ReachableFrom) != 16 || !p.Truncated || p.LastFetch == nil || *p.LastFetch != "2026-03-04T05:06:07Z" {
		t.Fatalf("%+v", p)
	}
}

func TestPinsAuditUnbornSuperprojectEvaluatesPublicationOfTheStagedPin(t *testing.T) {
	f := newPinsFixture(t)
	d := pinsRealDir(t)
	git(t, d, "init", "-q", "-b", "main")
	git(t, d, "-c", "protocol.file.allow=always", "submodule", "add", "-q", f.Sub, "member")
	s := subOf(t, auditOf(t, d), "member")
	if s.Publication.Status != "reachable" || s.Pin.IndexCommit == nil || s.Pin.HeadCommit != nil {
		t.Fatalf("%+v", s.Publication)
	}
}

func TestPinsAuditNestedPresentIsSetOnTheSubmodule(t *testing.T) {
	f := newPinsFixture(t)
	writeFile(t, filepath.Join(memberOf(f), ".gitmodules"), "[submodule \"n\"]\n\tpath = n\n\turl = https://example.invalid/n.git\n")
	git(t, memberOf(f), "add", ".gitmodules")
	git(t, memberOf(f), "commit", "-qm", "nested")
	if s := subOf(t, auditOf(t, f.Super), "member"); !s.Nested.Present {
		t.Fatalf("%+v", s.Nested)
	}
	if s := subOf(t, auditOf(t, newPinsFixtureOnly(t)), "member"); s.Nested.Present {
		t.Fatal("nested reported without a .gitmodules")
	}
}

func newPinsFixtureOnly(t *testing.T) string { return newPinsFixture(t).Super }

func TestPinsAuditNegativeFindings(t *testing.T) {
	// An absent pin object: the relation is unknown, so no checkout-differs-from-pin.
	f := newPinsFixture(t)
	gitlink(t, f.Super, strings.Repeat("ab", 20), "member")
	git(t, f.Super, "commit", "-qm", "bad pin")
	r := auditOf(t, f.Super)
	requireFinding(t, r, "pin-object-absent", "error", "member")
	requireNoFinding(t, r, "checkout-differs-from-pin")
	requireNoFinding(t, r, "pin-unpublished")
}

func TestPinsAuditConflictedGitlinkRaisesStagedPinDiffers(t *testing.T) {
	d := pinsRealDir(t)
	pinsIsolateHome(t)
	git(t, d, "init", "-q", "-b", "main")
	o := strings.Repeat("ab", 20)
	gitStdin(t, d, "160000 "+o+" 1\tm\n160000 "+o+" 2\tm\n160000 "+o+" 3\tm\n", "update-index", "--index-info")
	r := auditOf(t, d)
	if r.Status == "error" || r.Status == "refused" {
		t.Fatalf("%+v", r.Error)
	}
	if s := subOf(t, r, "m"); !s.Pin.StagedDiffers {
		t.Fatalf("%+v", s.Pin)
	}
	requireFinding(t, r, "staged-pin-differs", "warning", "m")
}

func TestPinsAuditNameIsEscapedPerField(t *testing.T) {
	f := newPinsFixture(t)
	writeFile(t, filepath.Join(f.Super, ".gitmodules"), "[submodule \"n\xff\"]\n\tpath = member\n\turl = "+f.Sub+"\n")
	git(t, f.Super, "add", ".gitmodules")
	git(t, f.Super, "commit", "-qm", "odd name")
	r := auditOf(t, f.Super)
	if r.Status == "error" {
		t.Fatalf("%+v", r.Error)
	}
	if s := subOf(t, r, "member"); s.Name == nil || *s.Name != `n\xff` {
		t.Fatalf("%v", s.Name)
	}
}
