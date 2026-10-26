// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

// ObserveRegistrationPruneV4 reuses the complete expert admission twice under
// one deadline. It creates no plan, store, lock file, recovery payload or ref.
// Other proven owners may be viewed; selection/authorization is a caller gate.
func ObserveRegistrationPruneV4(ctx context.Context, o RegistrationPruneV4Options) RegistrationPruneV4Observation {
	ctx, cancel := context.WithTimeout(ctx, planObservationTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return refusedPruneV4Overview(err)
	}
	if err := validatePruneV4Options(o); err != nil {
		return refusedPruneV4Overview(err)
	}
	source, err := canonicalSource(o.Path)
	if err != nil {
		return refusedPruneV4Overview(err)
	}
	o.Path = source
	ctx, _, closeSession, err := withPlanningSession(ctx)
	if err != nil {
		return refusedPruneV4Overview(err)
	}
	defer closeSession()
	first, reg, opts, err := observePruneV4(ctx, o, nil)
	if err != nil {
		return refusedPruneV4Overview(err)
	}
	second, reg2, _, err := observePruneV4(ctx, o, &opts)
	if err != nil {
		return refusedPruneV4Overview(err)
	}
	if !reflect.DeepEqual(first, second) || reg != reg2 {
		return refusedPruneV4Overview(failure("prune-evidence-changed", "Prune evidence changed during read-only overview observation."))
	}
	if err = ctx.Err(); err != nil {
		return refusedPruneV4Overview(err)
	}
	digest, err := pruneV4OverviewDigest(reg, first)
	if err != nil {
		return refusedPruneV4Overview(err)
	}
	return RegistrationPruneV4Observation{Status: "ok", Registration: reg, Expected: first, EvidenceDigest: digest, admittedDigest: digest, Limitations: []string{
		"Absence does not prove deletion rather than a move; administrative restore and separately authorized owner-run repair may be needed.",
		"Evidence is sampled; external mount transitions and same-user ABA races are not atomically excluded.",
		"No recovery is prepared; fresh planning and verified private payload/history recovery are required before metadata-only removal. Recovery has no automatic expiry.",
	}}
}

// ObserveRegistrationPruneV4Inventory reads native registrations and their exact
// immediate administrative names. No checkout content/history or online proof
// is observed. Inaccessible destinations remain candidates for refusal.
func ObserveRegistrationPruneV4Inventory(ctx context.Context, path string) RegistrationPruneV4Inventory {
	out := RegistrationPruneV4Inventory{Status: "refused", Candidates: []RegistrationPruneV4Candidate{}}
	fail := func(err error) RegistrationPruneV4Inventory {
		out.Diagnostic = refusedPruneV4Overview(err).Diagnostic
		out.Candidates = []RegistrationPruneV4Candidate{}
		return out
	}
	ctx, cancel := context.WithTimeout(ctx, planObservationTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	source, err := canonicalSource(path)
	if err != nil {
		return fail(err)
	}
	ctx, _, closeSession, err := withPlanningSession(ctx)
	if err != nil {
		return fail(err)
	}
	defer closeSession()
	id, err := gitNativeIdentity(ctx, source)
	if err != nil {
		return fail(err)
	}
	bin, err := gitExecutable()
	if err != nil {
		return fail(err)
	}
	native, err := readTidyV3Registrations(gitCollectionReaderFor(ctx, bin, source), id.ObjectFormat)
	if err != nil {
		return fail(err)
	}
	parent := filepath.Join(id.RepositoryDir, "worktrees")
	if _, err = os.Lstat(parent); errors.Is(err, os.ErrNotExist) {
		if len(native) != 1 {
			return fail(failure("prune-sibling-refused", "Linked registration inventory has no administrative parent."))
		}
		out.Status = "ok"
		return out
	} else if err != nil {
		return fail(err)
	}
	siblings, err := observePruneV4Siblings(ctx, id.RepositoryDir)
	if err != nil {
		return fail(err)
	}
	byPath := map[string]RegistrationPruneV4Candidate{}
	for _, sibling := range siblings {
		if err = ctx.Err(); err != nil {
			return fail(err)
		}
		raw, _, _, err := gitLayoutFile(filepath.Join(sibling.Identity.Path, "gitdir"))
		if err != nil {
			return fail(failure("prune-pointer-refused", "Administrative destination pointer is unavailable."))
		}
		pointer := strings.TrimSuffix(string(raw), "\n")
		if !v3CanonicalPath(pointer) || filepath.Base(pointer) != ".git" {
			return fail(failure("prune-pointer-refused", "Administrative destination pointer is not an exact native backlink."))
		}
		dest := filepath.Dir(pointer)
		if _, duplicate := byPath[dest]; duplicate {
			return fail(failure("prune-sibling-refused", "Several administrative entries name the same checkout."))
		}
		task := ""
		lock, _, _, lockErr := gitLayoutFile(filepath.Join(sibling.Identity.Path, "locked"))
		if lockErr == nil {
			hint, ok := strings.CutPrefix(strings.TrimSuffix(string(lock), "\n"), "stw:")
			if ok && validID(hint) {
				task = hint
			}
		}
		byPath[dest] = RegistrationPruneV4Candidate{Name: sibling.Name, GitDir: sibling.Identity.Path, Destination: dest, Task: task}
	}
	for _, reg := range native {
		if reg.Path == id.WorkspaceRoot || reg.Path == id.RepositoryDir || reg.Bare {
			continue
		}
		if _, err = os.Lstat(reg.Path); err == nil {
			continue
		}
		candidate, ok := byPath[reg.Path]
		if !ok {
			return fail(failure("prune-registration-refused", "Missing native registration has no exact administrative mapping."))
		}
		out.Candidates = append(out.Candidates, candidate)
	}
	if err = ctx.Err(); err != nil {
		return fail(err)
	}
	out.Status = "ok"
	return out
}
