// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package workspace

import "context"

// These private, in-memory witnesses do not define a stored prune plan, CLI
// selector, journal variant or frozen JSON contract. They authorize no writes.
type pruneInspectionOptions struct{ Source, StateDir, Task, Owner, Registration string }
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
	Registration          PathIdentity
	Absence               pruneAbsenceWitness
	Metadata              []pruneMetadataEntry
	MetadataBytes         int64
}

func observePruneRegistration(ctx context.Context, o pruneInspectionOptions) (pruneRegistrationObservation, error) {
	return pruneRegistrationObservation{}, failure("prune-unavailable", "Narrow registration observation is not implemented.")
}
func observePruneAbsence(ctx context.Context, destination string, parent PathIdentity) (pruneAbsenceWitness, error) {
	return pruneAbsenceWitness{}, failure("prune-unavailable", "Missing-directory observation is not implemented.")
}
func revalidatePruneAbsence(ctx context.Context, witness pruneAbsenceWitness) error {
	return failure("prune-unavailable", "Missing-directory revalidation is not implemented.")
}
