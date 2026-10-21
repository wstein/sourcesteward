// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
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
