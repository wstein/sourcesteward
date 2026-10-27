// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/tidywizard/prompt"
)

// docSession is one session of the janitor wizard that docs/tidy.md shows. The sessions run on the
// fixture of newJanitor (the same one the wizard tests use); the text of the documentation is the
// real output, pasted, and TestTidyDocsShowTheJanitorSessions fails when it drifts.
type docSession struct {
	name string
	// answers are the lines typed on stdin, in order; an empty line is Enter.
	answers []string
	// ownOnly keeps the safe rows of scope own, as the store question of the wizard needs.
	ownOnly bool
	// fromStart shows the whole output; otherwise it starts at the first question.
	fromStart bool
	// from and until cut the shown part: it starts at the first occurrence of from and ends before
	// the first occurrence of until after it (either may be empty).
	from, until string
	// storeYes answers the store question, which is read from the controlling terminal, with y.
	storeYes bool
	// showTyped shows the text written to the controlling terminal for the typed words.
	showTyped bool
}

var docSessions = []docSession{
	{name: "walk", answers: []string{"", "", "", "", "l", "d 6", "7", "s", ""}, fromStart: true, showTyped: true},
	{name: "review", answers: []string{"", "r", "y", "n", "s"}, from: "Step 1 of 4", until: "Step 3 of 4"},
	{name: "terms", answers: []string{"", "s", "s", "s", "9", "w except", "w except 3", "s", "e", "o:codex", ""}, from: "      7  orange  /w/unk"},
	{name: "decision", answers: []string{"", "s", "s", "s", "6", "y", "s", "q"}, from: "      7  orange  /w/unk"},
	{name: "own", answers: []string{"a", ""}, ownOnly: true, storeYes: true},
}

var (
	docPlanID    = regexp.MustCompile(`\b[0-9a-f]{12}\b`)
	docWordPlan  = regexp.MustCompile(`(Type "[a-z]+ )[0-9a-f]{6}(")`)
	docAnswerSep = ", "
)

// answerLine is the line of the documentation that lists the answers of a session.
func (s docSession) answerLine() string {
	parts := make([]string, len(s.answers))
	for i, a := range s.answers {
		parts[i] = "`" + a + "`"
		if a == "" {
			parts[i] = "Enter"
		}
	}
	return "Answers, in order: " + strings.Join(parts, docAnswerSep) + "."
}

// docOutput runs the session and returns what the documentation shows: the output of the run, and
// what was written to the terminal for the typed words. The plan id (12 hex characters of a SHA-256
// over the plan content) is normalized to 3f9c2a7e41b8 and the state directory to /state, so the
// sample stays stable; trailing spaces of the prompts are dropped.
func docOutput(t *testing.T, s docSession) (stdout, typed string) {
	t.Helper()
	j := newJanitor(t)
	if s.ownOnly {
		var own []tidywizard.Candidate
		for _, c := range j.res.Candidates {
			if c.Scope == owner.ScopeOwn && c.Verdict == tidywizard.VerdictSafe {
				own = append(own, c)
			}
		}
		j.res.Candidates = own
	}
	out, tty, err := j.run(strings.Join(s.answers, "\n")+"\n", func(q string) string {
		if s.storeYes && strings.Contains(q, "Store this plan set?") {
			return "y"
		}
		return rightWord(q)
	})
	if err != nil {
		t.Fatalf("%s: %v\n%s", s.name, err, out)
	}
	norm := func(text string) string {
		text = strings.ReplaceAll(text, j.prep.PlanStore, "/state")
		text = docPlanID.ReplaceAllString(text, "3f9c2a7e41b8")
		text = docWordPlan.ReplaceAllString(text, "${1}3f9c2a${2}")
		lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
		for i := range lines {
			lines[i] = strings.TrimRight(lines[i], " ")
		}
		return strings.Join(lines, "\n")
	}
	stdout = norm(out)
	if s.from != "" {
		i := strings.Index(stdout, s.from)
		if i < 0 {
			t.Fatalf("%s: %q is not in the output:\n%s", s.name, s.from, stdout)
		}
		stdout = stdout[i:]
		if s.until != "" {
			if k := strings.Index(stdout, s.until); k >= 0 {
				stdout = strings.TrimRight(stdout[:k], "\n")
			}
		}
	} else if !s.fromStart {
		if i := strings.Index(stdout, "Continue?"); i >= 0 {
			stdout = stdout[i:]
		} else if i := strings.Index(stdout, "Can be cleaned"); i >= 0 {
			stdout = stdout[i:]
		}
	}
	return stdout, norm(strings.Join(tty.prompts, "\n"))
}

func TestTidyDocsShowTheJanitorSessions(t *testing.T) {
	b, err := os.ReadFile("../../docs/tidy.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(b)
	for _, s := range docSessions {
		stdout, typed := docOutput(t, s)
		if got := "```text\n" + stdout + "\n```\n"; !strings.Contains(doc, got) {
			t.Errorf("docs/tidy.md does not contain the session %q:\n%s", s.name, got)
		}
		if !strings.Contains(doc, s.answerLine()) {
			t.Errorf("docs/tidy.md does not contain the answers of the session %q: %s", s.name, s.answerLine())
		}
		if s.showTyped {
			if got := "```text\n" + typed + "\n```\n"; !strings.Contains(doc, got) {
				t.Errorf("docs/tidy.md does not contain the typed prompts of the session %q:\n%s", s.name, got)
			}
		}
	}
	// The one number of the prose that the sessions do not show is pinned to the code.
	if want := fmt.Sprintf("lines over %d bytes", prompt.MaxSelectionBytes); !strings.Contains(doc, want) {
		t.Errorf("docs/tidy.md does not say %q", want)
	}
}
