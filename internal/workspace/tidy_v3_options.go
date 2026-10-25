// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"time"
)

// v3HintSelector renders a validated hint (40 hex digits and a number, far
// below the 4,096-byte message limit) as the quoted flag text that selected it.
func v3HintSelector(h V3PRHint) string {
	return "--pr " + strconv.Quote(h.OID+"="+strconv.FormatInt(h.Number, 10))
}

// normalizeV3PRHints keys hints by head OID: identical repeats collapse to one
// hint (Spec 031 FR-014) and the result is ordered by OID so equal selections
// store equal plans. Callers validate conflicts first.
func normalizeV3PRHints(hints []V3PRHint) []V3PRHint {
	byOID := map[string]V3PRHint{}
	for _, h := range hints {
		byOID[h.OID] = h
	}
	out := make([]V3PRHint, 0, len(byOID))
	for _, h := range byOID {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].OID < out[j].OID })
	return out
}

type V3PlanOptions struct {
	Path, Base, StateDir               string
	Refs                               []string
	Task, Owner                        string
	CompletedWorktree                  string
	RemoveRegistration                 bool
	RemoveFiles                        string
	ProtectWorktrees                   []string
	Online                             bool
	Remote, TrackingRemote, Repo, Host string
	PRHints                            []V3PRHint
	// IntegrationProofs narrows the forge-free proof classes (FR-006); empty
	// means all four. AcceptWhitespaceDifferences makes a patch-id match that
	// differs only in whitespace deletable.
	IntegrationProofs           []string
	AcceptWhitespaceDifferences bool
	// ReportDir is the owner-named absolute directory of the review report
	// (FR-032). The three bounds are 0 for their defaults (FR-035).
	ReportDir                                            string
	ReportMaxEntries, ReportMaxBytes, ReportMaxDiffBytes int
	// Per-entry report bounds (0: defaults of 16 files, 1,000 lines, 50 commits).
	ReportMaxFiles, ReportMaxLines, ReportMaxCommits int
	// ConsolidationRanges are `A..B` selectors whose commits the report pairs
	// with each reported branch (FR-037), at most four.
	ConsolidationRanges []string
	// DecisionsFile is the absolute path of a reviewer decision file (FR-039).
	DecisionsFile string
	// Timeout replaces the default deadlines of the operation when positive.
	Timeout time.Duration
}
type V3ApplyOptions struct {
	StateDir, PlanID         string
	Authorize, Online        bool
	CompletedWorktree, Owner string
	ProtectWorktrees         []string
	// Timeout replaces the default deadlines of the operation when positive.
	Timeout time.Duration
	// RunMode is the mode evidence recorded in the journal (Spec 041); nil records none.
	RunMode *V3RunMode
	// SiblingPlans names the other plans of the same plan set and repository, in the same state
	// directory (Spec 041 FR-031). Only a sibling whose journal is complete contributes: its
	// journaled effects explain the live repository, so a plan stored before they happened
	// does not turn stale because of them. Anything else stays strictly compared.
	SiblingPlans []string
}

func validateV3PlanOptions(o V3PlanOptions) error {
	bad := func() error {
		return failure("invalid-options", "V3 options must select one exact mode, canonical paths and explicit online inputs.")
	}
	if !v3CanonicalPath(o.Path) || o.Base == "" || !validText(o.Base) || (o.StateDir != "" && !v3CanonicalPath(o.StateDir)) || !v3Paths(o.ProtectWorktrees) || (o.Host != "" && o.Host != "github.com") {
		return bad()
	}
	retire := o.Task != "" || o.CompletedWorktree != ""
	if o.Task != "" && o.CompletedWorktree != "" {
		return bad()
	}
	if retire {
		if o.StateDir == "" || !validID(o.Owner) || !o.RemoveRegistration || !v3CanonicalPath(o.RemoveFiles) || len(o.Refs) > 0 || o.TrackingRemote != "" {
			return bad()
		}
		if o.Task != "" {
			if !validID(o.Task) || len(o.ProtectWorktrees) > 0 {
				return bad()
			}
		} else if !v3CanonicalPath(o.CompletedWorktree) || o.RemoveFiles != o.CompletedWorktree {
			return bad()
		}
	} else if o.Owner != "" || o.RemoveRegistration || o.RemoveFiles != "" || len(o.ProtectWorktrees) > 0 {
		return bad()
	}
	if !o.Online {
		if o.Remote != "" || o.TrackingRemote != "" || o.Host != "" || o.Repo != "" || len(o.PRHints) > 0 {
			return bad()
		}
	} else {
		remote := o.Remote
		if o.TrackingRemote != "" {
			if remote != "" || len(o.PRHints) > 0 {
				return bad()
			}
			remote = o.TrackingRemote
		}
		if !validID(remote) {
			return bad()
		}
		if o.Repo != "" && !v3Repo(V3Repository{Host: "github.com", ID: 1, NodeID: "selector", Name: o.Repo}) {
			return bad()
		}
	}
	refs := map[string]bool{}
	for _, name := range o.Refs {
		prefix := "refs/heads/"
		if o.TrackingRemote != "" {
			prefix = "refs/remotes/" + o.TrackingRemote + "/"
		}
		if !v3Ref(name) || len(name) <= len(prefix) || name[:len(prefix)] != prefix || refs[name] {
			return bad()
		}
		refs[name] = true
	}
	if o.TrackingRemote != "" && (len(o.IntegrationProofs) > 0 || o.AcceptWhitespaceDifferences) {
		return bad()
	}
	if _, err := v3ProofPolicyFromOptions(o); err != nil {
		return err
	}
	if o.reviewRequested() {
		if retire || o.TrackingRemote != "" {
			return bad()
		}
		if _, err := resolveReportBounds(o.reportBounds()); err != nil {
			return err
		}
		if o.ReportDir != "" {
			if err := checkReportDirectory(o.ReportDir, nil); err != nil {
				return err
			}
		}
		if len(o.ConsolidationRanges) > reportMaxRanges {
			return failure("invalid-options", "--consolidation-range may be given at most four times.")
		}
		for _, selector := range o.ConsolidationRanges {
			if _, _, err := parseConsolidationRange(selector); err != nil {
				return err
			}
		}
	}
	hints := map[string]int64{}
	for _, h := range o.PRHints {
		if !v3OID(h.OID, "sha1") || h.Number <= 0 {
			return bad()
		}
		if n, ok := hints[h.OID]; ok && n != h.Number {
			return failure("invalid-options", fmt.Sprintf("%s conflicts with %s for head %s.", v3HintSelector(V3PRHint{OID: h.OID, Number: n}), v3HintSelector(h), h.OID))
		}
		hints[h.OID] = h.Number
	}
	if len(o.Refs)+len(o.PRHints)+len(o.ProtectWorktrees) > observationLimit {
		return bad()
	}
	return nil
}
func validateV3ApplyOptions(o V3ApplyOptions, p V3Plan) error {
	if !o.Authorize || o.StateDir != p.StateDir || o.PlanID != p.PlanID || !v3CanonicalPath(o.StateDir) || !validID(o.PlanID) || o.Online != (p.Online != nil) || !v3Paths(o.ProtectWorktrees) {
		return failure("authorization-required", "Apply requires exact stored identity and explicit local/online acknowledgments.")
	}
	if p.Mode == "legacy-retirement" {
		if p.Retirement == nil || p.Retirement.Provenance.Legacy == nil {
			return failure("invalid-plan", "Legacy provenance is missing.")
		}
		l := p.Retirement.Provenance.Legacy
		if o.CompletedWorktree != l.SelectedPath || o.Owner != l.Owner || !reflect.DeepEqual(o.ProtectWorktrees, l.ProtectedRoots) {
			return failure("completion-required", "Repeat the exact current completion owner, destination and complete protection set.")
		}
	} else if o.CompletedWorktree != "" || o.Owner != "" || len(o.ProtectWorktrees) > 0 {
		return failure("invalid-options", "Completion declarations apply only to legacy retirement.")
	}
	return nil
}

// v3ProofPolicyFromOptions canonicalizes the selected kinds (FR-006): a subset
// of the four classes in the order of FR-006, so the order given never changes
// the plan identity. Unknown or duplicate kinds are usage errors.
func v3ProofPolicyFromOptions(o V3PlanOptions) (V3ProofPolicy, error) {
	policy := defaultIntegrationPolicy()
	policy.AcceptWhitespaceDifferences = o.AcceptWhitespaceDifferences
	if len(o.IntegrationProofs) == 0 {
		return policy, nil
	}
	seen := map[string]bool{}
	for _, kind := range o.IntegrationProofs {
		if !slices.Contains(integrationKindOrder, kind) || seen[kind] {
			return policy, failure("invalid-options", "Integration proof kinds must be distinct members of ancestor, patch-id, merge-noop and no-net-change.")
		}
		seen[kind] = true
	}
	policy.Kinds = []string{}
	for _, kind := range integrationKindOrder {
		if seen[kind] {
			policy.Kinds = append(policy.Kinds, kind)
		}
	}
	return policy, nil
}

// planObservationTimeout is the deadline of one plan or one native observation.
// It was a fixed 10 s that a loaded runner or a repository of a thousand files
// could exceed; it is now 60 s. It is deterministic, not scaled by the
// repository, and a caller-chosen timeout (--timeout) may override it.
const planObservationTimeout = 60 * time.Second

// planOnlineTimeout is the extra online phase of a plan.
const planOnlineTimeout = 30 * time.Second

// v3ProofPlanTimeout is the total planning time of a local-reference plan: the
// 60 s observation, the 60 s forge-free proof phase and, online, the 30 s
// online phase (Spec 029 contract amendment, FR-008).
func v3ProofPlanTimeout(o V3PlanOptions) time.Duration {
	if o.Online {
		return planObservationTimeout + 60*time.Second + planOnlineTimeout
	}
	return planObservationTimeout + 60*time.Second
}

// reviewRequested reports whether any review report option is set.
func (o V3PlanOptions) reviewRequested() bool {
	return o.ReportDir != "" || o.ReportMaxEntries != 0 || o.ReportMaxBytes != 0 || o.ReportMaxDiffBytes != 0 || o.ReportMaxFiles != 0 || o.ReportMaxLines != 0 || o.ReportMaxCommits != 0 || len(o.ConsolidationRanges) > 0 || o.DecisionsFile != ""
}

func (o V3PlanOptions) reportBounds() reportBounds {
	return reportBounds{MaxEntries: o.ReportMaxEntries, MaxBytes: o.ReportMaxBytes, MaxDiffBytes: o.ReportMaxDiffBytes, MaxFiles: o.ReportMaxFiles, MaxLines: o.ReportMaxLines, MaxCommits: o.ReportMaxCommits}
}
