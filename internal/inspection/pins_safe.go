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
	pinsCommondirLimit = 4096
)

// pinsDirID identifies a directory by device and inode for the later resample.
type pinsDirID struct{ Dev, Ino uint64 }

// pinsSafetyRefusal refuses one submodule; Code is a finding code of the normative table.
type pinsSafetyRefusal struct {
	Code, Message string
	limit         bool // an exhausted bound, not a verdict on the submodule: callers turn it into a limit error
}

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
	// entryLimit bounds the directory entries one validation walks; exhausting it is a limit, not a refusal.
	entryLimit int
}

func newPinsSafety(layout pinsSuperLayout, run *pinsRunner) *pinsSafety {
	return &pinsSafety{layout: layout, run: run, owner: pinsFileOwner, euid: pinsEffectiveUID(), gid: pinsFileGroup, egid: pinsEffectiveGID(), entryLimit: pinsEntryLimit}
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

// pinsBeforeOpen is a test seam run between the lstat and the open of pinsReadSmall.
var pinsBeforeOpen func()

// pinsReadSmall reads a small regular file without following a symlink and refuses larger ones.
func pinsReadSmall(base, rel string, limit int) ([]byte, error) {
	before, err := pinsLstat(base, rel)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	if pinsBeforeOpen != nil {
		pinsBeforeOpen()
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
	return s.notWritable(info, what, "the submodule")
}

// notWritable refuses a directory another user could swap contents in between validation and use. Group
// write is accepted only for the effective group (a private-group umask of 002 is common). Git creates
// every directory world-writable under umask 000, which is the usual cause.
func (s *pinsSafety) notWritable(info fs.FileInfo, what, fix string) *pinsSafetyRefusal {
	perm := info.Mode().Perm()
	if perm&0o002 != 0 {
		return &pinsSafetyRefusal{Code: "submodule-unsafe-owner", Message: "the submodule " + what + " is world-writable (mode " + fmt.Sprintf("%04o", perm) + "); Git creates such directories under umask 000, so run chmod -R o-w " + fix + " or re-clone it under a stricter umask"}
	}
	if perm&0o020 != 0 {
		if g, ok := s.gid(info); !ok || g != s.egid {
			return &pinsSafetyRefusal{Code: "submodule-unsafe-owner", Message: "the submodule " + what + " is group-writable (mode " + fmt.Sprintf("%04o", perm) + ") by a group other than the effective group; run chmod -R g-w " + fix + " or re-clone it under a stricter umask"}
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
	// Every component from the superproject root down to the submodule path must be unwritable by others.
	parts := strings.Split(spec.Path, "/")
	for i := range parts[:len(parts)-1] {
		prefix := strings.Join(parts[:i+1], "/")
		pi, err := pinsLstat(l.WorkTree, prefix)
		if err != nil {
			return pinsResolved{}, lstatRefusal("the submodule path", err), nil
		}
		if r := s.notWritable(pi, "path component "+prefix, filepath.Join(l.WorkTree, prefix)); r != nil {
			return pinsResolved{}, r, nil
		}
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
	var ginfo fs.FileInfo
	switch {
	case dinfo.IsDir():
		dirs.InPlace, dirs.GitDir, dirs.gitBase, dirs.gitRel, ginfo = true, filepath.Join(work, ".git"), l.WorkTree, dotRel, dinfo
	case dinfo.Mode().IsRegular():
		expected, refusal := s.expectedGitDir(spec)
		if refusal != nil {
			return pinsResolved{}, refusal, nil
		}
		if refusal := s.checkPointer(work, dotRel, expected); refusal != nil {
			return pinsResolved{}, refusal, nil
		}
		rel, _ := filepath.Rel(l.GitDir, expected)
		// Every component from modules down to the Git directory (a name a/b nests modules/a, and
		// submodulePathConfig can add intermediates) must be unwritable by others.
		comps := strings.Split(filepath.ToSlash(rel), "/")
		for i := range comps[:len(comps)-1] {
			prefix := strings.Join(comps[:i+1], "/")
			mi, err := pinsLstat(l.GitDir, prefix)
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					break
				}
				return pinsResolved{}, lstatRefusal("the modules directory", err), nil
			}
			if r := s.notWritable(mi, "modules path component "+prefix, l.ModulesDir); r != nil {
				return pinsResolved{}, r, nil
			}
		}
		dirs.GitDir, dirs.gitBase, dirs.gitRel = expected, l.GitDir, filepath.ToSlash(rel)
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
	if refusal := s.checkGitDirectory(dirs); refusal != nil {
		if refusal.limit {
			return pinsResolved{}, nil, &pinsLimitError{Kind: "entries"}
		}
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
// again before the first call (recheck). Git may open HEAD, config, config.worktree, index, packed-refs,
// shallow, sharedindex.*, the refs, reftable and info trees and the object store; each must be a plain
// file or directory reached without a symlink, so a FIFO, device or link can neither hang nor redirect
// Git. commondir is refused outright, and so is a non-empty alternates file (an empty one is accepted).
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
		if r := s.notWritable(info, "Git directory file "+part, "the submodule"); r != nil {
			return r
		}
	}
	budget := s.entryLimit
	top, r := s.readDir(d.gitBase, d.gitRel, "the Git directory", &budget)
	if r != nil {
		return r
	}
	for _, e := range top {
		if strings.HasPrefix(e.Name(), "sharedindex.") {
			if !e.Type().IsRegular() {
				return unsafePath("the Git directory entry %s is not a regular file", e.Name())
			}
			if r := s.entryWritable(e, "Git directory file "+e.Name()); r != nil {
				return r
			}
		}
	}
	for _, part := range []string{"refs", "reftable", "info"} {
		if r := s.walkTree(d.gitBase, within(part), part, true, &budget); r != nil {
			return r
		}
	}
	if r := s.checkObjects(d.gitBase, within("objects"), "objects", &budget); r != nil {
		return r
	}
	if _, err := pinsLstat(d.gitBase, within("commondir")); !errors.Is(err, fs.ErrNotExist) {
		return unsafePath("the Git directory has a commondir entry, which a submodule never has")
	}
	return s.checkNoAlternates(d)
}

// readDir lists a directory below base without following links; a missing directory is empty. Every
// directory it lists must pass the writable check, and the open must reach the very directory that was
// lstat'ed (O_DIRECTORY and O_NOFOLLOW, then the same file).
func (s *pinsSafety) readDir(base, rel, label string, budget *int) ([]fs.DirEntry, *pinsSafetyRefusal) {
	info, err := pinsLstat(base, rel)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, lstatRefusal(label, err)
	}
	if !info.IsDir() {
		return nil, unsafePath("%s is not a directory", label)
	}
	if r := s.notWritable(info, "Git directory entry "+label, "the submodule"); r != nil {
		return nil, r
	}
	if pinsBeforeOpen != nil {
		pinsBeforeOpen()
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return nil, unsafePath("%s cannot be opened safely", label)
	}
	defer root.Close()
	f, err := root.OpenFile(rel, os.O_RDONLY|pinsOpenFlags|pinsDirFlags, 0)
	if err != nil {
		return nil, unsafePath("%s cannot be read safely", label)
	}
	defer f.Close()
	if after, err := f.Stat(); err != nil || !os.SameFile(info, after) {
		return nil, unsafePath("%s changed while opening", label)
	}
	entries, err := f.ReadDir(*budget + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, unsafePath("%s cannot be read safely", label)
	}
	*budget -= len(entries)
	if *budget < 0 {
		return nil, &pinsSafetyRefusal{Message: label + " has too many entries to validate", limit: true}
	}
	return entries, nil
}

// walkTree requires rel to be a directory (when present) whose entries are regular files or, when
// recursive, directories walked the same way. With recursive false a directory entry is refused.
func (s *pinsSafety) walkTree(base, rel, label string, recursive bool, budget *int) *pinsSafetyRefusal {
	entries, r := s.readDir(base, rel, "the Git directory entry "+label, budget)
	if r != nil {
		return r
	}
	for _, e := range entries {
		switch {
		case e.IsDir() && recursive:
			if r := s.walkTree(base, rel+"/"+e.Name(), label+"/"+e.Name(), true, budget); r != nil {
				return r
			}
		case e.Type().IsRegular():
			if r := s.entryWritable(e, "Git directory file "+label+"/"+e.Name()); r != nil {
				return r
			}
		default:
			return unsafePath("the Git directory entry %s/%s is a symlink, a directory where a file belongs or a special file", label, e.Name())
		}
	}
	return nil
}

// checkObjects validates the object store the way Git will read it: info and pack are walked fully, a
// fan-out directory must hold only regular files, and objects/ itself holds directories only.
func (s *pinsSafety) checkObjects(base, rel, label string, budget *int) *pinsSafetyRefusal {
	entries, r := s.readDir(base, rel, "the Git directory entry "+label, budget)
	if r != nil {
		return r
	}
	for _, e := range entries {
		switch {
		case e.IsDir() && (e.Name() == "info" || e.Name() == "pack"):
			if r := s.walkTree(base, rel+"/"+e.Name(), label+"/"+e.Name(), true, budget); r != nil {
				return r
			}
		case e.IsDir():
			if r := s.walkTree(base, rel+"/"+e.Name(), label+"/"+e.Name(), false, budget); r != nil {
				return r
			}
		case e.Type().IsRegular():
			return unsafePath("the Git directory objects entry %s is not a directory", e.Name())
		default:
			return unsafePath("the Git directory entry %s/%s is a symlink or special file", label, e.Name())
		}
	}
	return nil
}

// entryWritable applies the write-bit check to a walked regular file.
func (s *pinsSafety) entryWritable(e fs.DirEntry, what string) *pinsSafetyRefusal {
	fi, err := e.Info()
	if err != nil {
		return unsafePath("%s cannot be inspected", what)
	}
	return s.notWritable(fi, what, "the submodule")
}

// checkNoAlternates refuses any non-empty objects/info/alternates. Git resolves each entry by realpath
// (a symlinked ".." component leaves the tree lexically checked) and trims no carriage return; the audit
// will not re-implement those semantics, so alternates are unsupported for pins. An empty file has no
// entries and is accepted.
func (s *pinsSafety) checkNoAlternates(d pinsSubmoduleDirs) *pinsSafetyRefusal {
	info, err := pinsLstat(d.gitBase, d.gitRel+"/objects/info/alternates")
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	const msg = "the Git directory has an objects/info/alternates entry, which is not supported for pins (Git resolves entries by realpath, which the audit does not reimplement)"
	if err != nil || !info.Mode().IsRegular() || info.Size() > 0 {
		return unsafePath("%s", msg)
	}
	return s.notWritable(info, "Git directory file objects/info/alternates", "the submodule")
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
		if r.limit {
			return &pinsLimitError{Kind: "entries"}
		}
		return errors.New("submodule Git directory changed: " + r.Message)
	}
	return nil
}
