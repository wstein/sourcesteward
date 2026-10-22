// SPDX-License-Identifier: EUPL-1.2
package watchcmd

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/wstein/sourcesteward/internal/stwsync"
	"github.com/wstein/sourcesteward/internal/untrusted"
	"github.com/wstein/sourcesteward/internal/watch"
)

// MaxRepos is the number of repositories per run (as stw status).
const MaxRepos = 256

// ErrUsage marks an error that is a usage or configuration error (exit 2): nothing was read or written.
var ErrUsage = errors.New("usage")

type usageError struct{ msg string }

func (e *usageError) Error() string        { return e.msg }
func (e *usageError) Is(target error) bool { return target == ErrUsage }

func usagef(format string, args ...any) error { return &usageError{fmt.Sprintf(format, args...)} }

// ParseRemote extracts OWNER/NAME from a remote URL of host (https, ssh:// and scp-like spellings).
func ParseRemote(remote, host string) (string, bool) {
	remote = strings.TrimSpace(remote)
	var h, path string
	switch {
	case strings.Contains(remote, "://"):
		u, err := url.Parse(remote)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "ssh" && u.Scheme != "git") {
			return "", false
		}
		h, path = u.Hostname(), strings.TrimPrefix(u.Path, "/")
	default: // scp-like: [user@]host:owner/name
		before, after, ok := strings.Cut(remote, ":")
		if !ok || strings.Contains(before, "/") {
			return "", false
		}
		if _, hostPart, hasUser := strings.Cut(before, "@"); hasUser {
			before = hostPart
		}
		h, path = before, after
	}
	if !strings.EqualFold(h, host) {
		return "", false
	}
	path = strings.TrimSuffix(strings.TrimSuffix(path, "/"), ".git")
	path = strings.TrimSuffix(path, "/")
	if strings.Count(path, "/") != 1 || !untrusted.ValidRepo(path) {
		return "", false
	}
	return path, true
}

// ResolveInput selects the repositories of a run.
type ResolveInput struct {
	Host          string
	Repos         []string // --repo flags
	Workspace     bool     // --workspace
	WorkspacePath string   // PATH of --workspace, "" for the current directory
	Config        *Config  // the loaded config file, if any
	// Discover is stwsync.Discover; a seam for tests.
	Discover func(context.Context, string) (stwsync.Workspace, error)
}

// Selection is the resolved repository set.
type Selection struct {
	Repos         []string
	Skipped       []watch.Skipped
	WorkspaceRoot string // the workspace root when --workspace was used
}

// Resolve unions --repo flags, the workspace and the config [repos], in that order, without duplicates.
// Anything that is not a GitHub repository of Host is skipped and reported, never an error.
func Resolve(ctx context.Context, in ResolveInput) (Selection, error) {
	var sel Selection
	var repos []string
	for _, r := range in.Repos {
		if !untrusted.ValidRepo(r) {
			return sel, usagef("--repo %q is not OWNER/NAME", r)
		}
		repos = append(repos, r)
	}
	if in.Workspace {
		discover := in.Discover
		if discover == nil {
			discover = stwsync.Discover
		}
		path := in.WorkspacePath
		if path == "" {
			path = "."
		}
		ws, err := discover(ctx, path)
		if err != nil {
			return sel, usagef("--workspace: %v", err)
		}
		sel.WorkspaceRoot = ws.Root
		found := 0
		for _, m := range ws.Members {
			switch {
			case !m.Initialized:
				sel.Skipped = append(sel.Skipped, watch.Skipped{Path: m.Name, Reason: "not initialized"})
			case m.Remote == "":
				sel.Skipped = append(sel.Skipped, watch.Skipped{Path: m.Name, Reason: "no remote"})
			default:
				if r, ok := ParseRemote(m.Remote, in.Host); ok {
					repos = append(repos, r)
					found++
				} else {
					sel.Skipped = append(sel.Skipped, watch.Skipped{Path: m.Name, Reason: "remote is not a GitHub repository on " + in.Host})
				}
			}
		}
		if found == 0 && len(repos) == 0 && in.Config == nil {
			return sel, usagef("--workspace: no member of %s has a GitHub remote on %s", ws.Root, in.Host)
		}
	}
	if in.Config != nil {
		repos = append(repos, in.Config.Repos...)
	}
	sel.Repos = dedupe(repos)
	switch {
	case len(sel.Repos) == 0:
		return sel, usagef("no repository: give --repo, --workspace or a config file with repos")
	case len(sel.Repos) > MaxRepos:
		return sel, usagef("%d repositories exceed the limit of %d per run", len(sel.Repos), MaxRepos)
	}
	return sel, nil
}
