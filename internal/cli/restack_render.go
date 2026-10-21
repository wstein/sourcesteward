// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/wstein/sourcesteward/internal/restack"
)

func restackAbbrev(oid *string) string {
	if oid == nil {
		return "none"
	}
	if len(*oid) > 12 {
		return escapePinsText((*oid)[:12])
	}
	return escapePinsText(*oid)
}

// renderRestackText renders the human output from the same result as --json. Every value that
// comes from the repository is escaped, so a ref or subject cannot forge lines or terminal effects.
func renderRestackText(w io.Writer, r restack.Result) error {
	var b strings.Builder
	e := escapePinsText
	in := r.Input
	fmt.Fprintf(&b, "Restack plan: %s onto %s (old parent %s), %d commits\n", e(in.Branch.Ref), restackAbbrev(in.NewParent.OID), restackAbbrev(in.OldParent.OID), in.Moved.Count)
	b.WriteString("effects: read-only, offline; objects only in a private quarantine that is removed\n")
	for _, d := range r.Diagnostics {
		fmt.Fprintf(&b, "%s: %s: %s\n", e(r.Status), e(d.Code), e(d.Message))
	}
	if r.Status != restack.StatusOK || r.Evidence == nil {
		if r.Status == restack.StatusIncomplete {
			b.WriteString("incomplete: the evidence does not cover the whole plan and authorizes nothing\n")
		}
		return writeAll(w, b.String())
	}
	fmt.Fprintf(&b, "Outcome: %s (names what the evidence shows; it authorizes nothing)\n", e(r.Outcome))
	ev := r.Evidence
	fmt.Fprintf(&b, "Replay (plumbing, nothing written): %s for %d of %d commits\n", e(ev.Replay.State), ev.Replay.CommitsReplayed, in.Moved.Count)
	marks := make([]string, 0, len(ev.RangePairing.Rows))
	for _, row := range ev.RangePairing.Rows {
		marks = append(marks, e(row.Mark))
	}
	dropped, ownEqual := 0, 0
	for _, row := range ev.ParentDropped.Rows {
		if row.State == "dropped" || row.State == "patch-equivalent-tree" {
			dropped++
		}
	}
	for _, row := range ev.OwnDiffEquality.Rows {
		if row.State == "equal" {
			ownEqual++
		}
	}
	fmt.Fprintf(&b, "Range pairing: %s    Parent commits dropped: %d of %d (cherry -)   Own diffs equal: %d of %d\n", strings.Join(marks, " "), dropped, len(ev.ParentDropped.Rows), ownEqual, len(ev.OwnDiffEquality.Rows))
	if ev.ParentDropped.Reason != "" {
		fmt.Fprintf(&b, "Parent commits: %s (%s)\n", e(ev.ParentDropped.State), e(ev.ParentDropped.Reason))
	}
	id := ev.Identity
	fmt.Fprintf(&b, "Identity: %s (source: %s", e(id.State), e(id.Source))
	if id.Reason != "" {
		fmt.Fprintf(&b, ", %s", e(id.Reason))
	}
	fmt.Fprintf(&b, "; %d expected, %d mismatches)\n", len(id.Expected), len(id.Mismatches))
	if ev.Conflicts.Predicted {
		fmt.Fprintf(&b, "Predicted conflicts at %s:\n", restackAbbrev(ev.Conflicts.Commit))
		for _, item := range ev.Conflicts.Items {
			fmt.Fprintf(&b, "  %s (%s)\n", e(item.Path), e(item.Kind))
		}
	}
	if len(ev.EmptyAfterReplay) > 0 {
		fmt.Fprintf(&b, "Already contained in the new parent (empty after replay): %d commits\n", len(ev.EmptyAfterReplay))
	}
	fmt.Fprintf(&b, "Overlap risk: %d paths changed by the new parent and by the branch (indicator: %s)", ev.Overlap.Count, e(ev.Overlap.Indicator))
	for i, path := range ev.Overlap.Paths {
		if i == 5 {
			b.WriteString(", ...")
			break
		}
		sep := ", "
		if i == 0 {
			sep = ": "
		}
		b.WriteString(sep + e(path))
	}
	b.WriteString("\n")
	if n := len(r.Effects.AffectedRefs); n > 0 {
		fmt.Fprintf(&b, "Affected local branches that a later apply would not move: %d\n", n)
		for _, ref := range r.Effects.AffectedRefs {
			fmt.Fprintf(&b, "  %s\n", e(ref.Ref))
		}
	}
	var hooks []string
	for _, h := range r.Effects.Hooks {
		if h.Present {
			hooks = append(hooks, e(h.Name))
		}
	}
	if len(hooks) > 0 {
		fmt.Fprintf(&b, "Hooks present (a porcelain rebase would run them; stw does not): %s\n", strings.Join(hooks, ", "))
	}
	b.WriteString("Limitation: semantic breaks are not detected; build and test the result.\n")
	if t := r.Texts.LeasePush; t != nil {
		fmt.Fprintf(&b, "Lease push (not run):\n  %s\n  %s\n", e(t.Assignment), e(t.Text))
	}
	if t := r.Texts.Resync; t != nil {
		fmt.Fprintf(&b, "Resync (owner, after the push):\n  %s\n  %s   # the owner has local commits on the branch\n  %s   # %s\n", e(t.Fetch), e(t.RebaseOnto), e(t.ResetHard), e(t.DestructiveNote))
	}
	return writeAll(w, b.String())
}

func writeAll(w io.Writer, s string) error {
	_, err := io.WriteString(w, s)
	return err
}
