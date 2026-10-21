// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// Markdown and JSON renderings of one report data set (FR-033, FR-034). Both
// are built from the same entries; an entry is in both files or in neither, so a
// reviewer of either file has seen the same evidence.

// reportDocument is the data set both renderings are built from.
type reportDocument struct {
	Base    V3ReportBase
	Bounds  reportBounds
	Entries []V3ReportEntry   // needs-review entries in reference order
	Omitted []V3ReportOmitted // needs-review references not collected (entry bound)
	Counts  V3ReportSummary   // buckets, reasons and retained references
}

type renderedReport struct {
	Markdown, JSON []byte
	Shown          []string          // branch names of the entries in the files
	Omitted        []V3ReportOmitted // everything left out by a bound, by name
	// Truncated names the shown entries a bound cut: a reviewer did not see all
	// of them (FR-039).
	Truncated map[string]bool
}

type reportJSON struct {
	SchemaVersion    int               `json:"schema_version"`
	Summary          V3ReportSummary   `json:"summary"`
	Base             V3ReportBase      `json:"base"`
	Entries          []json.RawMessage `json:"entries"`
	OmittedEntries   []V3ReportOmitted `json:"omitted_entries"`
	ReviewerGuidance V3ReportGuidance  `json:"reviewer_guidance"`
}

func marshalReport(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func omittedOf(e V3ReportEntry) V3ReportOmitted {
	return V3ReportOmitted{Branch: e.Branch, Reasons: append([]string{}, e.Reasons...)}
}

// renderReport renders both files within the byte bound. Entries that do not fit
// either file are omitted whole and listed by name and reasons.
func renderReport(d reportDocument) (renderedReport, error) {
	if d.Counts.Reasons == nil {
		d.Counts.Reasons = map[string]int{}
	}
	if d.Counts.RetainedRefs == nil {
		d.Counts.RetainedRefs = []V3ReportRetained{}
	}
	const worstCount = 999999
	// The skeleton with every entry omitted and the widest counts bounds the
	// size of everything but the entries.
	all := append([]V3ReportOmitted{}, d.Omitted...)
	for _, e := range d.Entries {
		all = append(all, omittedOf(e))
	}
	worst := d.Counts
	worst.Entries, worst.Omitted = worstCount, worstCount
	jsonSkeleton, err := marshalReport(reportJSON{SchemaVersion: reportSchemaVersion, Summary: worst, Base: d.Base, Entries: []json.RawMessage{}, OmittedEntries: all, ReviewerGuidance: reportGuidance()})
	if err != nil {
		return renderedReport{}, err
	}
	mdSkeleton := renderMarkdown(d, worst, nil, all)
	jsonUsed, mdUsed := len(jsonSkeleton), len(mdSkeleton)
	var pieces []json.RawMessage
	var mdPieces []string
	var shown []string
	cut := map[string]bool{}
	var omitted []V3ReportOmitted
	omitted = append(omitted, d.Omitted...)
	for i, e := range d.Entries {
		piece, err := marshalReport(e)
		if err != nil {
			return renderedReport{}, err
		}
		piece = bytes.TrimRight(piece, "\n")
		mdPiece := renderEntryMarkdown(len(pieces)+1, e)
		if i >= d.Bounds.MaxEntries || jsonUsed+len(piece)+1 > d.Bounds.MaxBytes || mdUsed+len(mdPiece) > d.Bounds.MaxBytes {
			omitted = append(omitted, omittedOf(e))
			continue
		}
		jsonUsed, mdUsed = jsonUsed+len(piece)+1, mdUsed+len(mdPiece)
		pieces, mdPieces, shown = append(pieces, piece), append(mdPieces, mdPiece), append(shown, e.Branch.Text)
		cut[e.Branch.Text] = e.Diff.Truncated != nil || e.CommitsTruncated != nil || e.Diff.Source == "unavailable" || e.RangePairing != nil && e.RangePairing.Truncated != nil
	}
	sort.SliceStable(omitted, func(i, j int) bool { return omitted[i].Branch.Text < omitted[j].Branch.Text })
	summary := d.Counts
	summary.Entries, summary.Omitted = len(pieces), len(omitted)
	if pieces == nil {
		pieces = []json.RawMessage{}
	}
	js, err := marshalReport(reportJSON{SchemaVersion: reportSchemaVersion, Summary: summary, Base: d.Base, Entries: pieces, OmittedEntries: append([]V3ReportOmitted{}, omitted...), ReviewerGuidance: reportGuidance()})
	if err != nil {
		return renderedReport{}, err
	}
	md := []byte(renderMarkdown(d, summary, mdPieces, omitted))
	if len(js) > d.Bounds.MaxBytes || len(md) > d.Bounds.MaxBytes {
		return renderedReport{}, failure("report-too-large", "The review report does not fit the byte bound even without entries; raise --report-max-bytes.")
	}
	return renderedReport{Markdown: md, JSON: js, Shown: shown, Omitted: omitted, Truncated: cut}, nil
}

// fenceFor returns a backtick fence longer than any backtick run in content.
func fenceFor(content string) string {
	longest, run := 0, 0
	for _, c := range content {
		if c == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return strings.Repeat("`", max(3, longest+1))
}

// untrustedBlock is the only place repository text enters the Markdown.
func untrustedBlock(content string) string {
	content = escapeMarkdownText(content)
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	fence := fenceFor(content)
	return fence + "untrusted\n" + content + fence + "\n"
}

// escapeMarkdownText keeps newlines and tabs and writes every other control or
// format character (terminal escapes, bidirectional marks) as \u{XXXX}.
func escapeMarkdownText(s string) string {
	var b strings.Builder
	for _, r := range strings.ToValidUTF8(s, "�") {
		switch {
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r):
			fmt.Fprintf(&b, "\\u{%04X}", r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// visibleWhitespace shows tab as →, trailing space as · and carriage return as ␍.
func visibleWhitespace(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		end := len(line)
		for end > 0 && (line[end-1] == ' ' || line[end-1] == '\r') {
			end--
		}
		head, tail := line[:end], line[end:]
		head = strings.NewReplacer("\t", "→", "\r", "␍").Replace(head)
		tail = strings.NewReplacer(" ", "·", "\r", "␍").Replace(tail)
		lines[i] = head + tail
	}
	return strings.Join(lines, "\n")
}

func short(oid string) string { return oid[:min(12, len(oid))] }

func renderMarkdown(d reportDocument, s V3ReportSummary, entries []string, omitted []V3ReportOmitted) string {
	var b strings.Builder
	b.WriteString("# Tidy review report\n\n> " + reviewerGuidance + "\n\n")
	b.WriteString(renderGuidanceMarkdown(reportGuidance()))
	fmt.Fprintf(&b, "Schema version: %d\n\nBase OID: `%s`\n\nBase selector:\n\n%s\n", reportSchemaVersion, d.Base.OID, untrustedBlock(d.Base.Selector))
	b.WriteString("## Summary\n\n")
	fmt.Fprintf(&b, "- Entries shown: %d\n- Omitted entries: %d\n- Proven: %d\n- Needs review: %d\n- Retained: %d\n", s.Entries, s.Omitted, s.Proven, s.NeedsReview, s.Retained)
	names := make([]string, 0, len(s.Reasons))
	for name := range s.Reasons {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) > 0 {
		b.WriteString("\nReasons:\n\n")
		for _, name := range names {
			fmt.Fprintf(&b, "- `%s`: %d\n", name, s.Reasons[name])
		}
	}
	if len(s.RetainedRefs) > 0 {
		fmt.Fprintf(&b, "\n## Retained references (%d)\n\nNo decision can clear a retained reference.\n\n", len(s.RetainedRefs))
		var block strings.Builder
		for _, r := range s.RetainedRefs {
			fmt.Fprintf(&block, "%s  %s  clearable: false\n", r.Reason, r.Branch.Text)
		}
		b.WriteString(untrustedBlock(block.String()))
	}
	for _, piece := range entries {
		b.WriteString(piece)
	}
	if len(omitted) > 0 {
		fmt.Fprintf(&b, "\n## Omitted entries (%d)\n\nNamed here only: a bound left them out of this report, so no decision can clear them.\n\n", len(omitted))
		var block strings.Builder
		for _, o := range omitted {
			fmt.Fprintf(&block, "%s  %s\n", o.Branch.Text, strings.Join(o.Reasons, ", "))
		}
		b.WriteString(untrustedBlock(block.String()))
	}
	return b.String()
}

func renderEntryMarkdown(index int, e V3ReportEntry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n## Entry %d\n\n", index)
	mergeBase := "`" + e.MergeBaseOID + "`"
	if e.MergeBaseOID == "" {
		mergeBase = "none (no single merge base)"
	}
	fmt.Fprintf(&b, "- Tip: `%s`\n- Base: `%s`\n- Merge base: %s\n- Reasons: %s\n- Suggested verdict: `%s`, confidence %s (basis %s)\n\nBranch:\n\n%s\n", e.TipOID, e.BaseOID, mergeBase, backticked(e.Reasons), e.Verdict.Verdict, e.Verdict.Confidence, backticked(e.Verdict.Basis), untrustedBlock(e.Branch.Text))
	if l := e.ProofLimit; l != nil {
		fmt.Fprintf(&b, "- Proof limit: `%s` %s, limit %d, observed %d\n", l.Name, l.Unit, l.Limit, l.Observed)
	}
	total := len(e.Commits)
	if e.CommitsTruncated != nil {
		total = e.CommitsTruncated.Total
	}
	fmt.Fprintf(&b, "### Commits (%d of %d)\n\n", len(e.Commits), total)
	var commits strings.Builder
	for _, c := range e.Commits {
		fmt.Fprintf(&commits, "%s %s (%s, %s)\n", short(c.OID), c.Subject.Text, c.Author.Text, c.Date)
	}
	b.WriteString(untrustedBlock(commits.String()))
	if e.CommitsTruncated != nil {
		fmt.Fprintf(&b, "\n... %d more commits\n", e.CommitsTruncated.Total-e.CommitsTruncated.Shown)
	}
	b.WriteString("\n### Proofs tried\n\n")
	for _, p := range e.Proofs {
		fmt.Fprintf(&b, "- %s: %s (%s)\n", p.Class, p.Outcome, p.Why)
	}
	b.WriteString("\n### What the base does not yet contain\n\n")
	d := e.Diff
	switch d.Source {
	case "merge-result":
		b.WriteString("The merge result of the base and the branch, diffed against the base.\n\n")
	case "branch-against-merge-base":
		b.WriteString("Branch against merge base: the merge gate applies, so merge-tree was not run and the suggested verdict is unclear.\n\n")
	default:
		b.WriteString("Not available: " + d.UnavailableReason + "\n\n")
	}
	if len(d.ConflictedPaths) > 0 {
		b.WriteString("Conflicted paths:\n\n")
		var paths strings.Builder
		for _, p := range d.ConflictedPaths {
			paths.WriteString(p.Text + "\n")
		}
		b.WriteString(untrustedBlock(paths.String()) + "\n")
	}
	if len(d.Files) > 0 {
		b.WriteString("Changed paths with file type and mode:\n\n")
		var files strings.Builder
		for _, f := range d.Files {
			files.WriteString(describeReportFile(f) + "\n")
		}
		for _, bin := range d.BinaryFiles {
			fmt.Fprintf(&files, "binary file changed: %s  old %s (%d bytes) new %s (%d bytes)\n", bin.Path.Text, orNone(bin.OldBlob), bin.OldSize, orNone(bin.NewBlob), bin.NewSize)
		}
		b.WriteString(untrustedBlock(files.String()) + "\n")
	}
	if d.WhitespaceOnly {
		b.WriteString("whitespace-only difference (tab as →, trailing space as ·, carriage return as ␍)\n\n")
	}
	text := redactCredentials(e.rawDiff)
	if d.WhitespaceOnly {
		text = visibleWhitespace(text)
	}
	if text != "" {
		b.WriteString(untrustedBlock(text))
	}
	if d.Truncated != nil {
		fmt.Fprintf(&b, "\ntruncated: shown %d of %d files, %d of %d lines", d.shownFiles, d.totalFiles, d.shownLines, d.totalLines)
		if d.Truncated.Unit == "bytes" {
			fmt.Fprintf(&b, ", %d of %d bytes", d.Truncated.Shown, d.Truncated.Total)
		}
		b.WriteString("\n")
	}
	if len(e.Candidates) > 0 {
		b.WriteString("\n### Candidate base commits\n\n")
		var cands strings.Builder
		for _, c := range e.Candidates {
			fmt.Fprintf(&cands, "%s %s %s\n", c.Match, short(c.OID), c.Subject.Text)
		}
		b.WriteString(untrustedBlock(cands.String()))
	}
	if p := e.RangePairing; p != nil {
		b.WriteString("\n### Range pairing\n\nEvidence `" + p.Evidence + "` only: it proves nothing and is never a deletion proof.\n\nRange:\n\n" + untrustedBlock(p.Range))
		if p.Note != "" {
			b.WriteString("\n" + p.Note + "\n")
		} else {
			var rows strings.Builder
			for _, pair := range p.Pairs {
				fmt.Fprintf(&rows, "%s  %s  %s\n", pair.Status, orNone(pair.BranchCommit), orNone(pair.RangeCommit))
			}
			b.WriteString("\nRows (status, branch commit, range commit):\n\n" + untrustedBlock(rows.String()))
			if p.Truncated != nil {
				fmt.Fprintf(&b, "\ntruncated: shown %d of %d rows\n", p.Truncated.Shown, p.Truncated.Total)
			}
		}
	}
	b.WriteString("\nAnswer Q1..Q4 and decide as the reviewer guidance in the report header says.\n")
	return b.String()
}

// renderGuidanceMarkdown is the reviewer guidance, written once in the header.
func renderGuidanceMarkdown(g V3ReportGuidance) string {
	var b strings.Builder
	b.WriteString("## Reviewer guidance\n\n")
	for _, q := range g.Questions {
		fmt.Fprintf(&b, "- %s: %s\n", q.ID, q.Text)
	}
	b.WriteString("\nDecisions and the evidence each needs:\n\n")
	for _, d := range g.Decisions {
		fmt.Fprintf(&b, "- `%s`: %s\n", d.Decision, d.Evidence)
	}
	b.WriteString("\n" + g.ReadOnlyGit + "\n\n")
	return b.String()
}

func orNone(oid string) string {
	if oid == "" {
		return "none"
	}
	return oid
}

func backticked(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = "`" + v + "`"
	}
	return strings.Join(out, ", ")
}

// describeReportFile is one line per changed path: kind, type and old to new mode.
func describeReportFile(f V3ReportFile) string {
	switch f.Kind {
	case "mode-only":
		return fmt.Sprintf("mode only  %s -> %s (%s)  %s", f.OldMode, f.NewMode, f.TypeChange, f.Path.Text)
	case "gitlink":
		return fmt.Sprintf("gitlink: submodule pointer %s -> %s  %s", orNone(f.OldOID), orNone(f.NewOID), f.Path.Text)
	}
	line := fmt.Sprintf("%s  %s -> %s", f.Kind, f.OldMode, f.NewMode)
	if f.TypeChange != "" {
		line += " (" + f.TypeChange + ")"
	}
	if f.Kind == "symlink" || f.TypeChange != "" {
		line += "  type change"
	}
	return line + "  " + f.Path.Text
}
