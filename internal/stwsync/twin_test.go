// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// twin is one single-repository scenario: a bare origin, a work clone behind (or diverged from) it.
type twin struct{ work string }

func newTwin(t *testing.T, cfg map[string]string, diverged bool, dirty string) twin {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bare, work, other := filepath.Join(root, "o.git"), filepath.Join(root, "w"), filepath.Join(root, "x")
	runGit(t, root, "init", "-q", "--bare", "-b", "main", bare)
	runGit(t, root, "clone", "-q", bare, work)
	runGit(t, work, "checkout", "-q", "-b", "main")
	var lines []string
	for i := 1; i <= 10; i++ {
		lines = append(lines, "line "+itoa(i))
	}
	write(t, filepath.Join(work, "f"), strings.Join(lines, "\n")+"\n")
	write(t, filepath.Join(work, "g"), "g\n")
	runGit(t, work, "add", ".")
	runGit(t, work, "commit", "-q", "-m", "base")
	runGit(t, work, "push", "-q", "-u", "origin", "main")
	runGit(t, root, "clone", "-q", bare, other)
	lines[0] = "line 1 edited remotely"
	write(t, filepath.Join(other, "f"), strings.Join(lines, "\n")+"\n")
	write(t, filepath.Join(other, "r"), "r\n")
	runGit(t, other, "add", ".")
	runGit(t, other, "commit", "-q", "-m", "remote")
	runGit(t, other, "push", "-q", "origin", "main")
	if diverged {
		commit(t, work, "l", "local\n")
	}
	switch dirty {
	case "other-file":
		write(t, filepath.Join(work, "g"), "g edited locally\n")
	case "same-file":
		lines[0] = "line 1"
		lines[9] = "line 10 edited locally"
		write(t, filepath.Join(work, "f"), strings.Join(lines, "\n")+"\n")
	}
	for k, v := range cfg {
		runGit(t, work, "config", k, v)
	}
	return twin{work}
}

// outcome describes the repository after a pull or a sync in terms both must agree on.
func (w twin) outcome(t *testing.T, old string) string {
	t.Helper()
	head := runGit(t, w.work, "rev-parse", "HEAD")
	class := "refused"
	switch {
	case head == old:
	case runGit(t, w.work, "rev-list", "--merges", "--count", old+".."+head) != "0":
		class = "merged"
	case gitCmd(w.work, "merge-base", "--is-ancestor", old, head).Run() == nil:
		class = "fast-forwarded"
	default:
		class = "rebased"
	}
	f, _ := os.ReadFile(filepath.Join(w.work, "f"))
	g, _ := os.ReadFile(filepath.Join(w.work, "g"))
	return strings.Join([]string{class, runGit(t, w.work, "rev-parse", "HEAD^{tree}"), runGit(t, w.work, "status", "--porcelain"), runGit(t, w.work, "stash", "list"), string(f), string(g)}, "|")
}

func gitPull(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"pull"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_MERGE_AUTOEDIT=no", "GIT_EDITOR=true", "GIT_TERMINAL_PROMPT=0")
	_ = cmd.Run() // a refusal is an outcome, not a test error
}

// TestSyncHasTheOutcomeOfGitPull runs real `git pull` and `stw sync` on twin repositories for every
// combination of pull.rebase, pull.ff, autostash settings, local changes and history shape, and requires
// the same outcome class (except where git pull merges, see below), the same tree and the same worktree and stash state.
func TestSyncHasTheOutcomeOfGitPull(t *testing.T) {
	for _, rebase := range []string{"", "true", "false"} {
		for _, ff := range []string{"", "true", "false", "only"} {
			for _, dirty := range []string{"", "other-file", "same-file"} {
				for _, autostash := range []bool{false, true} {
					for _, diverged := range []bool{false, true} {
						cfg := map[string]string{}
						if rebase != "" {
							cfg["pull.rebase"] = rebase
						}
						if ff != "" {
							cfg["pull.ff"] = ff
						}
						if autostash {
							cfg["rebase.autoStash"], cfg["merge.autoStash"] = "true", "true"
						}
						name := strings.Join([]string{"rebase=" + rebase, "ff=" + ff, "dirty=" + dirty, "autostash=" + map[bool]string{true: "y", false: "n"}[autostash], "diverged=" + map[bool]string{true: "y", false: "n"}[diverged]}, "/")
						t.Run(name, func(t *testing.T) {
							t.Parallel()
							a := newTwin(t, cfg, diverged, dirty)
							b := newTwin(t, cfg, diverged, dirty)
							oldA, oldB := runGit(t, a.work, "rev-parse", "HEAD"), runGit(t, b.work, "rev-parse", "HEAD")
							gitPull(t, a.work)
							want := a.outcome(t, oldA)
							r := Sync(context.Background(), Options{Path: b.work})
							if r.Error != nil {
								t.Fatal(r.Error)
							}
							got := b.outcome(t, oldB)
							if strings.HasPrefix(want, "merged|") {
								// owner decision 2026-10-24: the one deliberate difference to git pull. Where git pull would
								// create a merge commit, stw sync does not: a diverged branch stays, a fast-forwardable one is
								// fast-forwarded without a merge commit.
								if strings.HasPrefix(got, "merged|") {
									t.Errorf("stw sync created a merge commit where only git pull does: %s", got)
								}
								if mergeCount := runGit(t, b.work, "rev-list", "--merges", "--count", oldB+"..HEAD"); mergeCount != "0" {
									t.Errorf("%s merge commits", mergeCount)
								}
								// and the right alternative: a diverged branch is unchanged and reported diverged, a
								// fast-forwardable one reaches the upstream tip
								head := runGit(t, b.work, "rev-parse", "HEAD")
								if diverged {
									if head != oldB || r.Repos[0].Result != ResultDiverged {
										t.Errorf("a diverged branch must stay unchanged and diverged: %s (%v)", r.Repos[0].Result, deref(r.Repos[0].Reason))
									}
								} else if head != runGit(t, b.work, "rev-parse", "origin/main") && r.Repos[0].Result != ResultSkippedDirty {
									t.Errorf("a fast-forwardable branch must reach the upstream: %s", r.Repos[0].Result)
								}
								return
							}
							if got != want {
								t.Errorf("sync: %s (%v)\ngit pull: %s", got, deref(r.Repos[0].Reason), want)
							}
						})
					}
				}
			}
		}
	}
}
