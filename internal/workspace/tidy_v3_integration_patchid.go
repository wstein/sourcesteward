// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"bufio"
	"context"
	"errors"
	"io"
	"slices"
	"sort"
	"strings"
)

// patchIDResult is the outcome of the patch-id class (FR-003, FR-003w, FR-003b).
type patchIDResult struct {
	Proven        bool // every non-empty branch commit matched, no reason
	Whitespace    bool // some matched commit has no equal verbatim patch-id
	Reasons       []string
	Matched       int
	Unmatched     int
	Empty         int
	SkippedMerges int
	BinaryPaths   []string
}

type patchIDRow struct{ patch, commit string }

// logPatchArgs is the one patch stream of both sides: hashed by content, with
// no external diff, no textconv and no rename detection.
func logPatchArgs(rangeSpec string) []string {
	return []string{"log", "--no-merges", "-p", "--binary", "--no-ext-diff", "--no-textconv", "--no-renames", "--format=%H", rangeSpec, "--"}
}

// patchIDs pipes one `log -p` stream into `patch-id --stable` and
// `patch-id --verbatim` concurrently (one pass). The stream is never buffered.
func (r *integrationRun) patchIDs(ctx context.Context, attrTree, rangeSpec string) (stable, verbatim []patchIDRow, err error) {
	type outcome struct {
		rows []patchIDRow
		err  error
	}
	start := func(flag string, source *io.PipeReader) <-chan outcome {
		done := make(chan outcome, 1)
		go func() {
			var rows []patchIDRow
			_, e := r.run(ctx, integrationCommand{args: []string{"patch-id", flag}, stdin: source, limit: specPatchID.bytes(r.width)}, func(out io.Reader) error {
				return readIntegrationRecords(out, specPatchID, r.width, func(rec []byte) error {
					rows = append(rows, patchIDRow{patch: string(rec[:r.width]), commit: string(rec[r.width+1:])})
					return nil
				})
			})
			// An early exit must release the writer of the log stream.
			source.CloseWithError(io.ErrClosedPipe)
			done <- outcome{rows, e}
		}()
		return done
	}
	stableReader, stableWriter := io.Pipe()
	verbatimReader, verbatimWriter := io.Pipe()
	stableDone, verbatimDone := start("--stable", stableReader), start("--verbatim", verbatimReader)
	_, logErr := r.run(ctx, integrationCommand{attrTree: attrTree, args: logPatchArgs(rangeSpec), limit: integrationPatchStream}, func(out io.Reader) error {
		_, e := io.Copy(io.MultiWriter(stableWriter, verbatimWriter), out)
		return e
	})
	stableWriter.CloseWithError(logErr)
	verbatimWriter.CloseWithError(logErr)
	s, v := <-stableDone, <-verbatimDone
	// The budget and the caller's cancellation dominate the pipe errors that
	// an early stop of one process causes in the others.
	var first error
	for _, e := range []error{logErr, s.err, v.err} {
		switch {
		case e == nil:
		case errors.Is(e, errIntegrationBudget):
			return nil, nil, e
		case first == nil:
			first = e
		}
	}
	if first != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, first
	}
	return s.rows, v.rows, nil
}

// patchID evaluates the class for the tip against the base.
func (r *integrationRun) patchID(ctx context.Context, base, tip, mergeBase, tipTree, baseTree string) (patchIDResult, error) {
	var res patchIDResult
	fail := func(err error) (patchIDResult, error) {
		if err == errIntegrationLimit {
			return patchIDResult{Reasons: []string{"proof-limit"}}, nil
		}
		return patchIDResult{}, err
	}
	branchRange, baseRange := mergeBase+".."+tip, mergeBase+".."+base
	branchCount, err := r.count(ctx, "rev-list", "--count", "--no-merges", branchRange, "--")
	if err != nil {
		return fail(err)
	}
	baseCount, err := r.count(ctx, "rev-list", "--count", "--no-merges", baseRange, "--")
	if err != nil {
		return fail(err)
	}
	merges, err := r.count(ctx, "rev-list", "--count", "--merges", branchRange, "--")
	if err != nil {
		return fail(err)
	}
	if branchCount > 1024 || baseCount > 4096 {
		return patchIDResult{Reasons: []string{"proof-limit"}}, nil
	}
	res.SkippedMerges = merges
	if merges > 0 {
		res.Reasons = append(res.Reasons, "merge-commits-in-range")
	}
	if tidyV3HasConfigClass(r.notes, "attribute-source") {
		res.Reasons = append(res.Reasons, "attribute-source")
	}
	commits, err := r.lines(ctx, integrationCommand{args: []string{"log", "--no-merges", "--format=%H %T %P", branchRange, "--"}}, specLog)
	if err != nil {
		return fail(err)
	}
	if len(commits) != branchCount {
		return fail(errIntegrationLimit)
	}
	branchCommits := make([]string, len(commits))
	for i, line := range commits {
		branchCommits[i] = line[:r.width]
	}
	if res.BinaryPaths, err = r.binaryPaths(ctx, tipTree, branchCommits); err != nil {
		return fail(err)
	}
	diffReason, err := r.diffAttribute(ctx, tipTree, branchCommits)
	if err != nil {
		return fail(err)
	}
	if diffReason {
		res.Reasons = append(res.Reasons, "diff-attribute")
	}
	stable, verbatim, err := r.patchIDs(ctx, tipTree, branchRange)
	if err != nil {
		return fail(err)
	}
	baseStable, baseVerbatim, err := r.patchIDs(ctx, baseTree, baseRange)
	if err != nil {
		return fail(err)
	}
	res.Empty = len(branchCommits) - len(stable)
	if res.Empty < 0 || len(stable) != len(verbatim) {
		return fail(errIntegrationLimit)
	}
	baseCounts := map[string]int{}
	for _, row := range baseStable {
		baseCounts[row.patch]++
	}
	baseVerbatimByStable := map[string]map[string]bool{}
	verbatimOfBaseCommit := map[string]string{}
	for _, row := range baseVerbatim {
		verbatimOfBaseCommit[row.commit] = row.patch
	}
	for _, row := range baseStable {
		set := baseVerbatimByStable[row.patch]
		if set == nil {
			set = map[string]bool{}
			baseVerbatimByStable[row.patch] = set
		}
		set[verbatimOfBaseCommit[row.commit]] = true
	}
	verbatimOfCommit := map[string]string{}
	for _, row := range verbatim {
		verbatimOfCommit[row.commit] = row.patch
	}
	branchCounts := map[string]int{}
	for _, row := range stable {
		branchCounts[row.patch]++
	}
	multiset, absent := false, false
	for patch, n := range branchCounts {
		have := baseCounts[patch]
		res.Matched += min(n, have)
		res.Unmatched += n - min(n, have)
		multiset = multiset || have > 0 && have < n
		absent = absent || have == 0
	}
	for _, row := range stable {
		if baseCounts[row.patch] > 0 && !baseVerbatimByStable[row.patch][verbatimOfCommit[row.commit]] {
			res.Whitespace = true
		}
	}
	if multiset {
		res.Reasons = append(res.Reasons, "multiset-mismatch")
	}
	if res.Matched > 0 && absent {
		res.Reasons = append(res.Reasons, "partial-match")
	}
	res.Proven = res.Matched >= 1 && res.Unmatched == 0 && len(res.Reasons) == 0
	return res, nil
}

// binaryPaths lists the paths Git itself calls binary (numstat prints `-`),
// used only for report rendering (FR-003b); it decides no disposition.
func (r *integrationRun) binaryPaths(ctx context.Context, tipTree string, commits []string) ([]string, error) {
	if len(commits) == 0 {
		return nil, nil
	}
	out, err := r.lines(ctx, integrationCommand{attrTree: tipTree, args: []string{"diff-tree", "--stdin", "--no-commit-id", "--numstat", "-r", "-z", "--no-renames"}, stdin: strings.NewReader(strings.Join(commits, "\n") + "\n")}, specNumstat)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, record := range out {
		if strings.HasPrefix(record, "-\t-\t") {
			seen[record[4:]] = true
		}
	}
	paths := make([]string, 0, len(seen))
	for p := range seen {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths, nil
}

// touchedPaths lists the paths changed by the commits.
func (r *integrationRun) touchedPaths(ctx context.Context, tipTree string, commits []string) ([]string, error) {
	if len(commits) == 0 {
		return nil, nil
	}
	out, err := r.lines(ctx, integrationCommand{attrTree: tipTree, args: []string{"diff-tree", "--stdin", "--no-commit-id", "--name-only", "-r", "-z"}, stdin: strings.NewReader(strings.Join(commits, "\n") + "\n")}, specNameOnly)
	if err != nil {
		return nil, err
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

// diffAttribute reports whether `check-attr diff` on a touched path is anything
// but unspecified (FR-010a), with the attribute source pinned to the tip tree.
func (r *integrationRun) diffAttribute(ctx context.Context, tipTree string, commits []string) (bool, error) {
	paths, err := r.touchedPaths(ctx, tipTree, commits)
	if err != nil || len(paths) == 0 {
		return false, err
	}
	var found bool
	_, err = r.run(ctx, integrationCommand{attrTree: tipTree, args: []string{"check-attr", "--source=" + tipTree, "-z", "--stdin", "diff"}, stdin: strings.NewReader(strings.Join(paths, "\x00") + "\x00"), limit: specCheckAttrBytes}, func(out io.Reader) error {
		var e error
		found, e = parseCheckAttrStream(out, paths)
		return e
	})
	return found, err
}

const specCheckAttrBytes = 34603008

// parseCheckAttrStream reads `PATH NUL diff NUL VALUE NUL` for every path in
// order and reports whether any value is not unspecified (at most 8,192 records
// of at most 4,096-byte paths, 34,603,008 bytes).
func parseCheckAttrStream(out io.Reader, paths []string) (bool, error) {
	if len(paths) > 8192 {
		return false, errIntegrationLimit
	}
	reader := bufio.NewReaderSize(out, 64*1024)
	var total int64
	field := func() (string, error) {
		b, e := reader.ReadSlice(0)
		total += int64(len(b))
		if e != nil || total > specCheckAttrBytes || len(b) > integrationPathLimit+1 {
			return "", errIntegrationLimit
		}
		return string(b[:len(b)-1]), nil
	}
	found := false
	for _, want := range paths {
		path, e := field()
		if e != nil || path != want {
			return false, errIntegrationLimit
		}
		attr, e := field()
		if e != nil || attr != "diff" {
			return false, errIntegrationLimit
		}
		value, e := field()
		if e != nil {
			return false, e
		}
		found = found || value != "unspecified"
	}
	return found, nil
}
