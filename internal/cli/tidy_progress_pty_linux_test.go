// SPDX-License-Identifier: EUPL-1.2

//go:build linux

package cli

import (
	"bytes"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/wstein/sourcesteward/internal/stwsync"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"golang.org/x/sys/unix"
)

// ptyPair opens a pseudo-terminal of the given size and returns both ends.
func ptyPair(t *testing.T, cols, rows int) (master, slave *os.File) {
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
	if err := unix.IoctlSetWinsize(int(m.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Col: uint16(cols), Row: uint16(rows)}); err != nil {
		t.Skipf("window size: %v", err)
	}
	return m, s
}

func TestTerminalSizeReadsThePty(t *testing.T) {
	_, s := ptyPair(t, 97, 31)
	if c, r := stwsync.TerminalSize(s); c != 97 || r != 31 {
		t.Fatalf("%d x %d", c, r)
	}
	if c, r := stwsync.TerminalSize(&bytes.Buffer{}); c != 0 || r != 0 {
		t.Fatalf("a buffer has the size %d x %d", c, r)
	}
}

// On a real pseudo-terminal the final screen of a run holds one line per repository, the lines fit
// the width the terminal reports, and the cursor stands at the start of the line below the block.
func TestTidyPlanOnARealTerminalLeavesOneLinePerRepository(t *testing.T) {
	master, slave := ptyPair(t, 21, 20)
	progressWorld(t, true, false, false)
	tidyStderrIsTerminal = owner.IsTerminal
	var (
		mu  sync.Mutex
		got bytes.Buffer
	)
	end := make(chan struct{})
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			mu.Lock()
			got.Write(buf[:n])
			seen := strings.Contains(got.String(), "END-OF-RUN")
			mu.Unlock()
			if err != nil || seen {
				close(end)
				return
			}
		}
	}()
	cmd := tidyCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(slave)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{"plan"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	// What follows the block starts on a clean line.
	if _, err := io.WriteString(slave, "END-OF-RUN\n"); err != nil {
		t.Fatal(err)
	}
	<-end
	mu.Lock()
	text := got.String()
	mu.Unlock()
	screen := newFakeScreen(21, 20)
	screen.feed(text)
	lines := screen.lines()
	want := []string{
		"stw tidy: 2/2 repos…",
		"✓ a analysed       …",
		"✓ b analysed       …",
		"END-OF-RUN",
	}
	if !equalLines(lines, want) || len(screen.unknown) != 0 || screen.wraps != 0 {
		t.Fatalf("screen %q unknown %q wraps %d\nraw %q", lines, screen.unknown, screen.wraps, text)
	}
}
