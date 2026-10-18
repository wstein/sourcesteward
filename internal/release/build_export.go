// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type buildTreeEntry struct{ Name, Mode, ID string }

// The namespace index lives on disk, so export memory does not grow with the
// number of source paths. It retains spelling and node kind for every folded
// prefix; hashes only address index files, and stored keys are always compared.
type buildTreeWriter struct {
	root, index *os.Root
	indexPath   string
}

func newBuildTreeWriter(root string) (*buildTreeWriter, error) {
	dir, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	indexPath, err := os.MkdirTemp("", "stw-export-index-")
	if err != nil {
		dir.Close()
		return nil, err
	}
	index, err := os.OpenRoot(indexPath)
	if err != nil {
		dir.Close()
		os.RemoveAll(indexPath)
		return nil, err
	}
	return &buildTreeWriter{root: dir, index: index, indexPath: indexPath}, nil
}
func (w *buildTreeWriter) Close() { w.root.Close(); w.index.Close(); os.RemoveAll(w.indexPath) }
func exportFold(s string) string {
	return strings.Map(func(r rune) rune {
		small := r
		for n := unicode.SimpleFold(r); n != r; n = unicode.SimpleFold(n) {
			if n < small {
				small = n
			}
		}
		return small
	}, s)
}
func (w *buildTreeWriter) admit(e buildTreeEntry) error {
	if !utf8.ValidString(e.Name) || !fs.ValidPath(e.Name) || strings.ContainsAny(e.Name, "\\\x00") || (e.Mode != "100644" && e.Mode != "100755") {
		return errors.New("unsafe tree path or mode")
	}
	parts := strings.Split(e.Name, "/")
	for i, part := range parts {
		if strings.EqualFold(part, ".git") {
			return errors.New(".git export refused")
		}
		prefix := strings.Join(parts[:i+1], "/")
		sum := sha256.Sum256([]byte(exportFold(prefix)))
		key := hex.EncodeToString(sum[:])
		bucket := key[:2]
		if err := w.index.MkdirAll(bucket, 0700); err != nil {
			return err
		}
		name := bucket + "/" + key[2:]
		kind := "d"
		if i == len(parts)-1 {
			kind = "f"
		}
		value := kind + "\x00" + prefix
		marker, err := w.index.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if errors.Is(err, os.ErrExist) {
			raw, readErr := w.index.ReadFile(name)
			if readErr != nil {
				return readErr
			}
			if string(raw) != value {
				return errors.New("case-colliding or conflicting tree paths")
			}
			if kind == "f" {
				return errors.New("duplicate tree path")
			}
			continue
		}
		if err != nil {
			return err
		}
		_, writeErr := io.WriteString(marker, value)
		closeErr := marker.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}
func (w *buildTreeWriter) Write(e buildTreeEntry, body func(io.Writer) error) error {
	if err := w.admit(e); err != nil {
		return err
	}
	parent := path.Dir(e.Name)
	if parent != "." {
		if err := w.root.MkdirAll(parent, 0755); err != nil {
			return err
		}
	}
	mode := fs.FileMode(0644)
	if e.Mode == "100755" {
		mode = 0755
	}
	f, err := w.root.OpenFile(e.Name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	// The contract requires exact regular-file modes, independent of caller umask.
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	writeErr := body(f)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

// exportBuildTree is the fixture seam. Production enumerates metadata and copies
// blobs incrementally, without collecting entries or bodies in memory.
func exportBuildTree(root string, entries []buildTreeEntry, read func(string) ([]byte, error)) error {
	writer, err := newBuildTreeWriter(root)
	if err != nil {
		return err
	}
	defer writer.Close()
	for _, entry := range entries {
		if err := writer.Write(entry, func(dst io.Writer) error {
			raw, err := read(entry.ID)
			if err != nil {
				return err
			}
			_, err = dst.Write(raw)
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}
func exportGitBuildTree(ctx context.Context, source *gitSource, destination string) error {
	grafts, truncated, status, err := gitOutput(ctx, source.root, 65536, "", "rev-parse", "--path-format=absolute", "--git-path", "info/grafts")
	if err != nil || truncated || status != 0 {
		return errors.New("cannot inspect grafts")
	}
	if _, err := os.Lstat(strings.TrimSuffix(string(grafts), "\n")); !errors.Is(err, os.ErrNotExist) {
		return errors.New("grafts export refused")
	}
	writer, err := newBuildTreeWriter(destination)
	if err != nil {
		return err
	}
	defer writer.Close()
	return withBuildGitBatch(ctx, source.root, func(blobs *bufio.Reader, requests io.Writer) error {
		return withBuildGitRead(ctx, source.root, []string{"ls-tree", "-rz", "--full-tree", source.commit}, func(stream io.Reader) error {
			// The existing native-output bound applies to one path record, never to
			// aggregate tree output. A record too large for a native path is unsafe.
			listing := bufio.NewReaderSize(stream, 64<<10)
			for {
				raw, err := listing.ReadSlice(0)
				if errors.Is(err, io.EOF) && len(raw) == 0 {
					return nil
				}
				if err != nil {
					return errors.New("invalid or unexportable tree path record")
				}
				meta, name, ok := bytes.Cut(raw[:len(raw)-1], []byte{'\t'})
				fields := strings.Fields(string(meta))
				if !ok || len(fields) != 3 || fields[1] != "blob" || !IsFullCommitID(fields[2]) {
					return errors.New("unsafe export entry")
				}
				entry := buildTreeEntry{Name: string(name), Mode: fields[0], ID: fields[2]}
				if err := writer.Write(entry, func(dst io.Writer) error {
					if _, err := io.WriteString(requests, entry.ID+"\n"); err != nil {
						return err
					}
					return copyBuildExportBlob(dst, blobs, entry.ID)
				}); err != nil {
					return err
				}
			}
		})
	})
}

func copyBuildExportBlob(dst io.Writer, blobs *bufio.Reader, id string) error {
	header, err := blobs.ReadSlice('\n')
	if err != nil {
		return errors.New("invalid blob header")
	}
	parts := strings.Fields(string(header))
	if len(parts) != 3 || parts[0] != id || parts[1] != "blob" {
		return errors.New("unexpected export object")
	}
	size, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || size < 0 {
		return errors.New("invalid export blob size")
	}
	if _, err := io.CopyN(dst, blobs, size); err != nil {
		return err
	}
	delimiter, err := blobs.ReadByte()
	if err != nil || delimiter != '\n' {
		return fmt.Errorf("invalid export blob delimiter")
	}
	return nil
}
