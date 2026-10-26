// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func restoreV4Fixture(t *testing.T) RegistrationPruneV4Plan {
	t.Helper()
	_, p := fixturePruneV4(t)
	pruneV4GuardApply(t, p)
	return p
}
func restoreV4Options(p RegistrationPruneV4Plan) RegistrationPruneV4RestoreOptions {
	return RegistrationPruneV4RestoreOptions{StateDir: p.StateDir, PlanID: p.PlanID, Mode: p.Mode}
}
func TestRegistrationPruneV4RestoreInspectDoesNotWrite(t *testing.T) {
	p := restoreV4Fixture(t)
	before := pruneV4GuardFiles(t, p.StateDir)
	r := RestoreRegistrationPruneV4(context.Background(), restoreV4Options(p))
	if r.Status != "ok" || r.Command != "tidy-restore" || r.Operation != "registration-restore" {
		t.Fatalf("inspect: %+v", r)
	}
	if !reflect.DeepEqual(before, pruneV4GuardFiles(t, p.StateDir)) {
		t.Fatal("inspection changed state")
	}
	if _, e := os.Lstat(p.Registration.GitDir); !os.IsNotExist(e) {
		t.Fatal("inspection recreated registration", e)
	}
}
func TestRegistrationPruneV4RestoreRawMetadataAndRepairMovedCheckout(t *testing.T) {
	_, p := fixturePruneV4(t)
	original := pruneV4GuardFiles(t, p.Registration.GitDir)
	moved := p.Registration.Destination + "-moved"
	index, e := os.ReadFile(filepath.Join(p.Registration.GitDir, "index"))
	pruneMust(t, e)
	pruneV4GuardApply(t, p)
	o := restoreV4Options(p)
	o.Apply, o.Authorize = true, true
	r := RestoreRegistrationPruneV4(context.Background(), o)
	if r.Status != "ok" {
		t.Fatalf("restore: %+v", r)
	}
	if !reflect.DeepEqual(original, pruneV4GuardFiles(t, p.Registration.GitDir)) {
		t.Fatal("restored metadata bytes differ")
	}
	if _, e = os.Lstat(p.Registration.Destination); !os.IsNotExist(e) {
		t.Fatal("restore created checkout", e)
	}
	// Repair is a separately authorized native operation in this disposable fixture.
	gitFixture(t, p.SourcePath, "worktree", "repair", moved)
	gitFixture(t, moved, "status", "--porcelain")
	after, e := os.ReadFile(filepath.Join(p.Registration.GitDir, "index"))
	pruneMust(t, e)
	if !bytes.Equal(index, after) {
		t.Fatal("repair changed saved index")
	}
	lock, e := os.ReadFile(filepath.Join(p.Registration.GitDir, "locked"))
	pruneMust(t, e)
	if string(lock) != "stw:"+p.Registration.Task && string(lock) != "stw:"+p.Registration.Task+"\n" {
		t.Fatal("original lock lost")
	}
}
func TestRegistrationPruneV4RestoreRefusalReasons(t *testing.T) {
	for _, kind := range []string{"apply-without-authorize", "authorize-without-apply", "occupied-name", "occupied-destination", "corrupt-payload", "missing-pin", "wrong-mode"} {
		t.Run(kind, func(t *testing.T) {
			p := restoreV4Fixture(t)
			o := restoreV4Options(p)
			o.Apply, o.Authorize = true, true
			code := "prune-restore-refused"
			switch kind {
			case "apply-without-authorize":
				o.Authorize = false
				code = "authorization-required"
			case "authorize-without-apply":
				o.Apply = false
				code = "authorization-required"
			case "occupied-name":
				pruneMust(t, os.Mkdir(p.Registration.GitDir, 0700))
			case "occupied-destination":
				pruneMust(t, os.Mkdir(p.Registration.Destination, 0700))
				code = "prune-destination-present"
			case "corrupt-payload":
				pruneMust(t, os.WriteFile(filepath.Join(p.PayloadPath, "files", "HEAD"), []byte("corrupt"), 0600))
				code = "prune-recovery-refused"
			case "missing-pin":
				gitFixture(t, p.SourcePath, "update-ref", "-d", pruneV4HistoryRef(p))
				code = "prune-postcondition-refused"
			case "wrong-mode":
				o.Mode.Caller = "foreign"
				code = "prune-scope-refused"
			}
			before := pruneV4GuardFiles(t, p.StateDir)
			r := RestoreRegistrationPruneV4(context.Background(), o)
			pruneV4GuardReason(t, r, "refused", code)
			if !reflect.DeepEqual(before, pruneV4GuardFiles(t, p.StateDir)) {
				t.Fatal("preflight refusal wrote state")
			}
		})
	}
}
func TestRegistrationPruneV4RestorePublicationDoesNotReplace(t *testing.T) {
	p := restoreV4Fixture(t)
	o := restoreV4Options(p)
	o.Apply, o.Authorize = true, true
	fired := false
	ctx := pruneHookContext(func(stage, path string, _ any) {
		if stage == "v4-restore-before-publish" {
			fired = true
			pruneMust(t, os.Mkdir(p.Registration.GitDir, 0700))
			pruneMust(t, os.WriteFile(filepath.Join(p.Registration.GitDir, "foreign"), []byte("preserve"), 0600))
		}
	})
	r := RestoreRegistrationPruneV4(ctx, o)
	if !fired {
		t.Fatal("publication seam did not run")
	}
	pruneV4GuardReason(t, r, "partial", "prune-restore-refused")
	b, e := os.ReadFile(filepath.Join(p.Registration.GitDir, "foreign"))
	if e != nil || string(b) != "preserve" {
		t.Fatal("occupied registration overwritten", e)
	}
}

func TestRegistrationPruneV4RestoreReplayRechecksOwnership(t *testing.T) {
	p := restoreV4Fixture(t)
	o := restoreV4Options(p)
	o.Apply, o.Authorize = true, true
	if r := RestoreRegistrationPruneV4(context.Background(), o); r.Status != "ok" {
		t.Fatalf("restore: %+v", r)
	}
	pruneMust(t, os.WriteFile(filepath.Join(p.StateDir, "tasks", p.Registration.Task+".json"), []byte("{}\n"), 0600))
	r := RestoreRegistrationPruneV4(context.Background(), o)
	pruneV4GuardReason(t, r, "refused", "prune-ownership-refused")
}
func TestRegistrationPruneV4RestoreNoReplacePrimitive(t *testing.T) {
	parent := gitTempDir(t)
	pruneMust(t, os.Mkdir(filepath.Join(parent, "staged"), 0700))
	pruneMust(t, os.Mkdir(filepath.Join(parent, "occupied"), 0700))
	root, _, e := openCanonical(parent)
	pruneMust(t, e)
	defer root.Close()
	// Empty directories are deliberate: ordinary rename would overwrite one.
	if e = publishPruneV4Restore(root, "staged", root, "occupied"); e == nil {
		t.Fatal("publication overwrote an occupied registration")
	}
	for _, name := range []string{"staged", "occupied"} {
		if _, e = os.Stat(filepath.Join(parent, name)); e != nil {
			t.Fatal("publication altered existing directories", e)
		}
	}
}
func TestRegistrationPruneV4RestoreCancellationKeepsIntent(t *testing.T) {
	p := restoreV4Fixture(t)
	o := restoreV4Options(p)
	o.Apply, o.Authorize = true, true
	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := context.WithValue(base, pruneObservationHookKey{}, pruneObservationHook(func(stage, path string, v any) {
		if stage == "v4-restore-before-publish" {
			cancel()
		}
	}))
	r := RestoreRegistrationPruneV4(ctx, o)
	if r.Status != "partial" || len(r.Diagnostics) != 1 || r.Diagnostics[0].Message != "Administrative restore canceled; retained journal records possible effects." {
		t.Fatalf("cancellation: %+v", r)
	}
	if _, e := os.Lstat(p.Registration.GitDir); !os.IsNotExist(e) {
		t.Fatal("canceled restore published", e)
	}
	r = RestoreRegistrationPruneV4(context.Background(), o)
	pruneV4GuardReason(t, r, "refused", "prune-restore-refused")
}

func TestRegistrationPruneV4RestoreJournalBindsPayload(t *testing.T) {
	p := restoreV4Fixture(t)
	o := restoreV4Options(p)
	o.Apply, o.Authorize = true, true
	if r := RestoreRegistrationPruneV4(context.Background(), o); r.Status != "ok" {
		t.Fatalf("restore: %+v", r)
	}
	dir := filepath.Join(p.StateDir, pruneV4Namespace, "restores")
	names, e := os.ReadDir(dir)
	pruneMust(t, e)
	if len(names) != 1 {
		t.Fatalf("restore journal count %d", len(names))
	}
	path := filepath.Join(dir, names[0].Name())
	b, e := os.ReadFile(path)
	pruneMust(t, e)
	var j pruneV4RestoreJournal
	pruneMust(t, json.Unmarshal(b, &j))
	j.PayloadDigest = strings.Repeat("0", 64)
	b, e = pruneV4Canonical(j, "")
	pruneMust(t, e)
	pruneMust(t, os.WriteFile(path, append(b, '\n'), 0600))
	s, e := openStoreKinds(filepath.Join(p.StateDir, pruneV4Namespace), false, []string{"journals"})
	pruneMust(t, e)
	defer s.Close()
	original, e := readPruneV4PrivateFile(s.dirs["journals"], p.PlanID+".json", int64(pruneV4RecordLimit))
	pruneMust(t, e)
	_, e = readPruneV4RestoreForPlan(s, p, pruneV4Hash("stw-registration-journal-file-v4", original), pruneV4GuardJournal(t, p).PayloadDigest)
	assertPruneReason(t, e, "prune-restore-refused", pruneV4RestoreError().Error())
	r := RestoreRegistrationPruneV4(context.Background(), o)
	pruneV4GuardReason(t, r, "refused", "prune-restore-refused")
}

func TestRegistrationPruneV4RestoreRefusesReusedCompletedSlot(t *testing.T) {
	p := restoreV4Fixture(t)
	o := restoreV4Options(p)
	o.Apply, o.Authorize = true, true
	if r := RestoreRegistrationPruneV4(context.Background(), o); r.Status != "ok" {
		t.Fatalf("restore: %+v", r)
	}
	common := p.Expected.SourceSnapshot.Identity.RepositoryDir
	pruneMust(t, os.Rename(p.Registration.GitDir, filepath.Join(common, "held-original")))
	root, _, e := openCanonical(common)
	pruneMust(t, e)
	defer root.Close()
	pruneMust(t, copyPruneV4Restore(context.Background(), p, root, "replacement"))
	pruneMust(t, os.Rename(filepath.Join(common, "replacement"), p.Registration.GitDir))
	r := RestoreRegistrationPruneV4(context.Background(), o)
	pruneV4GuardReason(t, r, "refused", "prune-restore-refused")
}

func TestRegistrationPruneV4RestoreCompletionFailureRetainsReadablePartial(t *testing.T) {
	p := restoreV4Fixture(t)
	o := restoreV4Options(p)
	o.Apply, o.Authorize = true, true
	fired := false
	ctx := pruneHookContext(func(stage, path string, v any) {
		if stage == "v4-restore-before-complete" {
			fired = true
			j := v.(*pruneV4RestoreJournal)
			j.Diagnostics = []Diagnostic{{Code: strings.Repeat("x", pruneV4RecordLimit), Message: "injected record limit"}}
		}
	})
	r := RestoreRegistrationPruneV4(ctx, o)
	if !fired {
		t.Fatal("completion failure seam not exercised")
	}
	pruneV4GuardReason(t, r, "partial", "record-limit")
	s, e := openStoreKinds(filepath.Join(p.StateDir, pruneV4Namespace), false, []string{"journals"})
	pruneMust(t, e)
	defer s.Close()
	raw, e := readPruneV4PrivateFile(s.dirs["journals"], p.PlanID+".json", int64(pruneV4RecordLimit))
	pruneMust(t, e)
	j, e := readPruneV4RestoreForPlan(s, p, pruneV4Hash("stw-registration-journal-file-v4", raw), pruneV4GuardJournal(t, p).PayloadDigest)
	pruneMust(t, e)
	if j == nil || j.Phase != "partial" || !reflect.DeepEqual(j.Remaining, []string{"restore-registration"}) {
		t.Fatalf("invalid retained partial: %+v", j)
	}
}
func TestRegistrationPruneV4RestoreReaderRejectsOtherPlanVocabulary(t *testing.T) {
	p := restoreV4Fixture(t)
	o := restoreV4Options(p)
	o.Apply, o.Authorize = true, true
	if r := RestoreRegistrationPruneV4(context.Background(), o); r.Status != "ok" {
		t.Fatalf("restore: %+v", r)
	}
	dir := filepath.Join(p.StateDir, pruneV4Namespace, "restores")
	names, e := os.ReadDir(dir)
	pruneMust(t, e)
	raw, e := os.ReadFile(filepath.Join(dir, names[0].Name()))
	pruneMust(t, e)
	var other pruneV4RestoreJournal
	pruneMust(t, json.Unmarshal(raw, &other))
	other.RestoreID = strings.Repeat("e", 32)
	other.PlanID = strings.Repeat("f", 32)
	other.HistoryRef = "refs/stw/recovery/registration-prune/" + other.PlanID + "/head"
	other.Effects[0].Action = "unknown-operation"
	raw, e = pruneV4Canonical(other, "")
	pruneMust(t, e)
	pruneMust(t, os.WriteFile(filepath.Join(dir, other.RestoreID+".json"), append(raw, '\n'), 0600))
	s, e := openStoreKinds(filepath.Join(p.StateDir, pruneV4Namespace), false, []string{"journals"})
	pruneMust(t, e)
	defer s.Close()
	raw, e = readPruneV4PrivateFile(s.dirs["journals"], p.PlanID+".json", int64(pruneV4RecordLimit))
	pruneMust(t, e)
	_, e = readPruneV4RestoreForPlan(s, p, pruneV4Hash("stw-registration-journal-file-v4", raw), pruneV4GuardJournal(t, p).PayloadDigest)
	assertPruneReason(t, e, "prune-restore-refused", pruneV4RestoreError().Error())
}

func TestRegistrationPruneV4RestoreStagingRemainsPrivate(t *testing.T) {
	p := restoreV4Fixture(t)
	o := restoreV4Options(p)
	o.Apply, o.Authorize = true, true
	fired := false
	ctx := pruneHookContext(func(stage, path string, v any) {
		if stage != "v4-restore-before-publish" {
			return
		}
		fired = true
		names, e := os.ReadDir(filepath.Join(p.StateDir, pruneV4Namespace, "restores"))
		pruneMust(t, e)
		if len(names) != 1 {
			t.Fatalf("restore journal count %d", len(names))
		}
		id := strings.TrimSuffix(names[0].Name(), ".json")
		st, e := os.Lstat(pruneV4RestoreStaging(p, id))
		pruneMust(t, e)
		if st.Mode().Perm() != 0700 {
			t.Fatalf("staging is not private before publication: %v", st.Mode())
		}
	})
	r := RestoreRegistrationPruneV4(ctx, o)
	if !fired || r.Status != "ok" {
		t.Fatalf("private staging fired=%v result=%+v", fired, r)
	}
}

func TestRegistrationPruneV4RestoreRepairArgvForInspectAndApply(t *testing.T) {
	p := restoreV4Fixture(t)
	o := restoreV4Options(p)
	for _, apply := range []bool{false, true} {
		o.Apply, o.Authorize = apply, apply
		r := RestoreRegistrationPruneV4(context.Background(), o)
		if r.Status != "ok" || len(r.Actions) != 1 {
			t.Fatalf("restore apply=%v: %+v", apply, r)
		}
		_, data, ok := strings.Cut(r.Actions[0].Detail, "Repair argv data (separate owner authorization): ")
		if !ok {
			t.Fatalf("repair argv absent for apply=%v: %s", apply, r.Actions[0].Detail)
		}
		var args []string
		pruneMust(t, json.Unmarshal([]byte(data), &args))
		if !reflect.DeepEqual(args, []string{"git", "-C", p.SourcePath, "worktree", "repair", "<owner-selected-found-path>"}) {
			t.Fatalf("wrong repair argv: %#v", args)
		}
	}
}
func TestRegistrationPruneV4RestoreRefusesColocatedOwnershipWithoutWrites(t *testing.T) {
	p := restoreV4Fixture(t)
	pruneMust(t, os.Mkdir(filepath.Join(p.SourcePath, ".jj"), 0700))
	o := restoreV4Options(p)
	o.Apply, o.Authorize = true, true
	before := treeSnapshot(t, p.StateDir, p.Expected.SourceSnapshot.Identity.RepositoryDir)
	r := RestoreRegistrationPruneV4(context.Background(), o)
	pruneV4GuardReason(t, r, "refused", "git-ownership-refused")
	if !reflect.DeepEqual(before, treeSnapshot(t, p.StateDir, p.Expected.SourceSnapshot.Identity.RepositoryDir)) {
		t.Fatal("colocated refusal changed protected state")
	}
}
