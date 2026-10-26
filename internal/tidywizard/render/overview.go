// SPDX-License-Identifier: EUPL-1.2

package render

import (
	"fmt"
	"sort"
	"strings"

	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/tidywizard/risk"
	"github.com/wstein/sourcesteward/internal/untrusted"
)

// OverviewOptions tunes the grouped text. The zero value is plain text.
type OverviewOptions struct {
	// Color wraps already escaped text in SGR sequences; it is decoration only, the level word is
	// always printed. The caller sets it only for a terminal without NO_COLOR.
	Color bool
}

// OverviewGroup is one group of the grouped overview (Spec 044 "Overview first"): the rows of one
// effect class and place. Its key is the one of the JSON (`group`); a branch group is one repository.
type OverviewGroup struct {
	Key                   string
	Kind                  tidywizard.Kind
	Place, Family, Parent string
	Repo                  string
	// WithBranch marks a directory group whose rows also delete their branch.
	WithBranch bool
	Rows       []risk.Row
	Level      risk.Level
	Counts     risk.Counts
	owners     []ownerCount
}

type ownerCount struct {
	label string
	n     int
}

// overviewGroups are the groups of the document in the order of the first screen: worst level
// first (green before yellow before orange), then larger before smaller (the order of risk.GroupRows).
func overviewGroups(d Document) []OverviewGroup {
	byID := make(map[string]tidywizard.Candidate, len(d.all))
	for _, c := range d.all {
		byID[c.ID] = c
	}
	out := make([]OverviewGroup, 0, len(d.groups))
	for _, g := range d.groups {
		og := OverviewGroup{Key: g.Key, Kind: tidywizard.Kind(g.Kind), Place: g.Place, Family: g.Family, Parent: g.Parent, Repo: g.Repo, Rows: g.Rows, Level: g.Level, Counts: g.Counts}
		for _, e := range g.EffectClass {
			if e == "delete-branch" && og.Kind == tidywizard.KindWorktree {
				og.WithBranch = true
			}
		}
		owners := map[string]int{}
		for _, r := range g.Rows {
			owners[ownerLabel(byID[r.ID])]++
		}
		for l, n := range owners {
			og.owners = append(og.owners, ownerCount{l, n})
		}
		sort.Slice(og.owners, func(a, b int) bool {
			if og.owners[a].n != og.owners[b].n {
				return og.owners[a].n > og.owners[b].n
			}
			return og.owners[a].label < og.owners[b].label
		})
		out = append(out, og)
	}
	return out
}

// ownerLabel is the owner as the overview shows it: the derived label, a commit-author hint as
// "hint NAME", and "unknown" for none. It is information, never a selector (Spec 044 D7).
func ownerLabel(c tidywizard.Candidate) string {
	switch {
	case c.Owner == "":
		return "unknown"
	case c.OwnerSource == owner.SourceCommitAuthor && strings.HasPrefix(c.Owner, "author:"):
		return "hint " + strings.TrimPrefix(c.Owner, "author:")
	}
	return c.Owner
}

// Label is the escaped name of the group, as the first screen shows it (count and place).
func (g OverviewGroup) Label() string { return g.noun() + " " + g.place() }

func (g OverviewGroup) noun() string {
	n := len(g.Rows)
	one := n == 1
	switch g.Kind {
	case tidywizard.KindBranch:
		return fmt.Sprintf("%d %s", n, plural(one, "branch", "branches"))
	case tidywizard.KindTrackingRef:
		return fmt.Sprintf("%d %s", n, plural(one, "tracking reference", "tracking references"))
	}
	return fmt.Sprintf("%d %s", n, plural(one, "directory", "directories"))
}

func plural(one bool, s, p string) string {
	if one {
		return s
	}
	return p
}

func (g OverviewGroup) place() string {
	e := untrusted.Esc
	switch g.Kind {
	case tidywizard.KindBranch, tidywizard.KindTrackingRef:
		return "in " + e(g.Repo)
	}
	switch g.Place {
	case "scratchpads":
		return "agent scratchpads"
	case "tmp-family":
		return "/tmp/" + g.Family + "*"
	case "tmp-other":
		return "other in /tmp"
	}
	return "in " + e(g.Parent)
}

func (g OverviewGroup) consequence() string {
	switch g.Kind {
	case tidywizard.KindBranch:
		return "reversible"
	case tidywizard.KindTrackingRef:
		return "reference deleted"
	}
	if g.WithBranch {
		return "files deleted, branch too"
	}
	return "files deleted"
}

// ownerText names the owner of a group, or both of two, or counts three or more; the group step
// shows the full count line. The owner is information, never a selector.
func (g OverviewGroup) ownerText() string {
	e := untrusted.Esc
	switch len(g.owners) {
	case 1:
		return e(g.owners[0].label)
	case 2:
		return e(g.owners[0].label) + " and " + e(g.owners[1].label)
	}
	return fmt.Sprintf("%d owners", len(g.owners))
}

// riskText is the level word and, for a mixed group, the counts per level.
func (g OverviewGroup) levels() (word, detail string) {
	var parts []string
	for _, p := range []struct {
		n    int
		name string
	}{{g.Counts.Green, "green"}, {g.Counts.Yellow, "yellow"}, {g.Counts.Orange, "orange"}} {
		if p.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", p.n, p.name))
		}
	}
	if len(parts) > 1 {
		detail = "(" + strings.Join(parts, ", ") + ")"
	}
	return string(g.Level), detail
}

func levelStyle(l risk.Level) string {
	switch l {
	case risk.Green:
		return styleGreen
	case risk.Yellow:
		return styleYellow
	case risk.Orange:
		return styleOrange
	case risk.Red:
		return styleRed
	}
	return styleDim
}

const styleOrange = "38;5;208"

// thousands writes 1129 as 1,129.
func thousands(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// Overview renders the grouped first screen of the janitor wizard (Spec 044): what can be cleaned,
// by kind and place with the level of the worst row, what each group does, and how many rows are
// kept. It shows no row and no internal code; --rows and --json keep both. Every string from
// outside passes esc and its bound.
func Overview(d Document, opts OverviewOptions) string {
	e := untrusted.Esc
	paint := painter(opts.Color)
	var b strings.Builder
	skipped, failed := 0, 0
	for _, r := range d.Repositories {
		switch r.State {
		case "skipped":
			skipped++
		case "error":
			failed++
		}
	}
	fmt.Fprintf(&b, "Workspace %s   %d %s analyzed", e(d.Workspace), len(d.Repositories)-skipped-failed, plural(len(d.Repositories)-skipped-failed == 1, "repository", "repositories"))
	if skipped+failed > 0 {
		fmt.Fprintf(&b, " (%d not analyzed)", skipped+failed)
	}
	b.WriteString("\n")
	// Every derived value and the mode stay on the first screen (Spec 041 FR-010): a refused state
	// directory and an agent marker explain why a run exits 1 or asks nothing.
	if line := derivedLine(d); line != "" {
		fmt.Fprintf(&b, "Derived:   %s\n", line)
	}
	fmt.Fprintf(&b, "Mode:      %s\n", modeLine(d.Mode))
	shown := 0
	for _, r := range d.Repositories {
		if r.State != "skipped" && r.State != "error" {
			continue
		}
		if shown == 3 {
			fmt.Fprintf(&b, "  and %d more not analyzed\n", skipped+failed-shown)
			break
		}
		shown++
		if r.State == "error" {
			fmt.Fprintf(&b, "  %s: %s\n", paint(styleBold, e(r.Name)), paint(styleRed, "could not be analyzed: "+e(r.Reason)))
		} else {
			fmt.Fprintf(&b, "  %s: %s\n", paint(styleBold, e(r.Name)), paint(styleYellow, "skipped, "+skipSentence(r.Reason)))
		}
	}

	groups := overviewGroups(d)
	choosable := 0
	for _, g := range groups {
		choosable += len(g.Rows)
	}
	total := d.Summary.Safe + d.Summary.Retain + d.Summary.Decision
	b.WriteString("\n")
	switch {
	case total == 0:
		b.WriteString("Nothing to clean.\n")
	case len(groups) == 0:
		b.WriteString("Nothing can be cleaned: no candidate is offered for deletion.\n")
	default:
		writeGroups(&b, groups, choosable, total, paint)
	}

	red, kept := 0, 0
	for _, r := range retainRows(d) {
		if r.Level == risk.Red {
			red += r.Count
		} else {
			kept += r.Count
		}
	}
	if red+kept > 0 {
		fmt.Fprintf(&b, "\nKept (%s): nothing here is offered for deletion\n", thousands(red+kept))
		if red > 0 {
			fmt.Fprintf(&b, "  %s %s: not merged, uncommitted or untracked files, not judged\n", fmt.Sprintf("%3s", thousands(red)), paint(styleRed, "may hold unique work (red)"))
		}
		if kept > 0 {
			fmt.Fprintf(&b, "  %s kept for another reason (record missing, branch in use, recently active, ...)\n", fmt.Sprintf("%3s", thousands(kept)))
		}
	}
	for _, n := range d.overviewNotices() {
		fmt.Fprintf(&b, "notice: %s\n", n)
	}
	if d.Truncated {
		b.WriteString("truncated: the overview is cut (use a narrower selection)\n")
	}
	b.WriteString("The rows with their codes: stw tidy --rows (10 per repository and owner)   Every row: stw tidy --json\n")
	return b.String()
}

// maxGroupLines is how many group lines the first screen shows; the smallest groups beyond that
// are folded into one last line so the screen stays short (Spec 044 SC-012). The JSON keeps every
// group key.
const maxGroupLines = 10

func writeGroups(b *strings.Builder, groups []OverviewGroup, choosable, total int, paint func(style, s string) string) {
	type line struct {
		num, what, place, effect, word, detail string
		level                                  risk.Level
	}
	rows := make([]line, 0, maxGroupLines)
	w := line{}
	longest := func(cur *string, s string) {
		if n := len([]rune(s)); n > len([]rune(*cur)) {
			*cur = s
		}
	}
	undo := false
	for _, g := range groups {
		if g.Kind == tidywizard.KindBranch || g.WithBranch {
			undo = true
		}
	}
	// The smallest groups (the later one of equal size first) are folded; the others keep their order.
	shown, folded := groups, []OverviewGroup(nil)
	if len(groups) > maxGroupLines {
		order := make([]int, len(groups))
		for i := range order {
			order[i] = i
		}
		sort.SliceStable(order, func(a, b int) bool {
			if len(groups[order[a]].Rows) != len(groups[order[b]].Rows) {
				return len(groups[order[a]].Rows) < len(groups[order[b]].Rows)
			}
			return order[a] > order[b]
		})
		fold := map[int]bool{}
		for _, i := range order[:len(groups)-(maxGroupLines-1)] {
			fold[i] = true
		}
		shown = nil
		for i, g := range groups {
			if fold[i] {
				folded = append(folded, g)
			} else {
				shown = append(shown, g)
			}
		}
	}
	for i, g := range shown {
		word, detail := g.levels()
		rows = append(rows, line{num: fmt.Sprint(i + 1), what: g.noun(), place: g.place(), effect: g.consequence() + ", " + g.ownerText(), word: word, detail: detail, level: g.Level})
	}
	if len(folded) > 0 {
		f := OverviewGroup{Level: risk.Green}
		n := 0
		for _, g := range folded {
			n += len(g.Rows)
			f.Counts.Green += g.Counts.Green
			f.Counts.Yellow += g.Counts.Yellow
			f.Counts.Orange += g.Counts.Orange
		}
		switch {
		case f.Counts.Orange > 0:
			f.Level = risk.Orange
		case f.Counts.Yellow > 0:
			f.Level = risk.Yellow
		}
		word, detail := f.levels()
		rows = append(rows, line{num: fmt.Sprint(len(shown) + 1), what: fmt.Sprintf("%d more groups", len(folded)), place: fmt.Sprintf("%d %s in all", n, plural(n == 1, "row", "rows")), effect: "the smallest groups; their keys are in --json", word: word, detail: detail, level: f.Level})
	}
	for _, l := range rows {
		longest(&w.num, l.num)
		longest(&w.what, l.what)
		longest(&w.place, l.place)
		longest(&w.effect, l.effect)
	}
	pad := func(s string, like string, right bool) string {
		gap := strings.Repeat(" ", len([]rune(like))-len([]rune(s)))
		if right {
			return gap + s
		}
		return s + gap
	}
	title := fmt.Sprintf("Can be cleaned (%s of %s candidates in %d groups)", thousands(choosable), thousands(total), len(groups))
	width := 2 + len(w.num) + 2 + len([]rune(w.what)) + 2 + len([]rune(w.place)) + 2 + len([]rune(w.effect)) + 2
	fmt.Fprintf(b, "%s%s%s\n", title, strings.Repeat(" ", max(2, width-len([]rune(title)))), "risk")
	for _, l := range rows {
		fmt.Fprintf(b, "  %s  %s  %s  %s  %s", pad(l.num, w.num, true), pad(l.what, w.what, false), pad(l.place, w.place, false), pad(l.effect, w.effect, false), paint(levelStyle(l.level), l.word))
		if l.detail != "" {
			b.WriteString("  " + l.detail)
		}
		b.WriteString("\n")
	}
	if undo {
		b.WriteString("Undo (branches only): stw tidy restore PATH --contract 3 --plan PLAN_ID --state-dir DIR --apply --authorize\n")
		b.WriteString("      PLAN_ID: see stw tidy history --contract 3 --state-dir DIR; deleted directories are not restored\n")
	}
}

// skipSentence is the wording of the reason a repository was not analyzed.
func skipSentence(reason string) string {
	switch reason {
	case "base-ambiguous":
		return "its base branch is ambiguous"
	case "base-missing":
		return "its base branch is missing"
	case "remote-ambiguous":
		return "its remote is ambiguous"
	}
	return untrusted.Esc(reason)
}

// overviewNotices are the repository notices, deduplicated and counted, then the run notices; all
// escaped. A notice that is a fixed code gets its sentence.
func (d Document) overviewNotices() []string {
	counts := map[string]int{}
	var order []string
	for _, r := range d.Repositories {
		for _, n := range d.repoNotices[r.Name] {
			if counts[n] == 0 {
				order = append(order, n)
			}
			counts[n]++
		}
	}
	var out []string
	for _, n := range order {
		text := n
		if n == "tracking-requires-online" {
			text = "remote-tracking references are not read without --online"
		}
		if counts[n] > 1 {
			text += fmt.Sprintf(" (%d repositories)", counts[n])
		}
		out = append(out, text)
	}
	for _, n := range d.runNotices {
		out = append(out, untrusted.Esc(n))
	}
	return out
}

// retainRow is one line of the retain table: the kept rows of one reason and level.
type retainRow struct {
	Reason      string
	Level       risk.Level
	Count       int
	Why, Action string
}

// selfRed lists the reasons that are red by themselves; any other red row is red because a content
// fact (not integrated) overrides the structural reason the classifier found first.
var selfRed = map[string]bool{"unmerged": true, "dirty": true, "ignored-extras": true, "evidence-incomplete": true}

// notChecked lists the reasons of kept worktree rows whose uncommitted files the view does not claim to know.
var notChecked = map[string]bool{"registration-record-missing": true, "locked-foreign": true, "recently-active": true}

// redActions are the actions of a red row whose classifier reason is structural.
var redActions = map[string]string{
	"registration-record-missing":   "do not delete; find the right --state-dir and judge it",
	"registration-record-ambiguous": "do not delete; find the right --state-dir and judge it",
}

// retainRows groups the retained candidates (red and kept) by reason and level, largest first.
func retainRows(d Document) []retainRow {
	type key struct {
		reason string
		level  risk.Level
		stale  bool // only for `unregistered`, which means two things: a safe row, or a gone directory
	}
	counts := map[key]int{}
	first := map[key]risk.Assessment{}
	for _, c := range d.all {
		a := d.assessed[c.ID]
		if a.Level != risk.Red && a.Level != risk.Kept {
			continue
		}
		k := key{reason: c.Reason, level: a.Level, stale: c.Reason == "unregistered" && c.Kind == tidywizard.KindStaleWorktree}
		counts[k]++
		if _, ok := first[k]; !ok {
			first[k] = a
		}
	}
	out := make([]retainRow, 0, len(counts))
	for k, n := range counts {
		s := Sentence{Why: "kept without a stated reason", Action: "see stw tidy --rows"}
		if k.reason != "" {
			s = reasonSentenceOrFallback(k.reason)
		}
		if k.level == risk.Kept && notChecked[k.reason] {
			// The dirt of an integrated row kept for a structural reason is not necessarily known here.
			s.Why += "; integrated, not checked for uncommitted files"
		}
		if k.stale {
			s = Sentence{Why: "worktree entry whose directory is gone", Action: "no stw action yet"}
		}
		if k.level == risk.Red && !selfRed[k.reason] && len(first[k].Reasons) > 0 {
			rs, _ := RiskReasonSentence(first[k].Reasons[0])
			s.Why += "; " + rs.Why
			s.Action = rs.Action
			if a, ok := redActions[k.reason]; ok {
				s.Action = a
			}
		}
		out = append(out, retainRow{Reason: k.reason, Level: k.level, Count: n, Why: s.Why, Action: s.Action})
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Count != out[b].Count {
			return out[a].Count > out[b].Count
		}
		if out[a].Reason != out[b].Reason {
			return out[a].Reason < out[b].Reason
		}
		return out[a].Level < out[b].Level
	})
	return out
}

// RetainTable renders one line per reason of the kept rows with its count, level, a sentence and
// what to do (Spec 044 "Retain explained"); rows only on request (`--rows`). Red rows may hold
// unique work and are told apart from the kept ones. An empty table is the empty string.
func RetainTable(d Document, opts OverviewOptions) string {
	rows := retainRows(d)
	if len(rows) == 0 {
		return ""
	}
	paint := painter(opts.Color)
	total := 0
	whyW := len("why")
	for _, r := range rows {
		total += r.Count
		if n := len([]rune(r.Why)); n > whyW {
			whyW = n
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Kept: %s candidates. Nothing here is offered for deletion.\n", thousands(total))
	fmt.Fprintf(&b, "  %5s  %-5s  %-*s  %s\n", "count", "level", whyW, "why", "what to do")
	for _, r := range rows {
		word := "kept"
		if r.Level == risk.Red {
			word = "RED"
		}
		fmt.Fprintf(&b, "  %5s  %s  %-*s  %s\n", thousands(r.Count), paint(levelStyle(r.Level), fmt.Sprintf("%-5s", word)), whyW, r.Why, r.Action)
	}
	b.WriteString("Rows with their codes: stw tidy --rows (10 per repository and owner); every row: stw tidy --json\n")
	return b.String()
}
