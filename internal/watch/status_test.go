// SPDX-License-Identifier: EUPL-1.2
package watch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadStatusFreshStaleAndMissing(t *testing.T) {
	r := newRig(t)
	r.must(ok(mk(1)))
	r.must(failWith("network"))
	r.w.Close() // a stopped watcher holds no lock
	probe, _ := ReadStatus(r.dir, r.now)
	hb, _ := time.Parse(time.RFC3339, probe.Heartbeat.Time) // the time of the last completed cycle
	now := hb
	s, err := ReadStatus(r.dir, now.Add(100*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !s.Fresh || s.Heartbeat == nil || s.Heartbeat.AgeSeconds != 100 || s.Heartbeat.PID != os.Getpid() || s.LockHeld ||
		s.IntervalSeconds != 60 || s.Seq < 1 || len(s.Repos) != 1 {
		t.Fatalf("%+v %+v", s, s.Heartbeat)
	}
	rp := s.Repos[0]
	if rp.Repo != "o/r" || rp.LastOK == nil || rp.AgeSeconds == nil || *rp.AgeSeconds <= 100 || rp.Failures != 1 || rp.OpenError == nil || *rp.OpenError != "network" {
		t.Fatalf("%+v", rp)
	}
	// 3 x 60 s is below the 3 minute floor: fresh until 180 s
	if s, _ = ReadStatus(r.dir, now.Add(179*time.Second)); !s.Fresh {
		t.Fatal("179 s must be fresh")
	}
	if s, _ = ReadStatus(r.dir, now.Add(181*time.Second)); s.Fresh {
		t.Fatal("181 s must be stale")
	}
	raw, _ := json.Marshal(s)
	var keys map[string]any
	json.Unmarshal(raw, &keys)
	for _, k := range []string{"schema_version", "state_dir", "heartbeat", "fresh", "interval_seconds", "seq", "lock_held", "repos"} {
		if _, ok := keys[k]; !ok {
			t.Errorf("missing key %s in %s", k, raw)
		}
	}
	// a missing directory is not fresh and writes nothing
	missing := filepath.Join(t.TempDir(), "none")
	m, err := ReadStatus(missing, now)
	if err != nil || m.Fresh || m.Heartbeat != nil || len(m.Repos) != 0 {
		t.Fatalf("%+v %v", m, err)
	}
	if _, err := os.Stat(missing); err == nil {
		t.Fatal("ReadStatus created the directory")
	}
	if _, err := ReadStatus("relative", now); err == nil {
		t.Fatal("a relative directory must be refused")
	}
}

func TestReadStatusSeesTheLockHolderAndWritesNothing(t *testing.T) {
	r := newRig(t)
	r.must(ok(mk(1)))
	before := dirListing(t, r.dir)
	s, err := ReadStatus(r.dir, r.now)
	if err != nil || !s.LockHeld || s.LockPID != os.Getpid() {
		t.Fatalf("%+v %v", s, err)
	}
	if after := dirListing(t, r.dir); after != before {
		t.Fatalf("status changed the directory: %s -> %s", before, after)
	}
}

func dirListing(t *testing.T, dir string) string {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	out := ""
	for _, e := range entries {
		info, _ := e.Info()
		out += e.Name() + ":" + info.ModTime().String() + ";"
	}
	return out
}
