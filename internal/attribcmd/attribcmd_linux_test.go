// SPDX-License-Identifier: EUPL-1.2

//go:build linux

package attribcmd

import (
	"os"
	"strconv"
	"testing"

	"github.com/wstein/sourcesteward/internal/attribution"
	"golang.org/x/sys/unix"
)

// A real terminal (a pseudo-terminal) on both streams is still a human origin; an agent marker or
// one stream that is not a terminal is not.
func TestOriginForStreamsOnAPseudoTerminal(t *testing.T) {
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	defer m.Close()
	if err := unix.IoctlSetPointerInt(int(m.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Skipf("unlock pty: %v", err)
	}
	n, err := unix.IoctlGetInt(int(m.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Skipf("pty number: %v", err)
	}
	tty, err := os.OpenFile("/dev/pts/"+strconv.Itoa(n), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("open pty slave: %v", err)
	}
	defer tty.Close()
	if o := originForStreams([]string{"HOME=/h"}, tty, tty, attributionNow); o.Kind != attribution.KindHuman {
		t.Errorf("pty: %+v", o)
	}
	null, _ := os.Open(os.DevNull)
	defer null.Close()
	if o := originForStreams([]string{"HOME=/h"}, tty, null, attributionNow); o.Kind == attribution.KindHuman {
		t.Errorf("stdout on the null device: %+v", o)
	}
	if o := originForStreams([]string{"STW_AGENT=1"}, tty, tty, attributionNow); o.Kind == attribution.KindHuman {
		t.Errorf("agent marker on a pty: %+v", o)
	}
}
