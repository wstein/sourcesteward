// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var ErrJJReleaseCommitUnresolved = errors.New("commit-unresolved")
var ErrJJReleaseLimit = errors.New("release-limit")

// ErrJJReleaseBlobTooLarge identifies only successful blob stdout beyond its bound.
var ErrJJReleaseBlobTooLarge = errors.New("native release blob exceeds size limit")

// JJReleaseReader exposes only fixed native object reads, pinned to one operation.
// It is valid only during the WithJJReleaseTree callback; no command text is accepted.
type JJReleaseReader struct {
	session                  jjSession
	path, op, commit, gitdir string
	entries                  map[string]string
	active                   bool
}

// WithJJReleaseTree preserves the existing sandbox, configuration isolation, snapshot
// suppression and final operation/identity resampling. It never substitutes Git commands.
func WithJJReleaseTree(ctx context.Context, path, commit string, visit func(*JJReleaseReader) error) error {
	if !identifier(commit, 40, "0123456789abcdef") {
		return ErrJJReleaseCommitUnresolved
	}
	ctx, cancel := context.WithTimeout(ctx, jjTimeout)
	defer cancel()
	identity := Inspect(ctx, path)
	if identity.Status != "ok" || identity.Backend != "jj" || identity.JJRepository == nil {
		return errors.New("supported native jj workspace required")
	}
	err := withJJSession(ctx, path, identity, func(s jjSession, root, metadata, repo string) error {
		observed, err := observeJJ(ctx, root, metadata, repo, func(op string, args ...string) (string, error) { return s.read(path, op, args...) })
		if err != nil {
			return err
		}
		if observed != *identity.JJRepository {
			return errJJChanged
		}
		r := &JJReleaseReader{session: s, path: path, op: observed.OperationID, commit: commit, active: true}
		defer func() { r.active = false }()
		ids, err := r.queryIDs("present(commit_id(" + jjLiteral(commit) + "))")
		if err != nil {
			return err
		}
		if len(ids) != 1 || ids[0] != commit {
			return ErrJJReleaseCommitUnresolved
		}
		r.gitdir, err = jjWorkspaceGitDir(repo)
		if err != nil {
			return err
		}
		out, err := s.read(path, r.op, "file", "list", "--revision", r.rev(), "--template", `json(self.path()) ++ "\n" ++ json(self.file_type()) ++ "\n" ++ json(self.conflict()) ++ "\n"`)
		if err != nil {
			return err
		}
		r.entries, err = jjReleaseEntries(out)
		if err != nil {
			return err
		}
		if err := visit(r); err != nil {
			return err
		}
		final, err := observeJJ(ctx, root, metadata, repo, func(op string, args ...string) (string, error) { return s.read(path, op, args...) })
		if err != nil {
			return err
		}
		if final != observed {
			return errJJChanged
		}
		return nil
	})
	if errors.Is(err, errJJOutput) {
		return ErrJJReleaseLimit
	}
	return err
}

func jjReleaseEntries(out string) (map[string]string, error) {
	records, err := jjInventoryRecords(out, 3)
	if err != nil {
		return nil, jjReleaseReadError(err)
	}
	if len(records) > 20000 {
		return nil, ErrJJReleaseLimit
	}
	entries := map[string]string{}
	for _, rec := range records {
		p, e1 := jjString(rec[0], false)
		kind, e2 := jjString(rec[1], false)
		conflict, e3 := jjBool(rec[2])
		if errors.Join(e1, e2, e3) != nil || !jjInventoryFilePath(*p) || entries[*p] != "" || conflict {
			return nil, errors.New("invalid or conflicted native release tree")
		}
		switch *kind {
		case "file", "symlink", "git-submodule", "tree":
		default:
			return nil, errors.New("unsupported native release tree entry")
		}
		entries[*p] = *kind
	}
	return entries, nil
}

func (r *JJReleaseReader) rev() string { return "commit_id(" + jjLiteral(r.commit) + ")" }
func (r *JJReleaseReader) queryIDs(expr string) ([]string, error) {
	out, err := r.session.read(r.path, r.op, "log", "--revisions", expr, "--no-graph", "--template", `json(self.commit_id()) ++ "\n"`)
	if err != nil {
		return nil, jjReleaseReadError(err)
	}
	records, err := jjInventoryRecords(out, 1)
	if err != nil {
		return nil, jjReleaseReadError(err)
	}
	ids := []string{}
	for _, rec := range records {
		id, err := jjString(rec[0], false)
		if err != nil || !jjCommit(id) {
			return nil, errors.New("invalid native release commit")
		}
		ids = append(ids, *id)
	}
	return ids, nil
}

// Entries returns a detached tree listing; directories are implicit in native file paths.
func (r *JJReleaseReader) Entries() (map[string]string, error) {
	if !r.active {
		return nil, errors.New("closed native release session")
	}
	out := map[string]string{}
	for p, k := range r.entries {
		out[p] = k
	}
	return out, nil
}
func (r *JJReleaseReader) Nonempty(path string) (bool, error) {
	if !r.active {
		return false, errors.New("closed native release session")
	}
	if r.entries[path] != "file" {
		return false, nil
	}
	nonempty, code, err := r.session.releaseNonempty(r.path, r.op, r.commit, path)
	if err != nil {
		return false, jjReleaseReadError(err)
	}
	if code != 0 {
		return false, errors.New("cannot read native readiness file")
	}
	return nonempty, nil
}
func (r *JJReleaseReader) Read(path string) ([]byte, error) {
	if !r.active || r.entries[path] != "file" {
		return nil, errors.New("native release path is not a blob")
	}
	var out capture
	code, err := r.session.executeSink(r.path, r.session.bin, releaseFileArgs(r.op, r.commit, path), &out)
	if err != nil {
		return nil, jjReleaseReadError(err)
	}
	if code != 0 {
		return nil, errors.New("cannot read native release blob")
	}
	if out.overflow {
		return nil, ErrJJReleaseBlobTooLarge
	}
	return []byte(out.String()), nil
}
func (r *JJReleaseReader) CommitTime() (string, error) {
	if !r.active {
		return "", errors.New("closed native release session")
	}
	out, err := r.session.read(r.path, r.op, "log", "--revisions", r.rev(), "--no-graph", "--template", `self.committer().timestamp().format("%s") ++ "\n"`)
	if err != nil {
		return "", jjReleaseReadError(err)
	}
	epoch := strings.TrimSuffix(out, "\n")
	if _, err := strconv.ParseInt(epoch, 10, 64); err != nil {
		return "", errors.New("invalid native commit time")
	}
	return epoch, nil
}

// Main reports native ancestry against a locally recorded remote bookmark; no fetch occurs.
func (r *JJReleaseReader) Main(remote string) (commit, reason string, ancestor bool, err error) {
	if !r.active {
		return "", "", false, errors.New("closed native release session")
	}
	out, err := r.session.read(r.path, r.op, "bookmark", "list", "--all-remotes", "--template", jjInventoryBookmarks)
	if err != nil {
		return "", "", false, jjReleaseReadError(err)
	}
	refs, err := jjInventoryReferences(out)
	if err != nil {
		return "", "", false, jjReleaseReadError(err)
	}
	for _, ref := range refs {
		if ref.Name == "main" && ref.Remote != nil && *ref.Remote == remote && ref.CommitID != nil {
			commit = *ref.CommitID
			break
		}
	}
	for _, entry := range []struct{ path, reason string }{{"info/grafts", "grafts-present"}, {"shallow", "shallow-clone"}} {
		_, e := os.Lstat(filepath.Join(r.gitdir, entry.path))
		if e == nil {
			return commit, entry.reason, false, nil
		}
		if !errors.Is(e, os.ErrNotExist) {
			return "", "", false, e
		}
	}
	if commit == "" {
		return "", "main-ref-absent", false, nil
	}
	ids, err := r.queryIDs(r.rev() + " & ancestors(commit_id(" + jjLiteral(commit) + "))")
	if err != nil {
		return "", "", false, jjReleaseReadError(err)
	}
	if len(ids) > 1 || len(ids) == 1 && ids[0] != r.commit {
		return "", "", false, errors.New("invalid native release ancestry")
	}
	return commit, "", len(ids) == 1, nil
}

// nonemptySink retains no content and accepts every byte, including invalid UTF-8.
type nonemptySink struct{ seen bool }

func (s *nonemptySink) Write(data []byte) (int, error) {
	s.seen = s.seen || len(data) > 0
	return len(data), nil
}

// releaseNonempty is a private fixed native read. Only complete successful reads
// can prove nonemptiness; stderr, cancellation and process status remain authoritative.
// The owner-approved readiness path consumes and discards stdout without returning content.
func (s jjSession) releaseNonempty(dir, op, commit, path string) (bool, int, error) {
	var sink nonemptySink
	args := releaseFileArgs(op, commit, path)
	code, err := s.executeSink(dir, s.bin, args, &sink)
	if err != nil || code != 0 {
		return false, code, err
	}
	return sink.seen, code, nil
}

// Callback readers must expose the same typed bound as session setup/finalization.
// Callers can then distinguish a refused limit from an ordinary native read failure.
func jjReleaseReadError(err error) error {
	if errors.Is(err, errJJOutput) {
		return ErrJJReleaseLimit
	}
	return err
}

// releaseFileArgs is shared only by the two fixed native blob readers.
func releaseFileArgs(op, commit, path string) []string {
	return []string{"--no-pager", "--color=never", "--ignore-working-copy", "--at-operation=" + op, "file", "show", "--revision", "commit_id(" + jjLiteral(commit) + ")", "--", "root-file:" + jjLiteral(path)}
}
