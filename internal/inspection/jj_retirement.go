// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
)

type JJRetirementStoredFile struct {
	Path, Digest string
	Size         int64
	Executable   bool
}
type JJRetirementState struct {
	Expected                                   JJWorkspaceState
	Target                                     JJRepository
	StoredFiles                                []JJRetirementStoredFile
	MetadataPaths, WorkspacePaths, Limitations []string
	WorkspacePathsComplete                     bool
	Proof                                      string
}

// ObserveJJRetirement supplies native stored facts; callers must independently prove full physical disk equality.
func ObserveJJRetirement(ctx context.Context, source, base, destination, name, repo string) (JJRetirementState, error) {
	ctx, cancel := context.WithTimeout(ctx, jjTimeout)
	defer cancel()
	r := JJRetirementState{StoredFiles: []JJRetirementStoredFile{}, MetadataPaths: []string{".jj", ".jj/repo", ".jj/working_copy", ".jj/working_copy/checkout", ".jj/working_copy/tree_state", ".jj/working_copy/type"}, WorkspacePaths: []string{}, Limitations: []string{"Stored file facts require an independent complete raw disk comparison, including ignored/untracked and exact manager metadata.", "Only regular stored files up to 64KiB and full sparse layout are supported.", "Sampled native state does not exclude concurrent or ABA changes."}, Proof: "none"}
	state, e := ObserveJJWorkspace(ctx, source, base)
	if e != nil {
		return r, e
	}
	r.Expected = state
	if repo != state.Source.RepositoryDir || destination == state.Source.WorkspaceRoot {
		return r, errors.New("cannot retire the selected source or a foreign jj repository")
	}
	r.Target, e = VerifyJJWorkspace(ctx, source, name, destination, repo)
	if e != nil {
		return r, e
	}
	if r.Target.OperationID != r.Expected.Source.OperationID {
		return r, errJJChanged
	}
	identity := Inspect(ctx, destination)
	if identity.Status != "ok" || identity.JJRepository == nil {
		return r, errJJChanged
	}
	e = withJJSession(ctx, destination, identity, func(s jjSession, _, _, _ string) error {
		op := r.Expected.Source.OperationID
		registrations, e := s.read(destination, op, "workspace", "list", "--template", jjInventoryWorkspaces)
		if e != nil {
			return e
		}
		if jjWorkspaceDigest(registrations) != r.Expected.RegistrationDigest {
			return errJJChanged
		}
		workspaces, e := jjInventoryWorkspaceRecords(ctx, identity, registrations)
		if e != nil {
			return e
		}
		r.WorkspacePaths, r.WorkspacePathsComplete = jjTidyWorkspacePaths(workspaces)
		sparse, e := s.read(destination, op, "sparse", "list")
		if e != nil {
			return e
		}
		if sparse != ".\n" {
			return errors.New("retirement requires a complete full-sparse jj workspace")
		}
		commit := "commit_id(" + jjLiteral(r.Target.CommitID) + ")"
		out, e := s.read(destination, op, "file", "list", "--revision", commit, "--template", `json(self.path()) ++ "\n" ++ json(self.file_type()) ++ "\n" ++ json(self.executable()) ++ "\n" ++ json(self.conflict()) ++ "\n"`)
		if e != nil {
			return e
		}
		records, e := jjInventoryRecords(out, 4)
		if e != nil {
			return e
		}
		for _, record := range records {
			path, e1 := jjString(record[0], false)
			kind, e2 := jjString(record[1], false)
			exec, e3 := jjBool(record[2])
			conflict, e4 := jjBool(record[3])
			if errors.Join(e1, e2, e3, e4) != nil || !jjInventoryFilePath(*path) || *kind != "file" || conflict {
				return errors.New("unsupported or malformed native retirement tree entry")
			}
			content, code, e := s.query(destination, op, "file", "show", "--revision", commit, "--", "root-file:"+jjLiteral(*path))
			if e != nil {
				return e
			}
			if code != 0 {
				return errors.New("cannot read complete raw native stored file")
			}
			hash := sha256.Sum256([]byte(content))
			r.StoredFiles = append(r.StoredFiles, JJRetirementStoredFile{*path, hex.EncodeToString(hash[:]), int64(len(content)), exec})
		}
		ancestry := func(from string) (bool, error) {
			out, e := s.read(destination, op, "log", "--revisions", "commit_id("+jjLiteral(from)+") & ancestors(commit_id("+jjLiteral(r.Expected.BaseCommit)+"))", "--no-graph", "--template", jjInventoryIDs)
			if e != nil {
				return false, e
			}
			records, e := jjInventoryRecords(out, 2)
			if e != nil || len(records) > 1 {
				return false, errors.New("invalid retirement ancestry evidence")
			}
			if len(records) == 0 {
				return false, nil
			}
			id, e1 := jjString(records[0][0], false)
			change, e2 := jjString(records[0][1], false)
			if errors.Join(e1, e2) != nil || !jjCommit(id) || !jjChange(change) || *id != from {
				return false, errors.New("invalid retirement ancestry identity")
			}
			return true, nil
		}
		integrated, e := ancestry(r.Target.CommitID)
		if e != nil {
			return e
		}
		if integrated {
			r.Proof = "native-ancestry"
			return nil
		}
		out, e = s.read(destination, op, "log", "--revisions", commit, "--no-graph", "--template", `json(self.parents().map(|c| stringify(c.commit_id()))) ++ "\n" ++ json(self.conflict()) ++ "\n"`)
		if e != nil {
			return e
		}
		parentsRecord, e := jjInventoryRecords(out, 2)
		if e != nil || len(parentsRecord) != 1 {
			return errors.New("invalid native working-copy parent evidence")
		}
		var parents []string
		conflict, e := jjBool(parentsRecord[0][1])
		if json.Unmarshal(parentsRecord[0][0], &parents) != nil || e != nil || conflict {
			return errors.New("conflicted or malformed native working-copy parents")
		}
		if len(parents) != 1 || !identifier(parents[0], 40, "0123456789abcdef") {
			return nil
		}
		integrated, e = ancestry(parents[0])
		if e != nil || !integrated {
			return e
		}
		out, e = s.read(destination, op, "diff", "--from", "commit_id("+jjLiteral(parents[0])+")", "--to", commit, "--template", jjInventoryDiff)
		if e != nil {
			return e
		}
		if out == "" {
			r.Proof = "empty-working-copy-integrated-parent"
		}
		return nil
	})
	if e != nil {
		return r, e
	}
	fresh, e := ObserveJJWorkspace(ctx, source, base)
	if e != nil {
		return r, e
	}
	target, e := VerifyJJWorkspace(ctx, source, name, destination, repo)
	if e != nil {
		return r, e
	}
	if !reflect.DeepEqual(fresh, r.Expected) || target != r.Target {
		return r, errJJChanged
	}
	return r, nil
}
