// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type layoutProtectedFile struct {
	Data     []byte
	Identity PathIdentity
	Mode     os.FileMode
}

func layoutProtectedFileAt(t *testing.T, path string) layoutProtectedFile {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	id, err := pathIdentity(path, st)
	if err != nil {
		t.Fatal(err)
	}
	return layoutProtectedFile{data, id, st.Mode()}
}

// Capture every existing source/primary/sibling payload byte. Native mutable
// refs and registrations are checked separately for their exact declared delta.
func layoutProtectedFiles(t *testing.T, source, primary, common string) map[string]layoutProtectedFile {
	t.Helper()
	out := map[string]layoutProtectedFile{}
	roots := []string{source, primary, filepath.Join(common, "objects"), filepath.Join(common, "hooks"), filepath.Join(common, "info"), filepath.Join(common, "worktrees")}
	// For initialized submodules, also preserve the surrounding superproject's
	// checkout and metadata while excluding only this selected common directory.
	if filepath.Base(primary) == "member" {
		roots = append(roots, filepath.Dir(primary))
	}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if path == common {
				return filepath.SkipDir
			}
			if !d.IsDir() {
				out[path] = layoutProtectedFileAt(t, path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"config", "index", "HEAD"} {
		path := filepath.Join(common, name)
		out[path] = layoutProtectedFileAt(t, path)
	}
	return out
}
func requireLayoutProtectedFiles(t *testing.T, want map[string]layoutProtectedFile) {
	t.Helper()
	for path, data := range want {
		got := layoutProtectedFileAt(t, path)
		if !reflect.DeepEqual(got, data) {
			t.Fatalf("protected bytes/mode/identity changed: %s", path)
		}
	}
}

func TestGitLayoutStoredCreationHandoffRetryAndOwnedRetirement(t *testing.T) {
	for _, layout := range []string{"submodule", "separate"} {
		for _, linked := range []bool{false, true} {
			name := layout
			if linked {
				name += "/linked"
			}
			t.Run(name, func(t *testing.T) {
				ctx := context.Background()
				source, primary, common := fixtureGitLayout(t, layout, linked, false)
				before := layoutProtectedFiles(t, source, primary, common)
				state := privateDir(t)
				dest := filepath.Join(gitTempDir(t), "task")
				plan := PlanCreate(ctx, PlanOptions{Path: source, Base: "HEAD", Destination: dest, Task: "layout-task", Owner: "fixture", StateDir: state})
				if plan.Status != "ok" {
					t.Fatal(plan.Error())
				}
				apply := Apply(ctx, ApplyOptions{StateDir: state, PlanID: plan.Plan.PlanID, Authorize: true})
				if apply.Status != "ok" || apply.Task == nil {
					t.Fatal(apply.Error())
				}
				retry := Apply(ctx, ApplyOptions{StateDir: state, PlanID: plan.Plan.PlanID, Authorize: true})
				if retry.Status != "ok" {
					t.Fatal(retry.Error())
				}
				handoff := Handoff(ctx, Options{Path: dest, StateDir: state})
				if handoff.Status != "ok" {
					t.Fatal(handoff.Error())
				}
				retired := PlanTidy(ctx, TidyPlanOptions{Path: source, Base: "HEAD", Task: "layout-task", Owner: "fixture", StateDir: state, RemoveRegistration: true, RemoveFiles: dest})
				if retired.Status != "ok" || retired.Retirement == nil {
					t.Fatal(retired.Error())
				}
				result := ApplyTidy(ctx, TidyApplyOptions{StateDir: state, PlanID: retired.Retirement.PlanID, Authorize: true})
				if result.Status != "ok" {
					t.Fatal(result.Error())
				}
				again := ApplyTidy(ctx, TidyApplyOptions{StateDir: state, PlanID: retired.Retirement.PlanID, Authorize: true})
				if again.Status != "ok" {
					t.Fatal(again.Error())
				}
				if _, err := os.Lstat(dest); !os.IsNotExist(err) {
					t.Fatal("owned destination retained")
				}
				if _, err := os.Lstat(apply.Task.Native.GitDir); !os.IsNotExist(err) {
					t.Fatal("owned registration retained")
				}
				r := retirementRecoveryFor(*retired.Retirement)
				if got := gitFixture(t, source, "rev-parse", r.Name); got != apply.Task.Native.CommitID {
					t.Fatal("recovery lost target")
				}
				requireLayoutProtectedFiles(t, before)
			})
		}
	}
}

func fixtureGitLayoutTidy(t *testing.T, layout string, linked bool) (string, string, string, TidyPlanRecord, TidyCandidate) {
	t.Helper()
	source, primary, common := fixtureGitLayout(t, layout, linked, false)
	// Advance the primary beyond the candidate, then advance a linked source too:
	// no candidate tip may be used by any registered checkout.
	gitFixture(t, primary, "branch", "deletable", "HEAD")
	gitFixture(t, primary, "branch", "keep", "HEAD")
	gitFixture(t, primary, "commit", "--allow-empty", "-qm", "advance base")
	if linked {
		gitFixture(t, source, "checkout", "--detach", gitFixture(t, primary, "rev-parse", "HEAD"))
	}
	gitFixture(t, primary, "pack-refs", "--all")
	observation, err := (gitTidyAdapter{}).Observe(context.Background(), source, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range observation.Candidates {
		if c.Name == "refs/heads/deletable" {
			if c.Decision != "delete" {
				t.Fatalf("candidate retained: %+v", c)
			}
			return source, primary, common, TidyPlanRecord{PlanID: "layout-packed", Backend: "git", SourcePath: source, Expected: observation.Expected}, c
		}
	}
	t.Fatal("candidate missing")
	return "", "", "", TidyPlanRecord{}, TidyCandidate{}
}

func TestGitLayoutTidyRecoveryAndPackedDeletePreserveOtherState(t *testing.T) {
	for _, layout := range []string{"submodule", "separate"} {
		for _, linked := range []bool{false, true} {
			name := layout
			if linked {
				name += "/linked"
			}
			t.Run(name, func(t *testing.T) {
				ctx := context.Background()
				source, primary, common, p, c := fixtureGitLayoutTidy(t, layout, linked)
				before := layoutProtectedFiles(t, source, primary, common)
				bin, err := gitExecutable()
				if err != nil {
					t.Fatal(err)
				}
				refs, err := gitTidyReferenceMap(ctx, bin, source)
				if err != nil {
					t.Fatal(err)
				}
				recovery, expected, out, err := (gitTidyAdapter{}).PrepareRecovery(ctx, p, c)
				if err != nil || !out.Complete {
					t.Fatalf("recovery: %+v %v", out, err)
				}
				out, err = (gitTidyAdapter{}).Delete(ctx, p, c, recovery, expected)
				if err != nil || !out.Complete {
					t.Fatalf("delete: %+v %v", out, err)
				}
				delete(refs, c.Name)
				refs[recovery.Name] = c.CommitID + "\x00"
				after, err := gitTidyReferenceMap(ctx, bin, source)
				if err != nil || !reflect.DeepEqual(refs, after) {
					t.Fatalf("unexpected reference delta: %v", err)
				}
				requireLayoutProtectedFiles(t, before)
			})
		}
	}
}

func TestGitLayoutTidyConfigChangeAfterRecoveryRetainsEverything(t *testing.T) {
	for _, layout := range []string{"submodule", "separate"} {
		t.Run(layout, func(t *testing.T) {
			ctx := context.Background()
			source, primary, common, p, c := fixtureGitLayoutTidy(t, layout, true)
			recovery, expected, out, err := (gitTidyAdapter{}).PrepareRecovery(ctx, p, c)
			if err != nil || !out.Complete {
				t.Fatal(err)
			}
			gitFixture(t, source, "config", "safe.layout-test", "changed")
			before := layoutProtectedFiles(t, source, primary, common)
			bin, err := gitExecutable()
			if err != nil {
				t.Fatal(err)
			}
			refs, err := gitTidyReferenceMap(ctx, bin, source)
			if err != nil {
				t.Fatal(err)
			}
			out, err = (gitTidyAdapter{}).Delete(ctx, p, c, recovery, expected)
			if err == nil || out.Attempted {
				t.Fatalf("stale deletion attempted: %+v %v", out, err)
			}
			after, err := gitTidyReferenceMap(ctx, bin, source)
			if err != nil || !reflect.DeepEqual(refs, after) {
				t.Fatal("stale cleanup changed refs/recovery")
			}
			requireLayoutProtectedFiles(t, before)
		})
	}
}

func TestGitLayoutUnsafeHelpersHavePreciseDiagnosticsAndNoEffects(t *testing.T) {
	for _, key := range []string{"core.fsmonitor", "core.hooksPath", "filter.set.clean"} {
		t.Run(key, func(t *testing.T) {
			source, primary, common := fixtureGitLayout(t, "submodule", true, false)
			sentinel := filepath.Join(gitTempDir(t), "called")
			gitFixture(t, source, "config", key, "touch "+sentinel)
			before := layoutProtectedFiles(t, source, primary, common)
			_, err := (gitAdapter{}).Observe(context.Background(), source, "HEAD")
			if err == nil || !strings.Contains(err.Error(), gitSafetyRule(strings.ToLower(key))) || strings.Contains(err.Error(), sentinel) {
				t.Fatalf("imprecise or sensitive diagnostic: %v", err)
			}
			if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
				t.Fatal("helper executed")
			}
			requireLayoutProtectedFiles(t, before)
		})
	}
}

func TestGitLayoutFilterAndHookGuardsPreserveNativeState(t *testing.T) {
	for _, kind := range []string{"unused-lfs", "committed-attributes", "info-attributes", "global-attributes", "dirty-attributes", "post-checkout", "reference-transaction"} {
		t.Run(kind, func(t *testing.T) {
			source, primary, common := fixtureGitLayout(t, "submodule", true, false)
			sentinel := filepath.Join(gitTempDir(t), "executed")
			switch kind {
			case "committed-attributes":
				os.WriteFile(filepath.Join(source, ".gitattributes"), []byte("file filter=lfs\n"), 0600)
				gitFixture(t, source, "add", ".gitattributes")
				gitFixture(t, source, "commit", "-qm", "attributes")
			case "info-attributes":
				os.MkdirAll(filepath.Join(common, "info"), 0700)
				os.WriteFile(filepath.Join(common, "info", "attributes"), []byte("file filter=lfs\n"), 0600)
			case "global-attributes":
				os.MkdirAll(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "git"), 0700)
				os.WriteFile(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "git", "attributes"), []byte("file filter=lfs\n"), 0600)
			case "dirty-attributes":
				os.WriteFile(filepath.Join(source, ".gitattributes"), []byte("file filter=lfs\n"), 0600)
			case "post-checkout", "reference-transaction":
				os.WriteFile(filepath.Join(common, "hooks", kind), []byte("#!/bin/sh\ntouch '"+sentinel+"'\n"), 0700)
			}
			gitFixture(t, source, "config", "filter.lfs.clean", "touch "+sentinel)
			gitFixture(t, source, "config", "filter.lfs.smudge", "touch "+sentinel)
			before := layoutProtectedFiles(t, source, primary, common)
			s, err := (gitAdapter{}).Observe(context.Background(), source, "HEAD")
			if kind == "unused-lfs" || kind == "dirty-attributes" {
				if err != nil {
					t.Fatal(err)
				}
				n, err := (gitAdapter{}).Create(context.Background(), MutationPlan{TaskID: "filter-safe", SourcePath: source, Destination: gitTempDir(t), Expected: s})
				if err != nil || !n.Complete {
					t.Fatalf("unused helper: %+v %v", n, err)
				}
			} else if err == nil {
				t.Fatal("active helper admitted")
			}
			if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
				t.Fatal("helper executed")
			}
			requireLayoutProtectedFiles(t, before)
		})
	}
}

func TestGitLayoutRetirementReplacementAfterRecoveryRefusesNextEffect(t *testing.T) {
	for _, kind := range []string{"source-pointer", "target-pointer", "config"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			source, primary, common := fixtureGitLayout(t, "submodule", true, false)
			state := privateDir(t)
			dest := filepath.Join(gitTempDir(t), "task")
			planned := PlanCreate(ctx, PlanOptions{Path: source, Base: "HEAD", Destination: dest, Task: "stale-retire", Owner: "fixture", StateDir: state})
			if planned.Status != "ok" {
				t.Fatal(planned.Error())
			}
			applied := Apply(ctx, ApplyOptions{StateDir: state, PlanID: planned.Plan.PlanID, Authorize: true})
			if applied.Status != "ok" {
				t.Fatal(applied.Error())
			}
			o, err := (gitRetirementAdapter{}).Observe(ctx, source, "HEAD", *applied.Task, *planned.Plan)
			if err != nil {
				t.Fatal(err)
			}
			p := RetirementPlanRecord{SchemaVersion: 2, PlanID: "stale-layout-retirement", SourcePath: source, Backend: "git", CreationPlan: *planned.Plan, Task: *applied.Task, Expected: o, RemoveRegistration: true, RemoveFiles: dest}
			r, out, err := (gitRetirementAdapter{}).PrepareRecovery(ctx, p)
			if err != nil || !out.Complete {
				t.Fatalf("recovery: %+v %v", out, err)
			}
			path := filepath.Join(source, ".git")
			if kind == "target-pointer" {
				path = filepath.Join(dest, ".git")
			}
			if kind == "config" {
				path = filepath.Join(common, "config")
			}
			replaceLayoutFile(t, path)
			// A replacement target must be checked by inode too, even with identical bytes.
			before := layoutProtectedFiles(t, source, primary, common)
			target := layoutProtectedFiles(t, dest, primary, common)
			result, err := (gitRetirementAdapter{}).RemoveRegistration(ctx, p, r, *out.Expected)
			if err == nil || result.Attempted {
				t.Fatalf("stale removal: %+v %v", result, err)
			}
			if got := gitFixture(t, source, "rev-parse", r.Name); got != r.CommitID {
				t.Fatal("recovery root lost")
			}
			requireLayoutProtectedFiles(t, before)
			requireLayoutProtectedFiles(t, target)
		})
	}
}

func TestGitLayoutNativeStartFailurePreservesExistingState(t *testing.T) {
	source, primary, common := fixtureGitLayout(t, "submodule", true, false)
	before := layoutProtectedFiles(t, source, primary, common)
	s, err := (gitAdapter{}).Observe(context.Background(), source, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	registrations := filepath.Join(common, "worktrees")
	st, err := os.Stat(registrations)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(registrations, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(registrations, st.Mode().Perm())
	n, err := (gitAdapter{}).Create(context.Background(), MutationPlan{TaskID: "native-failure", SourcePath: source, Destination: gitTempDir(t), Expected: s})
	if err == nil || n.Complete || n.Phase != "unknown" || len(n.Effects) == 0 {
		t.Fatalf("native-start failure lost partial outcome: %+v %v", n, err)
	}
	requireLayoutProtectedFiles(t, before)
}

func TestGitLayoutTidyPointerReplacementAfterRecoveryRetainsEverything(t *testing.T) {
	for _, kind := range []string{"source-pointer", "primary-pointer", "backlink"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			source, primary, common, p, c := fixtureGitLayoutTidy(t, "submodule", true)
			recovery, expected, out, err := (gitTidyAdapter{}).PrepareRecovery(ctx, p, c)
			if err != nil || !out.Complete {
				t.Fatal(err)
			}
			path := filepath.Join(source, ".git")
			if kind == "primary-pointer" {
				path = filepath.Join(primary, ".git")
			}
			if kind == "backlink" {
				path = filepath.Join(p.Expected.Identity.GitDir, "gitdir")
			}
			replaceLayoutFile(t, path)
			before := layoutProtectedFiles(t, source, primary, common)
			bin, err := gitExecutable()
			if err != nil {
				t.Fatal(err)
			}
			refs, err := gitTidyReferenceMap(ctx, bin, source)
			if err != nil {
				t.Fatal(err)
			}
			out, err = (gitTidyAdapter{}).Delete(ctx, p, c, recovery, expected)
			if err == nil || out.Attempted {
				t.Fatalf("stale deletion attempted: %+v %v", out, err)
			}
			after, err := gitTidyReferenceMap(ctx, bin, source)
			if err != nil || !reflect.DeepEqual(refs, after) {
				t.Fatal("stale cleanup changed refs/recovery")
			}
			requireLayoutProtectedFiles(t, before)
		})
	}
}

func TestGitLayoutOwnedRetirementRetainsUnsafeTargets(t *testing.T) {
	for _, layout := range []string{"submodule", "separate"} {
		for _, linked := range []bool{false, true} {
			name := layout
			if linked {
				name += "/linked"
			}
			t.Run(name, func(t *testing.T) {
				ctx := context.Background()
				source, primary, common := fixtureGitLayout(t, layout, linked, false)
				state := privateDir(t)
				dest := filepath.Join(gitTempDir(t), "task")
				plan := PlanCreate(ctx, PlanOptions{Path: source, Base: "HEAD", Destination: dest, Task: "retained-layout", Owner: "fixture", StateDir: state})
				if plan.Status != "ok" {
					t.Fatal(plan.Error())
				}
				applied := Apply(ctx, ApplyOptions{StateDir: state, PlanID: plan.Plan.PlanID, Authorize: true})
				if applied.Status != "ok" {
					t.Fatal(applied.Error())
				}
				options := TidyPlanOptions{Path: source, Base: "HEAD", Task: "retained-layout", Owner: "fixture", StateDir: state, RemoveRegistration: true, RemoveFiles: dest}
				refuse := func() {
					t.Helper()
					before := layoutProtectedFiles(t, dest, primary, common)
					r := PlanTidy(ctx, options)
					if r.Status == "ok" {
						t.Fatal("unsafe target received actionable retirement plan")
					}
					requireLayoutProtectedFiles(t, before)
				}
				if err := os.WriteFile(filepath.Join(dest, "file"), []byte("dirty source-independent task work\n"), 0600); err != nil {
					t.Fatal(err)
				}
				refuse()
				if err := os.WriteFile(filepath.Join(dest, "file"), []byte("stored\n"), 0600); err != nil {
					t.Fatal(err)
				}
				for _, path := range []string{filepath.Join(dest, "unknown"), filepath.Join(applied.Task.Native.GitDir, "unknown")} {
					if err := os.WriteFile(path, []byte("unowned payload\n"), 0600); err != nil {
						t.Fatal(err)
					}
					refuse()
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
				}
				options.Owner = "foreign"
				refuse()
				options.Owner = "fixture"
				before := layoutProtectedFiles(t, dest, primary, common)
				coordinator := defaultRetirementCoordinator()
				coordinator.cwd = func() (string, error) { return dest, nil }
				if r := coordinator.plan(ctx, options); r.Status == "ok" {
					t.Fatal("actual current workspace admitted as retirement target")
				}
				requireLayoutProtectedFiles(t, before)
			})
		}
	}
}
