// SPDX-License-Identifier: EUPL-1.2

//go:build linux

package owner

import (
	"os"
	"slices"
	"strconv"
	"testing"

	"golang.org/x/sys/unix"
)

// openPTY returns a pseudo-terminal pair: a master and the slave a program sees as its terminal.
func openPTY(t *testing.T) (master, slave *os.File) {
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

func TestTerminalFactsOnAPseudoTerminal(t *testing.T) {
	_, tty := openPTY(t)
	pipeR, pipeW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pipeR.Close()
	defer pipeW.Close()
	if !IsTerminal(tty) {
		t.Fatal("a pty slave is a terminal")
	}
	for _, c := range []struct {
		name         string
		in, out      any
		wantIn, want bool
	}{
		{"both terminals", tty, tty, true, true},
		{"stdin piped", pipeR, tty, false, true},
		{"stdout piped", tty, pipeW, true, false},
	} {
		in, out := TerminalFacts(c.in, c.out)
		if in != c.wantIn || out != c.want {
			t.Errorf("%s: stdin=%v stdout=%v", c.name, in, out)
		}
	}
}

// The facts feed mode detection: two terminals and a clean environment are human mode; one stream
// that is not a terminal, or an agent marker on a terminal, is agent mode.
func TestDetectFromTerminalFacts(t *testing.T) {
	_, tty := openPTY(t)
	pipeR, pipeW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pipeR.Close()
	defer pipeW.Close()
	detect := func(in, out any, environ ...string) Evidence {
		si, so := TerminalFacts(in, out)
		return Detect(Env{StdinTTY: si, StdoutTTY: so, Environ: environ})
	}
	if e := detect(tty, tty, "HOME=/h"); e.Mode != ModeHuman || !e.StdinTTY || !e.StdoutTTY {
		t.Errorf("pty: %+v", e)
	}
	if e := detect(pipeR, tty); e.Mode != ModeAgent || e.StdinTTY {
		t.Errorf("piped stdin: %+v", e)
	}
	if e := detect(tty, pipeW); e.Mode != ModeAgent || e.StdoutTTY {
		t.Errorf("piped stdout: %+v", e)
	}
	if e := detect(tty, tty, "STW_AGENT=1"); e.Mode != ModeAgent || !slices.Equal(e.Markers, []string{"STW_AGENT"}) {
		t.Errorf("marker on a pty: %+v", e)
	}
}
