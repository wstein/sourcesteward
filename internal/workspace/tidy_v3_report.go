// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Review report data collection (Spec 031 FR-033, contracts/review-report.md).
// Every command is a fixed typed argv under the planning session and the
// private object directory of the run, like the proofs; the result tree of
// `merge-tree --write-tree` lives only in that private directory. A command
// bound exceeded is the reason proof-limit for the entry, never a partial
// section.

// reportRequest is one tip whose proofs found it needing review.
type reportRequest struct {
	Base, Tip string
	Verdict   integrationVerdict
	Policy    V3ProofPolicy
	Bounds    reportBounds
	// Ranges are the resolved consolidation ranges, in command line order.
	Ranges []V3ReviewRange
}

const reportRecordBytes = 4224

var (
	reportDatePattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:Z|[+-][0-9]{2}:[0-9]{2})$`)
	reportHunkHeader  = regexp.MustCompile(`^@@ -([0-9]+)(?:,([0-9]+))? \+([0-9]+)(?:,([0-9]+))? @@`)
	reportModePattern = regexp.MustCompile(`^[0-7]{6}$`)

	// Report command bounds (contracts/review-report.md#command-bounds).
	specReportLog = integrationSpec{"log", reportMaxCommits, bothWidths(reportMaxCommits * reportRecordBytes), '\n', func(r []byte, w int) bool {
		parts := bytes.Split(r, []byte{0})
		return len(r)+1 <= reportRecordBytes && len(parts) == 4 && hexExact(parts[0], w) && reportDatePattern.Match(parts[3])
	}}
	// The base-side scan that finds candidate commits is bounded by the
	// base-side commit bound of the proofs (FR-008).
	specReportScan = integrationSpec{"log candidates", 4096, bothWidths(4096 * reportRecordBytes), '\n', func(r []byte, w int) bool {
		parts := bytes.Split(r, []byte{0})
		return len(r)+1 <= reportRecordBytes && len(parts) == 2 && hexExact(parts[0], w)
	}}
	specReportBlobSizes = integrationSpec{"cat-file --batch-check", 2 * reportMaxFiles, [2]int64{2 * reportMaxFiles * 61, 2 * reportMaxFiles * 89}, '\n', func(r []byte, w int) bool {
		fields := strings.Fields(string(r))
		return len(fields) == 3 && hexExact([]byte(fields[0]), w) && fields[1] == "blob" && integrationCountPattern.MatchString(fields[2])
	}}
)

const (
	reportRawRecords = 8192
	reportRawBytes   = 34603008
	reportTreeBytes  = 4325376
)

// collectReportEntry gathers the report data of one tip. The branch name is the
// caller's: tips are shared by several references.
func (r *integrationRun) collectReportEntry(ctx context.Context, q reportRequest) (V3ReportEntry, error) {
	r.reporting = true
	defer func() { r.reporting = false }()
	e := V3ReportEntry{TipOID: q.Tip, BaseOID: q.Base, Commits: []V3ReportCommit{}, Candidates: []V3ReportCandidate{}, Diff: emptyReportDiff("unavailable")}
	reasons := slices.Clone(q.Verdict.Reasons)
	finish := func() (V3ReportEntry, error) {
		e.Reasons = reasonOrder(reasons)
		e.Proofs = reportProofs(q.Policy, e.Reasons, q.Verdict)
		e.Verdict = suggestVerdict(e)
		return e, nil
	}
	// degrade turns a command bound into the entry reason proof-limit.
	degrade := func(err error) error {
		if errors.Is(err, errIntegrationLimit) {
			reasons = append(reasons, "proof-limit")
			return nil
		}
		return err
	}
	r.ensureObjectDir()
	if r.objErr != nil {
		reasons = append(reasons, "object-dir-unavailable")
		return finish()
	}
	span := q.Base + ".." + q.Tip
	if err := r.reportCommits(ctx, &e, span); err != nil {
		if err = degrade(err); err != nil {
			return e, err
		}
	}
	bases, err := r.mergeBases(ctx, q.Base, q.Tip)
	if err != nil {
		if err = degrade(err); err != nil {
			return e, err
		}
		return finish()
	}
	if len(bases) != 1 {
		reasons = append(reasons, "ambiguous-merge-base")
		return finish()
	}
	mergeBase := bases[0]
	e.MergeBaseOID = mergeBase
	tipTree, mbTree, baseTree, err := r.trees(ctx, q.Tip, mergeBase, q.Base)
	if err != nil {
		if err = degrade(err); err != nil {
			return e, err
		}
		return finish()
	}
	gate, err := r.mergeGate(ctx, q.Base, q.Tip, mergeBase, tipTree, mbTree, baseTree)
	if err != nil {
		if err = degrade(err); err != nil {
			return e, err
		}
		return finish()
	}
	reasons = append(reasons, gate...)
	// pair is the two sides of the diff shown; attr pins the attribute source.
	var pair [2]string
	var attr, source string
	var conflicted []string
	if len(gate) > 0 {
		pair, attr, source = [2]string{mergeBase, q.Tip}, tipTree, "branch-against-merge-base"
	} else {
		result, paths, code, err := r.reportMergeTree(ctx, baseTree, q.Base, q.Tip)
		if err != nil {
			if err = degrade(err); err != nil {
				return e, err
			}
			return finish()
		}
		switch {
		case code == 1:
			reasons = append(reasons, "conflict")
		case result != baseTree:
			reasons = append(reasons, "would-add-content")
		}
		pair, attr, source, conflicted = [2]string{baseTree, result}, result, "merge-result", paths
	}
	if err = r.reportDiff(ctx, &e, q, pair, attr, source, conflicted, &reasons); err != nil {
		if err = degrade(err); err != nil {
			return e, err
		}
	}
	if err = r.reportCandidates(ctx, &e, q, mergeBase); err != nil {
		if err = degrade(err); err != nil {
			return e, err
		}
	}
	if len(q.Ranges) > 0 {
		if err = r.reportPairing(ctx, &e, q, reasons); err != nil {
			if err = degrade(err); err != nil {
				return e, err
			}
		}
	}
	return finish()
}

func emptyReportDiff(source string) V3ReportDiff {
	return V3ReportDiff{Source: source, ConflictedPaths: []V3Untrusted{}, BinaryFiles: []V3ReportBinary{}, Files: []V3ReportFile{}, Hunks: []V3ReportHunk{}}
}

// reportCommits lists the newest commits of base..tip (merge commits included).
func (r *integrationRun) reportCommits(ctx context.Context, e *V3ReportEntry, span string) error {
	total, err := r.count(ctx, "rev-list", "--count", span, "--")
	if err != nil {
		return err
	}
	records, err := r.lines(ctx, integrationCommand{args: []string{"log", "-n", strconv.Itoa(reportMaxCommits), "--format=%H%x00%s%x00%an%x00%aI", span, "--"}}, specReportLog)
	if err != nil {
		return err
	}
	for _, record := range records {
		parts := strings.Split(record, "\x00")
		e.Commits = append(e.Commits, V3ReportCommit{OID: parts[0], Subject: untrusted(clipText(parts[1], reportSubjectBytes)), Author: untrusted(parts[2]), Date: parts[3]})
	}
	if total > len(e.Commits) {
		e.CommitsTruncated = &V3ReportTruncation{Unit: "commits", Shown: len(e.Commits), Total: total}
	}
	return nil
}

// clipText cuts s to at most n bytes on a rune boundary.
func clipText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && n < len(s) && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n]
}

// reportMergeTree runs the merge in the private object directory and lists the
// conflicted paths: exit 0 prints the result tree alone, exit 1 the tree and the
// conflicted names, any other exit is a limit.
func (r *integrationRun) reportMergeTree(ctx context.Context, attrTree, base, tip string) (string, []string, int, error) {
	var tree string
	var names []string
	code, err := r.run(ctx, integrationCommand{attrTree: attrTree, args: []string{"merge-tree", "--write-tree", "--no-messages", "--name-only", "-z", base, tip}, tolerate: true, limit: reportTreeBytes}, func(out io.Reader) error {
		var e error
		tree, names, e = parseReportMergeTree(out, r.width)
		return e
	})
	if err == nil && (code != 0 && code != 1 || code == 0 && len(names) > 0) {
		err = errIntegrationLimit
	}
	return tree, names, code, err
}

// parseReportMergeTree reads `OID NUL (PATH NUL)*`: the result tree and the
// conflicted paths, at most 8,192 of them in 4,325,376 bytes.
func parseReportMergeTree(out io.Reader, width int) (string, []string, error) {
	reader := bufio.NewReaderSize(out, 64*1024)
	var total int64
	field := func() (string, bool, error) {
		b, e := reader.ReadSlice(0)
		total += int64(len(b))
		if e == io.EOF && len(b) == 0 {
			return "", false, nil
		}
		if e != nil || len(b) > integrationPathLimit+1 || total > reportTreeBytes {
			return "", false, errIntegrationLimit
		}
		return string(b[:len(b)-1]), true, nil
	}
	tree, ok, err := field()
	if err != nil || !ok || !hexExact([]byte(tree), width) {
		return "", nil, errIntegrationLimit
	}
	var names []string
	for {
		name, ok, err := field()
		if err != nil {
			return "", nil, err
		}
		if !ok {
			return tree, names, nil
		}
		if !integrationPath([]byte(name)) {
			return "", nil, errIntegrationLimit
		}
		if !slices.Contains(names, name) {
			if len(names) >= reportRawRecords {
				return "", nil, errIntegrationLimit
			}
			names = append(names, name)
		}
	}
}

type reportChange struct {
	oldMode, newMode, oldOID, newOID, status, path string
}

// parseReportRaw reads `:OLDMODE NEWMODE OID OID STATUS NUL PATH NUL` records,
// at most 8,192 of them in 34,603,008 bytes.
func parseReportRaw(src io.Reader, width int) ([]reportChange, error) {
	var out []reportChange
	var total int64
	reader := bufio.NewReaderSize(src, 64*1024)
	for {
		meta, e := reader.ReadSlice(0)
		if e == io.EOF && len(meta) == 0 {
			return out, nil
		}
		path, pe := reader.ReadSlice(0)
		total += int64(len(meta) + len(path))
		if e != nil || pe != nil || len(out) >= reportRawRecords || total > reportRawBytes || len(meta) < 2 || meta[0] != ':' {
			return nil, errIntegrationLimit
		}
		f := strings.Fields(string(meta[1 : len(meta)-1]))
		if len(f) != 5 || !reportModePattern.MatchString(f[0]) || !reportModePattern.MatchString(f[1]) || !hexExact([]byte(f[2]), width) || !hexExact([]byte(f[3]), width) || len(f[4]) != 1 || !integrationPath(path[:len(path)-1]) {
			return nil, errIntegrationLimit
		}
		out = append(out, reportChange{f[0], f[1], f[2], f[3], f[4], string(path[:len(path)-1])})
	}
}

// reportRaw reads `diff-tree -r --raw --no-renames --no-abbrev -z` of two sides.
func (r *integrationRun) reportRaw(ctx context.Context, attrTree string, pair [2]string) ([]reportChange, error) {
	var out []reportChange
	_, err := r.run(ctx, integrationCommand{attrTree: attrTree, args: []string{"diff-tree", "-r", "--raw", "--no-renames", "--no-abbrev", "-z", pair[0], pair[1]}, limit: reportRawBytes}, func(src io.Reader) error {
		var e error
		out, e = parseReportRaw(src, r.width)
		return e
	})
	return out, err
}

type reportNumstat struct {
	added, deleted int
	binary         bool
}

// reportDiff fills the diff section: the stat, the files with their modes, the
// binary paths, the bounded unified diff and the whitespace-only flag.
func (r *integrationRun) reportDiff(ctx context.Context, e *V3ReportEntry, q reportRequest, pair [2]string, attr, source string, conflicted []string, reasons *[]string) error {
	raw, err := r.reportRaw(ctx, attr, pair)
	if err != nil {
		return err
	}
	rows, err := r.lines(ctx, integrationCommand{attrTree: attr, args: []string{"diff-tree", "--numstat", "-r", "-z", "--no-renames", pair[0], pair[1]}}, specNumstat)
	if err != nil {
		return err
	}
	stats := map[string]reportNumstat{}
	d := emptyReportDiff(source)
	for _, row := range rows {
		f := strings.SplitN(row, "\t", 3)
		n := reportNumstat{binary: f[0] == "-"}
		if !n.binary {
			n.added, _ = strconv.Atoi(f[0])
			n.deleted, _ = strconv.Atoi(f[1])
		}
		stats[f[2]] = n
		d.Stat.Insertions, d.Stat.Deletions = d.Stat.Insertions+n.added, d.Stat.Deletions+n.deleted
	}
	d.Stat.Files = len(raw)
	for _, p := range conflicted {
		d.ConflictedPaths = append(d.ConflictedPaths, untrusted(p))
	}
	var binaryOIDs []string
	for i, c := range raw {
		if source == "merge-result" && (c.oldMode == "160000" || c.newMode == "160000") {
			*reasons = append(*reasons, "gitlink-change")
		}
		if i >= reportMaxFiles {
			continue
		}
		file := V3ReportFile{Path: untrusted(c.path), OldMode: c.oldMode, NewMode: c.newMode, Kind: reportFileKind(c, stats[c.path].binary)}
		if c.oldMode != "000000" && c.newMode != "000000" && c.oldMode != c.newMode {
			file.TypeChange = reportModeName(c.oldMode) + "->" + reportModeName(c.newMode)
		}
		if file.Kind == "gitlink" {
			file.OldOID, file.NewOID = zeroAsEmpty(c.oldOID), zeroAsEmpty(c.newOID)
		}
		d.Files = append(d.Files, file)
		if file.Kind == "binary" {
			binaryOIDs = append(binaryOIDs, c.oldOID, c.newOID)
		}
	}
	if d.BinaryFiles, err = r.reportBinaries(ctx, d.Files, raw, binaryOIDs); err != nil {
		return err
	}
	text, total, lines, err := r.reportPatch(ctx, attr, pair, false)
	if err != nil {
		return err
	}
	if total > 0 && source == "merge-result" {
		_, whitespaceTotal, _, werr := r.reportPatch(ctx, attr, pair, true)
		if werr != nil {
			return werr
		}
		d.WhitespaceOnly = whitespaceTotal == 0
	}
	shown, cut := boundReportDiff(text, q.Bounds.MaxDiffBytes)
	d.shownFiles, d.totalFiles, d.shownLines, d.totalLines = shown.files, len(raw), shown.lines, lines
	switch {
	case cut == "files":
		d.Truncated = &V3ReportTruncation{Unit: "files", Shown: shown.files, Total: len(raw)}
	case cut == "lines":
		d.Truncated = &V3ReportTruncation{Unit: "lines", Shown: shown.lines, Total: lines}
	case cut == "bytes" || int64(len(text)) < total:
		d.Truncated = &V3ReportTruncation{Unit: "bytes", Shown: len(shown.text), Total: int(total)}
	}
	e.rawDiff = shown.text
	d.Hunks = parseReportHunks(shown.text, d.Files)
	e.Diff = d
	return nil
}

func zeroAsEmpty(oid string) string {
	if strings.Trim(oid, "0") == "" {
		return ""
	}
	return oid
}

func reportModeName(mode string) string {
	switch mode {
	case "100644":
		return "regular"
	case "100755":
		return "executable"
	case "120000":
		return "symlink"
	case "160000":
		return "gitlink"
	case "040000":
		return "tree"
	case "000000":
		return "none"
	}
	return mode
}

// reportFileKind classifies a changed path from Git's own modes and numstat.
func reportFileKind(c reportChange, binary bool) string {
	switch {
	case c.oldMode == "160000" || c.newMode == "160000":
		return "gitlink"
	case c.oldMode == "120000" || c.newMode == "120000":
		return "symlink"
	case binary:
		return "binary"
	case c.oldMode != c.newMode && c.oldOID == c.newOID && c.oldMode != "000000" && c.newMode != "000000":
		return "mode-only"
	}
	return "content"
}

// reportBinaries names the binary paths with their old and new blob and size.
func (r *integrationRun) reportBinaries(ctx context.Context, files []V3ReportFile, raw []reportChange, oids []string) ([]V3ReportBinary, error) {
	out := []V3ReportBinary{}
	var want []string
	for _, oid := range oids {
		if zeroAsEmpty(oid) != "" && !slices.Contains(want, oid) {
			want = append(want, oid)
		}
	}
	sizes := map[string]int64{}
	if len(want) > 0 {
		lines, err := r.lines(ctx, integrationCommand{args: []string{"cat-file", "--batch-check"}, stdin: bytes.NewReader(joinRecords(want, '\n'))}, specReportBlobSizes)
		if err != nil || len(lines) != len(want) {
			return nil, errIntegrationLimit
		}
		for i, line := range lines {
			f := strings.Fields(line)
			if f[0] != want[i] {
				return nil, errIntegrationLimit
			}
			sizes[f[0]], _ = strconv.ParseInt(f[2], 10, 64)
		}
	}
	for i, file := range files {
		if file.Kind != "binary" {
			continue
		}
		c := raw[i]
		out = append(out, V3ReportBinary{Path: file.Path, OldBlob: zeroAsEmpty(c.oldOID), NewBlob: zeroAsEmpty(c.newOID), OldSize: sizes[c.oldOID], NewSize: sizes[c.newOID]})
	}
	return out, nil
}

// reportPatch reads the unified diff of the two sides (no external diff, no
// textconv, no renames), keeping the first megabyte and counting the rest.
func (r *integrationRun) reportPatch(ctx context.Context, attr string, pair [2]string, ignoreWhitespace bool) (string, int64, int, error) {
	args := []string{"diff-tree", "-p"}
	if ignoreWhitespace {
		args = append(args, "-w")
	}
	args = append(args, "--no-ext-diff", "--no-textconv", "--no-renames", pair[0], pair[1])
	keep := reportDiffRetain
	if ignoreWhitespace {
		keep = 0
	}
	var head []byte
	var total int64
	var lines int
	var last byte
	_, err := r.run(ctx, integrationCommand{attrTree: attr, args: args, limit: reportDiffStream}, func(src io.Reader) error {
		buf := make([]byte, 32<<10)
		for {
			n, e := src.Read(buf)
			if n > 0 {
				chunk := buf[:n]
				if room := keep - len(head); room > 0 {
					head = append(head, chunk[:min(room, n)]...)
				}
				total += int64(n)
				lines += bytes.Count(chunk, []byte{'\n'})
				last = chunk[n-1]
			}
			if e == io.EOF {
				return nil
			}
			if e != nil {
				return e
			}
		}
	})
	if total > 0 && last != '\n' {
		lines++
	}
	if int64(len(head)) < total && keep > 0 {
		if i := bytes.LastIndexByte(head, '\n'); i >= 0 {
			head = head[:i+1]
		}
	}
	return string(head), total, lines, err
}

type reportShown struct {
	text         string
	files, lines int
}

// boundReportDiff cuts the diff at the file, line and byte bounds of FR-035 and
// names the first bound that applied.
func boundReportDiff(text string, maxBytes int) (reportShown, string) {
	var out reportShown
	var b strings.Builder
	cut := ""
	for rest := text; rest != ""; {
		line := rest
		if i := strings.IndexByte(rest, '\n'); i >= 0 {
			line = rest[:i+1]
		}
		rest = rest[len(line):]
		if strings.HasPrefix(line, "diff --git ") {
			if out.files == reportMaxFiles {
				cut = "files"
				break
			}
			out.files++
		}
		if out.lines == reportMaxLines {
			cut = "lines"
			break
		}
		if b.Len()+len(line) > maxBytes {
			cut = "bytes"
			break
		}
		b.WriteString(line)
		out.lines++
	}
	out.text = b.String()
	return out, cut
}

// parseReportHunks structures the bounded diff text: one entry per hunk, tied
// to the file of the nth `diff --git` chunk (the order of the raw listing).
func parseReportHunks(text string, files []V3ReportFile) []V3ReportHunk {
	hunks := []V3ReportHunk{}
	index := -1
	open := false
	for _, line := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			index++
			open = false
		case strings.HasPrefix(line, "@@ "):
			m := reportHunkHeader.FindStringSubmatch(line)
			if m == nil || index < 0 || index >= len(files) {
				open = false
				continue
			}
			h := V3ReportHunk{File: files[index].Path, OldStart: atoiDefault(m[1], 0), OldLines: atoiDefault(m[2], 1), NewStart: atoiDefault(m[3], 0), NewLines: atoiDefault(m[4], 1), Lines: []V3Untrusted{}}
			hunks = append(hunks, h)
			open = true
		case open && line != "" && strings.ContainsRune(" +-\\", rune(line[0])):
			h := &hunks[len(hunks)-1]
			h.Lines = append(h.Lines, untrusted(line))
		}
	}
	return hunks
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

// reportCandidates lists up to five base commits that look like the branch's
// work: the same patch-id first, then the same subject.
func (r *integrationRun) reportCandidates(ctx context.Context, e *V3ReportEntry, q reportRequest, mergeBase string) error {
	subjects := map[string]bool{}
	for _, c := range e.Commits {
		subjects[c.Subject.Text] = true
	}
	if len(q.Verdict.BaseMatches) == 0 && len(subjects) == 0 {
		return nil
	}
	rows, err := r.lines(ctx, integrationCommand{args: []string{"log", "--no-merges", "--format=%H%x00%s", mergeBase + ".." + q.Base, "--"}}, specReportScan)
	if err != nil {
		return err
	}
	subjectOf := map[string]string{}
	var order []string
	for _, row := range rows {
		oid, subject, _ := strings.Cut(row, "\x00")
		subjectOf[oid] = subject
		order = append(order, oid)
	}
	seen := map[string]bool{}
	add := func(oid, match string) {
		if len(e.Candidates) < reportMaxCandidates && !seen[oid] {
			seen[oid] = true
			e.Candidates = append(e.Candidates, V3ReportCandidate{OID: oid, Subject: untrusted(clipText(subjectOf[oid], reportSubjectBytes)), Match: match})
		}
	}
	for _, oid := range q.Verdict.BaseMatches {
		if _, ok := subjectOf[oid]; ok {
			add(oid, "patch-id")
		}
	}
	for _, oid := range order {
		if subjects[untrusted(clipText(subjectOf[oid], reportSubjectBytes)).Text] {
			add(oid, "subject")
		}
	}
	return nil
}

// reportClassOrder is the order in which the classes run (FR-007).
var reportClassOrder = []string{integrationAncestor, integrationNoNetChange, integrationPatchID, integrationMergeNoop}

// reasonClasses lists the proof classes each reason belongs to (reason table).
var reasonClasses = map[string][]string{
	"whitespace-differs": {integrationPatchID}, "partial-match": {integrationPatchID}, "multiset-mismatch": {integrationPatchID},
	"merge-commits-in-range": {integrationPatchID}, "diff-attribute": {integrationPatchID},
	"ambiguous-merge-base": {integrationPatchID, integrationMergeNoop, integrationNoNetChange},
	"gitlink-change":       {integrationMergeNoop}, "would-add-content": {integrationMergeNoop}, "conflict": {integrationMergeNoop},
	"merge-attribute": {integrationMergeNoop}, "merge-driver": {integrationMergeNoop},
	"attribute-source":       {integrationPatchID, integrationMergeNoop},
	"proof-limit":            {integrationPatchID, integrationMergeNoop, integrationNoNetChange},
	"proof-budget-exhausted": {integrationPatchID, integrationMergeNoop, integrationNoNetChange},
	"object-dir-unavailable": {integrationPatchID, integrationMergeNoop, integrationNoNetChange},
}

// reportProofs says, per selected class, that it did not prove the tip and why.
func reportProofs(policy V3ProofPolicy, reasons []string, v integrationVerdict) []V3ReportProof {
	out := []V3ReportProof{}
	for _, class := range reportClassOrder {
		if !policy.has(class) {
			continue
		}
		var why []string
		for _, reason := range reasons {
			if slices.Contains(reasonClasses[reason], class) {
				why = append(why, reason)
			}
		}
		text := strings.Join(why, ", ")
		switch class {
		case integrationAncestor:
			text = "the tip is not reachable from the base"
		case integrationNoNetChange:
			if text == "" {
				text = "the tip tree differs from the merge base tree"
			}
		case integrationPatchID:
			counts := fmt.Sprintf("matched %d, unmatched %d, empty %d, merge commits %d", v.Matched, v.Unmatched, v.Empty, v.SkippedMerges)
			if text != "" {
				text = counts + "; " + text
			} else {
				text = counts
			}
		case integrationMergeNoop:
			if text == "" {
				text = "the merge result differs from the base"
			}
		}
		out = append(out, V3ReportProof{Class: class, Outcome: "not-proven", Why: text})
	}
	return out
}
