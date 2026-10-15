// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInventoryOptionsValidation(t *testing.T) {
	for _, o := range []InventoryOptions{
		{}, {Query: "clean"}, {Query: "dirty", Scopes: []string{"stored"}},
		{Query: "ref-exists", Ref: "topic"}, {Query: "ancestor", From: "HEAD", To: "refs/heads/topic"},
		{Scopes: []string{"disk", "staged", "ignored"}, Paths: []string{"[literal]"}, Anchor: "cwd"},
	} {
		if err := ValidateInventoryOptions(o); err != nil {
			t.Errorf("valid options %#v: %v", o, err)
		}
	}
	for _, o := range []InventoryOptions{
		{Query: "maybe"}, {Query: "ref-exists"}, {Query: "ancestor", From: "HEAD"},
		{Query: "clean", Ref: "topic"}, {Query: "ref-exists", Ref: "topic", Scopes: []string{"disk"}},
		{Query: "ancestor", From: "HEAD", To: "HEAD", Paths: []string{"file"}},
		{Scopes: []string{"everything"}}, {Anchor: "other"}, {Paths: []string{"../outside"}},
		{Paths: []string{"/absolute"}}, {Ref: "\xff"}, {Remotes: []string{""}},
		{Query: "ref-exists", Ref: "topic", Base: "HEAD"},
	} {
		if err := ValidateInventoryOptions(o); err == nil {
			t.Errorf("accepted invalid options %#v", o)
		}
	}
}
func TestInventoryLiteralSelection(t *testing.T) {
	for _, tc := range []struct {
		path    string
		filters []string
		want    bool
	}{
		{"file", nil, true}, {"[x]", []string{"[x]"}, true}, {"x", []string{"[x]"}, false},
		{"a/b", []string{"a"}, true}, {"ab", []string{"a"}, false}, {"-x\n", []string{"-x\n"}, true},
	} {
		if got := inventoryPathSelected(tc.path, tc.filters); got != tc.want {
			t.Errorf("%q %v: %v", tc.path, tc.filters, got)
		}
	}
}
func TestInventoryEnvelopeAndLimits(t *testing.T) {
	id := Result{SchemaVersion: 1, Status: "ok", Backend: "git", Evidence: []Evidence{}, Diagnostics: []Diagnostic{}}
	r := newInventoryResult(id)
	raw, e := json.Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(raw), `":null`) && strings.Contains(string(raw), `"files":null`) {
		t.Fatal("null collections")
	}
	for _, key := range []string{`"workspaces":[]`, `"references":[]`, `"bases":[]`, `"files":[]`, `"diagnostics":[]`} {
		if !strings.Contains(string(raw), key) {
			t.Fatal(string(raw))
		}
	}
	r.References = make([]InventoryReference, 1025)
	if e := checkInventoryLimits(r); e == nil {
		t.Fatal("record bound not enforced")
	}
	r = newInventoryResult(id)
	r.Boundary.Limitations = []string{strings.Repeat("x", 1048577)}
	if e := checkInventoryLimits(r); e == nil {
		t.Fatal("output bound not enforced")
	}
	r = newInventoryResult(id)
	r.References = []InventoryReference{{AddedTargets: make([]InventoryTarget, 1025)}}
	if e := checkInventoryLimits(r); e == nil {
		t.Fatal("nested target bound not enforced")
	}
}
func TestInventoryUnknownIsNotFalse(t *testing.T) {
	if known(false).Value != "false" || unknown("unsupported").Value != "unknown" {
		t.Fatal("three valued observation lost")
	}
	r := newInventoryResult(Result{})
	r.partial("unsupported")
	if r.Status != "partial" || r.Boundary.Complete {
		t.Fatal(r)
	}
	r.fail("failure", "operation failed")
	r.partial("another unknown")
	if r.Status != "error" {
		t.Fatal("partial masked error")
	}
}

func TestInventoryCancellationAndInvalidOptions(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := Inventory(ctx, ".", InventoryOptions{})
	if r.Status != "error" || r.Boundary.Complete || len(r.Diagnostics) == 0 {
		t.Fatal(r)
	}
	r = Inventory(context.Background(), ".", InventoryOptions{Query: "not-a-predicate"})
	if r.Status != "error" || len(r.Diagnostics) == 0 || r.Diagnostics[0].Code != "invalid-options" {
		t.Fatal(r)
	}
}
func TestInventoryNormalizesLiteralPaths(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if e := os.Mkdir(sub, 0700); e != nil {
		t.Fatal(e)
	}
	id := Result{Backend: "git", Repository: &Repository{Worktree: &root}, InputPath: sub}
	o, e := normalizeInventoryOptions(id, InventoryOptions{Paths: []string{"a/../[x]"}})
	if e != nil || len(o.Paths) != 1 || o.Paths[0] != "[x]" {
		t.Fatal(o, e)
	}
	old, e := os.Getwd()
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chdir(sub); e != nil {
		t.Fatal(e)
	}
	defer os.Chdir(old)
	o, e = normalizeInventoryOptions(id, InventoryOptions{Anchor: "cwd", Paths: []string{"../sibling"}})
	if e != nil || o.Paths[0] != "sibling" {
		t.Fatal(o, e)
	}
	_, e = normalizeInventoryOptions(id, InventoryOptions{Anchor: "cwd", Paths: []string{"../../outside"}})
	if e == nil {
		t.Fatal("escaped workspace")
	}
}

func TestInventoryRejectsUnboundedOrUndecodableMetadata(t *testing.T) {
	r := newInventoryResult(Result{})
	r.Boundary.Limitations = []string{"\xff"}
	if checkInventoryLimits(r) == nil {
		t.Fatal("invalid UTF-8 would be silently replaced")
	}
	r = newInventoryResult(Result{})
	r.Query = &InventoryQuery{Evidence: []string{strings.Repeat("x", inventoryResultLimit+1)}}
	if checkInventoryLimits(r) == nil {
		t.Fatal("oversized query evidence accepted")
	}
	if ValidateInventoryOptions(InventoryOptions{Remotes: []string{strings.Repeat("x", outputLimit/2), strings.Repeat("y", outputLimit/2+1)}}) == nil {
		t.Fatal("aggregate input bound missing")
	}
}
