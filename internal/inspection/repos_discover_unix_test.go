// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package inspection

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// A FIFO as the .git entry or as HEAD of a submodule Git directory would block any process or read that
// opens it. The member is refused without opening it, quickly, and the others are unaffected.
func TestReposDiscoverRefusesFIFOs(t *testing.T) {
	for name, place := range map[string]func(f reposHostileFixture) string{
		".git entry": func(f reposHostileFixture) string { return filepath.Join(f.s1, ".git") },
		"HEAD":       func(f reposHostileFixture) string { return filepath.Join(f.super, ".git", "modules", "s1", "HEAD") },
	} {
		t.Run(name, func(t *testing.T) {
			f := newReposHostileFixture(t)
			p := place(f)
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			if err := syscall.Mkfifo(p, 0o600); err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			f.expectRefused(t, "")
			if time.Since(start) > 20*time.Second {
				t.Fatalf("FIFO delayed the run: %v", time.Since(start))
			}
		})
	}
}
