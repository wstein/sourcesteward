// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// `stw release build` compares the checkout through the frozen private view: the
// native comparison configuration is captured before the first project invocation
// and the second comparison refuses when the metadata moved during the build.
func buildViewWiringFixture(t *testing.T) (dir, commit string) {
	t.Helper()
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_REPOSITORY", "example/test")
	t.Setenv("GITHUB_RUN_ID", "1")
	t.Setenv("GITHUB_RUN_ATTEMPT", "1")
	dir = repoWith(t, map[string][]byte{".stw/release.toml": exampleBytes(t, "go-cli"), "tracked": []byte("original")})
	commit = commitAll(t, dir)
	git(t, dir, "remote", "add", "origin", "https://github.com/example/test.git")
	git(t, dir, "update-ref", "refs/remotes/origin/main", commit)
	return dir, commit
}

func buildViewWiringRunner(t *testing.T, dir string, during func()) *fakeBuildRunner {
	return &fakeBuildRunner{run: func(_ context.Context, c BuildCommand, _, _ io.Writer) (int, error) {
		if during != nil {
			during()
		}
		p := filepath.Join(dir, c.Argv[5])
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			return -1, err
		}
		return 0, os.WriteFile(p, []byte("FAKE compiler output"), 0644)
	}}
}

func TestBuildReleaseRefusesCheckoutMetadataChangedDuringBuild(t *testing.T) {
	for name, tamper := range map[string]func(dir string){
		"config appended": func(dir string) {
			f, err := os.OpenFile(filepath.Join(dir, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if _, err := f.WriteString("[fixture]\n\tmarker = changed-during-build\n"); err != nil {
				t.Fatal(err)
			}
		},
		"head ref rewritten": func(dir string) {
			git(t, dir, "commit", "--allow-empty", "-q", "-m", "moved during build")
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir, commit := buildViewWiringFixture(t)
			fake := buildViewWiringRunner(t, dir, func() { tamper(dir) })
			r := buildRelease(context.Background(), BuildOptions{Mode: "ci", Path: dir, Commit: commit, Version: "v1.2.3", STWVersion: "v1.0.0", Out: filepath.Join(t.TempDir(), "new")}, fake, nil)
			if r.Error == nil || r.Error.Code != "checkout-mismatch" {
				t.Fatalf("metadata change during the build was not refused: %+v", r)
			}
		})
	}
}

func TestBuildReleaseCheckoutViewLeavesNoPrivateStateOrLease(t *testing.T) {
	dir, commit := buildViewWiringFixture(t)
	scratch := t.TempDir()
	t.Setenv("TMPDIR", scratch)
	fake := buildViewWiringRunner(t, dir, nil)
	r := buildRelease(context.Background(), BuildOptions{Mode: "ci", Path: dir, Commit: commit, Version: "v1.2.3", STWVersion: "v1.0.0", Out: filepath.Join(t.TempDir(), "new")}, fake, nil)
	if r.Status != "ok" {
		t.Fatalf("%+v", r)
	}
	entries, err := os.ReadDir(scratch)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "stw-checkout-view-") {
			t.Fatalf("private checkout view %s outlived the build", e.Name())
		}
	}
	admission, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	free, err := acquireBuildViewLease(admission)
	if err != nil {
		t.Fatalf("the checkout view lease outlived the build: %v", err)
	}
	free()
}

func TestBuildReleaseRefusesCheckoutThatCannotBeFrozen(t *testing.T) {
	dir, commit := buildViewWiringFixture(t)
	// A tracked-file difference is refused before any project invocation, through the view.
	write(t, dir, "tracked", []byte("dirty"))
	fake := buildViewWiringRunner(t, dir, nil)
	r := buildRelease(context.Background(), BuildOptions{Mode: "ci", Path: dir, Commit: commit, Version: "v1.2.3", STWVersion: "v1.0.0", Out: filepath.Join(t.TempDir(), "new")}, fake, nil)
	if r.Error == nil || r.Error.Code != "checkout-mismatch" || len(fake.calls) != 0 {
		t.Fatalf("%+v calls=%d", r, len(fake.calls))
	}
}

// checkBuildCheckout is the one-shot form of the production comparison: capture, compare
// and close. The production build uses the view directly so that it can compare twice.
func checkBuildCheckout(ctx context.Context, root, commit string) error {
	view, err := openBuildCheckoutView(ctx, root, commit, os.Environ())
	if err != nil {
		return err
	}
	defer view.Close()
	return view.check(ctx)
}

// Project-triggered index rewrites that change no tracked state (a stat refresh by
// `git status`, `git describe --dirty` or `git update-index --refresh`) are not a
// tracked-tree difference: the source index is compared by its tracked content and
// flags, never by bytes or inode.
func TestBuildReleaseAcceptsSemanticallyUnchangedIndexRewrite(t *testing.T) {
	dir, commit := buildViewWiringFixture(t)
	// File identity is not evidence of a rewrite (a filesystem may reuse the inode of the
	// replaced index), so the bytes are compared: a stat refresh changes the stat data.
	before, err := os.ReadFile(filepath.Join(dir, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	rewritten := false
	fake := buildViewWiringRunner(t, dir, func() {
		future := time.Now().Add(time.Minute)
		if err := os.Chtimes(filepath.Join(dir, "tracked"), future, future); err != nil {
			t.Error(err)
		}
		git(t, dir, "status", "--porcelain")
		git(t, dir, "update-index", "--refresh")
		after, err := os.ReadFile(filepath.Join(dir, ".git", "index"))
		rewritten = rewritten || (err == nil && !bytes.Equal(before, after))
	})
	r := buildRelease(context.Background(), BuildOptions{Mode: "ci", Path: dir, Commit: commit, Version: "v1.2.3", STWVersion: "v1.0.0", Out: filepath.Join(t.TempDir(), "new")}, fake, nil)
	if !rewritten {
		t.Fatal("fixture did not rewrite the source index")
	}
	if r.Status != "ok" {
		t.Fatalf("index stat refresh refused a clean build: %+v", r)
	}
}

// The index stat data is dropped from the semantic comparison, so the tracked-tree
// comparison must not trust it either: a same-size change with a forged mtime, staged
// and then reset to the original object id, leaves an index entry whose stat data
// matches the modified file. Git has to re-hash the content whatever the mtime is: an
// hour old, the epoch itself, half a second after it, or before it, for the plain, the
// version 4 and the split index.
func TestBuildReleaseRefusesSameSizeContentBehindForgedStatData(t *testing.T) {
	mtimes := map[string]time.Time{"an hour old": time.Now().Add(-time.Hour), "epoch": time.Unix(0, 0), "half a second": time.Unix(0, 5e8), "one second": time.Unix(1, 0), "before 1970": time.Unix(-86400, 0)}
	indexes := map[string][]string{"plain": nil, "version 4": {"--index-version", "4"}, "split": {"--split-index"}}
	for indexName, setup := range indexes {
		for mtimeName, mtime := range mtimes {
			t.Run(indexName+"/"+mtimeName, func(t *testing.T) {
				dir, commit := buildViewWiringFixture(t)
				if setup != nil {
					git(t, dir, append([]string{"update-index"}, setup...)...)
				}
				original := git(t, dir, "rev-parse", commit+":tracked")
				fake := buildViewWiringRunner(t, dir, func() {
					write(t, dir, "tracked", []byte("modified"))
					if err := os.Chtimes(filepath.Join(dir, "tracked"), mtime, mtime); err != nil {
						t.Error(err)
					}
					git(t, dir, "add", "tracked")
					// Put the original object id back without touching the entry's stat data.
					staged, err := hex.DecodeString(git(t, dir, "rev-parse", ":tracked"))
					if err != nil {
						t.Error(err)
						return
					}
					raw, err := hex.DecodeString(original)
					if err != nil {
						t.Error(err)
						return
					}
					index, err := os.ReadFile(filepath.Join(dir, ".git", "index"))
					if err != nil {
						t.Error(err)
						return
					}
					body := bytes.Replace(index[:len(index)-sha1.Size], staged, raw, 1)
					sum := sha1.Sum(body)
					if err := os.WriteFile(filepath.Join(dir, ".git", "index"), append(body, sum[:]...), 0o644); err != nil {
						t.Error(err)
						return
					}
					if git(t, dir, "rev-parse", ":tracked") != original {
						t.Error("fixture did not restore the original object id")
					}
				})
				r := buildRelease(context.Background(), BuildOptions{Mode: "ci", Path: dir, Commit: commit, Version: "v1.2.3", STWVersion: "v1.0.0", Out: filepath.Join(t.TempDir(), "new")}, fake, nil)
				if r.Error == nil || r.Error.Code != "checkout-mismatch" {
					t.Fatalf("modified tracked content behind forged stat data was accepted: %+v", r)
				}
			})
		}
	}
}

func TestBuildReleaseStillRefusesTrackedChangeAndIndexFlagsDuringBuild(t *testing.T) {
	for name, tamper := range map[string]func(dir string){
		"tracked content": func(dir string) { write(t, dir, "tracked", []byte("changed")) },
		"assume-unchanged": func(dir string) {
			write(t, dir, "tracked", []byte("changed"))
			git(t, dir, "update-index", "--assume-unchanged", "tracked")
		},
		"skip-worktree": func(dir string) {
			write(t, dir, "tracked", []byte("changed"))
			git(t, dir, "update-index", "--skip-worktree", "tracked")
		},
		"staged": func(dir string) {
			write(t, dir, "tracked", []byte("changed"))
			git(t, dir, "add", "tracked")
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir, commit := buildViewWiringFixture(t)
			fake := buildViewWiringRunner(t, dir, func() { tamper(dir) })
			r := buildRelease(context.Background(), BuildOptions{Mode: "ci", Path: dir, Commit: commit, Version: "v1.2.3", STWVersion: "v1.0.0", Out: filepath.Join(t.TempDir(), "new")}, fake, nil)
			if r.Error == nil || r.Error.Code != "checkout-mismatch" {
				t.Fatalf("tracked change during the build was accepted: %+v", r)
			}
		})
	}
}

// The private view is what the native comparison trusts. A project step that edits a
// private copy (the same-user process can reach it through TMPDIR) must be refused
// rather than silently changing the comparison.
func TestBuildReleaseRefusesTamperedPrivateView(t *testing.T) {
	for name, tamper := range map[string]func(t *testing.T, dir, private string){
		"private index flag": func(t *testing.T, dir, private string) {
			write(t, dir, "tracked", []byte("changed"))
			cmd := exec.Command("git", "update-index", "--assume-unchanged", "tracked")
			cmd.Dir = dir
			cmd.Env = append(gitEnv(), "GIT_INDEX_FILE="+filepath.Join(private, "root-0", "index"))
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("%v: %s", err, out)
			}
		},
		"private config": func(t *testing.T, dir, private string) {
			f, err := os.OpenFile(filepath.Join(private, "root-0", "config"), os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Error(err)
				return
			}
			defer f.Close()
			_, _ = f.WriteString("[core]\n\tautocrlf = true\n")
		},
		"private symlink": func(t *testing.T, dir, private string) {
			link := filepath.Join(private, "root-0", "refs")
			if err := os.Remove(link); err != nil {
				t.Error(err)
				return
			}
			if err := os.Symlink(t.TempDir(), link); err != nil {
				t.Error(err)
			}
		},
		"private file added": func(t *testing.T, dir, private string) {
			if err := os.WriteFile(filepath.Join(private, "root-0", "sharedindex.0000000000000000000000000000000000000000"), []byte("x"), 0600); err != nil {
				t.Error(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir, commit := buildViewWiringFixture(t)
			scratch := t.TempDir()
			t.Setenv("TMPDIR", scratch)
			fake := buildViewWiringRunner(t, dir, func() {
				entries, err := filepath.Glob(filepath.Join(scratch, "stw-checkout-view-*"))
				if err != nil || len(entries) != 1 {
					t.Errorf("private view not found: %v %v", entries, err)
					return
				}
				tamper(t, dir, entries[0])
			})
			r := buildRelease(context.Background(), BuildOptions{Mode: "ci", Path: dir, Commit: commit, Version: "v1.2.3", STWVersion: "v1.0.0", Out: filepath.Join(t.TempDir(), "new")}, fake, nil)
			if r.Error == nil || r.Error.Code != "checkout-mismatch" {
				t.Fatalf("tampered private view accepted: %+v", r)
			}
		})
	}
}

// A limit or a cancellation while the view runs is reported as such, not as evidence
// that the checkout differs.
func TestBuildReleaseReportsLimitAndCancellationOfTheCheckoutView(t *testing.T) {
	dir, commit := buildViewWiringFixture(t)
	previous := buildViewListingLimit
	buildViewListingLimit = 16
	t.Cleanup(func() { buildViewListingLimit = previous })
	fake := buildViewWiringRunner(t, dir, nil)
	r := buildRelease(context.Background(), BuildOptions{Mode: "ci", Path: dir, Commit: commit, Version: "v1.2.3", STWVersion: "v1.0.0", Out: filepath.Join(t.TempDir(), "new")}, fake, nil)
	if r.Error == nil || r.Error.Code != "release-limit" || len(fake.calls) != 0 {
		t.Fatalf("view limit reported as %+v", r.Error)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for err, want := range map[error]string{
		context.Canceled:                                             "canceled",
		context.DeadlineExceeded:                                     "canceled",
		fmt.Errorf("x: %w", errReleaseLimit):                         "release-limit",
		errors.New("tracked checkout differs from requested commit"): "checkout-mismatch",
	} {
		if got := buildCheckoutFailureCode(context.Background(), err); got != want {
			t.Fatalf("%v: %s, want %s", err, got, want)
		}
	}
	if got := buildCheckoutFailureCode(canceled, errors.New("native group observation incomplete")); got != "canceled" {
		t.Fatalf("operational failure of a canceled build: %s", got)
	}
}

// A panic while the view is prepared must not leak the private directory or the lease.
func TestOpenBuildCheckoutViewCleansUpOnPanic(t *testing.T) {
	dir, commit := buildViewWiringFixture(t)
	scratch := t.TempDir()
	t.Setenv("TMPDIR", scratch)
	previous := buildViewSupervisor
	buildViewSupervisor = func(context.Context, buildViewCommand, io.Writer) (int, []byte, error) { panic("native runner panic") }
	t.Cleanup(func() { buildViewSupervisor = previous })
	func() {
		defer func() {
			if recover() == nil {
				t.Error("the panic was swallowed")
			}
		}()
		_, _ = openBuildCheckoutView(context.Background(), dir, commit, os.Environ())
	}()
	entries, err := filepath.Glob(filepath.Join(scratch, "stw-checkout-view-*"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("private view leaked after a panic: %v %v", entries, err)
	}
	admission, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	free, err := acquireBuildViewLease(admission)
	if err != nil {
		t.Fatalf("lease leaked after a panic: %v", err)
	}
	free()
}
