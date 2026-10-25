// SPDX-License-Identifier: EUPL-1.2

package stackpublish

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestEmptySelectionBindsOneRepositoryAndIsUnsaved(t *testing.T) {
	p := mustPlan(t, fixtureInput())
	if p.Saved() || p.PlanID() != nil || len(p.Actions()) != 0 || p.Repository() == nil {
		t.Fatalf("saved=%v id=%v actions=%d repository=%v", p.Saved(), p.PlanID(), len(p.Actions()), p.Repository())
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["schema_version"] != float64(1) || doc["kind"] != "forge-effect-plan" || doc["operation"] != "plan" || doc["plan_id"] != nil || doc["saved"] != false || doc["error"] != nil {
		t.Fatalf("pinned header or explicit nulls wrong: %s", raw)
	}
	if actions, ok := doc["actions"].([]any); !ok || len(actions) != 0 {
		t.Fatalf("empty selection must render an empty array, not null: %s", raw)
	}
	if repo, ok := doc["repository"].(map[string]any); !ok || repo["default_commit_id"] != sha('1') {
		t.Fatalf("an empty plan still binds its one repository: %s", raw)
	}
}

func TestCompletePlanWithoutRepositoryIsInvalid(t *testing.T) {
	in := fixtureInput()
	in.Repository = nil
	_, err := NewPlan(in)
	wantCode(t, err, "repository-missing", false)
	// An incomplete result that carries its error may have no resolved repository.
	in.Boundary.Complete = false
	in.Error = &Failure{Code: ReasonOwnershipUnknown, Message: "no repository"}
	if _, err := NewPlan(in); err != nil {
		t.Fatalf("an incomplete error result without a repository is allowed: %v", err)
	}
}

func TestRepositoryIdentityGrammar(t *testing.T) {
	for name, mutate := range map[string]func(*Repository){
		"short sha":            func(r *Repository) { r.DefaultCommitID = "1111111" },
		"sha256 object format": func(r *Repository) { r.DefaultCommitID = strings.Repeat("1", 64) },
		"uppercase sha":        func(r *Repository) { r.DefaultCommitID = strings.Repeat("A", 40) },
		"empty owner":          func(r *Repository) { r.Owner = "" },
		"unqualified default":  func(r *Repository) { r.DefaultRef = "main" },
		"url as name":          func(r *Repository) { r.Name = "https://github.com/o/p" },
		"unsupported forge":    func(r *Repository) { r.Forge = "gitlab" },
		"unknown backend":      func(r *Repository) { r.Backend = "hg" },
	} {
		in := fixtureInput()
		repo := *in.Repository
		mutate(&repo)
		in.Repository = &repo
		_, err := NewPlan(in)
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	in := fixtureInput()
	repo := *in.Repository
	repo.DefaultCommitID = strings.Repeat("1", 64)
	in.Repository = &repo
	_, err := NewPlan(in)
	wantCode(t, err, "unsupported-object-format", false)
}

func TestSixteenActionsAreAllowedAndSeventeenAreNot(t *testing.T) {
	var actions []Action
	for i := 0; i < 16; i++ {
		actions = append(actions, pushAction(fmt.Sprintf("a%02d", i), fmt.Sprintf("refs/heads/topic-%d", i)))
	}
	mustPlan(t, fixtureInput(actions...))
	actions = append(actions, pushAction("a16", "refs/heads/topic-16"))
	_, err := NewPlan(fixtureInput(actions...))
	wantCode(t, err, "too-many-actions", false)
}

func TestChangedEffectsGiveANewIDAndTheSameInputTheSameID(t *testing.T) {
	base := mustPlan(t, fixtureInput(pushAction("p1", "refs/heads/topic"))).ID()
	if len(base) != 64 || strings.Trim(base, "0123456789abcdef") != "" {
		t.Fatalf("plan id %q is not a lowercase sha256", base)
	}
	if again := mustPlan(t, fixtureInput(pushAction("p1", "refs/heads/topic"))).ID(); again != base {
		t.Fatalf("the id is not a pure function of the content: %s vs %s", again, base)
	}
	for name, mutate := range map[string]func(*PlanInput){
		"target":       func(in *PlanInput) { in.Actions[0].Target.Ref = str("refs/heads/other") },
		"effect":       func(in *PlanInput) { in.Actions[0].Effects = []string{"push something else"} },
		"expected sha": func(in *PlanInput) { in.Actions[0].Expected.NewSHA = str(sha('d')) },
		"state": func(in *PlanInput) {
			in.Actions[0].State = StateBlocked
			in.Actions[0].Blocker = reasonPtr(ReasonCapabilityUnavailable)
		},
		"action id":     func(in *PlanInput) { in.Actions[0].ID = "p2"; in.Options.Selected = []string{"p2"} },
		"task":          func(in *PlanInput) { in.Task.TaskID = "other" },
		"repository":    func(in *PlanInput) { r := *in.Repository; r.Name = "other"; in.Repository = &r },
		"online":        func(in *PlanInput) { in.Options.Online = true },
		"created at":    func(in *PlanInput) { in.CreatedAt = in.CreatedAt.Add(time.Second) },
		"limits":        func(in *PlanInput) { in.Limits.OperationSeconds = 30 },
		"blocker added": func(in *PlanInput) { in.Blockers = []Blocker{{Reason: ReasonStackTooDeep}} },
	} {
		in := fixtureInput(pushAction("p1", "refs/heads/topic"))
		mutate(&in)
		p, err := NewPlan(in)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if p.ID() == base {
			t.Errorf("%s: a changed plan kept its id", name)
		}
	}
}

func reasonPtr(r Reason) *Reason { return &r }

func TestActionsCarryNoAuthorizationAndTheIDIgnoresIt(t *testing.T) {
	raw, err := json.Marshal(mustPlan(t, fixtureInput(pushAction("p1", "refs/heads/topic"))))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(raw)), "authoriz") {
		t.Fatalf("a plan must not persist any authorization: %s", raw)
	}
}

func TestPlanIsImmutableAfterConstruction(t *testing.T) {
	in := fixtureInput(pushAction("p1", "refs/heads/topic"))
	p := mustPlan(t, in)
	id := p.ID()
	in.Actions[0].Effects[0] = "mutated"
	*in.Actions[0].Target.Ref = "refs/heads/mutated"
	in.Options.Selected[0] = "mutated"
	in.Repository.Name = "mutated"
	got := p.Actions()
	got[0].Effects[0] = "mutated again"
	*got[0].Expected.NewSHA = sha('f')
	got[0].State = StateCompleted
	repo := p.Repository()
	repo.Owner = "mutated"
	opts := p.Options()
	opts.Selected[0] = "mutated"
	if p.ID() != id || p.Actions()[0].Effects[0] != "push refs/heads/topic" || p.Repository().Owner != "owner" || p.Options().Selected[0] != "p1" {
		t.Fatalf("the plan changed after construction: %s", mustJSON(t, p))
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestActionKindsAreClosedAndCommentCannotExecute(t *testing.T) {
	in := fixtureInput(pushAction("p1", "refs/heads/topic"))
	in.Actions[0].Kind = KindCommentDeferred
	_, err := NewPlan(in)
	wantCode(t, err, "comment-deferred", false)
	in.Actions[0].Kind = "merge"
	_, err = NewPlan(in)
	wantCode(t, err, "unknown-kind", false)
	// Forbidden kinds of the contract are not kinds at all.
	for _, kind := range []ActionKind{"land", "delete-branch", "push-default", "tag", "auto-merge", "release", "sync"} {
		in.Actions[0].Kind = kind
		if _, err := NewPlan(in); err == nil {
			t.Errorf("%s accepted", kind)
		}
	}
}

func TestNoKindIsPlannedWithoutAdmittedCapability(t *testing.T) {
	for _, kind := range []ActionKind{KindPushTopic, KindCreatePR, KindEditBody, KindRetarget, KindReady, KindNativeLink, KindNativeUnstack} {
		a := validAction(kind)
		in := fixtureInput(a)
		in.Capabilities = CapabilitySet{}
		_, err := NewPlan(in)
		wantCode(t, err, "capability-unavailable", false)
		// The same action is a valid blocked entry with the typed blocker.
		a.State, a.Blocker = StateBlocked, reasonPtr(ReasonCapabilityUnavailable)
		in = fixtureInput(a)
		in.Capabilities = nil
		if _, err := NewPlan(in); err != nil {
			t.Errorf("%s: blocked capability-unavailable entry refused: %v", kind, err)
		}
	}
}

func validAction(kind ActionKind) Action {
	base := Action{ID: "act-" + string(kind), Kind: kind, Effects: []string{"effect"}, DependsOn: []string{}, Guards: []Guard{}, State: StatePlanned}
	switch kind {
	case KindPushTopic:
		base.Target = Target{Ref: str("refs/heads/topic")}
		base.Expected = Expected{OldSHA: str(sha('a')), NewSHA: str(sha('b'))}
	case KindCreatePR:
		base.Target = Target{Ref: str("refs/heads/topic")}
		base.Expected = Expected{PlannedBase: str("refs/heads/main")}
	case KindEditBody:
		base.Target = Target{PullRequest: num(7)}
		base.Expected = Expected{BodyDigest: str(strings.Repeat("e", 64))}
	case KindRetarget:
		base.Target = Target{PullRequest: num(7)}
		base.Expected = Expected{HeadSHA: str(sha('c')), BaseRef: str("refs/heads/parent"), PlannedBase: str("refs/heads/main")}
	case KindReady:
		base.Target = Target{PullRequest: num(7)}
		base.Expected = Expected{HeadSHA: str(sha('c'))}
	case KindNativeLink, KindNativeUnstack:
		base.Target = Target{PullRequests: []int{7, 8}, TrunkBase: str("refs/heads/main")}
	}
	return base
}

func TestEveryKindValidatesItsExactTargetAndExpectedState(t *testing.T) {
	for _, kind := range []ActionKind{KindPushTopic, KindCreatePR, KindEditBody, KindRetarget, KindReady, KindNativeLink, KindNativeUnstack} {
		mustPlan(t, fixtureInput(validAction(kind)))
	}
	mutations := map[string]func(*Action){
		"push short old sha":           func(a *Action) { a.Expected.OldSHA = str("abc123") },
		"push sha256 new":              func(a *Action) { a.Expected.NewSHA = str(strings.Repeat("b", 64)) },
		"push missing old sha":         func(a *Action) { a.Expected.OldSHA = nil },
		"push default branch is a ref": func(a *Action) { a.Target.Ref = str("main") },
		"push option-like ref":         func(a *Action) { a.Target.Ref = str("refs/heads/-D") },
		"push dotdot ref":              func(a *Action) { a.Target.Ref = str("refs/heads/a..b") },
		"push lock ref":                func(a *Action) { a.Target.Ref = str("refs/heads/topic.lock") },
		"push control ref":             func(a *Action) { a.Target.Ref = str("refs/heads/to\x01pic") },
		"push long ref":                func(a *Action) { a.Target.Ref = str("refs/heads/" + strings.Repeat("a", 200)) },
		"push tag ref":                 func(a *Action) { a.Target.Ref = str("refs/tags/v1") },
		"push hidden component":        func(a *Action) { a.Target.Ref = str("refs/heads/.hidden") },
		"push url target":              func(a *Action) { a.Target.Ref = str("https://github.com/o/p.git") },
	}
	for name, mutate := range mutations {
		a := validAction(KindPushTopic)
		mutate(&a)
		if _, err := NewPlan(fixtureInput(a)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	other := map[string]struct {
		kind   ActionKind
		mutate func(*Action)
	}{
		"create-pr without base":    {KindCreatePR, func(a *Action) { a.Expected.PlannedBase = nil }},
		"edit-body zero pr":         {KindEditBody, func(a *Action) { a.Target.PullRequest = num(0) }},
		"edit-body negative pr":     {KindEditBody, func(a *Action) { a.Target.PullRequest = num(-1) }},
		"edit-body no digest":       {KindEditBody, func(a *Action) { a.Expected.BodyDigest = nil }},
		"edit-body short digest":    {KindEditBody, func(a *Action) { a.Expected.BodyDigest = str("abc") }},
		"retarget same base":        {KindRetarget, func(a *Action) { a.Expected.PlannedBase = a.Expected.BaseRef }},
		"retarget no head":          {KindRetarget, func(a *Action) { a.Expected.HeadSHA = nil }},
		"retarget unqualified base": {KindRetarget, func(a *Action) { a.Expected.PlannedBase = str("main") }},
		"ready no head":             {KindReady, func(a *Action) { a.Expected.HeadSHA = nil }},
		"ready short head":          {KindReady, func(a *Action) { a.Expected.HeadSHA = str("deadbeef") }},
		"link one pr":               {KindNativeLink, func(a *Action) { a.Target.PullRequests = []int{7} }},
		"link duplicate pr":         {KindNativeLink, func(a *Action) { a.Target.PullRequests = []int{7, 7} }},
		"link zero pr":              {KindNativeLink, func(a *Action) { a.Target.PullRequests = []int{0, 8} }},
		"link without trunk":        {KindNativeLink, func(a *Action) { a.Target.TrunkBase = nil }},
		"unstack unqualified trunk": {KindNativeUnstack, func(a *Action) { a.Target.TrunkBase = str("main") }},
	}
	for name, tc := range other {
		a := validAction(tc.kind)
		tc.mutate(&a)
		if _, err := NewPlan(fixtureInput(a)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestActionIdsAreUniqueBoundedAndDependenciesPointBackward(t *testing.T) {
	a, b := pushAction("p1", "refs/heads/one"), pushAction("p2", "refs/heads/two")
	b.DependsOn = []string{"p1"}
	mustPlan(t, fixtureInput(a, b))
	dup := pushAction("p1", "refs/heads/two")
	_, err := NewPlan(fixtureInput(a, dup))
	wantCode(t, err, "duplicate-action", false)
	for name, deps := range map[string][]string{"forward": {"p3"}, "self": {"p2"}, "undeclared": {"zz"}} {
		c := pushAction("p2", "refs/heads/two")
		c.DependsOn = deps
		d := pushAction("p3", "refs/heads/three")
		if _, err := NewPlan(fixtureInput(a, c, d)); err == nil {
			t.Errorf("%s dependency accepted", name)
		}
	}
	for _, id := range []string{"", "A", "a b", "a/b", "-a", strings.Repeat("a", 65), "a\n"} {
		bad := pushAction(id, "refs/heads/one")
		in := fixtureInput(bad)
		in.Options.Selected = []string{id}
		if _, err := NewPlan(in); err == nil {
			t.Errorf("action id %q accepted", id)
		}
	}
}

func TestActionStateAndBlockerAreConsistent(t *testing.T) {
	a := pushAction("p1", "refs/heads/topic")
	a.State, a.Blocker = StateBlocked, nil
	if _, err := NewPlan(fixtureInput(a)); err == nil {
		t.Error("blocked without a blocker accepted")
	}
	a.State, a.Blocker = StatePlanned, reasonPtr(ReasonStackTooDeep)
	if _, err := NewPlan(fixtureInput(a)); err == nil {
		t.Error("planned with a blocker accepted")
	}
	a.State, a.Blocker = StateBlocked, reasonPtr("made-up-reason")
	if _, err := NewPlan(fixtureInput(a)); err == nil {
		t.Error("an unknown blocker reason accepted")
	}
	for _, st := range []ActionState{StatePrepared, StateCompleted, StateRefused, StateFailed, StateUnknown, StateNotStarted, "running", ""} {
		a.State, a.Blocker = st, nil
		if _, err := NewPlan(fixtureInput(a)); err == nil {
			t.Errorf("a new plan accepted action state %q: only planned or blocked exist before apply", st)
		}
	}
	// Guards are typed: pass, fail, unknown, not-applicable.
	g := pushAction("p1", "refs/heads/topic")
	g.Guards = []Guard{{Name: "owned", Result: GuardPass}, {Name: "d5", Result: GuardNotApplicable}}
	mustPlan(t, fixtureInput(g))
	g.Guards = []Guard{{Name: "owned", Result: "maybe"}}
	if _, err := NewPlan(fixtureInput(g)); err == nil {
		t.Error("an untyped guard result accepted")
	}
	g.Guards = []Guard{{Name: "owned", Result: GuardPass, Reason: ReasonNotOwned}}
	if _, err := NewPlan(fixtureInput(g)); err == nil {
		t.Error("a passing guard that names a refusal reason accepted")
	}
	g.Guards = []Guard{{Name: "owned", Result: GuardFail}}
	if _, err := NewPlan(fixtureInput(g)); err == nil {
		t.Error("a failed guard without its stable reason accepted")
	}
}

func TestSelectionIsExactAndNeverImplicit(t *testing.T) {
	cands := []Action{pushAction("p1", "refs/heads/one"), pushAction("p2", "refs/heads/two"), pushAction("p3", "refs/heads/three")}
	got, err := SelectActions(cands, nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("no selection must select nothing: %v %v", got, err)
	}
	got, err = SelectActions(cands, []string{"p3", "p1"})
	if err != nil || len(got) != 2 || got[0].ID != "p1" || got[1].ID != "p3" {
		t.Fatalf("selection keeps candidate order: %+v %v", got, err)
	}
	_, err = SelectActions(cands, []string{"p9"})
	wantCode(t, err, "selection-unknown", true)
	_, err = SelectActions(cands, []string{"p1", "p1"})
	wantCode(t, err, "selection-duplicate", true)
	// A plan whose actions differ from its recorded selection is inconsistent.
	in := fixtureInput(cands[0])
	in.Options.Selected = []string{}
	_, err = NewPlan(in)
	wantCode(t, err, "selection-inconsistent", false)
	in = fixtureInput(cands[0])
	in.Options.Selected = []string{"p1", "p2"}
	_, err = NewPlan(in)
	wantCode(t, err, "selection-inconsistent", false)
}

func TestBoundaryIsInternallyConsistent(t *testing.T) {
	for name, mutate := range map[string]func(*PlanInput){
		"offline plan requested the network": func(in *PlanInput) { in.Boundary.NetworkRequested = true },
		"offline plan counted requests":      func(in *PlanInput) { in.Boundary.Requests = 1; in.Boundary.NetworkAttempted = true },
		"attempted without requested":        func(in *PlanInput) { in.Options.Online = true; in.Boundary.NetworkAttempted = true },
		"requests without attempt": func(in *PlanInput) {
			in.Options.Online = true
			in.Boundary.NetworkRequested = true
			in.Boundary.Requests = 3
		},
		"more requests than the limit": func(in *PlanInput) {
			in.Options.Online = true
			in.Boundary.NetworkRequested, in.Boundary.NetworkAttempted, in.Boundary.Requests = true, true, 51
		},
		"unsaved plan reports a private write": func(in *PlanInput) { in.Boundary.PrivateStateWrites = 1 },
		"complete but cancelled":               func(in *PlanInput) { in.Boundary.Cancelled = true },
		"complete with an error":               func(in *PlanInput) { in.Error = &Failure{Code: ReasonCancelled, Message: "x"} },
		"negative reads":                       func(in *PlanInput) { in.Boundary.LocalReads = -1 },
	} {
		in := fixtureInput()
		mutate(&in)
		if _, err := NewPlan(in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	in := fixtureInput()
	in.Options.Online = true
	in.Boundary.NetworkRequested, in.Boundary.NetworkAttempted, in.Boundary.Requests = true, true, 50
	mustPlan(t, in)
}

func TestLimitsAndTextBoundsAreEnforcedInThePlan(t *testing.T) {
	in := fixtureInput()
	in.Limits.Actions = 17
	if _, err := NewPlan(in); err == nil {
		t.Error("limits above the documented ceiling accepted")
	}
	in = fixtureInput()
	in.Limits = Limits{}
	if _, err := NewPlan(in); err == nil {
		t.Error("zero limits accepted")
	}
	long := pushAction("p1", "refs/heads/topic")
	long.Effects = []string{strings.Repeat("x", 513)}
	_, err := NewPlan(fixtureInput(long))
	wantCode(t, err, "bound-exceeded", false)
	in = fixtureInput()
	in.Boundary.Complete = false
	in.Error = &Failure{Code: ReasonBoundExceeded, Message: strings.Repeat("e", 2049)}
	_, err = NewPlan(in)
	wantCode(t, err, "bound-exceeded", false)
}
