// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Disclosure texts of Spec 027 (contracts/inventory-limits.md, TXT table). Tests assert them verbatim.
const (
	txtIndexLimit      = "index exceeds 100,000 entries or 8 MiB of paths; disk comparison not performed"
	txtBudget          = "content read budget of 256 MiB exhausted; remaining tracked disk content not compared"
	txtFileLimit       = "tracked disk content exceeds 16 MiB per-file observation limit"
	txtCheckoutMissing = "Git directory listed as workspace; checkout not identified from this worktree"
	txtGitlinkTrue     = "submodule disk state not observed; tracked is true from staged or ordinary-file evidence only"
	txtGitlink         = "submodule disk state excluded to avoid recursive helpers"
	txtSkipWorktree    = "skip-worktree entries not compared"
	txtSymlinks        = "core.symlinks is false; symlink entries not compared"
	txtOutsideFilter   = "disk path outside requested literal selection"
	txtRefScan         = "reference enumeration exceeds its scan bound"
)

func txtScan(scope string) string {
	return "native output exceeded the 64 MiB scan allowance; " + scope + " not observed"
}
func txtRecord(scope string) string {
	return "native output record exceeded 64 KiB; " + scope + " not observed"
}
func txtFilesTruncated(n int, lower bool) string {
	count := fmt.Sprint(n)
	if lower {
		count = "at least " + count
	}
	return "file listing truncated: " + count + " file records omitted by the 1,024-record bound; omitted paths are not clean"
}
func txtRefsTruncated(n int) string {
	return fmt.Sprintf("reference listing truncated: %d references omitted by the 1,024-record bound (tags first); predicates use all references", n)
}

const txtResultTrimmed = "listing trimmed to fit the 1 MiB result bound"

// withCause appends the operating system condition of a failed Git process start or wait (for example
// resource exhaustion under load) to a generic message. Other errors are not appended: they can carry
// repository or executable paths.
func withCause(msg string, err error) error {
	var process *processError
	if errors.As(err, &process) {
		return fmt.Errorf("%s: %s", msg, process.msg)
	}
	return errors.New(msg)
}

// nativeFormatError marks native output that is not valid (DEG-14): invalid-metadata.
type nativeFormatError struct{ msg string }

func (e *nativeFormatError) Error() string { return e.msg }

// changedDuringError marks content or state that changed while it was read: changed-repository.
type changedDuringError struct{ msg string }

func (e *changedDuringError) Error() string { return e.msg }

// streamReason turns a streaming bound failure into the disclosure text of the observation it fed.
func streamReason(err error, scope string) (string, bool) {
	var limit *streamLimitError
	if errors.As(err, &limit) {
		if limit.Kind == "record" {
			return txtRecord(scope), true
		}
		return txtScan(scope), true
	}
	return "", false
}

// scanFailure maps an error of a scan helper to a result code.
func scanFailure(e error) (string, string) {
	var format *nativeFormatError
	var changed *changedDuringError
	switch {
	case errors.As(e, &format):
		return "invalid-metadata", e.Error()
	case errors.As(e, &changed):
		return "changed-repository", e.Error()
	case errors.Is(e, errStreamUnterminated):
		return "invalid-metadata", e.Error()
	}
	return "git-inventory-failed", e.Error()
}

// ---- references (FEED-1) ----

const gitInventoryRefFormat = "--format=%(refname)%00%(objectname)%00%(upstream)%00%(symref)%00%(objecttype)%00%(*objectname)%00%(*objecttype)%00"

var errRefLimit = errors.New(txtRefScan)

// parseGitInventoryRef parses one reference record; a nil reference means a namespace the adapter ignores.
func parseGitInventoryRef(record, format string) (*InventoryReference, error) {
	if !utf8.ValidString(record) {
		return nil, &nativeFormatError{"malformed Git reference records"}
	}
	p := strings.Split(record, "\x00")
	if len(p) != 7 || !strings.HasPrefix(p[0], "refs/") || !gitInventoryOID(p[1], format) {
		return nil, &nativeFormatError{"invalid Git reference identity"}
	}
	kind := "tag"
	var remote *string
	if strings.HasPrefix(p[0], "refs/heads/") {
		kind = "local"
	} else if strings.HasPrefix(p[0], "refs/remotes/") {
		kind = "remote"
		name, _, _ := strings.Cut(strings.TrimPrefix(p[0], "refs/remotes/"), "/")
		remote = ptr(name)
	} else if !strings.HasPrefix(p[0], "refs/tags/") {
		return nil, nil
	}
	ref := InventoryReference{Name: p[0], Kind: kind, Remote: remote, Upstreams: []string{}, AddedTargets: []InventoryTarget{}, RemovedTargets: []InventoryTarget{}, Tracked: unknown("not a jj tracking relationship"), ContainsRevision: unknown("no revision selected"), MergedIntoBase: unknown("no base selected"), LocalOnly: unknown("no complete explicit remote sample selected"), Limitations: []string{}}
	oid := p[1]
	if p[4] == "tag" && p[6] == "commit" {
		if !gitInventoryOID(p[5], format) {
			return nil, &nativeFormatError{"invalid peeled Git tag"}
		}
		oid = p[5]
	}
	if p[4] == "commit" || p[4] == "tag" && p[6] == "commit" {
		ref.CommitID = ptr(oid)
		ref.AddedTargets = append(ref.AddedTargets, InventoryTarget{CommitID: oid})
	} else {
		ref.Limitations = append(ref.Limitations, "non-commit tag object; commit reachability not evaluated")
	}
	if p[2] != "" {
		ref.Upstream = ptr(p[2])
		ref.Upstreams = append(ref.Upstreams, p[2])
	}
	if kind == "local" {
		ref.Tracked = known(p[2] != "")
	}
	return &ref, nil
}

// parseGitInventoryRefs parses complete for-each-ref output (kept for tests and small inputs).
func parseGitInventoryRefs(raw, format string) ([]InventoryReference, error) {
	out := []InventoryReference{}
	for raw != "" {
		record, rest, ok := strings.Cut(raw, "\x00\n")
		if !ok {
			return nil, &nativeFormatError{"malformed Git reference records"}
		}
		raw = rest
		ref, e := parseGitInventoryRef(record, format)
		if e != nil {
			return nil, e
		}
		if ref != nil {
			out = append(out, *ref)
		}
	}
	return out, nil
}

// scanReferences streams every reference of the three namespaces (DEG-6 on overflow) and returns the
// digest of the stream used by the recheck. code is empty on success.
func (s gitInventorySession) scanReferences(dir string, keep bool) (refs []InventoryReference, digest string, code, msg string) {
	h := sha256.New()
	refs = []InventoryReference{}
	exit, _, err := s.streamCommand(dir, nil, "\x00\n", func(rec []byte) error {
		h.Write(rec)
		h.Write([]byte{0, '\n'})
		if !keep {
			return nil
		}
		ref, e := parseGitInventoryRef(string(rec), s.format)
		if e != nil {
			return e
		}
		if ref != nil {
			if len(refs) >= s.bounds.References {
				return errRefLimit
			}
			refs = append(refs, *ref)
		}
		return nil
	}, "for-each-ref", gitInventoryRefFormat, "refs/heads", "refs/remotes", "refs/tags")
	var limit *streamLimitError
	switch {
	case errors.As(err, &limit) || errors.Is(err, errRefLimit):
		return nil, "", "inventory-limit", txtRefScan
	case err != nil:
		c, m := scanFailure(err)
		if c == "git-inventory-failed" {
			m = withCause("cannot enumerate Git references", err).Error()
		}
		return nil, "", c, m
	case exit != 0:
		return nil, "", "git-inventory-failed", "cannot enumerate Git references"
	}
	return refs, fmt.Sprintf("%x", h.Sum(nil)), "", ""
}

// referenceCost is the number of returned records one reference uses.
func referenceCost(ref InventoryReference) int {
	return 1 + len(ref.AddedTargets) + len(ref.RemovedTargets) + len(ref.Upstreams)
}

// fairShare implements FS-1: how many file records and how many reference cost units are returned.
func fairShare(files, refCost, budget int) (fileKeep, refBudget int) {
	if files+refCost <= budget {
		return files, refCost
	}
	half := budget / 2
	switch {
	case files <= half:
		return files, budget - files
	case refCost <= budget-half:
		return budget - refCost, refCost
	}
	return half, budget - half
}

// truncateReferences keeps whole references, in the given (kind, name) order, while their cost fits.
func truncateReferences(refs []InventoryReference, budget int) []InventoryReference {
	used := 0
	for i, ref := range refs {
		c := referenceCost(ref)
		if used+c > budget {
			return refs[:i]
		}
		used += c
	}
	return refs
}

func totalReferenceCost(refs []InventoryReference) int {
	n := 0
	for _, ref := range refs {
		n += referenceCost(ref)
	}
	return n
}

// ---- diff records (FEED-5, FEED-7) ----

type gitDiffAccumulator struct {
	scope      string
	limitation string
	allowU     bool
	state      int
	code       byte
	first      string
}

func (a *gitDiffAccumulator) feed(rec []byte, emit func(InventoryFile) error) error {
	if !utf8.Valid(rec) {
		return &nativeFormatError{"invalid NUL-delimited native output"}
	}
	str := string(rec)
	switch a.state {
	case 0:
		if str == "" {
			return &nativeFormatError{"malformed native Git diff"}
		}
		a.code = str[0]
		switch a.code {
		case 'A', 'M', 'T', 'D', 'R', 'C':
		case 'U':
			if !a.allowU {
				return &nativeFormatError{"unknown stored Git change"}
			}
		default:
			if a.scope == "stored" {
				return &nativeFormatError{"unknown stored Git change"}
			}
			return &nativeFormatError{"unknown Git change"}
		}
		a.state = 1
	case 1:
		if a.code == 'R' || a.code == 'C' {
			a.first = str
			a.state = 2
			return nil
		}
		a.state = 0
		return emit(a.file(str, nil))
	case 2:
		a.state = 0
		old := a.first
		return emit(a.file(str, &old))
	}
	return nil
}

func (a *gitDiffAccumulator) file(path string, old *string) InventoryFile {
	f := InventoryFile{Path: path, OldPath: old, Scope: a.scope, Limitations: []string{}}
	if a.limitation != "" {
		f.Limitations = []string{a.limitation}
	}
	switch a.code {
	case 'A':
		f.Change = "added"
	case 'M', 'T':
		f.Change = "modified"
	case 'D':
		f.Change = "deleted"
	case 'R':
		f.Change = "renamed"
	case 'C':
		f.Change = "copied"
	case 'U':
		f.Change = "conflict"
	}
	return f
}

func (a *gitDiffAccumulator) done() error {
	if a.state != 0 {
		return &nativeFormatError{"malformed native Git diff"}
	}
	return nil
}

// parseGitInventoryDiff parses complete -z name-status output (kept for tests and small inputs).
func parseGitInventoryDiff(raw, scope string) ([]InventoryFile, error) {
	records, e := gitInventoryNUL(raw)
	if e != nil {
		return nil, e
	}
	out := []InventoryFile{}
	acc := gitDiffAccumulator{scope: scope, allowU: scope != "stored"}
	for _, rec := range records {
		if e := acc.feed([]byte(rec), func(f InventoryFile) error { out = append(out, f); return nil }); e != nil {
			return nil, e
		}
	}
	if e := acc.done(); e != nil {
		return nil, e
	}
	return out, nil
}

// ---- stored comparison (FEED-7) ----

type storedResult struct {
	files      []InventoryFile
	omitted    int
	lowerBound bool
	abort      string
}

func (s gitInventorySession) storedFiles(dir string, oid, base *string, paths []string) (storedResult, error) {
	res := storedResult{files: []InventoryFile{}}
	if oid == nil {
		return res, nil
	}
	parents, c, e := s.command(dir, "rev-list", "--parents", "-n", "1", *oid)
	if e != nil || c != 0 {
		return res, errors.New("cannot observe stored Git parents")
	}
	p := strings.Fields(parents)
	if len(p) == 0 || p[0] != *oid {
		return res, errors.New("invalid stored Git parents")
	}
	args := []string{"diff-tree", "--root", "--no-commit-id", "--name-status", "-r", "-z", "-M", "--no-ext-diff", "--no-textconv", *oid}
	if base == nil && len(p) > 2 {
		return res, errors.New("unknown stored comparison: multi-parent revision requires explicit --base")
	}
	if base != nil || len(p) > 1 {
		parent := ""
		if base != nil {
			parent = *base
		} else {
			parent = p[1]
		}
		if !gitInventoryOID(parent, s.format) {
			return res, errors.New("invalid stored Git parent")
		}
		args = []string{"diff", "--name-status", "-z", "-M", "--no-ext-diff", "--no-textconv", "--ignore-submodules=none", "--submodule=short", parent, *oid, "--"}
	}
	acc := gitDiffAccumulator{scope: "stored", limitation: "stored revision delta against explicit base, sole parent, or empty tree for root revisions"}
	retainedBytes := 0
	exit, _, err := s.streamCommand(dir, nil, "\x00", func(rec []byte) error {
		return acc.feed(rec, func(f InventoryFile) error {
			if !inventoryPathSelected(f.Path, paths) && (f.OldPath == nil || !inventoryPathSelected(*f.OldPath, paths)) {
				return nil
			}
			if len(res.files) >= s.bounds.Candidates || retainedBytes+len(f.Path) > s.bounds.CandidatePathBytes {
				res.omitted++
				return nil
			}
			retainedBytes += len(f.Path)
			res.files = append(res.files, f)
			return nil
		})
	}, args...)
	if reason, ok := streamReason(err, "stored"); ok {
		res.abort = reason
		res.files = []InventoryFile{}
		res.omitted = 0
		return res, nil
	}
	if err != nil {
		return res, err
	}
	if exit != 0 {
		return res, errors.New("cannot observe stored Git files")
	}
	if e := acc.done(); e != nil {
		return res, e
	}
	res.lowerBound = false
	return res, nil
}

// ---- content attributes (FEED-2, FEED-3) ----

// workspaceTransforms inspects literal attributes without running content filters. unobserved is
// non-empty when the attribute usage could not be observed within the bounds (DEG-5).
func (s gitInventorySession) workspaceTransforms(dir string, filters []string) (transforms bool, unobserved string, err error) {
	autocrlf, c, e := s.command(dir, "config", "--get", "core.autocrlf")
	if e != nil || c > 1 {
		return false, "", errors.New("cannot observe native normalization configuration")
	}
	normalizes := c == 0 && terminated(autocrlf) != "false" && terminated(autocrlf) != ""
	var paths bytes.Buffer
	entries, pathBytes, selected := 0, 0, 0
	tooLarge := false
	exit, _, serr := s.streamCommand(dir, nil, "\x00", func(rec []byte) error {
		entries++
		pathBytes += len(rec)
		if entries > s.bounds.IndexEntries || pathBytes > s.bounds.IndexPathBytes {
			tooLarge = true
			return errStreamStop
		}
		if !utf8.Valid(rec) {
			return &nativeFormatError{"invalid NUL-delimited native output"}
		}
		if inventoryPathSelected(string(rec), filters) {
			paths.Write(rec)
			paths.WriteByte(0)
			selected++
		}
		return nil
	}, "ls-files", "--cached", "-z")
	if reason, ok := streamReason(serr, "disk comparison"); ok {
		return false, reason, nil
	}
	if serr != nil {
		var format *nativeFormatError
		if errors.As(serr, &format) || errors.Is(serr, errStreamUnterminated) {
			return false, "", serr
		}
		return false, "", withCause("cannot safely observe content attribute usage", serr)
	}
	if tooLarge {
		return false, txtIndexLimit, nil
	}
	if exit != 0 {
		return false, "", errors.New("cannot safely observe content attribute usage")
	}
	if selected == 0 {
		return false, "", nil
	}
	if normalizes {
		return true, "", nil
	}
	fields := 0
	attr := ""
	decisive := false
	exit, stopped, serr := s.streamCommand(dir, bytes.NewReader(paths.Bytes()), "\x00", func(rec []byte) error {
		if !utf8.Valid(rec) {
			return &nativeFormatError{"invalid NUL-delimited native output"}
		}
		idx := fields % 3
		fields++
		switch idx {
		case 1:
			attr = string(rec)
		case 2:
			value := string(rec)
			// check-attr prints "unset" and "unspecified" both for those states and for a literal value of
			// that name, which Git passes to a driver of that name: a configured driver counts either way
			// (a driver named "unset" next to a real unset attribute is a conservative false positive).
			// The attributes consulted are those gitattributes(5) lists as changing content between the working tree
			// and the index: filter, text, eol, the legacy crlf (it converts like text), ident and
			// working-tree-encoding; binary and -text are the unset state of text and need no query. For the
			// non-filter ones a literal "unset" has no silent effect in Git:
			// it is ignored or, for an encoding, an error, never a clean comparison of transformed content.
			if attr == "filter" {
				if s.filterNames["filter."+value] {
					decisive = true
					return errStreamStop
				}
			} else if value == "unspecified" || value == "unset" {
				return nil
			} else {
				decisive = true
				return errStreamStop
			}
		}
		return nil
	}, "check-attr", "--stdin", "-z", "filter", "text", "eol", "crlf", "ident", "working-tree-encoding")
	if reason, ok := streamReason(serr, "disk comparison"); ok {
		return false, reason, nil
	}
	if serr != nil {
		var format *nativeFormatError
		if errors.As(serr, &format) || errors.Is(serr, errStreamUnterminated) {
			return false, "", serr
		}
		return false, "", withCause("cannot safely observe content attributes", serr)
	}
	if decisive || stopped {
		return true, "", nil
	}
	if exit != 0 {
		return false, "", errors.New("cannot safely observe content attributes")
	}
	if fields%3 != 0 {
		return false, "", errors.New("malformed content attribute observation")
	}
	return false, "", nil
}

// ---- workspace status (FEED-4, FEED-5, FEED-6) ----

type gitStatusOptions struct {
	dir     string
	rawDisk bool
	scopes  []string
	filters []string
	retain  bool
	// skipIndex is non-empty when the index is not read because the attribute check already failed.
	skipIndex string
	// charge is the remaining content budget of the run (first pass only); cutoff is the first pass's
	// cutoff ordinal for the second pass (-1: none).
	charge *int
	cutoff int
}

type gitStatusMeta struct {
	tracked, untracked, conflicts  bool
	submodules, selectedSubmodules bool
	rawUnknown, selectedRawUnknown string
	indexAbort                     string
	stagedAbort, untrackedAbort    string
	ignoredAbort                   string
	sparse                         bool
	outside                        bool
	omitted                        int
	lowerBound                     bool
	cutoff                         int
	signature                      string
}

type statusCollector struct {
	meta          gitStatusMeta
	out           []gitInventoryStatus
	positions     map[string]int
	retainedBytes int
	retain        bool
	filters       []string
	bounds        inventoryBounds
}

func newStatusCollector(o gitStatusOptions, b inventoryBounds) *statusCollector {
	return &statusCollector{out: []gitInventoryStatus{}, positions: map[string]int{}, retain: o.retain, filters: o.filters, bounds: b, meta: gitStatusMeta{cutoff: -1}}
}

func (c *statusCollector) flag(v gitInventoryStatus) {
	if v.kind == "tracked" && v.xy != ".." || v.kind == "conflict" {
		c.meta.tracked = true
	}
	if v.kind == "untracked" {
		c.meta.untracked = true
	}
	if v.kind == "conflict" {
		c.meta.conflicts = true
	}
	if v.submodule {
		c.meta.submodules = true
		if inventoryPathSelected(v.path, c.filters) {
			c.meta.selectedSubmodules = true
		}
	}
	if v.unknown != "" {
		c.meta.rawUnknown = v.unknown
		if inventoryPathSelected(v.path, c.filters) {
			c.meta.selectedRawUnknown = v.unknown
		}
	}
}

// full reports whether another candidate cannot be retained (LIM-8).
func (c *statusCollector) full(pathLen int) bool {
	return len(c.out) >= c.bounds.Candidates || c.retainedBytes+pathLen > c.bounds.CandidatePathBytes
}

// add records a changed entry: flags always, retention only for the inspected workspace and within LIM-8.
func (c *statusCollector) add(v gitInventoryStatus) {
	c.flag(v)
	if !c.retain || v.submodule {
		return
	}
	if c.full(len(v.path)) {
		c.meta.omitted++
		c.meta.lowerBound = true
		return
	}
	c.positions[v.path] = len(c.out)
	c.retainedBytes += len(v.path)
	c.out = append(c.out, v)
}

func (s gitInventorySession) indexRecord(rec []byte) (tag byte, mode, oid, stage, path string, err error) {
	if !utf8.Valid(rec) {
		return 0, "", "", "", "", &nativeFormatError{"invalid NUL-delimited native output"}
	}
	head, path, ok := strings.Cut(string(rec), "\t")
	p := strings.Split(head, " ")
	if !ok || len(p) != 4 || len(p[0]) != 1 || !gitInventoryOID(p[2], s.format) || len(p[1]) != 6 || len(p[3]) != 1 || p[3][0] < '0' || p[3][0] > '3' || path == "" || filepath.IsAbs(path) || strings.HasPrefix(path, "../") {
		return 0, "", "", "", "", &nativeFormatError{"malformed native Git index record"}
	}
	tag = p[0][0]
	switch {
	case tag == 'H' || tag == 'S':
		if p[3] != "0" {
			return 0, "", "", "", "", &nativeFormatError{"malformed native Git index record"}
		}
	case tag == 'M':
		if p[3] == "0" {
			return 0, "", "", "", "", &nativeFormatError{"malformed native Git index record"}
		}
	default:
		return 0, "", "", "", "", &nativeFormatError{"malformed native Git index record"}
	}
	return tag, p[1], p[2], p[3], path, nil
}

// changeStamp is a seam for the platform change time, so tests can exercise the identity checks without it.
var changeStamp = osChangeStamp

// trackedChanged reports whether a tracked file changed while it was hashed: before is the Lstat taken before
// opening, opened is the stat of the descriptor that was read and path the Lstat of the path afterwards.
// Size, modification time, file identity and (where the platform offers it) the inode change time are
// compared. The change time is set by the kernel and cannot be restored, so a rewrite that preserves size
// and modification time is still seen unless it falls into the same tick of the filesystem clock; a tool
// that rewrites a file within one tick and restores its modification time cannot be detected.
func trackedChanged(before, opened, path os.FileInfo) bool {
	if before.Size() != opened.Size() || !before.ModTime().Equal(opened.ModTime()) {
		return true
	}
	if !os.SameFile(before, path) || before.Size() != path.Size() || !before.ModTime().Equal(path.ModTime()) {
		return true
	}
	b, okB := changeStamp(before)
	o, okO := changeStamp(opened)
	p, okP := changeStamp(path)
	return okB && okO && okP && (b != o || b != p)
}

// readTrackedLink reads a symlink the comparison just found with Lstat. A failure here means the entry was
// swapped after the Lstat, which is a change during the inventory (and the error must not carry the path).
func readTrackedLink(root *os.Root, path string) (string, error) {
	link, err := root.Readlink(path)
	if err != nil {
		return "", &changedDuringError{"tracked content changed during inventory"}
	}
	return link, nil
}

// openInventoryRoot is a test seam for the workspace root open.
var openInventoryRoot = os.OpenRoot

// inventoryBeforeHash is a test seam: it runs after a tracked file was opened and before it is hashed.
var inventoryBeforeHash func(path string)

// hashDisk hashes git-style (blob header from the stat size) and checks the bytes read.
func hashDisk(ctx context.Context, f *os.File, size int64, h hash.Hash) error {
	fmt.Fprintf(h, "blob %d%c", size, 0)
	buf := make([]byte, 32<<10)
	var read int64
	for {
		if e := ctx.Err(); e != nil {
			return e
		}
		n, e := f.Read(buf)
		if n > 0 {
			read += int64(n)
			if read > size {
				return &changedDuringError{"tracked content changed during inventory"}
			}
			h.Write(buf[:n])
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
	}
	if read != size {
		return &changedDuringError{"tracked content changed during inventory"}
	}
	return nil
}

func (s gitInventorySession) workspaceStatus(o gitStatusOptions) ([]gitInventoryStatus, gitStatusMeta, error) {
	c := newStatusCollector(o, s.bounds)
	sig := sha256.New()
	dir := o.dir
	// Index pass (FEED-4): runs for flags and conflicts even when no raw comparison is requested.
	if o.skipIndex != "" {
		c.meta.indexAbort = o.skipIndex
		fmt.Fprintf(sig, "skipped:%s", o.skipIndex)
	} else {
		modeRaw, modeCode, modeErr := s.command(dir, "config", "--type=bool", "--get", "core.filemode")
		if modeErr != nil || modeCode > 1 {
			return nil, c.meta, errors.New("cannot observe native file mode semantics")
		}
		filemode := modeCode == 1 || terminated(modeRaw) == "true"
		symRaw, symCode, symErr := s.command(dir, "config", "--type=bool", "--get", "core.symlinks")
		if symErr != nil || symCode > 1 {
			return nil, c.meta, errors.New("cannot observe native symlink semantics")
		}
		symlinks := symCode == 1 || terminated(symRaw) != "false"
		var diskRoot *os.Root
		if o.rawDisk {
			var rootErr error
			diskRoot, rootErr = openInventoryRoot(dir)
			if rootErr != nil { // the error names the path: keep it out of the result
				return nil, c.meta, errors.New("cannot open the workspace for comparison")
			}
			defer diskRoot.Close()
		}
		isig := sha256.New()
		entries, pathBytes, ordinal := 0, 0, 0
		abort := ""
		lastConflict := ""
		hasLast := false
		exit, _, err := s.streamCommand(dir, nil, "\x00", func(rec []byte) error {
			if s.ctx.Err() != nil {
				return s.ctx.Err()
			}
			entries++
			if entries > s.bounds.IndexEntries {
				abort = txtIndexLimit
				return errStreamStop
			}
			isig.Write(rec)
			isig.Write([]byte{0})
			tag, mode, oid, stage, path, e := s.indexRecord(rec)
			if e != nil {
				return e
			}
			pathBytes += len(path)
			if pathBytes > s.bounds.IndexPathBytes {
				abort = txtIndexLimit
				return errStreamStop
			}
			if stage != "0" {
				if hasLast && lastConflict == path {
					return nil
				}
				lastConflict, hasLast = path, true
				c.add(gitInventoryStatus{kind: "conflict", path: path, xy: "UU"})
				return nil
			}
			ordinal++
			v := gitInventoryStatus{kind: "tracked", path: path, xy: ".."}
			if mode == "160000" {
				v.submodule = true
				c.add(v)
				return nil
			}
			if !o.rawDisk {
				return nil
			}
			if !inventoryPathSelected(path, o.filters) {
				c.meta.outside = true
				return nil
			}
			st, e := diskRoot.Lstat(path)
			if os.IsNotExist(e) {
				if tag == 'S' {
					c.meta.sparse = true
					return nil
				}
				v.xy = ".D"
				fmt.Fprintf(isig, "missing:%s", path)
				c.add(v)
				return nil
			}
			if e != nil {
				v.unknown = "tracked disk path is unreadable"
				c.add(v)
				return nil
			}
			if mode == "120000" && !symlinks {
				v.unknown = txtSymlinks
				c.add(v)
				return nil
			}
			var h hash.Hash = sha1.New()
			if s.format == "sha256" {
				h = sha256.New()
			}
			if mode == "120000" {
				if st.Mode()&os.ModeSymlink == 0 {
					v.xy = ".T"
					fmt.Fprintf(isig, "type:%s", path)
					c.add(v)
					return nil
				}
				link, e := readTrackedLink(diskRoot, path)
				if e != nil {
					return e
				}
				if len(link) > outputLimit {
					return errors.New("tracked content limit exceeded")
				}
				fmt.Fprintf(h, "blob %d%c", len(link), 0)
				h.Write([]byte(link))
			} else {
				if !st.Mode().IsRegular() {
					v.unknown = "tracked disk path has an unsupported or unsafe file type; content not read"
					c.add(v)
					return nil
				}
				size := st.Size()
				if size > int64(s.bounds.FileBytes) {
					v.unknown = txtFileLimit
					fmt.Fprintf(isig, "oversize:%s:%d:%s", path, size, st.ModTime().String())
					c.add(v)
					return nil
				}
				if o.charge != nil {
					if c.meta.cutoff >= 0 || size > int64(*o.charge) {
						if c.meta.cutoff < 0 {
							c.meta.cutoff = ordinal
						}
						v.unknown = txtBudget
						c.add(v)
						return nil
					}
					*o.charge -= int(size)
				} else if o.cutoff >= 0 && ordinal >= o.cutoff {
					v.unknown = txtBudget
					c.add(v)
					return nil
				}
				f, e := openGitInventoryDisk(diskRoot, path)
				if e != nil {
					if strings.Contains(e.Error(), "changed while opening") {
						return &changedDuringError{e.Error()}
					}
					v.unknown = "tracked disk content could not be safely opened"
					c.add(v)
					return nil
				}
				if inventoryBeforeHash != nil {
					inventoryBeforeHash(path)
				}
				e = hashDisk(s.ctx, f, size, h)
				after, statErr := f.Stat()
				f.Close()
				if e != nil {
					var changed *changedDuringError
					if s.ctx.Err() != nil || errors.As(e, &changed) {
						return e
					}
					v.unknown = "tracked disk content is unreadable"
					c.add(v)
					return nil
				}
				if statErr != nil {
					v.unknown = "tracked disk content is unreadable"
					c.add(v)
					return nil
				}
				pathAfter, pathErr := diskRoot.Lstat(path)
				if pathErr != nil || trackedChanged(st, after, pathAfter) {
					return &changedDuringError{"tracked content changed during inventory"}
				}
			}
			digest := fmt.Sprintf("%x", h.Sum(nil))
			fmt.Fprintf(isig, "%s:%s:%s", path, digest, st.Mode().String())
			if v.xy != ".T" && (digest != oid || filemode && mode != "120000" && (st.Mode()&0111 != 0) != (mode == "100755")) {
				v.xy = ".M"
			}
			if v.xy != ".." {
				c.add(v)
			}
			return nil
		}, "ls-files", "--stage", "-t", "-z")
		if reason, ok := streamReason(err, "disk comparison"); ok {
			abort = reason
			err = nil
		}
		if err != nil {
			var format *nativeFormatError
			var changed *changedDuringError
			if errors.As(err, &format) || errors.As(err, &changed) || errors.Is(err, errStreamUnterminated) || s.ctx.Err() != nil {
				return nil, c.meta, err
			}
			return nil, c.meta, withCause("cannot observe Git index", err)
		}
		if abort == "" && exit != 0 {
			return nil, c.meta, errors.New("cannot observe Git index")
		}
		if abort != "" {
			fmt.Fprintf(sig, "aborted:%s", abort)
			c = newStatusCollector(o, s.bounds)
			c.meta.indexAbort = abort
		} else {
			sig.Write(isig.Sum(nil))
		}
		if abort == "" && c.meta.outside && c.meta.rawUnknown == "" {
			c.meta.rawUnknown = txtOutsideFilter
		}
	}
	// Staged pass (FEED-5).
	{
		ssig := sha256.New()
		acc := gitDiffAccumulator{scope: "staged", allowU: true}
		exit, _, err := s.streamCommand(dir, nil, "\x00", func(rec []byte) error {
			ssig.Write(rec)
			ssig.Write([]byte{0})
			return acc.feed(rec, func(f InventoryFile) error {
				code := byte('M')
				switch f.Change {
				case "added":
					code = 'A'
				case "deleted":
					code = 'D'
				case "renamed":
					code = 'R'
				case "copied":
					code = 'C'
				case "conflict":
					code = 'U'
				}
				if i, ok := c.positions[f.Path]; ok {
					v := &c.out[i]
					v.xy = string([]byte{code, v.xy[1]})
					v.old = f.OldPath
					c.flag(*v)
					return nil
				}
				c.add(gitInventoryStatus{kind: "tracked", path: f.Path, xy: string([]byte{code, '.'}), old: f.OldPath})
				return nil
			})
		}, "diff", "--cached", "--name-status", "-z", "-M", "--no-ext-diff", "--no-textconv", "--ignore-submodules=none", "--submodule=short", "--")
		if reason, ok := streamReason(err, "staged"); ok {
			c.meta.stagedAbort = reason
			fmt.Fprintf(sig, "aborted:staged")
		} else if err != nil {
			var format *nativeFormatError
			if errors.As(err, &format) || errors.Is(err, errStreamUnterminated) || s.ctx.Err() != nil {
				return nil, c.meta, err
			}
			return nil, c.meta, withCause("cannot observe staged Git files", err)
		} else {
			if exit != 0 {
				return nil, c.meta, errors.New("cannot observe staged Git files")
			}
			sig.Write(ssig.Sum(nil))
			if e := acc.done(); e != nil {
				return nil, c.meta, e
			}
		}
	}
	// Untracked and ignored passes (FEED-6).
	for _, ignored := range []bool{false, true} {
		if ignored && (!gitInventoryHasScope(o.scopes, "ignored") || !o.retain) || !ignored && !gitInventoryHasScope(o.scopes, "untracked") {
			continue
		}
		args := []string{"--literal-pathspecs", "ls-files", "--others", "--exclude-standard", "-z"}
		kind := "untracked"
		if ignored {
			args = append(args, "--ignored")
			kind = "ignored"
		}
		if len(o.filters) > 0 {
			args = append(args, "--")
			args = append(args, o.filters...)
		}
		usig := sha256.New()
		exit, stopped, err := s.streamCommand(dir, nil, "\x00", func(rec []byte) error {
			usig.Write(rec)
			usig.Write([]byte{0})
			if !utf8.Valid(rec) {
				return &nativeFormatError{"invalid NUL-delimited native output"}
			}
			v := gitInventoryStatus{kind: kind, path: string(rec)}
			if !o.retain {
				c.flag(v)
				if kind == "untracked" {
					return errStreamStop
				}
				return nil
			}
			if c.full(len(v.path)) {
				c.flag(v)
				c.meta.omitted++
				c.meta.lowerBound = true
				return errStreamStop
			}
			c.add(v)
			return nil
		}, args...)
		if reason, ok := streamReason(err, kind); ok {
			if ignored {
				c.meta.ignoredAbort = reason
			} else {
				c.meta.untrackedAbort = reason
			}
			fmt.Fprintf(sig, "aborted:%s", kind)
			continue
		}
		if err != nil {
			var format *nativeFormatError
			if errors.As(err, &format) || errors.Is(err, errStreamUnterminated) || s.ctx.Err() != nil {
				return nil, c.meta, err
			}
			return nil, c.meta, withCause("cannot enumerate native Git untracked/ignored paths", err)
		}
		if !stopped && exit != 0 {
			return nil, c.meta, errors.New("cannot enumerate native Git untracked/ignored paths")
		}
		sig.Write(usig.Sum(nil))
	}
	c.meta.signature = fmt.Sprintf("%x", sig.Sum(nil))
	return c.out, c.meta, nil
}
