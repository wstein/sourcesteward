// SPDX-License-Identifier: EUPL-1.2
package workspace

import "context"

// RegistrationPruneV4HistoryOptions selects only the separate contract-4 store.
// A zero Limit selects the default; explicit CLI zero is rejected before this API.
type RegistrationPruneV4HistoryOptions struct {
	Path, StateDir string
	Limit          int
}

// RegistrationPruneV4HistoryEffect reports recorded effects, not new observations.
type RegistrationPruneV4HistoryEffect struct {
	Action  string `json:"action"`
	Target  string `json:"target"`
	Intent  string `json:"intent"`
	Outcome string `json:"outcome"`
}

// RegistrationPruneV4HistoryEntry exposes pointers without reading private payload bytes.
type RegistrationPruneV4HistoryEntry struct {
	PlanID         string                             `json:"plan_id"`
	Time           string                             `json:"time"`
	Phase          string                             `json:"phase"`
	Registration   PruneV4Registration                `json:"registration"`
	PayloadPath    string                             `json:"payload_path"`
	PayloadDigest  *string                            `json:"payload_digest"`
	HistoryRef     string                             `json:"history_ref"`
	HistoryOID     string                             `json:"history_oid"`
	RecoveryStatus string                             `json:"recovery_status"`
	Effects        []RegistrationPruneV4HistoryEffect `json:"effects"`
	Remaining      []string                           `json:"remaining"`
}

type RegistrationPruneV4HistoryResult struct {
	SchemaVersion int                               `json:"schema_version"`
	Command       string                            `json:"command"`
	Operation     string                            `json:"operation"`
	Status        string                            `json:"status"`
	Entries       []RegistrationPruneV4HistoryEntry `json:"entries"`
	Total         int                               `json:"total"`
	Truncated     bool                              `json:"truncated"`
	Truncation    []V3HistoryTruncation             `json:"truncation"`
	Diagnostics   []Diagnostic                      `json:"diagnostics"`
	Limitations   []string                          `json:"limitations"`
}

func newPruneV4HistoryResult() RegistrationPruneV4HistoryResult {
	return RegistrationPruneV4HistoryResult{SchemaVersion: 4, Command: "tidy-history", Operation: "registration-prune", Status: "refused", Entries: []RegistrationPruneV4HistoryEntry{}, Truncation: []V3HistoryTruncation{}, Diagnostics: []Diagnostic{}, Limitations: []string{
		"Recovery pointers and recorded effects only; private payload bytes and native history pins are not verified by listing. Use restore inspection to verify recovery availability.",
		"Prune journals only: restore journals are not listed, so this listing cannot identify a completed or partial restore. Preserve the restore ID and journal pointer from the restore result for separate inspection.",
		"History and restore --list select the repository from optional PATH, or the current directory when PATH is omitted; --state-dir alone does not select a repository.",
		"Newest first among the bounded scanned journals; total counts only records matching the selected repository and is a lower bound when the directory or byte scan is truncated.",
	}}
}

func failPruneV4History(r RegistrationPruneV4HistoryResult, err error) RegistrationPruneV4HistoryResult {
	r.Status = "refused"
	code := tidyErrorCode(err)
	if code == "" {
		code = "prune-history-refused"
	}
	message := pruneV4Message(err)
	if err == context.Canceled {
		message = "Registration prune history canceled."
	}
	r.Entries = []RegistrationPruneV4HistoryEntry{}
	r.Diagnostics = []Diagnostic{{Code: code, Message: message}}
	return r
}
