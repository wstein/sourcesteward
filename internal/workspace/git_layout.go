// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// A layout proof contains only exact metadata, never checkout content or sibling
// traversal. Directory mtimes are excluded: native registration changes are
// separately accounted for by each operation's registration digest.
type gitLayoutProof struct {
	evidence    strings.Builder
	directories map[string]PathIdentity
	files       map[string]PathIdentity
	contents    map[string]string
}

func gitLayoutRefused(message string) error { return failure("git-layout-refused", message) }

func (p *gitLayoutProof) directory(path string) error {
	if _, ok := p.directories[path]; ok {
		return nil
	}
	root, id, err := openCanonical(path)
	if err != nil {
		return gitLayoutRefused("Git layout directory is missing, noncanonical or aliased")
	}
	defer root.Close()
	st, err := root.Stat(".")
	if err != nil || !privateOwner(st) {
		return gitLayoutRefused("Git layout directory ownership is ambiguous")
	}
	for other, previous := range p.directories {
		if other != path && previous.Device == id.Device && previous.Inode == id.Inode {
			return gitLayoutRefused("Git layout directories have duplicate physical identities")
		}
	}
	p.directories[path] = id
	fmt.Fprintf(&p.evidence, "directory:%q:%d:%d\x00", path, id.Device, id.Inode)
	return nil
}

func (p *gitLayoutProof) file(path string) ([]byte, error) {
	if err := p.directory(filepath.Dir(path)); err != nil {
		return nil, err
	}
	data, id, mode, err := gitLayoutFile(path)
	if err != nil {
		return nil, gitLayoutRefused("Git layout pointer/configuration must be a bounded, owned, single-link regular file")
	}
	for other, previous := range p.files {
		if other != path && previous.Device == id.Device && previous.Inode == id.Inode {
			return nil, gitLayoutRefused("Git layout metadata has duplicate physical identities")
		}
	}
	if previous, ok := p.files[path]; ok && (previous != id || p.contents[path] != fmt.Sprintf("%d:%q", mode, data)) {
		return nil, gitLayoutRefused("Git layout metadata changed during proof")
	}
	p.files[path] = id
	p.contents[path] = fmt.Sprintf("%d:%q", mode, data)
	fmt.Fprintf(&p.evidence, "file:%q:%d:%d:%d:%q\x00", path, id.Device, id.Inode, mode, data)
	return data, nil
}

// Resolve generated relative paths, including '..', while rejecting symlinks in
// the literal traversal too. Cleaning first could hide a symlink followed by '..'.
func gitLayoutDirectory(base, value string) (string, error) {
	if !validText(value) || strings.ContainsAny(value, "\r\n") {
		return "", gitLayoutRefused("Git layout mapping has an empty, malformed or oversized path")
	}
	raw := value
	if !filepath.IsAbs(raw) {
		raw = base + string(filepath.Separator) + raw
	}
	current := string(filepath.Separator)
	for _, part := range strings.Split(raw, string(filepath.Separator)) {
		switch part {
		case "", ".":
			continue
		case "..":
			current = filepath.Dir(current)
			continue
		}
		current = filepath.Join(current, part)
		st, err := os.Lstat(current)
		if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return "", gitLayoutRefused("Git layout mapping traverses a missing or symbolic directory")
		}
	}
	if len(current) > 4096 {
		return "", gitLayoutRefused("Git layout mapping exceeds the path bound")
	}
	return current, nil
}

func (p *gitLayoutProof) pointer(path, prefix string) (string, error) {
	raw, err := p.file(path)
	if err != nil {
		return "", err
	}
	value := strings.TrimSuffix(string(raw), "\n")
	if !strings.HasPrefix(value, prefix) {
		return "", gitLayoutRefused("Git layout pointer syntax is malformed")
	}
	value = strings.TrimPrefix(value, prefix)
	return gitLayoutDirectory(filepath.Dir(path), value)
}

func (p *gitLayoutProof) checkout(root, target string) error {
	if err := p.directory(root); err != nil {
		return err
	}
	path := filepath.Join(root, ".git")
	st, err := os.Lstat(path)
	if err != nil {
		return gitLayoutRefused("Git checkout has no provable .git mapping")
	}
	if st.IsDir() {
		if path != target {
			return gitLayoutRefused("Git checkout directory does not match the native Git directory")
		}
		return p.directory(path)
	}
	resolved, err := p.pointer(path, "gitdir: ")
	if err != nil {
		return err
	}
	if resolved != target {
		return gitLayoutRefused("Git checkout pointer does not match the native Git directory")
	}
	return nil
}

// Return the same effective configuration used by all existing helper guards,
// plus a fingerprint of literal config, canonical mappings and reciprocal files.
func gitLayoutSafety(ctx context.Context, bin string, id NativeIdentity) (string, string, error) {
	proof := gitLayoutProof{directories: map[string]PathIdentity{}, files: map[string]PathIdentity{}, contents: map[string]string{}}
	for _, path := range []string{id.WorkspaceRoot, id.GitDir, id.RepositoryDir} {
		if err := proof.directory(path); err != nil {
			return "", "", err
		}
	}
	configPath := filepath.Join(id.RepositoryDir, "config")
	if _, err := proof.file(configPath); err != nil {
		return "", "", err
	}
	// The common directory must be primary metadata, never another redirection.
	if _, err := os.Lstat(filepath.Join(id.RepositoryDir, "commondir")); !os.IsNotExist(err) {
		return "", "", gitLayoutRefused("Git common directory contains an unsupported commondir redirection")
	}
	if err := proof.checkout(id.WorkspaceRoot, id.GitDir); err != nil {
		return "", "", err
	}
	if id.GitDir != id.RepositoryDir {
		if filepath.Dir(id.GitDir) != filepath.Join(id.RepositoryDir, "worktrees") {
			return "", "", gitLayoutRefused("Linked Git directory is not an exact common-directory registration")
		}
		common, err := proof.pointer(filepath.Join(id.GitDir, "commondir"), "")
		if err != nil {
			return "", "", err
		}
		if common != id.RepositoryDir {
			return "", "", gitLayoutRefused("Linked Git commondir does not match the native common directory")
		}
		backlink, err := proof.file(filepath.Join(id.GitDir, "gitdir"))
		if err != nil {
			return "", "", err
		}
		value := strings.TrimSuffix(string(backlink), "\n")
		// A backlink names a metadata file, so validate its parent as a directory.
		if !validText(value) || strings.ContainsAny(value, "\r\n") {
			return "", "", gitLayoutRefused("Linked Git backlink syntax is malformed")
		}
		if !filepath.IsAbs(value) {
			value = id.GitDir + string(filepath.Separator) + value
		}
		// Split the literal filename without filepath.Dir/Clean: cleaning here
		// would erase symlink, missing or regular-file parents followed by '..'
		// before gitLayoutDirectory can validate their actual traversal.
		separator := strings.LastIndexByte(value, filepath.Separator)
		if separator < 0 || value[separator+1:] != ".git" {
			return "", "", gitLayoutRefused("Linked Git backlink must name the exact checkout .git pointer")
		}
		parent, err := gitLayoutDirectory("/", value[:separator+1])
		if err != nil {
			return "", "", err
		}
		if filepath.Join(parent, ".git") != filepath.Join(id.WorkspaceRoot, ".git") {
			return "", "", gitLayoutRefused("Linked Git backlink does not identify the current checkout pointer")
		}
	}
	raw, err := gitRead(ctx, bin, id.WorkspaceRoot, "config", "--null", "--list", "--includes", "--show-origin")
	if err != nil {
		return "", "", err
	}
	fields := strings.Split(strings.TrimSuffix(raw, "\x00"), "\x00")
	if len(fields)%2 != 0 || len(fields)/2 > observationLimit {
		return "", "", gitLayoutRefused("Git configuration origins are incomplete or exceed the observation bound")
	}
	var config strings.Builder
	worktree := ""
	occurrences := 0
	for i := 0; i < len(fields); i += 2 {
		origin, entry := fields[i], fields[i+1]
		if strings.HasPrefix(origin, "file:") {
			name := strings.TrimPrefix(origin, "file:")
			if !filepath.IsAbs(name) {
				name = filepath.Join(id.WorkspaceRoot, name)
			}
			origin = "file:" + name
		}
		config.WriteString(entry)
		config.WriteByte(0)
		key, value, _ := strings.Cut(entry, "\n")
		if strings.EqualFold(key, "core.worktree") {
			occurrences++
			if occurrences != 1 || origin != "file:"+configPath || value == "" {
				return "", "", failure("git-worktree-config-refused", "core.worktree requires exactly one nonempty value directly in the canonical common config; duplicate and external origins refuse")
			}
			worktree = value
		}
	}
	if occurrences != 0 {
		primary, err := gitLayoutDirectory(id.RepositoryDir, worktree)
		if err != nil {
			return "", "", err
		}
		if err := proof.checkout(primary, id.RepositoryDir); err != nil {
			return "", "", err
		}
		// An independent checkout mapping proves the shared value; never compare it
		// to the current linked root or adopt it as the current workspace identity.
		fmt.Fprintf(&proof.evidence, "primary:%q\x00", primary)
	}
	// Re-read the exact files to refuse replacement observed during native reads.
	// Rebuilding the entire proof is intentionally avoided; observations remain
	// sampled rather than an adversarial transaction/ABA guarantee.
	paths := make([]string, 0, len(proof.files))
	for path := range proof.files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		identity := proof.files[path]
		data, current, mode, err := gitLayoutFile(path)
		if err != nil || current != identity || proof.contents[path] != fmt.Sprintf("%d:%q", mode, data) {
			return "", "", gitLayoutRefused("Git layout metadata changed during native configuration observation")
		}
		fmt.Fprintf(&proof.evidence, "final:%q:%d:%q\x00", path, mode, data)
	}
	return config.String(), gitDigest(proof.evidence.String()), nil
}
