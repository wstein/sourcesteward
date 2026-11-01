// SPDX-License-Identifier: EUPL-1.2
package carryover

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// hostileCase is one verdict the runner must reach whatever the configuration of the user says.
type hostileCase struct {
	name                               string
	oldBase, oldHead, newBase, newHead string
	wantYes                            bool
}

func lines12(change map[int]string) string {
	var b strings.Builder
	for i := 1; i <= 12; i++ {
		if i == 5 {
			b.WriteString("\n") // a blank context line
			continue
		}
		if v, ok := change[i]; ok {
			b.WriteString(v + "\n")
			continue
		}
		fmt.Fprintf(&b, "l%d\n", i)
	}
	return b.String()
}

// hostileRepo builds one repository with every scenario as a branch pair. f.txt and c.txt live in the
// root, sub/ holds only keep.txt: a run from sub/ sees nothing of the scenarios unless it escapes.
func hostileRepo(t *testing.T) (*repo, []hostileCase) {
	t.Helper()
	r := newRepo(t)
	r.write("a2.txt", lines12(nil))
	r.write("f.txt", "start\n")
	r.write("c.txt", "p\n")
	if err := os.MkdirAll(filepath.Join(r.dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.write("sub/keep.txt", "k\n")
	r.git("add", "-A")
	r.git("commit", "-q", "-m", "base")
	b0 := r.head()

	story := func(branch, from, l6, mid string) string {
		r.git("checkout", "-q", "-b", branch, from)
		r.write("a2.txt", lines12(map[int]string{6: l6}))
		r.write("f.txt", mid+"\n")
		r.git("add", "-A")
		r.git("commit", "-q", "-m", "step")
		r.write("f.txt", "final\n")
		r.git("add", "-A")
		r.git("commit", "-q", "-m", "final")
		return r.head()
	}
	oldHead := story("old", b0, "L6", "ok")
	evil := story("evil", b0, "L6", "EVIL")
	ws := story("ws", b0, "L6 ", "ok")

	r.git("checkout", "-q", "-b", "main1", b0)
	m1 := r.commit("c.txt", "p\nq\n", "m1")
	r.git("checkout", "-q", "-b", "pure", m1)
	r.git("cherry-pick", "old~1", "old")
	pure := r.head()

	r.git("checkout", "-q", "-b", "main2", b0)
	m2 := r.commit("a2.txt", lines12(map[int]string{4: "CHANGED4"}), "m2")
	r.git("checkout", "-q", "-b", "ctx", m2)
	r.git("cherry-pick", "old~1", "old")
	ctx := r.head()

	gitlink := func(branch, id string) string {
		r.git("checkout", "-q", "-b", branch, b0)
		r.git("update-index", "--add", "--cacheinfo", "160000,"+id+",gl")
		r.git("commit", "-q", "-m", "bump gl")
		return r.head()
	}
	g2, g3 := gitlink("g2", sha("2")), gitlink("g3", sha("3"))
	r.git("checkout", "-q", "main1")

	return r, []hostileCase{
		{"pure rebase", b0, oldHead, m1, pure, true},
		{"other content in an intermediate commit", b0, oldHead, b0, evil, false},
		{"whitespace only", b0, oldHead, b0, ws, false},
		{"changed context after a restack", b0, oldHead, m2, ctx, false},
		{"gitlink", b0, g2, b0, g3, false},
	}
}

func (c hostileCase) request() Request {
	return Request{PR: 9, OldBase: c.oldBase, OldHead: c.oldHead, NewBase: c.newBase, NewHead: c.newHead}
}

func TestVerdictsOfTheCleanConfiguration(t *testing.T) {
	r, cases := hostileRepo(t)
	for _, c := range cases {
		res := Check(t.Context(), NewRunner(r.dir), c.request())
		if res.CarryOver != c.wantYes {
			t.Errorf("%s: carry_over %v, want %v: %+v", c.name, res.CarryOver, c.wantYes, res)
		}
	}
}

// hostileEnvironment sets every variable that can change the output of diff, log or patch-id, or
// make Git start a program, to a harmful value. The returned files record any program that ran.
func hostileEnvironment(t *testing.T, r *repo) (markerLog string, trace string) {
	t.Helper()
	dir := t.TempDir()
	script, markerLog := marker(t, `cat "$1" 2>/dev/null`)
	cfg := "[diff]\n\tcontext = 0\n\trelative = true\n\texternal = " + script + "\n[core]\n\tquotePath = false\n\tpager = " + script + "\n[log]\n\tshowRoot = false\n"
	write := func(path, body string) string {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	global := write(filepath.Join(dir, "global"), cfg)
	home := filepath.Join(dir, "home")
	write(filepath.Join(home, ".gitconfig"), cfg)
	write(filepath.Join(dir, "xdg", "git", "config"), cfg)
	trace = filepath.Join(dir, "trace")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_CONFIG_SYSTEM", global)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "")
	t.Setenv("GIT_CONFIG_COUNT", "2")
	t.Setenv("GIT_CONFIG_KEY_0", "diff.relative")
	t.Setenv("GIT_CONFIG_VALUE_0", "true")
	t.Setenv("GIT_CONFIG_KEY_1", "diff.context")
	t.Setenv("GIT_CONFIG_VALUE_1", "0")
	t.Setenv("GIT_CONFIG_PARAMETERS", "'diff.context=0' 'diff.relative=true'")
	t.Setenv("GIT_DIFF_OPTS", "-u0")
	t.Setenv("GIT_EXTERNAL_DIFF", script)
	t.Setenv("GIT_PAGER", script)
	t.Setenv("GIT_NOTES_REF", "refs/notes/hostile")
	t.Setenv("GIT_EXEC_PATH", "/nonexistent/exec-path")
	t.Setenv("GIT_ATTR_SOURCE", "HEAD~1000")

	order := write(filepath.Join(dir, "order"), "sub/\n")
	attrs := write(filepath.Join(dir, "attrs"), "* -diff\n*.txt diff=strip\n")
	for k, v := range map[string]string{
		"diff.relative": "true", "diff.context": "0", "diff.algorithm": "patience", "diff.indentHeuristic": "false",
		"diff.interHunkContext": "50", "diff.mnemonicPrefix": "true", "diff.noprefix": "true", "diff.srcPrefix": "X/",
		"diff.dstPrefix": "Y/", "diff.suppressBlankEmpty": "true", "diff.orderFile": order, "diff.renames": "copies",
		"diff.renameLimit": "1", "diff.colorMoved": "zebra", "diff.colorMovedWS": "allow-indentation-change",
		"diff.wsErrorHighlight": "all", "diff.ignoreSubmodules": "all", "diff.submodule": "diff", "diff.external": script,
		"diff.strip.textconv": script, "diff.strip.command": script, "core.quotePath": "false", "core.abbrev": "4",
		"core.bigFileThreshold": "1", "core.attributesFile": attrs, "core.whitespace": "-trailing-space",
		"core.autocrlf": "true", "core.eol": "crlf", "core.pager": script, "core.notesRef": "refs/notes/hostile",
		"core.fsmonitor": script, "core.precomposeUnicode": "false", "log.showRoot": "false", "log.abbrevCommit": "true",
		"log.date": "relative", "log.decorate": "full", "log.follow": "true", "log.mailmap": "true",
		"log.showSignature": "true", "log.diffMerges": "separate", "i18n.logOutputEncoding": "ISO-8859-1",
		"i18n.commitEncoding": "ISO-8859-1", "format.pretty": "oneline", "format.notes": "true",
		"notes.displayRef": "refs/notes/*", "color.ui": "always", "color.diff": "always", "pager.log": script,
		"pager.diff": script, "gc.auto": "1", "fetch.writeCommitGraph": "true", "gpg.program": script,
	} {
		r.git("config", k, v)
	}
	t.Setenv("GIT_TRACE", trace) // after the fixture's own Git calls
	return markerLog, trace
}

func TestVerdictsDoNotChangeUnderHostileConfiguration(t *testing.T) {
	r, cases := hostileRepo(t)
	clean := make([]Result, len(cases))
	for i, c := range cases {
		clean[i] = Check(t.Context(), NewRunner(r.dir), c.request())
		if clean[i].CarryOver != c.wantYes {
			t.Fatalf("%s: the clean verdict is %v, want %v: %+v", c.name, clean[i].CarryOver, c.wantYes, clean[i])
		}
	}
	markerLog, trace := hostileEnvironment(t, r)
	for _, dir := range []struct{ name, path string }{{"root", r.dir}, {"subdirectory sub/", filepath.Join(r.dir, "sub")}} {
		for i, c := range cases {
			got := Check(t.Context(), NewRunner(dir.path), c.request())
			if !reflect.DeepEqual(got, clean[i]) {
				t.Errorf("%s from %s: the verdict changed\nclean:   %+v\nhostile: %+v", c.name, dir.name, clean[i], got)
			}
		}
	}
	if n := ran(t, markerLog); n != 0 {
		t.Errorf("a program of the hostile configuration ran %d times", n)
	}
	if _, err := os.Stat(trace); err == nil {
		t.Error("GIT_TRACE was honored")
	}
}

// The two reproductions of the review, each on its own so that a failure names the key.
func TestOneHostileSettingAtATime(t *testing.T) {
	for name, apply := range map[string]func(t *testing.T, r *repo){
		"diff.relative in the repository":     func(t *testing.T, r *repo) { r.git("config", "diff.relative", "true") },
		"diff.context=0 in the repository":    func(t *testing.T, r *repo) { r.git("config", "diff.context", "0") },
		"GIT_CONFIG_COUNT diff.relative":      func(t *testing.T, r *repo) { setConfigEnv(t, "diff.relative", "true") },
		"GIT_CONFIG_COUNT diff.context":       func(t *testing.T, r *repo) { setConfigEnv(t, "diff.context", "0") },
		"GIT_DIFF_OPTS=-u0":                   func(t *testing.T, r *repo) { t.Setenv("GIT_DIFF_OPTS", "-u0") },
		"global config diff.context=0":        func(t *testing.T, r *repo) { setGlobal(t, "[diff]\n\tcontext = 0\n") },
		"global config diff.relative=true":    func(t *testing.T, r *repo) { setGlobal(t, "[diff]\n\trelative = true\n") },
		"GIT_CONFIG_PARAMETERS diff.relative": func(t *testing.T, r *repo) { t.Setenv("GIT_CONFIG_PARAMETERS", "'diff.relative=true'") },
	} {
		t.Run(name, func(t *testing.T) {
			r, cases := hostileRepo(t)
			apply(t, r)
			for _, dir := range []string{r.dir, filepath.Join(r.dir, "sub")} {
				for _, c := range cases {
					if got := Check(t.Context(), NewRunner(dir), c.request()); got.CarryOver != c.wantYes {
						t.Errorf("%s (run in %s): carry_over %v, want %v: %+v", c.name, filepath.Base(dir), got.CarryOver, c.wantYes, got)
					}
				}
			}
		})
	}
}

func setConfigEnv(t *testing.T, key, value string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", key)
	t.Setenv("GIT_CONFIG_VALUE_0", value)
}

func setGlobal(t *testing.T, body string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "global")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", p)
}
