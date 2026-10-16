// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"
)

// Publication, tracking and nested-submodule evidence (Spec 021 FR-006, FR-007, FR-013). The evidence is
// the local record of what remotes last advertised (refs/remotes/*): no fetch is made and it may be stale.

const (
	pinsMaxRemoteRefs = 16
	// pinsRefQuery asks for more refs than are named, because --count applies before the symbolic refs
	// (a remote's HEAD) are filtered out in Go.
	pinsRefQuery = 64
)

// pinsPublication is the publication evidence of one pin.
type pinsPublication struct {
	Status       string // reachable, unreachable or unknown
	Reason       string // why unknown
	Refs         []PinsRemoteRef
	Truncated    bool
	LastFetch    *string
	ObjectAbsent bool // the pinned object is not a commit in the repository
}

// pinsRemoteOfRef names the remote a refs/remotes ref belongs to: the longest configured remote name that
// prefixes it (a remote name may contain a slash), else the first path component.
func pinsRemoteOfRef(ref string, remotes []pinsRemote) string {
	rest := strings.TrimPrefix(ref, "refs/remotes/")
	best := ""
	for _, r := range remotes {
		if strings.HasPrefix(rest, r.Name+"/") && len(r.Name) > len(best) {
			best = r.Name
		}
	}
	if best == "" {
		best, _, _ = strings.Cut(rest, "/")
	}
	return best
}

// pinsPublish decides whether remote-tracking refs contain the pin. The object is checked first, because
// `for-each-ref --contains` exits 129 for an absent object or a non-commit and that failure could not be
// told from absence. Local branches and tags are never consulted.
func pinsPublish(run *pinsRunner, repo pinsGitRepo, d pinsSubmoduleDirs, facts pinsRepoFacts, pin string) (pinsPublication, error) {
	p := pinsPublication{Refs: []PinsRemoteRef{}}
	p.LastFetch = pinsLastFetch(d)
	exists, err := run.run(pinsCall{Repo: repo, Args: []string{"cat-file", "-e", pin + "^{commit}"}})
	if err != nil {
		return p, err
	}
	if exists.Exit != 0 {
		p.Status, p.ObjectAbsent = "unknown", true
		p.Reason = "the pinned commit is not in the submodule repository (absent object or not a commit)"
		return p, nil
	}
	// The records end with a NUL so only the ones that can be named are retained (the first 17 real refs
	// show that 16 are not all), instead of charging the shared 1 MiB budget for all 64.
	raw, real := 0, 0
	keep := func(rec []byte) bool {
		rec = bytes.TrimPrefix(rec, []byte("\n"))
		if len(rec) == 0 { // the newline Git prints after the last record
			return false
		}
		raw++
		if _, sym, _ := bytes.Cut(rec, []byte("\t")); len(sym) > 0 {
			return false
		}
		real++
		return real <= pinsMaxRemoteRefs+1
	}
	out, err := run.run(pinsCall{Repo: repo, Keep: keep, Args: []string{"for-each-ref", "--contains", pin, fmt.Sprintf("--count=%d", pinsRefQuery), "--format=%(refname)%09%(symref)%00", "refs/remotes/"}})
	if err != nil {
		return p, err
	}
	if out.Exit != 0 {
		return p, errors.New("cannot list the remote-tracking refs containing the pin")
	}
	kept := real
	for _, rec := range bytes.Split(out.Stdout, []byte{0}) {
		ref, _, _ := strings.Cut(strings.TrimPrefix(string(rec), "\n"), "\t")
		if ref == "" || !strings.HasPrefix(ref, "refs/remotes/") || len(p.Refs) >= pinsMaxRemoteRefs {
			continue
		}
		p.Refs = append(p.Refs, PinsRemoteRef{Remote: boundPinsMessage(pinsRemoteOfRef(ref, facts.Remotes)), Ref: boundPinsMessage(ref)})
	}
	p.Truncated = kept > pinsMaxRemoteRefs || raw >= pinsRefQuery
	switch {
	case kept > 0:
		p.Status = "reachable"
	case raw >= pinsRefQuery:
		p.Status, p.Reason = "unknown", "too many symbolic remote-tracking refs to decide"
	case facts.Shallow:
		// A cut history can hide the connection between a remote-tracking ref and an older pin that
		// was fetched on its own (git submodule update --depth 1): absence of containment proves nothing.
		p.Status, p.Reason = "unknown", "shallow repository: the history is cut, so containment cannot be decided"
	default:
		p.Status = "unreachable"
	}
	return p, nil
}

// pinsLastFetch is the modification time of FETCH_HEAD, advisory and read without writing. A symlink or a
// missing file gives nil.
func pinsLastFetch(d pinsSubmoduleDirs) *string {
	info, err := pinsLstat(d.gitBase, d.gitRel+"/FETCH_HEAD")
	if err != nil || !info.Mode().IsRegular() {
		return nil
	}
	s := info.ModTime().UTC().Format(time.RFC3339)
	return &s
}

// pinsTracking is the relation of the pin to its tracking ref.
type pinsTracking struct {
	Ref           *string
	Relation      string // equal, behind, ahead, diverged or unknown
	Ahead, Behind *int
	Capped        bool
	Reason        string
}

type pinsTrackInput struct {
	Pin         *string // the HEAD pin; nil when the superproject HEAD has none
	Branch      *string // the .gitmodules branch value
	SuperBranch *string // the superproject's current branch, for the value "."
	Remote      *string // the remote the submodule uses (matched URL or the only remote)
}

// pinsSoleRemote is the only remote name when exactly one is configured.
func pinsSoleRemote(facts pinsRepoFacts) *string {
	var name *string
	for i := range facts.Remotes {
		if name != nil && *name != facts.Remotes[i].Name {
			return nil
		}
		name = &facts.Remotes[i].Name
	}
	return name
}

// pinsPlainRefName accepts only names that need no quoting and cannot be read as anything but a branch
// or remote name: branch values come from .gitmodules, which is untrusted, and are placed inside a
// revision argument.
func pinsPlainRefName(s string) bool {
	if s == "" || len(s) > 255 || strings.Contains(s, "..") || strings.Contains(s, "//") ||
		strings.HasPrefix(s, "/") || strings.HasSuffix(s, "/") || strings.HasPrefix(s, "-") || strings.HasPrefix(s, ".") || strings.HasSuffix(s, ".") {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._/+-", c)) {
			return false
		}
	}
	for _, seg := range strings.Split(s, "/") {
		if strings.HasPrefix(seg, ".") || strings.HasSuffix(seg, ".lock") {
			return false
		}
	}
	return true
}

// pinsTrack relates the pin to the tracking ref: the .gitmodules branch of the remote the submodule uses,
// or that remote's recorded default (refs/remotes/<remote>/HEAD). Nothing is fetched; a ref that was never
// fetched is unknown.
func pinsTrack(run *pinsRunner, repo pinsGitRepo, facts pinsRepoFacts, in pinsTrackInput) (pinsTracking, error) {
	unknown := func(ref *string, reason string) (pinsTracking, error) {
		return pinsTracking{Ref: ref, Relation: "unknown", Reason: reason}, nil
	}
	if in.Remote == nil {
		return unknown(nil, "no single remote identifies the tracking ref")
	}
	remote := *in.Remote
	if !pinsPlainRefName(remote) {
		return unknown(nil, "the remote name is not a plain name")
	}
	prefix := "refs/remotes/" + remote + "/"
	var branch string
	switch {
	case in.Branch != nil && *in.Branch == ".":
		if in.SuperBranch == nil {
			return unknown(nil, "the .gitmodules branch '.' follows the superproject branch, which is detached or unborn")
		}
		branch = *in.SuperBranch
	case in.Branch != nil:
		branch = *in.Branch
	default:
		out, err := run.run(pinsCall{Repo: repo, Args: []string{"symbolic-ref", "-q", prefix + "HEAD"}})
		if err != nil {
			return pinsTracking{}, err
		}
		def, ok := strings.CutPrefix(strings.TrimSpace(string(out.Stdout)), prefix)
		if out.Exit != 0 || !ok {
			return unknown(nil, "no tracking branch is configured and the remote has no recorded default branch")
		}
		branch = def
	}
	if !pinsPlainRefName(branch) {
		return unknown(nil, "the tracking branch name is not a plain branch name")
	}
	ref := prefix + branch
	refp := &ref
	if in.Pin == nil {
		return unknown(refp, "the superproject HEAD records no pin")
	}
	tip, err := run.run(pinsCall{Repo: repo, Args: []string{"rev-parse", "--verify", "--quiet", ref + "^{commit}"}})
	if err != nil {
		return pinsTracking{}, err
	}
	oid := strings.TrimSpace(string(tip.Stdout))
	if tip.Exit != 0 || !validPinsOID(oid) {
		return unknown(refp, "the tracking ref is not in the repository (never fetched, or not a commit)")
	}
	rel, err := pinsRelate(run, repo, facts, *in.Pin, oid)
	if err != nil {
		return pinsTracking{}, err
	}
	switch rel.Relation {
	case "same":
		rel.Relation = "equal"
	case "unrelated":
		return unknown(refp, "the pin and the tracking ref share no history")
	}
	return pinsTracking{Ref: refp, Relation: rel.Relation, Ahead: rel.Ahead, Behind: rel.Behind, Capped: rel.Capped, Reason: rel.Reason}, nil
}

// pinsNestedPresent reports whether the submodule's HEAD tree has a regular .gitmodules, which means that
// nested submodules exist. They are never traversed.
func pinsNestedPresent(run *pinsRunner, repo pinsGitRepo, head pinsHead) (bool, error) {
	if head.Unborn || head.Commit == nil {
		return false, nil
	}
	out, err := run.run(pinsCall{Repo: repo, Args: []string{"ls-tree", "HEAD", ".gitmodules"}})
	if err != nil {
		return false, err
	}
	if out.Exit != 0 {
		return false, errors.New("cannot read the submodule HEAD tree")
	}
	meta, path, ok := strings.Cut(strings.TrimSuffix(string(out.Stdout), "\n"), "\t")
	f := strings.Fields(meta)
	return ok && path == ".gitmodules" && len(f) == 3 && (f[0] == "100644" || f[0] == "100755") && f[1] == "blob", nil
}

// pinsSubmoduleBackend detects a submodule that is itself jj- or GitButler-managed from filesystem
// markers alone (no process), so a managed submodule is not examined by Git at all. The result is "",
// "jj", "gitbutler" or "ambiguous".
func pinsSubmoduleBackend(d pinsSubmoduleDirs) (string, error) {
	exists := func(base, rel string) (bool, error) {
		_, err := pinsLstat(base, rel)
		var link *pinsSymlinkError
		switch {
		case err == nil, errors.As(err, &link):
			return true, nil
		case errors.Is(err, fs.ErrNotExist), errors.Is(err, errPinsNotDirectory):
			return false, nil
		}
		return false, err
	}
	jj, err := exists(d.WorkTree, ".jj")
	if err != nil {
		return "", err
	}
	butler := false
	for _, rel := range []string{"gitbutler", "refs/gitbutler", "refs/heads/gitbutler/workspace"} {
		found, err := exists(d.gitBase, d.gitRel+"/"+rel)
		if err != nil {
			return "", err
		}
		butler = butler || found
	}
	switch {
	case jj && butler:
		return "ambiguous", nil
	case jj:
		return "jj", nil
	case butler:
		return "gitbutler", nil
	}
	return "", nil
}
