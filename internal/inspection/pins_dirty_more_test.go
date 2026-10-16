// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// scanOnly runs just the worktree comparison, so the index entry under test is judged by the comparison
// and not short-circuited by the staged check.
func (c dirtyCase) scanOnly(t *testing.T) PinsDirty {
	t.Helper()
	s := &pinsDirtyScan{run: c.run, d: c.dirs, facts: c.facts, head: c.head, budget: pinsDefaultDirtyBudget,
		repo: pinsGitRepo{GitDir: c.dirs.GitDir, WorkTree: c.wt}, deadline: time.Now().Add(time.Minute), tracked: "absent", untracked: "absent"}
	mustOS(t, s.worktree())
	return PinsDirty{Tracked: s.tracked, Untracked: s.untracked, Reason: s.reasons.text()}
}

func TestPinsDirtyIndexScanJudgesFlaggedEntriesAsUnknownWithTheirReason(t *testing.T) {
	for _, tc := range []struct{ name, reason string }{{"intent-to-add", "intent-to-add"}, {"conflict stage", "conflict stage"}, {"gitlink", "gitlink"}} {
		t.Run(tc.name, func(t *testing.T) {
			c := newDirtyCase(t, newPinsFixture(t))
			switch tc.name {
			case "intent-to-add":
				writeFile(t, filepath.Join(c.wt, "new.txt"), "new\n")
				git(t, c.wt, "add", "-N", "new.txt")
			case "conflict stage":
				oid := func(s string) string { return gitHash(t, c.wt, s) }
				git(t, c.wt, "update-index", "--force-remove", "file.txt")
				gitStdin(t, c.wt, "100644 "+oid("b\n")+" 1\tfile.txt\n100644 "+oid("o\n")+" 2\tfile.txt\n100644 "+oid("t\n")+" 3\tfile.txt\n", "update-index", "--index-info")
			case "gitlink":
				git(t, c.wt, "update-index", "--add", "--cacheinfo", "160000,"+strings.Repeat("a", 40)+",nested")
			}
			settleIndex(t, c.dirs.GitDir)
			got := c.scanOnly(t)
			if got.Tracked != "unknown" || !strings.Contains(got.Reason, tc.reason) {
				t.Fatalf("%+v", got)
			}
		})
	}
}

func TestPinsDirtyRepositoryFileModeAndSymlinkSettingsAreHonored(t *testing.T) {
	// core.fileMode=false: an executable bit is not a mode change (the change time still makes it unknown).
	f := newPinsFixture(t)
	_, repo, _ := validatedMember(t, f)
	git(t, repo.WorkTree, "config", "core.fileMode", "false")
	c := newDirtyCase(t, f)
	if c.facts.FileMode {
		t.Fatal("core.fileMode=false was not read")
	}
	time.Sleep(20 * time.Millisecond)
	mustOS(t, os.Chmod(filepath.Join(c.wt, "file.txt"), 0o755))
	got := c.scanOnly(t)
	if got.Tracked != "unknown" || strings.Contains(got.Reason, "mode") {
		t.Fatalf("an executable bit counted as a change: %+v", got)
	}
	// With the default the same change is a definite difference.
	f2 := newPinsFixture(t)
	c2 := newDirtyCase(t, f2)
	if !c2.facts.FileMode || !c2.facts.Symlinks {
		t.Fatal("defaults must be true")
	}
	time.Sleep(20 * time.Millisecond)
	mustOS(t, os.Chmod(filepath.Join(c2.wt, "file.txt"), 0o755))
	if got = c2.scanOnly(t); got.Tracked != "present" {
		t.Fatalf("%+v", got)
	}

	// core.symlinks=false: a recorded symlink checked out as a regular file is not a type change.
	for _, symlinks := range []string{"true", "false"} {
		f3 := newPinsFixture(t)
		_, r3, _ := validatedMember(t, f3)
		mustOS(t, os.Symlink("file.txt", filepath.Join(r3.WorkTree, "link")))
		git(t, r3.WorkTree, "add", "link")
		git(t, r3.WorkTree, "commit", "-qm", "link")
		mustOS(t, os.Remove(filepath.Join(r3.WorkTree, "link")))
		writeFile(t, filepath.Join(r3.WorkTree, "link"), "file.txt")
		git(t, r3.WorkTree, "config", "core.symlinks", symlinks)
		c3 := newDirtyCase(t, f3)
		got = c3.scanOnly(t)
		if (symlinks == "true") != (got.Tracked == "present") {
			t.Fatalf("core.symlinks=%s: %+v", symlinks, got)
		}
	}
}

func TestPinsDirtyCleanSymlinkAndEmptyFileAreAbsent(t *testing.T) {
	f := newPinsFixture(t)
	_, repo, _ := validatedMember(t, f)
	wt := repo.WorkTree
	mustOS(t, os.Symlink("file.txt", filepath.Join(wt, "link")))
	writeFile(t, filepath.Join(wt, "empty"), "")
	git(t, wt, "add", "-A")
	git(t, wt, "commit", "-qm", "link and empty")
	c := newDirtyCase(t, f)
	requireDirty(t, c.state(t), "absent", "absent", "")
}

func TestPinsDirtyCleanSHA256Repository(t *testing.T) {
	pinsIsolateHome(t)
	dir := pinsRealDir(t)
	if out, err := exec.Command("git", "-C", dir, "init", "-q", "--object-format=sha256", "-b", "main").CombinedOutput(); err != nil {
		t.Skipf("this Git cannot create a sha256 repository: %s", out)
	}
	writeFile(t, filepath.Join(dir, "a.txt"), "a\n")
	writeFile(t, filepath.Join(dir, "empty"), "")
	mustOS(t, os.Symlink("a.txt", filepath.Join(dir, "link")))
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "sha256")
	repo := pinsGitRepo{GitDir: filepath.Join(dir, ".git"), WorkTree: dir}
	later := time.Now().Add(time.Minute)
	mustOS(t, os.Chtimes(filepath.Join(repo.GitDir, "index"), later, later))
	run := newPinsRunner(context.Background())
	d := pinsSubmoduleDirs{GitDir: repo.GitDir, WorkTree: repo.WorkTree, gitBase: dir, gitRel: ".git"}
	got, err := pinsDirtyState(run, d, mustFacts(t, run, d), mustHead(t, run, repo), pinsDefaultDirtyBudget)
	if err != nil {
		t.Fatal(err)
	}
	requireDirty(t, got, "absent", "absent", "")
}

func TestPinsDirtySplitIndexIsUnknown(t *testing.T) {
	c := newDirtyCase(t, newPinsFixture(t))
	git(t, c.wt, "update-index", "--split-index")
	settleIndex(t, c.dirs.GitDir)
	got := c.state(t)
	if got.Tracked != "unknown" || !strings.Contains(got.Reason, "split index") {
		t.Fatalf("%+v", got)
	}
}

func TestPinsDirtyIndexRewrittenDuringTheScanIsUnknown(t *testing.T) {
	c := newDirtyCase(t, newPinsFixture(t))
	idx := filepath.Join(c.dirs.GitDir, "index")
	pinsScanHook = func() {
		pinsScanHook = nil
		later := time.Now().Add(2 * time.Minute)
		mustOS(t, os.Chtimes(idx, later, later))
	}
	t.Cleanup(func() { pinsScanHook = nil })
	got := c.state(t)
	if got.Tracked != "unknown" || !strings.Contains(got.Reason, "changed during the scan") {
		t.Fatalf("%+v", got)
	}
}

func TestPinsIndexPathProblem(t *testing.T) {
	for p, bad := range map[string]bool{
		"a/b/c": false, "a b/ü": false, ".gitignore": false, "dir/.gitmodules": false,
		"": true, "/abs": true, "a//b": true, "a/./b": true, "a/../b": true, "../x": true, ".git/config": true,
		"a/.GIT/b": true, "a/.Git": true, "x\x00y": true,
	} {
		if got := pinsIndexPathProblem(p) != ""; got != bad {
			t.Errorf("%q: problem=%v, want %v", p, got, bad)
		}
	}
}

// A hostile index path must never be looked up: the stream is faked because Git itself refuses to write
// such an entry.
func TestPinsDirtyHostileIndexPathsAreUnknownAndNeverStatted(t *testing.T) {
	c := newDirtyCase(t, newPinsFixture(t))
	block := debugBlock("1:2", "3:4", "5", "6", "7", "0")
	stream := "100644 " + oidA + " 0\t../outside\x00" + block + "100644 " + oidA + " 0\tA/.GIT/x\x00" + block
	dir := t.TempDir()
	mustOS(t, os.WriteFile(filepath.Join(dir, "stream"), []byte(stream), 0o600))
	real, _ := exec.LookPath("git")
	script := "#!/bin/sh\ncase \"$*\" in *'ls-files --stage --debug'*) cat '" + filepath.Join(dir, "stream") + "'; exit 0;; esac\nexec '" + real + "' \"$@\"\n"
	mustOS(t, os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	got := c.scanOnly(t)
	if got.Tracked != "unknown" || !strings.Contains(got.Reason, "index path") {
		t.Fatalf("%+v", got)
	}
}

func TestPinsFingerprintIndexSeesSameSizeRewrites(t *testing.T) {
	dir := pinsRealDir(t)
	l := pinsSuperLayout{GitDir: dir}
	idx := filepath.Join(dir, "index")
	mustOS(t, os.WriteFile(idx, []byte("AAAA"), 0o600))
	when := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	mustOS(t, os.Chtimes(idx, when, when))
	a, err := pinsFingerprintIndex(l)
	mustOS(t, err)
	mustOS(t, os.WriteFile(idx, []byte("BBBB"), 0o600))
	mustOS(t, os.Chtimes(idx, when, when))
	b, err := pinsFingerprintIndex(l)
	mustOS(t, err)
	if a == b || a.Size != b.Size || a.Mtime != b.Mtime {
		t.Fatalf("a same-size, same-time rewrite must change the fingerprint: %+v %+v", a, b)
	}
	rmAll(t, idx)
	if z, err := pinsFingerprintIndex(l); err != nil || z != (pinsIndexFingerprint{}) {
		t.Fatalf("%+v %v", z, err)
	}
}

func TestPinsAuditDirectoriesSwappedBeforeEvidenceAreRefusedNotFatal(t *testing.T) {
	f := newPinsFixture(t)
	pinsAfterResolve = func() {
		pinsAfterResolve = nil
		gd := filepath.Join(f.Super, ".git", "modules", "member")
		mustOS(t, os.Rename(gd, gd+".bak"))
		if out, err := exec.Command("cp", "-a", gd+".bak", gd).CombinedOutput(); err != nil {
			t.Fatalf("%s %v", out, err)
		}
	}
	t.Cleanup(func() { pinsAfterResolve = nil })
	r := auditOf(t, f.Super)
	s := subOf(t, r, "member")
	if r.Status != "ok" || s.Refusal == nil || s.Refusal.Code != "submodule-path-unsafe" || !strings.Contains(s.Refusal.Message, "changed after validation") {
		t.Fatalf("%s %+v", r.Status, s.Refusal)
	}
}

func TestPinsAuditUnbornSubmoduleIsStableAcrossTheResample(t *testing.T) {
	f := newPinsFixture(t)
	git(t, memberOf(f), "checkout", "-q", "--orphan", "fresh")
	r := auditOf(t, f.Super)
	requireNoFinding(t, r, "pins-state-changed")
}
