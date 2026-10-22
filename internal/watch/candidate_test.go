// SPDX-License-Identifier: EUPL-1.2
package watch

import "testing"

// A debounce candidate of the volatile fields needs a second consecutive sighting, so the observer must be
// told to read afresh: a stored class candidate and a stored mergeability candidate both show in the hints.
func TestStoredCandidatesAreVisibleToTheObserver(t *testing.T) {
	r := newRig(t)
	clean := ok(mk(1, detail(tt(), "clean"), runs(passing())))
	r.must(clean) // baseline
	r.must(ok(mk(1, detail(ff(), "dirty"), runs(failing("x", "")))))
	var k KnownPR
	r.must(clean) // the next cycle starts from the stored candidates; its hints are taken before the poll
	for _, c := range r.obs.hints["o/r"].Known {
		if c.Number == 1 {
			k = c
		}
	}
	if k.ClassCand != ClassRed {
		t.Fatalf("class candidate not in the hints: %+v", k)
	}
	if !k.DetailDue {
		t.Fatalf("a mergeability candidate must make the detail read due: %+v", k)
	}
}

// A confirming read that finds mergeability still being computed (null) must not drop the due flag: the
// candidate resolves on the next non-null read, not at the slow refresh, and ids stay unique.
func TestNullMergeableKeepsTheDetailReadDueWhileACandidateIsStored(t *testing.T) {
	r := newRig(t)
	due := func() bool {
		for _, c := range r.obs.hints["o/r"].Known {
			if c.Number == 1 {
				return c.DetailDue
			}
		}
		t.Fatal("PR 1 not in the hints")
		return false
	}
	r.must(ok(mk(1, detail(tt(), "clean"))))  // baseline
	r.must(ok(mk(1, detail(ff(), "dirty"))))  // first sighting: candidate
	r.must(ok(mk(1, detail(nil, "unknown")))) // GitHub is still computing
	r.must(ok(mk(1, detail(nil, "unknown"))))
	if !due() {
		t.Fatal("the detail read must stay due while a candidate waits")
	}
	evs := r.must(ok(mk(1, detail(ff(), "dirty"))))
	if len(evs) != 1 || evs[0].Kind != KindMergeableChanged {
		t.Fatalf("the candidate must resolve on the next non-null read: %v", ids(evs))
	}
	r.must(ok(mk(1, detail(ff(), "dirty"))))
	if due() {
		t.Fatal("nothing is pending any more")
	}
	uniq(t, r.journalIDs())
}
