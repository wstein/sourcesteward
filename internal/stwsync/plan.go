// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"context"
	"errors"
	"fmt"
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
	defaultRemote   = "origin"
	branchLimit     = 1024
	prunedRefsLimit = 256
)

var remoteName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$`)

// ValidateOptions is the usage check that runs before any effect.
func ValidateOptions(o Options) error {
	if o.Remote != "" && !remoteName.MatchString(o.Remote) {
		return errors.New("--remote must be a plain remote name of at most 128 characters")
	}
	if len(o.Path) > 4096 || !utf8.ValidString(o.Path) || strings.ContainsRune(o.Path, 0) {
		return errors.New("PATH must be valid UTF-8 text without NUL, at most 4096 bytes")
	}
	return nil
}

func newResult(o Options) Result {
	return Result{
		SchemaVersion: 1, Command: "sync", Mode: "plan", Remote: o.Remote,
		Options: ResultOptions{Prune: o.Prune},
		Fetch:   Fetch{PrunedRefs: []string{}}, Items: []Item{}, Submodules: []Submodule{},
		Summary: Summary{ByResult: map[string]int{}},
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

// Plan fetches the selected remote once (remote-tracking refs only) and classifies every local branch
// that tracks it. It changes no local branch, worktree, stash or tag, and refuses before the fetch
// for a jj-owned or unsupported path, an unknown remote or too many branches.
func Plan(ctx context.Context, o Options) Result {
	if o.Remote == "" {
		o.Remote = defaultRemote
	}
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
	dir := id.InputPath
	g := func(args ...string) (gitOutput, error) { return git(ctx, bin, dir, args...) }
	inTree := func(tree string, args ...string) (gitOutput, error) { return git(ctx, bin, tree, args...) }

	remotes, err := g("remote")
	if err != nil || remotes.Code != 0 || !contains(strings.Split(remotes.Out, "\n"), o.Remote) {
		return r.fail("sync-remote-unknown", "the repository has no remote "+o.Remote)
	}
	branches, err := listBranches(g)
	if err != nil {
		return r.fail("sync-read-failed", err.Error())
	}
	if len(branches) > branchLimit {
		return r.fail("sync-limit", fmt.Sprintf("the repository has more than %d local branches", branchLimit))
	}
	prefix := "refs/remotes/" + o.Remote + "/"
	if err := checkRefspecs(g, o.Remote, prefix); err != nil {
		return r.fail("sync-refspec-unsafe"[:0]+"sync-refspec-unsafe", err.Error())
	}
	before, err := listRefs(g, prefix)
	if err != nil {
		return r.fail("sync-read-failed", err.Error())
	}

	// The one network effect; it may only change refs/remotes/REMOTE/*.
	args := []string{"fetch", "--no-tags", "--no-write-fetch-head", "--no-recurse-submodules"}
	if o.Prune {
		args = append(args, "--prune")
	} else {
		args = append(args, "--no-prune") // a fetch.prune setting must not remove refs without --prune
	}
	if out, err := g(append(args, "--", o.Remote)...); err != nil || out.Code != 0 {
		return r.fail("sync-fetch-failed", "git fetch failed: "+out.Diagnostic)
	}
	r.Fetch.Performed = true
	after, err := listRefs(g, prefix)
	if err != nil {
		return r.fail("sync-read-failed", err.Error())
	}
	var pruned []string
	for name := range before {
		if _, ok := after[name]; !ok {
			pruned = append(pruned, name)
		}
	}
	sort.Strings(pruned)
	r.Fetch.PrunedTotal = len(pruned)
	if len(pruned) > prunedRefsLimit {
		pruned = pruned[:prunedRefsLimit]
	}
	r.Fetch.PrunedRefs = append(r.Fetch.PrunedRefs, pruned...)

	checkedOut, err := listWorktrees(g)
	if err != nil {
		return r.fail("sync-read-failed", err.Error())
	}
	dirty := map[string]bool{}
	for _, b := range branches {
		if !strings.HasPrefix(b.upstream, prefix) {
			r.Summary.Untracked++
			continue
		}
		item := Item{Branch: strings.TrimPrefix(b.name, "refs/heads/"), Result: ResultUpToDate, OldOID: ptr(b.oid)}
		if tree, ok := checkedOut[b.name]; ok {
			item.Worktree = ptr(tree)
		}
		upstreamOID, present := after[b.upstream]
		switch {
		case !present:
			item.Result, item.Reason = ResultUpstreamGone, ptr("the remote branch no longer exists")
		case upstreamOID == b.oid:
		default:
			item.Result, item.Reason = classify(g, b.name, b.upstream)
			if item.Result == ResultFastForward {
				item.NewOID = ptr(upstreamOID)
				if item.Worktree != nil {
					if _, seen := dirty[*item.Worktree]; !seen {
						dirty[*item.Worktree] = hasTrackedChanges(inTree, *item.Worktree)
					}
					if dirty[*item.Worktree] {
						item.Result, item.Reason = ResultSkippedDirty, ptr("the worktree has tracked or staged changes")
					}
				}
			}
		}
		r.Items = append(r.Items, item)
		r.Summary.ByResult[item.Result]++
	}
	sort.Slice(r.Items, func(i, j int) bool { return r.Items[i].Branch < r.Items[j].Branch })
	r.Summary.Total = len(r.Items)
	return r
}

func ptr(s string) *string { return &s }

func contains(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

type branch struct{ name, oid, upstream string }

func listBranches(g func(...string) (gitOutput, error)) ([]branch, error) {
	out, err := g("for-each-ref", "--format=%(refname)%00%(objectname)%00%(upstream)", "refs/heads/")
	if err != nil || out.Code != 0 {
		return nil, errors.New("cannot list the local branches")
	}
	var list []branch
	for _, line := range strings.Split(strings.TrimSuffix(out.Out, "\n"), "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, "\x00")
		if len(f) != 3 {
			return nil, errors.New("unexpected branch listing")
		}
		list = append(list, branch{f[0], f[1], f[2]})
	}
	return list, nil
}

func listRefs(g func(...string) (gitOutput, error), prefix string) (map[string]string, error) {
	out, err := g("for-each-ref", "--format=%(refname)%00%(objectname)", prefix)
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

// listWorktrees maps each checked-out branch (full ref name) to the path of its worktree.
func listWorktrees(g func(...string) (gitOutput, error)) (map[string]string, error) {
	out, err := g("worktree", "list", "--porcelain", "-z")
	if err != nil || out.Code != 0 {
		return nil, errors.New("cannot list the worktrees")
	}
	checkedOut, path := map[string]string{}, ""
	for _, field := range strings.Split(out.Out, "\x00") {
		switch {
		case strings.HasPrefix(field, "worktree "):
			path = strings.TrimPrefix(field, "worktree ")
		case strings.HasPrefix(field, "branch "):
			checkedOut[strings.TrimPrefix(field, "branch ")] = path
		case field == "":
			path = ""
		}
	}
	return checkedOut, nil
}

// classify compares a local branch with its upstream by commit counts (left = local only, right = upstream only).
func classify(g func(...string) (gitOutput, error), local, upstream string) (string, *string) {
	out, err := g("rev-list", "--left-right", "--count", "--end-of-options", local+"..."+upstream)
	fields := strings.Fields(out.Out)
	if err != nil || out.Code != 0 || len(fields) != 2 {
		return ResultDiverged, ptr("the branch cannot be compared with its upstream")
	}
	ahead, errA := strconv.Atoi(fields[0])
	behind, errB := strconv.Atoi(fields[1])
	switch {
	case errA != nil || errB != nil:
		return ResultDiverged, ptr("the branch cannot be compared with its upstream")
	case ahead == 0 && behind > 0:
		return ResultFastForward, nil
	case ahead > 0 && behind == 0:
		return ResultAhead, nil
	default:
		return ResultDiverged, ptr("local and upstream both have new commits; stw sync never merges")
	}
}

// hasTrackedChanges reports staged or unstaged changes of tracked files; untracked files do not count.
// A worktree that cannot be read counts as dirty so that nothing is ever changed in it.
func hasTrackedChanges(inTree func(string, ...string) (gitOutput, error), worktree string) bool {
	out, err := inTree(worktree, "status", "--porcelain=v1", "-z", "--untracked-files=no")
	return err != nil || out.Code != 0 || out.Out != ""
}

// checkRefspecs refuses a remote whose configured fetch refspecs would write anywhere but
// refs/remotes/REMOTE/, because the plan must change no local branch or tag.
func checkRefspecs(g func(...string) (gitOutput, error), remote, prefix string) error {
	out, err := g("config", "--get-all", "remote."+remote+".fetch")
	if err != nil || out.Code > 1 {
		return errors.New("cannot read the fetch refspecs of the remote")
	}
	for _, spec := range strings.Split(out.Out, "\n") {
		spec = strings.TrimPrefix(spec, "+")
		if spec == "" || strings.HasPrefix(spec, "^") {
			continue
		}
		if _, dst, ok := strings.Cut(spec, ":"); ok && dst != "" && !strings.HasPrefix(dst, prefix) {
			return fmt.Errorf("the fetch refspec %q of remote %s writes outside %s", spec, remote, prefix+"*")
		}
	}
	return nil
}
