// SPDX-License-Identifier: EUPL-1.2
package watch

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

// step is one poll: optional repository set change, clock jump and the fake forge answers.
type step struct {
	repos []string
	adv   time.Duration
	snaps map[string]snap
}

func one1(s snap) step { return step{snaps: map[string]snap{"o/r": s}} }

func (r *rig) do(st step) ([]Event, error) {
	if st.repos != nil {
		r.repos = st.repos
		r.mustOpen()
	}
	r.now = r.now.Add(st.adv)
	for repo, s := range st.snaps {
		r.set(repo, s)
	}
	return r.run()
}

// crashAt kills the watcher at a durable step: the process memory is gone, only the directory is left.
func crashAt(point string, n int) func(string, int) error {
	return func(p string, i int) error {
		if p == point && i == n {
			return errCrash
		}
		return nil
	}
}

func (r *rig) crashed() {
	r.faults = nil
	if r.w != nil {
		r.w.Close()
		r.w = nil
	}
}

// crashCase is one row of the "Replay effects" table. After a crash following the journal append of the
// event wantID, the first poll after the restart sees either the condition still holding (still) or the
// change flapped back (flapped).
type crashCase struct {
	kind    string
	repos   []string
	timeout time.Duration
	setup   []step
	trigger step
	still   []step
	flapped []step
	wantID  string
	wantNew []string // ids the flap-back adds, in order
	// reorder: the flap-back poll is also the poll that finishes the pending restack work, so its events
	// interleave differently from the crash-free run; only the set of ids must match.
	reorder bool
}

func sn(s snap) []step { return []step{one1(s)} }

func crashCases() []crashCase {
	h1, hb, ha := sha("11"), sha("bb"), sha("aa")
	pend := ok(mk(1, runs(running())))
	pass := ok(mk(1, runs(passing())))
	red := ok(mk(1, runs(failing("x", ""))))
	dirty := ok(mk(1, detail(ff(), "dirty")))
	clean := ok(mk(1, detail(tt(), "clean")))
	cc := sha("cc")
	two := map[string]snap{"o/a": ok(mk(1)), "o/b": ok(mk(7))}
	oneRepo := map[string]snap{"o/a": ok(mk(1))}
	return []crashCase{
		{kind: KindNewPR, setup: sn(ok(mk(1))), trigger: one1(ok(mk(1), mk(2))), still: sn(ok(mk(1), mk(2))),
			flapped: []step{one1(ok(mk(1)).gone(2, closedGone(sha("21")))), one1(ok(mk(1), mk(2)))},
			wantID:  "o/r#2:new-pr:g0", wantNew: []string{"o/r#2:closed:g0", "o/r#2:new-pr:g1"}},
		{kind: KindHeadChanged, setup: sn(ok(mk(1, head("aa")))), trigger: one1(ok(mk(1, head("bb")))), still: sn(ok(mk(1, head("bb")))),
			flapped: sn(ok(mk(1, head("aa")))), wantID: "o/r#1:head-changed:" + hb + ":h1", wantNew: []string{"o/r#1:head-changed:" + ha + ":h2"}},
		{kind: KindChecksGreen, setup: []step{one1(pend), one1(pass)}, trigger: one1(pass), still: sn(pass),
			flapped: []step{one1(red), one1(red)}, wantID: "o/r#1:checks-green:" + h1 + ":c1", wantNew: []string{"o/r#1:checks-red:" + h1 + ":c2"}},
		{kind: KindChecksRed, setup: []step{one1(pend), one1(red)}, trigger: one1(red), still: sn(red),
			flapped: []step{one1(pass), one1(pass)}, wantID: "o/r#1:checks-red:" + h1 + ":c1", wantNew: []string{"o/r#1:checks-green:" + h1 + ":c2"}},
		{kind: KindChecksPendingTimout, timeout: 3 * time.Minute, setup: []step{one1(pend), one1(pend), one1(pend), one1(pend)}, trigger: one1(pend), still: sn(pend),
			flapped: []step{one1(pass), one1(pass)}, wantID: "o/r#1:checks-pending-timeout:" + h1 + ":c0:t", wantNew: []string{"o/r#1:checks-green:" + h1 + ":c1"}},
		{kind: KindMergeableChanged, setup: []step{one1(clean), one1(dirty)}, trigger: one1(dirty), still: sn(dirty),
			flapped: []step{one1(clean), one1(clean)}, wantID: "o/r#1:mergeable-changed:" + h1 + ":m1", wantNew: []string{"o/r#1:mergeable-changed:" + h1 + ":m2"}},
		{kind: KindReadyStateChanged, setup: sn(ok(mk(1, draft()))), trigger: one1(ok(mk(1))), still: sn(ok(mk(1))),
			flapped: sn(ok(mk(1, draft()))), wantID: "o/r#1:ready-state-changed:r1", wantNew: []string{"o/r#1:ready-state-changed:r2"}},
		{kind: KindBaseChanged, setup: sn(ok(mk(1))), trigger: one1(ok(mk(1, base("dev")))), still: sn(ok(mk(1, base("dev")))),
			flapped: sn(ok(mk(1))), wantID: "o/r#1:base-changed:b1", wantNew: []string{"o/r#1:base-changed:b2"}},
		{kind: KindMerged, setup: sn(ok(mk(10, head("aa")))), trigger: one1(ok().gone(10, merged(cc, ha))), still: sn(ok().gone(10, merged(cc, ha))),
			flapped: sn(ok(mk(10, head("aa")))), reorder: true, wantID: "o/r#10:merged:" + cc, wantNew: []string{"o/r#10:new-pr:g0"}},
		{kind: KindClosed, setup: sn(ok(mk(5))), trigger: one1(ok().gone(5, closedGone(sha("51")))), still: sn(ok().gone(5, closedGone(sha("51")))),
			flapped: sn(ok(mk(5))), wantID: "o/r#5:closed:g0", wantNew: []string{"o/r#5:new-pr:g1"}},
		{kind: KindRestackNeeded, setup: sn(ok(mk(10, head("aa")), mk(12))), trigger: one1(ok(mk(12)).gone(10, merged(cc, ha))), still: sn(ok(mk(12))),
			flapped: sn(ok(mk(12, head("dd")))), wantID: "o/r#12:restack-needed:10:" + cc, wantNew: []string{"o/r#12:head-changed:" + sha("dd") + ":h1"}},
		{kind: KindReposChanged, repos: []string{"o/a"}, setup: []step{{snaps: oneRepo}},
			trigger: step{repos: []string{"o/a", "o/b"}, snaps: two}, still: []step{{snaps: two}},
			flapped: []step{{repos: []string{"o/a"}, snaps: oneRepo}}, wantID: "-:repos-changed:p1", wantNew: []string{"-:repos-changed:p2"}},
		{kind: KindWatchError, setup: sn(ok(mk(1))), trigger: one1(failWith("network")), still: sn(failWith("network")),
			flapped: sn(ok(mk(1))), wantID: "o/r#-:watch-error:network:e1:f1", wantNew: []string{"o/r#-:watch-recovered:e1"}},
		{kind: KindWatchRecovered, setup: []step{one1(ok(mk(1))), one1(failWith("network"))}, trigger: step{adv: 5 * time.Minute, snaps: map[string]snap{"o/r": ok(mk(1))}}, still: sn(ok(mk(1))),
			flapped: sn(failWith("network")), wantID: "o/r#-:watch-recovered:e1", wantNew: []string{"o/r#-:watch-error:network:e2:f1"}},
		{kind: KindWatchStalled, setup: sn(ok(mk(1))), trigger: step{adv: 11 * time.Minute, snaps: map[string]snap{"o/r": failWith("network")}}, still: sn(failWith("network")),
			flapped: []step{{adv: 20 * time.Minute, snaps: map[string]snap{"o/r": ok(mk(1))}}}, wantID: "o/r#-:watch-stalled:s1",
			wantNew: []string{"o/r#-:watch-recovered:e1", "o/r#-:watch-recovered:s1"}},
	}
}

func (c crashCase) rig(t *testing.T) *rig {
	r := newRig(t, append([]string{}, orDefault(c.repos)...)...)
	r.rs.byMerge[10] = []RestackCandidate{{PR: 12, Reason: ReasonContains, Branch: "feat/12", Argv: []string{"git", "rebase"}}}
	if c.timeout != 0 {
		r.timeout = c.timeout
	}
	return r
}

func orDefault(r []string) []string {
	if len(r) == 0 {
		return []string{"o/r"}
	}
	return r
}

// play runs setup and trigger (optionally crashing at point/n), restarts after a crash and runs the variant.
// It returns the rig, whether the crash happened and the events of the trigger cycle in a crash-free run.
func (c crashCase) play(t *testing.T, variant []step, point string, n int) (*rig, []Event) {
	r := c.rig(t)
	for _, s := range c.setup {
		if _, err := r.do(s); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	var trig []Event
	if point != "" {
		r.faults = crashAt(point, n)
		_, err := r.do(c.trigger)
		if err != errCrash {
			t.Fatalf("expected the crash at %s/%d, got %v", point, n, err)
		}
		r.crashed()
	} else {
		var err error
		if trig, err = r.do(c.trigger); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range variant {
		if _, err := r.do(s); err != nil {
			t.Fatalf("variant: %v", err)
		}
	}
	return r, trig
}

func indexOf(list []string, id string) int {
	for i, x := range list {
		if x == id {
			return i
		}
	}
	return -1
}

var crashCasesRun int

// TestReplayEffectsCrashMatrix is the generated acceptance test of the spec section "Replay effects": for
// each of the 15 kinds it kills the watcher after every journal append, after every print and after the
// state write of the cycle that emits the kind, restarts against an observation that still shows the
// condition and one that shows it flapped back, and compares with the crash-free run.
func TestReplayEffectsCrashMatrix(t *testing.T) {
	cases := crashCases()
	if len(cases) != len(Kinds) {
		t.Fatalf("%d rows for %d kinds", len(cases), len(Kinds))
	}
	for _, c := range cases {
		t.Run(c.kind, func(t *testing.T) {
			type variant struct {
				name  string
				steps []step
			}
			variants := []variant{{"still", c.still}, {"flapped", c.flapped}}
			refs := map[string][]string{}
			var trigIDs []string
			for _, v := range variants {
				r, trig := c.play(t, v.steps, "", 0)
				refs[v.name] = r.journalIDs()
				trigIDs = ids(trig)
				uniq(t, refs[v.name])
				if indexOf(refs[v.name], c.wantID) < 0 {
					t.Fatalf("reference run lacks %s: %v", c.wantID, refs[v.name])
				}
				r.crashed()
			}
			posK := indexOf(trigIDs, c.wantID)
			if posK < 0 {
				t.Fatalf("the trigger cycle did not emit %s: %v", c.wantID, trigIDs)
			}
			if got := refs["flapped"]; !c.reorder && !reflect.DeepEqual(got[len(got)-len(c.wantNew):], c.wantNew) {
				t.Fatalf("flap-back adds %v, want %v", got[len(got)-len(c.wantNew):], c.wantNew)
			}
			type point struct {
				name string
				n    int
			}
			var points []point
			for i := 1; i <= len(trigIDs); i++ {
				points = append(points, point{"append", i}, point{"print", i})
			}
			points = append(points, point{"state", 1})
			for _, p := range points {
				for _, v := range variants {
					// the flap-back is meaningful only once the kind is journalled
					if v.name == "flapped" && p.name == "append" && p.n < posK+1 {
						continue
					}
					crashCasesRun++
					name := fmt.Sprintf("%s/%s%d", v.name, p.name, p.n)
					r, _ := c.play(t, v.steps, p.name, p.n)
					got := r.journalIDs()
					uniq(t, got)
					if !reflect.DeepEqual(got, refs[v.name]) && !(c.reorder && sameSet(got, refs[v.name])) {
						t.Fatalf("%s: journal differs from the crash-free run\n got %v\nwant %v", name, got, refs[v.name])
					}
					count := 0
					for _, id := range got {
						if id == c.wantID {
							count++
						}
					}
					if count != 1 {
						t.Fatalf("%s: %d journal lines for %s", name, count, c.wantID)
					}
					// at least once on stdout, nothing that is not journalled, seq gapless
					out := r.stdoutIDs()
					for _, id := range got {
						if indexOf(out, id) < 0 {
							t.Fatalf("%s: %s never printed", name, id)
						}
					}
					for _, id := range out {
						if indexOf(got, id) < 0 {
							t.Fatalf("%s: printed %s is not journalled", name, id)
						}
					}
					for i, e := range r.journal() {
						if e.Seq != int64(i+1) {
							t.Fatalf("%s: seq gap", name)
						}
					}
					r.crashed()
				}
			}
		})
	}
	t.Logf("generated crash cases: %d", crashCasesRun)
}

// A crash between two appends of one cycle leaves a partial batch: the rest appears after the restart,
// in the order of the crash-free run.
func TestPartialBatchCrashCompletesInOrder(t *testing.T) {
	steps := func() (*rig, []step) {
		r := newRig(t)
		return r, []step{one1(ok(mk(1), mk(2), mk(3))),
			one1(ok(mk(1, head("aa")), mk(2, draft()), mk(3, base("dev")), mk(4)))}
	}
	r0, st := steps()
	for _, s := range st {
		r0.do(s)
	}
	want := r0.journalIDs()
	if len(want) != 4 {
		t.Fatal(want)
	}
	for k := 1; k <= 3; k++ {
		r, st := steps()
		r.do(st[0])
		r.faults = crashAt("append", k)
		if _, err := r.do(st[1]); err != errCrash {
			t.Fatal(err)
		}
		if got := len(r.journal()); got != k {
			t.Fatalf("partial batch holds %d lines, want %d", got, k)
		}
		r.crashed()
		r.now = r.now.Add(-time.Minute) // the restart polls at the time of the cycle
		r.do(step{snaps: map[string]snap{"o/r": st[1].snaps["o/r"]}})
		if got := r.journalIDs(); !reflect.DeepEqual(got, want) {
			t.Fatalf("k=%d\n got %v\nwant %v", k, got, want)
		}
	}
}

func TestStdoutHasTheEventTwiceWhenTheCrashFollowsThePrint(t *testing.T) {
	r := newRig(t)
	r.do(one1(ok(mk(1))))
	r.faults = crashAt("print", 1)
	if _, err := r.do(one1(ok(mk(1), mk(2)))); err != errCrash {
		t.Fatal(err)
	}
	r.crashed()
	r.mustOpen() // the replay at start re-prints every journal line past printed_seq
	if got := r.stdoutIDs(); !reflect.DeepEqual(got, []string{"o/r#2:new-pr:g0", "o/r#2:new-pr:g0"}) {
		t.Fatal(got)
	}
	if len(r.journal()) != 1 {
		t.Fatal("journal must hold it once")
	}
	r.do(one1(ok(mk(1), mk(2))))
	if len(r.stdoutIDs()) != 2 {
		t.Fatal("printed again after a completed cycle")
	}
}

func TestCrashAfterAppendBeforePrintPrintsOnceOnRestart(t *testing.T) {
	r := newRig(t)
	r.do(one1(ok(mk(1))))
	r.faults = crashAt("append", 1)
	r.do(one1(ok(mk(1), mk(2))))
	r.crashed()
	if r.out.Len() != 0 {
		t.Fatal("nothing printed before the crash")
	}
	r.mustOpen()
	if got := r.stdoutIDs(); !reflect.DeepEqual(got, []string{"o/r#2:new-pr:g0"}) {
		t.Fatal(got)
	}
}

func TestFlapBackAfterCrashIsANewEventWithTheNextEpochAndNothingIsLost(t *testing.T) {
	r := newRig(t)
	r.do(one1(ok(mk(1, head("aa")))))
	r.faults = crashAt("append", 1)
	r.do(one1(ok(mk(1, head("bb")))))
	r.crashed()
	r.do(one1(ok(mk(1, head("aa")))))
	r.do(one1(ok(mk(1, head("cc")))))
	want := []string{"o/r#1:head-changed:" + sha("bb") + ":h1", "o/r#1:head-changed:" + sha("aa") + ":h2", "o/r#1:head-changed:" + sha("cc") + ":h3"}
	if got := r.journalIDs(); !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, x := range a {
		if indexOf(b, x) < 0 {
			return false
		}
	}
	return true
}
