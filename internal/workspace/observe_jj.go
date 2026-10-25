// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"path/filepath"
	"reflect"
	"sort"
	"time"

	"github.com/wstein/sourcesteward/internal/inspection"
)

// observeJJ is the jj arm of ObserveWorktrees. Every jj query is the pinned, read-only form of the
// inspection session (--ignore-working-copy, --at-operation), so nothing is snapshotted and no
// operation is written. A workspace is compared with the files stored in its working-copy commit
// at the pinned operation, as the jj retirement admission does: a difference is Dirty or
// IgnoredExtras relative to that operation (an edit not yet snapshotted shows as one, which is the
// safe direction). jj has no worktree lock, no branch attached to a workspace and no tracking
// evidence here: HeadRef is empty, nothing is Locked and TrackingSkipped says so.
func (c worktreeObserver) observeJJ(ctx context.Context, o ObserveOptions) (WorktreeObservation, error) {
	// The registrations are read under the base window; the window of the rest of the observation
	// is then set from their count, counted from the same start.
	parent, start := ctx, time.Now()
	ctx, cancel := context.WithDeadline(parent, start.Add(c.windowFor(parent, 0)))
	defer cancel()
	tidy, err := inspection.ObserveJJTidy(ctx, o.Path, o.Base)
	if err != nil {
		return WorktreeObservation{}, err
	}
	reg, err := inspection.ObserveJJWorkspaces(ctx, o.Path, o.Base)
	if err != nil {
		return WorktreeObservation{}, err
	}
	ctx, cancelScaled := context.WithDeadline(parent, start.Add(c.windowFor(parent, len(reg.Workspaces))))
	defer cancelScaled()
	if !reflect.DeepEqual(tidy.Expected, reg.Expected) {
		return WorktreeObservation{}, failure("evidence-changed", "The jj operation changed during observation.")
	}
	out := WorktreeObservation{Backend: "jj", Base: o.Base, BaseOID: reg.Expected.BaseCommit, RepositoryDir: reg.Expected.Source.RepositoryDir, Worktrees: []WorktreeEvidence{}, Branches: []BranchEvidence{}, TrackingRefs: []TrackingRefEvidence{}, TrackingSkipped: "tracking-unsupported-for-jj",
		Limitations: []string{"Evidence is sampled at one jj operation; nothing was changed, no working copy was snapshotted and nothing was fetched."}}
	candidates := append([]inspection.JJTidyCandidate{}, tidy.Candidates...)
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Name < candidates[j].Name })
	observed := map[string]string{}
	for _, b := range reg.Bookmarks {
		observed[b.Name] = b.CommitID
	}
	for i, cand := range candidates {
		out.Branches = append(out.Branches, jjBranch(cand, o.Base, i, reg.History, observed))
	}
	workspaces := append([]inspection.JJObservedWorkspace{}, reg.Workspaces...)
	sort.Slice(workspaces, func(i, j int) bool { return workspaces[i].Path < workspaces[j].Path })
	for i, ws := range workspaces {
		w := jjRegistration(ws)
		switch {
		case w.Incomplete != "":
		case ws.Primary || ws.Missing:
		case i >= observerWorktreeLimit:
			w.Incomplete = "worktree-limit"
		default:
			c.observeJJWorkspace(ctx, o, reg, ws, &w)
		}
		out.Worktrees = append(out.Worktrees, w)
	}
	return out, nil
}

// jjRegistration maps the registration facts of a workspace. jj has no lock and no branch attached
// to a workspace: Locked is false and HeadRef is empty. A workspace whose registered path is
// unknown cannot be observed.
func jjRegistration(ws inspection.JJObservedWorkspace) WorktreeEvidence {
	w := WorktreeEvidence{Path: ws.Path, HeadOID: ws.CommitID, Primary: ws.Primary, DirMissing: ws.Missing, Prunable: ws.Missing}
	switch {
	case ws.Path == "":
		w.Incomplete = "workspace-path-unknown"
	case ws.Foreign:
		w.Primary, w.Incomplete = false, "registration-mismatch"
	}
	return w
}

// jjBranch maps one local bookmark. The native tidy decides whether a bookmark may be deleted: only
// its "delete" decision is a proof; every keep reason (a remote counterpart, a recovery bookmark,
// the selected base or an alias of it, a bookmark a workspace uses) is passed on as Reason, so
// the bookmark is retained and never carries a delete effect. observed is the commit of each
// bookmark in the registrations read; a different commit means the two reads disagree.
func jjBranch(cand inspection.JJTidyCandidate, base string, index int, history map[string]inspection.JJHistory, observed map[string]string) BranchEvidence {
	b := BranchEvidence{Name: cand.Name, OID: cand.CommitID, IsBase: cand.Name == base, Proof: jjProof(cand.Proof)}
	if cand.Decision == "delete" {
		b.Proof = integrationAncestor
	} else if cand.Reason != "not-integrated" && cand.Reason != "ambiguous-target" {
		b.Reason = cand.Reason
	}
	h, ok := history[cand.CommitID]
	switch {
	case index >= observerBranchLimit:
		b.Proof, b.Reason, b.Incomplete = "", "", "branch-limit"
	case cand.CommitID == "":
		b.Proof, b.Reason, b.Incomplete = "", "", "ambiguous-target"
	case observed[cand.Name] != cand.CommitID:
		b.Proof, b.Reason, b.Incomplete = "", "", "evidence-changed"
	case !ok:
		b.Proof, b.Reason, b.Incomplete = "", "", "history-unavailable"
	default:
		b.Ahead, b.AuthorHint = h.Ahead, h.Author
	}
	return b
}

// observeJJWorkspace fills the content facts of one secondary workspace. The workspace holding
// the observation itself cannot be compared with itself and is Incomplete.
func (c worktreeObserver) observeJJWorkspace(ctx context.Context, o ObserveOptions, reg inspection.JJWorkspacesObservation, ws inspection.JJObservedWorkspace, w *WorktreeEvidence) {
	fail := func(reason string) {
		clearContent(w)
		w.Incomplete = reason
		if ctx.Err() != nil && reason == "observation-failed" {
			w.Incomplete = incompleteReason(ctx.Err())
		}
	}
	if ws.Path == reg.Expected.Source.WorkspaceRoot {
		fail("source-workspace")
		return
	}
	state, err := c.retire(ctx, o.Path, o.Base, ws.Path, ws.Name, reg.Expected.Source.RepositoryDir)
	if err != nil {
		fail(incompleteReason(err))
		return
	}
	// The retirement read pins its own operation: every workspace of one observation must have been
	// read at the observation's operation, at its registered working-copy commit.
	if state.Expected.Source.OperationID != reg.Expected.Source.OperationID || state.Target.CommitID != ws.CommitID {
		fail("evidence-changed")
		return
	}
	stored := make([]RetirementStoredFile, 0, len(state.StoredFiles))
	for _, f := range state.StoredFiles {
		stored = append(stored, RetirementStoredFile{Path: f.Path, Digest: f.Digest, Size: f.Size, Executable: f.Executable})
	}
	admission := retirementManifestAdmission{Backend: "jj", NativeMetadataPaths: state.MetadataPaths, StoredFiles: stored}
	_, err = observeRetirementManifest(ctx, ws.Path, admission)
	dirty, extras, reason := manifestOutcome(err)
	if reason != "" {
		fail(reason)
		return
	}
	w.Dirty, w.IgnoredExtras = dirty, extras
	state1 := filepath.Join(ws.Path, ".jj", "working_copy")
	if w.LastActivity, err = newestModTime([]string{filepath.Join(state1, "tree_state"), filepath.Join(state1, "checkout")}, ws.Path, stored); err != nil {
		fail("activity-unreadable")
		return
	}
	h, ok := reg.History[ws.CommitID]
	if !ok {
		fail("history-unavailable")
		return
	}
	w.Proof = jjProof(state.Proof)
	w.Ahead, w.AuthorHint = h.Ahead, h.Author
	// An empty working copy on an integrated parent has no work of its own.
	w.HasOwnCommit = w.Ahead > 0 && w.Proof != "empty-working-copy-integrated-parent"
}

// jjProof maps the jj retirement proof to the observer proof words: native ancestry is the
// ancestor proof, the empty working copy keeps its own word, anything else is not proven.
func jjProof(p string) string {
	switch p {
	case "native-ancestry":
		return integrationAncestor
	case "empty-working-copy-integrated-parent":
		return p
	}
	return "none"
}
