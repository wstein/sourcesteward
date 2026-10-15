// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"encoding/json"
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
