// SPDX-License-Identifier: EUPL-1.2

package stackpublish

import (
	"encoding/json"
	"strings"
	"testing"
)

func nonEmptyPlan(t *testing.T) Plan {
	t.Helper()
	return mustPlan(t, fixtureInput(pushAction("p1", "refs/heads/topic")))
}

func TestUnsavedIsTheDefaultAndNeverPersists(t *testing.T) {
	p := nonEmptyPlan(t)
	// Without --save nothing persists, whether or not a state dir was named.
	for _, req := range []SaveRequest{{}, {StateDir: "/some/dir", Store: StoreValidated}, {Store: StoreValidated}} {
		adm, err := p.AdmitSave(req)
		if err != nil || adm.Persist || adm.Target != TargetNone || adm.NothingSelected {
			t.Errorf("%+v: %+v %v", req, adm, err)
		}
	}
}

func TestEmptySelectionNeverSavesEvenWithSave(t *testing.T) {
	p := mustPlan(t, fixtureInput())
	for _, req := range []SaveRequest{{Save: true, Store: StoreValidated}, {Save: true, StateDir: "/d", Store: StoreValidated}, {Save: true, Store: StoreFull}} {
		adm, err := p.AdmitSave(req)
		if err != nil || adm.Persist || !adm.NothingSelected || adm.Target != TargetNone {
			t.Errorf("%+v: %+v %v", req, adm, err)
		}
	}
	if _, err := p.WithSaved(SaveAdmission{Persist: true, Target: TargetWorkspaceDefault}); err == nil {
		t.Error("an empty plan was marked saved")
	}
	if p.Saved() || p.PlanID() != nil {
		t.Error("an empty plan must keep a null id")
	}
}

func TestExplicitSaveUsesTheWorkspaceDefaultOrTheExplicitDirectory(t *testing.T) {
	p := nonEmptyPlan(t)
	adm, err := p.AdmitSave(SaveRequest{Save: true, Store: StoreValidated})
	if err != nil || !adm.Persist || adm.Target != TargetWorkspaceDefault {
		t.Fatalf("default workspace store: %+v %v", adm, err)
	}
	adm, err = p.AdmitSave(SaveRequest{Save: true, StateDir: "/explicit", Store: StoreValidated})
	if err != nil || !adm.Persist || adm.Target != TargetExplicitDir {
		t.Fatalf("explicit directory: %+v %v", adm, err)
	}
}

func TestSaveIsRefusedUnlessTheStoreWasValidated(t *testing.T) {
	p := nonEmptyPlan(t)
	for state, code := range map[StoreState]string{StoreWorkspaceUnknown: "workspace-unknown", StoreExportRisk: "plan-store-export-risk", StoreFull: "plan-store-full", StoreUnsafe: "plan-store-unsafe", "": "plan-store-unsafe", "validated-ish": "plan-store-unsafe"} {
		adm, err := p.AdmitSave(SaveRequest{Save: true, Store: state})
		if adm.Persist {
			t.Errorf("%q: persisted", state)
		}
		wantCode(t, err, code, false)
	}
	_, err := p.AdmitSave(SaveRequest{Save: true, StateDir: "relative/dir", Store: StoreValidated})
	wantCode(t, err, "state-dir-invalid", true)
	_, err = p.AdmitSave(SaveRequest{Save: true, StateDir: "/d\x00x", Store: StoreValidated})
	wantCode(t, err, "state-dir-invalid", true)
}

func TestWithSavedReportsTheImmutableContentID(t *testing.T) {
	p := nonEmptyPlan(t)
	adm, err := p.AdmitSave(SaveRequest{Save: true, Store: StoreValidated})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := p.WithSaved(adm)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Saved() || saved.PlanID() == nil || *saved.PlanID() != p.ID() || saved.ID() != p.ID() {
		t.Fatalf("saved plan: saved=%v id=%v want %s", saved.Saved(), saved.PlanID(), p.ID())
	}
	if p.Saved() || p.PlanID() != nil {
		t.Fatal("WithSaved changed the original plan")
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(mustJSON(t, saved)), &doc); err != nil || doc["saved"] != true || doc["plan_id"] != p.ID() {
		t.Fatalf("%v %v", doc, err)
	}
	if _, err := p.WithSaved(SaveAdmission{}); err == nil {
		t.Error("a plan was marked saved without an admitted save")
	}
	if _, err := saved.WithSaved(adm); err == nil {
		t.Error("an already saved plan was saved again")
	}
}

func TestApplyAdmitsOnlyTheExactSavedID(t *testing.T) {
	p := nonEmptyPlan(t)
	err := p.AdmitApply(p.ID())
	wantCode(t, err, "plan-not-saved", false)
	adm, _ := p.AdmitSave(SaveRequest{Save: true, Store: StoreValidated})
	saved, err := p.WithSaved(adm)
	if err != nil {
		t.Fatal(err)
	}
	if err := saved.AdmitApply(*saved.PlanID()); err != nil {
		t.Fatalf("the exact saved id is admitted: %v", err)
	}
	for _, id := range []string{"", strings.Repeat("0", 64), strings.ToUpper(*saved.PlanID()), (*saved.PlanID())[:63], (*saved.PlanID())[:32]} {
		if err := saved.AdmitApply(id); err == nil {
			t.Errorf("apply admitted the id %q", id)
		}
	}
	err = saved.AdmitApply(strings.ToUpper(*saved.PlanID()))
	wantCode(t, err, "plan-id-invalid", true)
	err = saved.AdmitApply(strings.Repeat("0", 64))
	wantCode(t, err, "plan-not-found", false)
	// A saved plan with an empty selection cannot exist, so there is nothing to apply.
	if err := mustPlan(t, fixtureInput()).AdmitApply(""); err == nil {
		t.Error("an unsaved empty plan was admitted for apply")
	}
}
