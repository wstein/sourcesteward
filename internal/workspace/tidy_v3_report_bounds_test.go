// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

// Spec 031 T029: the bounds of the report data, each at the bound (accepted)
// and one over (refused or marked), for SHA-1 and SHA-256 widths where the
// grammar has a width.

func TestReportCommandBoundsAtAndOverTheBound(t *testing.T) {
	date := time.Unix(0, 0).UTC().Format(time.RFC3339)
	for _, width := range []int{40, 64} {
		hex := func(c byte) string { return strings.Repeat(string(c), width) }
		type row struct {
			spec   integrationSpec
			record func(pad int) string // a record padded by pad bytes
			bad    string
			cap    int // the longest record
		}
		rows := []row{
			{specReportLog, func(pad int) string { return hex('a') + "\x00" + strings.Repeat("s", pad) + "\x00an\x00" + date }, hex('a') + "\x00s\x00an", reportRecordBytes - 1},
			{specReportScan, func(pad int) string { return hex('a') + "\x00" + strings.Repeat("s", pad) }, "xyz\x00s", reportRecordBytes - 1},
			{specReportBlobSizes, func(pad int) string { return hex('a') + " blob 12" }, hex('a') + " tree 12", 0},
		}
		for _, r := range rows {
			t.Run(fmt.Sprintf("%s/%d", r.spec.name, width), func(t *testing.T) {
				stream := func(n int, pad int, extra string) io.Reader {
					var b strings.Builder
					for i := 0; i < n; i++ {
						b.WriteString(r.record(pad))
						b.WriteByte(r.spec.sep)
					}
					b.WriteString(extra)
					return strings.NewReader(b.String())
				}
				// The padding at which a record has exactly the longest size.
				pad := 1
				if r.cap > 0 {
					pad = r.cap - len(r.record(0))
				}
				count := 0
				accept := func([]byte) error { count++; return nil }
				if err := readIntegrationRecords(stream(r.spec.maxRecords, pad, ""), r.spec, width, accept); err != nil || count != r.spec.maxRecords {
					t.Fatalf("at the bound: %v after %d records", err, count)
				}
				if err := readIntegrationRecords(stream(r.spec.maxRecords+1, 1, ""), r.spec, width, accept); err != errIntegrationLimit {
					t.Fatalf("one record over: %v", err)
				}
				if r.cap > 0 {
					if err := readIntegrationRecords(stream(1, pad+1, ""), r.spec, width, accept); err != errIntegrationLimit {
						t.Fatalf("one byte over the record bound: %v", err)
					}
				}
				if err := readIntegrationRecords(stream(0, 0, r.bad+string(r.spec.sep)), r.spec, width, accept); err != errIntegrationLimit {
					t.Fatalf("malformed record: %v", err)
				}
			})
		}
		t.Run(fmt.Sprintf("raw and merge-tree/%d", width), func(t *testing.T) {
			record := func(path string) string {
				return ":100644 100755 " + hex('a') + " " + hex('b') + " M\x00" + path + "\x00"
			}
			stream := func(n int, path string) io.Reader {
				return strings.NewReader(strings.Repeat(record(path), n))
			}
			if got, err := parseReportRaw(stream(reportRawRecords, "p"), width); err != nil || len(got) != reportRawRecords {
				t.Fatalf("raw at the bound: %d %v", len(got), err)
			}
			if _, err := parseReportRaw(stream(reportRawRecords+1, "p"), width); err != errIntegrationLimit {
				t.Fatalf("raw one over: %v", err)
			}
			if _, err := parseReportRaw(stream(1, strings.Repeat("p", integrationPathLimit+1)), width); err != errIntegrationLimit {
				t.Fatalf("raw path one over: %v", err)
			}
			if _, err := parseReportRaw(strings.NewReader(":100644 100755 "+hex('a')+" "+hex('b')+"\x00p\x00"), width); err != errIntegrationLimit {
				t.Fatalf("raw malformed: %v", err)
			}
			names := func(n int) string {
				var b strings.Builder
				b.WriteString(hex('c') + "\x00")
				for i := 0; i < n; i++ {
					fmt.Fprintf(&b, "path-%d\x00", i)
				}
				return b.String()
			}
			if tree, got, err := parseReportMergeTree(strings.NewReader(names(reportRawRecords)), width); err != nil || len(got) != reportRawRecords || tree != hex('c') {
				t.Fatalf("merge-tree at the bound: %d %v", len(got), err)
			}
			if _, _, err := parseReportMergeTree(strings.NewReader(names(reportRawRecords+1)), width); err != errIntegrationLimit {
				t.Fatalf("merge-tree one over: %v", err)
			}
			if _, _, err := parseReportMergeTree(strings.NewReader(hex('c')[1:]+"\x00"), width); err != errIntegrationLimit {
				t.Fatalf("merge-tree malformed tree: %v", err)
			}
		})
	}
}

// diffLength is the exact size of the plumbing diff the report truncates.
func (f *proofFix) diffLength(a, b string) (bytes, lines int) {
	f.t.Helper()
	out := f.git("diff-tree", "-p", "--no-ext-diff", "--no-textconv", "--no-renames", a, b) + "\n"
	return len(out), strings.Count(out, "\n")
}

func bigFixture(t *testing.T, files map[string]string) (*proofFix, string) {
	f := newProofFix(t)
	m0 := f.rev("HEAD")
	f.branchFrom("big", m0, files, "big change")
	f.commit("main moves", map[string]string{"other.txt": "o\n"})
	return f, m0
}

func TestReportDiffTruncationMarkersCarryExactCounts(t *testing.T) {
	t.Run("files", func(t *testing.T) {
		files := map[string]string{}
		for i := 0; i < 20; i++ {
			files[fmt.Sprintf("f%02d.txt", i)] = "x\n"
		}
		f, _ := bigFixture(t, files)
		e, _ := f.reportEntry(f.main, "big", true, nil)
		d := e.Diff
		if d.Truncated == nil || *d.Truncated != (V3ReportTruncation{Unit: "files", Shown: 16, Total: 20}) || len(d.Files) != 16 || d.Stat.Files != 20 {
			t.Fatalf("diff %+v files %d", d.Truncated, len(d.Files))
		}
		if strings.Count(e.rawDiff, "diff --git ") != 16 {
			t.Errorf("rendered %d files", strings.Count(e.rawDiff, "diff --git "))
		}
	})
	t.Run("lines", func(t *testing.T) {
		f, _ := bigFixture(t, map[string]string{"long.txt": strings.Repeat("line\n", 1200)})
		e, _ := f.reportEntry(f.main, "big", true, nil)
		result := strings.Fields(f.git("merge-tree", "--write-tree", "--no-messages", f.main, "big"))[0]
		_, lines := f.diffLength(f.treeOf(f.main), result)
		if e.Diff.Truncated == nil || *e.Diff.Truncated != (V3ReportTruncation{Unit: "lines", Shown: 1000, Total: lines}) || strings.Count(e.rawDiff, "\n") != 1000 {
			t.Fatalf("truncated %+v want 1000 of %d, rendered %d", e.Diff.Truncated, lines, strings.Count(e.rawDiff, "\n"))
		}
	})
	t.Run("bytes", func(t *testing.T) {
		f, _ := bigFixture(t, map[string]string{"wide.txt": strings.Repeat(strings.Repeat("w", 200)+"\n", 40)})
		bounds := defaultReportBounds()
		bounds.MaxDiffBytes = 2048
		e, _ := f.reportEntryBounded(f.main, "big", true, nil, bounds)
		result := strings.Fields(f.git("merge-tree", "--write-tree", "--no-messages", f.main, "big"))[0]
		total, _ := f.diffLength(f.treeOf(f.main), result)
		tr := e.Diff.Truncated
		if tr == nil || tr.Unit != "bytes" || tr.Total != total || tr.Shown != len(e.rawDiff) || tr.Shown > 2048 || tr.Shown < 2048-201 {
			t.Fatalf("truncated %+v total %d rendered %d", tr, total, len(e.rawDiff))
		}
	})
	t.Run("more than the retained megabyte", func(t *testing.T) {
		long := func(c string) string { return strings.Repeat(c, 600000) + "\n" }
		f, _ := bigFixture(t, map[string]string{"huge.txt": long("a") + long("b") + long("c")})
		bounds := defaultReportBounds()
		bounds.MaxDiffBytes = reportDiffBytesCeiling
		e, _ := f.reportEntryBounded(f.main, "big", true, nil, bounds)
		result := strings.Fields(f.git("merge-tree", "--write-tree", "--no-messages", f.main, "big"))[0]
		total, _ := f.diffLength(f.treeOf(f.main), result)
		tr := e.Diff.Truncated
		if tr == nil || tr.Unit != "bytes" || tr.Total != total || tr.Shown != len(e.rawDiff) || tr.Shown > reportDiffRetain {
			t.Fatalf("truncated %+v total %d rendered %d", tr, total, len(e.rawDiff))
		}
	})
}
