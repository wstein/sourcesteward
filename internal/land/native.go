// SPDX-License-Identifier: EUPL-1.2

package land

import (
	"fmt"
	"strconv"
)

// The native stack sequence (Spec 043 FR-084 to FR-088). The plan lists the retarget effects; the
// functions below are pure and read-only: they compare two plans and name the restore commands. The
// writes of an apply are not in this package (FR-092).

// Reasons of the re-read after the retarget (FR-087).
const (
	ReasonRetargetNotApplied = "retarget-not-applied"
	ReasonChildClosed        = "child-closed"
	ReasonBaseMoved          = "base-moved"
	ReasonPlanNotLandable    = "plan-not-landable"
)

// NewNativeShapeSet builds a set whose keys are recorded as measured for the native sequence; such an
// entry admits the flat chain of the same shape as well.
func NewNativeShapeSet(keys ...string) *ShapeSet {
	set := NewShapeSet(keys...)
	for _, k := range keys {
		set.native[k] = true
	}
	return set
}

// HasNative reports whether the shape is recorded as measured for the native sequence. An unreadable
// shape never matches.
func (s *ShapeSet) HasNative(shape Shape) bool {
	return s != nil && shape.Readable && s.keys[shape.Key()] && s.native[shape.Key()]
}

func retargets(r Report) []Effect {
	var out []Effect
	for _, e := range r.Effects {
		if e.Kind == retargetKind && len(e.Argv) == 8 {
			out = append(out, e)
		}
	}
	return out
}

func entry(r Report, number int) (ChainEntry, bool) {
	for _, c := range r.Chain {
		if c.Number == number {
			return c, true
		}
	}
	return ChainEntry{}, false
}

// Reverify is the re-read of FR-087: before is the landable plan with its retarget effects, after is
// the plan evaluated again once the retargets are done. It returns the reasons that refuse the push;
// an empty result means the push may follow. It reads nothing and writes nothing.
func Reverify(before, after Report) []Reason {
	if !before.Landable {
		return []Reason{{Code: ReasonPlanNotLandable, Detail: "the plan before the retarget was not landable"}}
	}
	var out []Reason
	seen := map[string]bool{}
	add := func(code string, n int, detail string) {
		k := code + "#" + strconv.Itoa(n)
		if !seen[k] {
			seen[k] = true
			out = append(out, Reason{Code: code, Number: n, Detail: detail})
		}
	}
	closed := map[int]bool{}
	for _, b := range before.Chain {
		a, ok := entry(after, b.Number)
		switch {
		case !ok:
			add(ReasonChildClosed, b.Number, "the pull request is absent from the re-read")
			closed[b.Number] = true
		case a.State != "open":
			add(ReasonChildClosed, b.Number, fmt.Sprintf("the pull request is %s after the retarget; stw does not reopen or recreate it", a.State))
			closed[b.Number] = true
		case a.Head != b.Head:
			add(ReasonHeadChanged, b.Number, fmt.Sprintf("head was %s, is %s", b.Head, a.Head))
		}
	}
	for _, e := range retargets(before) {
		n, err := strconv.Atoi(e.Argv[3])
		if err != nil || closed[n] {
			continue
		}
		if a, ok := entry(after, n); ok && a.BaseRef != e.Argv[7] {
			add(ReasonRetargetNotApplied, n, fmt.Sprintf("the base is %s, expected %s", a.BaseRef, e.Argv[7]))
		}
	}
	if after.DefaultTip != before.DefaultTip {
		add(ReasonBaseMoved, 0, fmt.Sprintf("the default branch tip was %s, is %s", before.DefaultTip, after.DefaultTip))
	} else if after.PlanDigest != before.PlanDigest && len(out) == 0 {
		add(ReasonHeadChanged, 0, "the plan digest differs from the one the approval bound")
	}
	for _, x := range after.Reasons {
		add(x.Code, x.Number, x.Detail)
	}
	return out
}

// RestoreArgv lists, in chain order, the commands that put each retargeted pull request back on its
// old base (FR-088). They are printed for the owner and never run by stw.
func RestoreArgv(before Report) [][]string {
	var out [][]string
	for _, e := range retargets(before) {
		n, err := strconv.Atoi(e.Argv[3])
		if err != nil {
			continue
		}
		if c, ok := entry(before, n); ok {
			out = append(out, RetargetArgv(e.Argv[5], n, c.BaseRef))
		}
	}
	return out
}
