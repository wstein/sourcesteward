// SPDX-License-Identifier: EUPL-1.2

package carryover

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
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

// rowGrammar matches a range-diff header such as "1:  abc1234 = 1:  def5678 subject". Detail lines
// are indented by four spaces and never match.
var rowGrammar = regexp.MustCompile(`^ {0,3}(-|[0-9]+):\s+(?:-+|[0-9a-f]+) ([=!<>]) (-|[0-9]+):\s+(?:-+|[0-9a-f]+)(?: |$)`)

func position(s string) *int {
	if s == "-" {
		return nil
	}
	n, _ := strconv.Atoi(s)
	return &n
}

func checkRange(ctx context.Context, g Git, req Request, res *Result, fail func(string)) {
	oldN, err1 := g.Count(ctx, req.OldBase, req.OldHead)
	newN, err2 := g.Count(ctx, req.NewBase, req.NewHead)
	if err1 != nil || err2 != nil {
		fail(ReasonReadFailed)
		return
	}
	if oldN == 0 || newN == 0 {
		res.RangeDiff.State = StateDiffers
		fail(ReasonRangeEmpty)
		return
	}
	out, err := g.RangeDiff(ctx, req.OldBase, req.OldHead, req.NewBase, req.NewHead)
	if err != nil {
		fail(ReasonReadFailed)
		return
	}
	for _, line := range strings.Split(out, "\n") {
		m := rowGrammar.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		res.RangeDiff.Total++
		o, n := position(m[1]), position(m[3])
		if m[2] == "=" && o != nil && n != nil && *o == *n {
			continue
		}
		res.RangeDiff.NotEqual++
		if len(res.RangeDiff.Deviations) < MaxDeviations {
			res.RangeDiff.Deviations = append(res.RangeDiff.Deviations, Deviation{Old: o, New: n, Mark: m[2]})
		} else {
			res.RangeDiff.Truncated = true
		}
	}
	switch {
	case res.RangeDiff.Total == 0:
		fail(ReasonReadFailed)
	case res.RangeDiff.NotEqual > 0:
		res.RangeDiff.State = StateDiffers
		fail(ReasonRangeDiff)
	case res.RangeDiff.Total != oldN || oldN != newN:
		// Every commit must appear as one "=" row; a different count means the output was not read whole.
		fail(ReasonReadFailed)
	default:
		res.RangeDiff.State = StateEqual
	}
}
