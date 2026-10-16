// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// reposBudgetInventory is a valid, ok inventory whose single limitation is n bytes of padding.
func reposBudgetInventory(n int) *InventoryResult {
	return &InventoryResult{Status: "ok", Backend: "git", Workspaces: []InventoryWorkspace{{Manager: "git", Limitations: []string{strings.Repeat("x", n)}}}}
}

func reposBudgetEntry(i int, status string, code string, inv *InventoryResult) ReposEntry {
	e := ReposEntry{Index: i, Path: "/w/" + string(rune('a'+i)), Origin: "explicit", Status: status, Backend: "git", Notes: []string{}, Inventory: inv}
	if inv != nil && (status == "ok" || status == "partial") {
		e.Facts = &ReposFacts{HeadState: "attached", Dirty: ReposDirty{Value: "false"}}
	}
	if code != "" {
		e.Error = &ReposError{Code: code, Message: code + " message"}
	}
	return e
}

// M11: an omitted inventory records the output-limit cause as "<code>: <message>", moves a displaced lower
// precedence cause into the notes, keeps a higher precedence inherited cause as primary, counts the
// omission, and leaves entries that never had an inventory alone.
func TestReposOutputBudgetOmissionCauses(t *testing.T) {
	res := ReposResult{Status: "partial", Boundary: ReposBoundary{Limitations: []string{}}, Diagnostics: []Diagnostic{}, Entries: []ReposEntry{
		reposBudgetEntry(0, "ok", "", reposBudgetInventory(10)),
		reposBudgetEntry(1, "partial", "discovery-failed", reposBudgetInventory(5<<20)),
		reposBudgetEntry(2, "partial", "discovery-failed", reposBudgetInventory(5<<20)),   // omitted: output-limit outranks discovery-failed
		reposBudgetEntry(3, "error", "git-inventory-failed", reposBudgetInventory(5<<20)), // omitted: keeps its inherited code
		reposBudgetEntry(4, "skipped", "submodule-uninitialized", nil),
	}}
	applyOutputBudget(&res)
	if err := ReposCheckJSONBudget(res); err != nil {
		t.Fatalf("result over budget: %v", err)
	}
	if res.Entries[0].Inventory == nil || res.Entries[1].Inventory == nil {
		t.Fatal("inventories that fit must stay")
	}
	e2 := res.Entries[2]
	if e2.Inventory != nil || e2.Status != "error" || e2.Error.Code != ReposCodeOutputLimit || e2.Facts == nil {
		t.Fatalf("entry 2: %+v %+v", e2, e2.Error)
	}
	if len(e2.Notes) != 1 || e2.Notes[0] != "discovery-failed: discovery-failed message" {
		t.Fatalf("displaced cause must move to notes: %q", e2.Notes)
	}
	e3 := res.Entries[3]
	if e3.Inventory != nil || e3.Error.Code != "git-inventory-failed" || e3.Facts != nil {
		t.Fatalf("entry 3: %+v %+v", e3, e3.Error)
	}
	if len(e3.Notes) != 1 || !strings.HasPrefix(e3.Notes[0], "output-limit: ") {
		t.Fatalf("output-limit note format: %q", e3.Notes)
	}
	if e4 := res.Entries[4]; e4.Status != "skipped" || e4.Error.Code != "submodule-uninitialized" || len(e4.Notes) != 0 {
		t.Fatalf("an entry without inventory is not an omission: %+v", e4)
	}
	if res.Summary.InventoriesOmitted != 2 {
		t.Fatalf("inventories_omitted %d, want 2", res.Summary.InventoriesOmitted)
	}
	if res.Status != "partial" || res.Boundary.Complete {
		t.Fatalf("status %s complete %v", res.Status, res.Boundary.Complete)
	}
}

// M21: when the content around the inventories alone exceeds the bound, every inventory is dropped, the
// result is an error, and entries that never had an inventory are not relabelled.
func TestReposOutputBudgetFinalRecheck(t *testing.T) {
	skipped := reposBudgetEntry(2, "skipped", "submodule-uninitialized", nil)
	skipped.Path = strings.Repeat("p", 9<<20) // paths are validated earlier and not clipped here
	res := ReposResult{Status: "ok", Boundary: ReposBoundary{Limitations: []string{}}, Diagnostics: []Diagnostic{}, Entries: []ReposEntry{
		reposBudgetEntry(0, "ok", "", reposBudgetInventory(10)),
		reposBudgetEntry(1, "ok", "", reposBudgetInventory(10)),
		skipped,
	}}
	applyOutputBudget(&res)
	if res.Status != "error" || res.Boundary.Complete {
		t.Fatalf("status %s complete %v", res.Status, res.Boundary.Complete)
	}
	for i := 0; i < 2; i++ {
		if e := res.Entries[i]; e.Inventory != nil || e.Error == nil || e.Error.Code != ReposCodeOutputLimit || e.Status != "error" {
			t.Fatalf("entry %d: %+v", i, e)
		}
	}
	if e := res.Entries[2]; e.Status != "skipped" || e.Error.Code != "submodule-uninitialized" {
		t.Fatalf("skipped entry relabelled: %+v %+v", e, e.Error)
	}
	if res.Summary.InventoriesOmitted != 2 {
		t.Fatalf("inventories_omitted %d, want 2", res.Summary.InventoriesOmitted)
	}
}

// M3: invalid UTF-8 or an over-long message is cleaned, never a reason to drop every inventory.
func TestReposOutputBudgetKeepsInventoriesForEncodingProblems(t *testing.T) {
	e0 := reposBudgetEntry(0, "ok", "", reposBudgetInventory(10))
	e0.Notes = []string{"bad \xff note", strings.Repeat("é", 600)}
	e1 := reposBudgetEntry(1, "error", "git-failed", nil)
	e1.Error.Message = "bad \xfe message"
	res := ReposResult{Status: "partial", Boundary: ReposBoundary{Limitations: []string{"lim \xff"}}, Diagnostics: []Diagnostic{}, Entries: []ReposEntry{e0, e1, reposBudgetEntry(2, "ok", "", reposBudgetInventory(10))}}
	applyOutputBudget(&res)
	if res.Summary.InventoriesOmitted != 0 || res.Entries[0].Inventory == nil || res.Entries[2].Inventory == nil {
		t.Fatalf("an encoding problem dropped inventories: %+v", res.Summary)
	}
	for _, s := range append(append([]string{}, res.Entries[0].Notes...), res.Entries[1].Error.Message, res.Boundary.Limitations[0]) {
		if !utf8.ValidString(s) || len(s) > ReposMessageLimit {
			t.Fatalf("not cleaned: %d bytes, valid=%v", len(s), utf8.ValidString(s))
		}
	}
	if err := ReposCheckJSONBudget(res); err != nil {
		t.Fatal(err)
	}
}

func TestReposJSONBudgetSeparatesEncodingFromSize(t *testing.T) {
	enc := ReposCheckJSONBudget(ReposResult{Diagnostics: []Diagnostic{{Code: "x", Message: "bad \xff"}}})
	size := ReposCheckJSONBudget(ReposResult{Diagnostics: []Diagnostic{{Code: "x", Message: strings.Repeat("y", 9<<20)}}})
	if enc == nil || size == nil || !isReposEncodingError(enc) || isReposEncodingError(size) {
		t.Fatalf("encoding %v / size %v must be distinguishable", enc, size)
	}
}

// The omission itself adds bytes (an error, notes, a changed status). When the inventories that were kept
// leave less room than that, the final re-check must drop them too, or the result exceeds the bound.
func TestReposOutputBudgetFinalRecheckAfterOmissionGrowth(t *testing.T) {
	build := func(keep int) ReposResult {
		entries := []ReposEntry{reposBudgetEntry(0, "ok", "", reposBudgetInventory(keep))}
		for i := 1; i < 5; i++ {
			entries = append(entries, reposBudgetEntry(i, "ok", "", reposBudgetInventory(5<<20)))
		}
		return ReposResult{Status: "ok", Boundary: ReposBoundary{Limitations: []string{}}, Diagnostics: []Diagnostic{}, Entries: entries}
	}
	// The largest first inventory that still fits when the others are absent.
	fits := func(n int) bool {
		r := build(n)
		for i := 1; i < len(r.Entries); i++ {
			r.Entries[i].Inventory = nil
		}
		return ReposCheckJSONBudget(r) == nil
	}
	lo, hi := 0, 9<<20
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if fits(mid) {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	res := build(lo)
	applyOutputBudget(&res)
	if err := ReposCheckJSONBudget(res); err != nil {
		t.Fatalf("result exceeds the bound after omission: %v", err)
	}
	if res.Status != "error" || res.Summary.InventoriesOmitted != 5 {
		t.Fatalf("status %s omitted %d", res.Status, res.Summary.InventoriesOmitted)
	}
}
