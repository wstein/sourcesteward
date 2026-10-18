// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Commit in the selected target itself: retaining normal COMMIT_EDITMSG is
// essential. Never remove it merely to make the retirement fixture admissible.
func tidyV3CommittedLegacyFixture(t *testing.T, detached bool, count int) (V3PlanOptions, map[string][]byte) {
	t.Helper()
	o, active := tidyV3LegacyFixture(t, detached)
	sourceID, e := gitNativeIdentity(context.Background(), o.Path)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(sourceID.RepositoryDir, "info", "exclude"), []byte("ignored-cache\n"), 0600); e != nil {
		t.Fatal(e)
	}
	preserved := map[string][]byte{filepath.Join(o.Path, "user-plan.md"): []byte("primary untracked user plan\n"), filepath.Join(o.Path, "ignored-cache"): []byte("primary ignored bytes\n"), filepath.Join(active, "file"): []byte("dirty active bytes\n"), filepath.Join(active, "ignored-cache"): []byte("active ignored bytes\n")}
	for p, b := range preserved {
		if e = os.WriteFile(p, b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	for i := 0; i < count-2; i++ {
		p := filepath.Join(o.CompletedWorktree, fmt.Sprintf("content/section-%02d/file-%03d", i%12, i))
		if e = os.MkdirAll(filepath.Dir(p), 0700); e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(p, []byte(fmt.Sprintf("ordinary native task %d\x00\n", i)), 0600); e != nil {
			t.Fatal(e)
		}
	}
	prefix, suffix := []byte("<svg xmlns=\"http://www.w3.org/2000/svg\"><!--"), []byte("--></svg>")
	svg := append(prefix, bytes.Repeat([]byte("s"), 301117-len(prefix)-len(suffix))...)
	svg = append(svg, suffix...)
	if e = os.WriteFile(filepath.Join(o.CompletedWorktree, "brand.svg"), svg, 0600); e != nil {
		t.Fatal(e)
	}
	gitFixture(t, o.CompletedWorktree, "add", ".")
	gitFixture(t, o.CompletedWorktree, "commit", "-qm", "completed in native target")
	head := gitFixture(t, o.CompletedWorktree, "rev-parse", "HEAD")
	gitFixture(t, o.Path, "merge", "--ff-only", head)
	gitFixture(t, o.CompletedWorktree, "update-index", "--index-version=4")
	if status := gitFixture(t, o.CompletedWorktree, "status", "--porcelain=v1", "--untracked-files=all", "--ignored"); status != "" {
		t.Fatalf("target incomplete clean scopes: %q", status)
	}
	return o, preserved
}

func TestTidyV3LegacyAdmitsNativeCommitMessageMetadata(t *testing.T) {
	for i, count := range []int{315, 323, 332, 341, 350, 358} {
		detached := i%2 == 1
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			o, preserved := tidyV3CommittedLegacyFixture(t, detached, count)
			budget := newTidyV3ProofBudget(false)
			started := time.Now()
			got, e := observeTidyV3Legacy(context.Background(), o, budget)
			if e != nil {
				t.Fatalf("real native in-place commit must admit bounded COMMIT_EDITMSG: %v", e)
			}
			found := false
			for _, entry := range got.Retirement.Metadata.Entries {
				if filepath.Base(entry.Identity.Path) == "COMMIT_EDITMSG" {
					found = true
					if entry.Identity.Kind != "regular" || entry.Identity.Size > 64<<10 {
						t.Fatal("invalid metadata facts")
					}
				}
			}
			if !found {
				t.Fatal("commit metadata omitted from complete registration proof")
			}
			var userBytes, pointerBytes, registrationBytes int64
			tracked := 0
			for _, entry := range got.Retirement.Disk.Entries {
				if entry.Identity.Kind != "regular" {
					continue
				}
				if filepath.Base(entry.Identity.Path) == ".git" {
					pointerBytes += entry.Identity.Size
				} else {
					userBytes += entry.Identity.Size
					tracked++
				}
			}
			for _, entry := range got.Retirement.Metadata.Entries {
				if entry.Identity.Kind == "regular" {
					registrationBytes += entry.Identity.Size
				}
			}
			counters := budget.snapshot()
			if tracked != count || counters.Observations != 1 || counters.RawProcesses != 1 || counters.UserPasses != 3 || counters.MetadataPasses != 4 || counters.UserBytes != 3*userBytes || counters.MetadataBytes != 2*(pointerBytes+registrationBytes) || counters.MaxReadBuffer > 64<<10 {
				t.Fatalf("incomplete committed native sample counters %+v tracked=%d", counters, tracked)
			}
			t.Logf("tracked=%d native-in-place=true detached=%t COMMIT_EDITMSG=included counters=%+v elapsed=%s; complete clean target and protected primary/active bytes", tracked, detached, counters, time.Since(started))

			for p, want := range preserved {
				b, e := os.ReadFile(p)
				if e != nil || !bytes.Equal(b, want) {
					t.Fatal("protected actor bytes changed")
				}
			}
		})
	}
}

func TestTidyV3CommitMessagePolicyAndMetadataBounds(t *testing.T) {
	if containsString(gitRetirementMetadataPaths(), "COMMIT_EDITMSG") {
		t.Fatal("old metadata vocabulary broadened")
	}
	for _, p := range []retirementContentPolicy{0, retirementLegacyContent, 99} {
		if _, e := gitRetirementV3MetadataPaths(p); e == nil {
			t.Fatal("non-v3 metadata policy admitted")
		}
	}
	for _, size := range []int{64 << 10, (64 << 10) + 1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			o, _ := tidyV3LegacyFixture(t, false)
			id, e := gitNativeIdentity(context.Background(), o.CompletedWorktree)
			if e != nil {
				t.Fatal(e)
			}
			message := filepath.Join(id.GitDir, "COMMIT_EDITMSG")
			payload := bytes.Repeat([]byte{0, 255, '\n', 13}, (size+3)/4)[:size]
			if e = os.WriteFile(message, payload, 0600); e != nil {
				t.Fatal(e)
			}
			_, e = observeTidyV3Legacy(context.Background(), o, newTidyV3ProofBudget(false))
			if (e == nil) != (size == 64<<10) {
				t.Fatalf("size%d error%v", size, e)
			}
			old := retirementManifestAdmission{Backend: "git", NativeMetadataPaths: []string{}}
			for _, p := range gitRetirementMetadataPaths() {
				if _, se := os.Lstat(filepath.Join(id.GitDir, p)); se == nil {
					old.NativeMetadataPaths = append(old.NativeMetadataPaths, p)
				}
			}
			if _, e = observeRetirementManifest(context.Background(), id.GitDir, old); e == nil {
				t.Fatal("old Git/jj manifest vocabulary admitted new metadata")
			}
			if got, e := os.ReadFile(message); e != nil || !bytes.Equal(got, payload) {
				t.Fatal("metadata actor bytes changed")
			}
		})
	}
}

func TestTidyV3CommitMessageDoesNotAdmitOtherPathsOrNodes(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "directory", "nested", "lookalike", "other-native-file", "native-operation", "loose-lock", "untracked", "ignored"} {
		t.Run(kind, func(t *testing.T) {
			o, _ := tidyV3LegacyFixture(t, false)
			id, e := gitNativeIdentity(context.Background(), o.CompletedWorktree)
			if e != nil {
				t.Fatal(e)
			}
			message := filepath.Join(id.GitDir, "COMMIT_EDITMSG")
			if e = os.WriteFile(message, []byte("native metadata\n"), 0600); e != nil {
				t.Fatal(e)
			}
			switch kind {
			case "symlink":
				e = os.Rename(message, message+"-outside")
				if e == nil {
					e = os.Symlink(message+"-outside", message)
				}
			case "hardlink":
				e = os.Link(message, filepath.Join(gitTempDir(t), "actor-message"))
			case "directory":
				e = os.Remove(message)
				if e == nil {
					e = os.Mkdir(message, 0700)
				}
			case "nested":
				e = os.Mkdir(filepath.Join(id.GitDir, "nested"), 0700)
				if e == nil {
					e = os.WriteFile(filepath.Join(id.GitDir, "nested", "COMMIT_EDITMSG"), []byte("actor"), 0600)
				}
			case "lookalike":
				e = os.WriteFile(message+".bak", []byte("actor"), 0600)
			case "other-native-file":
				e = os.WriteFile(filepath.Join(id.GitDir, "SQUASH_MSG"), []byte("actor"), 0600)
			case "native-operation":
				e = os.WriteFile(filepath.Join(id.GitDir, "MERGE_HEAD"), []byte(id.CommitID+"\n"), 0600)
			case "loose-lock":
				e = os.WriteFile(filepath.Join(id.RepositoryDir, id.HeadRef+".lock"), []byte("actor lock"), 0600)
			case "untracked":
				e = os.WriteFile(filepath.Join(o.CompletedWorktree, "actor-extra"), []byte("actor"), 0600)
			case "ignored":
				e = os.WriteFile(filepath.Join(id.RepositoryDir, "info", "exclude"), []byte("actor-cache\n"), 0600)
				if e == nil {
					e = os.WriteFile(filepath.Join(o.CompletedWorktree, "actor-cache"), []byte("actor"), 0600)
				}
			}
			if e != nil {
				t.Fatal(e)
			}
			if _, e = observeTidyV3Legacy(context.Background(), o, newTidyV3ProofBudget(false)); e == nil {
				t.Fatal("metadata presence bypassed another safety guard")
			}
			if _, e = os.Lstat(message); e != nil {
				t.Fatal("metadata actor path removed")
			}
		})
	}
}

func TestTidyV3CommitMessageChangesBetweenPassesRefuse(t *testing.T) {
	for _, kind := range []string{"same-size-restored-mtime", "same-bytes-restored-mtime", "mode", "inode", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			o, _ := tidyV3LegacyFixture(t, false)
			id, e := gitNativeIdentity(context.Background(), o.CompletedWorktree)
			if e != nil {
				t.Fatal(e)
			}
			message := filepath.Join(id.GitDir, "COMMIT_EDITMSG")
			original := []byte{0, 255, '\n', 1}
			if e = os.WriteFile(message, original, 0600); e != nil {
				t.Fatal(e)
			}
			before, e := os.Stat(message)
			if e != nil {
				t.Fatal(e)
			}
			calls := 0
			observer := tidyV3LegacyObserver{source: observeTidyV3Git, target: func(ctx context.Context, path string) (NativeIdentity, error) {
				id, e := gitNativeIdentity(ctx, path)
				calls++
				if calls != 2 {
					return id, e
				}
				switch kind {
				case "same-size-restored-mtime":
					e = os.WriteFile(message, []byte{1, 255, '\n', 1}, 0600)
					if e == nil {
						e = os.Chtimes(message, before.ModTime(), before.ModTime())
					}
				case "same-bytes-restored-mtime":
					e = os.WriteFile(message, original, 0600)
					if e == nil {
						e = os.Chtimes(message, before.ModTime(), before.ModTime())
					}
				case "mode":
					e = os.Chmod(message, 0700)
				case "inode":
					e = os.Rename(message, filepath.Join(gitTempDir(t), "actor-original"))
					if e == nil {
						e = os.WriteFile(message, original, 0600)
					}
				case "hardlink":
					e = os.Link(message, filepath.Join(gitTempDir(t), "actor-link"))
				}
				if e != nil {
					t.Fatal(e)
				}
				return id, nil
			}}
			if _, e = observer.observe(context.Background(), o, newTidyV3ProofBudget(false)); e == nil {
				t.Fatal("changed native metadata admitted")
			}
			if calls != 2 {
				t.Fatal("metadata actor not reached")
			}
			if _, e = os.Lstat(message); e != nil {
				t.Fatal("actor metadata removed")
			}
		})
	}
}
