// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func gitRetirementFixture(t *testing.T) (string, MutationPlan, TaskRecord) {
	d := fixtureGit(t)
	if e := os.WriteFile(filepath.Join(d, "binary"), []byte{0, 255, 128, 7}, 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(d, "executable"), []byte("executable\n"), 0700); e != nil {
		t.Fatal(e)
	}
	gitFixture(t, d, "add", ".")
	gitFixture(t, d, "commit", "-qm", "binary and executable")
	parent := gitTempDir(t)
	state := gitTempDir(t)
	if e := os.Chmod(state, 0700); e != nil {
		t.Fatal(e)
	}
	planned := PlanCreate(context.Background(), PlanOptions{Path: d, Base: "HEAD", Destination: filepath.Join(parent, "task"), Task: "retire-owned", Owner: "fixture", StateDir: state})
	if planned.Status != "ok" {
		t.Fatalf("creation plan: %+v", planned)
	}
	applied := Apply(context.Background(), ApplyOptions{StateDir: state, PlanID: planned.Plan.PlanID, Authorize: true})
	if applied.Status != "ok" || applied.Task == nil {
		t.Fatalf("creation apply: %+v", applied)
	}
	return d, *planned.Plan, *applied.Task
}

func TestGitRetirementObservesFullCleanOwnedBinaryTree(t *testing.T) {
	d, creation, task := gitRetirementFixture(t)
	o, e := (gitRetirementAdapter{}).Observe(context.Background(), d, "HEAD", task, creation)
	if e != nil {
		t.Fatal(e)
	}
	if o.Proof != "native-ancestry" || !o.Manifest.Complete || !o.Metadata.Complete || o.LockReason != "stw:"+task.TaskID || o.Target.CommitID != task.Native.CommitID {
		t.Fatalf("incomplete evidence: %+v", o)
	}
	foundPointer, foundBinary := false, false
	for _, entry := range o.Manifest.Entries {
		if entry.Path == ".git" {
			foundPointer = true
		}
		if entry.Path == "binary" {
			foundBinary = true
		}
	}
	if !foundPointer || !foundBinary {
		t.Fatal("physical manifest omitted metadata or binary content")
	}
}

func TestGitRetirementRetainsChangedAndUnknownPayload(t *testing.T) {
	for _, kind := range []string{"dirty", "untracked", "ignored", "registration-payload", "metadata-pointer", "executable-mode"} {
		t.Run(kind, func(t *testing.T) {
			d, creation, task := gitRetirementFixture(t)
			switch kind {
			case "dirty":
				os.WriteFile(filepath.Join(task.Destination, "file"), []byte("dirty"), 0600)
			case "untracked":
				os.WriteFile(filepath.Join(task.Destination, "extra"), []byte("keep"), 0600)
			case "ignored":
				ignore := filepath.Join(gitTempDir(t), "ignore")
				if e := os.WriteFile(ignore, []byte("extra\n"), 0600); e != nil {
					t.Fatal(e)
				}
				gitFixture(t, d, "config", "core.excludesFile", ignore)
				os.WriteFile(filepath.Join(task.Destination, "extra"), []byte("keep"), 0600)
			case "registration-payload":
				os.WriteFile(filepath.Join(task.Native.GitDir, "user-payload"), []byte("keep"), 0600)
			case "metadata-pointer":
				os.WriteFile(filepath.Join(task.Destination, ".git"), []byte("gitdir: /missing\n"), 0600)
			case "executable-mode":
				os.Chmod(filepath.Join(task.Destination, "executable"), 0600)
			}
			if _, e := (gitRetirementAdapter{}).Observe(context.Background(), d, "HEAD", task, creation); e == nil {
				t.Fatalf("unsafe %s observation accepted", kind)
			}
		})
	}
}

func TestGitRetirementNativeCombinedRemovalAndRecovery(t *testing.T) {
	d, creation, task := gitRetirementFixture(t)
	sibling := filepath.Join(filepath.Dir(task.Destination), "sibling")
	if e := os.WriteFile(sibling, []byte("keep"), 0600); e != nil {
		t.Fatal(e)
	}
	o, e := (gitRetirementAdapter{}).Observe(context.Background(), d, "HEAD", task, creation)
	if e != nil {
		t.Fatal(e)
	}
	p := RetirementPlanRecord{SchemaVersion: 2, PlanID: "retire-combined", SourcePath: d, Backend: "git", CreationPlan: creation, Task: task, Expected: o, RemoveRegistration: true, RemoveFiles: task.Destination}
	recovery, out, e := (gitRetirementAdapter{}).PrepareRecovery(context.Background(), p)
	if e != nil || !out.Complete || out.Expected == nil {
		t.Fatalf("recovery: %+v %v", out, e)
	}
	out, e = (gitRetirementAdapter{}).RemoveRegistration(context.Background(), p, recovery, *out.Expected)
	if e != nil || !out.Complete || out.Expected == nil {
		t.Fatalf("remove: %+v %v", out, e)
	}
	if _, e := os.Lstat(task.Destination); !os.IsNotExist(e) {
		t.Fatal("exact destination not removed")
	}
	if _, e := os.Lstat(task.Native.GitDir); !os.IsNotExist(e) {
		t.Fatal("exact registration not removed")
	}
	if data, e := os.ReadFile(sibling); e != nil || string(data) != "keep" {
		t.Fatal("sibling changed")
	}
	if got := gitFixture(t, d, "rev-parse", recovery.Name); got != task.Native.CommitID {
		t.Fatal("native recovery missing")
	}
	if strings.Contains(gitFixture(t, d, "worktree", "list", "--porcelain"), task.Destination) {
		t.Fatal("registration remained")
	}
	files, e := (gitRetirementAdapter{}).RemoveFiles(context.Background(), p, recovery, *out.Expected)
	if e != nil || !files.Complete || files.Attempted {
		t.Fatalf("duplicate file removal attempted: %+v %v", files, e)
	}
}

func TestGitRetirementSandboxRootHelper(t *testing.T) {
	for i, arg := range os.Args {
		if arg == "stw-retirement-root-helper" {
			if e := os.Remove(os.Args[i+1]); e == nil {
				os.Exit(41)
			}
			os.Exit(0)
		}
	}
}

func TestGitRetirementSandboxPreservesGrantedRoot(t *testing.T) {
	root := gitTempDir(t)
	bin, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	bin, e = filepath.EvalSymlinks(bin)
	if e != nil {
		t.Fatal(e)
	}
	_, code, e := runSandboxed(context.Background(), bin, []string{"-test.run=TestGitRetirementSandboxRootHelper", "--", "stw-retirement-root-helper", root}, filepath.Dir(bin), []string{root}, nil)
	if e != nil || code != 0 {
		t.Fatalf("granted root could be removed: %d %v", code, e)
	}
	if _, e := os.Stat(root); e != nil {
		t.Fatal("granted root removed")
	}
}
