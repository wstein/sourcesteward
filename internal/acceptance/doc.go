// SPDX-License-Identifier: EUPL-1.2

// Package acceptance holds the opt-in plan-only source acceptance runner of
// Spec 031 (T040). It contains tests only: the runner is an in-process Go test
// that plans a target repository with workspace.PlanTidyV3, never applies, and
// proves that the target did not change. See docs/acceptance-runner.md.
package acceptance
