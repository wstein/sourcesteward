// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"
)

func PrepareJJRetirementRecovery(ctx context.Context, source string, expected JJWorkspaceState, target JJRepository, name string) (JJTidyRecovery, JJWorkspaceState, JJTidyNativeOutcome, error) {
	recovery := JJTidyRecovery{name, target.CommitID, target.ChangeID, expected.Source.OperationID}
	post, o, e := mutateJJRetirement(ctx, source, expected, target, recovery, false)
	if e == nil {
		recovery.OperationID = post.Source.OperationID
	}
	return recovery, post, o, e
}
func ForgetJJRetirement(ctx context.Context, source string, expected JJWorkspaceState, target JJRepository, recovery JJTidyRecovery) (JJTidyNativeOutcome, error) {
	_, o, e := mutateJJRetirement(ctx, source, expected, target, recovery, true)
	return o, e
}

func mutateJJRetirement(ctx context.Context, source string, expected JJWorkspaceState, target JJRepository, recovery JJTidyRecovery, forget bool) (JJWorkspaceState, JJTidyNativeOutcome, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	var post JJWorkspaceState
	o := jjTidyOutcome()
	if !strings.HasPrefix(recovery.Name, "stw-recovery-") || !strings.HasSuffix(recovery.Name, "?") || !validJJTaskName(strings.TrimSuffix(recovery.Name, "?")) || !validJJTaskName(target.WorkspaceName) || target.WorkspaceRoot == expected.Source.WorkspaceRoot || target.RepositoryDir != expected.Source.RepositoryDir || recovery.CommitID != target.CommitID || recovery.ChangeID != target.ChangeID {
		return post, o, errors.New("invalid native jj retirement target or recovery identity")
	}
	fresh, e := ObserveJJWorkspace(ctx, source, expected.BaseSelector)
	if e != nil {
		return post, o, e
	}
	if !reflect.DeepEqual(fresh, expected) {
		return post, o, errJJChanged
	}
	observed, e := VerifyJJWorkspace(ctx, source, target.WorkspaceName, target.WorkspaceRoot, target.RepositoryDir)
	if e != nil {
		return post, o, e
	}
	observed.OperationID = target.OperationID
	if observed != target {
		return post, o, errJJChanged
	}
	identity := Inspect(ctx, source)
	if identity.Status != "ok" || identity.JJRepository == nil || *identity.JJRepository != expected.Source {
		return post, o, errJJChanged
	}
	e = withJJSession(ctx, source, identity, func(s jjSession, _, metadata, repo string) error {
		op := expected.Source.OperationID
		beforeRefs, e := s.read(source, op, "bookmark", "list", "--all-remotes", "--template", jjInventoryBookmarks)
		if e != nil {
			return e
		}
		refs, e := jjInventoryReferences(beforeRefs)
		if e != nil {
			return e
		}
		found := false
		for _, r := range refs {
			if r.Name == recovery.Name {
				if !forget || r.Remote != nil || r.CommitID == nil || r.ChangeID == nil || *r.CommitID != target.CommitID || *r.ChangeID != target.ChangeID {
					return errors.New("native retirement recovery collision or mismatch")
				}
				found = true
			}
		}
		if forget && (!found || recovery.OperationID != op) {
			return errors.New("confirmed retirement recovery is unavailable")
		}
		beforeWorkspaces, e := s.read(source, op, "workspace", "list", "--template", jjInventoryWorkspaces)
		if e != nil {
			return e
		}
		if jjWorkspaceDigest(beforeWorkspaces) != expected.RegistrationDigest {
			return errJJChanged
		}
		roots, e := jjTidyWriteRoots(expected, repo)
		if e != nil {
			return e
		}
		execution, verify, e := prepareJJTidyContext(s.config, metadata, repo)
		if e != nil {
			return e
		}
		if e = verify(); e != nil {
			return e
		}
		args := []string{"--ignore-working-copy", "--at-operation=@", "bookmark", "create", "--revision", "commit_id(" + jjLiteral(target.CommitID) + ")", "--", jjLiteral(recovery.Name)}
		if forget {
			args = []string{"--ignore-working-copy", "--at-operation=@", "workspace", "forget", "--", target.WorkspaceName}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		o.Attempted = true
		o.Effects = []string{"Declared shared native jj operation/index/retention stores may change; source Git/WC and target files remain read-only.", "A private read-only native execution context uses bounded copied metadata, without native initialization or registration."}
		if forget {
			o.Effects = append(o.Effects, "Only the exact selected native workspace registration may have been forgotten; files remain present.")
		} else {
			o.Effects = append(o.Effects, "The predeclared Git-invalid native recovery bookmark may have been created.")
		}
		_, code, e := s.workspaceExecute(execution, roots, args)
		v := verify()
		if e != nil {
			return errors.Join(e, v)
		}
		if v != nil {
			return v
		}
		if code != 0 {
			return errors.New("native jj retirement action failed; inspect retained recovery")
		}
		post, e = ObserveJJWorkspace(ctx, source, expected.BaseSelector)
		if e != nil {
			return e
		}
		stable := post
		stable.Source.OperationID = expected.Source.OperationID
		if forget {
			stable.RegistrationDigest = expected.RegistrationDigest
		}
		if !reflect.DeepEqual(stable, expected) {
			return errJJChanged
		}
		opOut, e := s.read(source, "@", "operation", "log", "--limit", "1", "--no-graph", "--template", `json(self.id()) ++ "\n" ++ json(self.parents().map(|o| stringify(o.id()))) ++ "\n"`)
		if e != nil {
			return e
		}
		records, e := jjInventoryRecords(opOut, 2)
		if e != nil || len(records) != 1 {
			return errJJChanged
		}
		id, e := jjString(records[0][0], false)
		var parents []string
		if e != nil || id == nil || *id != post.Source.OperationID || json.Unmarshal(records[0][1], &parents) != nil {
			return errJJChanged
		}
		if e = validateJJTidyOperationParents(op, post.Source.OperationID, parents); e != nil {
			return e
		}
		afterRefs, e := s.read(source, post.Source.OperationID, "bookmark", "list", "--all-remotes", "--template", jjInventoryBookmarks)
		if e != nil {
			return e
		}
		if forget {
			if afterRefs != beforeRefs {
				return errJJChanged
			}
		} else {
			after, e := jjInventoryReferences(afterRefs)
			if e != nil {
				return e
			}
			if e = validateJJTidyBookmarkDelta(refs, after, JJTidyCandidate{CommitID: target.CommitID, ChangeID: target.ChangeID}, recovery, false); e != nil {
				return e
			}
		}
		afterWorkspaces, e := s.read(source, post.Source.OperationID, "workspace", "list", "--template", jjInventoryWorkspaces)
		if e != nil {
			return e
		}
		if forget {
			if e = validateJJRetirementRegistrationDelta(beforeWorkspaces, afterWorkspaces, target); e != nil {
				return e
			}
		} else {
			if beforeWorkspaces != afterWorkspaces {
				return errJJChanged
			}
			freshTarget, e := VerifyJJWorkspace(ctx, source, target.WorkspaceName, target.WorkspaceRoot, repo)
			if e != nil {
				return e
			}
			freshTarget.OperationID = target.OperationID
			if freshTarget != target {
				return errJJChanged
			}
		}
		if jjWorkspaceDigest(afterWorkspaces) != post.RegistrationDigest {
			return errJJChanged
		}
		final, e := ObserveJJWorkspace(ctx, source, expected.BaseSelector)
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(final, post) {
			return errJJChanged
		}
		if e = verify(); e != nil {
			return e
		}
		o.Complete = true
		o.Expected = &post
		return nil
	})
	return post, o, e
}

func validateJJRetirementRegistrationDelta(before, after string, target JJRepository) error {
	old, e := jjInventoryRecords(before, 5)
	if e != nil {
		return e
	}
	fresh, e := jjInventoryRecords(after, 5)
	if e != nil {
		return e
	}
	kept := [][]json.RawMessage{}
	removed := false
	for _, r := range old {
		name, e := jjString(r[0], false)
		if e != nil {
			return e
		}
		if *name == target.WorkspaceName {
			path, e1 := jjString(r[1], false)
			commit, e2 := jjString(r[2], false)
			change, e3 := jjString(r[3], false)
			if removed || errors.Join(e1, e2, e3) != nil || *path != target.WorkspaceRoot || *commit != target.CommitID || *change != target.ChangeID {
				return errJJChanged
			}
			removed = true
			continue
		}
		kept = append(kept, r)
	}
	if !removed || !reflect.DeepEqual(kept, fresh) {
		return errJJChanged
	}
	return nil
}
