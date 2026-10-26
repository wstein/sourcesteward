// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package workspace

import (
	"context"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

// Every unlink is relative to a held administrative directory descriptor. No
// checkout or destination-parent descriptor is ever granted for writes.
func removePruneV4Metadata(ctx context.Context, p RegistrationPruneV4Plan, expected PruneV4Expected, j *pruneV4Journal, s, parent *store) error {
	root, id, e := openCanonical(p.Registration.GitDir)
	if e != nil {
		return e
	}
	defer root.Close()
	if id != expected.Registration {
		return failure("prune-evidence-changed", "Registration identity changed before removal.")
	}
	regParent, _, e := openCanonical(filepath.Dir(p.Registration.GitDir))
	if e != nil {
		return e
	}
	defer regParent.Close()
	entries := append([]PruneV4Metadata{}, p.Expected.Metadata...)
	// Descendants precede parents. Keep the original lock until all other files
	// have been removed; there is no unlock-and-prune transition.
	sort.Slice(entries, func(i, k int) bool {
		rank := func(m PruneV4Metadata) int {
			if m.Path == "." {
				return -3
			}
			if m.Path == "locked" {
				return -2
			}
			if m.Kind == "directory" {
				return -1
			}
			return strings.Count(m.Path, "/") + 1
		}
		a, b := rank(entries[i]), rank(entries[k])
		if a != b {
			return a > b
		}
		return entries[i].Path > entries[k].Path
	})
	removed := map[string]bool{}
	for _, m := range entries {
		if m.Kind == "absent" {
			continue
		}
		pruneObservationStep(ctx, "v4-before-unlink", m.Path, nil)
		if e = validatePruneV4RemovalBoundary(ctx, p, expected, removed, s, parent); e != nil {
			return e
		}
		if _, digest, e := readPruneV4Payload(ctx, p); e != nil || digest != j.PayloadDigest {
			return pruneV4PayloadError()
		}
		j.Phase = "removing"
		j.Effects = append(j.Effects, pruneV4Effect{"remove-metadata", m.Path, "unlink-exact", "pending"})
		if e = writePruneV4Record(s.dirs["journals"], p.PlanID+".json", *j, false); e != nil {
			return e
		}
		// Recheck again after intent persistence, so an injected or external change
		// during the journal write never gains removal authority.
		if e = validatePruneV4RemovalBoundary(ctx, p, expected, removed, s, parent); e != nil {
			return e
		}
		if m.Path == "." {
			e = regParent.Remove(p.Registration.Name)
		} else {
			e = root.Remove(m.Path)
		}
		if e != nil {
			return failure("prune-removal-incomplete", "Exact administrative metadata removal failed; recovery and partial evidence are retained.")
		}
		removed[m.Path] = true
		j.Effects[len(j.Effects)-1].Outcome = "complete"
		if m.Path != "." {
			if e = syncDirectory(root); e != nil {
				return failure("prune-removal-incomplete", "Administrative removal durability is unconfirmed.")
			}
		} else {
			if e = syncDirectory(regParent); e != nil {
				return failure("prune-removal-incomplete", "Registration removal durability is unconfirmed.")
			}
		}
		if e = writePruneV4Record(s.dirs["journals"], p.PlanID+".json", *j, false); e != nil {
			return e
		}
	}
	return nil
}
func validatePruneV4RemovalBoundary(ctx context.Context, p RegistrationPruneV4Plan, expected PruneV4Expected, removed map[string]bool, s, parent *store) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if e := parent.validate(); e != nil {
		return e
	}
	if e := s.validate(); e != nil {
		return e
	}
	common, e := observePruneV4Common(ctx, expected.SourceSnapshot.Identity.RepositoryDir)
	if e != nil {
		return e
	}
	if common != expected.CommonDirMount {
		return failure("prune-mount-changed", "Common-directory witness changed before an administrative effect.")
	}
	if e = revalidatePruneV4Absence(ctx, p); e != nil {
		return e
	}
	ownership, e := openStore(p.StateDir, false)
	if e != nil {
		return e
	}
	creation, task, journalDigest, e := readRetirementOwnership(ownership, p.Registration.Task, p.Registration.Owner)
	ownership.Close()
	if e != nil {
		return pruneOwnershipReadError(e)
	}
	digest, e := v3RecordDigest(task)
	if e != nil {
		return e
	}
	if digest != p.Expected.TaskDigest || creation.Digest != p.Expected.CreationDigest || journalDigest != p.Expected.CreationJournalDigest {
		return failure("prune-ownership-refused", "Original creation ownership changed before an administrative effect.")
	}
	source, e := gitNativeIdentity(ctx, p.SourcePath)
	if e != nil {
		return e
	}
	if source != p.Expected.SourceSnapshot.Identity {
		return failure("prune-evidence-changed", "Source native identity changed during removal.")
	}
	siblings, e := observePruneV4Siblings(ctx, source.RepositoryDir)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(siblings, expected.ProtectedSiblings) {
		return failure("prune-evidence-changed", "Protected registration siblings changed during removal.")
	}
	actual, _, e := observePruneMetadata(ctx, p.Registration.GitDir, defaultPruneMetadataLimits())
	if e != nil {
		return e
	}
	byPath := map[string]pruneMetadataEntry{}
	for _, f := range actual {
		byPath[f.Path] = f
	}
	for _, want := range p.Expected.Metadata {
		got, ok := byPath[want.Path]
		if !ok {
			return failure("prune-evidence-changed", "Registration metadata manifest changed before removal.")
		}
		if removed[want.Path] || want.Kind == "absent" {
			if got.Kind != "absent" {
				return failure("prune-evidence-changed", "Removed or absent administrative metadata reappeared.")
			}
			continue
		}
		if want.Kind == "directory" {
			changed := false
			for path := range removed {
				if filepath.Dir(path) == want.Path {
					changed = true
				}
			}
			if changed {
				if !retirementV3SamePostParent(got.Physical, want.Physical) {
					return failure("prune-evidence-changed", "Administrative directory identity or protection changed.")
				}
			} else if got.Physical != want.Physical {
				return failure("prune-evidence-changed", "Administrative directory changed without an explained removal.")
			}
		} else if got.Kind != "regular" || got.Digest != want.Digest || got.Physical != want.Physical {
			return failure("prune-evidence-changed", "Administrative file identity or content changed before removal.")
		}
	}
	return nil
}
