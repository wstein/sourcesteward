// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Metadata is untrusted. Limit every file and reject symlink routing before use.
func metadataFile(path string) ([]byte, error) {
	if err := noSymlink(path); err != nil {
		return nil, err
	}
	f, e := openMetadata(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() {
		return nil, errors.New("invalid metadata file")
	}
	b, e := io.ReadAll(io.LimitReader(f, outputLimit+1))
	if e != nil {
		return nil, e
	}
	if len(b) > outputLimit {
		return nil, errors.New("metadata limit exceeded")
	}
	return b, nil
}
func noSymlink(path string) error {
	for p := path; ; p = filepath.Dir(p) {
		st, e := os.Lstat(p)
		if e != nil {
			return e
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return errors.New("symlink metadata")
		}
		if filepath.Dir(p) == p {
			return nil
		}
	}
}
func metadataDir(path string) error {
	if e := noSymlink(path); e != nil {
		return e
	}
	st, e := os.Stat(path)
	if e != nil {
		return e
	}
	if !st.IsDir() {
		return errors.New("invalid metadata directory")
	}
	return nil
}
func resolveJJRepository(metadata string) (string, error) {
	p := filepath.Join(metadata, "repo")
	for i := 0; i < 8; i++ {
		if e := noSymlink(p); e != nil {
			return "", e
		}
		st, e := os.Stat(p)
		if e != nil {
			return "", e
		}
		if st.IsDir() {
			return p, nil
		}
		b, e := metadataFile(p)
		if e != nil || !utf8.Valid(b) || len(b) == 0 || strings.ContainsAny(string(b), "\x00\n\r") {
			return "", errors.New("invalid jj repository pointer")
		}
		v := string(b)
		if !filepath.IsAbs(v) {
			v = filepath.Join(filepath.Dir(p), v)
		}
		p = filepath.Clean(v)
	}
	return "", errors.New("jj pointer limit exceeded")
}
func detectJJGitButler(ctx context.Context, _ string, r *Result) error {
	root := filepath.Dir(r.Evidence[0].Location)
	p := filepath.Join(root, ".git")
	st, e := os.Lstat(p)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	if !st.IsDir() {
		b, e := metadataFile(p)
		if e != nil {
			return e
		}
		s := strings.TrimSuffix(string(b), "\n")
		if !strings.HasPrefix(s, "gitdir: ") || !utf8.ValidString(s) {
			return errors.New("invalid gitfile")
		}
		p = strings.TrimPrefix(s, "gitdir: ")
		if !filepath.IsAbs(p) {
			p = filepath.Join(root, p)
		}
		p = filepath.Clean(p)
	}
	if e := metadataDir(p); e != nil {
		return e
	}
	dirs := []string{p}
	b, e := metadataFile(filepath.Join(p, "commondir"))
	if e == nil {
		s := strings.TrimSuffix(string(b), "\n")
		if s == "" || !utf8.ValidString(s) || strings.ContainsAny(s, "\n\r\x00") {
			return errors.New("invalid commondir")
		}
		if !filepath.IsAbs(s) {
			s = filepath.Join(p, s)
		}
		s = filepath.Clean(s)
		if e := metadataDir(s); e != nil {
			return e
		}
		dirs = append(dirs, s)
	} else if !os.IsNotExist(e) {
		return e
	}
	seen := map[string]bool{}
	budget := 128
	for _, d := range dirs {
		if e := ctx.Err(); e != nil {
			return e
		}
		if seen[d] {
			continue
		}
		seen[d] = true
		if _, e := marker(filepath.Join(d, "gitbutler"), "gitbutler-metadata", r); e != nil {
			return e
		}
		for _, ref := range []string{"refs/gitbutler", "refs/heads/gitbutler/workspace"} {
			p := filepath.Join(d, ref)
			st, e := os.Lstat(p)
			if os.IsNotExist(e) {
				continue
			}
			if e != nil {
				return e
			}
			if e := noSymlink(p); e != nil {
				return e
			}
			if st.IsDir() {
				if e := boundedRefs(ctx, p, 0, &budget); e != nil {
					return e
				}
			} else {
				if _, e := metadataFile(p); e != nil {
					return e
				}
			}
			r.Evidence = append(r.Evidence, Evidence{"gitbutler-reference", ref})
		}
		b, e := metadataFile(filepath.Join(d, "packed-refs"))
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		if !utf8.Valid(b) {
			return errors.New("invalid packed refs")
		}
		for _, line := range strings.Split(string(b), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && (strings.HasPrefix(fields[1], "refs/gitbutler/") || fields[1] == "refs/heads/gitbutler/workspace") {
				r.Evidence = append(r.Evidence, Evidence{"gitbutler-reference", fields[1]})
			}
		}
	}
	return nil
}
func boundedRefs(ctx context.Context, path string, depth int, budget *int) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if depth >= 8 {
		return fmt.Errorf("metadata depth limit")
	}
	if e := metadataDir(path); e != nil {
		return e
	}
	f, e := openMetadataDirectory(path)
	if e != nil {
		return e
	}
	entries, e := f.ReadDir(*budget + 1)
	f.Close()
	if e != nil && e != io.EOF {
		return e
	}
	if len(entries) > *budget {
		return fmt.Errorf("metadata entry limit")
	}
	*budget -= len(entries)
	for _, d := range entries {
		if e := ctx.Err(); e != nil {
			return e
		}
		p := filepath.Join(path, d.Name())
		if d.IsDir() {
			if e := boundedRefs(ctx, p, depth+1, budget); e != nil {
				return e
			}
		} else {
			if _, e := metadataFile(p); e != nil {
				return e
			}
		}
	}
	return nil
}
