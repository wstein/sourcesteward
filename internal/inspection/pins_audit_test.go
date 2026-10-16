// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func auditOf(t *testing.T, dir string) PinsResult {
	t.Helper()
	return Pins(context.Background(), PinsOptions{Path: dir})
}

func subOf(t *testing.T, r PinsResult, path string) PinsSubmodule {
	t.Helper()
	for _, s := range r.Submodules {
		if s.Path == path {
			return s
		}
	}
	t.Fatalf("no submodule %q in %+v", path, r.Submodules)
	return PinsSubmodule{}
}

func findingOf(r PinsResult, code string, sub string) *PinsFinding {
	for i, f := range r.Findings {
		if f.Code == code && (sub == "" && f.Submodule == nil || f.Submodule != nil && *f.Submodule == sub) {
			return &r.Findings[i]
		}
	}
	return nil
}

func requireFinding(t *testing.T, r PinsResult, code, severity, sub string) {
	t.Helper()
	f := findingOf(r, code, sub)
	if f == nil || f.Severity != severity || f.Message == "" {
		t.Fatalf("want %s/%s for %q, findings: %+v", code, severity, sub, r.Findings)
	}
}

func requireNoFinding(t *testing.T, r PinsResult, code string) {
	t.Helper()
	for _, f := range r.Findings {
		if f.Code == code {
			t.Fatalf("unexpected finding %+v", f)
		}
	}
}

// localOnlyPin makes a commit that exists only in the member checkout and pins it in the superproject,
// which is the incident this feature answers.
func localOnlyPin(t *testing.T, super string) string {
	t.Helper()
	member := filepath.Join(super, "member")
	oid := commitIn(t, member, "local-only")
	git(t, super, "add", "member")
	git(t, super, "commit", "-qm", "pin local-only commit")
	return oid
}

func TestPinsAuditPublishedPinIsCompleteAndClean(t *testing.T) {
	f := newPinsFixture(t)
	settleIndexOf(t, f)
	r := auditOf(t, f.Super)
	if r.Status != "ok" || r.Backend != "git" || !r.Boundary.Complete || r.Error != nil || r.Identity == nil || r.Identity.Status != "ok" || r.Identity.Backend != "git" {
		t.Fatalf("%+v", r)
	}
	s := subOf(t, r, "member")
	if s.Checkout.State != "initialized" || s.Checkout.RelationToPin != "same" || s.Publication.Status != "reachable" || len(s.Publication.ReachableFrom) != 1 ||
		s.Pin.HeadCommit == nil || *s.Pin.HeadCommit != f.Pin || s.Pin.StagedDiffers || s.Name == nil || *s.Name != "member" || s.Checkout.Branch == nil || *s.Checkout.Branch != "main" {
		t.Fatalf("%+v", s)
	}
	if s.URL.Match != "match" || s.URL.Configured == nil || s.Registration.Active == nil || !*s.Registration.Active {
		t.Fatalf("%+v", s.URL)
	}
	if r.Summary.Submodules != 1 || r.Summary.Errors != 0 || r.Summary.Publication.Reachable != 1 {
		t.Fatalf("%+v", r.Summary)
	}
	requireFinding(t, r, "remote-refs-may-be-stale", "info", "")
	for _, c := range []string{"pin-unpublished", "submodule-dirty", "url-mismatch", "staged-pin-differs", "checkout-differs-from-pin"} {
		requireNoFinding(t, r, c)
	}
}

// settleIndexOf keeps entries from being racily clean so the clean fixture reads as clean.
func settleIndexOf(t *testing.T, f pinsFixture) {
	t.Helper()
	settleIndex(t, filepath.Join(f.Super, ".git", "modules", "member"))
}

func TestPinsAuditIncidentFixtureMainWorktree(t *testing.T) {
	f := newPinsFixture(t)
	oid := localOnlyPin(t, f.Super)
	settleIndexOf(t, f)
	r := auditOf(t, f.Super)
	s := subOf(t, r, "member")
	if r.Status != "ok" || s.Publication.Status != "unreachable" || s.Pin.HeadCommit == nil || *s.Pin.HeadCommit != oid || s.Checkout.RelationToPin != "same" {
		t.Fatalf("%+v", r)
	}
	requireFinding(t, r, "pin-unpublished", "error", "member")
	if !strings.Contains(findingOf(r, "pin-unpublished", "member").Message, "local evidence") || r.Summary.Errors != 1 || r.Summary.Publication.Unreachable != 1 {
		t.Fatalf("%+v", r.Summary)
	}
}

func TestPinsAuditIncidentFixtureLinkedWorktree(t *testing.T) {
	f := newPinsFixture(t)
	linked := filepath.Join(f.Root, "linked")
	git(t, f.Super, "worktree", "add", "-q", "-b", "wt", linked)
	git(t, linked, "-c", "protocol.file.allow=always", "submodule", "update", "--init", "-q")
	oid := localOnlyPin(t, linked)
	settleIndex(t, filepath.Join(f.Super, ".git", "worktrees", "linked", "modules", "member"))
	r := auditOf(t, linked)
	s := subOf(t, r, "member")
	if r.Status != "ok" || s.Publication.Status != "unreachable" || *s.Pin.HeadCommit != oid {
		t.Fatalf("%+v", r)
	}
	requireFinding(t, r, "pin-unpublished", "error", "member")
	// The main worktree still sees the published pin.
	main := auditOf(t, f.Super)
	if subOf(t, main, "member").Publication.Status != "reachable" {
		t.Fatalf("%+v", main.Submodules)
	}
}

func TestPinsAuditUninitializedSubmodule(t *testing.T) {
	dir, oid := newBareSuper(t)
	gitlink(t, dir, oid, "ghost")
	git(t, dir, "commit", "-qm", "gitlink")
	r := auditOf(t, dir)
	s := subOf(t, r, "ghost")
	if r.Status != "ok" || s.Checkout.State != "uninitialized" || s.Publication.Status != "unknown" || s.Publication.Reason == "" || s.Checkout.Dirty.Reason == "" {
		t.Fatalf("%+v", s)
	}
	requireFinding(t, r, "submodule-uninitialized", "info", "ghost")
	requireFinding(t, r, "gitlink-without-entry", "error", "ghost")
	requireNoFinding(t, r, "remote-refs-may-be-stale")
}

func TestPinsAuditFindingsFromTheNormativeTable(t *testing.T) {
	type tc struct {
		name, code, severity string
		mutate               func(t *testing.T, f pinsFixture)
	}
	member := func(f pinsFixture) string { return filepath.Join(f.Super, "member") }
	for _, c := range []tc{
		{"absent pin object", "pin-object-absent", "error", func(t *testing.T, f pinsFixture) {
			gitlink(t, f.Super, strings.Repeat("ab", 20), "member")
			git(t, f.Super, "commit", "-qm", "bad pin")
		}},
		{"staged pin differs", "staged-pin-differs", "warning", func(t *testing.T, f pinsFixture) {
			commitIn(t, member(f), "two")
			git(t, f.Super, "add", "member")
		}},
		{"checkout ahead", "checkout-differs-from-pin", "warning", func(t *testing.T, f pinsFixture) { commitIn(t, member(f), "two") }},
		{"dirty tracked", "submodule-dirty", "warning", func(t *testing.T, f pinsFixture) {
			writeFile(t, filepath.Join(member(f), "file.txt"), "a much longer line\n")
		}},
		{"dirty untracked", "submodule-dirty", "warning", func(t *testing.T, f pinsFixture) { writeFile(t, filepath.Join(member(f), "new.txt"), "x") }},
		{"url mismatch", "url-mismatch", "warning", func(t *testing.T, f pinsFixture) {
			git(t, member(f), "remote", "set-url", "origin", "https://example.invalid/elsewhere.git")
		}},
		{"gitmodules modified", "gitmodules-modified", "warning", func(t *testing.T, f pinsFixture) {
			writeFile(t, filepath.Join(f.Super, ".gitmodules"), "[submodule \"member\"]\n\tpath = member\n\turl = "+f.Sub+"\n\tbranch = other\n")
		}},
		{"entry without gitlink", "entry-without-gitlink", "warning", func(t *testing.T, f pinsFixture) {
			writeFile(t, filepath.Join(f.Super, ".gitmodules"), "[submodule \"member\"]\n\tpath = member\n\turl = "+f.Sub+"\n[submodule \"x\"]\n\tpath = x\n\turl = https://example.invalid/x.git\n")
			git(t, f.Super, "add", ".gitmodules")
			git(t, f.Super, "commit", "-qm", "entry")
		}},
		{"credentials", "url-has-credentials", "error", func(t *testing.T, f pinsFixture) {
			writeFile(t, filepath.Join(f.Super, ".gitmodules"), "[submodule \"member\"]\n\tpath = member\n\turl = https://user:secret@example.invalid/r.git\n")
			git(t, f.Super, "add", ".gitmodules")
			git(t, f.Super, "commit", "-qm", "creds")
		}},
		{"nested", "nested-submodules-present", "info", func(t *testing.T, f pinsFixture) {
			writeFile(t, filepath.Join(member(f), ".gitmodules"), "[submodule \"n\"]\n\tpath = n\n\turl = https://example.invalid/n.git\n")
			git(t, member(f), "add", ".gitmodules")
			git(t, member(f), "commit", "-qm", "nested")
			git(t, f.Super, "add", "member")
			git(t, f.Super, "commit", "-qm", "pin nested")
		}},
		{"pin behind tracking", "pin-behind-tracking", "info", func(t *testing.T, f pinsFixture) {
			c := commitIn(t, member(f), "upstream")
			git(t, member(f), "update-ref", "refs/remotes/origin/main", c)
			git(t, member(f), "reset", "-q", "--hard", f.Pin)
		}},
		{"jj-managed submodule", "submodule-unsupported-backend", "warning", func(t *testing.T, f pinsFixture) { mkdirs(t, filepath.Join(member(f), ".jj")) }},
		{"unsafe path", "submodule-path-unsafe", "error", func(t *testing.T, f pinsFixture) {
			writeFile(t, filepath.Join(member(f), ".git"), "gitdir: /tmp/elsewhere\n")
		}},
		{"commondir", "submodule-path-unsafe", "error", func(t *testing.T, f pinsFixture) {
			writeFile(t, filepath.Join(f.Super, ".git", "modules", "member", "commondir"), "../..\n")
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newPinsFixture(t)
			c.mutate(t, f)
			settleIndexOf(t, f)
			r := auditOf(t, f.Super)
			if r.Status == "error" || r.Status == "refused" {
				t.Fatalf("%+v", r)
			}
			sub := "member"
			if c.code == "entry-without-gitlink" {
				sub = "x"
			}
			if c.code == "gitmodules-modified" {
				sub = ""
			}
			requireFinding(t, r, c.code, c.severity, sub)
			for _, fd := range r.Findings {
				if want, _ := PinsFindingSeverity(fd.Code); want != fd.Severity {
					t.Fatalf("severity drift: %+v", fd)
				}
			}
		})
	}
}

func TestPinsAuditRefusedSubmoduleDoesNotStopOthers(t *testing.T) {
	f := newPinsFixture(t)
	git(t, f.Super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", f.Sub, "second")
	git(t, f.Super, "commit", "-qm", "second")
	writeFile(t, filepath.Join(f.Super, "member", ".git"), "gitdir: /tmp/elsewhere\n")
	r := auditOf(t, f.Super)
	if r.Status != "ok" || !r.Boundary.Complete {
		t.Fatalf("%+v", r)
	}
	m := subOf(t, r, "member")
	if m.Refusal == nil || m.Refusal.Code != "submodule-path-unsafe" || m.Checkout.State != "refused" || m.Publication.Status != "unknown" {
		t.Fatalf("%+v", m)
	}
	if s := subOf(t, r, "second"); s.Checkout.State != "initialized" {
		t.Fatalf("%+v", s)
	}
}

func TestPinsAuditNoEvidenceProcessRunsForJJManagedSubmodule(t *testing.T) {
	f := newPinsFixture(t)
	mkdirs(t, filepath.Join(f.Super, "member", ".jj"))
	gitLog, jjLog := pinsSentinels(t)
	auditOf(t, f.Super)
	for _, l := range gitLog() {
		if strings.Contains(l, "--git-dir=") && strings.Contains(l, "modules/member") {
			t.Fatalf("Git ran against a jj-managed submodule: %s", l)
		}
	}
	if jjLog() != nil {
		t.Fatal("jj started")
	}
}

func TestPinsAuditStartsOnlyAllowlistedPinsCommands(t *testing.T) {
	f := newPinsFixture(t)
	settleIndexOf(t, f)
	gitLog, _ := pinsSentinels(t)
	r := auditOf(t, f.Super)
	if r.Status != "ok" {
		t.Fatalf("%+v", r)
	}
	var mine []string
	prefix := strings.Join(pinsIsolation(), " ")
	for _, l := range gitLog() {
		if strings.HasPrefix(l, prefix) {
			mine = append(mine, l)
		}
	}
	requireAllowlisted(t, mine, "rev-parse --verify --quiet HEAD", "cat-file -e <OID>^{commit}", "for-each-ref --contains <OID> --count=64 --format=%(refname)%09%(symref) refs/remotes/",
		"diff-index --cached --no-ext-diff --no-textconv -z <OID> --", "ls-files --stage --debug -z", "ls-files --others --exclude-standard -z", "ls-tree HEAD .gitmodules", "symbolic-ref -q <REMOTE-REF>")
}

func TestPinsAuditHostileConfigurationNeverExecutes(t *testing.T) {
	f := newPinsFixture(t)
	h := newHostile(t)
	member := filepath.Join(f.Super, "member")
	h.plant(t, filepath.Join(f.Super, ".git", "modules", "member"), member, false)
	h.plant(t, filepath.Join(f.Super, ".git"), f.Super, true)
	h.hostileGlobalConfig(t)
	writeFile(t, filepath.Join(member, "file.txt"), "ONE\n")
	statDirty(t, f.Super)
	statDirty(t, member)
	rmAll(t, h.log)
	r := auditOf(t, f.Super)
	if r.Status == "error" || r.Status == "refused" {
		t.Fatalf("%+v", r)
	}
	if got := h.executed(); len(got) != 0 {
		t.Fatalf("planted commands ran: %v", got)
	}
}

func TestPinsAuditSuperprojectWithoutSubmodulesIsComplete(t *testing.T) {
	dir, _ := newBareSuper(t)
	r := auditOf(t, dir)
	if r.Status != "ok" || !r.Boundary.Complete || len(r.Submodules) != 0 || r.Submodules == nil || r.Identity == nil {
		t.Fatalf("%+v", r)
	}
	requireNoFinding(t, r, "remote-refs-may-be-stale")
}

func TestPinsAuditUnbornSuperprojectHead(t *testing.T) {
	f := newPinsFixture(t)
	d := pinsRealDir(t)
	git(t, d, "init", "-q", "-b", "main")
	git(t, d, "-c", "protocol.file.allow=always", "submodule", "add", "-q", f.Sub, "member")
	r := auditOf(t, d)
	if r.Status == "error" || r.Status == "refused" {
		t.Fatalf("%+v", r)
	}
	s := subOf(t, r, "member")
	if s.Pin.HeadCommit != nil || s.Pin.IndexCommit == nil || !s.Pin.StagedDiffers || s.Checkout.RelationToPin != "unknown" {
		t.Fatalf("%+v", s)
	}
	requireFinding(t, r, "staged-pin-differs", "warning", "member")
}

func TestPinsAuditDetachedSuperprojectHead(t *testing.T) {
	f := newPinsFixture(t)
	git(t, f.Super, "checkout", "-q", "--detach")
	settleIndexOf(t, f)
	r := auditOf(t, f.Super)
	if r.Status != "ok" || !r.Boundary.Complete || subOf(t, r, "member").Publication.Status != "reachable" {
		t.Fatalf("%+v", r)
	}
}

func withLimits(t *testing.T, mutate func(*pinsAuditLimits)) {
	t.Helper()
	old := pinsLimits
	l := old
	mutate(&l)
	pinsLimits = l
	t.Cleanup(func() { pinsLimits = old })
}

func TestPinsAuditMoreThan256SubmodulesIsIncomplete(t *testing.T) {
	dir, oid := newBareSuper(t)
	paths := make([]string, 257)
	for i := range paths {
		paths[i] = fmt.Sprintf("m%03d", i)
	}
	gitlink(t, dir, oid, paths...)
	git(t, dir, "commit", "-qm", "many")
	r := auditOf(t, dir)
	if r.Status != "incomplete" || r.Boundary.Complete || len(r.Submodules) != 256 {
		t.Fatalf("%s complete=%v n=%d", r.Status, r.Boundary.Complete, len(r.Submodules))
	}
	requireFinding(t, r, "pins-limit", "error", "")
}

func TestPinsAuditExactly256SubmodulesIsComplete(t *testing.T) {
	dir, oid := newBareSuper(t)
	paths := make([]string, 256)
	for i := range paths {
		paths[i] = fmt.Sprintf("m%03d", i)
	}
	gitlink(t, dir, oid, paths...)
	git(t, dir, "commit", "-qm", "many")
	r := auditOf(t, dir)
	if r.Status != "ok" || len(r.Submodules) != 256 {
		t.Fatalf("%s n=%d %+v", r.Status, len(r.Submodules), r.Error)
	}
}

func TestPinsAuditBoundsGiveIncompleteResultsNeverSuccess(t *testing.T) {
	for name, mutate := range map[string]func(*pinsAuditLimits){
		"retained output": func(l *pinsAuditLimits) { l.Retain = 200 },
		"scan allowance":  func(l *pinsAuditLimits) { l.Scan = 50 },
		"per submodule":   func(l *pinsAuditLimits) { l.PerSubmodule = time.Nanosecond },
		"total":           func(l *pinsAuditLimits) { l.Total = time.Nanosecond },
	} {
		t.Run(name, func(t *testing.T) {
			f := newPinsFixture(t)
			withLimits(t, mutate)
			r := auditOf(t, f.Super)
			if r.Status != "incomplete" || r.Boundary.Complete {
				t.Fatalf("%s: %+v err=%+v", r.Status, r.Findings, r.Error)
			}
			requireNoFinding(t, r, "pin-unpublished")
			var limit bool
			for _, fd := range r.Findings {
				limit = limit || fd.Code == "pins-limit"
			}
			if !limit {
				t.Fatalf("no pins-limit: %+v", r.Findings)
			}
		})
	}
}

func TestPinsAuditDirtyBudgetNeverMakesTheResultIncomplete(t *testing.T) {
	f := newPinsFixture(t)
	withLimits(t, func(l *pinsAuditLimits) { l.Dirty = pinsDirtyBudget{Entries: 0, Time: time.Minute} })
	r := auditOf(t, f.Super)
	s := subOf(t, r, "member")
	if r.Status != "ok" || !r.Boundary.Complete || s.Checkout.Dirty.Tracked != "unknown" || s.Checkout.Dirty.Reason == "" {
		t.Fatalf("%+v", r)
	}
	requireNoFinding(t, r, "pins-limit")
}

func TestPinsAuditCancellationIsAFailure(t *testing.T) {
	f := newPinsFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := Pins(ctx, PinsOptions{Path: f.Super})
	if r.Status != "error" || r.Error == nil || r.Error.Code != "pins-failed" {
		t.Fatalf("%+v", r)
	}
}

func TestPinsAuditDetectsStateChangedDuringTheAudit(t *testing.T) {
	for name, tc := range map[string]struct {
		change func(t *testing.T, f pinsFixture)
		sub    string
	}{
		"superproject HEAD": {func(t *testing.T, f pinsFixture) {
			git(t, f.Super, "commit", "--allow-empty", "-qm", "moved")
		}, ""},
		"superproject index": {func(t *testing.T, f pinsFixture) {
			writeFile(t, filepath.Join(f.Super, "staged.txt"), "x")
			git(t, f.Super, "add", "staged.txt")
		}, ""},
		"submodule HEAD": {func(t *testing.T, f pinsFixture) { commitIn(t, filepath.Join(f.Super, "member"), "moved") }, "member"},
	} {
		change := tc.change
		t.Run(name, func(t *testing.T) {
			f := newPinsFixture(t)
			pinsBeforeResample = func() { change(t, f) }
			t.Cleanup(func() { pinsBeforeResample = nil })
			r := auditOf(t, f.Super)
			if r.Status != "incomplete" || r.Boundary.Complete {
				t.Fatalf("%s %+v", r.Status, r.Findings)
			}
			requireFinding(t, r, "pins-state-changed", "error", tc.sub)
		})
	}
}

func TestPinsAuditStableStateIsNotReportedAsChanged(t *testing.T) {
	f := newPinsFixture(t)
	called := false
	pinsBeforeResample = func() { called = true }
	t.Cleanup(func() { pinsBeforeResample = nil })
	r := auditOf(t, f.Super)
	requireNoFinding(t, r, "pins-state-changed")
	if !called {
		t.Fatal("the resample seam never ran")
	}
}

func TestPinsAuditEffectSentinelsLeaveTheRepositoryUntouched(t *testing.T) {
	f := newPinsFixture(t)
	settleIndexOf(t, f)
	idx := filepath.Join(f.Super, ".git", "index")
	midx := filepath.Join(f.Super, ".git", "modules", "member", "index")
	read := func(p string) (string, time.Time) {
		b, err := os.ReadFile(p)
		mustOS(t, err)
		st, _ := os.Stat(p)
		return string(b), st.ModTime()
	}
	b1, m1 := read(idx)
	b2, m2 := read(midx)
	refs := gitIn(t, f.Super, "for-each-ref")
	auditOf(t, f.Super)
	b1a, m1a := read(idx)
	b2a, m2a := read(midx)
	if b1 != b1a || !m1.Equal(m1a) || b2 != b2a || !m2.Equal(m2a) || refs != gitIn(t, f.Super, "for-each-ref") {
		t.Fatal("the audit changed repository state")
	}
	for _, p := range []string{filepath.Join(f.Super, ".git"), filepath.Join(f.Super, ".git", "modules", "member")} {
		if matches, _ := filepath.Glob(filepath.Join(p, "*.lock")); len(matches) != 0 {
			t.Fatalf("lock files remain: %v", matches)
		}
	}
}

func TestPinsAuditNonUTF8BranchIsEscapedPerFieldNotRefusedWhole(t *testing.T) {
	f := newPinsFixture(t)
	member := filepath.Join(f.Super, "member")
	git(t, member, "checkout", "-q", "-b", "br\xff")
	git(t, member, "remote", "add", "caf\xe9", "https://example.invalid/x.git")
	settleIndexOf(t, f)
	r := auditOf(t, f.Super)
	if r.Status != "ok" || r.Error != nil || r.Identity == nil {
		t.Fatalf("one hostile branch must not fail the whole result: %+v", r.Error)
	}
	s := subOf(t, r, "member")
	if s.Checkout.Branch == nil || *s.Checkout.Branch != `br\xff` {
		t.Fatalf("%v", s.Checkout.Branch)
	}
	if _, err := json.Marshal(r); err != nil {
		t.Fatal(err)
	}
}

func TestPinsFailedResultNeverKeepsAnIdentity(t *testing.T) {
	r := NewPinsResult(".")
	id := Result{Status: "ok", Backend: "git"}
	r.Identity, r.Status = &id, "ok"
	r.Findings = []PinsFinding{{Code: "pins-limit", Severity: "error", Message: "bad \xff"}}
	r.failWhole("result holds invalid text")
	if r.Identity != nil || r.Status != "error" || r.Error == nil || r.Error.Code != "pins-failed" || len(r.Findings) != 0 || len(r.Submodules) != 0 {
		t.Fatalf("%+v", r)
	}
}

func longPaths(n, comps int) []string {
	paths := make([]string, n)
	for i := range paths {
		parts := make([]string, comps)
		for j := range parts {
			parts[j] = fmt.Sprintf("%s%03d%03d", strings.Repeat("x", 190), i, j)
		}
		paths[i] = strings.Join(parts, "/")
	}
	return paths
}

func TestPinsAuditEarlyBoundStillCarriesTheIdentity(t *testing.T) {
	dir, oid := newBareSuper(t)
	gitlink(t, dir, oid, longPaths(256, 19)...) // about 3.7 KB each: enumeration exceeds the 1 MiB budget
	git(t, dir, "commit", "-qm", "long")
	r := auditOf(t, dir)
	if r.Status != "incomplete" || r.Identity == nil || r.Identity.Status != "ok" || r.Identity.Backend != "git" {
		t.Fatalf("%s identity=%v findings=%+v", r.Status, r.Identity, r.Findings)
	}
}

// The baseline for the consistency check is taken before enumeration reads anything, and the registry is
// read from the HEAD object name it resolved, not from the moving name HEAD.
func TestPinsAuditDetectsChangeDuringEnumeration(t *testing.T) {
	for name, change := range map[string]func(t *testing.T, f pinsFixture){
		"HEAD": func(t *testing.T, f pinsFixture) { git(t, f.Super, "commit", "--allow-empty", "-qm", "moved") },
		"index": func(t *testing.T, f pinsFixture) {
			writeFile(t, filepath.Join(f.Super, "staged.txt"), "x")
			git(t, f.Super, "add", "staged.txt")
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newPinsFixture(t)
			pinsAfterHead = func() { pinsAfterHead = nil; change(t, f) }
			t.Cleanup(func() { pinsAfterHead = nil })
			r := auditOf(t, f.Super)
			if r.Status != "incomplete" {
				t.Fatalf("%s %+v", r.Status, r.Findings)
			}
			requireFinding(t, r, "pins-state-changed", "error", "")
		})
	}
}

func TestPinsRegistryReadsTheResolvedHeadObjectNotTheName(t *testing.T) {
	f := newPinsFixture(t)
	old := gitIn(t, f.Super, "rev-parse", "HEAD")
	pinsAfterHead = func() {
		pinsAfterHead = nil
		gitlink(t, f.Super, strings.Repeat("ab", 20), "other")
		git(t, f.Super, "commit", "-qm", "moved")
	}
	t.Cleanup(func() { pinsAfterHead = nil })
	reg := registryOf(t, f.Super)
	if reg.HeadCommit == nil || *reg.HeadCommit != old {
		t.Fatalf("%v", reg.HeadCommit)
	}
	for _, e := range reg.Entries {
		if e.Path == "other" && e.GitlinkInHead {
			t.Fatal("the registry read a HEAD that moved after it was resolved")
		}
	}
}

func TestPinsAuditUnbornSubmoduleWithAWorktreeFileNamedHEAD(t *testing.T) {
	f := newPinsFixture(t)
	member := filepath.Join(f.Super, "member")
	git(t, member, "checkout", "-q", "--orphan", "fresh")
	writeFile(t, filepath.Join(member, "HEAD"), "x")
	settleIndexOf(t, f)
	r := auditOf(t, f.Super)
	s := subOf(t, r, "member")
	if r.Status != "ok" || s.Checkout.State != "initialized" || s.Checkout.HeadCommit != nil || s.Checkout.Branch == nil || *s.Checkout.Branch != "fresh" {
		t.Fatalf("%s %+v", r.Status, s.Checkout)
	}
}

// One submodule whose evidence cannot be read is unknown with a reason; it never fails the whole audit.
func TestPinsAuditUnreadableEvidenceIsUnknownForThatSubmoduleOnly(t *testing.T) {
	f := newPinsFixture(t)
	git(t, f.Super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", f.Sub, "second")
	git(t, f.Super, "commit", "-qm", "second")
	mustOS(t, os.WriteFile(filepath.Join(f.Super, ".git", "modules", "member", "refs", "heads", "main"), []byte("not-an-object-name\n"), 0o600))
	settleIndex(t, filepath.Join(f.Super, ".git", "modules", "second"))
	r := auditOf(t, f.Super)
	if r.Status != "ok" || !r.Boundary.Complete || r.Error != nil {
		t.Fatalf("%s %+v", r.Status, r.Error)
	}
	m := subOf(t, r, "member")
	if m.Publication.Status != "unknown" || !strings.Contains(m.Publication.Reason, "could not be read") || m.Checkout.Dirty.Reason == "" {
		t.Fatalf("%+v", m)
	}
	if s := subOf(t, r, "second"); s.Publication.Status != "reachable" {
		t.Fatalf("%+v", s.Publication)
	}
}
