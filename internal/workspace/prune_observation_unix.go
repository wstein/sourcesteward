// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package workspace

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
)

// These private, in-memory witnesses do not define a stored prune plan, CLI
// selector, journal variant or frozen JSON contract. They authorize no writes.
type pruneInspectionOptions struct {
	Source, StateDir, Task, Owner string
	Registrations                 []string
}
type pruneDirectoryWitness struct {
	Identity PathIdentity
	UID, GID uint64
	Mode     uint32
	Mount    string
}
type pruneAbsenceWitness struct {
	Destination string
	Parent      PathIdentity
	Ancestors   []pruneDirectoryWitness
}
type pruneMetadataEntry struct {
	Physical           V3PhysicalIdentity
	Path, Kind, Digest string
	Identity           PathIdentity
	Mode               uint32
	Size               int64
}
type pruneRegistrationObservation struct {
	Creation              MutationPlan
	Task                  TaskRecord
	CreationJournalDigest string
	Source                NativeIdentity
	SafetyDigest          string
	SourceSnapshot        Snapshot
	ResolvedHEAD, HeadRef string
	Registration          PathIdentity
	Absence               pruneAbsenceWitness
	Metadata              []pruneMetadataEntry
	MetadataBytes         int64
}

func observePruneRegistration(ctx context.Context, o pruneInspectionOptions) (pruneRegistrationObservation, error) {
	var out pruneRegistrationObservation
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if len(o.Registrations) != 1 || o.Registrations[0] == "" || !validText(o.Registrations[0]) || filepath.Base(o.Registrations[0]) != o.Registrations[0] || o.Registrations[0] == "." || o.Registrations[0] == ".." {
		return out, failure("prune-selection-refused", "Exactly one literal native registration name is required.")
	}
	source, err := gitNativeIdentity(ctx, o.Source)
	if err != nil {
		return out, err
	}
	out.Source = source
	state, err := openStore(o.StateDir, false)
	if err != nil {
		return out, pruneOwnershipReadError(err)
	}
	defer state.Close()
	out.Creation, out.Task, out.CreationJournalDigest, err = readRetirementOwnership(state, o.Task, o.Owner)
	if err != nil {
		return out, pruneOwnershipReadError(err)
	}
	task := out.Task
	if task.Backend != "git" || task.Native.RepositoryDir != source.RepositoryDir || out.Creation.Expected.Identity.RepositoryDir != source.RepositoryDir {
		return out, failure("prune-repository-refused", "Creation ownership belongs to a different native repository.")
	}
	if task.Native.GitDir != filepath.Join(source.RepositoryDir, "worktrees", o.Registrations[0]) || task.Native.GitDir == source.GitDir || task.Native.GitDir == source.RepositoryDir {
		return out, failure("prune-registration-refused", "Selector does not identify the exact owned linked registration.")
	}
	out.Absence, err = observePruneAbsence(ctx, task.Destination, out.Creation.DestinationParent)
	if err != nil {
		return out, err
	}
	out.Registration, err = currentIdentity(task.Native.GitDir)
	if err != nil {
		return out, failure("prune-registration-refused", "Exact registration directory is unavailable or aliased.")
	}
	out.Metadata, out.MetadataBytes, err = observePruneMetadata(ctx, task.Native.GitDir, defaultPruneMetadataLimits())
	if err != nil {
		return out, err
	}
	read := func(name string) ([]byte, error) {
		data, _, _, err := gitLayoutFile(filepath.Join(task.Native.GitDir, name))
		if err != nil {
			return nil, err
		}
		for _, entry := range out.Metadata {
			if entry.Path == name && entry.Digest == gitDigest(string(data)) {
				return data, nil
			}
		}
		return nil, failure("prune-metadata-refused", "Registration metadata changed between bounded observations.")
	}
	lock, err := read("locked")
	if err != nil || !bytes.Equal(lock, []byte("stw:"+task.TaskID)) && !bytes.Equal(lock, []byte("stw:"+task.TaskID+"\n")) {
		return out, failure("prune-lock-refused", "Registration lock is not the original exact stw task lock.")
	}
	pointer, err := read("gitdir")
	if err != nil || !bytes.Equal(pointer, []byte(filepath.Join(task.Destination, ".git"))) && !bytes.Equal(pointer, []byte(filepath.Join(task.Destination, ".git")+"\n")) {
		return out, failure("prune-pointer-refused", "Registration gitdir does not name the original checkout backlink.")
	}
	common, err := read("commondir")
	if err != nil {
		return out, failure("prune-pointer-refused", "Registration common-directory pointer is unavailable.")
	}
	commonPath := strings.TrimSuffix(string(common), "\n")
	if commonPath != "../.." && commonPath != source.RepositoryDir {
		return out, failure("prune-pointer-refused", "Registration common-directory pointer is not the admitted native mapping.")
	}
	bin, err := gitExecutable()
	if err != nil {
		return out, err
	}
	snapshot, err := (gitAdapter{}).Observe(ctx, source.WorkspaceRoot, source.CommitID)
	if err != nil {
		return out, err
	}
	out.SafetyDigest = snapshot.SafetyDigest
	out.SourceSnapshot = snapshot
	parser := newGitTidyWorktreeParser(source.ObjectFormat)
	selected := 0
	err = gitCollectionReaderFor(ctx, bin, source.WorkspaceRoot)(gitRecordBlock, []string{"worktree", "list", "--porcelain", "-z"}, func(record []byte) error {
		if err := parser.add(record); err != nil {
			return err
		}
		w := parser.out[len(parser.out)-1]
		if w.path == task.Destination {
			selected++
			out.ResolvedHEAD = w.head
			out.HeadRef = w.branch
		}
		for _, field := range strings.Split(string(record), "\x00") {
			key, _, _ := strings.Cut(field, " ")
			if key == "prunable" && w.path != task.Destination {
				return failure("prune-sibling-refused", "Another prunable registration blocks the narrow observation.")
			}
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	if selected != 1 {
		return out, failure("prune-registration-refused", "Native inventory does not identify the exact missing registration.")
	}
	if err = revalidatePruneAbsence(ctx, out.Absence); err != nil {
		return out, err
	}
	fresh, size, err := observePruneMetadata(ctx, task.Native.GitDir, defaultPruneMetadataLimits())
	if err != nil {
		return out, err
	}
	if size != out.MetadataBytes || !reflect.DeepEqual(fresh, out.Metadata) {
		return out, failure("prune-metadata-refused", "Registration metadata changed during observation.")
	}
	creation, again, digest, err := readRetirementOwnership(state, o.Task, o.Owner)
	if err != nil {
		return out, pruneOwnershipReadError(err)
	}
	if !reflect.DeepEqual(creation, out.Creation) || !reflect.DeepEqual(again, out.Task) || digest != out.CreationJournalDigest {
		return out, failure("changed-ownership", "Original creation evidence changed during observation.")
	}
	final, err := (gitAdapter{}).Observe(ctx, source.WorkspaceRoot, source.CommitID)
	if err != nil {
		return out, err
	}
	if !reflect.DeepEqual(snapshot, final) {
		return out, failure("prune-repository-changed", "Source native state changed during registration observation.")
	}
	return out, nil
}
func pruneOwnershipReadError(err error) error {
	var typed operationError
	if errors.As(err, &typed) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return failure("prune-ownership-unavailable", "Original private creation ownership is unavailable.")
}
