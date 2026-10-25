// SPDX-License-Identifier: EUPL-1.2

//go:build linux

package cli

import (
	"bytes"
	"context"
	"os"
	"strconv"
	"testing"

	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/tidywizard/planset"
	"golang.org/x/sys/unix"
)

func testPTYPair(t *testing.T) (master, slave *os.File) {
	t.Helper()
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })
	if err := unix.IoctlSetPointerInt(int(m.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Skipf("unlock pty: %v", err)
	}
	n, err := unix.IoctlGetInt(int(m.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Skipf("pty number: %v", err)
	}
	s, err := os.OpenFile("/dev/pts/"+strconv.Itoa(n), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("open pty slave: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return m, s
}

// End to end over a pseudo-terminal through the real entry point: `stw tidy apply ID` on a terminal
// asks "Apply plan ID ...? [y/N]" and a typed y authorizes it, a bare Enter does not; with the
// empty reader of Run the same command line is agent mode and never asks.
func TestApplyPromptsOnARealTerminalThroughRunWithStdin(t *testing.T) {
	for _, tc := range []struct {
		typed string
		want  bool
	}{{"y\n", true}, {"\n", false}, {"n\n", false}} {
		master, tty := testPTYPair(t)
		var calls []planset.ApplyRequest
		oldApply, oldFind, oldEnv := tidyApplyPlanSet, tidyFindPlanSet, tidyEnviron
		t.Cleanup(func() { tidyApplyPlanSet, tidyFindPlanSet, tidyEnviron = oldApply, oldFind, oldEnv })
		tidyEnviron = func() []string { return nil }
		tidyApplyPlanSet = func(_ context.Context, r planset.ApplyRequest) planset.ApplyResult {
			calls = append(calls, r)
			if !r.Authorize {
				return planset.ApplyResult{SchemaVersion: 1, Command: "tidy-apply-set", Status: "refused", Code: planset.CodeAuthorizationRequired, PlanID: setID, Outcomes: []planset.Outcome{}}
			}
			return okSet()
		}
		tidyFindPlanSet = func([]string, string) (planset.Set, error) {
			return planset.Set{PlanID: setID, SubPlans: []planset.SubPlan{{ID: "a"}}, Items: []planset.Item{{ID: "i"}}}, nil
		}
		if _, err := master.WriteString(tc.typed); err != nil {
			t.Fatal(err)
		}
		var diag bytes.Buffer
		code := RunWithStdin(context.Background(), "test", []string{"tidy", "apply", setID, "--state-dir", "/s"}, tty, tty, &diag)
		if !tc.want {
			// Declining stops before the apply path: exit 1, nothing applied.
			if len(calls) != 0 || code != 1 {
				t.Errorf("typed %q: code %d, calls %+v, stderr %q", tc.typed, code, calls, diag.String())
			}
			continue
		}
		if len(calls) != 1 || !calls[0].Authorize || calls[0].Mode.Mode != owner.ModeHuman || !calls[0].Mode.StdinTTY || !calls[0].Mode.StdoutTTY || code != 0 {
			t.Errorf("typed %q: code %d, calls %+v, stderr %q", tc.typed, code, calls, diag.String())
		}
	}
}

// Bare `stw tidy` through the real entry point on a pseudo-terminal is human mode and asks: the
// answers typed on the terminal choose the rows, the second question declines to apply.
func TestBareTidyAsksOnARealTerminalThroughRunWithStdin(t *testing.T) {
	master, tty := testPTYPair(t)
	i := newInteractive(t, true, true)
	tidyTerminalFacts = owner.TerminalFacts // the real descriptors, not the stub
	if _, err := master.WriteString("a\nn\n"); err != nil {
		t.Fatal(err)
	}
	var diag bytes.Buffer
	code := RunWithStdin(context.Background(), "test", []string{"tidy"}, tty, tty, &diag)
	if code != 0 || len(i.planner.specs) == 0 || len(i.applied) != 0 || len(storedSets(t, i.prep.PlanStore)) != 1 {
		t.Errorf("code %d, planned %d, applied %d, stored %v, stderr %q", code, len(i.planner.specs), len(i.applied), storedSets(t, i.prep.PlanStore), diag.String())
	}
}
