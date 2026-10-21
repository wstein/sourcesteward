// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os/exec"
	"unicode/utf8"
)

// Collection reads (for-each-ref and worktree list) stream through the bounded
// native stream executor and are parsed record by record. Memory is bounded by
// one record plus the caller's parsed set; every bound below refuses, never
// truncates. Non-collection reads keep the buffered 64 KiB nativeOutputLimit.
type gitRecordKind int

const (
	// gitRecordLine records end at a newline (for-each-ref formats end in %00 then LF).
	gitRecordLine gitRecordKind = iota
	// gitRecordBlock records are NUL-terminated fields ended by an empty field
	// (worktree list --porcelain -z).
	gitRecordBlock
	// gitRecordNUL records are single NUL-terminated fields (ls-files -z).
	gitRecordNUL
)

var (
	errGitRecordLimit = errors.New("git record exceeds its bound")
	errGitRecordCount = errors.New("git collection exceeds its record bound")
)

// parseGitRecords delivers each complete record, delimiters included, to accept.
// The slice is valid only until accept returns. A final record without its
// delimiter is io.ErrUnexpectedEOF: no prefix is ever actionable.
func parseGitRecords(source io.Reader, kind gitRecordKind, accept func([]byte) error) error {
	return parseGitRecordsLimit(source, kind, observationLimit, accept)
}

// parseGitRecordsLimit is parseGitRecords with an explicit record-count bound.
func parseGitRecordsLimit(source io.Reader, kind gitRecordKind, limit int, accept func([]byte) error) error {
	reader := newGitMetadataReader(source)
	var block []byte
	for count := 0; ; {
		reader.beginLogicalRecord()
		var record []byte
		var e error
		if kind == gitRecordBlock {
			block, e = readGitBlockRecord(reader, block[:0])
			record = block
		} else if kind == gitRecordNUL {
			record, e = reader.buffer.ReadSlice(0)
			e = reader.recordError(e, len(record), gitMetadataRecordLimit)
		} else {
			record, e = reader.buffer.ReadSlice('\n')
			e = reader.recordError(e, len(record), gitMetadataRecordLimit)
		}
		if e == io.EOF && len(record) == 0 {
			return nil
		}
		if e != nil {
			if e == io.EOF {
				return io.ErrUnexpectedEOF
			}
			return e
		}
		if count++; count > limit {
			return errGitRecordCount
		}
		if e = accept(record); e != nil {
			return e
		}
	}
}

func readGitBlockRecord(r *gitMetadataReader, record []byte) ([]byte, error) {
	for {
		field, e := r.buffer.ReadSlice(0)
		record = append(record, field...)
		if e = r.recordError(e, len(record), gitMetadataRecordLimit); e != nil {
			return record, e
		}
		if len(field) == 1 {
			return record, nil
		}
	}
}

// recordError maps the shared reader's bounds to one record-level error. The
// logical-record read cap and the stream cap share one sentinel; the reader's
// absolute record end tells them apart.
func (r *gitMetadataReader) recordError(e error, length, limit int) error {
	if e == bufio.ErrBufferFull || length > limit || (e == errNativeStreamLimit && r.input.count >= r.input.end) {
		return errGitRecordLimit
	}
	return e
}

func gitStreamRecords(ctx context.Context, cancel context.CancelFunc, cmd *exec.Cmd, kind gitRecordKind, accept func([]byte) error) error {
	return gitStreamRecordsLimit(ctx, cancel, cmd, kind, observationLimit, accept)
}

func gitStreamRecordsLimit(ctx context.Context, cancel context.CancelFunc, cmd *exec.Cmd, kind gitRecordKind, limit int, accept func([]byte) error) error {
	_, e := executeNativeStream(ctx, cancel, cmd, nil, gitMetadataStreamLimit, func(out io.Reader) error {
		return parseGitRecordsLimit(out, kind, limit, accept)
	})
	return gitCollectionError(e)
}

// gitReadRecords is the streamed counterpart of gitRead for collections.
func gitReadRecords(ctx context.Context, bin, dir string, kind gitRecordKind, args []string, accept func([]byte) error) error {
	return gitReadRecordsLimit(ctx, bin, dir, kind, observationLimit, args, accept)
}

// gitReadRecordsLimit reads a collection of at most limit records. Exceeding
// the limit, the per-record bound, the stream bound, a missing final
// delimiter or a failed exit refuses; nothing is ever truncated.
func gitReadRecordsLimit(ctx context.Context, bin, dir string, kind gitRecordKind, limit int, args []string, accept func([]byte) error) error {
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd, e := prepareSandboxed(streamCtx, bin, append([]string{"--no-replace-objects"}, args...), dir, nil, nil)
	if e != nil {
		return gitExecutionError(e)
	}
	return gitStreamRecordsLimit(ctx, cancel, cmd, kind, limit, func(record []byte) error {
		if !utf8.Valid(record) {
			return failure("git-query-refused", "bounded native Git safety evidence is unavailable")
		}
		return accept(record)
	})
}

// gitCollectionReader reads one collection. Callers take it as a parameter so a
// truncated or malformed stream reaches the same code path as a real read.
// A truncated final record is io.ErrUnexpectedEOF; each caller maps it to its
// own malformed-evidence code.
type gitCollectionReader func(kind gitRecordKind, args []string, accept func([]byte) error) error

func gitCollectionReaderFor(ctx context.Context, bin, dir string) gitCollectionReader {
	return func(kind gitRecordKind, args []string, accept func([]byte) error) error {
		return gitReadRecords(ctx, bin, dir, kind, args, accept)
	}
}

// gitReadRecordsDigest returns the SHA-256 of the complete output, equal to
// gitDigest of the buffered text, without holding it.
func gitReadRecordsDigest(ctx context.Context, bin, dir string, kind gitRecordKind, args ...string) (string, error) {
	h := sha256.New()
	if e := gitReadRecords(ctx, bin, dir, kind, args, func(record []byte) error { h.Write(record); return nil }); e != nil {
		if errors.Is(e, io.ErrUnexpectedEOF) {
			return "", failure("git-query-refused", "bounded native Git safety evidence is unavailable")
		}
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func gitCollectionError(e error) error {
	switch {
	case e == nil:
		return nil
	case errors.Is(e, errGitRecordLimit):
		return failure("git-output-limit", "a Git record exceeds 4224 bytes")
	case errors.Is(e, errGitRecordCount):
		return failure("git-observation-limit", "Git collection exceeds 1024 records")
	case errors.Is(e, errNativeStreamLimit):
		return failure("git-output-limit", "Git stdout exceeds 4325376 bytes")
	case errors.Is(e, errNativeOutputLimit):
		return failure("git-output-limit", "Git stderr exceeds 65536 bytes")
	case errors.Is(e, errNativeStreamCommandFailed):
		return failure("git-query-refused", "bounded native Git safety evidence is unavailable")
	}
	return gitExecutionError(e)
}
