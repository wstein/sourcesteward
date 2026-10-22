// SPDX-License-Identifier: EUPL-1.2

// Package statuscmd runs the online `stw status` (Spec 037): it reads the local status, selects the
// repositories the remote sources apply to, collects them through internal/statusremote, builds the
// action model of internal/overview and builds the schema 2 document. It decides what the output
// channels and the environment allow (colour, icon set, progress); the human text is printed by
// internal/cli, which owns the Spec 033 repository blocks. Nothing is written to disk or to the forge.
package statuscmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/wstein/sourcesteward/internal/overview"
	"github.com/wstein/sourcesteward/internal/stwsync"
)

// Terminal is what the output channels and the environment allow.
type Terminal struct {
	Color  bool // stdout is a terminal and NO_COLOR is unset or empty
	Stdout bool // stdout is a terminal
	Stderr bool // stderr is a terminal: the only place for progress
	UTF8   bool // the locale (LC_ALL, else LC_CTYPE, else LANG) is UTF-8
	Dumb   bool // TERM is dumb
}

// DetectTerminal reads the two writers and the environment.
func DetectTerminal(stdout, stderr io.Writer) Terminal {
	return Terminal{
		Color:  stwsync.ColorEnabled(stdout),
		Stdout: stwsync.IsTerminal(stdout),
		Stderr: stwsync.IsTerminal(stderr),
		UTF8:   utf8Locale(),
		Dumb:   os.Getenv("TERM") == "dumb",
	}
}

// utf8Locale reports whether the effective locale names UTF-8: the first of LC_ALL, LC_CTYPE and LANG
// that is set decides.
func utf8Locale() bool {
	for _, name := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := os.Getenv(name); v != "" {
			v = strings.ToLower(v)
			return strings.Contains(v, "utf-8") || strings.Contains(v, "utf8")
		}
	}
	return false
}

// IconSet is one of the three glyph sets of the human output.
type IconSet string

// The icon sets of `--icons`.
const (
	Unicode IconSet = "unicode"
	Nerd    IconSet = "nerd"
	ASCII   IconSet = "ascii"
)

// glyphs holds the glyphs of one set: one per priority and the two markers of the repository blocks.
// The Nerd Font code points are those of nf-fa-times_circle, nf-fa-warning, nf-fa-check_circle,
// nf-fa-info_circle and nf-fa-arrow_circle_up.
type glyphs struct {
	priority          map[overview.Priority]string
	attention, failed string
}

var iconTable = map[IconSet]glyphs{
	Unicode: {
		priority:  map[overview.Priority]string{overview.Red: "✖", overview.Yellow: "▲", overview.Green: "✔", overview.Blue: "ℹ", overview.Violet: "⬆"},
		attention: "▲ ", failed: "✖ ",
	},
	Nerd: {
		priority:  map[overview.Priority]string{overview.Red: "", overview.Yellow: "", overview.Green: "", overview.Blue: "", overview.Violet: ""},
		attention: " ", failed: " ",
	},
	ASCII: {
		priority:  map[overview.Priority]string{overview.Red: "[!!]", overview.Yellow: "[! ]", overview.Green: "[ok]", overview.Blue: "[i ]", overview.Violet: "[^ ]"},
		attention: "! ", failed: "! ",
	},
}

// ResolveIcons gives the set of `--icons`: an explicit value always wins, the default is unicode on a
// terminal with a UTF-8 locale and ascii everywhere else (a pipe, a file, a non-UTF-8 locale, TERM=dumb).
func ResolveIcons(flag string, t Terminal) (IconSet, error) {
	switch IconSet(flag) {
	case Unicode, Nerd, ASCII:
		return IconSet(flag), nil
	case "":
		if t.Stdout && t.UTF8 && !t.Dumb {
			return Unicode, nil
		}
		return ASCII, nil
	}
	return "", fmt.Errorf("--icons %q is not unicode, nerd or ascii", flag)
}

// Icon is the glyph of a priority.
func (s IconSet) Icon(p overview.Priority) string { return iconTable[s].priority[p] }

// Attention is the marker of a repository that needs attention, with its trailing space.
func (s IconSet) Attention() string { return iconTable[s].attention }

// Failure is the marker of a repository that cannot be read, with its trailing space.
func (s IconSet) Failure() string { return iconTable[s].failed }
