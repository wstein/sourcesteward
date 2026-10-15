// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

type JJTidyRecovery struct{ Name, CommitID, ChangeID, OperationID string }
type JJTidyNativeOutcome struct {
	Attempted, Complete  bool
	Effects, Limitations []string
	Expected             *JJWorkspaceState
}

func jjTidyOutcome() JJTidyNativeOutcome {
	return JJTidyNativeOutcome{Effects: []string{}, Limitations: []string{"Native operation checks are sampled, not compare-and-swap; concurrent and ABA changes may be missed.", "Recovery bookmark retains the exact commit/change; no automatic restore, abandonment or recovery cleanup."}}
}

// PrepareJJTidyRecovery creates a native retention root; callers must journal its confirmation before deletion.
func PrepareJJTidyRecovery(ctx context.Context, path string, expected JJWorkspaceState, target JJTidyCandidate, name string) (JJTidyRecovery, JJWorkspaceState, JJTidyNativeOutcome, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	recovery := JJTidyRecovery{Name: name, CommitID: target.CommitID, ChangeID: target.ChangeID, OperationID: expected.Source.OperationID}
	outcome := jjTidyOutcome()
	if !strings.HasPrefix(name, "stw-recovery-") || !validJJTaskName(strings.TrimSuffix(name, "?")) || !strings.HasSuffix(name, "?") {
		return recovery, JJWorkspaceState{}, outcome, errors.New("invalid protected jj recovery name")
	}
	fresh, err := ObserveJJTidy(ctx, path, expected.BaseSelector)
	if err != nil {
		return recovery, JJWorkspaceState{}, outcome, err
	}
	if !reflect.DeepEqual(fresh.Expected, expected) || !jjTidyEligible(fresh.Candidates, target) {
		return recovery, JJWorkspaceState{}, outcome, errJJChanged
	}
	for _, c := range fresh.Candidates {
		if c.Name == name {
			return recovery, JJWorkspaceState{}, outcome, errors.New("jj recovery bookmark already exists")
		}
	}
	post, outcome, err := mutateJJTidy(ctx, path, fresh, target, recovery, false)
	if err == nil {
		recovery.OperationID = post.Source.OperationID
	}
	return recovery, post, outcome, err
}

// DeleteJJTidyBookmark deletes only the exact local name, after native recovery was independently confirmed.
func DeleteJJTidyBookmark(ctx context.Context, path string, expected JJWorkspaceState, target JJTidyCandidate, recovery JJTidyRecovery) (JJTidyNativeOutcome, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	outcome := jjTidyOutcome()
	fresh, err := ObserveJJTidy(ctx, path, expected.BaseSelector)
	if err != nil {
		return outcome, err
	}
	if !reflect.DeepEqual(fresh.Expected, expected) || !jjTidyEligible(fresh.Candidates, target) {
		return outcome, errJJChanged
	}
	if recovery.OperationID != expected.Source.OperationID || recovery.CommitID != target.CommitID || recovery.ChangeID != target.ChangeID || !strings.HasPrefix(recovery.Name, "stw-recovery-") || !validJJTaskName(strings.TrimSuffix(recovery.Name, "?")) || !strings.HasSuffix(recovery.Name, "?") {
		return outcome, errors.New("jj recovery identity does not match the verified operation")
	}
	confirmed := false
	for _, c := range fresh.Candidates {
		if c.Name == recovery.Name && c.CommitID == target.CommitID && c.ChangeID == target.ChangeID && c.Reason == "recovery-reference" {
			confirmed = true
		}
	}
	if !confirmed {
		return outcome, errors.New("exact native recovery bookmark is unavailable")
	}
	_, outcome, err = mutateJJTidy(ctx, path, fresh, target, recovery, true)
	return outcome, err
}

func jjTidyEligible(candidates []JJTidyCandidate, target JJTidyCandidate) bool {
	for _, c := range candidates {
		if c.Name == target.Name && c.CommitID == target.CommitID && c.ChangeID == target.ChangeID && c.Decision == "delete" && c.Proof == "native-ancestry" && len(c.WorkspacePaths) == 0 {
			return true
		}
	}
	return false
}

func mutateJJTidy(ctx context.Context, path string, before JJTidyState, target JJTidyCandidate, recovery JJTidyRecovery, deleting bool) (JJWorkspaceState, JJTidyNativeOutcome, error) {
	outcome := jjTidyOutcome()
	var post JJWorkspaceState
	identity := Inspect(ctx, path)
	if identity.Status != "ok" || identity.JJRepository == nil || *identity.JJRepository != before.Expected.Source {
		return post, outcome, errJJChanged
	}
	err := withJJSession(ctx, path, identity, func(s jjSession, root, metadata, repo string) error {
		observed, e := observeJJ(ctx, root, metadata, repo, func(op string, a ...string) (string, error) { return s.read(path, op, a...) })
		if e != nil {
			return e
		}
		if observed != before.Expected.Source {
			return errJJChanged
		}
		refs, e := s.read(path, observed.OperationID, "bookmark", "list", "--all-remotes", "--template", jjInventoryBookmarks)
		if e != nil {
			return e
		}
		oldRefs, e := jjInventoryReferences(refs)
		if e != nil {
			return e
		}
		roots, e := jjTidyWriteRoots(before.Expected, repo)
		if e != nil {
			return e
		}
		args := []string{"--ignore-working-copy", "--at-operation=@", "bookmark", "create", "--revision", "commit_id(" + jjLiteral(target.CommitID) + ")", "--", jjLiteral(recovery.Name)}
		if deleting {
			args = []string{"--ignore-working-copy", "--at-operation=@", "bookmark", "delete", "--", "exact:" + jjLiteral(target.Name)}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		execution, verify, e := prepareJJTidyContext(s.config, metadata, repo)
		if e != nil {
			return e
		}
		if e = verify(); e != nil {
			return e
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		outcome.Attempted = true
		outcome.Effects = []string{"Shared native jj operation/index/object retention storage may change; source working copy and files are excluded."}
		if deleting {
			outcome.Effects = append(outcome.Effects, "Exact local bookmark deletion may have occurred; its protected native recovery bookmark remains.")
		} else {
			outcome.Effects = append(outcome.Effects, "Predeclared protected native recovery bookmark may have been created.")
		}
		outcome.Effects = append(outcome.Effects, "A private native execution context is prepared from bounded metadata and kept read-only during native execution; it is not registered or initialized as another repository.")
		_, code, e := s.workspaceExecute(execution, roots, args)
		verifyErr := verify()
		if e != nil {
			return errors.Join(e, verifyErr)
		}
		if verifyErr != nil {
			return verifyErr
		}
		if code != 0 {
			return errors.New("native jj tidy command failed; inspect the predeclared recovery identity")
		}
		post, e = ObserveJJWorkspace(ctx, path, before.Expected.BaseSelector)
		if e != nil {
			return e
		}
		if e = jjTidyStableSnapshot(before.Expected, post); e != nil {
			return e
		}
		op, e := s.read(path, "@", "operation", "log", "--limit", "1", "--no-graph", "--template", `json(self.id()) ++ "\n" ++ json(self.parents().map(|o| stringify(o.id()))) ++ "\n"`)
		if e != nil {
			return e
		}
		records, e := jjInventoryRecords(op, 2)
		if e != nil || len(records) != 1 {
			return errors.New("invalid jj operation parent evidence")
		}
		id, e := jjString(records[0][0], false)
		if e != nil || id == nil || *id != post.Source.OperationID {
			return errJJChanged
		}
		var parents []string
		if json.Unmarshal(records[0][1], &parents) != nil {
			return errors.New("invalid jj operation parent list")
		}
		if e = validateJJTidyOperationParents(before.Expected.Source.OperationID, post.Source.OperationID, parents); e != nil {
			return e
		}
		afterRefs, e := s.read(path, post.Source.OperationID, "bookmark", "list", "--all-remotes", "--template", jjInventoryBookmarks)
		if e != nil {
			return e
		}
		newRefs, e := jjInventoryReferences(afterRefs)
		if e != nil {
			return e
		}
		if e = validateJJTidyBookmarkDelta(oldRefs, newRefs, target, recovery, deleting); e != nil {
			return e
		}
		final, e := ObserveJJWorkspace(ctx, path, before.Expected.BaseSelector)
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(final, post) {
			return errJJChanged
		}
		if e = verify(); e != nil {
			return e
		}
		outcome.Complete = true
		outcome.Expected = &post
		return nil
	})
	return post, outcome, err
}

func jjTidyWriteRoots(expected JJWorkspaceState, repo string) ([]string, error) {
	roots := []string{filepath.Join(expected.GitDir, "objects"), filepath.Join(expected.GitDir, "refs", "jj")}
	for _, p := range []string{"index", "op_heads", "op_store", "store/extra", "submodule_store", "workspace_store"} {
		roots = append(roots, filepath.Join(repo, filepath.FromSlash(p)))
	}
	for _, p := range roots {
		if e := metadataDir(p); e != nil {
			return nil, e
		}
	}
	return roots, nil
}

func jjTidyStableSnapshot(before, after JJWorkspaceState) error {
	copy := after
	copy.Source.OperationID = before.Source.OperationID
	if !reflect.DeepEqual(before, copy) {
		return errJJChanged
	}
	return nil
}
func validateJJTidyOperationParents(expected, current string, parents []string) error {
	if !identifier(current, 128, "0123456789abcdef") || current == expected || len(parents) != 1 || parents[0] != expected {
		return errJJChanged
	}
	return nil
}
func validateJJTidyBookmarkDelta(before, after []InventoryReference, target JJTidyCandidate, recovery JJTidyRecovery, deleting bool) error {
	old := []InventoryReference{}
	fresh := []InventoryReference{}
	removed, added := false, false
	for _, r := range before {
		if deleting && r.Remote == nil && r.Name == target.Name {
			if removed || r.CommitID == nil || r.ChangeID == nil || *r.CommitID != target.CommitID || *r.ChangeID != target.ChangeID {
				return errJJChanged
			}
			removed = true
			continue
		}
		old = append(old, r)
	}
	for _, r := range after {
		if !deleting && r.Remote == nil && r.Name == recovery.Name {
			if added || r.CommitID == nil || r.ChangeID == nil || *r.CommitID != target.CommitID || *r.ChangeID != target.ChangeID {
				return errJJChanged
			}
			added = true
			continue
		}
		fresh = append(fresh, r)
	}
	if deleting && !removed || !deleting && !added || !reflect.DeepEqual(old, fresh) {
		return errJJChanged
	}
	return nil
}
