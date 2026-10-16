// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/wstein/sourcesteward/internal/inspection"
)

func renderRepos(t *testing.T, r inspection.ReposResult) string {
	t.Helper()
	var b bytes.Buffer
	if err := renderReposText(&b, r); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// Repository text is data: control characters, newlines, U+2028 and invalid UTF-8 never reach the
// terminal raw, and every rendered line stays one line.
func TestReposTextEscapesRepositoryText(t *testing.T) {
	r := reposCLIResult("partial")
	r.Entries[0].Path = "a\x1b[31m\nb\u2028c\xffd\u202ee"
	r.Entries[0].Error = &inspection.ReposError{Code: "git-failed", Message: "msg\rwith\ttab\x00nul\u0085next"}
	r.Entries[0].Notes = []string{"note\x1b]0;title\x07"}
	out := renderRepos(t, r)
	for _, bad := range []string{"\x1b", " ", "\xff", "\x00", "\r", "\u0085", "‮", "\x07"} {
		if strings.Contains(out, bad) {
			t.Errorf("raw %q in output %q", bad, out)
		}
	}
	for _, want := range []string{"\\x1b[31m\\nb\\u2028c\\xffd\\u202ee", `msg\rwith\ttab\x00nul\u0085next`, `note\x1b]0;title\x07`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing escaped %q in %q", want, out)
		}
	}
	if !utf8.ValidString(out) {
		t.Error("output is not valid UTF-8")
	}
}

// Each line is at most 512 bytes of valid UTF-8, cut at a rune boundary and never inside an escape.
func TestReposTextLineBound(t *testing.T) {
	for name, path := range map[string]string{
		"wide runes":     strings.Repeat("é", 2000),
		"escapes":        strings.Repeat("\x1b", 800),
		"escape on edge": strings.Repeat("a", 506) + "\x1b\x1b\x1b",
		"three-byte":     strings.Repeat("a", 507) + "日本語",
	} {
		r := reposCLIResult("partial")
		r.Entries[0].Path = path
		out := renderRepos(t, r)
		for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
			if len(line) > 512 || !utf8.ValidString(line) {
				t.Errorf("%s: line of %d bytes, valid=%v", name, len(line), utf8.ValidString(line))
			}
			if strings.HasSuffix(line, "...") && regexp.MustCompile(`\\x?[0-9a-f]{0,1}\.\.\.$`).MatchString(line) && !regexp.MustCompile(`(\\x[0-9a-f]{2}|[^\\x0-9a-f])\.\.\.$`).MatchString(line) {
				t.Errorf("%s: cut inside an escape: %q", name, line[len(line)-12:])
			}
		}
		if name == "escapes" && !regexp.MustCompile(`(\\x1b)+\.\.\.`).MatchString(out) {
			t.Errorf("escapes: expected whole escapes before the cut")
		}
	}
}

func staleWorkspaces(n int) []inspection.InventoryWorkspace {
	var ws []inspection.InventoryWorkspace
	for i := 0; i < n; i++ {
		p := fmt.Sprintf("/w/stale-%02d", i)
		ws = append(ws, inspection.InventoryWorkspace{Name: p, Path: &p, Manager: "git", Registration: "missing"})
	}
	return ws
}

func TestReposTextBoundsAttentionAndLines(t *testing.T) {
	r := reposCLIResult("partial")
	r.Entries[0].Inventory = &inspection.InventoryResult{Workspaces: staleWorkspaces(30)}
	out := renderRepos(t, r)
	if n := strings.Count(out, "(stale)"); n != 16 {
		t.Errorf("%d stale lines, want 16", n)
	}
	if !strings.Contains(out, "+14 more") {
		t.Errorf("missing '+14 more' in %q", out)
	}
	// With many notes the entry block is cut at 24 lines.
	r = reposCLIResult("partial")
	for i := 0; i < 40; i++ {
		r.Entries[0].Notes = append(r.Entries[0].Notes, fmt.Sprintf("note %02d", i))
	}
	r.Entries[0].Inventory = &inspection.InventoryResult{Workspaces: staleWorkspaces(30)}
	out = renderRepos(t, r)
	block := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "[0]") || strings.HasPrefix(line, "      ") {
			block++
		}
	}
	if block != 24 {
		t.Errorf("entry block has %d lines, want exactly 24 (bounded)", block)
	}
}

// L1: text and facts classify workspaces identically: a workspace without a manager is never foreign.
func TestReposTextWorkspaceClassificationMatchesFacts(t *testing.T) {
	r := reposCLIResult("partial")
	r.Entries[0].Inventory = &inspection.InventoryResult{Workspaces: []inspection.InventoryWorkspace{
		{Name: "no-manager", Manager: "", Registration: "unknown"},
		{Name: "other-manager", Manager: "jj", Registration: "unknown"},
		{Name: "gone", Manager: "jj", Registration: "missing"},
		{Name: "fine", Manager: "git", Registration: "present"},
	}}
	out := renderRepos(t, r)
	for _, want := range []string{"workspace no-manager (unverified)", "workspace other-manager (foreign)", "workspace gone (stale)"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
	if strings.Contains(out, "workspace fine") {
		t.Error("a healthy workspace is not an attention line")
	}
}

// Each boundary limitation is its own line, so the long residual-race disclosure is not cut off by the
// 512-byte line bound of a joined line.
func TestReposTextRendersOneLimitationPerLine(t *testing.T) {
	r := reposCLIResult("ok")
	long := "Discovered submodule paths are checked before Git runs, but a path swapped for a symlink in between can be read before it is rejected."
	r.Boundary.Limitations = []string{strings.Repeat("a", 300), strings.Repeat("b", 300), long}
	out := renderRepos(t, r)
	lines := 0
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "limitations: ") {
			lines++
		}
	}
	if lines != 3 || !strings.Contains(out, "limitations: "+long+"\n") {
		t.Fatalf("limitations are not one per line:\n%s", out)
	}
}
