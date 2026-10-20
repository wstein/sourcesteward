// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
)

// Native update-ref needs its common metadata parent writable for packed-refs
// atomic replacement. Protect every existing sibling except the exact selected
// reference/recovery files, their locks/reflogs, and packed-refs plus its lock.
// No broad refs, objects, worktree, configuration or source-index grant remains.
func gitTidyWriteProtection(id NativeIdentity, refs []string) ([]string, error) {
	allowed := map[string]bool{"packed-refs": true, "packed-refs.lock": true}
	for _, ref := range refs {
		if !strings.HasPrefix(ref, "refs/") || len(ref) > 4096 || strings.ContainsAny(ref, "\x00\r\n ~^:?*[\\") || strings.Contains(ref, "..") || strings.Contains(ref, "@{") || strings.HasSuffix(ref, ".") {
			return nil, failure("tidy-invalid-reference", "Invalid exact mutable Git reference")
		}
		for _, part := range strings.Split(ref, "/") {
			if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
				return nil, failure("tidy-invalid-reference", "Unsafe mutable Git reference component")
			}
		}
		for _, p := range []string{ref, ref + ".lock", "logs/" + ref, "logs/" + ref + ".lock"} {
			allowed[p] = true
		}
	}
	protected := []string{}
	count := 0
	var walk func(string) error
	walk = func(relative string) error {
		dir := filepath.Join(id.RepositoryDir, relative)
		f, e := os.Open(dir)
		if e != nil {
			return e
		}
		entries, e := f.ReadDir(observationLimit + 1)
		f.Close()
		if e != nil && e != io.EOF {
			return e
		}
		count += len(entries)
		if count > observationLimit {
			return failure("tidy-metadata-limit", "Git mutable metadata protection exceeds 1024 entries")
		}
		for _, entry := range entries {
			rel := filepath.Join(relative, entry.Name())
			path := filepath.Join(id.RepositoryDir, rel)
			st, e := os.Lstat(path)
			if e != nil || (!st.Mode().IsRegular() && !st.IsDir()) {
				return failure("tidy-metadata-refused", "Git metadata contains unsupported or symbolic paths")
			}
			if allowed[rel] {
				if !gitTidyMutableFile(st) || strings.HasSuffix(rel, ".lock") {
					return failure("tidy-reference-lock", "Git target metadata is locked or not a regular file")
				}
				continue
			}
			parent := false
			for mutable := range allowed {
				if strings.HasPrefix(mutable, rel+string(filepath.Separator)) {
					parent = true
					break
				}
			}
			if parent {
				if !st.IsDir() {
					return failure("tidy-metadata-refused", "Git mutable reference parent is not a directory")
				}
				if e := walk(rel); e != nil {
					return e
				}
			} else {
				protected = append(protected, path)
			}
		}
		return nil
	}
	if e := walk(""); e != nil {
		return nil, e
	}
	sort.Strings(protected)
	return protected, nil
}

func gitTidyMutationOutcome() TidyNativeOutcome {
	return TidyNativeOutcome{Effects: []string{}, Limitations: []string{"Git old-OID CAS protects the target reference; source/base/registration checks are sampled and cannot exclude external races or ABA changes.", "Native recovery references keep commits reachable until explicitly removed; no automatic restoration or retention expiry is promised."}}
}

func gitTidyCurrentCandidate(ctx context.Context, p TidyPlanRecord, c TidyCandidate, expected Snapshot) (TidyObservation, error) {
	o, e := (gitTidyAdapter{}).Observe(ctx, p.SourcePath, expected.BaseSelector)
	if e != nil {
		return o, e
	}
	if !reflect.DeepEqual(o.Expected, expected) {
		return o, failure("tidy-stale-plan", "Git source, base, references, registrations or safety evidence changed")
	}
	if p.Backend != "git" || !validID(p.PlanID) || c.Kind != "branch" || c.Decision != "delete" || c.Proof != "native-ancestry" || !fullGitOID(c.CommitID, expected.Identity.ObjectFormat) {
		return o, failure("tidy-target-refused", "Git cleanup requires an exact eligible persisted branch candidate")
	}
	for _, current := range o.Candidates {
		if current.Name == c.Name && current.CommitID == c.CommitID && current.Decision == "delete" && current.Proof == "native-ancestry" && len(current.WorkspacePaths) == 0 {
			return o, nil
		}
	}
	return o, failure("tidy-target-refused", "Git branch identity or native integration proof is no longer eligible")
}

func gitTidyReferenceMap(ctx context.Context, bin, path string) (map[string]string, error) {
	return readGitTidyReferenceMap(gitCollectionReaderFor(ctx, bin, path))
}

func readGitTidyReferenceMap(read gitCollectionReader) (map[string]string, error) {
	refs := map[string]string{}
	e := read(gitRecordLine, []string{"for-each-ref", "--format=%(refname)%00%(objectname)%00%(symref)%00"}, func(raw []byte) error {
		record, ok := strings.CutSuffix(string(raw), "\x00\n")
		if !ok {
			return failure("tidy-invalid-reference", "Malformed full reference state")
		}
		name, oid, ok := strings.Cut(record, "\x00")
		if !ok || refs[name] != "" {
			return failure("tidy-invalid-reference", "Ambiguous full reference state")
		}
		refs[name] = oid
		return nil
	})
	if errors.Is(e, io.ErrUnexpectedEOF) {
		return nil, failure("tidy-invalid-reference", "Malformed full reference state")
	}
	if e != nil {
		return nil, e
	}
	return refs, nil
}
func gitTidyReferenceDigest(refs map[string]string) string {
	names := make([]string, 0, len(refs))
	for name := range refs {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		b.WriteString(name)
		b.WriteByte(0)
		b.WriteString(refs[name])
		b.WriteString("\x00\n")
	}
	return gitDigest(b.String())
}
func gitTidyVerifyDelta(ctx context.Context, p TidyPlanRecord, expected Snapshot, refs map[string]string) (Snapshot, error) {
	fresh, e := (gitTidyAdapter{}).Observe(ctx, p.SourcePath, expected.BaseSelector)
	if e != nil {
		return Snapshot{}, e
	}
	desired := expected
	desired.ReferenceDigest = gitTidyReferenceDigest(refs)
	if !reflect.DeepEqual(fresh.Expected, desired) {
		return Snapshot{}, failure("tidy-native-outcome-incomplete", "Native Git changed state outside the exact declared reference delta; retain recovery and inspect the journal")
	}
	return fresh.Expected, nil
}

func (gitTidyAdapter) PrepareRecovery(ctx context.Context, p TidyPlanRecord, c TidyCandidate) (TidyRecovery, Snapshot, TidyNativeOutcome, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	recovery := tidyRecoveryFor(p, c)
	out := gitTidyMutationOutcome()
	if _, e := gitTidyCurrentCandidate(ctx, p, c, p.Expected); e != nil {
		return recovery, Snapshot{}, out, e
	}
	bin, e := gitExecutable()
	if e != nil {
		return recovery, Snapshot{}, out, e
	}
	refs, e := gitTidyReferenceMap(ctx, bin, p.SourcePath)
	if e != nil {
		return recovery, Snapshot{}, out, e
	}
	if gitTidyReferenceDigest(refs) != p.Expected.ReferenceDigest || refs[recovery.Name] != "" {
		return recovery, Snapshot{}, out, failure("tidy-recovery-collision", "Recovery ref exists or reference evidence changed; no native action was attempted")
	}
	if _, e := gitTidyWriteProtection(p.Expected.Identity, []string{recovery.Name, c.Name}); e != nil {
		return recovery, Snapshot{}, out, e
	}
	protection, e := gitTidyWriteProtection(p.Expected.Identity, []string{recovery.Name})
	if e != nil {
		return recovery, Snapshot{}, out, e
	}
	out.Attempted = true
	out.Effects = append(out.Effects, "Attempted native creation of exact recovery reference "+recovery.Name)
	_, code, runErr := runSandboxed(ctx, bin, []string{"--no-replace-objects", "update-ref", "--no-deref", recovery.Name, c.CommitID, strings.Repeat("0", len(c.CommitID))}, p.SourcePath, []string{p.Expected.Identity.RepositoryDir}, protection)
	if runErr != nil {
		return recovery, Snapshot{}, out, gitExecutionError(runErr)
	}
	if code != 0 {
		return recovery, Snapshot{}, out, failure("tidy-recovery-failed", "Native recovery ref creation failed; inspect the exact proposed root before any retry")
	}
	refs[recovery.Name] = c.CommitID + "\x00"
	fresh, e := gitTidyVerifyDelta(ctx, p, p.Expected, refs)
	if e != nil {
		return recovery, Snapshot{}, out, e
	}
	out.Complete = true
	out.Expected = &fresh
	out.Effects = []string{"Created native recovery reference " + recovery.Name + " at " + c.CommitID}
	return recovery, fresh, out, nil
}

func (gitTidyAdapter) Delete(ctx context.Context, p TidyPlanRecord, c TidyCandidate, recovery TidyRecovery, expected Snapshot) (TidyNativeOutcome, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out := gitTidyMutationOutcome()
	if recovery != tidyRecoveryFor(p, c) {
		return out, failure("tidy-recovery-refused", "Recovery identity differs from the predeclared exact native root")
	}
	if _, e := gitTidyCurrentCandidate(ctx, p, c, expected); e != nil {
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
	if gitTidyReferenceDigest(refs) != expected.ReferenceDigest || refs[recovery.Name] != c.CommitID+"\x00" || refs[c.Name] != c.CommitID+"\x00" {
		return out, failure("tidy-recovery-refused", "Exact recovery ref or original branch tip is unavailable")
	}
	if e := gitTidyLiteralRecovery(ctx, bin, p.SourcePath, recovery); e != nil {
		return out, e
	}
	if _, e := gitTidyWriteProtection(expected.Identity, []string{recovery.Name, c.Name}); e != nil {
		return out, e
	}
	protection, e := gitTidyWriteProtection(expected.Identity, []string{c.Name})
	if e != nil {
		return out, e
	}
	out.Attempted = true
	out.Effects = append(out.Effects, "Attempted native old-OID CAS deletion of local branch "+c.Name)
	_, code, runErr := runSandboxed(ctx, bin, []string{"--no-replace-objects", "update-ref", "--no-deref", "-d", c.Name, c.CommitID}, p.SourcePath, []string{expected.Identity.RepositoryDir}, protection)
	if runErr != nil {
		return out, gitExecutionError(runErr)
	}
	if code != 0 {
		return out, failure("tidy-delete-failed", "Native old-OID CAS deletion failed; recovery is retained and no rollback is implied")
	}
	delete(refs, c.Name)
	fresh, e := gitTidyVerifyDelta(ctx, p, expected, refs)
	if e != nil {
		return out, e
	}
	out.Complete = true
	out.Expected = &fresh
	out.Effects = []string{"Deleted exact local branch " + c.Name + "; native recovery retained at " + recovery.Name}
	return out, nil
}

func gitTidyLiteralRecovery(ctx context.Context, bin, path string, recovery TidyRecovery) error {
	_, code, e := runSandboxed(ctx, bin, []string{"--no-replace-objects", "symbolic-ref", "--quiet", recovery.Name}, path, nil, nil)
	if e != nil {
		return gitExecutionError(e)
	}
	if code != 1 {
		return failure("tidy-recovery-refused", "Recovery must remain an independent nonsymbolic native reference")
	}
	return nil
}
