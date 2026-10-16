// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
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

// boundPinsMessage keeps messages plain text: NUL is dropped and invalid UTF-8 bytes are shown as \xNN,
// the same escaped form used for non-UTF-8 paths, never as a silent U+FFFD replacement.
func boundPinsMessage(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	if !utf8.ValidString(s) {
		var b strings.Builder
		for i := 0; i < len(s); {
			r, size := utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError && size == 1 {
				fmt.Fprintf(&b, `\x%02x`, s[i])
			} else {
				b.WriteString(s[i : i+size])
			}
			i += size
		}
		s = b.String()
	}
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
// finding codes, completeness and status follow from the findings. It is idempotent and returns an
// error when any string is not valid UTF-8.
func (r *PinsResult) Finalize() error {
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
	return validPinsText(reflect.ValueOf(*r))
}

// failWhole turns the result into an operational error: empty lists, no identity (the schema allows
// one only for ok and incomplete results) and a bounded message.
func (r *PinsResult) failWhole(message string) {
	r.Status, r.Identity = "error", nil
	r.Submodules, r.Findings = []PinsSubmodule{}, []PinsFinding{}
	r.Error = &PinsError{Code: "pins-failed", Message: boundPinsMessage(message)}
	_ = r.Finalize()
}

// validPinsText fails on any string that is not valid UTF-8. encoding/json would silently replace
// such bytes with U+FFFD; a repository-derived path must instead be refused or shown escaped.
func validPinsText(v reflect.Value) error {
	switch v.Kind() {
	case reflect.String:
		if !utf8.ValidString(v.String()) {
			return errors.New("pins result holds a string that is not valid UTF-8")
		}
	case reflect.Pointer, reflect.Interface:
		// Identity is walked too. Later work items fill it from repository-derived names: they must escape
		// or refuse a non-UTF-8 identity string per field so one bad ref never fails the whole result.
		if !v.IsNil() {
			return validPinsText(v.Elem())
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if err := validPinsText(v.Field(i)); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if err := validPinsText(v.Index(i)); err != nil {
				return err
			}
		}
	}
	return nil
}

// MarshalJSON refuses invalid UTF-8 so it can never become a replacement character silently.
func (r PinsResult) MarshalJSON() ([]byte, error) {
	if err := validPinsText(reflect.ValueOf(r)); err != nil {
		return nil, err
	}
	type plain PinsResult
	return json.Marshal(plain(r))
}

// boundOutput enforces max_output_bytes on the marshalled result (FR-011), not only on retained Git output:
// the largest prefix of submodules (and their findings) that fits is kept, and a global pins-limit
// finding says how many were omitted, which also makes the result incomplete.
func (r *PinsResult) boundOutput() {
	max := r.Boundary.Limits.MaxOutputBytes - 16 // room for the encoder's newline
	size := func() int {
		if err := r.Finalize(); err != nil {
			return 0
		}
		b, err := json.Marshal(*r)
		if err != nil {
			return 0
		}
		return len(b)
	}
	if size() <= max {
		return
	}
	all, findings := r.Submodules, r.Findings
	fit := func(k int) bool {
		kept := map[string]bool{}
		for _, s := range all[:k] {
			kept[s.Path] = true
		}
		r.Submodules, r.Findings = append([]PinsSubmodule(nil), all[:k]...), nil
		for _, f := range findings {
			if f.Submodule == nil || kept[*f.Submodule] {
				r.Findings = append(r.Findings, f)
			}
		}
		_ = r.AddFinding("pins-limit", nil, fmt.Sprintf("the result exceeds the %d byte output bound; %d of %d submodules are listed", r.Boundary.Limits.MaxOutputBytes, k, len(all)))
		return size() <= max
	}
	lo, hi := 0, len(all)-1 // the full list does not fit
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if fit(mid) {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	fit(lo)
}
