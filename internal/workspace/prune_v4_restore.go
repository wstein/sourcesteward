// SPDX-License-Identifier: EUPL-1.2
package workspace

// RegistrationPruneV4RestoreOptions separates read-only eligibility inspection
// from an explicitly authorized administrative restore. Repair is never run.
type RegistrationPruneV4RestoreOptions struct {
	StateDir, PlanID string
	Apply, Authorize bool
	Mode             V3RunMode
	Confirm          func(word, planID string) error
}
