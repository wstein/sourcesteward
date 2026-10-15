// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestJJRetirementAdapterOwnedLifecycle(t *testing.T) {
	if _, e := exec.LookPath("jj"); e != nil {
		if os.Getenv("STW_REQUIRE_JJ") == "1" {
			t.Fatal(e)
		}
		t.Skip("jj unavailable")
	}
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	source, state, target := filepath.Join(root, "source"), filepath.Join(root, "state"), filepath.Join(root, "task")
	os.Mkdir(state, 0700)
	cmd := exec.Command("jj", "--config", "git.colocate=true", "git", "init", source)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "JJ_CONFIG=", "JJ_USER=Test", "JJ_EMAIL=test@example.invalid")
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("init %s %v", b, e)
	}
	planned := PlanCreate(context.Background(), PlanOptions{Path: source, Base: "@", Destination: target, Task: "owned-task", Owner: "tester", StateDir: state})
	if planned.Status != "ok" {
		if os.Getenv("STW_REQUIRE_JJ") != "1" {
			t.Skip("native sandbox unavailable")
		}
		t.Fatalf("plan %+v", planned)
	}
	created := Apply(context.Background(), ApplyOptions{StateDir: state, PlanID: planned.Plan.PlanID, Authorize: true})
	if created.Status != "ok" || created.Task == nil {
		t.Fatalf("create %+v", created)
	}
	a := jjRetirementAdapter{}
	obs, e := a.Observe(context.Background(), source, "@", *created.Task, *planned.Plan)
	if e != nil {
		t.Fatal(e)
	}
	p := RetirementPlanRecord{SchemaVersion: 2, PlanID: "native-retirement", SourcePath: source, StateDir: state, Backend: "jj", CreationPlan: *planned.Plan, Task: *created.Task, Expected: obs, RemoveRegistration: true, RemoveFiles: target}
	if obs.Proof != "empty-working-copy-integrated-parent" {
		t.Fatalf("%+v", obs)
	}
	extra := filepath.Join(target, "ignored-extra")
	os.WriteFile(extra, []byte("retain"), 0600)
	_, failed, e := a.PrepareRecovery(context.Background(), p)
	if e == nil || failed.Attempted {
		t.Fatalf("untracked file authorized retirement %+v %v", failed, e)
	}
	os.Remove(extra)
	r, o, e := a.PrepareRecovery(context.Background(), p)
	if e != nil || !o.Complete || o.Expected == nil {
		t.Fatalf("recovery %+v %v", o, e)
	}
	o, e = a.RemoveRegistration(context.Background(), p, r, *o.Expected)
	if e != nil || !o.Complete || o.Expected == nil {
		t.Fatalf("forget %+v %v", o, e)
	}
	if _, e := os.Stat(target); e != nil {
		t.Fatal("registration forgetting removed files", e)
	}
	o, e = a.RemoveFiles(context.Background(), p, r, *o.Expected)
	if e != nil || !o.Complete || o.Expected == nil {
		t.Fatalf("files %+v %v", o, e)
	}
	if _, e := os.Lstat(target); !os.IsNotExist(e) {
		t.Fatal("owned files remain", e)
	}
}
