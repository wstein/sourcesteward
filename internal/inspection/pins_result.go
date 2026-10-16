// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"errors"
	"sort"
	"strings"
	"unicode/utf8"
)

// pinsMessageLimit bounds every message so repository text cannot grow a result without limit.
const pinsMessageLimit = 512

// pinsStaleness is fixed: publication evidence is a local record, never a fresh observation.
const pinsStaleness = "Remote-tracking refs are local records; no fetch was made."

// The normative finding table of Spec 021. Codes are append-only within schema_version 1 and severities are fixed.
var pinsFindingTable = []struct{ code, severity string }{
	{"pin-unpublished", "error"},
	{"gitlink-without-entry", "error"},
	{"url-has-credentials", "error"},
	{"submodule-path-unsafe", "error"},
	{"submodule-unsafe-owner", "error"},
	{"submodule-unsupported-backend", "warning"},
	{"pins-limit", "error"},
	{"pins-state-changed", "error"},
	{"staged-pin-differs", "warning"},
	{"checkout-differs-from-pin", "warning"},
	{"submodule-dirty", "warning"},
	{"url-mismatch", "warning"},
	{"entry-without-gitlink", "warning"},
	{"gitmodules-modified", "warning"},
	{"pin-object-absent", "error"},
	{"submodule-uninitialized", "info"},
	{"pin-behind-tracking", "info"},
	{"nested-submodules-present", "info"},
	{"remote-refs-may-be-stale", "info"},
}

// PinsFindingCodes returns a copy of the stable finding codes in specification order.
func PinsFindingCodes() []string {
	codes := make([]string, len(pinsFindingTable))
	for i, f := range pinsFindingTable {
		codes[i] = f.code
	}
	return codes
}

// PinsFindingSeverity returns the fixed severity of a finding code.
func PinsFindingSeverity(code string) (string, bool) {
	for _, f := range pinsFindingTable {
		if f.code == code {
			return f.severity, true
		}
	}
	return "", false
}

// PinsOptions selects the superproject path; an empty path means the current directory.
type PinsOptions struct{ Path string }

// ValidatePinsOptions validates usage without accessing repository state.
func ValidatePinsOptions(o PinsOptions) error {
	if len(o.Path) > outputLimit {
		return errors.New("pins path exceeds its byte limit")
	}
	if !utf8.ValidString(o.Path) || strings.ContainsRune(o.Path, 0) {
		return errors.New("invalid pins path encoding")
	}
	return nil
}

type PinsEffects struct {
	Network bool `json:"network"`
	Fetch   bool `json:"fetch"`
	Writes  bool `json:"writes"`
	Hooks   bool `json:"hooks"`
}
type PinsLimits struct {
	MaxSubmodules    int `json:"max_submodules"`
	PerSubmoduleSecs int `json:"per_submodule_seconds"`
	TotalSeconds     int `json:"total_seconds"`
	MaxOutputBytes   int `json:"max_output_bytes"`
	MaxRemoteRefs    int `json:"max_remote_refs"`
	MaxCount         int `json:"max_count"`
	DirtyScanSeconds int `json:"dirty_scan_seconds"`
	DirtyScanEntries int `json:"dirty_scan_entries"`
}
type PinsBoundary struct {
	Effects   PinsEffects `json:"effects"`
	Limits    PinsLimits  `json:"limits"`
	Complete  bool        `json:"complete"`
	Staleness string      `json:"staleness"`
}
type PinsRegistration struct {
	GitlinkInHead   bool  `json:"gitlink_in_head"`
	GitlinkInIndex  bool  `json:"gitlink_in_index"`
	GitmodulesEntry bool  `json:"gitmodules_entry"`
	Active          *bool `json:"active"`
}
type PinsURL struct {
	Configured     *string `json:"configured"`
	HasCredentials bool    `json:"has_credentials"`
	RemoteName     *string `json:"remote_name"`
	RemoteURL      *string `json:"remote_url"`
	Match          string  `json:"match"`
}
type PinsPin struct {
	HeadCommit    *string `json:"head_commit"`
	IndexCommit   *string `json:"index_commit"`
	StagedDiffers bool    `json:"staged_differs"`
}
type PinsDirty struct {
	Tracked   string `json:"tracked"`
	Untracked string `json:"untracked"`
	Reason    string `json:"reason"`
}
type PinsCheckout struct {
	State         string    `json:"state"`
	HeadCommit    *string   `json:"head_commit"`
	Branch        *string   `json:"branch"`
	Detached      bool      `json:"detached"`
	RelationToPin string    `json:"relation_to_pin"`
	Ahead         *int      `json:"ahead"`
	Behind        *int      `json:"behind"`
	Capped        bool      `json:"capped"`
	Dirty         PinsDirty `json:"dirty"`
}
type PinsRemoteRef struct {
	Remote string `json:"remote"`
	Ref    string `json:"ref"`
}
type PinsPublication struct {
	Status        string          `json:"status"`
	Reason        string          `json:"reason"`
	ReachableFrom []PinsRemoteRef `json:"reachable_from"`
	Truncated     bool            `json:"truncated"`
	LastFetch     *string         `json:"last_fetch"`
}
type PinsTracking struct {
	Ref      *string `json:"ref"`
	Relation string  `json:"relation"`
	Ahead    *int    `json:"ahead"`
	Behind   *int    `json:"behind"`
	Capped   bool    `json:"capped"`
}
type PinsNested struct {
	Present bool `json:"present"`
}
type PinsRefusal struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type PinsSubmodule struct {
	Path         string           `json:"path"`
	PathEncoding string           `json:"path_encoding"`
	Name         *string          `json:"name"`
	Registration PinsRegistration `json:"registration"`
	URL          PinsURL          `json:"url"`
	Pin          PinsPin          `json:"pin"`
	Checkout     PinsCheckout     `json:"checkout"`
	Publication  PinsPublication  `json:"publication"`
	Tracking     PinsTracking     `json:"tracking"`
	Nested       PinsNested       `json:"nested"`
	Refusal      *PinsRefusal     `json:"refusal"`
	FindingCodes []string         `json:"finding_codes"`
}
type PinsFinding struct {
	Code      string  `json:"code"`
	Severity  string  `json:"severity"`
	Submodule *string `json:"submodule"`
	Message   string  `json:"message"`
}
type PinsPublicationSummary struct {
	Reachable   int `json:"reachable"`
	Unreachable int `json:"unreachable"`
	Unknown     int `json:"unknown"`
}
type PinsSummary struct {
	Submodules  int                    `json:"submodules"`
	Errors      int                    `json:"errors"`
	Warnings    int                    `json:"warnings"`
	Infos       int                    `json:"infos"`
	Publication PinsPublicationSummary `json:"publication"`
}
type PinsError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// PinsResult is the single result rendered as text or JSON (schemas/v1/pins.schema.json).
type PinsResult struct {
	SchemaVersion int             `json:"schema_version"`
	Command       string          `json:"command"`
	Status        string          `json:"status"`
	Backend       string          `json:"backend"`
	InputPath     string          `json:"input_path"`
	Identity      *Result         `json:"identity"`
	Boundary      PinsBoundary    `json:"boundary"`
	Submodules    []PinsSubmodule `json:"submodules"`
	Findings      []PinsFinding   `json:"findings"`
	Summary       PinsSummary     `json:"summary"`
	Error         *PinsError      `json:"error"`
}

// NewPinsResult returns an empty result with the fixed effects, limits and staleness note.
func NewPinsResult(inputPath string) PinsResult {
	return PinsResult{
		SchemaVersion: 1, Command: "pins", Status: "ok", Backend: "unknown", InputPath: inputPath,
		Boundary: PinsBoundary{
			Effects:   PinsEffects{},
			Limits:    PinsLimits{MaxSubmodules: 256, PerSubmoduleSecs: 5, TotalSeconds: 30, MaxOutputBytes: 1 << 20, MaxRemoteRefs: 16, MaxCount: 10000, DirtyScanSeconds: 3, DirtyScanEntries: 20000},
			Staleness: pinsStaleness,
		},
		Submodules: []PinsSubmodule{}, Findings: []PinsFinding{},
	}
}

// AddFinding appends a finding whose severity comes from the normative table.
func (r *PinsResult) AddFinding(code string, submodule *string, message string) error {
	severity, ok := PinsFindingSeverity(code)
	if !ok {
		return errors.New("unknown pins finding code")
	}
	r.Findings = append(r.Findings, PinsFinding{Code: code, Severity: severity, Submodule: submodule, Message: boundPinsMessage(message)})
	return nil
}
func boundPinsMessage(s string) string {
	s = strings.ToValidUTF8(strings.ReplaceAll(s, "\x00", ""), "�")
	if len(s) <= pinsMessageLimit {
		return s
	}
	s = s[:pinsMessageLimit]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
func pinsSeverityRank(s string) int {
	switch s {
	case "error":
		return 0
	case "warning":
		return 1
	}
	return 2
}

// Finalize makes the result deterministic and self-consistent: arrays are never null, submodules are
// sorted by path bytes, findings by severity, submodule path and code, and the summary, per-submodule
// finding codes, completeness and status follow from the findings. It is idempotent.
func (r *PinsResult) Finalize() {
	if r.Submodules == nil {
		r.Submodules = []PinsSubmodule{}
	}
	if r.Findings == nil {
		r.Findings = []PinsFinding{}
	}
	sort.SliceStable(r.Submodules, func(i, j int) bool { return r.Submodules[i].Path < r.Submodules[j].Path })
	sort.SliceStable(r.Findings, func(i, j int) bool {
		a, b := r.Findings[i], r.Findings[j]
		if x, y := pinsSeverityRank(a.Severity), pinsSeverityRank(b.Severity); x != y {
			return x < y
		}
		if (a.Submodule == nil) != (b.Submodule == nil) {
			return a.Submodule == nil
		}
		if a.Submodule != nil && *a.Submodule != *b.Submodule {
			return *a.Submodule < *b.Submodule
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Message < b.Message
	})
	limited := false
	var summary PinsSummary
	summary.Submodules = len(r.Submodules)
	for _, f := range r.Findings {
		switch f.Severity {
		case "error":
			summary.Errors++
		case "warning":
			summary.Warnings++
		default:
			summary.Infos++
		}
		limited = limited || f.Code == "pins-limit" || f.Code == "pins-state-changed"
	}
	for i := range r.Submodules {
		s := &r.Submodules[i]
		codes := []string{}
		for _, f := range r.Findings {
			if f.Submodule != nil && *f.Submodule == s.Path && (len(codes) == 0 || codes[len(codes)-1] != f.Code) {
				codes = append(codes, f.Code)
			}
		}
		sort.Strings(codes)
		s.FindingCodes = codes
		if s.Publication.ReachableFrom == nil {
			s.Publication.ReachableFrom = []PinsRemoteRef{}
		}
		switch s.Publication.Status {
		case "reachable":
			summary.Publication.Reachable++
		case "unreachable":
			summary.Publication.Unreachable++
		default:
			summary.Publication.Unknown++
		}
	}
	r.Summary = summary
	switch r.Status {
	case "refused", "error":
		r.Boundary.Complete = false
	default:
		r.Boundary.Complete = !limited
		r.Status = "ok"
		if limited {
			r.Status = "incomplete"
		}
	}
}
