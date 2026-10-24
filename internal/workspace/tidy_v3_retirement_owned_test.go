// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func fixtureTidyV3OriginalLargeOwnedTask(t *testing.T) (V3PlanOptions, MutationPlan, TaskRecord) {
	t.Helper()
	source := fixtureGit(t)
	if e := os.WriteFile(filepath.Join(source, "large.svg"), bytes.Repeat([]byte("s"), 301117), 0600); e != nil {
		t.Fatal(e)
	}
	gitFixture(t, source, "add", "large.svg")
	gitFixture(t, source, "commit", "-qm", "large original creation tree")
	state := privateDir(t)
	created := PlanCreate(context.Background(), PlanOptions{Path: source, Base: "HEAD", Destination: filepath.Join(gitTempDir(t), "original-owned"), Task: "original-owned", Owner: "operator", StateDir: state})
	if created.Plan == nil {
		t.Fatal(created.Diagnostics)
	}
	applied := Apply(context.Background(), ApplyOptions{StateDir: state, PlanID: created.Plan.PlanID, Authorize: true})
	if applied.Status != "ok" || applied.Task == nil {
		t.Fatal(applied.Diagnostics)
	}
	task := *applied.Task
	return V3PlanOptions{Path: source, Base: "HEAD", StateDir: state, Task: task.TaskID, Owner: task.Owner, RemoveRegistration: true, RemoveFiles: task.Destination, ProtectWorktrees: []string{}}, *created.Plan, task
}

func TestTidyV3ReadOnlyOwnedPolicyKeepsOriginal64KiBV2Refusal(t *testing.T) {
	o, creation, task := fixtureTidyV3OriginalLargeOwnedTask(t)
	if _, e := (gitRetirementAdapter{}).Observe(context.Background(), o.Path, o.Base, task, creation); e == nil {
		t.Fatal("v2 silently inherited larger policy")
	}
	before := layoutProtectedFileAt(t, filepath.Join(task.Native.GitDir, "locked"))
	budget := newTidyV3ProofBudget(false)
	observation, e := observeTidyV3Retirement(context.Background(), o, budget)
	if e != nil {
		t.Fatal("explicit owned v3 read-only policy", e)
	}
	provenance := observation.Retirement.Provenance
	if provenance.Kind != "owned" || provenance.Legacy != nil || provenance.Owned == nil || !reflect.DeepEqual(provenance.Owned.CreationPlan, creation) || !reflect.DeepEqual(provenance.Owned.Task, task) || provenance.Owned.LockReason != "stw:"+task.TaskID {
		t.Fatal("original ownership replaced")
	}
	if after := layoutProtectedFileAt(t, filepath.Join(task.Native.GitDir, "locked")); !reflect.DeepEqual(before, after) {
		t.Fatal("read-only v3 changed original owned lock")
	}
	if _, e = observeTidyV3Legacy(context.Background(), o, newTidyV3ProofBudget(false)); e == nil {
		t.Fatal("legacy completion adopted original owned task")
	}
	if counters := budget.snapshot(); counters.Observations != 1 || counters.RawProcesses != 1 || counters.UserPasses != 3 || counters.MetadataPasses != 4 {
		t.Fatal("owned sample budget changed", counters)
	}
}

func TestTidyV3ReadOnlyOwnedPolicyRefusesWrongOriginalLockOrCreation(t *testing.T) {
	for _, kind := range []string{"wrong-lock", "missing-lock", "wrong-owner", "head-changed-during-proof", "dirty"} {
		t.Run(kind, func(t *testing.T) {
			o, _, task := fixtureTidyV3OriginalLargeOwnedTask(t)
			observe := observeTidyV3Retirement
			switch kind {
			case "wrong-lock":
				if e := os.WriteFile(filepath.Join(task.Native.GitDir, "locked"), []byte("actor lock\n"), 0600); e != nil {
					t.Fatal(e)
				}
			case "missing-lock":
				if e := os.Remove(filepath.Join(task.Native.GitDir, "locked")); e != nil {
					t.Fatal(e)
				}
			case "wrong-owner":
				o.Owner = "other"
			case "head-changed-during-proof":
				owned, e := readTidyV3OwnedProvenance(o)
				if e != nil {
					t.Fatal(e)
				}
				calls := 0
				observer := tidyV3LegacyObserver{source: observeTidyV3Git, target: func(ctx context.Context, path string) (NativeIdentity, error) {
					id, e := gitNativeIdentity(ctx, path)
					calls++
					if calls == 1 {
						gitFixture(t, task.Destination, "commit", "--allow-empty", "-qm", "actor head during proof")
					}
					return id, e
				}}
				observe = func(ctx context.Context, o V3PlanOptions, b *tidyV3ProofBudget) (tidyV3LegacyObservation, error) {
					return observer.observeWithOwnership(ctx, o, b, &owned)
				}
			case "dirty":
				if e := os.WriteFile(filepath.Join(task.Destination, "file"), []byte("actor bytes"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			if _, e := observe(context.Background(), o, newTidyV3ProofBudget(false)); e == nil {
				t.Fatal("original ownership/clean guard bypassed")
			}
			if _, e := os.Stat(task.Destination); e != nil {
				t.Fatal("read-only admission removed files")
			}
		})
	}
}

func TestTidyV3ReadOnlyOwnedTaskProgressBindsCurrentIntegratedHead(t *testing.T) {
	source, creation, task := gitRetirementFixture(t)
	gitFixture(t, task.Destination, "commit", "--allow-empty", "-qm", "normal detached task progress")
	current := gitFixture(t, task.Destination, "rev-parse", "HEAD")
	if current == task.Native.CommitID {
		t.Fatal("fixture has no task progress")
	}
	gitFixture(t, source, "merge", "--ff-only", current)
	verified, e := (gitAdapter{}).Verify(context.Background(), creation)
	if e != nil || !verified.Complete || !sameOwner(verified.Identity, task.Native) || verified.Identity.CommitID != current {
		t.Fatal("original stable registration ownership unavailable", e)
	}
	if _, e = os.Stat(filepath.Join(task.Native.GitDir, "COMMIT_EDITMSG")); e != nil {
		t.Fatal("normal in-place commit metadata missing")
	}
	original := layoutProtectedFileAt(t, filepath.Join(creation.StateDir, "tasks", task.TaskID+".json"))
	o := V3PlanOptions{Path: source, Base: "HEAD", StateDir: creation.StateDir, Task: task.TaskID, Owner: task.Owner, RemoveRegistration: true, RemoveFiles: task.Destination, ProtectWorktrees: []string{}}
	observed, e := observeTidyV3Retirement(context.Background(), o, newTidyV3ProofBudget(false))
	if e != nil {
		t.Fatalf("normal task progress must retain original stable ownership while proving current integration: %v", e)
	}
	if observed.Retirement.Target.CommitID != current || observed.Retirement.Integration.OID != current || observed.Retirement.Integration.Decision != "delete" || observed.Retirement.Provenance.Owned.Task.Native.CommitID != task.Native.CommitID {
		t.Fatal("current integration or original creation identity substituted")
	}
	if got := layoutProtectedFileAt(t, filepath.Join(creation.StateDir, "tasks", task.TaskID+".json")); !reflect.DeepEqual(got, original) {
		t.Fatal("read-only observer rewrote original task native snapshot")
	}
}

func TestTidyV3ReadOnlyOwnedTaskProgressRetainsUnintegratedCurrentHead(t *testing.T) {
	source, creation, task := gitRetirementFixture(t)
	commitUnintegratedChange(t, task.Destination, "unintegrated-progress")
	current := gitFixture(t, task.Destination, "rev-parse", "HEAD")
	o := V3PlanOptions{Path: source, Base: "HEAD", StateDir: creation.StateDir, Task: task.TaskID, Owner: task.Owner, RemoveRegistration: true, RemoveFiles: task.Destination, ProtectWorktrees: []string{}}
	observed, e := observeTidyV3Retirement(context.Background(), o, newTidyV3ProofBudget(false))
	if e != nil {
		t.Fatal("stable-owned unintegrated progress should be retained", e)
	}
	if observed.Retirement.Target.CommitID != current || observed.Retirement.Integration.OID != current || observed.Retirement.Integration.Decision != "retain" || v3RetirementEligible(observed.Retirement) {
		t.Fatal("unintegrated current head became eligible")
	}
	if observed.Retirement.Provenance.Owned.Task.Native.CommitID != task.Native.CommitID {
		t.Fatal("original creation snapshot rewritten")
	}
}

func TestTidyV3ReadOnlyOwnedOriginalFactsCannotBeSubstituted(t *testing.T) {
	for _, kind := range []string{"format", "registration", "physical", "journal-digest", "original-snapshot"} {
		t.Run(kind, func(t *testing.T) {
			source, creation, task := gitRetirementFixture(t)
			o := V3PlanOptions{Path: source, Base: "HEAD", StateDir: creation.StateDir, Task: task.TaskID, Owner: task.Owner, RemoveRegistration: true, RemoveFiles: task.Destination, ProtectWorktrees: []string{}}
			owned, e := readTidyV3OwnedProvenance(o)
			if e != nil {
				t.Fatal(e)
			}
			switch kind {
			case "format":
				owned.Task.Native.ObjectFormat = "sha256"
				owned.Task.Native.CommitID = strings.Repeat("a", 64)
			case "registration":
				owned.Task.Native.GitDir = filepath.Join(filepath.Dir(task.Native.GitDir), "foreign")
			case "physical":
				owned.Task.DestinationIdentity.Inode++
			case "journal-digest":
				owned.CreationJournalDigest = strings.Repeat("f", 64)
			case "original-snapshot":
				owned.Task.Native.CommitID = strings.Repeat("a", 40)
			}
			observer := tidyV3LegacyObserver{source: observeTidyV3Git, target: gitNativeIdentity}
			if _, e = observer.observeWithOwnership(context.Background(), o, newTidyV3ProofBudget(false), &owned); e == nil {
				t.Fatal("substituted original fact accepted", kind)
			}
			if _, e = os.Stat(task.Destination); e != nil {
				t.Fatal("read-only original-fact test removed target")
			}
		})
	}
}
