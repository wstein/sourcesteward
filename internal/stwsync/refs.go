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

// maxRefs bounds the references LocalRefs reads (the output is also bounded to 4 MiB by git()); a
// variable only so that a test can lower it.
var maxRefs = 100000

// RepoRefs are the local references of one repository that the wizard's base derivation reads.
type RepoRefs struct {
	// Refs holds every local branch (refs/heads/...) and remote-tracking reference (refs/remotes/...).
	Refs map[string]bool
	// Remotes are the configured remotes (git remote), sorted, also those that were never fetched.
	Remotes []string
	// Heads has an entry for every configured remote: the target of its symbolic
	// refs/remotes/<remote>/HEAD, empty when it has none.
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
	names, err := git(ctx, bin, dir, "remote")
	if err != nil {
		return RepoRefs{}, err
	}
	if names.Code != 0 {
		return RepoRefs{}, errors.New("the remotes cannot be read: " + strings.TrimSpace(names.Diagnostic))
	}
	out, err := git(ctx, bin, dir, "for-each-ref", "--format=%(refname)%09%(symref)", "--", "refs/heads", "refs/remotes")
	if err != nil {
		return RepoRefs{}, err
	}
	if out.Code != 0 {
		return RepoRefs{}, errors.New("the references cannot be read: " + strings.TrimSpace(out.Diagnostic))
	}
	r := RepoRefs{Refs: map[string]bool{}, Remotes: []string{}, Heads: map[string]string{}}
	symrefs := map[string]string{}
	for _, line := range strings.Split(strings.TrimRight(out.Out, "\n"), "\n") {
		if line == "" {
			continue
		}
		if len(r.Refs) >= maxRefs {
			return RepoRefs{}, errors.New("too many references")
		}
		name, symref, _ := strings.Cut(line, "\t")
		r.Refs[name] = true
		if symref != "" {
			symrefs[name] = symref
		}
	}
	for _, remote := range strings.Split(strings.TrimRight(names.Out, "\n"), "\n") {
		if remote == "" {
			continue
		}
		r.Remotes = append(r.Remotes, remote)
		r.Heads[remote] = symrefs["refs/remotes/"+remote+"/HEAD"]
	}
	sort.Strings(r.Remotes)
	return r, nil
}
