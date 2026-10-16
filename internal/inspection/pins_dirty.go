// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"errors"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Dirty state from metadata, never from content (Spec 021 FR-005, DD5, T012).
//
// `git status` can run clean and process filters on stat-dirty entries, so it is not used. Tracked state is
// decided from three sources that never read a file: `diff-index --cached` (the index against the HEAD
// tree, object names only), the index's recorded stat data from `ls-files --stage --debug -z`, streamed and
// compared with lstat here, and a bounded `ls-files --others` for untracked presence. Whatever the
// metadata cannot prove is `unknown` with a reason; the answer is never a guess.

// Entry flags as printed by `ls-files --debug` (the in-memory ce_flags).
const (
	pinsFlagAssumeUnchanged = 0x8000
	pinsFlagIntentToAdd     = 0x20000000
	pinsFlagSkipWorktree    = 0x40000000
)

// pinsIndexEntry is one index entry with its recorded stat data. The stat fields are what Git stores: 32-bit
// values, so they are compared modulo 2^32.
type pinsIndexEntry struct {
	Mode, OID                                string
	Stage                                    int
	Path                                     string
	CtimeSec, CtimeNsec, MtimeSec, MtimeNsec int64
	Dev, Ino, UID, GID, Size, Flags          uint32
}

// pinsStatData is an lstat result reduced to the fields an index entry records.
type pinsStatData struct {
	Mode                                     fs.FileMode
	Size                                     int64
	CtimeSec, CtimeNsec, MtimeSec, MtimeNsec int64
	Dev, Ino, UID, GID                       uint64
}

// pinsCompareConfig carries the repository settings the comparison depends on.
type pinsCompareConfig struct {
	FileMode, Symlinks            bool   // core.fileMode and core.symlinks
	IndexMtimeSec, IndexMtimeNsec int64  // modification time of the index file
	EmptyBlob                     string // object name of the empty blob in this repository's hash
}

func pinsCompareEntry(e pinsIndexEntry, st *pinsStatData, c pinsCompareConfig) (verdict, reason string) {
	unknown := func(r string) (string, string) { return "unknown", r }
	present := func(r string) (string, string) { return "present", r }
	switch {
	case e.Flags&pinsFlagSkipWorktree != 0:
		return unknown("skip-worktree entry")
	case e.Flags&pinsFlagAssumeUnchanged != 0:
		return unknown("assume-unchanged entry")
	case e.Flags&pinsFlagIntentToAdd != 0:
		return unknown("intent-to-add entry")
	case e.Stage != 0:
		return unknown("entry with a conflict stage")
	case e.Mode == "160000":
		return unknown("gitlink entry")
	}
	if st == nil {
		return present("tracked file is missing")
	}
	isRegular, isLink := st.Mode.IsRegular(), st.Mode&fs.ModeSymlink != 0
	switch e.Mode {
	case "100644", "100755":
		if !isRegular {
			return present("file type or mode changed")
		}
		if c.FileMode && (e.Mode == "100755") != (st.Mode.Perm()&0o100 != 0) {
			return present("file type or mode changed")
		}
	case "120000":
		// Without core.symlinks Git stores a link as a regular file holding its target.
		if !isLink && !(isRegular && !c.Symlinks) {
			return present("file type or mode changed")
		}
	default:
		return unknown("entry with an unrecognized mode")
	}
	if e.Size == 0 && e.OID != c.EmptyBlob {
		return unknown("size-0 smudged entry (racily clean when the index was written)")
	}
	if uint32(st.Size) != e.Size {
		return present("size differs")
	}
	var differs []string
	if uint32(st.MtimeSec) != uint32(e.MtimeSec) || st.MtimeNsec != e.MtimeNsec {
		differs = append(differs, "modification time")
	}
	if uint32(st.CtimeSec) != uint32(e.CtimeSec) || st.CtimeNsec != e.CtimeNsec {
		differs = append(differs, "change time")
	}
	if uint32(st.Ino) != e.Ino {
		differs = append(differs, "inode")
	}
	if uint32(st.Dev) != e.Dev {
		differs = append(differs, "device")
	}
	if uint32(st.UID) != e.UID || uint32(st.GID) != e.GID {
		differs = append(differs, "owner")
	}
	if len(differs) > 0 {
		return unknown("only the " + strings.Join(differs, ", ") + " differs; content is never read")
	}
	es, is := uint32(e.MtimeSec), uint32(c.IndexMtimeSec)
	if es > is || es == is && e.MtimeNsec >= c.IndexMtimeNsec {
		return unknown("racily clean entry (not older than the index file)")
	}
	return "absent", ""
}

// pinsIndexParser reads the `ls-files --stage --debug -z` stream. Git documents the --debug output as free
// to change, so the parser accepts exactly the known shape and fails closed: any deviation is an error and
// the caller reports unknown, never a guess. Per entry the stream is `mode SP oid SP stage TAB path NUL`
// followed by five fixed lines (ctime, mtime, dev and ino, uid and gid, size and flags); because the path
// ends with the NUL, the next NUL-terminated record is a block and the following header.
type pinsIndexParser struct {
	header *pinsIndexEntry
	broken error
}

// pinsParseError marks output that is not the known shape, as opposed to a failed process.
type pinsParseError struct{ msg string }

func (e *pinsParseError) Error() string { return e.msg }

func parseFail(msg string) error { return &pinsParseError{msg} }

func pinsDecimal(s string, bits int, signed bool) (int64, bool) {
	body := strings.TrimPrefix(s, "-")
	if body == "" || (!signed && body != s) || strings.Trim(body, "0123456789") != "" {
		return 0, false
	}
	if bits == 32 && !signed {
		v, err := strconv.ParseUint(s, 10, 32)
		return int64(v), err == nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	return v, err == nil
}

func pinsParseTime(s string) (sec, nsec int64, ok bool) {
	a, b, found := strings.Cut(s, ":")
	if !found {
		return 0, 0, false
	}
	sec, ok1 := pinsDecimal(a, 64, true)
	nsec, ok2 := pinsDecimal(b, 64, false)
	return sec, nsec, ok1 && ok2 && nsec <= 999999999
}

func pinsParseHeader(rec []byte) (*pinsIndexEntry, error) {
	meta, path, ok := strings.Cut(string(rec), "\t")
	f := strings.Split(meta, " ")
	if !ok || path == "" || len(f) != 3 || !validPinsOID(f[1]) {
		return nil, parseFail("malformed index entry header")
	}
	switch f[0] {
	case "100644", "100755", "120000", "160000":
	default:
		return nil, parseFail("unrecognized index entry mode")
	}
	if len(f[2]) != 1 || f[2][0] < '0' || f[2][0] > '3' {
		return nil, parseFail("malformed index entry stage")
	}
	return &pinsIndexEntry{Mode: f[0], OID: f[1], Stage: int(f[2][0] - '0'), Path: path}, nil
}

// pinsParseBlock fills the stat fields from the five debug lines; the text must be exactly the block.
func pinsParseBlock(block string, e *pinsIndexEntry) error {
	lines := strings.Split(block, "\n")
	if len(lines) != 6 || lines[5] != "" {
		return parseFail("malformed index stat block")
	}
	bad := parseFail("malformed index stat block")
	var ok bool
	var v string
	if v, ok = strings.CutPrefix(lines[0], "  ctime: "); !ok {
		return bad
	}
	if e.CtimeSec, e.CtimeNsec, ok = pinsParseTime(v); !ok {
		return bad
	}
	if v, ok = strings.CutPrefix(lines[1], "  mtime: "); !ok {
		return bad
	}
	if e.MtimeSec, e.MtimeNsec, ok = pinsParseTime(v); !ok {
		return bad
	}
	pair := func(line, prefix, mid string) (string, string, bool) {
		rest, ok := strings.CutPrefix(line, prefix)
		if !ok {
			return "", "", false
		}
		a, b, ok := strings.Cut(rest, mid)
		return a, b, ok
	}
	a, b, ok := pair(lines[2], "  dev: ", "\tino: ")
	if !ok {
		return bad
	}
	dev, ok1 := pinsDecimal(a, 32, false)
	ino, ok2 := pinsDecimal(b, 32, false)
	if !ok1 || !ok2 {
		return bad
	}
	e.Dev, e.Ino = uint32(dev), uint32(ino)
	if a, b, ok = pair(lines[3], "  uid: ", "\tgid: "); !ok {
		return bad
	}
	uid, ok1 := pinsDecimal(a, 32, false)
	gid, ok2 := pinsDecimal(b, 32, false)
	if !ok1 || !ok2 {
		return bad
	}
	e.UID, e.GID = uint32(uid), uint32(gid)
	if a, b, ok = pair(lines[4], "  size: ", "\tflags: "); !ok {
		return bad
	}
	size, ok1 := pinsDecimal(a, 32, false)
	if !ok1 || b == "" || len(b) > 8 || strings.Trim(b, "0123456789abcdef") != "" {
		return bad
	}
	flags, _ := strconv.ParseUint(b, 16, 32)
	e.Size, e.Flags = uint32(size), uint32(flags)
	return nil
}

// feed consumes one record of the stream; emit receives each completed entry.
func (p *pinsIndexParser) feed(rec []byte, last bool, emit func(pinsIndexEntry) error) error {
	if p.broken != nil {
		return p.broken
	}
	fail := func(err error) error { p.broken = err; return err }
	if p.header == nil {
		if last {
			return fail(parseFail("unterminated index entry header"))
		}
		h, err := pinsParseHeader(rec)
		if err != nil {
			return fail(err)
		}
		p.header = h
		return nil
	}
	// A block is five newline-terminated lines; what follows is the next header.
	text, cut := string(rec), 0
	for i := 0; i < 5; i++ {
		n := strings.IndexByte(text[cut:], '\n')
		if n < 0 {
			return fail(parseFail("truncated index stat block"))
		}
		cut += n + 1
	}
	e := *p.header
	if err := pinsParseBlock(text[:cut], &e); err != nil {
		return fail(err)
	}
	rest := text[cut:]
	if last && rest != "" {
		return fail(parseFail("unterminated index entry header"))
	}
	if !last && rest == "" {
		return fail(parseFail("index stat block without a following header"))
	}
	p.header = nil
	if err := emit(e); err != nil {
		return err
	}
	if !last {
		h, err := pinsParseHeader([]byte(rest))
		if err != nil {
			return fail(err)
		}
		p.header = h
	}
	return nil
}

// finish reports a stream that ended inside an entry.
func (p *pinsIndexParser) finish() error {
	if p.broken != nil {
		return p.broken
	}
	if p.header != nil {
		return parseFail("index stream ended inside an entry")
	}
	return nil
}

// pinsDirtyBudget is the shared per-submodule scan budget (FR-005): exceeding it yields unknown values and
// never an incomplete result.
type pinsDirtyBudget struct {
	Entries int
	Time    time.Duration
}

var pinsDefaultDirtyBudget = pinsDirtyBudget{Entries: 20000, Time: 3 * time.Second}

const pinsBudgetReason = "dirty-scan budget exceeded (3 seconds or 20,000 entries)"

// pinsEmptyBlob is the object name of the empty blob for the hash in use.
func pinsEmptyBlob(oid string) string {
	if len(oid) == 64 {
		return "473a0f4c3be8a93681a267e3b1e9a7dcda1185436fe141f7749120a303721813"
	}
	return "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391"
}

func pinsIndexPathProblem(p string) string {
	if p == "" || strings.ContainsRune(p, 0) || strings.HasPrefix(p, "/") {
		return "an index path is empty, absolute or contains a NUL"
	}
	for _, c := range strings.Split(p, "/") {
		if c == "" || c == "." || c == ".." || strings.EqualFold(c, ".git") {
			return "an index path has an empty, '.', '..' or '.git' component"
		}
	}
	return ""
}

// pinsTrackedStat lstats a tracked path below the worktree without following a symlink at any component.
// A nil result with no error means the file is not there as the index recorded it (missing, or below a
// component that is no longer a directory); dirs caches the directories already verified.
func pinsTrackedStat(root *os.Root, path string, dirs map[string]bool) (*pinsStatData, error) {
	parts := strings.Split(path, "/")
	for i := range parts[:len(parts)-1] {
		prefix := strings.Join(parts[:i+1], "/")
		ok, seen := dirs[prefix]
		if !seen {
			info, err := root.Lstat(prefix)
			switch {
			case errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR):
				ok = false
			case err != nil:
				return nil, err
			default:
				ok = info.IsDir()
			}
			dirs[prefix] = ok
		}
		if !ok {
			return nil, nil
		}
	}
	info, err := root.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR):
		return nil, nil
	case err != nil:
		return nil, err
	}
	st, ok := pinsStatOf(info)
	if !ok {
		return nil, errors.New("stat data is not available on this platform")
	}
	return &st, nil
}

func pinsHasSharedIndex(d pinsSubmoduleDirs) bool {
	root, err := os.OpenRoot(d.gitBase)
	if err != nil {
		return true
	}
	defer root.Close()
	f, err := root.OpenFile(d.gitRel, os.O_RDONLY|pinsOpenFlags|pinsDirFlags, 0)
	if err != nil {
		return true
	}
	defer f.Close()
	names, err := f.Readdirnames(pinsEntryLimit)
	if err != nil {
		return true
	}
	for _, n := range names {
		if strings.HasPrefix(n, "sharedindex.") {
			return true
		}
	}
	return false
}

type pinsReasons struct{ list []string }

func (r *pinsReasons) add(s string) {
	for _, x := range r.list {
		if x == s {
			return
		}
	}
	if len(r.list) < 8 {
		r.list = append(r.list, s)
	}
}
func (r *pinsReasons) text() string { return boundPinsMessage(strings.Join(r.list, "; ")) }

// pinsDirtyScan is one dirty-state evaluation with its shared budget.
type pinsDirtyScan struct {
	run      *pinsRunner
	d        pinsSubmoduleDirs
	facts    pinsRepoFacts
	head     pinsHead
	budget   pinsDirtyBudget
	repo     pinsGitRepo
	deadline time.Time
	entries  int
	reasons  pinsReasons

	tracked, untracked string
}

// downgrade makes an absent value unknown and records why; a present value stays present.
func (s *pinsDirtyScan) downgrade(value *string, reason string) {
	if *value == "absent" {
		*value = "unknown"
	}
	s.reasons.add(reason)
}

// call runs one streaming command within the remaining budget. A bound (time, output) comes back as a
// reason with a nil error so the caller makes its value unknown; anything else is operational.
func (s *pinsDirtyScan) call(args []string, each func([]byte, bool) error) (out pinsOutput, bound string, err error) {
	left := time.Until(s.deadline)
	if left <= 0 {
		return out, pinsBudgetReason, nil
	}
	out, err = s.run.run(pinsCall{Repo: s.repo, Args: args, Each: each, Timeout: left})
	var limit *pinsLimitError
	if errors.As(err, &limit) {
		if limit.Kind == "deadline" {
			return out, pinsBudgetReason, nil
		}
		// The scan allowance, the record length and the retained budget are bounds of the audit (FR-011),
		// not of the dirty scan: they reach the caller as pins-limit.
		return out, "", err
	}
	return out, "", err
}

// pinsDirtyState reports tracked changes and untracked presence of one validated submodule. An error is an
// operational failure (cancellation); every shortfall of the metadata is an unknown value with a reason.
func pinsDirtyState(run *pinsRunner, d pinsSubmoduleDirs, facts pinsRepoFacts, head pinsHead, b pinsDirtyBudget) (PinsDirty, error) {
	s := &pinsDirtyScan{run: run, d: d, facts: facts, head: head, budget: b, repo: pinsGitRepo{GitDir: d.GitDir, WorkTree: d.WorkTree},
		deadline: time.Now().Add(b.Time), tracked: "absent", untracked: "absent"}
	if err := s.staged(); err != nil {
		return PinsDirty{}, err
	}
	if s.tracked != "present" {
		if err := s.worktree(); err != nil {
			return PinsDirty{}, err
		}
	}
	if err := s.untrackedPresence(); err != nil {
		return PinsDirty{}, err
	}
	dirty := PinsDirty{Tracked: s.tracked, Untracked: s.untracked}
	if s.tracked == "unknown" || s.untracked == "unknown" {
		dirty.Reason = s.reasons.text()
	}
	return dirty, nil
}

// staged compares the index with the HEAD tree by object name; the first difference is enough.
func (s *pinsDirtyScan) staged() error {
	if s.head.Unborn || s.head.Commit == nil {
		s.downgrade(&s.tracked, "unborn HEAD: staged changes cannot be compared")
		return nil
	}
	found := false
	out, bound, err := s.call([]string{"diff-index", "--cached", "--no-ext-diff", "--no-textconv", "-z", *s.head.Commit, "--"}, func(rec []byte, _ bool) error {
		if len(rec) > 0 {
			found = true
			return errPinsStop
		}
		return nil
	})
	switch {
	case err != nil:
		return err
	case found:
		s.tracked = "present"
	case bound != "":
		s.downgrade(&s.tracked, bound)
	case out.Exit != 0:
		s.downgrade(&s.tracked, "the staged comparison failed")
	}
	return nil
}

// untrackedPresence needs only the first record. Walking is bounded by the time budget.
func (s *pinsDirtyScan) untrackedPresence() error {
	found := false
	out, bound, err := s.call([]string{"ls-files", "--others", "--exclude-standard", "-z"}, func(rec []byte, _ bool) error {
		if len(rec) > 0 {
			found = true
			return errPinsStop
		}
		return nil
	})
	switch {
	case err != nil:
		return err
	case found:
		s.untracked = "present"
	case bound != "":
		s.downgrade(&s.untracked, bound)
	case out.Exit != 0:
		s.downgrade(&s.untracked, "the untracked listing failed")
	}
	return nil
}

// worktree streams the index's stat data and compares each entry with lstat. The first entry that is
// certainly different ends the scan: present beats unknown.
func (s *pinsDirtyScan) worktree() error {
	idx, err := pinsLstat(s.d.gitBase, s.d.gitRel+"/index")
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		s.downgrade(&s.tracked, "the index file cannot be inspected")
		return nil
	}
	var before pinsStatData
	haveIndex := err == nil
	if haveIndex {
		var ok bool
		if before, ok = pinsStatOf(idx); !ok {
			s.downgrade(&s.tracked, "stat data is not available on this platform")
			return nil
		}
	}
	if pinsHasSharedIndex(s.d) {
		s.downgrade(&s.tracked, "split index: the write time of the shared part is unknown")
	}
	root, err := os.OpenRoot(s.d.WorkTree)
	if err != nil {
		s.downgrade(&s.tracked, "the submodule worktree cannot be opened")
		return nil
	}
	defer root.Close()
	cfg := pinsCompareConfig{FileMode: s.facts.FileMode, Symlinks: s.facts.Symlinks, IndexMtimeSec: before.MtimeSec, IndexMtimeNsec: before.MtimeNsec}
	dirs := map[string]bool{}
	parser := &pinsIndexParser{}
	ended := false // the scan stopped on its own: present found or the budget ran out
	out, bound, err := s.call([]string{"ls-files", "--stage", "--debug", "-z"}, func(rec []byte, last bool) error {
		return parser.feed(rec, last, func(e pinsIndexEntry) error {
			s.entries++
			if s.entries > s.budget.Entries || time.Now().After(s.deadline) {
				ended = true
				s.downgrade(&s.tracked, pinsBudgetReason)
				return errPinsStop
			}
			if msg := pinsIndexPathProblem(e.Path); msg != "" {
				s.downgrade(&s.tracked, msg)
				return nil
			}
			var st *pinsStatData
			if e.Flags&(pinsFlagSkipWorktree|pinsFlagAssumeUnchanged|pinsFlagIntentToAdd) == 0 && e.Stage == 0 && e.Mode != "160000" {
				var serr error
				if st, serr = pinsTrackedStat(root, e.Path, dirs); serr != nil {
					s.downgrade(&s.tracked, "a tracked file cannot be inspected: "+serr.Error())
					return nil
				}
			}
			cfg.EmptyBlob = pinsEmptyBlob(e.OID)
			switch verdict, reason := pinsCompareEntry(e, st, cfg); verdict {
			case "present":
				s.tracked, ended = "present", true
				return errPinsStop
			case "unknown":
				s.downgrade(&s.tracked, reason)
			}
			return nil
		})
	})
	var parseErr *pinsParseError
	switch {
	case errors.As(err, &parseErr):
		s.downgrade(&s.tracked, "index metadata not understood: "+parseErr.msg)
	case err != nil:
		return err
	case ended:
	case bound != "":
		s.downgrade(&s.tracked, bound)
	case out.Exit != 0:
		s.downgrade(&s.tracked, "the index could not be listed")
	default:
		if ferr := parser.finish(); ferr != nil {
			s.downgrade(&s.tracked, "index metadata not understood: "+ferr.Error())
		}
	}
	// An index rewritten during the scan makes the streamed view and the sampled write time disagree.
	if s.tracked != "present" && haveIndex {
		after, aerr := pinsLstat(s.d.gitBase, s.d.gitRel+"/index")
		if as, aok := pinsStatOf(after); aerr != nil || !aok || as.MtimeSec != before.MtimeSec || as.MtimeNsec != before.MtimeNsec || as.Size != before.Size {
			s.downgrade(&s.tracked, "the index changed during the scan")
		}
	}
	return nil
}
