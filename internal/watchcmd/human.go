// SPDX-License-Identifier: EUPL-1.2
package watchcmd

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/wstein/sourcesteward/internal/untrusted"
	"github.com/wstein/sourcesteward/internal/watch"
)

// Human renders one event as a single line, `TIME REPO#PR KIND summary` (contracts/watch-events.md):
// no colour, 7-character SHAs, control characters escaped and text bounded. JSON always carries the full values.
func Human(e watch.Event) []byte {
	var b strings.Builder
	b.WriteString(humanTime(e.Time))
	b.WriteByte(' ')
	switch {
	case e.Repo == nil:
		b.WriteByte('-')
	case e.PR != nil:
		b.WriteString(esc(*e.Repo) + "#" + strconv.Itoa(*e.PR))
	default:
		b.WriteString(esc(*e.Repo))
	}
	b.WriteByte(' ')
	b.WriteString(esc(e.Kind))
	if sum := summary(e); sum != "" {
		b.WriteByte(' ')
		b.WriteString(sum)
	}
	b.WriteByte('\n')
	return []byte(b.String())
}

func humanTime(s string) string {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC().Format("15:04:05")
	}
	return esc(s)
}

// esc is the unexported name used throughout this package.
func esc(s string) string { return untrusted.Esc(s) }

func short(sha string) string {
	if len(sha) > 7 {
		sha = sha[:7]
	}
	return esc(sha)
}

func summary(e watch.Event) string {
	switch d := e.Details.(type) {
	case *watch.NewPRDetails:
		out := esc(d.Title) + " by " + esc(d.Author) + " into " + esc(d.Base)
		if d.Draft {
			out += " (draft)"
		}
		return out
	case *watch.HeadChangedDetails:
		return short(d.Old) + " -> " + short(d.New)
	case *watch.ChecksGreenDetails:
		return fmt.Sprintf("%d checks on %s", d.Total, short(d.Head))
	case *watch.ChecksRedDetails:
		names := make([]string, 0, len(d.Failing))
		for _, n := range d.Failing {
			names = append(names, esc(n))
		}
		out := strings.Join(names, ", ")
		if more := d.FailingTotal - len(d.Failing); more > 0 {
			out += fmt.Sprintf(" and %d more", more)
		}
		return out + " (" + esc(d.Shape) + ")"
	case *watch.PendingTimeoutDetails:
		names := make([]string, 0, len(d.Pending))
		for _, n := range d.Pending {
			names = append(names, esc(n))
		}
		return strings.Join(names, ", ") + " pending since " + esc(d.Since)
	case *watch.MergeableDetails:
		m := "unknown"
		if d.Mergeable != nil {
			m = strconv.FormatBool(*d.Mergeable)
		}
		out := "mergeable=" + m + " state=" + esc(d.State)
		switch {
		case d.Conflicting:
			out += " (conflicting)"
		case d.Behind:
			out += " (behind)"
		}
		return out
	case *watch.ReadyDetails:
		return esc(d.From) + " -> " + esc(d.To)
	case *watch.BaseChangedDetails:
		return esc(d.Old) + " -> " + esc(d.New)
	case *watch.MergedDetails:
		commit := "unknown"
		if d.MergeCommit != nil {
			commit = short(*d.MergeCommit)
		}
		return "into " + esc(d.Base) + " as " + commit + " (" + esc(d.MethodHint) + ")"
	case *watch.ClosedDetails:
		return "without merge (base " + esc(d.Base) + ")"
	case *watch.RestackDetails:
		out := "after #" + strconv.Itoa(d.MergedPR) + ": "
		if d.Argv == nil || d.NewParent == nil {
			return out + "no command (" + esc(d.Branch) + ")"
		}
		return out + "git rebase --onto " + short(*d.NewParent) + " " + short(d.OldParent) + " " + esc(d.Branch)
	case *watch.ReposChangedDetails:
		return fmt.Sprintf("+%d -%d skipped %d", len(d.Added), len(d.Removed), len(d.Skipped))
	case *watch.WatchErrorDetails:
		return fmt.Sprintf("%s: %s (failure %d, retry in %ds)", esc(d.Code), esc(d.Message), d.Failures, d.RetryInSeconds)
	case *watch.WatchRecoveredDetails:
		return fmt.Sprintf("after %d failures", d.AfterFailures)
	case *watch.WatchStalledDetails:
		return "no success since " + esc(d.Since)
	}
	return ""
}
