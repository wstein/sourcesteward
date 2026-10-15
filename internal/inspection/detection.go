// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

func marker(path, kind string, r *Result) (bool, error) {
	st, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("metadata-unreadable")
	}
	if !st.IsDir() {
		return false, fmt.Errorf("invalid-metadata")
	}
	r.Evidence = append(r.Evidence, Evidence{kind, path})
	return true, nil
}
func ancestors(path string, r *Result) (bool, error) {
	for {
		if _, err := marker(filepath.Join(path, ".jj"), "jj-metadata", r); err != nil {
			return false, err
		}
		_, err := os.Lstat(filepath.Join(path, ".git"))
		if err == nil {
			return true, nil
		}
		if !os.IsNotExist(err) {
			return false, fmt.Errorf("metadata-unreadable")
		}
		parent := filepath.Dir(path)
		if parent == path {
			return false, nil
		}
		path = parent
	}
}
func detectGitButler(ctx context.Context, run runner, dir string, repo *Repository, r *Result) error {
	seen := map[string]bool{}
	for _, metadata := range []string{repo.GitDir, repo.CommonDir} {
		if !seen[metadata] {
			seen[metadata] = true
			if _, err := marker(filepath.Join(metadata, "gitbutler"), "gitbutler-metadata", r); err != nil {
				return err
			}
		}
	}
	out, code, err := run(ctx, dir, "for-each-ref", "--format=%(refname)", "refs/gitbutler/", "refs/heads/gitbutler/workspace")
	if err != nil || code != 0 {
		return fmt.Errorf("git-failed")
	}
	for _, ref := range strings.Split(terminated(out), "\n") {
		if !utf8.ValidString(ref) {
			return fmt.Errorf("invalid-metadata")
		}
		if strings.HasPrefix(ref, "refs/gitbutler/") || ref == "refs/heads/gitbutler/workspace" {
			r.Evidence = append(r.Evidence, Evidence{"gitbutler-reference", ref})
		}
	}
	return nil
}
func managed(r *Result) bool {
	jj, but := false, false
	for _, e := range r.Evidence {
		jj = jj || e.Kind == "jj-metadata"
		but = but || strings.HasPrefix(e.Kind, "gitbutler-")
	}
	sort.Slice(r.Evidence, func(i, j int) bool {
		a, b := r.Evidence[i], r.Evidence[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Location < b.Location
	})
	if !jj && !but {
		return false
	}
	r.Status = "unsupported"
	code := "unsupported-backend"
	r.Backend = "jj"
	if but {
		r.Backend = "gitbutler"
	}
	if jj && but {
		r.Backend = "ambiguous"
		code = "ambiguous-backend"
	}
	msg := "inspection unsupported for detected " + r.Backend + " backend"
	for _, e := range r.Evidence {
		msg += fmt.Sprintf("; %s %q", e.Kind, e.Location)
	}
	r.Repository = nil
	r.Diagnostics = []Diagnostic{{code, msg}}
	return true
}
