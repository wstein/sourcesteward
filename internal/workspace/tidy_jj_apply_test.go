// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestJJTidyApplyAdapterNative(t *testing.T) {
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
	repo := filepath.Join(root, "repo")
	native := func(path string, args ...string) string {
		t.Helper()
		cmd := exec.Command("jj", args...)
		cmd.Dir = path
		cmd.Env = append(os.Environ(), "JJ_CONFIG=", "JJ_USER=Test", "JJ_EMAIL=test@example.invalid")
		out, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("jj %v: %s %v", args, out, e)
		}
		return strings.TrimSpace(string(out))
	}
	native(root, "--config", "git.colocate=false", "git", "init", repo)
	native(repo, "bookmark", "create", "topic")
	native(repo, "new")
	native(repo, "bookmark", "create", "main")
	a := jjTidyAdapter{}
	obs, e := a.Observe(context.Background(), repo, "main")
	if e != nil {
		if os.Getenv("STW_REQUIRE_JJ") != "1" {
			t.Skip("native sandbox unavailable")
		}
		t.Fatal(e)
	}
	var c TidyCandidate
	for _, candidate := range obs.Candidates {
		if candidate.Name == "topic" {
			c = candidate
		}
	}
	p := TidyPlanRecord{PlanID: "adapter-test", SourcePath: repo, Backend: "jj", Expected: obs.Expected}
	r, s, o, e := a.PrepareRecovery(context.Background(), p, c)
	if e != nil || !o.Complete {
		t.Fatalf("prepare %+v %v", o, e)
	}
	o, e = a.Delete(context.Background(), p, c, r, s)
	if e != nil || !o.Complete || o.Expected == nil {
		t.Fatalf("delete %+v %v", o, e)
	}
}
