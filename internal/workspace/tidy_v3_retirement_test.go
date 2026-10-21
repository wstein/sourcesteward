// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestTidyV3RetirementPlanRejectsActorAtEveryObservation(t *testing.T) {
	for boundary := 1; boundary <= 3; boundary++ {
		t.Run(fmt.Sprint(boundary), func(t *testing.T) {
			o, _ := tidyV3LegacyFixture(t, false)
			o.StateDir = privateDir(t)
			c := defaultTidyV3RetirementCoordinator()
			observe := c.observe
			calls := 0
			actor := filepath.Join(o.CompletedWorktree, "actor-untracked")
			c.observe = func(ctx context.Context, o V3PlanOptions, b *tidyV3ProofBudget) (tidyV3LegacyObservation, error) {
				calls++
				if calls == boundary {
					if e := os.WriteFile(actor, []byte("preserve user bytes"), 0600); e != nil {
						t.Fatal(e)
					}
				}
				return observe(ctx, o, b)
			}
			r := c.plan(context.Background(), o)
			if r.Status == "ok" || r.Plan != nil || calls != boundary {
				t.Fatalf("accepted stale boundary%d: %+v calls%d", boundary, r, calls)
			}
			if got, e := os.ReadFile(actor); e != nil || string(got) != "preserve user bytes" {
				t.Fatal("actor lost")
			}
			entries, e := os.ReadDir(filepath.Join(o.StateDir, "tidy-v3-plans"))
			if e == nil && len(entries) > 0 {
				t.Fatal("executable stale plan stored")
			}
		})
	}
}

func TestTidyV3RetirementApplyRejectsActorAtEveryObservation(t *testing.T) {
	for boundary := 1; boundary <= 8; boundary++ {
		t.Run(fmt.Sprint(boundary), func(t *testing.T) {
			parallelFixture(t)
			o, _ := tidyV3LegacyFixture(t, false)
			o.StateDir = privateDir(t)
			c := defaultTidyV3RetirementCoordinator()
			planned := c.plan(context.Background(), o)
			if planned.Status != "ok" {
				t.Fatalf("plan %+v", planned)
			}
			p := *planned.Plan
			observe := c.observe
			calls, removes := 0, 0
			actor := filepath.Join(o.CompletedWorktree, "actor-untracked")
			c.observe = func(ctx context.Context, o V3PlanOptions, b *tidyV3ProofBudget) (tidyV3LegacyObservation, error) {
				calls++
				if calls == boundary {
					if e := os.WriteFile(actor, []byte("preserve late actor"), 0600); e != nil {
						t.Fatal(e)
					}
				}
				return observe(ctx, o, b)
			}
			c.remove = func(context.Context, string, []string, string, []string, []string) (string, int, error) {
				removes++
				t.Fatal("removal reached after changed evidence")
				return "", 1, nil
			}
			r := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, CompletedWorktree: o.CompletedWorktree, Owner: o.Owner, ProtectWorktrees: o.ProtectWorktrees})
			if r.Status == "ok" || r.Plan != nil || calls != boundary || removes != 0 {
				t.Fatalf("accepted changed boundary%d: %+v calls%d", boundary, r, calls)
			}
			if got, e := os.ReadFile(actor); e != nil || string(got) != "preserve late actor" {
				t.Fatal("late actor lost")
			}
			recovery := v3RecoveryFor(p, p.Retirement.ActionID, p.Retirement.Target.CommitID)
			refs := gitFixture(t, o.Path, "for-each-ref", "--format=%(refname)")
			found := containsString(strings.Split(refs, "\n"), recovery.Name)
			if found != (boundary >= 4) {
				t.Fatalf("recovery bound wrong boundary%d refs%q", boundary, refs)
			}
			if boundary >= 3 {
				j := readLegacyRetirementJournal(t, p)
				if j.Phase != "incomplete" {
					t.Fatal("stopped journal replayable")
				}
			}
		})
	}
}

func TestTidyV3OwnedRetirementKeepsOriginalCreationAndExactLock(t *testing.T) {
	source, creation, task := gitRetirementFixture(t)
	originals := map[string]layoutProtectedFile{}
	for _, path := range []string{filepath.Join(creation.StateDir, "tasks", task.TaskID+".json"), filepath.Join(creation.StateDir, "plans", creation.PlanID+".json"), filepath.Join(creation.StateDir, "operations", creation.PlanID+".json")} {
		originals[path] = layoutProtectedFileAt(t, path)
	}
	c := defaultTidyV3RetirementCoordinator()
	o := V3PlanOptions{Path: source, Base: "HEAD", StateDir: creation.StateDir, Task: task.TaskID, Owner: task.Owner, RemoveRegistration: true, RemoveFiles: task.Destination, ProtectWorktrees: []string{}}
	r := c.plan(context.Background(), o)
	if r.Status != "ok" || r.Plan == nil {
		t.Fatalf("explicit owned v3 policy unavailable: %+v", r.Diagnostics)
	}
	if r.Plan.Mode != "owned-retirement" || r.Plan.Retirement.Provenance.Owned == nil || !reflect.DeepEqual(r.Plan.Retirement.Provenance.Owned.CreationPlan, creation) {
		t.Fatal("original ownership replaced")
	}
	owned := c.ownedRemove
	calls := 0
	c.ownedRemove = func(ctx context.Context, bin, source, dest string, parents, protection []string) (string, int, error) {
		calls++
		lock, e := os.ReadFile(filepath.Join(task.Native.GitDir, "locked"))
		if e != nil || string(lock) != "stw:"+task.TaskID+"\n" {
			t.Fatal("original exact lock changed before original native runner")
		}
		return owned(ctx, bin, source, dest, parents, protection)
	}
	c.remove = func(context.Context, string, []string, string, []string, []string) (string, int, error) {
		t.Fatal("owned mode used legacy runner")
		return "", 1, nil
	}
	applied := c.apply(context.Background(), V3ApplyOptions{StateDir: creation.StateDir, PlanID: r.Plan.PlanID, Authorize: true, ProtectWorktrees: []string{}})
	if applied.Status != "ok" || calls != 1 || applied.Boundary.Budget.Observations != 8 || applied.Boundary.Budget.NativeProcesses != 8 {
		t.Fatalf("owned apply %+v calls%d counts%+v", applied.Diagnostics, calls, applied.Boundary.Budget)
	}
	if _, e := os.Lstat(task.Destination); !os.IsNotExist(e) {
		t.Fatal("owned target not retired")
	}
	for path, want := range originals {
		if got := layoutProtectedFileAt(t, path); !reflect.DeepEqual(got, want) {
			t.Fatal("original ownership records modified", path)
		}
	}
}

func TestTidyV3RetirementPhysicalActorsAtEveryApplyObservation(t *testing.T) {
	for boundary := 1; boundary <= 8; boundary++ {
		for _, kind := range []string{"same-size-restored-mtime", "same-bytes-restored-mtime", "mode", "hardlink", "symlink", "path-replacement"} {
			t.Run(fmt.Sprintf("%d/%s", boundary, kind), func(t *testing.T) {
				parallelFixture(t)
				o, _ := tidyV3LegacyFixture(t, false)
				o.StateDir = privateDir(t)
				path := filepath.Join(o.CompletedWorktree, "file")
				before, e := os.Stat(path)
				if e != nil {
					t.Fatal(e)
				}
				original, e := os.ReadFile(path)
				if e != nil {
					t.Fatal(e)
				}
				c := defaultTidyV3RetirementCoordinator()
				planned := c.plan(context.Background(), o)
				if planned.Plan == nil {
					t.Fatal(planned.Diagnostics)
				}
				p := *planned.Plan
				observe := c.observe
				calls := 0
				outside := filepath.Join(gitTempDir(t), "actor-original")
				c.observe = func(ctx context.Context, o V3PlanOptions, b *tidyV3ProofBudget) (tidyV3LegacyObservation, error) {
					calls++
					if calls == boundary {
						var e error
						switch kind {
						case "same-size-restored-mtime":
							data := append([]byte{}, original...)
							data[0] ^= 1
							e = os.WriteFile(path, data, 0600)
							if e == nil {
								e = os.Chtimes(path, before.ModTime(), before.ModTime())
							}
						case "same-bytes-restored-mtime":
							e = os.WriteFile(path, original, 0600)
							if e == nil {
								e = os.Chtimes(path, before.ModTime(), before.ModTime())
							}
						case "mode":
							e = os.Chmod(path, 0700)
						case "hardlink":
							e = os.Link(path, outside)
						case "symlink":
							e = os.Rename(path, outside)
							if e == nil {
								e = os.Symlink(outside, path)
							}
						case "path-replacement":
							e = os.Rename(path, outside)
							if e == nil {
								e = os.WriteFile(path, original, 0600)
							}
						}
						if e != nil {
							t.Fatal(e)
						}
					}
					return observe(ctx, o, b)
				}
				removes := 0
				c.remove = func(context.Context, string, []string, string, []string, []string) (string, int, error) {
					removes++
					return "", 1, fmt.Errorf("unexpected removal")
				}
				r := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, CompletedWorktree: o.CompletedWorktree, Owner: o.Owner, ProtectWorktrees: o.ProtectWorktrees})
				if r.Status == "ok" || r.Plan != nil || calls != boundary || removes != 0 {
					t.Fatalf("physical actor accepted boundary%d kind%s calls%d removes%d %+v", boundary, kind, calls, removes, r.Diagnostics)
				}
				if _, e = os.Lstat(path); e != nil {
					t.Fatal("actor path removed")
				}
			})
		}
	}
}
