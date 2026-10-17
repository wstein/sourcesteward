// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Submodule discovery (Spec 023 FR-006, FR-008, FR-009).
//
// Everything the superproject says about its submodules is untrusted data. Registration is read with a
// minimal set of Git calls made through the Spec 021 isolation runner (pinsRunner); confinement of each
// candidate - paths, the .git pointer, the Git directory, ownership, alternates and commondir - is decided by
// the Spec 021 safety layer (pinsSafety) before any process runs in the submodule. Consequently a submodule
// that Spec 021 refuses is refused here too; in particular a submodule with objects/info/alternates or a
// commondir entry is path-unsafe and `repos --submodules` does not inventory it.

// reposNewRunner creates the isolated runner of one discovery. Each Git invocation has its own per-call
// deadline inside the context of the run.
type reposNewRunner func(ctx context.Context) *pinsRunner

func newReposRunner(ctx context.Context) *pinsRunner {
	r := newPinsRunner(ctx)
	r.timeout = defaultReposBounds.call
	return r
}

// reposCandidate is one registered submodule path after confirmation and safety checks.
type reposCandidate struct {
	rel, abs   string
	kind       string // "ok", "skipped", "unsafe"
	code, msg  string
	dirs       *pinsSubmoduleDirs
	safety     *pinsSafety
	parentRoot string
}

// reposParentDiscovery is the data outcome of discovery for one parent; it never touches an entry.
type reposParentDiscovery struct {
	status     string // "ok", "failed", "limit"
	reason     string
	err        *ReposError
	ignored    int
	notes      []string
	candidates []reposCandidate
}

// reposPendingSub is a discovered entry whose identity is inspected next and then checked against the
// directories that were validated.
type reposPendingSub struct {
	index int
	cand  reposCandidate
}

// reposMapError reports the timeout code for an error of an identity, discovery or inventory call, or an
// empty code when err is no timeout or cancellation. A caller whose own context ended always gets
// `canceled`, never a timeout of the repos deadline.
func reposMapError(callerCtx, totalCtx context.Context, err error) (code, message string) {
	switch {
	case callerCtx.Err() == context.DeadlineExceeded:
		return ReposCodeCanceled, "caller deadline exceeded"
	case callerCtx.Err() != nil:
		return ReposCodeCanceled, "caller canceled operation"
	case totalCtx.Err() != nil:
		return ReposCodeInventoryTimeout, fmt.Sprintf("total deadline of %d seconds exceeded", ReposTotalSeconds)
	}
	var limit *pinsLimitError
	if errors.As(err, &limit) && limit.Kind == "deadline" {
		return ReposCodeInventoryTimeout, fmt.Sprintf("per-call deadline of %d seconds exceeded", ReposPerRepositorySeconds)
	}
	return "", ""
}

// discoverSubmodules is stage S4. Parents are examined concurrently (at most ReposParallelism at a time);
// the merge into the entry list is sequential in parent order, so concurrency never changes the result.
// It returns the added entries that still need an identity inspection.
func discoverSubmodules(callerCtx, totalCtx context.Context, entries *[]ReposEntry, inspections *[]Result, newRunner reposNewRunner, b reposBounds) []reposPendingSub {
	parentCount := len(*entries)
	var work []int
	for i := 0; i < parentCount; i++ {
		entry := &(*entries)[i]
		if !entry.Discovery.Requested {
			continue
		}
		// Discovery runs only for explicit parents that passed root and duplicate checks and are Git-owned.
		if entry.Origin != "explicit" || entry.Status == "error" || entry.DuplicateOf != nil || entry.Backend != "git" || entry.ResolvedPath == nil || (*inspections)[i].Repository == nil {
			entry.Discovery.Status = "not-applicable"
			continue
		}
		work = append(work, i)
	}
	results := make([]reposParentDiscovery, parentCount)
	runWorkerPool(work, ReposParallelism, func(i int) {
		results[i] = discoverParent(callerCtx, totalCtx, newRunner, b, *(*entries)[i].ResolvedPath, (*inspections)[i])
	})

	seenPaths := make(map[string]bool)
	for _, e := range *entries {
		if e.ResolvedPath != nil {
			seenPaths[filepath.Clean(*e.ResolvedPath)] = true
		}
	}
	var pending []reposPendingSub
	for _, i := range work {
		entry, d := &(*entries)[i], results[i]
		entry.Discovery.Ignored = d.ignored
		entry.Notes = append(entry.Notes, d.notes...)
		if d.status != "ok" {
			entry.Discovery.Status, entry.Discovery.Reason = d.status, d.reason
			entry.Status, entry.Error = "partial", d.err
			continue
		}
		var kept []reposCandidate
		for _, c := range d.candidates {
			if seenPaths[c.abs] {
				entry.Discovery.Deduplicated++
				continue
			}
			kept = append(kept, c)
		}
		if len(*entries)+len(kept) > ReposMaxEntries {
			entry.Discovery.Status = "limit"
			entry.Discovery.Reason = fmt.Sprintf("total entries would exceed limit of %d", ReposMaxEntries)
			entry.Status = "partial"
			entry.Error = &ReposError{Code: ReposCodeDiscoveryLimit, Message: entry.Discovery.Reason}
			continue
		}
		entry.Discovery.Status = "ok"
		entry.Discovery.Submodules = len(kept)
		parentIdx := entry.Index
		for _, c := range kept {
			abs := c.abs
			sub := ReposEntry{
				Index:        len(*entries),
				Path:         c.rel,
				ResolvedPath: &abs,
				Origin:       "submodule",
				Parent:       &parentIdx,
				Backend:      "unknown",
				Discovery:    ReposDiscovery{Status: "not-applicable", Reason: "discovery is not recursive"},
				Notes:        []string{},
			}
			seenPaths[abs] = true
			switch c.kind {
			case "skipped":
				sub.Status, sub.Error = "skipped", &ReposError{Code: c.code, Message: c.msg}
			case "unsafe":
				sub.Status, sub.Error = "error", &ReposError{Code: c.code, Message: c.msg}
			default:
				sub.Status = "ok"
				pending = append(pending, reposPendingSub{index: sub.Index, cand: c})
			}
			*entries = append(*entries, sub)
			*inspections = append(*inspections, Result{Status: "error"})
		}
	}
	return pending
}

// discoverParent reads the registration of one Git parent and confines every candidate. It is the unit of
// work of the pool: a panic is recovered into this parent's discovery, never into the process.
func discoverParent(callerCtx, totalCtx context.Context, newRunner reposNewRunner, b reposBounds, root string, insp Result) (d reposParentDiscovery) {
	defer func() {
		if r := recover(); r != nil {
			d = reposParentDiscovery{status: "failed", reason: "worker panic during discovery",
				err: &ReposError{Code: ReposCodeMemberFailed, Message: fmt.Sprintf("worker panic during discovery: %v", r)}}
		}
	}()
	// All of this parent's Git calls share ONE per-call bound inside the total (FR-013): a parent with many
	// candidates cannot spend more than that, whatever the number of calls.
	parentCtx, parentCancel := context.WithTimeout(totalCtx, b.call)
	defer parentCancel()
	failed := func(err error, reason string) reposParentDiscovery {
		// What was learned before the failure (ignored paths and their notes) is kept.
		out := reposParentDiscovery{status: "failed", ignored: d.ignored, notes: d.notes}
		code, msg := reposMapError(callerCtx, totalCtx, err)
		if code == "" && err != nil && parentCtx.Err() != nil {
			code, msg = ReposCodeInventoryTimeout, fmt.Sprintf("per-call deadline of %d seconds exceeded", ReposPerRepositorySeconds)
		}
		if code == "" {
			code, msg = ReposCodeDiscoveryFailed, reason
		}
		out.reason, out.err = msg, &ReposError{Code: code, Message: msg}
		return out
	}
	repo := insp.Repository
	// A bare repository has no checkout and an unborn HEAD no registration: nothing to discover, no process.
	if repo.Bare || repo.Head.State == "unborn" {
		return reposParentDiscovery{status: "ok"}
	}
	run := newRunner(parentCtx)
	if run.timeout > b.call {
		run.timeout = b.call
	}
	layout, err := newPinsSuperLayout(run, root)
	if err != nil {
		return failed(err, "cannot read the superproject Git directory layout")
	}
	if want, err := filepath.EvalSymlinks(repo.GitDir); err != nil || filepath.Clean(want) != filepath.Clean(layout.GitDir) {
		return failed(nil, "the superproject Git directory differs from the inspected identity")
	}

	// 1. The HEAD .gitmodules blob must be a small regular file.
	ls, err := run.run(pinsCall{Dir: root, Args: []string{"ls-tree", "-z", "-l", "HEAD", "--", ".gitmodules"}})
	if err != nil {
		return failed(err, "cannot read the HEAD .gitmodules entry")
	}
	if ls.Exit != 0 {
		return failed(nil, "git ls-tree failed for .gitmodules")
	}
	record := strings.TrimRight(string(ls.Stdout), "\x00")
	if record == "" {
		return reposParentDiscovery{status: "ok"}
	}
	meta, name, found := strings.Cut(record, "\t")
	fields := strings.Fields(meta)
	if !found || len(fields) != 4 || name != ".gitmodules" {
		return failed(nil, "malformed ls-tree output for .gitmodules")
	}
	mode, blob, sizeText := fields[0], fields[2], fields[3]
	if mode != "100644" && mode != "100755" {
		return failed(nil, fmt.Sprintf(".gitmodules is not a regular file (mode %s)", mode))
	}
	size, err := strconv.Atoi(sizeText)
	if err != nil { // a missing object shows no size
		return failed(nil, "cannot parse the .gitmodules blob")
	}
	if size > ReposGitmodulesBytes {
		return failed(nil, ".gitmodules exceeds maximum size of 64 KiB")
	}

	// 2. Registered paths: the last value per name; names sharing a path use the lowest name.
	kvs, ok, err := pinsConfigFromObject(run, pinsGitRepo{}, root, blob, ReposGitmodulesBytes)
	if err != nil {
		return failed(err, "cannot parse the .gitmodules blob")
	}
	if !ok {
		return failed(nil, "cannot parse the .gitmodules blob")
	}
	namePath := map[string]string{}
	for _, kv := range kvs {
		if n, variable, ok := splitSubmoduleKey(kv.Key); ok && variable == "path" && kv.HasValue && kv.Value != "" {
			namePath[n] = kv.Value
		}
	}
	names := make([]string, 0, len(namePath))
	for n := range namePath {
		names = append(names, n)
	}
	sort.Strings(names)
	pathName := map[string]string{}
	for _, n := range names {
		if _, dup := pathName[namePath[n]]; !dup {
			pathName[namePath[n]] = n
		}
	}
	paths := make([]string, 0, len(pathName))
	for p := range pathName {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	if len(paths) > ReposMaxDiscoveryCandidates {
		reason := fmt.Sprintf("submodule candidates exceed limit of %d", ReposMaxDiscoveryCandidates)
		return reposParentDiscovery{status: "limit", reason: reason, err: &ReposError{Code: ReposCodeDiscoveryLimit, Message: reason}}
	}

	// extensions.submodulePathConfig lets the local configuration name a Git directory per submodule.
	configured := map[string]string{}
	if layout.PathConfig {
		local, err := run.run(pinsCall{Dir: root, Args: []string{"config", "--local", "--null", "--list", "--no-includes"}, Keep: func(rec []byte) bool {
			k, _, _ := strings.Cut(string(rec), "\n")
			_, variable, ok := splitSubmoduleKey(k)
			return ok && variable == "gitdir"
		}})
		if err != nil {
			return failed(err, "cannot read the superproject configuration")
		}
		if local.Exit != 0 {
			return failed(nil, "cannot read the superproject configuration")
		}
		for _, kv := range parsePinsConfig(local.Stdout) {
			if n, variable, ok := splitSubmoduleKey(kv.Key); ok && variable == "gitdir" && kv.HasValue {
				configured[n] = kv.Value
			}
		}
	}

	// 3. Confirm each path in the index and confine it.
	safety := newPinsSafety(layout, run)
	d.status = "ok"
	ignore := func(p string) {
		d.ignored++
		d.notes = append(d.notes, "discovery-ignored: "+reposEscapePath(p))
	}
	var valid []string
	for _, p := range paths {
		if !isLexicallyValidSubmodulePath(p) {
			ignore(p)
			continue
		}
		valid = append(valid, p)
	}
	// One index query confirms every candidate: a record with mode 160000 and exactly that path.
	gitlinks := map[string]bool{}
	if len(valid) > 0 {
		args := append([]string{"ls-files", "--stage", "-z", "--"}, valid...)
		out, err := run.run(pinsCall{Dir: root, Args: args, Keep: func(rec []byte) bool {
			return strings.HasPrefix(string(rec), "160000 ")
		}})
		switch {
		case err != nil:
			return failed(err, "cannot confirm the registered paths in the index")
		case out.Exit != 0:
			return failed(nil, "git ls-files failed")
		}
		for _, rec := range strings.Split(string(out.Stdout), "\x00") {
			meta, name, ok := strings.Cut(rec, "\t")
			if f := strings.Fields(meta); ok && len(f) == 3 && f[0] == "160000" {
				gitlinks[name] = true
			}
		}
	}
	for _, p := range valid {
		if !gitlinks[p] {
			ignore(p)
			continue
		}
		spec := pinsSubmoduleSpec{Path: p, Name: pathName[p]}
		if g, ok := configured[spec.Name]; ok {
			spec.ConfiguredGitDir = &g
		}
		abs := filepath.Join(layout.WorkTree, filepath.FromSlash(p))
		res, refusal, err := safety.resolve(spec)
		switch {
		case err != nil:
			// A failure of the whole discovery (cancellation, a deadline) ends it; a limit or error inside
			// this one submodule is this submodule's own entry and leaves its siblings alone.
			var limit *pinsLimitError
			switch {
			case parentCtx.Err() != nil:
				return failed(err, "cannot validate a submodule location")
			case errors.As(err, &limit) && limit.Kind == "entries":
				d.candidates = append(d.candidates, reposCandidate{rel: p, abs: abs, kind: "unsafe", code: ReposCodeDiscoveryLimit, msg: "the submodule Git directory has too many entries to validate"})
			case errors.As(err, &limit) && limit.Kind == "deadline":
				return failed(err, "cannot validate a submodule location")
			default:
				d.candidates = append(d.candidates, reposCandidate{rel: p, abs: abs, kind: "unsafe", code: ReposCodePathUnsafe, msg: "the submodule location cannot be validated: " + err.Error()})
			}
		case refusal != nil:
			d.candidates = append(d.candidates, reposCandidate{rel: p, abs: abs, kind: "unsafe", code: ReposCodePathUnsafe, msg: refusal.Message})
		case res.Uninitialized:
			d.candidates = append(d.candidates, reposCandidate{rel: p, abs: abs, kind: "skipped", code: ReposCodeSubmoduleUninitialized, msg: "submodule is uninitialized"})
		default:
			d.candidates = append(d.candidates, reposCandidate{rel: p, abs: abs, kind: "ok", dirs: res.Dirs, safety: safety, parentRoot: root})
		}
	}
	return d
}

// reposIsGitlink reports whether the kept ls-files records name exactly path as a gitlink.
func reposIsGitlink(out []byte, path string) bool {
	for _, rec := range strings.Split(string(out), "\x00") {
		meta, name, ok := strings.Cut(rec, "\t")
		if f := strings.Fields(meta); ok && len(f) == 3 && f[0] == "160000" && name == path {
			return true
		}
	}
	return false
}

// reposEscapePath makes a repository-supplied path safe for a note: no control characters, valid UTF-8 and a
// bounded length (cut at a rune boundary).
func reposEscapePath(p string) string {
	q := strconv.QuoteToASCII(p)
	q = q[1 : len(q)-1]
	const limit = 256
	if len(q) > limit {
		cut := limit
		for cut > 0 && !utf8.RuneStart(q[cut]) {
			cut--
		}
		q = q[:cut] + "..."
	}
	return q
}

// verifyDiscoveredSubmodules is the post-check of FR-008 after the identity inspection: the checkout and
// the Git directory Git reported must be the ones that were validated, and the directories must still be the
// validated ones. A submodule that is the same repository as an earlier entry is a duplicate (FR-005).
func verifyDiscoveredSubmodules(entries []ReposEntry, inspections []Result, pending []reposPendingSub, ids *reposIdentityIndex) {
	for _, p := range pending {
		entry := &entries[p.index]
		if entry.Status == "error" {
			continue
		}
		insp := inspections[p.index]
		unsafe := func(msg string) {
			entry.Status = "error"
			entry.Error = &ReposError{Code: ReposCodePathUnsafe, Message: msg}
		}
		repo := insp.Repository
		switch {
		case insp.Backend != "git" || repo == nil || repo.Worktree == nil:
			unsafe("submodule is not a Git checkout of its own")
		case filepath.Clean(*repo.Worktree) != filepath.Clean(p.cand.dirs.WorkTree):
			unsafe("submodule worktree does not match expected path")
		case filepath.Clean(repo.GitDir) != filepath.Clean(p.cand.dirs.GitDir):
			unsafe("submodule Git directory does not match the validated location")
		default:
			if err := p.cand.safety.recheck(*p.cand.dirs); err != nil {
				unsafe("submodule directories changed after validation: " + err.Error())
				continue
			}
			ids.admit(entry, insp)
		}
	}
}

func isLexicallyValidSubmodulePath(p string) bool {
	if !utf8.ValidString(p) || len(p) == 0 || len(p) > ReposMaxPathBytes {
		return false
	}
	if strings.HasPrefix(p, "-") || strings.HasPrefix(p, "/") {
		return false
	}
	if strings.HasSuffix(p, "/") || strings.ContainsRune(p, 0) {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}
