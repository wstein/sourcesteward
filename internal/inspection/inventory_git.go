// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
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
}

func (s gitInventorySession) command(dir string, args ...string) (string, int, error) {
	prefix := []string{"-c", "core.untrackedCache=false", "-c", "core.hooksPath=/dev/null"}
	prefix = append(prefix, s.flags...)
	return s.run(s.ctx, dir, append(prefix, args...)...)
}
func inventoryGit(ctx context.Context, id Result, o InventoryOptions) InventoryResult {
	return inventoryGitRun(ctx, id, o, runGit)
}
func inventoryGitRun(ctx context.Context, id Result, o InventoryOptions, run runner) InventoryResult {
	r := newInventoryResult(id)
	if id.Repository == nil {
		return r.fail("git-inventory-failed", "Git identity is unavailable")
	}
	s := gitInventorySession{ctx: ctx, run: run, format: id.Repository.ObjectFormat}
	dir := id.InputPath
	config, code, err := s.command(dir, "config", "--null", "--name-only", "--get-regexp", `^filter\..*\.(clean|process|required)$`)
	if err != nil || code > 1 {
		return r.fail("git-inventory-failed", "cannot safely suppress configured content helpers")
	}
	if code == 0 {
		keys, e := gitInventoryNUL(config)
		if e != nil {
			return r.fail("invalid-metadata", "invalid content filter configuration")
		}
		names := map[string]bool{}
		for _, key := range keys {
			dot := strings.LastIndexByte(key, '.')
			if dot < 7 || !utf8.ValidString(key) || strings.ContainsAny(key, "\r\n") {
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
	works, e := parseGitInventoryWorktrees(workraw, s.format)
	if e != nil {
		return r.fail("invalid-metadata", e.Error())
	}
	refraw, c, e := s.command(dir, "for-each-ref", "--format=%(refname)%00%(objectname)%00%(upstream)%00%(symref)%00%(objecttype)%00")
	if e != nil || c != 0 {
		return r.fail("git-inventory-failed", "cannot enumerate Git references")
	}
	refs, e := parseGitInventoryRefs(refraw, s.format)
	if e != nil {
		return r.fail("invalid-metadata", e.Error())
	}
	r.References = refs
	needsDisk := false
	for _, scope := range o.Scopes {
		if scope != "stored" {
			needsDisk = true
		}
	}
	var currentStatus []gitInventoryStatus
	statusComplete := true
	for _, w := range works {
		if len(r.Workspaces)+len(r.References) >= inventoryRecordLimit {
			return r.fail("inventory-limit", "inventory record limit exceeded")
		}
		w.Tracked = unknown("disk state unavailable")
		w.Untracked = unknown("disk state unavailable")
		w.Conflicts = unknown("disk state unavailable")
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
			if id.Repository.Worktree != nil && *w.Path == *id.Repository.Worktree {
				statusComplete = false
			}
			r.Workspaces = append(r.Workspaces, w)
			continue
		}
		var ownership Result
		_, ownerr := ancestors(*w.Path, &ownership)
		if ownerr != nil || len(ownership.Evidence) > 0 {
			w.Manager = "unknown"
			if id.Repository.Worktree != nil && *w.Path == *id.Repository.Worktree {
				statusComplete = false
			}
			if len(ownership.Evidence) > 0 {
				w.Manager = "jj"
			}
			w.Limitations = append(w.Limitations, "foreign or ambiguous metadata; workspace not queried")
			r.partial("registered workspace ownership unavailable")
			r.Workspaces = append(r.Workspaces, w)
			continue
		}
		if id.Repository.Bare && *w.Path == id.Repository.CommonDir {
			w.Registration = "present"
			w.Limitations = append(w.Limitations, "bare repository has no disk checkout")
			r.Workspaces = append(r.Workspaces, w)
			continue
		}
		check := inspect(ctx, *w.Path, run)
		if check.Status != "ok" || check.Backend != "git" || check.Repository == nil || check.Repository.CommonDir != id.Repository.CommonDir {
			w.Manager = check.Backend
			if id.Repository.Worktree != nil && *w.Path == *id.Repository.Worktree {
				statusComplete = false
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
		transforms, transformErr := s.workspaceTransforms(*w.Path)
		if transformErr != nil {
			return r.fail("git-inventory-failed", transformErr.Error())
		}
		parsed, signature, e := s.workspaceStatus(*w.Path)
		if e != nil {
			return r.fail("git-inventory-failed", e.Error())
		}
		second, nextSignature, e := s.workspaceStatus(*w.Path)
		if e != nil {
			return r.fail("git-inventory-failed", e.Error())
		}
		if signature != nextSignature {
			return r.fail("changed-repository", "Git workspace state changed during inventory")
		}
		_ = second

		tracked, untracked, conflicts := false, false, false
		submodules := false
		for _, v := range parsed {
			tracked = tracked || v.kind == "tracked" && v.xy != ".." || v.kind == "conflict"
			untracked = untracked || v.kind == "untracked"
			conflicts = conflicts || v.kind == "conflict"
			submodules = submodules || v.submodule
		}
		w.Tracked = known(tracked)
		w.Untracked = known(untracked)
		w.Conflicts = known(conflicts)
		if transforms {
			w.Tracked = unknown("configured transformations suppressed; raw comparisons not authoritative")
			w.Limitations = append(w.Limitations, w.Tracked.Reason)
		}
		if submodules {
			w.Tracked = unknown("submodule disk state excluded to avoid recursive helpers")
			w.Limitations = append(w.Limitations, w.Tracked.Reason)
		}
		if id.Repository.Worktree != nil && *w.Path == *id.Repository.Worktree {
			currentStatus = parsed
			s.transforms = transforms
			if needsDisk && (transforms || submodules) {
				statusComplete = false
				r.partial(w.Tracked.Reason)
			}
		}
		r.Workspaces = append(r.Workspaces, w)
	}
	if needsDisk && id.Repository.Bare {
		statusComplete = false
		r.partial("bare repository has no disk or index checkout scopes")
	}
	if needsDisk {
		for _, v := range currentStatus {
			for _, scope := range o.Scopes {
				if v.matches(scope) {
					f := InventoryFile{Path: v.path, OldPath: v.old, Scope: scope, Change: v.change(scope), Limitations: []string{}}
					if s.transforms && (scope == "tracked" || scope == "disk") {
						f.Limitations = append(f.Limitations, "configured transformations suppressed; raw comparison")
					}
					if inventoryPathSelected(f.Path, o.Paths) || (f.OldPath != nil && inventoryPathSelected(*f.OldPath, o.Paths)) {
						if len(r.Files)+len(r.References)+len(r.Workspaces) >= inventoryRecordLimit {
							return r.fail("inventory-limit", "inventory record limit exceeded")
						}
						r.Files = append(r.Files, f)
					}
				}
			}
		}
	}
	for _, scope := range o.Scopes {
		if scope == "stored" {
			files, e := s.storedFiles(dir, id.Repository.Head.OID, o.Paths)
			if e != nil {
				return r.fail("git-inventory-failed", e.Error())
			}
			r.Files = append(r.Files, files...)
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
			if statusComplete {
				dirty := len(r.Files) > 0
				q.Value = known(dirty == (o.Query == "dirty")).Value
				q.Reason = ""
				q.Evidence = append(q.Evidence, "requested literal paths and scopes inspected without refresh")
			} else {
				r.partial("clean/dirty requires complete selected scope observations")
			}
		case "ref-exists":
			matches := gitInventoryRefMatches(refs, o.Ref)
			if len(matches) == 0 {
				q.Value = "false"
				q.Reason = ""
			} else if len(matches) == 1 {
				q.Value = "true"
				q.Reason = ""
				q.Evidence = append(q.Evidence, matches[0].Name)
			} else {
				q.Reason = "reference selector is ambiguous"
				r.partial(q.Reason)
			}
		case "ancestor":
			from, ef := resolve(o.From)
			to, et := resolve(o.To)
			if ef != nil || et != nil {
				q.Reason = "ancestry requires uniquely resolved native commit identities"
				r.partial(q.Reason)
			} else {
				value, e := s.ancestor(dir, *from, *to)
				if e != nil {
					return r.fail("git-inventory-failed", e.Error())
				}
				q.Value = known(value).Value
				q.Reason = ""
				q.Evidence = []string{*from, *to}
			}
		}
	}
	last, c, e := s.command(dir, "worktree", "list", "--porcelain", "-z")
	if e != nil || c != 0 {
		return r.fail("git-inventory-failed", "cannot recheck Git registrations")
	}
	if workraw != last {
		return r.fail("changed-repository", "Git registrations changed during inventory")
	}
	last, c, e = s.command(dir, "for-each-ref", "--format=%(refname)%00%(objectname)%00%(upstream)%00%(symref)%00%(objecttype)%00")
	if e != nil || c != 0 {
		return r.fail("git-inventory-failed", "cannot recheck Git references")
	}
	if refraw != last {
		return r.fail("changed-repository", "Git references changed during inventory")
	}
	sort.Slice(r.Workspaces, func(i, j int) bool { return r.Workspaces[i].Name < r.Workspaces[j].Name })
	sort.Slice(r.Files, func(i, j int) bool {
		if r.Files[i].Path == r.Files[j].Path {
			return r.Files[i].Scope < r.Files[j].Scope
		}
		return r.Files[i].Path < r.Files[j].Path
	})
	if e := checkInventoryLimits(r); e != nil {
		return r.fail("inventory-limit", e.Error())
	}
	if ctx.Err() != nil {
		return r.fail("canceled", "inventory canceled")
	}
	return r
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
func parseGitInventoryWorktrees(raw, format string) ([]InventoryWorkspace, error) {
	fields, e := gitInventoryNUL(raw)
	if e != nil {
		return nil, e
	}
	out := []InventoryWorkspace{}
	var w *InventoryWorkspace
	for _, field := range fields {
		if field == "" {
			w = nil
			continue
		}
		key, val, ok := strings.Cut(field, " ")
		if key == "worktree" {
			if !ok || !filepath.IsAbs(val) {
				return nil, errors.New("invalid registered Git worktree path")
			}
			if len(out) >= inventoryRecordLimit {
				return nil, errors.New("inventory record limit exceeded")
			}
			out = append(out, InventoryWorkspace{Path: ptr(val), Name: val, Manager: "git", Registration: "unknown", TaskOwner: "unknown", Limitations: []string{}})
			w = &out[len(out)-1]
			continue
		}
		if w == nil {
			return nil, errors.New("malformed Git worktree registration")
		}
		switch key {
		case "HEAD":
			if !gitInventoryOID(val, format) {
				return nil, errors.New("invalid registered Git commit")
			}
			if strings.Trim(val, "0") != "" {
				w.CommitID = ptr(val)
			}
		case "branch":
			if !strings.HasPrefix(val, "refs/heads/") {
				return nil, errors.New("invalid registered branch")
			}
		case "bare", "detached", "locked", "prunable":
			if key == "prunable" || key == "locked" {
				w.Limitations = append(w.Limitations, field)
			}
		default:
			return nil, errors.New("unknown Git registration record")
		}
	}
	return out, nil
}
func parseGitInventoryRefs(raw, format string) ([]InventoryReference, error) {
	out := []InventoryReference{}
	for raw != "" {
		record, rest, ok := strings.Cut(raw, "\x00\n")
		if !ok || !utf8.ValidString(record) {
			return nil, errors.New("malformed Git reference records")
		}
		raw = rest
		p := strings.Split(record, "\x00")
		if len(p) != 5 || !strings.HasPrefix(p[0], "refs/") || !gitInventoryOID(p[1], format) {
			return nil, errors.New("invalid Git reference identity")
		}
		kind := "tag"
		var remote *string
		if strings.HasPrefix(p[0], "refs/heads/") {
			kind = "local"
		} else if strings.HasPrefix(p[0], "refs/remotes/") {
			kind = "remote"
			name, _, _ := strings.Cut(strings.TrimPrefix(p[0], "refs/remotes/"), "/")
			remote = ptr(name)
		} else if !strings.HasPrefix(p[0], "refs/tags/") {
			continue
		}
		if len(out) >= inventoryRecordLimit {
			return nil, errors.New("inventory record limit exceeded")
		}
		ref := InventoryReference{Name: p[0], Kind: kind, Remote: remote, Upstreams: []string{}, AddedTargets: []InventoryTarget{}, RemovedTargets: []InventoryTarget{}, Tracked: unknown("not a jj tracking relationship"), ContainsRevision: unknown("no revision selected"), MergedIntoBase: unknown("no base selected"), LocalOnly: unknown("no complete explicit remote sample selected"), Limitations: []string{}}
		if p[4] == "commit" {
			ref.CommitID = ptr(p[1])
			ref.AddedTargets = append(ref.AddedTargets, InventoryTarget{CommitID: p[1]})
		} else {
			ref.Limitations = append(ref.Limitations, "non-commit tag object; commit reachability not evaluated")
		}
		if p[2] != "" {
			ref.Upstream = ptr(p[2])
			ref.Upstreams = append(ref.Upstreams, p[2])
		}
		if kind == "local" {
			ref.Tracked = known(p[2] != "")
		}
		out = append(out, ref)
	}
	return out, nil
}

type gitInventoryStatus struct {
	kind, path, xy string
	old            *string
	submodule      bool
}

func parseGitInventoryStatus(raw string) ([]gitInventoryStatus, error) {
	fields, e := gitInventoryNUL(raw)
	if e != nil {
		return nil, e
	}
	out := []gitInventoryStatus{}
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		v := gitInventoryStatus{}
		if len(f) < 3 {
			return nil, errors.New("malformed Git status record")
		}
		switch f[0] {
		case '?', '!':
			if f[1] != ' ' {
				return nil, errors.New("malformed Git path status")
			}
			v.path = f[2:]
			v.kind = "untracked"
			if f[0] == '!' {
				v.kind = "ignored"
			}
		case '1', '2', 'u':
			n := 9
			if f[0] == '2' {
				n = 10
			}
			if f[0] == 'u' {
				n = 11
			}
			p := strings.SplitN(f, " ", n)
			if len(p) != n || len(p[1]) != 2 || strings.Trim(p[1], ".MADRCUT") != "" || len(p[2]) != 4 {
				return nil, errors.New("malformed Git tracked status")
			}
			v.kind = "tracked"
			v.xy = p[1]
			v.path = p[n-1]
			v.submodule = p[2][0] == 'S'
			if f[0] == 'u' {
				v.kind = "conflict"
			}
			if f[0] == '2' {
				i++
				if i >= len(fields) {
					return nil, errors.New("missing rename source")
				}
				v.old = ptr(fields[i])
			}
		default:
			return nil, errors.New("unknown Git status record")
		}
		if v.path == "" || filepath.IsAbs(v.path) || v.path == ".." || strings.HasPrefix(v.path, "../") || v.old != nil && (*v.old == "" || filepath.IsAbs(*v.old) || strings.HasPrefix(*v.old, "../")) {
			return nil, errors.New("invalid native Git path")
		}
		if len(out) >= inventoryRecordLimit {
			return nil, errors.New("inventory record limit exceeded")
		}
		out = append(out, v)
	}
	return out, nil
}
func (v gitInventoryStatus) matches(scope string) bool {
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
func (s gitInventorySession) storedFiles(dir string, oid *string, paths []string) ([]InventoryFile, error) {
	out := []InventoryFile{}
	if oid == nil {
		return out, nil
	}
	parents, c, e := s.command(dir, "rev-list", "--parents", "-n", "1", *oid)
	if e != nil || c != 0 {
		return nil, errors.New("cannot observe stored Git parents")
	}
	p := strings.Fields(parents)
	if len(p) == 0 || p[0] != *oid {
		return nil, errors.New("invalid stored Git parents")
	}
	args := []string{"diff-tree", "--root", "--no-commit-id", "--name-status", "-r", "-z", "-M", "--no-ext-diff", "--no-textconv", *oid}
	if len(p) > 1 {
		if !gitInventoryOID(p[1], s.format) {
			return nil, errors.New("invalid stored Git parent")
		}
		args = []string{"diff", "--name-status", "-z", "-M", "--no-ext-diff", "--no-textconv", "--ignore-submodules=all", p[1], *oid, "--"}
	}
	raw, c, e := s.command(dir, args...)
	if e != nil || c != 0 {
		return nil, errors.New("cannot observe stored Git files")
	}
	records, e := gitInventoryNUL(raw)
	if e != nil {
		return nil, e
	}
	for i := 0; i < len(records); i++ {
		code := records[i]
		if code == "" || i+1 >= len(records) {
			return nil, errors.New("malformed stored Git diff")
		}
		i++
		f := InventoryFile{Path: records[i], Scope: "stored", Limitations: []string{"stored commit delta against first parent, or empty tree for root commits"}}
		switch code[0] {
		case 'A':
			f.Change = "added"
		case 'M', 'T':
			f.Change = "modified"
		case 'D':
			f.Change = "deleted"
		case 'R', 'C':
			f.OldPath = ptr(f.Path)
			i++
			if i >= len(records) {
				return nil, errors.New("malformed stored Git rename")
			}
			f.Path = records[i]
			f.Change = "renamed"
			if code[0] == 'C' {
				f.Change = "copied"
			}
		default:
			return nil, errors.New("unknown stored Git change")
		}
		if !inventoryPathSelected(f.Path, paths) && (f.OldPath == nil || !inventoryPathSelected(*f.OldPath, paths)) {
			continue
		}
		if len(out) >= inventoryRecordLimit {
			return nil, errors.New("inventory record limit exceeded")
		}
		out = append(out, f)
	}
	return out, nil
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
			for _, name := range values {
				b := InventoryBase{Name: name, Provenance: "configured:branch." + branch + ".merge", Limitations: []string{}}
				for _, ref := range r.References {
					if ref.Name == name {
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
		if len(r.Bases)+len(r.References)+len(r.Workspaces) >= inventoryRecordLimit {
			return errors.New("inventory record limit exceeded")
		}
		r.Bases = append(r.Bases, candidates[key])
	}
	if len(candidates) > 1 {
		r.Boundary.Limitations = append(r.Boundary.Limitations, "multiple base candidates: explicit --base selection required; candidates are never automatically chosen")
	}
	return nil
}

// workspaceTransforms inspects literal attributes without running content filters.
func (s gitInventorySession) workspaceTransforms(dir string) (bool, error) {
	if len(s.filterNames) == 0 {
		return false, nil
	}
	raw, c, e := s.command(dir, "ls-files", "--cached", "-z")
	if e != nil || c != 0 {
		return false, errors.New("cannot safely observe content filter usage")
	}
	paths, e := gitInventoryNUL(raw)
	if e != nil {
		return false, e
	}
	if len(paths) > inventoryRecordLimit {
		return false, errors.New("inventory record limit exceeded")
	}
	if len(paths) == 0 {
		return false, nil
	}
	args := []string{"check-attr", "-z", "filter", "--"}
	args = append(args, paths...)
	raw, c, e = s.command(dir, args...)
	if e != nil || c != 0 {
		return false, errors.New("cannot safely observe content attributes")
	}
	values, e := gitInventoryNUL(raw)
	if e != nil || len(values)%3 != 0 {
		return false, errors.New("malformed content attribute observation")
	}
	for i := 0; i < len(values); i += 3 {
		if values[i+1] != "filter" {
			return false, errors.New("invalid content attribute observation")
		}
		if s.filterNames["filter."+values[i+2]] {
			return true, nil
		}
	}
	return false, nil
}

// workspaceStatus never asks Git to hash worktree contents. Content comparisons
// happen here, so even concurrent configuration changes cannot execute filters.
func (s gitInventorySession) workspaceStatus(dir string) ([]gitInventoryStatus, string, error) {
	raw, c, e := s.command(dir, "ls-files", "--stage", "-z")
	if e != nil || c != 0 {
		return nil, "", errors.New("cannot observe Git index")
	}
	fields, e := gitInventoryNUL(raw)
	if e != nil {
		return nil, "", e
	}
	if len(fields) > inventoryRecordLimit {
		return nil, "", errors.New("inventory record limit exceeded")
	}
	out := []gitInventoryStatus{}
	positions := map[string]int{}
	signature := raw
	for _, record := range fields {
		head, path, ok := strings.Cut(record, "\t")
		p := strings.Split(head, " ")
		if !ok || len(p) != 3 || !gitInventoryOID(p[1], s.format) || len(p[0]) != 6 || len(p[2]) != 1 || p[2][0] < '0' || p[2][0] > '3' || path == "" || filepath.IsAbs(path) || strings.HasPrefix(path, "../") {
			return nil, "", errors.New("malformed native Git index record")
		}
		if p[2] != "0" {
			if i, ok := positions[path]; ok {
				out[i].kind = "conflict"
				out[i].xy = "UU"
			} else {
				positions[path] = len(out)
				out = append(out, gitInventoryStatus{kind: "conflict", path: path, xy: "UU"})
			}
			continue
		}
		v := gitInventoryStatus{kind: "tracked", path: path, xy: ".."}
		if p[0] == "160000" {
			v.submodule = true
			positions[path] = len(out)
			out = append(out, v)
			continue
		}
		full := filepath.Join(dir, filepath.FromSlash(path))
		if e := noSymlink(filepath.Dir(full)); e != nil {
			return nil, "", errors.New("tracked path has inaccessible or symlinked parent")
		}
		st, e := os.Lstat(full)
		if os.IsNotExist(e) {
			v.xy = ".D"
			signature += "\x00missing:" + path
		} else if e != nil {
			return nil, "", errors.New("cannot inspect tracked disk path")
		} else {
			data := []byte{}
			if p[0] == "120000" {
				if st.Mode()&os.ModeSymlink == 0 {
					v.xy = ".T"
				} else {
					link, e := os.Readlink(full)
					if e != nil {
						return nil, "", e
					}
					if len(link) > outputLimit {
						return nil, "", errors.New("tracked content limit exceeded")
					}
					data = []byte(link)
				}
			} else if !st.Mode().IsRegular() {
				return nil, "", errors.New("tracked disk path is not a regular file")
			} else {
				f, e := openMetadata(full)
				if e != nil {
					return nil, "", e
				}
				data, e = io.ReadAll(io.LimitReader(f, outputLimit+1))
				after, statErr := f.Stat()
				f.Close()
				if e != nil || statErr != nil {
					return nil, "", errors.New("cannot read tracked disk content")
				}
				if len(data) > outputLimit {
					return nil, "", errors.New("tracked content limit exceeded")
				}
				if st.Size() != after.Size() || !st.ModTime().Equal(after.ModTime()) {
					return nil, "", errors.New("tracked content changed during inventory")
				}
			}
			var h hash.Hash = sha1.New()
			if s.format == "sha256" {
				h = sha256.New()
			}
			fmt.Fprintf(h, "blob %d%c", len(data), 0)
			h.Write(data)
			oid := fmt.Sprintf("%x", h.Sum(nil))
			signature += "\x00" + path + ":" + oid + ":" + st.Mode().String()
			if v.xy != ".T" && oid != p[1] {
				v.xy = ".M"
			}
		}
		positions[path] = len(out)
		out = append(out, v)
	}
	raw, c, e = s.command(dir, "diff", "--cached", "--name-status", "-z", "-M", "--no-ext-diff", "--no-textconv", "--ignore-submodules=all", "--")
	if e != nil || c != 0 {
		return nil, "", errors.New("cannot observe staged Git files")
	}
	signature += raw
	staged, e := parseGitInventoryDiff(raw, "staged")
	if e != nil {
		return nil, "", e
	}
	for _, f := range staged {
		code := byte('M')
		switch f.Change {
		case "added":
			code = 'A'
		case "deleted":
			code = 'D'
		case "renamed":
			code = 'R'
		case "copied":
			code = 'C'
		case "conflict":
			code = 'U'
		}
		if i, ok := positions[f.Path]; ok {
			v := &out[i]
			v.xy = string([]byte{code, v.xy[1]})
			v.old = f.OldPath
		} else {
			positions[f.Path] = len(out)
			out = append(out, gitInventoryStatus{kind: "tracked", path: f.Path, xy: string([]byte{code, '.'}), old: f.OldPath})
		}
	}
	for _, ignored := range []bool{false, true} {
		args := []string{"ls-files", "--others", "--exclude-standard", "-z"}
		kind := "untracked"
		if ignored {
			args = append(args, "--ignored")
			kind = "ignored"
		}
		raw, c, e = s.command(dir, args...)
		if e != nil || c != 0 {
			return nil, "", errors.New("cannot enumerate native Git untracked/ignored paths")
		}
		signature += raw
		paths, e := gitInventoryNUL(raw)
		if e != nil {
			return nil, "", e
		}
		for _, path := range paths {
			if len(out) >= inventoryRecordLimit {
				return nil, "", errors.New("inventory record limit exceeded")
			}
			out = append(out, gitInventoryStatus{kind: kind, path: path})
		}
	}
	// Keep index entries for submodule qualification, but omit unchanged normal files.
	filtered := out[:0]
	for _, v := range out {
		if v.xy != ".." || v.submodule {
			filtered = append(filtered, v)
		}
	}
	return filtered, signature, nil
}
func parseGitInventoryDiff(raw, scope string) ([]InventoryFile, error) {
	records, e := gitInventoryNUL(raw)
	if e != nil {
		return nil, e
	}
	out := []InventoryFile{}
	for i := 0; i < len(records); i++ {
		code := records[i]
		if code == "" || i+1 >= len(records) {
			return nil, errors.New("malformed native Git diff")
		}
		i++
		f := InventoryFile{Path: records[i], Scope: scope, Limitations: []string{}}
		switch code[0] {
		case 'A':
			f.Change = "added"
		case 'M', 'T':
			f.Change = "modified"
		case 'D':
			f.Change = "deleted"
		case 'R', 'C':
			f.OldPath = ptr(f.Path)
			i++
			if i >= len(records) {
				return nil, errors.New("malformed Git rename")
			}
			f.Path = records[i]
			f.Change = "renamed"
			if code[0] == 'C' {
				f.Change = "copied"
			}
		case 'U':
			f.Change = "conflict"
		default:
			return nil, errors.New("unknown Git change")
		}
		if len(out) >= inventoryRecordLimit {
			return nil, errors.New("inventory record limit exceeded")
		}
		out = append(out, f)
	}
	return out, nil
}
