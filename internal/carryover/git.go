// SPDX-License-Identifier: EUPL-1.2

package carryover

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	gitOutputLimit = 32 << 20
	gitTimeout     = 2 * time.Minute
	// NamespaceRoot is the only ref namespace the check writes (the fetch of a pull request head).
	NamespaceRoot = "refs/stw/carry-over/"
)

// PullRef is the private ref of the fetched head of pull request n.
func PullRef(n int) string { return NamespaceRoot + "pr-" + strconv.Itoa(n) }

// Runner is the guarded Git of the check, backed by the git executable in a checkout. Every call is
// a fixed argument vector with validated object ids, a clean environment and no replace objects.
// Only the subcommands of readOnly run, and the one write is the fetch into NamespaceRoot.
type Runner struct {
	Bin string
	Dir string
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

func environment() []string {
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

// readOnly are the subcommands that never write; fetch is checked separately.
var readOnly = map[string]bool{"cat-file": true, "diff": true, "patch-id": true, "rev-list": true, "range-diff": true}

// guard refuses any vector that is not one of the reviewed read-only commands or the one fetch.
func guard(args []string) error {
	if len(args) == 0 {
		return errors.New("empty git command")
	}
	if readOnly[args[0]] {
		return nil
	}
	if args[0] != "fetch" {
		return fmt.Errorf("git %s is not a read-only command of the carry-over check", args[0])
	}
	// fetch --quiet --no-tags --no-write-fetch-head --no-recurse-submodules -- REMOTE REFSPEC
	if len(args) != 8 || args[6] == "" || !ValidRemote(args[6]) {
		return errors.New("unexpected fetch vector")
	}
	spec := args[7]
	src, dst, ok := strings.Cut(strings.TrimPrefix(spec, "+"), ":")
	if !ok || !strings.HasPrefix(spec, "+refs/pull/") || !strings.HasSuffix(src, "/head") || !strings.HasPrefix(dst, NamespaceRoot) {
		return errors.New("fetch outside the private namespace")
	}
	return nil
}

func (r *Runner) run(ctx context.Context, stdin []byte, args ...string) (result, error) {
	if err := guard(args); err != nil {
		return result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	full := append([]string{"--no-replace-objects", "--literal-pathspecs", "-c", "core.fsmonitor=false", "-c", "gc.auto=0", "-c", "maintenance.auto=false", "-C", r.Dir}, args...)
	cmd := exec.CommandContext(ctx, r.Bin, full...)
	cmd.Env = environment()
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
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

func (r *Runner) ok(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	res, err := r.run(ctx, stdin, args...)
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", args[0], err)
	}
	if res.code != 0 {
		return nil, fmt.Errorf("git %s failed (exit %d)", args[0], res.code)
	}
	return res.out, nil
}

func needSHA(shas ...string) error {
	for _, s := range shas {
		if !ValidSHA(s) {
			return errors.New("invalid commit id")
		}
	}
	return nil
}

// FetchPull implements Git: objects and the one private ref only.
func (r *Runner) FetchPull(ctx context.Context, remote string, n int) (string, error) {
	if !ValidRemote(remote) || n < 1 {
		return "", errors.New("invalid remote or pull request number")
	}
	spec := fmt.Sprintf("+refs/pull/%d/head:%s", n, PullRef(n))
	if _, err := r.ok(ctx, nil, "fetch", "--quiet", "--no-tags", "--no-write-fetch-head", "--no-recurse-submodules", "--", remote, spec); err != nil {
		return "", err
	}
	out, err := r.ok(ctx, nil, "rev-list", "--no-walk", "--end-of-options", PullRef(n))
	if err != nil {
		return "", err
	}
	tip := strings.TrimSpace(string(out))
	if !ValidSHA(tip) {
		return "", errors.New("unexpected rev-list output")
	}
	return tip, nil
}

// HasCommit implements Git.
func (r *Runner) HasCommit(ctx context.Context, sha string) (bool, error) {
	if err := needSHA(sha); err != nil {
		return false, err
	}
	res, err := r.run(ctx, nil, "cat-file", "-e", sha+"^{commit}")
	if err != nil {
		return false, err
	}
	return res.code == 0, nil
}

// PatchID implements Git. The diff is pinned against configuration: no external diff, no textconv,
// no rename detection, binary content included.
func (r *Runner) PatchID(ctx context.Context, base, head string) (string, error) {
	if err := needSHA(base, head); err != nil {
		return "", err
	}
	diff, err := r.ok(ctx, nil, "diff", "--no-color", "--no-ext-diff", "--no-textconv", "--no-renames", "--binary", base+"..."+head, "--")
	if err != nil {
		return "", err
	}
	if len(bytes.TrimSpace(diff)) == 0 {
		return "", nil
	}
	out, err := r.ok(ctx, diff, "patch-id", "--stable")
	if err != nil {
		return "", err
	}
	id, _, _ := strings.Cut(strings.TrimSpace(string(out)), " ")
	if id == "" {
		return "", errors.New("git patch-id printed no id")
	}
	return id, nil
}

// Merges implements Git.
func (r *Runner) Merges(ctx context.Context, base, head string) ([]string, error) {
	if err := needSHA(base, head); err != nil {
		return nil, err
	}
	out, err := r.ok(ctx, nil, "rev-list", "--merges", "--end-of-options", base+".."+head)
	if err != nil {
		return nil, err
	}
	var shas []string
	for _, l := range strings.Fields(string(out)) {
		if !ValidSHA(l) {
			return nil, errors.New("unexpected rev-list output")
		}
		shas = append(shas, l)
	}
	return shas, nil
}

// Count implements Git: the commits of base..head without merge commits, the ones range-diff pairs.
func (r *Runner) Count(ctx context.Context, base, head string) (int, error) {
	if err := needSHA(base, head); err != nil {
		return 0, err
	}
	out, err := r.ok(ctx, nil, "rev-list", "--count", "--no-merges", "--end-of-options", base+".."+head)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || n < 0 {
		return 0, errors.New("unexpected rev-list output")
	}
	return n, nil
}

// RangeDiff implements Git.
func (r *Runner) RangeDiff(ctx context.Context, oldBase, oldHead, newBase, newHead string) (string, error) {
	if err := needSHA(oldBase, oldHead, newBase, newHead); err != nil {
		return "", err
	}
	out, err := r.ok(ctx, nil, "range-diff", "--no-color", "--no-patch", "--no-notes", oldBase+".."+oldHead, newBase+".."+newHead)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
