// SPDX-License-Identifier: EUPL-1.2
package inspection_test

import (
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/inspection"
)

func TestValidateReposOptions(t *testing.T) {
	// Zero paths
	if err := inspection.ValidateReposOptions(inspection.ReposOptions{}); err == nil {
		t.Fatal("expected error for 0 paths, got nil")
	}

	// More than 16 paths
	paths := make([]string, 17)
	for i := range paths {
		paths[i] = "/w/repo"
	}
	if err := inspection.ValidateReposOptions(inspection.ReposOptions{Paths: paths}); err == nil {
		t.Fatal("expected error for >16 paths, got nil")
	}

	// Empty path
	if err := inspection.ValidateReposOptions(inspection.ReposOptions{Paths: []string{""}}); err == nil {
		t.Fatal("expected error for empty path, got nil")
	}

	// Path with null byte
	if err := inspection.ValidateReposOptions(inspection.ReposOptions{Paths: []string{"/w/\x00repo"}}); err == nil {
		t.Fatal("expected error for path with null byte, got nil")
	}

	// Path with invalid UTF-8
	if err := inspection.ValidateReposOptions(inspection.ReposOptions{Paths: []string{"/w/\xffrepo"}}); err == nil {
		t.Fatal("expected error for path with invalid UTF-8, got nil")
	}

	// Path exceeding 4096 bytes
	longPath := "/" + strings.Repeat("a", 4097)
	if err := inspection.ValidateReposOptions(inspection.ReposOptions{Paths: []string{longPath}}); err == nil {
		t.Fatal("expected error for path exceeding 4096 bytes, got nil")
	}

	// Valid options
	if err := inspection.ValidateReposOptions(inspection.ReposOptions{Paths: []string{"/w/repo1", "/w/repo2"}}); err != nil {
		t.Fatalf("expected valid options, got: %v", err)
	}
}

func TestReposOverallStatus(t *testing.T) {
	// Empty entries -> error
	if got := inspection.ReposOverallStatus(nil); got != "error" {
		t.Fatalf("empty entries: got %s, want error", got)
	}

	okInv := &inspection.InventoryResult{Status: "ok"}
	partialInv := &inspection.InventoryResult{Status: "partial"}

	// All ok
	entriesOk := []inspection.ReposEntry{
		{Status: "ok", Inventory: okInv},
	}
	if got := inspection.ReposOverallStatus(entriesOk); got != "ok" {
		t.Fatalf("all ok: got %s, want ok", got)
	}

	// Ok and skipped -> ok
	entriesOkSkipped := []inspection.ReposEntry{
		{Status: "ok", Inventory: okInv},
		{Status: "skipped"},
	}
	if got := inspection.ReposOverallStatus(entriesOkSkipped); got != "ok" {
		t.Fatalf("ok and skipped: got %s, want ok", got)
	}

	// Only partial entries -> partial
	entriesPartialOnly := []inspection.ReposEntry{
		{Status: "partial", Inventory: partialInv},
		{Status: "partial", Inventory: partialInv},
	}
	if got := inspection.ReposOverallStatus(entriesPartialOnly); got != "partial" {
		t.Fatalf("only partial: got %s, want partial", got)
	}

	// Mixed ok and partial -> partial
	entriesMixed := []inspection.ReposEntry{
		{Status: "ok", Inventory: okInv},
		{Status: "partial", Inventory: partialInv},
	}
	if got := inspection.ReposOverallStatus(entriesMixed); got != "partial" {
		t.Fatalf("mixed ok/partial: got %s, want partial", got)
	}

	// Ok and error -> partial
	entriesOkError := []inspection.ReposEntry{
		{Status: "ok", Inventory: okInv},
		{Status: "error"},
	}
	if got := inspection.ReposOverallStatus(entriesOkError); got != "partial" {
		t.Fatalf("ok and error: got %s, want partial", got)
	}

	// Only failed entries -> error
	entriesFailedOnly := []inspection.ReposEntry{
		{Status: "error"},
		{Status: "error"},
	}
	if got := inspection.ReposOverallStatus(entriesFailedOnly); got != "error" {
		t.Fatalf("only failed: got %s, want error", got)
	}

	// Only skipped entries -> error (no usable inventory)
	entriesSkippedOnly := []inspection.ReposEntry{
		{Status: "skipped"},
	}
	if got := inspection.ReposOverallStatus(entriesSkippedOnly); got != "error" {
		t.Fatalf("only skipped: got %s, want error", got)
	}
}

func TestReposResolveCausesPrecedence(t *testing.T) {
	causes := []inspection.ReposError{
		{Code: "inventory-partial", Message: "submodule disk state excluded"},
		{Code: "not-repository-root", Message: "path is not root"},
		{Code: "discovery-failed", Message: "blob too large"},
	}
	primary, notes := inspection.ReposResolveCauses(causes)
	if primary.Code != "not-repository-root" {
		t.Fatalf("got primary %s, want not-repository-root", primary.Code)
	}
	if len(notes) != 2 {
		t.Fatalf("expected 2 notes, got %d", len(notes))
	}
	if notes[0] != "discovery-failed: blob too large" || notes[1] != "inventory-partial: submodule disk state excluded" {
		t.Fatalf("unexpected notes order: %v", notes)
	}
}

func TestReposJSONBudget(t *testing.T) {
	// A standard result should pass budget check
	res := inspection.ReposResult{
		SchemaVersion: 1,
		Command:       "repos",
		Status:        "ok",
		InputPaths:    []string{"/w/lib"},
		Boundary: inspection.ReposBoundary{
			Complete:  true,
			Freshness: "sampled-nontransactional",
			Effects:   inspection.ReposDefaultEffects(),
			Limits:    inspection.ReposDefaultLimits(),
		},
		Entries: []inspection.ReposEntry{
			{Index: 0, Path: "/w/lib", Status: "ok"},
		},
	}
	if err := inspection.ReposCheckJSONBudget(res); err != nil {
		t.Fatalf("expected budget check to pass, got: %v", err)
	}

	// Oversized string should fail budget check
	hugeRes := res
	hugeRes.Boundary.Limitations = []string{strings.Repeat("x", inspection.ReposMaxOutputBytes+10)}
	if err := inspection.ReposCheckJSONBudget(hugeRes); err == nil {
		t.Fatal("expected budget check to fail for oversized result, got nil")
	}
}
