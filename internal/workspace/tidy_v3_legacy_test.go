// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func tidyV3LegacyFixture(t *testing.T, detached bool) (V3PlanOptions, string) {
	t.Helper()
	source := fixtureGit(t)
	dest := filepath.Join(gitTempDir(t), "completed")
	if detached {
		gitFixture(t, source, "worktree", "add", "-q", "--detach", dest, "HEAD")
	} else {
		gitFixture(t, source, "worktree", "add", "-q", "-b", "completed", dest, "HEAD")
	}
	active := filepath.Join(gitTempDir(t), "active")
	gitFixture(t, source, "worktree", "add", "-q", "-b", "active", active, "HEAD")
	return V3PlanOptions{Path: source, Base: "HEAD", StateDir: gitTempDir(t), CompletedWorktree: dest, Owner: "operator", RemoveRegistration: true, RemoveFiles: dest, ProtectWorktrees: []string{active}}, active
}
func TestTidyV3LegacyAdmitsExplicitCompletedLinkedTarget(t *testing.T) {
	for _, detached := range []bool{false, true} {
		o, active := tidyV3LegacyFixture(t, detached)
		got, e := observeTidyV3Legacy(context.Background(), o, newTidyV3ProofBudget(false))
		if e != nil {
			t.Fatal(e)
		}
		r := got.Retirement
		if r.Provenance.Kind != "legacy-completion" || r.Provenance.Owned != nil || r.Provenance.Legacy == nil || !r.Provenance.Legacy.Completed || !r.Provenance.Legacy.SameUserAuthority || r.Destination.Path != o.CompletedWorktree || !r.Disk.Complete || !r.Metadata.Complete || !containsString(r.ProtectedRoots, active) {
			t.Fatalf("incomplete explicit legacy admission: %+v", r)
		}
		if (r.Target.HeadRef == "") != detached {
			t.Fatalf("wrong attached/detached identity %+v", r.Target)
		}
		if _, e = os.Stat(o.CompletedWorktree); e != nil {
			t.Fatal("admission removed destination")
		}
	}
}

func TestTidyV3LegacyRetainsBlockedTargets(t *testing.T) {
	for _, kind := range []string{"missing-completion", "missing-owner", "unselected-files", "missing-registration-authorization", "active-clean", "source", "source-subdirectory", "primary-from-linked-source", "inside-source", "missing-target", "foreign-repository", "locked", "dirty", "staged", "untracked", "ignored", "empty-directory", "hardlink", "symlink", "missing-backlink", "wrong-backlink", "wrong-common", "bad-pointer", "linked-config", "ongoing-operation", "registration-extra", "state-overlap", "mixed-jj", "current-directory"} {
		t.Run(kind, func(t *testing.T) {
			o, _ := tidyV3LegacyFixture(t, false)
			dest := o.CompletedWorktree
			id, e := gitNativeIdentity(context.Background(), dest)
			if e != nil {
				t.Fatal(e)
			}
			switch kind {
			case "missing-completion":
				o.CompletedWorktree = ""
			case "missing-owner":
				o.Owner = ""
			case "unselected-files":
				o.RemoveFiles = o.Path
			case "missing-registration-authorization":
				o.RemoveRegistration = false
			case "active-clean":
				o.ProtectWorktrees = append(o.ProtectWorktrees, dest)
			case "source-subdirectory":
				o.Path = filepath.Join(o.Path, "subdirectory")
				e = os.Mkdir(o.Path, 0700)
			case "source":
				o.CompletedWorktree = o.Path
				o.RemoveFiles = o.Path
			case "primary-from-linked-source":
				o.Path, dest = dest, o.Path
				o.CompletedWorktree = dest
				o.RemoveFiles = dest
			case "inside-source":
				dest = filepath.Join(o.Path, "child")
				gitFixture(t, o.Path, "worktree", "add", "-q", "--detach", dest, "HEAD")
				o.CompletedWorktree = dest
				o.RemoveFiles = dest
			case "missing-target":
				o.CompletedWorktree = filepath.Join(filepath.Dir(dest), "missing")
				o.RemoveFiles = o.CompletedWorktree
			case "foreign-repository":
				dest = fixtureGit(t)
				o.CompletedWorktree = dest
				o.RemoveFiles = dest
			case "locked":
				gitFixture(t, o.Path, "worktree", "lock", "--reason", "actor active", dest)
			case "dirty":
				e = os.WriteFile(filepath.Join(dest, "file"), []byte("actor dirty\n"), 0600)
			case "staged":
				e = os.WriteFile(filepath.Join(dest, "file"), []byte("actor staged\n"), 0600)
				gitFixture(t, dest, "add", "file")
			case "untracked":
				e = os.WriteFile(filepath.Join(dest, "actor"), []byte("keep actor"), 0600)
			case "ignored":
				e = os.WriteFile(filepath.Join(id.RepositoryDir, "info", "exclude"), []byte("actor-cache\n"), 0600)
				if e == nil {
					e = os.WriteFile(filepath.Join(dest, "actor-cache"), []byte("keep ignored"), 0600)
				}
			case "empty-directory":
				e = os.Mkdir(filepath.Join(dest, "actor-empty"), 0700)
			case "hardlink":
				e = os.Link(filepath.Join(dest, "file"), filepath.Join(gitTempDir(t), "actor-linked-file"))
			case "symlink":
				e = os.Remove(filepath.Join(dest, "file"))
				if e == nil {
					e = os.Symlink(filepath.Join(o.Path, "file"), filepath.Join(dest, "file"))
				}
			case "missing-backlink":
				e = os.Remove(filepath.Join(id.GitDir, "gitdir"))
			case "wrong-backlink":
				e = os.WriteFile(filepath.Join(id.GitDir, "gitdir"), []byte(filepath.Join(o.Path, ".git")+"\n"), 0600)
			case "wrong-common":
				e = os.WriteFile(filepath.Join(id.GitDir, "commondir"), []byte(gitTempDir(t)+"\n"), 0600)
			case "bad-pointer":
				e = os.WriteFile(filepath.Join(dest, ".git"), []byte("gitdir: "+filepath.Join(id.RepositoryDir, "worktrees", "other")+"\n"), 0600)
			case "linked-config":
				e = os.WriteFile(filepath.Join(id.GitDir, "config.worktree"), []byte("[core]\n bare = false\n"), 0600)
			case "ongoing-operation":
				e = os.WriteFile(filepath.Join(id.GitDir, "MERGE_HEAD"), []byte(id.CommitID+"\n"), 0600)
			case "registration-extra":
				e = os.WriteFile(filepath.Join(id.GitDir, "actor-extra"), []byte("keep metadata"), 0600)
			case "state-overlap":
				o.StateDir = filepath.Join(dest, "state")
			case "mixed-jj":
				e = os.Mkdir(filepath.Join(dest, ".jj"), 0700)
			case "current-directory":
				t.Chdir(dest)
			}
			if e != nil {
				t.Fatal(e)
			}
			if _, e = observeTidyV3Legacy(context.Background(), o, newTidyV3ProofBudget(false)); e == nil {
				t.Fatalf("%s admitted", kind)
			}
			if _, e = os.Lstat(dest); e != nil {
				t.Fatal("refusal removed actor destination")
			}
		})
	}
}

func TestTidyV3LegacyBindsNativeAndPhysicalEvidence(t *testing.T) {
	o, _ := tidyV3LegacyFixture(t, false)
	budget := newTidyV3ProofBudget(false)
	got, e := observeTidyV3Legacy(context.Background(), o, budget)
	if e != nil {
		t.Fatal(e)
	}
	r := got.Retirement
	p := V3Plan{SchemaVersion: 3, Policy: "tidy-git-v3", Mode: "legacy-retirement", SourcePath: o.Path, StateDir: o.StateDir, Expected: got.Native.Expected, SelectedRefs: []string{}, Candidates: []V3Candidate{}, Retirement: &r, Effects: []string{}, RecoveryLimits: v3RecoveryLimits()}
	if !validV3Retirement(r, p) || !validV3NativeState(got.Native.Native, p, p.Expected) {
		t.Fatal("admission does not bind the existing central model")
	}
	c := budget.snapshot()
	if c.Observations != 1 || c.RawProcesses != 1 || c.UserPasses != 3 || c.MetadataPasses != 4 {
		t.Fatalf("wrong complete sample count %+v", c)
	}
	if _, e = os.Stat(filepath.Join(o.StateDir, "tasks")); !os.IsNotExist(e) {
		t.Fatal("legacy admission fabricated task state")
	}
	if _, e = os.Stat(filepath.Join(r.Target.GitDir, "locked")); !os.IsNotExist(e) {
		t.Fatal("legacy admission fabricated ownership lock")
	}
	p.PlanID = "plan"
	p.Digest = "unused"
	apply := V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, CompletedWorktree: o.CompletedWorktree, Owner: o.Owner, ProtectWorktrees: o.ProtectWorktrees}
	if e = validateV3ApplyOptions(apply, p); e != nil {
		t.Fatal(e)
	}
	apply.ProtectWorktrees = []string{}
	if e = validateV3ApplyOptions(apply, p); e == nil {
		t.Fatal("apply lost known active protection")
	}
}

func TestTidyV3LegacyRefusesChangesBetweenPhysicalPasses(t *testing.T) {
	for _, actor := range []string{"same-size-write-restored-mtime", "chmod", "HEAD-binding", "pointer-replacement", "parent-replacement", "configuration-change"} {
		t.Run(actor, func(t *testing.T) {
			o, _ := tidyV3LegacyFixture(t, false)
			dest := o.CompletedWorktree
			p := filepath.Join(dest, "file")
			st, e := os.Stat(p)
			if e != nil {
				t.Fatal(e)
			}
			budget := newTidyV3ProofBudget(false)
			done := false
			mutate := func() {
				var e error
				switch actor {
				case "same-size-write-restored-mtime":
					e = os.WriteFile(p, []byte("actor!\n"), 0600)
					if e == nil {
						e = os.Chtimes(p, st.ModTime(), st.ModTime())
					}
				case "chmod":
					e = os.Chmod(p, 0700)
				case "HEAD-binding":
					gitFixture(t, dest, "checkout", "-q", "--detach", "HEAD")
				case "pointer-replacement":
					pointer := filepath.Join(dest, ".git")
					b, re := os.ReadFile(pointer)
					if re != nil {
						t.Fatal(re)
					}
					e = os.Rename(pointer, pointer+"-actor")
					if e == nil {
						e = os.WriteFile(pointer, b, 0600)
					}
				case "parent-replacement":
					parent := filepath.Dir(dest)
					e = os.Rename(parent, parent+"-actor")
					if e == nil {
						e = os.Mkdir(parent, 0700)
					}
					if e == nil {
						e = os.Rename(filepath.Join(parent+"-actor", filepath.Base(dest)), dest)
					}
				case "configuration-change":
					gitFixture(t, o.Path, "config", "user.name", "Actor")
				}
				if e != nil {
					t.Fatal(e)
				}
			}
			calls := 0
			observer := tidyV3LegacyObserver{source: observeTidyV3Git, target: func(ctx context.Context, path string) (NativeIdentity, error) {
				calls++
				if calls == 2 {
					done = true
					mutate()
				}
				return gitNativeIdentity(ctx, path)
			}}
			if _, e = observer.observe(context.Background(), o, budget); e == nil {
				t.Fatal("late actor state admitted")
			}
			if !done {
				t.Fatal("actor not injected between physical passes")
			}
			if _, e = os.Lstat(dest); e != nil {
				t.Fatal("actor destination removed")
			}
		})
	}
}

func TestTidyV3LegacyPreservesPrimaryAndActiveUserContent(t *testing.T) {
	o, active := tidyV3LegacyFixture(t, false)
	id, e := gitNativeIdentity(context.Background(), o.Path)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(id.GitDir, "info", "exclude"), []byte("ignored-cache\n"), 0600); e != nil {
		t.Fatal(e)
	}
	files := map[string][]byte{filepath.Join(o.Path, "user-plan.md"): []byte("primary user plan"), filepath.Join(o.Path, "ignored-cache"): []byte("primary ignored cache"), filepath.Join(active, "file"): []byte("active actor modifications"), filepath.Join(active, "ignored-cache"): []byte("active ignored cache")}
	for p, b := range files {
		if e = os.WriteFile(p, b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = observeTidyV3Legacy(context.Background(), o, newTidyV3ProofBudget(false)); e != nil {
		t.Fatal(e)
	}
	for p, want := range files {
		got, e := os.ReadFile(p)
		if e != nil || string(got) != string(want) {
			t.Fatalf("changed protected user bytes %s: %v", p, e)
		}
	}
}

func TestTidyV3LegacySourceLayoutsKeepNativePrimaryMapping(t *testing.T) {
	for _, layout := range []string{"ordinary", "separate", "submodule"} {
		for _, linked := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/linked-%t", layout, linked), func(t *testing.T) {
				source, primary, common := fixtureGitLayout(t, layout, linked, true)
				dest := filepath.Join(gitTempDir(t), "completed")
				gitFixture(t, source, "worktree", "add", "-q", "-b", "legacy-completed", dest, "HEAD")
				protect := []string{}
				if linked {
					protect = append(protect, primary)
				}
				o := V3PlanOptions{Path: source, Base: "HEAD", StateDir: gitTempDir(t), CompletedWorktree: dest, Owner: "operator", RemoveRegistration: true, RemoveFiles: dest, ProtectWorktrees: protect}
				got, e := observeTidyV3Legacy(context.Background(), o, newTidyV3ProofBudget(false))
				if e != nil {
					t.Fatal(e)
				}
				if !containsString(got.Retirement.RegisteredRoots, primary) || containsString(got.Retirement.RegisteredRoots, common) {
					t.Fatalf("authoritative primary root absent or metadata path substituted: %v", got.Retirement.RegisteredRoots)
				}
				p := V3Plan{SourcePath: o.Path, StateDir: o.StateDir, Expected: got.Native.Expected, Mode: "legacy-retirement", Retirement: &got.Retirement}
				if !validV3Retirement(got.Retirement, p) || !validV3NativeState(got.Native.Native, p, p.Expected) {
					t.Fatalf("layout does not bind central model; roots=%v source=%s", got.Retirement.RegisteredRoots, o.Path)
				}
			})
		}
	}
}

func TestTidyV3LegacyStateCannotOverlapSeparateNativeMetadata(t *testing.T) {
	source, _, common := fixtureGitLayout(t, "separate", true, true)
	dest := filepath.Join(gitTempDir(t), "completed")
	gitFixture(t, source, "worktree", "add", "-q", "-b", "completed", dest, "HEAD")
	o := V3PlanOptions{Path: source, Base: "HEAD", StateDir: filepath.Join(common, "state"), CompletedWorktree: dest, Owner: "operator", RemoveRegistration: true, RemoveFiles: dest, ProtectWorktrees: []string{}}
	if _, e := observeTidyV3Legacy(context.Background(), o, newTidyV3ProofBudget(false)); e == nil {
		t.Fatal("state within external common directory admitted")
	}
}

func TestTidyV3LegacyCancellationRetainsTarget(t *testing.T) {
	o, _ := tidyV3LegacyFixture(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	budget := newTidyV3ProofBudget(false)
	if _, e := observeTidyV3Legacy(ctx, o, budget); e == nil {
		t.Fatal("canceled admission accepted")
	}
	if budget.snapshot().RawProcesses != 0 {
		t.Fatal("canceled admission launched raw content sampling")
	}
	if _, e := os.Stat(o.CompletedWorktree); e != nil {
		t.Fatal("canceled admission removed target")
	}
}

func TestTidyV3LegacyRefusesLooseReferenceLocks(t *testing.T) {
	for _, kind := range []string{"target-branch", "source-branch", "unrelated-nested-branch", "late-target-branch"} {
		t.Run(kind, func(t *testing.T) {
			o, _ := tidyV3LegacyFixture(t, false)
			target, e := gitNativeIdentity(context.Background(), o.CompletedWorktree)
			if e != nil {
				t.Fatal(e)
			}
			ref := target.HeadRef
			if kind == "source-branch" {
				source, e := gitNativeIdentity(context.Background(), o.Path)
				if e != nil {
					t.Fatal(e)
				}
				ref = source.HeadRef
			}
			if kind == "unrelated-nested-branch" {
				ref = "refs/heads/actor/nested"
				if e = os.MkdirAll(filepath.Join(target.RepositoryDir, "refs", "heads", "actor"), 0700); e != nil {
					t.Fatal(e)
				}
			}
			lock := filepath.Join(target.RepositoryDir, ref+".lock")
			payload := []byte("active actor ref update\n")
			writeLock := func() {
				t.Helper()
				if e := os.WriteFile(lock, payload, 0600); e != nil {
					t.Fatal(e)
				}
			}
			observer := tidyV3LegacyObserver{source: observeTidyV3Git, target: gitNativeIdentity}
			calls := 0
			if kind == "late-target-branch" {
				observer.target = func(ctx context.Context, path string) (NativeIdentity, error) {
					id, e := gitNativeIdentity(ctx, path)
					calls++
					if calls == 2 {
						writeLock()
					}
					return id, e
				}
			} else {
				writeLock()
			}
			got, e := observer.observe(context.Background(), o, newTidyV3ProofBudget(false))
			if e == nil {
				t.Fatalf("loose %s lock admitted with %s", kind, got.Retirement.Actions[0].Decision)
			}
			if kind == "late-target-branch" && calls != 2 {
				t.Fatal("late actor not reached")
			}
			if got, e := os.ReadFile(lock); e != nil || string(got) != string(payload) {
				t.Fatal("actor lock payload changed")
			}
			if _, e = os.Stat(o.CompletedWorktree); e != nil {
				t.Fatal("target removed on lock refusal")
			}
		})
	}
}

func TestTidyV3LegacyRefusesPhysicalCurrentDirectoryAliases(t *testing.T) {
	for _, nested := range []bool{false, true} {
		t.Run(fmt.Sprint(nested), func(t *testing.T) {
			o, _ := tidyV3LegacyFixture(t, false)
			actual := o.CompletedWorktree
			if nested {
				sourceNested := filepath.Join(o.Path, "nested")
				if e := os.Mkdir(sourceNested, 0700); e != nil {
					t.Fatal(e)
				}
				if e := os.WriteFile(filepath.Join(sourceNested, "tracked"), []byte("nested actor\n"), 0600); e != nil {
					t.Fatal(e)
				}
				gitFixture(t, o.Path, "add", "nested/tracked")
				gitFixture(t, o.Path, "commit", "-qm", "tracked nested workspace")
				gitFixture(t, o.CompletedWorktree, "reset", "--hard", gitFixture(t, o.Path, "rev-parse", "HEAD"))
				actual = filepath.Join(actual, "nested")
			}
			alias := filepath.Join(gitTempDir(t), "outside-current-alias")
			if e := os.Symlink(actual, alias); e != nil {
				t.Fatal(e)
			}
			t.Chdir(actual)
			t.Setenv("PWD", alias)
			if _, e := observeTidyV3Legacy(context.Background(), o, newTidyV3ProofBudget(false)); e == nil {
				t.Fatal("actual current directory target admitted through logical alias")
			}
			if got, e := os.ReadFile(filepath.Join(o.CompletedWorktree, "file")); e != nil || string(got) != "stored\n" {
				t.Fatal("current actor data changed")
			}
		})
	}
}

func TestTidyV3LegacyLooseLockGuardBoundsAndAliases(t *testing.T) {
	for _, kind := range []string{"over-entry-bound", "directory-symlink", "regular-symlink", "cancellation"} {
		t.Run(kind, func(t *testing.T) {
			refs := gitTempDir(t)
			ctx := context.Background()
			switch kind {
			case "over-entry-bound":
				for i := 0; i <= observationLimit; i++ {
					if e := os.WriteFile(filepath.Join(refs, fmt.Sprintf("actor-%04d", i)), []byte("actor metadata\n"), 0600); e != nil {
						t.Fatal(e)
					}
				}
			case "directory-symlink":
				if e := os.Symlink(gitTempDir(t), filepath.Join(refs, "actor")); e != nil {
					t.Fatal(e)
				}
			case "regular-symlink":
				outside := filepath.Join(gitTempDir(t), "outside")
				if e := os.WriteFile(outside, []byte("outside actor"), 0600); e != nil {
					t.Fatal(e)
				}
				if e := os.Symlink(outside, filepath.Join(refs, "actor")); e != nil {
					t.Fatal(e)
				}
			case "cancellation":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if e := retirementV3RefLocks(ctx, refs); e == nil {
				t.Fatal("incomplete lock evidence accepted")
			}
			if _, e := os.Stat(refs); e != nil {
				t.Fatal("control refusal removed actor root")
			}
		})
	}
}

func TestTidyV3LegacyPreservesRegularAndDirectorySiblings(t *testing.T) {
	o, _ := tidyV3LegacyFixture(t, false)
	parent := filepath.Dir(o.CompletedWorktree)
	file := filepath.Join(parent, "user-notes.bin")
	payload := []byte("preserve sibling\x00\xff\n")
	if e := os.WriteFile(file, payload, 0600); e != nil {
		t.Fatal(e)
	}
	dir := filepath.Join(parent, "actor-directory")
	if e := os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	got, e := observeTidyV3Legacy(context.Background(), o, newTidyV3ProofBudget(false))
	if e != nil {
		t.Fatal(e)
	}
	if len(got.Retirement.Siblings) != 2 {
		t.Fatalf("omitted siblings %+v", got.Retirement.Siblings)
	}
	p := V3Plan{SourcePath: o.Path, StateDir: o.StateDir, Expected: got.Native.Expected, Mode: "legacy-retirement", Retirement: &got.Retirement}
	if !validV3Retirement(got.Retirement, p) {
		t.Fatal("complete sibling identities rejected by central model")
	}
	if data, e := os.ReadFile(file); e != nil || string(data) != string(payload) {
		t.Fatal("regular sibling bytes changed")
	}
	if _, e = os.Stat(dir); e != nil {
		t.Fatal("directory sibling changed")
	}
	// Siblings are protected identities, not retirement user payload. Their size
	// does not inherit user-file/pass caps and their content is never sampled.
	for i := range got.Retirement.Siblings {
		if got.Retirement.Siblings[i].Kind == "regular" {
			got.Retirement.Siblings[i].Size = (64 << 20) + 1
		}
	}
	if !validV3Retirement(got.Retirement, p) {
		t.Fatal("sibling identity inherited a user-content size cap")
	}
	for _, kind := range []string{"hardlink", "foreign-owner", "unsupported-kind", "wrong-parent", "overlap"} {
		t.Run(kind, func(t *testing.T) {
			r := got.Retirement
			r.Siblings = append([]V3PhysicalIdentity{}, r.Siblings...)
			for i := range r.Siblings {
				if r.Siblings[i].Kind != "regular" {
					continue
				}
				switch kind {
				case "hardlink":
					r.Siblings[i].Nlink = 2
				case "foreign-owner":
					r.Siblings[i].UID++
				case "unsupported-kind":
					r.Siblings[i].Kind = "symlink"
				case "wrong-parent":
					r.Siblings[i].Path = filepath.Join(parent, "nested", "file")
				case "overlap":
					r.Siblings[i].Path = o.CompletedWorktree
				}
			}
			if validV3Retirement(r, p) {
				t.Fatal("invalid sibling identity admitted")
			}
		})
	}
}

func TestTidyV3LegacySiblingChangesRefuseAndPreserveActors(t *testing.T) {
	for _, kind := range []string{"replacement", "mode", "hardlink", "symlink", "directory-replacement"} {
		t.Run(kind, func(t *testing.T) {
			o, _ := tidyV3LegacyFixture(t, false)
			parent := filepath.Dir(o.CompletedWorktree)
			sibling := filepath.Join(parent, "actor-sibling")
			if kind == "directory-replacement" {
				if e := os.Mkdir(sibling, 0700); e != nil {
					t.Fatal(e)
				}
			} else {
				if e := os.WriteFile(sibling, []byte("actor data"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			calls := 0
			observer := tidyV3LegacyObserver{source: observeTidyV3Git, target: func(ctx context.Context, path string) (NativeIdentity, error) {
				id, e := gitNativeIdentity(ctx, path)
				calls++
				if calls != 2 {
					return id, e
				}
				switch kind {
				case "replacement":
					e = os.Rename(sibling, sibling+"-original")
					if e == nil {
						e = os.WriteFile(sibling, []byte("new actor!"), 0600)
					}
				case "mode":
					e = os.Chmod(sibling, 0700)
				case "hardlink":
					e = os.Link(sibling, filepath.Join(gitTempDir(t), "actor-link"))
				case "symlink":
					e = os.Rename(sibling, sibling+"-original")
					if e == nil {
						e = os.Symlink(sibling+"-original", sibling)
					}
				case "directory-replacement":
					e = os.Rename(sibling, sibling+"-original")
					if e == nil {
						e = os.Mkdir(sibling, 0700)
					}
				}
				if e != nil {
					t.Fatal(e)
				}
				return id, nil
			}}
			if _, e := observer.observe(context.Background(), o, newTidyV3ProofBudget(false)); e == nil {
				t.Fatal("changed sibling admitted")
			}
			if calls != 2 {
				t.Fatal("late sibling actor not reached")
			}
			if _, e := os.Lstat(sibling); e != nil {
				t.Fatal("actor sibling removed")
			}
		})
	}
}

func TestTidyV3LegacySiblingListingBound(t *testing.T) {
	parent := gitTempDir(t)
	for i := 0; i <= observationLimit; i++ {
		if e := os.WriteFile(filepath.Join(parent, fmt.Sprintf("actor-%04d", i)), []byte("keep"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := retirementV3Siblings(parent, filepath.Join(parent, "selected")); e == nil {
		t.Fatal("incomplete sibling list admitted")
	}
	if got, e := os.ReadFile(filepath.Join(parent, "actor-0000")); e != nil || string(got) != "keep" {
		t.Fatal("listing refusal changed actor bytes")
	}
}
