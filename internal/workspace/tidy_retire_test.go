// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRetirementRequiresExactCompletedCreationEvidence(t *testing.T) {
	c, _, o := fixtureCoordinator(t)
	planned := c.plan(context.Background(), o)
	if planned.Status != "ok" {
		t.Fatal(planned)
	}
	result := c.apply(context.Background(), ApplyOptions{StateDir: o.StateDir, PlanID: planned.Plan.PlanID, Authorize: true})
	if result.Status != "ok" {
		t.Fatal(result)
	}
	s, err := openRetirementStore(o.StateDir, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, task, digest, err := readRetirementOwnership(s, o.Task, o.Owner)
	if err != nil || task.TaskID != o.Task || p.PlanID != planned.Plan.PlanID || len(digest) != 64 {
		t.Fatalf("ownership %v", err)
	}
	if _, _, _, err = readRetirementOwnership(s, o.Task, "foreign-owner"); err == nil {
		t.Fatal("wrong owner accepted")
	}
	var j journal
	if err = s.read("operations", p.PlanID, &j); err != nil {
		t.Fatal(err)
	}
	j.Phase = "prepared"
	if err = s.write("operations", p.PlanID, j, false); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = readRetirementOwnership(s, o.Task, o.Owner); err == nil {
		t.Fatal("incomplete creation journal accepted")
	}
}
func TestRetirementCurrentWorkspaceProtectionUsesActualCWD(t *testing.T) {
	parent := privateDir(t)
	source := filepath.Join(parent, "source")
	target := filepath.Join(parent, "target")
	os.Mkdir(source, 0700)
	os.Mkdir(target, 0700)
	if err := protectRetirementWorkspace(source, target, target); err == nil {
		t.Fatal("explicit alternate source permitted deleting actual current directory")
	}
	if err := protectRetirementWorkspace(source, source, parent); err == nil {
		t.Fatal("source permitted as retirement target")
	}
	if err := protectRetirementWorkspace(source, target, source); err != nil {
		t.Fatal(err)
	}
}
func TestRetirementRecoveryPinsTargetIdentity(t *testing.T) {
	p := RetirementPlanRecord{PlanID: strings.Repeat("a", 32), Backend: "git", Task: TaskRecord{TaskID: "task"}, Expected: RetirementObservation{Target: NativeIdentity{CommitID: strings.Repeat("b", 40)}, Expected: Snapshot{Identity: NativeIdentity{CommitID: strings.Repeat("c", 40)}}}}
	if retirementRecoveryFor(p).CommitID != p.Expected.Target.CommitID {
		t.Fatal("source commit pinned instead of retired target")
	}
}

type fakeRetirementAdapter struct {
	recoveryPresent, registrationReused bool
	observation                         RetirementObservation
	state, plan                         string
	pins, registrations, files          int
	pinErr                              bool
	onPin                               func()
	prepared, confirmed                 bool
}

func (f *fakeRetirementAdapter) Observe(context.Context, string, string, TaskRecord, MutationPlan) (RetirementObservation, error) {
	return f.observation, nil
}
func (f *fakeRetirementAdapter) PrepareRecovery(_ context.Context, p RetirementPlanRecord) (TidyRecovery, TidyNativeOutcome, error) {
	f.pins++
	f.recoveryPresent = true
	s, err := openRetirementStore(f.state, false)
	if err == nil {
		var j RetirementJournal
		err = s.read("tidy-retirement-operations", f.plan, &j)
		s.Close()
		f.prepared = err == nil && j.Phase == "prepared" && j.PlannedRecovery == retirementRecoveryFor(p)
	}
	r := retirementRecoveryFor(p)
	f.observation.Expected.ReferenceDigest = strings.Repeat("4", 64)
	if f.onPin != nil {
		f.onPin()
	}
	snap := f.observation.Expected
	out := TidyNativeOutcome{Attempted: true, Complete: !f.pinErr, Effects: []string{"Created exact target recovery root " + r.Name}, Limitations: []string{}, Expected: &snap}
	if f.pinErr {
		return r, out, os.ErrPermission
	}
	return r, out, nil
}
func (f *fakeRetirementAdapter) RemoveRegistration(_ context.Context, p RetirementPlanRecord, _ TidyRecovery, _ Snapshot) (TidyNativeOutcome, error) {
	f.registrations++
	s, err := openRetirementStore(f.state, false)
	if err == nil {
		var j RetirementJournal
		err = s.read("tidy-retirement-operations", f.plan, &j)
		s.Close()
		f.confirmed = err == nil && j.Phase == "recovery-confirmed" && j.Recovery != nil
	}
	if err := os.Remove(p.Task.Destination); err != nil {
		return TidyNativeOutcome{}, err
	}
	f.observation.Expected.RegistrationDigest = strings.Repeat("5", 64)
	snap := f.observation.Expected
	return TidyNativeOutcome{Attempted: true, Complete: true, Effects: []string{"Removed exact owned registration and destination"}, Limitations: []string{}, Expected: &snap}, nil
}
func (f *fakeRetirementAdapter) RemoveFiles(_ context.Context, _ RetirementPlanRecord, _ TidyRecovery, _ Snapshot) (TidyNativeOutcome, error) {
	f.files++
	snap := f.observation.Expected
	return TidyNativeOutcome{Attempted: false, Complete: true, Effects: []string{"Verified exact destination absent after coupled native removal"}, Limitations: []string{}, Expected: &snap}, nil
}
func fixtureRetirement(t *testing.T) (retirementCoordinator, *fakeRetirementAdapter, TidyPlanOptions) {
	t.Helper()
	creation, native, o := fixtureCoordinator(t)
	planned := creation.plan(context.Background(), o)
	created := creation.apply(context.Background(), ApplyOptions{StateDir: o.StateDir, PlanID: planned.Plan.PlanID, Authorize: true})
	if created.Status != "ok" {
		t.Fatal(created)
	}
	registrationParent := filepath.Dir(created.Task.Native.GitDir)
	if err := os.MkdirAll(registrationParent, 0700); err != nil {
		t.Fatal(err)
	}
	destParent, err := currentIdentity(filepath.Dir(o.Destination))
	if err != nil {
		t.Fatal(err)
	}
	regParent, err := currentIdentity(registrationParent)
	if err != nil {
		t.Fatal(err)
	}
	manifest := RetirementManifest{Entries: []RetirementEntry{}, Complete: true}
	manifest.Digest = retirementManifestDigest(manifest)
	siblings := []RetirementSibling{}
	observation := RetirementObservation{Expected: native.snap, Target: created.Task.Native, Destination: created.Task.DestinationIdentity, DestinationParent: destParent, RegistrationParent: regParent, Manifest: manifest, Metadata: manifest, Siblings: siblings, SiblingDigest: retirementRecordDigest(siblings), LockReason: "stw:" + o.Task, WorkspacePaths: []string{o.Path, o.Destination}, WorkspacePathsComplete: true, Proof: "native-ancestry", Actions: []RetirementAction{{Name: "remove-registration", Target: o.Destination, Decision: "delete", Reason: "integrated", Proof: "native-ancestry", Effects: []string{}, Blockers: []string{}}, {Name: "remove-files", Target: o.Destination, Decision: "delete", Reason: "clean", Proof: "native-ancestry", Effects: []string{}, Blockers: []string{}}}, Limitations: []string{}}
	f := &fakeRetirementAdapter{observation: observation, state: o.StateDir}
	c := retirementCoordinator{inspect: creation.inspect, adapter: func(string) (retirementAdapter, error) { return f, nil }, sourceObserve: func(context.Context, string, string, string) (Snapshot, error) { return f.observation.Expected, nil }, cwd: func() (string, error) { return o.Path, nil }}
	return c, f, TidyPlanOptions{Path: o.Path, Base: o.Base, StateDir: o.StateDir, Task: o.Task, Owner: o.Owner, RemoveRegistration: true, RemoveFiles: o.Destination}
}
func TestRetirementPlanAndApplyDurableRecoveryAndVerifiedRetry(t *testing.T) {
	c, f, o := fixtureRetirement(t)
	r := c.plan(context.Background(), o)
	if r.Status != "ok" || r.Retirement == nil {
		t.Fatalf("plan %+v", r)
	}
	f.plan = r.Retirement.PlanID
	opts := TidyApplyOptions{StateDir: o.StateDir, PlanID: f.plan, Authorize: true}
	r = c.apply(context.Background(), opts)
	if r.Status != "ok" || !f.prepared || !f.confirmed || f.pins != 1 || f.registrations != 1 || f.files != 1 {
		t.Fatalf("apply %+v prepared%v confirmed%v", r, f.prepared, f.confirmed)
	}
	r = c.apply(context.Background(), opts)
	if r.Status != "ok" || f.registrations != 1 || f.pins != 1 {
		t.Fatalf("completed retry %+v", r)
	}
}
func TestRetirementPartialPinNeverRemovesOrReplays(t *testing.T) {
	c, f, o := fixtureRetirement(t)
	r := c.plan(context.Background(), o)
	if r.Status != "ok" {
		t.Fatal(r)
	}
	f.plan = r.Retirement.PlanID
	f.pinErr = true
	opts := TidyApplyOptions{StateDir: o.StateDir, PlanID: f.plan, Authorize: true}
	r = c.apply(context.Background(), opts)
	if r.Status != "partial" || f.registrations != 0 || f.pins != 1 {
		t.Fatalf("partial %+v", r)
	}
	r = c.apply(context.Background(), opts)
	if r.Status != "partial" || f.pins != 1 || f.registrations != 0 {
		t.Fatalf("blind retirement replay %+v", r)
	}
}
func TestRetirementBadStateNeverCreatesFiles(t *testing.T) {
	c, _, o := fixtureRetirement(t)
	o.StateDir = o.Path
	before, err := os.ReadDir(o.Path)
	if err != nil {
		t.Fatal(err)
	}
	r := c.plan(context.Background(), o)
	if r.Status == "ok" {
		t.Fatal("source state accepted")
	}
	after, err := os.ReadDir(o.Path)
	if err != nil || len(after) != len(before) {
		t.Fatal("unsafe source state gained files on refusal")
	}
}

func TestRetirementNativeGitPublicLifecycle(t *testing.T) {
	source, creation, task := gitRetirementFixture(t)
	r := PlanTidy(context.Background(), TidyPlanOptions{Path: source, Base: "HEAD", StateDir: creation.StateDir, Task: task.TaskID, Owner: task.Owner, RemoveRegistration: true, RemoveFiles: task.Destination})
	if r.Status != "ok" || r.SchemaVersion != 2 || r.Retirement == nil {
		t.Fatalf("public native plan %+v", r)
	}
	options := TidyApplyOptions{StateDir: creation.StateDir, PlanID: r.Retirement.PlanID, Authorize: true}
	r = ApplyTidy(context.Background(), options)
	if r.Status != "ok" {
		t.Fatalf("public native apply %+v", r)
	}
	r = ApplyTidy(context.Background(), options)
	if r.Status != "ok" {
		t.Fatalf("public native retry %+v", r)
	}
}

func (f *fakeRetirementAdapter) VerifyPost(context.Context, RetirementPlanRecord, TidyRecovery, Snapshot) error {
	if !f.recoveryPresent || f.registrationReused {
		return os.ErrNotExist
	}
	return nil
}
func TestRetirementRetryRequiresNativeRecoveryAndRegistrationProof(t *testing.T) {
	for _, kind := range []string{"missing-recovery", "reused-registration"} {
		t.Run(kind, func(t *testing.T) {
			c, f, o := fixtureRetirement(t)
			planned := c.plan(context.Background(), o)
			if planned.Status != "ok" {
				t.Fatal(planned)
			}
			f.plan = planned.Retirement.PlanID
			options := TidyApplyOptions{StateDir: o.StateDir, PlanID: f.plan, Authorize: true}
			if r := c.apply(context.Background(), options); r.Status != "ok" {
				t.Fatal(r)
			}
			if kind == "missing-recovery" {
				f.recoveryPresent = false
				f.observation.Expected.ReferenceDigest = strings.Repeat("6", 64)
			} else {
				f.registrationReused = true
				f.observation.Expected.RegistrationDigest = strings.Repeat("6", 64)
			}
			s, err := openRetirementStore(o.StateDir, true)
			if err != nil {
				t.Fatal(err)
			}
			var j RetirementJournal
			if err = s.read("tidy-retirement-operations", f.plan, &j); err != nil {
				t.Fatal(err)
			}
			j.ExpectedSnapshot = f.observation.Expected
			if err = s.write("tidy-retirement-operations", f.plan, j, false); err != nil {
				t.Fatal(err)
			}
			s.Close()
			r := c.apply(context.Background(), options)
			if r.Status == "ok" || f.pins != 1 || f.registrations != 1 {
				t.Fatalf("edited expected snapshot blessed invalid native recovery/registration state: %+v", r)
			}
		})
	}
}

type retirementAdapterWithoutPost struct{ retirementAdapter }

func TestRetirementMissingReadOnlyPostCapabilityRefusesBeforeEffects(t *testing.T) {
	c, f, o := fixtureRetirement(t)
	r := c.plan(context.Background(), o)
	if r.Status != "ok" {
		t.Fatal(r)
	}
	f.plan = r.Retirement.PlanID
	c.adapter = func(string) (retirementAdapter, error) { return retirementAdapterWithoutPost{f}, nil }
	r = c.apply(context.Background(), TidyApplyOptions{StateDir: o.StateDir, PlanID: f.plan, Authorize: true})
	if r.Status == "ok" || f.pins != 0 || f.registrations != 0 {
		t.Fatalf("missing required verification capability caused native effects: %+v", r)
	}
}
