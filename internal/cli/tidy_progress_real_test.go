// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/analysis"
	"github.com/wstein/sourcesteward/internal/tidywizard/render"
)

// realWorld runs the real analysis (the real Git observation) over a small repository with two
// linked worktrees and three branches, with the progress clock moving one second per report.
func realWorld(t *testing.T) (repo string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo = filepath.Join(root, "fx")
	git := func(dir string, args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-c", "user.name=Fixture", "-c", "user.email=f@example.invalid", "-c", "init.defaultBranch=main"}, args...)...)
		c.Dir = dir
		if b, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, b)
		}
	}
	if err := os.Mkdir(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	git(repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "f"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(repo, "add", ".")
	git(repo, "commit", "-qm", "one")
	git(repo, "branch", "old")
	git(repo, "worktree", "add", "-q", "-b", "task-a", filepath.Join(root, "wt-a"))
	git(repo, "worktree", "add", "-q", "-b", "task-b", filepath.Join(root, "wt-b"))
	newWizardWorld(t, false, false)
	var tick atomic.Int64
	base := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)
	oldNow, oldPrep, oldAn, oldTTY := tidyProgressNow, tidyPrepare, tidyAnalyse, tidyStderrIsTerminal
	t.Cleanup(func() {
		tidyProgressNow, tidyPrepare, tidyAnalyse, tidyStderrIsTerminal = oldNow, oldPrep, oldAn, oldTTY
	})
	tidyProgressNow = func() time.Time { return base.Add(time.Duration(tick.Add(1)) * time.Second) }
	tidyStderrIsTerminal = func(any) bool { return false }
	tidyPrepare = func(context.Context, analysis.Options) (analysis.Prepared, error) {
		return analysis.Prepared{Workspace: root, Names: []string{"fx"}, Notices: []string{},
			Specs: []tidywizard.RepoSpec{{Name: "fx", Path: repo, Base: "refs/heads/main"}},
			Meta:  map[string]render.RepoMeta{"fx": {Path: repo, Backend: "git", Base: "refs/heads/main"}}}, nil
	}
	tidyAnalyse = analysis.Run
	return repo
}

var (
	gitLine     = regexp.MustCompile(`git processes: [\d,]+, \S+ in total`)
	slowestTail = regexp.MustCompile(`(?s)  slowest \d+ items:.*`)
)

// normalizeVerbose hides what depends on the machine: the Git process count and time, and the
// slowest items with their durations.
func normalizeVerbose(s string) string {
	s = gitLine.ReplaceAllString(s, "git processes: N, T in total")
	return slowestTail.ReplaceAllString(s, "  slowest K items:\n    ...\n")
}

// realVerboseSummary is the --verbose report of the real analysis, as the docs show it.
func realVerboseSummary(t *testing.T) string {
	t.Helper()
	realWorld(t)
	_, errText, err := runTidyErr(t, "plan", "--json", "--verbose")
	if err != nil {
		t.Fatal(err)
	}
	return errText[strings.Index(errText, "Timings:"):]
}

func TestTidyVerboseOfARealAnalysisHasNoBogusRows(t *testing.T) {
	sum := realVerboseSummary(t)
	if strings.Contains(sum, "[0/0]") || strings.Contains(sum, "unfinished") {
		t.Fatalf("a row without a repository: %s", sum)
	}
	for _, want := range []string{"[1/1] fx: analysed in", "observing worktrees 3/3", "reading branch history 4/4", "proving integration", "git processes: ", "observation window: not reached"} {
		if !strings.Contains(sum, want) {
			t.Errorf("%q missing in\n%s", want, sum)
		}
	}
}

// --trace writes one JSON line per Git process of the real analysis, attributed to the
// repository; without the wiring of the trace sink the file would stay empty.
func TestTidyTraceOfARealAnalysis(t *testing.T) {
	realWorld(t)
	file := filepath.Join(t.TempDir(), "git.jsonl")
	_, _, err := runTidyErr(t, "plan", "--json", "--trace", file)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	verbs := map[string]int{}
	repos := map[string]int{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var line struct {
			Repo string   `json:"repo"`
			Argv []string `json:"argv"`
			MS   float64  `json:"ms"`
			Exit *int     `json:"exit"`
		}
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil || len(line.Argv) == 0 || line.Exit == nil {
			t.Fatalf("%q: %v", sc.Text(), err)
		}
		verbs[line.Argv[0]]++
		repos[line.Repo]++
	}
	if verbs["rev-list"] == 0 || verbs["ls-tree"] == 0 || repos["fx"] == 0 {
		t.Fatalf("verbs %v repos %v", verbs, repos)
	}
	if info, err := os.Stat(file); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("%v %v", info, err)
	}
	// An existing file is never overwritten.
	if _, _, err = runTidyErr(t, "plan", "--json", "--trace", file); !isUsage(err) {
		t.Fatalf("existing trace file: %v", err)
	}
}

// A trace file inside the workspace or a repository would be an untracked file that changes what
// the analysis finds (a safe worktree becomes retained): it is refused before anything is read.
func TestTidyTraceInsideARepositoryIsRefused(t *testing.T) {
	repo := realWorld(t)
	for _, path := range []string{filepath.Join(repo, "trace.jsonl"), filepath.Join(filepath.Dir(repo), "trace.jsonl"), filepath.Join(repo, "sub", "..", "x")} {
		_, _, err := runTidyErr(t, "plan", "--json", "--trace", path)
		if !isUsage(err) || !strings.Contains(err.Error(), "trace-inside-repository") {
			t.Errorf("%s: %v", path, err)
		}
		if _, statErr := os.Stat(path); statErr == nil {
			t.Errorf("%s was created", path)
		}
	}
}

// The directory of the trace file is resolved: a symbolic link that leads into a repository is
// inside it, and a link that leads elsewhere is not.
func TestTraceThroughASymbolicLinkIntoARepositoryIsRefused(t *testing.T) {
	repo := realWorld(t)
	links := t.TempDir()
	into := filepath.Join(links, "into")
	away := filepath.Join(links, "away")
	if err := os.Symlink(repo, into); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), away); err != nil {
		t.Fatal(err)
	}
	if _, inside := tracePathInside("/cwd", filepath.Join(into, "t.jsonl"), []string{repo}); !inside {
		t.Error("a link into the repository is not seen")
	}
	if _, inside := tracePathInside("/cwd", filepath.Join(away, "t.jsonl"), []string{repo}); inside {
		t.Error("a link away from the repository is refused")
	}
	// The whole command, with the link as a relative path from a directory beside it.
	_, _, err := runTidyErr(t, "plan", "--json", "--trace", filepath.Join(into, "t.jsonl"))
	if !isUsage(err) || !strings.Contains(err.Error(), "trace-inside-repository") {
		t.Fatalf("%v", err)
	}
	if _, statErr := os.Stat(filepath.Join(repo, "t.jsonl")); statErr == nil {
		t.Fatal("the trace file was created in the repository")
	}
}

func TestTracePathInside(t *testing.T) {
	roots := []string{"/ws/a", "/ws/b"}
	for _, c := range []struct {
		cwd, path string
		inside    bool
	}{
		{"/cwd", "/ws/a/t", true}, {"/ws/a", "t", true}, {"/cwd", "/ws/at", false}, {"/cwd", "/ws/a", true},
		{"/cwd", "/other/ws/a/t", false}, {"/ws/a/b", "../../a/x", true}, {"/ws/a/b", "../../x", false}, {"/cwd", "/ws/b/deep/t", true},
	} {
		if _, got := tracePathInside(c.cwd, c.path, roots); got != c.inside {
			t.Errorf("%+v: %v", c, got)
		}
	}
}
