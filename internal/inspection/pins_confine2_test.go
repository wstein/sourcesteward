// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package inspection

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestPinsConfineReftableSharedIndexAndFanOutAreChecked(t *testing.T) {
	f := newPinsFixture(t)
	s := safetyFor(t, f.Super)
	spec := pinsSubmoduleSpec{Path: "member", Name: "member"}
	gd := filepath.Join(f.Super, ".git", "modules", "member")
	outside := filepath.Join(f.Root, "outside")
	mkdirs(t, outside)
	// reftable/ as a symlink, and a FIFO tables.list inside a real reftable directory.
	mustSymlink(t, outside, filepath.Join(gd, "reftable"))
	mustRefuse(t, s, spec, "submodule-path-unsafe")
	rmAll(t, filepath.Join(gd, "reftable"))
	mkdirs(t, filepath.Join(gd, "reftable"))
	mustMkfifo(t, filepath.Join(gd, "reftable", "tables.list"))
	mustRefuse(t, s, spec, "submodule-path-unsafe")
	rmAll(t, filepath.Join(gd, "reftable"))
	// sharedindex.<sha> must be a regular file.
	si := filepath.Join(gd, "sharedindex."+strings.Repeat("a", 40))
	mustSymlink(t, filepath.Join(gd, "config"), si)
	mustRefuse(t, s, spec, "submodule-path-unsafe")
	rmAll(t, si)
	writeFile(t, si, "x")
	mustResolve(t, s, spec)
	rmAll(t, si)
	// A loose object that is a symlink one level below the fan-out directory.
	mkdirs(t, filepath.Join(gd, "objects", "ab"))
	mustSymlink(t, filepath.Join(gd, "config"), filepath.Join(gd, "objects", "ab", strings.Repeat("c", 38)))
	mustRefuse(t, s, spec, "submodule-path-unsafe")
	rmAll(t, filepath.Join(gd, "objects", "ab"))
	// A regular file directly in objects/.
	writeFile(t, filepath.Join(gd, "objects", "stray"), "x")
	if msg := mustRefuse(t, s, spec, "submodule-path-unsafe"); !strings.Contains(msg, "not a directory") {
		t.Fatal(msg)
	}
}

func TestPinsConfineEntryBudgetIsALimitNotAnUnsafePath(t *testing.T) {
	f := newPinsFixture(t)
	s := safetyFor(t, f.Super)
	s.entryLimit = 3
	_, refusal, err := s.resolve(pinsSubmoduleSpec{Path: "member", Name: "member"})
	var limit *pinsLimitError
	if refusal != nil || !errors.As(err, &limit) || limit.Kind != "entries" {
		t.Fatalf("want an entries limit, got refusal=%v err=%v", refusal, err)
	}
}

func TestPinsReadSmallRefusesAFileSwappedBeforeOpen(t *testing.T) {
	dir := pinsRealDir(t)
	writeFile(t, filepath.Join(dir, "f"), "ok")
	other := filepath.Join(dir, "other")
	writeFile(t, other, "evil")
	defer func() { pinsBeforeOpen = nil }()
	// Replaced by a symlink between lstat and open: O_NOFOLLOW stops it.
	pinsBeforeOpen = func() {
		mustOS(t, os.Remove(filepath.Join(dir, "f")))
		mustSymlink(t, other, filepath.Join(dir, "f"))
	}
	if b, err := pinsReadSmall(dir, "f", 100); err == nil {
		t.Fatalf("a swapped symlink was read: %q", b)
	}
	// Replaced by a different regular file: the identity check stops it.
	rmAll(t, filepath.Join(dir, "f"))
	writeFile(t, filepath.Join(dir, "f"), "ok")
	// The new file is created while the old one still exists and then renamed
	// over it, so the two never share an inode (remove-then-create may reuse it).
	pinsBeforeOpen = func() {
		writeFile(t, filepath.Join(dir, "f.new"), "swapped")
		mustRename(t, filepath.Join(dir, "f.new"), filepath.Join(dir, "f"))
	}
	if b, err := pinsReadSmall(dir, "f", 100); err == nil {
		t.Fatalf("a swapped file was read: %q", b)
	}
	pinsBeforeOpen = nil
	if b, err := pinsReadSmall(dir, "f", 100); err != nil || string(b) != "swapped" {
		t.Fatalf("%q %v", b, err)
	}
	// A FIFO is refused without opening it.
	mustMkfifo(t, filepath.Join(dir, "p"))
	if _, err := pinsReadSmall(dir, "p", 100); err == nil {
		t.Fatal("FIFO read")
	}
}

func TestPinsRunnerKeepsOnlyTheFirstStderrBytesAndLimitsUnterminatedRecords(t *testing.T) {
	dir := t.TempDir()
	stub := func(body string) {
		writeFile(t, filepath.Join(dir, "git"), "#!/bin/sh\n"+body)
		mustOS(t, os.Chmod(filepath.Join(dir, "git"), 0o755))
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	stub("head -c 20000 /dev/zero | tr '\\0' e >&2\nexit 3\n")
	out, err := newPinsRunner(context.Background()).run(pinsCall{Dir: dir, Args: []string{"rev-parse"}})
	if err != nil || out.Exit != 3 || len(out.Stderr) != pinsStderrKeep {
		t.Fatalf("stderr kept %d bytes (want %d), exit %d, err %v", len(out.Stderr), pinsStderrKeep, out.Exit, err)
	}
	// An unterminated record longer than the record limit is its own limit.
	stub("head -c 100000 /dev/zero | tr '\\0' e\n")
	_, err = newPinsRunner(context.Background()).run(pinsCall{Dir: dir, Args: []string{"x"}, Keep: func([]byte) bool { return true }})
	var limit *pinsLimitError
	if !errors.As(err, &limit) || limit.Kind != "record" {
		t.Fatalf("want a record limit, got %v", err)
	}
	// A short unterminated final record is still offered to Keep.
	stub("printf abc\n")
	out, err = newPinsRunner(context.Background()).run(pinsCall{Dir: dir, Args: []string{"x"}, Keep: func(r []byte) bool { return string(r) == "abc" }})
	if err != nil || string(out.Stdout) != "abc" {
		t.Fatalf("%q %v", out.Stdout, err)
	}
}

func TestPinsEnvironmentRestrictsProtocolsAndUserTrust(t *testing.T) {
	env := strings.Join(pinsEnvironment(), "\n") + "\n"
	for _, w := range []string{"GIT_ALLOW_PROTOCOL=\n", "GIT_PROTOCOL_FROM_USER=0\n"} {
		if !strings.Contains(env, w) {
			t.Errorf("environment lacks %q", strings.TrimSpace(w))
		}
	}
}

// TestPinsProtocolIsolationBlocksFtpLazyFetch covers a transport the per-name overrides do not list: a
// partial clone whose remote is ftp:// to a local listener. The control proves Git connects; the audit must not.
func TestPinsProtocolIsolationBlocksFtpLazyFetch(t *testing.T) {
	f := newPinsFixture(t)
	git(t, f.Sub, "config", "uploadpack.allowFilter", "true")
	git(t, f.Sub, "config", "uploadpack.allowAnySHA1InWant", "true")
	partial := filepath.Join(f.Root, "partial")
	git(t, f.Root, "-c", "protocol.file.allow=always", "clone", "-q", "--filter=blob:none", "--no-checkout", "file://"+f.Sub, partial)
	blob := gitIn(t, f.Sub, "rev-parse", "HEAD:file.txt")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	defer ln.Close()
	hits := make(chan struct{}, 100)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			hits <- struct{}{}
			c.Close()
		}
	}()
	git(t, partial, "config", "remote.origin.url", "ftp://"+ln.Addr().String()+"/r")
	git(t, partial, "config", "protocol.ftp.allow", "always")
	c := exec.Command("git", "cat-file", "-p", blob)
	c.Dir = partial
	c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	_ = c.Run()
	select {
	case <-hits:
	case <-time.After(time.Second):
		t.Skip("control: this Git has no ftp transport (git-remote-ftp); the fixture cannot prove isolation here")
	}
	for len(hits) > 0 {
		<-hits
	}
	run := newPinsRunner(context.Background())
	run.env = func(env []string) []string {
		kept := env[:0:0]
		for _, e := range env {
			if !strings.HasPrefix(e, "GIT_NO_LAZY_FETCH=") {
				kept = append(kept, e)
			}
		}
		return kept
	}
	if _, err := run.run(pinsCall{Repo: pinsGitRepo{GitDir: filepath.Join(partial, ".git"), WorkTree: partial}, Args: []string{"cat-file", "-p", blob}}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-hits:
		t.Fatal("an ftp:// remote was contacted through lazy fetch")
	case <-time.After(500 * time.Millisecond):
	}
}

// TestPinsRefusesWorldWritableDirectoriesWithAnActionableMessage pins the behavior under umask 000, as in
// this devcontainer: Git creates every submodule directory world-writable and the audit refuses it,
// saying why and how to fix it. It does not rely on TestMain's umask.
func TestPinsRefusesWorldWritableDirectoriesWithAnActionableMessage(t *testing.T) {
	old := syscall.Umask(0)
	defer syscall.Umask(old)
	f := newPinsFixture(t)
	syscall.Umask(old) // creation is done; validate under the normal umask
	s := safetyFor(t, f.Super)
	msg := mustRefuse(t, s, pinsSubmoduleSpec{Path: "member", Name: "member"}, "submodule-unsafe-owner")
	for _, want := range []string{"world-writable", "mode", "umask", "chmod"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q lacks %q", msg, want)
		}
	}
}

// Under umask 000 every directory Git creates is world-writable; chmod-ing only the worktree and the top
// of the Git directory is not enough, because Git can be raced through any directory it reads.
func TestPinsWritableCheckCoversEveryWalkedDirectoryAndTheModulesDirectory(t *testing.T) {
	old := syscall.Umask(0)
	f := newPinsFixture(t)
	syscall.Umask(old)
	spec := pinsSubmoduleSpec{Path: "member", Name: "member"}
	s := safetyFor(t, f.Super)
	gd := filepath.Join(f.Super, ".git", "modules", "member")
	for _, d := range []string{filepath.Join(f.Super, "member"), gd} {
		mustOS(t, os.Chmod(d, 0o755))
	}
	msg := mustRefuse(t, s, spec, "submodule-unsafe-owner")
	for _, want := range []string{"chmod -R", "umask"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q lacks %q", msg, want)
		}
	}
	// Fix the whole submodule tree but leave the modules directory itself world-writable.
	for _, d := range []string{gd, filepath.Join(f.Super, "member")} {
		if out, err := exec.Command("chmod", "-R", "o-w", d).CombinedOutput(); err != nil {
			t.Fatalf("%s %v", out, err)
		}
	}
	mods := filepath.Join(f.Super, ".git", "modules")
	mustOS(t, os.Chmod(mods, 0o777))
	mustRefuse(t, s, spec, "submodule-unsafe-owner")
	mustOS(t, os.Chmod(mods, 0o755))
	mustResolve(t, s, spec)
	// One deep directory is enough to refuse.
	deep := filepath.Join(gd, "refs", "heads")
	mustOS(t, os.Chmod(deep, 0o777))
	mustRefuse(t, s, spec, "submodule-unsafe-owner")
}

func TestPinsConfineCommitGraphChainIsWalked(t *testing.T) {
	f := newPinsFixture(t)
	s := safetyFor(t, f.Super)
	gd := filepath.Join(f.Super, ".git", "modules", "member")
	dir := filepath.Join(gd, "objects", "info", "commit-graphs")
	mkdirs(t, dir)
	writeFile(t, filepath.Join(dir, "commit-graph-chain"), "x\n")
	mustResolve(t, s, pinsSubmoduleSpec{Path: "member", Name: "member"})
	mustSymlink(t, filepath.Join(gd, "config"), filepath.Join(dir, "graph-"+strings.Repeat("a", 40)+".graph"))
	mustRefuse(t, s, pinsSubmoduleSpec{Path: "member", Name: "member"}, "submodule-path-unsafe")
}

func TestPinsRecheckConvertsAnExhaustedWalkBudgetToALimit(t *testing.T) {
	f := newPinsFixture(t)
	s := safetyFor(t, f.Super)
	dirs := mustResolve(t, s, pinsSubmoduleSpec{Path: "member", Name: "member"})
	s.entryLimit = 3
	var limit *pinsLimitError
	if err := s.recheck(dirs); !errors.As(err, &limit) || limit.Kind != "entries" || limit.Error() == "" || !strings.Contains(limit.Error(), "entries") {
		t.Fatalf("%v", err)
	}
}

func TestPinsReadDirRefusesADirectorySwappedForALinkBeforeOpen(t *testing.T) {
	base := pinsRealDir(t)
	mkdirs(t, filepath.Join(base, "d"), filepath.Join(base, "other"))
	writeFile(t, filepath.Join(base, "other", "evil"), "x")
	defer func() { pinsBeforeOpen = nil }()
	pinsBeforeOpen = func() {
		mustOS(t, os.Remove(filepath.Join(base, "d")))
		mustSymlink(t, filepath.Join(base, "other"), filepath.Join(base, "d"))
	}
	s := &pinsSafety{owner: pinsFileOwner, euid: pinsEffectiveUID(), gid: pinsFileGroup, egid: pinsEffectiveGID(), entryLimit: 10}
	budget := 10
	if entries, r := s.readDir(base, "d", "test", &budget); r == nil {
		t.Fatalf("a swapped directory was listed: %v", entries)
	}
}

// partialClone returns a blob-less clone whose single missing blob forces a lazy fetch, and that blob.
func partialClone(t *testing.T, f pinsFixture) (clone, blob string) {
	t.Helper()
	git(t, f.Sub, "config", "uploadpack.allowFilter", "true")
	git(t, f.Sub, "config", "uploadpack.allowAnySHA1InWant", "true")
	clone = filepath.Join(f.Root, "partial")
	git(t, f.Root, "-c", "protocol.file.allow=always", "clone", "-q", "--filter=blob:none", "--no-checkout", "file://"+f.Sub, clone)
	return clone, gitIn(t, f.Sub, "rev-parse", "HEAD:file.txt")
}

func controlLazyFetch(clone, blob string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, "git", "cat-file", "-p", blob)
	c.Dir = clone
	c.WaitDelay = time.Second
	c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	_ = c.Run()
}

func isolatedLazyFetch(t *testing.T, clone, blob string) {
	t.Helper()
	run := newPinsRunner(context.Background())
	run.env = func(env []string) []string {
		kept := env[:0:0]
		for _, e := range env {
			if !strings.HasPrefix(e, "GIT_NO_LAZY_FETCH=") {
				kept = append(kept, e)
			}
		}
		return kept
	}
	if _, err := run.run(pinsCall{Repo: pinsGitRepo{GitDir: filepath.Join(clone, ".git"), WorkTree: clone}, Args: []string{"cat-file", "-p", blob}}); err != nil {
		t.Fatal(err)
	}
}

func TestPinsProtocolIsolationBlocksFtpsAndHelperTransports(t *testing.T) {
	f := newPinsFixture(t)
	clone, blob := partialClone(t, f)
	// A recording remote helper for helper::address. fd:: cannot be observed: Git runs its built-in helper
	// before any PATH stub, and the runner passes no extra descriptor, so it is not covered here.
	bin, logs := t.TempDir(), t.TempDir()
	log := filepath.Join(logs, "helper.log")
	for _, name := range []string{"git-remote-pinshelper"} {
		writeFile(t, filepath.Join(bin, name), "#!/bin/sh\nprintf '%s\\n' \""+name+" $*\" >> '"+log+"'\nexit 1\n")
		mustOS(t, os.Chmod(filepath.Join(bin, name), 0o755))
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	logged := func() bool { b, _ := os.ReadFile(log); return len(b) > 0 }
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	defer ln.Close()
	hits := make(chan struct{}, 100)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			hits <- struct{}{}
			c.Close()
		}
	}()
	for _, tc := range []struct {
		name, url, protocol string
		contacted           func() bool
	}{
		{"ftps", "ftps://" + ln.Addr().String() + "/r", "ftps", func() bool {
			select {
			case <-hits:
				return true
			case <-time.After(time.Second):
				return false
			}
		}},
		{"helper", "pinshelper::addr", "pinshelper", logged},
	} {
		t.Run(tc.name, func(t *testing.T) {
			git(t, clone, "config", "remote.origin.url", tc.url)
			git(t, clone, "config", "protocol."+tc.protocol+".allow", "always")
			rmAll(t, log)
			for len(hits) > 0 {
				<-hits
			}
			controlLazyFetch(clone, blob)
			if !tc.contacted() {
				t.Skipf("control: this Git did not use the %s transport; isolation cannot be proven here", tc.name)
			}
			rmAll(t, log)
			for len(hits) > 0 {
				<-hits
			}
			isolatedLazyFetch(t, clone, blob)
			if len(hits) > 0 || logged() {
				t.Fatalf("the %s remote was contacted through lazy fetch", tc.name)
			}
		})
	}
}

func chmodNonDirs(t *testing.T, root string, mode func(os.FileInfo) os.FileMode) {
	t.Helper()
	mustOS(t, filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			fi, _ := d.Info()
			return os.Chmod(p, mode(fi))
		}
		return nil
	}))
}

func TestPinsWritableCheckCoversEveryPathComponent(t *testing.T) {
	f := newPinsFixture(t)
	git(t, f.Super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", "--name", "x/y", f.Sub, "dir/sub")
	git(t, f.Super, "commit", "-qm", "nested")
	s := safetyFor(t, f.Super)
	spec := pinsSubmoduleSpec{Path: "dir/sub", Name: "x/y"}
	mustResolve(t, s, spec)
	mods := filepath.Join(f.Super, ".git", "modules")
	for name, d := range map[string]string{"parent of a nested path": filepath.Join(f.Super, "dir"), "name directory a/": filepath.Join(mods, "x")} {
		old, _ := os.Stat(d)
		mustOS(t, os.Chmod(d, 0o777))
		mustRefuse(t, s, spec, "submodule-unsafe-owner")
		mustOS(t, os.Chmod(d, old.Mode().Perm()))
		_ = name
	}
	mustOS(t, os.Chmod(mods, 0o777))
	if msg := mustRefuse(t, s, spec, "submodule-unsafe-owner"); !strings.Contains(msg, ".git/modules") || !strings.Contains(msg, "chmod -R o-w") {
		t.Fatal(msg)
	}
}

func TestPinsWritableCheckCoversFilesUnderUmask000(t *testing.T) {
	old := syscall.Umask(0)
	f := newPinsFixture(t)
	syscall.Umask(old)
	spec := pinsSubmoduleSpec{Path: "member", Name: "member"}
	gd := filepath.Join(f.Super, ".git", "modules", "member")
	// Fix every directory but leave the files as Git created them (0666): still refused.
	for _, root := range []string{filepath.Join(f.Super, ".git", "modules"), filepath.Join(f.Super, "member")} {
		mustOS(t, filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				return os.Chmod(p, 0o755)
			}
			return err
		}))
	}
	s := safetyFor(t, f.Super)
	mustRefuse(t, s, spec, "submodule-unsafe-owner")
	// Each fixed-list file on its own.
	chmodNonDirs(t, gd, func(fi os.FileInfo) os.FileMode { return fi.Mode().Perm() &^ 0o022 })
	mustResolve(t, s, spec)
	for _, name := range []string{"HEAD", "config", "index", "packed-refs", "shallow", "config.worktree"} {
		p := filepath.Join(gd, name)
		created := false
		if _, err := os.Stat(p); err != nil {
			writeFile(t, p, "x")
			created = true
		}
		fi, _ := os.Stat(p)
		mustOS(t, os.Chmod(p, fi.Mode().Perm()|0o002))
		if name == "config.worktree" || name == "shallow" || name == "packed-refs" || name == "index" {
			// these may be unparseable as written; only the refusal reason matters
			if _, refusal, _ := s.resolve(spec); refusal == nil {
				t.Errorf("%s world-writable accepted", name)
			}
		} else {
			mustRefuse(t, s, spec, "submodule-unsafe-owner")
		}
		if created {
			rmAll(t, p)
		} else {
			mustOS(t, os.Chmod(p, fi.Mode().Perm()&^0o022))
		}
	}
	// A world-writable empty alternates file and a world-writable walked regular entry (a ref).
	alt := filepath.Join(gd, "objects", "info", "alternates")
	writeFile(t, alt, "")
	mustOS(t, os.Chmod(alt, 0o666))
	mustRefuse(t, s, spec, "submodule-unsafe-owner")
	rmAll(t, alt)
	ref := filepath.Join(gd, "refs", "heads", "main")
	if _, err := os.Stat(ref); err == nil {
		mustOS(t, os.Chmod(ref, 0o666))
		mustRefuse(t, s, spec, "submodule-unsafe-owner")
	}
}

func TestPinsFixtureUnderUmask002Resolves(t *testing.T) {
	old := syscall.Umask(0o002)
	defer syscall.Umask(old)
	f := newPinsFixture(t)
	mustResolve(t, safetyFor(t, f.Super), pinsSubmoduleSpec{Path: "member", Name: "member"})
}

func TestPinsReadDirRefusesAnotherRealDirectoryRenamedIntoPlace(t *testing.T) {
	base := pinsRealDir(t)
	mkdirs(t, filepath.Join(base, "d"), filepath.Join(base, "other"))
	defer func() { pinsBeforeOpen = nil }()
	pinsBeforeOpen = func() {
		mustRename(t, filepath.Join(base, "d"), filepath.Join(base, "d.old"))
		mustRename(t, filepath.Join(base, "other"), filepath.Join(base, "d"))
	}
	s := &pinsSafety{owner: pinsFileOwner, euid: pinsEffectiveUID(), gid: pinsFileGroup, egid: pinsEffectiveGID(), entryLimit: 10}
	budget := 10
	if _, r := s.readDir(base, "d", "test", &budget); r == nil || !strings.Contains(r.Message, "changed while opening") {
		t.Fatalf("%v", r)
	}
}

func TestPinsRecheckSeesASameSizeInPlaceConfigRewrite(t *testing.T) {
	f := newPinsFixture(t)
	s := safetyFor(t, f.Super)
	d := mustResolve(t, s, pinsSubmoduleSpec{Path: "member", Name: "member"})
	cfg := filepath.Join(d.GitDir, "config")
	info, err := os.Stat(cfg)
	mustOS(t, err)
	b, err := os.ReadFile(cfg)
	mustOS(t, err)
	time.Sleep(30 * time.Millisecond)
	b[len(b)-2] ^= 0x01 // same size, and the modification time is put back below
	mustOS(t, os.WriteFile(cfg, b, 0o600))
	mustOS(t, os.Chtimes(cfg, info.ModTime(), info.ModTime()))
	if err := s.recheck(d); err == nil {
		t.Fatal("a same-size rewrite with the old modification time passed the recheck")
	}
}

func TestPinsConfigIdentityFailsOnAnyErrorButAbsence(t *testing.T) {
	f := newPinsFixture(t)
	s := safetyFor(t, f.Super)
	d := mustResolve(t, s, pinsSubmoduleSpec{Path: "member", Name: "member"})
	if _, err := s.configIdentity(d); err != nil {
		t.Fatalf("a missing config.worktree is fine: %v", err)
	}
	d.gitRel += "/HEAD" // HEAD is a file, so config below it fails with ENOTDIR, not ENOENT
	if _, err := s.configIdentity(d); err == nil {
		t.Fatal("an unreadable configuration compared equal")
	}
	if err := s.recheck(d); err == nil {
		t.Fatal("recheck passed an unreadable configuration")
	}
}
