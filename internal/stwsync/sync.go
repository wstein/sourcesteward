// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/wstein/sourcesteward/internal/inspection"
)

const (
	repoLimit       = 256
	branchRefsLimit = 1 << 16
)

// ValidateOptions is the usage check that runs before any effect.
func ValidateOptions(o Options) error {
	if len(o.Path) > 4096 || !utf8.ValidString(o.Path) || strings.ContainsRune(o.Path, 0) {
		return errors.New("PATH must be valid UTF-8 text without NUL, at most 4096 bytes")
	}
	return nil
}

func newResult(o Options) Result {
	mode := "sync"
	if o.Plan {
		mode = "plan"
	}
	return Result{
		SchemaVersion: 1, Command: "sync", Mode: mode,
		Options: ResultOptions{Rebase: o.Rebase.json(), Prune: o.Prune.json(), Autostash: o.Autostash.json()},
		Repos:   []Repo{}, Summary: Summary{ByResult: map[string]int{}},
	}
}

func (r Result) fail(code, message string) Result {
	r.Error = &Failure{Code: code, Message: oneLine(message)}
	return r
}

func oneLine(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	if len(s) > 200 {
		s = strings.ToValidUTF8(s[:200], "") + "..."
	}
	return strings.TrimSpace(s)
}

func ptr(s string) *string { return &s }

// env is one repository: the resolved Git executable and its working directory.
type env struct{ bin, dir string }

func (e env) g(ctx context.Context, args ...string) (gitOutput, error) {
	return git(ctx, e.bin, e.dir, args...)
}

func (e env) in(ctx context.Context, tree string, args ...string) (gitOutput, error) {
	return git(ctx, e.bin, tree, args...)
}

func (e env) withDir(dir string) env { return env{e.bin, dir} }

// Sync fetches and fast-forwards (or, with Rebase, rebases) the current branch of the workspace root
// repository and of each submodule that sits on a branch. With Plan it fetches the remote-tracking refs
// and reports what a run would do. Detached submodules are only reported.
func Sync(ctx context.Context, o Options) Result {
	r := newResult(o)
	if err := ValidateOptions(o); err != nil {
		return r.fail("sync-invalid-options", err.Error())
	}
	bin, err := exec.LookPath("git")
	if err != nil {
		return r.fail("sync-git-unavailable", "the Git executable is unavailable")
	}
	if bin, err = filepath.Abs(bin); err != nil {
		return r.fail("sync-git-unavailable", "the Git executable is unavailable")
	}
	id := inspection.Inspect(ctx, o.Path)
	if len(id.Evidence) > 0 || id.Backend == "jj" {
		return r.fail("sync-unsupported-backend", "jj-owned paths are not supported by stw sync; use jj git fetch (Git only, Spec 032)")
	}
	if id.Status != "ok" || id.Backend != "git" || id.Repository == nil {
		return r.fail("sync-unsupported-backend", "stw sync needs an unambiguous Git repository: "+id.Error())
	}
	e := env{bin, id.InputPath}
	top, err := e.g(ctx, "rev-parse", "--show-toplevel")
	if err != nil || top.Code != 0 || strings.TrimSpace(top.Out) == "" {
		return r.fail("sync-read-failed", "the repository has no working tree")
	}
	root := env{bin, strings.TrimSpace(top.Out)}
	subs, err := root.submodules(ctx)
	if err != nil {
		return r.fail("sync-read-failed", err.Error())
	}
	if len(subs) >= repoLimit {
		return r.fail("sync-limit", fmt.Sprintf("the repository has more than %d submodules", repoLimit-1))
	}
	runID, err := newRunID()
	if err != nil {
		return r.fail("sync-read-failed", "cannot create a run ID")
	}
	s := &session{opts: o, runID: runID}
	r.Repos = append(r.Repos, s.repo(ctx, root, ".", "root"))
	for _, p := range subs {
		r.Repos = append(r.Repos, s.repo(ctx, root.withDir(filepath.Join(root.dir, filepath.FromSlash(p))), p, "submodule"))
	}
	if s.journaled {
		r.RunID = ptr(runID)
	}
	for _, it := range r.Repos {
		r.Summary.ByResult[it.Result]++
	}
	r.Summary.Total = len(r.Repos)
	return r
}

// session carries the run ID and whether any journal record was written.
type session struct {
	opts      Options
	runID     string
	journaled bool
	journals  map[string]*journal
}

// submodules lists the paths of the direct submodules (gitlinks of the index), sorted.
func (e env) submodules(ctx context.Context) ([]string, error) {
	out, err := e.g(ctx, "ls-files", "--stage", "-z")
	if err != nil || out.Code != 0 {
		return nil, errors.New("cannot list the submodules")
	}
	var paths []string
	for _, rec := range strings.Split(out.Out, "\x00") {
		meta, path, ok := strings.Cut(rec, "\t")
		if ok && strings.HasPrefix(meta, "160000 ") && path != "" && !filepath.IsAbs(path) && !strings.Contains("/"+path+"/", "/../") {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return paths, nil
}

var remoteName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$`)

// repo syncs the current branch of one repository.
func (s *session) repo(ctx context.Context, e env, name, kind string) Repo {
	rp := Repo{Name: name, Path: e.dir, Kind: kind}
	skip := func(result, reason string) Repo { rp.Result, rp.Reason = result, ptr(reason); return rp }
	fail := func(reason string) Repo {
		rp.Result, rp.Reason, rp.NewOID = ResultFailed, ptr(oneLine(reason)), nil
		return rp
	}
	if _, err := os.Lstat(filepath.Join(e.dir, ".git")); err != nil {
		return skip(ResultSkippedUninitialized, "the submodule is not initialized")
	}
	if reason, _ := e.operation(ctx, e.dir); reason != "" {
		return skip(ResultSkippedBusy, reason)
	}
	sym, err := e.g(ctx, "symbolic-ref", "-q", "HEAD")
	if err != nil {
		return fail("cannot read HEAD")
	}
	ref := strings.TrimSpace(sym.Out)
	if sym.Code != 0 || !strings.HasPrefix(ref, "refs/heads/") {
		return skip(ResultSkippedDetached, "HEAD is detached; stw sync never moves a detached HEAD")
	}
	rp.Branch = ptr(strings.TrimPrefix(ref, "refs/heads/"))
	oldOID := e.revParse(ctx, e.dir, "HEAD")
	if oldOID == "" {
		return skip(ResultSkippedNoUpstream, "the branch has no commit yet")
	}
	rp.OldOID = ptr(oldOID)
	up, err := e.g(ctx, "for-each-ref", "--format=%(upstream)%00%(upstream:remotename)", ref)
	fields := strings.Split(strings.TrimSpace(up.Out), "\x00")
	if err != nil || up.Code != 0 || len(fields) != 2 || fields[0] == "" || fields[1] == "" || fields[1] == "." {
		return skip(ResultSkippedNoUpstream, "the branch has no upstream on a remote")
	}
	upstream, remote := fields[0], fields[1]
	rp.Remote = ptr(remote)
	prefix := "refs/remotes/" + remote + "/"
	if !remoteName.MatchString(remote) || !strings.HasPrefix(upstream, prefix) {
		return skip(ResultSkippedNoUpstream, "the upstream is not a remote-tracking branch of a plain remote name")
	}
	h, err := e.inventory(ctx)
	if err != nil {
		return fail("cannot list the worktrees")
	}
	if reason := h.block(ref); reason != "" {
		return skip(ResultSkippedBusy, reason)
	}
	before, err := listRefs(ctx, e, prefix)
	if err != nil {
		return fail(err.Error())
	}
	args := []string{"fetch", "--no-tags", "--no-write-fetch-head", "--no-recurse-submodules"}
	switch s.opts.Prune { // Unset: fetch.prune and remote.NAME.prune decide, as for git pull
	case On:
		args = append(args, "--prune")
	case Off:
		args = append(args, "--no-prune")
	}
	if out, err := e.g(ctx, append(args, "--", remote)...); err != nil || out.Code != 0 {
		return fail("git fetch failed: " + out.Diagnostic)
	}
	after, err := listRefs(ctx, e, prefix)
	if err != nil {
		return fail(err.Error())
	}
	for n := range before {
		if _, ok := after[n]; !ok {
			rp.Pruned++
		}
	}
	upstreamOID, ok := after[upstream]
	if !ok {
		return skip(ResultSkippedNoUpstream, "the remote branch no longer exists")
	}
	counts, err := e.g(ctx, "rev-list", "--left-right", "--count", "--end-of-options", ref+"..."+upstream)
	f := strings.Fields(counts.Out)
	if err != nil || counts.Code != 0 || len(f) != 2 {
		return fail("the branch cannot be compared with its upstream")
	}
	rp.Ahead, _ = strconv.Atoi(f[0])
	rp.Behind, _ = strconv.Atoi(f[1])
	mode := e.pullMode(ctx, s.opts, *rp.Branch)
	action := ""
	switch {
	case rp.Ahead == 0 && rp.Behind == 0:
		rp.Result = ResultUpToDate
		return rp
	case rp.Behind == 0:
		rp.Result = ResultAhead
		return rp
	case rp.Ahead == 0:
		action = ResultFastForward
	case mode.rebase:
		action = ResultRebase
	case mode.ffOnly:
		return skip(ResultDiverged, "not a fast-forward and --ff-only or pull.ff=only forbids anything else")
	default:
		return skip(ResultDiverged, "needs a choice: use --rebase or set pull.rebase (stw sync never merges)")
	}
	autostash := e.autostash(ctx, s.opts, mode)
	if action == ResultFastForward {
		rp.NewOID = ptr(upstreamOID)
	}
	if hasTrackedChanges(ctx, e, e.dir) && !autostash {
		if action == ResultRebase {
			return skip(ResultSkippedDirty, "cannot rebase with local changes (use --autostash or rebase.autoStash)")
		}
		if files := e.dirtyOverlap(ctx, oldOID, upstreamOID); len(files) > 0 {
			return skip(ResultSkippedDirty, "local changes would be overwritten: "+strings.Join(files, ", "))
		}
	}
	if s.opts.Plan {
		rp.Result = action
		return rp
	}
	return s.apply(ctx, e, rp, action, ref, upstream, upstreamOID, mode, autostash)
}

// hasTrackedChanges reports staged or unstaged changes of tracked files; untracked files do not count.
// A worktree that cannot be read counts as dirty so that nothing is ever changed in it.
func hasTrackedChanges(ctx context.Context, e env, tree string) bool {
	out, err := e.in(ctx, tree, "status", "--porcelain=v1", "-z", "--untracked-files=no")
	return err != nil || out.Code != 0 || out.Out != ""
}

func listRefs(ctx context.Context, e env, prefix string) (map[string]string, error) {
	out, err := e.g(ctx, "for-each-ref", "--format=%(refname)%00%(objectname)", prefix)
	if err != nil || out.Code != 0 {
		return nil, errors.New("cannot list the remote-tracking refs")
	}
	refs := map[string]string{}
	for _, line := range strings.Split(strings.TrimSuffix(out.Out, "\n"), "\n") {
		if name, oid, ok := strings.Cut(line, "\x00"); ok {
			refs[name] = oid
		}
	}
	return refs, nil
}
