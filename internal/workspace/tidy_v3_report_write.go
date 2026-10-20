// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// The owner-named report directory and the bounded writer (FR-032): files are
// created mode 0600 exclusively without following symlinks, written to
// temporary names and renamed; nothing partial remains after a failure.

const (
	reportMarkdownName = "tidy-report.md"
	reportJSONName     = "tidy-report.json"
)

func reportOptionError(message string) error { return failure("invalid-options", message) }

// checkReportDirectory refuses, as a usage error, a directory the report must
// not use: not absolute and canonical, missing, not a real directory (a symlink
// included), already holding either target, equal to or inside any root (the
// repository, its git directory, a worktree, the workspace root, the state
// directory) or inside any repository.
func checkReportDirectory(dir string, roots []string) error {
	if !v3CanonicalPath(dir) {
		return reportOptionError("--report-dir must be an absolute canonical path.")
	}
	st, err := os.Lstat(dir)
	if err != nil || !st.IsDir() {
		return reportOptionError("--report-dir must be an existing directory and not a symbolic link.")
	}
	if real, rerr := filepath.EvalSymlinks(dir); rerr != nil || real != dir {
		return reportOptionError("--report-dir must not contain symbolic links.")
	}
	for _, name := range []string{reportMarkdownName, reportJSONName} {
		if _, err = os.Lstat(filepath.Join(dir, name)); err == nil || !errors.Is(err, os.ErrNotExist) {
			return reportOptionError("--report-dir already holds " + name + ".")
		}
	}
	for _, root := range roots {
		if root != "" && (dir == root || strings.HasPrefix(dir, strings.TrimSuffix(root, string(filepath.Separator))+string(filepath.Separator))) {
			return reportOptionError("--report-dir must not be inside a repository, worktree, git directory, workspace root or the state directory.")
		}
	}
	for p := dir; ; p = filepath.Dir(p) {
		if _, err = os.Lstat(filepath.Join(p, ".git")); err == nil {
			return reportOptionError("--report-dir must not be inside a repository.")
		}
		if filepath.Dir(p) == p {
			return nil
		}
	}
}

// reportFileOps are the file operations of the writer; tests replace one at a
// time to make a step fail.
type reportFileOps struct {
	tempName func(dir string) string
	create   func(path string) (*os.File, error)
	write    func(f *os.File, data []byte) error
	rename   func(from, to string) error
	remove   func(path string) error
}

func defaultReportFileOps() reportFileOps {
	return reportFileOps{
		tempName: func(dir string) string {
			var b [8]byte
			if _, err := rand.Read(b[:]); err != nil {
				panic(err)
			}
			return filepath.Join(dir, ".tidy-report-"+hex.EncodeToString(b[:])+".tmp")
		},
		create: func(path string) (*os.File, error) {
			return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|noFollow, 0o600)
		},
		write: func(f *os.File, data []byte) error {
			if _, err := f.Write(data); err != nil {
				return err
			}
			return f.Sync()
		},
		rename: os.Rename,
		remove: os.Remove,
	}
}

// writeReportFiles writes both files or neither.
func writeReportFiles(dir string, markdown, jsonData []byte, ops reportFileOps) (err error) {
	targets := []string{filepath.Join(dir, reportMarkdownName), filepath.Join(dir, reportJSONName)}
	for _, target := range targets {
		if _, serr := os.Lstat(target); serr == nil || !errors.Is(serr, os.ErrNotExist) {
			return reportOptionError("--report-dir already holds " + filepath.Base(target) + ".")
		}
	}
	var temps, renamed []string
	defer func() {
		if err == nil {
			return
		}
		for _, p := range append(temps, renamed...) {
			ops.remove(p)
		}
	}()
	for _, data := range [][]byte{markdown, jsonData} {
		temp := ops.tempName(dir)
		f, cerr := ops.create(temp)
		if cerr != nil {
			return errors.Join(failure("report-write-failed", "The review report could not be written."), cerr)
		}
		temps = append(temps, temp)
		werr := ops.write(f, data)
		if cerr = f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			return errors.Join(failure("report-write-failed", "The review report could not be written."), werr)
		}
	}
	for i, temp := range temps {
		if rerr := ops.rename(temp, targets[i]); rerr != nil {
			return errors.Join(failure("report-write-failed", "The review report could not be written."), rerr)
		}
		renamed = append(renamed, targets[i])
		temps[i] = ""
	}
	return nil
}
