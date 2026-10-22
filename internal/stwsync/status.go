// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/wstein/sourcesteward/internal/inspection"
)

// gitConcurrency bounds the Git processes a status run has in flight at once.
const gitConcurrency = 16

// StatusOptions selects one status run. Fetch is the only option that changes anything (remote-tracking refs).
type StatusOptions struct {
	Path      string
	Fetch     bool
	Worktrees bool
}

// StatusEcho echoes the options of the run.
type StatusEcho struct {
	Fetch     bool `json:"fetch"`
	Worktrees bool `json:"worktrees"`
}

// Changes counts the files of the main worktree by kind; a file changed in both the index and the
// worktree counts as staged and as unstaged, an unmerged file only as a conflict.
type Changes struct {
	Staged    int `json:"staged"`
	Unstaged  int `json:"unstaged"`
	Untracked int `json:"untracked"`
	Conflicts int `json:"conflicts"`
}

// BranchCounts compares the local branches with the default branch of the upstream remote. Integrated and
// NotIntegrated are null when no default branch is known (Base null); the default branch itself is in neither.
type BranchCounts struct {
	Local         int     `json:"local"`
	Base          *string `json:"base"`
	Integrated    *int    `json:"integrated"`
	NotIntegrated *int    `json:"not_integrated"`
}

// WorktreeInfo is one linked worktree. Locked carries the lock reason (empty when none was given).
type WorktreeInfo struct {
	Path     string  `json:"path"`
	Branch   *string `json:"branch"`
	Detached bool    `json:"detached"`
	Dirty    bool    `json:"dirty"`
	Missing  bool    `json:"missing"`
	Locked   *string `json:"locked"`
	Prunable bool    `json:"prunable"`
}

// WorktreeCounts counts the linked worktrees (the main one excluded); List is filled only with Worktrees.
type WorktreeCounts struct {
	Linked   int            `json:"linked"`
	Dirty    int            `json:"dirty"`
	Locked   int            `json:"locked"`
	Prunable int            `json:"prunable"`
	Missing  int            `json:"missing"`
	List     []WorktreeInfo `json:"list"`
}

// Gitlink compares the commit the superproject records for a submodule with the commit checked out.
// State is ok, drift or uninitialized; Relation (drift only) is where the checked-out commit stands
// relative to the recorded one: ahead, behind, diverged, unrelated (no common ancestor) or unknown.
type Gitlink struct {
	Recorded   *string `json:"recorded"`
	CheckedOut *string `json:"checked_out"`
	State      string  `json:"state"`
	Relation   *string `json:"relation"`
}

// RepoStatus is the status of the main worktree of one repository of the workspace.
type RepoStatus struct {
	Name         string         `json:"name"`
	Path         string         `json:"path"`
	Kind         string         `json:"kind"`
	State        string         `json:"state"`
	Branch       *string        `json:"branch"`
	Detached     bool           `json:"detached"`
	Head         *string        `json:"head"`
	Upstream     *string        `json:"upstream"`
	UpstreamGone bool           `json:"upstream_gone"`
	Ahead        *int           `json:"ahead"`
	Behind       *int           `json:"behind"`
	Changes      Changes        `json:"changes"`
	Operation    *string        `json:"operation"`
	Stash        int            `json:"stash"`
	Branches     BranchCounts   `json:"branches"`
	Worktrees    WorktreeCounts `json:"worktrees"`
	Gitlink      *Gitlink       `json:"gitlink"`
	FetchError   *string        `json:"fetch_error"`
	Attention    bool           `json:"attention"`
	Reasons      []string       `json:"reasons"`
	Error        *Failure       `json:"error"`
}

// StatusSummary counts the repositories and those with a non-empty attention list.
type StatusSummary struct {
	Total     int `json:"total"`
	Attention int `json:"attention"`
	Errors    int `json:"errors"`
}

// StatusResult is the one model behind the human and the JSON output.
type StatusResult struct {
	SchemaVersion int           `json:"schema_version"`
	Command       string        `json:"command"`
	Options       StatusEcho    `json:"options"`
	Root          *string       `json:"root"`
	Note          *string       `json:"note"`
	Repos         []RepoStatus  `json:"repos"`
	Summary       StatusSummary `json:"summary"`
	Error         *Failure      `json:"error"`
}

// ExitCode is 1 when the run failed or a repository could not be read, else 0: attention is not a failure.
func (r StatusResult) ExitCode() int {
	if r.Error != nil || r.Summary.Errors > 0 {
		return 1
	}
	return 0
}

func (r StatusResult) fail(code, message string) StatusResult {
	r.Error = &Failure{Code: code, Message: oneLine(message)}
	return r
}

// Status reports the state of the main worktree of the workspace root and of every submodule, found from
// any directory inside the workspace, in the form of git status, git worktree list and git branch -vv. It
// changes nothing; only Fetch updates remote-tracking refs. Git only.
func Status(ctx context.Context, o StatusOptions) StatusResult {
	r := StatusResult{SchemaVersion: 1, Command: "status", Options: StatusEcho{Fetch: o.Fetch, Worktrees: o.Worktrees}, Repos: []RepoStatus{}}
	if err := ValidateOptions(Options{Path: o.Path}); err != nil {
		return r.fail("status-invalid-options", err.Error())
	}
	bin, err := exec.LookPath("git")
	if err != nil {
		return r.fail("status-git-unavailable", "the Git executable is unavailable")
	}
	if bin, err = filepath.Abs(bin); err != nil {
		return r.fail("status-git-unavailable", "the Git executable is unavailable")
	}
	id := inspection.Inspect(ctx, o.Path)
	if len(id.Evidence) > 0 || id.Backend == "jj" {
		return r.fail("status-unsupported-backend", "jj-owned paths are not supported by stw status; use jj status (Git only)")
	}
	if id.Status != "ok" || id.Backend != "git" || id.Repository == nil {
		return r.fail("status-not-a-repository", "stw status needs a Git repository: "+id.Error())
	}
	root, linked, err := (env{bin, id.InputPath}).workspaceRoot(ctx)
	if err != nil {
		return r.fail("status-read-failed", err.Error())
	}
	r.Root = ptr(root.dir)
	if linked != "" {
		r.Note = ptr("started inside the linked worktree " + oneLine(linked) + "; the main worktrees of the workspace are shown")
	}
	var specs []statusSpec
	if err := walkStatus(ctx, root, ".", "root", nil, &specs); err != nil {
		return r.fail("status-limit", err.Error())
	}
	s := &statusRun{opts: o, slots: make(chan struct{}, gitConcurrency)}
	r.Repos = make([]RepoStatus, len(specs))
	var wg sync.WaitGroup
	for i := range specs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Repos[i] = s.repo(ctx, specs[i])
		}()
	}
	wg.Wait()
	for _, rp := range r.Repos {
		r.Summary.Total++
		if rp.Error != nil {
			r.Summary.Errors++
		}
		if rp.Attention {
			r.Summary.Attention++
		}
	}
	return r
}

type statusSpec struct {
	e            env
	name, kind   string
	gitlink      *Gitlink
	uninitalized bool
}

// walkStatus lists the repository at e and, depth-first, its initialized submodules in the order stw sync uses.
func walkStatus(ctx context.Context, e env, name, kind string, link *Gitlink, out *[]statusSpec) error {
	spec := statusSpec{e: e, name: name, kind: kind, gitlink: link}
	if _, err := os.Lstat(filepath.Join(e.dir, ".git")); err != nil {
		spec.uninitalized = true
	}
	*out = append(*out, spec)
	if len(*out) > repoLimit {
		return errors.New("the workspace has more than " + strconv.Itoa(repoLimit) + " repositories")
	}
	if spec.uninitalized {
		return nil
	}
	subs, err := e.submodules(ctx)
	if err != nil {
		return nil // reported by the repository's own read
	}
	for _, p := range subs {
		child := e.withDir(filepath.Join(e.dir, filepath.FromSlash(p)))
		childName := name + "/" + p
		if name == "." {
			childName = p
		}
		gl := &Gitlink{State: "ok"}
		if rec := e.revParse(ctx, e.dir, "HEAD:"+p); rec != "" {
			gl.Recorded = ptr(rec)
		}
		if _, err := os.Lstat(filepath.Join(child.dir, ".git")); err != nil {
			gl.State = "uninitialized"
		} else if co := e.revParse(ctx, child.dir, "HEAD"); co != "" {
			gl.CheckedOut = ptr(co)
			if gl.Recorded != nil && *gl.Recorded != co {
				gl.State = "drift"
				gl.Relation = ptr(gitlinkRelation(ctx, child, *gl.Recorded, co))
			}
		}
		if err := walkStatus(ctx, child, childName, "submodule", gl, out); err != nil {
			return err
		}
	}
	return nil
}

// gitlinkRelation says where the checked-out commit stands relative to the recorded one.
func gitlinkRelation(ctx context.Context, e env, recorded, checked string) string {
	is := func(a, b string) (bool, bool) {
		out, err := e.g(ctx, "merge-base", "--is-ancestor", a, b)
		return out.Code == 0 && err == nil, err == nil && out.Code <= 1
	}
	if yes, ok := is(recorded, checked); ok && yes {
		return "ahead"
	} else if !ok {
		return "unknown"
	}
	if yes, ok := is(checked, recorded); ok && yes {
		return "behind"
	} else if !ok {
		return "unknown"
	}
	// no common ancestor at all: unrelated histories, not a diverged branch
	if out, err := e.g(ctx, "merge-base", recorded, checked); err == nil && out.Code == 1 {
		return "unrelated"
	}
	return "diverged"
}

type statusRun struct {
	opts  StatusOptions
	slots chan struct{}
}

// g runs one Git command, at most gitConcurrency at a time across the run.
func (s *statusRun) g(ctx context.Context, e env, dir string, args ...string) (gitOutput, error) {
	s.slots <- struct{}{}
	defer func() { <-s.slots }()
	return e.in(ctx, dir, args...)
}

func (s *statusRun) repo(ctx context.Context, sp statusSpec) RepoStatus {
	rp := RepoStatus{Name: sp.name, Path: sp.e.dir, Kind: sp.kind, State: "ok", Gitlink: sp.gitlink, Reasons: []string{}, Worktrees: WorktreeCounts{List: []WorktreeInfo{}}}
	if sp.uninitalized {
		rp.State = "uninitialized"
		return rp
	}
	fail := func(what string) RepoStatus {
		rp.State = "error"
		rp.Error = &Failure{Code: "status-read-failed", Message: "cannot read " + what}
		rp.Reasons, rp.Attention = append(rp.Reasons, errorReason), true
		return rp
	}
	e, dir := sp.e, sp.e.dir
	if s.opts.Fetch {
		out, err := s.g(ctx, e, dir, "fetch", "--no-tags", "--no-write-fetch-head", "--no-recurse-submodules", "--no-prune")
		if err != nil || out.Code != 0 {
			rp.FetchError = ptr(oneLine(out.Diagnostic))
			if *rp.FetchError == "" {
				rp.FetchError = ptr("git fetch failed")
			}
		}
	}
	out, err := s.g(ctx, e, dir, "status", "--porcelain=v2", "--branch", "--show-stash", "-z")
	if err != nil || out.Code != 0 {
		return fail("the repository status")
	}
	parseStatus(out.Out, &rp)
	if reason, _ := e.operation(ctx, dir); reason != "" {
		rp.Operation = ptr(operationName(reason))
	}
	if !s.branches(ctx, e, &rp) {
		return fail("the local branches")
	}
	if !s.worktrees(ctx, e, &rp) {
		return fail("the worktrees")
	}
	rp.Reasons = attention(rp)
	rp.Attention = len(rp.Reasons) > 0
	return rp
}

func operationName(reason string) string {
	if !strings.HasSuffix(reason, " is in progress in the worktree") {
		return "unreadable"
	}
	return strings.Fields(reason)[1]
}

// parseStatus reads `git status --porcelain=v2 --branch --show-stash -z`.
func parseStatus(raw string, rp *RepoStatus) {
	fields := strings.Split(raw, "\x00")
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		switch {
		case strings.HasPrefix(f, "# branch.oid "):
			if oid := strings.TrimPrefix(f, "# branch.oid "); oid != "(initial)" {
				rp.Head = ptr(oid)
			}
		case strings.HasPrefix(f, "# branch.head "):
			if name := strings.TrimPrefix(f, "# branch.head "); name == "(detached)" {
				rp.Detached = true
			} else {
				rp.Branch = ptr(name)
			}
		case strings.HasPrefix(f, "# branch.upstream "):
			rp.Upstream = ptr(strings.TrimPrefix(f, "# branch.upstream "))
		case strings.HasPrefix(f, "# branch.ab "):
			var a, b int
			ab := strings.Fields(strings.TrimPrefix(f, "# branch.ab "))
			if len(ab) == 2 {
				a, _ = strconv.Atoi(strings.TrimPrefix(ab[0], "+"))
				b, _ = strconv.Atoi(strings.TrimPrefix(ab[1], "-"))
			}
			rp.Ahead, rp.Behind = &a, &b
		case strings.HasPrefix(f, "# stash "):
			rp.Stash, _ = strconv.Atoi(strings.TrimPrefix(f, "# stash "))
		case strings.HasPrefix(f, "1 ") || strings.HasPrefix(f, "2 "):
			// a changed submodule (S... flag) is a gitlink change; the gitlink check reports it, not the file counts
			if len(f) > 4 && !(f[0] == '1' && len(f) > 8 && f[5] == 'S') && !(f[0] == '2' && len(f) > 8 && f[5] == 'S') {
				if f[2] != '.' {
					rp.Changes.Staged++
				}
				if f[3] != '.' {
					rp.Changes.Unstaged++
				}
			}
			if f[0] == '2' {
				i++ // the original path of a rename follows as its own field
			}
		case strings.HasPrefix(f, "u "):
			rp.Changes.Conflicts++
		case strings.HasPrefix(f, "? "):
			rp.Changes.Untracked++
		}
	}
	rp.UpstreamGone = rp.Upstream != nil && rp.Ahead == nil
}

// branches counts the local branches and how many are merged into the default branch of the upstream remote.
func (s *statusRun) branches(ctx context.Context, e env, rp *RepoStatus) bool {
	out, err := s.g(ctx, e, e.dir, "for-each-ref", "--format=%(refname:short)", "refs/heads/")
	if err != nil || out.Code != 0 {
		return false
	}
	local := splitLines(out.Out)
	rp.Branches.Local = len(local)
	remote := "origin"
	if rp.Upstream != nil {
		if r, _, ok := strings.Cut(*rp.Upstream, "/"); ok && remoteName.MatchString(r) {
			remote = r
		}
	}
	if !remoteName.MatchString(remote) {
		return true
	}
	base := ""
	if sym, err := s.g(ctx, e, e.dir, "symbolic-ref", "-q", "refs/remotes/"+remote+"/HEAD"); err == nil && sym.Code == 0 {
		base = strings.TrimSpace(sym.Out)
	}
	for _, name := range []string{"main", "master"} {
		if base != "" {
			break
		}
		if v, err := s.g(ctx, e, e.dir, "rev-parse", "--verify", "-q", "refs/remotes/"+remote+"/"+name); err == nil && v.Code == 0 {
			base = "refs/remotes/" + remote + "/" + name
		}
	}
	if !strings.HasPrefix(base, "refs/remotes/"+remote+"/") {
		return true
	}
	merged, err := s.g(ctx, e, e.dir, "for-each-ref", "--merged="+base, "--format=%(refname:short)", "refs/heads/")
	if err != nil || merged.Code != 0 {
		return true
	}
	defaultName := strings.TrimPrefix(base, "refs/remotes/"+remote+"/")
	isMerged := map[string]bool{}
	for _, n := range splitLines(merged.Out) {
		isMerged[n] = true
	}
	integrated, not := 0, 0
	for _, n := range local {
		switch {
		case n == defaultName:
		case isMerged[n]:
			integrated++
		default:
			not++
		}
	}
	rp.Branches.Base = ptr(strings.TrimPrefix(base, "refs/remotes/"))
	rp.Branches.Integrated, rp.Branches.NotIntegrated = &integrated, &not
	return true
}

func splitLines(s string) []string {
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// worktrees reads `git worktree list --porcelain -z` and counts the linked ones; a dirty check is one
// `git status` per present linked worktree, run concurrently.
func (s *statusRun) worktrees(ctx context.Context, e env, rp *RepoStatus) bool {
	out, err := s.g(ctx, e, e.dir, "worktree", "list", "--porcelain", "-z")
	if err != nil || out.Code != 0 {
		return false
	}
	var list []WorktreeInfo
	var cur *WorktreeInfo
	first := true
	flush := func() {
		if cur != nil && !first {
			list = append(list, *cur)
		}
		if cur != nil {
			first = false
		}
		cur = nil
	}
	for _, f := range strings.Split(out.Out, "\x00") {
		switch {
		case strings.HasPrefix(f, "worktree "):
			flush()
			cur = &WorktreeInfo{Path: strings.TrimPrefix(f, "worktree ")}
		case cur == nil:
		case strings.HasPrefix(f, "branch "):
			cur.Branch = ptr(strings.TrimPrefix(strings.TrimPrefix(f, "branch "), "refs/heads/"))
		case f == "detached":
			cur.Detached = true
		case f == "locked" || strings.HasPrefix(f, "locked "):
			cur.Locked = ptr(strings.TrimPrefix(strings.TrimPrefix(f, "locked"), " "))
		case f == "prunable" || strings.HasPrefix(f, "prunable "):
			cur.Prunable = true
		}
	}
	flush()
	var wg sync.WaitGroup
	for i := range list {
		if _, err := os.Stat(list[i].Path); err != nil {
			list[i].Missing = true // a locked worktree whose directory is gone: no dirty claim
			continue
		}
		if list[i].Prunable {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			o, err := s.g(ctx, e, list[i].Path, "status", "--porcelain=v1", "-z")
			list[i].Dirty = err != nil || o.Code != 0 || o.Out != ""
		}()
	}
	wg.Wait()
	rp.Worktrees.Linked = len(list)
	for _, w := range list {
		if w.Dirty {
			rp.Worktrees.Dirty++
		}
		if w.Locked != nil {
			rp.Worktrees.Locked++
		}
		if w.Prunable {
			rp.Worktrees.Prunable++
		}
		if w.Missing {
			rp.Worktrees.Missing++
		}
	}
	if s.opts.Worktrees {
		rp.Worktrees.List = append(rp.Worktrees.List, list...)
	}
	return true
}

// errorReason is the only reason of a repository that could not be read.
const errorReason = "error"

// attentionRules is the one list of attention reasons, in the order of the rule: a repository needs attention
// when any of these facts holds. Everything else (commits ahead, a gone upstream, stashes, branches not
// integrated, linked worktrees, a failed fetch, a gitlink whose checked-out commit is ahead of the recorded one,
// an uninitialized submodule) is shown but never counts.
var attentionRules = []struct {
	reason string
	holds  func(RepoStatus) bool
}{
	{errorReason, func(rp RepoStatus) bool { return rp.State == "error" }},
	{"operation", func(rp RepoStatus) bool { return rp.Operation != nil }},
	{"conflicts", func(rp RepoStatus) bool { return rp.Changes.Conflicts > 0 }},
	{"staged", func(rp RepoStatus) bool { return rp.Changes.Staged > 0 }},
	{"unstaged", func(rp RepoStatus) bool { return rp.Changes.Unstaged > 0 }},
	{"untracked", func(rp RepoStatus) bool { return rp.Changes.Untracked > 0 }},
	{"behind", func(rp RepoStatus) bool { return rp.Behind != nil && *rp.Behind > 0 }},
	{"diverged", func(rp RepoStatus) bool {
		return rp.Ahead != nil && *rp.Ahead > 0 && rp.Behind != nil && *rp.Behind > 0
	}},
	{"no-upstream", func(rp RepoStatus) bool { return rp.Branch != nil && rp.Upstream == nil }},
	{"detached", func(rp RepoStatus) bool { return rp.Detached }},
	{"gitlink-drift", func(rp RepoStatus) bool {
		return rp.Gitlink != nil && rp.Gitlink.State == "drift" && (rp.Gitlink.Relation == nil || *rp.Gitlink.Relation != "ahead")
	}},
}

// AttentionReasons lists every reason a repository can carry in its `reasons`, in the order of the rule.
func AttentionReasons() []string {
	reasons := make([]string, len(attentionRules))
	for i, rule := range attentionRules {
		reasons[i] = rule.reason
	}
	return reasons
}

// attention returns the reasons that hold for rp, in the order of attentionRules.
func attention(rp RepoStatus) []string {
	reasons := []string{}
	for _, rule := range attentionRules {
		if rule.holds(rp) {
			reasons = append(reasons, rule.reason)
		}
	}
	return reasons
}
