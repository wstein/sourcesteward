// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

// The audit proper (Spec 021 FR-003 to FR-013): registry, validated directories, per-submodule evidence,
// findings from the normative table, bounds and a consistency resample. It runs only after ownership is
// established and the superproject is a non-bare Git worktree.

// pinsAuditLimits are the bounds of FR-011; tests lower them through pinsLimits.
type pinsAuditLimits struct {
	Retain, Scan int
	PerSubmodule time.Duration
	Total        time.Duration
	Dirty        pinsDirtyBudget
}

var pinsLimits = pinsAuditLimits{Retain: pinsRetainBudget, Scan: pinsScanAllowance, PerSubmodule: 5 * time.Second, Total: 30 * time.Second, Dirty: pinsDefaultDirtyBudget}

// pinsBeforeResample is a test seam that runs after the evidence is gathered and before the resample.
var pinsBeforeResample func()

type pinsIndexFingerprint struct {
	Size  int64
	Mtime int64
	Hash  [sha256.Size]byte
}

// pinsFingerprintIndex identifies the superproject index file without refreshing it.
func pinsFingerprintIndex(l pinsSuperLayout) (pinsIndexFingerprint, error) {
	var fp pinsIndexFingerprint
	info, err := pinsLstat(l.GitDir, "index")
	if err != nil {
		if os.IsNotExist(err) {
			return fp, nil
		}
		return fp, err
	}
	if !info.Mode().IsRegular() {
		return fp, errors.New("the superproject index is not a regular file")
	}
	root, err := os.OpenRoot(l.GitDir)
	if err != nil {
		return fp, err
	}
	defer root.Close()
	f, err := root.OpenFile("index", os.O_RDONLY|pinsOpenFlags, 0)
	if err != nil {
		return fp, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(f, 1<<30)); err != nil {
		return fp, err
	}
	copy(fp.Hash[:], h.Sum(nil))
	fp.Size, fp.Mtime = info.Size(), info.ModTime().UnixNano()
	return fp, nil
}

func pinsDisplayPtr(s *string) *string {
	if s == nil {
		return nil
	}
	d := pinsDisplay(*s)
	return &d
}

func pinsDisplay(s string) string {
	if utf8.ValidString(s) && !strings.ContainsRune(s, 0) {
		return s
	}
	return boundPinsMessage(s)
}

type pinsPendingFinding struct{ code, message string }

type pinsAuditor struct {
	r          *PinsResult
	total      context.Context
	parent     context.Context
	limits     pinsAuditLimits
	run        *pinsRunner
	safety     *pinsSafety
	superBrnch *string
	heads      map[string]string // submodule path -> HEAD commit at examination ("" when unborn)
	anyEval    bool
	dirs       []pinsExamined
}

// pinsExamined remembers the validated directories of an initialized submodule for the resample.
type pinsExamined struct {
	path string
	dirs pinsSubmoduleDirs
}

// limitText converts an exhausted bound into text; anything else is operational.
func (a *pinsAuditor) limitText(err error) (string, bool) {
	var le *pinsLimitError
	switch {
	case errors.As(err, &le):
		return le.Error(), true
	case a.parent.Err() != nil:
		return "", false
	case errors.Is(err, context.DeadlineExceeded):
		return "a deadline was exceeded", true
	}
	return "", false
}

func (a *pinsAuditor) add(code string, sub *string, msg string) {
	_ = a.r.AddFinding(code, sub, msg)
}

func pinsAudit(ctx context.Context, abs string, identity Result, r *PinsResult) error {
	limits := pinsLimits
	total, cancel := context.WithTimeout(ctx, limits.Total)
	defer cancel()
	run := newPinsRunner(total)
	run.budget.limit, run.scan = limits.Retain, limits.Scan
	a := &pinsAuditor{r: r, total: total, parent: ctx, limits: limits, run: run, heads: map[string]string{}}
	if identity.Repository != nil && identity.Repository.Head.Ref != nil {
		if b, ok := strings.CutPrefix(*identity.Repository.Head.Ref, "refs/heads/"); ok {
			a.superBrnch = &b
		}
	}
	err := a.audit(abs, identity)
	if err != nil {
		if msg, ok := a.limitText(err); ok {
			a.add("pins-limit", nil, "the audit stopped because a bound was exhausted: "+msg)
			return nil
		}
		return err
	}
	return nil
}

func (a *pinsAuditor) audit(abs string, identity Result) error {
	r := a.r
	layout, err := newPinsSuperLayout(a.run, abs)
	if err != nil {
		return err
	}
	reg, err := pinsEnumerate(a.run, layout)
	if err != nil {
		return err
	}
	a.safety = newPinsSafety(layout, a.run)
	resolved, err := pinsResolveAll(a.safety, reg)
	if err != nil {
		return err
	}
	headBefore, err := a.superHead(layout)
	if err != nil {
		return err
	}
	indexBefore, err := pinsFingerprintIndex(layout)
	if err != nil {
		return err
	}
	r.Identity = &identity
	if reg.Gitmodules.Modified {
		a.add("gitmodules-modified", nil, "the registration source differs: the HEAD .gitmodules is not a regular file, or the staged or worktree .gitmodules differs from it; registration is read from the HEAD blob")
	}
	for _, re := range resolved {
		if a.total.Err() != nil {
			break
		}
		if err := a.examineOne(re, reg); err != nil {
			return err
		}
	}
	if reg.Truncated {
		a.add("pins-limit", nil, fmt.Sprintf("%d submodules were found; only the first %d were examined", reg.Total, pinsMaxSubmodules))
	}
	if a.anyEval {
		a.add("remote-refs-may-be-stale", nil, "remote-tracking refs are local records and no fetch was made; they may be stale")
	}
	if pinsBeforeResample != nil {
		pinsBeforeResample()
	}
	return a.resample(layout, headBefore, indexBefore)
}

func (a *pinsAuditor) superHead(l pinsSuperLayout) (string, error) {
	out, err := a.run.run(pinsCall{Repo: pinsGitRepo{GitDir: l.GitDir, WorkTree: l.WorkTree}, Args: []string{"rev-parse", "--verify", "--quiet", "HEAD"}})
	if err != nil {
		return "", err
	}
	if out.Exit == 0 {
		return strings.TrimSpace(string(out.Stdout)), nil
	}
	return "", nil
}

// resample compares the superproject HEAD, the index identity and every examined submodule HEAD with
// what the evidence was based on. Any difference makes the result incomplete.
func (a *pinsAuditor) resample(l pinsSuperLayout, head string, index pinsIndexFingerprint) error {
	now, err := a.superHead(l)
	if err != nil {
		return err
	}
	if now != head {
		a.add("pins-state-changed", nil, "the superproject HEAD changed during the audit")
	}
	fp, err := pinsFingerprintIndex(l)
	if err != nil {
		return err
	}
	if fp != index {
		a.add("pins-state-changed", nil, "the superproject index changed during the audit")
	}
	for _, s := range a.r.Submodules {
		before, ok := a.heads[s.Path]
		if !ok {
			continue
		}
		cur, err := a.currentSubmoduleHead(s.Path)
		if err != nil {
			return err
		}
		if cur != before {
			p := s.Path
			a.add("pins-state-changed", &p, "the submodule HEAD changed during the audit")
		}
	}
	return nil
}

func (a *pinsAuditor) currentSubmoduleHead(path string) (string, error) {
	for _, d := range a.dirs {
		if d.path == path {
			h, err := pinsReadHead(a.run, pinsGitRepo{GitDir: d.dirs.GitDir, WorkTree: d.dirs.WorkTree})
			if err != nil {
				return "", err
			}
			if h.Commit == nil {
				return "", nil
			}
			return *h.Commit, nil
		}
	}
	return "", nil
}
