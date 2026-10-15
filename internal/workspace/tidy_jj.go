// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"github.com/wstein/sourcesteward/internal/inspection"
)

type jjTidyAdapter struct{}

func (jjTidyAdapter) Observe(ctx context.Context, path, base string) (TidyObservation, error) {
	state, err := inspection.ObserveJJTidy(ctx, path, base)
	if err != nil {
		return TidyObservation{}, err
	}
	o := TidyObservation{Expected: jjSnapshot(state.Expected), Candidates: []TidyCandidate{}, Limitations: state.Limitations}
	for _, c := range state.Candidates {
		o.Candidates = append(o.Candidates, TidyCandidate{Kind: "bookmark", Name: c.Name, CommitID: c.CommitID, ChangeID: c.ChangeID, Decision: c.Decision, Reason: c.Reason, Proof: c.Proof, WorkspacePaths: c.WorkspacePaths, Limitations: c.Limitations})
	}
	return o, nil
}
