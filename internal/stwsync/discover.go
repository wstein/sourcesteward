// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/wstein/sourcesteward/internal/inspection"
)

// Member is one repository of a workspace: the root or an initialized or uninitialized submodule.
// Remote is the URL of `origin`, else of the only remote, else empty.
type Member struct {
	Name        string
	Path        string
	Kind        string // root or submodule
	Initialized bool
	Remote      string
}

// Workspace is the top-level superproject found from any directory inside it, with its members in the
// order of stw status (root first, then the submodules depth first).
type Workspace struct {
	Root    string
	Members []Member
}

// Discover finds the workspace exactly as stw status does and reads the remote of every initialized
// member. It changes nothing and does not touch the network. Git only.
func Discover(ctx context.Context, path string) (Workspace, error) {
	var w Workspace
	if err := ValidateOptions(Options{Path: path}); err != nil {
		return w, err
	}
	bin, err := exec.LookPath("git")
	if err != nil {
		return w, errors.New("the Git executable is unavailable")
	}
	if bin, err = filepath.Abs(bin); err != nil {
		return w, errors.New("the Git executable is unavailable")
	}
	id := inspection.Inspect(ctx, path)
	if len(id.Evidence) > 0 || id.Backend == "jj" {
		return w, errors.New("jj-owned paths are not supported (Git only)")
	}
	if id.Status != "ok" || id.Backend != "git" || id.Repository == nil {
		return w, errors.New("a Git repository is needed: " + id.Error())
	}
	root, _, err := (env{bin, id.InputPath}).workspaceRoot(ctx)
	if err != nil {
		return w, err
	}
	var specs []statusSpec
	if err := walkStatus(ctx, root, ".", "root", nil, &specs); err != nil {
		return w, err
	}
	w.Root = root.dir
	for _, sp := range specs {
		m := Member{Name: sp.name, Path: sp.e.dir, Kind: sp.kind, Initialized: !sp.uninitalized}
		if _, err := os.Lstat(filepath.Join(sp.e.dir, ".git")); err != nil {
			m.Initialized = false
		}
		if m.Initialized {
			m.Remote = remoteURL(ctx, sp.e)
		}
		w.Members = append(w.Members, m)
	}
	return w, nil
}

// remoteURL is the URL of origin, else of the only remote; empty when there is none or the choice is ambiguous.
func remoteURL(ctx context.Context, e env) string {
	out, err := e.g(ctx, "remote")
	if err != nil || out.Code != 0 {
		return ""
	}
	names := strings.Fields(out.Out)
	name := ""
	for _, n := range names {
		if n == "origin" {
			name = n
		}
	}
	if name == "" && len(names) == 1 {
		name = names[0]
	}
	if name == "" {
		return ""
	}
	url, err := e.g(ctx, "remote", "get-url", "--", name)
	if err != nil || url.Code != 0 {
		return ""
	}
	return strings.TrimSpace(url.Out)
}
