// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// This file holds the fixed, read-only Git invocations of the release commands. They run under
// an isolation prefix (DD18): the user's and the system's Git configuration are ignored, replace
// objects are off, nothing is locked or prompted. Only `cat-file` and `ls-tree` are used, which
// read objects and run no hook, filter, textconv or fsmonitor.

const (
	gitListLimit     = 1 << 20  // bytes of the ls-tree listing of .stw
	gitTreeListLimit = 16 << 20 // bytes of the ls-tree listing of the parent directories of probed paths
	gitBatchLimit    = 8 << 20  // bytes of a cat-file --batch-check reply (a missing path echoes its name)
	gitDeadline      = 30 * time.Second
	maxTreeProbes    = 20000
)

// gitEnvironment drops every GIT_ variable and pins the isolating ones.
func gitEnvironment() []string {
	env := make([]string, 0, len(os.Environ())+8)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(strings.ToUpper(key), "GIT_") {
			env = append(env, entry)
		}
	}
	return append(env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GIT_NO_LAZY_FETCH=1", "GIT_PAGER=cat", "GIT_ATTR_NOSYSTEM=1", "GIT_LITERAL_PATHSPECS=1")
}

func gitCommand(ctx context.Context, root string, args ...string) *exec.Cmd {
	prefix := []string{"--no-replace-objects", "--no-optional-locks", "-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null"}
	cmd := exec.CommandContext(ctx, "git", append(prefix, args...)...)
	cmd.Dir = root
	cmd.Env = gitEnvironment()
	cmd.WaitDelay = time.Second
	return cmd
}

// errGitFailed is a Git failure that is neither a missing object nor cancellation.
var errGitFailed = errors.New("git failed")

// gitOutput runs a read and returns its standard output, capped at limit bytes (truncated is
// reported, the process stopped). The exit status is returned; a start failure is an error.
func gitOutput(ctx context.Context, root string, limit int, stdin string, args ...string) (out []byte, truncated bool, status int, err error) {
	ctx, cancel := context.WithTimeout(ctx, gitDeadline)
	defer cancel()
	cmd := gitCommand(ctx, root, args...)
	cmd.Stdin = strings.NewReader(stdin)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, false, -1, err
	}
	if err := cmd.Start(); err != nil {
		return nil, false, -1, err
	}
	out, readErr := io.ReadAll(io.LimitReader(pipe, int64(limit)+1))
	if len(out) > limit {
		out, truncated = out[:limit], true
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return nil, false, -1, ctx.Err()
	}
	if readErr != nil {
		return nil, false, -1, readErr
	}
	var exit *exec.ExitError
	switch {
	case truncated:
		return out, true, 0, nil
	case waitErr == nil:
		return out, false, 0, nil
	case errors.As(waitErr, &exit):
		return out, false, exit.ExitCode(), nil
	}
	return nil, false, -1, waitErr
}

// gitSource reads the contract files of one commit.
type gitSource struct {
	ctx    context.Context
	root   string
	commit string
}

// newGitSource checks that the commit id names a commit object of the repository.
func newGitSource(ctx context.Context, root, commit string) (*gitSource, error) {
	// Inspect the exact object, not a peeled revision: a tag id must never be
	// reported or substituted into argv as though it were the commit it targets.
	if !IsFullCommitID(commit) {
		return nil, errCommitUnresolved
	}
	out, cut, status, err := gitOutput(ctx, root, 128, commit+"\n", "cat-file", "--batch-check")
	if err != nil {
		return nil, err
	}
	if cut {
		return nil, errReleaseLimit
	}
	if status != 0 {
		return nil, fmt.Errorf("%w: cat-file --batch-check", errGitFailed)
	}
	fields := strings.Fields(string(out))
	if len(fields) == 2 && fields[0] == commit && fields[1] == "missing" {
		return nil, errCommitUnresolved
	}
	if len(fields) != 3 || fields[0] != commit {
		return nil, fmt.Errorf("%w: invalid object header", errGitFailed)
	}
	switch fields[1] {
	case "blob", "tree", "tag":
		return nil, errCommitUnresolved
	case "commit":
	default:
		return nil, fmt.Errorf("%w: invalid object type", errGitFailed)
	}
	if size, err := strconv.ParseInt(fields[2], 10, 64); err != nil || size < 0 {
		return nil, fmt.Errorf("%w: invalid object size", errGitFailed)
	}
	return &gitSource{ctx: ctx, root: root, commit: commit}, nil
}

var errCommitUnresolved = errors.New("commit-unresolved")

// StwEntries lists the .stw directory of the commit; a missing or non-directory .stw is no entries.
func (g *gitSource) StwEntries() ([]sourceEntry, bool, error) {
	kind, _, status, err := gitOutput(g.ctx, g.root, 64, "", "cat-file", "-t", g.commit+":.stw")
	if err != nil {
		return nil, false, err
	}
	if status != 0 || strings.TrimSpace(string(kind)) != "tree" {
		return nil, false, nil
	}
	listing, truncated, status, err := gitOutput(g.ctx, g.root, gitListLimit, "", "ls-tree", "-z", g.commit+":.stw")
	if err != nil {
		return nil, false, err
	}
	if status != 0 {
		return nil, false, fmt.Errorf("%w: ls-tree", errGitFailed)
	}
	var entries []sourceEntry
	for _, record := range bytes.Split(listing, []byte{0}) {
		meta, name, ok := bytes.Cut(record, []byte{'\t'})
		if !ok {
			continue
		}
		fields := strings.Fields(string(meta))
		if len(fields) != 3 {
			continue
		}
		entry := sourceEntry{Name: string(name), id: fields[2], Kind: "other"}
		switch {
		case fields[1] == "tree":
			entry.Kind = "dir"
		case fields[0] == "100644" || fields[0] == "100755":
			entry.Kind = "file"
		case fields[0] == "120000":
			entry.Kind = "symlink"
		}
		entries = append(entries, entry)
	}
	return entries, truncated, nil
}

// Read returns a blob, at most limit bytes; tooLarge reports a longer blob.
func (g *gitSource) Read(e sourceEntry, limit int) ([]byte, bool, error) {
	data, truncated, status, err := gitOutput(g.ctx, g.root, limit, "", "cat-file", "blob", e.id)
	if err != nil {
		return nil, false, err
	}
	if status != 0 {
		return nil, false, fmt.Errorf("%w: cat-file", errGitFailed)
	}
	return data, truncated, nil
}

// Entries reports the entries at exactly the given paths of the commit's tree: `ls-tree -z` lists
// the modes, and one `cat-file --batch` reads the targets of symlinks. A path below a symlink is
// absent here; the caller resolves symlinks component by component.
func (g *gitSource) Entries(paths []string) (map[string]TreeEntry, error) {
	if len(paths) > maxTreeProbes {
		return nil, fmt.Errorf("%w: more than %d paths to probe", errGitFailed, maxTreeProbes)
	}
	out := map[string]TreeEntry{}
	links := map[string]string{} // path -> blob id
	wanted := map[string]bool{}
	parents := map[string]bool{}
	for _, p := range paths {
		if strings.ContainsAny(p, "\n\x00") {
			return nil, fmt.Errorf("%w: unprobeable path", errGitFailed)
		}
		wanted[p] = true
		parent := ""
		if i := strings.LastIndexByte(p, '/'); i >= 0 {
			parent = p[:i+1]
		}
		parents[parent] = true
	}
	// Listing the parent directories gives every entry of each; a directory path listed on its own
	// next to a path below it would not be printed by ls-tree, so entries are read per parent.
	var dirs []string
	for d := range parents {
		if d == "" {
			// The root cannot be combined with pathspecs; list it by itself.
			if err := g.listInto([]string{"ls-tree", "-z", g.commit}, wanted, out, links); err != nil {
				return nil, err
			}
			continue
		}
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	for start := 0; start < len(dirs); start += 100 {
		args := append([]string{"ls-tree", "-z", g.commit, "--"}, dirs[start:min(start+100, len(dirs))]...)
		if err := g.listInto(args, wanted, out, links); err != nil {
			return nil, err
		}
	}
	if len(links) == 0 {
		return out, nil
	}
	names := make([]string, 0, len(links))
	for p := range links {
		names = append(names, p)
	}
	sort.Strings(names)
	var stdin strings.Builder
	for _, p := range names {
		stdin.WriteString(links[p] + "\n")
	}
	reply, truncated, status, err := gitOutput(g.ctx, g.root, gitBatchLimit, stdin.String(), "cat-file", "--batch")
	if err != nil {
		return nil, err
	}
	if truncated || status != 0 {
		return nil, fmt.Errorf("%w: cat-file --batch", errGitFailed)
	}
	for _, p := range names {
		header, rest, ok := bytes.Cut(reply, []byte{'\n'})
		fields := strings.Fields(string(header))
		if !ok || len(fields) != 3 || fields[1] != "blob" {
			return nil, fmt.Errorf("%w: cat-file --batch reply", errGitFailed)
		}
		size, convErr := strconv.Atoi(fields[2])
		if convErr != nil || size < 0 || len(rest) < size+1 {
			return nil, fmt.Errorf("%w: cat-file --batch size", errGitFailed)
		}
		out[p] = TreeEntry{Kind: "symlink", Target: string(rest[:size])}
		reply = rest[size+1:]
	}
	return out, nil
}

// listInto runs one ls-tree and records the requested entries it prints.
func (g *gitSource) listInto(args []string, wanted map[string]bool, out map[string]TreeEntry, links map[string]string) error {
	listing, truncated, status, err := gitOutput(g.ctx, g.root, gitTreeListLimit, "", args...)
	if err != nil {
		return err
	}
	if truncated || status != 0 {
		return fmt.Errorf("%w: ls-tree", errGitFailed)
	}
	for _, record := range bytes.Split(listing, []byte{0}) {
		meta, name, ok := bytes.Cut(record, []byte{'\t'})
		fields := strings.Fields(string(meta))
		p := string(name)
		if !ok || len(fields) != 3 || !wanted[p] {
			continue
		}
		switch {
		case fields[0] == "120000":
			links[p] = fields[2]
		case fields[1] == "tree" || fields[1] == "commit":
			out[p] = TreeEntry{Kind: "dir"}
		default:
			out[p] = TreeEntry{Kind: "file"}
		}
	}
	return nil
}

// gitToplevel returns the root of the working tree that contains dir.
func gitToplevel(ctx context.Context, dir string) (string, error) {
	out, _, status, err := gitOutput(ctx, dir, 4096, "", "rev-parse", "--path-format=absolute", "--show-toplevel")
	if err != nil {
		return "", err
	}
	if status != 0 {
		return "", fmt.Errorf("%w: rev-parse --show-toplevel", errGitFailed)
	}
	return strings.TrimSuffix(string(out), "\n"), nil
}

// mainAncestry uses only the selected remote's locally recorded main. It never fetches.
func (g *gitSource) mainAncestry(remote string) (MainRef, Gate, error) {
	main := MainRef{Remote: remote}
	id, err := g.recordedMain(remote)
	if err != nil {
		return main, Gate{}, err
	}
	if id != "" {
		main.Commit = &id
	}
	notRun := func(reason string) (MainRef, Gate, error) {
		return main, gate("commit-not-on-main", "not-run", reason, "no fetch was made"), nil
	}
	out, cut, status, err := gitOutput(g.ctx, g.root, 4096, "", "rev-parse", "--path-format=absolute", "--git-path", "info/grafts")
	if err != nil || cut || status != 0 {
		return main, Gate{}, fmt.Errorf("cannot resolve grafts path: %w", errGitFailed)
	}
	_, err = os.Lstat(strings.TrimSuffix(string(out), "\n"))
	if err == nil {
		return notRun("grafts-present")
	}
	if !errors.Is(err, os.ErrNotExist) {
		return main, Gate{}, err
	}
	out, cut, status, err = gitOutput(g.ctx, g.root, 64, "", "rev-parse", "--is-shallow-repository")
	if err != nil || cut || status != 0 {
		return main, Gate{}, fmt.Errorf("cannot read shallow state: %w", errGitFailed)
	}
	if strings.TrimSpace(string(out)) == "true" {
		return notRun("shallow-clone")
	}
	if id == "" {
		return notRun("main-ref-absent")
	}
	_, cut, status, err = gitOutput(g.ctx, g.root, 64, "", "merge-base", "--is-ancestor", g.commit, id)
	if err != nil {
		return main, Gate{}, err
	}
	if cut || status > 1 {
		return main, Gate{}, errGitFailed
	}
	outcome := "pass"
	if status == 1 {
		outcome = "fail"
	}
	return main, gate("commit-not-on-main", outcome, "", "recorded locally; no fetch was made"), nil
}

// recordedMain samples the local ref even when ancestry must remain not-run.
func (g *gitSource) recordedMain(remote string) (string, error) {
	if remote == "" {
		return "", nil
	}
	out, cut, status, err := gitOutput(g.ctx, g.root, 66, "", "rev-parse", "--verify", "--quiet", "refs/remotes/"+remote+"/main^{commit}")
	if err != nil {
		return "", err
	}
	if status == 1 {
		return "", nil
	}
	id := strings.TrimSpace(string(out))
	if cut || status != 0 || !IsFullCommitID(id) {
		return "", errGitFailed
	}
	return id, nil
}

// pathEntry reads exact tree metadata without attributes, symlink traversal or checkout filters.
func (g *gitSource) pathEntry(path string) (sourceEntry, int64, error) {
	out, cut, status, err := gitOutput(g.ctx, g.root, gitTreeListLimit, "", "ls-tree", "-z", "-l", g.commit, "--", path)
	if err != nil {
		return sourceEntry{}, 0, err
	}
	if cut {
		return sourceEntry{}, 0, errReleaseLimit
	}
	if status != 0 {
		return sourceEntry{}, 0, errGitFailed
	}
	for _, rec := range bytes.Split(out, []byte{0}) {
		meta, name, ok := bytes.Cut(rec, []byte{'\t'})
		if !ok || string(name) != path {
			continue
		}
		fields := strings.Fields(string(meta))
		if len(fields) != 4 {
			return sourceEntry{}, 0, errGitFailed
		}
		e := sourceEntry{Name: path, id: fields[2], Kind: "other"}
		if fields[0] == "100644" || fields[0] == "100755" {
			e.Kind = "file"
		}
		n, _ := strconv.ParseInt(fields[3], 10, 64)
		return e, n, nil
	}
	return sourceEntry{Name: path}, 0, nil
}

func (g *gitSource) commitTime() (string, error) {
	out, cut, status, err := gitOutput(g.ctx, g.root, MaxContractBytes, "", "cat-file", "commit", g.commit)
	if err != nil {
		return "", err
	}
	if cut {
		return "", errReleaseLimit
	}
	if status != 0 {
		return "", errGitFailed
	}
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "committer ") {
			f := strings.Fields(line)
			if len(f) < 3 {
				break
			}
			epoch := f[len(f)-2]
			if _, e := strconv.ParseInt(epoch, 10, 64); e == nil {
				return epoch, nil
			}
		}
	}
	return "", fmt.Errorf("invalid commit timestamp")
}

// outputDirectory is descriptive only; plan never creates it.
func outputDirectory(root string) string { return filepath.Join(root, "dist", "release") }
