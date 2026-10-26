// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
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
	beforeState := pruneV4HistoryTree(t, p.StateDir)
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
	if !reflect.DeepEqual(beforeState, pruneV4HistoryTree(t, p.StateDir)) {
		t.Fatal("history changed state bytes or entries")
	}
	for _, path := range []string{filepath.Join(p.StateDir, pruneV4Namespace, "restores")} {
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

func TestRegistrationPruneV4RecordByteCeiling(t *testing.T) {
	dir := t.TempDir()
	pruneMust(t, os.Chmod(dir, 0700))
	root, err := os.OpenRoot(dir)
	pruneMust(t, err)
	defer root.Close()
	// JSON quotes plus the required final LF count toward the encoded record ceiling.
	exact := strings.Repeat("a", (1<<20)-3)
	pruneMust(t, writePruneV4Record(root, "exact.json", exact, true))
	info, err := root.Stat("exact.json")
	pruneMust(t, err)
	if info.Size() != 1<<20 {
		t.Fatalf("encoded bytes=%d", info.Size())
	}
	err = writePruneV4Record(root, "over.json", exact+"a", true)
	if tidyErrorCode(err) != "record-limit" {
		t.Fatalf("want record-limit, got %v", err)
	}
	if _, err = root.Stat("over.json"); !os.IsNotExist(err) {
		t.Fatalf("oversize record persisted: %v", err)
	}
}

func pruneV4HistoryTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	pruneMust(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		value := fmt.Sprint(info.Mode())
		if d.Type()&os.ModeSymlink != 0 {
			target, e := os.Readlink(path)
			if e != nil {
				return e
			}
			value += target
		} else if !d.IsDir() {
			b, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			value += string(b)
		}
		out[path] = value
		return nil
	}))
	return out
}

func TestRegistrationPruneV4HistoryRefusesConcurrentRecordReplacement(t *testing.T) {
	for _, kind := range []string{"plans", "journals"} {
		t.Run(kind, func(t *testing.T) {
			p := pruneV4HistoryFixture(t)
			path := filepath.Join(p.StateDir, pruneV4Namespace, kind, p.PlanID+".json")
			c := pruneV4HistoryCoordinator{scanLimit: 4096, scanBytes: 4 << 20, outputBytes: 4 << 20, afterRead: func() {
				b, e := os.ReadFile(path)
				pruneMust(t, e)
				pruneMust(t, os.Rename(path, path+".replaced"))
				pruneMust(t, os.WriteFile(path, b, 0600))
			}}
			r := c.history(context.Background(), RegistrationPruneV4HistoryOptions{Path: p.SourcePath, StateDir: p.StateDir})
			if r.Status != "refused" || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "changed-state" {
				t.Fatalf("replacement: %+v", r)
			}
		})
	}
}

func TestRegistrationPruneV4HistoryReportsUnverifiedRecovery(t *testing.T) {
	p := pruneV4HistoryFixture(t)
	path := filepath.Join(p.StateDir, pruneV4Namespace, "journals", p.PlanID+".json")
	j := pruneV4GuardJournal(t, p)
	j.PayloadDigest = strings.Repeat("a", 64)
	b, err := pruneV4Canonical(j, "")
	pruneMust(t, err)
	pruneMust(t, os.WriteFile(path, append(b, '\n'), 0600))
	// Neither a recorded digest nor a pointer proves present, intact recovery.
	r := HistoryRegistrationPruneV4(context.Background(), RegistrationPruneV4HistoryOptions{Path: p.SourcePath, StateDir: p.StateDir})
	if r.Status != "ok" || len(r.Entries) != 1 || r.Entries[0].RecoveryStatus != "unknown" || r.Entries[0].PayloadDigest == nil || *r.Entries[0].PayloadDigest != j.PayloadDigest {
		t.Fatalf("availability: %+v", r)
	}
}

func TestRegistrationPruneV4HistoryReportsScanAndOutputBounds(t *testing.T) {
	p := pruneV4HistoryFixture(t)
	for _, tc := range []struct {
		name                              string
		scanLimit, scanBytes, outputBytes int
		unit                              string
	}{
		{"directory", 0, 4 << 20, 4 << 20, "directory-entries"},
		{"scan bytes", 4096, 0, 4 << 20, "scan-bytes"},
		{"output bytes", 4096, 4 << 20, 1800, "output-bytes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := pruneV4HistoryCoordinator{scanLimit: tc.scanLimit, scanBytes: tc.scanBytes, outputBytes: tc.outputBytes}
			r := c.history(context.Background(), RegistrationPruneV4HistoryOptions{Path: p.SourcePath, StateDir: p.StateDir})
			if r.Status != "ok" || !r.Truncated || len(r.Entries) != 0 {
				t.Fatalf("bounded: %+v", r)
			}
			found := false
			for _, truncation := range r.Truncation {
				found = found || truncation.Unit == tc.unit
			}
			if !found {
				t.Fatalf("missing %s: %+v", tc.unit, r)
			}
			b, err := json.Marshal(r)
			pruneMust(t, err)
			if len(b)+1 > tc.outputBytes {
				t.Fatalf("output exceeds bound: %d", len(b)+1)
			}
		})
	}
}

func TestRegistrationPruneV4HistoryRefusesUnsafeJournalFiles(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "directory", "oversize", "invalid-id"} {
		t.Run(kind, func(t *testing.T) {
			p := pruneV4HistoryFixture(t)
			path := filepath.Join(p.StateDir, pruneV4Namespace, "journals", p.PlanID+".json")
			switch kind {
			case "symlink":
				pruneMust(t, os.Rename(path, path+".safe"))
				pruneMust(t, os.Symlink(path+".safe", path))
			case "hardlink":
				pruneMust(t, os.Link(path, path+".linked"))
			case "directory":
				pruneMust(t, os.Remove(path))
				pruneMust(t, os.Mkdir(path, 0700))
			case "oversize":
				f, e := os.OpenFile(path, os.O_RDWR, 0600)
				pruneMust(t, e)
				pruneMust(t, f.Truncate((1<<20)+1))
				pruneMust(t, f.Close())
			case "invalid-id":
				pruneMust(t, os.Rename(path, filepath.Join(filepath.Dir(path), "old-format.json")))
			}
			r := HistoryRegistrationPruneV4(context.Background(), RegistrationPruneV4HistoryOptions{Path: p.SourcePath, StateDir: p.StateDir})
			if r.Status != "refused" || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "prune-journal-refused" {
				t.Fatalf("unsafe file: %+v", r)
			}
		})
	}
}

func TestRegistrationPruneV4HistoryDefaultCountLimit(t *testing.T) {
	p := pruneV4HistoryFixture(t)
	for i := 0; i < 256; i++ {
		q := p
		q.PlanID = fmt.Sprintf("%032x", i)
		q.PayloadPath = filepath.Join(q.StateDir, pruneV4Namespace, "payloads", q.PlanID)
		savePruneV4Fixture(t, q)
		q, err := ReadRegistrationPruneV4Plan(q.StateDir, q.PlanID)
		pruneMust(t, err)
		j := pruneV4Journal{4, "registration-prune", q.PlanID, q.Digest, "prepared", "", pruneV4HistoryRef(q), q.Expected.ResolvedHEAD, []pruneV4Effect{}, []string{"prepare-recovery", "pin-history", "remove-registration"}, []Diagnostic{}}
		b, err := pruneV4Canonical(j, "")
		pruneMust(t, err)
		pruneMust(t, os.WriteFile(filepath.Join(q.StateDir, pruneV4Namespace, "journals", q.PlanID+".json"), append(b, '\n'), 0600))
	}
	// Raise only the independent scan budget to isolate the default count limit.
	c := pruneV4HistoryCoordinator{scanLimit: 4096, scanBytes: 64 << 20, outputBytes: 4 << 20}
	r := c.history(context.Background(), RegistrationPruneV4HistoryOptions{Path: p.SourcePath, StateDir: p.StateDir})
	if r.Status != "ok" || !r.Truncated || len(r.Entries) != 256 || r.Total != 257 {
		t.Fatalf("default count: status=%s shown=%d total=%d truncation=%+v diagnostics=%+v", r.Status, len(r.Entries), r.Total, r.Truncation, r.Diagnostics)
	}
}
