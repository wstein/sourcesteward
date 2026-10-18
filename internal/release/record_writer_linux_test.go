// SPDX-License-Identifier: EUPL-1.2
//go:build linux

package release

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestRecordWriterClosesEveryDescriptor(t *testing.T) {
	root := t.TempDir()
	path := "docs/releases/warmup.json"
	if _, e := writeRecordExclusive(root, path, []byte("warmup")); e != nil {
		t.Fatal(e)
	}
	count := func() int {
		entries, e := os.ReadDir("/proc/self/fd")
		if e != nil {
			t.Fatal(e)
		}
		return len(entries)
	}
	before := count()
	for i := 0; i < 50; i++ {
		name := "docs/releases/v" + strconv.Itoa(i) + ".json"
		if _, e := writeRecordExclusive(root, name, []byte("canonical")); e != nil {
			t.Fatal(e)
		}
		if _, e := writeRecordExclusive(root, name, []byte("different")); e == nil {
			t.Fatal("overwrote existing")
		}
	}
	outside := t.TempDir()
	os.MkdirAll(filepath.Join(root, "bad/docs"), 0755)
	os.Symlink(outside, filepath.Join(root, "bad/docs/releases"))
	for i := 0; i < 50; i++ {
		if _, e := writeRecordExclusive(filepath.Join(root, "bad"), path, []byte("canonical")); e == nil {
			t.Fatal("followed link")
		}
	}
	if after := count(); after != before {
		t.Fatalf("descriptors: before=%d after=%d", before, after)
	}
}
