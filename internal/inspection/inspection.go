// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Inspect reads native identity only, without modifying repository state.
func Inspect(ctx context.Context, path string) Result { return inspect(ctx, path, runGit) }
func inspect(ctx context.Context, path string, run runner) (result Result) {
	defer func() {
		if ctx.Err() != nil {
			result = result.fail("canceled", "inspection canceled")
		}
	}()
	r := Result{SchemaVersion: 1, Status: "error", Backend: "unknown", Evidence: []Evidence{}, Diagnostics: []Diagnostic{}}
	if !utf8.ValidString(path) {
		return r.fail("invalid-path", "inspection path is not valid UTF-8")
	}
	if path == "" {
		path = "."
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return r.fail("invalid-path", "cannot resolve inspection path")
	}
	if !utf8.ValidString(abs) {
		return r.fail("invalid-path", "absolute inspection path is not valid UTF-8")
	}
	r.InputPath = abs
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return r.fail("invalid-path", "cannot access inspection path")
	}
	if !utf8.ValidString(abs) {
		return r.fail("invalid-path", "resolved inspection path is not valid UTF-8")
	}
	r.InputPath = abs
	st, err := os.Stat(abs)
	if err != nil || !st.IsDir() {
		return r.fail("invalid-path", "inspection path must be an accessible directory")
	}
	if ctx.Err() != nil {
		return r.fail("canceled", "inspection canceled")
	}
	gitBoundary, detectErr := ancestors(abs, &r)
	if detectErr != nil {
		err = detectErr
		return r.fail(err.Error(), "cannot inspect backend metadata")
	}
	jj := len(r.Evidence) > 0
	if jj {
		jjCtx, cancel := context.WithTimeout(ctx, jjTimeout)
		defer cancel()
		if err := detectJJGitButler(jjCtx, abs, &r); err != nil {
			r.SchemaVersion = 2
			r.Backend = "jj"
			if jjCtx.Err() != nil {
				return r.fail("jj-timeout", "jj inspection exceeded its ten-second deadline")
			}
			return r.fail("invalid-metadata", "cannot safely inspect colocated ownership metadata")
		}
		if managed(&r) && r.Backend == "ambiguous" {
			return r
		}
		r.SchemaVersion = 2
		r.Backend = "jj"
		r.Status = "error"
		r.Diagnostics = []Diagnostic{}
		return inspectJJ(jjCtx, abs, r)
	}
	version, code, err := run(ctx, abs, "version")
	if err != nil {
		if ctx.Err() != nil {
			return r.fail("canceled", "inspection canceled")
		}
		if jj {
			managed(&r)
			return r
		}
		if errors.Is(err, exec.ErrNotFound) {
			return r.fail("git-unavailable", "Git executable is unavailable")
		}
		return r.fail("git-failed", "cannot execute Git identity inspection")
	}
	fields := strings.Fields(version)
	if code != 0 || len(fields) < 3 || !supportedVersion(fields[2]) {
		return r.fail("git-failed", "inspection requires Git 2.46 or newer")
	}
	gitdir, code, err := run(ctx, abs, "rev-parse", "--path-format=absolute", "--absolute-git-dir")
	if err != nil || code != 0 {
		if ctx.Err() != nil {
			return r.fail("canceled", "inspection canceled")
		}
		if jj && code == 128 && !gitBoundary {
			managed(&r)
			return r
		}
		if gitBoundary {
			return r.fail("git-failed", "Git repository identity discovery failed (check metadata and ownership)")
		}
		return r.fail("not-repository", "path is not an accessible Git repository")
	}
	repo := &Repository{GitDir: terminated(gitdir)}
	query := func(args ...string) (string, error) {
		v, c, e := run(ctx, abs, args...)
		if e != nil || c != 0 {
			return "", fmt.Errorf("git-failed")
		}
		return terminated(v), nil
	}
	repo.CommonDir, err = query("rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return r.fail("git-failed", "cannot read shared Git identity")
	}
	bare, e := query("rev-parse", "--is-bare-repository")
	if e != nil || (bare != "true" && bare != "false") {
		return r.fail("invalid-metadata", "cannot determine repository type")
	}
	repo.Bare = bare == "true"
	if !repo.Bare {
		wt, e := query("rev-parse", "--path-format=absolute", "--show-toplevel")
		if e != nil {
			return r.fail("git-failed", "cannot discover working tree")
		}
		repo.Worktree = &wt
	}
	repo.LinkedWorktree = repo.GitDir != repo.CommonDir
	repo.ObjectFormat, err = query("rev-parse", "--show-object-format")
	if err != nil || (repo.ObjectFormat != "sha1" && repo.ObjectFormat != "sha256") {
		return r.fail("invalid-metadata", "unsupported repository object format")
	}
	for _, v := range []string{repo.GitDir, repo.CommonDir} {
		if !filepath.IsAbs(v) || !utf8.ValidString(v) {
			return r.fail("invalid-metadata", "invalid repository metadata path")
		}
	}
	if repo.Worktree != nil && (!filepath.IsAbs(*repo.Worktree) || !utf8.ValidString(*repo.Worktree)) {
		return r.fail("invalid-metadata", "invalid working tree path")
	}
	if err = detectGitButler(ctx, run, abs, repo, &r); err != nil {
		if ctx.Err() != nil {
			return r.fail("canceled", "inspection canceled")
		}
		return r.fail(err.Error(), "cannot inspect Git backend metadata")
	}
	if managed(&r) {
		return r
	}
	r.Backend = "git"
	first, err := readHead(ctx, run, abs, repo.ObjectFormat)
	if err != nil {
		if ctx.Err() != nil {
			return r.fail("canceled", "inspection canceled")
		}
		return r.fail("invalid-metadata", "cannot read valid Git HEAD identity")
	}
	second, err := readHead(ctx, run, abs, repo.ObjectFormat)
	if err != nil || !reflect.DeepEqual(first, second) {
		if ctx.Err() != nil {
			return r.fail("canceled", "inspection canceled")
		}
		return r.fail("changed-repository", "Git HEAD changed during inspection")
	}
	if ctx.Err() != nil {
		return r.fail("canceled", "inspection canceled")
	}
	repo.Head = first
	r.Repository = repo
	r.Status = "ok"
	return r
}
func supportedVersion(v string) bool {
	p := strings.Split(v, ".")
	if len(p) < 2 {
		return false
	}
	major, e := strconv.Atoi(p[0])
	minor, f := strconv.Atoi(p[1])
	return e == nil && f == nil && (major > 2 || (major == 2 && minor >= 46))
}
func readHead(ctx context.Context, run runner, dir, format string) (Head, error) {
	ref, code, err := run(ctx, dir, "symbolic-ref", "--quiet", "HEAD")
	if err != nil || code > 1 {
		return Head{}, fmt.Errorf("invalid HEAD")
	}
	h := Head{State: "detached"}
	if code == 0 {
		ref = terminated(ref)
		if !utf8.ValidString(ref) || !strings.HasPrefix(ref, "refs/heads/") || strings.ContainsAny(ref, "\n\r\x00") {
			return h, fmt.Errorf("invalid branch ref")
		}
		h.Ref = &ref
		h.State = "attached"
		_, exists, e := run(ctx, dir, "show-ref", "--exists", ref)
		if e != nil {
			return h, e
		}
		if exists == 2 {
			h.State = "unborn"
			return h, nil
		}
		if exists != 0 {
			return h, fmt.Errorf("invalid branch ref")
		}
	}
	oid, c, e := run(ctx, dir, "rev-parse", "--verify", "HEAD")
	if e != nil || c != 0 {
		return h, fmt.Errorf("invalid HEAD object")
	}
	commit, c, e := run(ctx, dir, "rev-parse", "--verify", "HEAD^{commit}")
	oid = terminated(oid)
	if e != nil || c != 0 || oid != terminated(commit) {
		return h, fmt.Errorf("HEAD is not a commit")
	}
	n := 40
	if format == "sha256" {
		n = 64
	}
	if len(oid) != n || strings.Trim(oid, "0123456789abcdef") != "" {
		return h, fmt.Errorf("invalid object ID")
	}
	h.OID = &oid
	return h, nil
}
