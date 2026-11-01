// SPDX-License-Identifier: EUPL-1.2

package conflicts

import (
	"context"
	"sort"
	"strconv"
)

// Detect probes every pull request against the current tip of its base. At most MaxProbed pull
// requests (in the given order) are probed; the rest is unknown. The result has one Finding per
// pull request, in the same order. Any failure leaves the pull request unknown with a reason: the
// detection never reports a conflict or a clean merge it did not see.
func Detect(ctx context.Context, g Git, remote string, prs []PR) []Finding {
	out := make([]Finding, len(prs))
	var eligible []int
	var pulls []int
	var bases []string
	seen := map[string]bool{}
	for i, p := range prs {
		out[i] = Finding{Number: p.Number, State: StateUnknown, Base: clip(p.Base)}
		switch {
		case i >= MaxProbed:
			out[i].Reason = ReasonPastLimit
		case p.Number < 1 || !ValidBranch(p.Base) || !ValidSHA(p.Head):
			out[i].Reason = ReasonRefInvalid
		default:
			eligible = append(eligible, i)
			pulls = append(pulls, p.Number)
			if !seen[p.Base] {
				seen[p.Base] = true
				bases = append(bases, p.Base)
			}
		}
	}
	if len(eligible) == 0 {
		return out
	}
	if err := g.Fetch(ctx, remote, pulls, bases); err != nil {
		for _, i := range eligible {
			out[i].Reason = ReasonFetchFailed
		}
		return out
	}
	for _, i := range eligible {
		out[i] = probe(ctx, g, prs[i])
	}
	return out
}

func unknown(p PR, reason string) Finding {
	return Finding{Number: p.Number, State: StateUnknown, Reason: reason, Base: clip(p.Base)}
}

func probe(ctx context.Context, g Git, p PR) Finding {
	head, err := g.Resolve(ctx, NamespaceRoot+"pull/"+strconv.Itoa(p.Number))
	if err != nil || head == "" {
		return unknown(p, ReasonFetchFailed)
	}
	if head != p.Head {
		return unknown(p, ReasonHeadMoved)
	}
	tip, err := g.Resolve(ctx, NamespaceRoot+"base/"+p.Base)
	if err != nil || tip == "" {
		return unknown(p, ReasonFetchFailed)
	}
	mb, err := g.MergeBase(ctx, tip, head)
	if err != nil {
		return unknown(p, ReasonProbeFailed)
	}
	if mb == "" {
		return unknown(p, ReasonNoBase)
	}
	f := Finding{Number: p.Number, Base: clip(p.Base), BaseSHA: tip, Head: head, MergeBase: mb}
	conflict, files, err := g.MergeTree(ctx, tip, head)
	if err != nil {
		return unknown(p, ReasonProbeFailed)
	}
	if conflict {
		f.State = StateConflicts
		if len(files) > MaxFiles {
			files, f.Truncated = files[:MaxFiles], true
		}
		for _, path := range files {
			last, _ := g.LastTouch(ctx, mb, tip, path)
			f.Files = append(f.Files, FileConflict{Path: clip(path), BaseCommit: last})
		}
		return f
	}
	f.State = StateClean
	f.Overlap, f.Truncated = overlap(ctx, g, mb, tip, head)
	return f
}

// overlap lists the files that the base changed since the merge base and the pull request changed too.
func overlap(ctx context.Context, g Git, mb, tip, head string) ([]string, bool) {
	theirs, err1 := g.Changed(ctx, mb, tip)
	ours, err2 := g.Changed(ctx, mb, head)
	if err1 != nil || err2 != nil {
		return nil, false
	}
	in := map[string]bool{}
	for _, f := range theirs {
		in[f] = true
	}
	var both []string
	for _, f := range ours {
		if in[f] {
			both = append(both, clip(f))
		}
	}
	sort.Strings(both)
	if len(both) > MaxFiles {
		return both[:MaxFiles], true
	}
	return both, false
}

// clip cuts s to MaxString bytes on a UTF-8 boundary.
func clip(s string) string {
	if len(s) <= MaxString {
		return s
	}
	n := MaxString
	for n > 0 && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n]
}
