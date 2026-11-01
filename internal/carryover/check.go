// SPDX-License-Identifier: EUPL-1.2

package carryover

import (
	"context"
	"fmt"
	"strings"
)

// Check runs the three conditions and the optional head comparison. Read failures never turn into
// a yes: a check that could not run is "unknown" and the carry-over is refused.
func Check(ctx context.Context, g Git, req Request) Result {
	res := Result{
		SchemaVersion: SchemaVersion, Command: CommandName, PR: req.PR,
		From:         Side{Base: req.OldBase, Head: req.OldHead},
		To:           Side{Base: req.NewBase, Head: req.NewHead},
		PatchID:      PatchIDCheck{State: StateUnknown},
		MergeCommits: MergeCheck{State: StateUnknown, Commits: []string{}},
		RangeDiff:    RangeCheck{State: StateUnknown, Deviations: []Deviation{}},
		PRHead:       HeadCheck{State: StateNotChecked},
		Reasons:      []string{},
	}
	fail := func(code string) {
		for _, r := range res.Reasons {
			if r == code {
				return
			}
		}
		res.Reasons = append(res.Reasons, code)
	}

	if req.Remote != "" {
		tip, err := g.FetchPull(ctx, req.Remote, req.PR)
		switch {
		case err != nil:
			res.PRHead.State = StateUnknown
			fail(ReasonReadFailed)
		case tip == req.NewHead:
			res.PRHead.State = StateMatch
		default:
			res.PRHead.State = StateMismatch
			fail(ReasonHeadMismatch)
		}
	}

	for _, sha := range []string{req.OldBase, req.OldHead, req.NewBase, req.NewHead} {
		ok, err := g.HasCommit(ctx, sha)
		if err != nil {
			fail(ReasonReadFailed)
			return finish(res)
		}
		if !ok {
			fail(ReasonObjectMissing)
			return finish(res)
		}
	}

	checkPatchID(ctx, g, req, &res, fail)
	checkMerges(ctx, g, req, &res, fail)
	checkRange(ctx, g, req, &res, fail)
	return finish(res)
}

// Line formats the Carried-over line. It is the only place that knows the format; switch to
// internal/reviewstate once it is on main (PR #349), which owns the one parser of these lines.
func Line(from, to, patchID string, pr int) string {
	return fmt.Sprintf("Carried-over: from=%s to=%s patch-id=%s pr=%d", from, to, patchID, pr)
}

func finish(res Result) Result {
	res.CarryOver = len(res.Reasons) == 0 && res.PatchID.State == StateEqual && res.MergeCommits.State == StateNone &&
		res.RangeDiff.State == StateEqual && res.PRHead.State != StateMismatch && res.PRHead.State != StateUnknown
	if res.CarryOver {
		line := Line(res.From.Head, res.To.Head, res.PatchID.New, res.PR)
		res.Line = &line
	}
	return res
}

func checkPatchID(ctx context.Context, g Git, req Request, res *Result, fail func(string)) {
	oldID, err1 := g.PatchID(ctx, req.OldBase, req.OldHead)
	newID, err2 := g.PatchID(ctx, req.NewBase, req.NewHead)
	if err1 != nil || err2 != nil {
		fail(ReasonReadFailed)
		return
	}
	res.PatchID.Old, res.PatchID.New = oldID, newID
	switch {
	case oldID == "" || newID == "":
		fail(ReasonPatchIDEmpty)
	case oldID != newID:
		res.PatchID.State = StateDiffers
		fail(ReasonPatchIDDiffers)
	default:
		res.PatchID.State = StateEqual
	}
}

func checkMerges(ctx context.Context, g Git, req Request, res *Result, fail func(string)) {
	merges, err := g.Merges(ctx, req.NewBase, req.NewHead)
	if err != nil {
		fail(ReasonReadFailed)
		return
	}
	res.MergeCommits.Count = len(merges)
	if len(merges) == 0 {
		res.MergeCommits.State = StateNone
		return
	}
	res.MergeCommits.State = StateFound
	res.MergeCommits.Commits = merges
	fail(ReasonMergeCommits)
}

// normalize drops what a rebase may change without changing the commit: the blob ids of the index
// lines and the line numbers of the hunk headers. Everything else, whitespace included, is compared
// byte for byte, which is what the patch-id cannot do.
func normalize(cp CommitPatch) string {
	var b strings.Builder
	b.WriteString(cp.Message)
	b.WriteByte(0)
	for _, line := range strings.Split(cp.Patch, "\n") {
		switch {
		case strings.HasPrefix(line, "index "):
			continue
		case strings.HasPrefix(line, "@@ "):
			line = "@@"
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func position(i int) *int { n := i + 1; return &n }

// checkRange is condition 3: the commits of the old and the new range are equal pairwise, in the
// same order, in message and patch. It does not run git range-diff: range-diff starts the textconv
// drivers of the repository. A rebase that only shifts line numbers or blob ids stays equal; changed
// context, a reordering, an added, removed or edited commit do not (the rule then asks for a delta check).
func checkRange(ctx context.Context, g Git, req Request, res *Result, fail func(string)) {
	oldC, err1 := g.Commits(ctx, req.OldBase, req.OldHead)
	newC, err2 := g.Commits(ctx, req.NewBase, req.NewHead)
	if err1 != nil || err2 != nil {
		fail(ReasonReadFailed)
		return
	}
	if len(oldC) == 0 || len(newC) == 0 {
		res.RangeDiff.State = StateDiffers
		fail(ReasonRangeEmpty)
		return
	}
	total := max(len(oldC), len(newC))
	res.RangeDiff.Total = total
	for i := 0; i < total; i++ {
		var dev Deviation
		switch {
		case i >= len(oldC):
			dev = Deviation{New: position(i), Mark: ">"}
		case i >= len(newC):
			dev = Deviation{Old: position(i), Mark: "<"}
		case normalize(oldC[i]) != normalize(newC[i]):
			dev = Deviation{Old: position(i), New: position(i), Mark: "!"}
		default:
			continue
		}
		res.RangeDiff.NotEqual++
		if len(res.RangeDiff.Deviations) < MaxDeviations {
			res.RangeDiff.Deviations = append(res.RangeDiff.Deviations, dev)
		} else {
			res.RangeDiff.Truncated = true
		}
	}
	if res.RangeDiff.NotEqual > 0 {
		res.RangeDiff.State = StateDiffers
		fail(ReasonRangeDiff)
		return
	}
	res.RangeDiff.State = StateEqual
}
