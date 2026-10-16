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

// objectsTargetFixture builds a superproject whose member is main-worktree style and returns the modules
// directory, the member alternates file and a sibling bare repository `b`.
func objectsTargetFixture(t *testing.T) (f pinsFixture, s *pinsSafety, mods, alt, sib string) {
	t.Helper()
	f = newPinsFixture(t)
	s = safetyFor(t, f.Super)
	mods = filepath.Join(f.Super, ".git", "modules")
	sib = filepath.Join(mods, "b")
	git(t, f.Root, "clone", "-q", "--bare", f.Sub, sib)
	alt = filepath.Join(mods, "member", "objects", "info", "alternates")
	return
}

func TestPinsConfineAlternatesTargetContentsAreWalked(t *testing.T) {
	spec := pinsSubmoduleSpec{Path: "member", Name: "member"}
	f, s, mods, alt, sib := objectsTargetFixture(t)
	outside := filepath.Join(f.Root, "outside-pack")
	mkdirs(t, outside)
	writeFile(t, filepath.Join(outside, "x.pack"), "p")
	pack := filepath.Join(sib, "objects", "pack")
	// Healthy sibling passes.
	writeFile(t, alt, filepath.Join(sib, "objects")+"\n")
	mustResolve(t, s, spec)
	// A symlinked pack file inside the target.
	mustSymlink(t, filepath.Join(outside, "x.pack"), filepath.Join(pack, "evil.pack"))
	mustRefuse(t, s, spec, "submodule-path-unsafe")
	rmAll(t, filepath.Join(pack, "evil.pack"))
	// A FIFO in the target pack directory refuses without hanging.
	mustMkfifo(t, filepath.Join(pack, "fifo.pack"))
	start := time.Now()
	mustRefuse(t, s, spec, "submodule-path-unsafe")
	if time.Since(start) > 2*time.Second {
		t.Fatal("hung on a FIFO")
	}
	rmAll(t, filepath.Join(pack, "fifo.pack"))
	// A symlinked loose-object directory and a symlinked loose object (one level below the fan-out).
	mustSymlink(t, outside, filepath.Join(sib, "objects", "ab"))
	mustRefuse(t, s, spec, "submodule-path-unsafe")
	rmAll(t, filepath.Join(sib, "objects", "ab"))
	mkdirs(t, filepath.Join(sib, "objects", "cd"))
	mustSymlink(t, filepath.Join(outside, "x.pack"), filepath.Join(sib, "objects", "cd", "rest"))
	mustRefuse(t, s, spec, "submodule-path-unsafe")
	rmAll(t, filepath.Join(sib, "objects", "cd"))
	mustResolve(t, s, spec)
	// The modules directory itself is never a valid alternate.
	writeFile(t, alt, mods+"\n")
	mustRefuse(t, s, spec, "submodule-path-unsafe")
}

func TestPinsConfineInPlaceAlternatesTargetContentsAreWalked(t *testing.T) {
	f, member := inPlaceFixture(t)
	s := safetyFor(t, f.Super)
	spec := pinsSubmoduleSpec{Path: "member", Name: "member"}
	target := filepath.Join(member, ".git", "x", "objects")
	mkdirs(t, filepath.Join(target, "pack"))
	outside := filepath.Join(f.Root, "o")
	writeFile(t, outside, "p")
	writeFile(t, filepath.Join(member, ".git", "objects", "info", "alternates"), target+"\n")
	mustResolve(t, s, spec)
	mustSymlink(t, outside, filepath.Join(target, "pack", "evil.pack"))
	mustRefuse(t, s, spec, "submodule-path-unsafe")
}

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

func TestPinsConfineAlternatesQuotedEscapeIsRefused(t *testing.T) {
	f, s, _, alt, sib := objectsTargetFixture(t)
	_ = f
	writeFile(t, alt, "\""+filepath.Join(sib, "objects")+"\\n\"\n")
	if msg := mustRefuse(t, s, pinsSubmoduleSpec{Path: "member", Name: "member"}, "submodule-path-unsafe"); !strings.Contains(msg, "escape") {
		t.Fatal(msg)
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
	pinsBeforeOpen = func() {
		mustOS(t, os.Remove(filepath.Join(dir, "f")))
		writeFile(t, filepath.Join(dir, "f"), "swapped")
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
