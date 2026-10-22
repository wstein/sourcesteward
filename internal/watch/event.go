// SPDX-License-Identifier: EUPL-1.2
package watch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Event kinds of schema v1. The enum is closed; review-state-changed is reserved and never emitted.
const (
	KindNewPR               = "new-pr"
	KindHeadChanged         = "head-changed"
	KindChecksGreen         = "checks-green"
	KindChecksRed           = "checks-red"
	KindChecksPendingTimout = "checks-pending-timeout"
	KindMergeableChanged    = "mergeable-changed"
	KindReadyStateChanged   = "ready-state-changed"
	KindBaseChanged         = "base-changed"
	KindMerged              = "merged"
	KindClosed              = "closed"
	KindRestackNeeded       = "restack-needed"
	KindReposChanged        = "repos-changed"
	KindWatchError          = "watch-error"
	KindWatchRecovered      = "watch-recovered"
	KindWatchStalled        = "watch-stalled"
)

// Kinds lists the closed v1 enum.
var Kinds = []string{KindNewPR, KindHeadChanged, KindChecksGreen, KindChecksRed, KindChecksPendingTimout, KindMergeableChanged,
	KindReadyStateChanged, KindBaseChanged, KindMerged, KindClosed, KindRestackNeeded, KindReposChanged, KindWatchError,
	KindWatchRecovered, KindWatchStalled}

// Restack reasons.
const (
	ReasonChildOfMerged = "child-of-merged-branch"
	ReasonContains      = "contains-merged-commits"
)

// Event is one line of the journal and of stdout (contracts/watch-events.md). Keys are never omitted.
type Event struct {
	SchemaVersion int     `json:"schema_version"`
	Seq           int64   `json:"seq"`
	ID            string  `json:"id"`
	Time          string  `json:"time"`
	Repo          *string `json:"repo"`
	PR            *int    `json:"pr"`
	Kind          string  `json:"kind"`
	Details       any     `json:"details"`
}

// Details of every kind, exactly the keys of the contract.
type (
	NewPRDetails struct {
		Title   string `json:"title"`
		Author  string `json:"author"`
		Draft   bool   `json:"draft"`
		Base    string `json:"base"`
		HeadRef string `json:"head_ref"`
		Head    string `json:"head"`
		Fork    bool   `json:"fork"` // the head repository differs from the base repository
	}
	HeadChangedDetails struct {
		Old string `json:"old"`
		New string `json:"new"`
	}
	ChecksGreenDetails struct {
		Head  string `json:"head"`
		Draft bool   `json:"draft"`
		Base  string `json:"base"`
		Total int    `json:"total"`
	}
	ChecksRedDetails struct {
		Head         string   `json:"head"`
		Failing      []string `json:"failing"`
		FailingTotal int      `json:"failing_total"`
		Shape        string   `json:"shape"`
		Base         string   `json:"base"`
		Draft        bool     `json:"draft"`
	}
	PendingTimeoutDetails struct {
		Head    string   `json:"head"`
		Pending []string `json:"pending"`
		Since   string   `json:"since"`
	}
	MergeableDetails struct {
		Mergeable   *bool  `json:"mergeable"`
		State       string `json:"state"`
		Conflicting bool   `json:"conflicting"`
		Behind      bool   `json:"behind"`
		Base        string `json:"base"`
	}
	ReadyDetails struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	BaseChangedDetails struct {
		Old string `json:"old"`
		New string `json:"new"`
	}
	MergedDetails struct {
		MergeCommit *string `json:"merge_commit"`
		Base        string  `json:"base"`
		Head        string  `json:"head"`
		HeadRef     string  `json:"head_ref"`
		MergedAt    string  `json:"merged_at"`
		MethodHint  string  `json:"method_hint"`
	}
	ClosedDetails struct {
		Head string `json:"head"`
		Base string `json:"base"`
	}
	RestackDetails struct {
		MergedPR        int      `json:"merged_pr"`
		Base            string   `json:"base"`
		Reason          string   `json:"reason"`
		OldParent       string   `json:"old_parent"`
		NewParent       *string  `json:"new_parent"`
		NewParentSource string   `json:"new_parent_source"`
		Branch          string   `json:"branch"`
		Argv            []string `json:"argv"`
	}
	Skipped struct {
		Path   string `json:"path"`
		Reason string `json:"reason"`
	}
	ReposChangedDetails struct {
		Added   []string  `json:"added"`
		Removed []string  `json:"removed"`
		Skipped []Skipped `json:"skipped"`
	}
	WatchErrorDetails struct {
		Code           string  `json:"code"`
		Message        string  `json:"message"`
		Failures       int     `json:"failures"`
		RetryInSeconds int     `json:"retry_in_seconds"`
		Repo           *string `json:"repo"`
	}
	WatchRecoveredDetails struct {
		AfterFailures int     `json:"after_failures"`
		Repo          *string `json:"repo"`
	}
	WatchStalledDetails struct {
		Since            string `json:"since"`
		ThresholdSeconds int    `json:"threshold_seconds"`
		Repo             string `json:"repo"`
	}
)

func newDetails(kind string) any {
	switch kind {
	case KindNewPR:
		return &NewPRDetails{}
	case KindHeadChanged:
		return &HeadChangedDetails{}
	case KindChecksGreen:
		return &ChecksGreenDetails{}
	case KindChecksRed:
		return &ChecksRedDetails{}
	case KindChecksPendingTimout:
		return &PendingTimeoutDetails{}
	case KindMergeableChanged:
		return &MergeableDetails{}
	case KindReadyStateChanged:
		return &ReadyDetails{}
	case KindBaseChanged:
		return &BaseChangedDetails{}
	case KindMerged:
		return &MergedDetails{}
	case KindClosed:
		return &ClosedDetails{}
	case KindRestackNeeded:
		return &RestackDetails{}
	case KindReposChanged:
		return &ReposChangedDetails{}
	case KindWatchError:
		return &WatchErrorDetails{}
	case KindWatchRecovered:
		return &WatchRecoveredDetails{}
	case KindWatchStalled:
		return &WatchStalledDetails{}
	}
	return nil
}

// UnmarshalJSON decodes strictly: the closed kind enum, schema version 1 and exact detail keys.
func (e *Event) UnmarshalJSON(raw []byte) error {
	var aux struct {
		SchemaVersion int             `json:"schema_version"`
		Seq           int64           `json:"seq"`
		ID            string          `json:"id"`
		Time          string          `json:"time"`
		Repo          *string         `json:"repo"`
		PR            *int            `json:"pr"`
		Kind          string          `json:"kind"`
		Details       json.RawMessage `json:"details"`
	}
	if err := strictDecode(raw, &aux); err != nil {
		return err
	}
	if aux.SchemaVersion != 1 {
		return fmt.Errorf("unsupported schema_version %d", aux.SchemaVersion)
	}
	d := newDetails(aux.Kind)
	if d == nil {
		return fmt.Errorf("unknown kind %q", aux.Kind)
	}
	if err := strictDecode(aux.Details, d); err != nil {
		return fmt.Errorf("%s details: %w", aux.Kind, err)
	}
	*e = Event{aux.SchemaVersion, aux.Seq, aux.ID, aux.Time, aux.Repo, aux.PR, aux.Kind, d}
	return nil
}

func strictDecode(raw []byte, into any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return err
	}
	if dec.More() {
		return fmt.Errorf("trailing data")
	}
	return nil
}

// eventID builds repo#pr:kind:key. An absent PR is "-" (repo#-); with no repo at all the prefix is just "-",
// as in the contract example "-:watch-error:rate-limited:e1:f1".
func eventID(repo string, pr *int, kind, key string) string {
	prefix := "-"
	if repo != "" {
		p := "-"
		if pr != nil {
			p = strconv.Itoa(*pr)
		}
		prefix = repo + "#" + p
	}
	return prefix + ":" + kind + ":" + key
}

func keyGeneration(g int) string       { return "g" + strconv.Itoa(g) }
func keyHead(sha string, e int) string { return sha + ":h" + strconv.Itoa(e) }
func keyChecks(head string, e int) string {
	return head + ":c" + strconv.Itoa(e)
}
func keyTimeout(head string, e int) string   { return keyChecks(head, e) + ":t" }
func keyMergeable(head string, e int) string { return head + ":m" + strconv.Itoa(e) }
func keyReady(e int) string                  { return "r" + strconv.Itoa(e) }
func keyBase(e int) string                   { return "b" + strconv.Itoa(e) }
func keyRepos(e int) string                  { return "p" + strconv.Itoa(e) }
func keyRecoveredError(e int) string         { return "e" + strconv.Itoa(e) }
func keyStall(e int) string                  { return "s" + strconv.Itoa(e) }
func keyError(code string, episode, failures int) string {
	return code + ":e" + strconv.Itoa(episode) + ":f" + strconv.Itoa(failures)
}
func keyMerged(commit *string) string {
	if commit == nil || *commit == "" {
		return "unknown"
	}
	return *commit
}
func keyRestack(mergedPR int, commit *string) string {
	return strconv.Itoa(mergedPR) + ":" + keyMerged(commit)
}

// idCounter reads the counter encoded in an event id: the colon-separated part made of the letter and
// digits only (h4, c3, e2, ...). Counter parts are short, so a hex SHA never matches.
func idCounter(id string, letter byte) (int, bool) {
	parts := strings.Split(id, ":")
	for i := len(parts) - 1; i >= 1; i-- {
		p := parts[i]
		if len(p) < 2 || len(p) > 9 || p[0] != letter {
			continue
		}
		if n, err := strconv.Atoi(p[1:]); err == nil && n >= 0 {
			return n, true
		}
	}
	return 0, false
}

// sanitize bounds a message to max bytes of valid UTF-8 without control characters.
func sanitize(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, strings.ToValidUTF8(s, "?"))
	if len(s) > max {
		s = s[:max]
		for !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	return s
}
