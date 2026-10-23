// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func v3StorePlan(t *testing.T) V3Plan {
	t.Helper()
	p := v3TestPlan()
	p.StateDir = t.TempDir()
	if e := os.Chmod(p.StateDir, 0700); e != nil {
		t.Fatal(e)
	}
	p.Effects = v3PlanEffects(p)
	p.Digest, _ = v3PlanDigest(p)
	return p
}
func TestV3StorePrivateImmutableAndNamespaceAdmission(t *testing.T) {
	p := v3StorePlan(t)
	s, e := openV3Store(p.StateDir, true, v3NativeRoots(v3TestNative(p)), v3TestNative(p).ProtectedRoots)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.writePlan(p, v3TestNative(p)); e != nil {
		t.Fatal(e)
	}
	if e = s.writePlan(p, v3TestNative(p)); e == nil {
		t.Fatal("immutable plan overwritten")
	}
	got, _, e := s.readPlan(p.PlanID)
	if e != nil || got.Digest != p.Digest {
		t.Fatalf("read %v", e)
	}
	if e = os.Mkdir(filepath.Join(p.StateDir, "tidy-plans"), 0700); e != nil {
		t.Fatal(e)
	}
	old := []byte(`{"old":"untouched"}`)
	path := filepath.Join(p.StateDir, "tidy-plans", p.PlanID+".json")
	if e = os.WriteFile(path, old, 0600); e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.readPlan(p.PlanID); e == nil {
		t.Fatal("cross-namespace collision admitted")
	}
	b, _ := os.ReadFile(path)
	if string(b) != string(old) {
		t.Fatal("legacy record changed")
	}
}
func TestV3StoreRejectsPrivateAndAllRootOverlap(t *testing.T) {
	for _, kind := range []string{"mode", "symlink", "protected", "registered"} {
		t.Run(kind, func(t *testing.T) {
			p := v3StorePlan(t)
			roots, protected := []string{"/repo"}, []string{}
			switch kind {
			case "mode":
				_ = os.Chmod(p.StateDir, 0755)
			case "symlink":
				alias := filepath.Join(t.TempDir(), "alias")
				if e := os.Symlink(p.StateDir, alias); e != nil {
					t.Fatal(e)
				}
				p.StateDir = alias
			case "protected":
				protected = []string{filepath.Join(p.StateDir, "active")}
			case "registered":
				roots = append(roots, filepath.Join(p.StateDir, "other"))
			}
			s, e := openV3Store(p.StateDir, true, roots, protected)
			if e == nil {
				s.Close()
				t.Fatal("unsafe store admitted")
			}
		})
	}
}
func TestV3StoreRejectsForgedRecordAndRecordLinks(t *testing.T) {
	p := v3StorePlan(t)
	native := v3TestNative(p)
	s, e := openV3Store(p.StateDir, true, v3NativeRoots(native), native.ProtectedRoots)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.writePlan(p, native); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(p.StateDir, "tidy-v3-plans", p.PlanID+".json")
	baseline, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	readable := func(t *testing.T) {
		t.Helper()
		if e := os.WriteFile(path, baseline, 0600); e != nil {
			t.Fatal(e)
		}
		got, observed, e := s.readPlan(p.PlanID)
		if e != nil || got.Digest != p.Digest || !reflect.DeepEqual(observed, native) {
			t.Fatalf("unmodified private-envelope control must be readable: %v", e)
		}
	}
	readable(t)
	cases := map[string]func([]byte) []byte{
		"unsupported-policy-with-valid-digests": func(b []byte) []byte {
			var record v3StoredPlan
			if e := json.Unmarshal(b, &record); e != nil {
				t.Fatal(e)
			}
			record.Plan.Policy = "other"
			record.Plan.Digest, _ = v3PlanDigest(record.Plan)
			record.Digest = v3PrivatePlanDigest(record.Plan, record.Native)
			bad, _ := json.Marshal(record)
			return bad
		},
		"duplicate-field": func(b []byte) []byte {
			return []byte(strings.Replace(string(b), `"schema_version":3`, `"schema_version":3,"schema_version":3`, 1))
		},
		"unknown-field": func(b []byte) []byte {
			return []byte(strings.Replace(string(b), `"schema_version":3`, `"schema_version":3,"extra":true`, 1))
		},
		"invalid-utf8": func(b []byte) []byte { return append([]byte{0xff}, b...) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			readable(t)
			bad := mutate(baseline)
			if string(bad) == string(baseline) {
				t.Fatal("negative fixture did not change its valid control")
			}
			if e := os.WriteFile(path, bad, 0600); e != nil {
				t.Fatal(e)
			}
			if _, _, e := s.readPlan(p.PlanID); e == nil {
				t.Fatal("single mutated private envelope admitted")
			}
		})
	}
	for _, kind := range []string{"symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			readable(t)
			other := filepath.Join(t.TempDir(), "valid-record")
			if e := os.WriteFile(other, baseline, 0600); e != nil {
				t.Fatal(e)
			}
			if e := os.Remove(path); e != nil {
				t.Fatal(e)
			}
			var e error
			if kind == "symlink" {
				e = os.Symlink(other, path)
			} else {
				e = os.Link(other, path)
			}
			if e != nil {
				t.Fatal(e)
			}
			if _, _, e = s.readPlan(p.PlanID); e == nil {
				t.Fatal("linked otherwise-valid private envelope admitted")
			}
			if e = os.Remove(path); e != nil {
				t.Fatal(e)
			}
		})
	}
	readable(t)
}
func TestV3JournalOrderingDurabilityAndRetry(t *testing.T) {
	p := v3StorePlan(t)
	s, e := openV3Store(p.StateDir, true, v3NativeRoots(v3TestNative(p)), v3TestNative(p).ProtectedRoots)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.writePlan(p, v3TestNative(p)); e != nil {
		t.Fatal(e)
	}
	j := newV3Journal(p, v3TestNative(p))
	if e = s.prepareJournal(p, j); e != nil {
		t.Fatal(e)
	}
	if e = validateV3Journal(j, p); e != nil {
		t.Fatal(e)
	}
	if e = verifyV3Retry(context.Background(), p, j, nil); e == nil {
		t.Fatal("prepared journal replay admitted")
	}
	next := v3CloneJournal(t, j)
	next.Phase = "complete"
	if e = s.advanceJournal(p, j, next); e == nil {
		t.Fatal("premature complete")
	}
	next = v3RecoveredJournal(t, p, j)
	injected := errors.New("sync failed")
	original := s.writeRecord
	s.writeRecord = func(string, string, any, bool) error { return injected }
	if e = s.advanceJournal(p, j, next); !errors.Is(e, injected) {
		t.Fatalf("durability failure hidden: %v", e)
	}
	s.writeRecord = original
	stored, e := s.readJournal(p)
	if e != nil || stored.Phase != "prepared" {
		t.Fatalf("durability failure advanced record: %v", e)
	}
	if e = s.advanceJournal(p, j, next); e != nil {
		t.Fatal(e)
	}
	j = next
	next = v3CloneJournal(t, j)
	next.Entries[0].Phase = "target-prepared"
	next.Entries[0].Transaction = "prepared"
	next.Phase = "target-prepared"
	next.KnownEffects = v3JournalEffects(p, next)
	if e = s.advanceJournal(p, j, next); e != nil {
		t.Fatal(e)
	}
	j = next
	next = v3CloneJournal(t, j)
	next.Entries[0].Transaction = "commit-attempted"
	next.KnownEffects = v3JournalEffects(p, next)
	if e = s.advanceJournal(p, j, next); e != nil {
		t.Fatal(e)
	}
	j = next
	next = v3CloneJournal(t, j)
	next.Entries[0].Phase = "action-complete"
	next.Entries[0].Transaction = "confirmed"
	next.Entries[0].Outcome = "complete"
	next.Phase = "action-complete"
	before := next.CurrentExpected
	state := v3ApplyStateDelta(p, next.CurrentNative, "reference-deleted", next.Entries[0].ActionID)
	after := v3SnapshotWithState(before, state)
	next.Entries[0].Deltas = append(next.Entries[0].Deltas, V3StateDelta{Kind: "reference-deleted", Before: before, After: after})
	next.CurrentNative = state
	next.CurrentExpected = after
	next.KnownEffects = v3JournalEffects(p, next)
	if e = s.advanceJournal(p, j, next); e != nil {
		t.Fatal(e)
	}
	j = next
	next = v3CloneJournal(t, j)
	next.Phase = "complete"
	if e = s.advanceJournal(p, j, next); e != nil {
		t.Fatal(e)
	}
	calls := 0
	verify := func(context.Context, V3Plan, V3Journal) error { calls++; return nil }
	if e = verifyV3Retry(context.Background(), p, next, verify); e != nil || calls != 1 {
		t.Fatalf("completed verification %d %v", calls, e)
	}
	if e = verifyV3Retry(context.Background(), p, next, func(context.Context, V3Plan, V3Journal) error { return errors.New("target recreated") }); e == nil {
		t.Fatal("recreated target admitted")
	}
	if e = s.advanceJournal(p, next, next); e == nil {
		t.Fatal("completed retry wrote journal")
	}
}
func v3CloneJournal(t *testing.T, j V3Journal) V3Journal {
	t.Helper()
	b, _ := json.Marshal(j)
	var n V3Journal
	if e := json.Unmarshal(b, &n); e != nil {
		t.Fatal(e)
	}
	return n
}
func v3RecoveredJournal(t *testing.T, p V3Plan, j V3Journal) V3Journal {
	t.Helper()
	n := v3CloneJournal(t, j)
	n.Phase = "recovery-confirmed"
	e := &n.Entries[0]
	e.Phase = "recovery-confirmed"
	e.RecoveryAttempted = true
	r := e.PlannedRecovery
	e.Recovery = &r
	before := n.CurrentExpected
	state := v3ApplyStateDelta(p, n.CurrentNative, "recovery-created", e.ActionID)
	after := v3SnapshotWithState(before, state)
	e.Deltas = append(e.Deltas, V3StateDelta{Kind: "recovery-created", Before: before, After: after})
	n.CurrentNative = state
	n.CurrentExpected = after
	n.KnownEffects = v3JournalEffects(p, n)
	return n
}

func v3IntentStages(t *testing.T, p V3Plan, initial V3Journal) []V3Journal {
	t.Helper()
	recovered := v3RecoveredJournal(t, p, initial)
	prepared := v3CloneJournal(t, recovered)
	prepared.Phase, prepared.Entries[0].Phase, prepared.Entries[0].Transaction = "target-prepared", "target-prepared", "prepared"
	prepared.KnownEffects = v3JournalEffects(p, prepared)
	intent := v3CloneJournal(t, prepared)
	intent.Entries[0].Transaction = "commit-attempted"
	intent.KnownEffects = v3JournalEffects(p, intent)
	return []V3Journal{initial, recovered, prepared, intent}
}

func v3TwoActionStorePlan(t *testing.T) (V3Plan, V3NativeState) {
	t.Helper()
	p := v3StorePlan(t)
	c := p.Candidates[0]
	c.ActionID, c.Name, c.OID = "action-2", "refs/heads/zulu", strings.Repeat("e", 40)
	c.Evidence = &V3ProofEvidence{Native: &V3AncestryProof{TargetOID: c.OID, BaseOID: p.Expected.BaseCommit}}
	p.Candidates, p.SelectedRefs = append(p.Candidates, c), append(p.SelectedRefs, c.Name)
	native := v3TestNative(p)
	native.References = append(native.References, V3Reference{Name: c.Name, OID: c.OID})
	p.Expected = v3SnapshotWithState(p.Expected, native)
	p.Effects = v3PlanEffects(p)
	p.Digest, _ = v3PlanDigest(p)
	return p, native
}

func TestV3JournalConfirmedAbortAfterDurableIntent(t *testing.T) {
	p, native := v3TwoActionStorePlan(t)
	s, err := openV3Store(p.StateDir, true, v3NativeRoots(native), native.ProtectedRoots)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.writePlan(p, native); err != nil {
		t.Fatal(err)
	}
	stages := v3IntentStages(t, p, newV3Journal(p, native))
	if err = s.prepareJournal(p, stages[0]); err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(stages); i++ {
		if err = s.advanceJournal(p, stages[i-1], stages[i]); err != nil {
			t.Fatal(err)
		}
	}
	intent := stages[len(stages)-1]
	unknown := v3CloneJournal(t, intent)
	unknown.Phase, unknown.Entries[0].Transaction = "incomplete", "unknown"
	unknown.Diagnostics = append(unknown.Diagnostics, Diagnostic{Code: "native-commit-unconfirmed", Message: "Native send, abort or completion is unconfirmed; retain possible effects and inspect manually."})
	unknown.KnownEffects = v3JournalEffects(p, unknown)
	if err = validateV3Journal(unknown, p); err != nil || !validV3Forward(intent, unknown, p) || !reflect.DeepEqual(unknown.KnownEffects, intent.KnownEffects) {
		t.Fatalf("unconfirmed intent lost uncertainty or possible deletion: %v", err)
	}
	aborted := v3CloneJournal(t, intent)
	aborted.Phase, aborted.Entries[0].Transaction = "incomplete", "aborted"
	aborted.Diagnostics = append(aborted.Diagnostics, Diagnostic{Code: "native-commit-aborted:" + aborted.Entries[0].ActionID, Message: "Native abort and process completion confirmed; COMMIT was not sent. Recovery remains; inspect manually, without replay."})
	aborted.KnownEffects = v3JournalEffects(p, aborted)
	if err = validateV3Journal(aborted, p); err != nil {
		t.Fatal(err)
	}
	if !validV3Forward(intent, aborted, p) {
		t.Fatal("confirmed no-send abort after durable commit intent cannot be recorded")
	}
	reused := v3CloneJournal(t, intent)
	reused.Diagnostics = append(reused.Diagnostics, aborted.Diagnostics[0])
	if validV3Forward(reused, aborted, p) {
		t.Fatal("existing diagnostic substituted for a new abort confirmation")
	}
	for name, mutate := range map[string]func(*V3Journal){
		"nonterminal":             func(j *V3Journal) { j.Phase = "target-prepared" },
		"missing-diagnostic":      func(j *V3Journal) { j.Diagnostics = []Diagnostic{} },
		"other-action-diagnostic": func(j *V3Journal) { j.Diagnostics[0].Code = "native-commit-aborted:action-2" },
		"unbound-diagnostic":      func(j *V3Journal) { j.Diagnostics[0].Code = "native-commit-aborted" },
		"additional-diagnostic": func(j *V3Journal) {
			j.Diagnostics = append(j.Diagnostics, Diagnostic{Code: "other", Message: "Another transition."})
		},
		"other-action":  func(j *V3Journal) { j.Entries[1].RecoveryAttempted = true },
		"entry-outcome": func(j *V3Journal) { j.Entries[0].Outcome = "unknown" },
		"recovery":      func(j *V3Journal) { j.Entries[0].Recovery.CommitID = strings.Repeat("e", 40) },
		"delta":         func(j *V3Journal) { j.Entries[0].Deltas[0].After.SafetyDigest = strings.Repeat("f", 64) },
		"native":        func(j *V3Journal) { j.CurrentNative.References = []V3Reference{} },
		"snapshot":      func(j *V3Journal) { j.CurrentExpected.SafetyDigest = strings.Repeat("f", 64) },
		"effects":       func(j *V3Journal) { j.KnownEffects = []string{} },
	} {
		t.Run(name, func(t *testing.T) {
			bad := v3CloneJournal(t, aborted)
			mutate(&bad)
			if validV3Forward(intent, bad, p) {
				t.Fatal("terminal abort admitted unrelated changes or unbound confirmation")
			}
		})
	}
	write := s.writeRecord
	injected := errors.New("terminal abort sync failed")
	s.writeRecord = func(string, string, any, bool) error { return injected }
	if err = s.advanceJournal(p, intent, aborted); !errors.Is(err, injected) {
		t.Fatalf("terminal abort durability failure hidden: %v", err)
	}
	s.writeRecord = write
	stored, err := s.readJournal(p)
	if err != nil || !reflect.DeepEqual(stored, intent) {
		t.Fatalf("failed persistence erased durable uncertainty: %v", err)
	}
	if err = s.advanceJournal(p, intent, aborted); err != nil {
		t.Fatal(err)
	}
	stored, err = s.readJournal(p)
	if err != nil || !reflect.DeepEqual(stored, aborted) {
		t.Fatalf("confirmed terminal abort did not roundtrip: %v", err)
	}
	want := []string{"recovery:" + aborted.Entries[0].PlannedRecovery.Name, "native-target-lock:" + aborted.Entries[0].ActionID}
	if !reflect.DeepEqual(stored.KnownEffects, want) {
		t.Fatalf("abort erased recovery/lock history or invented deletion: %q", stored.KnownEffects)
	}
	if err = verifyV3Retry(context.Background(), p, stored, func(context.Context, V3Plan, V3Journal) error { t.Fatal("abort replay reached verifier"); return nil }); err == nil {
		t.Fatal("terminal abort replay admitted")
	}
	if err = s.advanceJournal(p, stored, stored); err == nil {
		t.Fatal("terminal abort rewritten")
	}
}

func TestV3JournalSecondActionRecoveryAttemptIsDurable(t *testing.T) {
	p, native := v3TwoActionStorePlan(t)
	third := p.Candidates[1]
	third.ActionID, third.Name, third.OID = "action-3", "refs/heads/zz-third", strings.Repeat("b", 40)
	third.Evidence = &V3ProofEvidence{Native: &V3AncestryProof{TargetOID: third.OID, BaseOID: p.Expected.BaseCommit}}
	p.Candidates, p.SelectedRefs = append(p.Candidates, third), append(p.SelectedRefs, third.Name)
	native.References = append(native.References, V3Reference{Name: third.Name, OID: third.OID})
	p.Expected = v3SnapshotWithState(p.Expected, native)
	p.Effects = v3PlanEffects(p)
	p.Digest, _ = v3PlanDigest(p)
	s, err := openV3Store(p.StateDir, true, v3NativeRoots(native), native.ProtectedRoots)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.writePlan(p, native); err != nil {
		t.Fatal(err)
	}
	stages := v3IntentStages(t, p, newV3Journal(p, native))
	if err = s.prepareJournal(p, stages[0]); err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(stages); i++ {
		if err = s.advanceJournal(p, stages[i-1], stages[i]); err != nil {
			t.Fatal(err)
		}
	}
	prior := stages[len(stages)-1]
	premature := v3CloneJournal(t, prior)
	premature.Phase, premature.Entries[1].RecoveryAttempted = "prepared", true
	premature.KnownEffects = v3JournalEffects(p, premature)
	if validV3Forward(prior, premature, p) {
		t.Fatal("second recovery attempt overtook the unfinished first action")
	}
	complete := v3CloneJournal(t, prior)
	complete.Phase, complete.Entries[0].Phase, complete.Entries[0].Transaction, complete.Entries[0].Outcome = "action-complete", "action-complete", "confirmed", "complete"
	state := v3ApplyStateDelta(p, prior.CurrentNative, "reference-deleted", complete.Entries[0].ActionID)
	snapshot := v3SnapshotWithState(prior.CurrentExpected, state)
	complete.Entries[0].Deltas = append(complete.Entries[0].Deltas, V3StateDelta{Kind: "reference-deleted", Before: prior.CurrentExpected, After: snapshot})
	complete.CurrentNative, complete.CurrentExpected = state, snapshot
	complete.KnownEffects = v3JournalEffects(p, complete)
	if err = s.advanceJournal(p, prior, complete); err != nil {
		t.Fatal(err)
	}
	attempt := v3CloneJournal(t, complete)
	attempt.Phase, attempt.Entries[1].RecoveryAttempted = "prepared", true
	attempt.KnownEffects = v3JournalEffects(p, attempt)
	if err = s.advanceJournal(p, complete, attempt); err != nil {
		t.Fatalf("second action cannot durably record its recovery attempt before native execution: %v", err)
	}
	stored, err := s.readJournal(p)
	if err != nil || !reflect.DeepEqual(stored, attempt) {
		t.Fatalf("second recovery attempt did not roundtrip: %v", err)
	}
	if !reflect.DeepEqual(stored.Entries[0], complete.Entries[0]) || !reflect.DeepEqual(stored.CurrentNative, complete.CurrentNative) || stored.CurrentExpected != complete.CurrentExpected {
		t.Fatal("write-ahead attempt changed prior action or native evidence")
	}
	if !reflect.DeepEqual(stored.Entries[2], complete.Entries[2]) || stored.Entries[2].RecoveryAttempted {
		t.Fatal("untouched third action replaced the active second attempt")
	}
	erased := v3CloneJournal(t, stored)
	erased.Phase, erased.Entries[1].RecoveryAttempted = "action-complete", false
	erased.KnownEffects = v3JournalEffects(p, erased)
	if validV3Forward(stored, erased, p) {
		t.Fatal("second write-ahead attempt could be erased")
	}
	unknown := v3CloneJournal(t, stored)
	unknown.Phase = "incomplete"
	unknown.Diagnostics = append(unknown.Diagnostics, Diagnostic{Code: "recovery-ack-lost", Message: "Second recovery is unconfirmed; inspect its exact planned name without replay."})
	if err = s.advanceJournal(p, stored, unknown); err != nil {
		t.Fatal(err)
	}
	if verifyV3Retry(context.Background(), p, unknown, nil) == nil {
		t.Fatal("second-action recovery uncertainty replay admitted")
	}
}

func TestV3JournalRejectsFutureRecoveryAttemptsOnRead(t *testing.T) {
	for _, earlier := range []string{"untouched", "attempted", "commit-intent"} {
		t.Run(earlier, func(t *testing.T) {
			p, native := v3TwoActionStorePlan(t)
			s, err := openV3Store(p.StateDir, true, v3NativeRoots(native), native.ProtectedRoots)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if err = s.writePlan(p, native); err != nil {
				t.Fatal(err)
			}
			j := newV3Journal(p, native)
			if earlier == "attempted" {
				j.Entries[0].RecoveryAttempted = true
			} else if earlier == "commit-intent" {
				stages := v3IntentStages(t, p, j)
				j = stages[len(stages)-1]
			}
			j.KnownEffects = v3JournalEffects(p, j)
			if err = validateV3Journal(j, p); err != nil {
				t.Fatalf("unmodified readable control is invalid: %v", err)
			}
			if err = s.store.write("tidy-v3-operations", p.PlanID, j, true); err != nil {
				t.Fatal(err)
			}
			if _, err = s.readJournal(p); err != nil {
				t.Fatalf("unmodified control is unreadable: %v", err)
			}
			j.Entries[1].RecoveryAttempted = true
			j.Phase = "prepared"
			j.KnownEffects = v3JournalEffects(p, j)
			if validateV3Journal(j, p) == nil {
				t.Error("later recovery attempt admitted before earlier action completed")
			}
			if err = s.store.write("tidy-v3-operations", p.PlanID, j, false); err != nil {
				t.Fatal(err)
			}
			if _, err = s.readJournal(p); err == nil {
				t.Error("private read admitted an impossible future recovery attempt")
			}
		})
	}
}
func TestV3JournalRejectsForgedDeltaAndEffects(t *testing.T) {
	p := v3TestPlan()
	j := newV3Journal(p, v3TestNative(p))
	j = v3RecoveredJournal(t, p, j)
	if e := validateV3Journal(j, p); e != nil {
		t.Fatal(e)
	}
	for name, mutate := range map[string]func(*V3Journal){"effects": func(j *V3Journal) { j.KnownEffects = append(j.KnownEffects, "fetch") }, "recovery": func(j *V3Journal) { j.Entries[0].Recovery.Name = "refs/heads/main" }, "source": func(j *V3Journal) { j.CurrentExpected.Identity.CommitID = strings.Repeat("e", 40) }, "unrelated-ref": func(j *V3Journal) {
		j.CurrentNative.References = append(j.CurrentNative.References, V3Reference{Name: "refs/heads/injected", OID: strings.Repeat("e", 40)})
	}, "delta": func(j *V3Journal) { j.Entries[0].Deltas[0].After.SafetyDigest = strings.Repeat("e", 64) }} {
		t.Run(name, func(t *testing.T) {
			n := v3CloneJournal(t, j)
			mutate(&n)
			if validateV3Journal(n, p) == nil {
				t.Fatal("forged journal admitted")
			}
		})
	}
}

func TestV3JournalRecordsUncertainRecoveryWithoutInventingConfirmation(t *testing.T) {
	p := v3StorePlan(t)
	s, e := openV3Store(p.StateDir, true, v3NativeRoots(v3TestNative(p)), []string{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	native := v3TestNative(p)
	if e = s.writePlan(p, native); e != nil {
		t.Fatal(e)
	}
	j := newV3Journal(p, native)
	if e = s.prepareJournal(p, j); e != nil {
		t.Fatal(e)
	}
	n := v3CloneJournal(t, j)
	n.Phase = "incomplete"
	n.Entries[0].RecoveryAttempted = true
	n.Entries[0].Outcome = "unknown"
	n.KnownEffects = v3JournalEffects(p, n)
	n.Diagnostics = []Diagnostic{{Code: "recovery-ack-lost", Message: "Recovery creation may have occurred; inspect its fixed planned name."}}
	if e = s.advanceJournal(p, j, n); e != nil {
		t.Fatal(e)
	}
	if len(n.KnownEffects) != 1 || n.Entries[0].Recovery != nil {
		t.Fatal("uncertain recovery hidden or falsely confirmed")
	}
	if verifyV3Retry(context.Background(), p, n, nil) == nil {
		t.Fatal("uncertain recovery replay admitted")
	}
}
func v3LegacyTestNative(p V3Plan) V3NativeState {
	return V3NativeState{ProtectedRoots: p.Retirement.ProtectedRoots, References: []V3Reference{{Name: "refs/heads/main", OID: p.Expected.BaseCommit}, {Name: p.Retirement.Target.HeadRef, OID: p.Retirement.Target.CommitID}}, Registrations: []V3Registration{{Path: p.Retirement.Destination.Path, HeadOID: p.Retirement.Target.CommitID, HeadRef: p.Retirement.Target.HeadRef}, {Path: p.SourcePath, HeadOID: p.Expected.Identity.CommitID, HeadRef: p.Expected.Identity.HeadRef}}}
}

func TestV3RetirementRegistrationFactsBindPrivateStore(t *testing.T) {
	p := v3RegistrationGrantPlan()
	p.StateDir = t.TempDir()
	if err := os.Chmod(p.StateDir, 0700); err != nil {
		t.Fatal(err)
	}
	native := v3LegacyTestNative(p)
	p.Expected = v3SnapshotWithState(p.Expected, native)
	p.Digest, _ = v3PlanDigest(p)
	s, err := openV3Store(p.StateDir, true, v3NativeRoots(native), native.ProtectedRoots)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.writePlan(p, native); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(p.StateDir, "tidy-v3-plans", p.PlanID+".json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"parent", "siblings"} {
		t.Run(field, func(t *testing.T) {
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			stored, observed, err := s.readPlan(p.PlanID)
			if err != nil || !reflect.DeepEqual(stored, p) || !reflect.DeepEqual(observed, native) {
				t.Fatalf("complete private control failed to roundtrip: %v", err)
			}
			var envelope v3StoredPlan
			if err := json.Unmarshal(original, &envelope); err != nil {
				t.Fatal(err)
			}
			if field == "parent" {
				envelope.Plan.Retirement.RegistrationParent.Mode ^= 1
			} else {
				envelope.Plan.Retirement.RegistrationSiblings[0].CtimeNS++
			}
			envelope.Plan.Digest, _ = v3PlanDigest(envelope.Plan)
			mutated, _ := json.Marshal(envelope)
			if err := os.WriteFile(path, mutated, 0600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := s.readPlan(p.PlanID); err == nil {
				t.Fatal("new physical facts escaped private envelope digest after public digest recomputation")
			}
		})
	}
}
func TestV3RetirementJournalSeparatesRegistrationAndFiles(t *testing.T) {
	p := v3LegacyTestPlan()
	p.StateDir = t.TempDir()
	_ = os.Chmod(p.StateDir, 0700)
	native := v3LegacyTestNative(p)
	p.Expected = v3SnapshotWithState(p.Expected, native)
	p.Effects = v3PlanEffects(p)
	p.Digest, _ = v3PlanDigest(p)
	s, e := openV3Store(p.StateDir, true, v3NativeRoots(native), native.ProtectedRoots)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.writePlan(p, native); e != nil {
		t.Fatal(e)
	}
	j := newV3Journal(p, native)
	if e = s.prepareJournal(p, j); e != nil {
		t.Fatal(e)
	}
	n := v3RecoveredJournal(t, p, j)
	if e = s.advanceJournal(p, j, n); e != nil {
		t.Fatal(e)
	}
	j = n
	n = v3CloneJournal(t, j)
	n.Entries[0].RemovalAttempted = true
	n.KnownEffects = v3JournalEffects(p, n)
	if e = s.advanceJournal(p, j, n); e != nil {
		t.Fatal(e)
	}
	j = n
	uncertain := v3CloneJournal(t, j)
	uncertain.Phase = "incomplete"
	uncertain.Entries[0].RegistrationOutcome = "unknown"
	uncertain.Entries[0].FilesOutcome = "unknown"
	uncertain.KnownEffects = v3JournalEffects(p, uncertain)
	if e = validateV3Journal(uncertain, p); e != nil {
		t.Fatal(e)
	}
	if len(uncertain.KnownEffects) != 3 {
		t.Fatal("coupled uncertain native effects omitted")
	}
	n = v3CloneJournal(t, j)
	n.Phase = "registration-complete"
	n.Entries[0].Phase = n.Phase
	n.Entries[0].RegistrationOutcome = "complete"
	before := n.CurrentExpected
	state := v3ApplyStateDelta(p, n.CurrentNative, "registration-removed", n.Entries[0].ActionID)
	after := v3SnapshotWithState(before, state)
	n.CurrentExpected = after
	n.CurrentNative = state
	n.Entries[0].Deltas = append(n.Entries[0].Deltas, V3StateDelta{Kind: "registration-removed", Before: before, After: after})
	n.KnownEffects = v3JournalEffects(p, n)
	if e = s.advanceJournal(p, j, n); e != nil {
		t.Fatal(e)
	}
	j = n
	for _, phase := range []string{"files-complete", "action-complete", "complete"} {
		n = v3CloneJournal(t, j)
		n.Phase = phase
		if phase != "complete" {
			n.Entries[0].Phase = phase
			n.Entries[0].FilesOutcome = "complete"
			if phase == "action-complete" {
				n.Entries[0].Outcome = "complete"
			}
		}
		n.KnownEffects = v3JournalEffects(p, n)
		if e = s.advanceJournal(p, j, n); e != nil {
			t.Fatal(e)
		}
		j = n
	}
	journalPath := filepath.Join(p.StateDir, "tidy-v3-operations", p.PlanID+".json")
	beforeBytes, _ := os.ReadFile(journalPath)
	readOnly, readErr := openV3Store(p.StateDir, false, v3NativeRoots(j.CurrentNative), native.ProtectedRoots)
	if readErr != nil {
		t.Fatal(readErr)
	}
	defer readOnly.Close()
	if _, _, readErr = readOnly.readPlan(p.PlanID); readErr != nil {
		t.Fatalf("completed retirement cannot be read with current surviving registrations: %v", readErr)
	}
	if _, readErr = readOnly.readJournal(p); readErr != nil {
		t.Fatal(readErr)
	}
	afterBytes, _ := os.ReadFile(journalPath)
	if string(beforeBytes) != string(afterBytes) {
		t.Fatal("completed read-only retry rewrote durable journal")
	}
	if e = verifyV3Retry(context.Background(), p, j, func(_ context.Context, _ V3Plan, got V3Journal) error {
		if len(got.CurrentNative.Registrations) != 1 || got.CurrentNative.Registrations[0].Path != p.SourcePath {
			return errors.New("wrong surviving registration")
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}

func TestV3StoreRefusesOrphanOperationIdentityReuse(t *testing.T) {
	for _, kind := range []string{"tidy-operations", "tidy-retirement-operations", "tidy-v3-operations"} {
		t.Run(kind, func(t *testing.T) {
			p := v3StorePlan(t)
			s, e := openV3Store(p.StateDir, true, v3NativeRoots(v3TestNative(p)), []string{})
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			if kind != "tidy-v3-operations" {
				if e = os.Mkdir(filepath.Join(p.StateDir, kind), 0700); e != nil {
					t.Fatal(e)
				}
			}
			path := filepath.Join(p.StateDir, kind, p.PlanID+".json")
			original := []byte(`{"orphan":"preserved"}`)
			if e = os.WriteFile(path, original, 0600); e != nil {
				t.Fatal(e)
			}
			if e = s.writePlan(p, v3TestNative(p)); e == nil {
				t.Fatal("orphan operation ID was adopted")
			}
			got, _ := os.ReadFile(path)
			if string(got) != string(original) {
				t.Fatal("old operation changed")
			}
		})
	}
}

func v3OwnedModelFixture(t *testing.T) (V3Plan, V3NativeState, []string) {
	t.Helper()
	c, _, options := fixtureRetirement(t)
	oldResult := c.plan(context.Background(), options)
	if oldResult.Status != "ok" {
		t.Fatal(oldResult)
	}
	old := oldResult.Retirement
	physical := func(path string, device, inode uint64) V3PhysicalIdentity {
		return V3PhysicalIdentity{Path: path, Device: device, Inode: inode, UID: uint64(os.Geteuid()), OwnerKnown: true, Kind: "directory", Mode: 0700, Nlink: 2, Size: 1, MtimeNS: 1, CtimeNS: 1, TimestampPrecision: "nanosecond"}
	}
	p := v3LegacyTestPlan()
	p.PlanID = "v3-owned-plan"
	p.Mode = "owned-retirement"
	p.SourcePath = old.SourcePath
	p.StateDir = old.StateDir
	p.Expected = old.Expected.Expected
	r := p.Retirement
	r.RegisteredRoots = []string{old.Task.Destination, old.SourcePath}
	r.ProtectedRoots = []string{}
	r.Destination = physical(old.Task.Destination, old.Task.DestinationIdentity.Device, old.Task.DestinationIdentity.Inode)
	r.Parent = physical(filepath.Dir(old.Task.Destination), old.CreationPlan.DestinationParent.Device, old.CreationPlan.DestinationParent.Inode)
	r.Registration = physical(old.Task.Native.GitDir, 1, 3)
	r.RegistrationParent = physical(filepath.Dir(old.Task.Native.GitDir), 1, 4)
	r.Target = old.Task.Native
	r.RemoveFiles = old.Task.Destination
	r.Provenance = V3Provenance{Kind: "owned", Owned: &V3OwnedProvenance{CreationPlan: old.CreationPlan, Task: old.Task, CreationJournalDigest: old.CreationJournalDigest, LockReason: "stw:" + old.Task.TaskID}}
	for i := range r.Actions {
		r.Actions[i].Target = r.Destination.Path
	}
	r.Integration.OID = r.Target.CommitID
	r.Integration.Evidence.Native.TargetOID = r.Target.CommitID
	r.Integration.Evidence.Native.BaseOID = p.Expected.BaseCommit
	native := V3NativeState{ProtectedRoots: []string{}, References: []V3Reference{}, Registrations: []V3Registration{{Path: r.Destination.Path, HeadOID: r.Target.CommitID, HeadRef: r.Target.HeadRef, Locked: true, LockReason: r.Provenance.Owned.LockReason}, {Path: p.SourcePath, HeadOID: p.Expected.Identity.CommitID, HeadRef: p.Expected.Identity.HeadRef}}}
	r.Integration.WorkspacePaths = []string{}
	for _, registration := range native.Registrations {
		if registration.HeadOID == r.Target.CommitID {
			r.Integration.WorkspacePaths = append(r.Integration.WorkspacePaths, registration.Path)
		}
	}
	p.Expected = v3SnapshotWithState(p.Expected, native)
	p.Effects = v3PlanEffects(p)
	p.Digest, _ = v3PlanDigest(p)
	paths := []string{filepath.Join(p.StateDir, "plans", old.CreationPlan.PlanID+".json"), filepath.Join(p.StateDir, "tasks", old.Task.TaskID+".json"), filepath.Join(p.StateDir, "operations", old.CreationPlan.PlanID+".json")}
	return p, native, paths
}

func TestV3OwnedProvenanceReadsOriginalEvidenceWithoutMigration(t *testing.T) {
	p, native, paths := v3OwnedModelFixture(t)
	before := [][]byte{}
	for _, path := range paths {
		b, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		before = append(before, b)
	}
	s, e := openV3Store(p.StateDir, true, v3NativeRoots(native), native.ProtectedRoots)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.writePlan(p, native); e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.readPlan(p.PlanID); e != nil {
		t.Fatal(e)
	}
	for i, path := range paths {
		b, e := os.ReadFile(path)
		if e != nil || string(b) != string(before[i]) {
			t.Fatalf("old ownership changed: %s %v", filepath.Base(path), e)
		}
	}
	// A changed completed creation record invalidates v3 provenance. It is never
	// adopted under the current operator label or repaired by the v3 reader.
	var original map[string]any
	if e = json.Unmarshal(before[2], &original); e != nil {
		t.Fatal(e)
	}
	original["phase"] = "prepared"
	changed, _ := json.Marshal(original)
	if e = os.WriteFile(paths[2], changed, 0600); e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.readPlan(p.PlanID); e == nil {
		t.Fatal("changed original completed journal was adopted")
	}
}

func TestV3IncompleteRetryNeverInvokesVerifier(t *testing.T) {
	p := v3TestPlan()
	j := newV3Journal(p, v3TestNative(p))
	calls := 0
	e := verifyV3Retry(context.Background(), p, j, func(context.Context, V3Plan, V3Journal) error { calls++; return nil })
	if e == nil || calls != 0 {
		t.Fatalf("prepared operation reached completed verifier: calls=%d err=%v", calls, e)
	}
}
func TestV3JournalRejectsSkippedCommitAttemptAndRegression(t *testing.T) {
	p := v3StorePlan(t)
	native := v3TestNative(p)
	s, e := openV3Store(p.StateDir, true, v3NativeRoots(native), native.ProtectedRoots)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.writePlan(p, native); e != nil {
		t.Fatal(e)
	}
	j := newV3Journal(p, native)
	if e = s.prepareJournal(p, j); e != nil {
		t.Fatal(e)
	}
	n := v3RecoveredJournal(t, p, j)
	if e = s.advanceJournal(p, j, n); e != nil {
		t.Fatal(e)
	}
	recovered := n
	j = n
	n = v3CloneJournal(t, j)
	n.Phase = "target-prepared"
	n.Entries[0].Phase = n.Phase
	n.Entries[0].Transaction = "prepared"
	n.KnownEffects = v3JournalEffects(p, n)
	if e = s.advanceJournal(p, j, n); e != nil {
		t.Fatal(e)
	}
	j = n
	if e = s.advanceJournal(p, j, recovered); e == nil {
		t.Fatal("journal regressed to prior recovery phase")
	}
	n = v3CloneJournal(t, j)
	n.Phase = "action-complete"
	n.Entries[0].Phase = n.Phase
	n.Entries[0].Transaction = "confirmed"
	n.Entries[0].Outcome = "complete"
	before := n.CurrentExpected
	state := v3ApplyStateDelta(p, n.CurrentNative, "reference-deleted", n.Entries[0].ActionID)
	after := v3SnapshotWithState(before, state)
	n.CurrentNative = state
	n.CurrentExpected = after
	n.Entries[0].Deltas = append(n.Entries[0].Deltas, V3StateDelta{Kind: "reference-deleted", Before: before, After: after})
	n.KnownEffects = v3JournalEffects(p, n)
	if e = validateV3Journal(n, p); e != nil {
		t.Fatal(e)
	}
	if e = s.advanceJournal(p, j, n); e == nil {
		t.Fatal("confirmation skipped durable commit attempt")
	}
}

func TestV3JournalRefusesChangedImmutablePlanOnAdvance(t *testing.T) {
	p := v3StorePlan(t)
	native := v3TestNative(p)
	s, e := openV3Store(p.StateDir, true, v3NativeRoots(native), native.ProtectedRoots)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.writePlan(p, native); e != nil {
		t.Fatal(e)
	}
	j := newV3Journal(p, native)
	if e = s.prepareJournal(p, j); e != nil {
		t.Fatal(e)
	}
	n := v3RecoveredJournal(t, p, j)
	path := filepath.Join(p.StateDir, "tidy-v3-plans", p.PlanID+".json")
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	changed := strings.Replace(string(b), `"source_path":"/repo"`, `"source_path":"/replaced"`, 1)
	if changed == string(b) {
		t.Fatal("fixture not changed")
	}
	if e = os.WriteFile(path, []byte(changed), 0600); e != nil {
		t.Fatal(e)
	}
	if e = s.advanceJournal(p, j, n); e == nil {
		t.Fatal("journal advanced against changed immutable plan")
	}
}

func TestV3NativeStateBindsLiteralBaseReference(t *testing.T) {
	for _, selector := range []string{"refs/heads/otherbase", "refs/remotes/origin/main"} {
		for _, kind := range []string{"matching", "mismatch", "missing"} {
			t.Run(selector+"/"+kind, func(t *testing.T) {
				p := v3TestPlan()
				p.Expected.BaseSelector = selector
				n := v3TestNative(p)
				if kind != "missing" {
					oid := p.Expected.BaseCommit
					if kind == "mismatch" {
						oid = p.Candidates[0].OID
					}
					n.References = append(n.References, V3Reference{Name: selector, OID: oid})
					sort.Slice(n.References, func(i, j int) bool { return n.References[i].Name < n.References[j].Name })
				}
				p.Expected = v3SnapshotWithState(p.Expected, n)
				p.Digest, _ = v3PlanDigest(p)
				if e := validateV3Plan(p, p.StateDir, p.PlanID); e != nil {
					t.Fatal(e)
				}
				got := validV3NativeState(n, p, p.Expected)
				if got != (kind == "matching") {
					t.Fatalf("literal base binding %s admitted=%v", kind, got)
				}
			})
		}
	}
	for _, selector := range []string{"refs/tags/release", "refs/heads/main~1", "HEAD^{commit}"} {
		t.Run("native-resolution/"+selector, func(t *testing.T) {
			p := v3TestPlan()
			p.Expected.BaseSelector = selector
			n := v3TestNative(p)
			if selector == "refs/tags/release" {
				n.References = append(n.References, V3Reference{Name: selector, OID: strings.Repeat("e", 40)})
			}
			if selector == "refs/heads/main~1" {
				p.Expected.Identity.CommitID = strings.Repeat("e", 40)
				n.References[0].OID = p.Expected.Identity.CommitID
				n.Registrations[0].HeadOID = p.Expected.Identity.CommitID
			}
			p.Expected = v3SnapshotWithState(p.Expected, n)
			p.Digest, _ = v3PlanDigest(p)
			if !validV3NativeState(n, p, p.Expected) {
				t.Fatal("peeled tag/expression commit resolution was replaced with a raw-reference comparison")
			}
		})
	}
}

func TestV3RetirementCommitWorkspaceEvidenceMatchesNativeState(t *testing.T) {
	p := v3LegacyTestPlan()
	native := v3LegacyTestNative(p)
	p.Expected = v3SnapshotWithState(p.Expected, native)
	p.Digest, _ = v3PlanDigest(p)
	if !validV3NativeState(native, p, p.Expected) {
		t.Fatal("valid complete commit workspace evidence rejected")
	}
	p.Retirement.Integration.WorkspacePaths = []string{"/old", "/repo"}
	p.Digest, _ = v3PlanDigest(p)
	if validV3NativeState(native, p, p.Expected) {
		t.Fatal("commit workspace proof includes a registered path at a different commit")
	}
}

// Creation provenance stays immutable while a task makes normal detached commits.
// Retirement proof, registration snapshots and recovery bind its CURRENT head.
func TestV3OwnedProgressKeepsCurrentHeadAndOriginalAuthority(t *testing.T) {
	p, native, paths := v3OwnedModelFixture(t)
	original := map[string][]byte{}
	for _, path := range paths {
		raw, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		original[path] = raw
	}
	oldNative := p.Retirement.Provenance.Owned.Task.Native
	current := strings.Repeat("e", 40)
	p.Retirement.Target.CommitID = current
	p.Retirement.Integration.OID = current
	p.Retirement.Integration.Evidence.Native.TargetOID = current
	p.Retirement.Integration.WorkspacePaths = []string{p.Retirement.Destination.Path}
	for i := range native.Registrations {
		if native.Registrations[i].Path == p.Retirement.Destination.Path {
			native.Registrations[i].HeadOID = current
		}
	}
	p.Expected = v3SnapshotWithState(p.Expected, native)
	p.Effects = v3PlanEffects(p)
	p.Digest, _ = v3PlanDigest(p)
	if e := validateV3Plan(p, p.StateDir, p.PlanID); e != nil {
		t.Fatalf("stable owned progress rejected: %v", e)
	}
	if !validV3NativeState(native, p, p.Expected) {
		t.Fatal("current registration snapshot rejected")
	}
	if p.Retirement.Provenance.Owned.Task.Native != oldNative {
		t.Fatal("original native provenance changed")
	}
	j := newV3Journal(p, native)
	if len(j.Entries) != 1 || j.Entries[0].PlannedRecovery.CommitID != current || j.Entries[0].PlannedRecovery.CommitID == oldNative.CommitID {
		t.Fatal("recovery did not bind current task head")
	}
	store, e := openV3Store(p.StateDir, true, v3NativeRoots(native), native.ProtectedRoots)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	if e = store.writePlan(p, native); e != nil {
		t.Fatal(e)
	}
	if _, _, e = store.readPlan(p.PlanID); e != nil {
		t.Fatal(e)
	}
	for path, before := range original {
		after, e := os.ReadFile(path)
		if e != nil || string(after) != string(before) {
			t.Fatalf("creation record changed: %s %v", path, e)
		}
	}
	t.Run("original-completed-journal-digest", func(t *testing.T) {
		raw, _ := json.Marshal(p)
		var q V3Plan
		_ = json.Unmarshal(raw, &q)
		q.Retirement.Provenance.Owned.CreationJournalDigest = strings.Repeat("f", 64)
		if store.verifyOwned(q) == nil {
			t.Fatal("different original completed journal digest admitted")
		}
	})

	for name, mutate := range map[string]func(*V3Plan){
		"creation-workspace-name": func(q *V3Plan) { q.Retirement.Provenance.Owned.Task.Native.WorkspaceName = "foreign" },
		"creation-registration":   func(q *V3Plan) { q.Retirement.Provenance.Owned.Task.Native.GitDir += "-foreign" },
		"creation-repository":     func(q *V3Plan) { q.Retirement.Provenance.Owned.Task.Native.RepositoryDir += "-foreign" },
		"creation-destination":    func(q *V3Plan) { q.Retirement.Provenance.Owned.Task.Native.WorkspaceRoot += "-foreign" },
		"physical-destination":    func(q *V3Plan) { q.Retirement.Destination.Inode++ },
		"current-head-changed":    func(q *V3Plan) { q.Retirement.Target.CommitID = strings.Repeat("d", 40) },
		"stale-integration":       func(q *V3Plan) { q.Retirement.Integration.Evidence.Native.TargetOID = oldNative.CommitID },
		"foreign-lock":            func(q *V3Plan) { q.Retirement.Provenance.Owned.LockReason = "stw:foreign" },
	} {
		t.Run(name, func(t *testing.T) {
			raw, _ := json.Marshal(p)
			var q V3Plan
			_ = json.Unmarshal(raw, &q)
			mutate(&q)
			q.Effects = v3PlanEffects(q)
			q.Digest, _ = v3PlanDigest(q)
			if validateV3Plan(q, q.StateDir, q.PlanID) == nil {
				t.Fatal("changed authority admitted")
			}
		})
	}
	t.Run("stable-object-format", func(t *testing.T) {
		raw, _ := json.Marshal(p)
		var q V3Plan
		_ = json.Unmarshal(raw, &q)
		q.Retirement.Provenance.Owned.Task.Native.ObjectFormat = "sha256"
		q.Retirement.Provenance.Owned.Task.Native.CommitID = strings.Repeat("a", 64)
		q.Digest, _ = v3PlanDigest(q)
		if validateV3Plan(q, q.StateDir, q.PlanID) == nil {
			t.Fatal("changed original repository object format admitted")
		}
	})
	for _, change := range []string{"head", "lock"} {
		t.Run("native-"+change, func(t *testing.T) {
			n := cloneV3Native(native)
			for i := range n.Registrations {
				if n.Registrations[i].Path == p.Retirement.Destination.Path {
					if change == "head" {
						n.Registrations[i].HeadOID = oldNative.CommitID
					} else {
						n.Registrations[i].LockReason = "stw:foreign"
					}
				}
			}
			raw, _ := json.Marshal(p)
			var q V3Plan
			_ = json.Unmarshal(raw, &q)
			q.Expected = v3SnapshotWithState(q.Expected, n)
			q.Digest, _ = v3PlanDigest(q)
			if e := validateV3Plan(q, q.StateDir, q.PlanID); e != nil {
				t.Fatal("public control invalid", e)
			}
			if validV3NativeState(n, q, q.Expected) {
				t.Fatal("changed current registration admitted")
			}
		})
	}
}
