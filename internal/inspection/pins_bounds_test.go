// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// slowGit puts a `git` first on PATH that sleeps before running the real one when the arguments contain
// the match text. The sleep detaches its output so a killed wrapper closes the pipe at once. It makes deadlines observable without depending on machine speed.
func slowGit(t *testing.T, match string, sleep string) {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is unavailable")
	}
	dir := t.TempDir()
	body := "#!/bin/sh\ncase \"$*\" in *'" + match + "'*) sleep " + sleep + " >/dev/null 2>&1 ;; esac\nexec '" + real + "' \"$@\"\n"
	mustOS(t, os.WriteFile(filepath.Join(dir, "git"), []byte(body), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func twoSubmoduleFixture(t *testing.T) pinsFixture {
	t.Helper()
	f := newPinsFixture(t)
	git(t, f.Super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", f.Sub, "second")
	git(t, f.Super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", f.Sub, "third")
	git(t, f.Super, "commit", "-qm", "more")
	return f
}

func TestPinsAuditCancelledDuringEvidenceIsAFailureNeverALimit(t *testing.T) {
	f := newPinsFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	pinsBeforeResample = cancel
	t.Cleanup(func() { pinsBeforeResample = nil })
	r := Pins(ctx, PinsOptions{Path: f.Super})
	if r.Status != "error" || r.Error == nil || r.Error.Code != "pins-failed" || r.Error.Message != "pins canceled" || r.Identity != nil {
		t.Fatalf("%s %+v", r.Status, r.Error)
	}
	for _, fd := range r.Findings {
		t.Fatalf("a cancellation must not leave findings: %+v", fd)
	}
}

func TestPinsAuditCancelledMidSubmoduleIsAFailureNeverALimit(t *testing.T) {
	f := newPinsFixture(t)
	slowGit(t, "ls-files --stage --debug", "1")
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(400*time.Millisecond, cancel)
	r := Pins(ctx, PinsOptions{Path: f.Super})
	if r.Status != "error" || r.Error == nil || r.Error.Code != "pins-failed" || r.Error.Message != "pins canceled" {
		t.Fatalf("%s %+v %+v", r.Status, r.Error, r.Findings)
	}
}

func TestPinsAuditTotalDeadlineExpiringBetweenSubmodulesIsIncomplete(t *testing.T) {
	f := twoSubmoduleFixture(t)
	slowGit(t, "ls-files --others", "0.6")
	withLimits(t, func(l *pinsAuditLimits) { l.Total = time.Second })
	r := Pins(context.Background(), PinsOptions{Path: f.Super})
	if r.Status != "incomplete" || r.Boundary.Complete || r.Error != nil {
		t.Fatalf("%s %+v %+v", r.Status, r.Error, r.Findings)
	}
	if len(r.Submodules) == 0 || len(r.Submodules) >= 3 {
		t.Fatalf("the loop must stop at the total deadline: %d submodules", len(r.Submodules))
	}
	if findingOf(r, "pins-limit", "") == nil && !hasLimitFinding(r) {
		t.Fatalf("%+v", r.Findings)
	}
}

func hasLimitFinding(r PinsResult) bool {
	for _, f := range r.Findings {
		if f.Code == "pins-limit" {
			return true
		}
	}
	return false
}

func TestPinsAuditPerSubmoduleDeadlineIsNotTheParentsAndLaterSubmodulesRun(t *testing.T) {
	f := twoSubmoduleFixture(t)
	// Only the first submodule's scan is slow; the others finish inside their own 5 seconds.
	slowGit(t, "modules/member", "4")
	withLimits(t, func(l *pinsAuditLimits) { l.PerSubmodule = 2 * time.Second })
	r := Pins(context.Background(), PinsOptions{Path: f.Super})
	if r.Status != "incomplete" || len(r.Submodules) != 3 {
		t.Fatalf("%s n=%d %+v", r.Status, len(r.Submodules), r.Findings)
	}
	requireFinding(t, r, "pins-limit", "error", "member")
	if s := subOf(t, r, "second"); s.Publication.Status != "reachable" {
		t.Fatalf("a slow submodule stopped the others: %+v", s.Publication)
	}
}

// The dirty budget is its own bound: a slow scan is unknown with the budget reason, status ok.
func TestPinsAuditSlowDirtyScanIsUnknownWithinTheDirtyBudget(t *testing.T) {
	f := newPinsFixture(t)
	slowGit(t, "ls-files --stage --debug", "2")
	withLimits(t, func(l *pinsAuditLimits) { l.Dirty = pinsDirtyBudget{Entries: 20000, Time: 200 * time.Millisecond} })
	start := time.Now()
	r := Pins(context.Background(), PinsOptions{Path: f.Super})
	s := subOf(t, r, "member")
	if r.Status != "ok" || !r.Boundary.Complete || s.Checkout.Dirty.Tracked != "unknown" || !strings.Contains(s.Checkout.Dirty.Reason, "budget") {
		t.Fatalf("%s %+v %+v", r.Status, s.Checkout.Dirty, r.Findings)
	}
	requireNoFinding(t, r, "pins-limit")
	if time.Since(start) > 3*time.Second {
		t.Fatalf("the dirty budget did not bound the scan: %s", time.Since(start))
	}
}

func TestPinsAuditDirtyScanAllowanceReachesTheResultAsALimit(t *testing.T) {
	f := newPinsFixture(t)
	withLimits(t, func(l *pinsAuditLimits) { l.Scan = 300 })
	r := Pins(context.Background(), PinsOptions{Path: f.Super})
	if r.Status != "incomplete" || !hasLimitFinding(r) {
		t.Fatalf("%s %+v", r.Status, r.Findings)
	}
}

// The caller's own deadline is the caller giving up: a failure, never one of the audit's bounds.
func TestPinsAuditCallersDeadlineIsAFailureNotALimit(t *testing.T) {
	f := newPinsFixture(t)
	slowGit(t, "ls-files --stage --debug", "2")
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	r := Pins(ctx, PinsOptions{Path: f.Super})
	if r.Status != "error" || r.Error == nil || r.Error.Message != "pins canceled" || len(r.Findings) != 0 {
		t.Fatalf("%s %+v %+v", r.Status, r.Error, r.Findings)
	}
}

// The 30 second total also bounds the evidence of a submodule that is still running, not only the gaps.
func TestPinsAuditTotalDeadlineStopsARunningSubmodule(t *testing.T) {
	f := newPinsFixture(t)
	slowGit(t, "ls-files --stage --debug", "5")
	withLimits(t, func(l *pinsAuditLimits) {
		l.Total = 500 * time.Millisecond
		l.PerSubmodule = time.Minute
		l.Dirty = pinsDirtyBudget{Entries: 20000, Time: time.Minute}
	})
	start := time.Now()
	r := Pins(context.Background(), PinsOptions{Path: f.Super})
	if r.Status != "incomplete" || !hasLimitFinding(r) || time.Since(start) > 3*time.Second {
		t.Fatalf("%s after %s %+v", r.Status, time.Since(start), r.Findings)
	}
}

func TestPinsAuditSameSizeIndexRewriteIsAStateChange(t *testing.T) {
	f := newPinsFixture(t)
	idx := filepath.Join(f.Super, ".git", "index")
	pinsBeforeResample = func() {
		info, err := os.Stat(idx)
		mustOS(t, err)
		b, err := os.ReadFile(idx)
		mustOS(t, err)
		b[len(b)-1] ^= 0xff // same size; the audit never parses the superproject index again
		mustOS(t, os.WriteFile(idx, b, 0o600))
		mustOS(t, os.Chtimes(idx, info.ModTime(), info.ModTime()))
	}
	t.Cleanup(func() { pinsBeforeResample = nil })
	r := auditOf(t, f.Super)
	if r.Status != "incomplete" {
		t.Fatalf("%s %+v", r.Status, r.Findings)
	}
	requireFinding(t, r, "pins-state-changed", "error", "")
}
