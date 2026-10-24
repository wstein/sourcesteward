// SPDX-License-Identifier: EUPL-1.2

package planset

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/derive"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
)

var (
	human = owner.Evidence{Mode: owner.ModeHuman, StdinTTY: true, StdoutTTY: true, Markers: []string{}, ByOwner: []string{}, Caller: "claude", CallerSource: "env"}
	agent = owner.Evidence{Mode: owner.ModeAgent, Markers: []string{"CLAUDECODE"}, ByOwner: []string{}, Caller: "claude", CallerSource: "env"}
)

func cand(kind tidywizard.Kind, repo, subject string, v tidywizard.Verdict, o string, src owner.Source, sc owner.Scope) tidywizard.Candidate {
	c := tidywizard.Candidate{ID: string(kind) + ":" + repo + ":" + subject, Repo: repo, Kind: kind, Verdict: v, Owner: o, OwnerSource: src, Scope: sc, Effects: []string{}}
	if v != tidywizard.VerdictRetain {
		c.Effects = []string{"remove"}
	}
	switch kind {
	case tidywizard.KindWorktree, tidywizard.KindStaleWorktree:
		c.Path = "/w/" + subject
	default:
		c.Branch = subject
	}
	return c
}

func own(kind tidywizard.Kind, repo, subject string) tidywizard.Candidate {
	return cand(kind, repo, subject, tidywizard.VerdictSafe, "claude", owner.SourceTaskRecord, owner.ScopeOwn)
}

func foreign(kind tidywizard.Kind, repo, subject string) tidywizard.Candidate {
	return cand(kind, repo, subject, tidywizard.VerdictSafe, "codex", owner.SourcePathConvention, owner.ScopeOther)
}

type fakePlanner struct {
	specs []SubSpec
	err   error
	id    func(SubSpec) string
}

func (p *fakePlanner) Plan(_ context.Context, s SubSpec) (string, error) {
	p.specs = append(p.specs, s)
	if p.err != nil {
		return "", p.err
	}
	if p.id != nil {
		return p.id(s), nil
	}
	h := sha256.Sum256([]byte(fmt.Sprint(s.Kind, s.Repo, s.Worktree, s.Items)))
	return "sub" + hex.EncodeToString(h[:8]), nil
}

type fakeTTY struct {
	in  *strings.Reader
	out strings.Builder
}

func (f *fakeTTY) Read(p []byte) (int, error)  { return f.in.Read(p) }
func (f *fakeTTY) Write(p []byte) (int, error) { return f.out.Write(p) }
func (f *fakeTTY) Close() error                { return nil }

func ttyWith(input string) owner.Opener {
	f := &fakeTTY{in: strings.NewReader(input)}
	return func() (io.ReadWriteCloser, error) { return f, nil }
}

func noTTY() (io.ReadWriteCloser, error) { return nil, owner.ErrNoTerminal }

func build(t *testing.T, cands []tidywizard.Candidate, chosen []string, ev owner.Evidence, p Planner) (Set, error) {
	t.Helper()
	return Build(context.Background(), Request{Candidates: cands, Chosen: chosen, Derived: []derive.Derived{{Name: "remote:r", Value: "origin", Source: "origin"}}, Mode: ev}, p)
}

func ids(cs ...tidywizard.Candidate) []string {
	out := []string{}
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return out
}

func TestSelectSafeHumanTakesOwnSafeItemsOnly(t *testing.T) {
	a := own(tidywizard.KindWorktree, "r", "a")
	f := foreign(tidywizard.KindWorktree, "r", "f")
	unk := cand(tidywizard.KindWorktree, "r", "u", tidywizard.VerdictSafe, "", owner.SourceUnknown, owner.ScopeUnknown)
	ret := cand(tidywizard.KindWorktree, "r", "t", tidywizard.VerdictRetain, "claude", owner.SourceTaskRecord, owner.ScopeOwn)
	dec := cand(tidywizard.KindWorktree, "r", "d", tidywizard.VerdictDecision, "claude", owner.SourceTaskRecord, owner.ScopeOwn)
	guess := cand(tidywizard.KindBranch, "r", "claude/x", tidywizard.VerdictSafe, "claude", owner.SourcePathConvention, owner.ScopeOwn)
	got := SelectSafe([]tidywizard.Candidate{f, unk, ret, dec, guess, a}, human)
	want := []string{a.ID, guess.ID}
	if strings.Join(got, ",") != strings.Join(sorted(want), ",") {
		t.Fatalf("human --save selects %v, want %v (never other, unknown, retain or decision)", got, want)
	}
}

func sorted(in []string) []string {
	out := append([]string{}, in...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func TestSelectSafeAgentNeedsTheTaskRecord(t *testing.T) {
	a := own(tidywizard.KindWorktree, "r", "a")
	guess := cand(tidywizard.KindBranch, "r", "claude/x", tidywizard.VerdictSafe, "claude", owner.SourcePathConvention, owner.ScopeOwn)
	f := foreign(tidywizard.KindWorktree, "r", "f")
	unk := cand(tidywizard.KindWorktree, "r", "u", tidywizard.VerdictSafe, "", owner.SourceUnknown, owner.ScopeUnknown)
	got := SelectSafe([]tidywizard.Candidate{guess, f, unk, a}, agent)
	if len(got) != 1 || got[0] != a.ID {
		t.Fatalf("agent --save selects %v, want only the task-record item %s", got, a.ID)
	}
}

func TestSelectSafeIsPermutationInvariant(t *testing.T) {
	x, y, z := own(tidywizard.KindWorktree, "r", "x"), own(tidywizard.KindBranch, "r", "y"), own(tidywizard.KindWorktree, "q", "z")
	a := SelectSafe([]tidywizard.Candidate{x, y, z}, human)
	b := SelectSafe([]tidywizard.Candidate{z, y, x}, human)
	if strings.Join(a, ",") != strings.Join(b, ",") || len(a) != 3 {
		t.Fatalf("%v vs %v", a, b)
	}
}

func TestBuildGroupsSubPlansByEffectKind(t *testing.T) {
	w1, w2 := own(tidywizard.KindWorktree, "r", "w1"), own(tidywizard.KindWorktree, "r", "w2")
	b1, b2 := own(tidywizard.KindBranch, "r", "b1"), own(tidywizard.KindBranch, "r", "b2")
	b3 := own(tidywizard.KindBranch, "q", "b3")
	tr := own(tidywizard.KindTrackingRef, "r", "origin/old")
	st := own(tidywizard.KindStaleWorktree, "r", "gone")
	all := []tidywizard.Candidate{w1, w2, b1, b2, b3, tr, st}
	p := &fakePlanner{}
	set, err := build(t, all, ids(all...), human, p)
	if err != nil {
		t.Fatal(err)
	}
	var retire, branches, tracking, stale, qBranches int
	for _, s := range set.SubPlans {
		switch {
		case s.Kind == KindRetire:
			retire++
			if len(s.Items) != 1 || s.Worktree == "" {
				t.Errorf("a retirement sub-plan covers exactly one worktree: %+v", s)
			}
		case s.Kind == KindBranches && s.Repo == "r":
			branches++
			if len(s.Items) != 2 {
				t.Errorf("branches of one repository share a sub-plan: %+v", s)
			}
		case s.Kind == KindBranches && s.Repo == "q":
			qBranches++
		case s.Kind == KindTracking:
			tracking++
		case s.Kind == KindStaleWorktrees:
			stale++
		}
	}
	if retire != 2 || branches != 1 || qBranches != 1 || tracking != 1 || stale != 1 || len(set.SubPlans) != 6 {
		t.Fatalf("retire %d branches %d q %d tracking %d stale %d total %d", retire, branches, qBranches, tracking, stale, len(set.SubPlans))
	}
	if len(p.specs) != 6 {
		t.Fatalf("the planner is asked once per sub-plan, got %d", len(p.specs))
	}
}

func TestOrderRunsRetirementsThenStaleThenBranchesThenTracking(t *testing.T) {
	all := []tidywizard.Candidate{
		own(tidywizard.KindTrackingRef, "a", "origin/old"), own(tidywizard.KindBranch, "a", "b"),
		own(tidywizard.KindStaleWorktree, "a", "gone"), own(tidywizard.KindWorktree, "b", "w2"), own(tidywizard.KindWorktree, "a", "w1"),
	}
	set, err := build(t, all, ids(all...), human, &fakePlanner{})
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	var repos []string
	for _, s := range set.Ordered() {
		kinds = append(kinds, string(s.Kind))
		repos = append(repos, s.Repo)
	}
	if strings.Join(kinds, ",") != "retire,retire,stale-worktrees,branches,tracking" || strings.Join(repos, ",") != "a,b,a,a,a" {
		t.Fatalf("order %v %v", kinds, repos)
	}
}

func TestPlanIDIsStableAndBindsEveryPart(t *testing.T) {
	w1, w2 := own(tidywizard.KindWorktree, "r", "w1"), own(tidywizard.KindWorktree, "r", "w2")
	base, err := build(t, []tidywizard.Candidate{w1, w2}, ids(w1, w2), human, &fakePlanner{})
	if err != nil {
		t.Fatal(err)
	}
	again, _ := build(t, []tidywizard.Candidate{w2, w1}, ids(w2, w1), human, &fakePlanner{})
	if base.PlanID == "" || len(base.PlanID) != 12 || base.PlanID != again.PlanID {
		t.Fatalf("ids %q %q must be equal under permutation", base.PlanID, again.PlanID)
	}
	fewer, _ := build(t, []tidywizard.Candidate{w1, w2}, ids(w1), human, &fakePlanner{})
	if fewer.PlanID == base.PlanID {
		t.Error("the selection is part of the id")
	}
	other := human
	other.Caller = "someone"
	diffMode, _ := build(t, []tidywizard.Candidate{w1, w2}, ids(w1, w2), other, &fakePlanner{})
	if diffMode.PlanID == base.PlanID {
		t.Error("the mode evidence is part of the id")
	}
	diffDerived, _ := Build(context.Background(), Request{Candidates: []tidywizard.Candidate{w1, w2}, Chosen: ids(w1, w2), Mode: human}, &fakePlanner{})
	if diffDerived.PlanID == base.PlanID {
		t.Error("the derived values are part of the id")
	}
	// Edits of the stored manifest that the id must catch.
	edits := map[string]func(*Set){
		"worktree of a sub-plan": func(s *Set) { s.SubPlans[0].Worktree = "/w/elsewhere" },
		"sub-plan kind":          func(s *Set) { s.SubPlans[0].Kind = KindBranches },
		"sub-plan repository":    func(s *Set) { s.SubPlans[0].Repo = "other" },
		"item scope":             func(s *Set) { s.Items[0].Scope = owner.ScopeOther },
		"item owner":             func(s *Set) { s.Items[0].Owner = "codex" },
		"item owner source":      func(s *Set) { s.Items[0].OwnerSource = owner.SourcePathConvention },
		"sub-plan id":            func(s *Set) { s.SubPlans[0].ID = "subtampered" },
	}
	for name, edit := range edits {
		c := base.clone()
		edit(&c)
		if c.computeID() == base.PlanID {
			t.Errorf("editing the %s must change the plan id", name)
		}
	}
}

func TestAuthorizedScopesAreInsideThePlanID(t *testing.T) {
	a := own(tidywizard.KindWorktree, "r", "a")
	set, err := build(t, []tidywizard.Candidate{a}, ids(a), human, &fakePlanner{})
	if err != nil {
		t.Fatal(err)
	}
	c := set.clone()
	c.AuthorizedScopes = []owner.Scope{owner.ScopeOther}
	if c.computeID() == set.PlanID {
		t.Fatal("editing authorized_scopes must change the plan id")
	}
}

func TestBuildForeignItemsNeedTheTypedConfirmationAndRecordScopes(t *testing.T) {
	a, f := own(tidywizard.KindWorktree, "r", "a"), foreign(tidywizard.KindWorktree, "r", "f")
	chosen := ids(a, f)
	var set Set
	var err error
	// The confirmation is "<label> <first 6 of the plan id>".
	prefix := planIDFor(t, []tidywizard.Candidate{a, f}, chosen, human)
	set, err = Build(context.Background(), Request{Candidates: []tidywizard.Candidate{a, f}, Chosen: chosen, Mode: human, Confirm: ttyWith("codex " + prefix + "\n")}, &fakePlanner{})
	if err != nil {
		t.Fatalf("typed confirmation: %v", err)
	}
	if len(set.AuthorizedScopes) != 1 || set.AuthorizedScopes[0] != owner.ScopeOther {
		t.Fatalf("authorized_scopes %v", set.AuthorizedScopes)
	}
	var marked bool
	for _, it := range set.Items {
		if it.ID == f.ID && it.Scope == owner.ScopeOther && it.Owner == "codex" {
			marked = true
		}
	}
	if !marked {
		t.Errorf("the foreign item carries its owner and scope in the stored plan: %+v", set.Items)
	}
	if !set.NeedsAuthorization() {
		t.Error("a set with a foreign item needs authorization")
	}
	if own1, _ := build(t, []tidywizard.Candidate{a}, ids(a), human, &fakePlanner{}); own1.NeedsAuthorization() || len(own1.AuthorizedScopes) != 0 {
		t.Error("an own-only set carries no authorized_scopes")
	}
}

// planIDFor returns the 6-character prefix a confirmation of the set must type.
func planIDFor(t *testing.T, cands []tidywizard.Candidate, chosen []string, ev owner.Evidence) string {
	t.Helper()
	s, err := assemble(context.Background(), Request{Candidates: cands, Chosen: chosen, Mode: ev}, &fakePlanner{})
	if err != nil {
		t.Fatal(err)
	}
	return s.PlanID[:6]
}

func TestBuildConfirmationRefusals(t *testing.T) {
	a, f := own(tidywizard.KindWorktree, "r", "a"), foreign(tidywizard.KindWorktree, "r", "f")
	cands, chosen := []tidywizard.Candidate{a, f}, ids(a, f)
	prefix := planIDFor(t, cands, chosen, human)
	for name, open := range map[string]owner.Opener{
		"bare y":             ttyWith("y\n"),
		"wrong word":         ttyWith("claude " + prefix + "\n"),
		"wrong prefix":       ttyWith("codex 000000\n"),
		"no controlling tty": noTTY,
		"nil opener":         nil,
	} {
		p := &fakePlanner{}
		_, err := Build(context.Background(), Request{Candidates: cands, Chosen: chosen, Mode: human, Confirm: open}, p)
		if err == nil {
			t.Errorf("%s: a foreign item without the typed confirmation must refuse", name)
		}
	}
	// Without a terminal nothing is planned (so nothing is stored by the planner).
	p := &fakePlanner{}
	_, _ = Build(context.Background(), Request{Candidates: cands, Chosen: chosen, Mode: human, Confirm: noTTY}, p)
	if len(p.specs) != 0 {
		t.Errorf("no terminal: the planner ran %d times, want 0", len(p.specs))
	}
}

func TestBuildUnknownScopeUsesTheUnknownWord(t *testing.T) {
	u := cand(tidywizard.KindWorktree, "r", "u", tidywizard.VerdictSafe, "", owner.SourceUnknown, owner.ScopeUnknown)
	prefix := planIDFor(t, []tidywizard.Candidate{u}, ids(u), human)
	set, err := Build(context.Background(), Request{Candidates: []tidywizard.Candidate{u}, Chosen: ids(u), Mode: human, Confirm: ttyWith("unknown " + prefix + "\n")}, &fakePlanner{})
	if err != nil {
		t.Fatal(err)
	}
	if len(set.AuthorizedScopes) != 1 || set.AuthorizedScopes[0] != owner.ScopeUnknown {
		t.Fatalf("scopes %v", set.AuthorizedScopes)
	}
}

func TestBuildAgentModeNeverPlansForeignOrGuessedItems(t *testing.T) {
	a := own(tidywizard.KindWorktree, "r", "a")
	f := foreign(tidywizard.KindWorktree, "r", "f")
	guess := cand(tidywizard.KindBranch, "r", "claude/x", tidywizard.VerdictSafe, "claude", owner.SourcePathConvention, owner.ScopeOwn)
	for name, c := range map[string]tidywizard.Candidate{"other": f, "guessed own": guess} {
		p := &fakePlanner{}
		_, err := build(t, []tidywizard.Candidate{a, f, guess}, ids(a, c), agent, p)
		if !errors.Is(err, ErrNotPlannable) {
			t.Errorf("%s: err %v, want ErrNotPlannable", name, err)
		}
		if len(p.specs) != 0 {
			t.Errorf("%s: nothing may be planned", name)
		}
	}
	set, err := build(t, []tidywizard.Candidate{a, f}, ids(a), agent, &fakePlanner{})
	if err != nil || len(set.AuthorizedScopes) != 0 {
		t.Fatalf("agent plan of own items: %v %v", set.AuthorizedScopes, err)
	}
}

func TestBuildRefusesRowsThatAreNotChoosable(t *testing.T) {
	ret := cand(tidywizard.KindWorktree, "r", "t", tidywizard.VerdictRetain, "claude", owner.SourceTaskRecord, owner.ScopeOwn)
	dec := cand(tidywizard.KindWorktree, "r", "d", tidywizard.VerdictDecision, "claude", owner.SourceTaskRecord, owner.ScopeOwn)
	for name, tc := range map[string]struct {
		chosen []string
		decide map[string]bool
	}{
		"retain":               {ids(ret), nil},
		"decision unconfirmed": {ids(dec), nil},
		"decision other id":    {ids(dec), map[string]bool{"x": true}},
		"unknown id":           {[]string{"worktree:r:nope"}, nil},
		"nothing chosen":       {nil, nil},
	} {
		p := &fakePlanner{}
		_, err := Build(context.Background(), Request{Candidates: []tidywizard.Candidate{ret, dec}, Chosen: tc.chosen, Mode: human, DecisionConfirmed: tc.decide}, p)
		if err == nil {
			t.Errorf("%s must be refused", name)
		}
		if len(p.specs) != 0 {
			t.Errorf("%s: nothing may be planned", name)
		}
	}
	set, err := Build(context.Background(), Request{Candidates: []tidywizard.Candidate{ret, dec}, Chosen: ids(dec), Mode: human, DecisionConfirmed: map[string]bool{dec.ID: true}}, &fakePlanner{})
	if err != nil || len(set.Items) != 1 {
		t.Fatalf("a confirmed decision row is choosable: %v %v", set.Items, err)
	}
}

func TestBuildPlannerFailures(t *testing.T) {
	a := own(tidywizard.KindWorktree, "r", "a")
	if _, err := build(t, []tidywizard.Candidate{a}, ids(a), human, &fakePlanner{err: errors.New("boom")}); err == nil {
		t.Error("a planner error must fail the build")
	}
	if _, err := build(t, []tidywizard.Candidate{a}, ids(a), human, &fakePlanner{id: func(SubSpec) string { return "" }}); err == nil {
		t.Error("an empty sub-plan id must fail the build")
	}
	if _, err := build(t, []tidywizard.Candidate{a}, ids(a), human, &fakePlanner{id: func(SubSpec) string { return "../x" }}); err == nil {
		t.Error("a sub-plan id that is not a plain identifier must fail the build")
	}
}

func TestValidateRefusesIncoherentSets(t *testing.T) {
	w1, w2 := own(tidywizard.KindWorktree, "r", "w1"), own(tidywizard.KindWorktree, "r", "w2")
	good, err := build(t, []tidywizard.Candidate{w1, w2}, ids(w1, w2), human, &fakePlanner{})
	if err != nil {
		t.Fatal(err)
	}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*Set){
		"two worktrees in one retirement": func(s *Set) {
			s.SubPlans[0].Items = append(s.SubPlans[0].Items, s.SubPlans[1].Items...)
			s.SubPlans = s.SubPlans[:1]
		},
		"one worktree in two retirements": func(s *Set) { s.SubPlans[1].Worktree = s.SubPlans[0].Worktree },
		"retirement without a worktree":   func(s *Set) { s.SubPlans[0].Worktree = "" },
		"duplicate sub-plan id":           func(s *Set) { s.SubPlans[1].ID = s.SubPlans[0].ID },
		"item without a sub-plan":         func(s *Set) { s.SubPlans = s.SubPlans[:1] },
		"sub-plan item not selected":      func(s *Set) { s.Items = s.Items[:1] },
		"unknown kind":                    func(s *Set) { s.SubPlans[0].Kind = "format-disk" },
		"empty set":                       func(s *Set) { s.SubPlans, s.Items = nil, nil },
		"agent mode with scopes":          func(s *Set) { s.Mode.Mode = owner.ModeAgent; s.AuthorizedScopes = []owner.Scope{owner.ScopeOther} },
		"own scope authorized":            func(s *Set) { s.AuthorizedScopes = []owner.Scope{owner.ScopeOwn} },
		"bad sub-plan id":                 func(s *Set) { s.SubPlans[0].ID = "a/b" },
	}
	for name, mutate := range cases {
		c := good.clone()
		mutate(&c)
		if err := c.Validate(); err == nil {
			t.Errorf("%s must be invalid", name)
		}
	}
}

func TestStoreAndLoadRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "s")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	a := own(tidywizard.KindWorktree, "r", "a")
	set, err := build(t, []tidywizard.Candidate{a}, ids(a), human, &fakePlanner{})
	if err != nil {
		t.Fatal(err)
	}
	if err := Store(dir, set); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir, set.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if got.PlanID != set.PlanID || len(got.SubPlans) != 1 || got.Mode.Caller != "claude" || got.Items[0].ID != a.ID {
		t.Fatalf("round trip: %+v", got)
	}
	if _, err := Load(dir, "000000000000"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("absent set: %v", err)
	}
}

func TestLoadRefusesAnEditedStore(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "s")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	a, f := own(tidywizard.KindWorktree, "r", "a"), foreign(tidywizard.KindWorktree, "r", "f")
	prefix := planIDFor(t, []tidywizard.Candidate{a, f}, ids(a, f), human)
	set, err := Build(context.Background(), Request{Candidates: []tidywizard.Candidate{a, f}, Chosen: ids(a, f), Mode: human, Confirm: ttyWith("codex " + prefix + "\n")}, &fakePlanner{})
	if err != nil {
		t.Fatal(err)
	}
	if err := Store(dir, set); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "tidy-plan-sets", set.PlanID+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	edits := map[string]string{
		"authorized_scopes removed": strings.Replace(string(raw), `"authorized_scopes":["other"]`, `"authorized_scopes":[]`, 1),
		"scope relabelled":          strings.Replace(string(raw), `"scope":"other"`, `"scope":"own"`, 1),
		"unknown field":             strings.Replace(string(raw), `{"`, `{"extra":1,"`, 1),
	}
	for name, edited := range edits {
		if edited == string(raw) {
			t.Fatalf("%s: the edit did not apply to %s", name, raw)
		}
		if err := os.WriteFile(path, []byte(edited), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(dir, set.PlanID); err == nil {
			t.Errorf("%s: an edited store entry must be refused", name)
		}
	}
	// A record stored under the wrong name is refused as well.
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir, set.PlanID); err != nil {
		t.Fatalf("the untouched record loads: %v", err)
	}
	moved := filepath.Join(dir, "tidy-plan-sets", "aaaaaaaaaaaa.json")
	if err := os.WriteFile(moved, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir, "aaaaaaaaaaaa"); !errors.Is(err, ErrIDMismatch) {
		t.Fatalf("a record under another id: %v", err)
	}
}

func TestStoreRefusesAnInvalidOrForgedSet(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "s")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	a := own(tidywizard.KindWorktree, "r", "a")
	set, _ := build(t, []tidywizard.Candidate{a}, ids(a), human, &fakePlanner{})
	forged := set.clone()
	forged.AuthorizedScopes = []owner.Scope{owner.ScopeOther}
	if err := Store(dir, forged); !errors.Is(err, ErrIDMismatch) {
		t.Errorf("a set whose id does not match its content must not be stored: %v", err)
	}
	empty := Set{}
	if err := Store(dir, empty); err == nil {
		t.Error("an empty set must not be stored")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("nothing stored: %v", entries)
	}
}
