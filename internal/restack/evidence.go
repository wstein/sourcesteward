// SPDX-License-Identifier: EUPL-1.2
package restack

import "context"

// collect runs the replay and every evidence class over the explicit input (FR-013).
func (p *planner) collect(ctx context.Context) error {
	e := &Evidence{
		RangePairing:     RangePairing{Rows: []PairRow{}},
		ParentDropped:    ParentDropped{Rows: []DroppedRow{}, State: "unknown"},
		OwnDiffEquality:  OwnDiffs{Rows: []OwnDiffRow{}},
		Identity:         IdentityCheck{State: "unknown", Source: "none", Expected: []string{}, Mismatches: []IdentityMismatch{}},
		Conflicts:        Conflicts{Items: []ConflictItem{}},
		Overlap:          Overlap{Paths: []string{}, Indicator: "unknown", Note: overlapNote},
		EmptyAfterReplay: []string{},
	}
	p.res.Evidence = e
	out, err := p.replay(ctx)
	if err != nil {
		return err
	}
	e.EmptyAfterReplay = append(e.EmptyAfterReplay, p.emptyAfter...)
	e.Replay = Replay{State: "clean", CommitsReplayed: p.replayedCount}
	if out.conflict != nil {
		e.Replay.State = "conflict"
		e.Conflicts.Predicted = true
		e.Conflicts.Items = out.conflict.items
		commit := p.moved[out.conflictAt].oid
		e.Conflicts.Commit = &commit
		if out.conflictAt+1 < len(p.moved) {
			next := p.moved[out.conflictAt+1].oid
			e.Conflicts.ReplayStoppedAt = &next
		}
	} else {
		final := out.tipTree
		e.Replay.FinalTree = &final
		// Cross-check: one three-way merge of the branch tip, the old parent and the new parent
		// must give the tree that the commit-by-commit replay reached.
		oneShot, conflict, err := p.mergeTree(ctx, *p.res.Input.OldParent.OID, *p.res.Input.NewParent.OID, *p.res.Input.Branch.OldHead)
		if err != nil {
			return err
		}
		same := conflict == nil && oneShot == final
		e.Replay.TreeEqualsOneShot = &same
	}
	return nil
}

const overlapNote = "A risk indicator only: an empty overlap is not evidence of safety."
