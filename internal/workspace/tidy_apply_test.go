// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

type fakeTidyMutation struct {
	*fakeTidyAdapter
	pins, deletes                int
	pinErr                       bool
	onPin                        func()
	onDelete                     func()
	pinPrepared, deleteConfirmed bool
	state, plan                  string
}

func (f *fakeTidyMutation) PrepareRecovery(_ context.Context, p TidyPlanRecord, c TidyCandidate) (TidyRecovery, Snapshot, TidyNativeOutcome, error) {
	f.pins++
	s, err := openTidyStore(f.state, false)
	if err == nil {
		var j TidyJournal
		err = s.read("tidy-operations", f.plan, &j)
		s.Close()
		f.pinPrepared = err == nil && len(j.Entries) > 0 && j.Entries[len(j.Entries)-1].Phase == "prepared" && j.Entries[len(j.Entries)-1].Recovery.Name == tidyRecoveryFor(p, c).Name
	}
	r := tidyRecoveryFor(p, c)
	f.observation.Expected.ReferenceDigest = strings.Repeat("4", 64)
	out := TidyNativeOutcome{Attempted: true, Complete: !f.pinErr, Effects: []string{"Created exact recovery root " + r.Name}, Limitations: []string{}}
	if f.onPin != nil {
		f.onPin()
	}
	if f.pinErr {
		return r, f.observation.Expected, out, errors.New("interrupted after pin")
	}
	return r, f.observation.Expected, out, nil
}
func (f *fakeTidyMutation) Delete(_ context.Context, p TidyPlanRecord, c TidyCandidate, r TidyRecovery, expected Snapshot) (TidyNativeOutcome, error) {
	f.deletes++
	s, err := openTidyStore(f.state, false)
	if err == nil {
		var j TidyJournal
		err = s.read("tidy-operations", f.plan, &j)
		s.Close()
		f.deleteConfirmed = err == nil && j.Entries[len(j.Entries)-1].Phase == "recovery-confirmed"
	}
	f.observation.Expected.ReferenceDigest = strings.Repeat("5", 64)
	f.observation.Candidates = []TidyCandidate{}
	if f.onDelete != nil {
		f.onDelete()
	}
	snap := f.observation.Expected
	return TidyNativeOutcome{Attempted: true, Complete: true, Effects: []string{"Deleted only " + c.Name}, Limitations: []string{}, Expected: &snap}, nil
}
func fixtureTidyApply(t *testing.T) (tidyCoordinator, *fakeTidyMutation, TidyApplyOptions) {
	t.Helper()
	c, f, o := fixtureTidyCoordinator(t)
	m := &fakeTidyMutation{fakeTidyAdapter: f}
	c.adapter = func(string) (tidyAdapter, error) { return m, nil }
	r := c.plan(context.Background(), o)
	if r.Status != "ok" {
		t.Fatalf("plan %#v", r)
	}
	m.state = o.StateDir
	m.plan = r.Plan.PlanID
	return c, m, TidyApplyOptions{StateDir: o.StateDir, PlanID: r.Plan.PlanID, Authorize: true}
}
func TestTidyApplyDurableRecoveryBeforeDeletionAndVerifiedRetry(t *testing.T) {
	c, f, o := fixtureTidyApply(t)
	r := c.apply(context.Background(), o)
	if r.Status != "ok" || f.pins != 1 || f.deletes != 1 || !f.pinPrepared || !f.deleteConfirmed {
		t.Fatalf("unsafe apply %+v pin%d delete%d prepared%v confirmed%v", r, f.pins, f.deletes, f.pinPrepared, f.deleteConfirmed)
	}
	r = c.apply(context.Background(), o)
	if r.Status != "ok" || f.pins != 1 || f.deletes != 1 {
		t.Fatalf("repeat effects %+v", r)
	}
	f.observation.Expected.SafetyDigest = strings.Repeat("9", 64)
	if r = c.apply(context.Background(), o); r.Status == "ok" {
		t.Fatal("changed complete retry accepted")
	}
}
func TestTidyApplyAuthorizationOldPurposeAndStaleRefusal(t *testing.T) {
	for _, kind := range []string{"authorization", "old-purpose", "stale"} {
		t.Run(kind, func(t *testing.T) {
			c, f, o := fixtureTidyApply(t)
			if kind == "authorization" {
				o.Authorize = false
			}
			if kind == "stale" {
				f.observation.Expected.BaseCommit = strings.Repeat("c", 40)
			}
			if kind == "old-purpose" {
				s, err := openTidyStore(o.StateDir, true)
				if err != nil {
					t.Fatal(err)
				}
				var p TidyPlanRecord
				if err = s.read("tidy-plans", o.PlanID, &p); err != nil {
					t.Fatal(err)
				}
				p.Effects = []string{"Persist one immutable plan in the explicitly selected private state directory; no native repository effects."}
				p.RecoveryLimits = []string{"Evidence is sampled and nontransactional; external or ABA changes may be missed.", "A plan does not authorize deletion; references, registrations, files and native state are unchanged.", "Only native ancestry is supported; squash-equivalence and forge proof are unavailable and do not authorize deletion."}
				p.Digest = tidyPlanDigest(p)
				if err = s.write("tidy-plans", p.PlanID, p, false); err != nil {
					t.Fatal(err)
				}
				s.Close()
			}
			r := c.apply(context.Background(), o)
			if r.Status != "refused" || f.pins != 0 || f.deletes != 0 {
				t.Fatalf("unsafe refusal %+v", r)
			}
		})
	}
}
func TestTidyApplyPossiblePinEffectsAndPersistenceFailureNeverDelete(t *testing.T) {
	for _, kind := range []string{"pin-error", "journal-failure", "canceled"} {
		t.Run(kind, func(t *testing.T) {
			c, f, o := fixtureTidyApply(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if kind == "pin-error" {
				f.pinErr = true
			}
			if kind == "journal-failure" {
				f.onPin = func() {
					if err := os.Chmod(o.StateDir, 0755); err != nil {
						t.Fatal(err)
					}
				}
			}
			if kind == "canceled" {
				f.onPin = cancel
			}
			r := c.apply(ctx, o)
			if r.Status != "partial" || f.pins != 1 || f.deletes != 0 || len(r.Actions) == 0 {
				t.Fatalf("lost pin effects %+v", r)
			}
			os.Chmod(o.StateDir, 0700)
			r = c.apply(context.Background(), o)
			if r.Status != "partial" || f.pins != 1 || f.deletes != 0 {
				t.Fatalf("blind uncertain replay %+v", r)
			}
		})
	}
}
func TestTidyBoundedPartialRetainsRecoveryJournalPointer(t *testing.T) {
	c, _, o := fixtureTidyApply(t)
	r := c.apply(context.Background(), o)
	r.Limitations = append(r.Limitations, strings.Repeat("\x01", recordLimit))
	r = boundedTidy(r)
	if r.Status != "partial" || !strings.Contains(strings.Join(r.Limitations, " "), o.StateDir+"/tidy-operations/"+o.PlanID+".json") {
		t.Fatalf("lost durable pointer %+v", r)
	}
}

func TestTidyBoundedPartialWithoutPlanRetainsRecoveryPointer(t *testing.T) {
	c, _, o := fixtureTidyApply(t)
	r := c.apply(context.Background(), o)
	r.Plan = nil
	r.Status = "partial"
	r.Limitations = append(r.Limitations, strings.Repeat("\x01", recordLimit))
	r = boundedTidy(r)
	if !strings.Contains(strings.Join(r.Limitations, " "), o.StateDir+"/tidy-operations/"+o.PlanID+".json") {
		t.Fatal("bounded error lost independent journal pointer")
	}
}
func TestTidyMalformedNativeOutcomeNeverAuthorizesDeletion(t *testing.T) {
	entry := TidyActionRecord{Actions: []ActionOutcome{}}
	if addTidyNativeOutcome(&entry, "create-recovery", TidyNativeOutcome{Attempted: true, Complete: true, Effects: make([]string, observationLimit+1), Limitations: []string{}}) {
		t.Fatal("oversized outcome accepted")
	}
	if len(entry.Actions) != 1 || entry.Actions[0].Status != "incomplete" {
		t.Fatal("possible effects lost")
	}
}

func TestTidyCoreRejectsUnrelatedStateDeltaAfterRecovery(t *testing.T) {
	c, f, o := fixtureTidyApply(t)
	f.onPin = func() { f.observation.Expected.BaseCommit = strings.Repeat("d", 40) }
	r := c.apply(context.Background(), o)
	if r.Status != "partial" || f.deletes != 0 {
		t.Fatalf("unrelated native base delta authorized deletion %+v", r)
	}
}

func TestTidyJournalReadRejectsUnboundedAndMalformedRecords(t *testing.T) {
	for _, kind := range []string{"aggregate", "diagnostic", "action-status", "identity"} {
		t.Run(kind, func(t *testing.T) {
			c, f, o := fixtureTidyApply(t)
			if r := c.apply(context.Background(), o); r.Status != "ok" {
				t.Fatal(r)
			}
			s, err := openTidyStore(o.StateDir, true)
			if err != nil {
				t.Fatal(err)
			}
			var p TidyPlanRecord
			var j TidyJournal
			if err = s.read("tidy-plans", o.PlanID, &p); err != nil {
				t.Fatal(err)
			}
			if err = s.read("tidy-operations", o.PlanID, &j); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "aggregate":
				j.Entries[0].Actions = make([]ActionOutcome, observationLimit+1)
				for i := range j.Entries[0].Actions {
					j.Entries[0].Actions[i] = ActionOutcome{"delete-reference", "completed", "bounded individual action"}
				}
			case "diagnostic":
				j.Diagnostics = []Diagnostic{{Code: "", Message: strings.Repeat("x", 4097)}}
			case "action-status":
				j.Entries[0].Actions[0].Status = "invented"
			case "identity":
				j.Expected.Identity.RepositoryDir = "/foreign-repository"
			}
			if validTidyJournal(j, p) {
				t.Fatalf("invalid %s journal accepted", kind)
			}
			if err = s.write("tidy-operations", o.PlanID, j, false); err != nil {
				t.Fatal(err)
			}
			s.Close()
			r := c.apply(context.Background(), o)
			if r.Status == "ok" || f.pins != 1 || f.deletes != 1 || len(r.Actions) != 0 {
				t.Fatalf("malformed journal replayed or flattened: %+v", r)
			}
			if !strings.Contains(strings.Join(r.Limitations, " "), o.StateDir+"/tidy-operations/"+o.PlanID+".json") {
				t.Fatal("malformed state dropped recovery pointer")
			}
		})
	}
}

func TestTidyCancellationDuringInitialApplyObservationIsExplicit(t *testing.T) {
	c, f, o := fixtureTidyApply(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.onObserve = func(int) { cancel() }
	r := c.apply(ctx, o)
	if len(r.Diagnostics) == 0 || r.Diagnostics[0].Code != "canceled" || f.pins != 0 || f.deletes != 0 {
		t.Fatalf("cancellation hidden as stale evidence: %+v", r)
	}
}
