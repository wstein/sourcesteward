// SPDX-License-Identifier: EUPL-1.2

package tidywizard

// AttachPR records on each candidate the number of the pull request merged into the base whose
// head is its tip, from the per-tip facts of the run (Spec 044 C2). It uses the facts as they are:
// a tip that was not asked, a repository judged offline, an unknown or closed-unmerged answer
// and a merged answer without a number set nothing, so the offline levels apply (fail closed). It
// changes no verdict, proof or effect; the number is presentation evidence for the risk ladder
// (which lifts only a safe row) and the `pr` member of the JSON document.
func (o OnlineOutcome) AttachPR(cands []Candidate) {
	for i := range cands {
		if f, ok := o.Lookup(cands[i]); ok && f.State == PRMerged && f.Number > 0 {
			cands[i].PR = f.Number
		}
	}
}
