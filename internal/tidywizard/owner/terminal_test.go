// SPDX-License-Identifier: EUPL-1.2

package owner

import (
	"bytes"
	"os"
	"testing"
)

type fdOnly struct{ fd uintptr }

func (f fdOnly) Fd() uintptr { return f.fd }

// Only a real terminal (isatty) counts: a buffer, a pipe, a regular file, the null device (a
// character device but no terminal) and a closed file are not.
func TestIsTerminalRefusesEverythingThatIsNotATerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	reg, err := os.CreateTemp(t.TempDir(), "f")
	if err != nil {
		t.Fatal(err)
	}
	closed, err := os.CreateTemp(t.TempDir(), "c")
	if err != nil {
		t.Fatal(err)
	}
	_ = closed.Close()
	for name, v := range map[string]any{
		"nil": nil, "buffer": &bytes.Buffer{}, "pipe read": r, "pipe write": w, "null device": null,
		"regular file": reg, "closed file": closed, "bad descriptor": fdOnly{fd: 1 << 20}, "string": "tty",
	} {
		if IsTerminal(v) {
			t.Errorf("%s reported as a terminal", name)
		}
	}
}

func TestTerminalFactsReportsEachStreamOnItsOwn(t *testing.T) {
	in, out := TerminalFacts(&bytes.Buffer{}, &bytes.Buffer{})
	if in || out {
		t.Fatalf("buffers: stdin=%v stdout=%v", in, out)
	}
}
