// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

var errNativeBlobProtocol = errors.New("invalid raw Git object stream")

type gitBlobRequest struct {
	Path, OID  string
	Size       int64
	Executable bool
}

func validateGitBlobRequests(requests []gitBlobRequest, format string) ([]byte, error) {
	if format != "sha1" && format != "sha256" {
		return nil, errNativeBlobProtocol
	}
	v := newGitTreeValidator()
	var input strings.Builder
	for _, r := range requests {
		if !fullGitOID(r.OID, format) {
			return nil, errNativeBlobProtocol
		}
		mode := "100644"
		if r.Executable {
			mode = "100755"
		}
		if e := v.add(fmt.Sprintf("%s blob %s %d\t%s", mode, r.OID, r.Size, r.Path)); e != nil {
			return nil, e
		}
		input.WriteString(r.OID)
		input.WriteByte('\n')
	}
	return []byte(input.String()), nil
}
func gitStoredBlobSample(ctx context.Context, bin, dir, format string, requests []gitBlobRequest, budget *tidyV3ProofBudget) ([]RetirementStoredFile, error) {
	input, e := validateGitBlobRequests(requests, format)
	if e != nil {
		return nil, e
	}
	if e = ctx.Err(); e != nil {
		return nil, e
	}
	if e = budget.beginRawSample(); e != nil {
		return nil, e
	}
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd, e := prepareSandboxed(streamCtx, bin, []string{"--no-replace-objects", "cat-file", "--batch"}, dir, nil, nil)
	if e != nil {
		return nil, gitExecutionError(e)
	}
	var files []RetirementStoredFile
	_, e = executeNativeStreamBudgeted(ctx, cancel, cmd, input, 64*1024*1024+128*1024, func(out io.Reader) error {
		var e error
		files, e = parseGitBlobSample(ctx, out, requests, budget)
		return e
	}, budget)
	if e != nil {
		return nil, e
	}
	return files, nil
}
func parseGitBlobSample(ctx context.Context, out io.Reader, requests []gitBlobRequest, budget *tidyV3ProofBudget) ([]RetirementStoredFile, error) {
	format := "sha1"
	if len(requests) > 0 && len(requests[0].OID) == 64 {
		format = "sha256"
	}
	if _, e := validateGitBlobRequests(requests, format); e != nil {
		return nil, e
	}
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if e := budget.beginUserPass(); e != nil {
		return nil, e
	}
	framing := &nativeCountReader{r: budget.reader(ctx, proofFraming, out), limit: 128 * 1024}
	payload := budget.reader(ctx, proofUser, out)
	files := make([]RetirementStoredFile, 0, len(requests))
	var buffer [64 * 1024]byte
	readByte := func() (byte, error) {
		var one [1]byte
		n, e := framing.Read(one[:])
		if e != nil && !(n == 1 && e == io.EOF) {
			return 0, e
		}
		if n != 1 {
			return 0, io.ErrNoProgress
		}
		return one[0], nil
	}
	for _, request := range requests {
		// No prefetch: a bad/missing header may consume at most the 127-byte header
		// allowance plus one detection byte; binary payload is a separate channel.
		header := make([]byte, 0, 127)
		for {
			b, e := readByte()
			if e != nil {
				return nil, e
			}
			header = append(header, b)
			if len(header) > 127 {
				return nil, errNativeBlobProtocol
			}
			if b == '\n' {
				break
			}
		}
		fields := strings.Split(string(header[:len(header)-1]), " ")
		if len(fields) != 3 || fields[0] != request.OID || fields[1] != "blob" || fields[2] != strconv.FormatInt(request.Size, 10) {
			return nil, errNativeBlobProtocol
		}
		digest := sha256.New()
		remaining := request.Size
		for remaining > 0 {
			n, e := payload.Read(buffer[:min(int64(len(buffer)), remaining)])
			if n > 0 {
				digest.Write(buffer[:n])
				remaining -= int64(n)
			}
			if e != nil {
				return nil, e
			}
			if n == 0 {
				return nil, io.ErrNoProgress
			}
		}
		b, e := readByte()
		if e != nil {
			return nil, e
		}
		if b != '\n' {
			return nil, errNativeBlobProtocol
		}
		files = append(files, RetirementStoredFile{Path: request.Path, Digest: hex.EncodeToString(digest.Sum(nil)), Size: request.Size, Executable: request.Executable})
	}
	var tail [1]byte
	n, e := framing.Read(tail[:])
	if e != nil && e != io.EOF {
		return nil, e
	}
	if n != 0 {
		return nil, errNativeBlobProtocol
	}
	if e != io.EOF {
		if e == nil {
			e = io.ErrNoProgress
		}
		return nil, e
	}
	return files, nil
}
