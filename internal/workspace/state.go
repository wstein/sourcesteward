// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
)

type store struct {
	path     string
	identity PathIdentity
	root     *os.Root
	dirs     map[string]*os.Root
	lock     *os.File
	writable bool
}

func openStore(path string, write bool) (s *store, err error) {
	root, id, e := openCanonical(path)
	if e != nil {
		return nil, e
	}
	s = &store{path: path, identity: id, root: root, dirs: map[string]*os.Root{}, writable: write}
	opened := s
	defer func() {
		if err != nil {
			opened.Close()
		}
	}()
	st, e := root.Stat(".")
	if e != nil || st.Mode().Perm() != 0700 || !privateOwner(st) {
		return nil, failure("unsafe-state", "State directory must be current-user owned with mode0700.")
	}
	if write {
		f, e := root.OpenFile("lock", os.O_CREATE|os.O_RDWR|noFollow, 0600)
		if e != nil {
			return nil, failure("unsafe-state", "Cannot safely open state lock.")
		}
		s.lock = f
		st, e = f.Stat()
		if e != nil || !regularPrivate(st) {
			return nil, failure("unsafe-state", "State lock must be a private single-link regular file.")
		}
		if e = lockFile(f); e != nil {
			return nil, e
		}
	}
	for _, name := range []string{"plans", "operations", "tasks"} {
		if write {
			if e = root.Mkdir(name, 0700); e != nil && !errors.Is(e, os.ErrExist) {
				return nil, e
			}
		}
		before, e := root.Lstat(name)
		if e != nil || !before.IsDir() || before.Mode().Perm() != 0700 || !privateOwner(before) {
			return nil, failure("unsafe-state", "State record directories must be private real directories.")
		}
		dir, e := root.OpenRoot(name)
		if e != nil {
			return nil, e
		}
		s.dirs[name] = dir
		after, e := dir.Stat(".")
		if e != nil || !os.SameFile(before, after) {
			return nil, failure("changed-state", "State subdirectory changed during opening.")
		}
	}
	if write {
		if e = syncDirectory(root); e != nil {
			return nil, e
		}
	}
	return s, nil
}
func (s *store) Close() {
	for _, r := range s.dirs {
		r.Close()
	}
	if s.lock != nil {
		s.lock.Close()
	}
	if s.root != nil {
		s.root.Close()
	}
}
func syncDirectory(r *os.Root) error {
	f, e := r.Open(".")
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
func (s *store) validate() error {
	r, id, e := openCanonical(s.path)
	if e != nil {
		return failure("changed-state", "State directory path was replaced or became inaccessible.")
	}
	defer r.Close()
	if id != s.identity {
		return failure("changed-state", "State directory identity changed.")
	}
	st, e := r.Stat(".")
	if e != nil || st.Mode().Perm() != 0700 || !privateOwner(st) {
		return failure("unsafe-state", "State directory security changed.")
	}
	for name, dir := range s.dirs {
		before, e := s.root.Lstat(name)
		after, a := dir.Stat(".")
		if e != nil || a != nil || !before.IsDir() || before.Mode().Perm() != 0700 || !privateOwner(before) || !os.SameFile(before, after) {
			return failure("changed-state", "State record directory changed.")
		}
	}
	return nil
}
func (s *store) read(kind, id string, value any) error {
	if !validID(id) || s.dirs[kind] == nil {
		return failure("invalid-state", "Invalid state record identity.")
	}
	if e := s.validate(); e != nil {
		return e
	}
	f, e := s.dirs[kind].OpenFile(id+".json", os.O_RDONLY|noFollow, 0)
	if e != nil {
		return e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !regularPrivate(st) || st.Size() > recordLimit {
		return failure("invalid-state", "Record must be a private bounded single-link regular file.")
	}
	b, e := io.ReadAll(io.LimitReader(f, recordLimit+1))
	if e != nil {
		return e
	}
	if len(b) > recordLimit {
		return failure("record-limit", "State record exceeds oneMiB.")
	}
	return decodeStrict(b, value)
}
func decodeStrict(b []byte, value any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	if e := checkJSON(d, 0); e != nil {
		return failure("invalid-state", "State contains malformed, repeated or excessively nested JSON fields.")
	}
	if _, e := d.Token(); e != io.EOF {
		return failure("invalid-state", "State contains trailing JSON data.")
	}
	d = json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e := d.Decode(value); e != nil {
		return failure("invalid-state", "State contains incompatible or unknown fields.")
	}
	return nil
}
func checkJSON(d *json.Decoder, depth int) error {
	if depth > 64 {
		return errors.New("JSON nesting bound")
	}
	t, e := d.Token()
	if e != nil {
		return e
	}
	v, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch v {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, e := d.Token()
			if e != nil {
				return e
			}
			k, ok := key.(string)
			if !ok || seen[k] {
				return errors.New("duplicate key")
			}
			seen[k] = true
			if e = checkJSON(d, depth+1); e != nil {
				return e
			}
		}
	case '[':
		for d.More() {
			if e = checkJSON(d, depth+1); e != nil {
				return e
			}
		}
	default:
		return errors.New("bad delimiter")
	}
	_, e = d.Token()
	return e
}
func (s *store) write(kind, id string, value any, exclusive bool) error {
	if !s.writable || !validID(id) || s.dirs[kind] == nil {
		return failure("invalid-state", "Invalid state write.")
	}
	if e := s.validate(); e != nil {
		return e
	}
	b, e := json.Marshal(value)
	if e != nil || len(b) > recordLimit {
		return failure("record-limit", "State record exceeds oneMiB or cannot be encoded.")
	}
	dir := s.dirs[kind]
	name := id + ".json"
	target := name
	if !exclusive {
		token, e := randomID()
		if e != nil {
			return e
		}
		target = "tmp-" + token + ".json"
	}
	f, e := dir.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY|noFollow, 0600)
	if e != nil {
		return e
	}
	ok := false
	defer func() {
		f.Close()
		if !ok {
			dir.Remove(target)
		}
	}()
	if _, e = f.Write(b); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if e = s.validate(); e != nil {
		return e
	}
	if !exclusive {
		if before, e := dir.Lstat(name); e == nil && !regularPrivate(before) {
			return failure("invalid-state", "Existing record is not private regular state.")
		} else if e != nil && !errors.Is(e, os.ErrNotExist) {
			return e
		}
		if e = dir.Rename(target, name); e != nil {
			return e
		}
	}
	if e = syncDirectory(dir); e != nil {
		return e
	}
	ok = true
	return nil
}
func (s *store) list(kind string) ([]string, error) {
	if e := s.validate(); e != nil {
		return nil, e
	}
	r := s.dirs[kind]
	if r == nil {
		return nil, failure("invalid-state", "Unknown state record kind.")
	}
	f, e := r.Open(".")
	if e != nil {
		return nil, e
	}
	defer f.Close()
	entries, e := f.ReadDir(observationLimit + 1)
	if e != nil && e != io.EOF {
		return nil, e
	}
	if len(entries) > observationLimit {
		return nil, failure("record-limit", "State directory exceeds1024 records.")
	}
	ids := []string{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || len(name) < 6 || name[len(name)-5:] != ".json" {
			return nil, failure("invalid-state", "Unexpected entry in private state directory.")
		}
		id := name[:len(name)-5]
		if !validID(id) {
			return nil, failure("invalid-state", "Invalid state filename.")
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}
func randomID() (string, error) {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", e
	}
	return hex.EncodeToString(b[:]), nil
}
func planDigest(p MutationPlan) string {
	p.Digest = ""
	p.DestinationIdentity = nil
	b, _ := json.Marshal(p)
	h := sha256.Sum256(b)
	return fmt.Sprintf("%x", h)
}
