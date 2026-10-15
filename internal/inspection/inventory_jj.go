// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const jjInventoryWorkspaces = `json(self.name()) ++ "\n" ++ json(self.root()) ++ "\n" ++ json(self.target().commit_id()) ++ "\n" ++ json(self.target().change_id()) ++ "\n" ++ json(self.target().conflict()) ++ "\n"`
const jjInventoryBookmarks = `json(self.name()) ++ "\n" ++ json(self.remote()) ++ "\n" ++ json(self.conflict()) ++ "\n" ++ if(self.normal_target(), json(self.normal_target().commit_id()), "null") ++ "\n" ++ if(self.normal_target(), json(self.normal_target().change_id()), "null") ++ "\n" ++ json(self.tracked()) ++ "\n" ++ json(self.added_targets().map(|c| stringify(c.commit_id() ++ ":" ++ c.change_id()))) ++ "\n" ++ json(self.removed_targets().map(|c| stringify(c.commit_id() ++ ":" ++ c.change_id()))) ++ "\n"`
const jjInventoryIDs = `json(self.commit_id()) ++ "\n" ++ json(self.change_id()) ++ "\n"`
const jjInventoryDiff = `json(self.path()) ++ "\n" ++ json(self.status()) ++ "\n" ++ json(self.source().path()) ++ "\n" ++ json(self.target().conflict()) ++ "\n"`

func inventoryJJ(ctx context.Context, identity Result, options InventoryOptions) InventoryResult {
	r := newInventoryResult(identity)
	if identity.JJRepository == nil {
		return r.fail("invalid-metadata", "native jj identity is unavailable")
	}
	err := withJJSession(ctx, identity.InputPath, identity, func(s jjSession, _, _, _ string) error {
		r = observeJJInventory(ctx, identity, options, func(op string, args ...string) (string, error) { return s.read(identity.InputPath, op, args...) })
		return nil
	})
	if err != nil {
		return jjInventoryFailure(&r, ctx, err)
	}
	return r
}

func jjInventoryFailure(r *InventoryResult, ctx context.Context, err error) InventoryResult {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return r.fail("jj-timeout", "jj inventory exceeded its ten-second deadline")
	}
	if ctx.Err() != nil {
		return r.fail("canceled", "inventory canceled")
	}
	var setup *jjSessionError
	if errors.As(err, &setup) {
		return r.fail(setup.code, setup.message)
	}
	if errors.Is(err, errJJOutput) {
		return r.fail("jj-output-limit", "jj inventory output exceeded its command limit")
	}
	if errors.Is(err, errJJChanged) {
		return r.fail("changed-repository", "jj operation or registrations changed during inventory")
	}
	return r.fail("jj-failed", "cannot read native jj inventory safely: "+err.Error())
}

func observeJJInventory(ctx context.Context, identity Result, o InventoryOptions, query func(string, ...string) (string, error)) InventoryResult {
	r := newInventoryResult(identity)
	fail := func(e error) InventoryResult { return jjInventoryFailure(&r, ctx, e) }
	if identity.JJRepository == nil {
		return r.fail("invalid-metadata", "native jj identity unavailable")
	}
	operation := identity.JJRepository.OperationID
	read := func(op string, args ...string) (string, error) {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		out, err := query(op, args...)
		if err != nil {
			return "", err
		}
		if len(out) > outputLimit {
			return "", errJJOutput
		}
		if !utf8.ValidString(out) {
			return "", errors.New("invalid native UTF-8 output")
		}
		return out, nil
	}
	sample := func() error {
		out, e := read("@", "operation", "log", "--limit", "1", "--no-graph", "--template", `json(self.id()) ++ "\n"`)
		if e != nil {
			return e
		}
		ids, e := scalarRecord(out, 1)
		if e != nil || !identifier(ids[0], 128, "0123456789abcdef") {
			return errJJInvalid
		}
		if ids[0] != operation {
			return errJJChanged
		}
		return nil
	}
	if e := sample(); e != nil {
		return fail(e)
	}
	bookmarks, e := read(operation, "bookmark", "list", "--all-remotes", "--template", jjInventoryBookmarks)
	if e != nil {
		return fail(e)
	}
	refs, e := jjInventoryReferences(bookmarks)
	if e != nil {
		return fail(e)
	}
	resolve := func(selector string) (*InventoryTarget, error) {
		return jjInventoryResolve(selector, identity.JJRepository, refs, func(expr string) (string, error) {
			return read(operation, "log", "--revisions", expr, "--no-graph", "--template", jjInventoryIDs)
		})
	}
	ancestor := func(from, to string) (InventoryObservation, error) {
		expr := "commit_id(" + jjLiteral(from) + ") & ancestors(commit_id(" + jjLiteral(to) + "))"
		out, e := read(operation, "log", "--revisions", expr, "--no-graph", "--template", jjInventoryIDs)
		if e != nil {
			return InventoryObservation{}, e
		}
		records, e := jjInventoryRecords(out, 2)
		if e != nil || len(records) > 1 {
			return InventoryObservation{}, errors.New("invalid ancestry records")
		}
		if len(records) == 1 {
			got, e1 := jjString(records[0][0], false)
			change, e2 := jjString(records[0][1], false)
			if errors.Join(e1, e2) != nil || !jjCommit(got) || !jjChange(change) || *got != from {
				return InventoryObservation{}, errors.New("invalid ancestry identity")
			}
		}
		return known(len(records) == 1), nil
	}
	if o.Query == "ref-exists" || o.Query == "ancestor" {
		r.Boundary.Limitations = append(r.Boundary.Limitations, "Predicate-only observation; unrelated inventory not requested.")
		r.Query = &InventoryQuery{Predicate: o.Query, Value: "unknown", Evidence: []string{operation}}
		if o.Query == "ref-exists" {
			found := false
			for _, ref := range refs {
				if jjReferenceName(ref) == o.Ref || ref.Remote == nil && ref.Name == o.Ref {
					found = true
					r.Query.Evidence = append(r.Query.Evidence, jjReferenceName(ref))
					for _, target := range ref.AddedTargets {
						r.Query.Evidence = append(r.Query.Evidence, target.CommitID)
					}
				}
			}
			r.Query.Value = fmt.Sprint(found)
		} else {
			from, fromErr := resolve(o.From)
			to, toErr := resolve(o.To)
			if fromErr != nil || toErr != nil {
				return fail(errors.Join(fromErr, toErr))
			}
			if from == nil || to == nil {
				r.Query.Reason = "Ancestry operands do not each resolve to one native revision."
				r.partial(r.Query.Reason)
			} else {
				value, e := ancestor(from.CommitID, to.CommitID)
				if e != nil {
					return fail(e)
				}
				r.Query.Value = value.Value
				r.Query.Evidence = []string{from.CommitID, to.CommitID, operation}
			}
		}
		final, e := read(operation, "bookmark", "list", "--all-remotes", "--template", jjInventoryBookmarks)
		if e != nil {
			return fail(e)
		}
		if final != bookmarks {
			return fail(errJJChanged)
		}
		if e := sample(); e != nil {
			return fail(e)
		}
		return r
	}
	r.References = refs
	if e := checkInventoryLimits(r); e != nil {
		return r.fail("inventory-limit", e.Error())
	}
	workspaces, e := read(operation, "workspace", "list", "--template", jjInventoryWorkspaces)
	if e != nil {
		return fail(e)
	}
	r.Workspaces, e = jjInventoryWorkspaceRecords(ctx, identity, workspaces)
	if e != nil {
		return fail(e)
	}
	for _, w := range r.Workspaces {
		if w.Registration != "present" || w.Manager != "jj" {
			r.partial("One or more registered jj workspaces has unavailable or foreign ownership evidence.")
		}
	}
	var base, revision *InventoryTarget
	if o.Base != "" {
		base, e = resolve(o.Base)
		if e != nil {
			return fail(e)
		}
		candidate := InventoryBase{Name: o.Base, Provenance: "explicit", Selected: true, Limitations: []string{}}
		if base != nil {
			candidate.CommitID = ptr(base.CommitID)
			candidate.ChangeID = base.ChangeID
		} else {
			candidate.Limitations = append(candidate.Limitations, "Selector has no unique native revision.")
			r.partial("Explicit base does not resolve uniquely.")
		}
		r.Bases = append(r.Bases, candidate)
	} else {
		r.Bases = append(r.Bases, InventoryBase{Name: "configured-trunk", Provenance: "isolated-config-unavailable", Limitations: []string{"Repository/user trunk expressions are not evaluated; select an explicit native base."}})
	}
	if o.Revision != "" {
		revision, e = resolve(o.Revision)
		if e != nil {
			return fail(e)
		}
		if revision == nil {
			r.partial("Containment revision does not resolve uniquely.")
		}
	}
	selected := map[string]bool{}
	for _, remote := range o.Remotes {
		selected[remote] = true
	}
	remoteTargets := []string{}
	covered := map[string]bool{}
	ambiguousRemote := false
	for _, ref := range refs {
		if ref.Remote != nil && *ref.Remote != "git" && selected[*ref.Remote] {
			covered[*ref.Remote] = true
			if ref.CommitID == nil {
				ambiguousRemote = true
			}
			for _, t := range ref.AddedTargets {
				remoteTargets = append(remoteTargets, t.CommitID)
			}
		}
	}
	for _, remote := range o.Remotes {
		if remote == "git" || !covered[remote] {
			ambiguousRemote = true
			r.partial("Selected remote has no complete sampled external bookmark coverage: " + remote)
		}
	}
	for i := range r.References {
		ref := &r.References[i]
		if ref.CommitID == nil {
			if o.Base != "" || o.Revision != "" || len(o.Remotes) > 0 {
				r.partial("Conflicted or deleted bookmark has no unique revision for requested reachability.")
			}
			continue
		}
		if o.Base != "" && base != nil {
			ref.MergedIntoBase, e = ancestor(*ref.CommitID, base.CommitID)
			if e != nil {
				return fail(e)
			}
		}
		if o.Revision != "" && revision != nil {
			ref.ContainsRevision, e = ancestor(revision.CommitID, *ref.CommitID)
			if e != nil {
				return fail(e)
			}
		}
		if ref.Remote == nil && len(o.Remotes) > 0 && !ambiguousRemote {
			ref.LocalOnly = known(true)
			for _, target := range remoteTargets {
				a, e := ancestor(*ref.CommitID, target)
				if e != nil {
					return fail(e)
				}
				if a.Value == "true" {
					ref.LocalOnly = known(false)
					break
				}
			}
		}
	}
	scopes := o.Scopes
	if len(scopes) == 0 {
		scopes = []string{"disk", "tracked", "untracked"}
	}
	scopeSet := map[string]bool{}
	for _, scope := range scopes {
		scopeSet[scope] = true
	}
	for _, scope := range scopes {
		if scope == "staged" {
			r.partial("jj staged scope is unsupported: jj has no Git index.")
		} else if scope != "stored" {
			r.partial("jj " + scope + " scope is unavailable without snapshotting; stored changes do not prove disk state.")
		}
	}
	if scopeSet["stored"] {
		selectedRevision := &InventoryTarget{identity.JJRepository.CommitID, ptr(identity.JJRepository.ChangeID)}
		if o.Revision != "" {
			selectedRevision = revision
		}
		args := []string{"diff", "--template", jjInventoryDiff}
		if selectedRevision == nil {
			args = nil
		} else if o.Base != "" {
			if base != nil {
				args = append(args, "--from", "commit_id("+jjLiteral(base.CommitID)+")", "--to", "commit_id("+jjLiteral(selectedRevision.CommitID)+")")
			} else {
				args = nil
			}
		} else {
			args = append(args, "--revisions", "commit_id("+jjLiteral(selectedRevision.CommitID)+")")
		}
		if args != nil {
			args = append(args, "--")
			// Native filtering by only the old name can convert a rename into
			// a deletion. Apply literal old/new filters after bounded collection.
			out, e := read(operation, args...)
			if e != nil {
				return fail(e)
			}
			r.Files, e = jjInventoryFiles(out, o.Paths)
			if e != nil {
				return fail(e)
			}
		}
	}
	if o.Query == "clean" || o.Query == "dirty" {
		r.Query = &InventoryQuery{Predicate: o.Query, Value: "unknown", Reason: "Requested file-state scopes are incomplete.", Evidence: []string{identity.JJRepository.CommitID, operation}}
		if r.Boundary.Complete {
			dirty := len(r.Files) > 0
			if o.Query == "clean" {
				dirty = !dirty
			}
			r.Query.Value = fmt.Sprint(dirty)
			r.Query.Reason = "Stored native revision delta; unsnapshotted disk state is excluded."
		}
	}
	if e := checkInventoryLimits(r); e != nil {
		return r.fail("inventory-limit", e.Error())
	}
	finalBookmarks, e := read(operation, "bookmark", "list", "--all-remotes", "--template", jjInventoryBookmarks)
	if e != nil {
		return fail(e)
	}
	finalWorkspaces, e := read(operation, "workspace", "list", "--template", jjInventoryWorkspaces)
	if e != nil {
		return fail(e)
	}
	finalWorkspaceRecords, e := jjInventoryWorkspaceRecords(ctx, identity, finalWorkspaces)
	if e != nil {
		return fail(e)
	}
	if finalBookmarks != bookmarks || finalWorkspaces != workspaces || !reflect.DeepEqual(r.Workspaces, finalWorkspaceRecords) {
		return fail(errJJChanged)
	}
	if e := sample(); e != nil {
		return fail(e)
	}
	return r
}

func jjReferenceName(ref InventoryReference) string {
	if ref.Remote != nil {
		return ref.Name + "@" + *ref.Remote
	}
	return ref.Name
}

func jjInventoryRecords(out string, width int) ([][]json.RawMessage, error) {
	if !utf8.ValidString(out) || width < 1 {
		return nil, errors.New("invalid native record encoding")
	}
	if out == "" {
		return [][]json.RawMessage{}, nil
	}
	if len(out) > outputLimit {
		return nil, errJJOutput
	}
	lines := strings.Split(out, "\n")
	if lines[len(lines)-1] != "" || (len(lines)-1)%width != 0 {
		return nil, errors.New("invalid native record count")
	}
	count := (len(lines) - 1) / width
	if count > inventoryRecordLimit {
		return nil, errors.New("native record limit exceeded")
	}
	records := make([][]json.RawMessage, 0, count)
	for n := 0; n < count; n++ {
		record := make([]json.RawMessage, width)
		for i := range record {
			v := lines[n*width+i]
			if !json.Valid([]byte(v)) {
				return nil, errors.New("invalid native JSON scalar")
			}
			record[i] = json.RawMessage(v)
		}
		records = append(records, record)
	}
	return records, nil
}
func jjString(v json.RawMessage, nullable bool) (*string, error) {
	if string(v) == "null" {
		if nullable {
			return nil, nil
		}
		return nil, errors.New("unexpected null native string")
	}
	var s string
	if len(v) == 0 || v[0] != '"' || json.Unmarshal(v, &s) != nil || !utf8.ValidString(s) || strings.ContainsRune(s, 0) || !jjValidJSONUnicode(v) {
		return nil, errors.New("invalid native string")
	}
	return &s, nil
}
func jjBool(v json.RawMessage) (bool, error) {
	if string(v) != "true" && string(v) != "false" {
		return false, errors.New("invalid native boolean")
	}
	return string(v) == "true", nil
}
func jjCommit(v *string) bool { return v != nil && identifier(*v, 40, "0123456789abcdef") }
func jjChange(v *string) bool { return v != nil && identifier(*v, 32, "klmnopqrstuvwxyz") }
func jjTargets(v json.RawMessage) ([]InventoryTarget, error) {
	if len(v) == 0 || v[0] != '[' {
		return nil, errors.New("invalid native target list")
	}
	var ids []string
	if e := json.Unmarshal(v, &ids); e != nil {
		return nil, e
	}
	if len(ids) > inventoryRecordLimit {
		return nil, errors.New("native target limit exceeded")
	}
	out := []InventoryTarget{}
	for _, encoded := range ids {
		id, change, ok := strings.Cut(encoded, ":")
		if !ok || !identifier(id, 40, "0123456789abcdef") || !identifier(change, 32, "klmnopqrstuvwxyz") {
			return nil, errors.New("invalid native target identity")
		}
		out = append(out, InventoryTarget{CommitID: id, ChangeID: ptr(change)})
	}
	return out, nil
}
func jjInventoryReferences(out string) ([]InventoryReference, error) {
	records, e := jjInventoryRecords(out, 8)
	if e != nil {
		return nil, e
	}
	refs := []InventoryReference{}
	for _, record := range records {
		name, e1 := jjString(record[0], false)
		remote, e2 := jjString(record[1], true)
		conflict, e3 := jjBool(record[2])
		commit, e4 := jjString(record[3], true)
		change, e5 := jjString(record[4], true)
		tracked, e6 := jjBool(record[5])
		added, e7 := jjTargets(record[6])
		removed, e8 := jjTargets(record[7])
		if errors.Join(e1, e2, e3, e4, e5, e6, e7, e8) != nil || *name == "" || remote != nil && *remote == "" || (commit == nil) != (change == nil) || commit != nil && (!jjCommit(commit) || !jjChange(change)) || conflict && commit != nil {
			return nil, errors.New("invalid native bookmark record")
		}
		ref := InventoryReference{Name: *name, Kind: "local", Remote: remote, CommitID: commit, ChangeID: change, Upstreams: []string{}, AddedTargets: added, RemovedTargets: removed, Tracked: known(tracked), ContainsRevision: unknown("No containment revision selected."), MergedIntoBase: unknown("No explicit base selected."), LocalOnly: unknown("No complete external remote selection."), Limitations: []string{}}
		if remote != nil {
			ref.Kind = "remote"
			if *remote == "git" {
				ref.Limitations = append(ref.Limitations, "Local Git backing-store bookmark; excluded from external remote reachability.")
			}
		}
		if conflict {
			ref.Limitations = append(ref.Limitations, "Conflicted bookmark; added and removed targets are distinct native evidence.")
		}
		refs = append(refs, ref)
	}
	for i := range refs {
		if refs[i].Remote != nil {
			continue
		}
		for _, remote := range refs {
			if remote.Remote != nil && *remote.Remote != "git" && remote.Name == refs[i].Name && remote.Tracked.Value == "true" {
				refs[i].Upstreams = append(refs[i].Upstreams, jjReferenceName(remote))
			}
		}
		sort.Strings(refs[i].Upstreams)
		if len(refs[i].Upstreams) == 1 {
			refs[i].Upstream = ptr(refs[i].Upstreams[0])
		}
	}
	sort.Slice(refs, func(i, j int) bool { return jjReferenceName(refs[i]) < jjReferenceName(refs[j]) })
	return refs, nil
}
func jjInventoryResolve(selector string, identity *JJRepository, refs []InventoryReference, query func(string) (string, error)) (*InventoryTarget, error) {
	if selector == "@" {
		return &InventoryTarget{identity.CommitID, ptr(identity.ChangeID)}, nil
	}
	var expr string
	if identifier(selector, 40, "0123456789abcdef") {
		expr = "commit_id(" + jjLiteral(selector) + ")"
	} else if identifier(selector, 32, "klmnopqrstuvwxyz") {
		expr = "change_id(" + jjLiteral(selector) + ")"
	} else {
		matches := []InventoryReference{}
		for _, ref := range refs {
			if jjReferenceName(ref) == selector {
				matches = append(matches, ref)
			}
		}
		if len(matches) != 1 || matches[0].CommitID == nil {
			return nil, nil
		}
		return &InventoryTarget{*matches[0].CommitID, matches[0].ChangeID}, nil
	}
	out, e := query(expr)
	if e != nil {
		return nil, e
	}
	records, e := jjInventoryRecords(out, 2)
	if e != nil {
		return nil, e
	}
	if len(records) != 1 {
		return nil, nil
	}
	commit, e1 := jjString(records[0][0], false)
	change, e2 := jjString(records[0][1], false)
	if errors.Join(e1, e2) != nil || !jjCommit(commit) || !jjChange(change) {
		return nil, errors.New("invalid native revision identity")
	}
	return &InventoryTarget{*commit, change}, nil
}
func jjInventoryWorkspaceRecords(ctx context.Context, identity Result, out string) ([]InventoryWorkspace, error) {
	records, e := jjInventoryRecords(out, 5)
	if e != nil {
		return nil, e
	}
	ws := []InventoryWorkspace{}
	for _, record := range records {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		name, e1 := jjString(record[0], false)
		path, e2 := jjString(record[1], true)
		commit, e3 := jjString(record[2], false)
		change, e4 := jjString(record[3], false)
		_, e5 := jjBool(record[4])
		if errors.Join(e1, e2, e3, e4, e5) != nil || *name == "" || !jjCommit(commit) || !jjChange(change) || path != nil && !filepath.IsAbs(*path) {
			return nil, errors.New("invalid native workspace record")
		}
		w := InventoryWorkspace{Path: path, Name: *name, Manager: "unknown", Registration: "unknown", TaskOwner: "unknown", CommitID: commit, ChangeID: change, OperationID: ptr(identity.JJRepository.OperationID), Tracked: unknown(jjLimitation), Untracked: unknown(jjLimitation), Conflicts: unknown(jjLimitation), Limitations: []string{jjLimitation}}
		if path != nil {
			st, err := os.Stat(*path)
			if os.IsNotExist(err) {
				w.Registration = "missing"
				w.Limitations = append(w.Limitations, "Registered workspace path is missing; no repair attempted.")
			} else if err == nil && st.IsDir() {
				w.Registration = "present"
				metadata := filepath.Join(*path, ".jj")
				repo, err := resolveJJRepository(metadata)
				if err == nil {
					repo, err = filepath.EvalSymlinks(repo)
					if err == nil && repo == identity.JJRepository.RepositoryDir {
						detection := Result{Evidence: []Evidence{{Kind: "jj-metadata", Location: metadata}}}
						if err := detectJJGitButler(ctx, *path, &detection); err != nil {
							return nil, err
						}
						managed(&detection)
						w.Manager = detection.Backend
					}
				}
				if w.Manager != "jj" {
					w.Limitations = append(w.Limitations, "Registered path ownership does not safely match the selected jj repository; no foreign backend queried.")
				}
			} else {
				w.Limitations = append(w.Limitations, "Registered workspace path cannot be inspected safely.")
			}
		} else {
			w.Limitations = append(w.Limitations, "Native registration has no available workspace path.")
		}
		ws = append(ws, w)
	}
	sort.Slice(ws, func(i, j int) bool { return ws[i].Name < ws[j].Name })
	return ws, nil
}
func jjInventoryFiles(out string, filters []string) ([]InventoryFile, error) {
	records, e := jjInventoryRecords(out, 4)
	if e != nil {
		return nil, e
	}
	files := []InventoryFile{}
	for _, record := range records {
		path, e1 := jjString(record[0], false)
		change, e2 := jjString(record[1], false)
		old, e3 := jjString(record[2], false)
		conflict, e4 := jjBool(record[3])
		if errors.Join(e1, e2, e3, e4) != nil || !jjInventoryFilePath(*path) || !jjInventoryFilePath(*old) {
			return nil, errors.New("invalid native diff record")
		}
		if *change == "removed" {
			*change = "deleted"
		}
		switch *change {
		case "added", "modified", "deleted", "renamed", "copied":
		default:
			return nil, errors.New("invalid native diff change")
		}
		if !inventoryPathSelected(*path, filters) && !inventoryPathSelected(*old, filters) {
			continue
		}
		file := InventoryFile{Path: *path, Scope: "stored", Change: *change, Limitations: []string{"Stored revision delta; disk changes were not snapshotted."}}
		if *change == "renamed" || *change == "copied" {
			file.OldPath = old
		}
		if conflict {
			file.Change = "conflict"
		}
		files = append(files, file)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}
func jjInventoryFilePath(path string) bool {
	clean := filepath.ToSlash(filepath.Clean(path))
	return path != "" && clean != "." && clean == path && !filepath.IsAbs(path) && clean != ".." && !strings.HasPrefix(clean, "../")
}

// encoding/json replaces lone UTF-16 surrogates; reject them rather than lose a name.
func jjValidJSONUnicode(raw []byte) bool {
	for i := 1; i < len(raw)-1; i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if i >= len(raw)-1 {
			return false
		}
		if raw[i] != 'u' {
			continue
		}
		if i+4 >= len(raw) {
			return false
		}
		value, e := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		if e != nil {
			return false
		}
		i += 4
		if value >= 0xdc00 && value <= 0xdfff {
			return false
		}
		if value >= 0xd800 && value <= 0xdbff {
			if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
				return false
			}
			low, e := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
			if e != nil || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return true
}
