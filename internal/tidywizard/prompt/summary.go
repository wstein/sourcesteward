// SPDX-License-Identifier: EUPL-1.2

package prompt

import (
	"fmt"
	"sort"
	"strings"

	pluralize "github.com/wstein/sourcesteward/internal/tidywizard/plural"
	"github.com/wstein/sourcesteward/internal/tidywizard/risk"
)

// summary shows the selection and asks whether to go on, edit or add the green rows; its last line
// states what is deleted for good, directly before the first typed prompt (Spec 044 D2, FR-011).
func (s *session) summary() (FlowResult, error) {
	for {
		if len(s.selected) == 0 {
			fmt.Fprintln(s.out, "\nNothing was chosen; nothing will be stored.")
			return FlowResult{Outcome: OutcomeNone, Rows: []Row{}, Confirmed: map[string]bool{}}, nil
		}
		s.showSummary()
		again, err := s.summaryKeys()
		if err != nil {
			return FlowResult{}, err
		}
		if !again {
			break
		}
	}
	rows := s.chosen()
	dirs, branches, tracking := tally(rows)
	if tracking > 0 {
		fmt.Fprintf(s.out, "It also deletes %d local tracking %s.\n", tracking, plural(tracking, "reference", "references"))
	}
	fmt.Fprintf(s.out, "This deletes %s for good and %s (restorable).\n", count(dirs, "directory", "directories"), count(branches, "branch", "branches"))
	confirmed := map[string]bool{}
	for _, r := range rows {
		if s.confirmed[r.ID] {
			confirmed[r.ID] = true
		}
	}
	return FlowResult{Outcome: OutcomeChosen, Rows: rows, Confirmed: confirmed}, nil
}

func count(n int, one, many string) string { return pluralize.Count(n, one, many) }

func tally(rows []Row) (dirs, branches, tracking int) {
	for _, r := range rows {
		dirs += r.Dirs
		branches += r.Branches
		tracking += r.Tracking
	}
	return dirs, branches, tracking
}

// chosen is the selection in number order.
func (s *session) chosen() []Row {
	out := make([]Row, 0, len(s.selected))
	for _, r := range s.selected {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out
}

// summaryKeys asks what to do with the summary; again is true when the selection changed and the
// summary must be shown again.
func (s *session) summaryKeys() (again bool, err error) {
	for tries := 0; ; {
		a, err := s.ask("Next: [Enter] continue  [e] edit the selection  [a] add all green  [q] quit: ")
		if err != nil {
			return false, err
		}
		if !a.tooLong {
			switch a.text {
			case "":
				return false, nil
			case "q":
				return false, ErrQuit
			case "a":
				s.addGreen()
				return true, nil
			case "e":
				return true, s.edit()
			}
		}
		if err := s.reject(&tries, "Answer Enter, e, a or q."); err != nil {
			return false, err
		}
	}
}

// edit replaces the selection with the rows of a line of terms; an empty line keeps it.
func (s *session) edit() error {
	for tries := 0; ; {
		a, err := s.ask("Terms (numbers, ranges, a b w t o:LABEL m:REPO, except; Enter keeps the selection): ")
		if err != nil {
			return err
		}
		if a.text == "" && !a.tooLong {
			return nil
		}
		var rows []Row
		if a.tooLong {
			err = ErrLineTooLong
		} else {
			rows, err = ParseSelection(a.text, s.all)
		}
		if err != nil {
			if err := s.reject(&tries, err.Error()+"."); err != nil {
				return err
			}
			continue
		}
		s.selected = map[int]Row{}
		_, err = s.addTerms(rows)
		return err
	}
}

func (s *session) showSummary() {
	rows := s.chosen()
	selectedIn := make([][]Row, len(s.groups))
	for _, r := range rows {
		gi := s.groupOf[r.ID]
		selectedIn[gi] = append(selectedIn[gi], r)
	}
	type line struct{ label, items, level, effect string }
	var lines []line
	width := len("group")
	skipped, skippedDirs, skippedBranches := 0, 0, 0
	for gi, g := range s.groups {
		sel := selectedIn[gi]
		if len(sel) == 0 {
			skipped++
			for _, r := range g.Rows {
				skippedDirs += r.Dirs
				skippedBranches += r.Branches
			}
			continue
		}
		label := clip(strings.TrimSpace(fmt.Sprintf("%d %s %s", len(sel), plural(len(sel), g.NounOne, g.NounMany), g.Place)), 60)
		if len([]rune(label)) > width {
			width = len([]rune(label))
		}
		dirs, branches, tracking := tally(sel)
		lines = append(lines, line{label, fmt.Sprint(len(sel)), string(worst(sel)), "delete " + effectText(dirs, branches, tracking)})
	}
	fmt.Fprintln(s.out, "\nSummary: nothing has been changed yet")
	fmt.Fprintf(s.out, "  %-*s  %5s  %-6s  %s\n", width, "group", "items", "risk", "effect")
	for _, l := range lines {
		fmt.Fprintf(s.out, "  %-*s  %5s  %-6s  %s\n", width, l.label, l.items, l.level, l.effect)
	}
	if skipped > 0 {
		var what []string
		if skippedDirs > 0 {
			what = append(what, count(skippedDirs, "directory", "directories"))
		}
		if skippedBranches > 0 {
			what = append(what, count(skippedBranches, "branch", "branches"))
		}
		detail := ""
		if len(what) > 0 {
			detail = " (" + strings.Join(what, ", ") + ")"
		}
		fmt.Fprintf(s.out, "  skipped: %s%s\n", count(skipped, "group", "groups"), detail)
	}
	repos := map[string]bool{}
	words := map[string]bool{}
	for _, r := range rows {
		repos[r.Repo] = true
		if r.Word != "" {
			words[r.Word] = true
		}
	}
	total := fmt.Sprintf("  total: %s in %s", count(len(rows), "item", "items"), count(len(repos), "repository", "repositories"))
	if s.opt.Kept > 0 {
		total += fmt.Sprintf("; %s untouched", count(s.opt.Kept, "kept row", "kept rows"))
	}
	fmt.Fprintln(s.out, total)
	if _, branches, _ := tally(rows); branches > 0 {
		fmt.Fprintln(s.out, "Undo (branches only): stw tidy restore PATH --contract 3 --plan PLAN_ID --state-dir DIR --apply --authorize")
		fmt.Fprintln(s.out, "      PLAN_ID: see stw tidy history --contract 3 --state-dir DIR; deleted directories are not restored")
	}
	if len(words) > 0 {
		sorted := make([]string, 0, len(words))
		for w := range words {
			sorted = append(sorted, w)
		}
		sort.Strings(sorted)
		// At most three words, each cut; the typed prompt itself names the word it asks for.
		shown := make([]string, 0, 6)
		for i, w := range sorted {
			if i == 3 {
				shown = append(shown, fmt.Sprintf("and %d more", len(sorted)-3))
				break
			}
			shown = append(shown, clip(escape(w), 16))
		}
		fmt.Fprintf(s.out, "Typed words that will be asked (on /dev/tty): %s\n", strings.Join(shown, ", "))
	}
}

func effectText(dirs, branches, tracking int) string {
	var parts []string
	if dirs > 0 {
		parts = append(parts, count(dirs, "directory", "directories"))
	}
	if branches > 0 {
		parts = append(parts, count(branches, "branch", "branches"))
	}
	if tracking > 0 {
		parts = append(parts, count(tracking, "tracking reference", "tracking references"))
	}
	return strings.Join(parts, " and ")
}

// worst is the worst level of the rows (orange over yellow over green).
func worst(rows []Row) risk.Level {
	out := risk.Green
	for _, r := range rows {
		switch r.Level {
		case risk.Orange:
			return risk.Orange
		case risk.Yellow:
			out = risk.Yellow
		}
	}
	return out
}
