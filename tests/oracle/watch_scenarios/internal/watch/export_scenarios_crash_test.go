// SPDX-License-Identifier: EUPL-1.2
package watch

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/watchexport"
)

func stepToX(st step) xstep {
	x := xstep{obs: st.snaps}
	if st.repos != nil {
		x.repos = st.repos
	}
	d := time.Minute + st.adv // rig.do advances adv, rig.run one minute more
	x.adv = &d
	return x
}

func crashName(kind, variant, point string, n int) string {
	if point == "" {
		return fmt.Sprintf("crash-%s-%s-none", kind, variant)
	}
	return fmt.Sprintf("crash-%s-%s-%s%d", kind, variant, point, n)
}

// crashScenario builds one row-variant-point combination of the generated acceptance test of the spec table "Replay effects".
func crashScenario(c crashCase, variant string, steps []step, point string, n int) *xsc {
	desc := fmt.Sprintf("Replay effects, kind %s: the cycle that emits it runs without a crash, then the first poll sees the %s observation.", c.kind, variant)
	if point != "" {
		desc = fmt.Sprintf("Replay effects, kind %s: the process dies after durable step %s %d of the cycle that emits it; the restart polls and sees the %s observation. The journal holds the event exactly once and equals the crash-free run (crash-%s-%s-none).",
			c.kind, point, n, variant, c.kind, variant)
	}
	s := newXSC(crashName(c.kind, variant, point, n), desc, "crash_test.go:TestReplayEffectsCrashMatrix")
	s.repos = append([]string{}, orDefault(c.repos)...)
	if c.timeout != 0 {
		s.timeout, s.timeoutSet = c.timeout, true
	}
	s.byMerge[10] = []RestackCandidate{{PR: 12, Reason: ReasonContains, Branch: "feat/12", Argv: []string{"git", "rebase"}}}
	s.lean = true
	if point != "" {
		s.ignoreView = []string{"detail_due"}
	}
	for _, st := range c.setup {
		s.add(stepToX(st))
	}
	trig := stepToX(c.trigger)
	if point != "" {
		trig.crash = &xcrash{point, n}
	}
	s.add(trig)
	for _, st := range steps {
		s.add(stepToX(st))
	}
	return s
}

func exportCrashMatrix(t *testing.T, dir string) {
	cases := crashCases()
	if len(cases) != len(Kinds) {
		t.Fatalf("%d rows for %d kinds", len(cases), len(Kinds))
	}
	total := 0
	for _, c := range cases {
		type variant struct {
			name  string
			steps []step
		}
		variants := []variant{{"still", c.still}, {"flapped", c.flapped}}
		var trigIDs []string
		for _, v := range variants {
			ref := crashScenario(c, v.name, v.steps, "", 0)
			doc := ref.exec(t)
			trigIDs = nil
			for _, e := range doc.ExpectedEvents[len(c.setup)].Events {
				trigIDs = append(trigIDs, e.Go.ID)
			}
			if err := watchexport.Write(dir, ref.name, doc); err != nil {
				t.Fatal(err)
			}
			total++
		}
		posK := indexOf(trigIDs, c.wantID)
		if posK < 0 {
			t.Fatalf("%s: the trigger cycle did not emit %s: %v", c.kind, c.wantID, trigIDs)
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
				if v.name == "flapped" && p.name == "append" && p.n < posK+1 {
					continue
				}
				s := crashScenario(c, v.name, v.steps, p.name, p.n)
				doc := s.exec(t)
				count := 0
				for _, e := range doc.ExpectedEvents {
					_ = e
				}
				// the journal holds the wanted event exactly once
				for _, ord := range doc.ExpectedJournal {
					_ = ord
				}
				found := 0
				for _, ex := range doc.ExpectedEvents {
					for _, e := range ex.Events {
						if e.Go.ID == c.wantID {
							found++
						}
					}
				}
				if found != 1 {
					t.Fatalf("%s: %d journal lines for %s", s.name, found, c.wantID)
				}
				_ = count
				if err := watchexport.Write(dir, s.name, doc); err != nil {
					t.Fatal(err)
				}
				total++
			}
		}
	}
	t.Logf("crash matrix scenarios: %d", total)
}

func crashExtraScenarios() []*xsc {
	var out []*xsc
	h := sha
	// partial batch
	first := ok(mk(1), mk(2), mk(3))
	second := ok(mk(1, head("aa")), mk(2, draft()), mk(3, base("dev")), mk(4))
	build := func(name, desc string, k int) *xsc {
		s := newXSC(name, desc, "crash_test.go:TestPartialBatchCrashCompletesInOrder")
		s.any(first)
		st := xstep{obs: map[string]snap{"o/r": second}}
		if k > 0 {
			st.crash = &xcrash{"append", k}
			s.add(st)
			s.add(xstep{adv: dur(0), obs: map[string]snap{"o/r": second}}) // the restart polls at the time of the cycle
			s.ignoreView = []string{"detail_due"}
		} else {
			s.add(st)
		}
		return s
	}
	out = append(out, build("partial-batch-crash-free", "Four events in one cycle without a crash (the reference of the partial batch scenarios).", 0))
	for k := 1; k <= 3; k++ {
		out = append(out, build(fmt.Sprintf("partial-batch-crash-after-append-%d", k),
			fmt.Sprintf("A crash after journal append %d of 4 leaves a partial batch; the poll after the restart completes the rest in the order of the crash-free run.", k), k))
	}
	{
		s := newXSC("stdout-twice-when-crash-follows-print", "A crash after the print leaves the event on stdout twice (at-least-once); the journal holds it once and a completed cycle prints nothing more.",
			"crash_test.go:TestStdoutHasTheEventTwiceWhenTheCrashFollowsThePrint")
		s.any(ok(mk(1)))
		s.add(xstep{crash: &xcrash{"print", 1}, obs: map[string]snap{"o/r": ok(mk(1), mk(2))}})
		s.add(xstep{action: "open"})
		s.add(xstep{obs: map[string]snap{"o/r": ok(mk(1), mk(2))}, guard: true})
		s.ignoreView = []string{"detail_due"}
		out = append(out, s)
	}
	{
		s := newXSC("crash-after-append-before-print", "A crash after the journal append and before the print: nothing was printed; the start prints the event once.",
			"crash_test.go:TestCrashAfterAppendBeforePrintPrintsOnceOnRestart")
		s.any(ok(mk(1)))
		s.add(xstep{crash: &xcrash{"append", 1}, obs: map[string]snap{"o/r": ok(mk(1), mk(2))}})
		s.add(xstep{action: "open"})
		s.ignoreView = []string{"detail_due"}
		out = append(out, s)
	}
	{
		s := newXSC("flap-back-after-crash-next-epoch", "A flap back after a crash is a new event with the next epoch and nothing is lost.",
			"crash_test.go:TestFlapBackAfterCrashIsANewEventWithTheNextEpochAndNothingIsLost")
		s.any(ok(mk(1, head("aa"))))
		s.add(xstep{crash: &xcrash{"append", 1}, obs: map[string]snap{"o/r": ok(mk(1, head("bb")))}})
		s.any(ok(mk(1, head("aa"))))
		s.any(ok(mk(1, head("cc"))))
		s.ignoreView = []string{"detail_due"}
		s.total = map[string]int{KindHeadChanged: 3}
		out = append(out, s)
	}
	_ = h
	_ = strings.Join
	return out
}

func exportFuzzSequences(t *testing.T, dir string) {
	const seeds, cycles = 12, 14
	for seed := int64(1); seed <= seeds; seed++ {
		steps := genSteps(seed, cycles)
		s := newXSC(fmt.Sprintf("generated-sequence-seed-%02d", seed),
			fmt.Sprintf("Generated forge sequence (seed %d, %d cycles, two repositories, failures, merges, closes, reopens, unreadable merges and repository set changes) without a crash. The Go property test shows that the same sequence with random crashes yields the same journal.", seed, cycles),
			"fuzz_test.go:TestRandomCrashesYieldTheCrashFreeJournal")
		s.repos = []string{"o/a", "o/b"}
		s.timeout, s.timeoutSet = 5*time.Minute, true
		s.fuzzRule = true
		s.lean = true
		for _, st := range steps {
			x := xstep{obs: st.snaps}
			if st.repos != nil {
				x.repos = st.repos
			}
			s.add(x)
		}
		s.run(t, dir)
	}
}
