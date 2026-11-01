// SPDX-License-Identifier: EUPL-1.2

package land

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
	gitOutputLimit = 4 << 20
	gitTimeout     = 2 * time.Minute
)

// GitRunner is the read-only Git of the plan (the Git interface). Its only effect is the disclosed
// fetch into refs/stw/land/. Arguments are fixed vectors; values from the repository or the forge
// are validated (RefName, ValidSHA) before they stand in one. There is no push method here: the
// landing push lives behind the authorization of the apply code.
type GitRunner struct {
	Bin string
	Dir string
	// repoOverride replaces the OWNER/NAME derived from the remote URL; tests with a bare local
	// remote use it.
	repoOverride string
	// Host is the GitHub host remote URLs are matched against.
	Host string
}

// NewGit returns a runner that works in dir (a checkout or worktree of the repository).
func NewGit(dir string) *GitRunner { return &GitRunner{Bin: "git", Dir: dir, Host: "github.com"} }

type gitResult struct {
	out, diag string
	code      int
}

var errGitOutput = errors.New("git output exceeds the bound")

type limited struct{ bytes.Buffer }

func (b *limited) Write(p []byte) (int, error) {
	if b.Len()+len(p) > gitOutputLimit {
		return 0, errGitOutput
	}
	return b.Buffer.Write(p)
}

func gitEnvironment() []string {
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

func (g *GitRunner) run(ctx context.Context, args ...string) (gitResult, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	full := append([]string{"--no-replace-objects", "-c", "core.fsmonitor=false", "-c", "gc.auto=0", "-c", "maintenance.auto=false", "-C", g.Dir}, args...)
	cmd := exec.CommandContext(ctx, g.Bin, full...)
	cmd.Env = gitEnvironment()
	var out, diag limited
	cmd.Stdout, cmd.Stderr = &out, &diag
	err := cmd.Run()
	res := gitResult{out: out.String(), diag: diag.String()}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		res.code = exit.ExitCode()
		return res, nil
	}
	return res, err
}

// RemoteRepo implements Git.
func (g *GitRunner) RemoteRepo(ctx context.Context, remote string) (string, error) {
	if !RefName(remote) {
		return "", fmt.Errorf("invalid remote name")
	}
	res, err := g.run(ctx, "remote", "get-url", remote)
	if err != nil {
		return "", err
	}
	if res.code != 0 {
		return "", fmt.Errorf("remote %s is not configured", clean(remote))
	}
	if g.repoOverride != "" {
		return g.repoOverride, nil
	}
	return parseGitHubURL(strings.TrimSpace(res.out), g.Host), nil
}

// parseGitHubURL returns OWNER/NAME of a GitHub remote URL on host, or "" for anything else.
func parseGitHubURL(raw, host string) string {
	var path string
	switch {
	case strings.HasPrefix(raw, "https://"+host+"/"):
		path = strings.TrimPrefix(raw, "https://"+host+"/")
	case strings.HasPrefix(raw, "ssh://git@"+host+"/"):
		path = strings.TrimPrefix(raw, "ssh://git@"+host+"/")
	case strings.HasPrefix(raw, "git@"+host+":"):
		path = strings.TrimPrefix(raw, "git@"+host+":")
	default:
		return ""
	}
	path = strings.TrimSuffix(strings.TrimSuffix(path, "/"), ".git")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	for _, p := range parts {
		for _, c := range p {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
				return ""
			}
		}
	}
	return path
}

// DefaultTip implements Git with `git ls-remote`.
func (g *GitRunner) DefaultTip(ctx context.Context, remote, branch string) (string, error) {
	if !RefName(remote) || !RefName(branch) {
		return "", fmt.Errorf("invalid remote or branch name")
	}
	res, err := g.run(ctx, "ls-remote", "--refs", remote, "refs/heads/"+branch)
	if err != nil {
		return "", err
	}
	if res.code != 0 {
		return "", fmt.Errorf("git ls-remote failed: %s", clean(res.diag))
	}
	for _, line := range strings.Split(res.out, "\n") {
		sha, ref, ok := strings.Cut(line, "\t")
		if ok && ref == "refs/heads/"+branch && ValidSHA(sha) {
			return sha, nil
		}
	}
	return "", fmt.Errorf("the remote has no branch %s", clean(branch))
}

// Fetch implements Git.
func (g *GitRunner) Fetch(ctx context.Context, remote, branch string, numbers []int) error {
	if !RefName(remote) || !RefName(branch) {
		return fmt.Errorf("invalid remote or branch name")
	}
	res, err := g.run(ctx, FetchArgs(remote, branch, numbers)...)
	if err != nil {
		return err
	}
	if res.code != 0 {
		return fmt.Errorf("git fetch failed: %s", clean(res.diag))
	}
	return nil
}

// Resolve implements Git.
func (g *GitRunner) Resolve(ctx context.Context, number int) (string, error) {
	res, err := g.run(ctx, "rev-parse", "--verify", "--quiet", PullRef(number)+"^{commit}")
	if err != nil {
		return "", err
	}
	if res.code == 1 {
		return "", nil
	}
	if res.code != 0 {
		return "", fmt.Errorf("git rev-parse failed: %s", clean(res.diag))
	}
	sha := strings.TrimSpace(res.out)
	if !ValidSHA(sha) {
		return "", fmt.Errorf("git rev-parse returned no object id")
	}
	return sha, nil
}

// IsAncestor implements Git: exit 0 yes, 1 no, anything else (128: an object is absent) unknown.
func (g *GitRunner) IsAncestor(ctx context.Context, older, newer string) (Ancestry, error) {
	if !ValidSHA(older) || !ValidSHA(newer) {
		return AncestorUnknown, nil
	}
	res, err := g.run(ctx, "merge-base", "--is-ancestor", older, newer)
	if err != nil {
		return AncestorUnknown, err
	}
	switch res.code {
	case 0:
		return AncestorYes, nil
	case 1:
		return AncestorNo, nil
	}
	return AncestorUnknown, nil
}

// Range implements Git.
func (g *GitRunner) Range(ctx context.Context, from, to string, limit int) ([]Commit, error) {
	if !ValidSHA(from) || !ValidSHA(to) || limit < 0 {
		return nil, fmt.Errorf("invalid range")
	}
	const us, rs = "\x1f", "\x1e"
	res, err := g.run(ctx, "log", "--no-color", "--no-show-signature", "--max-count="+strconv.Itoa(limit+1),
		"--format=%H"+us+"%an"+us+"%ae"+us+"%cn"+us+"%ce"+rs, "--end-of-options", from+".."+to, "--")
	if err != nil {
		return nil, err
	}
	if res.code != 0 {
		return nil, fmt.Errorf("git log failed: %s", clean(res.diag))
	}
	var out []Commit
	for _, rec := range strings.Split(res.out, rs) {
		rec = strings.Trim(rec, "\n")
		if rec == "" {
			continue
		}
		f := strings.Split(rec, us)
		if len(f) != 5 || !ValidSHA(f[0]) {
			return nil, fmt.Errorf("unexpected git log record")
		}
		out = append(out, Commit{SHA: f[0], AuthorName: f[1], AuthorEmail: f[2], CommitterName: f[3], CommitterEmail: f[4]})
	}
	return out, nil
}

// Remotes lists the configured remote names.
func (g *GitRunner) Remotes(ctx context.Context) ([]string, error) {
	res, err := g.run(ctx, "remote")
	if err != nil {
		return nil, err
	}
	if res.code != 0 {
		return nil, fmt.Errorf("git remote failed: %s", clean(res.diag))
	}
	var out []string
	for _, l := range strings.Split(res.out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out, nil
}
