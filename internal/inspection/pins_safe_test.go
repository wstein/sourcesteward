// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package inspection

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Environment limits recorded for Spec 021 T006/T007 (see the verification note):
//   - The machine runs Git 2.56.0. The minimum, Git 2.46, is not available, so everything touching
//     extensions.submodulePathConfig (and its encoded name `x%2fy`) only runs on Git 2.56 and skips on
//     an older Git; linked-worktree, in-place and pointer cases are version independent.
//   - Tests run as an unprivileged user and cannot chown. Foreign ownership is therefore exercised only
//     through the injected owner seam; the real owner extraction is exercised against a root-owned
//     directory where one exists. Real foreign-owned submodules, macOS and root (SUDO_UID) are NOT exercised.

func layoutFor(t *testing.T, superRoot string) (pinsSuperLayout, *pinsRunner) {
	t.Helper()
	run := newPinsRunner(context.Background())
	l, err := newPinsSuperLayout(run, superRoot)
	if err != nil {
		t.Fatal(err)
	}
	return l, run
}

func safetyFor(t *testing.T, superRoot string) *pinsSafety {
	t.Helper()
	l, run := layoutFor(t, superRoot)
	return newPinsSafety(l, run)
}

func mustResolve(t *testing.T, s *pinsSafety, spec pinsSubmoduleSpec) pinsSubmoduleDirs {
	t.Helper()
	res, refusal, err := s.resolve(spec)
	if err != nil || refusal != nil || res.Uninitialized || res.Dirs == nil {
		t.Fatalf("resolve %+v: %+v refusal=%v err=%v", spec, res, refusal, err)
	}
	return *res.Dirs
}

func mustRefuse(t *testing.T, s *pinsSafety, spec pinsSubmoduleSpec, code string) string {
	t.Helper()
	res, refusal, err := s.resolve(spec)
	if err != nil || refusal == nil || refusal.Code != code || refusal.Message == "" || res.Dirs != nil {
		t.Fatalf("resolve %+v: want refusal %s: %+v refusal=%v err=%v", spec, code, res, refusal, err)
	}
	return refusal.Message
}

func TestPinsSafeResolvesMainWorktreeSubmodule(t *testing.T) {
	f := newPinsFixture(t)
	l, run := layoutFor(t, f.Super)
	if l.GitDir != filepath.Join(f.Super, ".git") || l.ModulesDir != filepath.Join(f.Super, ".git", "modules") || l.PathConfig {
		t.Fatalf("layout %+v", l)
	}
	dirs := mustResolve(t, newPinsSafety(l, run), pinsSubmoduleSpec{Path: "member", Name: "member"})
	if dirs.WorkTree != filepath.Join(f.Super, "member") || dirs.GitDir != filepath.Join(l.ModulesDir, "member") || dirs.InPlace {
		t.Fatalf("%+v", dirs)
	}
	if dirs.WorkID == (pinsDirID{}) || dirs.GitID == (pinsDirID{}) || dirs.WorkID == dirs.GitID {
		t.Fatalf("directory identity not recorded: %+v", dirs)
	}
}

func TestPinsSafeLinkedWorktreeUsesItsOwnModulesDirectory(t *testing.T) {
	f := newPinsFixture(t)
	wt := filepath.Join(f.Root, "linked")
	git(t, f.Super, "worktree", "add", "-q", wt)
	git(t, wt, "-c", "protocol.file.allow=always", "submodule", "update", "--init", "-q")
	l, run := layoutFor(t, wt)
	want := filepath.Join(f.Super, ".git", "worktrees", "linked", "modules")
	if l.ModulesDir != want {
		t.Fatalf("modules dir %q, want %q", l.ModulesDir, want)
	}
	dirs := mustResolve(t, newPinsSafety(l, run), pinsSubmoduleSpec{Path: "member", Name: "member"})
	if dirs.GitDir != filepath.Join(want, "member") || dirs.WorkTree != filepath.Join(wt, "member") {
		t.Fatalf("%+v", dirs)
	}
	// The main worktree is unaffected and keeps its own location.
	main := mustResolve(t, safetyFor(t, f.Super), pinsSubmoduleSpec{Path: "member", Name: "member"})
	if main.GitDir == dirs.GitDir {
		t.Fatal("main and linked worktree must not share a Git directory")
	}
}

func TestPinsSafeInPlaceGitDirectory(t *testing.T) {
	f := newPinsFixture(t)
	git(t, f.Root, "clone", "-q", f.Sub, filepath.Join(f.Super, "legacy"))
	s := safetyFor(t, f.Super)
	dirs := mustResolve(t, s, pinsSubmoduleSpec{Path: "legacy", Name: "legacy"})
	if !dirs.InPlace || dirs.GitDir != filepath.Join(f.Super, "legacy", ".git") {
		t.Fatalf("%+v", dirs)
	}
	// An in-place directory with an alternates entry leaving the worktree is refused.
	writeFile(t, filepath.Join(f.Super, "legacy", ".git", "objects", "info", "alternates"), f.Sub+"/.git/objects\n")
	mustRefuse(t, s, pinsSubmoduleSpec{Path: "legacy", Name: "legacy"}, "submodule-path-unsafe")
}

func TestPinsSafeSubmodulePathConfigNameWithSlash(t *testing.T) {
	requireSubmodulePathConfig(t)
	f := newPinsFixture(t)
	git(t, f.Super, "config", "core.repositoryformatversion", "1")
	git(t, f.Super, "config", "extensions.submodulePathConfig", "true")
	git(t, f.Super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", "--name", "x/y", f.Sub, "slashy")
	git(t, f.Super, "commit", "-qm", "slashy")
	ptr, _ := os.ReadFile(filepath.Join(f.Super, "slashy", ".git"))
	if !strings.Contains(string(ptr), "x%2fy") {
		t.Skipf("this Git does not encode the name (pointer %q)", ptr)
	}
	l, run := layoutFor(t, f.Super)
	if !l.PathConfig {
		t.Fatalf("extension not detected: %+v", l)
	}
	s := newPinsSafety(l, run)
	configured := gitIn(t, f.Super, "config", "submodule.x/y.gitdir")
	dirs := mustResolve(t, s, pinsSubmoduleSpec{Path: "slashy", Name: "x/y", ConfiguredGitDir: &configured})
	if dirs.GitDir != filepath.Join(l.ModulesDir, "x%2fy") {
		t.Fatalf("%+v", dirs)
	}
	// The configured value is untrusted: outside the modules path is refused with its own message.
	var msgs = map[string]bool{}
	for _, bad := range []string{"../../outside", "/etc", ".git/modules/../../escape", ".git/modules", filepath.Join(f.Root, "elsewhere")} {
		bad := bad
		msgs[mustRefuse(t, s, pinsSubmoduleSpec{Path: "slashy", Name: "x/y", ConfiguredGitDir: &bad}, "submodule-path-unsafe")] = true
	}
	if !msgs["the configured submodule gitdir is not confined under the modules directory"] {
		t.Fatalf("messages %v", msgs)
	}
	// Without the configured value the plain name leads to a missing directory, never to the encoded one.
	mustRefuse(t, s, pinsSubmoduleSpec{Path: "slashy", Name: "x/y"}, "submodule-path-unsafe")
}

func TestPinsSafeUninitializedIsNotRefused(t *testing.T) {
	f := newPinsFixture(t)
	s := safetyFor(t, f.Super)
	mkdirs(t, filepath.Join(f.Super, "empty"))
	for _, p := range []string{"empty", "missing", "no/such/parent"} {
		res, refusal, err := s.resolve(pinsSubmoduleSpec{Path: p, Name: p})
		if err != nil || refusal != nil || !res.Uninitialized || res.Dirs != nil {
			t.Fatalf("%s: %+v %v %v", p, res, refusal, err)
		}
	}
}

func TestPinsSafeRefusesUnsafePathsWithDistinctMessages(t *testing.T) {
	f := newPinsFixture(t)
	s := safetyFor(t, f.Super)
	cases := map[string]string{
		"..":        "contains a '..' component",
		"a/../b":    "contains a '..' component",
		"/abs/path": "is absolute",
		"a//b":      "contains an empty or '.' component",
		"./a":       "contains an empty or '.' component",
		"a/.git/b":  "contains a '.git' component",
		"a/.GIT":    "contains a '.git' component",
		"a\xffb":    "is not valid UTF-8",
		"a\x00b":    "contains a NUL byte",
		"":          "is empty",
		"trailing/": "contains an empty or '.' component",
	}
	seen := map[string]string{}
	for p, want := range cases {
		got := mustRefuse(t, s, pinsSubmoduleSpec{Path: p, Name: "n"}, "submodule-path-unsafe")
		if !strings.Contains(got, want) {
			t.Errorf("%q: message %q lacks %q", p, got, want)
		}
		if prev, dup := seen[want]; !dup {
			seen[want] = got
		} else if prev != got {
			t.Errorf("message for %q differs within one reason: %q vs %q", p, got, prev)
		}
	}
	if len(seen) < 7 {
		t.Fatalf("messages are not distinct: %v", seen)
	}
	// Unsafe names are refused too: they would address a directory outside the modules path.
	for _, name := range []string{"..", "a/../../b", "/abs", "a//b", ""} {
		mustRefuse(t, s, pinsSubmoduleSpec{Path: "member", Name: name}, "submodule-path-unsafe")
	}
}

func TestPinsSafeRefusesSymlinks(t *testing.T) {
	f := newPinsFixture(t)
	elsewhere := filepath.Join(f.Root, "elsewhere")
	mkdirs(t, filepath.Join(elsewhere, "sub"))
	s := safetyFor(t, f.Super)
	// A symlinked submodule directory.
	if err := os.RemoveAll(filepath.Join(f.Super, "member")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(f.Super, "member")); err != nil {
		t.Fatal(err)
	}
	mustRefuse(t, s, pinsSubmoduleSpec{Path: "member", Name: "member"}, "submodule-path-unsafe")
	// A symlinked parent component.
	if err := os.Symlink(elsewhere, filepath.Join(f.Super, "link")); err != nil {
		t.Fatal(err)
	}
	mustRefuse(t, s, pinsSubmoduleSpec{Path: "link/sub", Name: "n"}, "submodule-path-unsafe")
}

func TestPinsSafeRefusesSymlinkedGitFileAndGitDirectoryParts(t *testing.T) {
	f := newPinsFixture(t)
	s := safetyFor(t, f.Super)
	spec := pinsSubmoduleSpec{Path: "member", Name: "member"}
	elsewhere := filepath.Join(f.Root, "elsewhere")
	mkdirs(t, elsewhere)
	// .git as a symlink.
	dotgit := filepath.Join(f.Super, "member", ".git")
	saved, _ := os.ReadFile(dotgit)
	rmAll(t, dotgit)
	if err := os.Symlink(elsewhere, dotgit); err != nil {
		t.Fatal(err)
	}
	mustRefuse(t, s, spec, "submodule-path-unsafe")
	rmAll(t, dotgit)
	mustOS(t, os.WriteFile(dotgit, saved, 0o600))
	mustResolve(t, s, spec)
	// A symlinked part inside the Git directory.
	gd := filepath.Join(f.Super, ".git", "modules", "member")
	for _, part := range []string{"objects", "HEAD", "config", "refs"} {
		orig := filepath.Join(gd, part)
		hold := orig + ".held"
		if err := os.Rename(orig, hold); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(hold, orig); err != nil { // even a link to a sibling is refused: never follow
			t.Fatal(err)
		}
		mustRefuse(t, s, spec, "submodule-path-unsafe")
		rmAll(t, orig)
		mustOS(t, os.Rename(hold, orig))
	}
	mustResolve(t, s, spec)
	// A symlinked modules directory.
	mods := filepath.Join(f.Super, ".git", "modules")
	mustOS(t, os.Rename(mods, mods+".held"))
	if err := os.Symlink(mods+".held", mods); err != nil {
		t.Fatal(err)
	}
	mustRefuse(t, s, spec, "submodule-path-unsafe")
}

func TestPinsSafeRefusesPointerOutsideOrUnexpected(t *testing.T) {
	f := newPinsFixture(t)
	s := safetyFor(t, f.Super)
	spec := pinsSubmoduleSpec{Path: "member", Name: "member"}
	dotgit := filepath.Join(f.Super, "member", ".git")
	outside := filepath.Join(f.Root, "outside")
	git(t, f.Root, "clone", "-q", "--bare", f.Sub, outside)
	other := filepath.Join(f.Super, ".git", "modules", "other")
	git(t, f.Root, "clone", "-q", "--bare", f.Sub, other)
	for name, content := range map[string]string{
		"absolute outside":     "gitdir: " + outside + "\n",
		"relative escape":      "gitdir: ../../../outside\n",
		"another submodule":    "gitdir: ../.git/modules/other\n",
		"the superproject dir": "gitdir: ../.git\n",
		"no prefix":            "../.git/modules/member\n",
		"two lines":            "gitdir: ../.git/modules/member\nextra\n",
		"nul":                  "gitdir: ../.git/modules/member\x00\n",
		"empty":                "gitdir: \n",
		"non-utf8":             "gitdir: ../.git/modules/m\xffember\n",
	} {
		if err := os.WriteFile(dotgit, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if msg := mustRefuse(t, s, spec, "submodule-path-unsafe"); msg == "" {
			t.Fatal(name)
		}
	}
	// The correct pointer in both relative and absolute spelling resolves.
	for _, ok := range []string{"gitdir: ../.git/modules/member\n", "gitdir: " + filepath.Join(f.Super, ".git", "modules", "member") + "\n"} {
		mustOS(t, os.WriteFile(dotgit, []byte(ok), 0o600))
		mustResolve(t, s, spec)
	}
}

func TestPinsSafeRefusesCommondirAlternatesAndWorktreeLeavingTheAllowedTree(t *testing.T) {
	f := newPinsFixture(t)
	s := safetyFor(t, f.Super)
	spec := pinsSubmoduleSpec{Path: "member", Name: "member"}
	gd := filepath.Join(f.Super, ".git", "modules", "member")
	outside := filepath.Join(f.Root, "outside")
	mkdirs(t, outside)
	check := func(name string, setup, undo func()) {
		t.Helper()
		setup()
		msg := mustRefuse(t, s, spec, "submodule-path-unsafe")
		undo()
		mustResolve(t, s, spec)
		t.Logf("%s: %s", name, msg)
	}
	commondir := filepath.Join(gd, "commondir")
	check("commondir outside", func() { writeFile(t, commondir, outside+"\n") }, func() { rmAll(t, commondir) })
	check("commondir relative escape", func() { writeFile(t, commondir, "../../../../outside\n") }, func() { rmAll(t, commondir) })
	alt := filepath.Join(gd, "objects", "info", "alternates")
	check("alternates outside", func() { writeFile(t, alt, outside+"/objects\n") }, func() { rmAll(t, alt) })
	check("alternates relative escape", func() { writeFile(t, alt, "# c\n../../../../../../outside\n") }, func() { rmAll(t, alt) })
	check("alternates quoted outside", func() { writeFile(t, alt, "\""+outside+"\"\n") }, func() { rmAll(t, alt) })
	check("alternates symlink", func() {
		mustOS(t, os.Symlink(outside, alt))
	}, func() { rmAll(t, alt) })
	cfg := filepath.Join(gd, "config")
	orig, _ := os.ReadFile(cfg)
	restore := func() { mustOS(t, os.WriteFile(cfg, orig, 0o600)) }
	check("core.worktree elsewhere", func() { git(t, f.Root, "config", "--file", cfg, "core.worktree", outside) }, restore)
	check("core.worktree relative escape", func() { git(t, f.Root, "config", "--file", cfg, "core.worktree", "../../../../outside") }, restore)
	// core.worktree unset is fine: Git is always told the validated worktree explicitly.
	git(t, f.Root, "config", "--file", cfg, "--unset", "core.worktree")
	mustResolve(t, s, spec)
	restore()
}

func TestPinsSafeNoGitProcessRunsAgainstARefusedLocation(t *testing.T) {
	f := newPinsFixture(t)
	s := safetyFor(t, f.Super)
	outside := filepath.Join(f.Root, "outside")
	git(t, f.Root, "clone", "-q", "--bare", f.Sub, outside)
	writeFile(t, filepath.Join(f.Super, "member", ".git"), "gitdir: "+outside+"\n")
	gitLog, _ := pinsSentinels(t)
	mustRefuse(t, s, pinsSubmoduleSpec{Path: "member", Name: "member"}, "submodule-path-unsafe")
	for _, line := range gitLog() {
		if strings.Contains(line, outside) || strings.Contains(line, "member") {
			t.Fatalf("a Git process touched the refused submodule: %s", line)
		}
	}
}

func TestPinsSafeRecheckDetectsReplacedDirectories(t *testing.T) {
	f := newPinsFixture(t)
	s := safetyFor(t, f.Super)
	dirs := mustResolve(t, s, pinsSubmoduleSpec{Path: "member", Name: "member"})
	if err := s.recheck(dirs); err != nil {
		t.Fatal(err)
	}
	// Replace the worktree directory by an identical copy: same path, different inode.
	w := filepath.Join(f.Super, "member")
	mustOS(t, os.Rename(w, w+".old"))
	mkdirs(t, w)
	if err := os.Rename(filepath.Join(w+".old", ".git"), filepath.Join(w, ".git")); err != nil {
		t.Fatal(err)
	}
	if err := s.recheck(dirs); err == nil {
		t.Fatal("a replaced worktree directory must fail the recheck")
	}
	// A directory replaced by a symlink also fails.
	rmAll(t, w)
	mustOS(t, os.Symlink(f.Root, w))
	if err := s.recheck(dirs); err == nil {
		t.Fatal("a symlink must fail the recheck")
	}
}

func TestPinsSafeRecheckDetectsReplacedGitDirectory(t *testing.T) {
	f := newPinsFixture(t)
	s := safetyFor(t, f.Super)
	dirs := mustResolve(t, s, pinsSubmoduleSpec{Path: "member", Name: "member"})
	gd := dirs.GitDir
	mustOS(t, os.Rename(gd, gd+".old"))
	if err := os.Rename(gd+".old", gd+".new"); err != nil {
		t.Fatal(err)
	}
	mustOS(t, os.Rename(gd+".new", gd)) // same directory moved back: identity unchanged
	if err := s.recheck(dirs); err != nil {
		t.Fatalf("an unchanged directory must pass: %v", err)
	}
	mkdirs(t, gd+".copy")
	rmAll(t, gd)
	mustOS(t, os.Rename(gd+".copy", gd))
	if err := s.recheck(dirs); err == nil {
		t.Fatal("a replaced Git directory must fail the recheck")
	}
}

// ---- T007: ownership --------------------------------------------------------------------------

func TestPinsOwnerCheckAcceptsEffectiveUserRealFiles(t *testing.T) {
	f := newPinsFixture(t)
	s := safetyFor(t, f.Super) // default seam: real lstat owner against the real effective user
	mustResolve(t, s, pinsSubmoduleSpec{Path: "member", Name: "member"})
}

func TestPinsOwnerExtractionReadsTheRealOwner(t *testing.T) {
	// A file this test creates belongs to the effective user.
	own := filepath.Join(t.TempDir(), "own")
	writeFile(t, own, "x")
	info, err := os.Lstat(own)
	if err != nil {
		t.Fatal(err)
	}
	if uid, ok := pinsFileOwner(info); !ok || uid != uint32(os.Geteuid()) || pinsEffectiveUID() != uint32(os.Geteuid()) {
		t.Fatalf("owner of a created file: uid %d ok=%v, effective %d", uid, ok, os.Geteuid())
	}
	// The root directory belongs to root on every supported Unix host.
	root, err := os.Lstat("/")
	if err != nil {
		t.Fatal(err)
	}
	if uid, ok := pinsFileOwner(root); !ok || uid != 0 {
		t.Fatalf("owner of /: uid %d ok=%v", uid, ok)
	}
}

func TestPinsOwnerUnknownFailsClosedEvenForRoot(t *testing.T) {
	f := newPinsFixture(t)
	l, run := layoutFor(t, f.Super)
	s := newPinsSafety(l, run)
	s.euid = 0 // an unknown owner reported as 0 must not match the effective user 0
	s.owner = func(fs.FileInfo) (uint32, bool) { return 0, false }
	mustRefuse(t, s, pinsSubmoduleSpec{Path: "member", Name: "member"}, "submodule-unsafe-owner")
}

func TestPinsConfigurationThatCannotBeReadFailsClosed(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read a mode 000 file")
	}
	f := newPinsFixture(t)
	s := safetyFor(t, f.Super)
	cfg := filepath.Join(f.Super, ".git", "modules", "member", "config")
	mustOS(t, os.Chmod(cfg, 0))
	defer os.Chmod(cfg, 0o600)
	mustRefuse(t, s, pinsSubmoduleSpec{Path: "member", Name: "member"}, "submodule-path-unsafe")
}

func TestPinsOwnerCheckRefusesBeforeAnyGitProcess(t *testing.T) {
	f := newPinsFixture(t)
	l, run := layoutFor(t, f.Super)
	started := noProcessPath(t) // every git/jj/sh start through PATH is recorded and fails
	me := pinsEffectiveUID()
	member := filepath.Join(f.Super, "member")
	gitdir := filepath.Join(f.Super, ".git", "modules", "member")
	foreign := func(path string) *pinsSafety {
		s := newPinsSafety(l, run)
		s.euid = me
		s.owner = func(info fs.FileInfo) (uint32, bool) {
			real, ok := pinsFileOwner(info)
			if p, err := os.Lstat(path); err == nil && os.SameFile(p, info) {
				return me + 1, true
			}
			return real, ok
		}
		return s
	}
	spec := pinsSubmoduleSpec{Path: "member", Name: "member"}
	for name, path := range map[string]string{"worktree": member, "git directory": gitdir} {
		msg := mustRefuse(t, foreign(path), spec, "submodule-unsafe-owner")
		if !strings.Contains(strings.ToLower(msg), name) {
			t.Errorf("message %q does not name the %s", msg, name)
		}
	}
	// The owner cannot be determined: refuse, fail closed.
	s := newPinsSafety(l, run)
	s.owner = func(fs.FileInfo) (uint32, bool) { return 0, false }
	mustRefuse(t, s, spec, "submodule-unsafe-owner")
	if got := started(); len(got) != 0 {
		t.Fatalf("a process started before the ownership decision: %v", got)
	}
}

func TestPinsOwnerCheckAppliesToInPlaceGitDirectory(t *testing.T) {
	f := newPinsFixture(t)
	git(t, f.Root, "clone", "-q", f.Sub, filepath.Join(f.Super, "legacy"))
	l, run := layoutFor(t, f.Super)
	s := newPinsSafety(l, run)
	dotgit := filepath.Join(f.Super, "legacy", ".git")
	s.owner = func(info fs.FileInfo) (uint32, bool) {
		real, ok := pinsFileOwner(info)
		if p, err := os.Lstat(dotgit); err == nil && os.SameFile(p, info) {
			return real + 1, true
		}
		return real, ok
	}
	mustRefuse(t, s, pinsSubmoduleSpec{Path: "legacy", Name: "legacy"}, "submodule-unsafe-owner")
}
