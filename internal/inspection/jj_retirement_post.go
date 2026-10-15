// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"errors"
	"reflect"
)

// VerifyJJRetirementPostState validates retained native recovery and absence without loading a forgotten workspace.
func VerifyJJRetirementPostState(ctx context.Context, source string, expected JJWorkspaceState, target JJRepository, recovery JJTidyRecovery) error {
	ctx, cancel := context.WithTimeout(ctx, jjTimeout)
	defer cancel()
	fresh, e := ObserveJJWorkspace(ctx, source, expected.BaseSelector)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(fresh, expected) {
		return errJJChanged
	}
	identity := Inspect(ctx, source)
	if identity.Status != "ok" || identity.JJRepository == nil {
		return errJJChanged
	}
	e = withJJSession(ctx, source, identity, func(s jjSession, _, _, _ string) error {
		out, e := s.read(source, expected.Source.OperationID, "workspace", "list", "--template", jjInventoryWorkspaces)
		if e != nil {
			return e
		}
		records, e := jjInventoryRecords(out, 5)
		if e != nil {
			return e
		}
		for _, r := range records {
			name, e1 := jjString(r[0], false)
			path, e2 := jjString(r[1], true)
			if errors.Join(e1, e2) != nil {
				return errJJInvalid
			}
			if *name == target.WorkspaceName || path != nil && *path == target.WorkspaceRoot {
				return errors.New("retirement registration or path has been reused")
			}
		}
		out, e = s.read(source, expected.Source.OperationID, "bookmark", "list", "--all-remotes", "--template", jjInventoryBookmarks)
		if e != nil {
			return e
		}
		refs, e := jjInventoryReferences(out)
		if e != nil {
			return e
		}
		found := false
		for _, r := range refs {
			if r.Name == recovery.Name {
				if found || r.Remote != nil || r.CommitID == nil || r.ChangeID == nil || *r.CommitID != target.CommitID || *r.ChangeID != target.ChangeID {
					return errors.New("retirement recovery changed")
				}
				found = true
			}
		}
		if !found {
			return errors.New("retirement recovery is unavailable")
		}
		return nil
	})
	if e != nil {
		return e
	}
	fresh, e = ObserveJJWorkspace(ctx, source, expected.BaseSelector)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(fresh, expected) {
		return errJJChanged
	}
	return nil
}
