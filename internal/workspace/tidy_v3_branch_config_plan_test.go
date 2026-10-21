// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Spec 031 T045: the recovery journal records the `branch.<name>.*` keys and the
// reflog tip of every deleted branch before its deletion; the restore plan shows
// which keys can be reapplied and the restore apply reapplies the safe ones.

// restoreConfigFix deletes alpha (with an upstream) and gives a clone state in
// which the keys are gone, as in a fresh clone that still has the remote.
func restoreConfigFix(t *testing.T, names ...string) *restoreFix {
	t.Helper()
	cfg := map[string][][2]string{}
	for _, n := range names {
		cfg[n] = upstreamOf(n)
	}
	f := newRestoreFixWith(t, restoreFixOptions{setup: withUpstream(cfg)}, names...)
	for _, n := range names {
		f.git("config", "--remove-section", "branch."+n)
	}
	return f
}

func keyStates(e V3RestoreEntry) map[string]string {
	out := map[string]string{}
	if e.BranchConfig != nil {
		for _, k := range e.BranchConfig.Keys {
			out[k.Key] = k.State
		}
	}
	return out
}

func TestRestorePlanShowsWhichKeysCanBeReapplied(t *testing.T) {
	f := restoreConfigFix(t, "alpha")
	before := f.snapshot()
	e := entryFor(t, restorePlan(t, f.options()), "alpha")
	bc := e.BranchConfig
	if bc == nil || bc.Status != "recorded" || bc.ReflogTip != f.tips["alpha"] || bc.BranchOID != f.tips["alpha"] {
		t.Fatalf("%+v", bc)
	}
	want := map[string]string{"branch.alpha.remote": "reapply", "branch.alpha.merge": "reapply", "branch.alpha.description": "skip-not-recorded"}
	if !reflect.DeepEqual(keyStates(e), want) {
		t.Errorf("states %v, want %v", keyStates(e), want)
	}
	for _, k := range bc.Keys {
		if k.Key == "branch.alpha.remote" && (k.Remote == nil || *k.Remote != "origin") {
			t.Errorf("remote name: %+v", k)
		}
	}
	raw, _ := json.Marshal(e)
	if strings.Contains(string(raw), "private words") || strings.Contains(string(raw), "example.invalid") {
		t.Errorf("a value or a URL leaked: %s", raw)
	}
	if !reflect.DeepEqual(before, f.snapshot()) {
		t.Error("the plan wrote something")
	}
	// The remote disappears: remote-dependent keys cannot be reapplied.
	f.git("remote", "remove", "origin")
	want = map[string]string{"branch.alpha.remote": "skip-remote-missing", "branch.alpha.merge": "skip-remote-missing", "branch.alpha.description": "skip-not-recorded"}
	if got := keyStates(entryFor(t, restorePlan(t, f.options()), "alpha")); !reflect.DeepEqual(got, want) {
		t.Errorf("without the remote %v, want %v", got, want)
	}
}

func TestRestorePlanKeepsKeysThatAreAlreadyConfigured(t *testing.T) {
	f := newRestoreFixWith(t, restoreFixOptions{setup: withUpstream(map[string][][2]string{"alpha": upstreamOf("alpha")})}, "alpha")
	// A branch deletion by reference transaction leaves the keys in place.
	got := keyStates(entryFor(t, restorePlan(t, f.options()), "alpha"))
	if got["branch.alpha.remote"] != "skip-already-set" || got["branch.alpha.merge"] != "skip-already-set" {
		t.Errorf("%v", got)
	}
}

func TestRestorePlanWithAnotherNameDoesNotReapply(t *testing.T) {
	f := restoreConfigFix(t, "alpha")
	o := f.options()
	o.Actions, o.As = []string{f.action("alpha")}, "refs/heads/renamed"
	if got := keyStates(entryFor(t, restorePlan(t, o), "renamed")); got["branch.alpha.remote"] != "skip-renamed" {
		t.Errorf("%v", got)
	}
}

func TestRestorePlanDoesNotReapplyAKeyWrittenWithoutAValue(t *testing.T) {
	setup := withUpstream(map[string][][2]string{"alpha": upstreamOf("alpha")})
	f := newRestoreFixWith(t, restoreFixOptions{setup: func(f *restoreFix) {
		setup(f)
		cfg := filepath.Join(f.dir, ".git", "config")
		b, err := os.ReadFile(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(cfg, append(b, []byte("[branch \"alpha\"]\n\trebase\n")...), 0o600); err != nil {
			t.Fatal(err)
		}
	}}, "alpha")
	f.git("config", "--remove-section", "branch.alpha")
	if got := keyStates(entryFor(t, restorePlan(t, f.options()), "alpha"))["branch.alpha.rebase"]; got != "skip-not-recorded" {
		t.Errorf("a key without a value: %q", got)
	}
	applyRestore(t, defaultTidyV3RestoreCoordinator(), f.applyOptions())
	if got, err := f.tryGit("config", "--local", "--get", "branch.alpha.rebase"); err == nil {
		t.Errorf("rebase was written as %q although it had no value", got)
	}
}

func TestRestorePlanSkipsMergeWhenTheRemoteIsNotReapplied(t *testing.T) {
	f := restoreConfigFix(t, "alpha")
	f.git("remote", "add", "other", "https://example.invalid/other.git")
	f.git("config", "branch.alpha.remote", "other")
	got := keyStates(entryFor(t, restorePlan(t, f.options()), "alpha"))
	if got["branch.alpha.remote"] != "skip-already-set" || got["branch.alpha.merge"] != "skip-remote-missing" {
		t.Errorf("%v", got)
	}
	applyRestore(t, defaultTidyV3RestoreCoordinator(), f.applyOptions())
	if got, err := f.tryGit("config", "--local", "--get", "branch.alpha.merge"); err == nil {
		t.Errorf("merge %q was reapplied next to another remote", got)
	}
}

func TestRestorePlanSkipsAMultiValuedKey(t *testing.T) {
	cfg := map[string][][2]string{"alpha": append(upstreamOf("alpha"), [2]string{"merge", "refs/heads/other"})}
	f := newRestoreFixWith(t, restoreFixOptions{setup: withUpstream(cfg)}, "alpha")
	f.git("config", "--remove-section", "branch.alpha")
	got := keyStates(entryFor(t, restorePlan(t, f.options()), "alpha"))
	if got["branch.alpha.merge"] != "skip-multi-valued" || got["branch.alpha.remote"] != "reapply" {
		t.Errorf("%v", got)
	}
	applyRestore(t, defaultTidyV3RestoreCoordinator(), f.applyOptions())
	if got, err := f.tryGit("config", "--local", "--get-all", "branch.alpha.merge"); err == nil {
		t.Errorf("a multi-valued key was reapplied as %q", got)
	}
}
