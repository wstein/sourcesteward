// SPDX-License-Identifier: EUPL-1.2

//go:build linux

package cli

import (
	"bytes"
	"context"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/tidywizard/planset"
	"golang.org/x/sys/unix"
)

func testPTY(t *testing.T) *os.File {
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
	return s
}

// With the shipped terminal facts (no seam), the mode of `stw tidy apply` follows the real
// streams: a terminal on both gives human mode, a terminal on one only gives agent mode.
func TestTidyApplyModeFollowsTheRealStreams(t *testing.T) {
	tty := testPTY(t)
	var calls []planset.ApplyRequest
	oldApply, oldEnv := tidyApplyPlanSet, tidyEnviron
	t.Cleanup(func() { tidyApplyPlanSet, tidyEnviron = oldApply, oldEnv })
	tidyEnviron = func() []string { return nil }
	tidyApplyPlanSet = func(_ context.Context, r planset.ApplyRequest) planset.ApplyResult {
		calls = append(calls, r)
		return okSet()
	}
	run := func(in, out any) owner.Evidence {
		calls = nil
		cmd := tidyCommand()
		cmd.SetIn(in.(interface{ Read([]byte) (int, error) }))
		cmd.SetOut(out.(interface{ Write([]byte) (int, error) }))
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"apply", setID, "--yes", "--state-dir", "/s"})
		if err := cmd.Execute(); err != nil || len(calls) != 1 {
			t.Fatalf("err %v calls %d", err, len(calls))
		}
		return calls[0].Mode
	}
	check := func(name string, e owner.Evidence, mode owner.Mode, stdin, stdout bool) {
		t.Helper()
		if e.Mode != mode || e.StdinTTY != stdin || e.StdoutTTY != stdout {
			t.Errorf("%s: %+v", name, e)
		}
	}
	check("both terminals", run(tty, tty), owner.ModeHuman, true, true)
	check("stdin not a terminal", run(strings.NewReader(""), tty), owner.ModeAgent, false, true)
	check("stdout not a terminal", run(tty, &bytes.Buffer{}), owner.ModeAgent, true, false)
}
