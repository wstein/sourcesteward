// SPDX-License-Identifier: EUPL-1.2

package statuscmd

import (
	"bytes"
	"os"
	"testing"

	"github.com/wstein/sourcesteward/internal/overview"
)

// devNull is a character device, which is what the command takes for a terminal.
func devNull(t *testing.T) *os.File {
	t.Helper()
	f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func TestDetectTerminalReadsTheWritersAndTheEnvironment(t *testing.T) {
	tty := devNull(t)
	for name, tc := range map[string]struct {
		env  map[string]string
		out  *os.File
		want Terminal
	}{
		"utf-8 terminal":       {map[string]string{"LC_ALL": "en_US.UTF-8", "TERM": "xterm"}, tty, Terminal{Color: true, Stdout: true, Stderr: true, UTF8: true}},
		"no colour":            {map[string]string{"NO_COLOR": "1", "LC_ALL": "C.UTF-8"}, tty, Terminal{Stdout: true, Stderr: true, UTF8: true}},
		"empty no colour":      {map[string]string{"NO_COLOR": "", "LC_ALL": "C.utf8"}, tty, Terminal{Color: true, Stdout: true, Stderr: true, UTF8: true}},
		"latin-1 locale":       {map[string]string{"LC_ALL": "de_DE.ISO-8859-1", "LANG": "en_US.UTF-8"}, tty, Terminal{Color: true, Stdout: true, Stderr: true}},
		"LC_CTYPE before LANG": {map[string]string{"LC_CTYPE": "en_US.UTF-8", "LANG": "C"}, tty, Terminal{Color: true, Stdout: true, Stderr: true, UTF8: true}},
		"LANG alone":           {map[string]string{"LANG": "en_US.UTF-8"}, tty, Terminal{Color: true, Stdout: true, Stderr: true, UTF8: true}},
		"no locale":            {map[string]string{}, tty, Terminal{Color: true, Stdout: true, Stderr: true}},
		"dumb terminal":        {map[string]string{"LC_ALL": "en_US.UTF-8", "TERM": "dumb"}, tty, Terminal{Color: true, Stdout: true, Stderr: true, UTF8: true, Dumb: true}},
	} {
		for _, k := range []string{"NO_COLOR", "LC_ALL", "LC_CTYPE", "LANG", "TERM"} {
			t.Setenv(k, "")
		}
		for k, v := range tc.env {
			t.Setenv(k, v)
		}
		if got := DetectTerminal(tc.out, tty); got != tc.want {
			t.Errorf("%s: %+v, want %+v", name, got, tc.want)
		}
	}
	for _, k := range []string{"NO_COLOR", "LC_ALL", "LC_CTYPE", "LANG", "TERM"} {
		t.Setenv(k, "")
	}
	t.Setenv("LC_ALL", "en_US.UTF-8")
	if got := DetectTerminal(&bytes.Buffer{}, &bytes.Buffer{}); got != (Terminal{UTF8: true}) {
		t.Errorf("pipes: %+v", got)
	}
}

func TestResolveIconsExplicitAlwaysWins(t *testing.T) {
	for _, tc := range []struct {
		flag string
		term Terminal
		want IconSet
	}{
		{"", Terminal{Stdout: true, UTF8: true}, Unicode},
		{"", Terminal{Stdout: true}, ASCII},
		{"", Terminal{UTF8: true}, ASCII},
		{"", Terminal{Stdout: true, UTF8: true, Dumb: true}, ASCII},
		{"unicode", Terminal{}, Unicode},
		{"nerd", Terminal{Dumb: true}, Nerd},
		{"ascii", Terminal{Stdout: true, UTF8: true}, ASCII},
	} {
		if got, err := ResolveIcons(tc.flag, tc.term); err != nil || got != tc.want {
			t.Errorf("%q %+v: %q %v, want %q", tc.flag, tc.term, got, err, tc.want)
		}
	}
	for _, bad := range []string{"emoji", "Unicode", " ascii", "nerd "} {
		if _, err := ResolveIcons(bad, Terminal{}); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
}

func TestEveryIconSetHasOneDistinctGlyphPerPriority(t *testing.T) {
	for _, set := range []IconSet{Unicode, Nerd, ASCII} {
		seen := map[string]overview.Priority{}
		for _, p := range overview.Priorities {
			g := set.Icon(p)
			if g == "" || seen[g] != "" {
				t.Errorf("%s: %q for %s repeats or is empty (%s)", set, g, p, seen[g])
			}
			seen[g] = p
			if set == ASCII {
				for _, r := range g {
					if r > 0x7e {
						t.Errorf("ascii glyph %q is not ASCII", g)
					}
				}
			}
		}
		if set.Attention() == "" || set.Failure() == "" {
			t.Errorf("%s: empty repository marker", set)
		}
	}
	if len(ASCII.Icon(overview.Red)) != len(ASCII.Icon(overview.Violet)) {
		t.Error("ascii glyphs must have one width")
	}
	if ASCII.Attention() != "! " || ASCII.Failure() != "! " {
		t.Error("the ascii repository marker is the one of Spec 033")
	}
}
