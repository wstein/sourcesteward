// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package inspection

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestJJMetadataFIFORefused(t *testing.T) {
	for _, shape := range []string{"config-id", "packed-refs"} {
		t.Run(shape, func(t *testing.T) {
			root := t.TempDir()
			os.MkdirAll(filepath.Join(root, ".jj/repo"), 0700)
			p := filepath.Join(root, ".jj/repo/config-id")
			if shape == "packed-refs" {
				os.Mkdir(filepath.Join(root, ".git"), 0700)
				p = filepath.Join(root, ".git/packed-refs")
			}
			if e := syscall.Mkfifo(p, 0600); e != nil {
				t.Fatal(e)
			}
			done := make(chan Result, 1)
			go func() { done <- Inspect(context.Background(), root) }()
			select {
			case r := <-done:
				if r.Status != "error" {
					t.Fatal(r)
				}
			case <-time.After(time.Second):
				t.Fatal("FIFO blocked inspection")
			}
		})
	}
}
func TestJJReferencesUseAggregateBudget(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	for i := 0; i < 2; i++ {
		d := filepath.Join(root, fmt.Sprint(i))
		os.Mkdir(d, 0700)
		for j := 0; j < 70; j++ {
			os.WriteFile(filepath.Join(d, fmt.Sprint(j)), []byte("identity"), 0600)
		}
	}
	budget := 128
	if e := boundedRefs(context.Background(), root, 0, &budget); e == nil {
		t.Fatal("aggregate traversal budget ignored")
	}
}
func TestJJMalformedPackedReferencesRefused(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	os.Mkdir(filepath.Join(root, ".jj"), 0700)
	os.Mkdir(filepath.Join(root, ".git"), 0700)
	os.WriteFile(filepath.Join(root, ".git/packed-refs"), []byte("not a valid packed reference\n"), 0600)
	r := Result{Evidence: []Evidence{{"jj-metadata", filepath.Join(root, ".jj")}}}
	if e := detectJJGitButler(context.Background(), root, &r); e == nil {
		t.Fatal("malformed packed refs accepted")
	}
}
