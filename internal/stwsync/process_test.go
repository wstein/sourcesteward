// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// leftoverProcesses lists every process of this user, other than the test itself, that a run could have left behind,
// found without relying on the parent: a process that inherited the marker in its environment (everything the run
// starts inherits it, also after setsid or a double fork reparented it to init) or whose working directory is below
// root (a daemonized Git command that dropped the environment still sits in a repository).
func leftoverProcesses(t *testing.T, marker, root string) []string {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("needs /proc")
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		t.Skip("needs /proc")
	}
	me := os.Getpid()
	var out []string
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == me {
			continue
		}
		reason := ""
		if env, err := os.ReadFile(filepath.Join("/proc", e.Name(), "environ")); err == nil && strings.Contains("\x00"+string(env), "\x00"+markerName+"="+marker+"\x00") {
			reason = "marker"
		}
		if cwd, err := os.Readlink(filepath.Join("/proc", e.Name(), "cwd")); err == nil && (cwd == root || strings.HasPrefix(cwd, root+string(os.PathSeparator))) {
			reason = "cwd"
		}
		if reason == "" {
			continue
		}
		stat, _ := os.ReadFile(filepath.Join("/proc", e.Name(), "stat"))
		if i := strings.LastIndex(string(stat), ")"); i >= 0 {
			if f := strings.Fields(string(stat)[i+1:]); len(f) > 0 && f[0] == "Z" {
				continue // an exited child that is not reaped yet is no process left running
			}
		}
		cmdline, _ := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		out = append(out, e.Name()+" "+reason+" "+strings.ReplaceAll(string(cmdline), "\x00", " "))
	}
	return out
}

const markerName = "STW_SYNC_TEST_MARKER"

// markRun sets the environment marker that every process of the run inherits and returns it.
func markRun(t *testing.T) string {
	t.Helper()
	marker := strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	t.Setenv(markerName, marker)
	return marker
}

// The detector sees a process that was detached with setsid (parent: init) and one that only kept the marker.
func TestLeftoverDetectorFindsDetachedProcesses(t *testing.T) {
	root := t.TempDir()
	marker := markRun(t)
	if leftovers := leftoverProcesses(t, marker, root); len(leftovers) != 0 {
		t.Fatalf("not clean to begin with: %v", leftovers)
	}
	cmd := exec.Command("setsid", "sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skip("setsid is not available")
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if len(leftoverProcesses(t, marker, root)) > 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("a setsid process carrying the marker was not found")
}

// FR-012: a run (plan, sync, restore, with every option that starts more Git work) installs no hook and leaves no
// process behind, and the hook directories of every repository are unchanged afterwards.
func TestARunInstallsNoHookAndLeavesNoProcess(t *testing.T) {
	w := newWorkspace(t)
	hooks := []string{filepath.Join(w.ws, ".git", "hooks")}
	for _, name := range []string{"a", "b", "c", "dev"} {
		hooks = append(hooks, filepath.Join(w.ws, ".git", "modules", name, "hooks"))
	}
	hooks = append(hooks, filepath.Join(w.ws, ".git", "modules", "a", "modules", "n", "hooks"))
	before := treeDigest(t, hooks...)
	if before == "" {
		t.Fatal("fixture: no hook files to compare")
	}
	w.advance(t, ".", "main")
	w.advance(t, "a", "main")
	commit(t, w.dir("b"), "local.txt", "l\n")
	w.advance(t, "b", "main")
	write(t, filepath.Join(w.ws, "ws.txt"), "dirty\n")
	marker := markRun(t)
	ctx := context.Background()
	for _, o := range []Options{{Plan: true}, {Autostash: On, Prune: On, Submodules: true}, {Rebase: On, Submodules: true}, {Merge: true}} {
		o.Path = w.ws
		if r := Sync(ctx, o); r.Error != nil {
			t.Fatal(r.Error)
		}
	}
	if r := Restore(ctx, RestoreOptions{Path: w.ws}); r.Error != nil {
		t.Fatal(r.Error)
	}
	if after := treeDigest(t, hooks...); after != before {
		t.Errorf("a hook was installed, removed or changed:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if procs := leftoverProcesses(t, marker, w.root); len(procs) != 0 {
		t.Errorf("processes left behind: %v", procs)
	}
}
