// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
)

func ownJanitor(t *testing.T) *janitor {
	t.Helper()
	j := newJanitor(t)
	var own []tidywizard.Candidate
	for _, c := range j.res.Candidates {
		if c.Scope == owner.ScopeOwn {
			own = append(own, c)
		}
	}
	j.res.Candidates = own
	return j
}

func TestTerminalStoreAnswerIgnoresStdin(t *testing.T) {
	for _, tc := range []struct {
		stdin, tty string
		stored     bool
	}{{"y\n", "n\n", false}, {"n\n", "y\n", true}} {
		t.Run(tc.tty, func(t *testing.T) {
			j := ownJanitor(t)
			tty := setTerminalAnswers(t, tc.tty)
			out, err := runTidy(t, "a\n\n"+tc.stdin)
			if err != nil {
				t.Fatal(err)
			}
			if got := len(storedSets(t, j.prep.PlanStore)) == 1; got != tc.stored {
				t.Fatalf("stored=%v want=%v, output=%s", got, tc.stored, out)
			}
			if strings.Count(tty.String(), "Store this plan set? [y/N/q]: ") != 1 || strings.Contains(out, "Store this plan set?") {
				t.Fatalf("question not on terminal: tty=%q stdout=%q", tty.String(), out)
			}
		})
	}
}

func TestTerminalRowApplyAnswerIgnoresStdin(t *testing.T) {
	for _, tc := range []struct {
		stdin, tty string
		applied    bool
	}{{"y\n", "n\n", false}, {"n\n", "y\n", true}} {
		t.Run(tc.tty, func(t *testing.T) {
			i := newInteractive(t, true, true)
			tty := setTerminalAnswers(t, tc.tty)
			out, err := runTidy(t, "a\n"+tc.stdin, "--rows")
			if err != nil {
				t.Fatal(err)
			}
			if (len(i.applied) == 1) != tc.applied {
				t.Fatalf("applied=%d want=%v", len(i.applied), tc.applied)
			}
			if strings.Count(tty.String(), "Apply it now?") != 1 || strings.Contains(out, "Apply it now?") {
				t.Fatalf("question not on terminal: tty=%q stdout=%q", tty.String(), out)
			}
		})
	}
}

func TestTerminalUnavailableRefusesBeforeAnyPlan(t *testing.T) {
	for _, rows := range []bool{false, true} {
		for _, foreign := range []bool{false, true} {
			name := "janitor-own"
			if rows {
				name = "rows-own"
			}
			if foreign {
				name += "-foreign"
			}
			t.Run(name, func(t *testing.T) {
				j := newJanitor(t)
				if !foreign {
					var own []tidywizard.Candidate
					for _, c := range j.res.Candidates {
						if c.Scope == owner.ScopeOwn {
							own = append(own, c)
						}
					}
					j.res.Candidates = own
				}
				before := terminalStoreListing(t, j.prep.PlanStore)
				old := tidyConfirmOpener
				tidyConfirmOpener = func() (io.ReadWriteCloser, error) { return nil, owner.ErrNoTerminal }
				t.Cleanup(func() { tidyConfirmOpener = old })
				input := "a\n\ny\n"
				var args []string
				if rows {
					args = []string{"--rows"}
					input = "a\ny\n"
					if foreign {
						input = "o\ny\ny\ny\ny\n"
					}
				}
				out, err := runTidy(t, input, args...)
				var failure runtimeFailure
				if !errors.As(err, &failure) || !strings.Contains(err.Error(), "no controlling terminal") || !strings.Contains(err.Error(), "stw tidy plan --save") || !strings.Contains(err.Error(), "apply from a terminal") {
					t.Fatalf("expected FR-018 refusal, got %v output=%s", err, out)
				}
				if len(j.planner.specs) != 0 || !reflect.DeepEqual(before, terminalStoreListing(t, j.prep.PlanStore)) || len(j.applied) != 0 {
					t.Fatalf("planned=%d before=%v after=%v applied=%d", len(j.planner.specs), before, terminalStoreListing(t, j.prep.PlanStore), len(j.applied))
				}
			})
		}
	}
}

// Reading even one byte beyond selection would consume answers intended for another program.
type selectionOnly struct {
	t *testing.T
	*strings.Reader
}

func (s selectionOnly) Read(p []byte) (int, error) {
	if s.Len() == 0 {
		s.t.Error("stdin read after selection")
		return 0, io.EOF
	}
	if len(p) > s.Len() {
		s.t.Error("stdin reader requested bytes beyond selection")
		p = p[:s.Len()]
	}
	return s.Reader.Read(p)
}
func TestTerminalNoStdinReadAfterSelection(t *testing.T) {
	for _, rows := range []bool{false, true} {
		t.Run(map[bool]string{false: "janitor", true: "rows"}[rows], func(t *testing.T) {
			ownJanitor(t)
			setTerminalAnswers(t, "y\n")
			cmd := tidyCommand()
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			input := "a\n\n"
			if rows {
				input = "a\n"
				cmd.SetArgs([]string{"--rows"})
			}
			cmd.SetIn(selectionOnly{t, strings.NewReader(input)})
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Include every directory and record, including lock files: a refusal must leave the whole store
// listing unchanged, regardless of the file names a sub-plan implementation chooses.
func terminalStoreListing(t *testing.T, store string) []string {
	t.Helper()
	var listing []string
	if err := filepath.WalkDir(store, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(store, path)
		if err != nil {
			return err
		}
		listing = append(listing, rel)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return listing
}
