// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"hash"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

const gitMetadataRecordLimit = 4224
const gitMetadataStreamLimit = 4325376

// A buffer refill may read ahead across fields and short records. Bind every
// underlying read to the current logical record's absolute end plus exactly
// one detection byte, rather than checking only the returned field length.
type gitMetadataInput struct {
	source     io.Reader
	count, end int64
}

func (r *gitMetadataInput) Read(p []byte) (int, error) {
	remaining := r.end - r.count
	if remaining <= 0 {
		return 0, errNativeStreamLimit
	}
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	n, e := r.source.Read(p)
	r.count += int64(n)
	return n, e
}

type gitMetadataReader struct {
	input  *gitMetadataInput
	buffer *bufio.Reader
}

func newGitMetadataReader(source io.Reader) *gitMetadataReader {
	input := &gitMetadataInput{source: source}
	reader := &gitMetadataReader{input: input, buffer: bufio.NewReaderSize(input, gitMetadataRecordLimit+1)}
	reader.beginLogicalRecord()
	return reader
}
func (r *gitMetadataReader) beginLogicalRecord() {
	consumed := r.input.count - int64(r.buffer.Buffered())
	r.input.end = consumed + gitMetadataRecordLimit + 1
}

// Delimiters are included in record bounds. No prefix is actionable without
// its final delimiter, complete stream EOF and successful native exit.
func readGitNULRecord(r *gitMetadataReader, limit int) ([]byte, error) {
	if limit < 0 || limit > gitMetadataRecordLimit {
		return nil, errNativeStreamLimit
	}
	record, e := r.buffer.ReadSlice(0)
	if len(record) > limit || e == bufio.ErrBufferFull {
		return nil, errNativeStreamLimit
	}
	return record, e
}

type gitTreeEntry struct {
	Path, OID, Mode string
	Size            int64
}

func gitTreeEvidence(ctx context.Context, bin, dir, commit, format string, digest hash.Hash) ([]string, error) {
	entries, e := gitTreeEntriesMode(ctx, bin, dir, commit, format, digest, planningSessionFrom(ctx) != nil || creationPolicyFrom(ctx) || tidyApplyPolicyFrom(ctx))
	if e != nil {
		return nil, e
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		paths = append(paths, entry.Path)
	}
	return paths, nil
}
func gitTreeEntries(ctx context.Context, bin, dir, commit, format string, digest hash.Hash) ([]gitTreeEntry, error) {
	return gitTreeEntriesMode(ctx, bin, dir, commit, format, digest, false)
}
func gitTreeEntriesMode(ctx context.Context, bin, dir, commit, format string, digest hash.Hash, gitlinks bool) ([]gitTreeEntry, error) {
	if !fullGitOID(commit, format) {
		return nil, failure("git-tree-refused", "tree selection requires a full admitted commit identity")
	}
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd, e := prepareSandboxed(streamCtx, bin, []string{"--no-replace-objects", "ls-tree", "-r", "-l", "-z", "--full-tree", commit}, dir, nil, nil)
	if e != nil {
		return nil, gitExecutionError(e)
	}
	entries := []gitTreeEntry{}
	validator := newGitTreeValidator()
	validator.gitlinks = gitlinks
	_, e = executeNativeStream(ctx, cancel, cmd, nil, gitMetadataStreamLimit, func(out io.Reader) error {
		return parseGitTreeStream(out, format, validator, func(record []byte, path string) {
			if digest != nil {
				digest.Write(record)
			}
			meta, _, _ := strings.Cut(string(record), "\t")
			fields := strings.Fields(meta)
			if session := planningSessionFrom(ctx); session != nil && fields[0] == "160000" {
				session.noteGitlink(path)
			}
			size, _ := strconv.ParseInt(fields[3], 10, 64)
			entries = append(entries, gitTreeEntry{Path: path, OID: fields[2], Mode: fields[0], Size: size})
		})
	})
	if e != nil {
		return nil, gitMetadataError(e)
	}
	return entries, nil
}

func parseGitTreeStream(out io.Reader, format string, validator *gitTreeValidator, accept func([]byte, string)) error {
	reader := newGitMetadataReader(out)
	for {
		reader.beginLogicalRecord()
		record, e := readGitNULRecord(reader, gitMetadataRecordLimit)
		if e == io.EOF && len(record) == 0 {
			return nil
		}
		if e != nil {
			if e == io.EOF {
				return io.ErrUnexpectedEOF
			}
			return e
		}
		if !utf8.Valid(record) {
			return failure("git-tree-refused", "tree metadata is not UTF-8")
		}
		entry := string(record[:len(record)-1])
		meta, path, ok := strings.Cut(entry, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 4 || !fullGitOID(fields[2], format) {
			return failure("git-tree-refused", "tree metadata lacks complete object identity")
		}
		if e = validator.add(entry); e != nil {
			return e
		}
		accept(record, path)
	}
}

func gitFilterEvidence(ctx context.Context, bin, dir, commit string, paths []string, digest hash.Hash) error {
	var input strings.Builder
	if len(paths) > checkoutLeafLimit {
		return failure("git-checkout-limit", fmt.Sprintf("filter path inventory exceeds %d entries", checkoutLeafLimit))
	}
	for _, path := range paths {
		if len(path) == 0 || len(path) > 4096 || strings.ContainsRune(path, 0) {
			return failure("git-tree-refused", "invalid filter path input")
		}
		input.WriteString(path)
		input.WriteByte(0)
	}
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	// Planning pins the attribute source to the tree through the policy;
	// worktree and untracked .gitattributes never apply.
	checkArgs := []string{"--no-replace-objects", "check-attr", "--source=" + commit, "--stdin", "-z", "filter"}
	if session := planningSessionFrom(ctx); session != nil {
		checkArgs = append(append([]string{"--no-replace-objects"}, session.attrSource(commit)...), "check-attr", "--stdin", "-z", "filter")
	}
	cmd, e := prepareSandboxed(streamCtx, bin, checkArgs, dir, nil, nil)
	if e != nil {
		return gitExecutionError(e)
	}
	_, e = executeNativeStream(ctx, cancel, cmd, []byte(input.String()), gitMetadataStreamLimit, func(out io.Reader) error { return parseGitFilterStream(out, paths, digest) })
	if e != nil {
		return gitMetadataError(e)
	}
	return nil
}

func parseGitFilterStream(out io.Reader, paths []string, digest hash.Hash) error {
	reader := newGitMetadataReader(out)
	for _, path := range paths {
		reader.beginLogicalRecord()
		var triple strings.Builder
		var fields [3]string
		for i := range fields {
			record, e := readGitNULRecord(reader, gitMetadataRecordLimit-triple.Len())
			if e != nil {
				if e == io.EOF {
					return io.ErrUnexpectedEOF
				}
				return e
			}
			if !utf8.Valid(record) {
				return failure("git-attributes-refused", "filter metadata is not UTF-8")
			}
			fields[i] = string(record[:len(record)-1])
			triple.Write(record)
		}
		if fields[0] != path || fields[1] != "filter" {
			return failure("git-attributes-refused", "checkout filter evidence does not match the selected base paths")
		}
		if fields[2] != "unspecified" && fields[2] != "unset" {
			return failure("git-active-filter-refused", "base checkout has an active filter attribute; stw (task creation, tidy plan and apply) never runs or silently suppresses configured filter helpers. Remove the filter attribute or the filter.<name> configuration for this run")
		}
		digest.Write([]byte(triple.String()))
	}
	var tail [1]byte
	n, e := reader.buffer.Read(tail[:])
	if n != 0 {
		return failure("git-attributes-refused", "checkout filter evidence contains extra fields")
	}
	if e != io.EOF {
		if e == nil {
			return io.ErrNoProgress
		}
		return e
	}
	return nil
}

func gitMetadataError(e error) error {
	if errors.Is(e, errNativeStreamLimit) {
		return failure("git-output-limit", "complete Git tree or filter evidence exceeds its metadata bound")
	}
	return gitExecutionError(e)
}
