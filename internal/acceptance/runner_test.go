// SPDX-License-Identifier: EUPL-1.2
package acceptance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/workspace"
)

// The runner is plan-only. It reaches the target repository through exactly two
// doors: workspace.PlanTidyV3 (a planner; ApplyTidyV3 is never referenced) and a
// fixed set of read-only Git commands. Every other path is a temporary
// directory the runner created.

// config is the validated runner input.
type config struct {
	Repo, Base, ReportDir string
	Online                bool
	Remote                string
}

// loadConfig reads the runner input. enabled is false unless STW_ACCEPTANCE=1,
// so a plain `go test ./...` skips the runner.
func loadConfig(get func(string) string) (c config, enabled bool, err error) {
	if get("STW_ACCEPTANCE") != "1" {
		return c, false, nil
	}
	c = config{Repo: get("STW_ACCEPT_REPO"), Base: get("STW_ACCEPT_BASE"), ReportDir: get("STW_ACCEPT_REPORT_DIR"), Online: get("STW_ACCEPT_ONLINE") == "1", Remote: get("STW_ACCEPT_REMOTE")}
	if c.Remote == "" {
		c.Remote = "origin"
	}
	if c.Repo == "" || c.Base == "" || c.ReportDir == "" {
		return c, true, errors.New("STW_ACCEPT_REPO, STW_ACCEPT_BASE and STW_ACCEPT_REPORT_DIR are required")
	}
	return c, true, nil
}

// gitEnv is the constant environment of every Git process the runner starts.
func gitEnv(home string) []string {
	return []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + home, "LC_ALL=C", "LANG=C",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=core.hooksPath", "GIT_CONFIG_VALUE_0=/dev/null",
	}
}

// readGit runs one read-only Git command with the constant environment.
func readGit(dir string, env []string, args ...string) (string, error) {
	full := append([]string{"--no-optional-locks", "-c", "core.fsmonitor=false", "-C", dir}, args...)
	cmd := exec.Command("git", full...)
	cmd.Env = env
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

func sum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// fingerprint is what must not change in the target.
type fingerprint struct {
	Refs, Worktrees, Config, Status string // SHA-256 of the command output
	StatusLines                     int
	Objects                         int // files below the common object directory
}

func countFiles(dir string) (int, error) {
	n := 0
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			n++
		}
		return nil
	})
	return n, err
}

func takeFingerprint(repo string, env []string) (f fingerprint, err error) {
	run := func(args ...string) string {
		if err != nil {
			return ""
		}
		var out string
		out, err = readGit(repo, env, args...)
		return out
	}
	f.Refs = sum(run("for-each-ref", "--format=%(refname) %(objectname) %(symref)"))
	f.Worktrees = sum(run("worktree", "list", "--porcelain"))
	f.Config = sum(run("config", "--local", "--list", "--show-origin"))
	status := run("status", "--porcelain")
	f.Status, f.StatusLines = sum(status), strings.Count(status, "\n")
	common := strings.TrimSpace(run("rev-parse", "--path-format=absolute", "--git-common-dir"))
	if err != nil {
		return f, err
	}
	f.Objects, err = countFiles(filepath.Join(common, "objects"))
	return f, err
}

// diffFingerprints names every part that differs.
func diffFingerprints(a, b fingerprint) []string {
	var d []string
	for _, c := range []struct {
		name string
		same bool
	}{{"for-each-ref", a.Refs == b.Refs}, {"worktree list", a.Worktrees == b.Worktrees}, {"local config", a.Config == b.Config}, {"git status --porcelain", a.Status == b.Status && a.StatusLines == b.StatusLines}, {"object file count", a.Objects == b.Objects}} {
		if !c.same {
			d = append(d, c.name)
		}
	}
	return d
}

// within reports whether p equals root or lies below it.
func within(p, root string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolved is the validated, canonical runner input.
type resolved struct {
	config
	CommonDir string
	Worktrees []worktree
}

// check refuses unsafe input before anything is planned. The report directory
// must be new or empty and outside the target repository: its working tree, its
// common Git directory and every linked worktree.
func check(c config, env []string) (r resolved, err error) {
	repo, err := filepath.EvalSymlinks(c.Repo)
	if err != nil || !filepath.IsAbs(c.Repo) {
		return r, errors.New("STW_ACCEPT_REPO must be an existing absolute path")
	}
	if !strings.HasPrefix(c.Base, "refs/heads/") {
		return r, errors.New("STW_ACCEPT_BASE must be a full branch name such as refs/heads/main")
	}
	if !filepath.IsAbs(c.ReportDir) {
		return r, errors.New("STW_ACCEPT_REPORT_DIR must be an absolute path")
	}
	if _, err = readGit(repo, env, "rev-parse", "--git-dir"); err != nil {
		return r, fmt.Errorf("STW_ACCEPT_REPO is not a Git repository: %w", err)
	}
	if _, err = readGit(repo, env, "rev-parse", "--verify", "--quiet", c.Base+"^{commit}"); err != nil {
		return r, fmt.Errorf("base %s does not name a commit", c.Base)
	}
	top, err := readGit(repo, env, "rev-parse", "--path-format=absolute", "--show-toplevel", "--git-common-dir")
	if err != nil {
		return r, err
	}
	lines := strings.Fields(top)
	if len(lines) != 2 {
		return r, errors.New("a bare or unusual repository layout is not supported")
	}
	r.config, r.Repo, r.CommonDir = c, repo, lines[1]
	if r.Worktrees, err = listWorktrees(repo, env); err != nil {
		return r, err
	}
	// The report directory may not exist yet; resolve its nearest existing parent.
	dir := filepath.Clean(c.ReportDir)
	existing, rest := dir, ""
	for {
		if _, serr := os.Lstat(existing); serr == nil {
			break
		}
		rest = filepath.Join(filepath.Base(existing), rest)
		existing = filepath.Dir(existing)
	}
	real, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return r, err
	}
	dir = filepath.Join(real, rest)
	roots := []string{repo, r.CommonDir}
	for _, w := range r.Worktrees {
		roots = append(roots, w.Path)
	}
	for _, root := range roots {
		if within(dir, root) || within(root, dir) {
			return r, fmt.Errorf("the report directory %s overlaps the target repository path %s", dir, root)
		}
	}
	if st, serr := os.Lstat(dir); serr == nil {
		if !st.IsDir() {
			return r, errors.New("the report directory is not a directory")
		}
		entries, rerr := os.ReadDir(dir)
		if rerr != nil {
			return r, rerr
		}
		if len(entries) > 0 {
			return r, errors.New("the report directory is not empty")
		}
	}
	r.ReportDir = dir
	return r, nil
}

// worktree is one entry of `git worktree list --porcelain` with its read-only
// working state. These facts are not part of the plan.
type worktree struct {
	Path, Head, Branch     string
	Detached, Locked, Bare bool
	Prunable, Missing      bool
	Dirty                  bool
	DirtyEntries           int
}

func listWorktrees(repo string, env []string) ([]worktree, error) {
	out, err := readGit(repo, env, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var ws []worktree
	for _, block := range strings.Split(strings.TrimSpace(out), "\n\n") {
		var w worktree
		for _, line := range strings.Split(block, "\n") {
			key, val, _ := strings.Cut(line, " ")
			switch key {
			case "worktree":
				w.Path = val
			case "HEAD":
				w.Head = val
			case "branch":
				w.Branch = val
			case "detached":
				w.Detached = true
			case "locked":
				w.Locked = true
			case "bare":
				w.Bare = true
			case "prunable":
				w.Prunable = true
			}
		}
		if w.Path == "" {
			continue
		}
		if st, serr := os.Stat(w.Path); serr != nil || !st.IsDir() {
			w.Missing = true
		} else if !w.Bare {
			status, serr := readGit(w.Path, env, "status", "--porcelain")
			if serr != nil {
				return nil, serr
			}
			w.DirtyEntries = strings.Count(status, "\n")
			w.Dirty = w.DirtyEntries > 0
		}
		ws = append(ws, w)
	}
	return ws, nil
}

// planner is the single planning door; tests replace it.
type planner func(context.Context, workspace.V3PlanOptions) workspace.V3Result

// summary is the compact result of one run.
type summary struct {
	Repo, Base      string
	Online          bool
	Status          string
	Branches        int
	Classes         map[string]int // decision:proof-or-reason
	Reasons         map[string]int // reason of the plan's review summary
	Proven, Review  int
	Retained        int
	Worktrees       []worktree
	Runtime         time.Duration
	NativeProcesses int64
	HTTPRequests    int64
	Diagnostics     []string
}

func summarize(r resolved, res workspace.V3Result, d time.Duration) summary {
	s := summary{Repo: r.Repo, Base: r.Base, Online: r.Online, Status: res.Status, Classes: map[string]int{}, Reasons: map[string]int{}, Worktrees: r.Worktrees, Runtime: d}
	s.NativeProcesses, s.HTTPRequests = res.Boundary.Budget.NativeProcesses, res.Boundary.Budget.HTTPRequests
	for _, dg := range res.Diagnostics {
		s.Diagnostics = append(s.Diagnostics, fmt.Sprintf("%+v", dg))
	}
	if res.ReviewSummary != nil {
		s.Proven, s.Review, s.Retained = res.ReviewSummary.Proven, res.ReviewSummary.NeedsReview, res.ReviewSummary.Retained
		for k, v := range res.ReviewSummary.Reasons {
			s.Reasons[k] = v
		}
	}
	if res.Plan != nil {
		for _, c := range res.Plan.Candidates {
			why := c.Proof
			if c.Decision != "delete" {
				why = c.Reason
			}
			s.Classes[c.Decision+":"+why]++
			s.Branches++
		}
	}
	return s
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// String renders the compact summary the runner prints.
func (s summary) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "plan-only acceptance: repo %s base %s online=%v status=%s\n", s.Repo, s.Base, s.Online, s.Status)
	fmt.Fprintf(&b, "branches planned: %d (deletable by proof %d, needs-review %d, retained %d)\n", s.Branches, s.Proven, s.Review, s.Retained)
	b.WriteString("decision classes:\n")
	for _, k := range sortedKeys(s.Classes) {
		fmt.Fprintf(&b, "  %-40s %d\n", k, s.Classes[k])
	}
	b.WriteString("retain and review reasons (plan review summary):\n")
	for _, k := range sortedKeys(s.Reasons) {
		fmt.Fprintf(&b, "  %-40s %d\n", k, s.Reasons[k])
	}
	var dirty, locked, detached, missing int
	for _, w := range s.Worktrees {
		if w.Dirty {
			dirty++
		}
		if w.Locked {
			locked++
		}
		if w.Detached {
			detached++
		}
		if w.Missing {
			missing++
		}
	}
	fmt.Fprintf(&b, "worktree facts (not part of the plan): %d seen, %d dirty, %d locked, %d detached, %d missing\n", len(s.Worktrees), dirty, locked, detached, missing)
	for _, w := range s.Worktrees {
		var flags []string
		for _, f := range []struct {
			on   bool
			name string
		}{{w.Dirty, fmt.Sprintf("dirty(%d)", w.DirtyEntries)}, {w.Locked, "locked"}, {w.Detached, "detached"}, {w.Missing, "missing"}, {w.Prunable, "prunable"}} {
			if f.on {
				flags = append(flags, f.name)
			}
		}
		if len(flags) > 0 {
			fmt.Fprintf(&b, "  %s [%s] %s\n", w.Path, strings.Join(flags, ","), w.Branch)
		}
	}
	fmt.Fprintf(&b, "runtime %s, native processes %d, http requests %d\n", s.Runtime.Round(time.Millisecond), s.NativeProcesses, s.HTTPRequests)
	for _, d := range s.Diagnostics {
		fmt.Fprintf(&b, "diagnostic: %s\n", d)
	}
	return b.String()
}

// scratch holds the temporary directories the runner owns: a private HOME, the
// private object directory (TMPDIR of the planner, where merge-tree objects are
// created) and the temporary plan state.
type scratch struct{ Home, Objects, State string }

func newScratch(t *testing.T) scratch {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sc := scratch{filepath.Join(root, "home"), filepath.Join(root, "objects"), filepath.Join(root, "state")}
	for _, d := range []string{sc.Home, sc.Objects, sc.State} {
		if err = os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return sc
}

// run plans the target once, writes the reports and proves non-mutation. The
// returned summary is valid even when an error reports a changed target.
func run(ctx context.Context, c config, plan planner, sc scratch) (summary, error) {
	env := gitEnv(sc.Home)
	r, err := check(c, env)
	if err != nil {
		return summary{}, fmt.Errorf("acceptance runner refused: %w", err)
	}
	if err = os.MkdirAll(r.ReportDir, 0o700); err != nil {
		return summary{}, err
	}
	before, err := takeFingerprint(r.Repo, env)
	if err != nil {
		return summary{}, err
	}
	opts := workspace.V3PlanOptions{Path: r.Repo, Base: r.Base, StateDir: sc.State, ReportDir: r.ReportDir, Online: r.Online, ProtectWorktrees: []string{}}
	if r.Online {
		opts.Remote = r.Remote
	}
	start := time.Now()
	res := plan(ctx, opts)
	s := summarize(r, res, time.Since(start))
	after, ferr := takeFingerprint(r.Repo, env)
	if ferr != nil {
		return s, ferr
	}
	if d := diffFingerprints(before, after); len(d) > 0 {
		return s, fmt.Errorf("the plan-only run changed the target repository: %s", strings.Join(d, ", "))
	}
	if res.Status != "ok" || res.Plan == nil {
		return s, fmt.Errorf("planning did not succeed (status %q): %s", res.Status, strings.Join(s.Diagnostics, "; "))
	}
	raw, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return s, err
	}
	if err = os.WriteFile(filepath.Join(r.ReportDir, "result.json"), append(raw, '\n'), 0o600); err != nil {
		return s, err
	}
	return s, os.WriteFile(filepath.Join(r.ReportDir, "summary.txt"), []byte(s.String()), 0o600)
}

// isolate gives the in-process planner the constant Git environment and a
// private temporary directory, where the merge-tree objects are created.
func isolate(t *testing.T, sc scratch) {
	t.Helper()
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "GIT_") {
			t.Setenv(name, "")
			os.Unsetenv(name)
		}
	}
	for _, kv := range gitEnv(sc.Home) {
		name, val, _ := strings.Cut(kv, "=")
		t.Setenv(name, val)
	}
	t.Setenv("TMPDIR", sc.Objects)
}
