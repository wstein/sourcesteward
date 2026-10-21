// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
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

func withUpstream(branches map[string][][2]string) func(*restoreFix) {
	return func(f *restoreFix) {
		f.git("remote", "add", "origin", "https://example.invalid/never-contacted.git")
		for name, kv := range branches {
			for _, p := range kv {
				f.git("config", "--add", "branch."+name+"."+p[0], p[1])
			}
		}
	}
}

func upstreamOf(name string) [][2]string {
	return [][2]string{{"remote", "origin"}, {"merge", "refs/heads/" + name}, {"description", "private words"}}
}

func journalEntryOf(t *testing.T, f *restoreFix, name string) V3JournalEntry {
	t.Helper()
	view, err := readRestoreJournal(f.state, f.stored.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range v3Eligible(view.Plan) {
		if c.Name == "refs/heads/"+name {
			return view.Journal.Entries[i]
		}
	}
	t.Fatalf("no journal entry for %s", name)
	return V3JournalEntry{}
}

func varsOf(ev *V3BranchEvidence) map[string]string {
	out := map[string]string{}
	for _, item := range ev.Config {
		out[item.Var] = item.Value
	}
	return out
}

func TestJournalRecordsBranchConfigAndReflogTip(t *testing.T) {
	f := newRestoreFixWith(t, restoreFixOptions{setup: withUpstream(map[string][][2]string{"alpha": upstreamOf("alpha")})}, "alpha", "beta")
	alpha := journalEntryOf(t, f, "alpha").Branch
	if alpha == nil || alpha.ConfigStatus != "recorded" || alpha.ConfigTruncated {
		t.Fatalf("alpha evidence: %+v", alpha)
	}
	want := map[string]string{"remote": "origin", "merge": "refs/heads/alpha", "description": "private words"}
	if !reflect.DeepEqual(varsOf(alpha), want) {
		t.Errorf("alpha keys %v, want %v", varsOf(alpha), want)
	}
	if alpha.BranchOID != f.tips["alpha"] || alpha.ReflogTip != f.tips["alpha"] {
		t.Errorf("alpha branch %s reflog %s, want %s", alpha.BranchOID, alpha.ReflogTip, f.tips["alpha"])
	}
	beta := journalEntryOf(t, f, "beta").Branch
	if beta == nil || beta.ConfigStatus != "recorded" || len(beta.Config) != 0 || beta.BranchOID != f.tips["beta"] {
		t.Errorf("a branch without configuration records no keys: %+v", beta)
	}
}

func TestJournalRecordsAnUnavailableReflogWithoutFailingTheDeletion(t *testing.T) {
	f := newRestoreFixWith(t, restoreFixOptions{setup: func(f *restoreFix) {
		if err := os.Remove(filepath.Join(f.dir, ".git", "logs", "refs", "heads", "alpha")); err != nil {
			t.Fatal(err)
		}
	}}, "alpha")
	ev := journalEntryOf(t, f, "alpha").Branch
	if ev == nil || ev.ReflogTip != "unavailable" || ev.BranchOID != f.tips["alpha"] {
		t.Fatalf("evidence: %+v", ev)
	}
	if _, err := f.tryGit("rev-parse", "--verify", "-q", "refs/heads/alpha"); err == nil {
		t.Error("the branch was not deleted")
	}
}

func TestJournalRecordsTheEvidenceBeforeTheDeletion(t *testing.T) {
	var atDeletion map[string]any
	var branchThen string
	f := newRestoreFixWith(t, restoreFixOptions{
		setup: withUpstream(map[string][][2]string{"alpha": upstreamOf("alpha")}),
		tweak: func(f *restoreFix, c *tidyV3RefsApplyCoordinator) {
			prepare := c.prepare
			c.prepare = func(ctx context.Context, bin string, id NativeIdentity, ref, oid string, write, ro []string) (tidyV3ApplyRefTransaction, error) {
				b, err := os.ReadFile(filepath.Join(f.state, "tidy-v3-operations", f.stored.PlanID+".json"))
				if err != nil {
					t.Error(err)
				} else if err = json.Unmarshal(b, &atDeletion); err != nil {
					t.Error(err)
				}
				branchThen, _ = f.tryGit("rev-parse", "--verify", "-q", ref)
				return prepare(ctx, bin, id, ref, oid, write, ro)
			}
		},
	}, "alpha")
	if branchThen != f.tips["alpha"] {
		t.Fatalf("the branch was already gone when the deletion was prepared: %q", branchThen)
	}
	entries, _ := atDeletion["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("journal at deletion: %v", atDeletion)
	}
	if _, ok := entries[0].(map[string]any)["branch_evidence"]; !ok {
		t.Error("the journal on disk lacked the evidence when the deletion was prepared")
	}
}

func TestJournalBoundsTheRecordedConfiguration(t *testing.T) {
	kv := [][2]string{{"description", strings.Repeat("x", 5000)}}
	for i := 0; i < 20; i++ {
		kv = append(kv, [2]string{"extra" + strings.Repeat("k", i), "v"})
	}
	f := newRestoreFixWith(t, restoreFixOptions{setup: withUpstream(map[string][][2]string{"alpha": kv})}, "alpha")
	ev := journalEntryOf(t, f, "alpha").Branch
	if ev == nil || !ev.ConfigTruncated || len(ev.Config) != branchConfigKeyLimit {
		t.Fatalf("count bound: %+v", ev)
	}
	found := false
	for _, item := range ev.Config {
		if item.Var == "description" {
			found = true
			if !item.Truncated || len(item.Value) > branchConfigValueLimit {
				t.Errorf("value bound: truncated=%v len=%d", item.Truncated, len(item.Value))
			}
		}
	}
	if !found {
		t.Error("the description key (first in config order) was dropped")
	}
	// A journal whose evidence exceeds the bounds is refused by the reader.
	b, err := os.ReadFile(f.journalPath())
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err = json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	ev0 := doc["entries"].([]any)[0].(map[string]any)["branch_evidence"].(map[string]any)
	ev0["config"].([]any)[0].(map[string]any)["value"] = strings.Repeat("y", branchConfigValueLimit+1)
	out, _ := json.Marshal(doc)
	if err = os.WriteFile(f.journalPath(), out, 0o600); err != nil {
		t.Fatal(err)
	}
	if r := restorePlan(t, f.options()); r.Status != "refused" {
		t.Errorf("an oversized recorded value was accepted: %s", r.Status)
	}
}

func TestOldJournalWithoutEvidenceStillRestoresExactlyAsBefore(t *testing.T) {
	f := newRestoreFixWith(t, restoreFixOptions{setup: withUpstream(map[string][][2]string{"alpha": upstreamOf("alpha")})}, "alpha", "beta")
	b, err := os.ReadFile(f.journalPath())
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err = json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	for _, e := range doc["entries"].([]any) {
		delete(e.(map[string]any), "branch_evidence")
	}
	old, _ := json.Marshal(doc)
	if strings.Contains(string(old), "branch_evidence") {
		t.Fatal("the fixture journal still carries the field")
	}
	if err = os.WriteFile(f.journalPath(), old, 0o600); err != nil {
		t.Fatal(err)
	}
	f.git("config", "--remove-section", "branch.alpha")
	configBefore := treeBytes(t, filepath.Join(f.dir, ".git"))["config"]
	plan := restorePlan(t, f.options())
	if plan.Status != "ok" || plan.Counts.Restorable != 2 {
		t.Fatalf("%+v", plan)
	}
	raw, _ := json.Marshal(plan)
	if strings.Contains(string(raw), "branch_config") {
		t.Errorf("an old journal produced configuration output: %s", raw)
	}
	r := applyRestore(t, defaultTidyV3RestoreCoordinator(), f.applyOptions())
	if r.Status != "ok" || outcomeFor(t, r, "alpha") != "created" || outcomeFor(t, r, "beta") != "created" {
		t.Fatalf("%+v", r)
	}
	if treeBytes(t, filepath.Join(f.dir, ".git"))["config"] != configBefore {
		t.Error("configuration changed although nothing was recorded")
	}
}

func TestJournalMatchesBranchNamesExactly(t *testing.T) {
	cfg := map[string][][2]string{
		"a":   {{"remote", "origin"}, {"merge", "refs/heads/a"}},
		"a.b": {{"remote", "origin"}, {"merge", "refs/heads/a.b"}, {"rebase", "true"}},
		// A hostile section: subsection `a.remote`, variable `evil`.
		"a.remote": {{"evil", "x"}},
		"x/y.z":    {{"remote", "origin"}, {"merge", "refs/heads/x/y.z"}},
	}
	f := newRestoreFixWith(t, restoreFixOptions{setup: withUpstream(cfg)}, "a", "a.b", "x/y.z")
	a := journalEntryOf(t, f, "a").Branch
	if want := map[string]string{"remote": "origin", "merge": "refs/heads/a"}; !reflect.DeepEqual(varsOf(a), want) {
		t.Fatalf("branch a recorded %v, want %v (prefix, dot or hostile keys leaked in)", varsOf(a), want)
	}
	if got := varsOf(journalEntryOf(t, f, "a.b").Branch); len(got) != 3 || got["merge"] != "refs/heads/a.b" {
		t.Fatalf("branch a.b recorded %v", got)
	}
	if got := varsOf(journalEntryOf(t, f, "x/y.z").Branch); len(got) != 2 || got["merge"] != "refs/heads/x/y.z" {
		t.Fatalf("branch x/y.z recorded %v", got)
	}
}
