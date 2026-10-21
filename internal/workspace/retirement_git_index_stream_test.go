// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The index listing of a real repository (about 900 tracked files with long paths) exceeds
// the 64 KiB buffered command bound. Retirement observation must stream it and
// compare record by record instead of refusing with git-output-limit.
func TestRetirementV3StoredFilesStreamIndexListingAbove64KiB(t *testing.T) {
	source := fixtureGit(t)
	for i := 0; i < 900; i++ {
		rel := filepath.Join("content", fmt.Sprintf("section-%02d", i%12), fmt.Sprintf("a-reasonably-long-file-name-%04d.dat", i))
		p := filepath.Join(source, rel)
		if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(p, []byte(fmt.Sprintf("content %d\n", i)), 0600); e != nil {
			t.Fatal(e)
		}
	}
	gitFixture(t, source, "add", ".")
	gitFixture(t, source, "commit", "-qm", "many tracked files")
	dest := filepath.Join(gitTempDir(t), "completed")
	gitFixture(t, source, "worktree", "add", "-q", "-b", "completed-many", dest, "HEAD")
	if listing := gitFixture(t, dest, "ls-files", "--stage", "-z"); len(listing) <= nativeOutputLimit {
		t.Fatalf("fixture listing %d bytes does not exceed the buffered bound", len(listing))
	}
	id := NativeIdentity{Backend: "git", WorkspaceRoot: dest, CommitID: gitFixture(t, dest, "rev-parse", "HEAD"), ObjectFormat: "sha1"}
	bin, e := gitExecutable()
	if e != nil {
		t.Fatal(e)
	}
	budget := newTidyV3ProofBudget(false)
	if e = budget.beginObservation(); e != nil {
		t.Fatal(e)
	}
	files, e := gitRetirementStoredFilesWithPolicy(context.Background(), bin, id, retirementGitV3Content, budget)
	if e != nil || len(files) != 901 {
		t.Fatalf("streamed index comparison failed: %d files (want 901), %v", len(files), e)
	}
	// A staged difference is still refused after streaming.
	gitFixture(t, dest, "rm", "-q", "--cached", "content/section-00/a-reasonably-long-file-name-0000.dat")
	again := newTidyV3ProofBudget(false)
	if e = again.beginObservation(); e != nil {
		t.Fatal(e)
	}
	_, e = gitRetirementStoredFilesWithPolicy(context.Background(), bin, id, retirementGitV3Content, again)
	if e == nil || !strings.Contains(e.Error(), "index") {
		t.Fatalf("staged removal admitted: %v", e)
	}
}

func TestNULRecordStreamRefusesTruncationAndCountOverflow(t *testing.T) {
	n := 0
	if e := parseGitRecordsLimit(strings.NewReader("a\x00b\x00"), gitRecordNUL, 2, func([]byte) error { n++; return nil }); e != nil || n != 2 {
		t.Fatalf("complete stream: %d, %v", n, e)
	}
	if e := parseGitRecordsLimit(strings.NewReader("a\x00b"), gitRecordNUL, 2, func([]byte) error { return nil }); e != io.ErrUnexpectedEOF {
		t.Fatalf("missing final delimiter: %v", e)
	}
	if e := parseGitRecordsLimit(strings.NewReader("a\x00b\x00c\x00"), gitRecordNUL, 2, func([]byte) error { return nil }); e != errGitRecordCount {
		t.Fatalf("record overflow: %v", e)
	}
}

// A registration index above 64 KiB (about 600 tracked files) used to refuse
// every retirement as an oversized metadata file.
func TestTidyV3LegacyAdmitsALargeRegistrationIndex(t *testing.T) {
	o, _ := tidyV3LegacyFixture(t, false)
	for i := 0; i < 900; i++ {
		rel := filepath.Join("content", fmt.Sprintf("section-%02d", i%12), fmt.Sprintf("a-reasonably-long-file-name-%04d.dat", i))
		p := filepath.Join(o.Path, rel)
		if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(p, []byte(fmt.Sprintf("content %d\n", i)), 0600); e != nil {
			t.Fatal(e)
		}
	}
	gitFixture(t, o.Path, "add", ".")
	gitFixture(t, o.Path, "commit", "-qm", "many tracked files")
	// A new linked worktree of the committed tree; the fixture's own is stale.
	dest := filepath.Join(gitTempDir(t), "big")
	gitFixture(t, o.Path, "worktree", "add", "-q", "-b", "big", dest, "HEAD")
	o.CompletedWorktree, o.RemoveFiles = dest, dest
	info, e := os.Stat(filepath.Join(strings.TrimSpace(gitFixture(t, dest, "rev-parse", "--absolute-git-dir")), "index"))
	if e != nil || info.Size() <= 64<<10 {
		t.Fatalf("fixture index does not exceed 64 KiB: %v %v", info, e)
	}
	if _, e := observeTidyV3Legacy(context.Background(), o, newTidyV3ProofBudget(false)); e != nil {
		t.Fatalf("large index refused: %v", e)
	}
}

// More than 1,024 tracked files is refused by name before any content is read.
func TestRetirementV3StoredFilesRefuseAboveTheManifestBoundByName(t *testing.T) {
	source := fixtureGit(t)
	for i := 0; i < retirementManifestEntries+50; i++ {
		if e := os.WriteFile(filepath.Join(source, fmt.Sprintf("f%05d", i)), []byte("x\n"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	gitFixture(t, source, "add", ".")
	gitFixture(t, source, "commit", "-qm", "many")
	dest := filepath.Join(gitTempDir(t), "many")
	gitFixture(t, source, "worktree", "add", "-q", "-b", "many", dest, "HEAD")
	id := NativeIdentity{Backend: "git", WorkspaceRoot: dest, CommitID: gitFixture(t, dest, "rev-parse", "HEAD"), ObjectFormat: "sha1"}
	bin, e := gitExecutable()
	if e != nil {
		t.Fatal(e)
	}
	budget := newTidyV3ProofBudget(false)
	if e = budget.beginObservation(); e != nil {
		t.Fatal(e)
	}
	_, e = gitRetirementStoredFilesWithPolicy(context.Background(), bin, id, retirementGitV3Content, budget)
	var op operationError
	if !errors.As(e, &op) || op.Code != "retirement-file-limit" {
		t.Fatalf("want retirement-file-limit, got %v", e)
	}
}

// Contract 2 (v2) records stay inside one MiB, so a 6,000 file worktree that the
// v3 retirement admits is refused by name under v2 instead of failing later on
// the encoded limit.
func TestRetirementV2StoredFilesRefuseWhatOnlyV3Admits(t *testing.T) {
	source := fixtureGit(t)
	for i := 0; i < 6000; i++ {
		if e := os.WriteFile(filepath.Join(source, fmt.Sprintf("f%05d", i)), []byte("x\n"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	gitFixture(t, source, "add", ".")
	gitFixture(t, source, "commit", "-qm", "many")
	dest := filepath.Join(gitTempDir(t), "many")
	gitFixture(t, source, "worktree", "add", "-q", "-b", "many", dest, "HEAD")
	id := NativeIdentity{Backend: "git", WorkspaceRoot: dest, CommitID: gitFixture(t, dest, "rev-parse", "HEAD"), ObjectFormat: "sha1"}
	bin, e := gitExecutable()
	if e != nil {
		t.Fatal(e)
	}
	_, e = gitRetirementStoredFiles(context.Background(), bin, id)
	var op operationError
	if !errors.As(e, &op) || op.Code != "retirement-file-limit" || !strings.Contains(op.Message, "1024") {
		t.Fatalf("v2 must refuse by name: %v", e)
	}
}
