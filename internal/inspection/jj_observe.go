// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

// JJObservedWorkspace is one registered jj workspace: its registration facts at the pinned
// operation. Primary is true for the workspace that holds the repository (its .jj/repo is a
// directory, not the pointer file of a secondary workspace). Missing means the registered
// directory does not exist.
type JJObservedWorkspace struct {
	Name, Path, CommitID, ChangeID string
	Primary, Missing               bool
}

// JJObservedBookmark is a local bookmark with a single, unconflicted target.
type JJObservedBookmark struct{ Name, CommitID string }

// JJHistory holds the history facts of one commit relative to the base: the commits that are not
// ancestors of the base (counting the commit itself) and the author name of the commit.
type JJHistory struct {
	Ahead  int
	Author string
}

// JJWorkspacesObservation is the read-only evidence of a jj repository at one pinned operation. A commit
// whose history could not be read is absent from History; the caller treats that as incomplete.
type JJWorkspacesObservation struct {
	Expected   JJWorkspaceState
	Workspaces []JJObservedWorkspace
	Bookmarks  []JJObservedBookmark
	History    map[string]JJHistory
}

// ObserveJJWorkspaces reads the workspace registrations, the local bookmarks and the history facts
// of their commits. Every query is the sandboxed, read-only form of the jj session
// (--ignore-working-copy, --at-operation=<observed operation>): no working-copy snapshot is taken
// and no operation is written or merged. The operation is observed before and after and must not
// have changed.
func ObserveJJWorkspaces(ctx context.Context, path, base string) (JJWorkspacesObservation, error) {
	ctx, cancel := context.WithTimeout(ctx, jjTimeout)
	defer cancel()
	o := JJWorkspacesObservation{Workspaces: []JJObservedWorkspace{}, Bookmarks: []JJObservedBookmark{}, History: map[string]JJHistory{}}
	expected, err := ObserveJJWorkspace(ctx, path, base)
	if err != nil {
		return o, err
	}
	o.Expected = expected
	identity := Inspect(ctx, path)
	if identity.Status != "ok" || identity.Backend != "jj" || identity.JJRepository == nil || *identity.JJRepository != expected.Source {
		return o, errJJChanged
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
		records, e := jjInventoryWorkspaceRecords(ctx, identity, registrations)
		if e != nil {
			return e
		}
		if len(refs)+len(records) > inventoryRecordLimit {
			return errors.New("native observation record bound exceeded")
		}
		commits := map[string]bool{}
		for _, r := range refs {
			if r.Remote == nil && r.CommitID != nil && jjCommit(r.CommitID) {
				o.Bookmarks = append(o.Bookmarks, JJObservedBookmark{Name: r.Name, CommitID: *r.CommitID})
				commits[*r.CommitID] = true
			}
		}
		for _, w := range records {
			if !jjCommit(w.CommitID) || !jjChange(w.ChangeID) {
				return errors.New("invalid native workspace identity")
			}
			ow := JJObservedWorkspace{Name: w.Name, CommitID: *w.CommitID, ChangeID: *w.ChangeID, Missing: w.Registration == "missing"}
			if w.Path != nil {
				ow.Path = *w.Path
				ow.Primary = jjHoldsRepository(ow.Path)
			}
			o.Workspaces = append(o.Workspaces, ow)
			commits[ow.CommitID] = true
		}
		sort.Slice(o.Workspaces, func(i, j int) bool { return o.Workspaces[i].Name < o.Workspaces[j].Name })
		sort.Slice(o.Bookmarks, func(i, j int) bool { return o.Bookmarks[i].Name < o.Bookmarks[j].Name })
		for commit := range commits {
			if h, e := jjHistory(s, path, op, commit, expected.BaseCommit); e == nil {
				o.History[commit] = h
			}
		}
		return nil
	})
	if err != nil {
		return JJWorkspacesObservation{Workspaces: []JJObservedWorkspace{}, Bookmarks: []JJObservedBookmark{}, History: map[string]JJHistory{}, Expected: expected}, err
	}
	fresh, err := ObserveJJWorkspace(ctx, path, base)
	if err != nil || !reflect.DeepEqual(fresh, expected) {
		if err == nil {
			err = errJJChanged
		}
		return JJWorkspacesObservation{Workspaces: []JJObservedWorkspace{}, Bookmarks: []JJObservedBookmark{}, History: map[string]JJHistory{}, Expected: expected}, err
	}
	return o, nil
}

// jjHoldsRepository reports whether the workspace directory holds the repository itself: its
// .jj/repo is a directory. A secondary workspace has a pointer file there.
func jjHoldsRepository(workspace string) bool {
	st, err := os.Lstat(filepath.Join(workspace, ".jj", "repo"))
	return err == nil && st.IsDir()
}

// jjHistory counts the commits that are not ancestors of the base, the commit itself included,
// and reads the author name, both at the pinned operation.
func jjHistory(s jjSession, dir, op, commit, base string) (JJHistory, error) {
	expr := "ancestors(commit_id(" + jjLiteral(commit) + ")) ~ ancestors(commit_id(" + jjLiteral(base) + "))"
	out, e := s.read(dir, op, "log", "--revisions", expr, "--no-graph", "--template", jjInventoryIDs)
	if e != nil {
		return JJHistory{}, e
	}
	records, e := jjInventoryRecords(out, 2)
	if e != nil {
		return JJHistory{}, e
	}
	out, e = s.read(dir, op, "log", "--revisions", "commit_id("+jjLiteral(commit)+")", "--no-graph", "--template", `json(self.author().name()) ++ "\n"`)
	if e != nil {
		return JJHistory{}, e
	}
	author, e := scalarRecord(out, 1)
	if e != nil {
		return JJHistory{}, e
	}
	return JJHistory{Ahead: len(records), Author: strings.TrimSpace(author[0])}, nil
}
