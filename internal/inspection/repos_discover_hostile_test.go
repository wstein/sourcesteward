// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// reposHostileFixture is a superproject with initialized submodules s1 and s2 (absorbed Git directories).
type reposHostileFixture struct{ base, super, s1, s2 string }

func newReposHostileFixture(t *testing.T) reposHostileFixture {
	t.Helper()
	reposHermetic(t)
	base := reposTempDir(t)
	super := reposSuper(t, filepath.Join(base, "super"), "s1", "s2")
	return reposHostileFixture{base: base, super: super, s1: filepath.Join(super, "s1"), s2: filepath.Join(super, "s2")}
}

// expectRefused runs discovery and requires s1 to be path-unsafe before any process ran in it, while s2 is
// inventoried normally (one bad submodule never damages the others).
func (f reposHostileFixture) expectRefused(t *testing.T, wantInMessage string) ReposEntry {
	t.Helper()
	var seen []string
	var mu sync.Mutex
	res := reposRunWith([]string{f.super}, reposRecordingInspect(&seen, &mu), nil)
	e := reposEntry(t, res, "s1")
	if e.Status != "error" || e.Error == nil || e.Error.Code != "path-unsafe" || e.Inventory != nil || e.Facts != nil {
		t.Fatalf("s1 must be path-unsafe: %+v %+v", e, e.Error)
	}
	if wantInMessage != "" && !strings.Contains(e.Error.Message, wantInMessage) {
		t.Errorf("message %q lacks %q", e.Error.Message, wantInMessage)
	}
	for _, p := range seen {
		if strings.HasPrefix(p, f.s1) {
			t.Errorf("a process-level inspection ran in the refused member: %s", p)
		}
	}
	if ok := reposEntry(t, res, "s2"); ok.Status != "ok" || ok.Inventory == nil {
		t.Errorf("healthy sibling damaged: %+v %+v", ok, ok.Error)
	}
	if res.Entries[0].Discovery.Deduplicated != 0 {
		t.Errorf("a refused path was counted as deduplicated: %+v", res.Entries[0].Discovery)
	}
	return e
}

func TestReposDiscoverRefusesHostileSubmodules(t *testing.T) {
	gitDir := func(f reposHostileFixture, name string) string {
		return filepath.Join(f.super, ".git", "modules", name)
	}
	for _, tc := range []struct {
		name   string
		mutate func(t *testing.T, f reposHostileFixture)
		want   string
	}{
		{"path is a symlink to a sibling", func(t *testing.T, f reposHostileFixture) {
			os.RemoveAll(f.s1)
			if err := os.Symlink("s2", f.s1); err != nil {
				t.Fatal(err)
			}
		}, "symlink"},
		{"path is a regular file", func(t *testing.T, f reposHostileFixture) {
			os.RemoveAll(f.s1)
			writeFile(t, f.s1, "not a directory")
		}, "directory"},
		{".git is a symlink", func(t *testing.T, f reposHostileFixture) {
			os.Remove(filepath.Join(f.s1, ".git"))
			if err := os.Symlink(gitDir(f, "s1"), filepath.Join(f.s1, ".git")); err != nil {
				t.Fatal(err)
			}
		}, "symlink"},
		{".git pointer names a repository outside the modules directory", func(t *testing.T, f reposHostileFixture) {
			writeFile(t, filepath.Join(f.s1, ".git"), "gitdir: "+filepath.Join(f.base, "super", ".git")+"\n")
		}, ""},
		{".git pointer names the modules directory itself", func(t *testing.T, f reposHostileFixture) {
			writeFile(t, filepath.Join(f.s1, ".git"), "gitdir: ../.git/modules\n")
		}, ""},
		{".git pointer names another module", func(t *testing.T, f reposHostileFixture) {
			writeFile(t, filepath.Join(f.s1, ".git"), "gitdir: ../.git/modules/s2\n")
		}, ""},
		{".git pointer is larger than 4096 bytes", func(t *testing.T, f reposHostileFixture) {
			writeFile(t, filepath.Join(f.s1, ".git"), "gitdir: "+strings.Repeat("a", 5000)+"\n")
		}, ""},
		{".git pointer is not a pointer", func(t *testing.T, f reposHostileFixture) {
			writeFile(t, filepath.Join(f.s1, ".git"), "garbage\n")
		}, ""},
		{"Git directory is world-writable", func(t *testing.T, f reposHostileFixture) {
			if err := os.Chmod(gitDir(f, "s1"), 0o777); err != nil {
				t.Fatal(err)
			}
		}, "writable"},
		{"Git directory has alternates", func(t *testing.T, f reposHostileFixture) {
			writeFile(t, filepath.Join(gitDir(f, "s1"), "objects", "info", "alternates"), filepath.Join(f.base, "src", ".git", "objects")+"\n")
		}, "alternates"},
		{"Git directory has a commondir entry", func(t *testing.T, f reposHostileFixture) {
			writeFile(t, filepath.Join(gitDir(f, "s1"), "commondir"), "../..\n")
		}, "commondir"},
		{"core.worktree names another directory", func(t *testing.T, f reposHostileFixture) {
			git(t, f.base, "config", "-f", filepath.Join(gitDir(f, "s1"), "config"), "core.worktree", f.base)
		}, "core.worktree"},
		{"configuration includes another file", func(t *testing.T, f reposHostileFixture) {
			git(t, f.base, "config", "-f", filepath.Join(gitDir(f, "s1"), "config"), "include.path", "/etc/hostname")
		}, "include"},
		{"Git directory is a symlink", func(t *testing.T, f reposHostileFixture) {
			moved := filepath.Join(f.base, "moved-s1")
			if err := os.Rename(gitDir(f, "s1"), moved); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(moved, gitDir(f, "s1")); err != nil {
				t.Fatal(err)
			}
		}, "symlink"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newReposHostileFixture(t)
			tc.mutate(t, f)
			f.expectRefused(t, tc.want)
		})
	}
}

func TestReposDiscoverRefusesSymlinkedIntermediateComponent(t *testing.T) {
	reposHermetic(t)
	base := reposTempDir(t)
	super := reposNewRepo(t, filepath.Join(base, "super"))
	src := reposNewRepo(t, filepath.Join(base, "src"))
	git(t, super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", src, "d/s3")
	git(t, super, "commit", "-qm", "nested")
	moved := filepath.Join(base, "elsewhere")
	if err := os.Rename(filepath.Join(super, "d"), moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(moved, filepath.Join(super, "d")); err != nil {
		t.Fatal(err)
	}
	var seen []string
	var mu sync.Mutex
	res := reposRunWith([]string{super}, reposRecordingInspect(&seen, &mu), nil)
	e := reposEntry(t, res, "d/s3")
	if e.Status != "error" || e.Error == nil || e.Error.Code != "path-unsafe" || len(seen) != 1 {
		t.Fatalf("%+v %+v seen %v", e, e.Error, seen)
	}
}

// A submodule path that is a symlink to another listed repository must not be reported as a deduplicated
// path: the path is unsafe, whatever it points to (M2).
func TestReposDiscoverSymlinkedPathToExplicitRepositoryIsNotDeduplicated(t *testing.T) {
	f := newReposHostileFixture(t)
	other := reposNewRepo(t, filepath.Join(f.base, "other"))
	os.RemoveAll(f.s1)
	if err := os.Symlink(other, f.s1); err != nil {
		t.Fatal(err)
	}
	res := reposRun([]string{f.super, other}, true)
	e := reposEntry(t, res, "s1")
	if e.Error == nil || e.Error.Code != "path-unsafe" || res.Entries[0].Discovery.Deduplicated != 0 {
		t.Fatalf("%+v %+v discovery %+v", e, e.Error, res.Entries[0].Discovery)
	}
}

// Submodules checked out in a path whose permissions are acceptable stay inventoried when the repository is
// reached through a mode 0775 private group; group write is accepted for the effective group only (Spec 021).
func TestReposDiscoverAcceptsGroupWritableForEffectiveGroup(t *testing.T) {
	f := newReposHostileFixture(t)
	if err := os.Chmod(filepath.Join(f.super, ".git", "modules", "s1"), 0o775); err != nil {
		t.Fatal(err)
	}
	e := reposEntry(t, reposRun([]string{f.super}, true), "s1")
	if e.Status != "ok" {
		t.Fatalf("%+v %+v", e, e.Error)
	}
}

// H3: every identity inspection of a discovered submodule has its own per-call deadline, a panic in one is
// that entry's member-failed, and the others are unaffected.
func TestReposDiscoveredSubmoduleInspectionIsBounded(t *testing.T) {
	f := newReposHostileFixture(t)
	var mu sync.Mutex
	var deadlines = map[string]time.Duration{}
	inspect := func(ctx context.Context, p string) Result {
		if dl, ok := ctx.Deadline(); ok {
			mu.Lock()
			deadlines[p] = time.Until(dl)
			mu.Unlock()
		}
		if p == f.s1 {
			panic("boom")
		}
		return Inspect(ctx, p)
	}
	res := reposRunWith([]string{f.super}, inspect, nil)
	if e := reposEntry(t, res, "s1"); e.Status != "error" || e.Error == nil || e.Error.Code != "member-failed" {
		t.Fatalf("panic must be recovered into the entry: %+v %+v", e, e.Error)
	}
	if e := reposEntry(t, res, "s2"); e.Status != "ok" {
		t.Fatalf("sibling damaged: %+v %+v", e, e.Error)
	}
	for _, p := range []string{f.s1, f.s2} {
		if d, ok := deadlines[p]; !ok || d > (ReposPerRepositorySeconds+1)*time.Second {
			t.Errorf("%s: per-call deadline %v, want at most %d s", p, d, ReposPerRepositorySeconds)
		}
	}
}

// H3: a discovery command that hangs is bounded by its own deadline and becomes inventory-timeout on that
// parent; the other members are still reported.
func TestReposDiscoveryCommandTimeoutIsPerParent(t *testing.T) {
	reposHermetic(t)
	base := reposTempDir(t)
	slow := reposSuper(t, filepath.Join(base, "slow"), "s1")
	fine := reposNewRepo(t, filepath.Join(base, "fine"))
	realGit, err := lookGit()
	if err != nil {
		t.Skip(err)
	}
	shim := t.TempDir()
	script := "#!/bin/sh\ncase \"$PWD\" in */slow) for a in \"$@\"; do if [ \"$a\" = ls-tree ]; then exec sleep 20; fi; done;; esac\nexec " + realGit + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(shim, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shim+string(os.PathListSeparator)+os.Getenv("PATH"))
	start := time.Now()
	res := reposRunWith([]string{slow, fine}, nil, func(ctx context.Context) *pinsRunner {
		r := newReposRunner(ctx)
		r.timeout = 300 * time.Millisecond
		return r
	})
	if time.Since(start) > 10*time.Second {
		t.Fatalf("a hanging discovery command was not bounded: %v", time.Since(start))
	}
	d := res.Entries[0].Discovery
	if d.Status != "failed" || res.Entries[0].Error == nil || res.Entries[0].Error.Code != "inventory-timeout" {
		t.Fatalf("slow parent: discovery %+v error %+v", d, res.Entries[0].Error)
	}
	if res.Entries[1].Status != "ok" || res.Entries[1].Inventory == nil {
		t.Fatalf("fine member damaged: %+v", res.Entries[1])
	}
}

func lookGit() (string, error) {
	for _, d := range filepath.SplitList(os.Getenv("PATH")) {
		if p := filepath.Join(d, "git"); fileExecutable(p) {
			return p, nil
		}
	}
	return "", fmt.Errorf("git not found")
}

func fileExecutable(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0
}

func TestReposRunnerUsesTheIsolationAndPerCallBound(t *testing.T) {
	r := newReposRunner(context.Background())
	if r.timeout != ReposPerRepositorySeconds*time.Second {
		t.Fatalf("per-call bound %v", r.timeout)
	}
	if ReposPerRepositorySeconds != 10 || ReposTotalSeconds != 60 || ReposParallelism != 4 || ReposMaxOutputBytes != 8<<20 {
		t.Fatal("normative limits changed")
	}
	if got := strings.Join(pinsIsolation(), " "); !strings.Contains(got, "--literal-pathspecs") || !strings.Contains(got, "credential.helper=") || !strings.Contains(got, "core.hooksPath=/dev/null") {
		t.Fatalf("discovery isolation incomplete: %s", got)
	}
}
