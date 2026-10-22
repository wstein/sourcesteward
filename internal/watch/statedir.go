// SPDX-License-Identifier: EUPL-1.2
package watch

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Errors of the state directory. The CLI maps ErrStateDirUsage and ErrInsideWorkTree to exit 2, the
// others to 1, and LockedError to 3.
var (
	ErrStateDirUsage  = errors.New("the state directory must be an absolute path")
	ErrInsideWorkTree = errors.New("the state directory must not be inside a Git work tree or a .stw directory")
	ErrStateDirMode   = errors.New("the state directory must have mode 0700")
	ErrCorruptState   = errors.New("state.json is corrupt; use --baseline to replace it")
)

// WorkspaceID is the first 16 hex digits of SHA-256 over the host and either the canonical workspace
// root or the sorted, de-duplicated, lowercased repository list (Spec 035, "State and journal layout").
func WorkspaceID(host, workspaceRoot string, repos []string) string {
	text := "stw-watch-v1\n" + host + "\n"
	if workspaceRoot != "" {
		text += "workspace:" + workspaceRoot
	} else {
		seen := map[string]bool{}
		var list []string
		for _, r := range repos {
			r = strings.ToLower(r)
			if !seen[r] {
				seen[r] = true
				list = append(list, r)
			}
		}
		sort.Strings(list)
		text += "repos:" + strings.Join(list, "\n")
	}
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])[:16]
}

// DefaultStateDir is $XDG_STATE_HOME/stw/watch/<id>, else ~/.local/state/stw/watch/<id>.
func DefaultStateDir(id string) string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "stw", "watch", id)
}

// EnsureStateDir creates dir (0700) or accepts an existing 0700 one. A directory inside a Git work tree
// (a .git directory or file in it or above it), below a .stw directory, or with a wider mode is refused;
// symlinks are resolved first and nothing is created when the answer is a refusal.
func EnsureStateDir(dir string) error {
	if !filepath.IsAbs(dir) {
		return ErrStateDirUsage
	}
	dir = filepath.Clean(dir)
	// resolve the deepest existing ancestor so a symlink into a work tree is seen
	existing, rest := dir, ""
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			break
		}
		rest = filepath.Join(filepath.Base(existing), rest)
		existing = parent
	}
	real, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return err
	}
	resolved := filepath.Join(real, rest)
	for p := resolved; ; p = filepath.Dir(p) {
		if filepath.Base(p) == ".stw" {
			return fmt.Errorf("%w: %s", ErrInsideWorkTree, resolved)
		}
		if _, err := os.Lstat(filepath.Join(p, ".git")); err == nil {
			return fmt.Errorf("%w: %s", ErrInsideWorkTree, resolved)
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	if err := os.MkdirAll(resolved, 0o700); err != nil {
		return err
	}
	st, err := os.Stat(resolved)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return fmt.Errorf("%s is not a directory", resolved)
	}
	if st.Mode().Perm() != 0o700 {
		return fmt.Errorf("%w: %s has mode %o", ErrStateDirMode, resolved, st.Mode().Perm())
	}
	return nil
}

// LockedError is returned when another watcher holds the lock of the state directory.
type LockedError struct{ PID int }

func (e *LockedError) Error() string {
	if e.PID > 0 {
		return "another stw watch (pid " + strconv.Itoa(e.PID) + ") holds the lock of this state directory"
	}
	return "another stw watch holds the lock of this state directory"
}

// writeFileAtomic writes a 0600 file: temp file, fsync, rename, fsync of the directory.
func writeFileAtomic(dir, name string, data []byte) error {
	tmp, err := os.CreateTemp(dir, name+".tmp-*")
	if err != nil {
		return err
	}
	clean := func(e error) error {
		tmp.Close()
		os.Remove(tmp.Name())
		return e
	}
	if err := tmp.Chmod(0o600); err != nil {
		return clean(err)
	}
	if _, err := tmp.Write(data); err != nil {
		return clean(err)
	}
	if err := tmp.Sync(); err != nil {
		return clean(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, name)); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func writeHeartbeat(dir string, now time.Time, pid int, mode string) error {
	return writeFileAtomic(dir, "heartbeat", []byte(now.UTC().Format(time.RFC3339)+" "+strconv.Itoa(pid)+" "+mode+"\n"))
}
