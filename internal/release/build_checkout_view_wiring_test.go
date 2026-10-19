// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"io"
	"os"
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
