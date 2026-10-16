// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// pinsSentinels puts recording stand-ins for git and jj first on PATH and returns a function that
// reads their logs. The jj stand-in always fails; the git stand-in records its arguments and then
// runs the real Git, so legitimate detection still works.
func pinsSentinels(t *testing.T) (gitLog func() []string, jjLog func() []string) {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is unavailable")
	}
	dir, logs := t.TempDir(), t.TempDir()
	gl, jl := filepath.Join(logs, "git.log"), filepath.Join(logs, "jj.log")
	script := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	script("git", "printf '%s\\n' \"$*\" >> '"+gl+"'\nexec '"+real+"' \"$@\"\n")
	script("jj", "printf '%s\\n' \"$*\" >> '"+jl+"'\nexit 99\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	read := func(path string) func() []string {
		return func() []string {
			b, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
		}
	}
	return read(gl), read(jl)
}

// noProcessPath makes every executable lookup fail except recording sentinels, so any process started
// by name through PATH is observable. It returns the sentinel log. Its limits are explicit: a process
// started by absolute path, by a Go library that does not consult PATH, or work done in-process is NOT
// seen. TestPinsOwnershipFilesImportNoProcessPackage and the failing injected runner are the second
// line of defense for the same claim.
func noProcessPath(t *testing.T) func() []string {
	t.Helper()
	dir, log := t.TempDir(), filepath.Join(t.TempDir(), "started.log")
	for _, name := range []string{"git", "jj", "sh", "bwrap", "sandbox-exec"} {
		body := "#!/bin/sh\nprintf '%s\\n' \"$0 $*\" >> '" + log + "'\nexit 99\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	return func() []string {
		b, err := os.ReadFile(log)
		if err != nil {
			return nil
		}
		return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	}
}
func mkdirs(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if err := os.MkdirAll(p, 0o700); err != nil {
			t.Fatal(err)
		}
	}
}
func requireRefusal(t *testing.T, r PinsResult, backend, code string) {
	t.Helper()
	if r.Status != "refused" || r.Backend != backend || r.Error == nil || r.Error.Code != code || r.Error.Message == "" {
		t.Fatalf("want refused %s %s: %+v", backend, code, r)
	}
	if r.Boundary.Complete || r.Identity != nil || len(r.Submodules) != 0 || r.Submodules == nil || len(r.Findings) != 0 || r.Command != "pins" || r.SchemaVersion != 1 {
		t.Fatalf("refusal is not an empty, incomplete result: %+v", r)
	}
}

func TestPinsJJAndAmbiguousOwnershipStartNoProcess(t *testing.T) {
	realGit := func(dir string, args ...string) { git(t, dir, args...) }
	colocated := func(t *testing.T) string {
		d := t.TempDir()
		realGit(d, "init", "-q", "-b", "main")
		mkdirs(t, filepath.Join(d, ".jj", "repo"))
		return d
	}
	for _, tc := range []struct {
		name    string
		setup   func(t *testing.T) string
		backend string
		code    string
	}{
		{"standalone", func(t *testing.T) string { d := t.TempDir(); mkdirs(t, filepath.Join(d, ".jj", "repo")); return d }, "jj", "unsupported-backend"},
		{"secondary", func(t *testing.T) string {
			d := t.TempDir()
			mkdirs(t, filepath.Join(d, ".jj"))
			if err := os.WriteFile(filepath.Join(d, ".jj", "repo"), []byte("../../primary/.jj/repo"), 0o600); err != nil {
				t.Fatal(err)
			}
			return d
		}, "jj", "unsupported-backend"},
		{"colocated", colocated, "jj", "unsupported-backend"},
		{"nested-path", func(t *testing.T) string {
			d := colocated(t)
			mkdirs(t, filepath.Join(d, "a", "b"))
			return filepath.Join(d, "a", "b")
		}, "jj", "unsupported-backend"},
		{"symlinked-path", func(t *testing.T) string {
			d := colocated(t)
			mkdirs(t, filepath.Join(d, "a", "b"))
			link := filepath.Join(t.TempDir(), "link")
			if err := os.Symlink(filepath.Join(d, "a", "b"), link); err != nil {
				t.Fatal(err)
			}
			return link
		}, "jj", "unsupported-backend"},
		{"jj-and-gitbutler-markers", func(t *testing.T) string {
			d := colocated(t)
			mkdirs(t, filepath.Join(d, ".git", "gitbutler"))
			return d
		}, "ambiguous", "ambiguous-backend"},
		{"jj-and-gitbutler-ref", func(t *testing.T) string {
			d := colocated(t)
			realGit(d, "commit", "-q", "--allow-empty", "-m", "initial")
			realGit(d, "update-ref", "refs/gitbutler/test", "HEAD")
			return d
		}, "ambiguous", "ambiguous-backend"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := tc.setup(t)
			started := noProcessPath(t)
			r := Pins(context.Background(), PinsOptions{Path: dir})
			requireRefusal(t, r, tc.backend, tc.code)
			if got := started(); len(got) != 0 {
				t.Fatalf("processes started: %q", got)
			}
			// The injected runner is a second, independent proof for the code path itself.
			r = pins(context.Background(), PinsOptions{Path: dir}, func(context.Context, string, ...string) (string, int, error) {
				t.Fatal("a Git runner call was made")
				return "", 0, nil
			})
			requireRefusal(t, r, tc.backend, tc.code)
		})
	}
}
func TestPinsOtherRefusalsUseOnlyGitDetectionProcesses(t *testing.T) {
	for _, tc := range []struct {
		name    string
		setup   func(t *testing.T) string
		backend string
		code    string
	}{
		{"not-repository", func(t *testing.T) string { return t.TempDir() }, "unknown", "not-repository"},
		{"bare", func(t *testing.T) string { d := t.TempDir(); git(t, d, "init", "-q", "--bare", "-b", "main"); return d }, "git", "bare-repository"},
		{"gitbutler-metadata", func(t *testing.T) string {
			d := repo(t)
			mkdirs(t, filepath.Join(d, ".git", "gitbutler"))
			return d
		}, "gitbutler", "unsupported-backend"},
		{"gitbutler-ref", func(t *testing.T) string {
			d := repo(t)
			git(t, d, "commit", "-q", "--allow-empty", "-m", "initial")
			git(t, d, "update-ref", "refs/gitbutler/test", "HEAD")
			return d
		}, "gitbutler", "unsupported-backend"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := tc.setup(t)
			gitLog, jjLog := pinsSentinels(t)
			r := Pins(context.Background(), PinsOptions{Path: dir})
			requireRefusal(t, r, tc.backend, tc.code)
			if tc.code != "not-repository" && len(gitLog()) == 0 {
				t.Fatal("the existing Git detection did not run")
			}
			for _, line := range gitLog() {
				if strings.Contains(line, "submodule") || strings.Contains(line, "gitmodules") {
					t.Fatalf("submodule command started: %q", line)
				}
			}
			if got := jjLog(); got != nil {
				t.Fatalf("jj started: %q", got)
			}
		})
	}
}
func TestPinsGitOwnedWorktreeReportsMissingEvidence(t *testing.T) {
	main := repo(t)
	git(t, main, "commit", "-q", "--allow-empty", "-m", "initial")
	linked := filepath.Join(t.TempDir(), "linked")
	git(t, main, "worktree", "add", "-q", "-b", "other", linked)
	sub := filepath.Join(main, "inner")
	mkdirs(t, sub)
	for name, dir := range map[string]string{"main": main, "linked": linked, "subdirectory": sub} {
		t.Run(name, func(t *testing.T) {
			_, jjLog := pinsSentinels(t)
			r := Pins(context.Background(), PinsOptions{Path: dir})
			if r.Status != "error" || r.Backend != "git" || r.Error == nil || r.Error.Code != "pins-failed" || r.Error.Message != "submodule evidence is not implemented in this revision" {
				t.Fatalf("%+v", r)
			}
			if r.Boundary.Complete || r.Identity != nil || len(r.Submodules) != 0 || r.Submodules == nil || r.Findings == nil || r.InputPath != dir {
				t.Fatalf("%+v", r)
			}
			if jjLog() != nil {
				t.Fatal("jj started")
			}
		})
	}
}
func TestPinsDefaultsInputPath(t *testing.T) {
	d := t.TempDir()
	mkdirs(t, filepath.Join(d, ".jj"))
	t.Chdir(d)
	r := Pins(context.Background(), PinsOptions{})
	requireRefusal(t, r, "jj", "unsupported-backend")
	if r.InputPath != "." {
		t.Fatalf("input path %q", r.InputPath)
	}
}
func TestPinsMissingOrNonDirectoryPathIsNotRepository(t *testing.T) {
	d := t.TempDir()
	file := filepath.Join(d, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(d, "missing"), file} {
		started := noProcessPath(t)
		requireRefusal(t, Pins(context.Background(), PinsOptions{Path: path}), "unknown", "not-repository")
		if started() != nil {
			t.Fatal("a process started")
		}
	}
}
func TestPinsInvalidEncodingIsAnEarlyFailureWithUnknownBackend(t *testing.T) {
	for _, path := range []string{"a\x00b", "\xff"} {
		r := Pins(context.Background(), PinsOptions{Path: path})
		if r.Status != "error" || r.Backend != "unknown" || r.Error == nil || r.Error.Code != "pins-failed" || r.Boundary.Complete {
			t.Fatalf("%q: %+v", path, r)
		}
	}
	// A resolved path that is not valid UTF-8 (through a symlink) is refused the same way.
	d := t.TempDir()
	bad := filepath.Join(d, "dir\xff")
	if err := os.Mkdir(bad, 0o700); err != nil {
		t.Skip("file system rejects non-UTF-8 names")
	}
	link := filepath.Join(d, "link")
	if err := os.Symlink(bad, link); err != nil {
		t.Fatal(err)
	}
	r := Pins(context.Background(), PinsOptions{Path: link})
	if r.Status != "error" || r.Backend != "unknown" || r.Error == nil || r.Error.Message != "the resolved pins path is not valid UTF-8" {
		t.Fatalf("%+v", r)
	}
}
func TestPinsEarlyFailuresNeverClaimGit(t *testing.T) {
	d := t.TempDir()
	mkdirs(t, filepath.Join(d, ".jj"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := Pins(ctx, PinsOptions{Path: d})
	if r.Status != "error" || r.Backend != "unknown" || r.Error == nil || r.Error.Message != "pins canceled" {
		t.Fatalf("%+v", r)
	}
	// An unreadable ownership marker must not label the directory as Git either.
	u := t.TempDir()
	mkdirs(t, filepath.Join(u, ".jj"))
	if err := os.WriteFile(filepath.Join(u, ".git"), []byte("not a gitfile"), 0o600); err != nil {
		t.Fatal(err)
	}
	r = Pins(context.Background(), PinsOptions{Path: u})
	if r.Status != "error" || r.Backend != "unknown" {
		t.Fatalf("%+v", r)
	}
}
func TestPinsCancellationDuringIdentityDetection(t *testing.T) {
	d := repo(t)
	ctx, cancel := context.WithCancel(context.Background())
	r := pins(ctx, PinsOptions{Path: d}, func(c context.Context, _ string, _ ...string) (string, int, error) {
		cancel()
		return "", -1, c.Err()
	})
	if r.Status != "error" || r.Error == nil || r.Error.Message != "pins canceled" || r.Backend != "unknown" {
		t.Fatalf("%+v", r)
	}
}
func TestPinsOwnershipFilesImportNoProcessPackage(t *testing.T) {
	for _, name := range []string{"pins.go", "detection.go"} {
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range file.Imports {
			if spec.Path.Value == `"os/exec"` {
				t.Fatalf("%s imports os/exec; process start belongs to the Git runner only", name)
			}
		}
	}
}
func TestPinsCanceledContextIsAnError(t *testing.T) {
	d := t.TempDir()
	mkdirs(t, filepath.Join(d, ".jj"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := Pins(ctx, PinsOptions{Path: d})
	if r.Status != "error" || r.Backend != "unknown" || r.Error == nil || r.Error.Code != "pins-failed" || !strings.Contains(r.Error.Message, "canceled") {
		t.Fatalf("%+v", r)
	}
}
func TestInspectOwnedWithoutJJNeverInspectsJJMetadata(t *testing.T) {
	d := t.TempDir()
	mkdirs(t, filepath.Join(d, ".jj"))
	started := noProcessPath(t)
	r := inspectOwned(context.Background(), d, runGit, true)
	if r.Status != "error" || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "changed-repository" || started() != nil {
		t.Fatalf("%+v %q", r, started())
	}
}
