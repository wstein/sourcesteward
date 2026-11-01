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
	// MaxCommits bounds the commits per side; zero means the default.
	MaxCommits int
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

const (
	logFormat = "--format=%x00%B%x00"
	shaSuffix = "^{commit}"
)

// fetchFlags are the exact flags of the one fetch: objects and the private ref only, no tags, no
// FETCH_HEAD, no submodules, and an empty refmap so that a configured remote.<name>.fetch writes nothing.
var fetchFlags = []string{"fetch", "--quiet", "--no-tags", "--no-write-fetch-head", "--no-recurse-submodules", "--refmap=", "--"}

// diffFlags and logFlags are the exact leading flags of the diff and the log vector.
var (
	diffFlags = []string{"diff", "--no-color", "--no-ext-diff", "--no-textconv", "--no-renames", "--binary", "--ignore-submodules=none", "--submodule=short"}
	logFlags  = []string{"log", "--reverse", "--no-merges", "--no-color", "--no-show-signature", "--no-ext-diff", "--no-textconv", "--no-renames", "--binary", "--ignore-submodules=none", "--submodule=short", "-p", logFormat, "--end-of-options"}
)

// shape reports whether args is exactly fixed followed by one argument per validator.
func shape(args, fixed []string, tail ...func(string) bool) bool {
	if len(args) != len(fixed)+len(tail) {
		return false
	}
	for i, f := range fixed {
		if args[i] != f {
			return false
		}
	}
	for i, ok := range tail {
		if !ok(args[len(fixed)+i]) {
			return false
		}
	}
	return true
}

func rangeArg(s string) bool {
	a, b, ok := strings.Cut(s, "..")
	return ok && ValidSHA(a) && ValidSHA(b) && !strings.HasPrefix(b, ".")
}

func tripleArg(s string) bool {
	a, b, ok := strings.Cut(s, "...")
	return ok && ValidSHA(a) && ValidSHA(b)
}

func isDashDash(s string) bool { return s == "--" }

func commitArg(s string) bool {
	return strings.HasSuffix(s, shaSuffix) && ValidSHA(strings.TrimSuffix(s, shaSuffix))
}

func pullRefArg(s string) bool { return strings.HasPrefix(s, NamespaceRoot+"pr-") }

// guard refuses every vector that is not exactly one of the reviewed commands. The flags are pinned
// (no external diff, no textconv, binary content in full, no submodule hiding, no signature program,
// no tags), so a changed flag, a missing flag or an extra argument fails here.
func guard(args []string) error {
	switch {
	case shape(args, []string{"rev-parse", "--verify", "--quiet"}, commitArg):
	case shape(args, diffFlags, tripleArg, isDashDash):
	case shape(args, []string{"patch-id", "--stable"}):
	case shape(args, []string{"rev-list", "--merges", "--end-of-options"}, rangeArg):
	case shape(args, []string{"rev-list", "--no-walk", "--end-of-options"}, pullRefArg):
	case shape(args, logFlags, rangeArg):
	case len(args) == len(fetchFlags)+2 && shape(args[:len(fetchFlags)], fetchFlags):
		return guardFetch(args[len(fetchFlags)], args[len(fetchFlags)+1])
	default:
		return fmt.Errorf("git %v is not one of the reviewed read-only commands of the carry-over check", args)
	}
	return nil
}

func guardFetch(remote, spec string) error {
	src, dst, ok := strings.Cut(strings.TrimPrefix(spec, "+"), ":")
	if !ValidRemote(remote) || !ok || !strings.HasPrefix(spec, "+refs/pull/") || !strings.HasSuffix(src, "/head") || !strings.HasPrefix(dst, NamespaceRoot) {
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
	full := append([]string{"--no-replace-objects", "--literal-pathspecs", "-c", "core.hooksPath=" + os.DevNull, "-c", "core.fsmonitor=false", "-c", "gc.auto=0", "-c", "maintenance.auto=false", "-C", r.Dir}, args...)
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
	if _, err := r.ok(ctx, nil, append(append([]string{}, fetchFlags...), remote, spec)...); err != nil {
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

// HasCommit implements Git: exit 1 of the quiet verify is a missing object; any other failure
// (no repository, a broken object store) is an error.
func (r *Runner) HasCommit(ctx context.Context, sha string) (bool, error) {
	if err := needSHA(sha); err != nil {
		return false, err
	}
	res, err := r.run(ctx, nil, "rev-parse", "--verify", "--quiet", sha+shaSuffix)
	if err != nil {
		return false, err
	}
	switch res.code {
	case 0:
		return true, nil
	case 1:
		return false, nil
	}
	return false, fmt.Errorf("git rev-parse failed (exit %d)", res.code)
}

// PatchID implements Git. The diff is pinned against configuration: no external diff, no textconv,
// no rename detection.
func (r *Runner) PatchID(ctx context.Context, base, head string) (string, error) {
	if err := needSHA(base, head); err != nil {
		return "", err
	}
	diff, err := r.ok(ctx, nil, append(append([]string{}, diffFlags...), base+"..."+head, "--")...)
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

// MaxCommits is the default bound of the commits compared per side.
const MaxCommits = 500

// parseLog splits the output of the log vector: NUL, message, NUL, patch, per commit.
func parseLog(out string, max int) ([]CommitPatch, error) {
	if out == "" {
		return nil, nil
	}
	parts := strings.Split(out, "\x00")
	if parts[0] != "" || len(parts)%2 != 1 {
		return nil, errors.New("unexpected git log output")
	}
	var commits []CommitPatch
	for i := 1; i+1 < len(parts); i += 2 {
		commits = append(commits, CommitPatch{Message: parts[i], Patch: parts[i+1]})
	}
	if len(commits) > max {
		return nil, fmt.Errorf("more than %d commits", max)
	}
	return commits, nil
}

// Commits implements Git: the commits of base..head without merge commits, oldest first, each with
// its message and its patch. No external diff, textconv, signature program or hook can start.
func (r *Runner) Commits(ctx context.Context, base, head string) ([]CommitPatch, error) {
	if err := needSHA(base, head); err != nil {
		return nil, err
	}
	out, err := r.ok(ctx, nil, append(append([]string{}, logFlags...), base+".."+head)...)
	if err != nil {
		return nil, err
	}
	max := r.MaxCommits
	if max == 0 {
		max = MaxCommits
	}
	return parseLog(string(out), max)
}
