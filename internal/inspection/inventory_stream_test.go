// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// isolatedHome keeps the user's Git configuration out of tests that run the inventory's own Git
// processes, which drop GIT_* variables and therefore ignore GIT_CONFIG_GLOBAL.
func isolatedHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
}

func manyFilesRepo(t *testing.T, n int, dirPrefix string) string {
	t.Helper()
	d := canonical(t, repo(t))
	for i := 0; i < n; i++ {
		sub := filepath.Join(d, fmt.Sprintf("%sd%03d", dirPrefix, i/50))
		if i%50 == 0 {
			if err := os.MkdirAll(sub, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(sub, fmt.Sprintf("file%05d.txt", i)), []byte(fmt.Sprintf("content %d\n", i)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git(t, d, "add", "-A")
	git(t, d, "commit", "-qm", "init")
	return d
}

// fakeGit installs an executable named git in a fresh directory placed first on PATH. It runs the shim once
// with a probe argument (which exits at once) before returning: executing a file that was just written can fail with ETXTBSY when another
// goroutine forked while the file was still open, and the retry absorbs that instead of failing the test.
func fakeGit(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "git")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nif [ \"$1\" = \"--shim-probe\" ]; then exit 0; fi\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200; i++ {
		c := exec.Command(path, "--shim-probe")
		c.Stdin = strings.NewReader("")
		out, err := c.CombinedOutput()
		_ = out
		if err == nil || !strings.Contains(err.Error(), "text file busy") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func streamLines(t *testing.T, s gitStream, dir, delim string, args ...string) ([]string, bool, error) {
	t.Helper()
	var out []string
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second) // a stuck stream fails this test, not the package
	defer cancel()
	_, stopped, err := s(ctx, dir, nil, delim, args, func(r []byte) error {
		out = append(out, string(r))
		return nil
	})
	return out, stopped, err
}

func TestStreamSplitsRecordsAcrossChunks(t *testing.T) {
	isolatedHome(t)
	d := manyFilesRepo(t, 3000, "longprefix-longprefix-longprefix-")
	s := newStreamGit(inventoryBounds{Scan: 8 << 20, Record: outputLimit})
	got, stopped, err := streamLines(t, s, d, "\x00", "ls-files", "-z")
	if err != nil || stopped || len(got) != 3000 || got[0] != "longprefix-longprefix-longprefix-d000/file00000.txt" || got[2999] != "longprefix-longprefix-longprefix-d059/file02999.txt" {
		t.Fatalf("%d %v %v %v", len(got), stopped, err, got[:1])
	}
	// More than one 32 KiB chunk was needed, so records straddled chunk boundaries.
	if total := len(strings.Join(got, "\x00")); total <= 64<<10 {
		t.Fatalf("fixture too small: %d", total)
	}
	git(t, d, "tag", "one")
	refs, _, err := streamLines(t, s, d, "\x00\n", "for-each-ref", "--format=%(refname)%00%(objectname)%00")
	if err != nil || len(refs) != 2 || !strings.HasPrefix(refs[0], "refs/heads/main\x00") || !strings.HasPrefix(refs[1], "refs/tags/one\x00") {
		t.Fatalf("%q %v", refs, err)
	}
}

func TestStreamDeliversHostileNamesAsBytes(t *testing.T) {
	isolatedHome(t)
	d := repo(t)
	names := []string{"-dash", "new\nline", "glob[*]?", "sp ace"}
	for _, n := range names {
		os.WriteFile(filepath.Join(d, n), []byte("x"), 0o644)
	}
	git(t, d, "add", "-A")
	git(t, d, "commit", "-qm", "x")
	s := newStreamGit(inventoryBounds{Scan: 1 << 20, Record: outputLimit})
	got, _, err := streamLines(t, s, d, "\x00", "ls-files", "-z")
	if err != nil || len(got) != 4 {
		t.Fatalf("%q %v", got, err)
	}
	seen := map[string]bool{}
	for _, g := range got {
		seen[g] = true
	}
	for _, n := range names {
		if !seen[n] {
			t.Fatalf("missing %q in %q", n, got)
		}
	}
}

func TestStreamEnforcesRecordAndScanBounds(t *testing.T) {
	isolatedHome(t)
	d := manyFilesRepo(t, 300, "")
	var limit *streamLimitError
	s := newStreamGit(inventoryBounds{Scan: 1 << 20, Record: 8})
	if _, _, err := streamLines(t, s, d, "\x00", "ls-files", "-z"); !errors.As(err, &limit) || limit.Kind != "record" {
		t.Fatalf("record: %v", err)
	}
	s = newStreamGit(inventoryBounds{Scan: 1000, Record: outputLimit})
	if _, _, err := streamLines(t, s, d, "\x00", "ls-files", "-z"); !errors.As(err, &limit) || limit.Kind != "scan" {
		t.Fatalf("scan: %v", err)
	}
	// stderr counts toward the same allowance.
	s = newStreamGit(inventoryBounds{Scan: 10, Record: outputLimit})
	if _, _, err := streamLines(t, s, d, "\x00", "ls-files", "--no-such-option"); !errors.As(err, &limit) || limit.Kind != "scan" {
		t.Fatalf("stderr: %v", err)
	}
}

func TestStreamRejectsUnterminatedFinalRecord(t *testing.T) {
	isolatedHome(t)
	d := repo(t)
	os.WriteFile(filepath.Join(d, "a"), []byte("x"), 0o644)
	git(t, d, "add", "-A")
	s := newStreamGit(inventoryBounds{Scan: 1 << 20, Record: outputLimit})
	// Without -z the records end in LF, so a NUL delimiter never matches.
	if _, _, err := streamLines(t, s, d, "\x00", "ls-files"); !errors.Is(err, errStreamUnterminated) {
		t.Fatalf("%v", err)
	}
}

func TestStreamEarlyStopLeavesNoLockNoProcessAndNoIndexChange(t *testing.T) {
	isolatedHome(t)
	d := manyFilesRepo(t, 3000, "")
	indexPath := filepath.Join(d, ".git", "index")
	before, _ := os.ReadFile(indexPath)
	stBefore, _ := os.Stat(indexPath)
	s := newStreamGit(inventoryBounds{Scan: 8 << 20, Record: outputLimit})
	n := 0
	_, stopped, err := s(context.Background(), d, nil, "\x00", []string{"ls-files", "--stage", "-z"}, func([]byte) error {
		n++
		if n == 5 {
			return errStreamStop
		}
		return nil
	})
	if err != nil || !stopped || n != 5 {
		t.Fatalf("%d %v %v", n, stopped, err)
	}
	after, _ := os.ReadFile(indexPath)
	stAfter, _ := os.Stat(indexPath)
	if !bytes.Equal(before, after) || !stBefore.ModTime().Equal(stAfter.ModTime()) {
		t.Fatal("index changed")
	}
	matches, _ := filepath.Glob(filepath.Join(d, ".git", "*.lock"))
	if len(matches) != 0 {
		t.Fatalf("lock files %v", matches)
	}
}

func processGone(pid int) bool {
	for i := 0; i < 1000; i++ { // up to 20 s; returns as soon as the process is gone
		if syscall.Kill(pid, 0) != nil {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func readPID(t *testing.T, path string) int {
	t.Helper()
	var pid int
	for i := 0; i < 1500; i++ { // up to 30 s; the shim writes the file as soon as it runs
		b, _ := os.ReadFile(path)
		if _, err := fmt.Sscanf(string(b), "%d", &pid); err == nil && pid > 0 {
			return pid
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("no pid")
	return 0
}

func TestStreamStopOverflowAndDeadlineKillTheProcess(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	fakeGit(t, "echo $$ > "+pidFile+"\nwhile :; do printf 'a\\0'; done\n")
	s := newStreamGit(inventoryBounds{Scan: 1 << 16, Record: outputLimit})
	_, stopped, err := s(context.Background(), t.TempDir(), nil, "\x00", []string{"ls-files"}, func([]byte) error { return errStreamStop })
	if err != nil || !stopped || !processGone(readPID(t, pidFile)) {
		t.Fatalf("stop %v %v", stopped, err)
	}
	os.Remove(pidFile)
	var limit *streamLimitError
	_, _, err = s(context.Background(), t.TempDir(), nil, "\x00", []string{"ls-files"}, func([]byte) error { return nil })
	if !errors.As(err, &limit) || limit.Kind != "scan" || !processGone(readPID(t, pidFile)) {
		t.Fatalf("overflow %v", err)
	}
	os.Remove(pidFile)
	fakeGit(t, "echo $$ > "+pidFile+"\nexec sleep 30\n")
	ctx := newTriggeredDeadline()
	go func() { readPIDQuiet(pidFile); ctx.expire() }() // the deadline fires only after the process started
	_, _, err = s(ctx, t.TempDir(), nil, "\x00", []string{"ls-files"}, func([]byte) error { return nil })
	if !errors.Is(err, context.DeadlineExceeded) || !processGone(readPID(t, pidFile)) {
		t.Fatalf("deadline %v", err)
	}
}

func TestStreamUsesTheSharedIsolation(t *testing.T) {
	argFile := filepath.Join(t.TempDir(), "args")
	envFile := filepath.Join(t.TempDir(), "env")
	fakeGit(t, "printf '%s\\n' \"$@\" > "+argFile+"\nenv > "+envFile+"\n")
	t.Setenv("GIT_DIR", "/nonexistent-planted")
	t.Setenv("GIT_INDEX_FILE", "/nonexistent-planted")
	s := newStreamGit(inventoryBounds{Scan: 1 << 20, Record: outputLimit})
	if _, _, err := s(context.Background(), t.TempDir(), nil, "\x00", []string{"-c", "core.hooksPath=/dev/null", "ls-files", "-z"}, func([]byte) error { return nil }); err != nil {
		t.Fatal(err)
	}
	args, _ := os.ReadFile(argFile)
	if got, want := string(args), "--no-replace-objects\n-c\ncore.fsmonitor=false\n-c\ncore.hooksPath=/dev/null\nls-files\n-z\n"; got != want {
		t.Fatalf("args %q", got)
	}
	env, _ := os.ReadFile(envFile)
	for _, want := range []string{"GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GIT_NO_LAZY_FETCH=1", "GIT_PAGER=cat"} {
		if !strings.Contains(string(env), want) {
			t.Fatalf("missing %s", want)
		}
	}
	if strings.Contains(string(env), "nonexistent-planted") {
		t.Fatal("inherited GIT_ variables leaked")
	}
}

func TestStreamFeedsStandardInputConcurrentlyWithOutput(t *testing.T) {
	isolatedHome(t)
	d := manyFilesRepo(t, 20000, "")
	raw, err := exec_ls(d)
	if err != nil {
		t.Fatal(err)
	}
	s := newStreamGit(inventoryBounds{Scan: 64 << 20, Record: outputLimit})
	fields := 0
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	_, _, err = s(ctx, d, bytes.NewReader(raw), "\x00", []string{"check-attr", "--stdin", "-z", "filter", "text"}, func([]byte) error { fields++; return nil })
	if err != nil || fields != 20000*3*2 {
		t.Fatalf("%d %v", fields, err)
	}
	// An early stop while the writer still has data ends both.
	n := 0
	_, stopped, err := s(ctx, d, bytes.NewReader(raw), "\x00", []string{"check-attr", "--stdin", "-z", "filter", "text"}, func([]byte) error {
		n++
		if n == 7 {
			return errStreamStop
		}
		return nil
	})
	if err != nil || !stopped {
		t.Fatalf("%v %v", stopped, err)
	}
}

func exec_ls(d string) ([]byte, error) {
	out, code, err := runGit(context.Background(), d, "ls-files", "-z")
	if err != nil || code != 0 {
		// ls-files output of 20,000 paths exceeds the buffered cap; read it through the stream.
		s := newStreamGit(inventoryBounds{Scan: 64 << 20, Record: outputLimit})
		var b bytes.Buffer
		_, _, e := s(context.Background(), d, nil, "\x00", []string{"ls-files", "-z"}, func(r []byte) error { b.Write(r); b.WriteByte(0); return nil })
		return b.Bytes(), e
	}
	return []byte(out), nil
}

func TestBufferedRunnerAdapterSplitsAndKeepsInterposition(t *testing.T) {
	calls := 0
	run := func(ctx context.Context, dir string, args ...string) (string, int, error) {
		calls++
		return "a\x00b\x00", 0, nil
	}
	s := streamFromRunner(run, newStreamGit(inventoryBounds{Scan: 1 << 20, Record: outputLimit}))
	got, _, err := streamLines(t, s, "/", "\x00", "ls-files", "-z")
	if err != nil || calls != 1 || len(got) != 2 || got[1] != "b" {
		t.Fatalf("%v %d %q", err, calls, got)
	}
	run = func(ctx context.Context, dir string, args ...string) (string, int, error) { return "a\x00b", 0, nil }
	s = streamFromRunner(run, newStreamGit(inventoryBounds{Scan: 1 << 20, Record: outputLimit}))
	if _, _, err := streamLines(t, s, "/", "\x00", "ls-files", "-z"); !errors.Is(err, errStreamUnterminated) {
		t.Fatalf("%v", err)
	}
}

func execGit(t *testing.T, dir, stdin string, args ...string) string {
	t.Helper()
	c := exec.Command("git", append([]string{"-c", "maintenance.auto=false"}, args...)...)
	c.Dir = dir
	c.Stdin = strings.NewReader(stdin)
	c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.invalid")
	b, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s %v", args, b, err)
	}
	return string(b)
}

// printShim installs a git that prints the given printf format and exits.
func printShim(t *testing.T, format string) {
	t.Helper()
	fakeGit(t, "printf '"+format+"'\n")
}

func TestStreamBoundaryValues(t *testing.T) {
	dir := t.TempDir()
	var limit *streamLimitError
	run := func(b inventoryBounds, delim string) ([]string, error) {
		s := newStreamGit(b)
		var got []string
		_, _, err := s(context.Background(), dir, nil, delim, []string{"x"}, func(r []byte) error { got = append(got, string(r)); return nil })
		return got, err
	}
	// LIM-4: a record of exactly Record bytes is accepted, one byte more is not.
	printShim(t, "aaaaaaaaaa\\0")
	if got, err := run(inventoryBounds{Scan: 1 << 20, Record: 10}, "\x00"); err != nil || len(got) != 1 {
		t.Fatalf("record at the limit: %v %v", got, err)
	}
	printShim(t, "aaaaaaaaaaa\\0")
	if _, err := run(inventoryBounds{Scan: 1 << 20, Record: 10}, "\x00"); !errors.As(err, &limit) || limit.Kind != "record" {
		t.Fatalf("record over the limit: %v", err)
	}
	// Output that ends inside a record: at the limit it is unterminated, over it a record overrun.
	printShim(t, "aaaaaaaaaa")
	if _, err := run(inventoryBounds{Scan: 1 << 20, Record: 10}, "\x00"); !errors.Is(err, errStreamUnterminated) {
		t.Fatalf("unterminated at the limit: %v", err)
	}
	printShim(t, "aaaaaaaaaaa")
	if _, err := run(inventoryBounds{Scan: 1 << 20, Record: 10}, "\x00"); !errors.As(err, &limit) || limit.Kind != "record" {
		t.Fatalf("undelimited bytes over the limit: %v", err)
	}
	// A two-byte delimiter may be split: Record bytes plus the first delimiter byte are still a legal prefix.
	printShim(t, "aaaaaaaaaa\\0\\n")
	if got, err := run(inventoryBounds{Scan: 1 << 20, Record: 10}, "\x00\n"); err != nil || len(got) != 1 {
		t.Fatalf("two-byte delimiter: %v %v", got, err)
	}
	// LIM-3: exactly Scan bytes are accepted, one more is not.
	printShim(t, "aaaaaaaaaaaaaaaaaaa\\0")
	if got, err := run(inventoryBounds{Scan: 20, Record: 64}, "\x00"); err != nil || len(got) != 1 {
		t.Fatalf("scan at the limit: %v %v", got, err)
	}
	printShim(t, "aaaaaaaaaaaaaaaaaaaa\\0")
	if _, err := run(inventoryBounds{Scan: 20, Record: 64}, "\x00"); !errors.As(err, &limit) || limit.Kind != "scan" {
		t.Fatalf("scan over the limit: %v", err)
	}
}

func TestStreamKillsTheWholeProcessGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child")
	// The wrapper forks a grandchild that keeps the output pipe open, as a forking git wrapper would.
	fakeGit(t, "sh -c 'echo $$ > "+pidFile+"; exec sleep 120' &\nprintf 'a\\0'\nwait\n")
	s := newStreamGit(inventoryBounds{Scan: 1 << 20, Record: outputLimit})
	var pid int
	type outcome struct {
		stopped bool
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		_, stopped, err := s(context.Background(), t.TempDir(), nil, "\x00", []string{"x"}, func([]byte) error {
			pid = readPID(t, pidFile) // the grandchild has started before the stop is requested
			return errStreamStop
		})
		done <- outcome{stopped, err}
	}()
	select {
	case o := <-done:
		if o.err != nil || !o.stopped {
			t.Fatalf("%v %v", o.stopped, o.err)
		}
	case <-time.After(60 * time.Second): // a correct stop returns in milliseconds; only a surviving grandchild blocks
		if pid > 0 {
			syscall.Kill(pid, syscall.SIGKILL)
		}
		t.Fatal("the early stop waited for a grandchild that still held the output pipe")
	}
	if !processGone(pid) {
		t.Fatal("grandchild survived the early stop")
	}
}

func canonical(t *testing.T, p string) string {
	t.Helper()
	c, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// triggeredDeadline is a context whose deadline "fires" when the test says so, so a deadline test does not
// depend on how fast the machine starts the process it wants to block.
type triggeredDeadline struct {
	context.Context
	done chan struct{}
	once sync.Once
}

func newTriggeredDeadline() *triggeredDeadline {
	return &triggeredDeadline{Context: context.Background(), done: make(chan struct{})}
}
func (c *triggeredDeadline) expire()               { c.once.Do(func() { close(c.done) }) }
func (c *triggeredDeadline) Done() <-chan struct{} { return c.done }
func (c *triggeredDeadline) Err() error {
	select {
	case <-c.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

// readPIDQuiet waits for the shim's pid file without failing (the caller then expires the deadline either way).
func readPIDQuiet(path string) {
	for i := 0; i < 1500; i++ {
		if b, _ := os.ReadFile(path); len(b) > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}
