// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPinsRunnerStreamsRecordsWithoutRetaining(t *testing.T) {
	f := newPinsFixture(t)
	run := newPinsRunner(context.Background())
	var recs []string
	var lasts []bool
	out, err := run.run(pinsCall{Repo: memberRepo(f), Args: []string{"ls-tree", "-r", "-z", "HEAD"}, Each: func(rec []byte, last bool) error {
		recs, lasts = append(recs, string(rec)), append(lasts, last)
		return nil
	}})
	if err != nil || out.Exit != 0 || out.Stopped || len(out.Stdout) != 0 {
		t.Fatalf("%+v %v", out, err)
	}
	if len(recs) != 1 || !strings.HasSuffix(recs[0], "\tfile.txt") || lasts[0] {
		t.Fatalf("records %q last=%v", recs, lasts)
	}
	if run.budget.retained != 0 {
		t.Fatalf("a streamed record was retained: %d", run.budget.retained)
	}
}

func TestPinsRunnerStreamDeliversAnUnterminatedTail(t *testing.T) {
	f := newPinsFixture(t)
	var recs []string
	var lasts []bool
	_, err := newPinsRunner(context.Background()).run(pinsCall{Repo: memberRepo(f), Args: []string{"rev-parse", "HEAD"}, Each: func(rec []byte, last bool) error {
		recs, lasts = append(recs, string(rec)), append(lasts, last)
		return nil
	}})
	if err != nil || len(recs) != 1 || recs[0] != f.Pin+"\n" || !lasts[0] {
		t.Fatalf("%q %v %v", recs, lasts, err)
	}
}

func TestPinsRunnerStreamStopEndsTheProcessWithoutAnError(t *testing.T) {
	f := newPinsFixture(t)
	for i := 0; i < 200; i++ {
		writeFile(t, filepath.Join(f.Super, "many", strings.Repeat("n", 60)+fmt.Sprint(i)), "x")
	}
	git(t, f.Super, "add", "-A")
	seen := 0
	out, err := newPinsRunner(context.Background()).run(pinsCall{Repo: pinsGitRepo{GitDir: filepath.Join(f.Super, ".git"), WorkTree: f.Super}, Args: []string{"ls-files", "--stage", "-z"},
		Each: func([]byte, bool) error { seen++; return errPinsStop }})
	if err != nil || !out.Stopped || seen != 1 {
		t.Fatalf("%+v seen=%d %v", out, seen, err)
	}
}

func TestPinsRunnerStreamCallbackErrorIsReturned(t *testing.T) {
	f := newPinsFixture(t)
	boom := errors.New("boom")
	_, err := newPinsRunner(context.Background()).run(pinsCall{Repo: memberRepo(f), Args: []string{"ls-tree", "-r", "-z", "HEAD"}, Each: func([]byte, bool) error { return boom }})
	if !errors.Is(err, boom) {
		t.Fatalf("%v", err)
	}
}

func TestPinsRunnerCallTimeoutIsADeadlineLimit(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "git"), "#!/bin/sh\nsleep 5\n")
	mustOS(t, os.Chmod(filepath.Join(dir, "git"), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	start := time.Now()
	_, err := newPinsRunner(context.Background()).run(pinsCall{Dir: dir, Args: []string{"rev-parse", "HEAD"}, Timeout: 100 * time.Millisecond})
	var limit *pinsLimitError
	if !errors.As(err, &limit) || limit.Kind != "deadline" {
		t.Fatalf("want a deadline limit, got %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("the per-call timeout did not apply")
	}
	// A timeout longer than the runner's default does not extend it.
	r := newPinsRunner(context.Background())
	r.timeout = 100 * time.Millisecond
	if _, err = r.run(pinsCall{Dir: dir, Args: []string{"rev-parse", "HEAD"}, Timeout: time.Hour}); !errors.As(err, &limit) {
		t.Fatalf("a longer call timeout extended the default: %v", err)
	}
}
