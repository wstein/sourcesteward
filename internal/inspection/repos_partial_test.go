// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

const reposSampling = "Sampled observations are not transactional; concurrent and ABA changes may be missed."

// reposPartialEntry runs one fake Git member whose inventory is partial with the given limitations.
func reposPartialEntry(t *testing.T, limitations ...string) ReposEntry {
	t.Helper()
	p := reposTempDir(t)
	inspect := func(ctx context.Context, path string) Result {
		wt := path
		return Result{Status: "ok", Backend: "git", Repository: &Repository{GitDir: filepath.Join(wt, ".git"), CommonDir: filepath.Join(wt, ".git"), Worktree: &wt}}
	}
	inventory := func(ctx context.Context, path string, o InventoryOptions) InventoryResult {
		wt := path
		return InventoryResult{Status: "partial", Backend: "git",
			Identity:   Result{Status: "ok", Backend: "git", Repository: &Repository{GitDir: filepath.Join(wt, ".git"), CommonDir: filepath.Join(wt, ".git"), Worktree: &wt}},
			Boundary:   InventoryBoundary{Limitations: limitations},
			Workspaces: []InventoryWorkspace{}}
	}
	return repos(context.Background(), ReposOptions{Paths: []string{p}}, inspect, inventory).Entries[0]
}

// H4: the message is the first limitation after the fixed sampling statement that is not a base-candidate
// remark, and every limitation after the sampling statement is a note; the sampling sentence is neither.
func TestReposInventoryPartialMessageAndNotes(t *testing.T) {
	e := reposPartialEntry(t, reposSampling, "multiple base candidates: explicit --base selection required", "submodule disk state excluded", "stale worktree")
	if e.Status != "partial" || e.Error == nil || e.Error.Code != ReposCodeInventoryPartial {
		t.Fatalf("%+v %+v", e.Status, e.Error)
	}
	if e.Error.Message != "submodule disk state excluded" {
		t.Fatalf("message %q", e.Error.Message)
	}
	want := []string{
		"inventory-partial: multiple base candidates: explicit --base selection required",
		"inventory-partial: submodule disk state excluded",
		"inventory-partial: stale worktree",
	}
	if fmt.Sprint(e.Notes) != fmt.Sprint(want) {
		t.Fatalf("notes %q, want %q", e.Notes, want)
	}
}

func TestReposInventoryPartialWithOnlyTheSamplingStatement(t *testing.T) {
	e := reposPartialEntry(t, reposSampling)
	if e.Error == nil || e.Error.Message != "inventory status partial" || len(e.Notes) != 0 {
		t.Fatalf("%+v %q", e.Error, e.Notes)
	}
}

func TestReposInventoryPartialNotesAreBounded(t *testing.T) {
	lims := []string{reposSampling}
	for i := 0; i < 40; i++ {
		lims = append(lims, fmt.Sprintf("limitation %02d", i))
	}
	lims = append(lims, "é"+strings.Repeat("é", 600)) // the longest one is cut at a rune boundary
	e := reposPartialEntry(t, lims...)
	if len(e.Notes) != reposMaxPartialNotes+1 {
		t.Fatalf("%d notes, want %d limitation notes and one summary", len(e.Notes), reposMaxPartialNotes)
	}
	if last := e.Notes[len(e.Notes)-1]; last != "inventory-partial: 25 more limitations omitted" {
		t.Fatalf("summary note %q", last)
	}
	long := reposPartialEntry(t, reposSampling, strings.Repeat("é", 600))
	if m := long.Error.Message; len(m) > ReposMessageLimit || !utf8.ValidString(m) {
		t.Fatalf("message of %d bytes, valid=%v", len(m), utf8.ValidString(m))
	}
	for _, n := range long.Notes {
		if len(n) > ReposMessageLimit+len("inventory-partial: ") || !utf8.ValidString(n) {
			t.Fatalf("note of %d bytes", len(n))
		}
	}
}
