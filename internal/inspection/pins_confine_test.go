// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package inspection

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// inPlaceFixture returns a superproject whose `member` has an old-style in-place .git directory.
func inPlaceFixture(t *testing.T) (f pinsFixture, member string) {
	t.Helper()
	f = newPinsFixture(t)
	member = filepath.Join(f.Super, "member")
	if err := os.RemoveAll(member); err != nil {
		t.Fatal(err)
	}
	git(t, f.Root, "clone", "-q", f.Sub, member)
	return f, member
}

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func mustRename(t *testing.T, from, to string) {
	t.Helper()
	if err := os.Rename(from, to); err != nil {
		t.Fatal(err)
	}
}

func mustMkfifo(t *testing.T, path string) {
	t.Helper()
	if out, err := exec.Command("mkfifo", path).CombinedOutput(); err != nil {
		t.Skipf("mkfifo unavailable: %s %v", out, err)
	}
}

func TestPinsConfineInPlaceAllowedTreeIsOnlyItsOwnGitDirectory(t *testing.T) {
	f, member := inPlaceFixture(t)
	s := safetyFor(t, f.Super)
	spec := pinsSubmoduleSpec{Path: "member", Name: "member"}
	other := filepath.Join(f.Super, ".git", "modules", "sibling")
	git(t, f.Root, "clone", "-q", "--bare", f.Sub, other)
	mkdirs(t, filepath.Join(member, "inside", "objects"))
	alt := filepath.Join(member, ".git", "objects", "info", "alternates")
	for name, target := range map[string]string{
		"into the worktree":          filepath.Join(member, "inside", "objects"),
		"into the superproject .git": filepath.Join(f.Super, ".git", "objects"),
		"into a sibling submodule":   filepath.Join(other, "objects"),
		"relative into the worktree": "../../inside/objects",
	} {
		writeFile(t, alt, target+"\n")
		if msg := mustRefuse(t, s, spec, "submodule-path-unsafe"); !strings.Contains(msg, "alternates") {
			t.Errorf("%s: %s", name, msg)
		}
		rmAll(t, alt)
	}
	// commondir is refused wherever it points: a submodule Git directory never legitimately has one.
	cd := filepath.Join(member, ".git", "commondir")
	for name, target := range map[string]string{"worktree": "../inside", "super objects": filepath.Join(f.Super, ".git"), "sibling": other, "itself": "."} {
		writeFile(t, cd, target+"\n")
		if msg := mustRefuse(t, s, spec, "submodule-path-unsafe"); !strings.Contains(msg, "commondir") {
			t.Errorf("%s: %s", name, msg)
		}
	}
	rmAll(t, cd)
	mustResolve(t, s, spec)
}

func TestPinsConfineSymlinkedCommondirAndAlternatesReproducer(t *testing.T) {
	f, member := inPlaceFixture(t)
	outside := filepath.Join(f.Root, "outside")
	git(t, f.Root, "clone", "-q", "--bare", f.Sub, outside)
	mustSymlink(t, outside, filepath.Join(member, "esc"))
	writeFile(t, filepath.Join(member, ".git", "commondir"), "../esc\n")
	writeFile(t, filepath.Join(member, ".git", "objects", "info", "alternates"), filepath.Join(member, "esc", "objects")+"\n")
	mustRefuse(t, safetyFor(t, f.Super), pinsSubmoduleSpec{Path: "member", Name: "member"}, "submodule-path-unsafe")
}

func TestPinsConfineAlternatesTargetsAreCheckedWithoutFollowingLinks(t *testing.T) {
	f := newPinsFixture(t)
	s := safetyFor(t, f.Super)
	spec := pinsSubmoduleSpec{Path: "member", Name: "member"}
	mods := filepath.Join(f.Super, ".git", "modules")
	alt := filepath.Join(mods, "member", "objects", "info", "alternates")
	sib := filepath.Join(mods, "sibling")
	git(t, f.Root, "clone", "-q", "--bare", f.Sub, sib)
	// A sibling's objects are inside the allowed tree and pass every check.
	writeFile(t, alt, filepath.Join(sib, "objects")+"\n")
	mustResolve(t, s, spec)
	// A symlinked alternate directory inside the tree is not followed.
	mustSymlink(t, filepath.Join(f.Root, "sub", ".git", "objects"), filepath.Join(mods, "linkobjs"))
	writeFile(t, alt, filepath.Join(mods, "linkobjs")+"\n")
	mustRefuse(t, s, spec, "submodule-path-unsafe")
	// A symlinked parent component inside the tree is not followed either.
	mustSymlink(t, sib, filepath.Join(mods, "linkparent"))
	writeFile(t, alt, filepath.Join(mods, "linkparent", "objects")+"\n")
	mustRefuse(t, s, spec, "submodule-path-unsafe")
	// Nested alternates in the target are refused.
	writeFile(t, filepath.Join(sib, "objects", "info", "alternates"), filepath.Join(f.Sub, ".git", "objects")+"\n")
	writeFile(t, alt, filepath.Join(sib, "objects")+"\n")
	mustRefuse(t, s, spec, "submodule-path-unsafe")
	rmAll(t, filepath.Join(sib, "objects", "info", "alternates"))
	mustResolve(t, s, spec)
	// A missing target is refused: Git would silently ignore it, an audit should not.
	writeFile(t, alt, filepath.Join(mods, "absent", "objects")+"\n")
	mustRefuse(t, s, spec, "submodule-path-unsafe")
	// A target owned by someone else is refused.
	writeFile(t, alt, filepath.Join(sib, "objects")+"\n")
	l, run := layoutFor(t, f.Super)
	foreign := newPinsSafety(l, run)
	real := foreign.owner
	foreign.owner = func(i fs.FileInfo) (uint32, bool) {
		if p, err := os.Lstat(filepath.Join(sib, "objects")); err == nil && os.SameFile(p, i) {
			return foreign.euid + 1, true
		}
		return real(i)
	}
	mustRefuse(t, foreign, spec, "submodule-unsafe-owner")
}

func TestPinsConfineCommondirIsRefusedAndUnreadableFailsClosed(t *testing.T) {
	f := newPinsFixture(t)
	s := safetyFor(t, f.Super)
	spec := pinsSubmoduleSpec{Path: "member", Name: "member"}
	gd := filepath.Join(f.Super, ".git", "modules", "member")
	cd := filepath.Join(gd, "commondir")
	writeFile(t, cd, ".\n")
	mustRefuse(t, s, spec, "submodule-path-unsafe")
	rmAll(t, cd)
	mustSymlink(t, gd, cd)
	mustRefuse(t, s, spec, "submodule-path-unsafe")
	rmAll(t, cd)
	mustMkfifo(t, cd)
	mustRefuse(t, s, spec, "submodule-path-unsafe")
	rmAll(t, cd)
	mustResolve(t, s, spec)
}

func TestPinsConfineConfigWorktreeAndIncludes(t *testing.T) {
	f := newPinsFixture(t)
	s := safetyFor(t, f.Super)
	spec := pinsSubmoduleSpec{Path: "member", Name: "member"}
	gd := filepath.Join(f.Super, ".git", "modules", "member")
	elsewhere := filepath.Join(f.Root, "elsewhere")
	mkdirs(t, elsewhere)
	// core.worktree in config.worktree is a configuration Git loads when extensions.worktreeConfig is on.
	cw := filepath.Join(gd, "config.worktree")
	git(t, f.Root, "config", "--file", cw, "core.worktree", elsewhere)
	mustRefuse(t, s, spec, "submodule-path-unsafe")
	rmAll(t, cw)
	// A symlinked config.worktree.
	mustSymlink(t, filepath.Join(gd, "config"), cw)
	mustRefuse(t, s, spec, "submodule-path-unsafe")
	rmAll(t, cw)
	// include and includeIf keys import configuration the audit has not read.
	cfg := filepath.Join(gd, "config")
	orig, _ := os.ReadFile(cfg)
	for _, kv := range [][2]string{{"include.path", filepath.Join(f.Root, "inc.cfg")}, {"includeIf.gitdir:/x/.path", filepath.Join(f.Root, "inc.cfg")}} {
		git(t, f.Root, "config", "--file", cfg, kv[0], kv[1])
		if msg := mustRefuse(t, s, spec, "submodule-path-unsafe"); !strings.Contains(msg, "include") {
			t.Errorf("%s: %s", kv[0], msg)
		}
		mustOS(t, os.WriteFile(cfg, orig, 0o600))
	}
	mustResolve(t, s, spec)
}

func TestPinsConfineGitDirectoryEntriesMustBePlainFilesAndDirectories(t *testing.T) {
	f := newPinsFixture(t)
	spec := pinsSubmoduleSpec{Path: "member", Name: "member"}
	gd := filepath.Join(f.Super, ".git", "modules", "member")
	outside := filepath.Join(f.Root, "outside")
	mkdirs(t, outside)
	type mutate func(path string)
	fifo := func(p string) { rmAll(t, p); mustMkfifo(t, p) }
	link := func(p string) { rmAll(t, p); mustSymlink(t, outside, p) }
	for name, c := range map[string]struct {
		rel string
		do  mutate
	}{
		"config is a FIFO":             {"config", fifo},
		"HEAD is a FIFO":               {"HEAD", fifo},
		"index is a FIFO":              {"index", fifo},
		"packed-refs is a FIFO":        {"packed-refs", fifo},
		"shallow is a FIFO":            {"shallow", fifo},
		"info/exclude is a FIFO":       {"info/exclude", fifo},
		"info/attributes is a link":    {"info/attributes", link},
		"objects/pack is a symlink":    {"objects/pack", link},
		"objects/info is a symlink":    {"objects/info", link},
		"refs/heads is a symlink":      {"refs/heads", link},
		"a ref file is a FIFO":         {"refs/heads/fifo", fifo},
		"a deep ref dir is a symlink":  {"refs/remotes/origin", link},
		"a loose object dir is a link": {"objects/ab", link},
		"config.worktree is a FIFO":    {"config.worktree", fifo},
	} {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(gd, filepath.FromSlash(c.rel))
			os.MkdirAll(filepath.Dir(p), 0o700)
			var saved []byte
			if b, err := os.ReadFile(p); err == nil {
				saved = b
			}
			c.do(p)
			start := time.Now()
			// A runner with a short deadline: the check must refuse before Git could ever block.
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			run := newPinsRunner(ctx)
			l, err := newPinsSuperLayout(run, f.Super)
			if err != nil {
				t.Skipf("layout needs Git on the hostile entry: %v", err)
			}
			mustRefuse(t, newPinsSafety(l, run), spec, "submodule-path-unsafe")
			if time.Since(start) > 2*time.Second {
				t.Fatal("refusal took until the deadline")
			}
			rmAll(t, p)
			if saved != nil {
				mustOS(t, os.WriteFile(p, saved, 0o600))
			}
		})
	}
}

func TestPinsConfineIncludePathFifoDoesNotHang(t *testing.T) {
	f := newPinsFixture(t)
	gd := filepath.Join(f.Super, ".git", "modules", "member")
	fifo := filepath.Join(f.Root, "inc-fifo")
	mustMkfifo(t, fifo)
	git(t, f.Root, "config", "--file", filepath.Join(gd, "config"), "include.path", fifo)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	run := newPinsRunner(ctx)
	l, err := newPinsSuperLayout(run, f.Super)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	mustRefuse(t, newPinsSafety(l, run), pinsSubmoduleSpec{Path: "member", Name: "member"}, "submodule-path-unsafe")
	if time.Since(start) > 3*time.Second {
		t.Fatal("hung on an include of a FIFO")
	}
}

func TestPinsConfineRecheckRevalidatesGitDirectoryContents(t *testing.T) {
	f := newPinsFixture(t)
	s := safetyFor(t, f.Super)
	dirs := mustResolve(t, s, pinsSubmoduleSpec{Path: "member", Name: "member"})
	gd := dirs.GitDir
	if err := s.recheck(dirs); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(gd, "config")
	mustRename(t, cfg, cfg+".held")
	mustSymlink(t, cfg+".held", cfg)
	if err := s.recheck(dirs); err == nil {
		t.Fatal("a config replaced by a symlink after validation must fail the recheck")
	}
	rmAll(t, cfg)
	mustRename(t, cfg+".held", cfg)
	writeFile(t, filepath.Join(gd, "commondir"), ".\n")
	if err := s.recheck(dirs); err == nil {
		t.Fatal("a commondir added after validation must fail the recheck")
	}
}

func TestPinsConfineRefusesWritableSubmoduleDirectories(t *testing.T) {
	f := newPinsFixture(t)
	spec := pinsSubmoduleSpec{Path: "member", Name: "member"}
	work := filepath.Join(f.Super, "member")
	gd := filepath.Join(f.Super, ".git", "modules", "member")
	l, run := layoutFor(t, f.Super)
	for _, dir := range []string{work, gd} {
		fi, _ := os.Stat(dir)
		orig := fi.Mode().Perm()
		// World-writable: refused.
		mustOS(t, os.Chmod(dir, orig|0o002))
		mustRefuse(t, newPinsSafety(l, run), spec, "submodule-unsafe-owner")
		// Group-writable by the effective group: allowed (private-group umask 002 is common).
		mustOS(t, os.Chmod(dir, orig&^0o002|0o020))
		mustResolve(t, newPinsSafety(l, run), spec)
		// Group-writable by a foreign group: refused.
		s := newPinsSafety(l, run)
		s.gid = func(fs.FileInfo) (uint32, bool) { return s.egid + 1, true }
		mustRefuse(t, s, spec, "submodule-unsafe-owner")
		mustOS(t, os.Chmod(dir, orig))
	}
}

func TestPinsRunnerEnforcesAPerInvocationDeadline(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "git"), "#!/bin/sh\nsleep 30\n")
	mustOS(t, os.Chmod(filepath.Join(dir, "git"), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	run := newPinsRunner(context.Background())
	run.timeout = 300 * time.Millisecond
	start := time.Now()
	_, err := run.run(pinsCall{Dir: dir, Args: []string{"rev-parse", "HEAD"}})
	var limit *pinsLimitError
	if !errors.As(err, &limit) || limit.Kind != "deadline" {
		t.Fatalf("want a deadline limit, got %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("deadline not enforced")
	}
	if got := newPinsRunner(context.Background()).timeout; got != 5*time.Second {
		t.Fatalf("default per-invocation deadline %v, want 5s (FR-011)", got)
	}
}
