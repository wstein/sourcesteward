// SPDX-License-Identifier: EUPL-1.2

package conflicts

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	gitOutputLimit = 4 << 20
	gitTimeout     = 2 * time.Minute
)

var (
	remoteName = regexp.MustCompile(`\A[A-Za-z0-9][A-Za-z0-9._-]{0,63}\z`)
	branchName = regexp.MustCompile(`\A[A-Za-z0-9._/+-]{1,200}\z`)
	shaPattern = regexp.MustCompile(`\A[0-9a-f]{40}\z`)
)

// ValidBranch reports whether a branch name from the forge is safe to put in a refspec: a plain
// allowlist, no leading dash or slash, no "..", "//", trailing slash, dot or ".lock".
func ValidBranch(b string) bool {
	return branchName.MatchString(b) && b[0] != '-' && b[0] != '/' && !strings.Contains(b, "..") && !strings.Contains(b, "//") &&
		!strings.HasSuffix(b, "/") && !strings.HasSuffix(b, ".") && !strings.HasSuffix(b, ".lock") && !strings.Contains(b, "/.")
}

// ValidSHA reports whether s is a full lower-case SHA-1 object name.
func ValidSHA(s string) bool { return shaPattern.MatchString(s) }

// Runner is the Git of the detection, backed by the git executable in a checkout. Every call uses
// fixed argument vectors, a clean environment and no replace objects. Close removes the quarantine.
type Runner struct {
	Bin string
	Dir string

	quarantine string
	objects    string
}

// NewRunner returns a runner for the checkout or worktree at dir.
func NewRunner(dir string) *Runner { return &Runner{Bin: "git", Dir: dir} }

var _ Git = (*Runner)(nil)

type result struct {
	out  []byte
	diag string
	code int
}

type limited struct{ bytes.Buffer }

var errOutput = errors.New("git output exceeds the bound")

func (b *limited) Write(p []byte) (int, error) {
	if b.Len()+len(p) > gitOutputLimit {
		return 0, errOutput
	}
	return b.Buffer.Write(p)
}

func baseEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		switch name {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE", "GIT_PREFIX":
			continue
		}
		env = append(env, kv)
	}
	return append(env, "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
}

func (r *Runner) run(ctx context.Context, extraEnv []string, args ...string) (result, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	full := append([]string{"--no-replace-objects", "--literal-pathspecs", "-c", "core.fsmonitor=false", "-c", "gc.auto=0", "-c", "maintenance.auto=false", "-C", r.Dir}, args...)
	cmd := exec.CommandContext(ctx, r.Bin, full...)
	cmd.Env = append(baseEnv(), extraEnv...)
	var out, diag limited
	cmd.Stdout, cmd.Stderr = &out, &diag
	err := cmd.Run()
	res := result{out: out.Bytes(), diag: strings.TrimSpace(diag.String())}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		res.code = exit.ExitCode()
		return res, nil
	}
	return res, err
}

func (r *Runner) ok(ctx context.Context, what string, args ...string) ([]byte, error) {
	res, err := r.run(ctx, nil, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	if res.code != 0 {
		return nil, fmt.Errorf("%s failed (exit %d)", what, res.code)
	}
	return res.out, nil
}

// Fetch implements Git. It writes objects and the refs below NamespaceRoot only.
func (r *Runner) Fetch(ctx context.Context, remote string, pulls []int, bases []string) error {
	if !remoteName.MatchString(remote) {
		return fmt.Errorf("invalid remote name")
	}
	specs := []string{}
	for _, n := range pulls {
		if n < 1 {
			return fmt.Errorf("invalid pull request number")
		}
		specs = append(specs, fmt.Sprintf("+refs/pull/%d/head:%spull/%d", n, NamespaceRoot, n))
	}
	for _, b := range bases {
		if !ValidBranch(b) {
			return fmt.Errorf("invalid base branch name")
		}
		specs = append(specs, fmt.Sprintf("+refs/heads/%s:%sbase/%s", b, NamespaceRoot, b))
	}
	if len(specs) == 0 {
		return nil
	}
	args := append([]string{"fetch", "--quiet", "--no-tags", "--no-write-fetch-head", "--no-recurse-submodules", "--", remote}, specs...)
	_, err := r.ok(ctx, "fetch", args...)
	return err
}

// Resolve implements Git.
func (r *Runner) Resolve(ctx context.Context, ref string) (string, error) {
	if !strings.HasPrefix(ref, NamespaceRoot) {
		return "", fmt.Errorf("ref outside the private namespace")
	}
	res, err := r.run(ctx, nil, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		return "", err
	}
	if res.code != 0 {
		return "", nil
	}
	sha := strings.TrimSpace(string(res.out))
	if !ValidSHA(sha) {
		return "", fmt.Errorf("unexpected rev-parse output")
	}
	return sha, nil
}

func (r *Runner) needSHA(shas ...string) error {
	for _, s := range shas {
		if !ValidSHA(s) {
			return fmt.Errorf("invalid commit name")
		}
	}
	return nil
}

// MergeBase implements Git.
func (r *Runner) MergeBase(ctx context.Context, a, b string) (string, error) {
	if err := r.needSHA(a, b); err != nil {
		return "", err
	}
	res, err := r.run(ctx, nil, "merge-base", a, b)
	if err != nil {
		return "", err
	}
	switch res.code {
	case 0:
		return strings.TrimSpace(string(res.out)), nil
	case 1:
		return "", nil
	}
	return "", fmt.Errorf("merge-base failed (exit %d)", res.code)
}

// quarantineEnv returns the environment that sends new objects into a temporary directory and keeps
// the repository's objects readable. The directory is created once and removed by Close.
func (r *Runner) quarantineEnv(ctx context.Context) ([]string, error) {
	if r.quarantine == "" {
		out, err := r.ok(ctx, "objects path", "rev-parse", "--path-format=absolute", "--git-path", "objects")
		if err != nil {
			return nil, err
		}
		dir, err := os.MkdirTemp("", "stw-conflicts-*")
		if err != nil {
			return nil, err
		}
		r.quarantine, r.objects = dir, strings.TrimSpace(string(out))
	}
	return []string{"GIT_OBJECT_DIRECTORY=" + r.quarantine, "GIT_ALTERNATE_OBJECT_DIRECTORIES=" + r.objects}, nil
}

// Close removes the quarantine directory.
func (r *Runner) Close() {
	if r.quarantine != "" {
		_ = os.RemoveAll(filepath.Clean(r.quarantine))
		r.quarantine = ""
	}
}

// MergeTree implements Git: `git merge-tree --write-tree` without touching the repository. Its
// objects go to the quarantine. Exit 0 is a clean merge, 1 a conflict (the files follow the tree).
func (r *Runner) MergeTree(ctx context.Context, base, head string) (bool, []string, error) {
	if err := r.needSHA(base, head); err != nil {
		return false, nil, err
	}
	env, err := r.quarantineEnv(ctx)
	if err != nil {
		return false, nil, err
	}
	res, err := r.run(ctx, env, "merge-tree", "--write-tree", "--name-only", "-z", "--no-messages", base, head)
	if err != nil {
		return false, nil, err
	}
	switch res.code {
	case 0:
		return false, nil, nil
	case 1:
		parts := bytes.Split(res.out, []byte{0})
		var files []string
		for _, p := range parts[1:] {
			if len(p) == 0 {
				break
			}
			files = append(files, string(p))
		}
		return true, files, nil
	}
	return false, nil, fmt.Errorf("merge-tree failed (exit %d)", res.code)
}

// LastTouch implements Git.
func (r *Runner) LastTouch(ctx context.Context, from, to, path string) (string, error) {
	if err := r.needSHA(from, to); err != nil {
		return "", err
	}
	out, err := r.ok(ctx, "log", "log", "-1", "--format=%H", from+".."+to, "--", path)
	if err != nil {
		return "", err
	}
	sha := strings.TrimSpace(string(out))
	if sha != "" && !ValidSHA(sha) {
		return "", fmt.Errorf("unexpected log output")
	}
	return sha, nil
}

// Changed implements Git.
func (r *Runner) Changed(ctx context.Context, from, to string) ([]string, error) {
	if err := r.needSHA(from, to); err != nil {
		return nil, err
	}
	out, err := r.ok(ctx, "diff", "diff", "--name-only", "-z", "--no-renames", from, to)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, p := range bytes.Split(out, []byte{0}) {
		if len(p) > 0 {
			files = append(files, string(p))
		}
	}
	return files, nil
}
