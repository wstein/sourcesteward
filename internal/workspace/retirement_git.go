// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

type gitRetirementAdapter struct{}

func gitRetirementMetadataPaths() []string {
	return []string{"HEAD", "ORIG_HEAD", "commondir", "gitdir", "index", "locked", "logs", "logs/HEAD", "refs"}
}

func (gitRetirementAdapter) Observe(ctx context.Context, source, base string, task TaskRecord, creation MutationPlan) (RetirementObservation, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	o := RetirementObservation{Siblings: []RetirementSibling{}, WorkspacePaths: []string{}, Actions: []RetirementAction{}, Limitations: []string{"Native and filesystem state are sampled; external races and ABA changes are not transactionally excluded.", "Git registration and filesystem removal are coupled and require both authorizations; unknown or dirty content is retained."}}
	sourceState, e := (gitTidyAdapter{}).Observe(ctx, source, base)
	if e != nil {
		return o, e
	}
	o.Expected = sourceState.Expected
	o.WorkspacePaths = sourceState.WorkspacePaths
	o.WorkspacePathsComplete = sourceState.WorkspacePathsComplete
	native, e := (gitAdapter{}).Verify(ctx, creation)
	if e != nil || !native.Complete || !sameOwner(native.Identity, task.Native) || native.Identity.HeadRef != "" {
		return o, failure("retirement-ownership-refused", "Exact owned detached Git worktree registration is unavailable")
	}
	o.Target = native.Identity
	destination, e := currentIdentity(task.Destination)
	if e != nil || destination != task.DestinationIdentity {
		return o, failure("retirement-destination-changed", "Owned worktree physical identity changed")
	}
	o.Destination = destination
	pointer, e := gitMetadata(filepath.Join(task.Destination, ".git"))
	if e != nil || string(pointer) != "gitdir: "+o.Target.GitDir+"\n" {
		return o, failure("retirement-pointer-refused", "Owned Git pointer bytes do not identify the exact native registration")
	}
	o.LockReason = "stw:" + task.TaskID
	bin, e := gitExecutable()
	if e != nil {
		return o, e
	}
	if _, e = gitSafety(ctx, bin, o.Target, o.Target.CommitID); e != nil {
		return o, e
	}
	stored, e := gitRetirementStoredFiles(ctx, bin, o.Target)
	if e != nil {
		return o, e
	}
	o.Manifest, e = observeRetirementManifest(ctx, task.Destination, retirementManifestAdmission{Backend: "git", NativeMetadataPaths: []string{".git"}, StoredFiles: stored})
	if e != nil {
		return o, e
	}
	o.Metadata, e = gitRetirementMetadata(ctx, o.Target.GitDir)
	if e != nil {
		return o, e
	}
	o.DestinationParent, e = currentIdentity(filepath.Dir(task.Destination))
	if e != nil {
		return o, e
	}
	o.RegistrationParent, e = currentIdentity(filepath.Dir(o.Target.GitDir))
	if e != nil {
		return o, e
	}
	o.Siblings, e = gitRetirementSiblings([]PathIdentity{o.DestinationParent, o.RegistrationParent})
	if e != nil {
		return o, e
	}
	o.SiblingDigest = gitRetirementSiblingDigest(o.Siblings)
	_, code, e := runSandboxed(ctx, bin, []string{"--no-replace-objects", "merge-base", "--is-ancestor", o.Target.CommitID, o.Expected.BaseCommit}, source, nil, nil)
	if e != nil {
		return o, gitExecutionError(e)
	}
	o.Proof = "none"
	decision, reason := "retain", "integration-not-proven"
	if code == 0 {
		o.Proof = "native-ancestry"
		decision, reason = "delete", "clean-owned-integrated-worktree"
	} else if code != 1 {
		return o, failure("retirement-ancestry-unavailable", "Native target integration evidence is unavailable")
	}
	o.Actions = []RetirementAction{{Name: "remove-registration", Target: o.Target.GitDir, Decision: decision, Reason: reason, Proof: o.Proof, Effects: []string{"Remove the exact owned Git registration and stw lock together with its physical worktree; both authorizations are required."}, Blockers: []string{}}, {Name: "remove-files", Target: task.Destination, Decision: decision, Reason: reason, Proof: o.Proof, Effects: []string{"Remove only the exact clean owned worktree tree through native coupled Git removal."}, Blockers: []string{}}}
	fresh, e := (gitTidyAdapter{}).Observe(ctx, source, base)
	if e != nil || !reflect.DeepEqual(fresh.Expected, o.Expected) {
		return o, failure("retirement-stale-source", "Source native state changed during retirement observation")
	}
	final, e := (gitAdapter{}).Verify(ctx, creation)
	if e != nil || !reflect.DeepEqual(final.Identity, o.Target) {
		return o, failure("retirement-stale-target", "Owned target native identity changed during retirement observation")
	}
	manifest, e := observeRetirementManifest(ctx, task.Destination, retirementManifestAdmission{Backend: "git", NativeMetadataPaths: []string{".git"}, StoredFiles: stored})
	if e != nil || !reflect.DeepEqual(manifest, o.Manifest) {
		return o, failure("retirement-stale-files", "Owned worktree files changed during retirement observation")
	}
	metadata, e := gitRetirementMetadata(ctx, o.Target.GitDir)
	if e != nil || !reflect.DeepEqual(metadata, o.Metadata) {
		return o, failure("retirement-stale-metadata", "Owned registration metadata changed during retirement observation")
	}
	return o, nil
}

func gitRetirementStoredFiles(ctx context.Context, bin string, id NativeIdentity) ([]RetirementStoredFile, error) {
	raw, e := gitRead(ctx, bin, id.WorkspaceRoot, "ls-tree", "-r", "-l", "-z", "--full-tree", id.CommitID)
	if e != nil {
		return nil, e
	}
	if e = validateGitTree(raw); e != nil {
		return nil, e
	}
	files := []RetirementStoredFile{}
	expectedIndex := map[string]string{}
	if raw != "" {
		for _, record := range strings.Split(strings.TrimSuffix(raw, "\x00"), "\x00") {
			meta, path, _ := strings.Cut(record, "\t")
			f := strings.Fields(meta)
			size, _ := strconv.ParseInt(f[3], 10, 64)
			if size > nativeOutputLimit {
				return nil, failure("retirement-file-limit", "Stored Git content exceeds 64 KiB; retain the target")
			}
			content, code, e := runSandboxed(ctx, bin, []string{"--no-replace-objects", "cat-file", "blob", f[2]}, id.WorkspaceRoot, nil, nil)
			if e != nil {
				return nil, gitExecutionError(e)
			}
			if code != 0 || int64(len(content)) != size {
				return nil, failure("retirement-stored-content-refused", "Complete raw stored Git content is unavailable")
			}
			files = append(files, RetirementStoredFile{Path: path, Digest: gitDigest(content), Size: size, Executable: f[0] == "100755"})
			expectedIndex[path] = f[0] + " " + f[2] + " 0"
		}
	}
	index, e := gitRead(ctx, bin, id.WorkspaceRoot, "ls-files", "--stage", "-z")
	if e != nil {
		return nil, e
	}
	count := 0
	seen := map[string]bool{}
	if index != "" {
		for _, record := range strings.Split(strings.TrimSuffix(index, "\x00"), "\x00") {
			meta, path, ok := strings.Cut(record, "\t")
			if !ok || seen[path] || expectedIndex[path] != meta {
				return nil, failure("retirement-index-not-clean", "Git index differs from the exact current stored tree; retain staged/conflicted work")
			}
			seen[path] = true
			count++
		}
	}
	if count != len(expectedIndex) {
		return nil, failure("retirement-index-not-clean", "Git index is incomplete relative to the stored tree")
	}
	return files, nil
}

func gitRetirementMetadata(ctx context.Context, path string) (RetirementManifest, error) {
	for _, name := range []string{"HEAD", "commondir", "gitdir", "index", "locked", "logs/HEAD"} {
		if _, e := gitMetadata(filepath.Join(path, name)); e != nil {
			return RetirementManifest{}, failure("retirement-metadata-refused", "Required owned registration metadata is missing, aliased or oversized")
		}
	}
	return observeRetirementManifest(ctx, path, retirementManifestAdmission{Backend: "git", NativeMetadataPaths: gitRetirementMetadataPaths(), StoredFiles: []RetirementStoredFile{}})
}

func gitRetirementSiblings(parents []PathIdentity) ([]RetirementSibling, error) {
	out := []RetirementSibling{}
	seen := map[string]bool{}
	for _, parent := range parents {
		current, e := currentIdentity(parent.Path)
		if e != nil || current != parent {
			return nil, failure("retirement-parent-changed", "Native removal parent identity changed")
		}
		f, e := os.Open(parent.Path)
		if e != nil {
			return nil, e
		}
		entries, e := f.ReadDir(observationLimit + 1)
		f.Close()
		if e != nil && e != io.EOF {
			return nil, e
		}
		for _, entry := range entries {
			path := filepath.Join(parent.Path, entry.Name())
			if seen[path] {
				continue
			}
			seen[path] = true
			st, e := os.Lstat(path)
			if e != nil || (!st.IsDir() && !st.Mode().IsRegular()) {
				return nil, failure("retirement-sibling-refused", "Removal parent contains symbolic or unsupported sibling entries")
			}
			id, e := pathIdentity(path, st)
			if e != nil {
				return nil, e
			}
			kind := "regular"
			if st.IsDir() {
				kind = "directory"
			}
			out = append(out, RetirementSibling{Identity: id, Kind: kind})
			if len(out) > observationLimit {
				return nil, failure("retirement-sibling-limit", "Removal parent siblings exceed 1024 records")
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Identity.Path < out[j].Identity.Path })
	return out, nil
}
func gitRetirementSiblingDigest(s []RetirementSibling) string {
	b, _ := json.Marshal(s)
	return gitDigest(string(b))
}

// Split read-only ancestors containing another mutable descendant, so Linux
// bind ordering cannot accidentally make native registration writes read-only.
func gitRetirementProtection(parents []PathIdentity, mutable []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	count := 0
	var walk func(string) error
	walk = func(dir string) error {
		f, e := os.Open(dir)
		if e != nil {
			return e
		}
		entries, e := f.ReadDir(observationLimit + 1)
		f.Close()
		if e != nil && e != io.EOF {
			return e
		}
		for _, entry := range entries {
			count++
			if count > observationLimit {
				return failure("retirement-protection-limit", "Native removal protection exceeds 1024 entries")
			}
			path := filepath.Join(dir, entry.Name())
			if seen[path] {
				continue
			}
			seen[path] = true
			st, e := os.Lstat(path)
			if e != nil || (!st.IsDir() && !st.Mode().IsRegular()) {
				return failure("retirement-protection-refused", "Native removal write grant contains unsupported sibling paths")
			}
			exact, ancestor := false, false
			for _, target := range mutable {
				if path == target {
					exact = true
				}
				if strings.HasPrefix(target, path+string(filepath.Separator)) {
					ancestor = true
				}
			}
			if exact {
				continue
			}
			if ancestor {
				if !st.IsDir() {
					return failure("retirement-protection-refused", "Mutable registration ancestor is not an ordinary directory")
				}
				if e := walk(path); e != nil {
					return e
				}
			} else {
				out = append(out, path)
			}
		}
		return nil
	}
	for _, parent := range parents {
		if e := walk(parent.Path); e != nil {
			return nil, e
		}
	}
	sort.Strings(out)
	return out, nil
}

func gitRetirementCurrent(ctx context.Context, p RetirementPlanRecord, expected Snapshot) (RetirementObservation, error) {
	o, e := (gitRetirementAdapter{}).Observe(ctx, p.SourcePath, expected.BaseSelector, p.Task, p.CreationPlan)
	if e != nil {
		return o, e
	}
	desired := p.Expected
	desired.Expected = expected
	desired.Actions = o.Actions
	if !reflect.DeepEqual(o, desired) {
		return o, failure("retirement-stale-plan", "Owned Git native, physical, metadata, parent or sibling evidence changed")
	}
	if p.Backend != "git" || !validID(p.PlanID) || o.Proof != "native-ancestry" || !p.RemoveRegistration || p.RemoveFiles != p.Task.Destination {
		return o, failure("retirement-authorization-refused", "Git coupled removal requires exact owned integrated target and both registration/files selectors")
	}
	return o, nil
}

func (gitRetirementAdapter) PrepareRecovery(ctx context.Context, p RetirementPlanRecord) (TidyRecovery, TidyNativeOutcome, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	r := retirementRecoveryFor(p)
	out := gitRetirementOutcome()
	if _, e := gitRetirementCurrent(ctx, p, p.Expected.Expected); e != nil {
		return r, out, e
	}
	bin, e := gitExecutable()
	if e != nil {
		return r, out, e
	}
	refs, e := gitTidyReferenceMap(ctx, bin, p.SourcePath)
	if e != nil {
		return r, out, e
	}
	if gitTidyReferenceDigest(refs) != p.Expected.Expected.ReferenceDigest || refs[r.Name] != "" {
		return r, out, failure("retirement-recovery-collision", "Native reference evidence changed or the exact recovery root exists")
	}
	protection, e := gitTidyWriteProtection(p.Expected.Expected.Identity, []string{r.Name})
	if e != nil {
		return r, out, e
	}
	out.Attempted = true
	out.Effects = []string{"Attempted native recovery ref creation at " + r.Name}
	_, code, e := runSandboxed(ctx, bin, []string{"--no-replace-objects", "update-ref", "--no-deref", r.Name, r.CommitID, strings.Repeat("0", len(r.CommitID))}, p.SourcePath, []string{p.Expected.Expected.Identity.RepositoryDir}, protection)
	if e != nil {
		return r, out, gitExecutionError(e)
	}
	if code != 0 {
		return r, out, failure("retirement-recovery-failed", "Native recovery pin creation failed; inspect the durable journal before any retry")
	}
	refs[r.Name] = r.CommitID + "\x00"
	expected := p.Expected.Expected
	expected.ReferenceDigest = gitTidyReferenceDigest(refs)
	if e := gitTidyLiteralRecovery(ctx, bin, p.SourcePath, r); e != nil {
		return r, out, e
	}
	if _, e := gitRetirementCurrent(ctx, p, expected); e != nil {
		return r, out, e
	}
	out.Complete = true
	out.Expected = &expected
	out.Effects = []string{"Created exact native recovery ref " + r.Name + " preserving current owned target commit " + r.CommitID}
	return r, out, nil
}

func (gitRetirementAdapter) RemoveRegistration(ctx context.Context, p RetirementPlanRecord, recovery TidyRecovery, expected Snapshot) (TidyNativeOutcome, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out := gitRetirementOutcome()
	if recovery != retirementRecoveryFor(p) {
		return out, failure("retirement-recovery-refused", "Native recovery identity differs from the predeclared target pin")
	}
	if _, e := gitRetirementCurrent(ctx, p, expected); e != nil {
		return out, e
	}
	bin, e := gitExecutable()
	if e != nil {
		return out, e
	}
	refs, e := gitTidyReferenceMap(ctx, bin, p.SourcePath)
	if e != nil {
		return out, e
	}
	if gitTidyReferenceDigest(refs) != expected.ReferenceDigest || refs[recovery.Name] != recovery.CommitID+"\x00" {
		return out, failure("retirement-recovery-refused", "Exact native target recovery pin is unavailable")
	}
	if e := gitTidyLiteralRecovery(ctx, bin, p.SourcePath, recovery); e != nil {
		return out, e
	}
	raw, e := gitRead(ctx, bin, p.SourcePath, "worktree", "list", "--porcelain", "-z")
	if e != nil {
		return out, e
	}
	registrations, e := gitRetirementWithoutRegistration(raw, p.Task.Destination)
	if e != nil {
		return out, e
	}
	parents := []PathIdentity{p.Expected.DestinationParent, p.Expected.RegistrationParent}
	protection, e := gitRetirementProtection(parents, []string{p.Task.Destination, p.Expected.Target.GitDir})
	if e != nil {
		return out, e
	}
	// Resample after computing grants; forced native removal must never reuse
	// stale clean-state or ownership proof merely because the owned lock matches.
	if _, e := gitRetirementCurrent(ctx, p, expected); e != nil {
		return out, e
	}
	out.Attempted = true
	out.Effects = []string{"Attempted native coupled removal of exact owned stw lock, registration " + p.Expected.Target.GitDir + " and physical worktree " + p.Task.Destination}
	_, code, e := runSandboxed(ctx, bin, []string{"--no-replace-objects", "worktree", "remove", "--force", "--force", "--", p.Task.Destination}, p.SourcePath, []string{p.Expected.DestinationParent.Path, p.Expected.RegistrationParent.Path}, protection)
	if e != nil {
		return out, gitExecutionError(e)
	}
	if code != 0 {
		return out, failure("retirement-native-remove-failed", "Native coupled worktree removal failed; possible effects and recovery remain recorded, with no rollback implied")
	}
	desired := expected
	desired.RegistrationDigest = gitDigest(registrations)
	if e := gitRetirementVerifyAbsent(ctx, p, recovery, desired); e != nil {
		return out, e
	}
	out.Complete = true
	out.Expected = &desired
	out.Effects = []string{"Removed exact owned native Git registration and stw lock " + p.Expected.Target.GitDir, "Removed exact clean owned physical worktree " + p.Task.Destination, "Retained native current-target recovery ref " + recovery.Name}
	return out, nil
}

func (gitRetirementAdapter) RemoveFiles(ctx context.Context, p RetirementPlanRecord, recovery TidyRecovery, expected Snapshot) (TidyNativeOutcome, error) {
	out := gitRetirementOutcome()
	if e := gitRetirementVerifyAbsent(ctx, p, recovery, expected); e != nil {
		return out, e
	}
	out.Complete = true
	out.Expected = &expected
	out.Effects = []string{"Exact worktree files were already removed by the separately authorized native coupled registration action; no duplicate filesystem mutation occurred."}
	return out, nil
}

func gitRetirementWithoutRegistration(raw, dest string) (string, error) {
	records := strings.Split(strings.TrimSuffix(raw, "\x00\x00"), "\x00\x00")
	out := []string{}
	found := 0
	for _, record := range records {
		first, _, _ := strings.Cut(record, "\x00")
		if first == "worktree "+dest {
			found++
			continue
		}
		out = append(out, record)
	}
	if found != 1 {
		return "", failure("retirement-registration-refused", "Exact owned native registration is absent or ambiguous")
	}
	return strings.Join(out, "\x00\x00") + "\x00\x00", nil
}

func gitRetirementVerifyAbsent(ctx context.Context, p RetirementPlanRecord, recovery TidyRecovery, expected Snapshot) error {
	if recovery != retirementRecoveryFor(p) || !p.RemoveRegistration || p.RemoveFiles != p.Task.Destination {
		return failure("retirement-authorization-refused", "Exact coupled removal authorization or recovery identity is unavailable")
	}
	for _, path := range []string{p.Task.Destination, p.Expected.Target.GitDir} {
		if _, e := os.Lstat(path); !os.IsNotExist(e) {
			return failure("retirement-outcome-incomplete", "Exact destination or registration is not proven absent")
		}
	}
	parents := []PathIdentity{p.Expected.DestinationParent, p.Expected.RegistrationParent}
	siblings, e := gitRetirementSiblings(parents)
	if e != nil {
		return e
	}
	wanted := []RetirementSibling{}
	for _, s := range p.Expected.Siblings {
		if s.Identity.Path != p.Task.Destination && s.Identity.Path != p.Expected.Target.GitDir {
			wanted = append(wanted, s)
		}
	}
	if !reflect.DeepEqual(siblings, wanted) {
		return failure("retirement-outcome-incomplete", "Unrelated native removal parent siblings changed")
	}
	fresh, e := (gitTidyAdapter{}).Observe(ctx, p.SourcePath, expected.BaseSelector)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(fresh.Expected, expected) {
		return failure("retirement-outcome-incomplete", "Native post-state differs from the exact permitted registration-removal delta")
	}
	bin, e := gitExecutable()
	if e != nil {
		return e
	}
	refs, e := gitTidyReferenceMap(ctx, bin, p.SourcePath)
	if e != nil {
		return e
	}
	if refs[recovery.Name] != recovery.CommitID+"\x00" {
		return failure("retirement-recovery-refused", "Native target recovery ref is no longer exact")
	}
	return gitTidyLiteralRecovery(ctx, bin, p.SourcePath, recovery)
}

func gitRetirementOutcome() TidyNativeOutcome {
	return TidyNativeOutcome{Effects: []string{}, Limitations: []string{"Native Git worktree removal has no CAS transaction; native ownership, disk content and parent identities are sampled, and external races or ABA changes cannot be excluded.", "Recovery pins preserve the current stored tree, not untracked files or original worktree index, lock, configuration, reflog bytes, or every unreachable historical commit. Granted parent directories remain present."}}
}
