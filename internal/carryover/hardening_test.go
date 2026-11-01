// SPDX-License-Identifier: EUPL-1.2
package carryover

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// twoStories builds two branches over the same base that end in the same tree, but whose first
// commit differs: old has "ok" first, new has "EVIL" first. A comparison that cannot see the first
// commit's content gives a false yes.
func twoStories(t *testing.T, r *repo) (base, oldHead, newHead string) {
	t.Helper()
	r.commit("f.txt", "start\n", "base")
	base = r.head()
	r.git("checkout", "-q", "-b", "old")
	r.commit("f.txt", "ok\n", "step")
	r.commit("f.txt", "final\n", "final")
	oldHead = r.head()
	r.git("checkout", "-q", "-b", "new", base)
	r.commit("f.txt", "EVIL\n", "step")
	r.commit("f.txt", "final\n", "final")
	return base, oldHead, r.head()
}

func TestAttributesThatHideTextDoNotHideACommit(t *testing.T) {
	for name, setup := range map[string]func(r *repo){
		"info/attributes": func(r *repo) {
			if err := os.WriteFile(filepath.Join(r.dir, ".git", "info", "attributes"), []byte("*.txt -diff\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		},
		"binary macro": func(r *repo) {
			if err := os.WriteFile(filepath.Join(r.dir, ".git", "info", "attributes"), []byte("*.txt binary\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		},
		"worktree .gitattributes": func(r *repo) { r.write(".gitattributes", "*.txt -diff\n") },
	} {
		t.Run(name, func(t *testing.T) {
			r := newRepo(t)
			base, oldHead, newHead := twoStories(t, r)
			setup(r)
			res := Check(t.Context(), NewRunner(r.dir), Request{PR: 1, OldBase: base, OldHead: oldHead, NewBase: base, NewHead: newHead})
			if res.CarryOver || res.RangeDiff.State != StateDiffers || res.RangeDiff.NotEqual == 0 {
				t.Fatalf("an intermediate commit with other content must differ: %+v", res)
			}
		})
	}
}

func TestSubmoduleIgnoreSettingsDoNotHideAGitlinkChange(t *testing.T) {
	for name, setup := range map[string]func(r *repo){
		".gitmodules ignore=all": func(r *repo) {
			r.write(".gitmodules", "[submodule \"sub\"]\n\tpath = sub\n\turl = https://example.invalid/sub.git\n\tignore = all\n")
		},
		"diff.ignoreSubmodules": func(r *repo) { r.git("config", "diff.ignoreSubmodules", "all") },
	} {
		t.Run(name, func(t *testing.T) {
			r := newRepo(t)
			r.write("a.txt", "a\n")
			setup(r)
			r.git("add", "-A")
			r.git("commit", "-q", "-m", "base")
			base := r.head()
			gitlink := func(branch, id string) string {
				r.git("checkout", "-q", "-b", branch, base)
				r.git("update-index", "--add", "--cacheinfo", "160000,"+id+",sub")
				r.git("commit", "-q", "-m", "bump sub")
				return r.head()
			}
			oldHead := gitlink("old", sha("2"))
			newHead := gitlink("new", sha("3"))
			res := Check(t.Context(), NewRunner(r.dir), Request{PR: 1, OldBase: base, OldHead: oldHead, NewBase: base, NewHead: newHead})
			if res.CarryOver || res.PatchID.State != StateDiffers || res.RangeDiff.State != StateDiffers {
				t.Fatalf("a different gitlink must differ in both conditions: %+v", res)
			}
		})
	}
}

func TestEnvironmentDropsTheRepositorySelectingVariables(t *testing.T) {
	names := []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE", "GIT_PREFIX"}
	for _, n := range names {
		t.Setenv(n, "/nonexistent/"+n)
	}
	t.Setenv("KEEP_ME", "1")
	env := strings.Join(environment(), "\n")
	for _, n := range names {
		if strings.Contains(env, n+"=") {
			t.Errorf("%s is passed to Git", n)
		}
	}
	for _, want := range []string{"KEEP_ME=1", "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C"} {
		if !strings.Contains(env, want) {
			t.Errorf("%s missing", want)
		}
	}
}

// The GIT_INDEX_FILE filter has no further observable effect on commit-to-commit reads in this Git
// (a corrupt index named by the variable is never opened); the variable stays filtered as a precaution.
func TestACorruptIndexFileOfTheEnvironmentChangesNothing(t *testing.T) {
	s := newStack(t)
	head := s.rebased("new")
	bad := filepath.Join(t.TempDir(), "index")
	if err := os.WriteFile(bad, []byte("not an index"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_INDEX_FILE", bad)
	wantReasons(t, s.check(t, s.m2, head))
}

func TestGuardRefusesAnExtraArgumentOnEveryShape(t *testing.T) {
	r := newRepo(t)
	_ = r
	vectors := map[string][]string{
		"rev-parse": {"rev-parse", "--verify", "--quiet", sha("1") + shaSuffix},
		"diff":      append(append([]string{}, diffFlags...), sha("1")+"..."+sha("2"), "--"),
		"patch-id":  {"patch-id", "--stable"},
		"merges":    {"rev-list", "--merges", "--end-of-options", sha("1") + ".." + sha("2")},
		"no-walk":   {"rev-list", "--no-walk", "--end-of-options", PullRef(7)},
		"log":       append(append([]string{}, logFlags...), sha("1")+".."+sha("2")),
		"fetch":     append(append([]string{}, fetchFlags...), "origin", "+refs/pull/7/head:"+PullRef(7)),
	}
	for name, v := range vectors {
		if err := guard(v); err != nil {
			t.Fatalf("%s: the reviewed vector is refused: %v", name, err)
		}
		if err := guard(append(append([]string{}, v...), "extra")); err == nil {
			t.Errorf("%s: an extra argument is allowed", name)
		}
		if err := guard(v[:len(v)-1]); err == nil {
			t.Errorf("%s: a missing argument is allowed", name)
		}
	}
	fetch := vectors["fetch"]
	if err := guard(append(append([]string{}, fetch...), "+refs/pull/8/head:"+PullRef(8))); err == nil {
		t.Error("a second refspec is allowed")
	}
}

func TestDiffAndLogPinTheSubmoduleAndBinaryFlags(t *testing.T) {
	for _, flag := range []string{"--binary", "--ignore-submodules=none", "--submodule=short"} {
		for name, flags := range map[string][]string{"diff": diffFlags, "log": logFlags} {
			found := false
			for _, f := range flags {
				found = found || f == flag
			}
			if !found {
				t.Errorf("%s vector lacks %s", name, flag)
			}
			var without []string
			for _, f := range flags {
				if f != flag {
					without = append(without, f)
				}
			}
			tail := sha("1") + ".." + sha("2")
			args := append(append([]string{}, without...), tail)
			if name == "diff" {
				args = append(append([]string{}, without...), sha("1")+"..."+sha("2"), "--")
			}
			if err := guard(args); err == nil {
				t.Errorf("%s without %s is allowed", name, flag)
			}
		}
	}
}

func TestParseLogRejectsMalformedOutput(t *testing.T) {
	good := "\x00msg1\n\x00\npatch1\n\x00msg2\n\x00\npatch2\n"
	got, err := parseLog(good, MaxCommits)
	if err != nil || len(got) != 2 || got[1].Message != "msg2\n" || got[0].Patch != "\npatch1\n" {
		t.Fatalf("%+v %v", got, err)
	}
	if got, err := parseLog("", MaxCommits); err != nil || len(got) != 0 {
		t.Fatalf("empty log: %+v %v", got, err)
	}
	for name, out := range map[string]string{
		"text before the first separator": "junk\x00msg\x00patch",
		"a message without a patch":       "\x00msg\x00patch\x00msg2",
		"no closing separator":            "\x00msg",
	} {
		if _, err := parseLog(out, MaxCommits); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := parseLog(good, 2); err != nil {
		t.Errorf("exactly the limit: %v", err)
	}
	if _, err := parseLog(good, 1); err == nil {
		t.Error("one over the limit: accepted")
	}
}

func TestRunnerBoundsTheCommitsPerSide(t *testing.T) {
	s := newStack(t)
	r := NewRunner(s.r.dir)
	r.MaxCommits = 1
	if _, err := r.Commits(t.Context(), s.m1, s.c2); err == nil || !strings.Contains(err.Error(), "more than 1") {
		t.Fatalf("two commits with a limit of one: %v", err)
	}
	r.MaxCommits = 2
	if got, err := r.Commits(t.Context(), s.m1, s.c2); err != nil || len(got) != 2 {
		t.Fatalf("%v %v", got, err)
	}
}
