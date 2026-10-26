// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package workspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

type pruneV4RestoreJournal struct {
	pruneV4Journal
	RestoreID          string `json:"restore_id"`
	PruneJournalDigest string `json:"prune_journal_digest"`
}

func pruneV4RestoreError() error {
	return failure("prune-restore-refused", "Administrative restore requires an absent original registration and unchanged recovery, ownership and native state.")
}

// RestoreRegistrationPruneV4 restores only a completed prune's administrative
// metadata. Partial/ambiguous attempts remain inspectable but are not retried.
func RestoreRegistrationPruneV4(ctx context.Context, o RegistrationPruneV4RestoreOptions) RegistrationPruneV4Result {
	r := pruneV4Result()
	r.Command = "tidy-restore"
	r.Operation = "registration-restore"
	if o.Apply != o.Authorize {
		return failPruneV4(r, failure("authorization-required", "Administrative restore requires --apply and --authorize together."))
	}
	p, e := ReadRegistrationPruneV4Plan(o.StateDir, o.PlanID)
	if e != nil {
		return failPruneV4(r, e)
	}
	r.Backend = "git"
	r.Plan = &p
	r.Limitations = []string{"Restoring metadata does not recover missing objects or user files; the complete retained payload and history pin must remain available."}
	r.Limitations = append(r.Limitations, "Only administrative metadata is restored; checkout files, ownership, inodes and timestamps are not recreated. Native git worktree repair of a found checkout requires a separate owner action.")
	if o.Apply {
		if ValidateV3RunMode(&o.Mode) != nil || !reflect.DeepEqual(o.Mode, p.Mode) {
			return failPruneV4(r, failure("prune-scope-refused", "Restore requires the stored caller and mode evidence."))
		}
		if p.Registration.Scope == "other" && (o.Mode.Mode != "human" || o.Confirm == nil || o.Confirm(p.Registration.Owner, p.PlanID) != nil) {
			return failPruneV4(r, failure("prune-scope-refused", "Other-owner restore requires fresh typed owner and plan confirmation."))
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	ctx, _, closeSession, e := withPlanningSession(ctx)
	if e != nil {
		return failPruneV4(r, e)
	}
	defer closeSession()
	s, e := openStoreKinds(filepath.Join(p.StateDir, pruneV4Namespace), false, []string{"plans", "journals", "payloads"})
	if e != nil {
		return failPruneV4(r, e)
	}
	j, e := readPruneV4Journal(s, p)
	if e != nil {
		s.Close()
		return failPruneV4(r, e)
	}
	raw, e := readPruneV4PrivateFile(s.dirs["journals"], p.PlanID+".json", int64(pruneV4RecordLimit))
	if e != nil {
		s.Close()
		return failPruneV4(r, e)
	}
	journalDigest := pruneV4Hash("stw-registration-journal-file-v4", raw)
	prior, e := readPruneV4RestoreForPlan(s, p, journalDigest, j.PayloadDigest)
	s.Close()
	if e != nil {
		return failPruneV4(r, e)
	}
	if j.Phase != "complete" {
		return failPruneV4(r, failure("prune-restore-refused", "An incomplete prune requires manual inspection; automatic administrative restore is unavailable."))
	}
	if prior != nil {
		if e = validatePruneV4RestoreOwnership(p); e != nil {
			return failPruneV4(r, e)
		}
		if prior.Phase != "complete" {
			return failPruneV4(r, failure("prune-restore-refused", "A prior administrative restore is incomplete; inspect its retained journal before recovery."))
		}
		return failPruneV4(r, failure("prune-restore-refused", "Administrative restore already completed; the original registration slot is not eligible for another restore."))
	}

	validate := func() error { return validatePruneV4Restore(ctx, p, j, journalDigest) }
	if e = validate(); e != nil {
		return failPruneV4(r, e)
	}
	if !o.Apply {
		repair := pruneV4RestoreRepairArgv(p)
		r.Status = "ok"
		r.Actions = []ActionOutcome{{Name: "restore-registration", Status: "planned", Detail: "Restore the exact saved registration at " + p.Registration.GitDir + "; original checkout remains absent and locked. Repair argv data (separate owner authorization): " + repair}}
		return r
	}
	parent, e := openStoreKinds(p.StateDir, true, []string{pruneV4Namespace})
	if e != nil {
		return failPruneV4(r, e)
	}
	defer parent.Close()
	// Revalidate under the same top-level state lock as prune before creating a
	// restore namespace, intent, or native staging directory.
	if e = validate(); e != nil {
		return failPruneV4(r, e)
	}
	s, e = openStoreKinds(filepath.Join(p.StateDir, pruneV4Namespace), true, []string{"plans", "journals", "payloads", "restores"})
	if e != nil {
		return failPruneV4(r, e)
	}
	defer s.Close()
	again, e := readPruneV4RestoreForPlan(s, p, journalDigest, j.PayloadDigest)
	if e != nil || again != nil {
		return failPruneV4(r, pruneV4RestoreError())
	}
	id, e := randomID()
	if e != nil {
		return failPruneV4(r, e)
	}
	restore := pruneV4RestoreJournal{pruneV4Journal: pruneV4Journal{SchemaVersion: 4, Operation: "registration-restore", PlanID: p.PlanID, PlanDigest: p.Digest, Phase: "prepared", PayloadDigest: j.PayloadDigest, HistoryRef: j.HistoryRef, HistoryOID: j.HistoryOID, Effects: []pruneV4Effect{{"restore-registration", p.Registration.GitDir, "create-absent", "planned"}}, Remaining: []string{"restore-registration"}, Diagnostics: []Diagnostic{}}, RestoreID: id, PruneJournalDigest: journalDigest}
	if e = writePruneV4Record(s.dirs["restores"], id+".json", restore, true); e != nil {
		return failPruneV4(r, e)
	}
	stop := func(err error) RegistrationPruneV4Result {
		restore.Phase = "partial"
		restore.Remaining = []string{"restore-registration"}
		restore.Diagnostics = []Diagnostic{{Code: tidyErrorCode(err), Message: "Administrative restore failed; retained journal records confirmed and uncertain effects."}}
		var op operationError
		if errors.As(err, &op) {
			restore.Diagnostics[0].Message = op.Message
		}
		if errors.Is(err, context.Canceled) {
			restore.Diagnostics = []Diagnostic{{Code: "canceled", Message: "Administrative restore canceled; retained journal records possible effects."}}
		}
		if errors.Is(err, context.DeadlineExceeded) {
			restore.Diagnostics = []Diagnostic{{Code: "canceled", Message: "Administrative restore deadline exceeded; retained journal records possible effects."}}
		}
		_ = writePruneV4Record(s.dirs["restores"], id+".json", restore, false)
		r.Status = "partial"
		r.Diagnostics = restore.Diagnostics
		r.Actions = []ActionOutcome{{Name: "restore-registration", Status: "incomplete", Detail: "Inspect retained restore journal " + id + " and native staging path " + pruneV4RestoreStaging(p, id) + "; no automatic cleanup or retry."}}
		return r
	}
	root, rootID, e := openCanonical(p.Expected.SourceSnapshot.Identity.RepositoryDir)
	if e != nil {
		return stop(e)
	}
	defer root.Close()
	if rootID != p.Expected.CommonDirMount.Identity {
		return stop(pruneV4RestoreError())
	}
	name := filepath.Base(pruneV4RestoreStaging(p, id))
	if e = copyPruneV4Restore(ctx, p, root, name); e != nil {
		return stop(e)
	}
	restore.Phase = "restoring"
	restore.Effects[0].Outcome = "unknown"
	if e = writePruneV4Record(s.dirs["restores"], id+".json", restore, false); e != nil {
		return stop(e)
	}
	pruneObservationStep(ctx, "v4-restore-before-publish", p.Registration.GitDir, nil)
	if e = parent.validate(); e != nil {
		return stop(e)
	}
	if e = s.validate(); e != nil {
		return stop(e)
	}
	if e = validate(); e != nil {
		return stop(e)
	}
	if e = verifyPruneV4RestoredMetadata(ctx, p, pruneV4RestoreStaging(p, id), true); e != nil {
		return stop(e)
	}
	regParent, regID, e := openCanonical(filepath.Dir(p.Registration.GitDir))
	if e != nil {
		return stop(e)
	}
	defer regParent.Close()
	if regID != p.Expected.RegistrationParent {
		return stop(pruneV4RestoreError())
	}
	staged, stagedID, e := openCanonical(pruneV4RestoreStaging(p, id))
	if e != nil {
		return stop(e)
	}
	defer staged.Close()
	named, e := root.Lstat(name)
	held, heldErr := staged.Stat(".")
	if e != nil || heldErr != nil || !os.SameFile(named, held) || stagedID.Path != pruneV4RestoreStaging(p, id) {
		return stop(pruneV4RestoreError())
	}
	if e = publishPruneV4Restore(root, name, regParent, p.Registration.Name); e != nil {
		return stop(pruneV4RestoreError())
	}
	// Keep the staged root private until it has become the original slot.
	// Chmod through the held descriptor, so a replaced path is never adopted.
	for _, m := range p.Expected.Metadata {
		if m.Path == "." {
			if e = staged.Chmod(".", os.FileMode(m.Mode)); e != nil {
				return stop(e)
			}
		}
	}
	if e = syncDirectory(staged); e != nil {
		return stop(e)
	}
	if e = syncDirectory(root); e != nil {
		return stop(e)
	}
	if e = syncDirectory(regParent); e != nil {
		return stop(e)
	}
	pruneObservationStep(ctx, "v4-restore-before-post", p.Registration.GitDir, nil)
	if e = verifyPruneV4Restored(ctx, p, j); e != nil {
		return stop(e)
	}
	restore.Phase = "complete"
	restore.Effects[0].Outcome = "observed"
	restore.Remaining = []string{}
	pruneObservationStep(ctx, "v4-restore-before-complete", p.Registration.GitDir, &restore)
	if e = writePruneV4Record(s.dirs["restores"], id+".json", restore, false); e != nil {
		return stop(e)
	}
	r.Status = "ok"
	r.Actions = []ActionOutcome{{Name: "restore-registration", Status: "complete", Detail: "Restored the exact saved administrative registration; checkout remains missing and locked. Repair argv data (separate owner authorization): " + pruneV4RestoreRepairArgv(p)}}
	return r
}
func pruneV4RestoreStaging(p RegistrationPruneV4Plan, id string) string {
	return filepath.Join(p.Expected.SourceSnapshot.Identity.RepositoryDir, ".stw-registration-restore-"+id)
}
func validatePruneV4Restore(ctx context.Context, p RegistrationPruneV4Plan, j pruneV4Journal, digest string) error {
	if _, e := os.Lstat(p.Registration.GitDir); !errors.Is(e, os.ErrNotExist) {
		return pruneV4RestoreError()
	}
	if e := revalidatePruneV4Absence(ctx, p); e != nil {
		return e
	}
	fresh, e := ReadRegistrationPruneV4Plan(p.StateDir, p.PlanID)
	if e != nil || !reflect.DeepEqual(fresh, p) {
		return pruneV4RestoreError()
	}
	s, e := openStoreKinds(filepath.Join(p.StateDir, pruneV4Namespace), false, []string{"journals"})
	if e != nil {
		return e
	}
	defer s.Close()
	b, e := readPruneV4PrivateFile(s.dirs["journals"], p.PlanID+".json", int64(pruneV4RecordLimit))
	if e != nil || pruneV4Hash("stw-registration-journal-file-v4", b) != digest {
		return pruneV4RestoreError()
	}
	if e = validatePruneV4RestoreOwnership(p); e != nil {
		return e
	}
	id, e := currentIdentity(filepath.Dir(p.Registration.GitDir))
	if e != nil || id != p.Expected.RegistrationParent {
		return pruneV4RestoreError()
	}
	return verifyPruneV4Post(ctx, p, p.Expected, j)
}
func verifyPruneV4RestoredMetadata(ctx context.Context, p RegistrationPruneV4Plan, path string, privateStaging bool) error {
	actual, total, e := observePruneMetadata(ctx, path, defaultPruneMetadataLimits())
	if e != nil {
		return e
	}
	if len(actual) != len(p.Expected.Metadata) || total != p.Expected.MetadataBytes {
		return pruneV4RestoreError()
	}
	for i, m := range p.Expected.Metadata {
		a := actual[i]
		mode := m.Mode
		if privateStaging && m.Path == "." {
			mode = 0700
		}
		kind := a.Kind
		if kind == "regular" {
			kind = "file"
		}
		if a.Path != m.Path || kind != m.Kind || a.Digest != m.Digest || a.Size != m.Size || a.Mode != mode {
			return pruneV4RestoreError()
		}
	}
	return nil
}
func verifyPruneV4Restored(ctx context.Context, p RegistrationPruneV4Plan, j pruneV4Journal) error {
	if e := validatePruneV4RestoreOwnership(p); e != nil {
		return e
	}
	if e := revalidatePruneV4Absence(ctx, p); e != nil {
		return e
	}
	if _, digest, e := readPruneV4Payload(ctx, p); e != nil || digest != j.PayloadDigest {
		return pruneV4PayloadError()
	}
	if e := verifyPruneV4RestoredMetadata(ctx, p, p.Registration.GitDir, false); e != nil {
		return e
	}
	common, e := observePruneV4Common(ctx, p.Expected.SourceSnapshot.Identity.RepositoryDir)
	if e != nil {
		return e
	}
	if common != p.Expected.CommonDirMount {
		return failure("prune-mount-changed", "Common-directory witness changed during restore.")
	}
	siblings, e := observePruneV4Siblings(ctx, common.Identity.Path)
	if e != nil {
		return e
	}
	want := []PruneV4Sibling{}
	for _, x := range p.Expected.ProtectedSiblings {
		if x.Name != p.Registration.Name {
			want = append(want, x)
		}
	}
	got := []PruneV4Sibling{}
	for _, x := range siblings {
		if x.Name != p.Registration.Name {
			got = append(got, x)
		}
	}
	if !reflect.DeepEqual(want, got) {
		return pruneV4RestoreError()
	}
	expected := cloneV3Native(p.Expected.Native)
	expected.References = append(expected.References, V3Reference{Name: j.HistoryRef, OID: j.HistoryOID})
	sort.Slice(expected.References, func(i, k int) bool { return expected.References[i].Name < expected.References[k].Name })
	observed, e := observeTidyV3Git(ctx, p.SourcePath, p.Expected.SourceSnapshot.Identity.CommitID)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(observed.Native, expected) || !reflect.DeepEqual(v3SnapshotWithState(observed.Expected, observed.Native), v3SnapshotWithState(p.Expected.SourceSnapshot, expected)) {
		return pruneV4RestoreError()
	}
	return nil
}
func copyPruneV4Restore(ctx context.Context, p RegistrationPruneV4Plan, root *os.Root, name string) error {
	if _, _, e := readPruneV4Payload(ctx, p); e != nil {
		return e
	}
	if e := root.Mkdir(name, 0700); e != nil {
		return pruneV4RestoreError()
	}
	if e := root.Chmod(name, 0700); e != nil {
		return e
	}
	dst, e := root.OpenRoot(name)
	if e != nil {
		return e
	}
	defer dst.Close()
	src, _, e := openCanonical(filepath.Join(p.PayloadPath, "files"))
	if e != nil {
		return e
	}
	defer src.Close()
	for _, m := range p.Expected.Metadata {
		if e = ctx.Err(); e != nil {
			return e
		}
		if m.Kind == "absent" || m.Path == "." {
			continue
		}
		if m.Kind == "directory" {
			if e = dst.Mkdir(m.Path, 0700); e != nil {
				return e
			}
			continue
		}
		in, e := src.OpenFile(m.Path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if e != nil {
			return e
		}
		out, e := dst.OpenFile(m.Path, os.O_CREATE|os.O_EXCL|os.O_WRONLY|unix.O_NOFOLLOW, 0600)
		if e != nil {
			in.Close()
			return e
		}
		h := sha256.New()
		n, copyErr := io.CopyBuffer(io.MultiWriter(out, h), io.LimitReader(pruneContextReader{ctx, in}, m.Size+1), make([]byte, 64<<10))
		in.Close()
		if copyErr == nil && n == m.Size && hex.EncodeToString(h.Sum(nil)) == m.Digest {
			copyErr = out.Chmod(os.FileMode(m.Mode))
			if copyErr == nil {
				copyErr = out.Sync()
			}
		} else if copyErr == nil {
			copyErr = pruneV4PayloadError()
		}
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	for i := len(p.Expected.Metadata) - 1; i >= 0; i-- {
		m := p.Expected.Metadata[i]
		if m.Kind == "directory" {
			if m.Path != "." {
				if e = dst.Chmod(m.Path, os.FileMode(m.Mode)); e != nil {
					return e
				}
			}
			d, e := dst.OpenRoot(m.Path)
			if e != nil {
				return e
			}
			e = syncDirectory(d)
			d.Close()
			if e != nil {
				return e
			}
		}
	}
	return syncDirectory(root)
}
func readPruneV4RestoreForPlan(s *store, p RegistrationPruneV4Plan, pruneDigest, payloadDigest string) (*pruneV4RestoreJournal, error) {
	st, e := s.root.Lstat("restores")
	if errors.Is(e, os.ErrNotExist) {
		return nil, nil
	}
	if e != nil || !st.IsDir() || st.Mode().Perm() != 0700 || !privateOwner(st) {
		return nil, pruneV4RestoreError()
	}
	dir, e := s.root.OpenRoot("restores")
	if e != nil {
		return nil, e
	}
	defer dir.Close()
	held, e := dir.Stat(".")
	if e != nil || !os.SameFile(st, held) {
		return nil, pruneV4RestoreError()
	}
	f, e := dir.Open(".")
	if e != nil {
		return nil, e
	}
	names, e := f.ReadDir(v3NativeFactsLimit + 1)
	f.Close()
	if e != nil && !errors.Is(e, io.EOF) {
		return nil, e
	}
	if len(names) > v3NativeFactsLimit {
		return nil, failure("record-limit", "Administrative restore history exceeds its scan bound.")
	}
	var found *pruneV4RestoreJournal
	total := 0
	for _, n := range names {
		id := strings.TrimSuffix(n.Name(), ".json")
		if !pruneV4ID(id, 32) || n.Name() != id+".json" {
			return nil, pruneV4RestoreError()
		}
		raw, e := readPruneV4PrivateFile(dir, n.Name(), int64(pruneV4RecordLimit))
		if e != nil {
			return nil, e
		}
		total += len(raw)
		if total > 4<<20 {
			return nil, failure("record-limit", "Administrative restore history exceeds its byte bound.")
		}
		var j pruneV4RestoreJournal
		if decodeStrict(raw, &j) != nil {
			return nil, pruneV4RestoreError()
		}
		canonical, e := pruneV4Canonical(j, "")
		if e != nil || !bytes.Equal(raw, append(canonical, '\n')) || j.SchemaVersion != 4 || j.Operation != "registration-restore" || j.RestoreID != id || !pruneV4ID(j.PlanID, 32) || !pruneV4ID(j.PlanDigest, 64) || !pruneV4ID(j.PruneJournalDigest, 64) || !pruneV4ID(j.PayloadDigest, 64) || j.Effects == nil || j.Remaining == nil || j.Diagnostics == nil {
			return nil, pruneV4RestoreError()
		}
		if j.Phase != "prepared" && j.Phase != "restoring" && j.Phase != "complete" && j.Phase != "partial" {
			return nil, pruneV4RestoreError()
		}

		if len(j.Effects) != 1 || j.HistoryRef != "refs/stw/recovery/registration-prune/"+j.PlanID+"/head" || !(pruneV4ID(j.HistoryOID, 40) || pruneV4ID(j.HistoryOID, 64)) {
			return nil, pruneV4RestoreError()
		}
		effect := j.Effects[0]
		if effect.Action != "restore-registration" || !v3CanonicalPath(effect.Target) || effect.Intent != "create-absent" {
			return nil, pruneV4RestoreError()
		}
		if effect.Outcome != "planned" && effect.Outcome != "unknown" && effect.Outcome != "observed" && effect.Outcome != "refused" {
			return nil, pruneV4RestoreError()
		}
		if j.Phase == "complete" {
			if effect.Outcome != "observed" || len(j.Remaining) != 0 || len(j.Diagnostics) != 0 {
				return nil, pruneV4RestoreError()
			}
		} else if !reflect.DeepEqual(j.Remaining, []string{"restore-registration"}) {
			return nil, pruneV4RestoreError()
		}
		if j.PlanID != p.PlanID {
			continue
		}
		if j.PayloadDigest != payloadDigest {
			return nil, pruneV4RestoreError()
		}

		if found != nil || j.PlanDigest != p.Digest || j.PruneJournalDigest != pruneDigest || j.HistoryRef != pruneV4HistoryRef(p) || j.HistoryOID != p.Expected.ResolvedHEAD || len(j.Effects) != 1 {
			return nil, pruneV4RestoreError()
		}
		effect = j.Effects[0]
		if effect.Action != "restore-registration" || effect.Target != p.Registration.GitDir || effect.Intent != "create-absent" {
			return nil, pruneV4RestoreError()
		}
		if effect.Outcome != "planned" && effect.Outcome != "unknown" && effect.Outcome != "observed" && effect.Outcome != "refused" {
			return nil, pruneV4RestoreError()
		}
		if j.Phase == "complete" {
			if effect.Outcome != "observed" || len(j.Remaining) != 0 || len(j.Diagnostics) != 0 {
				return nil, pruneV4RestoreError()
			}
		} else if !reflect.DeepEqual(j.Remaining, []string{"restore-registration"}) {
			return nil, pruneV4RestoreError()
		}
		found = &j
	}
	return found, nil
}

func validatePruneV4RestoreOwnership(p RegistrationPruneV4Plan) error {
	owner, e := openStore(p.StateDir, false)
	if e != nil {
		return e
	}
	creation, task, jd, e := readRetirementOwnership(owner, p.Registration.Task, p.Registration.Owner)
	owner.Close()
	if e != nil {
		return failure("prune-ownership-refused", "Original creation ownership is unavailable before restore.")
	}
	td, e := v3RecordDigest(task)
	if e != nil || td != p.Expected.TaskDigest || creation.Digest != p.Expected.CreationDigest || jd != p.Expected.CreationJournalDigest {
		return failure("prune-ownership-refused", "Original creation ownership changed before restore.")
	}
	return nil
}

func pruneV4RestoreRepairArgv(p RegistrationPruneV4Plan) string {
	var data bytes.Buffer
	encoder := json.NewEncoder(&data)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode([]string{"git", "-C", p.SourcePath, "worktree", "repair", "<owner-selected-found-path>"})
	return strings.TrimSuffix(data.String(), "\n")
}
