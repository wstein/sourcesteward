// SPDX-License-Identifier: EUPL-1.2
package acceptance

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/workspace"
)

// git runs a fixture-building command with a throwaway identity in the
// environment of that one command (never in any configuration).
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(gitEnv(t.TempDir()), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.test", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.test")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func lines(n int, tag string) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(tag + " line\n")
	}
	return b.String()
}

// fixture builds a repository whose main integrates `native` by ancestry,
// `rebased` by an equal patch (cherry-pick), and leaves `unmerged` alone; the
// branch `busy` is checked out in a dirty linked worktree.
func fixture(t *testing.T) (repo, linked string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo = filepath.Join(root, "target")
	if err = os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q", "-b", "main")
	write(t, filepath.Join(repo, "base.txt"), lines(5, "base"))
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "base")
	for _, b := range []string{"native", "rebased", "unmerged", "busy"} {
		git(t, repo, "checkout", "-q", "-b", b, "main")
		write(t, filepath.Join(repo, b+".txt"), lines(3, b))
		git(t, repo, "add", ".")
		git(t, repo, "commit", "-q", "-m", b)
	}
	git(t, repo, "checkout", "-q", "main")
	write(t, filepath.Join(repo, "main.txt"), lines(2, "main"))
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "main moves")
	git(t, repo, "merge", "-q", "--no-ff", "-m", "merge native", "native")
	git(t, repo, "cherry-pick", "rebased")
	linked = filepath.Join(root, "linked")
	git(t, repo, "worktree", "add", "-q", linked, "busy")
	write(t, filepath.Join(linked, "scratch.txt"), "uncommitted\n")
	return repo, linked
}

func newConfig(repo string) config {
	return config{Repo: repo, Base: "refs/heads/main", ReportDir: filepath.Join(filepath.Dir(repo), "report"), Remote: "origin"}
}

func TestDefaultRunIsDisabled(t *testing.T) {
	c, enabled, err := loadConfig(func(string) string { return "" })
	if enabled || err != nil || c.Repo != "" {
		t.Fatalf("the runner must be off by default: %+v %v %v", c, enabled, err)
	}
	_, enabled, err = loadConfig(func(k string) string { return map[string]string{"STW_ACCEPTANCE": "1"}[k] })
	if !enabled || err == nil {
		t.Fatalf("an enabled runner without input must refuse: %v %v", enabled, err)
	}
	c, enabled, err = loadConfig(func(k string) string {
		return map[string]string{"STW_ACCEPTANCE": "1", "STW_ACCEPT_REPO": "/r", "STW_ACCEPT_BASE": "refs/heads/main", "STW_ACCEPT_REPORT_DIR": "/o", "STW_ACCEPT_ONLINE": "1"}[k]
	})
	if !enabled || err != nil || !c.Online || c.Remote != "origin" {
		t.Fatalf("config %+v %v %v", c, enabled, err)
	}
}

func TestRefusesUnsafeInput(t *testing.T) {
	repo, linked := fixture(t)
	env := gitEnv(t.TempDir())
	cases := map[string]func(c *config){
		"report dir inside the target":        func(c *config) { c.ReportDir = filepath.Join(repo, "report") },
		"report dir inside the git directory": func(c *config) { c.ReportDir = filepath.Join(repo, ".git", "report") },
		"report dir inside a linked worktree": func(c *config) { c.ReportDir = filepath.Join(linked, "report") },
		"report dir is the target":            func(c *config) { c.ReportDir = repo },
		"report dir contains the target":      func(c *config) { c.ReportDir = filepath.Dir(repo) },
		"relative report dir":                 func(c *config) { c.ReportDir = "report" },
		"non-empty report dir":                func(c *config) { c.ReportDir = t.TempDir(); write(t, filepath.Join(c.ReportDir, "x"), "x") },
		"report dir is a file":                func(c *config) { c.ReportDir = filepath.Join(repo, "base.txt") },
		"unknown base":                        func(c *config) { c.Base = "refs/heads/nope" },
		"short base":                          func(c *config) { c.Base = "main" },
		"target is not a repository":          func(c *config) { c.Repo = t.TempDir() },
		"relative target":                     func(c *config) { c.Repo = "target" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c := newConfig(repo)
			mutate(&c)
			if _, err := check(c, env); err == nil {
				t.Fatalf("%s must be refused", name)
			}
		})
	}
	if _, err := check(newConfig(repo), env); err != nil {
		t.Fatalf("a new report directory beside the target must be accepted: %v", err)
	}
	c := newConfig(repo)
	c.ReportDir = t.TempDir()
	if _, err := check(c, env); err != nil {
		t.Fatalf("an empty report directory must be accepted: %v", err)
	}
}

func TestRefusalIsReportedBeforeAnythingIsPlanned(t *testing.T) {
	repo, _ := fixture(t)
	c := newConfig(repo)
	c.ReportDir = filepath.Join(repo, "report")
	planned := false
	_, err := run(context.Background(), c, func(context.Context, workspace.V3PlanOptions) workspace.V3Result {
		planned = true
		return workspace.V3Result{}
	}, newScratch(t))
	if err == nil || planned || !strings.Contains(err.Error(), "acceptance runner refused") {
		t.Fatalf("planned=%v err=%v", planned, err)
	}
	if _, serr := os.Lstat(c.ReportDir); serr == nil {
		t.Fatal("a refused run must not create the report directory")
	}
}

func TestNonMutationCheckDetectsEveryKindOfChange(t *testing.T) {
	cases := map[string]struct {
		mutate func(t *testing.T, repo, linked string)
		want   string
	}{
		"new ref":   {func(t *testing.T, repo, _ string) { git(t, repo, "branch", "intruder", "main") }, "refs/heads/"},
		"moved ref": {func(t *testing.T, repo, _ string) { git(t, repo, "branch", "-f", "unmerged", "main") }, "refs/heads/"},
		"worktree": {func(t *testing.T, repo, linked string) {
			git(t, repo, "worktree", "add", "-q", "-b", "extra", filepath.Join(filepath.Dir(linked), "extra"), "main")
		}, "worktree-list"},
		"local config": {func(t *testing.T, repo, _ string) { git(t, repo, "config", "--local", "tidy.test", "1") }, ".git/config"},
		"status":       {func(t *testing.T, repo, _ string) { write(t, filepath.Join(repo, "stray.txt"), "x") }, "status:"},
		"pack-refs":    {func(t *testing.T, repo, _ string) { git(t, repo, "pack-refs", "--all") }, "packed-refs"},
		"index refresh": {func(t *testing.T, repo, _ string) {
			now := time.Now().Add(time.Hour)
			if err := os.Chtimes(filepath.Join(repo, "base.txt"), now, now); err != nil {
				t.Fatal(err)
			}
			git(t, repo, "update-index", "--refresh")
		}, ".git/index"},
		"reflog append": {func(t *testing.T, repo, _ string) {
			f, err := os.OpenFile(filepath.Join(repo, ".git", "logs", "HEAD"), os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			f.WriteString("0 0 t <t@example.test> 0 +0000\tnoise\n")
			f.Close()
		}, "logs/HEAD"},
		"dirty edit in a linked worktree": {func(t *testing.T, _, linked string) { write(t, filepath.Join(linked, "another.txt"), "x") }, "status:"},
		"description":                     {func(t *testing.T, repo, _ string) { write(t, filepath.Join(repo, ".git", "description"), "changed\n") }, ".git/description"},
		"repack":                          {func(t *testing.T, repo, _ string) { git(t, repo, "repack", "-a", "-d", "-q") }, "objects/"},
		"object": {func(t *testing.T, repo, _ string) {
			cmd := exec.Command("git", "-C", repo, "hash-object", "-w", "--stdin")
			cmd.Stdin = strings.NewReader("a stray object\n")
			cmd.Env = gitEnv(t.TempDir())
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v %s", err, out)
			}
		}, "objects/"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			repo, linked := fixture(t)
			sc := newScratch(t)
			isolate(t, sc)
			_, err := run(context.Background(), newConfig(repo), func(ctx context.Context, o workspace.V3PlanOptions) workspace.V3Result {
				tc.mutate(t, repo, linked)
				return workspace.PlanTidyV3(ctx, o)
			}, sc)
			if err == nil || !strings.Contains(err.Error(), "changed the target repository") || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want a mutation error naming %q, got %v", tc.want, err)
			}
		})
	}
}

func TestPlanOnlyRunOnFixture(t *testing.T) {
	repo, linked := fixture(t)
	sc := newScratch(t)
	isolate(t, sc)
	c := newConfig(repo)
	// The planner removes its private object directory when it returns, so
	// watch it while the plan runs: the proof objects must appear there.
	var seen int
	planned := func(ctx context.Context, o workspace.V3PlanOptions) workspace.V3Result {
		done := make(chan struct{})
		finished := make(chan struct{})
		go func() {
			defer close(finished)
			for {
				if entries, _ := os.ReadDir(sc.Objects); len(entries) > seen {
					seen = len(entries)
				}
				select {
				case <-done:
					return
				case <-time.After(200 * time.Microsecond):
				}
			}
		}()
		res := workspace.PlanTidyV3(ctx, o)
		close(done)
		<-finished
		return res
	}
	s, err := run(context.Background(), c, planned, sc)
	if err != nil {
		t.Fatalf("%v\n%s", err, s)
	}
	if seen == 0 {
		t.Error("the planner created no object directory under the private TMPDIR")
	}
	if s.Status != "ok" || s.Branches != 5 || s.Online {
		t.Fatalf("summary %+v", s)
	}
	if s.Classes["delete:native-ancestry"] < 1 {
		t.Errorf("a native ancestry branch is expected: %v", s.Classes)
	}
	var patch, unmerged bool
	for k := range s.Classes {
		patch = patch || strings.HasPrefix(k, "delete:") && strings.Contains(k, "patch")
		unmerged = unmerged || strings.HasPrefix(k, "retain:")
	}
	if !patch || !unmerged {
		t.Errorf("patch-id deletion and a retained branch are expected: %v", s.Classes)
	}
	if s.Classes["retain:workspace-used"] != 1 {
		t.Errorf("the branch of the linked worktree is retained as workspace-used: %v", s.Classes)
	}
	var facts []worktree
	for _, w := range s.Worktrees {
		if w.Path == linked {
			facts = append(facts, w)
		}
	}
	if len(s.Worktrees) != 2 || len(facts) != 1 || !facts[0].Dirty || facts[0].Locked || facts[0].Detached || facts[0].Branch != "refs/heads/busy" {
		t.Errorf("worktree facts %+v", s.Worktrees)
	}
	for _, name := range []string{"tidy-report.json", "tidy-report.md", "result.json", "summary.txt"} {
		if st, serr := os.Stat(filepath.Join(c.ReportDir, name)); serr != nil || st.Size() == 0 {
			t.Errorf("%s missing: %v", name, serr)
		}
	}
	text := s.String()
	for _, want := range []string{"decision classes:", "retain and review reasons", "worktree facts (not part of the plan): 2 seen, 1 dirty, 0 locked, 0 detached", "dirty(1)", "runtime "} {
		if !strings.Contains(text, want) {
			t.Errorf("summary text lacks %q:\n%s", want, text)
		}
	}
}

func TestUnreadableWorktreeIsAFactNotAnAbort(t *testing.T) {
	repo, linked := fixture(t)
	if err := os.Remove(filepath.Join(linked, ".git")); err != nil {
		t.Fatal(err)
	}
	ws, err := listWorktrees(repo, gitEnv(t.TempDir()))
	if err != nil {
		t.Fatalf("an unreadable worktree must not abort: %v", err)
	}
	var found bool
	for _, w := range ws {
		found = found || w.Path == linked && w.Unreadable
	}
	if !found || !strings.Contains((summary{Worktrees: ws}).String(), "unreadable") {
		t.Fatalf("%+v", ws)
	}
}

func TestReportDirectorySymlinkIntoTheGitDirectoryIsRefused(t *testing.T) {
	repo, _ := fixture(t)
	link := filepath.Join(filepath.Dir(repo), "link")
	if err := os.Symlink(filepath.Join(repo, ".git"), link); err != nil {
		t.Fatal(err)
	}
	c := newConfig(repo)
	c.ReportDir = filepath.Join(link, "report")
	if _, err := check(c, gitEnv(t.TempDir())); err == nil {
		t.Fatal("a report directory reached through a symbolic link into .git must be refused")
	}
}

func TestWorktreeFactsReportLockedAndDetached(t *testing.T) {
	repo, linked := fixture(t)
	git(t, repo, "worktree", "lock", linked)
	detached := filepath.Join(filepath.Dir(repo), "detached")
	git(t, repo, "worktree", "add", "-q", "--detach", detached, "main")
	ws, err := listWorktrees(repo, gitEnv(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]worktree{}
	for _, w := range ws {
		got[w.Path] = w
	}
	if !got[linked].Locked || !got[linked].Dirty || !got[detached].Detached || got[detached].Dirty || got[repo].Branch != "refs/heads/main" {
		t.Fatalf("%+v", ws)
	}
}

// TestPlanOnlyAcceptance is the opt-in runner: STW_ACCEPTANCE=1 with
// STW_ACCEPT_REPO, STW_ACCEPT_BASE and STW_ACCEPT_REPORT_DIR (optional
// STW_ACCEPT_ONLINE=1, STW_ACCEPT_REMOTE). It never applies a plan.
func TestPlanOnlyAcceptance(t *testing.T) {
	c, enabled, err := loadConfig(os.Getenv)
	if !enabled {
		t.Skip("set STW_ACCEPTANCE=1 (and STW_ACCEPT_REPO, STW_ACCEPT_BASE, STW_ACCEPT_REPORT_DIR) to run the plan-only acceptance runner")
	}
	if err != nil {
		t.Fatalf("acceptance runner refused: %v", err)
	}
	sc := newScratch(t)
	isolate(t, sc)
	s, err := run(context.Background(), c, workspace.PlanTidyV3, sc)
	if s.Repo != "" {
		os.Stdout.WriteString(s.String())
	}
	if err != nil {
		t.Fatal(err)
	}
}
