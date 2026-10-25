// SPDX-License-Identifier: EUPL-1.2

package stackpublish

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Review F4: truncated, cancelled or failed evidence never becomes permission (FR-010).
func incompletePlans(t *testing.T) map[string]Plan {
	t.Helper()
	out := map[string]Plan{}
	for name, mutate := range map[string]func(*PlanInput){
		"not complete": func(in *PlanInput) { in.Boundary.Complete = false },
		"cancelled":    func(in *PlanInput) { in.Boundary.Complete, in.Boundary.Cancelled = false, true },
		"carries an error": func(in *PlanInput) {
			in.Boundary.Complete, in.Error = false, &Failure{Code: ReasonBoundExceeded, Message: "bound reached"}
		},
	} {
		in := fixtureInput(pushAction("p1", "refs/heads/topic"))
		mutate(&in)
		out[name] = mustPlan(t, in)
	}
	return out
}

func TestAnIncompleteCancelledOrErroredPlanCannotBeSavedOrApplied(t *testing.T) {
	for name, p := range incompletePlans(t) {
		_, err := p.AdmitSave(SaveRequest{Save: true, Store: StoreValidated})
		if v, ok := err.(*ValidationError); !ok || v.Code != "plan-incomplete" {
			t.Errorf("%s: save admitted: %v", name, err)
		}
		if _, err := p.WithSaved(SaveAdmission{Persist: true, Target: TargetWorkspaceDefault}); err == nil {
			t.Errorf("%s: WithSaved marked it saved", name)
		}
		// A hand-built saved copy (a stored record) is refused at apply too.
		hand, id := p, p.ID()
		hand.doc.PlanID, hand.doc.Saved = &id, true
		hand.doc.Boundary.PrivateStateWrites = 1
		if v, ok := hand.AdmitApply(id).(*ValidationError); !ok || v.Code != "plan-incomplete" {
			t.Errorf("%s: apply admitted: %v", name, hand.AdmitApply(id))
		}
		if _, err := NewRun(hand, ApplyRequest{PlanID: id, Online: true, AuthorizeRemote: []string{"p1"}}, allCapabilities(), newBudget()); err == nil {
			t.Errorf("%s: a run started", name)
		}
	}
	// Even with an empty selection, a save request on an incomplete plan is refused, not "nothing selected".
	in := fixtureInput()
	in.Boundary.Complete = false
	p := mustPlan(t, in)
	if _, err := p.AdmitSave(SaveRequest{Save: true, Store: StoreValidated}); err == nil {
		t.Error("an incomplete empty plan was admitted for save")
	}
}

// Review F6: one fact, one id; deterministic reporting.
func TestSelectionOrderAndMarkerOrderDoNotChangeThePlanID(t *testing.T) {
	a, b := pushAction("p1", "refs/heads/one"), pushAction("p2", "refs/heads/two")
	x, y := fixtureInput(a, b), fixtureInput(a, b)
	x.Options.Selected, y.Options.Selected = []string{"p1", "p2"}, []string{"p2", "p1"}
	px, py := mustPlan(t, x), mustPlan(t, y)
	if px.ID() != py.ID() || strings.Join(py.Options().Selected, ",") != "p1,p2" {
		t.Fatalf("--select order changed the id: %s vs %s, selected %v", px.ID(), py.ID(), py.Options().Selected)
	}
	m, n := fixtureInput(), fixtureInput()
	m.Mode.Markers, n.Mode.Markers = []string{"CODEX_HOME", "CLAUDECODE"}, []string{"CLAUDECODE", "CODEX_HOME", "CLAUDECODE"}
	pm, pn := mustPlan(t, m), mustPlan(t, n)
	if pm.ID() != pn.ID() {
		t.Fatalf("marker order or repetition changed the id")
	}
}

func TestRepositoryFieldErrorsAreReportedInAFixedOrder(t *testing.T) {
	for i := 0; i < 60; i++ {
		in := fixtureInput()
		r := *in.Repository
		r.LayoutIdentity, r.SelectionProvenance, r.DefaultSource = "", "", ""
		in.Repository = &r
		_, err := NewPlan(in)
		if v, ok := err.(*ValidationError); !ok || v.Field != "repository.layout_identity" {
			t.Fatalf("run %d: %v", i, err)
		}
	}
}

func TestErrorMessageIsSanitizedSoDistinctPlansKeepDistinctIDs(t *testing.T) {
	for name, msg := range map[string]string{"invalid utf-8": "x\xff", "other invalid utf-8": "x\xfe", "control": "bad\x1b[31m", "newline": "a\nb", "empty": ""} {
		in := fixtureInput()
		in.Boundary.Complete = false
		in.Error = &Failure{Code: ReasonBoundExceeded, Message: msg}
		if _, err := NewPlan(in); err == nil {
			t.Errorf("%s: accepted %q", name, msg)
		}
	}
}

func TestACreatedAtIsRequired(t *testing.T) {
	in := fixtureInput()
	in.CreatedAt = time.Time{}
	_, err := NewPlan(in)
	wantCode(t, err, "created-at-invalid", false)
}

// Review F7: every ref this package accepts must be accepted by git itself.
func TestValidRefIsASubsetOfGitCheckRefFormat(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	parts := []string{"a", "A-1", "x.y", ".hidden", "-opt", "end.", "ends.lock", "a..b", "a.b.", "feat/own", "feat//own", "feat/", "/feat", "a.lock/b", "a/.b", "a/b.", "@", "a@{b", "a b", "a~b", "a^b", "a:b", "a?b", "a*b", "a[b", "a\\b", "é", "", ".", "..", "-", "_", "0", "HEAD", "a/b/c/d", "a/b.lock", "x." + strings.Repeat("y", 10)}
	var refs []string
	for _, p := range parts {
		refs = append(refs, "refs/heads/"+p)
		for _, q := range parts[:12] {
			refs = append(refs, "refs/heads/"+p+"/"+q)
		}
	}
	refs = append(refs, "refs/tags/v1", "refs/heads", "refs/heads/", "heads/a", "main", "refs/heads/"+strings.Repeat("a", 190))
	home := t.TempDir()
	for _, ref := range refs {
		cmd := exec.Command(git, "check-ref-format", ref)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_CONFIG_NOSYSTEM=1"}
		gitOK := cmd.Run() == nil
		if validRef(ref) && !gitOK {
			t.Errorf("validRef accepts %q but git check-ref-format rejects it", ref)
		}
	}
	for _, ref := range []string{"refs/heads/foo.", "refs/heads/a/foo.", "refs/heads/.", "refs/heads/a.b."} {
		if validRef(ref) {
			t.Errorf("%q must be rejected: a ref cannot end with a dot", ref)
		}
	}
	for _, ref := range []string{"refs/heads/feat/own", "refs/heads/a.b", "refs/heads/A-1_x"} {
		if !validRef(ref) {
			t.Errorf("%q must be accepted", ref)
		}
	}
}

// Review F9: --timeout is any positive duration up to the maximum, like the shared flag.
func TestTimeoutAcceptsAnyPositiveDurationUpToTheMaximum(t *testing.T) {
	for d, millis := range map[time.Duration]int{1500 * time.Millisecond: 1500, 250 * time.Millisecond: 250, time.Millisecond: 1, 500 * time.Microsecond: 1, 60 * time.Second: 60000, 59999 * time.Millisecond: 59999} {
		l, err := DefaultLimits().WithTimeout(d)
		if err != nil || l.OperationMillis != millis || l.RequestMillis != 30000 {
			t.Errorf("%v: %+v %v", d, l, err)
		}
	}
	for _, d := range []time.Duration{0, -time.Millisecond, 60*time.Second + time.Millisecond, time.Hour} {
		_, err := DefaultLimits().WithTimeout(d)
		wantCode(t, err, "timeout-invalid", true)
	}
}

func TestTimeoutNeverLengthensAShorterBound(t *testing.T) {
	short := DefaultLimits()
	short.OperationMillis = 10000
	l, err := short.WithTimeout(30 * time.Second)
	if err != nil || l.OperationMillis != 10000 {
		t.Fatalf("%+v %v", l, err)
	}
}

// Review F11a: the id grammar is checked before the saved state, so a malformed --plan is usage.
func TestAMalformedPlanIDIsUsageEvenForAnUnsavedPlan(t *testing.T) {
	p := nonEmptyPlan(t)
	for _, id := range []string{"", "garbage", strings.ToUpper(p.ID()), p.ID()[:10]} {
		wantCode(t, p.AdmitApply(id), "plan-id-invalid", true)
	}
	wantCode(t, p.AdmitApply(p.ID()), "plan-not-saved", false)
}
