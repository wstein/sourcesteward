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
