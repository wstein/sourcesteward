// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package workspace

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func pruneV4HistoryFixture(t *testing.T) RegistrationPruneV4Plan {
	t.Helper()
	_, p := fixturePruneV4(t)
	dir := filepath.Join(p.StateDir, pruneV4Namespace, "journals")
	pruneMust(t, os.Mkdir(dir, 0700))
	j := pruneV4Journal{4, "registration-prune", p.PlanID, p.Digest, "prepared", "", pruneV4HistoryRef(p), p.Expected.ResolvedHEAD, []pruneV4Effect{}, []string{"prepare-recovery", "pin-history", "remove-registration"}, []Diagnostic{}}
	b, e := pruneV4Canonical(j, "")
	pruneMust(t, e)
	pruneMust(t, os.WriteFile(filepath.Join(dir, p.PlanID+".json"), append(b, '\n'), 0600))
	return p
}

func TestRegistrationPruneV4HistoryReadOnlyAndNoPayloadRead(t *testing.T) {
	p := pruneV4HistoryFixture(t)
	// Listing must succeed without opening even the payload namespace.
	pruneMust(t, os.Symlink("/nonexistent-private-payload", filepath.Join(p.StateDir, pruneV4Namespace, "payloads")))
	before := pruneV4GuardFiles(t, p.SourcePath)
	r := HistoryRegistrationPruneV4(context.Background(), RegistrationPruneV4HistoryOptions{Path: p.SourcePath, StateDir: p.StateDir})
	if r.Status != "ok" || r.Command != "tidy-history" || r.Operation != "registration-prune" || r.SchemaVersion != 4 || r.Truncated || len(r.Entries) != 1 {
		t.Fatalf("history: %+v", r)
	}
	e := r.Entries[0]
	if e.PlanID != p.PlanID || e.RecoveryStatus != "not-prepared" || e.PayloadDigest != nil || e.PayloadPath != p.PayloadPath || e.HistoryRef != pruneV4HistoryRef(p) {
		t.Fatalf("entry: %+v", e)
	}
	if !reflect.DeepEqual(before, pruneV4GuardFiles(t, p.SourcePath)) {
		t.Fatal("history changed repository bytes")
	}
	for _, path := range []string{filepath.Join(p.StateDir, pruneV4Namespace, "restores"), filepath.Join(p.StateDir, pruneV4Namespace, "lock")} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("inspection created %s: %v", path, err)
		}
	}
	raw, err := json.Marshal(r)
	pruneMust(t, err)
	if strings.Contains(string(raw), "\"entries\":null") || strings.Contains(string(raw), "\"diagnostics\":null") || !strings.Contains(string(raw), "\"payload_digest\":null") {
		t.Fatalf("closed arrays/null: %s", raw)
	}
}

func TestRegistrationPruneV4HistoryStrictJournalAndPlan(t *testing.T) {
	for _, kind := range []string{"version", "unknown", "duplicate", "missing", "plan"} {
		t.Run(kind, func(t *testing.T) {
			p := pruneV4HistoryFixture(t)
			path := filepath.Join(p.StateDir, pruneV4Namespace, "journals", p.PlanID+".json")
			b, err := os.ReadFile(path)
			pruneMust(t, err)
			code := "prune-journal-refused"
			switch kind {
			case "version":
				b = []byte(strings.Replace(string(b), "\"schema_version\":4", "\"schema_version\":3", 1))
			case "unknown":
				b = []byte(strings.Replace(string(b), "{", "{\"extra\":true,", 1))
			case "duplicate":
				b = []byte(strings.Replace(string(b), "{", "{\"phase\":\"prepared\",", 1))
			case "missing":
				b = []byte(strings.Replace(string(b), "\"payload_digest\":\"\",", "", 1))
			case "plan":
				path = pruneV4PlanPath(p)
				b, err = os.ReadFile(path)
				pruneMust(t, err)
				b = []byte(strings.Replace(string(b), "\"schema_version\":4", "\"schema_version\":3", 1))
				code = "prune-plan-refused"
			}
			pruneMust(t, os.WriteFile(path, b, 0600))
			r := HistoryRegistrationPruneV4(context.Background(), RegistrationPruneV4HistoryOptions{Path: p.SourcePath, StateDir: p.StateDir})
			if r.Status != "refused" || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != code {
				t.Fatalf("want refused/%s, got %+v", code, r)
			}
		})
	}
}

func TestRegistrationPruneV4HistoryBoundAndNewestFirst(t *testing.T) {
	p := pruneV4HistoryFixture(t)
	q := clonePruneV4(t, p)
	q.PlanID = strings.Repeat("a", 32)
	if q.PlanID == p.PlanID {
		q.PlanID = strings.Repeat("b", 32)
	}
	q.PayloadPath = filepath.Join(q.StateDir, pruneV4Namespace, "payloads", q.PlanID)
	savePruneV4Fixture(t, q)
	q, err := ReadRegistrationPruneV4Plan(q.StateDir, q.PlanID)
	pruneMust(t, err)
	j := pruneV4Journal{4, "registration-prune", q.PlanID, q.Digest, "prepared", "", pruneV4HistoryRef(q), q.Expected.ResolvedHEAD, []pruneV4Effect{}, []string{"prepare-recovery", "pin-history", "remove-registration"}, []Diagnostic{}}
	b, err := pruneV4Canonical(j, "")
	pruneMust(t, err)
	path := filepath.Join(q.StateDir, pruneV4Namespace, "journals", q.PlanID+".json")
	pruneMust(t, os.WriteFile(path, append(b, '\n'), 0600))
	future := time.Now().Add(time.Hour)
	pruneMust(t, os.Chtimes(path, future, future))
	r := HistoryRegistrationPruneV4(context.Background(), RegistrationPruneV4HistoryOptions{Path: p.SourcePath, StateDir: p.StateDir, Limit: 1})
	if r.Status != "ok" || !r.Truncated || len(r.Entries) != 1 || r.Entries[0].PlanID != q.PlanID || r.Total != 2 {
		t.Fatalf("bounded order: %+v", r)
	}
}

func TestRegistrationPruneV4HistoryMissingNamespaceDoesNotInitialize(t *testing.T) {
	_, p := fixturePruneV4(t)
	pruneMust(t, os.RemoveAll(filepath.Join(p.StateDir, pruneV4Namespace)))
	r := HistoryRegistrationPruneV4(context.Background(), RegistrationPruneV4HistoryOptions{Path: p.SourcePath, StateDir: p.StateDir})
	if r.Status != "refused" || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "unsafe-state" {
		t.Fatalf("missing: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(p.StateDir, pruneV4Namespace)); !os.IsNotExist(err) {
		t.Fatalf("namespace initialized: %v", err)
	}
}
