// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// reposGitShim puts a `git` wrapper first on PATH. prelude is shell text run before the real git with
// "$*" and "$PWD" available; discovery calls (the isolation runner) contain --literal-pathspecs, the
// identity and inventory calls do not. It returns the path of a log the shim appends one line per call to.
func reposGitShim(t *testing.T, prelude string) string {
	t.Helper()
	realGit, err := lookGit()
	if err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + log + "'\ncase \"$*\" in *--literal-pathspecs*) " + prelude + ";; esac\nexec " + realGit + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

// reposShimDiscoveryCalls counts the discovery calls (isolation runner) that mention word.
func reposShimDiscoveryCalls(t *testing.T, log, word string) int {
	t.Helper()
	b, _ := os.ReadFile(log)
	n := 0
	for _, line := range strings.Split(string(b), "\n") {
		if strings.Contains(line, "--literal-pathspecs") && strings.Contains(line, word) {
			n++
		}
	}
	return n
}

func reposShimCalls(t *testing.T, log, word string) int {
	t.Helper()
	b, _ := os.ReadFile(log)
	return strings.Count(string(b), word)
}

// N1: all candidates are confirmed with ONE ls-files call, so a parent with many candidates does not pay
// one call (and one deadline) per candidate.
func TestReposDiscoverConfirmsAllCandidatesWithOneIndexQuery(t *testing.T) {
	reposHermetic(t)
	repo := reposNewRepo(t, filepath.Join(reposTempDir(t), "super"))
	var np [][2]string
	for i := 0; i < 64; i++ {
		np = append(np, [2]string{fmt.Sprintf("n%02d", i), fmt.Sprintf("p%02d", i)})
	}
	reposGitlinks(t, repo, np...)
	log := reposGitShim(t, "case \"$*\" in *ls-files*) sleep 0.3;; esac")
	start := time.Now()
	res := reposRun([]string{repo}, true)
	_ = res
	if n := reposShimDiscoveryCalls(t, log, "ls-files --stage -z -- p0"); n != 1 {
		b, _ := os.ReadFile(log)
		t.Fatalf("%d ls-files calls for 64 candidates, want 1\n%.1500s", n, b)
	}
	if time.Since(start) > 8*time.Second {
		t.Fatalf("slow index queries accumulated: %v", time.Since(start))
	}
}

// N1: discovery of one parent runs under ONE per-call bound; several calls that are each fast but together
// slower than the bound end the discovery with inventory-timeout, and the other parent is unaffected.
func TestReposDiscoveryOfAParentHasOneAggregateBound(t *testing.T) {
	reposHermetic(t)
	base := reposTempDir(t)
	slow := reposSuper(t, filepath.Join(base, "slow"), "s1")
	fine := reposNewRepo(t, filepath.Join(base, "fine"))
	reposGitShim(t, "case \"$PWD\" in */slow) sleep 0.25;; esac")
	start := time.Now()
	res := reposWithBounds(context.Background(), ReposOptions{Paths: []string{slow, fine}, Submodules: true}, Inspect, Inventory, newReposRunner,
		reposBounds{total: time.Minute, call: 800 * time.Millisecond})
	if time.Since(start) > 15*time.Second {
		t.Fatalf("not bounded: %v", time.Since(start))
	}
	e := res.Entries[0]
	if e.Discovery.Status != "failed" || e.Error == nil || e.Error.Code != ReposCodeInventoryTimeout || !strings.Contains(e.Error.Message, "per-call deadline of 10 seconds") {
		t.Fatalf("slow parent: %+v %+v", e.Discovery, e.Error)
	}
	if res.Entries[1].Discovery.Status != "ok" {
		t.Fatalf("fine parent: %+v", res.Entries[1].Discovery)
	}
}

// Discovery honors the 60-second total (here shortened): the runner's own per-call timeout must not outlive it.
func TestReposDiscoveryHonorsTheTotalDeadline(t *testing.T) {
	reposHermetic(t)
	super := reposSuper(t, filepath.Join(reposTempDir(t), "super"), "s1")
	reposGitShim(t, "case \"$*\" in *ls-tree*) exec sleep 20;; esac")
	start := time.Now()
	res := reposWithBounds(context.Background(), ReposOptions{Paths: []string{super}, Submodules: true}, Inspect, Inventory, newReposRunner,
		reposBounds{total: 700 * time.Millisecond, call: 10 * time.Second})
	if time.Since(start) > 5*time.Second {
		t.Fatalf("discovery outlived the total deadline: %v", time.Since(start))
	}
	e := res.Entries[0]
	if e.Error == nil || e.Error.Code != ReposCodeInventoryTimeout || !strings.Contains(e.Error.Message, "total deadline of 60 seconds") {
		t.Fatalf("%+v %+v", e.Discovery, e.Error)
	}
	// The later inventory-not-started does not overwrite the discovery cause: it goes to the notes.
	if !strings.Contains(strings.Join(e.Notes, "\n"), "inventory-not-started: ") {
		t.Fatalf("displaced cause missing from notes: %q", e.Notes)
	}
}

// A real parent failure keeps what was learned before it: ignored paths and their notes.
func TestReposDiscoveryFailureKeepsIgnoredCountsAndNotes(t *testing.T) {
	reposHermetic(t)
	repo := reposNewRepo(t, filepath.Join(reposTempDir(t), "super"))
	reposRegister(t, repo, false, [2]string{"esc", "../escape"})
	reposGitlinks(t, repo, [2]string{"ok", "fine"})
	reposGitShim(t, "case \"$*\" in *ls-files*) exit 1;; esac")
	d := reposRun([]string{repo}, true).Entries[0]
	if d.Discovery.Status != "failed" || d.Discovery.Ignored != 1 || len(d.Notes) == 0 || !strings.Contains(strings.Join(d.Notes, "\n"), "discovery-ignored: ../escape") {
		t.Fatalf("%+v notes %q", d.Discovery, d.Notes)
	}
}

// N2: a limit or error inside ONE submodule is that submodule's own entry; the siblings, the ignored count
// and the notes of the parent survive.
func TestReposDiscoverOneSubmoduleOverTheEntryLimitDoesNotAbortTheParent(t *testing.T) {
	f := newReposHostileFixture(t)
	reposRegister(t, f.super, false, [2]string{"ghost", "ghost"})
	heads := filepath.Join(f.super, ".git", "modules", "s1", "refs", "heads")
	if err := os.MkdirAll(heads, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < pinsEntryLimit+50; i++ {
		if err := os.WriteFile(filepath.Join(heads, fmt.Sprintf("b%05d", i)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res := reposRun([]string{f.super}, true)
	e := reposEntry(t, res, "s1")
	if e.Status != "error" || e.Error == nil || e.Error.Code != ReposCodeDiscoveryLimit {
		t.Fatalf("s1: %+v %+v", e, e.Error)
	}
	if s2 := reposEntry(t, res, "s2"); s2.Status != "ok" {
		t.Fatalf("sibling lost: %+v %+v", s2, s2.Error)
	}
	if d := res.Entries[0].Discovery; d.Status != "ok" || d.Ignored != 1 || d.Submodules != 2 {
		t.Fatalf("parent: %+v notes %q", d, res.Entries[0].Notes)
	}
}

// Two parents that yield the same absolute submodule path: the second sees it as deduplicated, so the path
// becomes one entry (the seen set grows as entries are added).
func TestReposDiscoverTwoParentsYieldingOnePathAddItOnce(t *testing.T) {
	reposHermetic(t)
	base := reposTempDir(t)
	outer := reposNewRepo(t, filepath.Join(base, "outer"))
	inner := reposSuper(t, filepath.Join(outer, "inner"), "m")
	reposGitlinks(t, outer, [2]string{"m", "inner/m"})
	res := reposRun([]string{outer, inner}, true)
	count := 0
	for _, e := range res.Entries {
		if e.ResolvedPath != nil && *e.ResolvedPath == filepath.Join(inner, "m") {
			count++
		}
	}
	if count != 1 || res.Entries[1].Discovery.Deduplicated != 1 {
		t.Fatalf("%d entries for one path; inner discovery %+v; %v", count, res.Entries[1].Discovery, reposPaths(res))
	}
}

// Parents are discovered on the bounded pool: peak concurrency is exactly 4 for 8 parents.
func TestReposDiscoveryPoolWidthIsFour(t *testing.T) {
	reposHermetic(t)
	base := reposTempDir(t)
	var paths []string
	for i := 0; i < 8; i++ {
		paths = append(paths, reposSuper(t, filepath.Join(base, fmt.Sprintf("p%d", i)), "s1"))
	}
	var arrived, cur, peak int32
	var mu sync.Mutex
	factory := func(ctx context.Context) *pinsRunner {
		atomic.AddInt32(&arrived, 1)
		for deadline := time.Now().Add(3 * time.Second); atomic.LoadInt32(&arrived) < 4 && time.Now().Before(deadline); {
			time.Sleep(time.Millisecond)
		}
		c := atomic.AddInt32(&cur, 1)
		mu.Lock()
		if c > peak {
			peak = c
		}
		mu.Unlock()
		time.Sleep(150 * time.Millisecond) // stay inside the worker while the others try to enter
		atomic.AddInt32(&cur, -1)
		return newReposRunner(ctx)
	}
	reposRunWith(paths, nil, factory)
	if peak != 4 {
		t.Fatalf("peak discovery concurrency %d, want exactly 4", peak)
	}
}
