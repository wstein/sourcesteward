// SPDX-License-Identifier: EUPL-1.2
package watchcmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func prep(t *testing.T, f Flags) (Prepared, error) {
	t.Helper()
	return Prepare(context.Background(), f, &bytes.Buffer{})
}

func TestPrepareDefaults(t *testing.T) {
	p, err := prep(t, Flags{Repos: []string{"o/a", "o/b"}})
	if err != nil {
		t.Fatal(err)
	}
	r := p.Run
	if r.Mode != ModeStream || r.Interval != time.Minute || r.DetailEvery != 10 || r.PendingTimeout != 45*time.Minute || r.MaxOpenPRs != 2000 ||
		r.Host != "github.com" || r.Human || r.Baseline || len(r.Repos) != 2 || r.MaxIdle != 0 {
		t.Fatalf("%+v", r)
	}
	u, err := prep(t, Flags{Repos: []string{"o/a"}, UntilEvent: []string{"merged", "checks-green"}})
	if err != nil || u.Run.Mode != ModeUntil || u.Run.Timeout != 590*time.Second || !u.Run.UntilKinds["merged"] || !u.Run.UntilKinds["checks-green"] {
		t.Fatalf("%+v %v", u.Run, err)
	}
	o, err := prep(t, Flags{Repos: []string{"o/a"}, Once: true, Human: true, Baseline: true})
	if err != nil || o.Run.Mode != ModeOnce || !o.Run.Human || !o.Run.Baseline {
		t.Fatalf("%+v %v", o.Run, err)
	}
}

func TestPrepareRejectsInvalidCombinations(t *testing.T) {
	base := Flags{Repos: []string{"o/a"}}
	with := func(mod func(*Flags)) Flags { f := base; mod(&f); return f }
	cases := map[string]Flags{
		"once and stream":        with(func(f *Flags) { f.Once, f.Stream = true, true }),
		"once and until":         with(func(f *Flags) { f.Once, f.UntilEvent = true, []string{"merged"} }),
		"unknown kind":           with(func(f *Flags) { f.UntilEvent = []string{"review-state-changed"} }),
		"timeout without until":  with(func(f *Flags) { f.Timeout, f.TimeoutSet = "5m", true }),
		"timeout zero":           with(func(f *Flags) { f.UntilEvent, f.Timeout, f.TimeoutSet = []string{"merged"}, "0", true }),
		"timeout garbage":        with(func(f *Flags) { f.UntilEvent, f.Timeout, f.TimeoutSet = []string{"merged"}, "soon", true }),
		"interval too short":     with(func(f *Flags) { f.Interval, f.IntervalSet = "9s", true }),
		"detail-every zero":      with(func(f *Flags) { f.DetailEvery, f.DetailSet = 0, true }),
		"max-idle with once":     with(func(f *Flags) { f.Once, f.MaxIdle, f.MaxIdleSet = true, "5m", true }),
		"negative pending":       with(func(f *Flags) { f.PendingTO, f.PendingTOSet = "-1s", true }),
		"relative state dir":     with(func(f *Flags) { f.StateDir = "state" }),
		"bad host":               with(func(f *Flags) { f.Host = "git hub.com/x" }),
		"bad repo":               with(func(f *Flags) { f.Repos = []string{"nope"} }),
		"json without status":    with(func(f *Flags) { f.JSON = true }),
		"status with once":       with(func(f *Flags) { f.Status, f.Once = true, true }),
		"status with baseline":   with(func(f *Flags) { f.Status, f.Baseline = true, true }),
		"status with interval":   with(func(f *Flags) { f.Status, f.Interval, f.IntervalSet = true, "30s", true }),
		"status with human":      with(func(f *Flags) { f.Status, f.Human = true, true }),
		"path without workspace": with(func(f *Flags) { f.WorkspacePath = "/x" }),
		"missing config":         {Config: filepath.Join(t.TempDir(), "none.toml")},
		"no source":              {},
	}
	for name, f := range cases {
		if _, err := prep(t, f); !errors.Is(err, ErrUsage) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := prep(t, with(func(f *Flags) { f.PendingTO, f.PendingTOSet = "0", true })); err != nil {
		t.Fatalf("--pending-timeout 0 disables the timeout: %v", err)
	}
}

func TestPrepareMergesTheConfigAndLetsFlagsOverrideIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "watch.toml")
	os.WriteFile(path, []byte(fullConfig), 0o644)
	p, err := prep(t, Flags{Config: path, Repos: []string{"flag/first"}})
	if err != nil {
		t.Fatal(err)
	}
	r := p.Run
	if strings.Join(r.Repos, ",") != "flag/first,wstein/crewbook,wstein/sourcesteward" || r.Interval != 90*time.Second || r.DetailEvery != 4 || r.MaxOpenPRs != 500 ||
		len(r.IgnoreAuthors) != 1 || len(r.IgnoreLabels) != 1 || len(r.StackPatterns) != 1 {
		t.Fatalf("%+v", r)
	}
	p, err = prep(t, Flags{Config: path, Interval: "30s", IntervalSet: true, DetailEvery: 2, DetailSet: true})
	if err != nil || p.Run.Interval != 30*time.Second || p.Run.DetailEvery != 2 {
		t.Fatalf("%+v %v", p.Run, err)
	}
	bad := filepath.Join(t.TempDir(), "bad.toml")
	os.WriteFile(bad, []byte("schema_version = 1\nbogus = 1\n"), 0o644)
	if _, err = prep(t, Flags{Config: bad}); !errors.Is(err, ErrUsage) || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("%v", err)
	}
}

func TestPrepareStatusResolvesTheStateDirectory(t *testing.T) {
	p, err := prep(t, Flags{Status: true, JSON: true, StateDir: "/state/dir"})
	if err != nil || !p.IsStat || p.Status.StateDir != "/state/dir" || !p.Status.JSON {
		t.Fatalf("%+v %v", p, err)
	}
	a, err1 := prep(t, Flags{Status: true, Repos: []string{"o/a", "o/b"}})
	b, err2 := prep(t, Flags{Repos: []string{"O/B", "o/a"}})
	if err1 != nil || err2 != nil || a.Status.StateDir != StateDirFor("", "github.com", "", b.Run.Repos) || !filepath.IsAbs(a.Status.StateDir) {
		t.Fatalf("%v %v %q", err1, err2, a.Status.StateDir)
	}
	if _, err = prep(t, Flags{Status: true}); !errors.Is(err, ErrUsage) {
		t.Fatalf("a status without a directory or repositories: %v", err)
	}
}

func workspaceWithConfig(t *testing.T, content string) string {
	t.Helper()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	if out, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	os.Mkdir(filepath.Join(root, ".stw"), 0o755)
	if content != "" {
		os.WriteFile(filepath.Join(root, ".stw", "watch.toml"), []byte(content), 0o644)
	}
	t.Chdir(root)
	return root
}

func TestPrepareReadsTheImplicitConfigOnlyWithoutRepoOrWorkspace(t *testing.T) {
	workspaceWithConfig(t, fullConfig)
	p, err := prep(t, Flags{})
	if err != nil || p.Run.Interval != 90*time.Second || len(p.Run.Repos) != 2 {
		t.Fatalf("%+v %v", p.Run, err)
	}
	// with --repo the file is not read at all: neither its repos nor its interval apply
	p, err = prep(t, Flags{Repos: []string{"o/a"}})
	if err != nil || p.Run.Interval != time.Minute || len(p.Run.Repos) != 1 || len(p.Run.IgnoreAuthors) != 0 {
		t.Fatalf("%+v %v", p.Run, err)
	}
	// even an invalid file is not read then
	os.WriteFile(filepath.Join(".stw", "watch.toml"), []byte("bogus"), 0o644)
	if _, err = prep(t, Flags{Repos: []string{"o/a"}}); err != nil {
		t.Fatalf("%v", err)
	}
	if _, err = prep(t, Flags{}); !errors.Is(err, ErrUsage) {
		t.Fatalf("%v", err)
	}
}

func TestPrepareRefusesASymlinkOrFIFOAsTheImplicitConfig(t *testing.T) {
	root := workspaceWithConfig(t, "")
	target := filepath.Join(t.TempDir(), "real.toml")
	os.WriteFile(target, []byte(fullConfig), 0o644)
	link := filepath.Join(root, ".stw", "watch.toml")
	os.Symlink(target, link)
	if _, err := prep(t, Flags{}); !errors.Is(err, ErrUsage) || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("symlink: %v", err)
	}
	// an explicit --config may name a symlink
	if p, err := prep(t, Flags{Config: link}); err != nil || len(p.Run.Repos) != 2 {
		t.Fatalf("%+v %v", p.Run, err)
	}
	os.Remove(link)
	if err := syscall.Mkfifo(link, 0o644); err != nil {
		t.Skip("no FIFO support")
	}
	done := make(chan error, 1)
	go func() { _, err := prep(t, Flags{}); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, ErrUsage) {
			t.Fatalf("fifo: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a FIFO blocked the read")
	}
	os.Remove(link)
	os.RemoveAll(filepath.Join(root, ".stw"))
	os.Symlink(t.TempDir(), filepath.Join(root, ".stw"))
	if _, err := prep(t, Flags{}); err != nil && !errors.Is(err, ErrUsage) {
		t.Fatalf("%v", err)
	}
}
