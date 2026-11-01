// SPDX-License-Identifier: EUPL-1.2

package land

import "testing"

func TestShapeKeyIsNormalizedAndStackSensitive(t *testing.T) {
	repo := RepoFacts{DefaultBranch: "main", DeleteBranchOnMerge: boolp(true)}
	a := ShapeOf(repo, RulesetFacts{Readable: true, RuleTypes: []string{"b", "a", "a"}, BypassModes: []string{"always"}, Protection: "none"}, true)
	b := ShapeOf(repo, RulesetFacts{Readable: true, RuleTypes: []string{"a", "b"}, BypassModes: []string{"always"}, Protection: "none"}, true)
	if a.Key() != b.Key() {
		t.Fatalf("%q != %q", a.Key(), b.Key())
	}
	if a.Key() == ShapeOf(repo, RulesetFacts{Readable: true, RuleTypes: []string{"a", "b"}, BypassModes: []string{"pull_request"}, Protection: "none"}, true).Key() {
		t.Fatal("bypass mode must be part of the key")
	}
	if a.Key() == ShapeOf(repo, RulesetFacts{Readable: true, RuleTypes: []string{"a", "b"}, BypassModes: []string{"always"}, Protection: "none"}, false).Key() {
		t.Fatal("stack must be part of the key")
	}
	off := RepoFacts{DefaultBranch: "main", DeleteBranchOnMerge: boolp(false)}
	if a.Key() == ShapeOf(off, RulesetFacts{Readable: true, RuleTypes: []string{"a", "b"}, BypassModes: []string{"always"}, Protection: "none"}, true).Key() {
		t.Fatal("delete_branch_on_merge must be part of the key")
	}
}

func TestUnreadableShapeNeverMatches(t *testing.T) {
	repo := RepoFacts{DefaultBranch: "main", DeleteBranchOnMerge: boolp(true)}
	s := ShapeOf(repo, RulesetFacts{Protection: "unreadable"}, true)
	if s.Readable {
		t.Fatal("unreadable shape reported readable")
	}
	set := NewShapeSet(s.Key())
	if set.Has(s) {
		t.Fatal("an unreadable shape must never match, even when its key was recorded")
	}
	un := ShapeOf(RepoFacts{DefaultBranch: "main"}, RulesetFacts{Readable: true, Protection: "none"}, true)
	if un.Readable {
		t.Fatal("delete_branch_on_merge unreadable must make the shape unreadable")
	}
}

func TestEmbeddedAllowlistIsWellFormedAndEmptyUntilMeasured(t *testing.T) {
	set, err := LoadShapes(embeddedShapes)
	if err != nil {
		t.Fatal(err)
	}
	if n := set.Len(); n != 0 {
		t.Fatalf("the allowlist holds %d shapes; entries need the measured evidence of research.md (T021)", n)
	}
	for _, bad := range []string{`{`, `{"schema_version":2,"shapes":[]}`, `{"schema_version":1,"shapes":[{"key":"","stack":true,"evidence":"x"}]}`, `{"schema_version":1,"shapes":[{"key":"k","stack":true}]}`} {
		if _, err := LoadShapes([]byte(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}
