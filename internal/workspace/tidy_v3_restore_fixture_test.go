// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"os"
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
	tips   map[string]string // short branch name -> tip OID at deletion
	state  string
}

// newRestoreFix integrates each named branch into main with its own commit,
// plans their deletion and applies the plan.
func newRestoreFix(t *testing.T, names ...string) *restoreFix {
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
	f.applyPlan(t, V3PlanOptions{Refs: refs})
	return f
}

// applyPlan plans with the options and applies the stored plan for real.
func (f *restoreFix) applyPlan(t *testing.T, o V3PlanOptions) {
	t.Helper()
	r := f.plan(o)
	if r.Status != "ok" || r.Plan == nil {
		t.Fatalf("fixture plan: %+v", r.Diagnostics)
	}
	f.stored, f.state = *r.Plan, r.Plan.StateDir
	a := defaultTidyV3RefsApplyCoordinator().apply(context.Background(), V3ApplyOptions{StateDir: f.state, PlanID: f.stored.PlanID, Authorize: true, ProtectWorktrees: []string{}})
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
