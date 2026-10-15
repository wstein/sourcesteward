// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func privateDir(t *testing.T) string {
	t.Helper()
	p, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(p, 0700); e != nil {
		t.Fatal(e)
	}
	return p
}
func TestSecureStoreRejectsPermissionsSymlinksAndReplacement(t *testing.T) {
	p := privateDir(t)
	if e := os.Chmod(p, 0755); e != nil {
		t.Fatal(e)
	}
	if s, e := openStore(p, true); e == nil {
		s.Close()
		t.Fatal("accepted public store")
	}
	os.Chmod(p, 0700)
	link := filepath.Join(privateDir(t), "link")
	os.Symlink(p, link)
	if s, e := openStore(link, true); e == nil {
		s.Close()
		t.Fatal("accepted symlink store")
	}
	s, e := openStore(p, true)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	moved := p + "-moved"
	if e = os.Rename(p, moved); e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(moved)
	if e = os.Mkdir(p, 0700); e != nil {
		t.Fatal(e)
	}
	if e = s.write("plans", "a", map[string]string{"x": "y"}, true); e == nil {
		t.Fatal("wrote through replaced root")
	}
	if _, e = os.Stat(filepath.Join(p, "plans", "a.json")); !os.IsNotExist(e) {
		t.Fatal("replacement touched")
	}
}
func TestSecureStoreStrictRecordsAndLock(t *testing.T) {
	p := privateDir(t)
	s, e := openStore(p, true)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if other, e := openStore(p, true); e == nil {
		other.Close()
		t.Fatal("concurrent lock accepted")
	}
	if e = s.write("plans", "one", map[string]string{"value": "safe"}, true); e != nil {
		t.Fatal(e)
	}
	var v struct {
		Value string `json:"value"`
	}
	if e = s.read("plans", "one", &v); e != nil || v.Value != "safe" {
		t.Fatalf("read: %v %#v", e, v)
	}
	if e = s.write("plans", "one", v, true); e == nil {
		t.Fatal("overwrote immutable plan")
	}
	for _, data := range []string{`{"value":"a","value":"b"}`, `{"unknown":1}`, strings.Repeat("x", recordLimit+1)} {
		os.WriteFile(filepath.Join(p, "plans", "bad.json"), []byte(data), 0600)
		if e = s.read("plans", "bad", &v); e == nil {
			t.Fatalf("accepted bad record %.30s", data)
		}
	}
	outside := filepath.Join(privateDir(t), "data")
	os.WriteFile(outside, []byte(`{"value":"foreign"}`), 0600)
	os.Remove(filepath.Join(p, "plans", "bad.json"))
	os.Symlink(outside, filepath.Join(p, "plans", "bad.json"))
	if e = s.read("plans", "bad", &v); e == nil {
		t.Fatal("followed record symlink")
	}
}
func TestDestinationRejectsAliasesAndOccupiedPaths(t *testing.T) {
	parent := privateDir(t)
	dest := filepath.Join(parent, "task")
	id, e := destinationParent(dest)
	if e != nil || id.Path != parent {
		t.Fatalf("parent %v %#v", e, id)
	}
	os.Mkdir(dest, 0700)
	if _, e = destinationParent(dest); e == nil {
		t.Fatal("accepted occupied destination")
	}
	alias := filepath.Join(privateDir(t), "alias")
	os.Symlink(parent, alias)
	if _, e = destinationParent(filepath.Join(alias, "other")); e == nil {
		t.Fatal("accepted symlink parent")
	}
}
