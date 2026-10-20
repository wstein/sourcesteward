// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Helper process: stw-collection-helper <kind> <records> <recordBytes> <extraStdout> <stderrBytes>.
func TestGitCollectionHelper(t *testing.T) {
	for i, arg := range os.Args {
		if arg != "stw-collection-helper" {
			continue
		}
		a := os.Args[i+1:]
		records, _ := strconv.Atoi(a[1])
		size, _ := strconv.Atoi(a[2])
		extra, _ := strconv.Atoi(a[3])
		stderr, _ := strconv.Atoi(a[4])
		os.Stdout.WriteString(string(gitCollectionRecords(a[0] == "block", records, size)) + strings.Repeat("x", extra))
		os.Stderr.WriteString(strings.Repeat("e", stderr))
		os.Exit(0)
	}
}

func gitCollectionRecords(block bool, records, size int) []byte {
	var out bytes.Buffer
	for i := 0; i < records; i++ {
		if block {
			out.WriteString(strings.Repeat("a", size-2) + "\x00\x00")
		} else {
			out.WriteString(strings.Repeat("a", size-1) + "\n")
		}
	}
	return out.Bytes()
}

func collectionCommand(t *testing.T, ctx context.Context, kind string, records, size, extra, stderr int) *exec.Cmd {
	t.Helper()
	bin, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	if bin, e = filepath.EvalSymlinks(bin); e != nil {
		t.Fatal(e)
	}
	args := []string{"-test.run=^TestGitCollectionHelper$", "--", "stw-collection-helper", kind, strconv.Itoa(records), strconv.Itoa(size), strconv.Itoa(extra), strconv.Itoa(stderr)}
	cmd, e := prepareSandboxed(ctx, bin, args, filepath.Dir(bin), nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	return cmd
}

func collectionCode(e error) string {
	var oe operationError
	if errors.As(e, &oe) {
		return oe.Code + ": " + oe.Message
	}
	if e == nil {
		return ""
	}
	return "other: " + e.Error()
}

func runCollection(t *testing.T, kind string, records, size, extra, stderr int) (int, error) {
	t.Helper()
	ctx, deadline := context.WithTimeout(context.Background(), 20*time.Second)
	defer deadline()
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	count := 0
	k := gitRecordLine
	if kind == "block" {
		k = gitRecordBlock
	}
	e := gitStreamRecords(ctx, cancel, collectionCommand(t, streamCtx, kind, records, size, extra, stderr), k, func([]byte) error { count++; return nil })
	return count, e
}

func TestGitCollectionRecordCountBoundary(t *testing.T) {
	for _, kind := range []string{"line", "block"} {
		if n, e := runCollection(t, kind, 1024, 64, 0, 0); e != nil || n != 1024 {
			t.Fatalf("%s: 1,024 records: n=%d %v", kind, n, e)
		}
		_, e := runCollection(t, kind, 1025, 64, 0, 0)
		if !strings.HasPrefix(collectionCode(e), "git-observation-limit:") {
			t.Fatalf("%s: 1,025 records: %s", kind, collectionCode(e))
		}
	}
}

func TestGitCollectionByteAndRecordBoundaries(t *testing.T) {
	for _, kind := range []string{"line", "block"} {
		// 1,024 records of 4,224 bytes are exactly 4,325,376 stdout bytes.
		if n, e := runCollection(t, kind, 1024, 4224, 0, 65536); e != nil || n != 1024 {
			t.Fatalf("%s: maximum stream: n=%d %v", kind, n, e)
		}
		for _, tc := range []struct {
			name                 string
			records, size, extra int
			stderr               int
			want                 string
		}{
			{"stdout 4,325,377", 1024, 4224, 1, 0, "git-output-limit: Git stdout exceeds 4325376 bytes"},
			{"stderr 65,537", 1, 64, 0, 65537, "git-output-limit: Git stderr exceeds 65536 bytes"},
			{"record 4,225", 1, 4225, 0, 0, "git-output-limit: a Git record exceeds 4224 bytes"},
		} {
			_, e := runCollection(t, kind, tc.records, tc.size, tc.extra, tc.stderr)
			if collectionCode(e) != tc.want {
				t.Fatalf("%s %s: got %q want %q", kind, tc.name, collectionCode(e), tc.want)
			}
		}
	}
}

func TestGitCollectionParserHoldsOneRecord(t *testing.T) {
	for _, block := range []bool{false, true} {
		kind := gitRecordLine
		if block {
			kind = gitRecordBlock
		}
		data := gitCollectionRecords(block, 1024, 4224)
		var sum int
		allocs := testing.AllocsPerRun(5, func() {
			sum = 0
			if e := parseGitRecords(bytes.NewReader(data), kind, func(r []byte) error { sum += len(r); return nil }); e != nil {
				t.Fatal(e)
			}
		})
		if sum != len(data) || allocs > 8 {
			t.Fatalf("block=%v consumed=%d allocations per parse=%v (must not grow with 1,024 records)", block, sum, allocs)
		}
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		parseGitRecords(bytes.NewReader(data), kind, func([]byte) error { return nil })
		runtime.ReadMemStats(&after)
		if after.TotalAlloc-before.TotalAlloc > 64<<10 {
			t.Fatalf("block=%v parser allocated %d bytes for a %d byte stream", block, after.TotalAlloc-before.TotalAlloc, len(data))
		}
	}
}

func TestGitCollectionTruncatedFinalRecordIsUnexpectedEOF(t *testing.T) {
	for _, tc := range []struct {
		kind gitRecordKind
		raw  string
	}{{gitRecordLine, "a\nb"}, {gitRecordBlock, "a\x00\x00b\x00"}, {gitRecordBlock, "a\x00"}} {
		if e := parseGitRecords(strings.NewReader(tc.raw), tc.kind, func([]byte) error { return nil }); !errors.Is(e, io.ErrUnexpectedEOF) {
			t.Fatalf("%q: %v", tc.raw, e)
		}
	}
}

// The incremental digest of every record equals the digest of the buffered text,
// so snapshot digests and stored plan identities are unchanged.
func TestGitObserveDigestsMatchBufferedOutput(t *testing.T) {
	d := fixtureGit(t)
	gitFixture(t, d, "branch", "topic")
	s, e := (gitAdapter{}).Observe(context.Background(), d, "HEAD")
	if e != nil {
		t.Fatal(e)
	}
	digest := func(args ...string) string {
		c := exec.Command("git", append([]string{"-C", d}, args...)...)
		b, e := c.Output()
		if e != nil {
			t.Fatal(e)
		}
		h := sha256.Sum256(b)
		return hex.EncodeToString(h[:])
	}
	if s.ReferenceDigest != digest("for-each-ref", "--format=%(refname)%00%(objectname)%00") || s.RegistrationDigest != digest("worktree", "list", "--porcelain", "-z") {
		t.Fatal("snapshot digests changed")
	}
}

// A parser that buffered all stdout (for example with io.ReadAll) would allocate
// the 4,325,376-byte stream; the incremental reader stays far below that.
func TestGitStreamRecordsDoesNotBufferStdout(t *testing.T) {
	for _, kind := range []string{"line", "block"} {
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		if n, e := runCollection(t, kind, 1024, 4224, 0, 0); e != nil || n != 1024 {
			t.Fatalf("%s: n=%d %v", kind, n, e)
		}
		runtime.ReadMemStats(&after)
		if grown := after.TotalAlloc - before.TotalAlloc; grown > 1<<20 {
			t.Fatalf("%s: streaming a 4,325,376 byte output allocated %d bytes", kind, grown)
		}
	}
}

// Malformed or truncated streams reach the production read functions, which map
// them to the evidence codes of their own path.
func TestCollectionReadersMapTruncatedAndMalformedStreams(t *testing.T) {
	oid := strings.Repeat("a", 40)
	head := "worktree /w\x00HEAD " + oid + "\x00branch refs/heads/main\x00\x00"
	cases := []struct {
		name string
		run  func() error
		want string
	}{
		{"v1 refs", func() error {
			_, e := readGitTidyRefs(textReader("refs/heads/x\x00"+oid+"\x00\x00commit\x00"), "sha1")
			return e
		}, "tidy-invalid-reference"},
		{"v1 worktrees", func() error {
			_, e := readGitTidyWorktrees(textReader(strings.TrimSuffix(head, "\x00")), "sha1")
			return e
		}, "tidy-invalid-registration"},
		{"v3 refs", func() error {
			_, e := readTidyV3References(textReader("refs/heads/x\x00"+oid+"\x00\x00commit\x00"), "sha1")
			return e
		}, "invalid-reference"},
		{"v3 registrations", func() error {
			_, e := readTidyV3Registrations(textReader(strings.TrimSuffix(head, "\x00")), "sha1")
			return e
		}, "tidy-invalid-registration"},
		{"reference map", func() error {
			_, e := readGitTidyReferenceMap(textReader("refs/heads/x\x00" + oid + "\x00\x00"))
			return e
		}, "tidy-invalid-reference"},
		{"v1 empty registrations", func() error { _, e := readGitTidyWorktrees(textReader(""), "sha1"); return e }, "tidy-invalid-registration"},
		{"v1 refs bound", func() error {
			_, e := readGitTidyRefs(textReader(strings.Repeat("r", 4225)+"\n"), "sha1")
			return gitCollectionError(e)
		}, "git-output-limit"},
	}
	for _, tc := range cases {
		if got := collectionCode(tc.run()); !strings.HasPrefix(got, tc.want+":") {
			t.Fatalf("%s: got %q want code %s", tc.name, got, tc.want)
		}
	}
}
