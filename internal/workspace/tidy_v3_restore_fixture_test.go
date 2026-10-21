// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// restoreFix is a repository whose integrated branches were deleted by a real
// tidy v3 plan and apply (never a hand-written journal), so that restore reads
// exactly what production wrote.
type restoreFix struct {
	*proofFix
	stored V3Plan
	plans  []V3Plan          // every plan applied into a shared store, oldest first
	tips   map[string]string // short branch name -> tip OID at deletion
	state  string
}

// newRestoreFix integrates each named branch into main with its own commit,
// plans their deletion and applies the plan.
func newRestoreFix(t *testing.T, names ...string) *restoreFix {
	t.Helper()
	return newRestoreFixWith(t, restoreFixOptions{}, names...)
}

// restoreFixOptions lets a test prepare the repository (branch configuration,
// remotes) before the real apply and wrap the apply coordinator's seams.
type restoreFixOptions struct {
	setup func(*restoreFix)
	tweak func(*restoreFix, *tidyV3RefsApplyCoordinator)
}

func newRestoreFixWith(t *testing.T, opts restoreFixOptions, names ...string) *restoreFix {
	t.Helper()
	f := &restoreFix{proofFix: newProofFix(t), tips: map[string]string{}}
	for _, name := range names {
		f.git("checkout", "-q", "--detach", f.main)
		oid := f.commit("work on "+name, map[string]string{"work-" + strings.ReplaceAll(name, "/", "-") + ".txt": name + "\n"})
		f.git("update-ref", "refs/heads/"+name, oid)
		f.git("checkout", "-q", f.main)
		f.git("merge", "-q", "--no-ff", "-m", "integrate "+name, oid)
		f.tips[name] = oid
	}
	refs := make([]string, 0, len(names))
	for _, name := range names {
		refs = append(refs, "refs/heads/"+name)
	}
	if opts.setup != nil {
		opts.setup(f)
	}
	f.applyPlanWith(t, opts.tweak, V3PlanOptions{Refs: refs})
	return f
}

// applyPlan plans with the options and applies the stored plan for real.
func (f *restoreFix) applyPlan(t *testing.T, o V3PlanOptions) { f.applyPlanWith(t, nil, o) }

func (f *restoreFix) applyPlanWith(t *testing.T, tweak func(*restoreFix, *tidyV3RefsApplyCoordinator), o V3PlanOptions) {
	t.Helper()
	r := f.plan(o)
	if r.Status != "ok" || r.Plan == nil {
		t.Fatalf("fixture plan: %+v", r.Diagnostics)
	}
	f.stored, f.state = *r.Plan, r.Plan.StateDir
	c := defaultTidyV3RefsApplyCoordinator()
	if tweak != nil {
		tweak(f, &c)
	}
	a := c.apply(context.Background(), V3ApplyOptions{StateDir: f.state, PlanID: f.stored.PlanID, Authorize: true, ProtectWorktrees: []string{}})
	if a.Status != "ok" {
		t.Fatalf("fixture apply: %s %+v", a.Status, a.Diagnostics)
	}
}

func (f *restoreFix) action(name string) string {
	f.t.Helper()
	for _, c := range f.stored.Candidates {
		if c.Name == "refs/heads/"+name {
			return c.ActionID
		}
	}
	f.t.Fatalf("no candidate %s", name)
	return ""
}

func (f *restoreFix) recoveryRef(name string) string {
	return v3RecoveryFor(f.stored, f.action(name), f.tips[name]).Name
}

func (f *restoreFix) journalPath() string {
	return filepath.Join(f.state, "tidy-v3-operations", f.stored.PlanID+".json")
}

// treeBytes maps every regular file below dir (relative path) to its bytes.
func treeBytes(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		rel, _ := filepath.Rel(dir, path)
		out[rel] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (f *restoreFix) options() V3RestoreOptions {
	return V3RestoreOptions{Path: f.dir, StateDir: f.state, PlanID: f.stored.PlanID}
}

// repoSnapshot is everything a restore plan must leave as it found it: the
// references with their symbolic targets, the configuration, the reflogs and
// the private store.
func (f *restoreFix) snapshot() map[string]any {
	f.t.Helper()
	return map[string]any{
		"refs":   f.git("for-each-ref", "--format=%(refname) %(objectname) %(symref)"),
		"head":   f.git("symbolic-ref", "HEAD"),
		"config": treeBytes(f.t, filepath.Join(f.dir, ".git"))["config"],
		"logs":   treeBytes(f.t, filepath.Join(f.dir, ".git", "logs")),
		"state":  treeBytes(f.t, f.state),
	}
}

// entryFor returns the entry of the short branch name.
func entryFor(t *testing.T, r V3RestoreResult, name string) V3RestoreEntry {
	t.Helper()
	for _, e := range r.Entries {
		if e.Ref == "refs/heads/"+name {
			return e
		}
	}
	t.Fatalf("no entry for %s in %+v", name, r.Entries)
	return V3RestoreEntry{}
}

// newPartialRestoreFix applies a plan of three branches whose second native
// deletion is refused after its recovery was confirmed and whose third entry
// was never started, as the stopped real apply leaves them.
func newPartialRestoreFix(t *testing.T, names ...string) *restoreFix {
	t.Helper()
	f := &restoreFix{proofFix: newProofFix(t), tips: map[string]string{}}
	refs := []string{}
	for _, name := range names {
		f.git("checkout", "-q", "--detach", f.main)
		oid := f.commit("work on "+name, map[string]string{"work-" + name + ".txt": name + "\n"})
		f.git("update-ref", "refs/heads/"+name, oid)
		f.git("checkout", "-q", f.main)
		f.git("merge", "-q", "--no-ff", "-m", "integrate "+name, oid)
		f.tips[name] = oid
		refs = append(refs, "refs/heads/"+name)
	}
	r := f.plan(V3PlanOptions{Refs: refs})
	if r.Status != "ok" || r.Plan == nil {
		t.Fatalf("%+v", r.Diagnostics)
	}
	f.stored, f.state = *r.Plan, r.Plan.StateDir
	c := defaultTidyV3RefsApplyCoordinator()
	prepare := c.prepare
	calls := 0
	c.prepare = func(ctx context.Context, bin string, id NativeIdentity, ref, oid string, write, ro []string) (tidyV3ApplyRefTransaction, error) {
		calls++
		tx, err := prepare(ctx, bin, id, ref, oid, write, ro)
		if err == nil && calls == 2 {
			return &fixtureTidyV3RefuseAfterIntent{tidyV3ApplyRefTransaction: tx}, nil
		}
		return tx, err
	}
	a := c.apply(context.Background(), V3ApplyOptions{StateDir: f.state, PlanID: f.stored.PlanID, Authorize: true, ProtectWorktrees: []string{}})
	if a.Status != "partial" {
		t.Fatalf("the fixture apply did not stop: %s %+v", a.Status, a.Diagnostics)
	}
	return f
}

func (f *restoreFix) applyOptions() V3RestoreOptions {
	o := f.options()
	o.Apply, o.Authorize = true, true
	return o
}

// restoreRecords reads the raw restore records of the fixture's plan, by file name.
func (f *restoreFix) restoreRecords() map[string]string {
	f.t.Helper()
	dir := filepath.Join(f.state, "tidy-v3-restores")
	if _, err := os.Lstat(dir); os.IsNotExist(err) {
		return map[string]string{}
	}
	return treeBytes(f.t, dir)
}

func (f *restoreFix) reflog(name string) string {
	f.t.Helper()
	b, err := os.ReadFile(filepath.Join(f.dir, ".git", "logs", "refs", "heads", filepath.FromSlash(name)))
	if err != nil {
		f.t.Fatal(err)
	}
	return string(b)
}

// worktreeBytes is everything of the primary worktree except the repository.
func (f *restoreFix) worktreeBytes() map[string]string {
	f.t.Helper()
	out := map[string]string{}
	for k, v := range treeBytes(f.t, f.dir) {
		if !strings.HasPrefix(k, ".git"+string(filepath.Separator)) {
			out[k] = v
		}
	}
	return out
}

// tryGit runs git in the fixture and reports its failure instead of failing the test.
func (f *restoreFix) tryGit(args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", f.dir}, args...)...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
