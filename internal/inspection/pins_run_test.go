// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"bytes"
	"context"
	"errors"
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
