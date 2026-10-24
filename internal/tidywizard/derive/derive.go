// SPDX-License-Identifier: EUPL-1.2

// Package derive computes the defaults of the stw tidy wizard (Spec 041): workspace, member set,
// remote, base per repository, caller label, protected worktrees and state directories. Every
// value carries its source and whether a flag overrode it. A value that cannot be derived without
// guessing is a typed Ambiguity and never a guess. The functions take observed facts and return
// results; only the state directory code reads the file system.
package derive

import (
	"path/filepath"
	"sort"
	"strings"
)

// Derived is one entry of the derived[] list of the overview.
type Derived struct {
	Name       string `json:"name"`
	Value      string `json:"value"`
	Source     string `json:"source"`
	Overridden bool   `json:"overridden"`
}

// Ambiguity reports a value that was not derived. Code is the reason code of the specification.
type Ambiguity struct {
	Code       string   `json:"code"`
	Subject    string   `json:"subject"`
	Candidates []string `json:"candidates"`
	Hint       string   `json:"hint,omitempty"`
}

// Result holds a derived value or, when it is ambiguous, the Ambiguity and an empty value.
type Result struct {
	Derived   Derived
	Ambiguity *Ambiguity
}

func ambiguous(name, code, subject string, candidates []string) Result {
	if candidates == nil {
		candidates = []string{}
	}
	return Result{Derived: Derived{Name: name}, Ambiguity: &Ambiguity{Code: code, Subject: subject, Candidates: candidates}}
}

// Remote derives the remote of one repository: origin if present, else the single remote.
func Remote(repo string, remotes []string) Result {
	name := "remote:" + repo
	sorted := append([]string{}, remotes...)
	sort.Strings(sorted)
	for _, r := range sorted {
		if r == "origin" {
			return Result{Derived: Derived{Name: name, Value: r, Source: "origin"}}
		}
	}
	if len(sorted) == 1 {
		return Result{Derived: Derived{Name: name, Value: sorted[0], Source: "single-remote"}}
	}
	return ambiguous(name, "remote-ambiguous", repo, sorted)
}

// RepoFacts are the observed facts of one repository that the base derivation reads.
type RepoFacts struct {
	Name    string
	Backend string // git or jj
	// RemoteHead maps a remote to the target of its symbolic refs/remotes/<remote>/HEAD, when it has one.
	RemoteHead map[string]string
	// Trunk is the reference the jj trunk() revset resolves to, or empty.
	Trunk string
	// Refs holds the references that exist locally; the wizard never fetches.
	Refs map[string]bool
}

// Base derives the base reference of one repository from the symbolic HEAD of the remote (git) or
// the trunk bookmark (jj). An override (--member-base) wins but must exist locally. A missing or
// dangling default is base-ambiguous: the repository is not analysed.
func Base(f RepoFacts, remote, override string) Result {
	name := "base:" + f.Name
	if override != "" {
		if !f.Refs[override] {
			return ambiguous(name, "base-missing", f.Name, []string{override})
		}
		return Result{Derived: Derived{Name: name, Value: override, Source: "--member-base", Overridden: true}}
	}
	value, source := f.RemoteHead[remote], "refs/remotes/"+remote+"/HEAD"
	if f.Backend == "jj" {
		value, source = f.Trunk, "jj trunk()"
	}
	if value == "" || !f.Refs[value] {
		return ambiguous(name, "base-ambiguous", f.Name, nil)
	}
	return Result{Derived: Derived{Name: name, Value: value, Source: source}}
}

// Workspace derives the workspace root: --workspace, else the discovered root. None is a usage error.
func Workspace(flag, discovered string) Result {
	switch {
	case flag != "":
		return Result{Derived: Derived{Name: "workspace", Value: flag, Source: "--workspace", Overridden: true}}
	case discovered != "":
		return Result{Derived: Derived{Name: "workspace", Value: discovered, Source: "discovery"}}
	}
	return ambiguous("workspace", "workspace-not-found", "", nil)
}

// Members returns the member set: the workspace repository "." first, then the discovered members
// without repeats. No discovered member leaves only "." and a notice.
func Members(discovered []string) ([]string, string) {
	members, seen := []string{"."}, map[string]bool{".": true}
	for _, m := range discovered {
		if !seen[m] {
			seen[m] = true
			members = append(members, m)
		}
	}
	if len(members) == 1 {
		return members, "workspace-members-none"
	}
	return members, ""
}

// Caller derives the caller label: $STW_OWNER, else (human mode only) the OS user. It is used for
// --mine, scope and agent mode; an unset label is not an ambiguity.
func Caller(stwOwner, osUser string, agent bool) Derived {
	switch {
	case stwOwner != "":
		return Derived{Name: "caller", Value: stwOwner, Source: "$STW_OWNER"}
	case !agent && osUser != "":
		return Derived{Name: "caller", Value: osUser, Source: "os-user"}
	}
	return Derived{Name: "caller", Source: "unset"}
}

// Worktree is a worktree path of a repository and whether it is the primary checkout.
type Worktree struct {
	Path    string
	Primary bool
}

// Protection is a worktree that is retained automatically.
type Protection struct {
	Path   string
	Reason string // primary-checkout or current-worktree
}

// Protected returns the worktrees the wizard retains without a flag: primary checkouts and the
// worktree containing the working directory.
func Protected(cwd string, trees []Worktree) []Protection {
	var out []Protection
	for _, t := range trees {
		switch {
		case t.Primary:
			out = append(out, Protection{t.Path, "primary-checkout"})
		case cwd == t.Path || strings.HasPrefix(cwd, t.Path+string(filepath.Separator)):
			out = append(out, Protection{t.Path, "current-worktree"})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
