// SPDX-License-Identifier: EUPL-1.2

package owner

import (
	"strings"
	"testing"
)

func TestResolveOwner(t *testing.T) {
	cases := []struct {
		name   string
		item   Item
		caller string
		agent  bool
		want   Owner
	}{
		{"task record own", Item{TaskOwner: "claude", Path: "/tmp/whr1"}, "claude", false, Owner{"claude", SourceTaskRecord, ScopeOwn}},
		{"task record own for agent", Item{TaskOwner: "claude"}, "claude", true, Owner{"claude", SourceTaskRecord, ScopeOwn}},
		{"task record other", Item{TaskOwner: "codex"}, "claude", false, Owner{"codex", SourceTaskRecord, ScopeOther}},
		{"task record wins over path", Item{TaskOwner: "claude", Path: "/tmp/whr010-a", Branch: "codex/x"}, "werner", false, Owner{"claude", SourceTaskRecord, ScopeOther}},
		{"whr path is codex", Item{Path: "/tmp/whr010-a"}, "claude", false, Owner{"codex", SourcePathConvention, ScopeOther}},
		{"codex branch is codex", Item{Branch: "codex/old-1"}, "claude", false, Owner{"codex", SourcePathConvention, ScopeOther}},
		{"claude slash branch", Item{Branch: "claude/x"}, "werner", false, Owner{"claude", SourcePathConvention, ScopeOther}},
		{"claude dash branch", Item{Branch: "claude-x"}, "werner", false, Owner{"claude", SourcePathConvention, ScopeOther}},
		{"path guess own for human", Item{Branch: "claude/x"}, "claude", false, Owner{"claude", SourcePathConvention, ScopeOwn}},
		{"path guess never own for agent", Item{Branch: "claude/x"}, "claude", true, Owner{"claude", SourcePathConvention, ScopeUnknown}},
		{"unclean path is cleaned", Item{Path: "/tmp//whr1"}, "claude", false, Owner{"codex", SourcePathConvention, ScopeOther}},
		{"dot path is cleaned", Item{Path: "/tmp/./whr1"}, "claude", false, Owner{"codex", SourcePathConvention, ScopeOther}},
		{"path matches only at a boundary", Item{Path: "/home/u/tmp/whr1"}, "claude", false, Owner{"", SourceUnknown, ScopeUnknown}},
		{"author hint never own", Item{AuthorHint: "claude"}, "claude", false, Owner{"author:claude", SourceCommitAuthor, ScopeUnknown}},
		{"author hint never own for agent", Item{AuthorHint: "claude"}, "claude", true, Owner{"author:claude", SourceCommitAuthor, ScopeUnknown}},
		{"path beats author", Item{Branch: "codex/x", AuthorHint: "werner"}, "werner", false, Owner{"codex", SourcePathConvention, ScopeOther}},
		{"unknown", Item{Path: "/s/wt/x", Branch: "feature"}, "claude", false, Owner{"", SourceUnknown, ScopeUnknown}},
		{"no caller never own", Item{TaskOwner: "claude"}, "", false, Owner{"claude", SourceTaskRecord, ScopeOther}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Resolve(c.item, c.caller, c.agent); got != c.want {
				t.Fatalf("Resolve(%+v, %q, agent=%v) = %+v, want %+v", c.item, c.caller, c.agent, got, c.want)
			}
		})
	}
}

func TestValidLabelIsOnePlainIdentifier(t *testing.T) {
	for label, want := range map[string]bool{
		"codex": true, "claude-2": true, "a.b_c": true, "X": true,
		"": false, "-a": false, ".a": false, "author:Werner Stein": false, "a b": false, "a/b": false, "ä": false,
		strings.Repeat("a", 128): true, strings.Repeat("a", 129): false,
	} {
		if got := ValidLabel(label); got != want {
			t.Errorf("ValidLabel(%q) = %t, want %t", label, got, want)
		}
	}
}

// The rule of the legacy completion: one plain label and a known scope. A commit author hint and
// an unknown owner are not declarable (Spec 044 D2, owner decision of 2026-10-28).
func TestLegacyDeclarable(t *testing.T) {
	for name, tc := range map[string]struct {
		o    Owner
		want bool
	}{
		"path convention, other":                {Owner{"codex", SourcePathConvention, ScopeOther}, true},
		"own label":                             {Owner{"claude", SourceTaskRecord, ScopeOwn}, true},
		"label with unknown scope (agent mode)": {Owner{"codex", SourcePathConvention, ScopeUnknown}, false},
		"commit author hint":                    {Owner{"author:Werner Stein", SourceCommitAuthor, ScopeUnknown}, false},
		"unknown":                               {Owner{"", SourceUnknown, ScopeUnknown}, false},
		"hint-like label with other scope":      {Owner{"author:x", SourceCommitAuthor, ScopeOther}, false},
	} {
		if got := LegacyDeclarable(tc.o); got != tc.want {
			t.Errorf("%s: %t, want %t", name, got, tc.want)
		}
	}
}
