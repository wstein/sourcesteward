// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
)

// The in-run memo of base-side patch-ids (Spec 031 FR-003, FR-008).
//
// Every tip of one plan is proven against the same base, but each tip has its
// own merge base, so each tip streamed the patches of its own range M..B through
// `patch-id`: a run computed the same base commits again for every tip. A
// commit's patch-id is a function of the commit and of the pinned attribute
// source only, so the memo keeps it for the life of the run (one repository,
// one planning operation) and a tip computes only the commits of its range the
// run has not seen. The ids are Git's own (`log -p` into `patch-id`, nothing
// custom); their order and the bounds of the range are those of the range
// stream: the list of the range comes from `rev-list --no-merges M..B` (the
// order of `log`), and the recorded size of each commit's part of the stream
// makes the 64 MiB bound of FR-008 a judgment on the whole range, memoized
// commits included. Anything the memo cannot account for exactly (an unexpected
// count, a stream that does not segment) falls back to the range stream.

// patchMemoMaxEntries bounds the memo: four times the 4,096 base-side commits
// of one tip (FR-008). A full memo computes without storing.
const patchMemoMaxEntries = 4 * 4096

// patchMemoKey is everything the id of a commit depends on besides the fixed
// per-run environment: the commit and the attribute source pinned for its
// stream (the `--attr-source=<tree>` arguments of the command, so another pin
// can never reuse an id). Stable and verbatim ids come from one stream and are
// stored together.
type patchMemoKey struct{ pin, commit string }

type patchMemoEntry struct {
	stable, verbatim       string
	hasStable, hasVerbatim bool // false: the commit has an empty patch
	// bytes is the size of the commit's part of the `log -p` stream.
	bytes int64
}

type patchIDMemo struct {
	mu      sync.Mutex
	entries map[patchMemoKey]patchMemoEntry
}

func newPatchIDMemo() *patchIDMemo {
	return &patchIDMemo{entries: map[patchMemoKey]patchMemoEntry{}}
}

func (m *patchIDMemo) get(k patchMemoKey) (patchMemoEntry, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[k]
	return e, ok
}

// put stores a completed result; an existing key is kept and a full memo
// stores nothing.
func (m *patchIDMemo) put(k patchMemoKey, e patchMemoEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.entries[k]; ok || len(m.entries) >= patchMemoMaxEntries {
		return
	}
	m.entries[k] = e
}

func (m *patchIDMemo) size() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.entries)
}

// enablePatchMemo gives the run a memo. Planning calls it; apply never does.
func (r *integrationRun) enablePatchMemo() {
	r.patchMemo = newPatchIDMemo()
}

// patchSegmenter measures the part of a `log --format=%H -p` stream that
// belongs to each commit. A part starts at the line that is exactly the next
// expected commit id (a diff line always has a prefix; any other bare id line
// is a mismatch) and ends before the
// next such line. It never stores more than one id of a line.
type patchSegmenter struct {
	width   int
	expect  []string
	sizes   []int64
	next    int   // index of the next expected header
	cur     int   // commit whose part is open, -1 before the first header
	pending int64 // bytes of the current, incomplete line
	lineLen int
	head    []byte
	bad     bool
}

func newPatchSegmenter(width int, expect []string) *patchSegmenter {
	return &patchSegmenter{width: width, expect: expect, sizes: make([]int64, len(expect)), cur: -1, head: make([]byte, 0, width+1)}
}

func (s *patchSegmenter) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		piece := p
		if i >= 0 {
			piece = p[:i]
		}
		if room := s.width + 1 - len(s.head); room > 0 {
			s.head = append(s.head, piece[:min(room, len(piece))]...)
		}
		s.lineLen += len(piece)
		s.pending += int64(len(piece))
		if i < 0 {
			break
		}
		s.pending++ // the newline
		s.endLine()
		p = p[i+1:]
	}
	return n, nil
}

func (s *patchSegmenter) endLine() {
	if s.lineLen == s.width && hexExact(s.head, s.width) {
		if s.next < len(s.expect) && string(s.head) == s.expect[s.next] {
			s.cur = s.next
			s.next++
		} else {
			s.bad = true // a bare id that is not the next expected header
		}
	}
	if s.cur < 0 {
		s.bad = true // data before the first header
	} else {
		s.sizes[s.cur] += s.pending
	}
	s.pending, s.lineLen, s.head = 0, 0, s.head[:0]
}

// finish reports the sizes when the stream held every expected header, in
// order, and nothing else before the first.
func (s *patchSegmenter) finish() ([]int64, bool) {
	if s.pending > 0 {
		s.endLine() // a last line without a newline
	}
	return s.sizes, !s.bad && s.next == len(s.expect)
}

// basePatchIDs returns the rows of patchIDs(ctx, baseTree, baseRange) for the
// base side of a tip, computing only the commits of the range this run has not
// computed with the same pinned attribute source.
func (r *integrationRun) basePatchIDs(ctx context.Context, baseTree, baseRange string, baseCount int) (stable, verbatim []patchIDRow, err error) {
	memo := r.patchMemo
	if memo == nil {
		return r.patchIDs(ctx, baseTree, baseRange)
	}
	direct := func() ([]patchIDRow, []patchIDRow, error) { return r.patchIDs(ctx, baseTree, baseRange) }
	commits, err := r.lines(ctx, integrationCommand{args: []string{"rev-list", "--no-merges", baseRange, "--"}}, specBaseCommits)
	switch {
	case errors.Is(err, errIntegrationBudget) || ctx.Err() != nil:
		if err == nil {
			err = ctx.Err()
		}
		return nil, nil, err
	case err != nil || len(commits) != baseCount:
		return direct()
	}
	pin := strings.Join(r.session.attrSource(baseTree), "\x00")
	entries := make([]patchMemoEntry, len(commits))
	var missing []string
	var missingAt []int
	for i, c := range commits {
		if e, ok := memo.get(patchMemoKey{pin: pin, commit: c}); ok {
			entries[i] = e
		} else {
			missing, missingAt = append(missing, c), append(missingAt, i)
		}
	}
	if len(missing) > 0 {
		fresh, ok, err := r.computePatchEntries(ctx, baseTree, missing)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			return direct()
		}
		for j, i := range missingAt {
			entries[i] = fresh[j]
		}
		for j, c := range missing {
			memo.put(patchMemoKey{pin: pin, commit: c}, fresh[j])
		}
	}
	var total int64
	for _, e := range entries {
		total += e.bytes
	}
	if limit := r.patchBound(); total > limit {
		return nil, nil, limitHit(commandName(logPatchArgs(baseRange)), "bytes", limit, limit+1)
	}
	for i, c := range commits {
		if entries[i].hasStable {
			stable = append(stable, patchIDRow{patch: entries[i].stable, commit: c})
		}
		if entries[i].hasVerbatim {
			verbatim = append(verbatim, patchIDRow{patch: entries[i].verbatim, commit: c})
		}
	}
	return stable, verbatim, nil
}

// computePatchEntries streams the patches of exactly the given commits (their
// own diff against their first parent, as in a range) through both `patch-id`
// modes. It reports ok false when the stream cannot be attributed to the
// commits exactly; nothing is stored here and an error is returned as is.
func (r *integrationRun) computePatchEntries(ctx context.Context, baseTree string, commits []string) ([]patchMemoEntry, bool, error) {
	args := append(slices.Clone(patchStreamOptions), "--no-walk=unsorted", "--stdin")
	seg := newPatchSegmenter(r.width, commits)
	stable, verbatim, err := r.patchStreams(ctx, integrationCommand{attrTree: baseTree, args: args, stdin: bytes.NewReader(joinRecords(commits, '\n')), limit: r.patchBound()}, seg)
	if err != nil {
		return nil, false, err
	}
	sizes, ok := seg.finish()
	if !ok {
		return nil, false, nil
	}
	return assignPatchRows(commits, sizes, stable, verbatim)
}

// assignPatchRows attributes the patch-id rows of a stream to its commits. It
// reports ok false for a row that names an unexpected commit or names one twice.
func assignPatchRows(commits []string, sizes []int64, stable, verbatim []patchIDRow) ([]patchMemoEntry, bool, error) {
	index := make(map[string]int, len(commits))
	out := make([]patchMemoEntry, len(commits))
	for i, c := range commits {
		index[c] = i
		out[i].bytes = sizes[i]
	}
	for _, row := range stable {
		i, known := index[row.commit]
		if !known || out[i].hasStable {
			return nil, false, nil
		}
		out[i].stable, out[i].hasStable = row.patch, true
	}
	for _, row := range verbatim {
		i, known := index[row.commit]
		if !known || out[i].hasVerbatim {
			return nil, false, nil
		}
		out[i].verbatim, out[i].hasVerbatim = row.patch, true
	}
	return out, true, nil
}
