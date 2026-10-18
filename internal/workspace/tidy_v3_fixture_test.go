// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Each case is a real linked worktree, with the measured dogfood file counts
// and SVG size. These fixtures exercise content proof only; later retirement
// acceptance must also prove authority, 3/8 observations and native removal.
func TestRetirementV3SixRealisticContentSamples(t *testing.T) {
	for _, count := range []int{315, 323, 332, 341, 350, 358} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			source := fixtureGit(t)
			for i := 1; i < count-1; i++ {
				rel := fmt.Sprintf("content/section-%02d/file-%03d.dat", i%12, i)
				p := filepath.Join(source, rel)
				if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
					t.Fatal(e)
				}
				if e := os.WriteFile(p, []byte(fmt.Sprintf("ordinary stored content %d\n\x00\xff", i)), 0600); e != nil {
					t.Fatal(e)
				}
			}
			prefix, suffix := []byte("<svg xmlns=\"http://www.w3.org/2000/svg\"><!--"), []byte("--></svg>")
			svg := append(prefix, bytes.Repeat([]byte("s"), 301117-len(prefix)-len(suffix))...)
			svg = append(svg, suffix...)
			if len(svg) != 301117 {
				t.Fatalf("SVG size %d", len(svg))
			}
			if e := os.WriteFile(filepath.Join(source, "brand.svg"), svg, 0600); e != nil {
				t.Fatal(e)
			}
			gitFixture(t, source, "add", ".")
			gitFixture(t, source, "commit", "-qm", "realistic retirement content")
			dest := filepath.Join(gitTempDir(t), "completed")
			gitFixture(t, source, "worktree", "add", "-q", "-b", "completed-content", dest, "HEAD")
			gitFixture(t, dest, "update-index", "--index-version=4")
			if status := gitFixture(t, dest, "status", "--porcelain=v1", "--untracked-files=all", "--ignored"); status != "" {
				t.Fatalf("incomplete clean scopes: %q", status)
			}
			paths := strings.Split(gitFixture(t, dest, "ls-files"), "\n")
			if len(paths) != count {
				t.Fatalf("tracked files %d != %d", len(paths), count)
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
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			started := time.Now()
			files, e := gitRetirementStoredFilesWithPolicy(ctx, bin, id, retirementGitV3Content, budget)
			if e != nil {
				t.Fatal(e)
			}
			a := retirementManifestAdmission{Backend: "git", NativeMetadataPaths: []string{".git"}, StoredFiles: files}
			first, e := observeRetirementV3Manifest(ctx, dest, a, false, retirementGitV3Content, budget)
			if e != nil {
				t.Fatal(e)
			}
			if e = verifyRecordedRetirementV3Manifest(ctx, dest, a, false, retirementGitV3Content, budget, first); e != nil {
				t.Fatal(e)
			}
			counters := budget.snapshot()
			var total int64
			var maxSize int64
			for _, f := range files {
				total += f.Size
				maxSize = max(maxSize, f.Size)
			}
			pointer, e := os.Stat(filepath.Join(dest, ".git"))
			if e != nil {
				t.Fatal(e)
			}
			if counters.UserBytes != 3*total || counters.MetadataBytes != 2*pointer.Size() || counters.RawProcesses != 1 || counters.UserPasses != 3 || counters.MetadataPasses != 2 || counters.MaxReadBuffer > 64<<10 || maxSize != 301117 {
				t.Fatalf("incorrect complete sample counters %+v total=%d", counters, total)
			}
			t.Logf("tracked=%d manifest=%d max_blob=%d counters=%+v elapsed=%s; tracked/staged/untracked/ignored clean", count, len(first.Entries), maxSize, counters, time.Since(started))
			// The same native tree is intentionally still refused by old Git v2.
			if _, e = gitRetirementStoredFiles(ctx, bin, id); e == nil {
				t.Fatal("old Git v2 admitted large user file")
			}
		})
	}
}
