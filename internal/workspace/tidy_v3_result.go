// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"
)

// V3Result is independent of published tidy v1/v2 records. A missing or
// incomplete observation never carries an executable plan.
type V3Result struct {
	SchemaVersion int             `json:"schema_version"`
	Command       string          `json:"command"`
	Status        string          `json:"status"`
	Backend       string          `json:"backend"`
	Plan          *V3Plan         `json:"plan"`
	Boundary      V3Boundary      `json:"boundary"`
	Actions       []ActionOutcome `json:"actions"`
	Diagnostics   []Diagnostic    `json:"diagnostics"`
	Limitations   []string        `json:"limitations"`
	// ReviewSummary is present on a local-reference plan result.
	ReviewSummary *V3ReviewSummary `json:"review_summary,omitempty"`
}
type V3Boundary struct {
	Complete         bool             `json:"complete"`
	NetworkAttempted bool             `json:"network_attempted"`
	Freshness        string           `json:"freshness"`
	ObservedAt       string           `json:"observed_at"`
	Budget           V3BudgetCounters `json:"budget"`
}
type V3BudgetCounters struct {
	NativeProcesses int64 `json:"native_processes"`
	NativeBytes     int64 `json:"native_bytes"`
	HTTPRequests    int64 `json:"http_requests"`
	HTTPBytes       int64 `json:"http_bytes"`
	Observations    int64 `json:"observations"`
	UserBytes       int64 `json:"user_bytes"`
	MetadataBytes   int64 `json:"metadata_bytes"`
	FramingBytes    int64 `json:"framing_bytes"`
	StderrBytes     int64 `json:"stderr_bytes"`
}
type V3Plan struct {
	SchemaVersion  int                `json:"schema_version"`
	PlanID         string             `json:"plan_id"`
	Digest         string             `json:"digest"`
	Policy         string             `json:"policy"`
	Mode           string             `json:"mode"`
	SourcePath     string             `json:"source_path"`
	StateDir       string             `json:"state_dir"`
	Expected       Snapshot           `json:"expected"`
	SelectedRefs   []string           `json:"selected_refs"`
	Online         *V3OnlineSelection `json:"online"`
	Candidates     []V3Candidate      `json:"candidates"`
	Retirement     *V3Retirement      `json:"retirement"`
	Effects        []string           `json:"effects"`
	RecoveryLimits []string           `json:"recovery_limits"`
	// Planning configuration notes (Spec 031 FR-020); absent when empty so plans
	// of repositories without noted configuration keep their exact bytes.
	ConfigNotes []V3ConfigNote `json:"config_notes,omitempty"`
	// Proof policy of a local-reference plan (Spec 031 FR-011); absent in plans
	// that predate it, which are evaluated by ancestry alone.
	ProofPolicy *V3ProofPolicy `json:"proof_policy,omitempty"`
	// Review binds the report bounds when a review report was requested
	// (Spec 031 FR-011, FR-035); absent otherwise.
	Review *V3ReviewBinding `json:"review,omitempty"`
}

// V3ReviewBinding is the effective report bounds bound into the plan identity.
type V3ReviewBinding struct {
	MaxEntries   int             `json:"max_entries"`
	MaxBytes     int             `json:"max_bytes"`
	MaxDiffBytes int             `json:"max_diff_bytes"`
	Ranges       []V3ReviewRange `json:"ranges"`
	// DecisionsDigest is the SHA-256 of the decision file (FR-011); Notes list
	// the decisions that were ignored (FR-039).
	DecisionsDigest string         `json:"decisions_digest,omitempty"`
	Notes           []V3ReviewNote `json:"notes"`
}

// V3ReviewSummary is the review summary of a local-reference plan result
// (FR-032): counts per bucket and reason, and what the report holds.
type V3ReviewSummary struct {
	Proven        int            `json:"proven"`
	NeedsReview   int            `json:"needs_review"`
	Retained      int            `json:"retained"`
	Reasons       map[string]int `json:"reasons"`
	ReportWritten bool           `json:"report_written"`
	Entries       int            `json:"entries"`
	Omitted       int            `json:"omitted"`
}
type V3Candidate struct {
	ActionID       string           `json:"action_id"`
	Kind           string           `json:"kind"`
	Name           string           `json:"name"`
	OID            string           `json:"oid"`
	Decision       string           `json:"decision"`
	Reason         string           `json:"reason"`
	Proof          string           `json:"proof"`
	WorkspacePaths []string         `json:"workspace_paths"`
	Evidence       *V3ProofEvidence `json:"evidence"`
}

// V3CommitProof binds integration of a commit independently of any local
// reference. A detached owned or legacy workspace has no branch to invent.
// Workspace paths remain original sampled evidence, including the target.
type V3CommitProof struct {
	OID            string           `json:"oid"`
	Decision       string           `json:"decision"`
	Reason         string           `json:"reason"`
	Proof          string           `json:"proof"`
	WorkspacePaths []string         `json:"workspace_paths"`
	Evidence       *V3ProofEvidence `json:"evidence"`
}

// Exactly one arm corresponds to the candidate's proof tag. None has no arm.
type V3ProofEvidence struct {
	Native  *V3AncestryProof `json:"native"`
	GitHub  *V3MergeProof    `json:"github"`
	Absence *V3AbsenceProof  `json:"absence"`
	// Integration is the arm of the forge-free classes and of the review and
	// hard-stop dispositions (Spec 031); absent for the Spec 029 arms.
	Integration *V3IntegrationEvidence `json:"integration,omitempty"`
}
type V3AncestryProof struct {
	TargetOID string `json:"target_oid"`
	BaseOID   string `json:"base_oid"`
}
type V3Repository struct {
	Host   string `json:"host"`
	ID     int64  `json:"id"`
	NodeID string `json:"node_id"`
	Name   string `json:"name"`
}
type V3PRSide struct {
	Repository V3Repository `json:"repository"`
	Ref        string       `json:"ref"`
	OID        string       `json:"oid"`
}
type V3MergeProof struct {
	Repository        V3Repository `json:"repository"`
	ID                int64        `json:"id"`
	NodeID            string       `json:"node_id"`
	Number            int64        `json:"number"`
	State             string       `json:"state"`
	Merged            bool         `json:"merged"`
	MergedAt          string       `json:"merged_at"`
	Draft             bool         `json:"draft"`
	Head              V3PRSide     `json:"head"`
	Base              V3PRSide     `json:"base"`
	MergeOID          string       `json:"merge_oid"`
	ChosenBaseOID     string       `json:"chosen_base_oid"`
	ObservationDigest string       `json:"observation_digest"`
}
type V3AbsenceProof struct {
	Repository          V3Repository `json:"repository"`
	Remote              string       `json:"remote"`
	SourceRef           string       `json:"source_ref"`
	TrackingRef         string       `json:"tracking_ref"`
	Refspec             string       `json:"refspec"`
	ConfigDigest        string       `json:"config_digest"`
	AdvertisementDigest string       `json:"advertisement_digest"`
}
type V3PRHint struct {
	OID    string `json:"oid"`
	Number int64  `json:"number"`
}
type V3OnlineSelection struct {
	Repository         V3Repository `json:"repository"`
	Remote             string       `json:"remote"`
	RemoteURL          string       `json:"remote_url"`
	ConfigDigest       string       `json:"config_digest"`
	PRHints            []V3PRHint   `json:"pr_hints"`
	ObservationDigests []string     `json:"observation_digests"`
	BudgetPolicy       string       `json:"budget_policy"`
	// Discovery holds one record per distinct unproven head OID, listing every
	// selected ref that carries it (Spec 031 FR-014). Absent for plans without
	// provider discovery, so earlier stored plans keep their bytes.
	Discovery []V3DiscoveryRecord `json:"discovery,omitempty"`
}
type V3PRIdentity struct {
	ID     int64  `json:"id"`
	NodeID string `json:"node_id"`
	Number int64  `json:"number"`
}
type V3DiscoveryRecord struct {
	OID      string        `json:"oid"`
	Source   string        `json:"source"`
	PR       *V3PRIdentity `json:"pr"`
	Refs     []string      `json:"refs"`
	Coverage string        `json:"coverage"`
	Result   string        `json:"result"`
}

// Physical facts are sampled with native timestamp precision, never inferred
// from an old PathIdentity that omitted owner/link/time capability.
type V3PhysicalIdentity struct {
	Path               string `json:"path"`
	Device             uint64 `json:"device"`
	Inode              uint64 `json:"inode"`
	UID                uint64 `json:"uid"`
	OwnerKnown         bool   `json:"owner_known"`
	Kind               string `json:"kind"`
	Mode               uint32 `json:"mode"`
	Nlink              uint64 `json:"nlink"`
	Size               int64  `json:"size"`
	MtimeNS            int64  `json:"mtime_ns"`
	CtimeNS            int64  `json:"ctime_ns"`
	TimestampPrecision string `json:"timestamp_precision"`
}
type V3ManifestEntry struct {
	Identity   V3PhysicalIdentity `json:"identity"`
	Digest     string             `json:"digest"`
	Executable bool               `json:"executable"`
}
type V3Manifest struct {
	Complete bool              `json:"complete"`
	Entries  []V3ManifestEntry `json:"entries"`
	Digest   string            `json:"digest"`
}
type V3OwnedProvenance struct {
	CreationPlan          MutationPlan `json:"creation_plan"`
	Task                  TaskRecord   `json:"task"`
	CreationJournalDigest string       `json:"creation_journal_digest"`
	LockReason            string       `json:"lock_reason"`
}
type V3LegacyProvenance struct {
	Owner             string             `json:"owner"`
	Completed         bool               `json:"completed"`
	SameUserAuthority bool               `json:"same_user_authority"`
	SelectedPath      string             `json:"selected_path"`
	ProtectedRoots    []string           `json:"protected_roots"`
	ObservedIdentity  V3PhysicalIdentity `json:"observed_identity"`
}
type V3Provenance struct {
	Kind   string              `json:"kind"`
	Owned  *V3OwnedProvenance  `json:"owned"`
	Legacy *V3LegacyProvenance `json:"legacy"`
}
type V3RetirementAction struct {
	Name     string   `json:"name"`
	Target   string   `json:"target"`
	Decision string   `json:"decision"`
	Reason   string   `json:"reason"`
	Blockers []string `json:"blockers"`
}
type V3Retirement struct {
	Actions              []V3RetirementAction `json:"actions"`
	RegisteredRoots      []string             `json:"registered_roots"`
	ProtectedRoots       []string             `json:"protected_roots"`
	ActionID             string               `json:"action_id"`
	Provenance           V3Provenance         `json:"provenance"`
	Destination          V3PhysicalIdentity   `json:"destination"`
	Parent               V3PhysicalIdentity   `json:"parent"`
	Registration         V3PhysicalIdentity   `json:"registration"`
	RegistrationParent   V3PhysicalIdentity   `json:"registration_parent"`
	RegistrationSiblings []V3PhysicalIdentity `json:"registration_siblings"`
	Target               NativeIdentity       `json:"target"`
	RemoveRegistration   bool                 `json:"remove_registration"`
	RemoveFiles          string               `json:"remove_files"`
	ContentPolicy        string               `json:"content_policy"`
	Disk                 V3Manifest           `json:"disk"`
	Metadata             V3Manifest           `json:"metadata"`
	Siblings             []V3PhysicalIdentity `json:"siblings"`
	Integration          V3CommitProof        `json:"integration"`
}

func newV3Result(command string) V3Result {
	return V3Result{SchemaVersion: 3, Command: command, Status: "error", Backend: "unknown", Boundary: V3Boundary{Freshness: "sampled-nontransactional", ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}, Actions: []ActionOutcome{}, Diagnostics: []Diagnostic{}, Limitations: []string{}}
}
func (r V3Result) Error() string {
	if len(r.Diagnostics) > 0 {
		return r.Diagnostics[0].Message
	}
	return "tidy v3 operation failed"
}
func v3RecordDigest(v any) (string, error) {
	b, e := json.Marshal(v)
	if e != nil || len(b) > recordLimit {
		return "", failure("record-limit", "V3 complete evidence exceeds one MiB.")
	}
	return fmt.Sprintf("%x", sha256.Sum256(b)), nil
}

// The self field is omitted, rather than zeroed, from the digest input.
func v3PlanDigest(p V3Plan) (string, error) {
	b, e := json.Marshal(p)
	if e != nil {
		return "", e
	}
	var fields map[string]json.RawMessage
	if e = json.Unmarshal(b, &fields); e != nil {
		return "", e
	}
	delete(fields, "digest")
	return v3RecordDigest(fields)
}
func v3RecoveryLimits() []string {
	return []string{"Recovery preserves selected committed history, not index, reflog, untracked data or automatic restoration.", "Evidence is sampled and nontransactional; external and ABA races remain possible.", "Recovery roots remain retained; incomplete operations require manual inspection and never automatic replay."}
}
func v3RecoveryFor(p V3Plan, id, oid string) TidyRecovery {
	h := sha256.Sum256([]byte(id))
	return TidyRecovery{Kind: "git-recovery-ref", Name: fmt.Sprintf("refs/stw/recovery/v3/%s/%x", p.PlanID, h[:16]), CommitID: oid}
}
func v3PlanEffects(p V3Plan) []string {
	effects := []string{}
	if p.Online != nil {
		effects = append(effects, "Read bounded GitHub evidence for "+p.Online.Repository.Name+" at github.com.")
	}
	if p.StateDir == "" {
		return effects
	}
	effects = append(effects, "Persist immutable v3 plan and durable operation journal in the selected private state store.")
	for _, c := range p.Candidates {
		if c.Decision == "delete" {
			r := v3RecoveryFor(p, c.ActionID, c.OID)
			effects = append(effects, "Create and retain recovery "+r.Name+" at "+c.OID+".", "Prepare native target lock and delete exact direct reference "+c.Name+" at "+c.OID+".")
		}
	}
	if r := p.Retirement; r != nil && v3RetirementEligible(*r) {
		recovery := v3RecoveryFor(p, r.ActionID, r.Target.CommitID)
		effects = append(effects, "Create and retain recovery "+recovery.Name+" at "+r.Target.CommitID+".", "Remove exact native worktree registration for "+r.Destination.Path+".", "Remove exact verified worktree files at "+r.RemoveFiles+".")
	}
	return effects
}
func v3CanonicalPath(s string) bool {
	return s != "" && validText(s) && filepath.IsAbs(s) && filepath.Clean(s) == s
}
func v3OID(s, format string) bool {
	n := 40
	if format == "sha256" {
		n = 64
	} else if format != "sha1" {
		return false
	}
	return validAlphabet(s, n, "0123456789abcdef") && strings.Trim(s, "0") != ""
}
func v3Hash(s string) bool { return validAlphabet(s, 64, "0123456789abcdef") }
func v3Paths(paths []string) bool {
	if paths == nil {
		return false
	}
	seen := map[string]bool{}
	for _, p := range paths {
		if !v3CanonicalPath(p) || seen[p] {
			return false
		}
		seen[p] = true
	}
	return true
}
func v3Ref(s string) bool {
	if !validText(s) || !strings.HasPrefix(s, "refs/") || strings.HasSuffix(s, "/") || strings.HasSuffix(s, ".") || strings.Contains(s, "..") || strings.Contains(s, "@{") || strings.ContainsAny(s, " ~^:?*[\\") || strings.Contains(s, "//") {
		return false
	}
	for _, c := range s {
		if c < 32 || c == 127 {
			return false
		}
	}
	for _, part := range strings.Split(s, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}
func v3Mode(s string) bool {
	return s == "local-refs" || s == "tracking-refs" || s == "owned-retirement" || s == "legacy-retirement"
}
func v3Snapshot(s Snapshot, source string) bool {
	n := s.Identity
	return validSnapshot(s) && n.Backend == "git" && n.WorkspaceRoot == source && v3CanonicalPath(n.RepositoryDir) && v3CanonicalPath(n.GitDir) && n.ChangeID == "" && n.OperationID == "" && v3OID(n.CommitID, n.ObjectFormat) && v3OID(s.BaseCommit, n.ObjectFormat) && s.BaseChange == "" && s.BaseSelector != "" && validText(s.BaseSelector) && v3Hash(s.ReferenceDigest) && v3Hash(s.RegistrationDigest) && v3Hash(s.SafetyDigest)
}
func validateV3Plan(p V3Plan, state, id string) error {
	bad := func() error {
		return failure("invalid-plan", "V3 plan is incompatible, modified or does not bind exact policy, identities and effects.")
	}
	digest, e := v3PlanDigest(p)
	if e != nil {
		return e
	}
	if p.SchemaVersion != 3 || p.Policy != "tidy-git-v3" || !v3Mode(p.Mode) || p.StateDir != state || p.PlanID != id || p.Digest != digest || !v3Hash(p.Digest) || !v3CanonicalPath(p.SourcePath) || !v3Snapshot(p.Expected, p.SourcePath) || p.SelectedRefs == nil || p.Candidates == nil || !validStrings(p.Effects) || p.Effects == nil || !reflect.DeepEqual(p.RecoveryLimits, v3RecoveryLimits()) || !reflect.DeepEqual(p.Effects, v3PlanEffects(p)) || !validV3ConfigNotes(p.ConfigNotes) || p.ProofPolicy != nil && (p.Mode != "local-refs" || !validV3ProofPolicy(*p.ProofPolicy)) || p.Review != nil && (p.Mode != "local-refs" || !validV3ReviewBinding(*p.Review, p)) {
		return bad()
	}
	if state == "" {
		if id != "" || p.Retirement != nil {
			return bad()
		}
	} else if !v3CanonicalPath(state) || !validID(id) {
		return bad()
	}
	roots := []string{p.SourcePath, p.Expected.Identity.RepositoryDir, p.Expected.Identity.GitDir}
	if p.Retirement != nil {
		roots = append(roots, p.Retirement.Destination.Path, p.Retirement.Registration.Path)
		roots = append(roots, p.Retirement.RegisteredRoots...)
		roots = append(roots, p.Retirement.ProtectedRoots...)
	}
	for _, root := range roots {
		if state != "" && v3Overlaps(state, root) {
			return failure("state-overlap", "V3 state overlaps a registered, protected or native metadata root.")
		}
	}
	if p.Online != nil {
		if !validV3Online(*p.Online, p) {
			return bad()
		}
	} else if p.Mode == "tracking-refs" {
		return bad()
	}
	names, ids, selected := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, name := range p.SelectedRefs {
		if !v3Ref(name) || selected[name] {
			return bad()
		}
		selected[name] = true
	}
	for _, c := range p.Candidates {
		if names[c.Name] || ids[c.ActionID] || !validV3Candidate(c, p) {
			return bad()
		}
		names[c.Name] = true
		ids[c.ActionID] = true
		if len(selected) > 0 && !selected[c.Name] {
			return bad()
		}
	}
	if len(selected) > 0 && len(selected) != len(p.Candidates) {
		return bad()
	}
	if p.Mode == "local-refs" || p.Mode == "tracking-refs" {
		if p.Retirement != nil {
			return bad()
		}
	} else {
		if len(p.Candidates) > 0 || len(p.SelectedRefs) > 0 || p.Retirement == nil || !validV3Retirement(*p.Retirement, p) {
			return bad()
		}
	}
	if p.StateDir != "" {
		for _, c := range v3Eligible(p) {
			if !v3Ref(v3RecoveryFor(p, c.ActionID, c.OID).Name) {
				return bad()
			}
		}
	}
	return v3Bounds(p)
}
func validV3Candidate(c V3Candidate, p V3Plan) bool {
	if !validID(c.ActionID) || !v3Ref(c.Name) || !v3OID(c.OID, p.Expected.Identity.ObjectFormat) || !v3Paths(c.WorkspacePaths) || !validText(c.Reason) || c.Reason == "" || (c.Decision != "delete" && c.Decision != "retain") {
		return false
	}
	if p.Mode == "tracking-refs" {
		if p.Online == nil || c.Kind != "tracking-ref" || !strings.HasPrefix(c.Name, "refs/remotes/"+p.Online.Remote+"/") {
			return false
		}
	} else if c.Kind != "branch" || !strings.HasPrefix(c.Name, "refs/heads/") {
		return false
	}
	if (p.Mode == "local-refs" || p.Mode == "tracking-refs") && c.Decision == "delete" && (len(c.WorkspacePaths) > 0 || c.Name == p.Expected.BaseSelector || c.OID == p.Expected.BaseCommit || c.Name == p.Expected.Identity.HeadRef || c.OID == p.Expected.Identity.CommitID) {
		return false
	}
	// Only real reference candidates apply reference deletion protections.
	if c.Proof != "github-branch-absent" {
		return validV3CommitProof(V3CommitProof{OID: c.OID, Decision: c.Decision, Reason: c.Reason, Proof: c.Proof, WorkspacePaths: c.WorkspacePaths, Evidence: c.Evidence}, p)
	}
	if c.Evidence == nil || c.Evidence.Absence == nil || c.Evidence.Native != nil || c.Evidence.GitHub != nil || p.Mode != "tracking-refs" || p.Online == nil {
		return false
	}
	a := c.Evidence.Absence
	return a.Repository == p.Online.Repository && a.Remote == p.Online.Remote && a.TrackingRef == c.Name && a.SourceRef == "refs/heads/"+strings.TrimPrefix(c.Name, "refs/remotes/"+a.Remote+"/") && v3Ref(a.SourceRef) && a.Refspec == "+refs/heads/*:refs/remotes/"+a.Remote+"/*" && a.ConfigDigest == p.Online.ConfigDigest && v3Hash(a.AdvertisementDigest) && containsString(p.Online.ObservationDigests, a.AdvertisementDigest) && (c.Decision != "delete" || c.Reason == "github-branch-absent")
}
func validV3CommitProof(c V3CommitProof, p V3Plan) bool {
	if !v3OID(c.OID, p.Expected.Identity.ObjectFormat) || !v3Paths(c.WorkspacePaths) || c.Reason == "" || !validText(c.Reason) || (c.Decision != "delete" && c.Decision != "retain") {
		return false
	}
	if c.Proof == "none" {
		return c.Decision == "retain" && (c.Evidence == nil || validV3IntegrationReview(c, p))
	}
	if c.Evidence == nil || c.Evidence.Absence != nil {
		return false
	}
	if c.Evidence.Integration != nil && !integrationProofTag(c.Proof) && !reviewedProofTag(c.Proof) {
		return false
	}
	switch c.Proof {
	case integrationPatchID, integrationMergeNoop, integrationNoNetChange, reviewedIntegrated, reviewedDrop:
		return validV3IntegrationProven(c, p)
	case "native-ancestry":
		n := c.Evidence.Native
		return p.Mode != "tracking-refs" && n != nil && c.Evidence.GitHub == nil && n.TargetOID == c.OID && n.BaseOID == p.Expected.BaseCommit && (c.Decision != "delete" || c.Reason == "native-ancestry")
	case "github-merged-head":
		g := c.Evidence.GitHub
		return p.Mode != "tracking-refs" && g != nil && c.Evidence.Native == nil && p.Online != nil && validV3Merge(*g, c.OID, p) && (c.Decision != "delete" || c.Reason == "github-merged-head")
	}
	return false
}

// integrationProofTag reports the Proof values that carry the Integration arm.
func integrationProofTag(proof string) bool {
	return proof == integrationPatchID || proof == integrationMergeNoop || proof == integrationNoNetChange
}

// validV3IntegrationProven checks a deletable (or, for a missing delete, never
// stored) forge-free proof: only the Integration arm, the class equal to the
// proof tag and the plan's recorded policy.
func validV3IntegrationProven(c V3CommitProof, p V3Plan) bool {
	e := c.Evidence.Integration
	return p.Mode == "local-refs" && e != nil && c.Evidence.Native == nil && c.Evidence.GitHub == nil && c.Decision == "delete" && c.Reason == c.Proof &&
		e.Disposition == integrationProvenDisposition && e.Class == c.Proof && validV3IntegrationEvidence(*e, c.OID, p)
}

// validV3IntegrationReview checks the evidence of a retained tip with proof none:
// needs-review reasons or a hard stop, never a deletable class.
func validV3IntegrationReview(c V3CommitProof, p V3Plan) bool {
	e := c.Evidence.Integration
	if p.Mode != "local-refs" || e == nil || c.Evidence.Native != nil || c.Evidence.GitHub != nil || c.Evidence.Absence != nil || e.Disposition == integrationProvenDisposition || !validV3IntegrationEvidence(*e, c.OID, p) {
		return false
	}
	if e.Decision != nil {
		return c.Reason == reviewedKeep && e.Decision.Decision == "keep"
	}
	if e.Disposition == integrationRetainedDisposition {
		return c.Reason == e.Reasons[0]
	}
	return c.Reason == integrationReviewDisposition
}
func v3Repo(r V3Repository) bool {
	parts := strings.Split(r.Name, "/")
	if r.Host != "github.com" || r.ID <= 0 || !v3OpaqueID(r.NodeID) || len(parts) != 2 {
		return false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || !validText(part) {
			return false
		}
		for _, c := range part {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
				return false
			}
		}
	}
	return true
}
func validV3Online(o V3OnlineSelection, p V3Plan) bool {
	if !v3Repo(o.Repository) || !v3RemoteMatches(o.RemoteURL, o.Repository.Name) || !validID(o.Remote) || o.RemoteURL == "" || !validText(o.RemoteURL) || !v3Hash(o.ConfigDigest) || o.PRHints == nil || o.ObservationDigests == nil || o.BudgetPolicy != "github-read-v3" || p.Expected.Identity.ObjectFormat != "sha1" {
		return false
	}
	seen := map[string]bool{}
	for _, h := range o.PRHints {
		if !v3OID(h.OID, "sha1") || h.Number <= 0 || seen[h.OID] || p.Mode == "tracking-refs" {
			return false
		}
		seen[h.OID] = true
		found := false
		for _, c := range p.Candidates {
			if c.OID == h.OID {
				found = true
			}
		}
		if p.Retirement != nil && p.Retirement.Target.CommitID == h.OID {
			found = true
		}
		if !found {
			return false
		}
	}
	records := map[string]bool{}
	refTotal, byteTotal := 0, 0
	if len(o.Discovery) > v3DiscoveryRecordCap {
		return false
	}
	for _, d := range o.Discovery {
		refTotal += len(d.Refs)
		byteTotal += v3DiscoveryBytes(d)
		if refTotal > v3DiscoveryRefCap || byteTotal > v3DiscoveryByteCap {
			return false
		}
		if !validV3Discovery(d, p, o) || records[d.OID] {
			return false
		}
		records[d.OID] = true
	}
	seen = map[string]bool{}
	for _, h := range o.ObservationDigests {
		if !v3Hash(h) || seen[h] {
			return false
		}
		seen[h] = true
	}
	return true
}
func validV3Discovery(d V3DiscoveryRecord, p V3Plan, o V3OnlineSelection) bool {
	if !v3OID(d.OID, "sha1") || (d.Source != "discovered" && d.Source != "hint") || (d.Coverage != "complete" && d.Coverage != "incomplete") || len(d.Refs) == 0 || len(d.Refs) > 1024 {
		return false
	}
	hinted := false
	for _, h := range o.PRHints {
		hinted = hinted || h.OID == d.OID
	}
	if hinted != (d.Source == "hint") {
		return false
	}
	switch d.Result {
	case "github-merged-head":
		if d.PR == nil || d.PR.ID <= 0 || d.PR.Number <= 0 || !v3OpaqueID(d.PR.NodeID) {
			return false
		}
	case "no-merged-pr", "discovery-incomplete", "discovery-budget-exhausted", "ambiguous-needs-hint":
		if d.PR != nil || (d.Result == "no-merged-pr" && d.Coverage != "complete") {
			return false
		}
	default:
		return false
	}
	names := map[string]bool{}
	for _, c := range p.Candidates {
		if c.OID == d.OID {
			names[c.Name] = true
		}
	}
	for i, ref := range d.Refs {
		if !names[ref] || (i > 0 && d.Refs[i-1] >= ref) {
			return false
		}
	}
	return true
}
func v3MergeDigest(g V3MergeProof) (string, error) {
	g.ObservationDigest = ""
	return v3RecordDigest(g)
}
func validV3Merge(g V3MergeProof, oid string, p V3Plan) bool {
	for _, h := range p.Online.PRHints {
		if h.OID == oid && h.Number != g.Number {
			return false
		}
	}
	d, e := v3MergeDigest(g)
	_, te := time.Parse(time.RFC3339Nano, g.MergedAt)
	return e == nil && te == nil && g.ObservationDigest == d && containsString(p.Online.ObservationDigests, d) && g.Repository == p.Online.Repository && g.Head.Repository == g.Repository && g.Base.Repository == g.Repository && g.ID > 0 && g.Number > 0 && v3OpaqueID(g.NodeID) && g.State == "closed" && g.Merged && !g.Draft && g.Head.OID == oid && v3Ref("refs/heads/"+g.Head.Ref) && v3Ref("refs/heads/"+g.Base.Ref) && v3OID(g.Base.OID, "sha1") && v3OID(g.MergeOID, "sha1") && g.ChosenBaseOID == p.Expected.BaseCommit
}
func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
func v3Physical(i V3PhysicalIdentity, kind string) bool {
	return v3CanonicalPath(i.Path) && i.Inode > 0 && i.UID == uint64(os.Geteuid()) && i.OwnerKnown && i.Kind == kind && i.Nlink > 0 && i.Size >= 0 && (i.TimestampPrecision == "nanosecond" || i.TimestampPrecision == "microsecond" || i.TimestampPrecision == "second") && i.Mode <= 07777
}
func validV3Retirement(r V3Retirement, p V3Plan) bool {
	if !v3Paths(r.RegisteredRoots) || !v3Paths(r.ProtectedRoots) || !containsString(r.RegisteredRoots, p.SourcePath) || !validID(r.ActionID) || !v3Physical(r.Destination, "directory") || !v3Physical(r.Parent, "directory") || !v3Physical(r.Registration, "directory") || filepath.Dir(r.Destination.Path) != r.Parent.Path || !r.RemoveRegistration || r.RemoveFiles != r.Destination.Path || r.ContentPolicy != "git-v3" || r.Siblings == nil || !validNativeIdentity(r.Target) || r.Target.Backend != "git" || r.Target.WorkspaceRoot != r.Destination.Path || r.Target.GitDir != r.Registration.Path || filepath.Dir(r.Registration.Path) != filepath.Join(p.Expected.Identity.RepositoryDir, "worktrees") || r.Destination.UID != r.Parent.UID || r.Destination.UID != r.Registration.UID || r.Target.RepositoryDir != p.Expected.Identity.RepositoryDir || r.Target.ObjectFormat != p.Expected.Identity.ObjectFormat || !v3OID(r.Target.CommitID, r.Target.ObjectFormat) || r.Integration.OID != r.Target.CommitID {
		return false
	}

	if len(r.Actions) != 2 {
		return false
	}
	for i, a := range r.Actions {
		want := "remove-registration"
		if i == 1 {
			want = "remove-files"
		}
		if a.Name != want || a.Target != r.Destination.Path || a.Reason == "" || !validText(a.Reason) || a.Blockers == nil || !validStrings(a.Blockers) || (a.Decision != "delete" && a.Decision != "retain") {
			return false
		}
	}
	if r.Actions[0].Decision != r.Actions[1].Decision {
		return false
	}
	if v3RetirementEligible(r) {
		if r.Integration.Decision != "delete" || r.Actions[0].Reason != "integrated" || r.Actions[1].Reason != "clean" || len(r.Actions[0].Blockers) > 0 || len(r.Actions[1].Blockers) > 0 {
			return false
		}
	}
	// Retirement validates the original commit-proof paths. The target can be
	// checked out here; no reference is selected for deletion by this proof.
	if !validV3CommitProof(r.Integration, p) {
		return false
	}
	if !containsString(r.Integration.WorkspacePaths, r.Destination.Path) {
		return false
	}
	for _, path := range r.Integration.WorkspacePaths {
		if !containsString(r.RegisteredRoots, path) {
			return false
		}
	}

	if v3RetirementEligible(r) && (v3Overlaps(p.SourcePath, r.Destination.Path) || r.Destination.Path == string(filepath.Separator) || !containsString(r.RegisteredRoots, r.Destination.Path)) {
		return false
	}
	for _, root := range r.ProtectedRoots {
		if v3RetirementEligible(r) && v3Overlaps(root, r.Destination.Path) {
			return false
		}
	}
	for _, root := range r.RegisteredRoots {
		if v3RetirementEligible(r) && root != r.Destination.Path && v3Overlaps(root, r.Destination.Path) {
			return false
		}
	}
	for _, s := range r.Siblings {
		if (s.Kind != "directory" && s.Kind != "regular") || !v3Physical(s, s.Kind) || (s.Kind == "regular" && s.Nlink != 1) || v3Overlaps(s.Path, r.Destination.Path) || filepath.Dir(s.Path) != r.Parent.Path {
			return false
		}
	}
	if !validV3RegistrationGrant(r) {
		return false
	}
	if !validV3Manifest(r.Disk, r.Destination.Path, false) || !validV3Manifest(r.Metadata, r.Registration.Path, true) {
		return false
	}
	switch p.Mode {
	case "owned-retirement":
		if r.Provenance.Kind != "owned" || r.Provenance.Owned == nil || r.Provenance.Legacy != nil {
			return false
		}
		o := r.Provenance.Owned
		return o.CreationPlan.Backend == "git" && o.CreationPlan.Destination == r.Destination.Path && o.CreationPlan.StateDir == p.StateDir && validatePlan(o.CreationPlan, p.StateDir, o.CreationPlan.PlanID) == nil && validTask(o.Task, o.CreationPlan) && o.Task.Destination == r.Destination.Path && sameOwner(o.Task.Native, r.Target) && o.Task.Native.ObjectFormat == r.Target.ObjectFormat && o.Task.DestinationIdentity == (PathIdentity{Path: r.Destination.Path, Device: r.Destination.Device, Inode: r.Destination.Inode}) && r.Target.HeadRef == "" && v3Hash(o.CreationJournalDigest) && o.LockReason == "stw:"+o.Task.TaskID
	case "legacy-retirement":
		if r.Provenance.Kind != "legacy-completion" || r.Provenance.Legacy == nil || r.Provenance.Owned != nil {
			return false
		}
		l := r.Provenance.Legacy
		return validID(l.Owner) && l.Completed && l.SameUserAuthority && l.SelectedPath == r.Destination.Path && l.ObservedIdentity == r.Destination && reflect.DeepEqual(l.ProtectedRoots, r.ProtectedRoots) && r.Target.ChangeID == "" && r.Target.OperationID == ""
	}
	return false
}

func validV3RegistrationGrant(r V3Retirement) bool {
	if !v3Physical(r.RegistrationParent, "directory") || r.RegistrationParent.Path != filepath.Dir(r.Registration.Path) || r.RegistrationSiblings == nil || v3Overlaps(r.RegistrationParent.Path, r.Destination.Path) {
		return false
	}
	// Writable parents may be nested; their objects and protected children must
	// still be distinct. Bind aliases across both grant sets, not only names.
	paths := map[string]V3PhysicalIdentity{}
	objects := map[[2]uint64]string{}
	admit := func(i V3PhysicalIdentity, reuse bool) bool {
		key := [2]uint64{i.Device, i.Inode}
		if prior, exists := paths[i.Path]; exists {
			return reuse && prior == i
		}
		if _, exists := objects[key]; exists {
			return false
		}
		paths[i.Path], objects[key] = i, i.Path
		return true
	}
	for _, i := range []V3PhysicalIdentity{r.Destination, r.Parent, r.Registration, r.RegistrationParent} {
		if !admit(i, false) {
			return false
		}
	}
	seen := map[string]bool{}
	for _, s := range r.Siblings {
		if seen[s.Path] || !admit(s, s.Path == r.RegistrationParent.Path) {
			return false
		}
		seen[s.Path] = true
	}
	last := ""
	for _, s := range r.RegistrationSiblings {
		if s.Path <= last || (s.Kind != "directory" && s.Kind != "regular") || !v3Physical(s, s.Kind) || (s.Kind == "regular" && s.Nlink != 1) || filepath.Dir(s.Path) != r.RegistrationParent.Path || v3Overlaps(s.Path, r.Registration.Path) || v3Overlaps(s.Path, r.Destination.Path) || !admit(s, false) {
			return false
		}
		last = s.Path
	}
	return true
}

func validV3Manifest(m V3Manifest, root string, metadata bool) bool {
	if !m.Complete || m.Entries == nil {
		return false
	}
	d, e := v3RecordDigest(m.Entries)
	if e != nil || m.Digest != d {
		return false
	}
	last := ""
	var total int64
	for _, entry := range m.Entries {
		i := entry.Identity
		if i.Path <= last || !strings.HasPrefix(i.Path, root+string(filepath.Separator)) || !v3Physical(i, i.Kind) {
			return false
		}
		last = i.Path
		switch i.Kind {
		case "directory":
			if entry.Digest != "" || entry.Executable {
				return false
			}
		case "regular":
			limit := int64(16 << 20)
			if metadata || i.Path == filepath.Join(root, ".git") {
				limit = 64 << 10
			}
			if i.Nlink != 1 || i.Size > limit || !v3Hash(entry.Digest) || entry.Executable != (i.Mode&0111 != 0) {
				return false
			}
			// The physical .git pointer is metadata, accounted separately from
			// the 64 MiB user-content pass. Registration totals stay unchanged.
			if metadata || i.Path != filepath.Join(root, ".git") {
				total += i.Size
			}
		default:
			return false
		}
	}
	return total <= 64<<20 && len(m.Entries) <= observationLimit
}

const (
	v3DiscoveryRecordCap = 256
	v3DiscoveryRefCap    = 512
	// v3DiscoveryByteCap bounds the encoded size contributed by the repeated ref
	// names, so the informational list stays far below the 1 MiB record limit.
	v3DiscoveryByteCap = 128 << 10
)

// v3DiscoveryBytes approximates the encoded size of one record: its text and a
// fixed allowance for the member names, source, coverage, result and PR identity.
func v3DiscoveryBytes(d V3DiscoveryRecord) int {
	n := 320
	for _, r := range d.Refs {
		n += len(r) + 4
	}
	return n
}

// v3DiscoveryText validates the text of discovery records, which are counted
// against their own caps rather than the aggregate entry cap.
func v3DiscoveryText(v reflect.Value) bool {
	records, ok := v.Interface().([]V3DiscoveryRecord)
	if !ok {
		return false
	}
	for _, d := range records {
		if !validText(d.OID) || !validText(d.Source) || !validText(d.Coverage) || !validText(d.Result) || d.Refs == nil {
			return false
		}
		for _, r := range d.Refs {
			if !validText(r) {
				return false
			}
		}
		if d.PR != nil && !validText(d.PR.NodeID) {
			return false
		}
	}
	return true
}

// Validate original Go strings before encoding/json can replace invalid UTF-8.
func v3Bounds(v any) error {
	count := 0
	var walk func(reflect.Value, int) bool
	walk = func(v reflect.Value, depth int) bool {
		if depth > 64 {
			return false
		}
		if v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
			return v.IsNil() || walk(v.Elem(), depth+1)
		}
		switch v.Kind() {
		case reflect.String:
			return validText(v.String())
		case reflect.Slice:
			if v.IsNil() {
				return false
			}
			count += v.Len()
			if count > observationLimit {
				return false
			}
			for i := 0; i < v.Len(); i++ {
				if !walk(v.Index(i), depth+1) {
					return false
				}
			}
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				// An absent optional array (json omitempty) is not a null array.
				if f := v.Field(i); f.Kind() == reflect.Slice && f.IsNil() && strings.Contains(v.Type().Field(i).Tag.Get("json"), ",omitempty") {
					continue
				}
				// Discovery records have their own bounds (v3DiscoveryRecordCap,
				// v3DiscoveryRefCap) and never count toward the aggregate cap, so
				// they can never make a plan refuse (Spec 031 FR-017).
				if strings.HasPrefix(v.Type().Field(i).Tag.Get("json"), "discovery,") {
					if !v3DiscoveryText(v.Field(i)) {
						return false
					}
					continue
				}
				// Reasons and flags are closed enums of at most 17 values; like
				// the strings of a record they do not count against the 1,024
				// record plan bound, so reviewing many refs never refuses a plan.
				before := count
				if !walk(v.Field(i), depth+1) {
					return false
				}
				if (v.Type() == reflect.TypeOf(V3IntegrationEvidence{}) || v.Type() == reflect.TypeOf(V3ReviewBinding{})) && v.Field(i).Kind() == reflect.Slice {
					count = before
				}
			}
		}
		return true
	}
	if !walk(reflect.ValueOf(v), 0) {
		return failure("record-limit", "V3 record contains invalid text, null arrays or exceeds 1024 aggregate entries.")
	}
	b, e := json.Marshal(v)
	if e != nil || len(b) > recordLimit {
		return failure("record-limit", "V3 encoded record exceeds one MiB.")
	}
	return nil
}

// encoding/json accepts null scalars and omitted zero-value fields. V3's closed
// wire contract requires every typed field, and null is reserved for pointers.
func decodeV3Record(b []byte, value any) error {
	if len(b) > recordLimit || !utf8.Valid(b) {
		return failure("invalid-state", "V3 record is oversized or not UTF-8.")
	}
	if e := decodeStrict(b, value); e != nil {
		return e
	}
	var shape func(json.RawMessage, reflect.Type, int) bool
	shape = func(raw json.RawMessage, typ reflect.Type, depth int) bool {
		if depth > 64 {
			return false
		}
		raw = bytes.TrimSpace(raw)
		if typ.Kind() == reflect.Pointer {
			if bytes.Equal(raw, []byte("null")) {
				return true
			}
			return shape(raw, typ.Elem(), depth+1)
		}
		if bytes.Equal(raw, []byte("null")) {
			return false
		}
		switch typ.Kind() {
		case reflect.Struct:
			var fields map[string]json.RawMessage
			if json.Unmarshal(raw, &fields) != nil || len(fields) > typ.NumField() {
				return false
			}
			for i := 0; i < typ.NumField(); i++ {
				field := typ.Field(i)
				name := strings.Split(field.Tag.Get("json"), ",")[0]
				if name == "" {
					name = field.Name
				}
				part, exists := fields[name]
				if !exists && strings.Contains(field.Tag.Get("json"), ",omitempty") {
					continue
				}
				if !exists || !shape(part, field.Type, depth+1) {
					return false
				}
			}
		case reflect.Slice:
			var items []json.RawMessage
			if json.Unmarshal(raw, &items) != nil || items == nil {
				return false
			}
			for _, item := range items {
				if !shape(item, typ.Elem(), depth+1) {
					return false
				}
			}
		}
		return true
	}
	typ := reflect.TypeOf(value)
	if typ.Kind() != reflect.Pointer || !shape(b, typ.Elem(), 0) {
		return failure("invalid-state", "V3 fields must be present with their exact nonnull scalar/array types.")
	}
	return nil
}
func validateV3Result(r V3Result) error {
	bad := func() error { return failure("invalid-result", "Invalid or incomplete tidy v3 result authority.") }
	if r.SchemaVersion != 3 || (r.Command != "tidy-plan" && r.Command != "tidy-apply") || (r.Status != "ok" && r.Status != "refused" && r.Status != "partial" && r.Status != "error") || (r.Backend != "git" && r.Backend != "jj" && r.Backend != "gitbutler" && r.Backend != "unknown" && r.Backend != "ambiguous") || r.Actions == nil || r.Diagnostics == nil || r.Limitations == nil || r.Boundary.Freshness != "sampled-nontransactional" {
		return bad()
	}
	if _, e := time.Parse(time.RFC3339Nano, r.Boundary.ObservedAt); e != nil {
		return bad()
	}
	b := r.Boundary.Budget
	for _, n := range []int64{b.NativeProcesses, b.NativeBytes, b.HTTPRequests, b.HTTPBytes, b.Observations, b.UserBytes, b.MetadataBytes, b.FramingBytes, b.StderrBytes} {
		if n < 0 {
			return bad()
		}
	}
	if b.HTTPRequests > 128 || b.HTTPBytes > (32<<20)+1 || (!r.Boundary.NetworkAttempted && (b.HTTPRequests != 0 || b.HTTPBytes != 0)) {
		return bad()
	}
	// Counters measure consumed bytes, including the one overrun detection
	// byte. That failed observation never admits success or executable proof.
	if b.HTTPBytes > 32<<20 {
		limited := false
		for _, d := range r.Diagnostics {
			limited = limited || d.Code == "hosting-evidence-limit"
		}
		if r.Status == "ok" || r.Boundary.Complete || r.Plan != nil || b.HTTPRequests == 0 || !limited {
			return bad()
		}
	}
	if r.Plan != nil {
		if !r.Boundary.Complete || r.Backend != "git" || r.Status != "ok" || validateV3Plan(*r.Plan, r.Plan.StateDir, r.Plan.PlanID) != nil {
			return bad()
		}
	}
	if r.Status == "ok" && (!r.Boundary.Complete || r.Plan == nil) {
		return bad()
	}
	for _, a := range r.Actions {
		if a.Name == "" || (a.Status != "planned" && a.Status != "complete" && a.Status != "incomplete" && a.Status != "refused" && a.Status != "unknown") {
			return bad()
		}
	}
	for _, d := range r.Diagnostics {
		if d.Code == "" || d.Message == "" {
			return bad()
		}
	}
	return v3Bounds(r)
}
func decodeV3Result(b []byte, r *V3Result) error {
	if len(b) > recordLimit || !utf8.Valid(b) {
		return failure("record-limit", "V3 result is oversized or not UTF-8.")
	}
	var value V3Result
	if e := decodeV3Record(b, &value); e != nil {
		return e
	}
	if e := validateV3Result(value); e != nil {
		return e
	}
	*r = value
	return nil
}
func boundV3Result(r V3Result) V3Result {
	if validateV3Result(r) == nil {
		return r
	}
	safe := newV3Result("tidy-plan")
	if r.Command == "tidy-apply" {
		safe.Command = r.Command
	}
	safe.Status = "partial"
	switch r.Backend {
	case "git", "jj", "gitbutler", "ambiguous":
		safe.Backend = r.Backend
	}
	safe.Boundary.NetworkAttempted = r.Boundary.NetworkAttempted
	safe.Diagnostics = []Diagnostic{{Code: "incomplete-evidence", Message: "Complete bounded v3 evidence is unavailable; inspect the durable journal before any further action."}}
	safe.Actions = []ActionOutcome{{Name: "known-effects", Status: "unknown", Detail: "Native effects may remain; no omitted evidence authorizes mutation or replay."}}
	return safe
}

func v3RemoteMatches(raw, name string) bool {
	path := ""
	if strings.HasPrefix(raw, "git@github.com:") {
		path = strings.TrimPrefix(raw, "git@github.com:")
	} else {
		u, e := url.Parse(raw)
		if e != nil || u.Host != "github.com" || u.RawQuery != "" || u.ForceQuery || strings.Contains(raw, "#") || u.Fragment != "" || u.RawPath != "" || u.Opaque != "" {
			return false
		}
		switch u.Scheme {
		case "https":
			if u.User != nil {
				return false
			}
		case "ssh":
			if u.User == nil || u.User.Username() != "git" {
				return false
			}
			if _, password := u.User.Password(); password {
				return false
			}
		default:
			return false
		}
		path = strings.TrimPrefix(u.Path, "/")
	}
	path = strings.TrimSuffix(path, ".git")
	return strings.EqualFold(path, name)
}

func v3RetirementEligible(r V3Retirement) bool {
	return len(r.Actions) == 2 && r.Actions[0].Decision == "delete" && r.Actions[1].Decision == "delete"
}
func v3Overlaps(a, b string) bool {
	return a == string(filepath.Separator) || b == string(filepath.Separator) || overlaps(a, b)
}
func v3OpaqueID(s string) bool {
	if s == "" || len(s) > 128 || !validText(s) {
		return false
	}
	for _, c := range s {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}
