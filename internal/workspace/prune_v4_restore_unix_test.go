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
	r := RestoreRegistrationPruneV4(context.Background(), o)
	pruneV4GuardReason(t, r, "refused", "prune-restore-refused")
}
