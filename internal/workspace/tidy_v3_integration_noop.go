// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
)

// mergeNoopResult is the outcome of the merge-noop class (FR-004, FR-010a).
type mergeNoopResult struct {
	Proven  bool
	Reasons []string
	Limit   *integrationLimitError
}

// mergeNoop evaluates the class. The pre-checks never run a helper: only when
// no merge-driver, gitlink-change, merge-attribute or attribute-source reason
// applies does `merge-tree` run (it ran a configured merge driver when
// measured), with the attribute source pinned to the base tree.
func (r *integrationRun) mergeNoop(ctx context.Context, base, tip, mergeBase, tipTree, mbTree, baseTree string) (mergeNoopResult, error) {
	var res mergeNoopResult
	fail := func(err error) (mergeNoopResult, error) {
		if errors.Is(err, errIntegrationLimit) {
			var le *integrationLimitError
			errors.As(err, &le)
			return mergeNoopResult{Reasons: []string{"proof-limit"}, Limit: le}, nil
		}
		return mergeNoopResult{}, err
	}
	if tipTree == mbTree {
		return res, nil // nothing to merge: no-net-change territory
	}
	gate, err := r.mergeGate(ctx, base, tip, mergeBase, tipTree, mbTree, baseTree)
	if err != nil {
		return fail(err)
	}
	res.Reasons = append(res.Reasons, gate...)
	if len(res.Reasons) > 0 {
		return res, nil
	}
	tree, code, err := r.mergeTree(ctx, baseTree, base, tip)
	if err != nil {
		return fail(err)
	}
	switch {
	case code == 1:
		res.Reasons = append(res.Reasons, "conflict")
	case tree == baseTree:
		res.Proven = true
	default:
		res.Reasons = append(res.Reasons, "would-add-content")
	}
	return res, nil
}

// mergeGate lists the reasons that forbid running merge-tree (the merge gate of
// FR-033): a configured merge driver, info/attributes, a gitlink difference or a
// merge attribute. It runs no helper. The review report asks the same question.
func (r *integrationRun) mergeGate(ctx context.Context, base, tip, mergeBase, tipTree, mbTree, baseTree string) ([]string, error) {
	var reasons []string
	if tidyV3HasConfigClass(r.notes, "merge-driver") {
		reasons = append(reasons, "merge-driver")
	}
	if tidyV3HasConfigClass(r.notes, "attribute-source") {
		reasons = append(reasons, "attribute-source")
	}
	for _, side := range []string{tip, base} {
		gitlink, err := r.gitlinkDiffers(ctx, tipTree, mergeBase, side)
		if err != nil {
			return nil, err
		}
		if gitlink {
			reasons = append(reasons, "gitlink-change")
			break
		}
	}
	mergeAttribute, err := r.mergeAttributes(ctx, baseTree, tipTree, mbTree)
	if err != nil {
		return nil, err
	}
	if mergeAttribute {
		reasons = append(reasons, "merge-attribute")
	}
	return reasons, nil
}

// mergeTree runs the merge in the private object directory. Exit 0 prints the
// result tree alone; exit 1 (conflicts) prints it first, then a listing that
// counts only against the byte bound; any other exit is a limit.
func (r *integrationRun) mergeTree(ctx context.Context, attrTree, base, tip string) (string, int, error) {
	var tree string
	extra := false
	code, err := r.run(ctx, integrationCommand{attrTree: attrTree, args: []string{"merge-tree", "--write-tree", "--no-messages", base, tip}, tolerate: true, limit: specMergeTree.bytes(r.width)}, func(out io.Reader) error {
		reader := bufio.NewReaderSize(out, 64*1024)
		line, e := reader.ReadSlice('\n')
		if e != nil || !specMergeTree.check(line[:len(line)-1], r.width) {
			return errIntegrationLimit
		}
		tree = string(line[:len(line)-1])
		n, e := io.Copy(io.Discard, reader)
		extra = n > 0
		return e
	})
	if err == nil && (code != 0 && code != 1 || code == 0 && extra) {
		err = errIntegrationLimit
	}
	return tree, code, err
}

// gitlinkDiffers reports whether a mode-160000 path differs between the merge
// base and side (either side of the raw diff).
func (r *integrationRun) gitlinkDiffers(ctx context.Context, attrTree, mergeBase, side string) (bool, error) {
	out, err := r.lines(ctx, integrationCommand{attrTree: attrTree, args: []string{"diff-tree", "-r", "--raw", "--no-renames", "--no-abbrev", mergeBase, side}}, specRaw)
	if err != nil {
		return false, err
	}
	for _, line := range out {
		if line[1:7] == "160000" || line[8:14] == "160000" {
			return true, nil
		}
	}
	return false, nil
}

// mergeAttributes scans the .gitattributes files of the trees (at most 64 files
// of at most 1 MiB per tree, 1,024 tree records) for any use of merge or binary.
func (r *integrationRun) mergeAttributes(ctx context.Context, trees ...string) (bool, error) {
	var contents [][]byte
	seenTree, seenBlob := map[string]bool{}, map[string][]byte{}
	for _, tree := range trees {
		if seenTree[tree] {
			continue
		}
		seenTree[tree] = true
		entries, err := r.lines(ctx, integrationCommand{args: []string{"ls-tree", "-r", "-z", tree}}, specLsTree)
		if err != nil {
			return false, err
		}
		var blobs []string
		for _, entry := range entries {
			meta, path, _ := strings.Cut(entry, "\t")
			fields := strings.Fields(meta)
			if fields[0] == "120000" || fields[1] != "blob" || path != ".gitattributes" && !strings.HasSuffix(path, "/.gitattributes") {
				continue
			}
			blobs = append(blobs, fields[2])
		}
		if len(blobs) > integrationAttributeFiles {
			return false, errIntegrationLimit
		}
		var missing []string
		for _, oid := range blobs {
			if _, ok := seenBlob[oid]; !ok && !slices.Contains(missing, oid) {
				missing = append(missing, oid)
			}
		}
		if len(missing) > 0 {
			got, err := r.catFiles(ctx, missing)
			if err != nil {
				return false, err
			}
			for oid, data := range got {
				seenBlob[oid] = data
			}
		}
		for _, oid := range blobs {
			contents = append(contents, seenBlob[oid])
		}
	}
	return scanMergeAttributes(contents), nil
}

// catFiles reads blobs through `cat-file --batch` (at most 64 files of 1 MiB).
func (r *integrationRun) catFiles(ctx context.Context, oids []string) (map[string][]byte, error) {
	var got map[string][]byte
	limit := int64(len(oids)) * (integrationAttributeLimit + 256)
	_, err := r.run(ctx, integrationCommand{args: []string{"cat-file", "--batch"}, stdin: bytes.NewReader(joinRecords(oids, '\n')), limit: limit}, func(out io.Reader) error {
		var e error
		got, e = parseCatFileBatch(out, oids, r.width)
		return e
	})
	if err != nil {
		return nil, err
	}
	return got, nil
}

// parseCatFileBatch reads `OID blob SIZE LF CONTENT LF` for every requested
// object, in order. A missing or non-blob object, a size over 1 MiB, short
// content or an unrequested object is errIntegrationLimit.
func parseCatFileBatch(out io.Reader, oids []string, width int) (map[string][]byte, error) {
	reader := bufio.NewReaderSize(out, 64*1024)
	got := map[string][]byte{}
	for _, want := range oids {
		header, err := reader.ReadSlice('\n')
		if err != nil {
			return nil, errIntegrationLimit
		}
		fields := strings.Fields(string(header))
		if len(fields) != 3 || fields[0] != want || fields[1] != "blob" || !hexExact([]byte(fields[0]), width) {
			return nil, errIntegrationLimit
		}
		size, err := strconv.Atoi(fields[2])
		if err != nil || size < 0 || size > integrationAttributeLimit {
			return nil, errIntegrationLimit
		}
		data := make([]byte, size+1)
		if _, err = io.ReadFull(reader, data); err != nil || data[size] != '\n' {
			return nil, errIntegrationLimit
		}
		got[want] = data[:size]
	}
	var tail [1]byte
	if n, _ := reader.Read(tail[:]); n != 0 {
		return nil, errIntegrationLimit
	}
	return got, nil
}

// scanMergeAttributes is the conservative token scan of FR-010a: any attribute
// list, macro definition included, that contains the token merge in any form
// (merge, -merge, !merge, merge=value) or binary, in any of the files.
func scanMergeAttributes(files [][]byte) bool {
	for _, data := range files {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			var attrs []string
			switch {
			case strings.HasPrefix(line, "[attr]"):
				// [attr]NAME ATTRIBUTES...: the macro name is not an attribute list.
				fields := strings.Fields(strings.TrimPrefix(line, "[attr]"))
				if len(fields) > 0 {
					attrs = fields[1:]
				}
			case strings.HasPrefix(line, `"`):
				// A quoted pattern ends at the first unescaped quote.
				end := 1
				for end < len(line) && line[end] != '"' {
					if line[end] == '\\' {
						end++
					}
					end++
				}
				if end < len(line) {
					attrs = strings.Fields(line[end+1:])
				}
			default:
				if fields := strings.Fields(line); len(fields) > 0 {
					attrs = fields[1:]
				}
			}
			for _, token := range attrs {
				name := strings.TrimLeft(token, "-!")
				name, _, _ = strings.Cut(name, "=")
				if name = strings.ToLower(name); name == "merge" || name == "binary" {
					return true
				}
			}
		}
	}
	return false
}
