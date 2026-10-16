// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
)

// Checkout evidence (Spec 021 FR-005, T011). Everything here runs against a submodule whose worktree and
// Git directory pins_safe.go validated, through the isolated runner: replace objects are never honored
// (--no-replace-objects), no lazy fetch happens (GIT_NO_LAZY_FETCH) and no worktree content is read.

// pinsCountCap is the cap of each commit count (FR-005); one more is requested to see that it was hit.
const pinsCountCap = 10000

// pinsHead is the checked-out HEAD of a submodule.
type pinsHead struct {
	Commit   *string // nil with an unborn HEAD
	Branch   *string // nil when detached
	Detached bool
	Unborn   bool
}

// pinsRepoFacts are repository properties that weaken what a history walk can prove.
type pinsRepoFacts struct {
	Shallow bool
	Partial bool
	Remotes []pinsRemote
	// FileMode and Symlinks are core.fileMode and core.symlinks of the repository's own configuration,
	// both true when unset; the dirty comparison needs them to read the recorded mode.
	FileMode, Symlinks bool
}

// pinsRelation is the comparison of two commits; Ahead counts the subject's commits the reference lacks
// and Behind the reference's commits the subject lacks, each capped separately.
type pinsRelation struct {
	Relation      string // same, ahead, behind, diverged, unrelated or unknown
	Ahead, Behind *int
	Capped        bool
	Reason        string // why the relation is unknown
}

// pinsReadFacts reads the submodule's own configuration once (remotes, partial clone) and notes a
// shallow file. The configuration read is the same filtered `config --local` shape as the remote URLs.
func pinsReadFacts(run *pinsRunner, d pinsSubmoduleDirs) (pinsRepoFacts, error) {
	facts := pinsRepoFacts{FileMode: true, Symlinks: true}
	repo := pinsGitRepo{GitDir: d.GitDir, WorkTree: d.WorkTree}
	out, err := run.run(pinsCall{Repo: repo, Args: []string{"config", "--local", "--null", "--list", "--no-includes"}, Keep: func(rec []byte) bool {
		k, _, _ := strings.Cut(string(rec), "\n")
		k = strings.ToLower(k)
		return strings.HasPrefix(k, "remote.") && (strings.HasSuffix(k, ".url") || strings.HasSuffix(k, ".promisor") || strings.HasSuffix(k, ".partialclonefilter")) || strings.HasPrefix(k, "extensions.partialclone") || k == "core.filemode" || k == "core.symlinks"
	}})
	if err != nil {
		return facts, err
	}
	if out.Exit != 0 {
		return facts, errors.New("cannot read the submodule configuration")
	}
	for _, kv := range parsePinsConfig(out.Stdout) {
		key := strings.ToLower(kv.Key)
		switch {
		case key == "core.filemode" || key == "core.symlinks":
			if b := pinsParseBool(kv); b != nil {
				if key == "core.filemode" {
					facts.FileMode = *b
				} else {
					facts.Symlinks = *b
				}
			}
		case strings.HasPrefix(key, "extensions.partialclone"):
			facts.Partial = facts.Partial || kv.HasValue && kv.Value != ""
		case strings.HasSuffix(key, ".promisor"):
			if b := pinsParseBool(kv); b != nil && *b {
				facts.Partial = true
			}
		case strings.HasSuffix(key, ".partialclonefilter"):
			facts.Partial = facts.Partial || kv.HasValue && kv.Value != ""
		case strings.HasSuffix(key, ".url") && kv.HasValue:
			name := strings.TrimSuffix(strings.TrimPrefix(kv.Key, "remote."), ".url")
			if name != "" {
				facts.Remotes = append(facts.Remotes, pinsRemote{Name: name, URL: kv.Value})
			}
		}
	}
	if d.gitBase != "" {
		if _, err := pinsLstat(d.gitBase, d.gitRel+"/shallow"); err == nil {
			facts.Shallow = true
		} else if !errors.Is(err, fs.ErrNotExist) {
			return facts, fmt.Errorf("cannot inspect the shallow file: %w", err)
		}
	}
	return facts, nil
}

// pinsReadHead reads HEAD. An unborn HEAD is confirmed the way the superproject's is: symbolic-ref names a
// branch and for-each-ref finds neither it nor a broken ref, so a corrupt ref is an error, never "unborn".
func pinsReadHead(run *pinsRunner, repo pinsGitRepo) (pinsHead, error) {
	var h pinsHead
	call := func(args ...string) (pinsOutput, error) { return run.run(pinsCall{Repo: repo, Args: args}) }
	rev, err := call("rev-parse", "HEAD")
	if err != nil {
		return h, err
	}
	sym, err := call("symbolic-ref", "-q", "HEAD")
	if err != nil {
		return h, err
	}
	ref := strings.TrimSpace(string(sym.Stdout))
	switch sym.Exit {
	case 0:
		if !strings.HasPrefix(ref, "refs/") || strings.ContainsAny(ref, "\n\x00") {
			return h, errors.New("HEAD names an unexpected symbolic reference")
		}
		// Git allows any bytes in a branch name; the result carries an escaped form, never fails whole.
		branch := pinsDisplay(strings.TrimPrefix(ref, "refs/heads/"))
		h.Branch = &branch
	case 1:
		h.Detached = true
	default:
		return h, errors.New("cannot read the submodule HEAD")
	}
	if rev.Exit == 0 {
		oid := strings.TrimSpace(string(rev.Stdout))
		if !validPinsOID(oid) {
			return h, errors.New("unexpected submodule HEAD object name")
		}
		h.Commit = &oid
		return h, nil
	}
	if h.Detached || h.Branch == nil {
		return h, errors.New("the submodule HEAD cannot be resolved")
	}
	found, err := call("for-each-ref", "--format=%(refname)", "--", ref)
	if err != nil {
		return h, err
	}
	if found.Exit != 0 || len(found.Stdout) != 0 || found.Stderr != "" {
		return h, errors.New("the submodule HEAD names a branch ref that exists but cannot be resolved")
	}
	h.Unborn = true
	return h, nil
}

// pinsCount counts `exclude..include` with a cap: at most 10,001 commits are walked.
func pinsCount(run *pinsRunner, repo pinsGitRepo, exclude, include string) (n int, capped bool, ok bool, err error) {
	out, err := run.run(pinsCall{Repo: repo, Args: []string{"rev-list", "--count", fmt.Sprintf("--max-count=%d", pinsCountCap+1), exclude + ".." + include}})
	if err != nil {
		return 0, false, false, err
	}
	if out.Exit != 0 {
		return 0, false, false, nil
	}
	v, perr := strconv.Atoi(strings.TrimSpace(string(out.Stdout)))
	if perr != nil || v < 0 {
		return 0, false, false, nil
	}
	if v > pinsCountCap {
		return pinsCountCap, true, true, nil
	}
	return v, false, true, nil
}

// pinsRelate compares subject with reference. Both must be full object names. A missing object, a failed
// walk, or a shallow or partial history that could have hidden the connection gives "unknown" with the
// reason: only "same", "ahead" and "behind" survive a cut history, because they prove that one commit is an
// ancestor of the other.
func pinsRelate(run *pinsRunner, repo pinsGitRepo, facts pinsRepoFacts, subject, reference string) (pinsRelation, error) {
	zero := 0
	if subject == reference {
		return pinsRelation{Relation: "same", Ahead: &zero, Behind: &zero}, nil
	}
	unknown := func(reason string) (pinsRelation, error) {
		return pinsRelation{Relation: "unknown", Reason: reason}, nil
	}
	base, err := run.run(pinsCall{Repo: repo, Args: []string{"merge-base", reference, subject}})
	if err != nil {
		return pinsRelation{}, err
	}
	history := ""
	switch {
	case facts.Shallow:
		history = "shallow repository: history may be cut"
	case facts.Partial:
		history = "partial clone: history may be incomplete"
	}
	switch base.Exit {
	case 0:
	case 1:
		if history != "" {
			return unknown(history)
		}
		return pinsRelation{Relation: "unrelated"}, nil
	default:
		return unknown("a commit needed for the comparison is not in the repository")
	}
	ahead, aCapped, ok1, err := pinsCount(run, repo, reference, subject)
	if err != nil {
		return pinsRelation{}, err
	}
	behind, bCapped, ok2, err := pinsCount(run, repo, subject, reference)
	if err != nil {
		return pinsRelation{}, err
	}
	if !ok1 || !ok2 {
		return unknown("the commits could not be counted")
	}
	r := pinsRelation{Ahead: &ahead, Behind: &behind, Capped: aCapped || bCapped}
	switch {
	case ahead > 0 && behind > 0:
		r.Relation = "diverged"
	case ahead > 0:
		r.Relation = "ahead"
	case behind > 0:
		r.Relation = "behind"
	default:
		return unknown("distinct commits with no difference in their histories")
	}
	if r.Relation == "diverged" && history != "" {
		return unknown(history)
	}
	return r, nil
}
