// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func memberRepo(f pinsFixture) pinsGitRepo {
	return pinsGitRepo{GitDir: filepath.Join(f.Super, ".git", "modules", "member"), WorkTree: filepath.Join(f.Super, "member")}
}

func TestPinsRunnerUsesExplicitRepositoryAndIsolation(t *testing.T) {
	f := newPinsFixture(t)
	gitLog, _ := pinsSentinels(t)
	// An inherited index or repository selection must not reach Git: the runner strips GIT_*.
	t.Setenv("GIT_INDEX_FILE", filepath.Join(f.Root, "nonexistent-index"))
	t.Setenv("GIT_DIR", filepath.Join(f.Root, "nonexistent-gitdir"))
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.bare")
	t.Setenv("GIT_CONFIG_VALUE_0", "bogus")
	r := memberRepo(f)
	out, err := newPinsRunner(context.Background()).run(pinsCall{Repo: r, Args: []string{"rev-parse", "HEAD"}})
	if err != nil || out.Exit != 0 || strings.TrimSpace(string(out.Stdout)) != f.Pin {
		t.Fatalf("rev-parse: %q %+v %v", out.Stdout, out, err)
	}
	lines := gitLog()
	last := lines[len(lines)-1]
	for _, want := range []string{"--git-dir=" + r.GitDir, "--work-tree=" + r.WorkTree, "--no-replace-objects", "--no-optional-locks", "core.fsmonitor=false", "core.hooksPath=/dev/null", "credential.helper=", "protocol.allow=never"} {
		if !strings.Contains(last, want) {
			t.Fatalf("invocation lacks %q: %s", want, last)
		}
	}
}

func TestPinsRunnerReturnsExitCodeAndCancellation(t *testing.T) {
	f := newPinsFixture(t)
	r := memberRepo(f)
	out, err := newPinsRunner(context.Background()).run(pinsCall{Repo: r, Args: []string{"cat-file", "-e", strings.Repeat("0", 40) + "^{commit}"}})
	if err != nil || out.Exit == 0 {
		t.Fatalf("absent object must be a nonzero exit, not an error: %+v %v", out, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := newPinsRunner(ctx).run(pinsCall{Repo: r, Args: []string{"rev-parse", "HEAD"}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("want cancellation, got %v", err)
	}
}

func TestPinsRunnerKeepsOnlyWantedRecords(t *testing.T) {
	f := newPinsFixture(t)
	run := newPinsRunner(context.Background())
	out, err := run.run(pinsCall{Repo: pinsGitRepo{GitDir: filepath.Join(f.Super, ".git"), WorkTree: f.Super}, Args: []string{"ls-tree", "-r", "-z", "HEAD"},
		Keep: func(rec []byte) bool { return bytes.HasPrefix(rec, []byte("160000 ")) }})
	if err != nil {
		t.Fatal(err)
	}
	recs := bytes.Split(bytes.TrimSuffix(out.Stdout, []byte{0}), []byte{0})
	if len(recs) != 1 || !bytes.HasSuffix(recs[0], []byte("\tmember")) {
		t.Fatalf("records: %q", out.Stdout)
	}
	if run.budget.retained != len(out.Stdout) {
		t.Fatalf("budget %d, kept %d", run.budget.retained, len(out.Stdout))
	}
}

func TestPinsRunnerRetainedBudgetIsSharedAndReportsLimit(t *testing.T) {
	f := newPinsFixture(t)
	run := newPinsRunner(context.Background())
	run.budget.limit = 100
	repo := pinsGitRepo{GitDir: filepath.Join(f.Super, ".git"), WorkTree: f.Super}
	call := pinsCall{Repo: repo, Args: []string{"ls-tree", "-r", "-z", "HEAD"}} // two records, about 60 bytes each
	_, err := run.run(call)
	var limit *pinsLimitError
	if !errors.As(err, &limit) || limit.Kind != "retained" {
		t.Fatalf("want retained limit, got %v", err)
	}
	// The same budget refuses a second small call once it is exhausted: it is not per call.
	run2 := newPinsRunner(context.Background())
	run2.budget.limit = 100
	small := pinsCall{Repo: repo, Args: []string{"rev-parse", "HEAD"}}
	for i := 0; i < 3; i++ {
		if _, err = run2.run(small); err != nil {
			break
		}
	}
	if !errors.As(err, &limit) {
		t.Fatalf("budget did not accumulate across calls: %v", err)
	}
}

func TestPinsRunnerScanAllowanceStopsProcess(t *testing.T) {
	f := newPinsFixture(t)
	for i := 0; i < 50; i++ {
		writeFile(t, filepath.Join(f.Super, "many", strings.Repeat("n", 40)+string(rune('a'+i%26))+strings.Repeat("m", i)), "x")
	}
	git(t, f.Super, "add", "-A")
	run := newPinsRunner(context.Background())
	run.scan = 512
	start := time.Now()
	_, err := run.run(pinsCall{Repo: pinsGitRepo{GitDir: filepath.Join(f.Super, ".git"), WorkTree: f.Super}, Args: []string{"ls-files", "--stage", "-z"},
		Keep: func([]byte) bool { return false }})
	var limit *pinsLimitError
	if !errors.As(err, &limit) || limit.Kind != "scan" {
		t.Fatalf("want scan limit, got %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("process was not stopped")
	}
}

func TestPinsRunnerFeedsStdin(t *testing.T) {
	f := newPinsFixture(t)
	out, err := newPinsRunner(context.Background()).run(pinsCall{Repo: pinsGitRepo{GitDir: filepath.Join(f.Super, ".git"), WorkTree: f.Super},
		Args: []string{"config", "--file", "-", "--null", "--list", "--no-includes"}, Stdin: []byte("[submodule \"x\"]\n\tpath = y\n")})
	if err != nil || string(out.Stdout) != "submodule.x.path\ny\x00" {
		t.Fatalf("%q %v", out.Stdout, err)
	}
}

func TestPinsIsolationListIsExactlyTheDocumentedOne(t *testing.T) {
	want := []string{
		"--no-replace-objects", "--no-optional-locks", "--literal-pathspecs",
		"-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "-c", "core.untrackedCache=false",
		"-c", "credential.helper=", "-c", "core.sshCommand=/bin/false",
		"-c", "protocol.allow=never", "-c", "protocol.ext.allow=never", "-c", "protocol.file.allow=never",
		"-c", "protocol.ssh.allow=never", "-c", "protocol.git.allow=never", "-c", "protocol.http.allow=never", "-c", "protocol.https.allow=never",
		"-c", "gc.auto=0", "-c", "maintenance.auto=false",
	}
	if got := pinsIsolation(); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("isolation prefix changed:\n got %v\nwant %v", got, want)
	}
	env := strings.Join(pinsEnvironment(), "\n")
	for _, w := range []string{"GIT_CONFIG_NOSYSTEM=1", "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GIT_NO_LAZY_FETCH=1", "LC_ALL=C"} {
		if !strings.Contains(env, w) {
			t.Errorf("environment lacks %s", w)
		}
	}
}

func TestPinsRunnerNeverHonorsReplaceObjects(t *testing.T) {
	f := newPinsFixture(t)
	git(t, f.Sub, "commit", "--allow-empty", "-qm", "replacement")
	repl := gitIn(t, f.Sub, "rev-parse", "HEAD")
	member := filepath.Join(f.Super, "member")
	git(t, member, "fetch", "-q", "origin")
	git(t, member, "replace", f.Pin, repl)
	out, err := newPinsRunner(context.Background()).run(pinsCall{Repo: memberRepo(f), Args: []string{"cat-file", "-p", f.Pin}})
	if err != nil || strings.Contains(string(out.Stdout), "replacement") || !strings.Contains(string(out.Stdout), "sub one") {
		t.Fatalf("a replace ref was honored: %q %v", out.Stdout, err)
	}
	if plain := git(t, member, "cat-file", "-p", f.Pin); !strings.Contains(plain, "replacement") {
		t.Fatalf("control: plain Git must honor the replace ref: %q", plain)
	}
}

func TestPinsRunnerDoesNotRefreshTheIndex(t *testing.T) {
	f := newPinsFixture(t)
	member := filepath.Join(f.Super, "member")
	idx := filepath.Join(f.Super, ".git", "modules", "member", "index")
	// Touch a tracked file so a refreshing command would rewrite the index.
	old := time.Now().Add(-time.Hour)
	os.Chtimes(filepath.Join(member, "file.txt"), old, old)
	before, _ := os.ReadFile(idx)
	st, _ := os.Stat(idx)
	for _, args := range [][]string{{"ls-files", "--stage", "--debug", "-z"}, {"diff-index", "--cached", "--no-ext-diff", "--no-textconv", "HEAD"}, {"ls-files", "--others", "--exclude-standard", "-z"}} {
		if _, err := newPinsRunner(context.Background()).run(pinsCall{Repo: memberRepo(f), Args: args}); err != nil {
			t.Fatal(err)
		}
	}
	after, _ := os.ReadFile(idx)
	st2, _ := os.Stat(idx)
	if string(before) != string(after) || !st.ModTime().Equal(st2.ModTime()) {
		t.Fatal("the index was rewritten")
	}
}

func TestPinsRunnerReportsAnOverlongRecordAsItsOwnLimit(t *testing.T) {
	f := newPinsFixture(t)
	_, err := newPinsRunner(context.Background()).run(pinsCall{Repo: pinsGitRepo{GitDir: filepath.Join(f.Super, ".git"), WorkTree: f.Super},
		Args: []string{"config", "--file", "-", "--null", "--list"}, Stdin: []byte("[a]\n\tb = " + strings.Repeat("x", pinsRecordLimit+10) + "\n"),
		Keep: func([]byte) bool { return true }})
	var limit *pinsLimitError
	if !errors.As(err, &limit) || limit.Kind != "record" {
		t.Fatalf("want a record limit, got %v", err)
	}
}

func TestPinsRunnerStderrCountsTowardTheScanAllowance(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "git"), "#!/bin/sh\nyes 'noise noise noise' | head -c 200000 >&2\nsleep 1\n")
	os.Chmod(filepath.Join(dir, "git"), 0o755)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	run := newPinsRunner(context.Background())
	run.scan = 1000
	_, err := run.run(pinsCall{Dir: dir, Args: []string{"rev-parse", "HEAD"}})
	var limit *pinsLimitError
	if !errors.As(err, &limit) || limit.Kind != "scan" {
		t.Fatalf("stderr noise must hit the scan allowance: %v", err)
	}
}
