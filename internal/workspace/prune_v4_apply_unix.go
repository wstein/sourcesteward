// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package workspace

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
)

type pruneV4Effect struct {
	Action  string `json:"action"`
	Target  string `json:"target"`
	Intent  string `json:"intent"`
	Outcome string `json:"outcome"`
}
type pruneV4Journal struct {
	SchemaVersion int             `json:"schema_version"`
	Operation     string          `json:"operation"`
	PlanID        string          `json:"plan_id"`
	PlanDigest    string          `json:"plan_digest"`
	Phase         string          `json:"phase"`
	PayloadDigest string          `json:"payload_digest"`
	HistoryRef    string          `json:"history_ref"`
	HistoryOID    string          `json:"history_oid"`
	Effects       []pruneV4Effect `json:"effects"`
	Remaining     []string        `json:"remaining"`
	Diagnostics   []Diagnostic    `json:"diagnostics"`
}

func pruneV4HistoryRef(p RegistrationPruneV4Plan) string {
	return "refs/stw/recovery/registration-prune/" + p.PlanID + "/head"
}

// ApplyRegistrationPruneV4 consumes only an explicitly versioned immutable plan.
// It never removes a checkout, unlocks a registration, or invokes broad Git prune.
func ApplyRegistrationPruneV4(ctx context.Context, o RegistrationPruneV4ApplyOptions) RegistrationPruneV4Result {
	r := pruneV4Result()
	r.Command = "tidy-apply"
	if !o.Authorize {
		return failPruneV4(r, failure("authorization-required", "Registration prune requires explicit authorization."))
	}
	p, e := ReadRegistrationPruneV4Plan(o.StateDir, o.PlanID)
	if e != nil {
		return failPruneV4(r, e)
	}
	r.Backend = "git"
	r.Plan = &p
	r.Limitations = append([]string{}, p.RecoveryLimits...)
	if ValidateV3RunMode(&o.Mode) != nil || !reflect.DeepEqual(o.Mode, p.Mode) {
		return failPruneV4(r, failure("prune-scope-refused", "Apply requires the stored caller and mode evidence."))
	}
	if p.Registration.Scope == "other" {
		if o.Mode.Mode != "human" || o.Confirm == nil || o.Confirm(p.Registration.Owner, p.PlanID) != nil {
			return failPruneV4(r, failure("prune-scope-refused", "Other-owner apply requires fresh typed owner and plan confirmation."))
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	ctx, _, closeSession, e := withPlanningSession(ctx)
	if e != nil {
		return failPruneV4(r, e)
	}
	defer closeSession()
	parent, e := openStoreKinds(p.StateDir, true, []string{pruneV4Namespace})
	if e != nil {
		return failPruneV4(r, e)
	}
	defer parent.Close()
	s, e := openStoreKinds(filepath.Join(p.StateDir, pruneV4Namespace), true, []string{"plans", "journals", "payloads"})
	if e != nil {
		return failPruneV4(r, e)
	}
	defer s.Close()
	fresh, e := ReadRegistrationPruneV4Plan(p.StateDir, p.PlanID)
	if e != nil || !reflect.DeepEqual(fresh, p) {
		return failPruneV4(r, failure("prune-plan-refused", "Stored plan changed under the private state lock."))
	}
	// Use #285's verified-journal derivation, without altering the saved baseline
	// or accepting an unexplained live state as a replacement baseline.
	chain, e := loadV3Chain(p.StateDir, V3Plan{PlanID: p.PlanID, Expected: p.Expected.SourceSnapshot}, p.Expected.Native, o.SiblingPlans)
	if e != nil {
		return failPruneV4(r, e)
	}
	expected := p.Expected
	expected.SourceSnapshot, expected.Native = chain.apply(expected.SourceSnapshot, expected.Native)
	if chain != nil {
		out := []PruneV4Sibling{}
		for _, v := range expected.ProtectedSiblings {
			if !chain.removedRegistrations[v.Identity.Path] {
				out = append(out, v)
			}
		}
		expected.ProtectedSiblings = out
	}
	j, e := readPruneV4Journal(s, p)
	if e == nil {
		if j.Phase != "complete" {
			r.Status = "partial"
			r.Diagnostics = []Diagnostic{{Code: "prune-incomplete", Message: "A prior prune attempt is incomplete; inspect its retained journal and recovery payload."}}
			return r
		}
		if e = verifyPruneV4Post(ctx, p, expected, j); e != nil {
			return failPruneV4(r, e)
		}
		r.Status = "ok"
		r.Actions = []ActionOutcome{{Name: "verified-replay", Status: "complete", Detail: "Verified the prior exact removal and retained recovery; no effect repeated."}}
		return r
	}
	if !errors.Is(e, os.ErrNotExist) {
		return failPruneV4(r, e)
	}
	opts := RegistrationPruneV4Options{Path: p.SourcePath, StateDir: p.StateDir, Name: p.Registration.Name, GitDir: p.Registration.GitDir, Mode: o.Mode}
	validate := func() error {
		if e := parent.validate(); e != nil {
			return e
		}
		if e := s.validate(); e != nil {
			return e
		}
		actual, reg, _, e := observePruneV4(ctx, opts, nil)
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(actual, expected) || reg != p.Registration {
			return failure("prune-evidence-changed", "Prune evidence differs from its immutable baseline and verified prior effects.")
		}
		return nil
	}
	if e = validate(); e != nil {
		return failPruneV4(r, e)
	}
	j = pruneV4Journal{4, "registration-prune", p.PlanID, p.Digest, "prepared", "", pruneV4HistoryRef(p), p.Expected.ResolvedHEAD, []pruneV4Effect{}, []string{"prepare-recovery", "pin-history", "remove-registration"}, []Diagnostic{}}
	if e = writePruneV4Record(s.dirs["journals"], p.PlanID+".json", j, true); e != nil {
		return failPruneV4(r, e)
	}
	stop := func(err error) RegistrationPruneV4Result {
		r.Status = "partial"
		r.Plan = &p
		code, message := tidyErrorCode(err), pruneV4Message(err)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			code, message = "canceled", "Registration prune canceled or deadline exceeded; retained journal and recovery describe possible effects."
		}
		r.Diagnostics = []Diagnostic{{Code: code, Message: message}}
		j.Phase = "partial"
		j.Diagnostics = r.Diagnostics
		// Preserve the last confirmed phase and pending intent if persistence fails.
		_ = writePruneV4Record(s.dirs["journals"], p.PlanID+".json", j, false)
		return r
	}
	j.PayloadDigest, e = preparePruneV4Payload(ctx, p, s)
	if e != nil {
		return stop(e)
	}
	if e = validate(); e != nil {
		return stop(e)
	}
	j.Phase = "prepared"
	j.Remaining = []string{"pin-history", "remove-registration"}
	j.Effects = append(j.Effects, pruneV4Effect{"pin-history", j.HistoryRef, "create-absent", "unknown"})
	if e = writePruneV4Record(s.dirs["journals"], p.PlanID+".json", j, false); e != nil {
		return stop(e)
	}
	if e = validate(); e != nil {
		return stop(e)
	}
	if e = pinPruneV4History(ctx, p); e != nil {
		return stop(e)
	}
	expected.Native = cloneV3Native(expected.Native)
	expected.Native.References = append(expected.Native.References, V3Reference{Name: j.HistoryRef, OID: j.HistoryOID})
	sort.Slice(expected.Native.References, func(i, k int) bool { return expected.Native.References[i].Name < expected.Native.References[k].Name })
	expected.SourceSnapshot = v3SnapshotWithState(expected.SourceSnapshot, expected.Native)
	if e = validate(); e != nil {
		return stop(e)
	}
	if _, digest, e := readPruneV4Payload(ctx, p); e != nil || digest != j.PayloadDigest {
		return stop(pruneV4PayloadError())
	}
	j.Phase = "recovery-confirmed"
	j.Effects[0].Outcome = "observed"
	j.Remaining = []string{"remove-registration"}
	if e = writePruneV4Record(s.dirs["journals"], p.PlanID+".json", j, false); e != nil {
		return stop(e)
	}
	if e = validate(); e != nil {
		return stop(e)
	}
	e = removePruneV4Metadata(ctx, p, expected, &j, s, parent)
	if e != nil {
		return stop(e)
	}
	j.Phase = "complete"
	j.Remaining = []string{}
	if e = verifyPruneV4Post(ctx, p, expected, j); e != nil {
		return stop(e)
	}
	if e = writePruneV4Record(s.dirs["journals"], p.PlanID+".json", j, false); e != nil {
		return stop(e)
	}
	r.Status = "ok"
	r.Actions = []ActionOutcome{{Name: "prepare-recovery", Status: "complete", Detail: "Verified complete private binary recovery and native history pin."}, {Name: "remove-registration", Status: "complete", Detail: "Removed only the exact missing-directory administrative registration; no checkout or branch changed."}}
	return r
}

func readPruneV4Journal(s *store, p RegistrationPruneV4Plan) (pruneV4Journal, error) {
	var j pruneV4Journal
	b, e := readPruneV4PrivateFile(s.dirs["journals"], p.PlanID+".json", int64(v3RecordLimit))
	if e != nil {
		if _, se := s.dirs["journals"].Lstat(p.PlanID + ".json"); errors.Is(se, os.ErrNotExist) {
			return j, os.ErrNotExist
		}
		return j, e
	}
	if decodeStrict(b, &j) != nil {
		return j, failure("prune-journal-refused", "Stored prune journal is incompatible.")
	}
	canonical, e := pruneV4Canonical(j, "")
	if e != nil || !bytes.Equal(b, append(canonical, '\n')) || j.SchemaVersion != 4 || j.Operation != "registration-prune" || j.PlanID != p.PlanID || j.PlanDigest != p.Digest || j.HistoryRef != pruneV4HistoryRef(p) || j.HistoryOID != p.Expected.ResolvedHEAD || j.Effects == nil || j.Remaining == nil || j.Diagnostics == nil {
		return j, failure("prune-journal-refused", "Stored prune journal does not match its exact plan.")
	}
	switch j.Phase {
	case "prepared", "recovery-confirmed", "removing", "complete", "partial":
	default:
		return j, failure("prune-journal-refused", "Stored prune journal has an unknown phase.")
	}
	for _, effect := range j.Effects {
		switch effect.Outcome {
		case "planned", "observed", "refused", "unknown":
		default:
			return j, failure("prune-journal-refused", "Stored prune journal has an unknown effect outcome.")
		}
		if j.Phase == "complete" && effect.Outcome != "observed" {
			return j, failure("prune-journal-refused", "Complete prune journal has an unconfirmed effect.")
		}
	}
	if j.Phase == "complete" && (len(j.Remaining) != 0 || len(j.Diagnostics) != 0 || !pruneV4ID(j.PayloadDigest, 64)) {
		return j, failure("prune-journal-refused", "Complete prune journal has incomplete recovery or remaining work.")
	}
	return j, nil
}
func pinPruneV4History(ctx context.Context, p RegistrationPruneV4Plan) error {
	bin, e := gitExecutable()
	if e != nil {
		return e
	}
	name := pruneV4HistoryRef(p)
	ro, e := gitTidyWriteProtection(p.Expected.SourceSnapshot.Identity, []string{name})
	if e != nil {
		return e
	}
	_, code, e := runSandboxed(ctx, bin, []string{"--no-replace-objects", "update-ref", "--no-deref", name, p.Expected.ResolvedHEAD, strings.Repeat("0", len(p.Expected.ResolvedHEAD))}, p.SourcePath, []string{p.Expected.SourceSnapshot.Identity.RepositoryDir}, ro)
	if e != nil {
		return gitExecutionError(e)
	}
	if code != 0 {
		return failure("recovery-unconfirmed", "Exact absent-name registration history pin was not confirmed.")
	}
	return nil
}

func verifyPruneV4Post(ctx context.Context, p RegistrationPruneV4Plan, expected PruneV4Expected, j pruneV4Journal) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if _, e := os.Lstat(p.Registration.GitDir); !errors.Is(e, os.ErrNotExist) {
		return failure("prune-postcondition-refused", "Exact administrative registration removal is not confirmed.")
	}
	if _, digest, e := readPruneV4Payload(ctx, p); e != nil || digest != j.PayloadDigest {
		return pruneV4PayloadError()
	}
	// A completed replay derives the one pinned ref if it was not already present.
	present := false
	for _, r := range expected.Native.References {
		if r.Name == j.HistoryRef {
			if r.OID != j.HistoryOID {
				return failure("prune-postcondition-refused", "History pin changed.")
			}
			present = true
		}
	}
	if !present {
		expected.Native = cloneV3Native(expected.Native)
		expected.Native.References = append(expected.Native.References, V3Reference{Name: j.HistoryRef, OID: j.HistoryOID})
		sort.Slice(expected.Native.References, func(i, k int) bool { return expected.Native.References[i].Name < expected.Native.References[k].Name })
	}
	kept := []V3Registration{}
	for _, r := range expected.Native.Registrations {
		if r.Path != p.Registration.Destination {
			kept = append(kept, r)
		}
	}
	expected.Native.Registrations = kept
	source := expected.SourceSnapshot.Identity
	actual, e := observeTidyV3Git(ctx, p.SourcePath, source.CommitID)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(actual.Native, expected.Native) || !reflect.DeepEqual(v3SnapshotWithState(actual.Expected, actual.Native), v3SnapshotWithState(expected.SourceSnapshot, expected.Native)) {
		return failure("prune-postcondition-refused", "Native state differs from the exact registration and history-pin deltas.")
	}
	common, e := observePruneV4Common(ctx, source.RepositoryDir)
	if e != nil {
		return e
	}
	if common != expected.CommonDirMount {
		return failure("prune-mount-changed", "Common-directory witness changed after removal.")
	}
	siblings, e := observePruneV4Siblings(ctx, source.RepositoryDir)
	if e != nil {
		return e
	}
	want := []PruneV4Sibling{}
	for _, s := range expected.ProtectedSiblings {
		if s.Name != p.Registration.Name {
			want = append(want, s)
		}
	}
	if !reflect.DeepEqual(siblings, want) {
		return failure("prune-postcondition-refused", "Protected administrative siblings changed.")
	}
	return revalidatePruneV4Absence(ctx, p)
}
func revalidatePruneV4Absence(ctx context.Context, p RegistrationPruneV4Plan) error {
	w, e := observePruneAbsence(ctx, p.Registration.Destination, p.Expected.Absence.Parent)
	if e != nil {
		return e
	}
	dirs := []PruneV4Directory{}
	for _, d := range w.Ancestors {
		dirs = append(dirs, pruneV4Directory(d))
	}
	if !reflect.DeepEqual(dirs, p.Expected.Absence.Ancestors) {
		return failure("prune-evidence-changed", "Destination absence or ancestor witness changed.")
	}
	return nil
}
