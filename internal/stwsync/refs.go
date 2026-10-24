// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// maxRefs bounds the references LocalRefs reads (the output is also bounded to 4 MiB by git()).
const maxRefs = 100000

// RepoRefs are the local references of one repository that the wizard's base derivation reads.
type RepoRefs struct {
	// Refs holds every local branch (refs/heads/...) and remote-tracking reference (refs/remotes/...).
	Refs map[string]bool
	// Remotes are the remotes that have at least one remote-tracking reference, sorted.
	Remotes []string
	// Heads maps a remote to the target of its symbolic refs/remotes/<remote>/HEAD, empty when it has none.
	Heads map[string]string
}

// LocalRefs reads the branches and remote-tracking references of the repository at dir, read only
// and offline: one fixed `git for-each-ref` over refs/heads and refs/remotes, bounded output, the
// same environment as every other Git call here, argument vector only.
func LocalRefs(ctx context.Context, dir string) (RepoRefs, error) {
	bin, err := exec.LookPath("git")
	if err != nil {
		return RepoRefs{}, errors.New("the Git executable is unavailable")
	}
	if bin, err = filepath.Abs(bin); err != nil {
		return RepoRefs{}, errors.New("the Git executable is unavailable")
	}
	out, err := git(ctx, bin, dir, "for-each-ref", "--format=%(refname)%09%(symref)", "--", "refs/heads", "refs/remotes")
	if err != nil {
		return RepoRefs{}, err
	}
	if out.Code != 0 {
		return RepoRefs{}, errors.New("the references cannot be read: " + strings.TrimSpace(out.Diagnostic))
	}
	r := RepoRefs{Refs: map[string]bool{}, Remotes: []string{}, Heads: map[string]string{}}
	remotes := map[string]bool{}
	for _, line := range strings.Split(strings.TrimRight(out.Out, "\n"), "\n") {
		if line == "" {
			continue
		}
		if len(r.Refs) >= maxRefs {
			return RepoRefs{}, errors.New("too many references")
		}
		name, symref, _ := strings.Cut(line, "\t")
		r.Refs[name] = true
		if rest, ok := strings.CutPrefix(name, "refs/remotes/"); ok {
			remote, _, found := strings.Cut(rest, "/")
			if !found || remote == "" {
				continue
			}
			remotes[remote] = true
			if rest == remote+"/HEAD" && symref != "" {
				r.Heads[remote] = symref
			}
		}
	}
	for remote := range remotes {
		r.Remotes = append(r.Remotes, remote)
		if _, ok := r.Heads[remote]; !ok {
			r.Heads[remote] = ""
		}
	}
	sort.Strings(r.Remotes)
	return r, nil
}
