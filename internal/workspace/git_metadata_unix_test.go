//go:build darwin || linux

// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestGitRegistrationMetadataIsBoundedAndNonblocking(t *testing.T) {
	d := t.TempDir()
	regular := filepath.Join(d, "regular")
	if err := os.WriteFile(regular, []byte("native\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if b, err := gitMetadata(regular); err != nil || string(b) != "native\n" {
		t.Fatalf("regular: %q %v", b, err)
	}
	link := filepath.Join(d, "link")
	if err := os.Symlink(regular, link); err != nil {
		t.Fatal(err)
	}
	large := filepath.Join(d, "large")
	if err := os.WriteFile(large, make([]byte, 65537), 0600); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(d, "fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	hard := filepath.Join(d, "hard")
	if err := os.Link(regular, hard); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{link, large, fifo, hard} {
		done := make(chan error, 1)
		go func() { _, err := gitMetadata(path); done <- err }()
		select {
		case err := <-done:
			if err == nil {
				t.Fatalf("unsafe metadata admitted: %s", path)
			}
		case <-time.After(time.Second):
			t.Fatalf("metadata read blocked: %s", path)
		}
	}
}
