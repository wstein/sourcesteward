// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func blobTestBudget(t *testing.T) *tidyV3ProofBudget {
	t.Helper()
	b := newTidyV3ProofBudget(false)
	if e := b.beginObservation(); e != nil {
		t.Fatal(e)
	}
	return b
}
func blobWire(oid string, data []byte) []byte {
	return append(append([]byte(fmt.Sprintf("%s blob %d\n", oid, len(data))), data...), '\n')
}
func TestGitBlobSampleNativeBinaryDuplicateOID(t *testing.T) {
	d := fixtureGit(t)
	data := bytes.Repeat([]byte{0, 255, '\n', 128, 'x'}, 52430)
	data = append(data, 0, '\n', 255)
	if e := os.WriteFile(filepath.Join(d, "binary"), data, 0600); e != nil {
		t.Fatal(e)
	}
	gitFixture(t, d, "add", "binary")
	gitFixture(t, d, "commit", "-qm", "raw binary fixture")
	oid := gitFixture(t, d, "rev-parse", "HEAD:binary")
	bin, e := gitExecutable()
	if e != nil {
		t.Fatal(e)
	}
	b := blobTestBudget(t)
	requests := []gitBlobRequest{{Path: "one", OID: oid, Size: int64(len(data))}, {Path: "two", OID: oid, Size: int64(len(data)), Executable: true}}
	got, e := gitStoredBlobSample(context.Background(), bin, d, "sha1", requests, b)
	if e != nil {
		t.Fatalf("valid native binary batch refused: %v", e)
	}
	if len(got) != 2 || got[0].Digest != gitDigest(string(data)) || got[1].Digest != got[0].Digest || !got[1].Executable {
		t.Fatalf("binary/path proof incomplete: %+v", got)
	}
	counters := b.snapshot()
	if counters.RawProcesses != 1 || counters.UserBytes != int64(2*len(data)) || counters.Observations != 1 || counters.MaxReadBuffer > 65536 {
		t.Fatalf("sample accounting: %+v", counters)
	}
}
func TestGitBlobSampleFramingCompleteness(t *testing.T) {
	a, b := strings.Repeat("a", 40), strings.Repeat("b", 40)
	payload := []byte{0, '\n', 255, 'x'}
	request := []gitBlobRequest{{Path: "file", OID: a, Size: 4}}
	good := blobWire(a, payload)
	for _, tc := range []struct {
		name string
		wire []byte
		ok   bool
	}{
		{"binary", good, true}, {"wrong-oid", blobWire(b, payload), false},
		{"wrong-type", bytes.Replace(good, []byte(" blob "), []byte(" tree "), 1), false},
		{"wrong-size", bytes.Replace(good, []byte(" 4\n"), []byte(" 5\n"), 1), false},
		{"missing", []byte(a + " missing\n"), false}, {"short", good[:len(good)-2], false},
		{"wrong-trailer", append(append([]byte{}, good[:len(good)-1]...), 'x'), false},
		{"extra", append(append([]byte{}, good...), 0), false},
		{"oversized-header", []byte(a + " blob " + strings.Repeat("1", 200) + "\n"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			budget := blobTestBudget(t)
			got, e := parseGitBlobSample(context.Background(), bytes.NewReader(tc.wire), request, budget)
			if (e == nil) != tc.ok {
				t.Fatalf("accepted=%v expected=%v error=%v", e == nil, tc.ok, e)
			}
			if tc.ok && (len(got) != 1 || got[0].Digest != gitDigest(string(payload))) {
				t.Fatal("wrong binary hash")
			}
			c := budget.snapshot()
			if c.TotalBytes != int64(len(tc.wire)) && tc.ok {
				t.Fatalf("actual consumption not charged: %+v", c)
			}
		})
	}
	requests := []gitBlobRequest{{Path: "one", OID: a, Size: 4}, {Path: "two", OID: b, Size: 4}}
	if _, e := parseGitBlobSample(context.Background(), bytes.NewReader(append(blobWire(b, payload), blobWire(a, payload)...)), requests, blobTestBudget(t)); e == nil {
		t.Fatal("wrong object order admitted")
	}
}
func TestGitBlobSampleLimitsAndNonResettableBudget(t *testing.T) {
	for _, apply := range []bool{false, true} {
		max := 3
		if apply {
			max = 8
		}
		b := newTidyV3ProofBudget(apply)
		for i := 0; i < max; i++ {
			if e := b.beginObservation(); e != nil {
				t.Fatal(e)
			}
		}
		if e := b.beginObservation(); !errors.Is(e, errTidyV3ProofLimit) {
			t.Fatal("observation budget reset/overrun admitted", e)
		}
		b = newTidyV3ProofBudget(apply)
		if e := b.consumeUserBytes(int64(max) * 192 * 1024 * 1024); e != nil {
			t.Fatal(e)
		}
		if e := b.consumeUserBytes(1); !errors.Is(e, errTidyV3ProofLimit) {
			t.Fatal("operation user bytes overrun admitted", e)
		}
		if b.snapshot().UserBytes != int64(max)*192*1024*1024+1 {
			t.Fatal("failed read byte omitted")
		}
	}
	oid := strings.Repeat("a", 40)
	for _, size := range []int64{0, 16 * 1024 * 1024} {
		payload := bytes.Repeat([]byte{0, '\n'}, int(size/2))
		got, e := parseGitBlobSample(context.Background(), bytes.NewReader(blobWire(oid, payload)), []gitBlobRequest{{Path: "file", OID: oid, Size: size}}, blobTestBudget(t))
		if e != nil || len(got) != 1 {
			t.Fatalf("exact file size=%d: %v", size, e)
		}
	}
}
func TestGitBlobSampleCancellationAndUnderlyingBounds(t *testing.T) {
	oid := strings.Repeat("a", 40)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := parseGitBlobSample(ctx, strings.NewReader(oid+" blob 0\n\n"), []gitBlobRequest{{Path: "file", OID: oid}}, blobTestBudget(t)); !errors.Is(e, context.Canceled) {
		t.Fatalf("deadline reset/ignored: %v", e)
	}
	counted := &nativeCountReader{r: strings.NewReader(strings.Repeat("x", 10000)), limit: 10000}
	_, e := parseGitBlobSample(context.Background(), counted, []gitBlobRequest{{Path: "file", OID: oid}}, blobTestBudget(t))
	if e == nil || counted.count > 128 {
		t.Fatalf("framing overread=%d error=%v", counted.count, e)
	}
	reader, writer := io.Pipe()
	defer writer.Close()
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, e := parseGitBlobSample(ctx, reader, []gitBlobRequest{{Path: "file", OID: oid}}, blobTestBudget(t))
		done <- e
	}()
	<-ctx.Done()
	reader.CloseWithError(ctx.Err())
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("stalled framing accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("stalled parser did not terminate")
	}
}

type blobZeroReader struct{}

func (blobZeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }
func TestGitBlobSampleExactAggregateAndAdmission(t *testing.T) {
	oid := strings.Repeat("a", 40)
	requests := []gitBlobRequest{}
	parts := []io.Reader{}
	for i := 0; i < 4; i++ {
		requests = append(requests, gitBlobRequest{Path: fmt.Sprint(i), OID: oid, Size: 16 * 1024 * 1024})
		parts = append(parts, strings.NewReader(oid+" blob 16777216\n"), io.LimitReader(blobZeroReader{}, 16*1024*1024), strings.NewReader("\n"))
	}
	b := blobTestBudget(t)
	files, e := parseGitBlobSample(context.Background(), io.MultiReader(parts...), requests, b)
	if e != nil || len(files) != 4 || b.snapshot().UserBytes != 64*1024*1024 {
		t.Fatalf("exact aggregate: %v %+v", e, b.snapshot())
	}
	for _, invalid := range [][]gitBlobRequest{
		append(append([]gitBlobRequest{}, requests...), gitBlobRequest{Path: "extra", OID: oid, Size: 1}),
		{{Path: "file", OID: oid, Size: 16*1024*1024 + 1}},
		{{Path: "file", OID: oid, Size: -1}},
		{{Path: "../escape", OID: oid}},
		{{Path: "file", OID: oid}, {Path: "file", OID: oid}},
	} {
		counted := &nativeCountReader{r: strings.NewReader("unexpected"), limit: 100}
		if _, e := parseGitBlobSample(context.Background(), counted, invalid, blobTestBudget(t)); e == nil || counted.count != 0 {
			t.Fatalf("invalid admission consumed=%d error=%v", counted.count, e)
		}
	}
}
func TestTidyProofLedgerCapsBeforeReadsAndCountsFailures(t *testing.T) {
	for channel := 0; channel < proofChannels; channel++ {
		b := newTidyV3ProofBudget(false)
		remaining := b.limits[channel]
		if e := b.consume(channel, remaining-2); e != nil {
			t.Fatal(e)
		}
		source := &nativeCountReader{r: strings.NewReader(strings.Repeat("x", 65536)), limit: 65536}
		reader := b.reader(context.Background(), channel, source)
		var buffer [65536]byte
		if n, e := reader.Read(buffer[:]); n != 2 || e != nil {
			t.Fatalf("channel=%d n=%d e=%v", channel, n, e)
		}
		if n, e := reader.Read(buffer[:]); n != 1 || !errors.Is(e, errTidyV3ProofLimit) {
			t.Fatalf("channel=%d detection n=%d e=%v", channel, n, e)
		}
		if source.count != 3 || b.counts[channel] != remaining+1 {
			t.Fatalf("channel overread/omitted failure byte: %d/%d", source.count, b.counts[channel])
		}
		if n, e := reader.Read(buffer[:]); n != 0 || !errors.Is(e, errTidyV3ProofLimit) {
			t.Fatal("failed ledger reset")
		}
	}
}
func TestTidyProofLedgerConcurrentExactEOFAndGlobalOverrun(t *testing.T) {
	b := newTidyV3ProofBudget(false)
	for channel := 0; channel < proofChannels; channel++ {
		if e := b.consume(channel, b.limits[channel]); e != nil {
			t.Fatal(e)
		}
	}
	done := make(chan error, 4)
	for channel := 0; channel < proofChannels; channel++ {
		go func(channel int) {
			var one [1]byte
			_, e := b.reader(context.Background(), channel, strings.NewReader("")).Read(one[:])
			done <- e
		}(channel)
	}
	for i := 0; i < 4; i++ {
		if e := <-done; e != io.EOF {
			t.Fatal("exact aggregate EOF refused", e)
		}
	}
	if b.snapshot().TotalBytes != b.totalLimit {
		t.Fatal("EOF charged phantom bytes")
	}
	source := &nativeCountReader{r: strings.NewReader("xx"), limit: 2}
	var two [2]byte
	if n, e := b.reader(context.Background(), proofUser, source).Read(two[:]); n != 1 || !errors.Is(e, errTidyV3ProofLimit) || source.count != 1 {
		t.Fatalf("global detection n=%d e=%v consumed=%d", n, e, source.count)
	}
}

func TestTidyProofLedgerFullObservationPassAndProcessCounts(t *testing.T) {
	for _, apply := range []bool{false, true} {
		max := 3
		if apply {
			max = 8
		}
		b := newTidyV3ProofBudget(apply)
		for i := 0; i < max; i++ {
			if e := b.beginObservation(); e != nil {
				t.Fatal(e)
			}
			if e := b.beginRawSample(); e != nil {
				t.Fatal(e)
			}
			for j := 0; j < 3; j++ {
				if e := b.beginUserPass(); e != nil {
					t.Fatal(e)
				}
			}
			for j := 0; j < 4; j++ {
				if e := b.beginMetadataPass(); e != nil {
					t.Fatal(e)
				}
			}
		}
		c := b.snapshot()
		if c.Observations != max || c.RawProcesses != max || c.UserPasses != 3*max || c.MetadataPasses != 4*max {
			t.Fatalf("pass omission %+v", c)
		}
		if e := b.beginUserPass(); !errors.Is(e, errTidyV3ProofLimit) {
			t.Fatal("additional pass admitted")
		}
	}
	b := blobTestBudget(t)
	if e := b.beginRawSample(); e != nil {
		t.Fatal(e)
	}
	if e := b.beginRawSample(); !errors.Is(e, errTidyV3ProofLimit) {
		t.Fatal("second process in same observation admitted")
	}
}

type blobErrorReader struct{ err error }

func (r blobErrorReader) Read([]byte) (int, error) { return 0, r.err }
func TestGitBlobSampleReadErrorsAndSHA256(t *testing.T) {
	oid := strings.Repeat("a", 64)
	data := []byte{0, '\n', 255}
	request := []gitBlobRequest{{Path: "file", OID: oid, Size: 3}}
	sentinel := errors.New("injected EOF failure")
	if _, e := parseGitBlobSample(context.Background(), io.MultiReader(bytes.NewReader(blobWire(oid, data)), blobErrorReader{sentinel}), request, blobTestBudget(t)); !errors.Is(e, sentinel) {
		t.Fatal("read failure treated as success", e)
	}
	if _, e := parseGitBlobSample(context.Background(), blobErrorReader{}, request, blobTestBudget(t)); !errors.Is(e, io.ErrNoProgress) {
		t.Fatal("zero progress accepted", e)
	}
	got, e := parseGitBlobSample(context.Background(), bytes.NewReader(blobWire(oid, data)), request, blobTestBudget(t))
	if e != nil || len(got) != 1 {
		t.Fatal("SHA256 object framing refused", e)
	}
}
func TestGitBlobSampleNativeBoundedInputAndProcessCount(t *testing.T) {
	d := fixtureGit(t)
	oid := gitFixture(t, d, "rev-parse", "HEAD:file")
	bin, e := gitExecutable()
	if e != nil {
		t.Fatal(e)
	}
	requests := make([]gitBlobRequest, retirementManifestEntries)
	for i := range requests {
		requests[i] = gitBlobRequest{Path: fmt.Sprint(i), OID: oid, Size: 7}
	}
	b := blobTestBudget(t)
	files, e := gitStoredBlobSample(context.Background(), bin, d, "sha1", requests, b)
	if e != nil || len(files) != retirementManifestEntries || b.snapshot().RawProcesses != 1 || b.snapshot().UserBytes != 7*retirementManifestEntries || b.snapshot().UserPasses != 1 {
		t.Fatalf("bounded concurrent batch: %+v %v", b.snapshot(), e)
	}
	b = blobTestBudget(t)
	requests = append(requests, gitBlobRequest{Path: "over", OID: oid})
	if _, e = gitStoredBlobSample(context.Background(), bin, d, "sha1", requests, b); e == nil || b.snapshot().RawProcesses != 0 {
		t.Fatal("oversized selection spawned a process", e)
	}
}

type proofHeldReader struct{ entered, release chan struct{} }

func (r *proofHeldReader) Read(p []byte) (int, error) {
	close(r.entered)
	<-r.release
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}
func TestTidyProofLedgerInflightReservationsCannotMintDetection(t *testing.T) {
	for _, channelOnly := range []bool{false, true} {
		t.Run(fmt.Sprint(channelOnly), func(t *testing.T) {
			b := newTidyV3ProofBudget(false)
			probeChannel := proofStderr
			if channelOnly {
				b.limits[proofUser] = 10
				probeChannel = proofUser
			} else {
				b.totalLimit = 10
			}
			held := &proofHeldReader{make(chan struct{}), make(chan struct{})}
			released := false
			defer func() {
				if !released {
					close(held.release)
				}
			}()
			done := make(chan error, 1)
			go func() {
				var ten [10]byte
				_, e := b.reader(context.Background(), proofUser, held).Read(ten[:])
				done <- e
			}()
			<-held.entered
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
			defer cancel()
			source := &nativeCountReader{r: strings.NewReader("xx"), limit: 2}
			probe := b.reader(ctx, probeChannel, source)
			var one [1]byte
			n1, e1 := probe.Read(one[:])
			n2, e2 := probe.Read(one[:])
			close(held.release)
			released = true
			e3 := <-done
			t.Logf("held reservation=10 probes=%d/%d errors=%v/%v/%v actual=%d", n1, n2, e1, e2, e3, b.snapshot().TotalBytes)
			if n1 != 0 || n2 != 0 || !errors.Is(e1, context.DeadlineExceeded) || !errors.Is(e2, context.DeadlineExceeded) || source.count != 0 || e3 != nil || b.snapshot().TotalBytes != 10 {
				t.Fatal("in-flight reservations minted detection allowance")
			}
			n, e := b.reader(context.Background(), probeChannel, strings.NewReader("xx")).Read(one[:])
			if n != 1 || !errors.Is(e, errTidyV3ProofLimit) || b.snapshot().TotalBytes != 11 {
				t.Fatalf("settled global detection: n=%d e=%v actual=%d", n, e, b.snapshot().TotalBytes)
			}
		})
	}
}

type blobTailErrorReader struct{ err error }

func (r blobTailErrorReader) Read(p []byte) (int, error) { p[0] = 'x'; return 1, r.err }
func TestGitBlobFinalProbeRetainsBoundErrorAndDetectionByte(t *testing.T) {
	oid := strings.Repeat("a", 40)
	for _, boundErr := range []error{errTidyV3ProofLimit, errNativeStreamLimit} {
		b := blobTestBudget(t)
		wire := blobWire(oid, nil)
		_, e := parseGitBlobSample(context.Background(), io.MultiReader(bytes.NewReader(wire), blobTailErrorReader{boundErr}), []gitBlobRequest{{Path: "file", OID: oid}}, b)
		if !errors.Is(e, boundErr) || b.snapshot().TotalBytes != int64(len(wire)+1) {
			t.Fatalf("bound=%v got=%v actual=%d want=%d", boundErr, e, b.snapshot().TotalBytes, len(wire)+1)
		}
	}
}
