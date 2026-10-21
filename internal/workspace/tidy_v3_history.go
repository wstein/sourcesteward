// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"io"
	"sort"
	"time"
)

// History of the stored tidy v3 journals (Spec 031 FR-047, contracts/restore.md
// "History result"). It shares the journal reader with restore and is
// read-only. Every bound is a visible truncation marker or a journal status,
// never a silent omission.

const (
	historyDefaultLimit = 256
	historyMaxLimit     = 1024
	historyScanLimit    = 4096
	historyObjectLines  = 65536
	historyObjectBytes  = 4 << 20
	historyBudget       = 30 * time.Second
)

// V3HistoryDecisions counts the reviewer decisions per kind.
type V3HistoryDecisions struct {
	Human int `json:"human"`
	AI    int `json:"ai"`
}

// V3HistoryEntry is one stored journal.
type V3HistoryEntry struct {
	PlanID        string             `json:"plan_id"`
	Time          string             `json:"time"`
	Mode          string             `json:"mode"`
	Deleted       int                `json:"deleted"`
	Retained      int                `json:"retained"`
	Restorable    int                `json:"restorable"`
	RecoveryRefs  string             `json:"recovery_refs"`
	DecisionKinds V3HistoryDecisions `json:"decision_kinds"`
	JournalStatus string             `json:"journal_status"`
}

// V3HistoryTruncation is the marker of a bound that was reached: Shown of Total.
type V3HistoryTruncation struct {
	Unit  string `json:"unit"`
	Shown int    `json:"shown"`
	Total int    `json:"total"`
}

// V3HistoryResult is the candidate (unpublished) history result.
type V3HistoryResult struct {
	SchemaVersion int                   `json:"schema_version"`
	Command       string                `json:"command"`
	Status        string                `json:"status"`
	Entries       []V3HistoryEntry      `json:"entries"`
	Total         int                   `json:"total"`
	ScanTruncated bool                  `json:"scan_truncated"`
	Truncated     []V3HistoryTruncation `json:"truncated"`
	Diagnostics   []Diagnostic          `json:"diagnostics"`
}

// V3HistoryOptions selects the history view; List names it through restore --list.
type V3HistoryOptions struct {
	Path, StateDir string
	Limit          int
	List           bool
}

func newV3HistoryResult(list bool) V3HistoryResult {
	command := "tidy-history"
	if list {
		command = "tidy-restore"
	}
	return V3HistoryResult{SchemaVersion: 1, Command: command, Status: "error", Entries: []V3HistoryEntry{}, Truncated: []V3HistoryTruncation{}, Diagnostics: []Diagnostic{}}
}

// tidyV3HistoryCoordinator carries the bounds of one history call; tests lower
// them and replace the clock.
type tidyV3HistoryCoordinator struct {
	identity    func(context.Context, string) (NativeIdentity, error)
	observeArgs func([]string)
	scanLimit   int
	objectLines int
	// expired reports that the 30-second budget of the call is spent.
	expired func() bool
}

func defaultTidyV3HistoryCoordinator() tidyV3HistoryCoordinator {
	return tidyV3HistoryCoordinator{identity: gitNativeIdentity, scanLimit: historyScanLimit, objectLines: historyObjectLines, expired: budgetClock(historyBudget)}
}

// ValidateV3HistoryOptions reports the argument errors of a history command.
func ValidateV3HistoryOptions(o V3HistoryOptions) error {
	if o.Limit < 0 || o.Limit > historyMaxLimit {
		return failure("invalid-options", "--limit must be between 1 and 1024.")
	}
	if !v3CanonicalPath(o.StateDir) {
		return failure("invalid-options", "A canonical private state directory is required.")
	}
	return nil
}

// HistoryTidyV3 lists the stored journals, newest first.
func HistoryTidyV3(ctx context.Context, o V3HistoryOptions) V3HistoryResult {
	return defaultTidyV3HistoryCoordinator().history(ctx, o)
}

// budgetClock starts counting at its first use, which is the start of the call.
func budgetClock(budget time.Duration) func() bool {
	var deadline time.Time
	return func() bool {
		if deadline.IsZero() {
			deadline = time.Now().Add(budget)
		}
		return time.Now().After(deadline)
	}
}

func failHistory(r V3HistoryResult, err error) V3HistoryResult {
	failed := failResult(newResult(r.Command), err)
	r.Status, r.Diagnostics, r.Entries, r.Truncated = "error", failed.Diagnostics, []V3HistoryEntry{}, []V3HistoryTruncation{}
	if failed.Diagnostics[0].Code == "invalid-options" {
		r.Status = "refused"
	}
	return r
}

type historyRecord struct {
	id   string
	time time.Time
}

// scanJournals reads the names of at most limit journal records of the store with
// their times and reports whether more exist.
func scanJournals(s *v3Store, limit int) ([]historyRecord, bool, error) {
	f, err := s.dirs[v3OperationsKind].Open(".")
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	entries, err := f.ReadDir(limit + 1)
	if err != nil && err != io.EOF {
		return nil, false, err
	}
	truncated := len(entries) > limit
	if truncated {
		entries = entries[:limit]
	}
	out := []historyRecord{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || len(name) < 6 || name[len(name)-5:] != ".json" || !validID(name[:len(name)-5]) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, false, err
		}
		out = append(out, historyRecord{id: name[:len(name)-5], time: info.ModTime().UTC()})
	}
	return out, truncated, nil
}

// historyRow is the working state of one shown journal.
type historyRow struct {
	entry     V3HistoryEntry
	view      *v3JournalView
	checked   bool
	candidate []int // journal entry indexes that need an object check
	lines     int   // first object line of the journal
}

func (c tidyV3HistoryCoordinator) history(ctx context.Context, o V3HistoryOptions) V3HistoryResult {
	r := newV3HistoryResult(o.List)
	if err := ValidateV3HistoryOptions(o); err != nil {
		return failHistory(r, err)
	}
	limit := o.Limit
	if limit == 0 {
		limit = historyDefaultLimit
	}
	ctx, cancel := context.WithTimeout(ctx, 2*historyBudget)
	defer cancel()
	ctx, _, closeSession, err := withPlanningSession(ctx)
	if err != nil {
		return failHistory(r, err)
	}
	defer closeSession()
	n, err := restoreRepository(ctx, c.identity, c.observeArgs, o.Path, o.StateDir)
	if err != nil {
		return failHistory(r, err)
	}
	s, err := openV3ReadStore(o.StateDir)
	if err != nil {
		return failHistory(r, err)
	}
	defer s.Close()
	records, scanTruncated, err := scanJournals(s, c.scanLimit)
	if err != nil {
		return failHistory(r, err)
	}
	sort.Slice(records, func(i, j int) bool {
		if !records[i].time.Equal(records[j].time) {
			return records[i].time.After(records[j].time)
		}
		return records[i].id < records[j].id
	})
	r.Total, r.ScanTruncated = len(records), scanTruncated
	shown := records[:min(len(records), limit)]
	rows := make([]historyRow, len(shown))
	objects := []string{}
	objectWanted, objectShown := 0, 0
	checkedJournals := 0
	for i, rec := range shown {
		row := &rows[i]
		row.entry = V3HistoryEntry{PlanID: rec.id, Time: rec.time.Format(time.RFC3339Nano), Mode: "unknown", RecoveryRefs: "absent", JournalStatus: "incomplete"}
		if c.expired() {
			continue
		}
		checkedJournals++
		view, err := s.readJournalView(rec.id)
		if err != nil {
			if ctx.Err() != nil {
				return failHistory(r, err)
			}
			row.entry.JournalStatus = "unreadable"
			continue
		}
		row.view = &view
		if err := c.describe(ctx, n, row); err != nil {
			if ctx.Err() != nil {
				return failHistory(r, err)
			}
			row.entry.JournalStatus = "incomplete"
			row.view = nil
			continue
		}
		objectWanted += len(row.candidate)
		if len(objects)+len(row.candidate) > c.objectLines {
			row.entry.JournalStatus, row.candidate = "incomplete", nil
			continue
		}
		row.lines = len(objects)
		for _, k := range row.candidate {
			objects = append(objects, v3Eligible(view.Plan)[k].OID)
		}
		objectShown += len(row.candidate)
	}
	if len(objects) > 0 {
		readable, err := n.readableCommits(ctx, objects, historyObjectBytes)
		for i := range rows {
			row := &rows[i]
			if row.view == nil || len(row.candidate) == 0 {
				continue
			}
			if err != nil {
				if ctx.Err() != nil {
					return failHistory(r, err)
				}
				row.entry.JournalStatus = "incomplete"
				continue
			}
			for k := range row.candidate {
				if readable[row.lines+k] {
					row.entry.Restorable++
				}
			}
		}
		if err != nil {
			objectShown = 0
		}
	}
	for _, row := range rows {
		r.Entries = append(r.Entries, row.entry)
	}
	if len(shown) < r.Total || scanTruncated {
		r.Truncated = append(r.Truncated, V3HistoryTruncation{Unit: "journals", Shown: len(shown), Total: r.Total})
	}
	if objectWanted > objectShown {
		r.Truncated = append(r.Truncated, V3HistoryTruncation{Unit: "objects", Shown: objectShown, Total: objectWanted})
	}
	if checkedJournals < len(shown) {
		r.Truncated = append(r.Truncated, V3HistoryTruncation{Unit: "checked-journals", Shown: checkedJournals, Total: len(shown)})
	}
	r.Status = "ok"
	return r
}

// describe fills the journal-derived and reference-derived fields of a row and
// selects the entries whose commit has to be checked for the restorable count.
func (c tidyV3HistoryCoordinator) describe(ctx context.Context, n restoreNative, row *historyRow) error {
	view := *row.view
	e := &row.entry
	e.Mode = view.Plan.Mode
	e.JournalStatus = "incomplete"
	if view.Journal.Phase == "complete" {
		e.JournalStatus = "complete"
	}
	for _, c := range view.Plan.Candidates {
		if c.Decision != "delete" {
			e.Retained++
		}
	}
	names := []string{}
	for _, je := range view.Journal.Entries {
		if je.Phase == "action-complete" {
			e.Deleted++
		}
		if d := je.Decision; d != nil {
			switch d.Kind {
			case "human":
				e.DecisionKinds.Human++
			case "ai":
				e.DecisionKinds.AI++
			}
		}
	}
	eligible := v3Eligible(view.Plan)
	local := view.Plan.Mode == "local-refs"
	for i, je := range view.Journal.Entries {
		if !confirmedRecovery(je) {
			continue
		}
		names = append(names, je.PlannedRecovery.Name)
		if local && validRestoreRefName(eligible[i].Name) {
			names = append(names, eligible[i].Name)
		}
	}
	refs, err := n.readRefs(ctx, names)
	if err != nil {
		return err
	}
	confirmed, present := 0, 0
	for i, je := range view.Journal.Entries {
		if !confirmedRecovery(je) {
			continue
		}
		confirmed++
		recovery := refs[je.PlannedRecovery.Name]
		if !recovery.Present || recovery.Symbolic || recovery.OID != je.PlannedRecovery.CommitID {
			continue
		}
		present++
		if local && validRestoreRefName(eligible[i].Name) && !refs[eligible[i].Name].Present {
			row.candidate = append(row.candidate, i)
		}
	}
	switch {
	case confirmed == 0 || present == 0:
		e.RecoveryRefs = "absent"
	case present == confirmed:
		e.RecoveryRefs = "present"
	default:
		e.RecoveryRefs = "partial"
	}
	return nil
}
