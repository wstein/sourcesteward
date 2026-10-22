// SPDX-License-Identifier: EUPL-1.2
package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"testing"
	"time"
)

// worldPR is the hidden truth of one pull request in the generated forge.
type worldPR struct {
	present, dead bool
	fate          string // merged, closed, unreadable (for one cycle) once absent
	unreadable    int
	head          int
	draft         bool
	base          int
	checks        int
	detail        int
	commit        int
}

var (
	fuzzHeads   = []string{"aa", "bb", "cc"}
	fuzzBases   = []string{"main", "dev"}
	fuzzCommits = []string{"c1", "c2", "c3", "c4", "c5", "c6"}
)

func (w *worldPR) obs(n int) PRObs {
	p := mk(n, head(fuzzHeads[w.head]), base(fuzzBases[w.base]))
	p.Draft = w.draft
	switch w.checks {
	case 1:
		runs(running())(&p)
	case 2:
		runs(passing())(&p)
	case 3:
		runs(failing("t", "--- FAIL: TestX"))(&p)
	}
	switch w.detail {
	case 1:
		detail(tt(), "clean")(&p)
	case 2:
		detail(ff(), "dirty")(&p)
	case 3:
		detail(tt(), "behind")(&p)
	case 4:
		detail(nil, "unknown")(&p)
	}
	return p
}

// genSteps builds a reproducible sequence of forge answers for two repositories, with failures, merges,
// closes, reopens, unreadable merges and repository set changes.
func genSteps(seed int64, cycles int) []step {
	rng := rand.New(rand.NewSource(seed))
	repos := []string{"o/a", "o/b"}
	world := map[string]map[int]*worldPR{"o/a": {}, "o/b": {}}
	for _, r := range repos {
		for n := 1; n <= 5; n++ {
			world[r][n] = &worldPR{present: rng.Intn(2) == 0}
		}
	}
	streak := map[string]int{}
	cfg := []string{"o/a", "o/b"}
	var out []step
	for i := 0; i < cycles; i++ {
		st := step{snaps: map[string]snap{}}
		if i > 0 && rng.Intn(12) == 0 {
			if len(cfg) == 2 {
				cfg = []string{"o/a"}
			} else {
				cfg = []string{"o/a", "o/b"}
			}
			st.repos = append([]string{}, cfg...)
		}
		for _, repo := range repos {
			if i > 0 && streak[repo] < 2 && rng.Intn(12) == 0 {
				streak[repo]++
				codes := []string{"network", "http-5xx", "unauthorized"}
				st.snaps[repo] = failWith(codes[rng.Intn(len(codes))])
				continue
			}
			streak[repo] = 0
			var prs []PRObs
			gone := map[int]GoneObs{}
			for n := 1; n <= 5; n++ {
				w := world[repo][n]
				if i > 0 {
					switch {
					case w.present && !w.dead && rng.Intn(10) == 0:
						w.present = false
						w.fate = []string{"merged", "closed", "closed", "unreadable"}[rng.Intn(4)]
						if w.fate == "unreadable" {
							w.unreadable = 1 + rng.Intn(2)
						}
						w.commit = rng.Intn(len(fuzzCommits))
					case !w.present && !w.dead && w.fate != "merged" && rng.Intn(6) == 0:
						w.present = true
					}
					if w.present {
						if rng.Intn(8) == 0 {
							w.head = rng.Intn(len(fuzzHeads))
						}
						if rng.Intn(10) == 0 {
							w.draft = !w.draft
						}
						if rng.Intn(12) == 0 {
							w.base = rng.Intn(len(fuzzBases))
						}
						if rng.Intn(3) == 0 {
							w.checks = rng.Intn(4)
						}
						if rng.Intn(4) == 0 {
							w.detail = rng.Intn(5)
						}
					}
				}
				if w.present {
					prs = append(prs, w.obs(n))
					continue
				}
				if w.fate == "" {
					continue
				}
				if w.fate == "unreadable" && w.unreadable > 0 {
					w.unreadable--
					continue
				}
				fate := w.fate
				if fate == "unreadable" {
					fate, w.fate = "merged", "merged"
				}
				if fate == "merged" {
					w.dead = true
					gone[n] = merged(sha(fuzzCommits[w.commit]), sha(fuzzHeads[w.head]))
				} else {
					gone[n] = closedGone(sha(fuzzHeads[w.head]))
				}
			}
			st.snaps[repo] = snap{obs: RepoObservation{DefaultBranch: "main", PRs: prs, Gone: gone}}
		}
		out = append(out, st)
	}
	return out
}

// fuzzRestacker names every open PR numbered above the merged one: a pure function of the open set.
type fuzzRestacker struct{}

func (fuzzRestacker) Candidates(ctx context.Context, repo string, due RestackDue, open []OpenPR) ([]RestackCandidate, error) {
	var out []RestackCandidate
	for _, p := range open {
		if p.Number > due.MergedPR {
			out = append(out, RestackCandidate{PR: p.Number, Reason: ReasonContains, Branch: p.HeadRef, Argv: []string{"git", "rebase"}})
		}
	}
	return out, nil
}

// runSeq plays the steps; with a non-nil rng every durable step may crash (also during the restart), and
// the same poll is then repeated by a fresh process at the same fake time.
func runSeq(t *testing.T, steps []step, rng *rand.Rand) (*rig, int) {
	r := newRig(t, "o/a", "o/b")
	r.timeout = 5 * time.Minute
	r.rs = nil
	opts := func() Options {
		o := r.options()
		o.Restacker = fuzzRestacker{}
		return o
	}
	crashes := 0
	budget := 0
	lastPoint := ""
	r.faults = func(p string, n int) error {
		if rng != nil && budget > 0 && rng.Intn(100) < 14 {
			budget--
			crashes++
			lastPoint = p
			return errCrash
		}
		return nil
	}
	open := func() error {
		if r.w != nil {
			r.w.Close()
			r.w = nil
		}
		w, err := Open(opts())
		if err != nil {
			return err
		}
		r.w = w
		return nil
	}
	for i, st := range steps {
		if st.repos != nil {
			r.repos = st.repos
			if r.w != nil { // a config change is a restart
				r.w.Close()
				r.w = nil
			}
		}
		for repo, s := range st.snaps {
			r.set(repo, s)
		}
		budget = 3
		for attempt := 0; ; attempt++ {
			if attempt > 20 {
				t.Fatalf("step %d: no progress", i)
			}
			if r.w == nil {
				if err := open(); err != nil {
					if err == errCrash {
						continue
					}
					t.Fatalf("step %d open: %v", i, err)
				}
			}
			r.now = rigStart.Add(time.Duration(i+1) * time.Minute)
			_, err := r.w.Cycle(context.Background())
			if err == nil {
				break
			}
			if err != errCrash {
				t.Fatalf("step %d: %v", i, err)
			}
			r.w.Close()
			r.w = nil
			if lastPoint == "state" { // the cycle was complete: the next poll is the next snapshot
				break
			}
		}
		budget = 0
	}
	if r.w != nil {
		r.w.Close()
		r.w = nil
	}
	return r, crashes
}

// TestRandomCrashesYieldTheCrashFreeJournal is the property test: for random snapshot sequences and random
// crash points the final journal has the same ids in the same order as the crash-free run.
func TestRandomCrashesYieldTheCrashFreeJournal(t *testing.T) {
	seeds, cycles := 12, 14
	if testing.Short() {
		seeds, cycles = 6, 10
	}
	totalCrashes, totalEvents := 0, 0
	for seed := int64(1); seed <= int64(seeds); seed++ {
		steps := genSteps(seed, cycles)
		ref, _ := runSeq(t, steps, nil)
		want := ref.journalIDs()
		uniq(t, want)
		totalEvents += len(want)
		got, crashes := runSeq(t, steps, rand.New(rand.NewSource(seed*7919)))
		totalCrashes += crashes
		have := got.journalIDs()
		if !reflect.DeepEqual(have, want) {
			t.Fatalf("seed %d (%d crashes): journal differs\n got %v\nwant %v", seed, crashes, have, want)
		}
		for i, e := range got.journal() {
			if e.Seq != int64(i+1) {
				t.Fatalf("seed %d: seq gap", seed)
			}
		}
		out := got.stdoutIDs()
		for _, id := range have {
			if indexOf(out, id) < 0 {
				t.Fatalf("seed %d: %s never printed", seed, id)
			}
		}
		for _, id := range out {
			if indexOf(have, id) < 0 {
				t.Fatalf("seed %d: printed %s is not journalled", seed, id)
			}
		}
		// stdout minus duplicates is the journal in order
		var dedup []string
		seen := map[string]bool{}
		for _, id := range out {
			if !seen[id] {
				seen[id] = true
				dedup = append(dedup, id)
			}
		}
		if !reflect.DeepEqual(dedup, have) {
			t.Fatalf("seed %d: stdout order differs from the journal", seed)
		}
		if a, b := stateJSON(t, ref), stateJSON(t, got); a != b {
			i := 0
			for i < len(a) && i < len(b) && a[i] == b[i] {
				i++
			}
			lo := max(0, i-200)
			t.Fatalf("seed %d: final state differs at %d\n got ...%s\nwant ...%s", seed, i, b[lo:min(len(b), i+100)], a[lo:min(len(a), i+100)])
		}
	}
	t.Logf("%d sequences, %d journalled events, %d injected crashes", seeds, totalEvents, totalCrashes)
	if totalCrashes == 0 || totalEvents == 0 {
		t.Fatal("the generator produced no crashes or no events")
	}
}

func stateJSON(t *testing.T, r *rig) string {
	s := r.state()
	// detail_due is a read hint: a rerun poll that read the detail anyway clears it, a crash-free run
	// keeps it one cycle longer. It never changes an event, so it is not part of the comparison.
	for _, rs := range s.Repo {
		// the closed list is ordered by when it was recorded, which only matters for evicting the oldest
		sort.Slice(rs.Closed, func(i, j int) bool { return rs.Closed[i].Number < rs.Closed[j].Number })
		for _, p := range rs.PRs {
			p.DetailDue = false
		}
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprint(string(raw))
}
