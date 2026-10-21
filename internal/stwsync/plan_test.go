// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// classificationFixture creates one branch per result class.
func classificationFixture(t *testing.T) *fixture {
	f := newFixture(t)
	for _, b := range []string{"behind", "ahead", "diverged", "gone", "dirty", "untracked-files", "equal"} {
		f.branchOnRemote(t, b)
	}
	runGit(t, f.repo, "branch", "local-only", "main") // no upstream
	f.advanceLocal(t, "ahead")
	f.advanceLocal(t, "diverged")
	f.advanceRemote(t, "main")
	f.advanceRemote(t, "behind")
	f.advanceRemote(t, "diverged")
	f.advanceRemote(t, "dirty")
	f.advanceRemote(t, "untracked-files")
	runGit(t, f.other, "push", "-q", "origin", "--delete", "gone")
	runGit(t, f.other, "tag", "remote-tag", "main")
	runGit(t, f.other, "push", "-q", "origin", "remote-tag")
	return f
}

func TestPlanClassifiesEveryResult(t *testing.T) {
	f := classificationFixture(t)
	dirty := f.worktree(t, "dirty")
	write(t, filepath.Join(dirty, "a.txt"), "edited\n")
	untracked := f.worktree(t, "untracked-files")
	write(t, filepath.Join(untracked, "new.txt"), "x\n")
	oldMain, oldBehind := f.oid(t, "main"), f.oid(t, "behind")

	r := Plan(context.Background(), Options{Path: f.repo, Prune: true})
	if r.Error != nil {
		t.Fatal(r.Error)
	}
	if r.Command != "sync" || r.Mode != "plan" || r.RunID != nil || r.Remote != "origin" || !r.Fetch.Performed || r.SchemaVersion != 1 {
		t.Fatalf("header: %+v", r)
	}
	got := itemsByBranch(t, r)
	want := map[string]string{
		"main": "fast-forward", "behind": "fast-forward", "equal": "up-to-date", "ahead": "ahead",
		"diverged": "diverged", "gone": "upstream-gone", "dirty": "skipped-dirty", "untracked-files": "fast-forward",
	}
	for branch, result := range want {
		if got[branch].Result != result {
			t.Errorf("%s: %q, want %q (%+v)", branch, got[branch].Result, result, got[branch])
		}
	}
	if _, ok := got["local-only"]; ok || len(got) != len(want) || r.Summary.Untracked != 1 {
		t.Fatalf("untracked branch must be counted, not listed: %d items, untracked %d", len(got), r.Summary.Untracked)
	}
	if it := got["main"]; it.OldOID == nil || *it.OldOID != oldMain || it.NewOID == nil || *it.NewOID != f.oid(t, "origin/main") || it.Worktree == nil || *it.Worktree != f.repo {
		t.Fatalf("main item: %+v", it)
	}
	if it := got["behind"]; it.Worktree != nil || *it.OldOID != oldBehind || *it.NewOID != f.oid(t, "origin/behind") {
		t.Fatalf("non-checked-out item: %+v", it)
	}
	if it := got["dirty"]; it.Reason == nil || it.Worktree == nil || *it.Worktree != dirty {
		t.Fatalf("dirty item: %+v", it)
	}
	if got["gone"].NewOID != nil || got["ahead"].NewOID != nil || got["diverged"].NewOID != nil || got["equal"].NewOID != nil {
		t.Fatal("only a would-be move carries a new OID")
	}
	if !sort.SliceIsSorted(r.Items, func(i, j int) bool { return r.Items[i].Branch < r.Items[j].Branch }) {
		t.Fatal("items must be sorted by branch")
	}
	if r.Summary.Total != len(want) || r.Summary.ByResult["fast-forward"] != 3 || r.Summary.ByResult["skipped-dirty"] != 1 {
		t.Fatalf("summary: %+v", r.Summary)
	}
	if r.Items == nil || r.Submodules == nil || r.Fetch.PrunedRefs == nil {
		t.Fatal("arrays must be empty arrays, never nil")
	}
}

func TestExitCodeTable(t *testing.T) {
	for result, code := range map[string]int{
		"fast-forward": 0, "up-to-date": 0, "ahead": 0, "upstream-gone": 0,
		"skipped-dirty": 1, "diverged": 1,
	} {
		if got := (Result{Items: []Item{{Result: "up-to-date"}, {Result: result}}}).ExitCode(); got != code {
			t.Errorf("%s: exit %d, want %d", result, got, code)
		}
	}
	if (Result{Error: &Failure{Code: "x"}}).ExitCode() != 1 {
		t.Fatal("a failed run exits 1")
	}
}

// snapshot captures everything a plan must leave alone: local refs, per-worktree HEAD and files, the stash.
func snapshot(t *testing.T, f *fixture, trees ...string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString(runGit(t, f.repo, "for-each-ref", "--format=%(refname) %(objectname)", "refs/heads", "refs/tags", "refs/stash"))
	b.WriteString("\n" + runGit(t, f.repo, "stash", "list"))
	for _, tree := range append([]string{f.repo}, trees...) {
		b.WriteString("\n" + tree + " " + runGit(t, tree, "rev-parse", "HEAD") + "\n" + runGit(t, tree, "status", "--porcelain=v1", "--untracked-files=all"))
		var files []string
		filepath.WalkDir(tree, func(p string, d os.DirEntry, err error) error {
			if err == nil && d.IsDir() && d.Name() == ".git" {
				return filepath.SkipDir
			}
			if err == nil && !d.IsDir() {
				raw, _ := os.ReadFile(p)
				files = append(files, p+"="+string(raw))
			}
			return nil
		})
		sort.Strings(files)
		b.WriteString("\n" + strings.Join(files, "\n"))
	}
	return b.String()
}

func TestPlanChangesOnlyRemoteTrackingRefs(t *testing.T) {
	f := classificationFixture(t)
	dirty := f.worktree(t, "dirty")
	write(t, filepath.Join(dirty, "a.txt"), "edited\n")
	runGit(t, dirty, "add", "a.txt")
	write(t, filepath.Join(dirty, "untracked.txt"), "u\n")
	clean := f.worktree(t, "behind")
	trackingBefore := runGit(t, f.repo, "for-each-ref", "--format=%(refname) %(objectname)", "refs/remotes")
	before := snapshot(t, f, dirty, clean)

	r := Plan(context.Background(), Options{Path: f.repo, Prune: true})
	if r.Error != nil {
		t.Fatal(r.Error)
	}
	if after := snapshot(t, f, dirty, clean); after != before {
		t.Fatalf("plan changed local state:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	trackingAfter := runGit(t, f.repo, "for-each-ref", "--format=%(refname) %(objectname)", "refs/remotes")
	if trackingAfter == trackingBefore {
		t.Fatal("the fetch must have updated the remote-tracking refs, otherwise the plan classified stale refs")
	}
	if _, err := os.Stat(filepath.Join(f.repo, ".git", "FETCH_HEAD")); err == nil {
		t.Fatal("plan must not write FETCH_HEAD")
	}
	if runGit(t, f.repo, "status", "--porcelain=v1", "--untracked-files=no") != "" {
		t.Fatal("primary worktree must stay clean")
	}
}

func TestPlanPruneReportsRemovedRefsOnlyWithPrune(t *testing.T) {
	f := classificationFixture(t)
	r := Plan(context.Background(), Options{Path: f.repo})
	if r.Error != nil || len(r.Fetch.PrunedRefs) != 0 {
		t.Fatalf("no prune: %+v", r)
	}
	if runGit(t, f.repo, "for-each-ref", "refs/remotes/origin/gone") == "" {
		t.Fatal("without --prune no remote-tracking ref may be removed")
	}
	if it := itemsByBranch(t, r)["gone"]; it.Result == "upstream-gone" {
		t.Fatal("a branch whose tracking ref still exists is not gone before a prune")
	}
	r = Plan(context.Background(), Options{Path: f.repo, Prune: true})
	if r.Error != nil || len(r.Fetch.PrunedRefs) != 1 || r.Fetch.PrunedRefs[0] != "refs/remotes/origin/gone" {
		t.Fatalf("prune: %+v", r.Fetch)
	}
	if runGit(t, f.repo, "for-each-ref", "refs/remotes/origin/gone") != "" {
		t.Fatal("prune must remove the stale ref")
	}
	if runGit(t, f.repo, "rev-parse", "--verify", "refs/heads/gone") == "" {
		t.Fatal("local branches are never deleted")
	}
}

func TestPlanRefusalsHappenBeforeAnyEffect(t *testing.T) {
	f := classificationFixture(t)
	before := runGit(t, f.repo, "for-each-ref", "--format=%(refname) %(objectname)", "refs/remotes")

	jj := filepath.Join(f.root, "jjrepo")
	runGit(t, f.root, "init", "-q", "-b", "main", jj)
	if err := os.Mkdir(filepath.Join(jj, ".jj"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		o    Options
		code string
	}{
		"jj":           {Options{Path: jj}, "sync-unsupported-backend"},
		"not a repo":   {Options{Path: f.root}, "sync-unsupported-backend"},
		"missing path": {Options{Path: filepath.Join(f.root, "none")}, "sync-unsupported-backend"},
		"no remote":    {Options{Path: f.repo, Remote: "nowhere"}, "sync-remote-unknown"},
	} {
		r := Plan(context.Background(), tc.o)
		if r.Error == nil || r.Error.Code != tc.code || r.Fetch.Performed || len(r.Items) != 0 || r.ExitCode() != 1 {
			t.Errorf("%s: %+v", name, r)
		}
		if name == "jj" && (r.Error == nil || !strings.Contains(r.Error.Message, "jj-owned")) {
			t.Errorf("jj refusal must say so: %+v", r.Error)
		}
	}
	if after := runGit(t, f.repo, "for-each-ref", "--format=%(refname) %(objectname)", "refs/remotes"); after != before {
		t.Fatal("a refused run must not fetch")
	}
}

func TestValidateOptions(t *testing.T) {
	for _, remote := range []string{"", "origin", "up.stream-2"} {
		if err := ValidateOptions(Options{Path: ".", Remote: remote}); err != nil {
			t.Errorf("%q: %v", remote, err)
		}
	}
	for _, o := range []Options{
		{Path: ".", Remote: "-x"}, {Path: ".", Remote: "a b"}, {Path: ".", Remote: "a\nb"},
		{Path: ".", Remote: "refs/x"}, {Path: ".", Remote: strings.Repeat("a", 129)},
		{Path: "a\x00b"}, {Path: "\xff"}, {Path: strings.Repeat("a", 4097)},
	} {
		if ValidateOptions(o) == nil {
			t.Errorf("%+v must be refused", o)
		}
	}
}

func TestPlanRefusesMoreThanTheBranchLimit(t *testing.T) {
	f := newFixture(t)
	var b strings.Builder
	for i := 0; i < 1100; i++ {
		b.WriteString("create refs/heads/b" + strings.Repeat("0", 4-len(itoa(i))) + itoa(i) + " HEAD\n")
	}
	cmd := gitCmd(f.repo, "update-ref", "--stdin")
	cmd.Stdin = strings.NewReader(b.String())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	r := Plan(context.Background(), Options{Path: f.repo})
	if r.Error == nil || r.Error.Code != "sync-limit" || len(r.Items) != 0 {
		t.Fatalf("%+v", r)
	}
}

func TestPlanIgnoresFetchPruneConfigWithoutPruneFlag(t *testing.T) {
	f := classificationFixture(t)
	runGit(t, f.repo, "config", "fetch.prune", "true")
	r := Plan(context.Background(), Options{Path: f.repo})
	if r.Error != nil || len(r.Fetch.PrunedRefs) != 0 {
		t.Fatalf("%+v", r)
	}
	if runGit(t, f.repo, "for-each-ref", "refs/remotes/origin/gone") == "" {
		t.Fatal("fetch.prune must not remove refs without --prune")
	}
}

func TestPlanRefusesRefspecsThatWriteLocalRefs(t *testing.T) {
	for _, spec := range []string{"+refs/heads/behind:refs/heads/behind", "refs/tags/*:refs/tags/*"} {
		f := classificationFixture(t)
		runGit(t, f.repo, "config", "--add", "remote.origin.fetch", spec)
		before := runGit(t, f.repo, "for-each-ref", "--format=%(refname) %(objectname)")
		r := Plan(context.Background(), Options{Path: f.repo})
		if r.Error == nil || r.Error.Code != "sync-refspec-unsafe" || r.Fetch.Performed {
			t.Fatalf("%s: %+v", spec, r)
		}
		if after := runGit(t, f.repo, "for-each-ref", "--format=%(refname) %(objectname)"); after != before {
			t.Fatalf("%s: refs changed", spec)
		}
	}
}

func TestPlanIgnoresInheritedRepositoryVariables(t *testing.T) {
	f := classificationFixture(t)
	want := Plan(context.Background(), Options{Path: f.repo})
	decoy := filepath.Join(f.root, "decoy")
	runGit(t, f.root, "init", "-q", "-b", "main", decoy)
	t.Setenv("GIT_DIR", filepath.Join(decoy, ".git"))
	got := Plan(context.Background(), Options{Path: f.repo})
	if got.Error != nil || len(got.Items) != len(want.Items) || len(got.Items) == 0 {
		t.Fatalf("%+v", got)
	}
}
