// SPDX-License-Identifier: EUPL-1.2
package restack

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func hazardsOf(r Result) []string {
	if len(r.Diagnostics) == 0 {
		return nil
	}
	list, _ := r.Diagnostics[0].Details["hazards"].([]string)
	return list
}

func TestRepositoryHazardsAreRefusedByName(t *testing.T) {
	cases := []struct {
		name   string
		hazard string
		setup  func(*scenario)
	}{
		{"hooksPath", "core.hooksPath", func(s *scenario) { s.git("config", "core.hooksPath", "/nonexistent") }},
		{"fsmonitor", "core.fsmonitor", func(s *scenario) { s.git("config", "core.fsmonitor", "true") }},
		{"attributesFile", "core.attributesFile", func(s *scenario) { s.git("config", "core.attributesFile", "/nonexistent") }},
		{"include", "include", func(s *scenario) { s.git("config", "include.path", "other.cfg") }},
		{"includeIf", "include", func(s *scenario) { s.git("config", "includeIf.gitdir:/x/.path", "other.cfg") }},
		{"extensions", "extensions", func(s *scenario) { s.git("config", "extensions.worktreeConfig", "true") }},
		{"merge driver", "merge-driver", func(s *scenario) { s.git("config", "merge.custom.driver", "true") }},
		{"filter driver", "filter-driver", func(s *scenario) { s.git("config", "filter.custom.clean", "cat") }},
		{"diff driver", "diff-driver", func(s *scenario) { s.git("config", "diff.custom.textconv", "cat") }},
		{"diff external", "diff-driver", func(s *scenario) { s.git("config", "diff.external", "true") }},
		{"replace ref", "replace-refs", func(s *scenario) { s.git("replace", s.childCommits[0], s.base) }},
		{"grafts", "grafts", func(s *scenario) {
			os.WriteFile(filepath.Join(s.dir, ".git", "info", "grafts"), nil, 0o644)
		}},
		{"shallow", "shallow", func(s *scenario) { os.WriteFile(filepath.Join(s.dir, ".git", "shallow"), []byte(s.base+"\n"), 0o644) }},
		{"alternates", "alternates", func(s *scenario) {
			os.MkdirAll(filepath.Join(s.dir, ".git", "objects", "info"), 0o755)
			os.WriteFile(filepath.Join(s.dir, ".git", "objects", "info", "alternates"), []byte(filepath.Join(s.dir, ".git", "objects")+"\n"), 0o644)
		}},
		{"info attributes", "info-attributes", func(s *scenario) {
			os.WriteFile(filepath.Join(s.dir, ".git", "info", "attributes"), []byte("*.txt text\n"), 0o644)
		}},
	}
	for _, tc := range cases {
		s := rebaseMerged(t)
		tc.setup(s)
		res := s.plan(t)
		if res.Status != StatusError || firstCode(res) != "repository-hazard" || !slices.Contains(hazardsOf(res), tc.hazard) {
			t.Errorf("%s: %s %+v", tc.name, res.Status, res.Diagnostics)
		}
	}
}

func TestAttributeHazardsOnTouchedPathsAreRefused(t *testing.T) {
	for _, attr := range []string{"merge=custom", "diff=custom", "filter=custom", "-merge", "merge=binary"} {
		for _, side := range []string{"child", "main"} {
			s := rebaseMerged(t)
			s.checkout(side)
			s.commit("attrs", map[string]string{".gitattributes": "child1.txt " + attr + "\n"})
			opts := s.options()
			if side == "child" {
				opts.Branch = "refs/heads/child"
			} else {
				opts.NewParent = s.oid("main")
			}
			res := Plan(context.Background(), opts)
			if side == "child" {
				// the attributes file itself is a moved path: its content is not pinned to a tree
				if firstCode(res) != "repository-hazard" || !slices.Contains(hazardsOf(res), "attributes-changed") {
					t.Errorf("%s child: %s %+v", attr, res.Status, res.Diagnostics)
				}
				continue
			}
			if firstCode(res) != "repository-hazard" || len(hazardsOf(res)) == 0 {
				t.Errorf("%s main: %s %+v", attr, res.Status, res.Diagnostics)
			}
		}
	}
}

func TestAttributesOnUntouchedPathsAreNotAHazard(t *testing.T) {
	s := rebaseMerged(t)
	s.checkout("main")
	s.commit("attrs", map[string]string{".gitattributes": "*.go diff=go\n"})
	opts := s.options()
	opts.NewParent = s.oid("main")
	res := Plan(context.Background(), opts)
	if res.Status != StatusOK {
		t.Fatalf("%s %+v", res.Status, res.Diagnostics)
	}
}

func TestLocalIdentityConfigIsReportedNotRefused(t *testing.T) {
	s := rebaseMerged(t)
	s.git("config", "user.name", "Leaked Placeholder")
	s.git("config", "user.email", "t@t")
	res := s.plan(t)
	if res.Status != StatusOK || !slices.Contains(res.Effects.Hazards, "local-identity-config") {
		t.Fatalf("%s %+v hazards %v", res.Status, res.Diagnostics, res.Effects.Hazards)
	}
	found := false
	for _, d := range res.Diagnostics {
		found = found || d.Code == "local-identity-config"
	}
	if !found {
		t.Fatalf("no diagnostic: %+v", res.Diagnostics)
	}
}

func TestDirtyTargetAndOperationsInProgressAreRefused(t *testing.T) {
	s := rebaseMerged(t)
	s.write("README", "changed\n")
	if res := s.plan(t); firstCode(res) != "target-dirty" {
		t.Fatalf("modified file: %s %+v", res.Status, res.Diagnostics)
	}
	s.git("checkout", "--", "README")
	s.write("untracked.txt", "u\n")
	if res := s.plan(t); res.Status != StatusOK {
		t.Fatalf("an untracked file is not a dirty target: %+v", res.Diagnostics)
	}
	for _, name := range []string{"rebase-merge", "rebase-apply", "sequencer"} {
		os.Mkdir(filepath.Join(s.dir, ".git", name), 0o755)
		if res := s.plan(t); firstCode(res) != "operation-in-progress" {
			t.Errorf("%s: %s %+v", name, res.Status, res.Diagnostics)
		}
		os.Remove(filepath.Join(s.dir, ".git", name))
	}
	for _, name := range []string{"MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "BISECT_LOG"} {
		os.WriteFile(filepath.Join(s.dir, ".git", name), []byte(s.base+"\n"), 0o644)
		if res := s.plan(t); firstCode(res) != "operation-in-progress" {
			t.Errorf("%s: %s %+v", name, res.Status, res.Diagnostics)
		}
		os.Remove(filepath.Join(s.dir, ".git", name))
	}
}

func TestBranchCheckedOutElsewhereIsReportedAndRefused(t *testing.T) {
	s := rebaseMerged(t)
	other := filepath.Join(filepath.Dir(s.dir), "other")
	s.git("worktree", "add", "-q", other, "child")
	res := s.plan(t)
	if firstCode(res) != "branch-checked-out-elsewhere" || res.Diagnostics[0].Details["path"] != other {
		t.Fatalf("%s %+v", res.Status, res.Diagnostics)
	}
	var held bool
	for _, w := range res.Effects.Worktrees {
		held = held || (w.Path == other && w.State == "holds-branch")
	}
	if !held {
		t.Fatalf("worktrees %+v", res.Effects.Worktrees)
	}
}

func TestNonCanonicalPathsAndBareRepositoriesAreRefused(t *testing.T) {
	s := rebaseMerged(t)
	link := filepath.Join(filepath.Dir(s.dir), "link")
	if err := os.Symlink(s.dir, link); err != nil {
		t.Fatal(err)
	}
	o := s.options()
	o.Path = link
	if res := Plan(context.Background(), o); firstCode(res) != "path-not-canonical" {
		t.Fatalf("%s %+v", res.Status, res.Diagnostics)
	}
	bare := filepath.Join(filepath.Dir(s.dir), "bare.git")
	s.git("clone", "-q", "--bare", s.dir, bare)
	o.Path = bare
	if res := Plan(context.Background(), o); res.Status != StatusError || firstCode(res) != "bare-repository" {
		t.Fatalf("%s %+v", res.Status, res.Diagnostics)
	}
	o.Path = filepath.Join(filepath.Dir(s.dir), "missing")
	if res := Plan(context.Background(), o); res.Status != StatusError {
		t.Fatalf("%s", res.Status)
	}
}

func TestAttributeOfTheBranchTipTreeAloneIsAHazard(t *testing.T) {
	s := rebaseMergedWith(t, func(r *repo) {
		r.commit("B0", map[string]string{".gitattributes": "child1.txt merge=custom\n"})
		r.commit("B1", map[string]string{"parent1.txt": "p1\n"})
	}, func(r *repo) {
		r.commit("C1", map[string]string{"child1.txt": "c1\n"})
	}, func(r *repo) {
		r.git("rm", "-q", ".gitattributes")
		r.git("commit", "-q", "-m", "drop attributes")
	})
	res := s.plan(t)
	if firstCode(res) != "repository-hazard" || !slices.Contains(hazardsOf(res), "attribute-merge") {
		t.Fatalf("%s %+v", res.Status, res.Diagnostics)
	}
}

func TestJJAndNonRepositoriesAreRefusedWithoutAGitFallback(t *testing.T) {
	s := rebaseMerged(t)
	if err := os.Mkdir(filepath.Join(s.dir, ".jj"), 0o755); err != nil {
		t.Fatal(err)
	}
	res := s.plan(t)
	if res.Status != StatusError || firstCode(res) != "unsupported-backend" || res.Evidence != nil {
		t.Fatalf("jj: %s %s %+v", res.Status, res.Backend, res.Diagnostics)
	}
	os.Remove(filepath.Join(s.dir, ".jj"))
	plain := t.TempDir()
	o := s.options()
	o.Path = plain
	if resolved, err := filepath.EvalSymlinks(plain); err == nil {
		o.Path = resolved
	}
	if res := Plan(context.Background(), o); res.Status != StatusError || firstCode(res) != "not-repository" {
		t.Fatalf("%s %+v", res.Status, res.Diagnostics)
	}
}

func TestSHA256RepositoriesAreRefused(t *testing.T) {
	r := newRepo(t)
	dir := filepath.Join(filepath.Dir(r.dir), "sha256")
	if out, code := r.gitIn(filepath.Dir(r.dir), nil, "", "init", "-q", "--object-format=sha256", "-b", "main", dir); code != 0 {
		t.Skip("this Git cannot create a sha256 repository: " + out)
	}
	res := Plan(context.Background(), Options{Path: dir, Branch: "refs/heads/main", OldParent: "refs/heads/a", NewParent: "refs/heads/b", Remote: "origin"})
	if res.Status != StatusError || firstCode(res) != "object-format-unsupported" {
		t.Fatalf("%s %+v", res.Status, res.Diagnostics)
	}
}

// touchAll gives every tracked file a new mtime so that `git status` compares content (with the
// attributes of the real worktree) instead of trusting the index stat data.
func touchAll(t *testing.T, s *scenario, names ...string) {
	t.Helper()
	future := time.Now().Add(time.Hour)
	for _, n := range names {
		if err := os.Chtimes(filepath.Join(s.dir, n), future, future); err != nil {
			t.Fatal(err)
		}
	}
}

// The dirty check must see the real worktree, so it must apply the committed text and eol rules:
// a clean CRLF checkout of an `eol=crlf` file is not dirty (issue 241 review F1).
func TestStatusAppliesCommittedEolAttributesSoACleanCheckoutIsNotDirty(t *testing.T) {
	s := rebaseMergedWith(t, func(r *repo) {
		r.commit("B0", map[string]string{".gitattributes": "*.dat text eol=crlf\n"})
		r.commit("B1", map[string]string{"x.dat": "one\ntwo\n"})
	}, func(r *repo) {
		r.commit("C1", map[string]string{"child1.txt": "c1\n"})
	}, func(r *repo) {
		r.commit("M", map[string]string{"main.txt": "m\n"})
	})
	if b, err := os.ReadFile(filepath.Join(s.dir, "x.dat")); err != nil || !strings.Contains(string(b), "\r\n") {
		t.Fatalf("fixture: x.dat is not checked out with CRLF: %q %v", b, err)
	}
	touchAll(t, s, "x.dat")
	if res := s.plan(t); res.Status != StatusOK {
		t.Fatalf("a clean checkout reported dirty: %s %+v", res.Status, res.Diagnostics)
	}
}

// The other direction: a committed `text` rule over a CRLF blob makes the checkout differ.
func TestStatusAppliesCommittedTextAttributesSoARealChangeIsDirty(t *testing.T) {
	s := rebaseMergedWith(t, func(r *repo) {
		r.commit("B1", map[string]string{"parent1.txt": "p1\n"})
	}, func(r *repo) {
		r.commit("C1", map[string]string{"child1.txt": "c1\n"})
	}, func(r *repo) {
		r.commit("M0", map[string]string{"x.txt": "one\r\ntwo\r\n"})
		// add only the attribute file: `add -A` would renormalize the CRLF blob under the new rule
		r.write(".gitattributes", "*.txt eol=lf\n")
		r.git("add", ".gitattributes")
		r.git("commit", "-q", "-m", "M1")
	})
	touchAll(t, s, "x.txt")
	if out, _ := s.gitCode("status", "--porcelain", "--untracked-files=no"); !strings.Contains(out, "x.txt") {
		t.Fatalf("fixture: default Git does not report x.txt as modified: %q", out)
	}
	if res := s.plan(t); firstCode(res) != "target-dirty" {
		t.Fatalf("expected target-dirty: %s %+v", res.Status, res.Diagnostics)
	}
}
