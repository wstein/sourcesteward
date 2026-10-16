// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"unicode/utf8"
)

// Registry enumeration (Spec 021 FR-003, FR-017, DD1). Pins come from the gitlinks of the HEAD tree and
// of the index; names, URLs and tracking branches come from the HEAD blob of .gitmodules, read with
// includes disabled. The staged blob and the worktree file are compared, never preferred, and a worktree
// symlink is never followed. Only the process allowlist of research.md is used here: rev-parse, ls-tree,
// ls-files and config reads.

const (
	pinsMaxSubmodules   = 256
	pinsGitmodulesLimit = 1 << 20
)

// pinsEntry is one submodule path from any source. Paths are raw bytes as recorded; PathUTF8 is false for
// a path JSON cannot carry, which pins_safe.go refuses.
type pinsEntry struct {
	Path     string
	PathUTF8 bool

	HeadCommit, IndexCommit       *string
	GitlinkInHead, GitlinkInIndex bool
	IndexConflict                 bool // a non-zero stage entry exists; IndexCommit is the stage-0 value or nil

	Name             *string // from the registration source (HEAD blob; the staged blob with an unborn HEAD)
	GitmodulesEntry  bool
	URL, Branch      *string
	Active           *bool   // nil when unknown (a submodule.active pathspec is not evaluated)
	ConfiguredGitDir *string // submodule.<name>.gitdir (Git 2.56 extensions.submodulePathConfig)
	AmbiguousName    bool    // several .gitmodules names claim this path; the first by name is used

	// StagedGitmodulesEntry marks a path that only the staged .gitmodules blob registers, so a staged
	// submodule addition is not mistaken for an orphan.
	StagedGitmodulesEntry bool

	// OrphanGitlink: a gitlink with no registration in any readable source. Never set while registration
	// is unknown (HEAD .gitmodules is a symlink, a gitlink or unparseable): no false gitlink-without-entry.
	OrphanGitlink       bool
	EntryWithoutGitlink bool
}

// pinsGitmodules describes how the three .gitmodules sources relate.
type pinsGitmodules struct {
	Source              string // "head", "staged" (unborn HEAD) or "none"
	HeadKind            string // regular, absent, symlink, gitlink, unparseable or unborn
	StagedKind          string // regular, absent, symlink, gitlink or conflict
	Worktree            string // same, different, absent, symlink, not-regular or unreadable
	StagedDiffers       bool
	Modified            bool
	RegistrationUnknown bool
}

// pinsRegistry is the enumeration result. Entries are sorted by path bytes and capped at 256; Total
// counts every path found so the caller can raise pins-limit.
type pinsRegistry struct {
	HeadCommit *string
	HeadUnborn bool
	Entries    []pinsEntry
	Total      int
	Truncated  bool
	Gitmodules pinsGitmodules
}

type pinsConfigKV struct {
	Key, Value string
	HasValue   bool
}

func parsePinsConfig(out []byte) []pinsConfigKV {
	var kvs []pinsConfigKV
	for _, rec := range bytes.Split(bytes.TrimSuffix(out, []byte{0}), []byte{0}) {
		if len(rec) == 0 {
			continue
		}
		if k, v, ok := bytes.Cut(rec, []byte("\n")); ok {
			kvs = append(kvs, pinsConfigKV{string(k), string(v), true})
		} else {
			kvs = append(kvs, pinsConfigKV{Key: string(rec)})
		}
	}
	return kvs
}

// splitSubmoduleKey splits submodule.<name>.<variable>; the name may contain dots and slashes.
func splitSubmoduleKey(key string) (name, variable string, ok bool) {
	rest, found := strings.CutPrefix(key, "submodule.")
	if !found {
		return "", "", false
	}
	i := strings.LastIndexByte(rest, '.')
	if i <= 0 || i == len(rest)-1 {
		return "", "", false
	}
	return rest[:i], rest[i+1:], true
}

func pinsParseBool(kv pinsConfigKV) *bool {
	v := strings.ToLower(kv.Value)
	var b bool
	switch {
	case !kv.HasValue, v == "true", v == "yes", v == "on", v == "1":
		b = true
	case v == "false", v == "no", v == "off", v == "0", v == "":
		b = false
	default:
		return nil
	}
	return &b
}

type pinsSection struct {
	name             string
	path, url, brnch *string
}

func pinsSections(kvs []pinsConfigKV) map[string]*pinsSection {
	m := map[string]*pinsSection{}
	for _, kv := range kvs {
		name, variable, ok := splitSubmoduleKey(kv.Key)
		if !ok || !kv.HasValue {
			continue
		}
		s := m[name]
		if s == nil {
			s = &pinsSection{name: name}
			m[name] = s
		}
		v := kv.Value
		switch variable {
		case "path":
			s.path = &v
		case "url":
			s.url = &v
		case "branch":
			s.brnch = &v
		}
	}
	return m
}

func validPinsOID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

type pinsTreeRecord struct{ mode, oid, path string }

// parsePinsTreeRecord parses `mode SP type SP oid TAB path` (ls-tree -z) and `mode SP oid SP stage TAB
// path` (ls-files --stage -z); stage is empty for ls-tree.
func parsePinsTreeRecord(rec []byte, tree bool) (r pinsTreeRecord, stage string, err error) {
	meta, path, ok := bytes.Cut(rec, []byte("\t"))
	f := strings.Fields(string(meta))
	if !ok || (tree && len(f) != 3) || (!tree && len(f) != 3) {
		return r, "", errors.New("malformed Git record")
	}
	if tree {
		r = pinsTreeRecord{f[0], f[2], string(path)}
	} else {
		r, stage = pinsTreeRecord{f[0], f[1], string(path)}, f[2]
	}
	if !validPinsOID(r.oid) {
		return r, "", errors.New("malformed object name in Git record")
	}
	return r, stage, nil
}

func pinsKindOf(mode string) string {
	switch mode {
	case "100644", "100755":
		return "regular"
	case "120000":
		return "symlink"
	case "160000":
		return "gitlink"
	}
	return "other"
}

func wantRecord(rec []byte) bool {
	if bytes.HasPrefix(rec, []byte("160000 ")) {
		return true
	}
	return bytes.HasSuffix(rec, []byte("\t.gitmodules"))
}

// pinsEnumerate reads the registry of the superproject. A *pinsLimitError means a bound was exhausted.
func pinsEnumerate(run *pinsRunner, l pinsSuperLayout) (pinsRegistry, error) {
	repo := pinsGitRepo{GitDir: l.GitDir, WorkTree: l.WorkTree}
	call := func(keep func([]byte) bool, args ...string) (pinsOutput, error) {
		return run.run(pinsCall{Repo: repo, Args: args, Keep: keep})
	}
	var reg pinsRegistry
	head, err := call(nil, "rev-parse", "--verify", "--quiet", "HEAD")
	if err != nil {
		return reg, err
	}
	switch head.Exit {
	case 0:
		oid := strings.TrimSpace(string(head.Stdout))
		if !validPinsOID(oid) {
			return reg, errors.New("unexpected HEAD object name")
		}
		reg.HeadCommit = &oid
	case 1:
		// Exit 1 is also what a corrupt ref gives. Unborn means HEAD names a branch that does not exist:
		// symbolic-ref resolves it and for-each-ref finds neither the ref nor a broken one (which it
		// reports on standard error).
		sym, err := call(nil, "symbolic-ref", "-q", "HEAD")
		if err != nil {
			return reg, err
		}
		ref := strings.TrimSpace(string(sym.Stdout))
		if sym.Exit != 0 || !strings.HasPrefix(ref, "refs/") || strings.ContainsAny(ref, "\n\x00") {
			return reg, errors.New("HEAD is neither a commit nor an unborn branch")
		}
		found, err := call(nil, "for-each-ref", "--format=%(refname)", "--", ref)
		if err != nil {
			return reg, err
		}
		if found.Exit != 0 || len(found.Stdout) != 0 || found.Stderr != "" {
			return reg, errors.New("HEAD names a branch ref that exists but cannot be resolved")
		}
		reg.HeadUnborn = true
	default:
		return reg, errors.New("cannot read the superproject HEAD")
	}

	byPath := map[string]*pinsEntry{}
	entry := func(path string) *pinsEntry {
		e := byPath[path]
		if e == nil {
			e = &pinsEntry{Path: path, PathUTF8: utf8.ValidString(path)}
			byPath[path] = e
		}
		return e
	}
	reg.Gitmodules.HeadKind, reg.Gitmodules.StagedKind = "absent", "absent"
	var headGM, stagedGM string // object names of .gitmodules
	if reg.HeadUnborn {
		reg.Gitmodules.HeadKind = "unborn"
	} else {
		out, err := call(wantRecord, "ls-tree", "-r", "-z", "--full-tree", "HEAD")
		if err != nil {
			return reg, err
		}
		if out.Exit != 0 {
			return reg, errors.New("cannot read the superproject HEAD tree")
		}
		for _, rec := range bytes.Split(bytes.TrimSuffix(out.Stdout, []byte{0}), []byte{0}) {
			if len(rec) == 0 {
				continue
			}
			r, _, err := parsePinsTreeRecord(rec, true)
			if err != nil {
				return reg, err
			}
			if r.mode == "160000" {
				e := entry(r.path)
				oid := r.oid
				e.GitlinkInHead, e.HeadCommit = true, &oid
			}
			if r.path == ".gitmodules" {
				reg.Gitmodules.HeadKind, headGM = pinsKindOf(r.mode), r.oid
			}
		}
	}
	out, err := call(wantRecord, "ls-files", "--stage", "-z")
	if err != nil {
		return reg, err
	}
	if out.Exit != 0 {
		return reg, errors.New("cannot read the superproject index")
	}
	for _, rec := range bytes.Split(bytes.TrimSuffix(out.Stdout, []byte{0}), []byte{0}) {
		if len(rec) == 0 {
			continue
		}
		r, stage, err := parsePinsTreeRecord(rec, false)
		if err != nil {
			return reg, err
		}
		if r.path == ".gitmodules" {
			if stage != "0" {
				reg.Gitmodules.StagedKind = "conflict"
			} else {
				reg.Gitmodules.StagedKind, stagedGM = pinsKindOf(r.mode), r.oid
			}
		}
		if r.mode != "160000" {
			continue
		}
		e := entry(r.path)
		if stage != "0" {
			e.IndexConflict, e.GitlinkInIndex = true, true
			continue
		}
		oid := r.oid
		e.GitlinkInIndex, e.IndexCommit = true, &oid
	}

	// Registration comes from the HEAD blob; with an unborn HEAD only the staged blob exists.
	readBlob := func(rev string) ([]pinsConfigKV, bool, error) {
		out, err := call(nil, "config", "--blob", rev, "--null", "--list", "--no-includes")
		if err != nil {
			return nil, false, err
		}
		if out.Exit != 0 {
			return nil, false, nil
		}
		return parsePinsConfig(out.Stdout), true, nil
	}
	var base []pinsConfigKV
	basePresent := false
	g := &reg.Gitmodules
	switch {
	case reg.HeadUnborn:
		g.Source = "none"
		basePresent = g.StagedKind != "absent"
		if g.StagedKind == "regular" {
			kvs, ok, err := readBlob(":.gitmodules")
			if err != nil {
				return reg, err
			}
			if ok {
				g.Source, base = "staged", kvs
			} else {
				g.RegistrationUnknown = true
			}
		} else if g.StagedKind != "absent" {
			// A staged .gitmodules that is a symlink, a gitlink or in conflict is not read.
			g.RegistrationUnknown = true
		}
	case g.HeadKind == "regular":
		g.Source, basePresent = "head", true
		kvs, ok, err := readBlob("HEAD:.gitmodules")
		if err != nil {
			return reg, err
		}
		if ok {
			base = kvs
		} else {
			g.HeadKind, g.RegistrationUnknown = "unparseable", true
		}
	case g.HeadKind == "absent":
		g.Source = "head"
	default: // symlink, gitlink: the blob is not read
		g.Source, basePresent, g.RegistrationUnknown = "head", true, true
	}
	if g.RegistrationUnknown || (!reg.HeadUnborn && g.HeadKind != "absent" && g.HeadKind != "regular") {
		g.Modified = true
	}
	// Object names compare absent as empty, so an unborn HEAD with a staged blob differs too.
	g.StagedDiffers = g.StagedKind == "conflict" || stagedGM != headGM
	var stagedKVs []pinsConfigKV
	if g.StagedKind == "regular" && g.Source != "staged" && g.StagedDiffers {
		if kvs, ok, err := readBlob(":.gitmodules"); err != nil {
			return reg, err
		} else if ok {
			stagedKVs = kvs
		}
	}
	if g.Worktree, err = pinsWorktreeGitmodules(run, l, repo, base, basePresent, !g.RegistrationUnknown); err != nil {
		return reg, err
	}
	g.Modified = g.Modified || g.StagedDiffers || g.Worktree != "same"

	// Local configuration: activity and the Git 2.56 configured gitdir.
	cfg, err := call(func(rec []byte) bool {
		k, _, _ := bytes.Cut(rec, []byte("\n"))
		key := string(k)
		_, variable, ok := splitSubmoduleKey(key)
		return ok && (variable == "url" || variable == "active" || variable == "gitdir") || key == "submodule.active"
	}, "config", "--local", "--null", "--list", "--no-includes")
	if err != nil {
		return reg, err
	}
	if cfg.Exit != 0 {
		return reg, errors.New("cannot read the superproject configuration")
	}
	local := parsePinsConfig(cfg.Stdout)
	pathspecActive := false
	hasURL, activeCfg, gitdirCfg := map[string]bool{}, map[string]*bool{}, map[string]string{}
	for _, kv := range local {
		if kv.Key == "submodule.active" {
			pathspecActive = true
			continue
		}
		name, variable, _ := splitSubmoduleKey(kv.Key)
		switch variable {
		case "url":
			if kv.HasValue && kv.Value != "" {
				hasURL[name] = true
			}
		case "active":
			activeCfg[name] = pinsParseBool(kv)
		case "gitdir":
			if kv.HasValue && kv.Value != "" {
				gitdirCfg[name] = kv.Value
			}
		}
	}

	// Entries from the registration sources.
	apply := func(sections map[string]*pinsSection, staged bool) {
		names := make([]string, 0, len(sections))
		for n := range sections {
			names = append(names, n)
		}
		sort.Strings(names)
		seen := map[string]bool{}
		for _, n := range names {
			s := sections[n]
			if s.path == nil || *s.path == "" {
				continue
			}
			if staged {
				// Only a path that already has a gitlink or a HEAD registration: a registration found
				// only in the staged blob is not an entry and does not count toward the cap.
				if e := byPath[*s.path]; e != nil {
					e.StagedGitmodulesEntry = true
				}
				continue
			}
			e := entry(*s.path)
			if seen[*s.path] {
				e.AmbiguousName = true
				continue
			}
			seen[*s.path] = true
			name := n
			e.Name, e.GitmodulesEntry, e.URL, e.Branch = &name, true, s.url, s.brnch
		}
	}
	apply(pinsSections(base), false)
	if g.Source == "staged" {
		for _, e := range byPath {
			if e.GitmodulesEntry {
				e.StagedGitmodulesEntry = true
			}
		}
	}
	apply(pinsSections(stagedKVs), true)
	for _, e := range byPath {
		if e.Name != nil {
			if v, ok := gitdirCfg[*e.Name]; ok {
				v := v
				e.ConfiguredGitDir = &v
			}
			switch {
			case activeCfg[*e.Name] != nil || hasActive(activeCfg, *e.Name):
				e.Active = activeCfg[*e.Name]
			case pathspecActive:
				e.Active = nil
			default:
				b := hasURL[*e.Name]
				e.Active = &b
			}
		}
		gitlink := e.GitlinkInHead || e.GitlinkInIndex
		e.OrphanGitlink = gitlink && !e.GitmodulesEntry && !e.StagedGitmodulesEntry && !g.RegistrationUnknown
		e.EntryWithoutGitlink = e.GitmodulesEntry && !gitlink
	}
	paths := make([]string, 0, len(byPath))
	for p := range byPath {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	reg.Total = len(paths)
	if len(paths) > pinsMaxSubmodules {
		paths, reg.Truncated = paths[:pinsMaxSubmodules], true
	}
	reg.Entries = make([]pinsEntry, 0, len(paths))
	for _, p := range paths {
		reg.Entries = append(reg.Entries, *byPath[p])
	}
	return reg, nil
}

func hasActive(m map[string]*bool, name string) bool { _, ok := m[name]; return ok }

// pinsWorktreeGitmodules compares the worktree .gitmodules with the registration baseline. It lstats the
// file, never follows a symlink, reads a regular file with a bounded no-follow open and hands the bytes
// to Git on standard input, so Git never opens the worktree path (no race and no filter).
func pinsWorktreeGitmodules(run *pinsRunner, l pinsSuperLayout, repo pinsGitRepo, base []pinsConfigKV, basePresent, baseKnown bool) (string, error) {
	info, err := pinsLstat(l.WorkTree, ".gitmodules")
	var link *pinsSymlinkError
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if !basePresent {
			return "same", nil
		}
		return "absent", nil
	case errors.As(err, &link):
		return "symlink", nil
	case err != nil:
		return "unreadable", nil
	case !info.Mode().IsRegular():
		return "not-regular", nil
	}
	data, err := pinsReadSmall(l.WorkTree, ".gitmodules", pinsGitmodulesLimit)
	if err != nil {
		return "unreadable", nil
	}
	out, err := run.run(pinsCall{Repo: repo, Args: []string{"config", "--file", "-", "--null", "--list", "--no-includes"}, Stdin: data})
	if err != nil {
		return "", err
	}
	if out.Exit != 0 {
		return "unreadable", nil
	}
	if basePresent && baseKnown && sameConfig(parsePinsConfig(out.Stdout), base) {
		return "same", nil
	}
	return "different", nil
}

func sameConfig(a, b []pinsConfigKV) bool {
	norm := func(kvs []pinsConfigKV) []string {
		s := make([]string, 0, len(kvs))
		for _, kv := range kvs {
			s = append(s, fmt.Sprintf("%s\x00%v\x00%s", kv.Key, kv.HasValue, kv.Value))
		}
		sort.Strings(s)
		return s
	}
	x, y := norm(a), norm(b)
	if len(x) != len(y) {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// pinsResolvedEntry pairs a registry entry with the outcome of validating its directories. A refusal
// refuses only this submodule; the others are still resolved.
type pinsResolvedEntry struct {
	Entry    pinsEntry
	Resolved pinsResolved
	Refusal  *pinsSafetyRefusal
}

// pinsResolveAll validates every entry independently. An unregistered gitlink has no name, so its path
// stands in for the Git directory name, as Git itself does by default. A non-nil error is operational
// (cancellation, a bound) and stops the audit.
func pinsResolveAll(s *pinsSafety, reg pinsRegistry) ([]pinsResolvedEntry, error) {
	out := make([]pinsResolvedEntry, 0, len(reg.Entries))
	for _, e := range reg.Entries {
		spec := pinsSubmoduleSpec{Path: e.Path, Name: e.Path, ConfiguredGitDir: e.ConfiguredGitDir}
		if e.Name != nil {
			spec.Name = *e.Name
		}
		res, refusal, err := s.resolve(spec)
		if err != nil {
			return nil, err
		}
		out = append(out, pinsResolvedEntry{Entry: e, Resolved: res, Refusal: refusal})
	}
	return out, nil
}
