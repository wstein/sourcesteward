// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func pruneV4GuardOptions(p RegistrationPruneV4Plan) RegistrationPruneV4ApplyOptions {
	return RegistrationPruneV4ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Mode: p.Mode, Authorize: true}
}
func pruneV4GuardReason(t *testing.T, r RegistrationPruneV4Result, status, code string) {
	t.Helper()
	if r.Status != status || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != code {
		t.Fatalf("want %s/%s, got %+v", status, code, r)
	}
}
func pruneV4GuardJournal(t *testing.T, p RegistrationPruneV4Plan) pruneV4Journal {
	t.Helper()
	b, e := os.ReadFile(filepath.Join(p.StateDir, pruneV4Namespace, "journals", p.PlanID+".json"))
	pruneMust(t, e)
	var j pruneV4Journal
	pruneMust(t, json.Unmarshal(b, &j))
	return j
}
func pruneV4GuardApply(t *testing.T, p RegistrationPruneV4Plan) pruneV4Journal {
	t.Helper()
	if r := ApplyRegistrationPruneV4(context.Background(), pruneV4GuardOptions(p)); r.Status != "ok" {
		t.Fatalf("apply: %+v", r)
	}
	return pruneV4GuardJournal(t, p)
}
func pruneV4GuardStores(t *testing.T, p RegistrationPruneV4Plan) (*store, *store) {
	t.Helper()
	parent, e := openStoreKinds(p.StateDir, true, []string{pruneV4Namespace})
	pruneMust(t, e)
	t.Cleanup(parent.Close)
	s, e := openStoreKinds(filepath.Join(p.StateDir, pruneV4Namespace), true, []string{"plans", "journals", "payloads"})
	pruneMust(t, e)
	t.Cleanup(s.Close)
	return s, parent
}
func pruneV4GuardFiles(t *testing.T, root string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	pruneMust(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() {
			b, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			rel, e := filepath.Rel(root, path)
			if e != nil {
				return e
			}
			out[rel] = b
		}
		return nil
	}))
	return out
}
func pruneV4GuardNoRemovals(t *testing.T, p RegistrationPruneV4Plan) {
	t.Helper()
	j := pruneV4GuardJournal(t, p)
	for _, e := range j.Effects {
		if e.Action == "remove-metadata" && e.Outcome == "observed" {
			t.Fatalf("unexpected removal: %+v", e)
		}
	}
	for _, m := range p.Expected.Metadata {
		if m.Kind == "file" {
			b, e := os.ReadFile(filepath.Join(p.Registration.GitDir, m.Path))
			if e != nil || gitDigest(string(b)) != m.Digest {
				t.Fatalf("original metadata %s changed: %v", m.Path, e)
			}
		}
	}
}

func TestRegistrationPruneV4GuardOtherOwnerRequiresFreshConfirmation(t *testing.T) {
	opts, own := fixturePruneV4(t)
	opts.Mode.Mode = "human"
	opts.Mode.Caller = "another-human"
	planned := 0
	opts.Confirm = func(owner, id string) error {
		planned++
		if owner != own.Registration.Owner || id == "" {
			t.Fatal("incorrect confirmation identity")
		}
		return nil
	}
	result := PlanRegistrationPruneV4(context.Background(), opts)
	if result.Status != "ok" || result.Plan == nil || result.Plan.Registration.Scope != "other" || planned != 1 {
		t.Fatalf("other-owner plan: %+v confirmations=%d", result, planned)
	}
	p := *result.Plan
	before := pruneV4GuardFiles(t, p.Registration.GitDir)
	apply := pruneV4GuardOptions(p)
	pruneV4GuardReason(t, ApplyRegistrationPruneV4(context.Background(), apply), "refused", "prune-scope-refused")
	calls := 0
	apply.Confirm = func(owner, id string) error {
		calls++
		if owner != p.Registration.Owner || id != p.PlanID {
			t.Fatal("apply confirmation not bound to stored plan")
		}
		return errors.New("typed confirmation refused")
	}
	pruneV4GuardReason(t, ApplyRegistrationPruneV4(context.Background(), apply), "refused", "prune-scope-refused")
	if calls != 1 || !reflect.DeepEqual(before, pruneV4GuardFiles(t, p.Registration.GitDir)) {
		t.Fatal("confirmation refusal changed registration")
	}
	if _, e := os.Stat(p.PayloadPath); !os.IsNotExist(e) {
		t.Fatalf("confirmation refusal created recovery: %v", e)
	}
	apply.Confirm = func(owner, id string) error {
		calls++
		if owner != p.Registration.Owner || id != p.PlanID {
			t.Fatal("incorrect accepted confirmation identity")
		}
		return nil
	}
	if r := ApplyRegistrationPruneV4(context.Background(), apply); r.Status != "ok" || calls != 2 {
		t.Fatalf("confirmed apply: %+v calls=%d", r, calls)
	}
}

func TestRegistrationPruneV4GuardCompletedReplayVerifiesCurrentState(t *testing.T) {
	for _, kind := range []string{"registration", "pin-deleted", "pin-moved", "payload", "destination"} {
		t.Run(kind, func(t *testing.T) {
			_, p := fixturePruneV4(t)
			pruneV4GuardApply(t, p)
			code := "prune-postcondition-refused"
			switch kind {
			case "registration":
				pruneMust(t, os.Mkdir(p.Registration.GitDir, 0700))
			case "pin-deleted":
				gitFixture(t, p.SourcePath, "update-ref", "-d", pruneV4HistoryRef(p))
			case "pin-moved":
				gitFixture(t, p.SourcePath, "update-ref", pruneV4HistoryRef(p), gitFixture(t, p.SourcePath, "rev-parse", "HEAD^"))
			case "payload":
				pruneMust(t, os.WriteFile(filepath.Join(p.PayloadPath, "files", "index"), []byte("tampered"), 0600))
				code = "prune-recovery-refused"
			case "destination":
				pruneMust(t, os.Mkdir(p.Registration.Destination, 0700))
				code = "prune-destination-present"
			}
			before := pruneV4GuardJournal(t, p)
			pruneV4GuardReason(t, ApplyRegistrationPruneV4(context.Background(), pruneV4GuardOptions(p)), "refused", code)
			if !reflect.DeepEqual(before, pruneV4GuardJournal(t, p)) {
				t.Fatal("replay rewrote completed journal")
			}
		})
	}
}

func TestRegistrationPruneV4GuardPostNativeAbsenceAndSiblingChecks(t *testing.T) {
	for _, kind := range []string{"native", "absence", "sibling"} {
		t.Run(kind, func(t *testing.T) {
			_, p := fixturePruneV4(t)
			j := pruneV4GuardApply(t, p)
			expected := clonePruneV4(t, p).Expected
			code := "prune-postcondition-refused"
			switch kind {
			case "native":
				gitFixture(t, p.SourcePath, "update-ref", "refs/heads/unrelated-after-removal", p.Expected.ResolvedHEAD)
			case "absence":
				pruneMust(t, os.Mkdir(p.Registration.Destination, 0700))
				code = "prune-destination-present"
			case "sibling":
				expected.ProtectedSiblings = append(expected.ProtectedSiblings, PruneV4Sibling{Name: "unexplained-missing-sibling", Identity: PathIdentity{Path: filepath.Join(filepath.Dir(p.Registration.GitDir), "unexplained-missing-sibling"), Device: 1, Inode: 2}})
			}
			assertPruneReason(t, verifyPruneV4Post(context.Background(), p, expected, j), code, "")
		})
	}
}

func TestRegistrationPruneV4GuardBoundaryWitnesses(t *testing.T) {
	for _, kind := range []string{"absence", "ownership-digest", "file-digest", "file-identity", "sibling"} {
		t.Run(kind, func(t *testing.T) {
			_, p := fixturePruneV4(t)
			s, parent := pruneV4GuardStores(t, p)
			expected := clonePruneV4(t, p).Expected
			code := "prune-evidence-changed"
			switch kind {
			case "absence":
				pruneMust(t, os.Mkdir(p.Registration.Destination, 0700))
				code = "prune-destination-present"
			case "ownership-digest":
				p.Expected.TaskDigest = strings.Repeat("0", 64)
				code = "prune-ownership-refused"
			case "file-digest", "file-identity":
				for i, m := range p.Expected.Metadata {
					if m.Path == "index" {
						if kind == "file-digest" {
							p.Expected.Metadata[i].Digest = strings.Repeat("0", 64)
						} else {
							p.Expected.Metadata[i].Physical.Inode++
						}
					}
				}
			case "sibling":
				expected.ProtectedSiblings = append(expected.ProtectedSiblings, PruneV4Sibling{Name: "missing-sibling"})
			}
			assertPruneReason(t, validatePruneV4RemovalBoundary(context.Background(), p, expected, map[string]bool{}, s, parent), code, "")
		})
	}
}

func TestRegistrationPruneV4GuardHeldRegistrationIdentity(t *testing.T) {
	_, p := fixturePruneV4(t)
	expected := clonePruneV4(t, p).Expected
	expected.Registration.Inode++
	// The descriptor must be rejected before a hook, journal intent, or payload read.
	s, parent := pruneV4GuardStores(t, p)
	reached := false
	ctx := pruneHookContext(func(stage, _ string, _ any) {
		if stage == "v4-before-unlink" {
			reached = true
		}
	})
	j := pruneV4Journal{}
	assertPruneReason(t, removePruneV4Metadata(ctx, p, expected, &j, s, parent), "prune-evidence-changed", "Registration identity changed before removal.")
	if reached || len(j.Effects) != 0 {
		t.Fatal("mismatched held descriptor reached removal")
	}
}

func TestRegistrationPruneV4GuardHistoryPinRequiresAbsentName(t *testing.T) {
	_, p := fixturePruneV4(t)
	old := gitFixture(t, p.SourcePath, "rev-parse", "HEAD^")
	ref := pruneV4HistoryRef(p)
	gitFixture(t, p.SourcePath, "update-ref", ref, old)
	assertPruneReason(t, pinPruneV4History(context.Background(), p), "recovery-unconfirmed", "")
	if got := gitFixture(t, p.SourcePath, "rev-parse", ref); got != old {
		t.Fatalf("existing recovery pin overwritten: %s", got)
	}
	if _, e := os.Stat(p.Registration.GitDir); e != nil {
		t.Fatal("registration changed", e)
	}
}

func TestRegistrationPruneV4GuardJournalBindsOIDAndConfirmedOutcomes(t *testing.T) {
	for _, kind := range []string{"history-oid", "last-outcome"} {
		t.Run(kind, func(t *testing.T) {
			_, p := fixturePruneV4(t)
			j := pruneV4GuardApply(t, p)
			if kind == "history-oid" {
				j.HistoryOID = gitFixture(t, p.SourcePath, "rev-parse", "HEAD^")
			} else {
				j.Effects[len(j.Effects)-1].Outcome = "unknown"
			}
			b, e := pruneV4Canonical(j, "")
			pruneMust(t, e)
			pruneMust(t, os.WriteFile(filepath.Join(p.StateDir, pruneV4Namespace, "journals", p.PlanID+".json"), append(b, '\n'), 0600))
			pruneV4GuardReason(t, ApplyRegistrationPruneV4(context.Background(), pruneV4GuardOptions(p)), "refused", "prune-journal-refused")
		})
	}
}

func TestRegistrationPruneV4GuardLockIsPenultimateEffect(t *testing.T) {
	_, p := fixturePruneV4(t)
	j := pruneV4GuardApply(t, p)
	n := len(j.Effects)
	if n < 4 || j.Effects[n-2].Action != "remove-metadata" || j.Effects[n-2].Target != "locked" || j.Effects[n-1].Target != "." {
		t.Fatalf("lock/root removal order: %+v", j.Effects)
	}
	for _, e := range j.Effects {
		if e.Outcome != "observed" {
			t.Fatalf("unconfirmed complete effect: %+v", e)
		}
	}
}

func TestRegistrationPruneV4GuardPayloadRootMustStayPrivate(t *testing.T) {
	_, p := fixturePruneV4(t)
	pruneV4GuardApply(t, p)
	before := pruneV4GuardFiles(t, p.PayloadPath)
	pruneMust(t, os.Chmod(p.PayloadPath, 0750))
	_, _, e := readPruneV4Payload(context.Background(), p)
	assertPruneReason(t, e, "prune-recovery-refused", "")
	if !reflect.DeepEqual(before, pruneV4GuardFiles(t, p.PayloadPath)) {
		t.Fatal("validation changed payload")
	}
}

func TestRegistrationPruneV4GuardAfterIntentRechecksBeforeEffect(t *testing.T) {
	_, p := fixturePruneV4(t)
	fired := false
	ctx := pruneHookContext(func(stage, path string, _ any) {
		if fired || stage != "v4-after-unlink-intent" {
			return
		}
		fired = true
		j := pruneV4GuardJournal(t, p)
		last := j.Effects[len(j.Effects)-1]
		if last.Target != path || last.Outcome != "unknown" {
			t.Fatalf("hook is not after durable intent: %+v", last)
		}
		pruneMust(t, os.Mkdir(p.Registration.Destination, 0700))
	})
	r := ApplyRegistrationPruneV4(ctx, pruneV4GuardOptions(p))
	if !fired {
		t.Fatal("post-intent observation not reached")
	}
	pruneV4GuardReason(t, r, "partial", "prune-destination-present")
	pruneV4GuardNoRemovals(t, p)
}

func TestRegistrationPruneV4GuardPreservesSiblingBytesAndUnrelatedRefs(t *testing.T) {
	opts, _ := fixturePruneV4(t)
	sibling := filepath.Join(gitTempDir(t), "keep")
	gitFixture(t, opts.Path, "worktree", "add", "--detach", sibling, "HEAD")
	gitFixture(t, opts.Path, "worktree", "lock", "--reason", "keep exact registration", sibling)
	ref := "refs/heads/unrelated-keep"
	gitFixture(t, opts.Path, "update-ref", ref, gitFixture(t, opts.Path, "rev-parse", "HEAD^"))
	r := PlanRegistrationPruneV4(context.Background(), opts)
	if r.Status != "ok" || r.Plan == nil {
		t.Fatalf("plan with sibling: %+v", r)
	}
	p := *r.Plan
	siblingReg := filepath.Join(filepath.Dir(p.Registration.GitDir), "keep")
	before := pruneV4GuardFiles(t, siblingReg)
	refs := gitFixture(t, p.SourcePath, "for-each-ref", "--format=%(refname) %(objectname)")
	pruneV4GuardApply(t, p)
	if !reflect.DeepEqual(before, pruneV4GuardFiles(t, siblingReg)) {
		t.Fatal("protected registration bytes changed")
	}
	after := gitFixture(t, p.SourcePath, "for-each-ref", "--format=%(refname) %(objectname)")
	after = strings.TrimSpace(strings.ReplaceAll(after, pruneV4HistoryRef(p)+" "+p.Expected.ResolvedHEAD, ""))
	if !bytes.Equal([]byte(refs), []byte(after)) {
		t.Fatalf("unrelated refs changed:\nbefore %s\nafter %s", refs, after)
	}
}

func TestRegistrationPruneV4GuardMidRemovalStopsAtChangedEvidence(t *testing.T) {
	for _, kind := range []string{"ownership", "file", "destination", "sibling"} {
		t.Run(kind, func(t *testing.T) {
			opts, p := fixturePruneV4(t)
			siblingReg := ""
			if kind == "sibling" {
				sibling := filepath.Join(gitTempDir(t), "keep")
				gitFixture(t, p.SourcePath, "worktree", "add", "--detach", sibling, "HEAD")
				gitFixture(t, p.SourcePath, "worktree", "lock", "--reason", "keep", sibling)
				planned := PlanRegistrationPruneV4(context.Background(), opts)
				if planned.Status != "ok" || planned.Plan == nil {
					t.Fatalf("sibling plan: %+v", planned)
				}
				p = *planned.Plan
				siblingReg = filepath.Join(filepath.Dir(p.Registration.GitDir), "keep")
			}
			calls := 0
			target := ""
			var original []byte
			ctx := pruneHookContext(func(stage, path string, _ any) {
				if stage != "v4-before-unlink" {
					return
				}
				calls++
				if calls != 2 {
					return
				}
				target = path
				var e error
				switch kind {
				case "ownership":
					taskPath := filepath.Join(p.StateDir, "tasks", p.Registration.Task+".json")
					b, err := os.ReadFile(taskPath)
					pruneMust(t, err)
					var task TaskRecord
					pruneMust(t, json.Unmarshal(b, &task))
					task.RecoveryLimits = append(task.RecoveryLimits, "Changed but valid original ownership evidence.")
					b, err = json.Marshal(task)
					pruneMust(t, err)
					pruneMust(t, os.WriteFile(taskPath, append(b, '\n'), 0600))
				case "file":
					file := filepath.Join(p.Registration.GitDir, "index")
					original, e = os.ReadFile(file)
					pruneMust(t, e)
					changed := append([]byte{}, original...)
					changed[len(changed)-1] ^= 1
					pruneMust(t, os.WriteFile(file, changed, 0600))
				case "destination":
					pruneMust(t, os.Mkdir(p.Registration.Destination, 0700))
				case "sibling":
					saved := filepath.Join(gitTempDir(t), "saved-registration")
					pruneMust(t, os.Rename(siblingReg, saved))
					pruneMust(t, os.CopyFS(siblingReg, os.DirFS(saved)))
				}
			})
			r := ApplyRegistrationPruneV4(ctx, pruneV4GuardOptions(p))
			code := "prune-evidence-changed"
			if kind == "ownership" {
				code = "prune-ownership-refused"
			}
			if kind == "destination" {
				code = "prune-destination-present"
			}
			pruneV4GuardReason(t, r, "partial", code)
			if calls != 2 {
				t.Fatalf("did not stop at second effect: %d", calls)
			}
			if _, e := os.Lstat(filepath.Join(p.Registration.GitDir, target)); e != nil {
				t.Fatalf("next removal occurred despite changed evidence: %v", e)
			}
			j := pruneV4GuardJournal(t, p)
			removed := 0
			for _, e := range j.Effects {
				if e.Action == "remove-metadata" && e.Outcome == "observed" {
					removed++
				}
			}
			if removed != 1 {
				t.Fatalf("recorded removals=%d, want 1", removed)
			}
			if _, _, e := readPruneV4Payload(context.Background(), p); e != nil {
				t.Fatalf("recovery lost: %v", e)
			}
			if kind == "file" {
				got, e := os.ReadFile(filepath.Join(p.PayloadPath, "files", "index"))
				if e != nil || !bytes.Equal(got, original) {
					t.Fatalf("original file recovery changed: %v", e)
				}
			}
		})
	}
}

func TestRegistrationPruneV4GuardVerifiedSiblingChainRemovesOnlyExplainedWitness(t *testing.T) {
	opts, original := fixturePruneV4(t)
	sibling := filepath.Join(gitTempDir(t), "retire-sibling")
	gitFixture(t, opts.Path, "worktree", "add", "--detach", sibling, "HEAD")
	// V3 requires every locked workspace root to exist during its own operation.
	// Restore the moved fixture for its plan, then leave an empty root only while
	// that sibling retires. V4 still plans and applies with its selected root absent.
	pruneMust(t, os.Rename(original.Registration.Destination+"-moved", original.Registration.Destination))
	c := defaultTidyV3RetirementCoordinator()
	siblingPlan := c.plan(context.Background(), V3PlanOptions{Path: opts.Path, Base: "HEAD", StateDir: opts.StateDir, CompletedWorktree: sibling, Owner: "operator", RemoveRegistration: true, RemoveFiles: sibling, ProtectWorktrees: []string{}})
	if siblingPlan.Status != "ok" || siblingPlan.Plan == nil || !v3RetirementEligible(*siblingPlan.Plan.Retirement) {
		t.Fatalf("sibling retirement plan: %+v", siblingPlan)
	}
	pruneMust(t, os.Rename(original.Registration.Destination, original.Registration.Destination+"-moved"))
	result := PlanRegistrationPruneV4(context.Background(), opts)
	if result.Status != "ok" || result.Plan == nil {
		t.Fatalf("prune plan: %+v", result)
	}
	p := *result.Plan
	pruneMust(t, os.Mkdir(p.Registration.Destination, 0700))
	retired := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: siblingPlan.Plan.PlanID, Authorize: true, CompletedWorktree: sibling, Owner: "operator", ProtectWorktrees: []string{}})
	if retired.Status != "ok" {
		t.Fatalf("sibling retirement: %+v", retired)
	}
	pruneMust(t, os.Remove(p.Registration.Destination))
	apply := pruneV4GuardOptions(p)
	apply.SiblingPlans = []string{siblingPlan.Plan.PlanID}
	if r := ApplyRegistrationPruneV4(context.Background(), apply); r.Status != "ok" {
		t.Fatalf("verified sibling delta: %+v", r)
	}
	if r := ApplyRegistrationPruneV4(context.Background(), apply); r.Status != "ok" {
		t.Fatalf("verified chain replay: %+v", r)
	}
}
