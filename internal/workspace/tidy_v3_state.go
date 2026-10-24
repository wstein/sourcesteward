// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"errors"
	"os"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"
)

// These complete records are private state evidence, not additional CLI fields.
// Their digests are bound by the public plan's expected snapshot.
type V3Reference struct {
	Name     string `json:"name"`
	OID      string `json:"oid"`
	Symbolic string `json:"symbolic"`
}
type V3Registration struct {
	Locked     bool   `json:"locked"`
	Bare       bool   `json:"bare"`
	Prunable   bool   `json:"prunable"`
	Path       string `json:"path"`
	HeadOID    string `json:"head_oid"`
	HeadRef    string `json:"head_ref"`
	LockReason string `json:"lock_reason"`
}
type V3NativeState struct {
	ProtectedRoots []string         `json:"protected_roots"`
	References     []V3Reference    `json:"references"`
	Registrations  []V3Registration `json:"registrations"`
}
type v3StoredPlan struct {
	Digest string        `json:"digest"`
	Plan   V3Plan        `json:"plan"`
	Native V3NativeState `json:"native"`
}
type V3StateDelta struct {
	Kind   string   `json:"kind"`
	Before Snapshot `json:"before"`
	After  Snapshot `json:"after"`
}
type V3JournalEntry struct {
	RecoveryAttempted   bool           `json:"recovery_attempted"`
	RemovalAttempted    bool           `json:"removal_attempted"`
	ActionID            string         `json:"action_id"`
	Phase               string         `json:"phase"`
	PlannedRecovery     TidyRecovery   `json:"planned_recovery"`
	Recovery            *TidyRecovery  `json:"recovery"`
	Transaction         string         `json:"transaction"`
	Outcome             string         `json:"outcome"`
	RegistrationOutcome string         `json:"registration_outcome"`
	FilesOutcome        string         `json:"files_outcome"`
	Deltas              []V3StateDelta `json:"deltas"`
	// Decision is the reviewer decision behind the entry's proof (FR-012).
	Decision *V3DecisionRecord `json:"decision,omitempty"`
	// Branch is the evidence recorded with the confirmed recovery and before the
	// branch deletion (Spec 031 T045). Journals written before it lack the field.
	Branch *V3BranchEvidence `json:"branch_evidence,omitempty"`
}

// V3RunMode is the mode evidence of an apply (Spec 041 FR-006g): whether the run was the human
// path or an agent, what the terminal and the environment showed (variable names, never values),
// and the caller label with its source. It is recorded once in the apply journal and shown by
// history. The field set mirrors the wizard's owner.Evidence JSON.
type V3RunMode struct {
	Mode         string   `json:"mode"`
	StdinTTY     bool     `json:"stdin_tty"`
	StdoutTTY    bool     `json:"stdout_tty"`
	Markers      []string `json:"markers"`
	Mine         bool     `json:"mine"`
	ByOwner      []string `json:"by_owner"`
	Caller       string   `json:"caller"`
	CallerSource string   `json:"caller_source"`
}

const (
	runModeListMax = 32
	runModeTextMax = 128
)

// validV3RunMode reports whether the run mode is absent or complete and bounded.
func validV3RunMode(m *V3RunMode) bool {
	if m == nil {
		return true
	}
	if m.Mode != "human" && m.Mode != "agent" || m.Markers == nil || m.ByOwner == nil || len(m.Markers) > runModeListMax || len(m.ByOwner) > runModeListMax {
		return false
	}
	short := func(s string, max int) bool { return len(s) <= max && validText(s) }
	for _, s := range append(append([]string{}, m.Markers...), m.ByOwner...) {
		if !short(s, runModeTextMax) {
			return false
		}
	}
	return short(m.Caller, runModeTextMax) && short(m.CallerSource, 64)
}

func cloneRunMode(m *V3RunMode) *V3RunMode {
	if m == nil {
		return nil
	}
	c := *m
	c.Markers, c.ByOwner = append([]string{}, m.Markers...), append([]string{}, m.ByOwner...)
	return &c
}

type V3Journal struct {
	PrivatePlanDigest string           `json:"private_plan_digest"`
	SchemaVersion     int              `json:"schema_version"`
	PlanID            string           `json:"plan_id"`
	PlanDigest        string           `json:"plan_digest"`
	Mode              string           `json:"mode"`
	Phase             string           `json:"phase"`
	InitialNative     V3NativeState    `json:"initial_native"`
	CurrentNative     V3NativeState    `json:"current_native"`
	CurrentExpected   Snapshot         `json:"current_expected"`
	Entries           []V3JournalEntry `json:"entries"`
	KnownEffects      []string         `json:"known_effects"`
	Diagnostics       []Diagnostic     `json:"diagnostics"`
	// RunMode is the mode evidence of the apply that wrote the journal (Spec 041); absent in
	// journals of other applies. It is fixed when the journal is prepared and never changes.
	RunMode *V3RunMode `json:"run_mode,omitempty"`
}
type v3Store struct {
	*store
	registered, protected []string
	// Per-instance fault seam exercises the same pre-effect durability boundary;
	// production always uses the descriptor-pinned store's fsynced write.
	writeRecord func(string, string, any, bool) error
}

var v3TidyPlanKinds = []string{"tidy-plans", "tidy-retirement-plans", "tidy-operations", "tidy-retirement-operations", "tidy-v3-plans"}

func openV3Store(path string, write bool, registered, protected []string) (*v3Store, error) {
	if !v3Paths(registered) || !v3Paths(protected) || len(registered) == 0 {
		return nil, failure("incomplete-protection", "Complete registered and protected workspace roots are required.")
	}
	for _, root := range append(append([]string{}, registered...), protected...) {
		if v3Overlaps(path, root) {
			return nil, failure("state-overlap", "Private v3 state overlaps a registered or protected workspace.")
		}
	}
	s, e := openStoreKinds(path, write, []string{"tidy-v3-plans", "tidy-v3-operations"})
	if e != nil {
		return nil, e
	}
	result := &v3Store{store: s, registered: append([]string{}, registered...), protected: append([]string{}, protected...)}
	result.writeRecord = s.write
	return result, nil
}
func (s *v3Store) admitID(id string) error {
	if !validID(id) {
		return failure("invalid-state", "Invalid v3 record identity.")
	}
	for _, kind := range v3TidyPlanKinds {
		if kind == "tidy-v3-plans" {
			continue
		}
		if e := checkOtherTidyNamespace(s.store, id, kind); e != nil {
			return e
		}
	}
	return nil
}
func (s *v3Store) writePlan(p V3Plan, native V3NativeState) error {
	if e := validateV3Plan(p, s.path, p.PlanID); e != nil {
		return e
	}
	if e := s.validate(); e != nil {
		return e
	}
	if _, e := s.dirs["tidy-v3-operations"].Lstat(p.PlanID + ".json"); e == nil {
		return failure("plan-collision", "An existing v3 operation already claims this immutable plan identity.")
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}

	if e := s.admitID(p.PlanID); e != nil {
		return e
	}
	if !reflect.DeepEqual(s.registered, v3NativeRoots(native)) || !reflect.DeepEqual(s.protected, native.ProtectedRoots) || !validV3NativeState(native, p, p.Expected) {
		return failure("invalid-state", "Private native state does not match the complete immutable plan snapshot.")
	}
	if p.Retirement != nil && p.Mode == "owned-retirement" {
		if e := s.verifyOwned(p); e != nil {
			return e
		}
	}
	record := v3StoredPlan{Plan: p, Native: native, Digest: v3PrivatePlanDigest(p, native)}
	if e := v3Bounds(record); e != nil {
		return e
	}
	return s.writeRecord("tidy-v3-plans", p.PlanID, record, true)
}

// Reading retains the original complete registration baseline. Current roots
// are checked for store disjointness at opening, then native coordinators compare
// the fresh full post-state with the journal (retirement removes one root).
func (s *v3Store) readPlan(id string) (V3Plan, V3NativeState, error) {
	var record v3StoredPlan
	if e := s.admitID(id); e != nil {
		return record.Plan, record.Native, e
	}
	if e := s.read("tidy-v3-plans", id, &record); e != nil {
		return record.Plan, record.Native, e
	}
	if e := validateV3Plan(record.Plan, s.path, id); e != nil {
		return record.Plan, record.Native, e
	}
	if record.Digest != v3PrivatePlanDigest(record.Plan, record.Native) || !reflect.DeepEqual(record.Native.ProtectedRoots, s.protected) || !validV3NativeState(record.Native, record.Plan, record.Plan.Expected) {
		return record.Plan, record.Native, failure("invalid-state", "Stored native snapshot or complete protection set changed.")
	}
	if e := v3Bounds(record); e != nil {
		return record.Plan, record.Native, e
	}
	if record.Plan.Mode == "owned-retirement" {
		if e := s.verifyOwned(record.Plan); e != nil {
			return record.Plan, record.Native, e
		}
	}
	return record.Plan, record.Native, nil
}

// Original ownership is reopened read-only. No old directory is created,
// repaired, migrated or adopted by a v3 read or retry.
func (s *v3Store) verifyOwned(p V3Plan) error {
	old, e := openStoreKinds(s.path, false, []string{"plans", "tasks", "operations"})
	if e != nil {
		return e
	}
	defer old.Close()
	o := p.Retirement.Provenance.Owned
	creation, task, digest, e := readRetirementOwnership(old, o.Task.TaskID, o.Task.Owner)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(creation, o.CreationPlan) || !reflect.DeepEqual(task, o.Task) || digest != o.CreationJournalDigest {
		return failure("changed-ownership", "Original owned creation evidence changed.")
	}
	return nil
}
func v3SnapshotWithState(snapshot Snapshot, state V3NativeState) Snapshot {
	snapshot.ReferenceDigest, _ = v3RecordDigest(state.References)
	snapshot.RegistrationDigest, _ = v3RecordDigest(state.Registrations)
	return snapshot
}
func validV3NativeState(state V3NativeState, p V3Plan, snapshot Snapshot) bool {
	if state.References == nil || state.Registrations == nil || !v3Paths(state.ProtectedRoots) || !reflect.DeepEqual(snapshot, v3SnapshotWithState(snapshot, state)) {
		return false
	}
	last := ""
	refs := map[string]V3Reference{}
	for _, r := range state.References {
		if r.Name <= last || !v3Ref(r.Name) || !v3OID(r.OID, p.Expected.Identity.ObjectFormat) || (r.Symbolic != "" && !v3Ref(r.Symbolic)) {
			return false
		}
		last = r.Name
		refs[r.Name] = r
	}
	// Literal branch/tracking selectors resolve to their observed reference OID.
	// Tags may contain tag-object OIDs, and revision expressions are resolved by
	// the native observer; their raw ref OIDs must not replace the peeled commit.
	selector := snapshot.BaseSelector
	if v3Ref(selector) && (strings.HasPrefix(selector, "refs/heads/") || strings.HasPrefix(selector, "refs/remotes/")) {
		base, ok := refs[selector]
		if !ok || base.OID != snapshot.BaseCommit {
			return false
		}
	}
	last = ""
	roots := []string{}
	for _, r := range state.Registrations {
		if (!r.Locked && r.LockReason != "") || r.Bare || r.Prunable || r.Path <= last || !v3CanonicalPath(r.Path) || !v3OID(r.HeadOID, p.Expected.Identity.ObjectFormat) || (r.HeadRef != "" && !v3Ref(r.HeadRef)) || !validText(r.LockReason) {
			return false
		}
		last = r.Path
		if r.Path == p.SourcePath && (r.HeadOID != p.Expected.Identity.CommitID || r.HeadRef != p.Expected.Identity.HeadRef) {
			return false
		}
		if r.HeadRef != "" {
			ref, ok := refs[r.HeadRef]
			if !ok || ref.OID != r.HeadOID {
				return false
			}
		}
		roots = append(roots, r.Path)
	}
	if !containsString(roots, p.SourcePath) {
		return false
	}

	for _, root := range append(append([]string{}, roots...), state.ProtectedRoots...) {
		if p.StateDir != "" && v3Overlaps(root, p.StateDir) {
			return false
		}
	}
	for _, c := range v3Eligible(p) {
		recovery := v3RecoveryFor(p, c.ActionID, c.OID)
		if _, exists := refs[recovery.Name]; exists {
			return false
		}
	}
	if p.Retirement != nil && (!reflect.DeepEqual(p.Retirement.RegisteredRoots, roots) || !reflect.DeepEqual(p.Retirement.ProtectedRoots, state.ProtectedRoots)) {
		return false
	}
	for _, r := range state.References {
		if r.Symbolic != "" {
			seen := map[string]bool{}
			current := r
			for current.Symbolic != "" {
				if seen[current.Name] {
					return false
				}
				seen[current.Name] = true
				next, ok := refs[current.Symbolic]
				if !ok || next.OID != r.OID {
					return false
				}
				current = next
			}
		}
	}
	for _, c := range p.Candidates {
		r, ok := refs[c.Name]
		if !ok || r.OID != c.OID {
			return false
		}
		if c.Decision == "delete" {
			if r.Symbolic != "" {
				return false
			}
			for _, v := range state.References {
				if v.Symbolic == c.Name {
					return false
				}
			}
			for _, registration := range state.Registrations {
				if registration.HeadOID == c.OID || registration.HeadRef == c.Name {
					return false
				}
			}
		}
	}
	if r := p.Retirement; r != nil {
		expectedPaths := []string{}
		for _, registration := range state.Registrations {
			if registration.HeadOID == r.Target.CommitID {
				expectedPaths = append(expectedPaths, registration.Path)
			}
		}
		recordedPaths := append([]string{}, r.Integration.WorkspacePaths...)
		sort.Strings(recordedPaths)
		if !reflect.DeepEqual(expectedPaths, recordedPaths) {
			return false
		}
	}
	if r := p.Retirement; r != nil {
		found := false
		for _, registration := range state.Registrations {
			if registration.Path == r.Destination.Path {
				found = registration.HeadOID == r.Target.CommitID && registration.HeadRef == r.Target.HeadRef
				if v3RetirementEligible(*r) && p.Mode == "legacy-retirement" && (registration.Locked || registration.LockReason != "") {
					return false
				}
				if p.Mode == "owned-retirement" && (!registration.Locked || registration.LockReason != r.Provenance.Owned.LockReason) {
					return false
				}
			}
		}
		if !found {
			return false
		}
	}
	return true
}
func v3Eligible(p V3Plan) []V3Candidate {
	out := []V3Candidate{}
	for _, c := range p.Candidates {
		if c.Decision == "delete" {
			out = append(out, c)
		}
	}
	if r := p.Retirement; r != nil && v3RetirementEligible(*r) {
		out = append(out, V3Candidate{ActionID: r.ActionID, OID: r.Target.CommitID})
	}
	return out
}
func newV3Journal(p V3Plan, native V3NativeState) V3Journal {
	return newV3JournalWithMode(p, native, nil)
}
func newV3JournalWithMode(p V3Plan, native V3NativeState, mode *V3RunMode) V3Journal {
	j := V3Journal{PrivatePlanDigest: v3PrivatePlanDigest(p, native), SchemaVersion: 3, PlanID: p.PlanID, PlanDigest: p.Digest, Mode: p.Mode, Phase: "prepared", InitialNative: cloneV3Native(native), CurrentNative: cloneV3Native(native), CurrentExpected: p.Expected, Entries: []V3JournalEntry{}, KnownEffects: []string{}, Diagnostics: []Diagnostic{}, RunMode: cloneRunMode(mode)}
	for _, c := range v3Eligible(p) {
		registration, files := "not-selected", "not-selected"
		if p.Retirement != nil {
			registration, files = "pending", "pending"
		}
		j.Entries = append(j.Entries, V3JournalEntry{ActionID: c.ActionID, Phase: "prepared", PlannedRecovery: v3RecoveryFor(p, c.ActionID, c.OID), Transaction: "none", Outcome: "pending", RegistrationOutcome: registration, FilesOutcome: files, Deltas: []V3StateDelta{}, Decision: decisionOf(c)})
	}
	return j
}
func cloneV3Native(s V3NativeState) V3NativeState {
	return V3NativeState{ProtectedRoots: append([]string{}, s.ProtectedRoots...), References: append([]V3Reference{}, s.References...), Registrations: append([]V3Registration{}, s.Registrations...)}
}

// Delta reconstruction has no caller-provided new OID, path, ref or namespace.
func v3ApplyStateDelta(p V3Plan, s V3NativeState, kind, id string) V3NativeState {
	n := cloneV3Native(s)
	var target V3Candidate
	for _, c := range v3Eligible(p) {
		if c.ActionID == id {
			target = c
		}
	}
	switch kind {
	case "recovery-created":
		r := v3RecoveryFor(p, id, target.OID)
		n.References = append(n.References, V3Reference{Name: r.Name, OID: r.CommitID})
		sort.Slice(n.References, func(i, j int) bool { return n.References[i].Name < n.References[j].Name })
	case "reference-deleted":
		out := []V3Reference{}
		for _, r := range n.References {
			if r.Name != target.Name {
				out = append(out, r)
			}
		}
		n.References = out
	case "registration-removed":
		out := []V3Registration{}
		for _, r := range n.Registrations {
			if p.Retirement == nil || r.Path != p.Retirement.Destination.Path {
				out = append(out, r)
			}
		}
		n.Registrations = out
	}
	return n
}
func v3JournalEffects(p V3Plan, j V3Journal) []string {
	effects := []string{}
	for _, e := range j.Entries {
		if e.Recovery == nil && e.RecoveryAttempted {
			effects = append(effects, "possible-recovery:"+e.PlannedRecovery.Name)
		}
		if e.Recovery != nil {
			effects = append(effects, "recovery:"+e.PlannedRecovery.Name)
		}
		if e.Transaction != "none" {
			effects = append(effects, "native-target-lock:"+e.ActionID)
		}
		if e.Transaction == "commit-attempted" || e.Transaction == "unknown" {
			effects = append(effects, "possible-reference-deletion:"+e.ActionID)
		}
		if e.Transaction == "confirmed" {
			effects = append(effects, "reference-deleted:"+e.ActionID)
		}
		if e.RegistrationOutcome == "complete" {
			effects = append(effects, "registration-removed:"+e.ActionID)
		} else if e.RegistrationOutcome == "unknown" || e.RemovalAttempted && e.RegistrationOutcome == "pending" {
			effects = append(effects, "possible-registration-removal:"+e.ActionID)
		}
		if e.FilesOutcome == "complete" {
			effects = append(effects, "files-removed:"+e.ActionID)
		} else if e.FilesOutcome == "unknown" || e.RemovalAttempted && e.FilesOutcome == "pending" {
			effects = append(effects, "possible-files-removal:"+e.ActionID)
		}
	}
	return effects
}
func validateV3Journal(j V3Journal, p V3Plan) error {
	bad := func() error {
		return failure("invalid-state", "V3 journal phases, recovery, effects or exact native deltas disagree with the immutable plan.")
	}
	if validateV3Plan(p, p.StateDir, p.PlanID) != nil || j.PrivatePlanDigest != v3PrivatePlanDigest(p, j.InitialNative) || j.SchemaVersion != 3 || j.PlanID != p.PlanID || j.PlanDigest != p.Digest || j.Mode != p.Mode || j.Entries == nil || j.KnownEffects == nil || j.Diagnostics == nil || !validV3RunMode(j.RunMode) || !validV3NativeState(j.InitialNative, p, p.Expected) || !reflect.DeepEqual(j.KnownEffects, v3JournalEffects(p, j)) {
		return bad()
	}
	targets := v3Eligible(p)
	if len(j.Entries) != len(targets) {
		return bad()
	}
	state := cloneV3Native(j.InitialNative)
	expected := p.Expected
	unfinished := false
	activePhase := "prepared"
	for i, e := range j.Entries {
		if e.ActionID != targets[i].ActionID || e.PlannedRecovery != v3RecoveryFor(p, e.ActionID, targets[i].OID) || e.Deltas == nil || !reflect.DeepEqual(e.Decision, decisionOf(targets[i])) {
			return bad()
		}
		if unfinished && (e.Phase != "prepared" || e.RecoveryAttempted) {
			return bad()
		}
		recovery := e.Phase != "prepared"
		if recovery {
			if !e.RecoveryAttempted || e.Recovery == nil || *e.Recovery != e.PlannedRecovery || len(e.Deltas) == 0 {
				return bad()
			}
		} else if e.Recovery != nil || len(e.Deltas) != 0 {
			return bad()
		}
		if e.Branch != nil && (!recovery || p.Mode != "local-refs" || !validBranchEvidence(*e.Branch, targets[i])) {
			return bad()
		}
		wantKinds := []string{}
		if recovery {
			wantKinds = append(wantKinds, "recovery-created")
		}
		switch e.Phase {
		case "prepared":
			if e.Transaction != "none" || (e.Outcome != "pending" && !(j.Phase == "incomplete" && e.Outcome == "unknown" && e.RecoveryAttempted)) {
				return bad()
			}
		case "recovery-confirmed":
			if e.Transaction != "none" || e.Outcome != "pending" {
				return bad()
			}
		case "target-prepared":
			if p.Retirement != nil || (e.Transaction != "prepared" && e.Transaction != "commit-attempted" && e.Transaction != "unknown" && e.Transaction != "aborted") || e.Outcome != "pending" {
				return bad()
			}
		case "registration-complete":
			if p.Retirement == nil || e.Transaction != "none" || e.RegistrationOutcome != "complete" || e.FilesOutcome != "pending" || e.Outcome != "pending" {
				return bad()
			}
			wantKinds = append(wantKinds, "registration-removed")
		case "files-complete":
			if p.Retirement == nil || e.Transaction != "none" || e.RegistrationOutcome != "complete" || e.FilesOutcome != "complete" || e.Outcome != "pending" {
				return bad()
			}
			wantKinds = append(wantKinds, "registration-removed")
		case "action-complete":
			if e.Outcome != "complete" {
				return bad()
			}
			if p.Retirement == nil {
				if e.Transaction != "confirmed" {
					return bad()
				}
				wantKinds = append(wantKinds, "reference-deleted")
			} else {
				if e.Transaction != "none" || e.RegistrationOutcome != "complete" || e.FilesOutcome != "complete" {
					return bad()
				}
				wantKinds = append(wantKinds, "registration-removed")
			}
		default:
			return bad()
		}
		if p.Retirement == nil && e.RemovalAttempted {
			return bad()
		}
		if e.RemovalAttempted && e.Recovery == nil {
			return bad()
		}
		if p.Retirement != nil && (e.Phase == "registration-complete" || e.Phase == "files-complete" || e.Phase == "action-complete") && !e.RemovalAttempted {
			return bad()
		}
		if p.Retirement == nil {
			if e.RegistrationOutcome != "not-selected" || e.FilesOutcome != "not-selected" {
				return bad()
			}
		} else if e.Phase == "prepared" || e.Phase == "recovery-confirmed" {
			if (e.RegistrationOutcome != "pending" || e.FilesOutcome != "pending") && !(j.Phase == "incomplete" && e.RemovalAttempted && e.RegistrationOutcome == "unknown" && e.FilesOutcome == "unknown") {
				return bad()
			}
		}
		if len(e.Deltas) != len(wantKinds) {
			return bad()
		}
		for k, kind := range wantKinds {
			d := e.Deltas[k]
			if d.Kind != kind || d.Before != expected {
				return bad()
			}
			next := v3ApplyStateDelta(p, state, kind, e.ActionID)
			if reflect.DeepEqual(state, next) {
				return bad()
			}
			after := v3SnapshotWithState(expected, next)
			if d.After != after {
				return bad()
			}
			state = next
			expected = after
		}
		if e.Phase != "prepared" || e.RecoveryAttempted {
			activePhase = e.Phase
		}
		if e.Phase != "action-complete" {
			unfinished = true
		}
	}
	if !reflect.DeepEqual(state, j.CurrentNative) || expected != j.CurrentExpected {
		return bad()
	}
	if j.Phase == "complete" {
		if unfinished || len(j.Diagnostics) > 0 {
			return bad()
		}
	} else if j.Phase != "incomplete" && j.Phase != activePhase {
		return bad()
	}
	for _, d := range j.Diagnostics {
		if d.Code == "" || d.Message == "" || !validText(d.Code) || !validText(d.Message) {
			return bad()
		}
	}
	return v3Bounds(j)
}
func (s *v3Store) prepareJournal(p V3Plan, j V3Journal) error {
	stored, native, e := s.readPlan(p.PlanID)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(stored, p) || !reflect.DeepEqual(j, newV3JournalWithMode(p, native, j.RunMode)) {
		return failure("invalid-state", "Initial journal must exactly match the immutable complete plan.")
	}
	if e = validateV3Journal(j, p); e != nil {
		return e
	}
	return s.writeRecord("tidy-v3-operations", p.PlanID, j, true)
}
func (s *v3Store) readJournal(p V3Plan) (V3Journal, error) {
	var j V3Journal
	stored, native, e := s.readPlan(p.PlanID)
	if e != nil {
		return j, e
	}
	if !reflect.DeepEqual(stored, p) {
		return j, failure("changed-plan", "Immutable v3 plan changed before journal observation.")
	}
	if e := s.admitID(p.PlanID); e != nil {
		return j, e
	}
	if e := s.read("tidy-v3-operations", p.PlanID, &j); e != nil {
		return j, e
	}
	if !reflect.DeepEqual(native, j.InitialNative) {
		return j, failure("changed-plan", "Journal no longer binds the stored private native baseline.")
	}
	return j, validateV3Journal(j, p)
}
func (s *v3Store) advanceJournal(p V3Plan, prior, next V3Journal) error {
	if e := validateV3Journal(prior, p); e != nil {
		return e
	}
	if e := validateV3Journal(next, p); e != nil {
		return e
	}
	current, e := s.readJournal(p)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(current, prior) || prior.Phase == "complete" || prior.Phase == "incomplete" {
		return failure("incomplete-operation", "Stale, complete or incomplete journals cannot be replayed or rewritten.")
	}
	if !validV3Forward(prior, next, p) {
		return failure("invalid-transition", "Only one recognized forward journal transition may be durably recorded.")
	}
	return s.writeRecord("tidy-v3-operations", p.PlanID, next, false)
}
func validV3Forward(before, after V3Journal, p V3Plan) bool {
	if before.PrivatePlanDigest != after.PrivatePlanDigest || before.SchemaVersion != after.SchemaVersion || before.PlanID != after.PlanID || before.PlanDigest != after.PlanDigest || before.Mode != after.Mode || !reflect.DeepEqual(before.RunMode, after.RunMode) || !reflect.DeepEqual(before.InitialNative, after.InitialNative) || len(before.Entries) != len(after.Entries) {
		return false
	}
	changed := -1
	for i := range before.Entries {
		if !reflect.DeepEqual(before.Entries[i], after.Entries[i]) {
			if changed != -1 {
				return false
			}
			changed = i
		}
	}
	if len(after.Diagnostics) < len(before.Diagnostics) {
		return false
	}
	for i := range before.Diagnostics {
		if before.Diagnostics[i] != after.Diagnostics[i] {
			return false
		}
	}
	if changed == -1 {
		return (after.Phase == "complete" || after.Phase == "incomplete") && reflect.DeepEqual(before.CurrentNative, after.CurrentNative) && before.CurrentExpected == after.CurrentExpected && reflect.DeepEqual(before.KnownEffects, after.KnownEffects)
	}
	for i := 0; i < changed; i++ {
		if before.Entries[i].Phase != "action-complete" {
			return false
		}
	}
	b, a := before.Entries[changed], after.Entries[changed]
	if b.Phase == "target-prepared" && b.Transaction == "commit-attempted" && a.Transaction == "aborted" {
		// The durable intent precedes the native send. Only an explicitly
		// confirmed no-send abort can retire that uncertainty, terminally.
		want := b
		want.Transaction = "aborted"
		return p.Retirement == nil && before.Phase == "target-prepared" && after.Phase == "incomplete" &&
			reflect.DeepEqual(a, want) && reflect.DeepEqual(before.CurrentNative, after.CurrentNative) &&
			before.CurrentExpected == after.CurrentExpected && reflect.DeepEqual(after.KnownEffects, v3JournalEffects(p, after)) &&
			len(after.Diagnostics) == len(before.Diagnostics)+1 &&
			after.Diagnostics[len(before.Diagnostics)].Code == "native-commit-aborted:"+b.ActionID
	}
	legal := false
	switch b.Phase {
	case "prepared":
		legal = a.Phase == "recovery-confirmed" || a.Phase == "prepared" && !b.RecoveryAttempted && a.RecoveryAttempted
	case "recovery-confirmed":
		if p.Retirement != nil {
			legal = a.Phase == "registration-complete" || a.Phase == "recovery-confirmed" && (!b.RemovalAttempted && a.RemovalAttempted || b.RemovalAttempted && after.Phase == "incomplete")
		} else {
			legal = a.Phase == "target-prepared" && a.Transaction == "prepared"
		}
	case "target-prepared":
		legal = a.Phase == "target-prepared" && b.Transaction == "prepared" && (a.Transaction == "commit-attempted" || a.Transaction == "aborted") || b.Transaction == "commit-attempted" && (a.Phase == "action-complete" && a.Transaction == "confirmed" || a.Phase == "target-prepared" && a.Transaction == "unknown")
	case "registration-complete":
		legal = a.Phase == "files-complete"
	case "files-complete":
		legal = a.Phase == "action-complete"
	}
	if b.RecoveryAttempted && !a.RecoveryAttempted || b.RemovalAttempted && !a.RemovalAttempted {
		return false
	}
	// Branch evidence arrives once, with the confirmed recovery, and never changes.
	if !(b.Phase == "prepared" && a.Phase == "recovery-confirmed") && !reflect.DeepEqual(a.Branch, b.Branch) {
		return false
	}
	if !legal || a.ActionID != b.ActionID || a.PlannedRecovery != b.PlannedRecovery || (b.Recovery != nil && !reflect.DeepEqual(a.Recovery, b.Recovery)) || len(a.Deltas) < len(b.Deltas) {
		return false
	}
	for i := range b.Deltas {
		if b.Deltas[i] != a.Deltas[i] {
			return false
		}
	}
	return after.Phase == a.Phase || after.Phase == "incomplete"
}

// Completed retries are observations only. The verifier must prove target
// absence, retained recovery and the complete exact native post-state. No store
// or native writer is passed to it. All other phases refuse automatic replay.
func verifyV3Retry(ctx context.Context, p V3Plan, j V3Journal, verify func(context.Context, V3Plan, V3Journal) error) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if e := validateV3Journal(j, p); e != nil {
		return e
	}
	if j.Phase != "complete" {
		return failure("incomplete-operation", "Prepared or uncertain operations require manual inspection; automatic replay is refused.")
	}
	if verify == nil {
		return failure("postcondition-required", "Completed retry requires fresh exact native postcondition verification.")
	}
	return verify(ctx, p, j)
}

// V3 records additionally reject invalid UTF-8, which encoding/json otherwise
// replaces with U+FFFD. The shared reader still enforces no-follow/private/EOF.
func (s *v3Store) read(kind, id string, value any) error {
	var raw jsonRawV3
	if e := s.store.read(kind, id, &raw); e != nil {
		return e
	}
	if !utf8.Valid(raw) {
		return failure("invalid-state", "V3 state is not valid UTF-8.")
	}
	return decodeV3Record(raw, value)
}

type jsonRawV3 []byte

func (r *jsonRawV3) UnmarshalJSON(b []byte) error { *r = append((*r)[:0], b...); return nil }

func v3NativeRoots(s V3NativeState) []string {
	roots := []string{}
	for _, r := range s.Registrations {
		roots = append(roots, r.Path)
	}
	return roots
}
func v3PrivatePlanDigest(p V3Plan, n V3NativeState) string {
	d, _ := v3RecordDigest(struct {
		Plan   V3Plan        `json:"plan"`
		Native V3NativeState `json:"native"`
	}{p, n})
	return d
}
