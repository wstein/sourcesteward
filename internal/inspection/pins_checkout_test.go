// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// validatedMember validates the fixture's submodule through the production safety path and returns the runner
// and the explicit repository the evidence functions take.
func validatedMember(t *testing.T, f pinsFixture) (*pinsRunner, pinsGitRepo, pinsSubmoduleDirs) {
	t.Helper()
	s := safetyFor(t, f.Super)
	d := mustResolve(t, s, pinsSubmoduleSpec{Path: "member", Name: "member"})
	return s.run, pinsGitRepo{GitDir: d.GitDir, WorkTree: d.WorkTree}, d
}

func commitIn(t *testing.T, dir, name string) string {
	t.Helper()
	writeFile(t, filepath.Join(dir, name), name+"\n")
	git(t, dir, "add", name)
	git(t, dir, "commit", "-qm", name)
	return gitIn(t, dir, "rev-parse", "HEAD")
}

func relationOf(t *testing.T, run *pinsRunner, repo pinsGitRepo, d pinsSubmoduleDirs, pin string) (pinsHead, pinsRelation) {
	t.Helper()
	facts, err := pinsReadFacts(run, d)
	if err != nil {
		t.Fatal(err)
	}
	head, err := pinsReadHead(run, repo)
	if err != nil {
		t.Fatal(err)
	}
	var rel pinsRelation
	if head.Commit != nil {
		rel, err = pinsRelate(run, repo, facts, *head.Commit, pin)
		if err != nil {
			t.Fatal(err)
		}
	}
	return head, rel
}

func TestPinsCheckoutHeadBranchAndDetached(t *testing.T) {
	f := newPinsFixture(t)
	run, repo, _ := validatedMember(t, f)
	h, err := pinsReadHead(run, repo)
	if err != nil || h.Commit == nil || *h.Commit != f.Pin || h.Branch == nil || *h.Branch != "main" || h.Detached || h.Unborn {
		t.Fatalf("%+v %v", h, err)
	}
	git(t, repo.WorkTree, "checkout", "-q", "--detach")
	h, err = pinsReadHead(run, repo)
	if err != nil || h.Commit == nil || *h.Commit != f.Pin || h.Branch != nil || !h.Detached || h.Unborn {
		t.Fatalf("%+v %v", h, err)
	}
}

func TestPinsCheckoutUnbornHead(t *testing.T) {
	f := newPinsFixture(t)
	run, repo, _ := validatedMember(t, f)
	git(t, repo.WorkTree, "checkout", "-q", "--orphan", "fresh")
	h, err := pinsReadHead(run, repo)
	if err != nil || h.Commit != nil || h.Branch == nil || *h.Branch != "fresh" || h.Detached || !h.Unborn {
		t.Fatalf("%+v %v", h, err)
	}
}

func TestPinsCheckoutUnbornNeedsAMissingRefNotACorruptOne(t *testing.T) {
	f := newPinsFixture(t)
	run, repo, _ := validatedMember(t, f)
	// A branch ref that exists but cannot be resolved is corruption, never an unborn HEAD.
	ref := filepath.Join(repo.GitDir, "refs", "heads", "main")
	mustOS(t, os.WriteFile(ref, []byte("not-an-object-name\n"), 0o600))
	if _, err := pinsReadHead(run, repo); err == nil {
		t.Fatal("a corrupt branch ref was reported as a head")
	}
}

func TestPinsCheckoutRelationToPin(t *testing.T) {
	f := newPinsFixture(t)
	run, repo, d := validatedMember(t, f)
	wt := repo.WorkTree
	head, rel := relationOf(t, run, repo, d, f.Pin)
	if rel.Relation != "same" || rel.Ahead == nil || *rel.Ahead != 0 || *rel.Behind != 0 || rel.Capped || head.Commit == nil {
		t.Fatalf("same: %+v", rel)
	}
	c2 := commitIn(t, wt, "two")
	c3 := commitIn(t, wt, "three")
	_, rel = relationOf(t, run, repo, d, f.Pin)
	if rel.Relation != "ahead" || *rel.Ahead != 2 || *rel.Behind != 0 {
		t.Fatalf("ahead: %+v", rel)
	}
	_, rel = relationOf(t, run, repo, d, c3)
	if rel.Relation != "same" {
		t.Fatalf("same again: %+v", rel)
	}
	git(t, wt, "checkout", "-q", "--detach", f.Pin)
	_, rel = relationOf(t, run, repo, d, c3)
	if rel.Relation != "behind" || *rel.Ahead != 0 || *rel.Behind != 2 {
		t.Fatalf("behind: %+v", rel)
	}
	git(t, wt, "checkout", "-q", "-b", "side", f.Pin)
	commitIn(t, wt, "side")
	_, rel = relationOf(t, run, repo, d, c2)
	if rel.Relation != "diverged" || *rel.Ahead != 1 || *rel.Behind != 1 {
		t.Fatalf("diverged: %+v", rel)
	}
	git(t, wt, "checkout", "-q", "--orphan", "alien")
	commitIn(t, wt, "alien")
	_, rel = relationOf(t, run, repo, d, f.Pin)
	if rel.Relation != "unrelated" || rel.Ahead != nil || rel.Behind != nil {
		t.Fatalf("unrelated: %+v", rel)
	}
}

func TestPinsCheckoutMissingPinObjectIsUnknownNeverAFailure(t *testing.T) {
	f := newPinsFixture(t)
	run, repo, d := validatedMember(t, f)
	_, rel := relationOf(t, run, repo, d, strings.Repeat("ab", 20))
	if rel.Relation != "unknown" || rel.Reason == "" || rel.Ahead != nil || rel.Behind != nil {
		t.Fatalf("%+v", rel)
	}
}

// fastImport builds a linear history of n commits on branch, starting from `from`, with one stream. The
// commit dates are later than the fixture commits: Git walks by date, so skewed dates would make a count wrong.
func fastImport(t *testing.T, dir, branch, from string, n int) {
	t.Helper()
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "commit refs/heads/%s\ncommitter T <t@e.invalid> %d +0000\ndata %d\n%s\n", branch, 4000000000+i, len(branch), branch)
		if i == 0 && from != "" {
			fmt.Fprintf(&b, "from %s\n", from)
		}
		b.WriteString("\n")
	}
	gitStdin(t, dir, b.String(), "fast-import", "--quiet")
}

func TestPinsCheckoutEachSideIsCountedAndCappedSeparately(t *testing.T) {
	f := newPinsFixture(t)
	run, repo, d := validatedMember(t, f)
	wt := repo.WorkTree
	fastImport(t, wt, "long", f.Pin, 10005)
	fastImport(t, wt, "short", f.Pin, 3)
	git(t, wt, "checkout", "-q", "long")
	short := gitIn(t, wt, "rev-parse", "short")
	_, rel := relationOf(t, run, repo, d, short)
	if rel.Relation != "diverged" || *rel.Ahead != 10000 || *rel.Behind != 3 || !rel.Capped {
		t.Fatalf("%+v", rel)
	}
	_, rel = relationOf(t, run, repo, d, f.Pin)
	if rel.Relation != "ahead" || *rel.Ahead != 10000 || *rel.Behind != 0 || !rel.Capped {
		t.Fatalf("%+v", rel)
	}
	// Exactly 10,000 is not capped.
	fastImport(t, wt, "exact", f.Pin, 10000)
	git(t, wt, "checkout", "-q", "exact")
	_, rel = relationOf(t, run, repo, d, f.Pin)
	if *rel.Ahead != 10000 || rel.Capped {
		t.Fatalf("exact: %+v", rel)
	}
}

func TestPinsCheckoutReplacedObjectsAreNeverHonored(t *testing.T) {
	f := newPinsFixture(t)
	run, repo, d := validatedMember(t, f)
	wt := repo.WorkTree
	commitIn(t, wt, "two")
	c3 := commitIn(t, wt, "three")
	git(t, wt, "checkout", "-q", "--orphan", "alien")
	alien := commitIn(t, wt, "alien")
	git(t, wt, "checkout", "-q", "main")
	// With the replacement honored c3 would read as the unrelated alien commit.
	git(t, wt, "replace", c3, alien)
	_, rel := relationOf(t, run, repo, d, f.Pin)
	if rel.Relation != "ahead" || *rel.Ahead != 2 {
		t.Fatalf("a replace ref changed the answer: %+v", rel)
	}
}

func cloneIn(t *testing.T, src string, args ...string) (string, pinsGitRepo) {
	t.Helper()
	dst := filepath.Join(pinsRealDir(t), "clone")
	git(t, filepath.Dir(dst), append(append([]string{"clone", "-q"}, args...), "file://"+src, dst)...)
	return dst, pinsGitRepo{GitDir: filepath.Join(dst, ".git"), WorkTree: dst}
}

func TestPinsCheckoutShallowCannotClaimUnrelatedOrDiverged(t *testing.T) {
	f := newPinsFixture(t)
	for _, n := range []string{"two", "three"} {
		commitIn(t, f.Sub, n)
	}
	git(t, f.Sub, "checkout", "-q", "-b", "other", f.Pin)
	other := commitIn(t, f.Sub, "other")
	git(t, f.Sub, "checkout", "-q", "main")
	dst, repo := cloneIn(t, f.Sub, "--depth", "1", "--branch", "main")
	git(t, dst, "fetch", "-q", "--depth", "1", "origin", "other")
	run := newPinsRunner(context.Background())
	facts, err := pinsReadFacts(run, pinsSubmoduleDirs{GitDir: repo.GitDir, WorkTree: repo.WorkTree, gitBase: dst, gitRel: ".git"})
	if err != nil || !facts.Shallow {
		t.Fatalf("%+v %v", facts, err)
	}
	head, _ := pinsReadHead(run, repo)
	rel, err := pinsRelate(run, repo, facts, *head.Commit, other)
	if err != nil || rel.Relation != "unknown" || !strings.Contains(rel.Reason, "shallow") {
		t.Fatalf("%+v %v", rel, err)
	}
	// A definite answer stays definite: the checkout is the pin.
	rel, err = pinsRelate(run, repo, facts, *head.Commit, *head.Commit)
	if err != nil || rel.Relation != "same" {
		t.Fatalf("%+v %v", rel, err)
	}
}

func TestPinsCheckoutPartialCloneIsDetectedAndNeverFetches(t *testing.T) {
	f := newPinsFixture(t)
	git(t, f.Sub, "config", "uploadpack.allowFilter", "true")
	git(t, f.Sub, "config", "uploadpack.allowAnySHA1InWant", "true")
	c2 := commitIn(t, f.Sub, "two")
	dst, repo := cloneIn(t, f.Sub, "--filter=blob:none", "--no-checkout")
	run := newPinsRunner(context.Background())
	facts, err := pinsReadFacts(run, pinsSubmoduleDirs{GitDir: repo.GitDir, WorkTree: repo.WorkTree, gitBase: dst, gitRel: ".git"})
	if err != nil || !facts.Partial || facts.Shallow {
		t.Fatalf("%+v %v", facts, err)
	}
	head, _ := pinsReadHead(run, repo)
	rel, err := pinsRelate(run, repo, facts, *head.Commit, f.Pin)
	if err != nil || rel.Relation != "ahead" || *rel.Ahead != 1 {
		t.Fatalf("%+v %v (c2=%s)", rel, err, c2)
	}
}
