// SPDX-License-Identifier: EUPL-1.2

package land

import (
	"strconv"
	"strings"
	"testing"
)

// native turns the scenario into a native stack: 12 targets the head branch of 11, 13 the one of 12.
func native(s *scenario) {
	for n, base := range map[int]string{12: "feat-1", 13: "feat-2"} {
		p := s.forge.pulls[n]
		p.BaseRef = base
		s.forge.pulls[n] = p
	}
}

// nativeMeasured records the repository shape as measured for the native sequence.
func nativeMeasured(s *scenario) {
	shape := ShapeOf(s.forge.repo, s.forge.rulesets, true)
	s.opts.Shapes = NewNativeShapeSet(shape.Key())
}

func nativeHappy() *scenario {
	s := newScenario()
	native(s)
	nativeMeasured(s)
	return s
}

func effectsOf(r Report, kind string) []Effect {
	var out []Effect
	for _, e := range r.Effects {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

func TestNativeChainIsLandableWithRecordedEvidenceAndRetargetsBottomFirst(t *testing.T) {
	r := nativeHappy().plan()
	if !r.Landable || len(r.Reasons) != 0 {
		t.Fatalf("landable=%v reasons=%v", r.Landable, reasonCodes(r))
	}
	var order []string
	for _, e := range r.Effects {
		order = append(order, e.ID)
	}
	if got := strings.Join(order, ","); got != "fetch,retarget-12,retarget-13,push" {
		t.Fatalf("effect order %s", got)
	}
	rt := effectsOf(r, "retarget")
	if strings.Join(rt[0].Argv, " ") != "gh pr edit 12 --base main" || strings.Join(rt[1].Argv, " ") != "gh pr edit 13 --base main" {
		t.Fatalf("retarget argv %v %v", rt[0].Argv, rt[1].Argv)
	}
}

func TestNativeChainWithoutEvidenceIsLandUnmeasuredButShowsTheSequence(t *testing.T) {
	s := newScenario()
	native(s)
	measured(s) // the shape is recorded for the flat chain only
	r := s.plan()
	if r.Landable || !has(r, ReasonLandUnmeasured, 12) || !has(r, ReasonLandUnmeasured, 13) {
		t.Fatalf("landable=%v reasons=%v", r.Landable, reasonCodes(r))
	}
	detail := ""
	for _, x := range r.Reasons {
		if x.Number == 12 && x.Code == ReasonLandUnmeasured {
			detail = x.Detail
		}
	}
	if !strings.Contains(detail, DetailNativeStackShape) {
		t.Fatalf("detail %q", detail)
	}
	if len(effectsOf(r, "retarget")) != 2 {
		t.Fatalf("the sequence must be listed even when refused: %+v", r.Effects)
	}
	joined := strings.Join(r.Notes, " | ")
	if !strings.Contains(joined, "fixtures only") || !strings.Contains(joined, "scripts/measure-native-stack.sh") {
		t.Fatalf("evidence note missing: %q", joined)
	}
	if r.ApplyAllowed {
		t.Fatal("apply must not be allowed")
	}
}

func TestFlatChainStaysWithoutRetarget(t *testing.T) {
	r := happy().plan()
	if n := len(effectsOf(r, "retarget")); n != 0 {
		t.Fatalf("flat chain planned %d retargets", n)
	}
}

func TestNativeBaseShapes(t *testing.T) {
	// a mixed chain: 12 native, 13 already targets main
	s := nativeHappy()
	p := s.forge.pulls[13]
	p.BaseRef = "main"
	s.forge.pulls[13] = p
	r := s.plan()
	if !r.Landable || len(effectsOf(r, "retarget")) != 1 || effectsOf(r, "retarget")[0].ID != "retarget-12" {
		t.Fatalf("mixed: landable=%v reasons=%v effects=%+v", r.Landable, reasonCodes(r), r.Effects)
	}
	// the base is the head branch of a pull request that is not the immediate predecessor
	s = nativeHappy()
	p = s.forge.pulls[13]
	p.BaseRef = "feat-1"
	s.forge.pulls[13] = p
	if r := s.plan(); r.Landable || !has(r, ReasonBaseNotInChain, 13) {
		t.Fatalf("skip base: %v", reasonCodes(r))
	}
	// the bottom must target the default branch
	s = nativeHappy()
	p = s.forge.pulls[11]
	p.BaseRef = "feat-2"
	s.forge.pulls[11] = p
	if r := s.plan(); r.Landable || !has(r, ReasonBaseNotInChain, 11) {
		t.Fatalf("bottom base: %v", reasonCodes(r))
	}
	// an unrelated base
	s = nativeHappy()
	p = s.forge.pulls[12]
	p.BaseRef = "elsewhere"
	s.forge.pulls[12] = p
	if r := s.plan(); r.Landable || !has(r, ReasonBaseNotInChain, 12) {
		t.Fatalf("unrelated base: %v", reasonCodes(r))
	}
}

func TestNativeEntryAlsoAdmitsTheFlatChainButAFlatEntryNotTheNativeOne(t *testing.T) {
	s := happy()
	nativeMeasured(s)
	if r := s.plan(); !r.Landable {
		t.Fatalf("flat under a native entry: %v", reasonCodes(r))
	}
	s = newScenario()
	native(s)
	measured(s)
	if r := s.plan(); r.Landable {
		t.Fatal("a flat entry must not admit the native chain")
	}
}

func TestNativeChainStillNeedsEveryOtherPrecondition(t *testing.T) {
	s := nativeHappy()
	s.forge.checks[sha('3')] = CheckFacts{Contexts: map[string]CheckState{"ci/all": CheckFailure}}
	if r := s.plan(); r.Landable || !has(r, ReasonChecksNotGreen, 13) {
		t.Fatalf("%v", reasonCodes(r))
	}
}

func TestClosedNativeChildNamesTheDeletedBase(t *testing.T) {
	s := nativeHappy()
	p := s.forge.pulls[13]
	p.State = "closed"
	s.forge.pulls[13] = p
	r := s.plan()
	if !has(r, ReasonNotOpen, 13) {
		t.Fatalf("%v", reasonCodes(r))
	}
	for _, x := range r.Reasons {
		if x.Code == ReasonNotOpen && x.Number == 13 && !strings.Contains(x.Detail, "base branch") {
			t.Fatalf("detail %q", x.Detail)
		}
	}
}

// retargeted simulates GitHub after the three `gh pr edit` calls: every base is the default branch.
func retargeted(s *scenario) {
	for n, p := range s.forge.pulls {
		p.BaseRef = "main"
		s.forge.pulls[n] = p
	}
}

func TestReverifyAfterRetarget(t *testing.T) {
	cases := []struct {
		name string
		edit func(*scenario)
		want []string
	}{
		{"success", func(s *scenario) {}, nil},
		{"base not retargeted", func(s *scenario) {
			p := s.forge.pulls[13]
			p.BaseRef = "feat-2"
			s.forge.pulls[13] = p
		}, []string{ReasonRetargetNotApplied + "#13"}},
		{"head moved", func(s *scenario) {
			s.forge.pulls[12] = pull(12, sha('9'), "feat-2")
		}, []string{ReasonHeadChanged + "#12"}},
		{"child closed", func(s *scenario) {
			p := s.forge.pulls[13]
			p.State = "closed"
			s.forge.pulls[13] = p
		}, []string{ReasonChildClosed + "#13"}},
		{"tip check started again and pending", func(s *scenario) {
			s.forge.checks[sha('3')] = CheckFacts{Contexts: map[string]CheckState{"ci/all": CheckPending}}
		}, []string{ReasonTipChecksPending + "#13"}},
		{"tip checks gone", func(s *scenario) {
			s.forge.checks[sha('3')] = CheckFacts{}
		}, []string{ReasonChecksMissing + "#13"}},
		{"default branch moved", func(s *scenario) {
			s.git.tip = sha('c')
			s.git.lin = []string{sha('0'), sha('1'), sha('2'), sha('3'), sha('c')}
			s.git.diverged = map[[2]string]bool{{sha('c'), sha('1')}: true, {sha('3'), sha('c')}: true}
		}, []string{ReasonBaseMoved + "#0", ReasonNotFastForward + "#11"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := nativeHappy().plan()
			if !before.Landable {
				t.Fatalf("before: %v", reasonCodes(before))
			}
			s := nativeHappy()
			retargeted(s)
			c.edit(s)
			got := Reverify(before, s.plan())
			var codes []string
			for _, x := range got {
				codes = append(codes, x.Code+"#"+strconv.Itoa(x.Number))
			}
			for _, w := range c.want {
				found := false
				for _, g := range codes {
					found = found || g == w
				}
				if !found {
					t.Errorf("missing %s in %v", w, codes)
				}
			}
			if len(c.want) == 0 && len(codes) != 0 {
				t.Errorf("unexpected %v", codes)
			}
		})
	}
}

func TestReverifyRefusesAPlanThatWasNeverLandable(t *testing.T) {
	s := newScenario()
	native(s)
	measured(s)
	before := s.plan() // refused: land-unmeasured
	s2 := nativeHappy()
	retargeted(s2)
	if got := Reverify(before, s2.plan()); len(got) == 0 {
		t.Fatal("a refused plan must not be verified")
	}
}

func TestRestoreArgvNamesTheOldBaseOfEachNativeItem(t *testing.T) {
	before := nativeHappy().plan()
	got := RestoreArgv(before)
	var lines []string
	for _, a := range got {
		lines = append(lines, strings.Join(a, " "))
	}
	if strings.Join(lines, "|") != "gh pr edit 12 --base feat-1|gh pr edit 13 --base feat-2" {
		t.Fatalf("%v", lines)
	}
	if n := len(RestoreArgv(happy().plan())); n != 0 {
		t.Fatalf("flat chain has %d restore commands", n)
	}
}

func TestShapeSetNativeFlagInTheAllowlistFile(t *testing.T) {
	key := "stack=true;delete_branch_on_merge=unknown;protection=none;rules=required_linear_history;bypass="
	set, err := LoadShapes([]byte(`{"schema_version":1,"shapes":[{"key":"` + key + `","stack":true,"native":true,"evidence":"research.md#x"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	shape := Shape{Stack: true, DeleteBranchOnMerge: "unknown", Protection: "none", RuleTypes: []string{"required_linear_history"}, BypassModes: []string{}, Readable: true}
	if shape.Key() != key {
		t.Fatalf("key %q", shape.Key())
	}
	if !set.Has(shape) || !set.HasNative(shape) {
		t.Fatal("native entry must admit both")
	}
	flat, _ := LoadShapes([]byte(`{"schema_version":1,"shapes":[{"key":"` + key + `","stack":true,"evidence":"x"}]}`))
	if !flat.Has(shape) || flat.HasNative(shape) {
		t.Fatal("a flat entry must not be native")
	}
	if _, err := LoadShapes([]byte(`{"schema_version":1,"shapes":[{"key":"stack=false;x","stack":false,"native":true,"evidence":"x"}]}`)); err == nil {
		t.Fatal("native without stack must be rejected")
	}
	unreadable := shape
	unreadable.Readable = false
	if set.HasNative(unreadable) {
		t.Fatal("an unreadable shape never matches")
	}
}
