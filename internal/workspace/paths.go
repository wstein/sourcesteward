// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// openCanonical pins every traversed directory and rejects symlink aliases.
func openCanonical(path string) (*os.Root, PathIdentity, error) {
	if !validText(path) || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, PathIdentity{}, failure("unsafe-path", "Paths must be canonical absolute directories without symlinks.")
	}
	root, e := os.OpenRoot(string(filepath.Separator))
	if e != nil {
		return nil, PathIdentity{}, e
	}
	for _, part := range strings.Split(strings.TrimPrefix(path, string(filepath.Separator)), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		st, e := root.Lstat(part)
		if e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			root.Close()
			return nil, PathIdentity{}, failure("unsafe-path", "Directory path has a missing, replaced or symlink component.")
		}
		next, e := root.OpenRoot(part)
		if e != nil {
			root.Close()
			return nil, PathIdentity{}, e
		}
		opened, e := next.Stat(".")
		if e != nil || !os.SameFile(st, opened) {
			next.Close()
			root.Close()
			return nil, PathIdentity{}, failure("changed-path", "Directory changed while opening.")
		}
		root.Close()
		root = next
	}
	st, e := root.Stat(".")
	if e != nil {
		root.Close()
		return nil, PathIdentity{}, e
	}
	id, e := pathIdentity(path, st)
	if e != nil {
		root.Close()
		return nil, PathIdentity{}, e
	}
	return root, id, nil
}
func destinationParent(dest string) (PathIdentity, error) {
	if !validText(dest) || !filepath.IsAbs(dest) || filepath.Clean(dest) != dest || dest == string(filepath.Separator) {
		return PathIdentity{}, failure("unsafe-destination", "Destination must be a canonical absolute unused path.")
	}
	p, id, e := openCanonical(filepath.Dir(dest))
	if e != nil {
		return PathIdentity{}, e
	}
	defer p.Close()
	if _, e = p.Lstat(filepath.Base(dest)); !errors.Is(e, os.ErrNotExist) {
		return PathIdentity{}, failure("occupied-destination", "Destination already exists or cannot be proven absent.")
	}
	return id, nil
}
func currentIdentity(path string) (PathIdentity, error) {
	r, id, e := openCanonical(path)
	if e == nil {
		r.Close()
	}
	return id, e
}
func overlaps(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+string(filepath.Separator)) || strings.HasPrefix(b, a+string(filepath.Separator))
}
func disjointStore(p MutationPlan) error {
	for _, other := range []string{p.SourcePath, p.Destination, p.Expected.Identity.WorkspaceRoot, p.Expected.Identity.RepositoryDir, p.Expected.Identity.GitDir} {
		if other != "" && overlaps(p.StateDir, other) {
			return failure("state-overlap", "State store must be disjoint from source, native metadata and destination.")
		}
	}
	for _, other := range []string{p.Expected.Identity.WorkspaceRoot, p.Expected.Identity.RepositoryDir, p.Expected.Identity.GitDir} {
		if other != "" && overlaps(p.Destination, other) {
			return failure("destination-overlap", "Destination must be outside source checkout and native metadata.")
		}
	}
	return nil
}
func reserveDestination(p MutationPlan) (PathIdentity, error) {
	parent, id, e := openCanonical(filepath.Dir(p.Destination))
	if e != nil {
		return PathIdentity{}, e
	}
	defer parent.Close()
	if id != p.DestinationParent {
		return PathIdentity{}, failure("changed-destination", "Destination parent identity changed.")
	}
	if e = parent.Mkdir(filepath.Base(p.Destination), 0700); e != nil {
		return PathIdentity{}, failure("occupied-destination", "Cannot exclusively reserve destination; no files were overwritten.")
	}
	st, e := parent.Lstat(filepath.Base(p.Destination))
	if e != nil {
		return PathIdentity{}, e
	}
	result, e := pathIdentity(p.Destination, st)
	if e != nil {
		return PathIdentity{}, e
	}
	if e = syncDirectory(parent); e != nil {
		return result, e
	}
	return result, nil
}
