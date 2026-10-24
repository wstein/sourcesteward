// SPDX-License-Identifier: EUPL-1.2
package derive

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/wstein/sourcesteward/internal/workspace"
)

// Issue is a refused state directory. Code is always state-dir-unsafe.
type Issue struct {
	Code   string `json:"code"`
	Reason string `json:"reason"`
}

func (i *Issue) Error() string { return i.Code + ": " + i.Reason }

func unsafe(format string, args ...any) *Issue {
	return &Issue{Code: "state-dir-unsafe", Reason: fmt.Sprintf(format, args...)}
}

// DefaultStateDir is the shared default of stw workspace and stw tidy:
// ${XDG_STATE_HOME:-~/.local/state}/stw/workspace.
func DefaultStateDir() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home := os.Getenv("HOME")
		if home == "" {
			return "", unsafe("neither XDG_STATE_HOME nor HOME is set")
		}
		base = filepath.Join(home, ".local", "state")
	}
	if !filepath.IsAbs(base) {
		return "", unsafe("XDG_STATE_HOME is not an absolute path")
	}
	return filepath.Join(base, "stw", "workspace"), nil
}

// EnvStateDir is $STW_STATE_DIR, which overrides the default for both commands.
func EnvStateDir() string { return os.Getenv("STW_STATE_DIR") }

// ResolveStateDir returns the state directory of a command: an explicit --state-dir unchanged
// (today's behaviour), else $STW_STATE_DIR, else the shared default, created 0700 and checked.
func ResolveStateDir(flag string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	dir := EnvStateDir()
	if dir == "" {
		var err error
		if dir, err = DefaultStateDir(); err != nil {
			return "", err
		}
	}
	if err := EnsureStateDir(dir); err != nil {
		return "", err
	}
	return dir, nil
}

// EnsureStateDir creates a missing state directory (and its missing parents) with mode 0700 and
// then applies the checks of a known store.
func EnsureStateDir(path string) error {
	if i := checkPath(path); i != nil {
		return i
	}
	if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
		if err = os.MkdirAll(path, 0700); err != nil {
			return unsafe("cannot create the directory: %v", err)
		}
	}
	if i := checkStore(path); i != nil {
		return i
	}
	return nil
}

// checkPath refuses a path that is not canonical and absolute, has a symlink component, or lies
// inside a repository. Components that do not exist yet are fine.
func checkPath(path string) *Issue {
	if len(path) > 4096 || !utf8.ValidString(path) || strings.ContainsRune(path, 0) {
		return unsafe("the path exceeds the text contract")
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return unsafe("the path is not a canonical absolute path")
	}
	for dir := path; ; dir = filepath.Dir(dir) {
		if st, err := os.Lstat(dir); err == nil && st.Mode()&os.ModeSymlink != 0 {
			return unsafe("a path component is a symlink")
		}
		for _, marker := range []string{".git", ".jj"} {
			if _, err := os.Lstat(filepath.Join(dir, marker)); err == nil {
				return unsafe("the directory is inside the repository at %s", dir)
			}
		}
		if parent := filepath.Dir(dir); parent == dir {
			return nil
		}
	}
}

// checkStore applies the checks of an existing state directory: a real directory, owned by the
// current user, with mode exactly 0700.
func checkStore(path string) *Issue {
	if i := checkPath(path); i != nil {
		return i
	}
	st, err := os.Lstat(path)
	switch {
	case err != nil:
		return unsafe("the directory cannot be read: %v", err)
	case !st.IsDir():
		return unsafe("the path is not a directory")
	case !ownedByCurrentUser(st):
		return unsafe("the directory is not owned by the current user")
	case st.Mode().Perm() != 0700:
		return unsafe("the directory mode is %04o, not 0700", st.Mode().Perm())
	}
	return nil
}

// Store is one known state directory of a wizard run.
type Store struct {
	Path    string
	Source  string // --state-dir, $STW_STATE_DIR or default
	Records int    // task creation records
	Issue   *Issue // set: the store is skipped
}

// Stores validates the known stores in order: the --state-dir values, $STW_STATE_DIR, the shared
// default. A repeated path counts once. An unsafe store carries its Issue and is skipped by the
// lookup; the run continues and ends with exit 1 (HasUnsafe). A missing default is not a store.
func Stores(flags []string, env, def string) []Store {
	var out []Store
	seen := map[string]bool{}
	add := func(path, source string) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		if _, err := os.Lstat(path); source == "default" && errors.Is(err, fs.ErrNotExist) {
			return
		}
		s := Store{Path: path, Source: source}
		if s.Issue = checkStore(path); s.Issue == nil {
			ids, err := workspace.TaskIDs(path)
			if err != nil {
				s.Issue = unsafe("the records cannot be read: %v", err)
			}
			s.Records = len(ids)
		}
		out = append(out, s)
	}
	for _, f := range flags {
		add(f, "--state-dir")
	}
	add(env, "$STW_STATE_DIR")
	add(def, "default")
	return out
}

// HasUnsafe reports whether a store was skipped, so that the run ends with exit 1.
func HasUnsafe(stores []Store) bool {
	for _, s := range stores {
		if s.Issue != nil {
			return true
		}
	}
	return false
}

// PlanStore is the first valid store.
func PlanStore(stores []Store) (Store, bool) {
	for _, s := range stores {
		if s.Issue == nil {
			return s, true
		}
	}
	return Store{}, false
}

// StoresDerived is the state-dirs entry of derived[]: the stores read and their record counts.
func StoresDerived(stores []Store) Derived {
	d := Derived{Name: "state-dirs", Source: "none"}
	var values, sources []string
	for _, s := range stores {
		if s.Issue != nil {
			values = append(values, fmt.Sprintf("%s (skipped: %s)", s.Path, s.Issue.Code))
		} else {
			values = append(values, fmt.Sprintf("%s (%d records)", s.Path, s.Records))
		}
		known := false
		for _, have := range sources {
			known = known || have == s.Source
		}
		if !known {
			sources = append(sources, s.Source)
		}
		d.Overridden = d.Overridden || s.Source != "default"
	}
	if len(stores) > 0 {
		d.Value, d.Source = strings.Join(values, "; "), strings.Join(sources, ", ")
	}
	return d
}

// TaskLookup is the result of the creation record lookup of one stw:<task> worktree.
type TaskLookup struct {
	Result
	Record workspace.TaskRecord
}

// LookupTask looks the creation record up by task id in every valid store. One record, or equal
// records in several stores, owns the worktree (source task-record). Differing records are
// registration-record-ambiguous, and so is a record that exists but cannot be read: its content
// is unknown, so it must never let another store's record own the worktree. None is
// registration-record-missing with a hint.
func LookupTask(stores []Store, task string) TaskLookup {
	name := "owner:" + task
	var found []workspace.TaskRecord
	var where, unreadable []string
	for _, s := range stores {
		if s.Issue != nil {
			continue
		}
		rec, ok, err := workspace.ReadTask(s.Path, task)
		switch {
		case err != nil:
			unreadable = append(unreadable, s.Path)
		case ok:
			found, where = append(found, rec), append(where, s.Path)
		}
	}
	if len(unreadable) > 0 {
		return TaskLookup{Result: ambiguous(name, "registration-record-ambiguous", task, append(where, unreadable...))}
	}
	if len(found) == 0 {
		r := ambiguous(name, "registration-record-missing", task, nil)
		r.Ambiguity.Hint = "pass the --state-dir that created it"
		return TaskLookup{Result: r}
	}
	for _, rec := range found[1:] {
		if !reflect.DeepEqual(rec, found[0]) {
			return TaskLookup{Result: ambiguous(name, "registration-record-ambiguous", task, where)}
		}
	}
	return TaskLookup{Result: Result{Derived: Derived{Name: name, Value: found[0].Owner, Source: "task-record"}}, Record: found[0]}
}

// DestinationLookup is the result of the creation record lookup of a jj workspace by its path.
// Skipped lists the ids of records that could not be read; SkippedStores counts the stores that
// could not be listed at all. Both are skipped, never fatal.
type DestinationLookup struct {
	TaskLookup
	Skipped       []string
	SkippedStores int
}

// DestinationIndex is a snapshot of the complete creation records of one backend, read from every
// valid store exactly once (Spec 041 T011a5): every lookup of a run sees the same records, however
// many workspaces are joined, and a store is never read again for a lookup. It keeps only the
// fields the join needs (task id, owner, backend, destination, state, repository directory).
type DestinationIndex struct {
	backend       string
	byDest        map[string][]indexed
	Skipped       []string // ids of records that could not be read, sorted
	SkippedStores int      // stores that could not be listed
}

type indexed struct {
	rec   workspace.TaskRecord
	store string
	repo  string
}

// IndexRecords reads the creation records of every valid store once.
func IndexRecords(stores []Store, backend string) *DestinationIndex {
	x := &DestinationIndex{backend: backend, byDest: map[string][]indexed{}, Skipped: []string{}}
	for _, s := range stores {
		if s.Issue != nil {
			continue
		}
		records, unreadable, err := workspace.ScanTasks(s.Path)
		if err != nil {
			// A store that cannot be listed is not a store without records.
			x.SkippedStores++
			continue
		}
		x.Skipped = append(x.Skipped, unreadable...)
		for _, rec := range records {
			if rec.State != "complete" || rec.Backend != backend || rec.Native.Backend != backend {
				continue
			}
			d, repo := cleanPath(rec.Destination), cleanPath(rec.Native.RepositoryDir)
			if d == "" || repo == "" {
				continue
			}
			kept := workspace.TaskRecord{TaskID: rec.TaskID, Owner: rec.Owner, Backend: rec.Backend, Destination: rec.Destination, State: rec.State,
				Native: workspace.NativeIdentity{Backend: rec.Native.Backend, RepositoryDir: rec.Native.RepositoryDir}}
			x.byDest[d] = append(x.byDest[d], indexed{rec: kept, store: s.Path, repo: repo})
		}
	}
	sort.Strings(x.Skipped)
	return x
}

// Lookup finds the record that owns the workspace at dest of the repository repoDir: the cleaned
// destination and repository directory must both match. Equal records in several stores are one
// record; differing matches are registration-record-ambiguous; none is registration-record-missing.
// An empty path owns nothing. A task id or a name never matches on its own.
func (x *DestinationIndex) Lookup(dest, repoDir string) TaskLookup {
	name := "owner-by-destination"
	d, repo := cleanPath(dest), cleanPath(repoDir)
	var found []indexed
	if d != "" && repo != "" {
		for _, e := range x.byDest[d] {
			if e.repo == repo {
				found = append(found, e)
			}
		}
	}
	if len(found) == 0 {
		return TaskLookup{Result: ambiguous(name, "registration-record-missing", dest, nil)}
	}
	var where []string
	for _, e := range found {
		where = append(where, e.store)
		if !reflect.DeepEqual(e.rec, found[0].rec) {
			return TaskLookup{Result: ambiguous(name, "registration-record-ambiguous", dest, where)}
		}
	}
	rec := found[0].rec
	return TaskLookup{Result: Result{Derived: Derived{Name: "owner:" + rec.TaskID, Value: rec.Owner, Source: "task-record"}}, Record: rec}
}

// Skips reports the ids of the records that could not be read and the number of stores that could not be listed.
func (x *DestinationIndex) Skips() ([]string, int) { return x.Skipped, x.SkippedStores }

// LookupTaskByDestination is IndexRecords followed by one Lookup (one scan per call; a run that
// joins several workspaces builds the index once instead).
func LookupTaskByDestination(stores []Store, backend, dest, repoDir string) DestinationLookup {
	x := IndexRecords(stores, backend)
	return DestinationLookup{TaskLookup: x.Lookup(dest, repoDir), Skipped: x.Skipped, SkippedStores: x.SkippedStores}
}

// cleanPath is the one canonical form paths are compared in; an empty path stays empty.
func cleanPath(p string) string {
	if p == "" {
		return ""
	}
	return filepath.Clean(p)
}
