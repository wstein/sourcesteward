// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestJJReleaseHashRetainsOnlyDigestAndRequiresSuccess(t *testing.T) {
	large := "i=0; while [ \"$i\" -lt 2048 ]; do printf xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx; i=$((i+1)); done"
	for _, tc := range []struct {
		name, body string
		data       []byte
		fail       bool
		limit      bool
	}{{"empty", "exit 0", nil, false, false}, {"binary", "printf '\\000\\377'", []byte{0, 255}, false, false}, {"over capture limit", large, []byte(strings.Repeat("x", 2048*64)), false, false}, {"late failure", large + "; exit 7", nil, true, false}, {"stderr overflow", large + "; (" + large + ") >&2", nil, true, true}} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			fake := filepath.Join(root, "sandbox")
			if e := os.WriteFile(fake, []byte("#!/bin/sh\n"+tc.body+"\n"), 0700); e != nil {
				t.Fatal(e)
			}
			r := &JJReleaseReader{session: jjSession{ctx: context.Background(), sandbox: fake, env: jjEnvironment(root)}, path: root, op: "operation", commit: "commit", entries: map[string]string{"lock": "file"}, active: true}
			got, e := r.Hash("lock")
			if tc.fail {
				if e == nil || got != "" || errors.Is(e, ErrJJReleaseLimit) != tc.limit {
					t.Fatalf("%q %v", got, e)
				}
			} else if e != nil || got != fmt.Sprintf("%x", sha256.Sum256(tc.data)) {
				t.Fatalf("%s %v", got, e)
			}
		})
	}
}
func TestJJReleaseHashRefusesInactiveNonFileAndCancellation(t *testing.T) {
	r := &JJReleaseReader{entries: map[string]string{"link": "symlink"}, active: true}
	if got, e := r.Hash("link"); got != "" || e == nil {
		t.Fatalf("%q %v", got, e)
	}
	r.active = false
	if _, e := r.Hash("link"); e == nil {
		t.Fatal("closed reader accepted")
	}
	root := t.TempDir()
	fake := filepath.Join(root, "sandbox")
	os.WriteFile(fake, []byte("#!/bin/sh\nprintf 'prefix'\nwhile :; do :; done\n"), 0700)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	r = &JJReleaseReader{session: jjSession{ctx: ctx, sandbox: fake, env: jjEnvironment(root)}, path: root, op: "op", commit: "commit", entries: map[string]string{"lock": "file"}, active: true}
	if got, e := r.Hash("lock"); got != "" || !errors.Is(e, context.DeadlineExceeded) {
		t.Fatalf("%q %v", got, e)
	}
}
