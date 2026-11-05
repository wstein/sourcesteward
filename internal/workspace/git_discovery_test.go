// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// discoveryFixture builds fake Git executables in a temporary tree. "apple" stands in
// for /usr/bin/git: discovery takes its path as a parameter.
type discoveryFixture struct {
	root string
}

func newDiscoveryFixture(t *testing.T) discoveryFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return discoveryFixture{root}
}

func (f discoveryFixture) git(t *testing.T, dir string) string {
	t.Helper()
	d := filepath.Join(f.root, dir)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(d, "git")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func (f discoveryFixture) dir(dir string) string { return filepath.Join(f.root, dir) }

func probeOnly(ok ...string) func(string) error {
	return func(p string) error {
		for _, o := range ok {
			if o == p {
				return nil
			}
		}
		return errors.New("exit status 71")
	}
}

func developerDir(dir string) func() (string, error) {
	return func() (string, error) { return dir, nil }
}

func TestGitDiscoverySkipsAppleLauncherOnPathAndUsesLaterEntry(t *testing.T) {
	f := newDiscoveryFixture(t)
	apple := f.git(t, "usr-bin")
	brew := f.git(t, "brew")
	d := gitDiscovery{PathDirs: []string{f.dir("usr-bin"), f.dir("brew")}, Apple: apple,
		Developer: developerDir(f.dir("none")), Probe: probeOnly(apple, brew)}
	got, attempts := d.find()
	if got != brew {
		t.Fatalf("chose %q, want %q (attempts %+v)", got, brew, attempts)
	}
	if len(attempts) == 0 || attempts[0].Path != apple || !strings.Contains(attempts[0].Reason, "Apple") {
		t.Fatalf("Apple launcher not reported as refused first: %+v", attempts)
	}
}

func TestGitDiscoveryNeverChoosesAppleLauncher(t *testing.T) {
	f := newDiscoveryFixture(t)
	apple := f.git(t, "usr-bin")
	// The probe would accept the launcher; only the explicit refusal may stop it.
	d := gitDiscovery{PathDirs: []string{f.dir("usr-bin")}, Apple: apple,
		Developer: developerDir(f.dir("none")), Probe: func(string) error { return nil }}
	if got, _ := d.find(); got != "" {
		t.Fatalf("chose the Apple launcher %q", got)
	}
}

func TestGitDiscoveryOrderPathThenDeveloperDirThenFallbacks(t *testing.T) {
	f := newDiscoveryFixture(t)
	apple := f.git(t, "usr-bin")
	pathGit := f.git(t, "path")
	clt := f.git(t, "clt/usr/bin")
	arm := f.git(t, "arm")
	intel := f.git(t, "intel")
	all := probeOnly(pathGit, clt, arm, intel)
	base := gitDiscovery{Apple: apple, Developer: developerDir(f.dir("clt")),
		Fallbacks: []string{arm, intel}, Probe: all}
	cases := []struct {
		name string
		path []string
		skip func(string) error
		want string
	}{
		{"PATH first", []string{f.dir("usr-bin"), f.dir("path")}, all, pathGit},
		{"developer dir before fallbacks", []string{f.dir("usr-bin")}, all, clt},
		{"arm before intel", []string{f.dir("usr-bin")}, probeOnly(arm, intel), arm},
		{"intel last", []string{f.dir("usr-bin")}, probeOnly(intel), intel},
	}
	for _, c := range cases {
		d := base
		d.PathDirs, d.Probe = c.path, c.skip
		if got, attempts := d.find(); got != c.want {
			t.Errorf("%s: chose %q, want %q (%+v)", c.name, got, c.want, attempts)
		}
	}
}

func TestGitDiscoveryProbeFailureMovesOnAndIsReported(t *testing.T) {
	f := newDiscoveryFixture(t)
	broken := f.git(t, "broken")
	good := f.git(t, "good")
	d := gitDiscovery{PathDirs: []string{f.dir("broken"), f.dir("good")}, Apple: "/nonexistent/git",
		Developer: developerDir(f.dir("none")), Probe: probeOnly(good)}
	got, attempts := d.find()
	if got != good {
		t.Fatalf("chose %q, want %q: a candidate that fails the probe must not be used", got, good)
	}
	if attempts[0].Path != broken || !strings.Contains(attempts[0].Reason, "probe failed") {
		t.Fatalf("probe failure not reported: %+v", attempts)
	}
}

func TestGitDiscoveryNoCandidateErrorListsEveryCandidateAndFix(t *testing.T) {
	f := newDiscoveryFixture(t)
	apple := f.git(t, "usr-bin")
	broken := f.git(t, "broken")
	missing := f.dir("missing/git")
	d := gitDiscovery{PathDirs: []string{f.dir("usr-bin"), f.dir("broken")}, Apple: apple,
		Developer: developerDir(f.dir("clt")), Fallbacks: []string{missing}, Probe: probeOnly()}
	got, attempts := d.find()
	if got != "" {
		t.Fatalf("chose %q", got)
	}
	err := gitDiscoveryError(attempts)
	var oe operationError
	if !errors.As(err, &oe) || oe.Code != "git-sandbox-unavailable" {
		t.Fatalf("error %v, want code git-sandbox-unavailable", err)
	}
	for _, want := range []string{apple, broken, filepath.Join(f.dir("clt"), "usr/bin/git"), missing, "Apple launcher", "probe failed", "not found", "brew install git"} {
		if !strings.Contains(oe.Message, want) {
			t.Errorf("message lacks %q: %s", want, oe.Message)
		}
	}
}

func TestGitDiscoveryNothingFoundAnywhereIsGitUnavailable(t *testing.T) {
	f := newDiscoveryFixture(t)
	d := gitDiscovery{PathDirs: []string{f.dir("empty")}, Apple: "/nonexistent/git",
		Developer: developerDir(f.dir("clt")), Fallbacks: []string{f.dir("x/git")}, Probe: probeOnly()}
	_, attempts := d.find()
	var oe operationError
	if err := gitDiscoveryError(attempts); !errors.As(err, &oe) || oe.Code != "git-unavailable" || !strings.Contains(oe.Message, "brew install git") {
		t.Fatalf("error %v", err)
	}
}

func TestGitDiscoveryIgnoresRelativePathEntries(t *testing.T) {
	f := newDiscoveryFixture(t)
	f.git(t, "rel")
	t.Chdir(f.root)
	d := gitDiscovery{PathDirs: []string{"", ".", "rel"}, Apple: "/nonexistent/git",
		Developer: developerDir(f.dir("none")), Probe: func(string) error { return nil }}
	if got, _ := d.find(); got != "" {
		t.Fatalf("chose relative candidate %q", got)
	}
}

func TestGitDiscoveryRejectsRelativeDeveloperDir(t *testing.T) {
	f := newDiscoveryFixture(t)
	f.git(t, "rel/usr/bin")
	t.Chdir(f.root)
	d := gitDiscovery{PathDirs: nil, Apple: "/nonexistent/git", Developer: developerDir("rel"),
		Probe: func(string) error { return nil }}
	got, attempts := d.find()
	if got != "" {
		t.Fatalf("chose %q from a relative developer dir", got)
	}
	if len(attempts) != 1 || !strings.Contains(attempts[0].Reason, "not absolute") {
		t.Fatalf("attempts %+v", attempts)
	}
}

func TestGitDiscoveryRefusesSymlinkToAppleLauncher(t *testing.T) {
	f := newDiscoveryFixture(t)
	apple := f.git(t, "usr-bin")
	if err := os.MkdirAll(f.dir("link"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(apple, filepath.Join(f.dir("link"), "git")); err != nil {
		t.Fatal(err)
	}
	d := gitDiscovery{PathDirs: []string{f.dir("link")}, Apple: apple, Probe: func(string) error { return nil }}
	got, attempts := d.find()
	if got != "" || len(attempts) != 1 || !strings.Contains(attempts[0].Reason, "Apple launcher") {
		t.Fatalf("chose %q, attempts %+v", got, attempts)
	}
}

func TestGitDiscoveryAppleLauncherFailsRealProbe(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS only")
	}
	err := probeGitInSandbox("/usr/bin/git")
	if err == nil || !strings.Contains(err.Error(), "exit status") {
		t.Fatalf("probe of the Apple launcher: %v", err)
	}
}

// The real probe runs git --version inside the sandbox; skipped where no native Git
// is selectable (stock macOS without Homebrew or Command Line Tools Git).
func TestGitDiscoveryRealProbeOnThisMachine(t *testing.T) {
	bin, err := gitExecutable()
	if err != nil {
		t.Skipf("no usable native Git here: %v", err)
	}
	if err := probeGitInSandbox(bin); err != nil {
		t.Fatalf("selected Git %s fails the sandbox probe: %v", bin, err)
	}
	if err := probeGitInSandbox("/nonexistent/git"); err == nil {
		t.Fatal("probe accepted a missing executable")
	}
}
