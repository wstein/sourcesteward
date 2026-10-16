// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Submodule worktree and Git-directory validation (Spec 021 FR-009, FR-010, DD2, DD3, DD11).
//
// Everything a submodule says about itself is untrusted data: its path, its .git pointer, the Git
// directory it names and the files in that directory. This file decides, without following a symlink and
// without starting a Git process against an unvalidated location, whether a submodule may be examined.
// Only after the worktree and the Git directory are confined and owned by the effective user does the
// single Git process of this file run (a config --file read of core.worktree).

const (
	pinsPointerLimit   = 4096
	pinsAlternatesMax  = 64 << 10
	pinsCommondirLimit = 4096
)

// pinsDirID identifies a directory by device and inode for the later resample.
type pinsDirID struct{ Dev, Ino uint64 }

// pinsSafetyRefusal refuses one submodule; Code is a finding code of the normative table.
type pinsSafetyRefusal struct{ Code, Message string }

func (r *pinsSafetyRefusal) Error() string { return r.Code + ": " + r.Message }

func unsafePath(format string, a ...any) *pinsSafetyRefusal {
	return &pinsSafetyRefusal{Code: "submodule-path-unsafe", Message: fmt.Sprintf(format, a...)}
}

// pinsSuperLayout is what Git itself reports about the superproject worktree in use.
type pinsSuperLayout struct {
	WorkTree   string // absolute, symlink-free
	GitDir     string // absolute, symlink-free; `<git-dir>/worktrees/<id>` in a linked worktree
	ModulesDir string // GitDir/modules, from rev-parse --git-path modules (per worktree, measured)
	PathConfig bool   // extensions.submodulePathConfig (Git 2.56)
}

// newPinsSuperLayout asks Git once, by discovery from the validated superproject root, where this
// worktree keeps its modules: a main worktree uses <git-dir>/modules and a linked worktree
// <git-dir>/worktrees/<id>/modules, and without --path-format=absolute Git prints a relative path.
func newPinsSuperLayout(run *pinsRunner, root string) (pinsSuperLayout, error) {
	root, err := filepath.Abs(root)
	if err == nil {
		root, err = filepath.EvalSymlinks(root)
	}
	if err != nil {
		return pinsSuperLayout{}, fmt.Errorf("superproject root: %w", err)
	}
	out, err := run.run(pinsCall{Dir: root, Args: []string{"rev-parse", "--path-format=absolute", "--git-dir", "--git-path", "modules"}})
	if err != nil {
		return pinsSuperLayout{}, err
	}
	lines := strings.Split(strings.TrimSuffix(string(out.Stdout), "\n"), "\n")
	if out.Exit != 0 || len(lines) != 2 || !filepath.IsAbs(lines[0]) || !filepath.IsAbs(lines[1]) {
		return pinsSuperLayout{}, errors.New("cannot read the superproject Git directory layout")
	}
	gitDir, err := filepath.EvalSymlinks(lines[0])
	if err != nil {
		return pinsSuperLayout{}, fmt.Errorf("superproject Git directory: %w", err)
	}
	if filepath.Clean(lines[1]) != filepath.Join(lines[0], "modules") {
		return pinsSuperLayout{}, errors.New("unexpected modules location reported by Git")
	}
	l := pinsSuperLayout{WorkTree: root, GitDir: gitDir, ModulesDir: filepath.Join(gitDir, "modules")}
	cfg, err := run.run(pinsCall{Repo: pinsGitRepo{GitDir: gitDir, WorkTree: root}, Args: []string{"config", "--no-includes", "--bool", "--get", "extensions.submodulePathConfig"}})
	if err != nil {
		return pinsSuperLayout{}, err
	}
	switch {
	case cfg.Exit == 0:
		l.PathConfig = strings.TrimSpace(string(cfg.Stdout)) == "true"
	case cfg.Exit != 1:
		return pinsSuperLayout{}, errors.New("cannot read extensions.submodulePathConfig")
	}
	return l, nil
}

// pinsSubmoduleSpec is the untrusted description of one submodule.
type pinsSubmoduleSpec struct {
	Path string // repository-relative path as recorded
	Name string // the .gitmodules name; the path stands in for an unregistered gitlink
	// ConfiguredGitDir is submodule.<name>.gitdir; it is consulted only when the superproject enables
	// extensions.submodulePathConfig.
	ConfiguredGitDir *string
}

// pinsSubmoduleDirs is a validated submodule worktree and Git directory.
type pinsSubmoduleDirs struct {
	WorkTree string
	GitDir   string
	InPlace  bool // old-style .git directory inside the submodule
	WorkID   pinsDirID
	GitID    pinsDirID

	workRel string // under the superproject worktree
	gitBase string // the directory gitRel is relative to
	gitRel  string
	allowed string // the tree commondir-like references may not leave: modules dir, or <work>/.git in place
}

// pinsResolved is the outcome of validating one submodule: initialized with validated directories, or
// uninitialized (no checkout), which is a valid observation and never a refusal.
type pinsResolved struct {
	Dirs          *pinsSubmoduleDirs
	Uninitialized bool
}

// pinsSafety validates submodule locations. owner and euid are seams: tests inject a foreign owner
// because an unprivileged test cannot chown.
type pinsSafety struct {
	layout pinsSuperLayout
	run    *pinsRunner
	owner  func(fs.FileInfo) (uint32, bool)
	euid   uint32
	gid    func(fs.FileInfo) (uint32, bool)
	egid   uint32
}

func newPinsSafety(layout pinsSuperLayout, run *pinsRunner) *pinsSafety {
	return &pinsSafety{layout: layout, run: run, owner: pinsFileOwner, euid: pinsEffectiveUID(), gid: pinsFileGroup, egid: pinsEffectiveGID()}
}

type pinsSymlinkError struct{ Path string }

func (e *pinsSymlinkError) Error() string { return "symlink at " + e.Path }

var errPinsNotDirectory = errors.New("not a directory")

// pinsLstat lstats base/rel one component at a time inside an os.Root and refuses a symlink at any
// component, so neither a symlinked parent nor a symlinked final component is ever followed. base is a
// trusted, already resolved directory; rel is slash separated with no empty component.
func pinsLstat(base, rel string) (fs.FileInfo, error) {
	if rel == "" || rel == "." {
		return os.Lstat(base)
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	parts := strings.Split(rel, "/")
	var info fs.FileInfo
	for i := range parts {
		prefix := strings.Join(parts[:i+1], "/")
		if info, err = root.Lstat(prefix); err != nil {
			return nil, err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return nil, &pinsSymlinkError{Path: prefix}
		}
		if i < len(parts)-1 && !info.IsDir() {
			return nil, errPinsNotDirectory
		}
	}
	return info, nil
}

// pinsReadSmall reads a small regular file without following a symlink and refuses larger ones.
func pinsReadSmall(base, rel string, limit int) ([]byte, error) {
	before, err := pinsLstat(base, rel)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.OpenFile(rel, os.O_RDONLY|pinsOpenFlags, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) {
		return nil, errors.New("changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, errors.New("larger than its limit")
	}
	return data, nil
}

// pinsComponentProblem checks one untrusted relative path; what names it in the message.
func pinsComponentProblem(what, p string) string {
	switch {
	case p == "":
		return "the submodule " + what + " is empty"
	case strings.ContainsRune(p, 0):
		return "the submodule " + what + " contains a NUL byte"
	case !utf8.ValidString(p):
		return "the submodule " + what + " is not valid UTF-8"
	case strings.HasPrefix(p, "/") || filepath.IsAbs(p):
		return "the submodule " + what + " is absolute"
	}
	for _, c := range strings.Split(p, "/") {
		switch {
		case c == "..":
			return "the submodule " + what + " contains a '..' component"
		case c == "" || c == ".":
			return "the submodule " + what + " contains an empty or '.' component"
		case strings.EqualFold(c, ".git"):
			return "the submodule " + what + " contains a '.git' component"
		}
	}
	return ""
}

func pinsConfined(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (s *pinsSafety) ownedByUser(info fs.FileInfo, what string) *pinsSafetyRefusal {
	uid, ok := s.owner(info)
	if !ok {
		return &pinsSafetyRefusal{Code: "submodule-unsafe-owner", Message: "the owner of the submodule " + what + " cannot be determined"}
	}
	if uid != s.euid {
		return &pinsSafetyRefusal{Code: "submodule-unsafe-owner", Message: "the submodule " + what + " is not owned by the effective user"}
	}
	// Another user able to write the directory could swap its contents between validation and use.
	// Group write is accepted only for the effective group (a private-group umask of 002 is common).
	perm := info.Mode().Perm()
	if perm&0o002 != 0 {
		return &pinsSafetyRefusal{Code: "submodule-unsafe-owner", Message: "the submodule " + what + " is writable by others"}
	}
	if perm&0o020 != 0 {
		if g, ok := s.gid(info); !ok || g != s.egid {
			return &pinsSafetyRefusal{Code: "submodule-unsafe-owner", Message: "the submodule " + what + " is writable by a group other than the effective group"}
		}
	}
	return nil
}

func lstatRefusal(what string, err error) *pinsSafetyRefusal {
	var link *pinsSymlinkError
	switch {
	case errors.As(err, &link):
		return unsafePath("%s goes through a symlink", what)
	case errors.Is(err, errPinsNotDirectory):
		return unsafePath("%s goes through a path component that is not a directory", what)
	}
	return unsafePath("%s cannot be inspected safely", what)
}

// resolve validates one submodule. A non-nil refusal refuses only this submodule; a non-nil error is an
// operational failure of the audit itself (cancellation, a bound, Git not starting).
func (s *pinsSafety) resolve(spec pinsSubmoduleSpec) (pinsResolved, *pinsSafetyRefusal, error) {
	if msg := pinsComponentProblem("path", spec.Path); msg != "" {
		return pinsResolved{}, unsafePath("%s", msg), nil
	}
	if msg := pinsComponentProblem("name", spec.Name); msg != "" {
		return pinsResolved{}, unsafePath("%s", msg), nil
	}
	l := s.layout
	work := filepath.Join(l.WorkTree, filepath.FromSlash(spec.Path))
	winfo, err := pinsLstat(l.WorkTree, spec.Path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return pinsResolved{Uninitialized: true}, nil, nil
	case err != nil:
		return pinsResolved{}, lstatRefusal("the submodule path", err), nil
	case !winfo.IsDir():
		return pinsResolved{}, unsafePath("the submodule path is not a directory"), nil
	}
	if r := s.ownedByUser(winfo, "worktree"); r != nil {
		return pinsResolved{}, r, nil
	}
	dotRel := spec.Path + "/.git"
	dinfo, err := pinsLstat(l.WorkTree, dotRel)
	if errors.Is(err, fs.ErrNotExist) {
		return pinsResolved{Uninitialized: true}, nil, nil
	}
	if err != nil {
		return pinsResolved{}, lstatRefusal("the submodule .git entry", err), nil
	}
	dirs := pinsSubmoduleDirs{WorkTree: work, WorkID: mustID(winfo), workRel: spec.Path}
	var allowed string
	var ginfo fs.FileInfo
	switch {
	case dinfo.IsDir():
		dirs.InPlace, dirs.GitDir, dirs.gitBase, dirs.gitRel, allowed, ginfo = true, filepath.Join(work, ".git"), l.WorkTree, dotRel, filepath.Join(work, ".git"), dinfo
	case dinfo.Mode().IsRegular():
		expected, refusal := s.expectedGitDir(spec)
		if refusal != nil {
			return pinsResolved{}, refusal, nil
		}
		if refusal := s.checkPointer(work, dotRel, expected); refusal != nil {
			return pinsResolved{}, refusal, nil
		}
		rel, _ := filepath.Rel(l.GitDir, expected)
		dirs.GitDir, dirs.gitBase, dirs.gitRel, allowed = expected, l.GitDir, filepath.ToSlash(rel), l.ModulesDir
		ginfo, err = pinsLstat(dirs.gitBase, dirs.gitRel)
		if errors.Is(err, fs.ErrNotExist) {
			return pinsResolved{}, unsafePath("the Git directory named by the .git pointer does not exist"), nil
		}
		if err != nil {
			return pinsResolved{}, lstatRefusal("the Git directory", err), nil
		}
		if !ginfo.IsDir() {
			return pinsResolved{}, unsafePath("the Git directory named by the .git pointer is not a directory"), nil
		}
	default:
		return pinsResolved{}, unsafePath("the submodule .git entry is neither a file nor a directory"), nil
	}
	if r := s.ownedByUser(ginfo, "git directory"); r != nil {
		return pinsResolved{}, r, nil
	}
	dirs.GitID = mustID(ginfo)
	dirs.allowed = allowed
	if refusal := s.checkGitDirectory(dirs); refusal != nil {
		return pinsResolved{}, refusal, nil
	}
	if refusal, err := s.checkConfiguration(dirs); refusal != nil || err != nil {
		return pinsResolved{}, refusal, err
	}
	return pinsResolved{Dirs: &dirs}, nil, nil
}

func mustID(info fs.FileInfo) pinsDirID {
	id, _ := pinsFileID(info)
	return id
}

// expectedGitDir is the only Git directory a submodule may name: the modules path Git reported for this
// worktree joined with the submodule's Git directory name, or, with extensions.submodulePathConfig, the
// configured gitdir when it stays confined under that modules path (a name with a slash is stored encoded
// as `modules/a%2fb`).
func (s *pinsSafety) expectedGitDir(spec pinsSubmoduleSpec) (string, *pinsSafetyRefusal) {
	l := s.layout
	if l.PathConfig && spec.ConfiguredGitDir != nil {
		v := *spec.ConfiguredGitDir
		if v == "" || strings.ContainsRune(v, 0) || !utf8.ValidString(v) {
			return "", unsafePath("the configured submodule gitdir is not confined under the modules directory")
		}
		if !filepath.IsAbs(v) {
			v = filepath.Join(l.WorkTree, v)
		}
		v = filepath.Clean(v)
		if !pinsConfined(l.ModulesDir, v) {
			return "", unsafePath("the configured submodule gitdir is not confined under the modules directory")
		}
		return v, nil
	}
	return filepath.Join(l.ModulesDir, filepath.FromSlash(spec.Name)), nil
}

// checkPointer reads the .git file as data and requires it to name exactly the expected Git directory.
func (s *pinsSafety) checkPointer(work, dotRel string, expected string) *pinsSafetyRefusal {
	data, err := pinsReadSmall(s.layout.WorkTree, dotRel, pinsPointerLimit)
	if err != nil {
		return unsafePath("the .git pointer file cannot be read safely")
	}
	text := strings.TrimSuffix(string(data), "\n")
	target, ok := strings.CutPrefix(text, "gitdir: ")
	switch {
	case !ok || target == "":
		return unsafePath("the .git pointer file is not a single gitdir line")
	case strings.ContainsAny(target, "\n\x00") || !utf8.ValidString(target):
		return unsafePath("the .git pointer contains a newline, NUL or invalid UTF-8")
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(work, target)
	}
	if filepath.Clean(target) != expected {
		return unsafePath("the .git pointer does not lead to the expected Git directory under the modules directory")
	}
	return nil
}

// pinsEntryLimit bounds the directory entries one validation walks.
const pinsEntryLimit = 20000

// checkGitDirectory is the content check that must pass before any Git process opens the directory and
// again before the first call (recheck). Git may open HEAD, config, config.worktree, info/*, objects/pack,
// the refs tree, the index, packed-refs and shallow; each must be a plain file or directory reached
// without a symlink, so a FIFO, device or link can neither hang nor redirect Git. commondir is refused
// outright, and every alternates entry is walked from the allowed tree without following links.
func (s *pinsSafety) checkGitDirectory(d pinsSubmoduleDirs) *pinsSafetyRefusal {
	within := func(part string) string { return d.gitRel + "/" + part }
	for _, part := range []string{"HEAD", "config", "config.worktree", "index", "packed-refs", "shallow"} {
		info, err := pinsLstat(d.gitBase, within(part))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return lstatRefusal("the Git directory entry "+part, err)
		}
		if !info.Mode().IsRegular() {
			return unsafePath("the Git directory entry %s is not a regular file", part)
		}
	}
	budget := pinsEntryLimit
	for _, part := range []string{"refs", "info", "objects/pack", "objects/info"} {
		if r := s.walkPlain(d, part, true, &budget); r != nil {
			return r
		}
	}
	// objects/: only its direct entries (fan-out directories, info, pack) are inspected.
	if r := s.walkPlain(d, "objects", false, &budget); r != nil {
		return r
	}
	if _, err := pinsLstat(d.gitBase, within("commondir")); !errors.Is(err, fs.ErrNotExist) {
		return unsafePath("the Git directory has a commondir entry, which a submodule never has")
	}
	return s.checkAlternates(d)
}

// walkPlain requires part to be a directory (when present) whose entries are regular files or
// directories, descending when recursive.
func (s *pinsSafety) walkPlain(d pinsSubmoduleDirs, part string, recursive bool, budget *int) *pinsSafetyRefusal {
	rel := d.gitRel + "/" + part
	info, err := pinsLstat(d.gitBase, rel)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return lstatRefusal("the Git directory entry "+part, err)
	}
	if !info.IsDir() {
		return unsafePath("the Git directory entry %s is not a directory", part)
	}
	root, err := os.OpenRoot(d.gitBase)
	if err != nil {
		return unsafePath("the Git directory cannot be opened safely")
	}
	defer root.Close()
	f, err := root.Open(rel)
	if err != nil {
		return unsafePath("the Git directory entry %s cannot be read safely", part)
	}
	entries, err := f.ReadDir(*budget + 1)
	f.Close()
	if err != nil && !errors.Is(err, io.EOF) {
		return unsafePath("the Git directory entry %s cannot be read safely", part)
	}
	*budget -= len(entries)
	if *budget < 0 {
		return unsafePath("the Git directory entry %s has too many entries to validate", part)
	}
	for _, e := range entries {
		switch {
		case e.IsDir():
			if recursive {
				if r := s.walkPlain(d, part+"/"+e.Name(), true, budget); r != nil {
					return r
				}
			}
		case e.Type().IsRegular():
			if part == "objects" {
				return unsafePath("the Git directory objects entry %s is not a directory", e.Name())
			}
		default:
			return unsafePath("the Git directory entry %s/%s is a symlink or special file", part, e.Name())
		}
	}
	return nil
}

// checkAlternates reads objects/info/alternates as data. Each entry must lie in the allowed tree and be
// reached from it without a symlink, be a directory owned by the effective user and carry no alternates
// of its own (nested alternates are refused rather than followed).
func (s *pinsSafety) checkAlternates(d pinsSubmoduleDirs) *pinsSafetyRefusal {
	data, err := pinsReadSmall(d.gitBase, d.gitRel+"/objects/info/alternates", pinsAlternatesMax)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return unsafePath("the Git directory alternates file cannot be read safely")
	}
	objects := filepath.Join(d.GitDir, "objects")
	for _, line := range bytes.Split(data, []byte("\n")) {
		entry := strings.TrimSuffix(string(line), "\r")
		if entry == "" || strings.HasPrefix(entry, "#") {
			continue
		}
		if len(entry) >= 2 && entry[0] == '"' && entry[len(entry)-1] == '"' {
			entry = entry[1 : len(entry)-1]
			if strings.Contains(entry, "\\") {
				return unsafePath("the Git directory alternates entry uses an escape that is not supported")
			}
		}
		if !utf8.ValidString(entry) || strings.ContainsRune(entry, 0) {
			return unsafePath("the Git directory alternates entry is not valid text")
		}
		if !filepath.IsAbs(entry) {
			entry = filepath.Join(objects, entry)
		}
		entry = filepath.Clean(entry)
		if !pinsConfined(d.allowed, entry) && entry != d.allowed {
			return unsafePath("the Git directory alternates entry leads outside the allowed tree")
		}
		rel, err := filepath.Rel(d.allowed, entry)
		if err != nil {
			return unsafePath("the Git directory alternates entry leads outside the allowed tree")
		}
		rel = filepath.ToSlash(rel)
		info, err := pinsLstat(d.allowed, rel)
		if err != nil {
			return lstatRefusal("the Git directory alternates entry", err)
		}
		if !info.IsDir() {
			return unsafePath("the Git directory alternates entry is not a directory")
		}
		if r := s.ownedByUser(info, "alternates directory"); r != nil {
			return r
		}
		if _, err := pinsLstat(d.allowed, strings.TrimSuffix(rel+"/info/alternates", "/")); !errors.Is(err, fs.ErrNotExist) {
			return unsafePath("the Git directory alternates entry has alternates of its own")
		}
	}
	return nil
}

// checkConfiguration reads the configuration files Git loads for this directory, config and
// config.worktree, with includes disabled (config --file reads no repository and runs after ownership and
// content checks). A key that imports other files is refused: Git would load them. core.worktree must
// equal the validated worktree; unset is fine, because every Git call names the worktree explicitly.
func (s *pinsSafety) checkConfiguration(d pinsSubmoduleDirs) (*pinsSafetyRefusal, error) {
	for _, name := range []string{"config", "config.worktree"} {
		path := filepath.Join(d.GitDir, name)
		if _, err := pinsLstat(d.gitBase, d.gitRel+"/"+name); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		out, err := s.run.run(pinsCall{Dir: s.layout.WorkTree, Args: []string{"config", "--file", path, "--null", "--list", "--no-includes"}, Keep: func(rec []byte) bool {
			k, _, _ := bytes.Cut(rec, []byte("\n"))
			key := strings.ToLower(string(k))
			return key == "core.worktree" || strings.HasPrefix(key, "include.") || strings.HasPrefix(key, "includeif.")
		}})
		if err != nil {
			return nil, err
		}
		if out.Exit != 0 {
			return unsafePath("the Git directory configuration %s cannot be read", name), nil
		}
		for _, kv := range parsePinsConfig(out.Stdout) {
			key := strings.ToLower(kv.Key)
			if strings.HasPrefix(key, "include.") || strings.HasPrefix(key, "includeif.") {
				return unsafePath("the Git directory configuration %s contains an include, which would load unread files", name), nil
			}
			target := kv.Value
			if !filepath.IsAbs(target) {
				target = filepath.Join(d.GitDir, target)
			}
			if !kv.HasValue || filepath.Clean(target) != d.WorkTree {
				return unsafePath("core.worktree of the submodule Git directory differs from the validated worktree"), nil
			}
		}
	}
	return nil, nil
}

// recheck verifies that both directories are still the ones that were validated: same path, same device
// and inode, still not symlinks.
func (s *pinsSafety) recheck(d pinsSubmoduleDirs) error {
	for _, c := range []struct {
		base, rel string
		id        pinsDirID
	}{{s.layout.WorkTree, d.workRel, d.WorkID}, {d.gitBase, d.gitRel, d.GitID}} {
		info, err := pinsLstat(c.base, c.rel)
		if err != nil {
			return fmt.Errorf("submodule directory changed: %w", err)
		}
		if id, ok := pinsFileID(info); !ok || id != c.id || !info.IsDir() {
			return errors.New("submodule directory was replaced")
		}
	}
	if r := s.checkGitDirectory(d); r != nil {
		return errors.New("submodule Git directory changed: " + r.Message)
	}
	return nil
}
