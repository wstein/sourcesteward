// SPDX-License-Identifier: EUPL-1.2

package carryover

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// WriteJSON prints the one JSON document.
func WriteJSON(w io.Writer, res Result) error { return json.NewEncoder(w).Encode(res) }

func mark(n *int) string {
	if n == nil {
		return "-"
	}
	return fmt.Sprint(*n)
}

// WriteHuman prints the result per condition, the verdict and, only for a yes, the line to post as
// the last line of the output. The command posts nothing itself.
func WriteHuman(w io.Writer, res Result) error {
	var b strings.Builder
	fmt.Fprintf(&b, "stw review carry-over, pull request #%d\n", res.PR)
	fmt.Fprintf(&b, "from  base %s  head %s\n", res.From.Base, res.From.Head)
	fmt.Fprintf(&b, "to    base %s  head %s\n", res.To.Base, res.To.Head)
	fmt.Fprintf(&b, "1 patch-id      %-8s old %s new %s\n", res.PatchID.State, orNone(res.PatchID.Old), orNone(res.PatchID.New))
	fmt.Fprintf(&b, "2 merge commits %-8s %d\n", res.MergeCommits.State, res.MergeCommits.Count)
	fmt.Fprintf(&b, "3 range-diff    %-8s %d commits, %d not \"=\" or at another position\n", res.RangeDiff.State, res.RangeDiff.Total, res.RangeDiff.NotEqual)
	for _, d := range res.RangeDiff.Deviations {
		fmt.Fprintf(&b, "    %s %s %s\n", mark(d.Old), d.Mark, mark(d.New))
	}
	if res.RangeDiff.Truncated {
		fmt.Fprintf(&b, "    (more rows not listed)\n")
	}
	fmt.Fprintf(&b, "  pr head       %s\n", res.PRHead.State)
	if res.CarryOver {
		fmt.Fprintf(&b, "carry-over: yes\n%s\n", *res.Line)
	} else {
		fmt.Fprintf(&b, "carry-over: no (%s)\nDelta check instead of a carry-over.\n", strings.Join(res.Reasons, ", "))
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}
