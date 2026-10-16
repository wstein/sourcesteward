// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// settleIndex makes the index file newer than every entry, so that no entry is racily clean and the
// scenario under test decides the answer. It only touches the fixture before the audit runs.
func settleIndex(t *testing.T, gitDir string) {
	t.Helper()
	later := time.Now().Add(time.Minute)
	mustOS(t, os.Chtimes(filepath.Join(gitDir, "index"), later, later))
}

type dirtyCase struct {
	run   *pinsRunner
	dirs  pinsSubmoduleDirs
	facts pinsRepoFacts
	head  pinsHead
	wt    string
}

func newDirtyCase(t *testing.T, f pinsFixture) dirtyCase {
	t.Helper()
	run, repo, d := validatedMember(t, f)
	settleIndex(t, repo.GitDir)
	facts, err := pinsReadFacts(run, d)
	if err != nil {
		t.Fatal(err)
	}
	head, err := pinsReadHead(run, repo)
	if err != nil {
		t.Fatal(err)
	}
	return dirtyCase{run: run, dirs: d, facts: facts, head: head, wt: repo.WorkTree}
}

func (c dirtyCase) state(t *testing.T) PinsDirty {
	t.Helper()
	return c.stateWith(t, pinsDefaultDirtyBudget)
}

func (c dirtyCase) stateWith(t *testing.T, b pinsDirtyBudget) PinsDirty {
	t.Helper()
	d, err := pinsDirtyState(c.run, c.dirs, c.facts, c.head, b)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func requireDirty(t *testing.T, d PinsDirty, tracked, untracked, reason string) {
	t.Helper()
	if d.Tracked != tracked || d.Untracked != untracked || (reason == "") != (d.Reason == "") || !strings.Contains(d.Reason, reason) {
		t.Fatalf("got %+v, want tracked=%s untracked=%s reason~%q", d, tracked, untracked, reason)
	}
}

func TestPinsDirtyCleanSubmodule(t *testing.T) {
	c := newDirtyCase(t, newPinsFixture(t))
	requireDirty(t, c.state(t), "absent", "absent", "")
}

func TestPinsDirtyTrackedChanges(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(t *testing.T, c dirtyCase)
		tracked string
		reason  string
	}{
		{"different size", func(t *testing.T, c dirtyCase) { writeFile(t, filepath.Join(c.wt, "file.txt"), "a longer line\n") }, "present", ""},
		{"deleted file", func(t *testing.T, c dirtyCase) { mustOS(t, os.Remove(filepath.Join(c.wt, "file.txt"))) }, "present", ""},
		{"file replaced by a directory", func(t *testing.T, c dirtyCase) {
			mustOS(t, os.Remove(filepath.Join(c.wt, "file.txt")))
			mustOS(t, os.Mkdir(filepath.Join(c.wt, "file.txt"), 0o755))
		}, "present", ""},
		{"file replaced by a symlink", func(t *testing.T, c dirtyCase) {
			mustOS(t, os.Remove(filepath.Join(c.wt, "file.txt")))
			mustOS(t, os.Symlink("elsewhere", filepath.Join(c.wt, "file.txt")))
		}, "present", ""},
		{"executable bit set", func(t *testing.T, c dirtyCase) { mustOS(t, os.Chmod(filepath.Join(c.wt, "file.txt"), 0o755)) }, "present", ""},
		{"staged change equal to the worktree", func(t *testing.T, c dirtyCase) {
			writeFile(t, filepath.Join(c.wt, "file.txt"), "staged\n")
			git(t, c.wt, "add", "file.txt")
			settleIndex(t, c.dirs.GitDir)
		}, "present", ""},
		{"staged deletion", func(t *testing.T, c dirtyCase) { git(t, c.wt, "rm", "-q", "file.txt"); settleIndex(t, c.dirs.GitDir) }, "present", ""},
		{"same size, new content", func(t *testing.T, c dirtyCase) { writeFile(t, filepath.Join(c.wt, "file.txt"), "ONE\n") }, "unknown", "modification time"},
		{"touched only", func(t *testing.T, c dirtyCase) {
			old := time.Now().Add(-time.Hour)
			mustOS(t, os.Chtimes(filepath.Join(c.wt, "file.txt"), old, old))
		}, "unknown", "modification time"},
		{"chmod to the same mode (change time only)", func(t *testing.T, c dirtyCase) {
			time.Sleep(20 * time.Millisecond)
			mustOS(t, os.Chmod(filepath.Join(c.wt, "file.txt"), 0o644))
		}, "unknown", "change time"},
		{"rewritten as a copy with the same size and time", func(t *testing.T, c dirtyCase) {
			p := filepath.Join(c.wt, "file.txt")
			info, err := os.Stat(p)
			mustOS(t, err)
			data, err := os.ReadFile(p)
			mustOS(t, err)
			mustOS(t, os.Remove(p))
			mustOS(t, os.WriteFile(p, data, 0o644))
			mustOS(t, os.Chtimes(p, info.ModTime(), info.ModTime()))
		}, "unknown", "change time"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newDirtyCase(t, newPinsFixture(t))
			tc.mutate(t, c)
			requireDirty(t, c.state(t), tc.tracked, "absent", tc.reason)
		})
	}
}

func TestPinsDirtyRacilyCleanEntryIsUnknown(t *testing.T) {
	f := newPinsFixture(t)
	c := newDirtyCase(t, f)
	info, err := os.Stat(filepath.Join(c.wt, "file.txt"))
	mustOS(t, err)
	mustOS(t, os.Chtimes(filepath.Join(c.dirs.GitDir, "index"), info.ModTime(), info.ModTime()))
	requireDirty(t, c.state(t), "unknown", "absent", "racily")
}

func TestPinsDirtyFlagsMakeTheEntryUnknown(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		mutate       func(t *testing.T, c dirtyCase)
	}{
		{"skip-worktree", "skip-worktree", func(t *testing.T, c dirtyCase) { git(t, c.wt, "update-index", "--skip-worktree", "file.txt") }},
		{"assume-unchanged", "assume-unchanged", func(t *testing.T, c dirtyCase) { git(t, c.wt, "update-index", "--assume-unchanged", "file.txt") }},
		{"intent-to-add", "intent-to-add", func(t *testing.T, c dirtyCase) {
			writeFile(t, filepath.Join(c.wt, "new.txt"), "new\n")
			git(t, c.wt, "add", "-N", "new.txt")
		}},
		{"conflict stage", "stage", func(t *testing.T, c dirtyCase) {
			oid := func(s string) string { return gitHash(t, c.wt, s) }
			git(t, c.wt, "update-index", "--force-remove", "file.txt")
			gitStdin(t, c.wt, "100644 "+oid("base\n")+" 1\tfile.txt\n100644 "+oid("ours\n")+" 2\tfile.txt\n100644 "+oid("theirs\n")+" 3\tfile.txt\n", "update-index", "--index-info")
		}},
		{"gitlink", "gitlink", func(t *testing.T, c dirtyCase) {
			git(t, c.wt, "update-index", "--add", "--cacheinfo", "160000,"+strings.Repeat("a", 40)+",nested")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newDirtyCase(t, newPinsFixture(t))
			tc.mutate(t, c)
			settleIndex(t, c.dirs.GitDir)
			d := c.state(t)
			if d.Tracked != "unknown" && d.Tracked != "present" {
				t.Fatalf("%+v", d)
			}
			if d.Tracked == "unknown" && !strings.Contains(d.Reason, tc.reason) {
				t.Fatalf("reason %q lacks %q", d.Reason, tc.reason)
			}
		})
	}
}

// Entries with these flags are never reported clean: with nothing else different the answer is unknown.
func TestPinsDirtyFlagOnlyChangeIsExactlyUnknown(t *testing.T) {
	for _, flag := range []string{"--skip-worktree", "--assume-unchanged"} {
		c := newDirtyCase(t, newPinsFixture(t))
		git(t, c.wt, "update-index", flag, "file.txt")
		settleIndex(t, c.dirs.GitDir)
		d := c.state(t)
		// The staged check sees nothing; the flagged entry alone makes the tracked answer unknown.
		requireDirty(t, d, "unknown", "absent", strings.TrimPrefix(flag, "--"))
	}
}

func TestPinsDirtySmudgedEntryIsUnknown(t *testing.T) {
	f := newPinsFixture(t)
	run, repo, d := validatedMember(t, f)
	wt := repo.WorkTree
	p := filepath.Join(wt, "smudge.txt")
	writeFile(t, p, "abc\n")
	git(t, wt, "add", "smudge.txt")
	git(t, wt, "commit", "-qm", "smudge")
	info, err := os.Stat(p)
	mustOS(t, err)
	// The touch-update-touch race: new content, same size, same time, and an index no newer than the entry.
	writeFile(t, p, "xyz\n")
	mustOS(t, os.Chtimes(p, info.ModTime(), info.ModTime()))
	mustOS(t, os.Chtimes(filepath.Join(repo.GitDir, "index"), info.ModTime(), info.ModTime()))
	// A version change rewrites the index without any staged difference, which smudges the racy entry.
	git(t, wt, "-c", "core.trustctime=false", "update-index", "--index-version", "3")
	if out := git(t, wt, "ls-files", "--stage", "--debug", "smudge.txt"); !strings.Contains(out, "size: 0") {
		t.Fatalf("the fixture did not produce a smudged entry:\n%s", out)
	}
	settleIndex(t, repo.GitDir)
	facts, _ := pinsReadFacts(run, d)
	head, _ := pinsReadHead(run, repo)
	got, err := pinsDirtyState(run, d, facts, head, pinsDefaultDirtyBudget)
	if err != nil || got.Tracked != "unknown" || !strings.Contains(got.Reason, "smudged") {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestPinsDirtyUntrackedPresence(t *testing.T) {
	c := newDirtyCase(t, newPinsFixture(t))
	writeFile(t, filepath.Join(c.wt, "scratch.txt"), "x")
	requireDirty(t, c.state(t), "absent", "present", "")
	// An ignored file is not untracked presence.
	c = newDirtyCase(t, newPinsFixture(t))
	writeFile(t, filepath.Join(c.dirs.GitDir, "info", "exclude"), "ignored.txt\n")
	writeFile(t, filepath.Join(c.wt, "ignored.txt"), "x")
	requireDirty(t, c.state(t), "absent", "absent", "")
}

func TestPinsDirtyUnbornHeadIsUnknownForStagedState(t *testing.T) {
	c := newDirtyCase(t, newPinsFixture(t))
	git(t, c.wt, "checkout", "-q", "--orphan", "fresh")
	var err error
	c.head, err = pinsReadHead(c.run, pinsGitRepo{GitDir: c.dirs.GitDir, WorkTree: c.wt})
	mustOS(t, err)
	settleIndex(t, c.dirs.GitDir)
	d := c.state(t)
	if d.Tracked != "unknown" || !strings.Contains(d.Reason, "unborn") {
		t.Fatalf("%+v", d)
	}
}

func TestPinsDirtyDecidesLargeFilesAndTextAttributesWithoutReadingContent(t *testing.T) {
	f := newPinsFixture(t)
	run, repo, d := validatedMember(t, f)
	wt := repo.WorkTree
	writeFile(t, filepath.Join(wt, "big.bin"), strings.Repeat("0123456789abcdef", 8192)) // 128 KiB
	writeFile(t, filepath.Join(wt, "crlf.txt"), "a\r\nb\r\n")
	writeFile(t, filepath.Join(wt, ".gitattributes"), "*.txt text eol=lf\n")
	git(t, wt, "-c", "core.autocrlf=true", "add", "-A")
	git(t, wt, "commit", "-qm", "files")
	settleIndex(t, repo.GitDir)
	facts, _ := pinsReadFacts(run, d)
	head, _ := pinsReadHead(run, repo)
	got, err := pinsDirtyState(run, d, facts, head, pinsDefaultDirtyBudget)
	if err != nil {
		t.Fatal(err)
	}
	requireDirty(t, got, "absent", "absent", "")
}

func TestPinsDirtyFilterSentinelsNeverRunAndTheIndexIsUntouched(t *testing.T) {
	f := newPinsFixture(t)
	h := newHostile(t)
	c := newDirtyCase(t, f)
	h.plant(t, c.dirs.GitDir, c.wt, false)
	writeFile(t, filepath.Join(c.wt, "file.txt"), "ONE\n") // same size, so the answer is decided by time, never content
	statDirty(t, c.wt)
	settleIndex(t, c.dirs.GitDir)
	idx := filepath.Join(c.dirs.GitDir, "index")
	before, _ := os.ReadFile(idx)
	st, _ := os.Stat(idx)
	got := c.state(t)
	if got.Tracked != "unknown" {
		t.Fatalf("%+v", got)
	}
	if ran := h.executed(); len(ran) != 0 {
		t.Fatalf("planted commands ran: %v", ran)
	}
	after, _ := os.ReadFile(idx)
	st2, _ := os.Stat(idx)
	if !bytes.Equal(before, after) || !st.ModTime().Equal(st2.ModTime()) {
		t.Fatal("the index changed")
	}
}

func TestPinsDirtyBudgetYieldsUnknownNeverALimit(t *testing.T) {
	f := newPinsFixture(t)
	c := newDirtyCase(t, f)
	for _, n := range []string{"a.txt", "b.txt", "c.txt", "d.txt"} {
		writeFile(t, filepath.Join(c.wt, n), n)
	}
	git(t, c.wt, "add", "-A")
	git(t, c.wt, "commit", "-qm", "more")
	settleIndex(t, c.dirs.GitDir)
	got := c.stateWith(t, pinsDirtyBudget{Entries: 3, Time: time.Minute})
	if got.Tracked != "unknown" || !strings.Contains(got.Reason, "budget") {
		t.Fatalf("entries: %+v", got)
	}
	got = c.stateWith(t, pinsDirtyBudget{Entries: 20000, Time: time.Nanosecond})
	if got.Tracked != "unknown" || got.Untracked != "unknown" || !strings.Contains(got.Reason, "budget") {
		t.Fatalf("time: %+v", got)
	}
	// A real difference found within the budget still wins.
	writeFile(t, filepath.Join(c.wt, "a.txt"), "much longer")
	got = c.stateWith(t, pinsDirtyBudget{Entries: 3, Time: time.Minute})
	if got.Tracked != "present" {
		t.Fatalf("present found before the budget ran out: %+v", got)
	}
}

func TestPinsDirtyStartsOnlyAllowlistedGitCommands(t *testing.T) {
	f := newPinsFixture(t)
	c := newDirtyCase(t, f)
	gitLog, _ := pinsSentinels(t)
	c.state(t)
	requireAllowlisted(t, gitLog(), "diff-index --cached --no-ext-diff --no-textconv HEAD", "ls-files --stage --debug -z", "ls-files --others --exclude-standard -z")
}

func TestPinsDirtyCancellationIsAnErrorNotAnAnswer(t *testing.T) {
	c := newDirtyCase(t, newPinsFixture(t))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.run = newPinsRunner(ctx)
	if _, err := pinsDirtyState(c.run, c.dirs, c.facts, c.head, pinsDefaultDirtyBudget); err == nil {
		t.Fatal("cancellation was reported as an answer")
	}
}
