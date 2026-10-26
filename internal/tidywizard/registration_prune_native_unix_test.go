// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package tidywizard

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/tidywizard/derive"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/workspace"
)

func nativeRegistrationPruneFixture(t *testing.T) (string, string, workspace.TaskRecord) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	source, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	nativePruneGit(t, source, "init", "-q")
	nativePruneGit(t, source, "config", "user.name", "Fixture")
	nativePruneGit(t, source, "config", "user.email", "fixture@example.invalid")
	if err = os.WriteFile(filepath.Join(source, "file"), []byte("stored\n"), 0600); err != nil {
		t.Fatal(err)
	}
	nativePruneGit(t, source, "add", ".")
	nativePruneGit(t, source, "commit", "-qm", "fixture")
	state, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(state, 0700); err != nil {
		t.Fatal(err)
	}
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	planned := workspace.PlanCreate(context.Background(), workspace.PlanOptions{Path: source, Base: "HEAD", Destination: filepath.Join(parent, "task"), Task: "prune-owned", Owner: "codex", StateDir: state})
	if planned.Status != "ok" || planned.Plan == nil {
		t.Fatalf("native creation plan: %+v", planned)
	}
	applied := workspace.Apply(context.Background(), workspace.ApplyOptions{StateDir: state, PlanID: planned.Plan.PlanID, Authorize: true})
	if applied.Status != "ok" || applied.Task == nil {
		t.Fatalf("native creation: %+v", applied)
	}
	task := *applied.Task
	if err = os.Rename(task.Destination, task.Destination+"-moved"); err != nil {
		t.Fatal(err)
	}
	return source, state, task
}
func nativePruneGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestRegistrationPruneNativeLegacyStartingPointAndOptIn(t *testing.T) {
	source, state, task := nativeRegistrationPruneFixture(t)
	cfg := Config{Caller: "codex", Agent: true, CWD: source}
	repos := []RepoSpec{{Name: "r", Path: source, Base: "HEAD"}}
	stores := []derive.Store{{Path: state}}
	old := Classify(context.Background(), NewAdapter(repos, stores), []string{"r"}, cfg)
	matched := false
	for _, candidate := range old.Candidates {
		if candidate.Path == task.Destination {
			matched = true
			if candidate.Verdict != VerdictRetain || candidate.Reason != "evidence-incomplete" || len(candidate.Effects) != 0 {
				t.Fatalf("legacy starting point=%+v", candidate)
			}
		}
	}
	if !matched {
		t.Fatalf("legacy missing row absent: %+v", old)
	}
	before := nativePruneGit(t, source, "worktree", "list", "--porcelain")
	result := RegistrationPrunes(context.Background(), repos, stores, pruneMode(), cfg)
	if result.Incomplete || len(result.Rows) != 1 {
		t.Fatalf("result=%+v", result)
	}
	row := result.Rows[0]
	if row.Verdict != RegistrationPruneSafe || row.Reason != "registration-prune-ready" || row.Proof != "owned-missing-registration" || row.OwnerSource != owner.SourceTaskRecord || row.Scope != owner.ScopeOwn || row.Task != task.TaskID || !reflect.DeepEqual(row.Effects, []string{"remove-registration"}) || len(row.EvidenceDigest) != 64 || len(row.ID) != 64 || len(row.Limitations) != 3 {
		t.Fatalf("row=%+v", row)
	}
	if selected := SelectRegistrationPrunes(result.Rows, cfg); len(selected) != 1 {
		t.Fatalf("selection=%+v", selected)
	}
	if after := nativePruneGit(t, source, "worktree", "list", "--porcelain"); after != before {
		t.Fatal("native registration changed")
	}
	if _, err := os.Lstat(filepath.Join(state, "tidy-registration-v4")); !os.IsNotExist(err) {
		t.Fatalf("overview created v4 state: %v", err)
	}
	if _, err := os.Lstat(task.Destination + "-moved/file"); err != nil {
		t.Fatal("moved checkout changed", err)
	}
	// Viewing another recorded owner stays safe evidence, but agent selection is own only.
	otherMode := pruneMode()
	otherMode.Caller = "viewer"
	otherCfg := Config{Caller: "viewer", Agent: true}
	other := RegistrationPrunes(context.Background(), repos, stores, otherMode, otherCfg)
	if other.Incomplete || len(other.Rows) != 1 || other.Rows[0].Scope != owner.ScopeOther || len(SelectRegistrationPrunes(other.Rows, otherCfg)) != 0 {
		t.Fatalf("other view=%+v", other)
	}
	human := otherCfg
	human.Agent = false
	if len(SelectRegistrationPrunes(other.Rows, human)) != 1 {
		t.Fatal("human cannot select proven other owner")
	}
}

func TestRegistrationPruneNativeWholeInventoryAndOwnershipRefusals(t *testing.T) {
	for _, kind := range []string{"unlocked-sibling", "missing-record", "copied-store", "partial-inventory", "jj-marker"} {
		t.Run(kind, func(t *testing.T) {
			source, state, task := nativeRegistrationPruneFixture(t)
			repos := []RepoSpec{{Name: "r", Path: source, Base: "HEAD"}}
			stores := []derive.Store{{Path: state}}
			wantReason := "registration-prune-ownership-unproven"
			switch kind {
			case "unlocked-sibling":
				path := filepath.Join(t.TempDir(), "sibling")
				nativePruneGit(t, source, "worktree", "add", "-q", "--detach", path, "HEAD")
				if err := os.RemoveAll(path); err != nil {
					t.Fatal(err)
				}
				wantReason = "registration-prune-inventory-blocked"
			case "missing-record":
				stores = nil
			case "copied-store":
				clone := filepath.Join(t.TempDir(), "store")
				if err := os.CopyFS(clone, os.DirFS(state)); err != nil {
					t.Fatal(err)
				}
				err := filepath.WalkDir(clone, func(path string, d os.DirEntry, err error) error {
					if err != nil {
						return err
					}
					mode := os.FileMode(0600)
					if d.IsDir() {
						mode = 0700
					}
					return os.Chmod(path, mode)
				})
				if err != nil {
					t.Fatal(err)
				}
				stores = append(stores, derive.Store{Path: clone})
			case "partial-inventory":
				repos = append(repos, RepoSpec{Name: "broken", Path: "/does-not-exist"})
				wantReason = "registration-prune-inventory-blocked"
			case "jj-marker":
				if err := os.Mkdir(filepath.Join(source, ".jj"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			got := RegistrationPrunes(context.Background(), repos, stores, pruneMode(), Config{Caller: "codex", Agent: true})
			if len(SelectRegistrationPrunes(got.Rows, Config{Caller: "codex", Agent: true})) != 0 {
				t.Fatalf("refusal selected: %+v", got)
			}
			if kind == "jj-marker" {
				if !got.Incomplete {
					t.Fatal("jj ownership did not block Git registration inventory")
				}
				return
			}
			found := false
			for _, row := range got.Rows {
				if row.Destination == task.Destination {
					found = true
					if row.Reason != wantReason || row.Verdict == RegistrationPruneSafe || len(row.Effects) != 0 {
						t.Fatalf("row=%+v want %s", row, wantReason)
					}
				}
			}
			if !found {
				t.Fatalf("selected registration lost refusal: %+v", got)
			}
		})
	}
}
