// SPDX-License-Identifier: EUPL-1.2
package restack

import (
	"bytes"
	"context"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

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
	e.RangePairing, err = p.pairRange(ctx, out)
	if err != nil {
		return err
	}
	if e.ParentDropped, err = p.parentDropped(ctx); err != nil {
		return err
	}
	if e.OwnDiffEquality, err = p.ownDiffs(ctx, out); err != nil {
		return err
	}
	e.Identity = p.identityCheck()
	e.Overlap, err = p.overlap(ctx)
	return err
}

const overlapNote = "A risk indicator only: an empty overlap is not evidence of safety."

var rangeDiffRow = regexp.MustCompile(`^\s*(\d+|-):\s+(?:[0-9a-f]+|-+) ([=!<>]) (\d+|-):\s+(?:[0-9a-f]+|-+)(?: |$)`)

// pairRange pairs the old commits with the replay commits by `git range-diff`, whose marks are
// reported as they are. A commit that has no replay commit (a conflict stopped before it, or its
// change was already contained) is `unpaired`.
func (p *planner) pairRange(ctx context.Context, out replayOutcome) (RangePairing, error) {
	rp := RangePairing{Rows: make([]PairRow, len(p.moved))}
	byOld := map[int]string{}
	if p.replayedCount > 0 {
		o, err := p.git.run(ctx, "range-diff", "--no-patch", "--no-color", "--creation-factor=60",
			*p.res.Input.OldParent.OID+".."+*p.res.Input.Branch.OldHead, *p.res.Input.NewParent.OID+".."+out.tip)
		if err != nil {
			return rp, err
		}
		parsed := o.code == 0
		for _, line := range strings.Split(strings.TrimRight(string(o.out), "\n"), "\n") {
			m := rangeDiffRow.FindStringSubmatch(line)
			if m == nil {
				parsed = false
				break
			}
			if n, err := strconv.Atoi(m[1]); err == nil {
				byOld[n-1] = m[2]
			}
		}
		if !parsed {
			byOld = nil
		}
	}
	rp.AllEqual = len(p.moved) > 0
	for i, c := range p.moved {
		mark := "unpaired"
		if p.replayed[i] != "" {
			if mark = byOld[i]; mark == "" {
				mark = "unknown"
			}
		}
		rp.Rows[i] = PairRow{Old: c.oid, Mark: mark}
		rp.AllEqual = rp.AllEqual && mark == "="
	}
	return rp, nil
}

// parentDropped classifies the parent's own commits that the new parent does not reach by `git
// cherry`; a `+` row gets a tree-equivalence probe of the paths it touches.
func (p *planner) parentDropped(ctx context.Context) (ParentDropped, error) {
	d := ParentDropped{Rows: []DroppedRow{}, State: "unknown"}
	o, err := p.git.run(ctx, "cherry", *p.res.Input.NewParent.OID, *p.res.Input.OldParent.OID)
	if err != nil {
		return d, err
	}
	if o.code != 0 {
		d.Reason = "object-missing"
		return d, nil
	}
	lines := strings.Fields(string(o.out))
	if len(lines) == 0 {
		d.Reason = "empty-parent-range"
		return d, nil
	}
	if len(lines)/2 > MaxCommits {
		return d, boundHit{"bound-exceeded", "max_commits"}
	}
	var differing map[string]bool
	d.AllDropped = true
	for i := 0; i+1 < len(lines); i += 2 {
		row := DroppedRow{OID: lines[i+1], Mark: lines[i]}
		switch row.Mark {
		case "-":
			row.State = "dropped"
		case "+":
			if differing == nil {
				if differing, err = p.treeDifferences(ctx); err != nil {
					row.State, row.Reason = "unknown", "limit"
					break
				}
			}
			touched, terr := p.touchedBy(ctx, row.OID)
			switch {
			case terr != nil && errors.As(terr, new(boundHit)):
				return d, terr
			case terr != nil || differing == nil:
				row.State, row.Reason = "unknown", "limit"
			default:
				row.State = "patch-equivalent-tree"
				for _, path := range touched {
					if differing[path] {
						row.State = "missing"
						break
					}
				}
			}
		default:
			row.State = "unknown"
		}
		d.AllDropped = d.AllDropped && (row.State == "dropped" || row.State == "patch-equivalent-tree")
		d.Rows = append(d.Rows, row)
	}
	d.State = "dropped"
	if !d.AllDropped {
		d.State = "unknown"
		for _, row := range d.Rows {
			if row.State == "missing" {
				d.State = "missing"
			}
		}
	}
	return d, nil
}

// treeDifferences is the set of paths whose entry differs between the old and the new parent.
// A nil set with a nil error cannot happen; an error means the set could not be read.
func (p *planner) treeDifferences(ctx context.Context) (map[string]bool, error) {
	o, err := p.git.run(ctx, "diff-tree", "-r", "--name-only", "-z", "--no-renames", *p.res.Input.OldParent.OID, *p.res.Input.NewParent.OID)
	if errors.Is(err, errOutputBound) {
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	if o.code != 0 {
		return nil, errCommandFailed
	}
	set := map[string]bool{}
	for _, f := range bytes.Split(o.out, []byte{0}) {
		if len(f) > 0 {
			set[string(f)] = true
		}
	}
	return set, nil
}

// touchedBy lists the paths one commit changes.
func (p *planner) touchedBy(ctx context.Context, oid string) ([]string, error) {
	o, err := p.git.run(ctx, "diff-tree", "--root", "-r", "--no-commit-id", "--name-only", "-z", "--no-renames", oid)
	if err != nil {
		return nil, err
	}
	if o.code != 0 {
		return nil, errCommandFailed
	}
	return nulPaths(o.out)
}

// patchIDs maps each commit of a range to its stable patch id; a commit without changes has none.
func (p *planner) patchIDs(ctx context.Context, spec string) (map[string]string, error) {
	log, err := p.git.start(ctx, false, nil, nil, patchOutputLimit, "log", "--no-merges", "-p", "--binary", "--no-ext-diff", "--no-textconv", "--no-renames", "--format=%H", spec, "--")
	if err != nil {
		return nil, err
	}
	if log.code != 0 {
		return nil, errCommandFailed
	}
	ids, err := p.git.start(ctx, false, log.out, nil, commandOutputLimit, "patch-id", "--stable")
	if err != nil {
		return nil, err
	}
	if ids.code != 0 {
		return nil, errCommandFailed
	}
	out := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(ids.out)), "\n") {
		if patch, commit, ok := strings.Cut(line, " "); ok {
			out[commit] = patch
		}
	}
	return out, nil
}

// ownDiffs compares, per moved commit, the patch id of the old commit with that of its replay.
func (p *planner) ownDiffs(ctx context.Context, out replayOutcome) (OwnDiffs, error) {
	o := OwnDiffs{Rows: make([]OwnDiffRow, len(p.moved)), AllEqual: len(p.moved) > 0}
	oldIDs, err := p.patchIDs(ctx, *p.res.Input.OldParent.OID+".."+*p.res.Input.Branch.OldHead)
	if err != nil && !errors.Is(err, errCommandFailed) {
		return o, err
	}
	var newIDs map[string]string
	if p.replayedCount > 0 && err == nil {
		if newIDs, err = p.patchIDs(ctx, *p.res.Input.NewParent.OID+".."+out.tip); err != nil && !errors.Is(err, errCommandFailed) {
			return o, err
		}
	}
	for i, c := range p.moved {
		row := OwnDiffRow{Old: c.oid, State: "unknown", OldPatchID: oldIDs[c.oid]}
		if p.replayed[i] != "" && oldIDs != nil && (p.replayedCount == 0 || newIDs != nil) {
			row.PredictedPatchID = newIDs[p.replayed[i]]
			row.State = "differs"
			if row.OldPatchID == row.PredictedPatchID {
				row.State = "equal"
			}
		}
		o.Rows[i] = row
		o.AllEqual = o.AllEqual && row.State == "equal"
	}
	return o, nil
}

// identityCheck compares the author and committer of every moved commit with the identities
// given by --identity, the only source of expected identities.
func (p *planner) identityCheck() IdentityCheck {
	id := IdentityCheck{State: "unknown", Source: "none", Expected: append([]string{}, p.opts.Identities...), Mismatches: []IdentityMismatch{}}
	if len(p.opts.Identities) == 0 {
		id.Reason = "no-expected-identity"
		return id
	}
	id.Source = "flag"
	for _, c := range p.moved {
		for _, who := range []struct{ role, value string }{{"author", c.authorIdent}, {"committer", c.committerIdent}} {
			if !slices.Contains(p.opts.Identities, who.value) {
				id.Mismatches = append(id.Mismatches, IdentityMismatch{Commit: c.oid, Role: who.role, Value: who.value})
			}
		}
	}
	id.State = "pass"
	if len(id.Mismatches) > 0 {
		id.State = "fail"
	}
	return id
}

// overlap lists the paths that the new parent changed relative to the old parent and that the
// moved commits also change: a risk indicator, never evidence of safety.
func (p *planner) overlap(ctx context.Context) (Overlap, error) {
	ov := Overlap{Paths: []string{}, Indicator: "unknown", Note: overlapNote}
	delta, err := p.treeDifferences(ctx)
	if err != nil {
		if errors.Is(err, errOutputBound) || errors.Is(err, errCommandFailed) {
			ov.Reason = "limit"
			return ov, nil
		}
		return ov, err
	}
	for _, path := range p.touched {
		if delta[path] {
			ov.Paths = append(ov.Paths, path)
		}
	}
	ov.Count = len(ov.Paths)
	ov.Indicator = "none"
	if ov.Count > 0 {
		ov.Indicator = "some"
	}
	return ov, nil
}
