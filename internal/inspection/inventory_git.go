// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

type gitInventorySession struct {
	ctx         context.Context
	run         runner
	flags       []string
	transforms  bool
	filterNames map[string]bool
	format      string
	stream      gitStream
	bounds      inventoryBounds
}

// streamCommand streams one Git command with the same prefix as command.
func (s gitInventorySession) streamCommand(dir string, stdin io.Reader, delim string, each func([]byte) error, args ...string) (int, bool, error) {
	prefix := []string{"-c", "core.untrackedCache=false", "-c", "core.hooksPath=/dev/null"}
	prefix = append(prefix, s.flags...)
	return s.stream(s.ctx, dir, stdin, delim, append(prefix, args...), each)
}

func (s gitInventorySession) command(dir string, args ...string) (string, int, error) {
	prefix := []string{"-c", "core.untrackedCache=false", "-c", "core.hooksPath=/dev/null"}
	prefix = append(prefix, s.flags...)
	return s.run(s.ctx, dir, append(prefix, args...)...)
}
func inventoryGit(ctx context.Context, id Result, o InventoryOptions) InventoryResult {
	bounds := defaultInventoryBounds()
	return inventoryGitBounded(ctx, id, o, runGit, newStreamGit(bounds), bounds)
}
func inventoryGitRun(ctx context.Context, id Result, o InventoryOptions, run runner) InventoryResult {
	bounds := defaultInventoryBounds()
	return inventoryGitBounded(ctx, id, o, run, streamFromRunner(run, newStreamGit(bounds)), bounds)
}
func inventoryGitBounded(ctx context.Context, id Result, o InventoryOptions, run runner, stream gitStream, bounds inventoryBounds) InventoryResult {
	r := newInventoryResult(id)
	if id.Repository == nil {
		return r.fail("git-inventory-failed", "Git identity is unavailable")
	}
	contentLeft := bounds.ContentBudget
	s := gitInventorySession{ctx: ctx, run: run, format: id.Repository.ObjectFormat, stream: stream, bounds: bounds}
	dir := id.InputPath
	if o.Query == "ref-exists" || o.Query == "ancestor" {
		return s.referencePredicate(id, o)
	}
	// The regular expression only anchors the ASCII prefix: a pattern such as ".*" does not match bytes that
	// are invalid in the active locale, so a driver named with such bytes would escape the suffix match.
	// The suffix is matched here, on bytes, and every matching key must be valid UTF-8.
	config, code, err := s.command(dir, "config", "--null", "--name-only", "--get-regexp", `^filter\.`)
	if err != nil || code > 1 {
		return r.fail("git-inventory-failed", "cannot safely suppress configured content helpers")
	}
	if code == 0 {
		if !strings.HasSuffix(config, "\x00") {
			return r.fail("invalid-metadata", "invalid content filter configuration")
		}
		names := map[string]bool{}
		for _, key := range strings.Split(strings.TrimSuffix(config, "\x00"), "\x00") {
			dot := strings.LastIndexByte(key, '.')
			if dot < 7 {
				continue
			}
			switch key[dot+1:] {
			case "clean", "process", "required":
			default:
				continue
			}
			if !utf8.ValidString(key) || strings.ContainsAny(key, "\r\n") {
				return r.fail("invalid-metadata", "invalid content filter configuration")
			}
			names[key[:dot]] = true
		}
		for name := range names {
			s.flags = append(s.flags, "-c", name+".clean=", "-c", name+".process=", "-c", name+".required=false")
		}
		s.filterNames = names
	}
	workraw, c, e := s.command(dir, "worktree", "list", "--porcelain", "-z")
	if e != nil || c != 0 {
		return r.fail("git-inventory-failed", "cannot enumerate registered Git worktrees")
	}
	works, bareEntries, e := parseGitInventoryWorktrees(workraw, s.format)
	if e != nil {
		return r.fail("invalid-metadata", e.Error())
	}
	refs, refDigest, refCode, refMsg := s.scanReferences(dir, true)
	if refCode != "" {
		return r.fail(refCode, refMsg)
	}
	remoteRaw, remoteCode, remoteErr := s.command(dir, "remote")
	if remoteErr != nil || remoteCode != 0 {
		return r.fail("git-inventory-failed", "cannot observe native remote names")
	}
	if !utf8.ValidString(remoteRaw) {
		return r.fail("invalid-metadata", "invalid native remote names")
	}
	remoteNames := strings.Split(strings.TrimSuffix(remoteRaw, "\n"), "\n")
	sort.Slice(remoteNames, func(i, j int) bool { return len(remoteNames[i]) > len(remoteNames[j]) })
	for i := range refs {
		if refs[i].Kind == "remote" {
			for _, name := range remoteNames {
				if name != "" && strings.HasPrefix(refs[i].Name, "refs/remotes/"+name+"/") {
					refs[i].Remote = ptr(name)
					break
				}
			}
		}
	}
	r.References = refs
	requiresRaw := false
	for _, scope := range o.Scopes {
		if scope == "disk" || scope == "tracked" {
			requiresRaw = true
		}
	}
	needsDisk := false
	for _, scope := range o.Scopes {
		if scope != "stored" {
			needsDisk = true
		}
	}
	var currentStatus []gitInventoryStatus
	hardIncomplete, softIncomplete := false, false
	softReasons := []string{}
	inspectedObserved := false
	// CK: describe the checkout instead of its Git directory, before any query-scope decision.
	checkoutMissing := map[int]bool{}
	for i := range works {
		w := &works[i]
		if w.Path == nil || bareEntries[i] || id.Repository.Bare || filepath.Clean(*w.Path) != filepath.Clean(id.Repository.CommonDir) {
			continue
		}
		if candidate, ok := s.checkoutCandidate(id, w); ok {
			w.Path = ptr(candidate)
			w.Name = candidate
		} else {
			checkoutMissing[i] = true
		}
	}
	// The inspected workspace is compared first so that the content budget cannot starve it.
	order := make([]int, 0, len(works))
	for i, w := range works {
		if w.Path != nil && id.Repository.Worktree != nil && *w.Path == *id.Repository.Worktree {
			order = append(order, i)
		}
	}
	for i, w := range works {
		if w.Path == nil || id.Repository.Worktree == nil || *w.Path != *id.Repository.Worktree {
			order = append(order, i)
		}
	}
	statusOmitted, storedOmitted, omittedLower := 0, 0, false
	for _, index := range order {
		w := works[index]
		w.Tracked = unknown("disk state unavailable")
		w.Untracked = unknown("disk state unavailable")
		w.Conflicts = unknown("disk state unavailable")
		if checkoutMissing[index] {
			w.Registration = "present"
			w.Tracked, w.Untracked, w.Conflicts = unknown(txtCheckoutMissing), unknown(txtCheckoutMissing), unknown(txtCheckoutMissing)
			w.Limitations = append(w.Limitations, txtCheckoutMissing)
			if o.Query == "" {
				r.partial(txtCheckoutMissing)
			}
			r.Workspaces = append(r.Workspaces, w)
			continue
		}
		isCurrent := w.Path != nil && id.Repository.Worktree != nil && *w.Path == *id.Repository.Worktree
		if o.Query != "" && w.Path != nil && !isCurrent {
			w.Limitations = append(w.Limitations, "workspace disk state is outside selected predicate scope")
			r.Workspaces = append(r.Workspaces, w)
			continue
		}
		if w.Path == nil {
			r.Workspaces = append(r.Workspaces, w)
			continue
		}
		info, staterr := os.Stat(*w.Path)
		if staterr != nil || !info.IsDir() {
			if os.IsNotExist(staterr) {
				w.Registration = "missing"
			} else {
				w.Registration = "unknown"
			}
			w.Limitations = append(w.Limitations, "registered path is unavailable; no repair attempted")
			r.partial("one or more registered workspace paths are unavailable")
			if isCurrent {
				hardIncomplete = true
			}
			r.Workspaces = append(r.Workspaces, w)
			continue
		}
		var ownership Result
		_, ownerr := ancestors(*w.Path, &ownership)
		if ownerr != nil || len(ownership.Evidence) > 0 {
			w.Manager = "unknown"
			if isCurrent {
				hardIncomplete = true
			}
			if len(ownership.Evidence) > 0 {
				w.Manager = "jj"
			}
			w.Limitations = append(w.Limitations, "foreign or ambiguous metadata; workspace not queried")
			r.partial("registered workspace ownership unavailable")
			r.Workspaces = append(r.Workspaces, w)
			continue
		}
		if bareEntries[index] || id.Repository.Bare && *w.Path == id.Repository.CommonDir {
			w.Registration = "present"
			w.Limitations = append(w.Limitations, "bare repository has no disk checkout")
			r.Workspaces = append(r.Workspaces, w)
			continue
		}
		check := inspect(ctx, *w.Path, run)
		if check.Status != "ok" || check.Backend != "git" || check.Repository == nil || check.Repository.CommonDir != id.Repository.CommonDir {
			w.Manager = check.Backend
			if isCurrent {
				hardIncomplete = true
			}
			w.Limitations = append(w.Limitations, "registered path ownership/identity could not be confirmed")
			r.partial("registered workspace identity unavailable")
			r.Workspaces = append(r.Workspaces, w)
			continue
		}
		if !sameGitInventoryOID(w.CommitID, check.Repository.Head.OID) {
			return r.fail("changed-repository", "registered checkout identity changed during inventory")
		}
		w.Registration = "present"
		transforms, unobserved, transformErr := false, "", error(nil)
		if requiresRaw {
			transforms, unobserved, transformErr = s.workspaceTransforms(*w.Path, nil)
		}
		if transformErr != nil {
			code, msg := scanFailure(transformErr)
			return r.fail(code, msg)
		}
		selectedTransforms := transforms
		if requiresRaw && transforms && len(o.Paths) > 0 {
			selectedTransforms, _, transformErr = s.workspaceTransforms(*w.Path, o.Paths)
			if transformErr != nil {
				code, msg := scanFailure(transformErr)
				return r.fail(code, msg)
			}
		}
		parsed, meta, e := s.workspaceStatus(gitStatusOptions{dir: *w.Path, rawDisk: requiresRaw, scopes: o.Scopes, filters: o.Paths, retain: isCurrent, skipIndex: unobserved, charge: &contentLeft, cutoff: -1})
		if e != nil {
			code, msg := scanFailure(e)
			return r.fail(code, msg)
		}
		_, second, e := s.workspaceStatus(gitStatusOptions{dir: *w.Path, rawDisk: requiresRaw, scopes: o.Scopes, filters: o.Paths, retain: isCurrent, skipIndex: unobserved, cutoff: meta.cutoff})
		if e != nil {
			code, msg := scanFailure(e)
			return r.fail(code, msg)
		}
		if meta.signature != second.signature {
			return r.fail("changed-repository", "Git workspace state changed during inventory")
		}
		w.Tracked = known(meta.tracked)
		if !requiresRaw && !meta.tracked {
			w.Tracked = unknown("raw disk comparison not requested")
		}
		w.Untracked = known(meta.untracked)
		if len(o.Paths) > 0 {
			w.Untracked = unknown("untracked paths outside requested literal selection were not observed")
		}
		if !gitInventoryHasScope(o.Scopes, "untracked") {
			w.Untracked = unknown("untracked scope not requested")
		}
		if meta.untrackedAbort != "" {
			w.Untracked = unknown(meta.untrackedAbort)
			w.Limitations = append(w.Limitations, meta.untrackedAbort)
		}
		w.Conflicts = known(meta.conflicts)
		if meta.indexAbort != "" {
			w.Conflicts = unknown(meta.indexAbort)
			w.Limitations = append(w.Limitations, meta.indexAbort)
			if requiresRaw {
				w.Tracked = unknown(meta.indexAbort)
			}
		}
		if meta.rawUnknown != "" {
			w.Tracked = unknown(meta.rawUnknown)
			w.Limitations = append(w.Limitations, meta.rawUnknown)
		}
		if transforms {
			w.Tracked = unknown("configured transformations suppressed; raw comparisons not authoritative")
			w.Limitations = append(w.Limitations, w.Tracked.Reason)
		}
		if meta.stagedAbort != "" {
			w.Tracked = unknown(meta.stagedAbort)
			w.Limitations = append(w.Limitations, meta.stagedAbort)
		}
		if meta.submodules {
			if w.Tracked.Value == "true" {
				w.Limitations = append(w.Limitations, txtGitlinkTrue)
			} else {
				w.Tracked = unknown(txtGitlink)
				w.Limitations = append(w.Limitations, w.Tracked.Reason)
			}
		}
		if meta.sparse {
			w.Limitations = append(w.Limitations, txtSkipWorktree)
		}
		if isCurrent {
			inspectedObserved = true
			currentStatus = parsed
			statusOmitted += meta.omitted
			omittedLower = omittedLower || meta.lowerBound
			s.transforms = selectedTransforms
			if requiresRaw && (selectedTransforms || meta.selectedRawUnknown != "") {
				hardIncomplete = true
				reason := "configured transformations suppressed; raw comparisons not authoritative"
				if meta.selectedSubmodules {
					reason = txtGitlink
				}
				if meta.selectedRawUnknown != "" {
					reason = meta.selectedRawUnknown
				}
				r.partial(reason)
			} else if requiresRaw && meta.selectedSubmodules {
				softIncomplete = true
				softReasons = append(softReasons, txtGitlink)
			}
			if requiresRaw && meta.indexAbort != "" {
				hardIncomplete = true
				r.partial(meta.indexAbort)
			}
			for _, abort := range []struct {
				reason string
				active bool
			}{
				{meta.stagedAbort, gitInventoryHasScope(o.Scopes, "staged") || requiresRaw},
				{meta.untrackedAbort, gitInventoryHasScope(o.Scopes, "untracked")},
				{meta.ignoredAbort, gitInventoryHasScope(o.Scopes, "ignored")},
			} {
				if abort.reason != "" && abort.active {
					softIncomplete = true
					softReasons = append(softReasons, abort.reason)
				}
			}
		}
		r.Workspaces = append(r.Workspaces, w)
	}
	if o.Query != "" && needsDisk && !id.Repository.Bare && !inspectedObserved {
		hardIncomplete = true
	}
	if needsDisk && id.Repository.Bare {
		hardIncomplete = true
		r.partial("bare repository has no disk or index checkout scopes")
	}
	if needsDisk {
		for _, v := range currentStatus {
			for _, scope := range o.Scopes {
				if v.matches(scope) {
					f := InventoryFile{Path: v.path, OldPath: v.old, Scope: scope, Change: v.change(scope), Limitations: []string{}}
					if v.unknown != "" {
						f.Limitations = append(f.Limitations, v.unknown)
					}
					if s.transforms && (scope == "tracked" || scope == "disk") {
						f.Limitations = append(f.Limitations, "configured transformations suppressed; raw comparison")
					}
					if inventoryPathSelected(f.Path, o.Paths) || (f.OldPath != nil && inventoryPathSelected(*f.OldPath, o.Paths)) {
						r.Files = append(r.Files, f)
					}
				}
			}
		}
	}

	resolve := func(selector string) (*string, error) { return s.resolve(dir, id, refs, selector) }
	var base, revision *string
	if o.Base != "" {
		base, e = resolve(o.Base)
		b := InventoryBase{Name: o.Base, CommitID: base, Provenance: "explicit", Selected: true, Limitations: []string{}}
		if e != nil {
			b.Limitations = append(b.Limitations, e.Error())
			r.partial("explicit base could not be uniquely resolved")
		}
		r.Bases = append(r.Bases, b)
	}
	if o.Revision != "" {
		revision, e = resolve(o.Revision)
		if e != nil {
			r.partial("selected revision could not be uniquely resolved")
		}
	}
	for _, scope := range o.Scopes {
		if scope == "stored" {
			target := id.Repository.Head.OID
			if o.Revision != "" {
				target = revision
			}
			if (o.Base != "" && base == nil) || (o.Revision != "" && revision == nil) {
				r.partial("stored comparison endpoints unavailable")
				hardIncomplete = true
				continue
			}
			stored, e := s.storedFiles(dir, target, base, o.Paths)
			if e != nil {
				if strings.HasPrefix(e.Error(), "unknown stored comparison:") {
					r.partial(e.Error())
					hardIncomplete = true
					continue
				}
				code, msg := scanFailure(e)
				return r.fail(code, msg)
			}
			if stored.abort != "" {
				softIncomplete = true
				softReasons = append(softReasons, stored.abort)
				continue
			}
			storedOmitted += stored.omitted // exact: the stored diff is counted to the end
			r.Files = append(r.Files, stored.files...)
		}
	}
	if e := s.bases(dir, id, &r); e != nil {
		return r.fail("git-inventory-failed", e.Error())
	}
	selectedRemoteRefs := []string{}
	remoteCoverage := len(o.Remotes) > 0
	for _, remote := range o.Remotes {
		found := false
		for _, ref := range refs {
			if ref.Kind == "remote" && ref.Remote != nil && *ref.Remote == remote && ref.CommitID != nil {
				selectedRemoteRefs = append(selectedRemoteRefs, *ref.CommitID)
				found = true
			}
		}
		if !found {
			remoteCoverage = false
			r.partial("selected remote has no sampled tracking-reference coverage")
		}
	}
	// Predicates use the candidate count before any truncation; the listings are cut afterwards (FS-1).
	sortInventoryFiles(r.Files)
	omittedCandidates := statusOmitted + storedOmitted
	candidateTotal := len(r.Files) + omittedCandidates
	positive := 0
	for _, f := range r.Files {
		if f.Change != "unknown" {
			positive++
		}
	}
	filesCut, refsCut := 0, 0
	{
		budget, fits := inventoryRecordBudget(len(r.Workspaces), len(r.Bases))
		if !fits {
			return r.fail("inventory-limit", "inventory record limit exceeded")
		}
		fileKeep, refBudget := fairShare(len(r.Files), totalReferenceCost(r.References), budget)
		filesCut = len(r.Files) - fileKeep
		r.Files = r.Files[:fileKeep]
		if kept := truncateReferences(r.References, refBudget); len(kept) < len(r.References) {
			refsCut = len(r.References) - len(kept)
			r.References = kept
		}
	}
	for i := range r.References {
		ref := &r.References[i]
		if ref.CommitID == nil {
			if ref.Kind == "tag" {
				continue
			}
			return r.fail("invalid-metadata", "reference has no commit identity")
		}
		if o.Revision != "" {
			if revision != nil {
				v, e := s.ancestor(dir, *revision, *ref.CommitID)
				if e != nil {
					return r.fail("git-inventory-failed", e.Error())
				}
				ref.ContainsRevision = known(v)
			} else {
				ref.ContainsRevision = unknown("selected revision unavailable")
			}
		}
		if o.Base != "" {
			if base != nil {
				v, e := s.ancestor(dir, *ref.CommitID, *base)
				if e != nil {
					return r.fail("git-inventory-failed", e.Error())
				}
				ref.MergedIntoBase = known(v)
			} else {
				ref.MergedIntoBase = unknown("selected base unavailable")
			}
		}
		if ref.Kind == "local" && remoteCoverage {
			reachable := false
			for _, target := range selectedRemoteRefs {
				v, e := s.ancestor(dir, *ref.CommitID, target)
				if e != nil {
					return r.fail("git-inventory-failed", e.Error())
				}
				reachable = reachable || v
			}
			ref.LocalOnly = known(!reachable)
			ref.Limitations = append(ref.Limitations, "local-only is sampled ancestry evidence, not proof of never-published work")
		}
	}
	if o.Query != "" {
		q := &InventoryQuery{Predicate: o.Query, Value: "unknown", Reason: "requested observation unavailable", Evidence: []string{}}
		r.Query = q
		switch o.Query {
		case "clean", "dirty":
			decided, dirty := false, false
			switch {
			case !hardIncomplete && !softIncomplete:
				decided, dirty = true, candidateTotal > 0
			case !hardIncomplete && softIncomplete && positive > 0:
				decided, dirty = true, true
			}
			if decided {
				q.Value = known(dirty == (o.Query == "dirty")).Value
				q.Reason = "selected scopes are clean"
				if dirty {
					q.Reason = "selected scopes are dirty"
				}
				q.Evidence = append(q.Evidence, "requested literal paths and scopes inspected without refresh")
			} else {
				for _, reason := range softReasons {
					r.partial(reason)
				}
				r.partial("clean/dirty requires complete selected scope observations")
			}
		}
	}
	queryKnown := r.Query != nil && r.Query.Value != "unknown"
	if o.Query == "" {
		for _, reason := range softReasons {
			r.partial(reason)
		}
	}
	last, c, e := s.command(dir, "worktree", "list", "--porcelain", "-z")
	if e != nil || c != 0 {
		return r.fail("git-inventory-failed", "cannot recheck Git registrations")
	}
	if workraw != last {
		return r.fail("changed-repository", "Git registrations changed during inventory")
	}
	_, lastDigest, lcode, lmsg := s.scanReferences(dir, false)
	if lcode != "" {
		if lcode == "git-inventory-failed" {
			lmsg = "cannot recheck Git references"
		}
		return r.fail(lcode, lmsg)
	}
	if refDigest != lastDigest {
		return r.fail("changed-repository", "Git references changed during inventory")
	}
	sort.Slice(r.Workspaces, func(i, j int) bool { return r.Workspaces[i].Name < r.Workspaces[j].Name })
	sortInventoryFiles(r.Files)
	notesFor := func(extraFiles, extraRefs int, trimmed bool) []string {
		notes := []string{}
		if n := filesCut + extraFiles + omittedCandidates; n > 0 {
			notes = append(notes, txtFilesTruncated(n, omittedLower))
		}
		if n := refsCut + extraRefs; n > 0 {
			notes = append(notes, txtRefsTruncated(n))
		}
		if trimmed {
			notes = append(notes, txtResultTrimmed)
		}
		return notes
	}
	applyTruncationNotes(&r, notesFor(0, 0, false), queryKnown)
	if checkInventoryLimits(r) != nil {
		// DEG-9: trim to the 1 MiB bound and report the records dropped here together with the earlier ones.
		stale := notesFor(0, 0, false)
		applyTruncationNotes(&r, []string{txtResultTrimmed}, queryKnown)
		files, refs := len(r.Files), len(r.References)
		if !fitInventoryBytes(&r) {
			return r.fail("inventory-limit", "inventory result exceeded its safe byte bound without trimming branches or files")
		}
		current := stale
		for {
			kept := r.Boundary.Limitations[:0]
			for _, l := range r.Boundary.Limitations {
				if !hasString(current, l) {
					kept = append(kept, l)
				}
			}
			r.Boundary.Limitations = kept
			current = notesFor(files-len(r.Files), refs-len(r.References), false)
			applyTruncationNotes(&r, current, queryKnown)
			// The updated counts are a few bytes longer; drop one more file until the notes fit.
			if checkInventoryLimits(r) == nil || len(r.Files) == 0 {
				break
			}
			r.Files = r.Files[:len(r.Files)-1]
		}
	}
	if e := checkInventoryLimits(r); e != nil {
		return r.fail("inventory-limit", e.Error())
	}
	if ctx.Err() != nil {
		return r.fail("canceled", "inventory canceled")
	}
	return r
}

// checkoutCandidate implements CK-1: the checkout that stands for a worktree-list entry which is a Git
// directory. The candidate must pass the existing checks before it replaces the entry.
func (s gitInventorySession) checkoutCandidate(id Result, w *InventoryWorkspace) (string, bool) {
	var candidate string
	entry := inspect(s.ctx, *w.Path, s.run)
	if entry.Status == "ok" && entry.Backend == "git" && entry.Repository != nil && entry.Repository.CommonDir == id.Repository.CommonDir && !entry.Repository.LinkedWorktree && entry.Repository.Worktree != nil {
		candidate = *entry.Repository.Worktree
	} else if !id.Repository.LinkedWorktree && id.Repository.Worktree != nil {
		candidate = *id.Repository.Worktree
	}
	if candidate == "" || candidate == *w.Path || !filepath.IsAbs(candidate) {
		return "", false
	}
	info, err := os.Stat(candidate)
	if err != nil || !info.IsDir() {
		return "", false
	}
	var ownership Result
	if _, ownerr := ancestors(candidate, &ownership); ownerr != nil || len(ownership.Evidence) > 0 {
		return "", false
	}
	check := inspect(s.ctx, candidate, s.run)
	if check.Status != "ok" || check.Backend != "git" || check.Repository == nil || check.Repository.CommonDir != id.Repository.CommonDir || check.Repository.Worktree == nil || *check.Repository.Worktree != candidate {
		return "", false
	}
	if !sameGitInventoryOID(w.CommitID, check.Repository.Head.OID) {
		return "", false
	}
	return candidate, true
}

func sortInventoryFiles(files []InventoryFile) {
	sort.Slice(files, func(i, j int) bool {
		if files[i].Path == files[j].Path {
			return files[i].Scope < files[j].Scope
		}
		return files[i].Path < files[j].Path
	})
}

// applyTruncationNotes adds the disclosures of omitted records. In a query run with a known predicate the
// listing is supporting evidence: the note is added without changing status or completeness (QR-1).
func applyTruncationNotes(r *InventoryResult, notes []string, queryKnown bool) {
	for _, note := range notes {
		if queryKnown {
			r.Boundary.Limitations = append(r.Boundary.Limitations, note)
		} else {
			r.partial(note)
		}
	}
}

// fitInventoryBytes trims file records, then tag references from the end, until the encoded result
// fits (DEG-9). Local and remote references are never trimmed: it reports false when the result does
// not fit without files and tags.
func fitInventoryBytes(r *InventoryResult) bool {
	allFiles, allRefs := r.Files, r.References
	fits := func(files, refs int) bool {
		r.Files = allFiles[:files]
		r.References = allRefs[:refs]
		return checkInventoryLimits(*r) == nil
	}
	lo, hi := 0, len(allFiles)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if fits(mid, len(allRefs)) {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	if fits(lo, len(allRefs)) {
		return true
	}
	firstTag := len(allRefs)
	for i, ref := range allRefs {
		if ref.Kind == "tag" {
			firstTag = i
			break
		}
	}
	lo, hi = firstTag, len(allRefs)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if fits(0, mid) {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return fits(0, lo)
}

func sameGitInventoryOID(a, b *string) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}
func gitInventoryOID(v, format string) bool {
	n := 40
	if format == "sha256" {
		n = 64
	}
	return len(v) == n && strings.Trim(v, "0123456789abcdef") == ""
}
func gitInventoryNUL(raw string) ([]string, error) {
	if raw == "" {
		return []string{}, nil
	}
	if !utf8.ValidString(raw) || !strings.HasSuffix(raw, "\x00") {
		return nil, errors.New("invalid NUL-delimited native output")
	}
	return strings.Split(strings.TrimSuffix(raw, "\x00"), "\x00"), nil
}

// parseGitInventoryWorktrees returns the registrations and, in parallel, whether each is the bare
// repository entry (CK-3).
func parseGitInventoryWorktrees(raw, format string) ([]InventoryWorkspace, []bool, error) {
	fields, e := gitInventoryNUL(raw)
	if e != nil {
		return nil, nil, e
	}
	out := []InventoryWorkspace{}
	bare := []bool{}
	var w *InventoryWorkspace
	for _, field := range fields {
		if field == "" {
			w = nil
			continue
		}
		key, val, ok := strings.Cut(field, " ")
		if key == "worktree" {
			if !ok || !filepath.IsAbs(val) {
				return nil, nil, errors.New("invalid registered Git worktree path")
			}
			if len(out) >= inventoryRecordLimit {
				return nil, nil, errors.New("inventory record limit exceeded")
			}
			out = append(out, InventoryWorkspace{Path: ptr(val), Name: val, Manager: "git", Registration: "unknown", TaskOwner: "unknown", Limitations: []string{}})
			bare = append(bare, false)
			w = &out[len(out)-1]
			continue
		}
		if w == nil {
			return nil, nil, errors.New("malformed Git worktree registration")
		}
		switch key {
		case "HEAD":
			if !gitInventoryOID(val, format) {
				return nil, nil, errors.New("invalid registered Git commit")
			}
			if strings.Trim(val, "0") != "" {
				w.CommitID = ptr(val)
			}
		case "branch":
			if !strings.HasPrefix(val, "refs/heads/") {
				return nil, nil, errors.New("invalid registered branch")
			}
		case "bare", "detached", "locked", "prunable":
			if key == "bare" {
				bare[len(bare)-1] = true
			}
			if key == "prunable" || key == "locked" {
				w.Limitations = append(w.Limitations, field)
			}
		default:
			return nil, nil, errors.New("unknown Git registration record")
		}
	}
	return out, bare, nil
}

type gitInventoryStatus struct {
	kind, path, xy string
	old            *string
	submodule      bool
	unknown        string
}

func (v gitInventoryStatus) matches(scope string) bool {
	if v.unknown != "" && (scope == "disk" || scope == "tracked") {
		return true
	}
	switch scope {
	case "tracked":
		return v.kind == "tracked" && v.xy != ".." || v.kind == "conflict"
	case "staged":
		return v.xy != "" && v.xy[0] != '.'
	case "disk":
		return v.xy != "" && v.xy[1] != '.'
	case "untracked", "ignored":
		return v.kind == scope
	}
	return false
}
func (v gitInventoryStatus) change(scope string) string {
	if v.unknown != "" && (scope == "disk" || scope == "tracked") {
		return "unknown"
	}
	if v.kind != "tracked" {
		return v.kind
	}
	code := v.xy[1]
	if scope == "staged" || scope == "tracked" && code == '.' {
		code = v.xy[0]
	}
	switch code {
	case 'A':
		return "added"
	case 'M', 'T':
		return "modified"
	case 'D':
		return "deleted"
	case 'R':
		return "renamed"
	case 'C':
		return "copied"
	case 'U':
		return "conflict"
	}
	return "unknown"
}
func gitInventoryRefMatches(refs []InventoryReference, name string) []InventoryReference {
	out := []InventoryReference{}
	for _, r := range refs {
		if r.Name == name || r.Name == "refs/heads/"+name || r.Name == "refs/tags/"+name || r.Name == "refs/remotes/"+name {
			out = append(out, r)
		}
	}
	return out
}
func (s gitInventorySession) resolve(dir string, id Result, refs []InventoryReference, selector string) (*string, error) {
	if selector == "HEAD" {
		if id.Repository.Head.OID != nil {
			return id.Repository.Head.OID, nil
		}
		return nil, errors.New("unborn HEAD")
	}
	matches := gitInventoryRefMatches(refs, selector)
	if len(matches) == 1 && matches[0].CommitID != nil {
		return matches[0].CommitID, nil
	}
	if len(matches) > 0 {
		return nil, errors.New("ambiguous or non-commit reference")
	}
	if !gitInventoryOID(selector, s.format) {
		return nil, errors.New("selector is not an exact native reference or full commit identity")
	}
	raw, c, e := s.command(dir, "rev-parse", "--verify", selector+"^{commit}")
	if e != nil || c != 0 || terminated(raw) != selector {
		return nil, errors.New("commit identity unavailable")
	}
	return ptr(selector), nil
}
func (s gitInventorySession) ancestor(dir, from, to string) (bool, error) {
	_, c, e := s.command(dir, "merge-base", "--is-ancestor", from, to)
	if e != nil || c > 1 {
		return false, errors.New("cannot observe sampled Git ancestry")
	}
	return c == 0, nil
}
func (s gitInventorySession) bases(dir string, id Result, r *InventoryResult) error {
	candidates := map[string]InventoryBase{}
	for _, ref := range r.References {
		if ref.Kind == "remote" && strings.HasSuffix(ref.Name, "/HEAD") {
			raw, c, e := s.command(dir, "symbolic-ref", "--quiet", ref.Name)
			if e != nil || c > 1 {
				return errors.New("cannot observe remote default reference")
			}
			if c == 0 {
				name := terminated(raw)
				for _, target := range r.References {
					if target.Name == name {
						candidates["remote:"+ref.Name] = InventoryBase{Name: name, CommitID: target.CommitID, Provenance: "sampled-remote-default:" + ref.Name, Limitations: []string{"sampled local symbolic reference; remote not contacted"}}
					}
				}
			}
		}
	}
	if id.Repository.Head.Ref != nil {
		branch := strings.TrimPrefix(*id.Repository.Head.Ref, "refs/heads/")
		raw, c, e := s.command(dir, "config", "--null", "--get-all", "branch."+branch+".merge")
		if e != nil || c > 1 {
			return errors.New("cannot observe configured base candidates")
		}
		if c == 0 {
			values, e := gitInventoryNUL(raw)
			if e != nil {
				return e
			}
			remote, remoteCode, remoteErr := s.command(dir, "config", "--get", "branch."+branch+".remote")
			if remoteErr != nil || remoteCode > 1 {
				return errors.New("cannot observe configured base remote")
			}
			remote = terminated(remote)
			for _, name := range values {
				nativeName := name
				if remoteCode == 0 && remote != "." && strings.HasPrefix(name, "refs/heads/") {
					nativeName = "refs/remotes/" + remote + "/" + strings.TrimPrefix(name, "refs/heads/")
				}
				b := InventoryBase{Name: nativeName, Provenance: "configured:branch." + branch + ".merge", Limitations: []string{}}
				for _, ref := range r.References {
					if ref.Name == nativeName {
						b.CommitID = ref.CommitID
					}
				}
				if b.CommitID == nil {
					b.Limitations = append(b.Limitations, "configured merge reference is not a uniquely resolved local reference")
				}
				candidates["configured:"+name] = b
			}
		}
	}
	keys := make([]string, 0, len(candidates))
	for key := range candidates {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		r.Bases = append(r.Bases, candidates[key])
	}
	if len(candidates) > 1 {
		r.Boundary.Limitations = append(r.Boundary.Limitations, "multiple base candidates: explicit --base selection required; candidates are never automatically chosen")
	}
	return nil
}

// Reference predicates collect no unrelated disk/workspace observations.
func (s gitInventorySession) referencePredicate(id Result, o InventoryOptions) InventoryResult {
	r := newInventoryResult(id)
	q := &InventoryQuery{Predicate: o.Query, Value: "unknown", Reason: "requested native reference evidence unavailable", Evidence: []string{}}
	r.Query = q
	refs, digest, code, msg := s.scanReferences(id.InputPath, true)
	if code != "" {
		if code == "git-inventory-failed" {
			msg = "cannot enumerate Git predicate references"
		}
		return r.fail(code, msg)
	}
	r.References = refs
	if o.Query == "ref-exists" {
		matches := gitInventoryRefMatches(refs, o.Ref)
		switch len(matches) {
		case 0:
			q.Value = "false"
			q.Reason = "reference absent in sampled native references"
		case 1:
			q.Value = "true"
			q.Reason = "reference present in sampled native references"
			q.Evidence = append(q.Evidence, matches[0].Name)
		default:
			q.Reason = "reference selector is ambiguous"
			r.partial(q.Reason)
		}
	} else {
		from, ef := s.resolve(id.InputPath, id, refs, o.From)
		to, et := s.resolve(id.InputPath, id, refs, o.To)
		if ef != nil || et != nil {
			q.Reason = "ancestry requires uniquely resolved native commit identities"
			r.partial(q.Reason)
		} else {
			value, e := s.ancestor(id.InputPath, *from, *to)
			if e != nil {
				return r.fail("git-inventory-failed", e.Error())
			}
			q.Value = known(value).Value
			q.Reason = "sampled native ancestry is " + q.Value
			q.Evidence = []string{*from, *to}
		}
	}
	_, last, lcode, lmsg := s.scanReferences(id.InputPath, false)
	if lcode != "" {
		if lcode == "git-inventory-failed" {
			lmsg = "cannot recheck Git predicate references"
		}
		return r.fail(lcode, lmsg)
	}
	if last != digest {
		return r.fail("changed-repository", "Git references changed during inventory")
	}
	head, e := readHead(s.ctx, s.run, id.InputPath, id.Repository.ObjectFormat)
	if e != nil || head.State != id.Repository.Head.State || !sameGitInventoryOID(head.OID, id.Repository.Head.OID) || !sameGitInventoryOID(head.Ref, id.Repository.Head.Ref) {
		return r.fail("changed-repository", "Git HEAD changed during inventory")
	}
	var notes []string
	if kept := truncateReferences(r.References, inventoryRecordLimit); len(kept) < len(r.References) {
		notes = append(notes, txtRefsTruncated(len(r.References)-len(kept)))
		r.References = kept
	}
	queryKnown := q.Value != "unknown"
	applyTruncationNotes(&r, notes, queryKnown)
	if checkInventoryLimits(r) != nil {
		applyTruncationNotes(&r, []string{txtResultTrimmed}, queryKnown)
		if !fitInventoryBytes(&r) {
			return r.fail("inventory-limit", "inventory result exceeded its safe byte bound without trimming branches")
		}
	}
	if e := checkInventoryLimits(r); e != nil {
		return r.fail("inventory-limit", e.Error())
	}
	return r
}

func gitInventoryHasScope(scopes []string, want string) bool {
	for _, scope := range scopes {
		if scope == want {
			return true
		}
	}
	return false
}

// inventoryRecordBudget is the number of records left for files and references (DEG-10).
func inventoryRecordBudget(workspaces, bases int) (int, bool) {
	left := inventoryRecordLimit - workspaces - bases
	return left, left >= 0
}

func hasString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
