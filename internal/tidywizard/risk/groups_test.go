// SPDX-License-Identifier: EUPL-1.2
package risk

import (
	"fmt"
	"reflect"
	"regexp"
	"testing"
)

func riskRow(id, repo, path string, level Level) Row {
	return Row{ID: id, Kind: "worktree", Repo: repo, Path: path, Effects: []string{"remove-registration", "remove-files"}, Risk: Assessment{Level: level, Reasons: []string{"proof-ancestor"}}}
}

func TestFamily(t *testing.T) {
	for _, tc := range []struct{ path, want string }{
		{"/tmp/workharbor-baseline-x/checkout", "workharbor"}, {"/tmp/whr007-ci-binding", "whr"}, {"/tmp/whr-006-t030-apply", "whr"},
		{"/tmp/crewbook-child/UPPER", "crewbook"}, {"/tmp/whr\x1b[2J-evil", "whr"}, {"/tmp/whr\r\x00\u202e", "whr"},
		{"/tmp/WHR-baseline", ""}, {"/tmp/7whr", ""}, {"/tmp/évil", ""}, {"/tmp/", ""}, {"/tmp", ""},
		{"/temporary/whr", ""}, {"/tmpish/whr", ""}, {"relative/tmp/whr", ""}, {"/tmp/-whr", ""},
	} {
		t.Run(fmt.Sprintf("%q", tc.path), func(t *testing.T) {
			if got := Family(tc.path); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestGroupPlacesFamiliesAndKeys(t *testing.T) {
	rows := []Row{
		riskRow("w1", "a", "/tmp/workharbor-a/checkout", Green), riskRow("w2", "b", "/tmp/workharbor-b", Green), riskRow("w3", "a", "/tmp/workharbor\x1b[2J-c", Green),
		riskRow("c1", "a", "/tmp/crewbook-a", Green), riskRow("c2", "a", "/tmp/crewbook-b", Green),
		riskRow("s1", "a", "/tmp/claude/scratchpad/task", Green), riskRow("s2", "a", "/ws/scratchpad/task", Green),
		riskRow("p1", "a", "/workspace/first", Green), riskRow("p2", "b", "/workspace/second", Green),
		riskRow("n1", "a", "/workspace/scratchpads/child", Green),
	}
	groups := GroupRows(rows)
	byPlace := map[string][]Group{}
	keys := map[string]bool{}
	for _, g := range groups {
		byPlace[g.Place] = append(byPlace[g.Place], g)
		if !regexp.MustCompile(`^[a-z]+$`).MatchString(g.Key) || keys[g.Key] {
			t.Fatalf("unsafe or duplicate group key %q", g.Key)
		}
		keys[g.Key] = true
	}
	if len(groups) != 5 || len(byPlace["tmp-family"]) != 1 || byPlace["tmp-family"][0].Family != "workharbor" || len(byPlace["tmp-family"][0].Rows) != 3 {
		t.Fatalf("families: %+v", groups)
	}
	if len(byPlace["tmp-other"]) != 1 || len(byPlace["tmp-other"][0].Rows) != 2 || len(byPlace["scratchpads"]) != 1 || len(byPlace["scratchpads"][0].Rows) != 2 {
		t.Fatalf("tmp/scratchpads: %+v", groups)
	}
	if len(byPlace["workspace"]) != 2 {
		t.Fatalf("parent grouping: %+v", groups)
	}
	reversed := append([]Row{}, rows...)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	if got := GroupRows(reversed); !reflect.DeepEqual(got, groups) {
		t.Fatal("group/key ordering depends on input order")
	}
	rows[0].Effects[0] = "mutated"
	rows[0].Risk.Reasons[0] = "mutated"
	found := false
	for _, g := range groups {
		for _, r := range g.Rows {
			if r.ID == "w1" {
				found = true
				if r.Effects[0] != "remove-registration" || r.Risk.Reasons[0] != "proof-ancestor" {
					t.Fatal("group aliases caller effects or reasons")
				}
			}
		}
	}
	if !found {
		t.Fatal("missing w1 row")
	}

}

func TestFamilyThresholdCountsOnlyDistinctChoosableDirectories(t *testing.T) {
	for _, n := range []int{2, 3} {
		rows := []Row{}
		for i := 0; i < n; i++ {
			rows = append(rows, riskRow(fmt.Sprint(i), "a", fmt.Sprintf("/tmp/whr-%d", i), Green))
		}
		rows = append(rows, riskRow("red", "a", "/tmp/whr-red", Red), riskRow("kept", "a", "/tmp/whr-kept", Kept))
		groups := GroupRows(rows)
		if len(groups) != 1 || len(groups[0].Rows) != n {
			t.Fatalf("non-choosable rows grouped: %+v", groups)
		}
		want := "tmp-other"
		if n == 3 {
			want = "tmp-family"
		}
		if groups[0].Place != want {
			t.Fatalf("threshold %d: %+v", n, groups)
		}
	}
	rows := []Row{riskRow("a", "a", "/tmp/whr-same", Green), riskRow("b", "a", "/tmp/whr-same", Green), riskRow("c", "a", "/tmp/whr-same", Green)}
	if g := GroupRows(rows); len(g) != 1 || g[0].Place != "tmp-other" {
		t.Fatalf("duplicate paths invented a family: %+v", g)
	}
}

func TestGroupsSeparateKindsEffectsAndRepositories(t *testing.T) {
	branch := func(id, repo string) Row {
		return Row{ID: id, Kind: "branch", Repo: repo, Effects: []string{"delete-branch:" + id}, Risk: Assessment{Level: Green}}
	}
	rows := []Row{branch("one", "a"), branch("two", "a"), branch("three", "b"), riskRow("w", "a", "/tmp/x", Green), {ID: "tracking", Kind: "tracking-ref", Repo: "a", Effects: []string{"delete-tracking-ref:x"}, Risk: Assessment{Level: Green}}}
	extra := riskRow("wb", "a", "/tmp/y", Green)
	extra.Effects = append(extra.Effects, "delete-branch:dependent")
	rows = append(rows, extra)
	groups := GroupRows(rows)
	if len(groups) != 5 {
		t.Fatalf("kind/effect/repository grouping: %+v", groups)
	}
	found := false
	for _, g := range groups {
		if g.Kind == "branch" && g.Repo == "a" {
			found = true
			if len(g.Rows) != 2 {
				t.Fatalf("branch refs split same effect: %+v", g)
			}
		}
	}
	if !found {
		t.Fatal("missing branch group")
	}
}
