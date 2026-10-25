// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// recordGit runs fn with a git wrapper first in PATH that logs the argument vector of every call (one line per call,
// arguments separated by 0x1f) before running the real git; it returns the calls without the fixed leading
// `-c gc.auto=0 -c maintenance.auto=false -C DIR` and with DIR separately.
func recordGit(t *testing.T, fn func()) (calls []gitCall) {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\037' \"$a\" >> '" + log + "'; done\nprintf '\\n' >> '" + log + "'\nexec '" + real + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	fn()
	raw, _ := os.ReadFile(log)
	for _, line := range strings.Split(string(raw), "\n") {
		args := strings.Split(strings.TrimSuffix(line, "\x1f"), "\x1f")
		if len(args) > 6 && args[0] == "-c" && args[1] == "gc.auto=0" && args[2] == "-c" && args[3] == "maintenance.auto=false" && args[4] == "-C" {
			calls = append(calls, gitCall{dir: args[5], args: args[6:]})
		}
	}
	return calls
}

type gitCall struct {
	dir  string
	args []string
}

// commandsIn returns the argument vectors that start (after the global -c options) with verb, run in dir.
func commandsIn(calls []gitCall, dir, verb string) [][]string {
	var out [][]string
	for _, c := range calls {
		i := 0
		for i+1 < len(c.args) && c.args[i] == "-c" {
			i += 2
		}
		if c.dir == dir && i < len(c.args) && c.args[i] == verb {
			out = append(out, c.args)
		}
	}
	return out
}

func remoteTip(t *testing.T, w *workspace, name string) string {
	t.Helper()
	return runGit(t, w.root, "--git-dir", w.bare[name], "rev-parse", "main")
}

// The exact argument vectors of the fixed overrides of decision 8, under a configuration that would otherwise make Git
// write beyond the journaled ref.
func TestFixedArgumentVectors(t *testing.T) {
	cfg := map[string]string{"submodule.recurse": "true", "rebase.updateRefs": "true", "rebase.autoStash": "true", "merge.autoStash": "true", "fetch.prune": "true"}
	cases := []struct {
		name  string
		repo  string // the repository under test: "." (default) or a submodule path
		extra map[string]string
		opts  Options
		setup func(t *testing.T, w *workspace)
		verb  string
		want  func(w *workspace) []string
	}{
		{"fast-forward with autostash from the configuration", "", nil, Options{}, func(t *testing.T, w *workspace) {}, "merge",
			func(w *workspace) []string {
				return []string{"-c", "submodule.recurse=false", "merge", "--ff-only", "--autostash", remoteTip(t, w, "ws")}
			}},
		{"fast-forward with --no-autostash", "", nil, Options{Autostash: Off}, func(t *testing.T, w *workspace) {}, "merge",
			func(w *workspace) []string {
				return []string{"-c", "submodule.recurse=false", "merge", "--ff-only", "--no-autostash", remoteTip(t, w, "ws")}
			}},
		{"rebase", "", map[string]string{"pull.rebase": "true"}, Options{}, func(t *testing.T, w *workspace) { commit(t, w.ws, "local.txt", "l\n") }, "rebase",
			func(w *workspace) []string {
				return []string{"-c", "submodule.recurse=false", "-c", "rebase.updateRefs=false", "rebase", "--no-update-refs", "--autostash", "--fork-point", "refs/remotes/origin/main"}
			}},
		{"rebase with --no-autostash (a dirty tree would be skipped, so the tree is clean)", "", map[string]string{"pull.rebase": "true"}, Options{Autostash: Off}, func(t *testing.T, w *workspace) { commit(t, w.ws, "local.txt", "l\n") }, "rebase",
			func(w *workspace) []string {
				return []string{"-c", "submodule.recurse=false", "-c", "rebase.updateRefs=false", "rebase", "--no-update-refs", "--no-autostash", "--fork-point", "refs/remotes/origin/main"}
			}},
		{"merge", "", nil, Options{Merge: true}, func(t *testing.T, w *workspace) { commit(t, w.ws, "local.txt", "l\n") }, "merge",
			func(w *workspace) []string {
				return []string{"-c", "submodule.recurse=false", "merge", "--no-edit", "--ff", "--autostash", remoteTip(t, w, "ws")}
			}},
		{"fetch from the configuration", "", nil, Options{}, func(t *testing.T, w *workspace) {}, "fetch",
			func(w *workspace) []string {
				return []string{"fetch", "--no-tags", "--no-write-fetch-head", "--no-recurse-submodules", "--", "origin"}
			}},
		{"fetch --prune", "", nil, Options{Prune: On}, func(t *testing.T, w *workspace) {}, "fetch",
			func(w *workspace) []string {
				return []string{"fetch", "--no-tags", "--no-write-fetch-head", "--no-recurse-submodules", "--prune", "--", "origin"}
			}},
		{"fetch --no-prune", "", nil, Options{Prune: Off}, func(t *testing.T, w *workspace) {}, "fetch",
			func(w *workspace) []string {
				return []string{"fetch", "--no-tags", "--no-write-fetch-head", "--no-recurse-submodules", "--no-prune", "--", "origin"}
			}},
		{"fast-forward with autostash off in the configuration", "", map[string]string{"merge.autoStash": "false"}, Options{}, func(t *testing.T, w *workspace) {}, "merge",
			func(w *workspace) []string {
				return []string{"-c", "submodule.recurse=false", "merge", "--ff-only", "--no-autostash", remoteTip(t, w, "ws")}
			}},
		{"rebase with autostash off in the configuration (clean tree)", "", map[string]string{"pull.rebase": "true", "rebase.autoStash": "false"}, Options{}, func(t *testing.T, w *workspace) { commit(t, w.ws, "local.txt", "l\n") }, "rebase",
			func(w *workspace) []string {
				return []string{"-c", "submodule.recurse=false", "-c", "rebase.updateRefs=false", "rebase", "--no-update-refs", "--no-autostash", "--fork-point", "refs/remotes/origin/main"}
			}},
		{"rebase keeping local merges (pull.rebase=merges)", "", map[string]string{"pull.rebase": "merges"}, Options{}, func(t *testing.T, w *workspace) { commit(t, w.ws, "local.txt", "l\n") }, "rebase",
			func(w *workspace) []string {
				return []string{"-c", "submodule.recurse=false", "-c", "rebase.updateRefs=false", "rebase", "--no-update-refs", "--autostash", "--fork-point", "--rebase-merges", "refs/remotes/origin/main"}
			}},
		{"submodule: fast-forward with its own autostash setting", "b", map[string]string{"merge.autoStash": "true"}, Options{}, func(t *testing.T, w *workspace) {}, "merge",
			func(w *workspace) []string {
				return []string{"-c", "submodule.recurse=false", "merge", "--ff-only", "--autostash", remoteTip(t, w, "b")}
			}},
		{"submodule: fast-forward without an autostash setting", "b", nil, Options{}, func(t *testing.T, w *workspace) {}, "merge",
			func(w *workspace) []string {
				return []string{"-c", "submodule.recurse=false", "merge", "--ff-only", "--no-autostash", remoteTip(t, w, "b")}
			}},
		{"submodule: rebase", "b", map[string]string{"pull.rebase": "true", "rebase.autoStash": "true"}, Options{}, func(t *testing.T, w *workspace) { commit(t, w.dir("b"), "local.txt", "l\n") }, "rebase",
			func(w *workspace) []string {
				return []string{"-c", "submodule.recurse=false", "-c", "rebase.updateRefs=false", "rebase", "--no-update-refs", "--autostash", "--fork-point", "refs/remotes/origin/main"}
			}},
		{"submodule: fetch", "b", nil, Options{}, func(t *testing.T, w *workspace) {}, "fetch",
			func(w *workspace) []string {
				return []string{"fetch", "--no-tags", "--no-write-fetch-head", "--no-recurse-submodules", "--", "origin"}
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorkspace(t)
			repo := tc.repo
			if repo == "" {
				repo = "."
			}
			dir := w.dir(repo)
			for k, v := range cfg {
				runGit(t, w.ws, "config", k, v)
			}
			for k, v := range tc.extra {
				runGit(t, dir, "config", k, v)
			}
			tc.setup(t, w)
			w.advance(t, repo, "main")
			if !strings.Contains(tc.name, "tree is clean") && !strings.Contains(tc.name, "clean tree") && repo == "." {
				write(t, filepath.Join(w.ws, "ws.txt"), "local edit\n") // a tracked edit: the autostash settings matter
			}
			var r Result
			calls := recordGit(t, func() {
				r = Sync(context.Background(), Options{Path: w.ws, Autostash: tc.opts.Autostash, Prune: tc.opts.Prune, Merge: tc.opts.Merge})
			})
			if r.Error != nil {
				t.Fatal(r.Error)
			}
			got := commandsIn(calls, dir, tc.verb)
			want := tc.want(w)
			if len(got) != 1 || !reflect.DeepEqual(got[0], want) {
				t.Fatalf("argument vectors %q, want exactly %q", got, want)
			}
		})
	}
}

// Decision 8 as a whole: a configuration that would move submodule HEADs, rewrite stacked branches, leave a hidden
// stash or prune behind must not do so; fetch.prune from the configuration IS honoured (never stricter than git pull)
// and --no-prune overrides it.
func TestConfigurationPolicyOfDecisionEight(t *testing.T) {
	for _, pruneFlag := range []Tri{Unset, Off} {
		w := newWorkspace(t)
		adv := filepath.Join(w.root, "bump")
		runGit(t, w.root, "clone", "-q", w.bare["ws"], adv)
		runGit(t, adv, "submodule", "update", "--init", "-q", "b")
		commit(t, filepath.Join(adv, "b"), "b-new.txt", "n\n")
		runGit(t, filepath.Join(adv, "b"), "push", "-q", "origin", "HEAD:refs/heads/side")
		runGit(t, adv, "add", "b")
		runGit(t, adv, "commit", "-q", "-m", "bump b")
		runGit(t, adv, "push", "-q", "origin", "main")
		runGit(t, filepath.Join(w.ws, "b"), "fetch", "-q", "origin")
		for k, v := range map[string]string{"submodule.recurse": "true", "rebase.updateRefs": "true", "rebase.autoStash": "true", "merge.autoStash": "true", "fetch.prune": "true", "pull.rebase": "true"} {
			runGit(t, w.ws, "config", k, v)
		}
		commit(t, w.ws, "local.txt", "l\n")
		runGit(t, w.ws, "branch", "stacked")
		commit(t, w.ws, "local2.txt", "l2\n")
		stacked, bHead, cHead := w.oid(t, ".", "stacked"), w.oid(t, "b", "HEAD"), w.oid(t, "c", "HEAD")
		runGit(t, w.ws, "push", "-q", "origin", "main:gone")
		runGit(t, w.ws, "fetch", "-q", "origin")
		runGit(t, w.root, "--git-dir", w.bare["ws"], "branch", "-q", "-D", "gone")
		write(t, filepath.Join(w.ws, "ws.txt"), "local edit\n")
		r := run(t, w, Options{Prune: pruneFlag})
		g := repoByName(t, r)["."]
		if g.Result != ResultRebased {
			t.Fatalf("%+v %v", g, deref(g.Reason))
		}
		if w.oid(t, ".", "stacked") != stacked {
			t.Error("rebase.updateRefs moved a stacked branch")
		}
		if w.oid(t, "b", "HEAD") != bHead || w.oid(t, "c", "HEAD") != cHead {
			t.Error("submodule.recurse moved a submodule HEAD")
		}
		if runGit(t, w.ws, "stash", "list") != "" {
			t.Error("a hidden stash is left")
		}
		if b, _ := os.ReadFile(filepath.Join(w.ws, "ws.txt")); string(b) != "local edit\n" {
			t.Errorf("the autostashed edit did not come back: %q", b)
		}
		pruned := runGit(t, w.ws, "branch", "-r", "--list", "origin/gone") == ""
		if pruned != (pruneFlag == Unset) {
			t.Errorf("prune flag %v: ref pruned=%v", pruneFlag, pruned)
		}
	}
}
