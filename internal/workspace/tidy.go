// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/wstein/sourcesteward/internal/inspection"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
)

type tidyCoordinator struct {
	inspect func(context.Context, string) inspection.Result
	adapter func(string) (tidyAdapter, error)
}

func PlanTidy(ctx context.Context, o TidyPlanOptions) TidyResult {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	c := tidyCoordinator{inspect: inspection.Inspect, adapter: selectTidyAdapter}
	return boundedTidy(c.plan(ctx, o))
}
func selectTidyAdapter(backend string) (tidyAdapter, error) {
	switch backend {
	case "git":
		return gitTidyAdapter{}, nil
	case "jj":
		return jjTidyAdapter{}, nil
	}
	return nil, failure("unsupported-backend", "Tidy requires unambiguous native Git or jj ownership.")
}
func refuseTidy(r TidyResult, code, message string) TidyResult {
	r.Status = "refused"
	r.Plan = nil
	r.Diagnostics = []Diagnostic{{code, message}}
	return r
}
func failTidy(r TidyResult, err error) TidyResult {
	failed := failResult(newResult(r.Command), err)
	r.Status = failed.Status
	r.Plan = nil
	r.Diagnostics = failed.Diagnostics
	return r
}
func (c tidyCoordinator) plan(ctx context.Context, o TidyPlanOptions) TidyResult {
	r := newTidyResult("tidy-plan")
	if o.Base == "" || !validText(o.Base) || !validText(o.StateDir) || len(o.Refs) > observationLimit || (o.Refs != nil && !validStrings(o.Refs)) || o.Task != "" || o.Owner != "" || o.RemoveRegistration || o.RemoveFiles != "" {
		return refuseTidy(r, "invalid-input", "An explicit bounded base and local reference selection are required; workspace retirement is not supported by this plan contract.")
	}
	selected := append([]string{}, o.Refs...)
	sort.Strings(selected)
	for i, name := range selected {
		if name == "" || (i > 0 && name == selected[i-1]) {
			return refuseTidy(r, "invalid-input", "Reference selectors must be nonempty unique exact native local names.")
		}
	}
	if err := ctx.Err(); err != nil {
		return failTidy(r, err)
	}
	source, err := canonicalSource(o.Path)
	if err != nil {
		return failTidy(r, err)
	}
	identity := c.inspect(ctx, source)
	r.Backend = identity.Backend
	if identity.Status != "ok" || (identity.Backend != "git" && identity.Backend != "jj") {
		return refuseTidy(r, "unsupported-backend", "Tidy requires successful unambiguous native Git or jj ownership inspection.")
	}
	adapter, err := c.adapter(identity.Backend)
	if err != nil {
		return failTidy(r, err)
	}
	observation, err := adapter.Observe(ctx, source, o.Base)
	if err != nil {
		return failTidy(r, err)
	}
	if err = validateTidyObservation(observation, identity.Backend, source, o.Base); err != nil {
		return failTidy(r, err)
	}
	if o.StateDir != "" {
		if !observation.WorkspacePathsComplete {
			return failTidy(r, failure("unknown-workspace-path", "Cannot prove state-store disjointness because a registered workspace root is unavailable."))
		}
		if err = qualifyTidyWorkspaceRoots(ctx, observation.WorkspacePaths, o.StateDir); err != nil {
			return failTidy(r, err)
		}
	}
	candidates := append([]TidyCandidate{}, observation.Candidates...)
	if len(selected) > 0 {
		candidates = []TidyCandidate{}
		for _, name := range selected {
			found := false
			for _, candidate := range observation.Candidates {
				if candidate.Name == name {
					candidates = append(candidates, candidate)
					found = true
					break
				}
			}
			if !found {
				return refuseTidy(r, "unknown-reference", "An explicitly selected local reference was not completely observed.")
			}
		}
	}
	p := TidyPlanRecord{SchemaVersion: 1, SourcePath: source, StateDir: o.StateDir, Backend: identity.Backend, Expected: observation.Expected, SelectedRefs: selected, Candidates: candidates, Effects: []string{}, RecoveryLimits: []string{"Evidence is sampled and nontransactional; external or ABA changes may be missed.", "A plan does not authorize deletion; references, registrations, files and native state are unchanged.", "Only native ancestry is supported; squash-equivalence and forge proof are unavailable and do not authorize deletion."}}
	r.Limitations = append(r.Limitations, observation.Limitations...)
	if !observation.WorkspacePathsComplete {
		r.Limitations = append(r.Limitations, "A native workspace root is unavailable; persistent state-store disjointness cannot be proven.")
	}
	r.Limitations = append(r.Limitations, p.RecoveryLimits...)
	if o.StateDir == "" {
		if err = qualifyTidyWorkspaceRoots(ctx, observation.WorkspacePaths, ""); err != nil {
			if ctx.Err() != nil {
				return failTidy(r, ctx.Err())
			}
			r.Limitations = append(r.Limitations, "At least one registered workspace root is missing or noncanonical; no writable state-store disjointness was proven.")
		}
	}
	if o.StateDir != "" {
		p.Effects = []string{"Persist one immutable plan in the explicitly selected private state directory; no native repository effects."}
		if err = disjointTidyStore(p); err != nil {
			return failTidy(r, err)
		}
	}
	// Validate before a second observation or any store write. Incomplete evidence
	// never becomes an eligible subset by filtering omitted native records.
	p.Digest = tidyPlanDigest(p)
	if err = validateTidyPlan(p, p.StateDir, ""); err != nil {
		return failTidy(r, err)
	}
	probe := r
	probe.Plan = &p
	if o.StateDir != "" {
		probe.Actions = []ActionOutcome{{"store-plan", "completed", "Immutable evidence plan stored; no native reference, registration or file was changed."}}
	}
	if !withinTidyBounds(probe) {
		return failTidy(r, failure("record-limit", "Tidy evidence exceeds its one-MiB or aggregate1024-record bound."))
	}
	fresh, err := adapter.Observe(ctx, source, o.Base)
	if err != nil {
		return failTidy(r, err)
	}
	if !reflect.DeepEqual(observation, fresh) {
		return refuseTidy(r, "stale-plan", "Native identities, base, references, workspaces or safety evidence changed while planning; create a new plan.")
	}
	finalIdentity := c.inspect(ctx, source)
	if finalIdentity.Status != "ok" || finalIdentity.Backend != identity.Backend {
		return refuseTidy(r, "changed-source", "Native workspace ownership changed while planning.")
	}
	if err = ctx.Err(); err != nil {
		return failTidy(r, err)
	}
	if o.StateDir != "" {
		if err = qualifyTidyWorkspaceRoots(ctx, fresh.WorkspacePaths, o.StateDir); err != nil {
			return failTidy(r, err)
		}
		s, err := openTidyStore(o.StateDir, true)
		if err != nil {
			return failTidy(r, err)
		}
		defer s.Close()
		ids, err := s.list("tidy-plans")
		if err != nil {
			return failTidy(r, err)
		}
		if len(ids) >= observationLimit {
			return failTidy(r, failure("record-limit", "Private state has1024 tidy plans."))
		}
		p.PlanID, err = randomID()
		if err != nil {
			return failTidy(r, err)
		}
		p.Digest = tidyPlanDigest(p)
		if err = validateTidyPlan(p, o.StateDir, p.PlanID); err != nil {
			return failTidy(r, err)
		}
		probe.Plan = &p
		if !withinTidyBounds(probe) {
			return failTidy(r, failure("record-limit", "Stored plan exceeds output bounds."))
		}
		if err = ctx.Err(); err != nil {
			return failTidy(r, err)
		}
		if err = s.write("tidy-plans", p.PlanID, p, true); err != nil {
			return failTidy(r, err)
		}
		r.Actions = append(r.Actions, ActionOutcome{"store-plan", "completed", "Immutable evidence plan stored; no native reference, registration or file was changed."})
	}
	r.Status = "ok"
	r.Plan = &p
	return boundedTidy(r)
}
func openTidyStore(path string, write bool) (*store, error) {
	return openStoreKinds(path, write, []string{"tidy-plans", "tidy-operations"})
}
func tidyPlanDigest(p TidyPlanRecord) string {
	p.Digest = ""
	b, _ := json.Marshal(p)
	h := sha256.Sum256(b)
	return fmt.Sprintf("%x", h)
}
func validateTidyObservation(o TidyObservation, backend, source, base string) error {
	if !validSnapshot(o.Expected) || o.Expected.Identity.Backend != backend || o.Expected.Identity.WorkspaceRoot != source || o.Expected.BaseSelector != base || o.Candidates == nil || !validTidyPaths(o.WorkspacePaths) || !validStrings(o.Limitations) {
		return failure("invalid-evidence", "Native tidy observation is incomplete or malformed.")
	}
	names := map[string]bool{}
	for _, c := range o.Candidates {
		if names[c.Name] || !validTidyCandidate(c, backend, o.Expected.Identity.ObjectFormat) {
			return failure("invalid-evidence", "Native tidy candidate is repeated, incomplete or malformed.")
		}
		names[c.Name] = true
	}
	probe := newTidyResult("tidy-plan")
	probe.Plan = &TidyPlanRecord{Expected: o.Expected, Candidates: o.Candidates, SelectedRefs: []string{}, Effects: []string{}, RecoveryLimits: []string{}}
	probe.Limitations = append(append([]string{}, o.Limitations...), o.WorkspacePaths...)
	if !withinTidyBounds(probe) {
		return failure("record-limit", "Native tidy observation exceeds bounded complete evidence.")
	}
	return nil
}
func validTidyCandidate(c TidyCandidate, backend, format string) bool {
	for _, s := range []string{c.Kind, c.Name, c.CommitID, c.ChangeID, c.Decision, c.Reason, c.Proof} {
		if !validText(s) {
			return false
		}
	}
	if c.Name == "" || c.Reason == "" || !validStrings(c.WorkspacePaths) || !validStrings(c.Limitations) || (c.Decision != "delete" && c.Decision != "retain") || (c.Proof != "native-ancestry" && c.Proof != "none") {
		return false
	}
	if c.Decision == "delete" && (c.Proof != "native-ancestry" || len(c.WorkspacePaths) > 0) {
		return false
	}
	seen := map[string]bool{}
	for _, path := range c.WorkspacePaths {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || seen[path] {
			return false
		}
		seen[path] = true
	}
	if backend == "git" {
		size := 40
		if format == "sha256" {
			size = 64
		}
		return c.Kind == "branch" && strings.HasPrefix(c.Name, "refs/heads/") && len(c.Name) > len("refs/heads/") && c.ChangeID == "" && validAlphabet(c.CommitID, size, "0123456789abcdef")
	}
	if backend != "jj" || c.Kind != "bookmark" {
		return false
	}
	if c.CommitID == "" || c.ChangeID == "" {
		return c.CommitID == "" && c.ChangeID == "" && c.Decision == "retain" && c.Proof == "none" && c.Reason == "ambiguous-target"
	}
	return validAlphabet(c.CommitID, 40, "0123456789abcdef") && validAlphabet(c.ChangeID, 32, "klmnopqrstuvwxyz")
}
func validateTidyPlan(p TidyPlanRecord, state, id string) error {
	if p.SchemaVersion != 1 || p.StateDir != state || p.PlanID != id || (id != "" && !validID(id)) || (state == "" && id != "") || !validAlphabet(p.Digest, 64, "0123456789abcdef") || p.Digest != tidyPlanDigest(p) || !filepath.IsAbs(p.SourcePath) || filepath.Clean(p.SourcePath) != p.SourcePath || !validText(p.SourcePath) || !validText(p.StateDir) || !validStrings(p.SelectedRefs) || !validStrings(p.Effects) || !validStrings(p.RecoveryLimits) {
		return failure("invalid-plan", "Tidy plan is incompatible, modified or not bound to this private store.")
	}
	observation := TidyObservation{Expected: p.Expected, Candidates: p.Candidates, Limitations: []string{}, WorkspacePaths: []string{}}
	if err := validateTidyObservation(observation, p.Backend, p.SourcePath, p.Expected.BaseSelector); err != nil {
		return err
	}
	selected := map[string]bool{}
	for _, name := range p.SelectedRefs {
		if name == "" || selected[name] {
			return failure("invalid-plan", "Tidy selection is repeated or malformed.")
		}
		selected[name] = true
	}
	if len(selected) > 0 {
		if len(selected) != len(p.Candidates) {
			return failure("invalid-plan", "Tidy candidates do not match selected references.")
		}
		for _, c := range p.Candidates {
			if !selected[c.Name] {
				return failure("invalid-plan", "Unselected tidy candidate in plan.")
			}
		}
	}
	r := newTidyResult("tidy-plan")
	r.Plan = &p
	if !withinTidyBounds(r) {
		return failure("record-limit", "Tidy plan exceeds its complete evidence bounds.")
	}
	return disjointTidyStore(p)
}
func disjointTidyStore(p TidyPlanRecord) error {
	if p.StateDir == "" {
		return nil
	}
	if !filepath.IsAbs(p.StateDir) || filepath.Clean(p.StateDir) != p.StateDir {
		return failure("unsafe-state", "Tidy state directory must be a canonical absolute path.")
	}
	paths := []string{p.SourcePath, p.Expected.Identity.RepositoryDir, p.Expected.Identity.WorkspaceRoot, p.Expected.Identity.GitDir}
	for _, c := range p.Candidates {
		paths = append(paths, c.WorkspacePaths...)
	}
	for _, path := range paths {
		if path != "" && overlaps(p.StateDir, path) {
			return failure("state-overlap", "Tidy state directory must be disjoint from all observed workspaces and native metadata.")
		}
	}
	return nil
}
func withinTidyBounds(r TidyResult) bool {
	count := len(r.Actions) + len(r.Diagnostics) + len(r.Limitations)
	if r.Plan != nil {
		p := r.Plan
		count += len(p.SelectedRefs) + len(p.Candidates) + len(p.Effects) + len(p.RecoveryLimits)
		for _, c := range p.Candidates {
			count += len(c.WorkspacePaths) + len(c.Limitations)
		}
	}
	if count > observationLimit {
		return false
	}
	b, err := json.Marshal(r)
	return err == nil && len(b) <= recordLimit
}
func boundedTidy(r TidyResult) TidyResult {
	if withinTidyBounds(r) {
		return r
	}
	safe := newTidyResult("tidy-plan")
	if r.Command == "tidy-apply" {
		safe.Command = r.Command
	}
	switch r.Backend {
	case "git", "jj", "unknown", "ambiguous", "gitbutler":
		safe.Backend = r.Backend
	}
	safe.Status = "partial"
	safe.Diagnostics = []Diagnostic{{"result-limit", "Tidy output exceeded its one-MiB or aggregate1024-record bound; no omitted evidence authorizes mutation."}}
	return safe
}

func validTidyPaths(paths []string) bool {
	if !validStrings(paths) {
		return false
	}
	seen := map[string]bool{}
	for _, path := range paths {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || seen[path] {
			return false
		}
		seen[path] = true
	}
	return true
}

// A native registration can contain a lexical symlink alias or missing root.
// Such paths are evidence, but never proof that a writable store is disjoint.
func qualifyTidyWorkspaceRoots(ctx context.Context, paths []string, state string) error {
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := currentIdentity(path); err != nil {
			return failure("unknown-workspace-path", "A registered workspace root is missing, replaced or not a canonical real directory; state-store disjointness is unproven.")
		}
		if overlaps(state, path) {
			return failure("state-overlap", "Tidy state directory must be disjoint from every registered workspace, including unselected or detached work.")
		}
	}
	return nil
}
