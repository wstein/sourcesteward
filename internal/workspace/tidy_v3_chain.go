// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"errors"
	"os"
	"reflect"
)

// A plan set stores one plan per sub-plan, each with the whole-repository baseline of plan time.
// Applying one of them changes that repository (a registration is removed, a recovery reference is
// created, a branch is deleted), so a later plan of the same repository would see a changed fact
// that the set itself caused. v3Chain derives, at apply time only, what the repository must look
// like when the journaled effects of the other plans of the set are added to a stored baseline.
// It stores nothing: plans, journals and their schemas are unchanged.
//
// The derivation is reusable. It takes the complete journals of other plans and returns the
// expected Snapshot and V3NativeState (apply) and the adjusted retirement facts (retirement).
// Anything the journals do not explain stays strictly compared and ends in evidence-changed.
type v3Chain struct {
	effects []v3ChainEffect
	// The destination and registration paths of the retirements the chain contains, and the
	// directories those removals happened in.
	removedDestinations  map[string]bool
	removedRegistrations map[string]bool
	changedDirectories   map[string]bool
}

type v3ChainEffect struct {
	plan     V3Plan
	kind, id string
}

// loadV3Chain reads the named sibling plans of one apply. A name that is malformed, unknown, the
// plan itself or a plan of another repository is rejected: the caller is not trusted. A sibling
// without a journal, or with a journal that is not complete, contributes nothing (a failed or
// skipped sibling leaves later plans strict). A complete sibling contributes exactly the deltas
// its journal records, and each delta must fit the stored baseline of the plan being applied.
// The result is nil when no sibling contributes.
func loadV3Chain(stateDir string, self V3Plan, baseline V3NativeState, ids []string) (*v3Chain, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	chain := &v3Chain{removedDestinations: map[string]bool{}, removedRegistrations: map[string]bool{}, changedDirectories: map[string]bool{}}
	state := cloneV3Native(baseline)
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		if id == self.PlanID {
			return nil, failure("invalid-options", "A plan cannot be its own sibling.")
		}
		plan, journal, err := readV3ChainRecords(stateDir, id)
		if err != nil {
			return nil, err
		}
		if plan.Expected.Identity.RepositoryDir != self.Expected.Identity.RepositoryDir || plan.Expected.Identity.ObjectFormat != self.Expected.Identity.ObjectFormat {
			return nil, failure("invalid-options", "A sibling plan belongs to another repository.")
		}
		if journal == nil || journal.Phase != "complete" {
			continue
		}
		for _, entry := range journal.Entries {
			for _, delta := range entry.Deltas {
				next, ok := v3ChainStep(plan, state, delta.Kind, entry.ActionID)
				if !ok {
					return nil, failure("evidence-changed", "A sibling plan's journaled effect does not fit the stored baseline of this plan.")
				}
				state = next
				chain.effects = append(chain.effects, v3ChainEffect{plan: plan, kind: delta.Kind, id: entry.ActionID})
				if delta.Kind == "registration-removed" {
					r := plan.Retirement
					chain.removedDestinations[r.Destination.Path] = true
					chain.removedRegistrations[r.Registration.Path] = true
					chain.changedDirectories[r.Parent.Path] = true
					chain.changedDirectories[r.RegistrationParent.Path] = true
				}
			}
		}
	}
	if len(chain.effects) == 0 {
		return nil, nil
	}
	return chain, nil
}

// readV3ChainRecords reads a sibling's immutable plan and, if present, its journal, read-only.
func readV3ChainRecords(stateDir, id string) (V3Plan, *V3Journal, error) {
	var plan V3Plan
	if !validID(id) {
		return plan, nil, failure("invalid-options", "A sibling plan identity is malformed.")
	}
	s, err := openV3ReadStore(stateDir)
	if err != nil {
		return plan, nil, err
	}
	defer s.Close()
	if err = s.admitID(id); err != nil {
		return plan, nil, failure("invalid-options", "A sibling plan identity is malformed.")
	}
	var stored v3StoredPlan
	if err = s.read("tidy-v3-plans", id, &stored); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return plan, nil, failure("invalid-options", "A sibling plan is not stored in the state directory of this plan.")
		}
		return plan, nil, err
	}
	if stored.Native.ProtectedRoots == nil {
		return plan, nil, failure("invalid-state", "Stored native snapshot is incomplete.")
	}
	s.protected = append([]string{}, stored.Native.ProtectedRoots...)
	plan, _, err = s.readPlan(id)
	if err != nil {
		return plan, nil, err
	}
	switch plan.Mode {
	case "legacy-retirement", "owned-retirement", "local-refs", "tracking-refs":
	default:
		return plan, nil, failure("invalid-options", "A sibling plan has an unsupported mode.")
	}
	journal, err := s.readJournal(plan)
	if errors.Is(err, os.ErrNotExist) {
		return plan, nil, nil
	}
	if err != nil {
		return plan, nil, err
	}
	return plan, &journal, nil
}

// v3ChainStep applies one journaled delta to a state, but only if the state is one the delta
// can have come from: a recovery reference must not exist yet, a deleted reference must exist at
// the planned OID and a removed registration must be registered.
func v3ChainStep(p V3Plan, s V3NativeState, kind, id string) (V3NativeState, bool) {
	var target V3Candidate
	found := false
	for _, c := range v3Eligible(p) {
		if c.ActionID == id {
			target, found = c, true
		}
	}
	if !found {
		return s, false
	}
	switch kind {
	case "recovery-created":
		name := v3RecoveryFor(p, id, target.OID).Name
		for _, r := range s.References {
			if r.Name == name {
				return s, false
			}
		}
	case "reference-deleted":
		present := false
		for _, r := range s.References {
			if r.Name == target.Name && r.OID == target.OID {
				present = true
			}
		}
		if !present {
			return s, false
		}
	case "registration-removed":
		present := false
		for _, r := range s.Registrations {
			if p.Retirement != nil && r.Path == p.Retirement.Destination.Path {
				present = true
			}
		}
		if !present {
			return s, false
		}
	default:
		return s, false
	}
	next := v3ApplyStateDelta(p, s, kind, id)
	return next, !reflect.DeepEqual(next, s)
}

// apply returns the snapshot and native state that the live repository must show when the
// chain's effects are added to a state that holds none of them. A nil chain changes nothing.
func (c *v3Chain) apply(expected Snapshot, native V3NativeState) (Snapshot, V3NativeState) {
	if c == nil {
		return expected, native
	}
	for _, e := range c.effects {
		native = v3ApplyStateDelta(e.plan, native, e.kind, e.id)
	}
	return v3SnapshotWithState(expected, native), native
}

// retirement returns the stored retirement facts with the chain's removals taken out of the
// lists they appear in. Every other fact is copied unchanged.
func (c *v3Chain) retirement(r V3Retirement) V3Retirement {
	if c == nil {
		return r
	}
	r.RegisteredRoots = c.without(r.RegisteredRoots, c.removedDestinations)
	r.Siblings = c.withoutIdentities(r.Siblings, c.removedDestinations)
	r.RegistrationSiblings = c.withoutIdentities(r.RegistrationSiblings, c.removedRegistrations)
	r.Integration.WorkspacePaths = c.without(r.Integration.WorkspacePaths, c.removedDestinations)
	return r
}

func (c *v3Chain) without(paths []string, drop map[string]bool) []string {
	out := []string{}
	for _, p := range paths {
		if !drop[p] {
			out = append(out, p)
		}
	}
	return out
}

func (c *v3Chain) withoutIdentities(list []V3PhysicalIdentity, drop map[string]bool) []V3PhysicalIdentity {
	out := []V3PhysicalIdentity{}
	for _, f := range list {
		if !drop[f.Path] {
			out = append(out, f)
		}
	}
	return out
}

// changed reports whether the chain removed a child of the directory, which changes the
// directory's size and timestamps and nothing else.
func (c *v3Chain) changed(path string) bool {
	return c != nil && c.changedDirectories[path]
}

// sameRetirement compares observed retirement facts with the stored ones as the chain changes
// them: removed entries disappear from the lists, and a directory a removal happened in may
// differ in the attributes a child removal changes (retirementV3SamePostParent).
func (c *v3Chain) sameRetirement(got, stored V3Retirement) bool {
	if c == nil {
		return reflect.DeepEqual(got, stored)
	}
	want := c.retirement(stored)
	got.Parent = c.settled(got.Parent, want.Parent)
	got.RegistrationParent = c.settled(got.RegistrationParent, want.RegistrationParent)
	got.Siblings = c.settledList(got.Siblings, want.Siblings)
	got.RegistrationSiblings = c.settledList(got.RegistrationSiblings, want.RegistrationSiblings)
	return reflect.DeepEqual(got, want)
}

func (c *v3Chain) settled(got, want V3PhysicalIdentity) V3PhysicalIdentity {
	if c.changed(want.Path) && retirementV3SamePostParent(got, want) {
		return want
	}
	return got
}

func (c *v3Chain) settledList(got, want []V3PhysicalIdentity) []V3PhysicalIdentity {
	if len(got) != len(want) {
		return got
	}
	out := append([]V3PhysicalIdentity{}, got...)
	for i := range out {
		out[i] = c.settled(out[i], want[i])
	}
	return out
}
