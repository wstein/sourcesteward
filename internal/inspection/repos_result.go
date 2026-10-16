// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"
)

// Normative limits of Spec 023.
const (
	ReposMaxEntries             = 16
	ReposMaxPathBytes           = 4096
	ReposMaxDiscoveryCandidates = 64
	ReposPerRepositorySeconds   = 10
	ReposTotalSeconds           = 60
	ReposParallelism            = 4
	ReposMaxOutputBytes         = 8388608 // 8 MiB
	ReposGitmodulesBytes        = 65536
	ReposMessageLimit           = 512
)

// Normative error codes introduced by Spec 023.
const (
	ReposCodeNotRepositoryRoot      = "not-repository-root"
	ReposCodeDuplicateRepository    = "duplicate-repository"
	ReposCodePathUnsafe             = "path-unsafe"
	ReposCodeSubmoduleUninitialized = "submodule-uninitialized"
	ReposCodeDiscoveryFailed        = "discovery-failed"
	ReposCodeDiscoveryLimit         = "discovery-limit"
	ReposCodeInventoryPartial       = "inventory-partial"
	ReposCodeMemberFailed           = "member-failed"
	ReposCodeInventoryNotStarted    = "inventory-not-started"
	ReposCodeOutputLimit            = "output-limit"
	ReposCodeChangedRepository      = "changed-repository"
	ReposCodeInventoryTimeout       = "inventory-timeout"
	ReposCodeCanceled               = "canceled"
)

// ReposOptions configures the multi-repository inventory operation.
type ReposOptions struct {
	Paths      []string
	Submodules bool
}

// ValidateReposOptions checks CLI and API usage errors before any process starts.
func ValidateReposOptions(o ReposOptions) error {
	if len(o.Paths) == 0 {
		return errors.New("at least one repository path is required")
	}
	if len(o.Paths) > ReposMaxEntries {
		return fmt.Errorf("exceeds maximum of %d repository paths", ReposMaxEntries)
	}
	for _, p := range o.Paths {
		if len(p) == 0 {
			return errors.New("empty repository path")
		}
		if len(p) > ReposMaxPathBytes {
			return fmt.Errorf("repository path exceeds maximum byte limit of %d", ReposMaxPathBytes)
		}
		if !utf8.ValidString(p) {
			return errors.New("invalid UTF-8 in repository path")
		}
		if strings.ContainsRune(p, 0) {
			return errors.New("null byte in repository path")
		}
	}
	return nil
}

// ReposResult is the contract root of `stw repos`.
type ReposResult struct {
	SchemaVersion       int           `json:"schema_version"`
	Command             string        `json:"command"`
	Status              string        `json:"status"` // "ok", "partial", "error"
	InputPaths          []string      `json:"input_paths"`
	SubmodulesRequested bool          `json:"submodules_requested"`
	Boundary            ReposBoundary `json:"boundary"`
	Entries             []ReposEntry  `json:"entries"`
	Summary             ReposSummary  `json:"summary"`
	Diagnostics         []Diagnostic  `json:"diagnostics"`
}

// ReposBoundary describes execution constraints, bounds and limitations.
type ReposBoundary struct {
	Complete    bool         `json:"complete"`
	Freshness   string       `json:"freshness"` // "sampled-nontransactional"
	Effects     ReposEffects `json:"effects"`
	Limits      ReposLimits  `json:"limits"`
	Limitations []string     `json:"limitations"`
}

// ReposEffects discloses side effects of the multi-repository inspection.
type ReposEffects struct {
	Network                    bool `json:"network"`
	Fetch                      bool `json:"fetch"`
	RepositoryWrites           bool `json:"repository_writes"`
	Hooks                      bool `json:"hooks"`
	PrivateTemporaryFilesForJJ bool `json:"private_temporary_files_for_jj"`
}

// ReposDefaultEffects returns the normative effects object for repos.
func ReposDefaultEffects() ReposEffects {
	return ReposEffects{
		Network:                    false,
		Fetch:                      false,
		RepositoryWrites:           false,
		Hooks:                      false,
		PrivateTemporaryFilesForJJ: true,
	}
}

// ReposLimits holds the normative limits table.
type ReposLimits struct {
	MaxEntries             int `json:"max_entries"`
	MaxPathBytes           int `json:"max_path_bytes"`
	MaxDiscoveryCandidates int `json:"max_discovery_candidates"`
	PerRepositorySeconds   int `json:"per_repository_seconds"`
	TotalSeconds           int `json:"total_seconds"`
	Parallelism            int `json:"parallelism"`
	MaxOutputBytes         int `json:"max_output_bytes"`
	GitmodulesBytes        int `json:"gitmodules_bytes"`
}

// ReposDefaultLimits returns the normative limits object for repos.
func ReposDefaultLimits() ReposLimits {
	return ReposLimits{
		MaxEntries:             ReposMaxEntries,
		MaxPathBytes:           ReposMaxPathBytes,
		MaxDiscoveryCandidates: ReposMaxDiscoveryCandidates,
		PerRepositorySeconds:   ReposPerRepositorySeconds,
		TotalSeconds:           ReposTotalSeconds,
		Parallelism:            ReposParallelism,
		MaxOutputBytes:         ReposMaxOutputBytes,
		GitmodulesBytes:        ReposGitmodulesBytes,
	}
}

// ReposEntry represents one explicit or discovered repository member.
type ReposEntry struct {
	Index        int              `json:"index"`
	Path         string           `json:"path"`
	ResolvedPath *string          `json:"resolved_path"`
	Origin       string           `json:"origin"` // "explicit", "submodule"
	Parent       *int             `json:"parent"`
	Status       string           `json:"status"` // "ok", "partial", "error", "skipped"
	Error        *ReposError      `json:"error"`
	Backend      string           `json:"backend"`
	Discovery    ReposDiscovery   `json:"discovery"`
	DuplicateOf  *int             `json:"duplicate_of"`
	ContainedIn  *int             `json:"contained_in"`
	Facts        *ReposFacts      `json:"facts"`
	Inventory    *InventoryResult `json:"inventory"`
	Notes        []string         `json:"notes"`
}

// ReposError contains error details for an entry or diagnostic.
type ReposError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ReposDiscovery records submodule discovery status and candidate counts for an entry.
type ReposDiscovery struct {
	Requested    bool   `json:"requested"`
	Status       string `json:"status"` // "not-requested", "ok", "not-applicable", "failed", "limit"
	Submodules   int    `json:"submodules"`
	Ignored      int    `json:"ignored"`
	Deduplicated int    `json:"deduplicated"`
	Reason       string `json:"reason"`
}

// ReposFacts aggregates high-level repository facts extracted from an inventory.
type ReposFacts struct {
	HeadState            string               `json:"head_state"`
	HeadRef              *string              `json:"head_ref"`
	HeadCommitID         *string              `json:"head_commit_id"`
	ChangeID             *string              `json:"change_id"`
	Dirty                ReposDirty           `json:"dirty"`
	ConflictedReferences int                  `json:"conflicted_references"`
	Workspaces           ReposWorkspacesFacts `json:"workspaces"`
}

// ReposDirty records dirty state determination.
type ReposDirty struct {
	Value  string `json:"value"` // "true", "false", "unknown"
	Reason string `json:"reason"`
}

// ReposWorkspacesFacts summarizes workspace records.
type ReposWorkspacesFacts struct {
	Total      int `json:"total"`
	Stale      int `json:"stale"`
	Foreign    int `json:"foreign"`
	Unverified int `json:"unverified"`
}

// ReposSummary reports total counts across all entries.
type ReposSummary struct {
	Entries                int               `json:"entries"`
	ByStatus               ReposByStatus     `json:"by_status"`
	ByBackend              ReposByBackend    `json:"by_backend"`
	Dirty                  ReposDirtySummary `json:"dirty"`
	StaleWorkspaces        int               `json:"stale_workspaces"`
	ForeignWorkspaces      int               `json:"foreign_workspaces"`
	UnverifiedWorkspaces   int               `json:"unverified_workspaces"`
	ConflictedReferences   int               `json:"conflicted_references"`
	SkippedUninitialized   int               `json:"skipped_uninitialized"`
	DiscoveryNotApplicable int               `json:"discovery_not_applicable"`
	InventoriesOmitted     int               `json:"inventories_omitted"`
}

// ReposByStatus counts entries by status.
type ReposByStatus struct {
	Ok      int `json:"ok"`
	Partial int `json:"partial"`
	Error   int `json:"error"`
	Skipped int `json:"skipped"`
}

// ReposByBackend counts entries by backend.
type ReposByBackend struct {
	Git   int `json:"git"`
	JJ    int `json:"jj"`
	Other int `json:"other"`
}

// ReposDirtySummary counts dirty states.
type ReposDirtySummary struct {
	True    int `json:"true"`
	False   int `json:"false"`
	Unknown int `json:"unknown"`
}

// ReposOverallStatus evaluates overall result status according to FR-012.
// "ok when every entry is ok or skipped; error when no entry has a usable inventory: ok or partial with a non-null embedded inventory; otherwise partial".
func ReposOverallStatus(entries []ReposEntry) string {
	if len(entries) == 0 {
		return "error"
	}
	usableCount := 0
	allOkOrSkipped := true
	for _, e := range entries {
		if e.Inventory != nil && (e.Inventory.Status == "ok" || e.Inventory.Status == "partial") {
			usableCount++
		}
		if e.Status != "ok" && e.Status != "skipped" {
			allOkOrSkipped = false
		}
	}
	if usableCount == 0 {
		return "error"
	}
	if allOkOrSkipped {
		return "ok"
	}
	return "partial"
}

// Cause precedence rank per FR-012:
// 1. path-unsafe
// 2. not-repository-root
// 3. duplicate-repository
// 4. inherited identity or inventory error
// 5. inventory-timeout / canceled / inventory-not-started
// 6. member-failed
// 7. output-limit
// 8. discovery-failed
// 9. discovery-limit
// 10. inventory-partial
func reposPrecedenceRank(code string) int {
	switch code {
	case ReposCodePathUnsafe:
		return 1
	case ReposCodeNotRepositoryRoot:
		return 2
	case ReposCodeDuplicateRepository:
		return 3
	case "inventory-timeout", "canceled", ReposCodeInventoryNotStarted:
		return 5
	case ReposCodeMemberFailed:
		return 6
	case ReposCodeOutputLimit:
		return 7
	case ReposCodeDiscoveryFailed:
		return 8
	case ReposCodeDiscoveryLimit:
		return 9
	case ReposCodeInventoryPartial:
		return 10
	default:
		// Inherited identity or inventory error
		return 4
	}
}

// ReposResolveCauses applies the precedence rule to a list of causes.
// First in precedence wins as primary; the others are returned as notes formatted "<code>: <message>".
func ReposResolveCauses(causes []ReposError) (ReposError, []string) {
	if len(causes) == 0 {
		return ReposError{}, nil
	}
	sorted := make([]ReposError, len(causes))
	copy(sorted, causes)
	sort.SliceStable(sorted, func(i, j int) bool {
		return reposPrecedenceRank(sorted[i].Code) < reposPrecedenceRank(sorted[j].Code)
	})

	primary := sorted[0]
	var notes []string
	if len(sorted) > 1 {
		notes = make([]string, len(sorted)-1)
		for i, c := range sorted[1:] {
			notes[i] = fmt.Sprintf("%s: %s", c.Code, c.Message)
		}
	}
	return primary, notes
}

// ReposCheckJSONBudget checks whether a ReposResult exceeds ReposMaxOutputBytes before allocation.
func ReposCheckJSONBudget(r ReposResult) error {
	budget := ReposMaxOutputBytes
	return reposJSONBudget(reflect.ValueOf(r), &budget)
}

// reposEncodingError reports a value that cannot be encoded as JSON (invalid UTF-8, an unsupported type);
// it is distinct from the size bound, which is the only reason to omit an inventory.
type reposEncodingError struct{ msg string }

func (e *reposEncodingError) Error() string { return e.msg }

func isReposEncodingError(err error) bool {
	var enc *reposEncodingError
	return errors.As(err, &enc)
}

// reposClean returns s as valid UTF-8 of at most ReposMessageLimit bytes, cut at a rune boundary.
func reposClean(s string) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "\uFFFD")
	}
	if len(s) <= ReposMessageLimit {
		return s
	}
	cut := ReposMessageLimit - len("...")
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}

func reposJSONBudget(v reflect.Value, remaining *int) error {
	spend := func(n int) error {
		if n > *remaining {
			return errors.New("repos result byte limit exceeded")
		}
		*remaining -= n
		return nil
	}
	if !v.IsValid() {
		return spend(4)
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return spend(4)
		}
		return reposJSONBudget(v.Elem(), remaining)
	case reflect.String:
		s := v.String()
		if len(s) > *remaining {
			return errors.New("repos result byte limit exceeded")
		}
		if !utf8.ValidString(s) {
			return &reposEncodingError{"repos metadata is not valid UTF-8"}
		}
		if e := spend(2); e != nil {
			return e
		}
		for _, r := range s {
			n := utf8.RuneLen(r)
			switch r {
			case '\b', '\f', '\n', '\r', '\t', '"', '\\':
				n = 2
			case '<', '>', '&', '\u2028', '\u2029':
				n = 6
			default:
				if r < 0x20 {
					n = 6
				}
			}
			if e := spend(n); e != nil {
				return e
			}
		}
		return nil
	case reflect.Bool:
		return spend(5)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return spend(20)
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			return spend(4)
		}
		if e := spend(2); e != nil {
			return e
		}
		for i := 0; i < v.Len(); i++ {
			if e := spend(1); e != nil {
				return e
			}
			if e := reposJSONBudget(v.Index(i), remaining); e != nil {
				return e
			}
		}
		return nil
	case reflect.Struct:
		if e := spend(2); e != nil {
			return e
		}
		typ := v.Type()
		for i := 0; i < v.NumField(); i++ {
			tag := typ.Field(i).Tag.Get("json")
			name, _, _ := strings.Cut(tag, ",")
			if name == "-" {
				continue
			}
			if name == "" {
				name = typ.Field(i).Name
			}
			if e := spend(len(name) + 4); e != nil {
				return e
			}
			if e := reposJSONBudget(v.Field(i), remaining); e != nil {
				return e
			}
		}
		return nil
	default:
		return &reposEncodingError{"unsupported repos result value"}
	}
}
