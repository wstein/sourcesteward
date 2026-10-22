// SPDX-License-Identifier: EUPL-1.2
package watch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// firedRig returns a rig whose PR 1 has already journalled its pending timeout.
func firedRig(t *testing.T) *rig {
	r := newRig(t)
	r.timeout = 10 * time.Minute
	r.must(ok(mk(1, runs(running()))))
	for i := 0; i < 11; i++ {
		r.must(ok(mk(1, runs(running()))))
	}
	if n := strings.Count(strings.Join(r.journalIDs(), " "), "checks-pending-timeout"); n != 1 {
		t.Fatalf("setup: %d timeouts: %v", n, r.journalIDs())
	}
	return r
}

func TestLostStateRebuildRaisesTheChecksEpochOfABaselinePR(t *testing.T) {
	r := newRig(t)
	r.must(ok(mk(1, runs(running()))))
	r.must(ok(mk(1, runs(passing()))))
	one(t, r.must(ok(mk(1, runs(passing())))), KindChecksGreen) // c1; the PR has no new-pr in the journal
	r.w.Close()
	r.w = nil
	os.Remove(filepath.Join(r.dir, "state.json"))
	r.mustOpen()
	r.must(ok(mk(1, runs(running()))))
	r.must(ok(mk(1, runs(passing()))))
	r.must(ok(mk(1, runs(passing()))))
	uniq(t, r.journalIDs())
	if got := r.journalIDs(); !strings.HasSuffix(got[len(got)-1], ":c2") {
		t.Fatal(got)
	}
}

func TestTimeoutDoesNotRefireAfterForcedBaseline(t *testing.T) {
	r := firedRig(t)
	r.w.Close()
	r.w = nil
	r.base = true
	r.mustOpen()
	r.base = false
	for i := 0; i < 14; i++ {
		r.must(ok(mk(1, runs(running()))))
	}
	uniq(t, r.journalIDs())
	if n := strings.Count(strings.Join(r.journalIDs(), " "), "checks-pending-timeout"); n != 1 {
		t.Fatal(r.journalIDs())
	}
}

func TestTimeoutDoesNotRefireAfterLostState(t *testing.T) {
	r := firedRig(t)
	r.w.Close()
	r.w = nil
	os.Remove(filepath.Join(r.dir, "state.json"))
	r.mustOpen()
	for i := 0; i < 14; i++ {
		r.must(ok(mk(1, runs(running()))))
	}
	uniq(t, r.journalIDs())
	if n := strings.Count(strings.Join(r.journalIDs(), " "), "checks-pending-timeout"); n != 1 {
		t.Fatal(r.journalIDs())
	}
}

func TestTimeoutDoesNotRefireAfterRepositoryRemoveAndReAdd(t *testing.T) {
	r := newRig(t, "o/r", "o/x")
	r.timeout = 10 * time.Minute
	r.set("o/x", ok())
	r.set("o/r", ok(mk(1, runs(running()))))
	for i := 0; i < 12; i++ {
		if _, err := r.run(); err != nil {
			t.Fatal(err)
		}
	}
	r.repos = []string{"o/x"}
	r.mustOpen()
	r.run() // repos-changed: o/r removed
	r.repos = []string{"o/r", "o/x"}
	r.mustOpen()
	for i := 0; i < 14; i++ {
		if _, err := r.run(); err != nil {
			t.Fatal(err)
		}
	}
	uniq(t, r.journalIDs())
	if n := strings.Count(strings.Join(r.journalIDs(), " "), "checks-pending-timeout"); n != 1 {
		t.Fatal(r.journalIDs())
	}
}

func TestLostStateKeepsCountersOfRepositoriesNoLongerConfigured(t *testing.T) {
	r := newRig(t, "o/r", "o/x")
	r.set("o/x", ok(mk(3, head("aa"))))
	r.set("o/r", ok())
	r.run()
	r.set("o/x", ok(mk(3, head("bb"))))
	r.run()
	r.w.Close()
	r.w = nil
	os.Remove(filepath.Join(r.dir, "state.json"))
	r.repos = []string{"o/r"}
	r.mustOpen()
	r.run()
	if rs := r.state().Repo["o/x"]; rs == nil || rs.PRs[3] == nil || rs.PRs[3].Epochs.Head != 1 {
		t.Fatalf("counters of o/x lost: %+v", rs)
	}
	r.repos = []string{"o/r", "o/x"}
	r.mustOpen()
	r.run() // repos-changed, o/x baselines silently
	r.set("o/x", ok(mk(3, head("aa"))))
	r.run()
	if got := r.journalIDs(); !strings.HasSuffix(got[len(got)-1], sha("aa")+":h2") {
		t.Fatal(got)
	}
}

func TestJournalJunkTailLongerThanTheNextLineIsCutOff(t *testing.T) {
	dir := t.TempDir()
	j, _ := openJournal(dir)
	j.Append(testEvent("a"))
	j.Close()
	f, _ := os.OpenFile(filepath.Join(dir, "journal.jsonl"), os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString(`{"schema_version":1,"seq":2,"id":"` + strings.Repeat("x", 600))
	f.Close()
	j, err := openJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := j.Append(testEvent("b")); err != nil {
		t.Fatal(err)
	}
	j.Close()
	j, err = openJournal(dir) // junk left behind a shorter line would be a corrupt middle line
	if err != nil || j.Tail() != 2 {
		t.Fatalf("%d %v", j.Tail(), err)
	}
	j.Close()
}

func TestClosedReplayPinsTheGenerationAcrossASecondReopen(t *testing.T) {
	r := newRig(t)
	r.must(ok(mk(5), mk(6)))
	one(t, r.must(ok(mk(6)).gone(5, closedGone(sha("51")))), KindClosed) // g0
	one(t, r.must(ok(mk(5), mk(6))), KindNewPR)                          // g1
	r.faults = crashAt("append", 1)
	if _, err := r.cycle(ok(mk(6)).gone(5, closedGone(sha("51")))); err != errCrash { // closed g1, state not written
		t.Fatal(err)
	}
	r.crashed()
	r.mustOpen() // the replay records the generation of the closed line
	got := one(t, r.must(ok(mk(5), mk(6))), KindNewPR)
	if got.ID != "o/r#5:new-pr:g2" {
		t.Fatalf("generation lost in the replay: %s", got.ID)
	}
	uniq(t, r.journalIDs())
}

func TestForkFlagTravelsInTheNewPREventAndIsRestoredByReplay(t *testing.T) {
	r := newRig(t)
	r.must(ok())
	r.faults = crashAt("append", 1)
	fork := func(p *PRObs) { p.Fork = true }
	if _, err := r.cycle(ok(mk(7, fork))); err != errCrash {
		t.Fatal(err)
	}
	r.crashed()
	r.mustOpen()
	if p := r.state().Repo["o/r"].PRs[7]; p == nil || !p.Fork {
		t.Fatalf("fork flag lost in the replay: %+v", p)
	}
	d := r.journal()[0].Details.(*NewPRDetails)
	if !d.Fork {
		t.Fatal("details.fork")
	}
}
