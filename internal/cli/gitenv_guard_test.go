// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// inheritedGitDirProblem refuses a GIT_DIR that is not a plain repository directory
// inside one of the temporary roots, so that no test can ever run against a real
// repository: a gitfile, a symbolic link out of the root and a linked-worktree
// directory whose common directory lies outside the roots are all refused.
func inheritedGitDirProblem(gitDir string, temps ...string) error {
	if gitDir == "" {
		return nil
	}
	inside := func(path string) bool {
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			path = resolved
		}
		for _, temp := range temps {
			if filepath.IsAbs(path) && strings.HasPrefix(path, temp+string(filepath.Separator)) {
				return true
			}
		}
		return false
	}
	clean := filepath.Clean(gitDir)
	if !inside(clean) {
		return fmt.Errorf("inherited GIT_DIR %q is not inside a temporary directory %q; unset the GIT_* variables", gitDir, temps)
	}
	if info, err := os.Stat(clean); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("inherited GIT_DIR %q is a file, not a repository directory", gitDir)
		}
		if raw, err := os.ReadFile(filepath.Join(clean, "commondir")); err == nil {
			common := strings.TrimSpace(string(raw))
			if !filepath.IsAbs(common) {
				common = filepath.Join(clean, common)
			}
			if !inside(filepath.Clean(common)) {
				return fmt.Errorf("inherited GIT_DIR %q belongs to a repository outside the temporary directories", gitDir)
			}
		}
	}
	return nil
}

// isolateGitEnvironment removes the repository-selecting and configuration
// variables from the process, so every fixture starts from a clean Git environment.
func isolateGitEnvironment() error {
	temps := temporaryRoots(os.TempDir(), "/tmp")
	if err := inheritedGitDirProblem(os.Getenv("GIT_DIR"), temps...); err != nil {
		return err
	}
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		switch {
		case strings.HasPrefix(name, "GIT_CONFIG"):
		case name == "GIT_DIR", name == "GIT_WORK_TREE", name == "GIT_INDEX_FILE", name == "GIT_PREFIX", name == "GIT_COMMON_DIR", name == "GIT_OBJECT_DIRECTORY", name == "GIT_ALTERNATE_OBJECT_DIRECTORIES", name == "GIT_NAMESPACE", name == "GIT_REFLOG_ACTION":
		default:
			continue
		}
		if err := os.Unsetenv(name); err != nil {
			return err
		}
	}
	return nil
}

// temporaryRoots resolves each candidate and keeps those that are not inside a Git
// repository: a TMPDIR that is a link into a working tree must not make that
// working tree an accepted place for a test repository.
func temporaryRoots(candidates ...string) []string {
	var roots []string
	for _, candidate := range candidates {
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			continue
		}
		inRepository := false
		for dir := resolved; ; dir = filepath.Dir(dir) {
			if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
				inRepository = true
			}
			if filepath.Dir(dir) == dir {
				break
			}
		}
		if !inRepository {
			roots = append(roots, resolved)
		}
	}
	return roots
}
