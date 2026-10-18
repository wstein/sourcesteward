// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package release

import (
	"context"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestArtifactSpecialFilesNeverRead(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "fifo")
	if e := syscall.Mkfifo(fifo, 0600); e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{fifo, "/dev/null"} {
		done := make(chan error, 1)
		go func() { _, e := hashArtifact(context.Background(), path); done <- e }()
		select {
		case e := <-done:
			if e == nil {
				t.Fatalf("accepted %s", path)
			}
		case <-time.After(time.Second):
			t.Fatalf("blocked on special file %s", path)
		}
	}
}
