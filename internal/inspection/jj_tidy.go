// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
)

// JJTidyState contains bounded native evidence, never apply authorization.
type JJTidyState struct {
	Expected               JJWorkspaceState
	Candidates             []JJTidyCandidate
	Limitations            []string
	WorkspacePaths         []string
	WorkspacePathsComplete bool
}
type JJTidyCandidate struct {
	Name, CommitID, ChangeID, Decision, Reason, Proof string
	WorkspacePaths, Limitations                       []string
}

// ObserveJJTidy inspects local bookmark ancestry without snapshotting disk or changing operations.
func ObserveJJTidy(ctx context.Context, path, base string) (JJTidyState, error) {
	ctx, cancel := context.WithTimeout(ctx, jjTimeout)
	defer cancel()
	state := JJTidyState{WorkspacePaths: []string{}, WorkspacePathsComplete: true, Candidates: []JJTidyCandidate{}, Limitations: []string{"Only native ancestry proves integration; squash-equivalence and forge evidence are unsupported.", "Bookmarks with any remote counterpart are retained to avoid recording deletion intent for later push.", "Stored workspace commit/change targets are protected; disk state is not snapshotted or declared clean.", "Operation and ownership checks are sampled; concurrent or ABA changes may be missed."}}
	expected, err := ObserveJJWorkspace(ctx, path, base)
	if err != nil {
		return state, err
	}
	state.Expected = expected
	identity := Inspect(ctx, path)
	if identity.Status != "ok" || identity.Backend != "jj" || identity.JJRepository == nil || *identity.JJRepository != expected.Source {
		return state, errJJChanged
	}
	err = withJJSession(ctx, path, identity, func(s jjSession, _, _, _ string) error {
		op := expected.Source.OperationID
		bookmarks, e := s.read(path, op, "bookmark", "list", "--all-remotes", "--template", jjInventoryBookmarks)
		if e != nil {
			return e
		}
		refs, e := jjInventoryReferences(bookmarks)
		if e != nil {
			return e
		}
		registrations, e := s.read(path, op, "workspace", "list", "--template", jjInventoryWorkspaces)
		if e != nil {
			return e
		}
		if jjWorkspaceDigest(registrations) != expected.RegistrationDigest {
			return errJJChanged
		}
		workspaces, e := jjInventoryWorkspaceRecords(ctx, identity, registrations)
		if e != nil {
			return e
		}
		state.WorkspacePaths, state.WorkspacePathsComplete = jjTidyWorkspacePaths(workspaces)
		if !state.WorkspacePathsComplete {
			state.Limitations = append(state.Limitations, "A native workspace root is unavailable; state-store disjointness cannot be proven.")
		}
		target := InventoryTarget{CommitID: expected.BaseCommit, ChangeID: ptr(expected.BaseChange)}
		state.Candidates, e = jjTidyCandidates(ctx, target, refs, workspaces, func(commit string) (bool, error) {
			expr := "commit_id(" + jjLiteral(commit) + ") & ancestors(commit_id(" + jjLiteral(expected.BaseCommit) + "))"
			out, e := s.read(path, op, "log", "--revisions", expr, "--no-graph", "--template", jjInventoryIDs)
			if e != nil {
				return false, e
			}
			records, e := jjInventoryRecords(out, 2)
			if e != nil || len(records) > 1 {
				return false, errors.New("invalid native tidy ancestry evidence")
			}
			if len(records) == 0 {
				return false, nil
			}
			id, e1 := jjString(records[0][0], false)
			change, e2 := jjString(records[0][1], false)
			if errors.Join(e1, e2) != nil || !jjCommit(id) || !jjChange(change) || *id != commit {
				return false, errors.New("invalid native tidy ancestry identity")
			}
			return true, nil
		})
		return e
	})
	if err != nil {
		state.Candidates = []JJTidyCandidate{}
		return state, err
	}
	fresh, err := ObserveJJWorkspace(ctx, path, base)
	if err != nil || !reflect.DeepEqual(fresh, expected) {
		state.Candidates = []JJTidyCandidate{}
		if err == nil {
			err = errJJChanged
		}
		return state, err
	}
	return state, nil
}

func jjTidyWorkspacePaths(workspaces []InventoryWorkspace) ([]string, bool) {
	paths, complete := []string{}, true
	for _, w := range workspaces {
		if w.Path == nil {
			complete = false
		} else {
			paths = append(paths, *w.Path)
		}
	}
	sort.Strings(paths)
	return paths, complete
}

func jjTidyCandidates(ctx context.Context, base InventoryTarget, refs []InventoryReference, workspaces []InventoryWorkspace, ancestor func(string) (bool, error)) ([]JJTidyCandidate, error) {
	if len(refs)+len(workspaces) > inventoryRecordLimit {
		return nil, errors.New("native tidy record bound exceeded")
	}
	remote := map[string]bool{}
	for _, r := range refs {
		if r.Remote != nil {
			remote[r.Name] = true
		}
	}
	out := []JJTidyCandidate{}
	for _, r := range refs {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if r.Remote != nil {
			continue
		}
		c := JJTidyCandidate{Name: r.Name, Decision: "retain", Reason: "ambiguous-target", Proof: "none", WorkspacePaths: []string{}, Limitations: []string{}}
		if !jjCommit(r.CommitID) || !jjChange(r.ChangeID) {
			out = append(out, c)
			continue
		}
		c.CommitID, c.ChangeID = *r.CommitID, *r.ChangeID
		integrated, e := ancestor(c.CommitID)
		if e != nil {
			return nil, e
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if integrated {
			c.Proof = "native-ancestry"
		}
		used := false
		for _, w := range workspaces {
			if w.CommitID != nil && *w.CommitID == c.CommitID || w.ChangeID != nil && *w.ChangeID == c.ChangeID {
				used = true
				if w.Path != nil {
					c.WorkspacePaths = append(c.WorkspacePaths, *w.Path)
				}
			}
		}
		sort.Strings(c.WorkspacePaths)
		switch {
		case c.CommitID == base.CommitID || base.ChangeID != nil && c.ChangeID == *base.ChangeID:
			c.Reason = "selected-base"
		case strings.HasPrefix(c.Name, "stw-recovery-"):
			c.Reason = "recovery-reference"
		case remote[c.Name]:
			c.Reason = "remote-counterpart"
		case used:
			c.Reason = "workspace-used"
		case !integrated:
			c.Reason = "not-integrated"
		default:
			c.Decision, c.Reason = "delete", "integrated"
		}
		out = append(out, c)
	}
	return out, nil
}
