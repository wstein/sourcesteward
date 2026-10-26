// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package workspace

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	pruneV4HistoryBytes = 4 << 20
)

type pruneV4HistoryCoordinator struct {
	scanLimit, scanBytes, outputBytes int
	afterRead                         func()
}

// HistoryRegistrationPruneV4 lists strict plan-bound prune journals read-only.
// It never opens the payload directory, creates state, or invokes native mutations.
func HistoryRegistrationPruneV4(ctx context.Context, o RegistrationPruneV4HistoryOptions) RegistrationPruneV4HistoryResult {
	return (pruneV4HistoryCoordinator{scanLimit: historyScanLimit, scanBytes: pruneV4HistoryBytes, outputBytes: pruneV4HistoryBytes}).history(ctx, o)
}

func (c pruneV4HistoryCoordinator) history(ctx context.Context, o RegistrationPruneV4HistoryOptions) RegistrationPruneV4HistoryResult {
	r := newPruneV4HistoryResult()
	if o.Limit < 0 || o.Limit > historyMaxLimit || !v3CanonicalPath(o.StateDir) || o.Path == "" || !validText(o.Path) {
		return failPruneV4History(r, failure("invalid-options", "A repository path, canonical private state directory and limit in 1..1024 are required."))
	}
	limit := o.Limit
	if limit == 0 {
		limit = historyDefaultLimit
	}
	ctx, cancel := context.WithTimeout(ctx, historyBudget)
	defer cancel()
	ctx, _, closeSession, err := withPlanningSession(ctx)
	if err != nil {
		return failPruneV4History(r, err)
	}
	defer closeSession()
	n, err := restoreRepository(ctx, gitNativeIdentity, nil, o.Path, o.StateDir)
	if err != nil {
		return failPruneV4History(r, err)
	}
	parent, err := openStoreKinds(o.StateDir, false, []string{pruneV4Namespace})
	if err != nil {
		return failPruneV4History(r, err)
	}
	defer parent.Close()
	s, err := openStoreKinds(filepath.Join(o.StateDir, pruneV4Namespace), false, []string{"plans", "journals"})
	if err != nil {
		return failPruneV4History(r, err)
	}
	defer s.Close()
	records, scanTruncated, err := scanPruneV4History(s, c.scanLimit)
	if err != nil {
		return failPruneV4History(r, err)
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].time.Equal(records[j].time) {
			return records[i].id < records[j].id
		}
		return records[i].time.After(records[j].time)
	})
	remaining := c.scanBytes
	recordsRead := []pruneV4HistoryRead{}
	// Reserve space for the complete envelope and truncation summary before adding rows.
	base, _ := json.Marshal(r)
	outputSize := len(base) + 1024
	scanBytesTruncated, outputTruncated := false, false
	for _, record := range records {
		if err = ctx.Err(); err != nil {
			return failPruneV4History(r, err)
		}
		// Each strict reader may consume its full 1-MiB record allowance under a
		// concurrent change. Reserve that worst case before calling it, then charge
		// the exact canonical record size on success. Even a refused read is bounded.
		if remaining < pruneV4RecordLimit {
			scanBytesTruncated = true
			break
		}
		planRead, e := pruneV4HistoryCapture(s.dirs["plans"], record.id+".json")
		if e != nil {
			return failPruneV4History(r, e)
		}
		p, e := ReadRegistrationPruneV4Plan(o.StateDir, record.id)
		if e != nil {
			return failPruneV4History(r, e)
		}
		b, e := pruneV4Canonical(p, "")
		if e != nil {
			return failPruneV4History(r, e)
		}
		remaining -= len(b) + 1
		if int64(len(b)+1) != planRead.info.Size() {
			return failPruneV4History(r, failure("changed-state", "Stored contract-4 plan changed during listing."))
		}
		recordsRead = append(recordsRead, planRead)
		if p.Expected.SourceSnapshot.Identity.RepositoryDir != n.id.RepositoryDir {
			continue
		}
		r.Total++
		// Keep scanning bounded plans after reaching the output/count bound so
		// total counts only this repository's matching records, not other repos.
		if len(r.Entries) >= limit || outputTruncated {
			continue
		}
		if remaining < pruneV4RecordLimit {
			scanBytesTruncated = true
			break
		}
		journalRead, e := pruneV4HistoryCapture(s.dirs["journals"], record.id+".json")
		if e != nil {
			return failPruneV4History(r, e)
		}
		j, e := readPruneV4Journal(s, p)
		if e != nil {
			// Private-file failures are journal failures on this surface, never payload availability.
			if tidyErrorCode(e) == "prune-recovery-refused" || os.IsNotExist(e) {
				e = failure("prune-journal-refused", "Stored prune journal cannot be read safely.")
			}
			return failPruneV4History(r, e)
		}
		b, e = pruneV4Canonical(j, "")
		if e != nil {
			return failPruneV4History(r, e)
		}
		remaining -= len(b) + 1
		if int64(len(b)+1) != journalRead.info.Size() {
			return failPruneV4History(r, failure("changed-state", "Stored contract-4 journal changed during listing."))
		}
		recordsRead = append(recordsRead, journalRead)
		row := RegistrationPruneV4HistoryEntry{PlanID: p.PlanID, Time: record.time.Format(time.RFC3339Nano), Phase: j.Phase, Registration: p.Registration, PayloadPath: p.PayloadPath, HistoryRef: j.HistoryRef, HistoryOID: j.HistoryOID, RecoveryStatus: "not-prepared", Effects: []RegistrationPruneV4HistoryEffect{}, Remaining: append([]string{}, j.Remaining...)}
		if j.PayloadDigest != "" {
			digest := j.PayloadDigest
			row.PayloadDigest = &digest
			row.RecoveryStatus = "unknown"
		}
		for _, effect := range j.Effects {
			row.Effects = append(row.Effects, RegistrationPruneV4HistoryEffect{effect.Action, effect.Target, effect.Intent, effect.Outcome})
		}
		encoded, e := json.Marshal(row)
		if e != nil {
			return failPruneV4History(r, e)
		}
		if outputSize+len(encoded)+1 > c.outputBytes {
			outputTruncated = true
			continue
		}
		outputSize += len(encoded) + 1
		r.Entries = append(r.Entries, row)
	}
	if c.afterRead != nil {
		c.afterRead()
	}
	if err = parent.validate(); err != nil {
		return failPruneV4History(r, err)
	}
	if err = s.validate(); err != nil {
		return failPruneV4History(r, err)
	}
	for _, record := range recordsRead {
		current, e := record.root.Lstat(record.name)
		if e != nil || !regularPrivate(current) || !os.SameFile(record.info, current) || current.Size() != record.info.Size() || !current.ModTime().Equal(record.info.ModTime()) {
			return failPruneV4History(r, failure("changed-state", "Stored contract-4 history record changed during listing."))
		}
	}
	if err = ctx.Err(); err != nil {
		return failPruneV4History(r, err)
	}
	r.Truncated = scanTruncated || scanBytesTruncated || outputTruncated || len(r.Entries) < r.Total
	if len(r.Entries) < r.Total && !scanBytesTruncated && !outputTruncated {
		r.Truncation = append(r.Truncation, V3HistoryTruncation{Unit: "journals", Shown: len(r.Entries), Total: r.Total})
	}
	if scanBytesTruncated {
		r.Truncation = append(r.Truncation, V3HistoryTruncation{Unit: "scan-bytes", Shown: len(r.Entries), Total: r.Total})
	}
	if outputTruncated {
		r.Truncation = append(r.Truncation, V3HistoryTruncation{Unit: "output-bytes", Shown: len(r.Entries), Total: r.Total})
	}
	if scanTruncated {
		r.Truncation = append(r.Truncation, V3HistoryTruncation{Unit: "directory-entries", Shown: c.scanLimit, Total: c.scanLimit + 1})
	}
	r.Status = "ok"
	encoded, err := json.Marshal(r)
	if err != nil || len(encoded)+1 > c.outputBytes {
		return failPruneV4History(r, failure("output-limit", "Cannot emit a complete bounded registration history result."))
	}
	return r
}

func scanPruneV4History(s *store, limit int) ([]historyRecord, bool, error) {
	dir, err := s.dirs["journals"].Open(".")
	if err != nil {
		return nil, false, err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(limit + 1)
	if err != nil && err != io.EOF {
		return nil, false, err
	}
	truncated := len(entries) > limit
	if truncated {
		entries = entries[:limit]
	}
	records := []historyRecord{}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		if !pruneV4ID(id, 32) {
			return nil, false, failure("prune-journal-refused", "Contract-4 journal has an invalid record identity.")
		}
		info, e := s.dirs["journals"].Lstat(name)
		if e != nil || !regularPrivate(info) || info.Size() > pruneV4RecordLimit {
			return nil, false, failure("prune-journal-refused", "Contract-4 journal must be a private bounded single-link regular file.")
		}
		records = append(records, historyRecord{id: id, time: info.ModTime().UTC()})
	}
	return records, truncated, nil
}

// Capture file identities as well as directory identities. Strict decoding alone
// does not protect a previously decoded record from replacement before output.
type pruneV4HistoryRead struct {
	root *os.Root
	name string
	info os.FileInfo
}

func pruneV4HistoryCapture(root *os.Root, name string) (pruneV4HistoryRead, error) {
	info, err := root.Lstat(name)
	if err != nil || !regularPrivate(info) || info.Size() > pruneV4RecordLimit {
		return pruneV4HistoryRead{}, failure("prune-history-refused", "History records must be private bounded single-link regular files.")
	}
	return pruneV4HistoryRead{root, name, info}, nil
}
