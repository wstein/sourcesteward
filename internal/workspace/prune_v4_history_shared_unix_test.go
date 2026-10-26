// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package workspace

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func pruneV4HistorySharedFixture(t *testing.T, state, task string) RegistrationPruneV4Plan {
	t.Helper()
	source := fixtureGit(t)
	destination := filepath.Join(gitTempDir(t), task)
	planned := PlanCreate(context.Background(), PlanOptions{Path: source, Base: "HEAD", Destination: destination, Task: task, Owner: "fixture", StateDir: state})
	if planned.Status != "ok" || planned.Plan == nil {
		t.Fatalf("creation plan: %+v", planned)
	}
	applied := Apply(context.Background(), ApplyOptions{StateDir: state, PlanID: planned.Plan.PlanID, Authorize: true})
	if applied.Status != "ok" || applied.Task == nil {
		t.Fatalf("creation apply: %+v", applied)
	}
	pruneMust(t, os.Rename(destination, destination+"-moved"))
	mode := V3RunMode{Mode: "agent", Markers: []string{}, ByOwner: []string{}, Caller: "fixture", CallerSource: "explicit"}
	prune := PlanRegistrationPruneV4(context.Background(), RegistrationPruneV4Options{Path: source, StateDir: state, Name: filepath.Base(applied.Task.Native.GitDir), GitDir: applied.Task.Native.GitDir, Mode: mode})
	if prune.Status != "ok" || prune.Plan == nil {
		t.Fatalf("prune plan: %+v", prune)
	}
	p := *prune.Plan
	pruneV4HistoryWritePrepared(t, p)
	return p
}

func pruneV4HistoryWritePrepared(t *testing.T, p RegistrationPruneV4Plan) {
	t.Helper()
	dir := filepath.Join(p.StateDir, pruneV4Namespace, "journals")
	if err := os.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
		t.Fatal(err)
	}
	j := pruneV4Journal{4, "registration-prune", p.PlanID, p.Digest, "prepared", "", pruneV4HistoryRef(p), p.Expected.ResolvedHEAD, []pruneV4Effect{}, []string{"prepare-recovery", "pin-history", "remove-registration"}, []Diagnostic{}}
	b, err := pruneV4Canonical(j, "")
	pruneMust(t, err)
	pruneMust(t, os.WriteFile(filepath.Join(dir, p.PlanID+".json"), append(b, '\n'), 0600))
}

func pruneV4HistorySetTime(t *testing.T, p RegistrationPruneV4Plan, when time.Time) {
	t.Helper()
	pruneMust(t, os.Chtimes(filepath.Join(p.StateDir, pruneV4Namespace, "journals", p.PlanID+".json"), when, when))
}

func TestRegistrationPruneV4HistoryFiltersSharedStoreBeforeCountAndLimit(t *testing.T) {
	state := gitTempDir(t)
	pruneMust(t, os.Chmod(state, 0700))
	a := pruneV4HistorySharedFixture(t, state, "repo-a")
	b := pruneV4HistorySharedFixture(t, state, "repo-b")
	newer := clonePruneV4(t, b)
	newer.PlanID = strings.Repeat("c", 32)
	newer.PayloadPath = filepath.Join(state, pruneV4Namespace, "payloads", newer.PlanID)
	savePruneV4Fixture(t, newer)
	var err error
	newer, err = ReadRegistrationPruneV4Plan(state, newer.PlanID)
	pruneMust(t, err)
	pruneV4HistoryWritePrepared(t, newer)
	now := time.Now()
	pruneV4HistorySetTime(t, b, now)
	pruneV4HistorySetTime(t, newer, now.Add(time.Hour))
	pruneV4HistorySetTime(t, a, now.Add(2*time.Hour))
	before := pruneV4HistoryTree(t, state)
	for _, tc := range []struct {
		name, path, plan string
		total            int
		truncated        bool
	}{
		{"repo a", a.SourcePath, a.PlanID, 1, false},
		{"repo b", b.SourcePath, newer.PlanID, 2, true},
		{"repo with no matching journals", fixtureGit(t), "", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := HistoryRegistrationPruneV4(context.Background(), RegistrationPruneV4HistoryOptions{Path: tc.path, StateDir: state, Limit: 1})
			if r.Status != "ok" || r.Total != tc.total || r.Truncated != tc.truncated || len(r.Entries) != min(1, tc.total) || len(r.Diagnostics) != 0 {
				t.Fatalf("filter/count/limit: %+v", r)
			}
			if tc.total > 0 && r.Entries[0].PlanID != tc.plan {
				t.Fatalf("wanted newest matching %s: %+v", tc.plan, r)
			}
		})
	}
	if !reflect.DeepEqual(before, pruneV4HistoryTree(t, state)) {
		t.Fatal("listing changed shared state")
	}
}

func TestRegistrationPruneV4HistoryChargesForeignPlansToScanBudget(t *testing.T) {
	state := gitTempDir(t)
	pruneMust(t, os.Chmod(state, 0700))
	a := pruneV4HistorySharedFixture(t, state, "repo-a")
	b := pruneV4HistorySharedFixture(t, state, "repo-b")
	pruneV4HistorySetTime(t, a, time.Now().Add(time.Hour))
	c := pruneV4HistoryCoordinator{scanLimit: 4096, scanBytes: pruneV4RecordLimit + 1, outputBytes: 4 << 20}
	r := c.history(context.Background(), RegistrationPruneV4HistoryOptions{Path: b.SourcePath, StateDir: state, Limit: 1})
	if r.Status != "ok" || !r.Truncated || r.Total != 0 || len(r.Entries) != 0 || len(r.Truncation) != 1 || r.Truncation[0].Unit != "scan-bytes" {
		t.Fatalf("foreign-plan scan budget: %+v", r)
	}
}

func TestRegistrationPruneV4HistoryDisclosesRestoreVisibility(t *testing.T) {
	r := newPruneV4HistoryResult()
	text := strings.Join(r.Limitations, "\n")
	for _, want := range []string{"restore journals are not listed", "completed or partial restore", "current directory", "optional PATH"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing disclosure %q: %s", want, text)
		}
	}
}

func TestRegistrationPruneV4HistoryStillRefusesInvalidForeignPlan(t *testing.T) {
	state := gitTempDir(t)
	pruneMust(t, os.Chmod(state, 0700))
	a := pruneV4HistorySharedFixture(t, state, "repo-a")
	b := pruneV4HistorySharedFixture(t, state, "repo-b")
	a.SchemaVersion = 3
	savePruneV4Fixture(t, a)
	r := HistoryRegistrationPruneV4(context.Background(), RegistrationPruneV4HistoryOptions{Path: b.SourcePath, StateDir: state, Limit: 1})
	const message = "Contract-4 plan version, operation, witness, scope or digest is invalid."
	if r.Status != "refused" || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "prune-plan-refused" || r.Diagnostics[0].Message != message || len(r.Entries) != 0 {
		t.Fatalf("invalid foreign plan: %+v", r)
	}
}
