//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
//
package workspace

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestStoreRejectsFIFOAndHardLinkedRecords(t *testing.T) {
	p := privateDir(t)
	s, e := openStore(p, true)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	path := filepath.Join(p, "plans", "fifo.json")
	if e = syscall.Mkfifo(path, 0600); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { var v map[string]string; done <- s.read("plans", "fifo", &v) }()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("accepted FIFO")
		}
	case <-time.After(time.Second):
		t.Fatal("FIFO record blocked beyond bound")
	}
	original := filepath.Join(p, "plans", "original.json")
	os.WriteFile(original, []byte(`{"value":"linked"}`), 0600)
	if e = os.Link(original, filepath.Join(p, "plans", "linked.json")); e != nil {
		t.Fatal(e)
	}
	var v map[string]string
	if e = s.read("plans", "linked", &v); e == nil {
		t.Fatal("accepted hardlinked record")
	}
}
