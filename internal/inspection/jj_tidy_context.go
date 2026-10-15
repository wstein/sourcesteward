// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
)

type jjTidyMetadataCopy struct {
	path     string
	identity os.FileInfo
	digest   [32]byte
}

// prepareJJTidyContext prevents colocated commands from exporting refs or resetting the source Git index.
// It performs setup-only private writes; the caller's mandatory sandbox keeps this context read-only.
func prepareJJTidyContext(private, metadata, repo string) (string, func() error, error) {
	context := filepath.Join(private, "tidy-execution")
	if !filepath.IsAbs(repo) || metadataDir(metadata) != nil || metadataDir(repo) != nil {
		return "", nil, errors.New("invalid native execution context source")
	}
	if e := os.Mkdir(context, 0700); e != nil {
		return "", nil, e
	}
	wc := filepath.Join(context, ".jj", "working_copy")
	if e := os.MkdirAll(wc, 0700); e != nil {
		return "", nil, e
	}
	if e := os.WriteFile(filepath.Join(context, ".jj", "repo"), []byte(repo), 0600); e != nil {
		return "", nil, e
	}
	copies := []jjTidyMetadataCopy{}
	for _, name := range []string{"checkout", "tree_state", "type"} {
		path := filepath.Join(metadata, "working_copy", name)
		st, e := os.Lstat(path)
		if e != nil || !jjTidySingleLink(st) {
			return "", nil, errors.New("native execution context requires regular single-link working-copy metadata")
		}
		bytes, e := metadataFile(path)
		if e != nil {
			return "", nil, e
		}
		after, e := os.Lstat(path)
		if e != nil || !os.SameFile(st, after) || st.Mode() != after.Mode() || st.Size() != after.Size() {
			return "", nil, errJJChanged
		}
		if e := os.WriteFile(filepath.Join(wc, name), bytes, 0600); e != nil {
			return "", nil, e
		}
		copies = append(copies, jjTidyMetadataCopy{path, st, sha256.Sum256(bytes)})
	}
	verify := func() error {
		for _, copy := range copies {
			st, e := os.Lstat(copy.path)
			if e != nil || !jjTidySingleLink(st) || !os.SameFile(copy.identity, st) || st.Mode() != copy.identity.Mode() || st.Size() != copy.identity.Size() {
				return errJJChanged
			}
			bytes, e := metadataFile(copy.path)
			if e != nil {
				return e
			}
			if sha256.Sum256(bytes) != copy.digest {
				return errJJChanged
			}
		}
		return nil
	}
	return context, verify, verify()
}
