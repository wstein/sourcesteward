// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
)

type tidyV3TrackingMapping struct {
	Remote, FetchURL, Refspec, ConfigDigest string
}
type tidyV3TrackingRef struct {
	Reference                  V3Reference
	SourceRef, ProtectedReason string
	WorkspacePaths             []string
}

// Mapping qualification uses the native observation and read-only origin
// qualification. It invokes no native command, transport or credential.
func qualifyTidyV3TrackingMapping(o tidyV3NativeObservation, remote, override string) (tidyV3TrackingMapping, error) {
	bad := func() (tidyV3TrackingMapping, error) {
		return tidyV3TrackingMapping{}, failure("tracking-mapping-unsupported", "One canonical fetch URL and one standard full-head mapping with no mirror, rewrite or overlapping destination are required.")
	}
	raw, digest, err := qualifyTidyV3Remote(o, remote, override)
	if err != nil {
		return bad()
	}
	fields := strings.Split(strings.TrimSuffix(o.Config, "\x00"), "\x00")
	standard := "refs/heads/*:refs/remotes/" + remote + "/*"
	selectedURLs, selectedMappings := 0, 0
	for i := 1; i < len(fields); i += 2 {
		key, value, hasValue := strings.Cut(fields[i], "\n")
		name, kind, ok := tidyV3RemoteConfigKey(key)
		if !ok {
			continue
		}
		if name == remote {
			switch kind {
			case "url":
				selectedURLs++
				if !hasValue || value != raw {
					return bad()
				}
			case "fetch":
				selectedMappings++
				if !hasValue || strings.TrimPrefix(value, "+") != standard {
					return bad()
				}
			case "mirror":
				if !hasValue {
					return bad()
				}
				switch strings.ToLower(strings.TrimSpace(value)) {
				case "false", "no", "off", "0":
				default:
					return bad()
				}
			}
			continue
		}
		if kind == "fetch" && tidyV3TrackingOverlap(value, "refs/remotes/"+remote+"/") {
			return bad()
		}
	}
	if selectedURLs != 1 || selectedMappings != 1 {
		return bad()
	}
	return tidyV3TrackingMapping{Remote: remote, FetchURL: raw, Refspec: "+" + standard, ConfigDigest: digest}, nil
}

// Native section/variable names are case-insensitive; the remote subsection is
// literal and case-sensitive. Never borrow another remote's fetch identity.
func tidyV3RemoteConfigKey(key string) (string, string, bool) {
	if len(key) < len("remote..x") || !strings.EqualFold(key[:len("remote.")], "remote.") {
		return "", "", false
	}
	rest := key[len("remote."):]
	at := strings.LastIndexByte(rest, '.')
	if at <= 0 {
		return "", "", false
	}
	return rest[:at], strings.ToLower(rest[at+1:]), true
}
func tidyV3TrackingOverlap(refspec, namespace string) bool {
	refspec = strings.TrimPrefix(refspec, "+")
	_, destination, hasDestination := strings.Cut(refspec, ":")
	if !hasDestination || destination == "" {
		return false
	}
	// An unqualified or unsupported wildcard destination cannot establish a
	// disjoint namespace. A standard mapping for a different remote is disjoint.
	if !strings.HasPrefix(destination, "refs/") {
		return true
	}
	prefix, _, _ := strings.Cut(destination, "*")
	return strings.HasPrefix(prefix, namespace) || strings.HasPrefix(namespace, prefix) || prefix == strings.TrimSuffix(namespace, "/")
}

// This selects targets for provider observation only. An unprotected ref is
// never a deletion decision until complete equal advertisements prove absence.
func selectTidyV3Tracking(o tidyV3NativeObservation, remote, override string, selected []string) (tidyV3TrackingMapping, []tidyV3TrackingRef, error) {
	mapping, err := qualifyTidyV3TrackingMapping(o, remote, override)
	if err != nil {
		return mapping, nil, err
	}
	bad := func() (tidyV3TrackingMapping, []tidyV3TrackingRef, error) {
		return mapping, nil, failure("incomplete-evidence", "Complete direct references and resolved symbolic relations are required for tracking protection.")
	}
	if len(o.Native.References)+len(o.Native.Registrations) > v3NativeFactsLimit || len(selected) > observationLimit {
		return bad()
	}
	byName := map[string]V3Reference{}
	for _, ref := range o.Native.References {
		if !v3Ref(ref.Name) || !v3OID(ref.OID, o.Expected.Identity.ObjectFormat) {
			return bad()
		}
		if _, exists := byName[ref.Name]; exists {
			return bad()
		}
		byName[ref.Name] = ref
	}
	symbolic := map[string]bool{}
	for _, ref := range o.Native.References {
		if ref.Symbolic == "" {
			continue
		}
		visiting := map[string]bool{}
		for current := ref.Name; current != ""; {
			if visiting[current] {
				return bad()
			}
			visiting[current] = true
			symbolic[current] = true
			node, exists := byName[current]
			if !exists {
				return bad()
			}
			current = node.Symbolic
		}
	}
	namespace := "refs/remotes/" + remote + "/"
	chosen := map[string]bool{}
	for _, name := range selected {
		if !v3Ref(name) || !strings.HasPrefix(name, namespace) || chosen[name] {
			return mapping, nil, failure("invalid-options", "Tracking selectors must be unique full refs in the exact selected remote namespace.")
		}
		chosen[name] = true
	}
	refs := []tidyV3TrackingRef{}
	for _, ref := range o.Native.References {
		if !strings.HasPrefix(ref.Name, namespace) || len(chosen) > 0 && !chosen[ref.Name] {
			continue
		}
		source := "refs/heads/" + strings.TrimPrefix(ref.Name, namespace)
		if !v3Ref(source) {
			return bad()
		}
		target := tidyV3TrackingRef{Reference: ref, SourceRef: source, WorkspacePaths: []string{}}
		paths := map[string]bool{}
		for _, workspace := range o.Native.Registrations {
			if workspace.HeadRef == ref.Name || workspace.HeadOID == ref.OID {
				paths[workspace.Path] = true
			}
		}
		for path := range paths {
			target.WorkspacePaths = append(target.WorkspacePaths, path)
		}
		sort.Strings(target.WorkspacePaths)
		switch {
		case symbolic[ref.Name]:
			target.ProtectedReason = "symbolic-reference"
		case ref.Name == o.Expected.BaseSelector || ref.OID == o.Expected.BaseCommit:
			target.ProtectedReason = "selected-base"
		case ref.Name == o.Expected.Identity.HeadRef || ref.OID == o.Expected.Identity.CommitID || len(target.WorkspacePaths) > 0:
			target.ProtectedReason = "workspace-used"
		}
		refs = append(refs, target)
	}
	if len(chosen) > 0 && len(refs) != len(chosen) {
		return mapping, nil, failure("unknown-reference", "A selected tracking reference was not completely observed.")
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Reference.Name < refs[j].Reference.Name })
	return mapping, refs, nil
}

type tidyV3TrackingHosting interface {
	tidyV3Hosting
	ReadBranches(context.Context) (hosting.TidyBranchAdvertisement, error)
	RevalidateBranches(context.Context, hosting.TidyBranchAdvertisement) error
}
type tidyV3TrackingCoordinator struct {
	refs        tidyV3RefsCoordinator
	openHosting func(context.Context, string, hosting.TidySessionOptions) (tidyV3TrackingHosting, error)
}

func defaultTidyV3TrackingCoordinator() tidyV3TrackingCoordinator {
	return tidyV3TrackingCoordinator{refs: defaultTidyV3RefsCoordinator(), openHosting: func(ctx context.Context, path string, o hosting.TidySessionOptions) (tidyV3TrackingHosting, error) {
		return hosting.OpenTidySession(ctx, path, o)
	}}
}

const tidyV3TrackingSamplingLimit = "Remote branch absence is sampled; server recreation after a sample remains possible. Apply declares local tracking deletions, recovery roots and journal writes; no fetch, synchronization or server deletion is performed."

func (c tidyV3TrackingCoordinator) plan(ctx context.Context, o V3PlanOptions) V3Result {
	ctx, cancel := context.WithTimeout(ctx, v3Timeout(ctx, 30*time.Second))
	defer cancel()
	ctx, _, closeSession, err := withPlanningSession(ctx)
	if err != nil {
		return boundV3Result(failTidyV3Tracking(newV3Result("tidy-plan"), err))
	}
	defer closeSession()
	return boundV3Result(c.planTracking(ctx, o))
}
func failTidyV3Tracking(r V3Result, err error) V3Result {
	r = failTidyV3(r, err)
	if len(r.Diagnostics) > 0 && r.Diagnostics[0].Code == "tracking-mapping-unsupported" {
		r.Status = "refused"
	}
	return r
}
func (c tidyV3TrackingCoordinator) planTracking(ctx context.Context, o V3PlanOptions) (r V3Result) {
	r = newV3Result("tidy-plan")
	if err := validateV3PlanOptions(o); err != nil {
		return failTidyV3Tracking(r, err)
	}
	if !o.Online || o.TrackingRemote == "" || o.Task != "" || o.CompletedWorktree != "" {
		return failTidyV3Tracking(r, failure("unsupported-mode", "Tracking planning requires its exact online tracking-remote selection."))
	}
	if err := ctx.Err(); err != nil {
		return failTidyV3Tracking(r, err)
	}
	source, err := canonicalSource(o.Path)
	if err != nil {
		return failTidyV3Tracking(r, err)
	}
	if source != o.Path {
		return failTidyV3Tracking(r, failure("invalid-input", "Source path must be canonical without aliases."))
	}
	identity := c.refs.inspect(ctx, source)
	r.Backend = identity.Backend
	if identity.Status != "ok" || identity.Backend != "git" {
		return failTidyV3Tracking(r, failure("unsupported-backend", "Tracking requires admitted native Git ownership."))
	}
	observed, err := c.refs.observe(ctx, source, o.Base)
	if err != nil {
		return failTidyV3Tracking(r, err)
	}
	if err = qualifyTidyWorkspaceRoots(ctx, v3NativeRoots(observed.Native), o.StateDir); err != nil {
		return failTidyV3Tracking(r, err)
	}
	mapping, targets, err := selectTidyV3Tracking(observed, o.TrackingRemote, o.Repo, o.Refs)
	if err != nil {
		return failTidyV3Tracking(r, err)
	}
	selected := append([]string{}, o.Refs...)
	sort.Strings(selected)
	p := V3Plan{SchemaVersion: 3, Policy: "tidy-git-v3", Mode: "tracking-refs", SourcePath: source, StateDir: o.StateDir, Expected: observed.Expected, SelectedRefs: selected, Candidates: []V3Candidate{}, Effects: []string{}, RecoveryLimits: v3RecoveryLimits(), ConfigNotes: observed.ConfigNotes}
	if !validV3NativeState(observed.Native, p, p.Expected) {
		return failTidyV3Tracking(r, failure("incomplete-evidence", "Complete native tracking, symbolic and registered workspace protections are required before online evidence."))
	}
	if p.StateDir != "" {
		p.PlanID, err = randomID()
		if err != nil {
			return failTidyV3Tracking(r, err)
		}
	}
	if err = ctx.Err(); err != nil {
		return failTidyV3Tracking(r, err)
	}
	session, err := c.openHosting(ctx, source, hosting.TidySessionOptions{Online: true, Remote: mapping.Remote, Repo: o.Repo})
	if session != nil {
		defer func() {
			budget := session.Budget()
			r.Boundary.NetworkAttempted = budget.NetworkAttempted
			r.Boundary.Budget.HTTPRequests = int64(budget.Requests)
			r.Boundary.Budget.HTTPBytes = budget.RawBytes
			session.Close()
		}()
	}
	if err != nil {
		return failTidyV3Tracking(r, tidyV3HostingError(err))
	}
	if session == nil {
		return failTidyV3Tracking(r, failure("hosting-evidence-unavailable", "A complete typed branch-advertisement session is required."))
	}
	repository := session.Repository()
	if !v3Repo(tidyV3Repository(repository)) || !v3RemoteMatches(mapping.FetchURL, repository.FullName) {
		return failTidyV3Tracking(r, failure("evidence-changed", "Provider repository identity does not match the exact selected fetch URL."))
	}
	advertisement, err := session.ReadBranches(ctx)
	if err != nil {
		return failTidyV3Tracking(r, tidyV3HostingError(err))
	}
	if err = validateTidyV3Advertisement(advertisement, repository); err != nil {
		return failTidyV3Tracking(r, err)
	}
	if err = session.RevalidateBranches(ctx, advertisement); err != nil {
		return failTidyV3Tracking(r, tidyV3HostingError(err))
	}
	p.Online = &V3OnlineSelection{Repository: tidyV3Repository(repository), Remote: mapping.Remote, RemoteURL: mapping.FetchURL, ConfigDigest: mapping.ConfigDigest, PRHints: []V3PRHint{}, ObservationDigests: []string{advertisement.Fingerprint}, BudgetPolicy: "github-read-v3"}
	present := map[string]bool{}
	for _, branch := range advertisement.Branches {
		present[branch.Name] = true
	}
	for _, target := range targets {
		ref := target.Reference
		candidate := V3Candidate{ActionID: "tracking-" + gitDigest(ref.Name)[:32], Kind: "tracking-ref", Name: ref.Name, OID: ref.OID, Decision: "retain", Reason: target.ProtectedReason, Proof: "none", WorkspacePaths: target.WorkspacePaths}
		if candidate.Reason == "" {
			if present[strings.TrimPrefix(target.SourceRef, "refs/heads/")] {
				candidate.Reason = "remote-source-present"
			} else {
				candidate.Decision, candidate.Reason, candidate.Proof = "delete", "github-branch-absent", "github-branch-absent"
				candidate.Evidence = &V3ProofEvidence{Absence: &V3AbsenceProof{Repository: p.Online.Repository, Remote: mapping.Remote, SourceRef: target.SourceRef, TrackingRef: ref.Name, Refspec: mapping.Refspec, ConfigDigest: mapping.ConfigDigest, AdvertisementDigest: advertisement.Fingerprint}}
			}
		}
		p.Candidates = append(p.Candidates, candidate)
	}
	p.Effects = v3PlanEffects(p)
	p.Digest, err = v3PlanDigest(p)
	if err != nil {
		return failTidyV3Tracking(r, err)
	}
	if err = validateV3Plan(p, p.StateDir, p.PlanID); err != nil {
		return failTidyV3Tracking(r, err)
	}
	fresh, err := c.refs.observe(ctx, source, o.Base)
	if err != nil {
		return failTidyV3Tracking(r, err)
	}
	if !sameTidyV3Observation(observed, fresh) {
		return failTidyV3Tracking(r, failure("evidence-changed", "Native configuration, references or protections changed during tracking planning."))
	}
	if err = qualifyTidyWorkspaceRoots(ctx, v3NativeRoots(fresh.Native), o.StateDir); err != nil {
		return failTidyV3Tracking(r, err)
	}
	if p.StateDir != "" {
		store, err := openV3Store(p.StateDir, true, v3NativeRoots(fresh.Native), fresh.Native.ProtectedRoots)
		if err != nil {
			return failTidyV3Tracking(r, err)
		}
		defer store.Close()
		if err = session.RevalidateBranches(ctx, advertisement); err != nil {
			return failTidyV3Tracking(r, tidyV3HostingError(err))
		}
		fresh, err = c.refs.observe(ctx, source, o.Base)
		if err != nil {
			return failTidyV3Tracking(r, err)
		}
		if !sameTidyV3Observation(observed, fresh) {
			return failTidyV3Tracking(r, failure("evidence-changed", "Native tracking evidence changed under the private plan lock."))
		}
		if err = qualifyTidyWorkspaceRoots(ctx, v3NativeRoots(fresh.Native), o.StateDir); err != nil {
			return failTidyV3Tracking(r, err)
		}
		if err = ctx.Err(); err != nil {
			return failTidyV3Tracking(r, err)
		}
		if err = store.writePlan(p, fresh.Native); err != nil {
			return failTidyV3Tracking(r, err)
		}
		r.Actions = append(r.Actions, ActionOutcome{Name: "store-plan", Status: "complete", Detail: "Stored exact tracking absence plan; native and server references were not changed."})
	}
	if err = ctx.Err(); err != nil {
		return failTidyV3Tracking(r, err)
	}
	r.Status, r.Plan, r.Boundary.Complete = "ok", &p, true
	r.Limitations = append(append([]string{}, p.RecoveryLimits...), tidyV3TrackingSamplingLimit)
	return r
}

// The provider owns collection/exhaustion and branch syntax. At this typed
// boundary bind its complete sorted inventory to the stable repository and
// recompute its full fingerprint before absence can become a local decision.
func validateTidyV3Advertisement(ad hosting.TidyBranchAdvertisement, repository hosting.TidyRepositoryIdentity) error {
	bad := func() error {
		return failure("hosting-evidence-unavailable", "A complete repository-bound branch advertisement with its exact full fingerprint is required.")
	}
	if ad.Repository != repository || ad.Branches == nil || len(ad.Branches) > observationLimit || !v3Hash(ad.Fingerprint) {
		return bad()
	}
	last := ""
	for _, branch := range ad.Branches {
		if branch.Name <= last || !validText(branch.Name) || !v3OID(branch.OID, "sha1") {
			return bad()
		}
		last = branch.Name
	}
	data, err := json.Marshal(struct {
		Repository hosting.TidyRepositoryIdentity `json:"repository"`
		Branches   []hosting.TidyBranch           `json:"branches"`
	}{ad.Repository, ad.Branches})
	if err != nil || gitDigest(string(data)) != ad.Fingerprint {
		return bad()
	}
	return nil
}
