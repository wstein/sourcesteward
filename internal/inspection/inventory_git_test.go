// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func gitInventory(t *testing.T, d string, o InventoryOptions) InventoryResult {
	t.Helper()
	if len(o.Scopes) == 0 && o.Query != "ref-exists" && o.Query != "ancestor" {
		o.Scopes = []string{"disk", "tracked", "untracked"}
	}
	id := Inspect(context.Background(), d)
	if id.Status != "ok" {
		t.Fatalf("identity: %+v", id)
	}
	return inventoryGit(context.Background(), id, o)
}
func TestGitInventoryWorktreesFilesAndEffects(t *testing.T) {
	root := t.TempDir()
	d := filepath.Join(root, "main")
	os.Mkdir(d, 0700)
	git(t, d, "init", "-q", "-b", "trunk")
	os.WriteFile(filepath.Join(d, "tracked"), []byte("old\n"), 0600)
	git(t, d, "add", ".")
	git(t, d, "commit", "-qm", "initial")
	linked := filepath.Join(root, "linked \n日本語")
	git(t, d, "worktree", "add", "-qb", "topic", linked)
	os.WriteFile(filepath.Join(d, "tracked"), []byte("new\n"), 0600)
	os.WriteFile(filepath.Join(d, "[literal]\n"), []byte("new"), 0600)
	before := snapshot(t, root)
	r := gitInventory(t, d, InventoryOptions{})
	if r.Status != "ok" || len(r.Workspaces) != 2 {
		t.Fatalf("%+v", r)
	}
	if len(r.Files) != 3 {
		t.Fatalf("files %+v", r.Files)
	}
	if r.Workspaces[0].TaskOwner != "unknown" {
		t.Fatal("invented ownership")
	}
	after := snapshot(t, root)
	if !equalSnapshot(before, after) {
		t.Fatal("inventory wrote target state")
	}
	q := gitInventory(t, d, InventoryOptions{Query: "dirty", Paths: []string{"[literal]\n"}})
	if q.Query == nil || q.Query.Value != "true" {
		t.Fatalf("%+v", q)
	}
	q = gitInventory(t, d, InventoryOptions{Query: "clean", Paths: []string{"absent"}})
	if q.Status != "ok" || q.Query.Value != "true" {
		t.Fatalf("%+v", q)
	}
}
func equalSnapshot(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
func TestGitInventoryReferencesAndPredicates(t *testing.T) {
	d := repo(t)
	git(t, d, "commit", "--allow-empty", "-qm", "initial")
	initial := strings.TrimSpace(git(t, d, "rev-parse", "HEAD"))
	git(t, d, "branch", "old")
	git(t, d, "remote", "add", "custom", "https://example.invalid/repo")
	git(t, d, "update-ref", "refs/remotes/custom/trunk", initial)
	git(t, d, "symbolic-ref", "refs/remotes/custom/HEAD", "refs/remotes/custom/trunk")
	git(t, d, "config", "branch.main.remote", "custom")
	git(t, d, "config", "branch.main.merge", "refs/heads/trunk")
	git(t, d, "commit", "--allow-empty", "-qm", "local")
	r := gitInventory(t, d, InventoryOptions{Base: "old", Revision: initial, Remotes: []string{"custom"}, Scopes: []string{"stored"}})
	if r.Status != "ok" {
		t.Fatalf("%+v", r)
	}
	found := false
	for _, ref := range r.References {
		if ref.Name == "refs/heads/main" {
			found = true
			if ref.LocalOnly.Value != "true" || ref.MergedIntoBase.Value != "false" || ref.ContainsRevision.Value != "true" || ref.Upstream == nil {
				t.Fatalf("%+v", ref)
			}
		}
	}
	if !found {
		t.Fatal("missing main")
	}
	for _, tc := range []struct {
		o     InventoryOptions
		value string
	}{{InventoryOptions{Query: "ref-exists", Ref: "absent"}, "false"}, {InventoryOptions{Query: "ref-exists", Ref: "main"}, "true"}, {InventoryOptions{Query: "ancestor", From: "HEAD", To: "old"}, "false"}, {InventoryOptions{Query: "ancestor", From: "old", To: "HEAD"}, "true"}, {InventoryOptions{Query: "ancestor", From: "HEAD~1", To: "HEAD"}, "unknown"}} {
		r := gitInventory(t, d, tc.o)
		if r.Query == nil || r.Query.Value != tc.value {
			t.Fatalf("%+v: %+v", tc.o, r)
		}
	}
}
func TestGitInventoryScopesRenamesAndBare(t *testing.T) {
	d := repo(t)
	os.WriteFile(filepath.Join(d, "old\n"), []byte("body\n"), 0600)
	git(t, d, "add", ".")
	git(t, d, "commit", "-qm", "initial")
	git(t, d, "mv", "old\n", "new\n")
	os.WriteFile(filepath.Join(d, ".gitignore"), []byte("ignored\n"), 0600)
	os.WriteFile(filepath.Join(d, "ignored"), []byte("i"), 0600)
	r := gitInventory(t, d, InventoryOptions{Scopes: []string{"staged", "ignored"}, Paths: []string{"old\n", "ignored"}})
	if r.Status != "ok" || len(r.Files) != 2 {
		t.Fatalf("%+v", r)
	}
	rename := r.Files[1]
	for _, f := range r.Files {
		if f.Change == "renamed" {
			rename = f
		}
	}
	if rename.OldPath == nil || *rename.OldPath != "old\n" {
		t.Fatalf("%+v", r.Files)
	}
	bare := t.TempDir()
	git(t, bare, "init", "--bare", "-q")
	r = gitInventory(t, bare, InventoryOptions{Query: "clean"})
	if r.Status != "partial" || r.Query.Value != "unknown" {
		t.Fatalf("%+v", r)
	}
}
func TestGitInventoryDoesNotRunContentHelpers(t *testing.T) {
	d := repo(t)
	sentinel := filepath.Join(t.TempDir(), "helper-ran")
	helper := "!touch '" + sentinel + "'; cat"
	os.WriteFile(filepath.Join(d, ".gitattributes"), []byte("tracked filter=hostile diff=hostile\n"), 0600)
	os.WriteFile(filepath.Join(d, "tracked"), []byte("old\n"), 0600)
	git(t, d, "add", ".")
	git(t, d, "commit", "-qm", "initial")
	git(t, d, "config", "filter.hostile.clean", helper)
	git(t, d, "config", "filter.hostile.process", helper)
	git(t, d, "config", "filter.hostile.required", "true")
	git(t, d, "config", "diff.hostile.textconv", helper)
	git(t, d, "config", "core.fsmonitor", helper)
	os.WriteFile(filepath.Join(d, "tracked"), []byte("new\n"), 0600)
	r := gitInventory(t, d, InventoryOptions{Scopes: []string{"disk", "staged", "stored"}})
	if r.Status != "partial" {
		t.Fatalf("%+v", r)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("helper ran: %v", err)
	}
}
func TestGitInventoryParsersRejectMalformed(t *testing.T) {
	for _, raw := range []string{"M\x00", "M\x00invalid\xff\x00", "M\x00missing-terminator"} {
		if _, err := parseGitInventoryDiff(raw, "stored"); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}

func TestGitInventoryNormalizationScopesAndMode(t *testing.T) {
	d := repo(t)
	os.WriteFile(filepath.Join(d, "file"), []byte("line\n"), 0600)
	git(t, d, "add", ".")
	git(t, d, "commit", "-qm", "initial")
	os.WriteFile(filepath.Join(d, ".gitattributes"), []byte("file text eol=lf\n"), 0600)
	os.WriteFile(filepath.Join(d, "file"), []byte("line\r\n"), 0600)
	r := gitInventory(t, d, InventoryOptions{Query: "clean", Scopes: []string{"disk"}})
	if r.Status != "partial" || r.Query.Value != "unknown" {
		t.Fatalf("normalization %+v", r)
	}
	r = gitInventory(t, d, InventoryOptions{Query: "clean", Scopes: []string{"staged"}})
	if r.Status != "ok" || r.Query.Value != "true" {
		t.Fatalf("staged %+v", r)
	}
	os.Remove(filepath.Join(d, ".gitattributes"))
	os.WriteFile(filepath.Join(d, "file"), []byte("line\n"), 0700)
	os.Chmod(filepath.Join(d, "file"), 0700)
	git(t, d, "config", "core.filemode", "true")
	r = gitInventory(t, d, InventoryOptions{Query: "dirty", Scopes: []string{"disk"}})
	if r.Query.Value != "true" {
		t.Fatalf("mode %+v", r)
	}
}
func TestGitInventoryIndependentScopesAvoidDiskReads(t *testing.T) {
	d := repo(t)
	os.WriteFile(filepath.Join(d, "large"), []byte("initial"), 0600)
	git(t, d, "add", ".")
	git(t, d, "commit", "-qm", "initial")
	os.WriteFile(filepath.Join(d, "large"), []byte(strings.Repeat("x", outputLimit+1)), 0600)
	r := gitInventory(t, d, InventoryOptions{Query: "clean", Scopes: []string{"staged"}})
	if r.Status != "ok" || r.Query.Value != "true" {
		t.Fatalf("%+v", r)
	}
}
func TestGitInventoryDeletedParentAndAnnotatedTags(t *testing.T) {
	d := repo(t)
	os.Mkdir(filepath.Join(d, "dir"), 0700)
	os.WriteFile(filepath.Join(d, "dir/file"), []byte("initial"), 0600)
	git(t, d, "add", ".")
	git(t, d, "commit", "-qm", "initial")
	git(t, d, "tag", "-am", "tag", "annotated")
	os.RemoveAll(filepath.Join(d, "dir"))
	r := gitInventory(t, d, InventoryOptions{Query: "dirty"})
	if r.Status != "ok" || r.Query.Value != "true" {
		t.Fatalf("deleted parent %+v", r)
	}
	r = gitInventory(t, d, InventoryOptions{Query: "ancestor", From: "annotated", To: "HEAD"})
	if r.Status != "ok" || r.Query.Value != "true" {
		t.Fatalf("annotated tag %+v", r)
	}
}
func TestGitInventoryRejectsConcurrentReferences(t *testing.T) {
	d := repo(t)
	git(t, d, "commit", "--allow-empty", "-qm", "initial")
	id := Inspect(context.Background(), d)
	n := 0
	run := func(ctx context.Context, dir string, args ...string) (string, int, error) {
		for _, arg := range args {
			if arg == "for-each-ref" {
				n++
				if n == 2 {
					git(t, d, "branch", "racing")
				}
			}
		}
		return runGit(ctx, dir, args...)
	}
	r := inventoryGitRun(context.Background(), id, InventoryOptions{Scopes: []string{"stored"}}, run)
	if r.Status != "error" || r.Diagnostics[0].Code != "changed-repository" {
		t.Fatalf("%+v", r)
	}
}
func TestGitInventorySHA256AndConflicts(t *testing.T) { gitInventorySHA256ConflictFixture(t) }
func gitInventorySHA256ConflictFixture(t *testing.T) {
	t.Helper()
	d := t.TempDir()
	git(t, d, "init", "-q", "--object-format=sha256", "-b", "main")
	os.WriteFile(filepath.Join(d, "file"), []byte("base\n"), 0600)
	git(t, d, "add", ".")
	git(t, d, "commit", "-qm", "initial")
	git(t, d, "checkout", "-qb", "other")
	os.WriteFile(filepath.Join(d, "file"), []byte("other\n"), 0600)
	git(t, d, "commit", "-qam", "other")
	git(t, d, "checkout", "-q", "main")
	os.WriteFile(filepath.Join(d, "file"), []byte("main\n"), 0600)
	git(t, d, "commit", "-qam", "main")
	_, code, e := runGit(context.Background(), d, "merge", "--no-edit", "other")
	if e != nil || code != 1 {
		t.Fatalf("fixture merge must stop with conflicts (exit 1), got exit=%d err=%v", code, e)
	}
	r := gitInventory(t, d, InventoryOptions{})
	if r.Status != "ok" || r.Workspaces[0].Conflicts.Value != "true" {
		raw, _ := json.Marshal(r)
		t.Fatalf("SHA-256 conflict inventory: %s", raw)
	}
	for _, ref := range r.References {
		if ref.CommitID != nil && len(*ref.CommitID) != 64 {
			t.Fatal("truncated sha256")
		}
	}
}

func TestGitInventorySlashedRemoteAndBaseProvenance(t *testing.T) {
	d := repo(t)
	git(t, d, "commit", "--allow-empty", "-qm", "initial")
	oid := strings.TrimSpace(git(t, d, "rev-parse", "HEAD"))
	git(t, d, "remote", "add", "team/upstream", "https://example.invalid/repo")
	git(t, d, "update-ref", "refs/remotes/team/upstream/trunk", oid)
	git(t, d, "symbolic-ref", "refs/remotes/team/upstream/HEAD", "refs/remotes/team/upstream/trunk")
	git(t, d, "config", "branch.main.remote", "team/upstream")
	git(t, d, "config", "branch.main.merge", "refs/heads/trunk")
	r := gitInventory(t, d, InventoryOptions{Scopes: []string{"stored"}, Remotes: []string{"team/upstream"}})
	if r.Status != "ok" {
		t.Fatalf("%+v", r)
	}
	for _, ref := range r.References {
		if ref.Name == "refs/remotes/team/upstream/trunk" && (ref.Remote == nil || *ref.Remote != "team/upstream") {
			t.Fatalf("%+v", ref)
		}
	}
	configured := false
	for _, base := range r.Bases {
		if strings.HasPrefix(base.Provenance, "configured:") {
			configured = true
			if base.CommitID == nil || *base.CommitID != oid {
				t.Fatalf("%+v", base)
			}
		}
	}
	if !configured {
		t.Fatal("missing configured candidate")
	}
}

func TestGitInventoryPredicatesIgnoreUnrelatedMissingWorktrees(t *testing.T) {
	d := repo(t)
	git(t, d, "commit", "--allow-empty", "-qm", "initial")
	linked := filepath.Join(t.TempDir(), "linked")
	git(t, d, "worktree", "add", "-qb", "topic", linked)
	os.RemoveAll(linked)
	r := gitInventory(t, d, InventoryOptions{Query: "ref-exists", Ref: "absent"})
	if r.Status != "ok" || r.Query.Value != "false" || r.Query.Reason == "" {
		t.Fatalf("%+v", r)
	}
	r = gitInventory(t, d, InventoryOptions{Query: "ancestor", From: "HEAD", To: "HEAD"})
	if r.Status != "ok" || r.Query.Value != "true" || r.Query.Reason == "" {
		t.Fatalf("%+v", r)
	}
}

func TestGitInventoryStoredSelectionAndMergeAmbiguity(t *testing.T) {
	d := repo(t)
	os.WriteFile(filepath.Join(d, "base"), []byte("base"), 0600)
	git(t, d, "add", ".")
	git(t, d, "commit", "-qm", "initial")
	git(t, d, "branch", "base")
	git(t, d, "checkout", "-qb", "other")
	os.WriteFile(filepath.Join(d, "other"), []byte("other"), 0600)
	git(t, d, "add", ".")
	git(t, d, "commit", "-qm", "other")
	git(t, d, "checkout", "-q", "main")
	os.WriteFile(filepath.Join(d, "main"), []byte("main"), 0600)
	git(t, d, "add", ".")
	git(t, d, "commit", "-qm", "main")
	git(t, d, "merge", "--no-edit", "other")
	r := gitInventory(t, d, InventoryOptions{Query: "dirty", Scopes: []string{"stored"}})
	if r.Status != "partial" || r.Query.Value != "unknown" {
		t.Fatalf("merge ambiguity %+v", r)
	}
	r = gitInventory(t, d, InventoryOptions{Scopes: []string{"stored"}, Base: "base", Revision: "other"})
	if r.Status != "ok" || len(r.Files) != 1 || r.Files[0].Path != "other" {
		t.Fatalf("explicit stored endpoints %+v", r)
	}
}

func TestGitInventoryOversizedDiskIsUnknown(t *testing.T) {
	d := repo(t)
	os.WriteFile(filepath.Join(d, "large"), []byte("small"), 0600)
	git(t, d, "add", ".")
	git(t, d, "commit", "-qm", "initial")
	os.WriteFile(filepath.Join(d, "large"), []byte(strings.Repeat("x", outputLimit+1)), 0600)
	r := gitInventory(t, d, InventoryOptions{Query: "clean", Scopes: []string{"disk"}})
	if r.Status != "partial" || r.Query.Value != "unknown" {
		t.Fatalf("%+v", r)
	}
	r = gitInventory(t, d, InventoryOptions{Query: "clean", Scopes: []string{"disk"}, Paths: []string{"absent"}})
	if r.Status != "ok" || r.Query.Value != "true" {
		t.Fatalf("unrelated content %+v", r)
	}
}

func TestGitInventoryQueriesIgnoreUnselectedNormalizationAndIgnoredFiles(t *testing.T) {
	d := repo(t)
	for _, name := range []string{"safe", "other"} {
		os.WriteFile(filepath.Join(d, name), []byte("clean\n"), 0600)
	}
	git(t, d, "add", ".")
	git(t, d, "commit", "-qm", "initial")
	os.WriteFile(filepath.Join(d, ".gitattributes"), []byte("other text\n"), 0600)
	r := gitInventory(t, d, InventoryOptions{Query: "clean", Scopes: []string{"disk"}, Paths: []string{"safe"}})
	if r.Status != "ok" || r.Query.Value != "true" {
		t.Fatalf("unrelated normalization %+v", r)
	}
	os.WriteFile(filepath.Join(d, ".gitignore"), []byte("ignored/\n"), 0600)
	os.Mkdir(filepath.Join(d, "ignored"), 0700)
	for i := 0; i < 1025; i++ {
		os.WriteFile(filepath.Join(d, "ignored", fmt.Sprint(i)), []byte("i"), 0600)
	}
	r = gitInventory(t, d, InventoryOptions{Query: "clean", Scopes: []string{"disk"}, Paths: []string{"safe"}})
	if r.Status != "ok" || r.Query.Value != "true" {
		t.Fatalf("unrequested ignored scope %+v", r)
	}
}

func TestGitInventoryDiskQueriesIgnoreOtherWorkspaceState(t *testing.T) {
	d := repo(t)
	git(t, d, "commit", "--allow-empty", "-qm", "initial")
	linked := filepath.Join(t.TempDir(), "linked")
	git(t, d, "worktree", "add", "-qb", "topic", linked)
	os.RemoveAll(linked)
	r := gitInventory(t, d, InventoryOptions{Query: "clean"})
	if r.Status != "ok" || r.Query.Value != "true" {
		t.Fatalf("%+v", r)
	}
	os.WriteFile(filepath.Join(d, "new"), []byte("dirty"), 0600)
	r = gitInventory(t, d, InventoryOptions{Query: "dirty"})
	if r.Status != "ok" || r.Query.Value != "true" {
		t.Fatalf("%+v", r)
	}
}

func TestGitInventoryGitlinkStoredAndStagedChanges(t *testing.T) {
	d := repo(t)
	git(t, d, "commit", "--allow-empty", "-qm", "one")
	one := strings.TrimSpace(git(t, d, "rev-parse", "HEAD"))
	git(t, d, "commit", "--allow-empty", "-qm", "two")
	two := strings.TrimSpace(git(t, d, "rev-parse", "HEAD"))
	git(t, d, "update-index", "--add", "--cacheinfo", "160000,"+one+",module")
	git(t, d, "commit", "-qm", "initial gitlink")
	git(t, d, "update-index", "--cacheinfo", "160000,"+two+",module")
	r := gitInventory(t, d, InventoryOptions{Query: "dirty", Scopes: []string{"staged"}})
	if r.Status != "ok" || r.Query.Value != "true" {
		t.Fatalf("staged gitlink %+v", r)
	}
	git(t, d, "commit", "-qm", "advance gitlink")
	r = gitInventory(t, d, InventoryOptions{Query: "dirty", Scopes: []string{"stored"}})
	if r.Status != "ok" || r.Query.Value != "true" {
		t.Fatalf("stored gitlink %+v", r)
	}
}

func TestGitInventoryFilteredUntrackedSummaryIsUnknown(t *testing.T) {
	d := repo(t)
	git(t, d, "commit", "--allow-empty", "-qm", "initial")
	os.WriteFile(filepath.Join(d, "outside"), []byte("untracked"), 0600)
	r := gitInventory(t, d, InventoryOptions{Query: "clean", Scopes: []string{"untracked"}, Paths: []string{"safe"}})
	if r.Status != "ok" || r.Query.Value != "true" {
		t.Fatalf("selected predicate %+v", r)
	}
	if r.Workspaces[0].Untracked.Value != "unknown" || r.Workspaces[0].Untracked.Reason == "" {
		t.Fatalf("broader workspace %+v", r.Workspaces[0])
	}
}

func TestGitInventorySHA256ConflictFixtureIgnoresGlobalConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	global := "[user]\n useConfigOnly = true\n[core]\n autocrlf = input\n"
	if e := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(global), 0600); e != nil {
		t.Fatal(e)
	}
	gitInventorySHA256ConflictFixture(t)
}
