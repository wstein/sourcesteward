// SPDX-License-Identifier: EUPL-1.2

package owner

import "testing"

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
