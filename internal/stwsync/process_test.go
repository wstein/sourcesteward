// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// childProcesses lists the processes whose parent is this test process (Linux /proc).
func childProcesses(t *testing.T) []string {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("needs /proc")
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		t.Skip("needs /proc")
	}
	var out []string
	me := strconv.Itoa(os.Getpid())
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("/proc", e.Name(), "stat"))
		if err != nil {
			continue
		}
		// pid (comm) state ppid ...: comm may contain spaces and parentheses, so cut after the last ")"
		s := string(raw)
		rest := strings.Fields(s[strings.LastIndex(s, ")")+1:])
		if len(rest) >= 2 && rest[1] == me {
			cmdline, _ := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
			out = append(out, e.Name()+" "+strings.ReplaceAll(string(cmdline), "\x00", " "))
		}
	}
	return out
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
	if procs := childProcesses(t); len(procs) != 0 {
		t.Errorf("processes left behind: %v", procs)
	}
}
