// SPDX-License-Identifier: EUPL-1.2
package conflicts

import (
	"strings"
	"testing"
)

func planFixture() []Node {
	conflict := func(n int, base, headRef, head, mb, tip string, files ...FileConflict) Node {
		return Node{Number: n, Repo: "o/a", Base: base, HeadRef: headRef, Head: head, Review: "clear", Checks: "green",
			Finding: Finding{Number: n, State: StateConflicts, Base: base, BaseSHA: tip, Head: head, MergeBase: mb, Files: files}}
	}
	clean := func(n int, base, headRef, head string) Node {
		return Node{Number: n, Repo: "o/a", Base: base, HeadRef: headRef, Head: head, Review: "clear", Checks: "green",
			Finding: Finding{Number: n, State: StateClean, Base: base, Head: head}}
	}
	return []Node{
		clean(5, "main", "feat-zero", sh("5")),
		conflict(6, "feat-zero", "feat-one", sh("6"), sh("e"), sh("f"), FileConflict{Path: "rules.md", BaseCommit: sh("b")}, FileConflict{Path: "docs/x.md"}),
		clean(7, "feat-one", "feat-two", sh("7")),
		clean(8, "feat-one", "feat-side", sh("8")),
		clean(9, "main", "elsewhere", sh("9")),
	}
}

func TestBuildPlansStep(t *testing.T) {
	plans := BuildPlans("o/a", "origin", planFixture())
	if len(plans) != 1 {
		t.Fatalf("one stack with a conflict, got %d plans", len(plans))
	}
	p := plans[0]
	if p.Repo != "o/a" || p.Remote != "origin" || p.Root != 5 {
		t.Fatalf("%+v", p)
	}
	var nums []int
	for _, s := range p.Steps {
		nums = append(nums, s.Number)
	}
	if len(nums) != 3 || nums[0] != 6 || nums[1] != 7 || nums[2] != 8 {
		t.Fatalf("steps %v: the lowest conflict and every descendant, in order; the clean parent #5 is untouched", nums)
	}
	low := p.Steps[0]
	wantLow := []string{
		"git fetch origin",
		"git switch feat-one",
		"git rebase --onto origin/feat-zero " + sh("e") + " feat-one",
	}
	if strings.Join(low.Rebase, "\n") != strings.Join(wantLow, "\n") {
		t.Fatalf("rebase of the lowest level:\n%s", strings.Join(low.Rebase, "\n"))
	}
	if low.Verify[0] != "git range-diff "+sh("e")+".."+sh("6")+" origin/feat-zero..feat-one" {
		t.Fatalf("range-diff: %v", low.Verify)
	}
	if low.Push != "git push --force-with-lease=feat-one:"+sh("6")+" origin feat-one" {
		t.Fatalf("push: %q", low.Push)
	}
	child := p.Steps[1]
	if child.Rebase[1] != "git switch feat-two" || child.Rebase[2] != "git rebase --onto feat-one "+sh("6")+" feat-two" {
		t.Fatalf("child rebase: %v", child.Rebase)
	}
	if child.Verify[0] != "git range-diff "+sh("6")+".."+sh("7")+" feat-one..feat-two" || child.Push != "git push --force-with-lease=feat-two:"+sh("7")+" origin feat-two" {
		t.Fatalf("child: %v %q", child.Verify, child.Push)
	}
	if child.Why != "child of #6, restacked after it" || low.Why != StateConflicts {
		t.Fatalf("why: %q %q", low.Why, child.Why)
	}
	for _, s := range p.Steps {
		for _, c := range append(append([]string{}, s.Rebase...), s.Verify...) {
			if strings.Contains(c, "--force") || strings.Contains(c, " push ") {
				t.Errorf("a command before the review writes to the remote: %s", c)
			}
		}
	}
	if len(p.GhStack) != 3 || p.GhStack[0] != "gh stack checkout 5" || p.GhStack[1] != "gh stack rebase" || p.GhStack[2] != "gh stack push" {
		t.Fatalf("gh stack: %v", p.GhStack)
	}
}

func TestBuildPlansNoConflictNoPlan(t *testing.T) {
	nodes := planFixture()
	nodes[1].Finding.State = StateClean
	if plans := BuildPlans("o/a", "origin", nodes); len(plans) != 0 {
		t.Fatalf("%+v", plans)
	}
}

func TestBuildPlansSingleAndFork(t *testing.T) {
	n := Node{Number: 1, Repo: "o/a", Base: "main", HeadRef: "x", Head: sh("1"), Finding: Finding{State: StateConflicts, Base: "main", BaseSHA: sh("f"), MergeBase: sh("e"), Head: sh("1"), Files: []FileConflict{{Path: "a.md"}}}}
	p := BuildPlans("o/a", "origin", []Node{n})[0]
	if len(p.Steps) != 1 || p.Steps[0].Rebase[2] != "git rebase --onto origin/main "+sh("e")+" x" || len(p.GhStack) != 0 {
		t.Fatalf("a single pull request is not a gh stack: %+v", p)
	}
	n.Fork = true
	f := BuildPlans("o/a", "origin", []Node{n})[0]
	if len(f.Steps) != 1 || f.Steps[0].Skip == "" || len(f.Steps[0].Rebase) != 0 || f.Steps[0].Push != "" {
		t.Fatalf("a fork head cannot be pushed from here: %+v", f.Steps[0])
	}
}

func TestBuildPlansTwoIndependentConflicts(t *testing.T) {
	a := Node{Number: 1, Repo: "o/a", Base: "main", HeadRef: "a", Head: sh("1"), Finding: Finding{State: StateConflicts, Base: "main", BaseSHA: sh("f"), MergeBase: sh("e"), Files: []FileConflict{{Path: "a"}}}}
	b := Node{Number: 2, Repo: "o/a", Base: "a", HeadRef: "b", Head: sh("2"), Finding: Finding{State: StateClean}}
	c := Node{Number: 3, Repo: "o/a", Base: "a", HeadRef: "c", Head: sh("3"), Finding: Finding{State: StateConflicts, Base: "a", BaseSHA: sh("c"), MergeBase: sh("d"), Files: []FileConflict{{Path: "c"}}}}
	p := BuildPlans("o/a", "origin", []Node{a, b, c})[0]
	if len(p.Steps) != 3 {
		t.Fatalf("a conflicting child of a conflicting parent is restacked once, behind its parent: %+v", p.Steps)
	}
	if p.Steps[2].Number != 3 || p.Steps[2].Rebase[2] != "git rebase --onto a "+sh("1")+" c" {
		t.Fatalf("%+v", p.Steps[2])
	}
}

func TestBriefHasEveryRequiredCheck(t *testing.T) {
	p := BuildPlans("o/a", "origin", planFixture())[0]
	b := p.Brief
	for _, want := range []string{
		"o/a", "#6", "`rules.md`", sh("b"), "`docs/x.md`", sh("e"), sh("f"),
		"git rebase --onto origin/feat-zero",
		"git range-diff",
		"--force-with-lease=feat-one:" + sh("6"),
		"conflict wording of the base unchanged",
		"back to Draft",
		"gh pr ready <number> --undo",
		"%an <%ae> | %cn <%ce>",
		"session link",
		"independent review",
		"rule, contract or security",
		"tests",
		"gh stack",
	} {
		if !strings.Contains(b, want) {
			t.Errorf("brief lacks %q", want)
		}
	}
	if strings.Contains(strings.ToLower(b), "claude.ai") || strings.Contains(strings.ToLower(b), "claude-session") {
		t.Error("the brief names a session link")
	}
	if strings.Contains(b, "- #7 (") || strings.Contains(b, "- #8 (") {
		t.Error("a clean child is listed as a conflict")
	}
	if !strings.HasPrefix(b, "# Restack brief") || !strings.HasSuffix(b, "\n") {
		t.Errorf("shape: %q", b[:20])
	}
}

func TestBriefQuotesUntrustedNames(t *testing.T) {
	n := planFixture()
	n[1].Finding.Files = []FileConflict{{Path: "evil`\n# injected"}}
	b := BuildPlans("o/a", "origin", n)[0].Brief
	if !strings.Contains(b, "`evil'\n") && !strings.Contains(b, "`evil'?# injected`") {
		t.Fatalf("backtick and newline are not replaced:\n%s", b)
	}
	if strings.Contains(b, "\n# injected") {
		t.Fatalf("a file name with a newline broke out of the list:\n%s", b)
	}
}

func TestBuildPlansSkipsUnsafeNames(t *testing.T) {
	good := Node{Number: 1, Repo: "o/a", Base: "main", HeadRef: "x", Head: sh("1"), Finding: Finding{State: StateConflicts, Base: "main", BaseSHA: sh("f"), MergeBase: sh("e"), Files: []FileConflict{{Path: "a"}}}}
	for name, mutate := range map[string]func(n *Node){
		"head ref":   func(n *Node) { n.HeadRef = "-x; rm" },
		"base":       func(n *Node) { n.Base = "a b" },
		"head sha":   func(n *Node) { n.Head = "nothex" },
		"merge base": func(n *Node) { n.Finding.MergeBase = "" },
	} {
		n := good
		mutate(&n)
		s := BuildPlans("o/a", "origin", []Node{n})[0].Steps[0]
		if s.Skip == "" || len(s.Rebase) != 0 || s.Push != "" {
			t.Errorf("%s: %+v", name, s)
		}
	}
	if s := BuildPlans("o/a", "bad remote", []Node{good})[0].Steps[0]; s.Skip == "" {
		t.Errorf("remote: %+v", s)
	}
}
