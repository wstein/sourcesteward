// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"context"
	"path/filepath"
	"testing"
)

func TestDiscoverListsTheWorkspaceMembersWithTheirRemotes(t *testing.T) {
	w := newWorkspace(t)
	runGit(t, w.dir("."), "remote", "set-url", "origin", "https://github.com/wstein/anchorage.git")
	runGit(t, w.dir("a"), "remote", "set-url", "origin", "git@github.com:wstein/a.git")
	runGit(t, w.dir("b"), "remote", "rename", "origin", "upstream") // the only remote is used
	runGit(t, w.dir("dev"), "remote", "add", "extra", "https://example.org/x/y")
	runGit(t, w.dir("dev"), "remote", "set-url", "origin", "https://github.com/wstein/dev")
	runGit(t, w.dir("."), "submodule", "deinit", "-q", "-f", "c")
	for _, start := range []string{w.ws, w.dir("a/n")} {
		got, err := Discover(context.Background(), start)
		if err != nil {
			t.Fatal(err)
		}
		if got.Root != w.ws {
			t.Fatalf("root %q", got.Root)
		}
		byName := map[string]Member{}
		var names []string
		for _, m := range got.Members {
			byName[m.Name] = m
			names = append(names, m.Name)
		}
		if len(names) != 6 || names[0] != "." {
			t.Fatalf("members %v", names)
		}
		for name, want := range map[string]string{".": "https://github.com/wstein/anchorage.git", "a": "git@github.com:wstein/a.git", "dev": "https://github.com/wstein/dev"} {
			if m := byName[name]; !m.Initialized || m.Remote != want || m.Path != filepath.Join(w.ws, name) && name != "." {
				t.Errorf("%s: %+v", name, m)
			}
		}
		if m := byName["b"]; m.Remote != w.bare["b"] {
			t.Errorf("only remote not used: %+v", m)
		}
		if m := byName["c"]; m.Initialized || m.Remote != "" {
			t.Errorf("uninitialized submodule: %+v", m)
		}
	}
}

func TestDiscoverRefusesAPathOutsideAnyRepository(t *testing.T) {
	if _, err := Discover(context.Background(), t.TempDir()); err == nil {
		t.Fatal("expected an error")
	}
}
