// SPDX-License-Identifier: EUPL-1.2
package restack

import "testing"

// dims are the nine dimensions of the outcome decision table of the contract.
type dims struct{ replay, moved, pairing, own, dropped, empty, identity, overlap, held int }

func (d dims) result() *Result {
	r := &Result{Status: StatusOK, Evidence: &Evidence{}}
	e := r.Evidence
	e.Replay.State = []string{"clean", "conflict", "incomplete"}[d.replay]
	n := []int{0, 2}[d.moved]
	r.Input.Moved.Count = n
	for i := 0; i < n; i++ {
		mark := []string{"=", "!", "unpaired"}[d.pairing]
		e.RangePairing.Rows = append(e.RangePairing.Rows, PairRow{Mark: mark})
		e.OwnDiffEquality.Rows = append(e.OwnDiffEquality.Rows, OwnDiffRow{State: []string{"equal", "differs", "unknown"}[d.own]})
	}
	e.ParentDropped.AllDropped = d.dropped == 0
	e.ParentDropped.State = []string{"dropped", "missing", "unknown"}[d.dropped]
	if d.empty == 1 {
		e.EmptyAfterReplay = []string{"x"}
	}
	e.Identity.State = []string{"pass", "fail", "unknown"}[d.identity]
	e.Identity.Source = "flag"
	if d.identity == 2 {
		e.Identity.Source = "none"
	}
	e.Overlap.Indicator = []string{"none", "some", "unknown"}[d.overlap]
	if d.held == 1 {
		r.Effects.AffectedRefs = []AffectedRef{{Ref: "refs/heads/other"}}
	}
	return r
}

// want is the contract's table written independently of decideOutcome.
func (d dims) want() string {
	if d.replay == 1 {
		return OutcomeConflicts
	}
	if d.replay == 0 && d.moved == 1 && d.pairing == 0 && d.own == 0 && d.dropped == 0 && d.empty == 0 && d.identity == 0 && d.overlap == 0 && d.held == 0 {
		return OutcomeClean
	}
	return OutcomeAttention
}

func TestOutcomeTableOverTheFullCartesianProduct(t *testing.T) {
	cases, clean := 0, 0
	for replay := 0; replay < 3; replay++ {
		for moved := 0; moved < 2; moved++ {
			for pairing := 0; pairing < 3; pairing++ {
				for own := 0; own < 3; own++ {
					for dropped := 0; dropped < 3; dropped++ {
						for empty := 0; empty < 2; empty++ {
							for identity := 0; identity < 3; identity++ {
								for overlap := 0; overlap < 3; overlap++ {
									for held := 0; held < 2; held++ {
										d := dims{replay, moved, pairing, own, dropped, empty, identity, overlap, held}
										cases++
										got := decideOutcome(d.result())
										if got != d.want() {
											t.Fatalf("%+v: got %s want %s", d, got, d.want())
										}
										if got == OutcomeClean {
											clean++
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}
	if cases != 5832 || clean != 1 {
		t.Fatalf("cases %d clean %d", cases, clean)
	}
}

func TestOutcomeStatusRowsAndValuesOutsideTheTable(t *testing.T) {
	good := dims{moved: 1}.result()
	if decideOutcome(good) != OutcomeClean {
		t.Fatal("the all-positive input must be evidence-clean")
	}
	for _, status := range []string{StatusIncomplete, StatusError} {
		r := dims{moved: 1}.result()
		r.Status = status
		if decideOutcome(r) != OutcomeNone {
			t.Errorf("%s did not give none", status)
		}
	}
	outside := map[string]func(*Result){
		"future replay state":   func(r *Result) { r.Evidence.Replay.State = "future" },
		"future pairing mark":   func(r *Result) { r.Evidence.RangePairing.Rows[0].Mark = "~" },
		"future own state":      func(r *Result) { r.Evidence.OwnDiffEquality.Rows[0].State = "future" },
		"future identity state": func(r *Result) { r.Evidence.Identity.State = "future" },
		"pass without flag":     func(r *Result) { r.Evidence.Identity.Source = "none" },
		"future overlap":        func(r *Result) { r.Evidence.Overlap.Indicator = "future" },
		"future parent state": func(r *Result) {
			r.Evidence.ParentDropped.State = "future"
			r.Evidence.ParentDropped.AllDropped = false
		},
		"row count mismatch":    func(r *Result) { r.Evidence.RangePairing.Rows = r.Evidence.RangePairing.Rows[:1] },
		"holds-branch worktree": func(r *Result) { r.Effects.Worktrees = []Worktree{{State: "holds-branch"}} },
		"no evidence":           func(r *Result) { r.Evidence = nil },
	}
	for name, mutate := range outside {
		r := dims{moved: 1}.result()
		mutate(r)
		if got := decideOutcome(r); got != OutcomeAttention {
			t.Errorf("%s: got %s", name, got)
		}
	}
}
