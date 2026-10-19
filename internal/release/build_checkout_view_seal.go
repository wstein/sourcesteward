// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"crypto/sha256"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// The private view is data the native comparison trusts. After preparation every file,
// symlink and directory in it is sealed (mode, content digest or link target), and every
// comparison first verifies the whole tree again, additions and removals included. The
// scratch directory is the one exception: it only holds results the job itself creates
// and reads back through descriptors it holds.
//
// This detects a project step that edits the private copies (for example by running
// `GIT_INDEX_FILE=<private>/index git update-index --assume-unchanged`). It cannot
// prevent a process of the same user from replacing a file after verification and
// restoring it before the next one; the private directory is 0700 and owned by the
// same user as the project, so that window is a documented limit, not a barrier.

type buildViewSealEntry struct {
	mode   fs.FileMode
	size   int64
	link   string
	digest [32]byte
}

func sealBuildViewPrivate(private string) (map[string]buildViewSealEntry, error) {
	seal := map[string]buildViewSealEntry{}
	err := filepath.WalkDir(private, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		entry := buildViewSealEntry{mode: info.Mode()}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			if entry.link, err = os.Readlink(path); err != nil {
				return err
			}
		case info.IsDir():
			if filepath.Base(path) == buildViewScratch && filepath.Dir(path) == private {
				seal[path] = entry
				return fs.SkipDir
			}
		case info.Mode().IsRegular():
			f, e := os.Open(path)
			if e != nil {
				return e
			}
			hash := sha256.New()
			n, e := io.Copy(hash, f)
			e = errors.Join(e, f.Close())
			if e != nil {
				return e
			}
			entry.size = n
			copy(entry.digest[:], hash.Sum(nil))
		default:
			return errors.New("unexpected private checkout view entry")
		}
		seal[path] = entry
		return nil
	})
	return seal, err
}

func (g *buildViewGraph) verifySeal() error {
	if g.sealed == nil {
		return nil
	}
	now, err := sealBuildViewPrivate(g.options.Private)
	if err != nil {
		return errors.New("private checkout view cannot be verified")
	}
	if len(now) != len(g.sealed) {
		return errors.New("private checkout view changed")
	}
	for path, want := range g.sealed {
		if got, ok := now[path]; !ok || got != want {
			return errors.New("private checkout view changed")
		}
	}
	return nil
}
