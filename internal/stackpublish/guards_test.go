// SPDX-License-Identifier: EUPL-1.2

package stackpublish

import (
	"reflect"
	"testing"
	"time"
)

func savedPlan(t *testing.T, actions ...Action) Plan {
	t.Helper()
	p := mustPlan(t, fixtureInput(actions...))
	adm, err := p.AdmitSave(SaveRequest{Save: true, Store: StoreValidated})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := p.WithSaved(adm)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

func passing(kind ActionKind) []Guard {
	gs := []Guard{{Name: GuardOwnership, Result: GuardPass}}
	if kind == KindPushTopic || kind == KindReady {
		gs = append(gs, Guard{Name: GuardAttribution, Result: GuardPass})
	}
	return gs
}

func cloneExpected(e Expected) Expected {
	return Expected{OldSHA: cloneString(e.OldSHA), NewSHA: cloneString(e.NewSHA), HeadSHA: cloneString(e.HeadSHA), BaseRef: cloneString(e.BaseRef), PlannedBase: cloneString(e.PlannedBase), BodyDigest: cloneString(e.BodyDigest)}
}

// matching serves fresh evidence identical to the plan, recording the order it was asked in.
func matching(asked *[]string, tweak func(Action, *Fresh)) FreshSource {
	return func(a Action) (Fresh, bool) {
		*asked = append(*asked, a.ID)
		f := Fresh{Expected: cloneExpected(a.Expected), Permission: PermissionGranted, Guards: passing(a.Kind)}
		if tweak != nil {
			tweak(a, &f)
		}
		return f, true
	}
}

func applyRequest(p Plan, ids ...string) ApplyRequest {
	return ApplyRequest{PlanID: *p.PlanID(), Online: true, AuthorizeRemote: ids}
}

func newBudget() *Budget { return NewBudget(DefaultLimits(), newClock().read) }

func states(e Evaluation) []string {
	var out []string
	for _, d := range e.Decisions {
		s := string(d.State)
		if d.Reason != nil {
			s += ":" + string(*d.Reason)
		}
		out = append(out, s)
	}
	return out
}

func twoPushes() []Action {
	return []Action{pushAction("p1", "refs/heads/one"), pushAction("p2", "refs/heads/two"), pushAction("p3", "refs/heads/three")}
}

func TestEveryActionNeedsItsOwnRemoteAuthorizationAndNothingElseGrantsIt(t *testing.T) {
	p := savedPlan(t, twoPushes()...)
	var asked []string
	e, err := EvaluateApply(p, applyRequest(p, "p1", "p2", "p3"), allCapabilities(), newBudget(), matching(&asked, nil))
	if err != nil || e.Status() != StatusComplete || !reflect.DeepEqual(states(e), []string{"prepared", "prepared", "prepared"}) {
		t.Fatalf("all authorized: %v %v %v", states(e), e.Status(), err)
	}
	asked = nil
	e, err = EvaluateApply(p, applyRequest(p, "p1", "p3"), allCapabilities(), newBudget(), matching(&asked, nil))
	if err != nil || !reflect.DeepEqual(states(e), []string{"prepared", "refused:authorization-missing", "not-started"}) || e.Status() != StatusRefused {
		t.Fatalf("p2 unauthorized stops the run and p3's authorization is not inherited or reordered: %v %v", states(e), err)
	}
	if !reflect.DeepEqual(asked, []string{"p1"}) {
		t.Errorf("fresh evidence was read for an unauthorized action: %v", asked)
	}
	// Local --authorize is never a remote permission.
	asked = nil
	req := applyRequest(p)
	req.LocalAuthorize = true
	e, err = EvaluateApply(p, req, allCapabilities(), newBudget(), matching(&asked, nil))
	if err != nil || !reflect.DeepEqual(states(e), []string{"refused:authorization-missing", "not-started", "not-started"}) || len(asked) != 0 {
		t.Fatalf("local authorization granted a remote effect: %v %v %v", states(e), asked, err)
	}
}

func TestExtraDuplicateAndForeignAuthorizationIdsAreUsageErrors(t *testing.T) {
	p := savedPlan(t, twoPushes()[:2]...)
	for name, ids := range map[string][]string{"extra": {"p1", "p9"}, "duplicate": {"p1", "p1"}, "old plan action": {"stale-from-old-plan"}, "empty id": {""}} {
		var asked []string
		_, err := EvaluateApply(p, applyRequest(p, ids...), allCapabilities(), newBudget(), matching(&asked, nil))
		var v *ValidationError
		if !asValidation(err, &v) || !v.Usage || len(asked) != 0 {
			t.Errorf("%s: %v asked=%v", name, err, asked)
		}
	}
}

func asValidation(err error, target **ValidationError) bool {
	v, ok := err.(*ValidationError)
	*target = v
	return ok
}

func TestApplyNeedsOnlineAndTheExactSavedPlan(t *testing.T) {
	p := savedPlan(t, twoPushes()[:1]...)
	req := applyRequest(p, "p1")
	req.Online = false
	var asked []string
	_, err := EvaluateApply(p, req, allCapabilities(), newBudget(), matching(&asked, nil))
	wantCode(t, err, "online-required", true)
	req = applyRequest(p, "p1")
	req.PlanID = "0000000000000000000000000000000000000000000000000000000000000000"
	_, err = EvaluateApply(p, req, allCapabilities(), newBudget(), matching(&asked, nil))
	wantCode(t, err, "plan-not-found", false)
	unsaved := mustPlan(t, fixtureInput(twoPushes()[0]))
	_, err = EvaluateApply(unsaved, ApplyRequest{PlanID: unsaved.ID(), Online: true, AuthorizeRemote: []string{"p1"}}, allCapabilities(), newBudget(), matching(&asked, nil))
	wantCode(t, err, "plan-not-saved", false)
	if len(asked) != 0 {
		t.Errorf("evidence read before the plan was admitted: %v", asked)
	}
}

func TestStaleEvidenceRefusesWithTheTypedReasonBeforeAnyIntent(t *testing.T) {
	retarget := retargetAction("r1", 7)
	push := pushAction("p1", "refs/heads/topic")
	cases := map[string]struct {
		action Action
		tweak  func(*Fresh)
		want   Reason
	}{
		"remote old sha moved":   {push, func(f *Fresh) { f.Expected.OldSHA = str(sha('9')) }, ReasonRemoteChanged},
		"actual new sha changed": {push, func(f *Fresh) { f.Expected.NewSHA = str(sha('9')) }, ReasonPlanStale},
		"old sha disappeared":    {push, func(f *Fresh) { f.Expected.OldSHA = nil }, ReasonRemoteChanged},
		"pr head moved":          {retarget, func(f *Fresh) { f.Expected.HeadSHA = str(sha('9')) }, ReasonHeadChanged},
		"pr base moved":          {retarget, func(f *Fresh) { f.Expected.BaseRef = str("refs/heads/elsewhere") }, ReasonPlanStale},
		"planned base moved":     {retarget, func(f *Fresh) { f.Expected.PlannedBase = str("refs/heads/other") }, ReasonPlanStale},
	}
	for name, tc := range cases {
		p := savedPlan(t, tc.action)
		var asked []string
		e, err := EvaluateApply(p, applyRequest(p, tc.action.ID), allCapabilities(), newBudget(), matching(&asked, func(_ Action, f *Fresh) { tc.tweak(f) }))
		want := "refused:" + string(tc.want)
		if err != nil || len(e.Decisions) != 1 || states(e)[0] != want {
			t.Errorf("%s: %v %v want %s", name, states(e), err, want)
		}
	}
	p := savedPlan(t, push)
	e, _ := EvaluateApply(p, applyRequest(p, "p1"), allCapabilities(), newBudget(), func(Action) (Fresh, bool) { return Fresh{}, false })
	if got := states(e); !reflect.DeepEqual(got, []string{"refused:plan-stale"}) {
		t.Errorf("unreadable fresh evidence must refuse as stale: %v", got)
	}
}

func TestPermissionMustBeKnownGranted(t *testing.T) {
	for perm, want := range map[Permission]string{PermissionGranted: "prepared", PermissionUnknown: "refused:permission-unknown", PermissionDenied: "refused:permission-unknown", "": "refused:permission-unknown", "granted ": "refused:permission-unknown"} {
		p := savedPlan(t, pushAction("p1", "refs/heads/topic"))
		var asked []string
		e, _ := EvaluateApply(p, applyRequest(p, "p1"), allCapabilities(), newBudget(), matching(&asked, func(_ Action, f *Fresh) { f.Permission = perm }))
		if got := states(e)[0]; got != want {
			t.Errorf("permission %q: %s want %s", perm, got, want)
		}
	}
}

func TestRequiredGuardsMustBePresentAndPassing(t *testing.T) {
	push := pushAction("p1", "refs/heads/topic")
	retarget := retargetAction("r1", 7)
	cases := map[string]struct {
		action Action
		guards []Guard
		want   string
	}{
		"ownership missing":             {push, []Guard{{Name: GuardAttribution, Result: GuardPass}}, "refused:ownership-unknown"},
		"attribution missing for push":  {push, []Guard{{Name: GuardOwnership, Result: GuardPass}}, "refused:attribution-unknown"},
		"attribution missing for ready": {validAction(KindReady), []Guard{{Name: GuardOwnership, Result: GuardPass}}, "refused:attribution-unknown"},
		"attribution not needed":        {retarget, []Guard{{Name: GuardOwnership, Result: GuardPass}}, "prepared"},
		"attribution not-applicable":    {retarget, []Guard{{Name: GuardOwnership, Result: GuardPass}, {Name: GuardAttribution, Result: GuardNotApplicable}}, "prepared"},
		"ownership fails":               {push, []Guard{{Name: GuardOwnership, Result: GuardFail, Reason: ReasonNotOwned}, {Name: GuardAttribution, Result: GuardPass}}, "refused:not-owned"},
		"ownership unknown":             {push, []Guard{{Name: GuardOwnership, Result: GuardUnknown, Reason: ReasonOwnershipUnknown}, {Name: GuardAttribution, Result: GuardPass}}, "refused:ownership-unknown"},
		"d5 mismatch":                   {push, []Guard{{Name: GuardOwnership, Result: GuardPass}, {Name: GuardAttribution, Result: GuardFail, Reason: ReasonAttributionMismatch}}, "refused:attribution-mismatch"},
		"extra relevant guard unknown":  {retarget, []Guard{{Name: GuardOwnership, Result: GuardPass}, {Name: "ci", Result: GuardUnknown, Reason: ReasonChecksUnknown}}, "refused:checks-unknown"},
		"malformed guard":               {retarget, []Guard{{Name: GuardOwnership, Result: GuardPass}, {Name: "x", Result: "maybe"}}, "refused:plan-stale"},
		"guard without a reason":        {retarget, []Guard{{Name: GuardOwnership, Result: GuardPass}, {Name: "x", Result: GuardFail}}, "refused:plan-stale"},
		"duplicate ownership guard":     {retarget, []Guard{{Name: GuardOwnership, Result: GuardPass}, {Name: GuardOwnership, Result: GuardFail, Reason: ReasonNotOwned}}, "refused:not-owned"},
	}
	for name, tc := range cases {
		p := savedPlan(t, tc.action)
		var asked []string
		e, err := EvaluateApply(p, applyRequest(p, tc.action.ID), allCapabilities(), newBudget(), matching(&asked, func(_ Action, f *Fresh) { f.Guards = tc.guards }))
		if err != nil || states(e)[0] != tc.want {
			t.Errorf("%s: %v %v want %s", name, states(e), err, tc.want)
		}
	}
}

func TestGuardsRecordedAtPlanTimeAreNotTrustedAtApply(t *testing.T) {
	a := pushAction("p1", "refs/heads/topic")
	a.Guards = passing(KindPushTopic)
	p := savedPlan(t, a)
	var asked []string
	e, _ := EvaluateApply(p, applyRequest(p, "p1"), allCapabilities(), newBudget(), matching(&asked, func(_ Action, f *Fresh) { f.Guards = nil }))
	if got := states(e); !reflect.DeepEqual(got, []string{"refused:ownership-unknown"}) {
		t.Fatalf("plan-time guards satisfied the fresh guard requirement: %v", got)
	}
}

func TestBlockedActionAndMissingCapabilityStopTheRun(t *testing.T) {
	blocked := pushAction("p1", "refs/heads/one")
	blocked.State, blocked.Blocker = StateBlocked, reasonPtr(ReasonNativeRewritePending)
	p := savedPlan(t, blocked, pushAction("p2", "refs/heads/two"))
	var asked []string
	e, _ := EvaluateApply(p, applyRequest(p, "p1", "p2"), allCapabilities(), newBudget(), matching(&asked, nil))
	if got := states(e); !reflect.DeepEqual(got, []string{"refused:native-rewrite-pending", "not-started"}) || len(asked) != 0 {
		t.Fatalf("a complete plan with a blocked action is not an apply success: %v %v", got, asked)
	}
	p = savedPlan(t, pushAction("p1", "refs/heads/one"))
	e, _ = EvaluateApply(p, applyRequest(p, "p1"), CapabilitySet{}, newBudget(), matching(&asked, nil))
	if got := states(e); !reflect.DeepEqual(got, []string{"refused:capability-unavailable"}) {
		t.Fatalf("capability gate at apply: %v", got)
	}
	if e.Status() != StatusRefused || ExitCode(e.Status()) != 1 {
		t.Errorf("status %v", e.Status())
	}
}

func TestEvidenceIsReadLazilyInPlanOrderAndTheRunStopsAtTheFirstRefusal(t *testing.T) {
	p := savedPlan(t, twoPushes()...)
	var asked []string
	e, _ := EvaluateApply(p, applyRequest(p, "p1", "p2", "p3"), allCapabilities(), newBudget(), matching(&asked, func(a Action, f *Fresh) {
		if a.ID == "p2" {
			f.Permission = PermissionUnknown
		}
	}))
	if !reflect.DeepEqual(asked, []string{"p1", "p2"}) || !reflect.DeepEqual(states(e), []string{"prepared", "refused:permission-unknown", "not-started"}) {
		t.Fatalf("asked=%v states=%v", asked, states(e))
	}
	// The same decisions come from the same inputs.
	asked = nil
	e2, _ := EvaluateApply(p, applyRequest(p, "p1", "p2", "p3"), allCapabilities(), newBudget(), matching(&asked, func(a Action, f *Fresh) {
		if a.ID == "p2" {
			f.Permission = PermissionUnknown
		}
	}))
	if !reflect.DeepEqual(states(e), states(e2)) {
		t.Error("evaluation is not deterministic")
	}
}

func TestCancellationAndDeadlineStartNothingFurther(t *testing.T) {
	p := savedPlan(t, twoPushes()...)
	b := newBudget()
	b.Cancel()
	var asked []string
	e, _ := EvaluateApply(p, applyRequest(p, "p1", "p2", "p3"), allCapabilities(), b, matching(&asked, nil))
	if !reflect.DeepEqual(states(e), []string{"not-started", "not-started", "not-started"}) || e.Status() != StatusCancelled || len(asked) != 0 || ExitCode(e.Status()) != 1 {
		t.Fatalf("%v %v %v", states(e), e.Status(), asked)
	}
	// Cancelled while the second action's evidence is being read: p1 stays prepared, p2 and p3 start nothing.
	b = newBudget()
	asked = nil
	e, _ = EvaluateApply(p, applyRequest(p, "p1", "p2", "p3"), allCapabilities(), b, matching(&asked, func(a Action, _ *Fresh) {
		if a.ID == "p2" {
			b.Cancel()
		}
	}))
	if !reflect.DeepEqual(states(e), []string{"prepared", "not-started", "not-started"}) || e.Status() != StatusCancelled {
		t.Fatalf("%v %v", states(e), e.Status())
	}
	// The injected clock passes the operation deadline during the first read.
	clock := newClock()
	b = NewBudget(DefaultLimits(), clock.read)
	asked = nil
	e, _ = EvaluateApply(p, applyRequest(p, "p1", "p2", "p3"), allCapabilities(), b, matching(&asked, func(_ Action, _ *Fresh) { clock.now = clock.now.Add(61 * time.Second) }))
	if !reflect.DeepEqual(states(e), []string{"not-started", "not-started", "not-started"}) || e.Status() != StatusIncomplete || ExitCode(e.Status()) != 1 {
		t.Fatalf("%v %v", states(e), e.Status())
	}
}

func TestEvaluationDoesNotChangeThePlan(t *testing.T) {
	p := savedPlan(t, twoPushes()...)
	before := mustJSON(t, p)
	var asked []string
	if _, err := EvaluateApply(p, applyRequest(p, "p1", "p2", "p3"), allCapabilities(), newBudget(), matching(&asked, func(_ Action, f *Fresh) { f.Expected.OldSHA = str(sha('9')) })); err != nil {
		t.Fatal(err)
	}
	if mustJSON(t, p) != before {
		t.Fatal("apply evaluation mutated the stored plan")
	}
}
