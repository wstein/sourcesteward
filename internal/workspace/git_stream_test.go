// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The measured ordinary repository has 695 leaves and 67,877 bytes of native
// tree metadata. Keep those dimensions exact, independently of parser constants.
func fixtureGitLargeMetadata(t *testing.T) string {
	t.Helper()
	d := fixtureGit(t)
	for i := 0; i < 694; i++ {
		n := 35
		if i < 493 {
			n++
		}
		name := strings.Repeat("x", n-5) + fmt.Sprintf("-%04d", i)
		if e := os.WriteFile(filepath.Join(d, name), []byte("x"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	gitFixture(t, d, "add", ".")
	gitFixture(t, d, "commit", "-qm", "ordinary metadata above old capture limit")
	raw := gitFixture(t, d, "ls-tree", "-r", "-l", "-z", "--full-tree", "HEAD")
	// gitFixture trims surrounding whitespace, but not the terminal NUL.
	if len(raw) != 67877 || strings.Count(raw, "\x00") != 695 {
		t.Fatalf("fixture shape: %d bytes / %d leaves", len(raw), strings.Count(raw, "\x00"))
	}
	return d
}

func TestGitCompleteMetadataAdmitsMeasuredOrdinaryBase(t *testing.T) {
	d := fixtureGitLargeMetadata(t)
	if _, e := (gitAdapter{}).Observe(context.Background(), d, "HEAD"); e != nil {
		t.Fatalf("complete 695-entry/67877-byte tree refused: %v", e)
	}
}

func TestGitTreeStreamCompleteBoundaries(t *testing.T) {
	oid := strings.Repeat("a", 40)
	row := func(path string, size int64) string {
		return fmt.Sprintf("100644 blob %s %7d\t%s\x00", oid, size, path)
	}
	parse := func(raw string) error {
		return parseGitTreeStream(strings.NewReader(raw), "sha1", newGitTreeValidator(), func([]byte, string) {})
	}
	for _, tc := range []struct {
		name, raw string
		ok        bool
	}{
		{"empty", "", true},
		{"path-exact", row(strings.Repeat("x", 4096), 0), true},
		{"path-over", row(strings.Repeat("x", 4097), 0), false},
		{"blob-exact", row("x", 16*1024*1024), true},
		{"blob-over", row("x", 16*1024*1024+1), false},
		{"missing-delimiter", strings.TrimSuffix(row("x", 0), "\x00"), false},
		{"final-unsafe", row("a", 0) + row("../final", 0), false},
		{"final-invalid-oid", row("a", 0) + strings.Replace(row("b", 0), oid, "missing", 1), false},
		{"final-alias", row("Case/a", 0) + row("case/b", 0), false},
		{"binary-path", row("a\xff", 0), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := parse(tc.raw)
			if (e == nil) != tc.ok {
				t.Fatalf("accepted=%v want=%v error=%v", e == nil, tc.ok, e)
			}
		})
	}
	var records strings.Builder
	for i := 0; i < checkoutLeafLimit; i++ {
		records.WriteString(row(fmt.Sprint(i), 0))
	}
	if e := parse(records.String()); e != nil {
		t.Fatal(e)
	}
	if e := parse(records.String() + row("last", 0)); e == nil {
		t.Fatalf("%d leaves admitted", checkoutLeafLimit+1)
	}
	var aggregate strings.Builder
	for i := 0; i < 4; i++ {
		aggregate.WriteString(row(fmt.Sprint(i), 16*1024*1024))
	}
	if e := parse(aggregate.String()); e != nil {
		t.Fatal(e)
	}
	if e := parse(aggregate.String() + row("extra", 1)); e == nil {
		t.Fatal("aggregate overrun admitted")
	}
}

func TestGitMetadataRecordAndOutputExactBounds(t *testing.T) {
	for _, n := range []int{4224, 4225} {
		r := newGitMetadataReader(strings.NewReader(strings.Repeat("x", n-1) + "\x00"))
		_, e := readGitNULRecord(r, 4224)
		if (e == nil) != (n == 4224) {
			t.Fatalf("record %d: %v", n, e)
		}
	}
	for _, n := range []int{4325376, 4325377} {
		counted := &nativeCountReader{r: strings.NewReader(strings.Repeat("x", n)), limit: 4325376}
		_, e := io.Copy(io.Discard, counted)
		if (e == nil) != (n == 4325376) || counted.count != int64(n) {
			t.Fatalf("stdout %d consumed=%d error=%v", n, counted.count, e)
		}
	}
}

func TestGitFilterStreamRequiresEveryOrderedTriple(t *testing.T) {
	paths := []string{"a", "last\nfile"}
	good := "a\x00filter\x00unspecified\x00last\nfile\x00filter\x00unset\x00"
	for _, raw := range []string{good, good[:len(good)-1], good + "extra\x00", strings.Replace(good, "unset", "active", 1), strings.Replace(good, "last\nfile", "different", 1), strings.Replace(good, "filter", "other", 1)} {
		h := sha256.New()
		e := parseGitFilterStream(strings.NewReader(raw), paths, h)
		if raw == good {
			if e != nil || hex.EncodeToString(h.Sum(nil)) != gitDigest(good) {
				t.Fatalf("complete evidence: %v", e)
			}
		} else if e == nil {
			t.Fatalf("incomplete/unsafe evidence admitted: %q", raw)
		}
	}
}

func TestGitCompleteMetadataPreservesSafetyDigestBytes(t *testing.T) {
	d := fixtureGitLargeMetadata(t)
	ctx := context.Background()
	id, e := gitNativeIdentity(ctx, d)
	if e != nil {
		t.Fatal(e)
	}
	bin, e := gitExecutable()
	if e != nil {
		t.Fatal(e)
	}
	config, layout, e := gitLayoutSafety(ctx, bin, id)
	if e != nil {
		t.Fatal(e)
	}
	hooks, _, e := gitHooksFingerprint(filepath.Join(id.RepositoryDir, "hooks"), false)
	if e != nil {
		t.Fatal(e)
	}
	tree := gitFixture(t, d, "ls-tree", "-r", "-l", "-z", "--full-tree", id.CommitID)
	var args = []string{"check-attr", "--source=" + id.CommitID, "-z", "filter", "--"}
	for _, record := range strings.Split(strings.TrimSuffix(tree, "\x00"), "\x00") {
		_, path, _ := strings.Cut(record, "\t")
		args = append(args, path)
	}
	attrs := gitFixture(t, d, args...)
	info, e := os.Stat(bin)
	if e != nil {
		t.Fatal(e)
	}
	want := gitDigest("layout-v1\x00" + layout + "\x00" + config + "\x00" + hooks + "\x00" + tree + "\x00" + attrs + "\x00" + bin + "\x00" + fmt.Sprintf("%d:%d:%d", info.Mode(), info.Size(), info.ModTime().UnixNano()))
	got, e := gitSafety(ctx, bin, id, id.CommitID)
	if e != nil || got != want {
		t.Fatalf("complete digest changed ordering/coverage: %v got=%s want=%s", e, got, want)
	}
}

func TestGitFilterLogicalRecordStopsAtDetectionByte(t *testing.T) {
	path := strings.Repeat("p", 4096)
	raw := path + "\x00filter\x00" + strings.Repeat("x", 10000) + "\x00"
	counted := &nativeCountReader{r: strings.NewReader(raw), limit: gitMetadataStreamLimit}
	e := parseGitFilterStream(counted, []string{path}, sha256.New())
	if e == nil {
		t.Fatal("overlong logical record accepted")
	}
	t.Logf("logical cap=%d detection bound=%d consumed=%d error=%v", gitMetadataRecordLimit, gitMetadataRecordLimit+1, counted.count, e)
	if counted.count > gitMetadataRecordLimit+1 {
		t.Fatalf("logical record consumed %d bytes beyond detection bound %d", counted.count, gitMetadataRecordLimit+1)
	}
}

func TestGitLogicalRecordBudgetSurvivesBufferedPrefixes(t *testing.T) {
	for _, n := range []int{1, 128, 1023} {
		var prefix strings.Builder
		paths := []string{}
		for i := 0; i < n; i++ {
			path := fmt.Sprint(i)
			paths = append(paths, path)
			prefix.WriteString(path + "\x00filter\x00unset\x00")
		}
		path := strings.Repeat("p", 4096)
		paths = append(paths, path)
		raw := prefix.String() + path + "\x00filter\x00" + strings.Repeat("x", 10000) + "\x00"
		counted := &nativeCountReader{r: strings.NewReader(raw), limit: gitMetadataStreamLimit}
		if e := parseGitFilterStream(counted, paths, sha256.New()); e == nil {
			t.Fatal("overlong final triple admitted")
		}
		bound := int64(prefix.Len() + gitMetadataRecordLimit + 1)
		if counted.count > bound {
			t.Fatalf("prefix triples=%d consumed=%d bound=%d", n, counted.count, bound)
		}
		// Legal short triples must survive multiple buffer refills and budget resets.
		if e := parseGitFilterStream(strings.NewReader(prefix.String()), paths[:len(paths)-1], sha256.New()); e != nil {
			t.Fatalf("valid prefix triples=%d: %v", n, e)
		}
	}
}

func TestGitTreeLogicalRecordStopsAfterBufferedPrefix(t *testing.T) {
	oid := strings.Repeat("a", 40)
	prefix := "100644 blob " + oid + "       0\tfirst\x00"
	raw := prefix + strings.Repeat("x", 10000) + "\x00"
	counted := &nativeCountReader{r: strings.NewReader(raw), limit: gitMetadataStreamLimit}
	e := parseGitTreeStream(counted, "sha1", newGitTreeValidator(), func([]byte, string) {})
	if e == nil {
		t.Fatal("overlong final tree record admitted")
	}
	bound := int64(len(prefix) + gitMetadataRecordLimit + 1)
	if counted.count > bound {
		t.Fatalf("tree logical record consumed=%d bound=%d", counted.count, bound)
	}
}
