// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestTidyV3LegacyNativeRemovalPreservesBranchAndRecovery(t *testing.T) {
	o, _ := tidyV3LegacyFixture(t, false)
	o.StateDir = privateDir(t)
	c := defaultTidyV3RetirementCoordinator()
	planned := c.plan(context.Background(), o)
	if planned.Status != "ok" || planned.Plan == nil {
		t.Fatalf("native retirement plan unavailable: status=%s diagnostics=%+v", planned.Status, planned.Diagnostics)
	}
	p := *planned.Plan
	if planned.Boundary.Budget.Observations != 3 || planned.Boundary.Budget.NativeProcesses != 3 {
		t.Fatalf("plan counts: %+v", planned.Boundary.Budget)
	}
	remove := c.remove
	calls := 0
	c.remove = func(ctx context.Context, bin string, args []string, cwd string, wr, ro []string) (string, int, error) {
		calls++
		if !reflect.DeepEqual(args, []string{"--no-replace-objects", "worktree", "remove", "--", o.CompletedWorktree}) {
			t.Fatalf("unsafe native removal argv: %q", args)
		}
		return remove(ctx, bin, args, cwd, wr, ro)
	}
	applied := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, CompletedWorktree: o.CompletedWorktree, Owner: o.Owner, ProtectWorktrees: o.ProtectWorktrees})
	if applied.Status != "ok" || applied.Plan == nil {
		t.Fatalf("native retirement missing: status=%s diagnostics=%+v", applied.Status, applied.Diagnostics)
	}
	if calls != 1 || applied.Boundary.Budget.Observations != 8 || applied.Boundary.Budget.NativeProcesses != 8 {
		t.Fatalf("native calls=%d apply counts=%+v", calls, applied.Boundary.Budget)
	}
	if err := validateV3Result(applied); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{o.CompletedWorktree, p.Retirement.Target.GitDir} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("selected native path remains: %s %v", path, err)
		}
	}
	if got := gitFixture(t, o.Path, "rev-parse", p.Retirement.Target.HeadRef); got != p.Retirement.Target.CommitID {
		t.Fatal("branch changed")
	}
	recovery := v3RecoveryFor(p, p.Retirement.ActionID, p.Retirement.Target.CommitID)
	if got := gitFixture(t, o.Path, "rev-parse", recovery.Name); got != recovery.CommitID {
		t.Fatal("recovery missing")
	}
	j := readLegacyRetirementJournal(t, p)
	if j.Phase != "complete" || j.Entries[0].RegistrationOutcome != "complete" || j.Entries[0].FilesOutcome != "complete" {
		t.Fatalf("bad journal: %+v", j)
	}
	before := layoutProtectedFileAt(t, p.StateDir+"/tidy-v3-operations/"+p.PlanID+".json")
	retry := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, CompletedWorktree: o.CompletedWorktree, Owner: o.Owner, ProtectWorktrees: o.ProtectWorktrees})
	if retry.Status != "ok" || calls != 1 {
		t.Fatalf("retry replayed or failed: %+v calls%d", retry, calls)
	}
	if after := layoutProtectedFileAt(t, p.StateDir+"/tidy-v3-operations/"+p.PlanID+".json"); !reflect.DeepEqual(before, after) {
		t.Fatal("retry changed journal")
	}
}

func readLegacyRetirementJournal(t *testing.T, p V3Plan) V3Journal {
	t.Helper()
	s, e := openV3Store(p.StateDir, false, p.Retirement.RegisteredRoots, p.Retirement.ProtectedRoots)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	j, e := s.readJournal(p)
	if e != nil {
		t.Fatal(e)
	}
	return j
}

func TestTidyV3LegacyRemovalLostResponseRetainsUncertainJournal(t *testing.T) {
	o, _ := tidyV3LegacyFixture(t, false)
	o.StateDir = privateDir(t)
	c := defaultTidyV3RetirementCoordinator()
	planned := c.plan(context.Background(), o)
	if planned.Plan == nil {
		t.Fatal(planned.Diagnostics)
	}
	p := *planned.Plan
	remove := c.remove
	calls := 0
	c.remove = func(ctx context.Context, bin string, args []string, cwd string, wr, ro []string) (string, int, error) {
		calls++
		out, code, e := remove(ctx, bin, args, cwd, wr, ro)
		if e != nil || code != 0 {
			t.Fatalf("native fixture removal failed %v %d", e, code)
		}
		return out, code, errors.New("lost native completion response")
	}
	opts := V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, CompletedWorktree: o.CompletedWorktree, Owner: o.Owner, ProtectWorktrees: o.ProtectWorktrees}
	r := c.apply(context.Background(), opts)
	if r.Status != "partial" || r.Plan != nil || calls != 1 {
		t.Fatalf("uncertain removal mislabeled: %+v", r)
	}
	recoveryAction := false
	for _, action := range r.Actions {
		if action.Name == "recovery:"+v3RecoveryFor(p, p.Retirement.ActionID, p.Retirement.Target.CommitID).Name && action.Status == "complete" {
			recoveryAction = true
		}
	}
	if !recoveryAction {
		t.Fatal("confirmed recovery omitted from partial result actions", r.Actions)
	}
	j := readLegacyRetirementJournal(t, p)
	if j.Phase != "incomplete" || !j.Entries[0].RemovalAttempted || j.Entries[0].Recovery == nil || !containsString(j.KnownEffects, "possible-registration-removal:"+p.Retirement.ActionID) || !containsString(j.KnownEffects, "possible-files-removal:"+p.Retirement.ActionID) {
		t.Fatalf("uncertain outcomes hidden %+v", j)
	}
	if retry := c.apply(context.Background(), opts); retry.Status != "partial" || retry.Plan != nil || calls != 1 {
		t.Fatal("uncertain operation replayed", retry, calls)
	}
}

func TestTidyV3LegacyRetirementDurabilityFaultsNeverReplay(t *testing.T) {
	for fault := 1; fault <= 8; fault++ {
		t.Run(fmt.Sprint(fault), func(t *testing.T) {
			parallelFixture(t)
			o, _ := tidyV3LegacyFixture(t, false)
			o.StateDir = privateDir(t)
			c := defaultTidyV3RetirementCoordinator()
			planned := c.plan(context.Background(), o)
			if planned.Plan == nil {
				t.Fatal(planned.Diagnostics)
			}
			p := *planned.Plan
			open := c.openStore
			writes, removes := 0, 0
			c.openStore = func(path string, write bool, registered, protected []string) (*v3Store, error) {
				s, e := open(path, write, registered, protected)
				if e != nil {
					return nil, e
				}
				persist := s.writeRecord
				s.writeRecord = func(kind, id string, v any, exclusive bool) error {
					writes++
					if writes == fault {
						return errors.New("fixture durable write refusal")
					}
					return persist(kind, id, v, exclusive)
				}
				return s, nil
			}
			remove := c.remove
			c.remove = func(ctx context.Context, bin string, args []string, cwd string, wr, ro []string) (string, int, error) {
				removes++
				return remove(ctx, bin, args, cwd, wr, ro)
			}
			opts := V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, CompletedWorktree: o.CompletedWorktree, Owner: o.Owner, ProtectWorktrees: o.ProtectWorktrees}
			r := c.apply(context.Background(), opts)
			if r.Status == "ok" || r.Plan != nil || writes < fault {
				t.Fatalf("durability fault accepted%d %+v writes%d", fault, r, writes)
			}
			if removes > 1 || fault <= 4 && removes != 0 {
				t.Fatalf("native effect crossed failed durability boundary%d removes%d", fault, removes)
			}
			if fault > 1 {
				before := removes
				retry := c.apply(context.Background(), opts)
				if retry.Status == "ok" || retry.Plan != nil || removes != before {
					t.Fatal("incomplete durable operation replayed")
				}
			}
		})
	}
}

func TestTidyV3LegacyCancellationAfterRecoveryRetainsActorData(t *testing.T) {
	o, _ := tidyV3LegacyFixture(t, false)
	o.StateDir = privateDir(t)
	c := defaultTidyV3RetirementCoordinator()
	planned := c.plan(context.Background(), o)
	if planned.Plan == nil {
		t.Fatal(planned.Diagnostics)
	}
	p := *planned.Plan
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pin := c.pin
	c.pin = func(ctx context.Context, bin string, id NativeIdentity, r TidyRecovery, ro []string) (bool, error) {
		attempted, e := pin(ctx, bin, id, r, ro)
		if e == nil {
			cancel()
		}
		return attempted, e
	}
	r := c.apply(ctx, V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, CompletedWorktree: o.CompletedWorktree, Owner: o.Owner, ProtectWorktrees: o.ProtectWorktrees})
	if r.Status != "partial" || r.Plan != nil {
		t.Fatal("canceled after recovery became success", r)
	}
	if _, e := os.Stat(o.CompletedWorktree); e != nil {
		t.Fatal("canceled retirement removed files")
	}
	recovery := v3RecoveryFor(p, p.Retirement.ActionID, p.Retirement.Target.CommitID)
	if got := gitFixture(t, o.Path, "rev-parse", recovery.Name); got != recovery.CommitID {
		t.Fatal("recovery lost")
	}
	if j := readLegacyRetirementJournal(t, p); j.Phase != "incomplete" {
		t.Fatal("canceled operation replayable")
	}
}

func TestTidyV3LegacyCompletedRetryRejectsReplacedDestinationParent(t *testing.T) {
	o, _ := tidyV3LegacyFixture(t, false)
	o.StateDir = privateDir(t)
	c := defaultTidyV3RetirementCoordinator()
	planned := c.plan(context.Background(), o)
	if planned.Plan == nil {
		t.Fatal(planned.Diagnostics)
	}
	p := *planned.Plan
	opts := V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, CompletedWorktree: o.CompletedWorktree, Owner: o.Owner, ProtectWorktrees: o.ProtectWorktrees}
	if r := c.apply(context.Background(), opts); r.Status != "ok" {
		t.Fatal(r.Diagnostics)
	}
	parent := p.Retirement.Parent.Path
	if e := os.Rename(parent, parent+"-actor-original"); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = os.RemoveAll(parent + "-actor-original") })
	if e := os.Mkdir(parent, 0700); e != nil {
		t.Fatal(e)
	}
	if retry := c.apply(context.Background(), opts); retry.Status == "ok" || retry.Plan != nil {
		t.Fatal("completed retry accepted replacement parent")
	}
}

func TestTidyV3LegacyNativeNoForceRejectsLateDirtyOrLockedTarget(t *testing.T) {
	for _, kind := range []string{"tracked", "untracked", "locked"} {
		t.Run(kind, func(t *testing.T) {
			o, _ := tidyV3LegacyFixture(t, false)
			o.StateDir = privateDir(t)
			c := defaultTidyV3RetirementCoordinator()
			planned := c.plan(context.Background(), o)
			if planned.Plan == nil {
				t.Fatal(planned.Diagnostics)
			}
			p := *planned.Plan
			remove := c.remove
			calls := 0
			c.remove = func(ctx context.Context, bin string, args []string, cwd string, wr, ro []string) (string, int, error) {
				calls++
				switch kind {
				case "tracked":
					if e := os.WriteFile(filepath.Join(o.CompletedWorktree, "file"), []byte("late actor\n"), 0600); e != nil {
						t.Fatal(e)
					}
				case "untracked":
					if e := os.WriteFile(filepath.Join(o.CompletedWorktree, "late-user"), []byte("late actor\n"), 0600); e != nil {
						t.Fatal(e)
					}
				case "locked":
					gitFixture(t, o.Path, "worktree", "lock", "--reason", "late actor", o.CompletedWorktree)
				}
				return remove(ctx, bin, args, cwd, wr, ro)
			}
			r := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, CompletedWorktree: o.CompletedWorktree, Owner: o.Owner, ProtectWorktrees: o.ProtectWorktrees})
			if r.Status != "partial" || r.Plan != nil || calls != 1 {
				t.Fatalf("late native guard failure hidden: %+v calls%d", r, calls)
			}
			if _, e := os.Stat(o.CompletedWorktree); e != nil {
				t.Fatal("late actor files removed")
			}
			if got := gitFixture(t, o.Path, "rev-parse", p.Retirement.Target.HeadRef); got != p.Retirement.Target.CommitID {
				t.Fatal("attached branch changed")
			}
			recovery := v3RecoveryFor(p, p.Retirement.ActionID, p.Retirement.Target.CommitID)
			if got := gitFixture(t, o.Path, "rev-parse", recovery.Name); got != recovery.CommitID {
				t.Fatal("late native failure lost recovery")
			}
		})
	}
}

func TestTidyV3LegacyPlanBindsRegistrationSiblings(t *testing.T) {
	o, _ := tidyV3LegacyFixture(t, false)
	o.StateDir = privateDir(t)
	c := defaultTidyV3RetirementCoordinator()
	planned := c.plan(context.Background(), o)
	if planned.Plan == nil {
		t.Fatal(planned.Diagnostics)
	}
	p := *planned.Plan
	actor := filepath.Join(filepath.Dir(p.Retirement.Registration.Path), "actor-sibling")
	if e := os.WriteFile(actor, []byte("new metadata parent sibling"), 0600); e != nil {
		t.Fatal(e)
	}
	r := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, CompletedWorktree: o.CompletedWorktree, Owner: o.Owner, ProtectWorktrees: o.ProtectWorktrees})
	if got, e := os.ReadFile(actor); e != nil || string(got) != "new metadata parent sibling" {
		t.Fatal("actor sibling bytes changed")
	}
	if r.Status == "ok" || r.Plan != nil {
		t.Fatal("changed registration sibling list accepted immutable plan")
	}
	if _, e := os.Stat(o.CompletedWorktree); e != nil {
		t.Fatal("stale plan removed target")
	}
}

func TestTidyV3LegacyApplyRequiresCurrentExactCompletionAndNamespace(t *testing.T) {
	for _, kind := range []string{"authorization", "completion", "owner", "new-protection", "files-path", "v2-namespace"} {
		t.Run(kind, func(t *testing.T) {
			o, _ := tidyV3LegacyFixture(t, false)
			o.StateDir = privateDir(t)
			c := defaultTidyV3RetirementCoordinator()
			planned := c.plan(context.Background(), o)
			if planned.Plan == nil {
				t.Fatal(planned.Diagnostics)
			}
			p := *planned.Plan
			opts := V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, CompletedWorktree: o.CompletedWorktree, Owner: o.Owner, ProtectWorktrees: append([]string{}, o.ProtectWorktrees...)}
			switch kind {
			case "authorization":
				opts.Authorize = false
			case "completion":
				opts.CompletedWorktree = ""
			case "owner":
				opts.Owner = "other"
			case "new-protection":
				opts.ProtectWorktrees = append(opts.ProtectWorktrees, o.CompletedWorktree)
				sort.Strings(opts.ProtectWorktrees)
			case "files-path":
				opts.CompletedWorktree = o.Path
			case "v2-namespace":
				dir := filepath.Join(p.StateDir, "tidy-retirement-plans")
				if e := os.Mkdir(dir, 0700); e != nil {
					t.Fatal(e)
				}
				if e := os.WriteFile(filepath.Join(dir, p.PlanID+".json"), []byte("actor v2 record"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			pins, removes := 0, 0
			c.pin = func(context.Context, string, NativeIdentity, TidyRecovery, []string) (bool, error) {
				pins++
				return false, errors.New("unexpected native recovery")
			}
			c.remove = func(context.Context, string, []string, string, []string, []string) (string, int, error) {
				removes++
				return "", 1, errors.New("unexpected native removal")
			}
			r := c.apply(context.Background(), opts)
			if r.Status == "ok" || r.Plan != nil || pins != 0 || removes != 0 {
				t.Fatalf("inexact current authority reached effects: %s %+v", kind, r)
			}
			if _, e := os.Stat(o.CompletedWorktree); e != nil {
				t.Fatal("target changed before authority")
			}
		})
	}
}

func TestTidyV3LegacyRemovalGrantsProtectRegularAndNativeSiblings(t *testing.T) {
	o, active := tidyV3LegacyFixture(t, false)
	o.StateDir = privateDir(t)
	sibling := filepath.Join(filepath.Dir(o.CompletedWorktree), "user-sibling.bin")
	if e := os.WriteFile(sibling, []byte("protected sibling\x00\xff"), 0600); e != nil {
		t.Fatal(e)
	}
	activeID, e := gitNativeIdentity(context.Background(), active)
	if e != nil {
		t.Fatal(e)
	}
	c := defaultTidyV3RetirementCoordinator()
	planned := c.plan(context.Background(), o)
	if planned.Plan == nil {
		t.Fatal(planned.Diagnostics)
	}
	p := *planned.Plan
	before := layoutProtectedFileAt(t, sibling)
	remove := c.remove
	calls := 0
	c.remove = func(ctx context.Context, bin string, args []string, cwd string, wr, ro []string) (string, int, error) {
		calls++
		expected := []string{p.Retirement.Parent.Path, filepath.Dir(p.Retirement.Registration.Path)}
		if !reflect.DeepEqual(wr, expected) || !containsString(ro, sibling) || !containsString(ro, activeID.GitDir) || containsString(ro, p.Retirement.Destination.Path) || containsString(ro, p.Retirement.Registration.Path) {
			t.Fatalf("incomplete parent/sibling grants wr%q ro%q", wr, ro)
		}
		return remove(ctx, bin, args, cwd, wr, ro)
	}
	r := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, CompletedWorktree: o.CompletedWorktree, Owner: o.Owner, ProtectWorktrees: o.ProtectWorktrees})
	if r.Status != "ok" || calls != 1 {
		t.Fatal("protected-grant native removal failed", r.Diagnostics)
	}
	if got := layoutProtectedFileAt(t, sibling); !reflect.DeepEqual(got, before) {
		t.Fatal("regular sibling changed")
	}
	if _, e := os.Stat(activeID.GitDir); e != nil {
		t.Fatal("active sibling registration removed")
	}
}

func TestTidyV3LegacyNativeRemovalKeepsCanonicalPrimaryLayouts(t *testing.T) {
	for _, layout := range []string{"ordinary", "separate", "submodule"} {
		for _, linked := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/linked-%t", layout, linked), func(t *testing.T) {
				parallelFixture(t)
				source, primary, common := fixtureGitLayout(t, layout, linked, true)
				target := filepath.Join(gitTempDir(t), "completed")
				gitFixture(t, source, "worktree", "add", "-q", "-b", "completed", target, "HEAD")
				protect := []string{}
				if linked {
					protect = append(protect, primary)
				}
				if e := os.WriteFile(filepath.Join(common, "info", "exclude"), []byte("ignored-cache\n"), 0600); e != nil {
					t.Fatal(e)
				}
				for path, bytes := range map[string]string{filepath.Join(primary, "user-plan.md"): "primary user plan", filepath.Join(primary, "ignored-cache"): "primary ignored cache"} {
					if e := os.WriteFile(path, []byte(bytes), 0600); e != nil {
						t.Fatal(e)
					}
				}
				preserved := layoutProtectedFiles(t, source, primary, common)
				doc := layoutProtectedFileAt(t, filepath.Join(primary, "user-plan.md"))
				cache := layoutProtectedFileAt(t, filepath.Join(primary, "ignored-cache"))
				c := defaultTidyV3RetirementCoordinator()
				o := V3PlanOptions{Path: source, Base: "HEAD", StateDir: privateDir(t), CompletedWorktree: target, Owner: "operator", RemoveRegistration: true, RemoveFiles: target, ProtectWorktrees: protect}
				planned := c.plan(context.Background(), o)
				if planned.Plan == nil {
					t.Fatal("canonical layout plan", planned.Diagnostics)
				}
				p := *planned.Plan
				for path := range preserved {
					if path == p.Retirement.Registration.Path || strings.HasPrefix(path, p.Retirement.Registration.Path+string(filepath.Separator)) {
						delete(preserved, path)
					}
				}
				applied := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, CompletedWorktree: target, Owner: o.Owner, ProtectWorktrees: protect})
				if applied.Status != "ok" || applied.Plan == nil {
					t.Fatal("canonical primary native retirement", applied.Diagnostics)
				}
				requireLayoutProtectedFiles(t, preserved)
				if got := layoutProtectedFileAt(t, filepath.Join(primary, "user-plan.md")); !reflect.DeepEqual(got, doc) {
					t.Fatal("primary user document changed")
				}
				if got := layoutProtectedFileAt(t, filepath.Join(primary, "ignored-cache")); !reflect.DeepEqual(got, cache) {
					t.Fatal("primary ignored cache changed")
				}
			})
		}
	}
}

func TestTidyV3RetainedRetirementCompletedRetryIsReadOnly(t *testing.T) {
	o, _ := tidyV3LegacyFixture(t, false)
	o.StateDir = privateDir(t)
	commitUnintegratedChange(t, o.CompletedWorktree, "not-integrated")
	c := defaultTidyV3RetirementCoordinator()
	planned := c.plan(context.Background(), o)
	if planned.Plan == nil {
		t.Fatal(planned.Diagnostics)
	}
	p := *planned.Plan
	if v3RetirementEligible(*p.Retirement) {
		t.Fatal("unintegrated target became eligible")
	}
	opts := V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, CompletedWorktree: o.CompletedWorktree, Owner: o.Owner, ProtectWorktrees: o.ProtectWorktrees}
	if applied := c.apply(context.Background(), opts); applied.Status != "ok" {
		t.Fatal("retained no-effect apply", applied.Diagnostics)
	}
	before := layoutProtectedFileAt(t, filepath.Join(p.StateDir, "tidy-v3-operations", p.PlanID+".json"))
	if retry := c.apply(context.Background(), opts); retry.Status != "ok" || retry.Plan == nil {
		t.Fatal("retained completed retry failed", retry.Diagnostics)
	}
	if after := layoutProtectedFileAt(t, filepath.Join(p.StateDir, "tidy-v3-operations", p.PlanID+".json")); !reflect.DeepEqual(after, before) {
		t.Fatal("retained retry rewrote journal")
	}
	if _, e := os.Stat(o.CompletedWorktree); e != nil {
		t.Fatal("retained target removed")
	}
}

func TestTidyV3RetirementSiblingFilesAreProtectionFactsNotPayload(t *testing.T) {
	o, _ := tidyV3LegacyFixture(t, false)
	o.StateDir = privateDir(t)
	id, e := gitNativeIdentity(context.Background(), o.CompletedWorktree)
	if e != nil {
		t.Fatal(e)
	}
	paths := []string{filepath.Join(filepath.Dir(o.CompletedWorktree), "large-user-sibling"), filepath.Join(filepath.Dir(id.GitDir), "large-registration-sibling")}
	before := map[string]layoutProtectedFile{}
	for _, path := range paths {
		f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		if e != nil {
			t.Fatal(e)
		}
		if e = f.Truncate(17 << 20); e != nil {
			t.Fatal(e)
		}
		if _, e = f.WriteAt([]byte("actor header"), 0); e != nil {
			t.Fatal(e)
		}
		if _, e = f.WriteAt([]byte("actor tail"), (17<<20)-10); e != nil {
			t.Fatal(e)
		}
		if e = f.Close(); e != nil {
			t.Fatal(e)
		}
		before[path] = layoutProtectedFileAt(t, path)
	}
	c := defaultTidyV3RetirementCoordinator()
	planned := c.plan(context.Background(), o)
	if planned.Plan == nil {
		t.Fatal("protected large sibling incorrectly treated as payload", planned.Diagnostics)
	}
	p := *planned.Plan
	if planned.Boundary.Budget.UserBytes != 9*7 || planned.Boundary.Budget.NativeBytes >= 1<<20 {
		t.Fatal("sibling contents entered proof ledger", planned.Boundary.Budget)
	}
	applied := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, CompletedWorktree: o.CompletedWorktree, Owner: o.Owner, ProtectWorktrees: o.ProtectWorktrees})
	if applied.Status != "ok" || applied.Boundary.Budget.UserBytes != 24*7 {
		t.Fatal("sibling protection blocked authorized removal or counted payload", applied.Diagnostics, applied.Boundary.Budget)
	}
	for path, want := range before {
		if got := layoutProtectedFileAt(t, path); !reflect.DeepEqual(got, want) {
			t.Fatal("large sibling bytes/identity changed", path)
		}
	}
}

func TestTidyV3LegacyFinalCurrentDirectoryRaceIsPartial(t *testing.T) {
	o, _ := tidyV3LegacyFixture(t, false)
	o.StateDir = privateDir(t)
	c := defaultTidyV3RetirementCoordinator()
	planned := c.plan(context.Background(), o)
	if planned.Plan == nil {
		t.Fatal(planned.Diagnostics)
	}
	p := *planned.Plan
	remove := c.remove
	c.remove = func(ctx context.Context, bin string, args []string, cwd string, wr, ro []string) (string, int, error) {
		t.Chdir(o.CompletedWorktree)
		return remove(ctx, bin, args, cwd, wr, ro)
	}
	r := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, CompletedWorktree: o.CompletedWorktree, Owner: o.Owner, ProtectWorktrees: o.ProtectWorktrees})
	if r.Status != "partial" || r.Plan != nil {
		t.Fatal("final current-directory race hidden", r.Diagnostics)
	}
}

func TestTidyV3OwnedProgressRecoveryPinsCurrentHeadAndPreservesCreationRecords(t *testing.T) {
	source, creation, task := gitRetirementFixture(t)
	originals := map[string]layoutProtectedFile{}
	for _, path := range []string{filepath.Join(creation.StateDir, "tasks", task.TaskID+".json"), filepath.Join(creation.StateDir, "plans", creation.PlanID+".json"), filepath.Join(creation.StateDir, "operations", creation.PlanID+".json")} {
		originals[path] = layoutProtectedFileAt(t, path)
	}
	gitFixture(t, task.Destination, "commit", "--allow-empty", "-qm", "normal owned progress before completion")
	current := gitFixture(t, task.Destination, "rev-parse", "HEAD")
	gitFixture(t, source, "merge", "--ff-only", current)
	c := defaultTidyV3RetirementCoordinator()
	o := V3PlanOptions{Path: source, Base: "HEAD", StateDir: creation.StateDir, Task: task.TaskID, Owner: task.Owner, RemoveRegistration: true, RemoveFiles: task.Destination, ProtectWorktrees: []string{}}
	planned := c.plan(context.Background(), o)
	if planned.Plan == nil {
		t.Fatal("current owned progress plan", planned.Diagnostics)
	}
	p := *planned.Plan
	if current == task.Native.CommitID || p.Retirement.Target.CommitID != current || p.Retirement.Integration.OID != current || p.Retirement.Provenance.Owned.Task.Native.CommitID != task.Native.CommitID {
		t.Fatal("creation/current identities conflated")
	}
	applied := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, ProtectWorktrees: []string{}})
	if applied.Status != "ok" || applied.Plan == nil {
		t.Fatal("current owned progress apply", applied.Diagnostics)
	}
	recovery := v3RecoveryFor(p, p.Retirement.ActionID, current)
	if got := gitFixture(t, source, "rev-parse", recovery.Name); got != current || got == task.Native.CommitID {
		t.Fatal("recovery pinned original creation instead of selected current head")
	}
	for path, want := range originals {
		if got := layoutProtectedFileAt(t, path); !reflect.DeepEqual(got, want) {
			t.Fatal("original creation records changed", path)
		}
	}
}

func TestTidyV3OwnedHeadChangeAfterPlanRefusesBeforeRecovery(t *testing.T) {
	source, creation, task := gitRetirementFixture(t)
	c := defaultTidyV3RetirementCoordinator()
	o := V3PlanOptions{Path: source, Base: "HEAD", StateDir: creation.StateDir, Task: task.TaskID, Owner: task.Owner, RemoveRegistration: true, RemoveFiles: task.Destination, ProtectWorktrees: []string{}}
	planned := c.plan(context.Background(), o)
	if planned.Plan == nil {
		t.Fatal(planned.Diagnostics)
	}
	p := *planned.Plan
	gitFixture(t, task.Destination, "commit", "--allow-empty", "-qm", "late actor task progress")
	current := gitFixture(t, task.Destination, "rev-parse", "HEAD")
	pins, removes := 0, 0
	c.pin = func(context.Context, string, NativeIdentity, TidyRecovery, []string) (bool, error) {
		pins++
		return false, errors.New("unexpected stale recovery")
	}
	c.ownedRemove = func(context.Context, string, string, string, []string, []string) (string, int, error) {
		removes++
		return "", 1, errors.New("unexpected stale owned removal")
	}
	r := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, ProtectWorktrees: []string{}})
	if r.Status == "ok" || r.Plan != nil || pins != 0 || removes != 0 {
		t.Fatal("current-head drift after plan reached effects", r.Diagnostics, pins, removes)
	}
	if got := gitFixture(t, task.Destination, "rev-parse", "HEAD"); got != current {
		t.Fatal("late task history changed")
	}
}

func TestTidyV3LegacyPlanBindsRegistrationSiblingIdentity(t *testing.T) {
	for _, kind := range []string{"mode", "replacement", "link", "same-size-write"} {
		t.Run(kind, func(t *testing.T) {
			o, _ := tidyV3LegacyFixture(t, false)
			o.StateDir = privateDir(t)
			id, e := gitNativeIdentity(context.Background(), o.CompletedWorktree)
			if e != nil {
				t.Fatal(e)
			}
			actor := filepath.Join(filepath.Dir(id.GitDir), "protected-regular-sibling")
			if e = os.WriteFile(actor, []byte("original"), 0600); e != nil {
				t.Fatal(e)
			}
			c := defaultTidyV3RetirementCoordinator()
			planned := c.plan(context.Background(), o)
			if planned.Plan == nil {
				t.Fatal(planned.Diagnostics)
			}
			p := *planned.Plan
			switch kind {
			case "mode":
				e = os.Chmod(actor, 0640)
			case "replacement":
				e = os.Rename(actor, actor+"-old")
				if e == nil {
					e = os.WriteFile(actor, []byte("replaced"), 0600)
				}
			case "link":
				e = os.Link(actor, actor+"-link")
			case "same-size-write":
				e = os.WriteFile(actor, []byte("modified"), 0600)
			}
			if e != nil {
				t.Fatal(e)
			}
			before := layoutProtectedFileAt(t, actor)
			r := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, CompletedWorktree: o.CompletedWorktree, Owner: o.Owner, ProtectWorktrees: o.ProtectWorktrees})
			if r.Status == "ok" || r.Plan != nil {
				t.Fatal("changed persisted sibling admitted", kind, r.Diagnostics)
			}
			if got := layoutProtectedFileAt(t, actor); !reflect.DeepEqual(got, before) {
				t.Fatal("actor sibling changed", kind)
			}
			if _, e := os.Stat(o.CompletedWorktree); e != nil {
				t.Fatal("stale sibling plan removed target")
			}
		})
	}
}
