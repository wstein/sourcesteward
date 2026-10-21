// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Spec 031 T045: the recovery journal records the `branch.<name>.*` keys and the
// reflog tip of every deleted branch before its deletion; the restore plan shows
// which keys can be reapplied and the restore apply reapplies the safe ones.

// withUpstream configures branches the way a tracking branch is: remote, merge
// and some unrelated keys, plus an `origin` remote that exists.
func TestRestoreApplyReappliesRemoteAndMergeOnly(t *testing.T) {
	f := restoreConfigFix(t, "alpha")
	r := applyRestore(t, defaultTidyV3RestoreCoordinator(), f.applyOptions())
	e := entryFor(t, r, "alpha")
	if r.Status != "ok" || e.Outcome != "created" {
		t.Fatalf("%+v", r)
	}
	outcomes := map[string]string{}
	for _, k := range e.BranchConfig.Keys {
		outcomes[k.Key] = k.Outcome
	}
	if outcomes["branch.alpha.remote"] != "applied" || outcomes["branch.alpha.merge"] != "applied" || outcomes["branch.alpha.description"] != "skipped" {
		t.Errorf("outcomes %v", outcomes)
	}
	if got := f.git("config", "--local", "branch.alpha.remote"); got != "origin" {
		t.Errorf("remote %q", got)
	}
	if got := f.git("config", "--local", "branch.alpha.merge"); got != "refs/heads/alpha" {
		t.Errorf("merge %q", got)
	}
	if _, err := f.tryGit("config", "--local", "branch.alpha.description"); err == nil {
		t.Error("an unrelated key was reapplied")
	}
	// The outcome record carries the per-key results.
	found := false
	for _, content := range f.restoreRecords() {
		if strings.Contains(content, `"outcome":"created"`) && strings.Contains(content, `"config"`) {
			found = true
			if strings.Contains(content, "origin") || strings.Contains(content, "private words") {
				t.Errorf("a value reached the restore journal: %s", content)
			}
		}
	}
	if !found {
		t.Errorf("no outcome record with per-key results: %v", f.restoreRecords())
	}
}

func TestRestoreApplyReappliesRebaseAndPushRemoteWhenRecorded(t *testing.T) {
	cfg := map[string][][2]string{"alpha": append(upstreamOf("alpha"), [2]string{"rebase", "true"}, [2]string{"pushRemote", "origin"})}
	f := newRestoreFixWith(t, restoreFixOptions{setup: withUpstream(cfg)}, "alpha")
	f.git("config", "--remove-section", "branch.alpha")
	applyRestore(t, defaultTidyV3RestoreCoordinator(), f.applyOptions())
	if f.git("config", "--local", "branch.alpha.rebase") != "true" || f.git("config", "--local", "branch.alpha.pushremote") != "origin" {
		t.Error("rebase or pushRemote was not reapplied")
	}
}

func TestRestoreApplyMatchesBranchNamesExactly(t *testing.T) {
	cfg := map[string][][2]string{
		"a":        {{"remote", "origin"}, {"merge", "refs/heads/a"}},
		"a.b":      {{"remote", "origin"}, {"merge", "refs/heads/a.b"}, {"rebase", "true"}},
		"a.remote": {{"evil", "x"}},
		"x/y.z":    {{"remote", "origin"}, {"merge", "refs/heads/x/y.z"}},
	}
	f := newRestoreFixWith(t, restoreFixOptions{setup: withUpstream(cfg)}, "a", "a.b", "x/y.z")
	for _, n := range []string{"a", "a.b", "x/y.z", "a.remote"} {
		f.tryGit("config", "--remove-section", "branch."+n)
	}
	o := f.applyOptions()
	o.Actions = []string{f.action("a")}
	if r := applyRestore(t, defaultTidyV3RestoreCoordinator(), o); r.Status != "ok" {
		t.Fatalf("%+v", r)
	}
	if f.git("config", "--local", "branch.a.merge") != "refs/heads/a" {
		t.Error("branch a lost its keys")
	}
	for _, key := range []string{"branch.a.b.remote", "branch.a.b.merge", "branch.a.b.rebase", "branch.a.remote.evil", "branch.x/y.z.remote"} {
		if _, err := f.tryGit("config", "--local", key); err == nil {
			t.Errorf("restoring a also configured %s", key)
		}
	}
	// Dots and slashes in the restored name itself.
	o = f.applyOptions()
	o.Actions = []string{f.action("a.b"), f.action("x/y.z")}
	if r := applyRestore(t, defaultTidyV3RestoreCoordinator(), o); r.Status != "ok" {
		t.Fatalf("%+v", r)
	}
	if f.git("config", "--local", "branch.a.b.merge") != "refs/heads/a.b" || f.git("config", "--local", "branch.a.b.rebase") != "true" || f.git("config", "--local", "branch.x/y.z.merge") != "refs/heads/x/y.z" {
		t.Error("keys of a name with dots or slashes were not reapplied")
	}
	if _, err := f.tryGit("config", "--local", "branch.a.remote.evil"); err == nil {
		t.Error("the hostile key appeared")
	}
}

func TestRestoreApplyDoesNotReapplyWithoutTheRemote(t *testing.T) {
	f := restoreConfigFix(t, "alpha")
	f.git("remote", "remove", "origin")
	r := applyRestore(t, defaultTidyV3RestoreCoordinator(), f.applyOptions())
	if r.Status != "ok" || outcomeFor(t, r, "alpha") != "created" {
		t.Fatalf("%+v", r)
	}
	for _, k := range entryFor(t, r, "alpha").BranchConfig.Keys {
		if k.Outcome != "skipped" {
			t.Errorf("%s: %s", k.Key, k.Outcome)
		}
	}
	if _, err := f.tryGit("config", "--local", "branch.alpha.remote"); err == nil {
		t.Error("a key pointing at a missing remote was written")
	}
}

func TestRestoreApplyConfigFailureKeepsTheBranchAndIsReported(t *testing.T) {
	f := restoreConfigFix(t, "alpha", "beta")
	lock := filepath.Join(f.dir, ".git", "config.lock")
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	r := applyRestore(t, defaultTidyV3RestoreCoordinator(), f.applyOptions())
	if r.Status != "partial" {
		t.Fatalf("status %s: %+v", r.Status, r)
	}
	for _, n := range []string{"alpha", "beta"} {
		e := entryFor(t, r, n)
		if e.Outcome != "created" || f.git("rev-parse", "refs/heads/"+n) != f.tips[n] {
			t.Errorf("%s: %+v", n, e)
		}
		failed := 0
		for _, k := range e.BranchConfig.Keys {
			if k.Outcome == "failed" {
				failed++
			}
		}
		if failed != 2 {
			t.Errorf("%s: %d failed keys: %+v", n, failed, e.BranchConfig.Keys)
		}
	}
	found := false
	for _, d := range r.Diagnostics {
		found = found || d.Code == "restore-config-failed"
	}
	if !found {
		t.Errorf("no diagnostic: %+v", r.Diagnostics)
	}
}

func TestRestoreApplyRerunIsIdempotentForConfiguration(t *testing.T) {
	f := restoreConfigFix(t, "alpha")
	applyRestore(t, defaultTidyV3RestoreCoordinator(), f.applyOptions())
	config := treeBytes(t, filepath.Join(f.dir, ".git"))["config"]
	records := f.restoreRecords()
	r := applyRestore(t, defaultTidyV3RestoreCoordinator(), f.applyOptions())
	if r.Status != "ok" || outcomeFor(t, r, "alpha") != "already-present" {
		t.Fatalf("%+v", r)
	}
	if treeBytes(t, filepath.Join(f.dir, ".git"))["config"] != config || !reflect.DeepEqual(records, f.restoreRecords()) {
		t.Error("a rerun changed the configuration or wrote records")
	}
}

func TestRestoreApplyAfterACrashReappliesTheKeysOnce(t *testing.T) {
	f := restoreConfigFix(t, "alpha")
	c := defaultTidyV3RestoreCoordinator()
	c.afterCAS = func(V3RestoreEntry) error { return os.ErrClosed }
	if r := applyRestore(t, c, f.applyOptions()); r.Status == "ok" {
		t.Fatal("the simulated crash did not stop the run")
	}
	r := applyRestore(t, defaultTidyV3RestoreCoordinator(), f.applyOptions())
	e := entryFor(t, r, "alpha")
	if r.Status != "ok" || e.Outcome != "created-recovered" {
		t.Fatalf("%+v", r)
	}
	if f.git("config", "--local", "--get-all", "branch.alpha.merge") != "refs/heads/alpha" {
		t.Error("the merge key is missing or duplicated")
	}
}
