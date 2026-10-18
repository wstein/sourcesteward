// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestJJReleaseTreeRecords(t *testing.T) {
	entries, err := jjReleaseEntries("\".stw/release.toml\"\n\"file\"\nfalse\n\"evidence\"\n\"symlink\"\nfalse\n")
	if err != nil || len(entries) != 2 || entries["evidence"] != "symlink" {
		t.Fatalf("%v %v", entries, err)
	}
	for _, input := range []string{"\"../bad\"\n\"file\"\nfalse\n", "\"x\"\n\"file\"\ntrue\n", "\"x\"\n\"file\"\nfalse\n\"x\"\n\"file\"\nfalse\n"} {
		if _, err := jjReleaseEntries(input); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
}

func TestJJReleaseNonemptySinkWaitsForSuccess(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       bool
		code       int
		limit      bool
	}{
		{"empty", "exit 0", false, 0, false},
		{"binary bytes", "printf '\\000\\377'", true, 0, false},
		{"large evidence", "i=0; while [ \"$i\" -lt 2048 ]; do printf xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx; i=$((i+1)); done", true, 0, false},
		{"late failure", "printf x; exit 7", false, 7, false},
		{"stderr overflow", "i=0; while [ \"$i\" -lt 2048 ]; do printf xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx >&2; i=$((i+1)); done", false, -1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			fake := filepath.Join(root, "sandbox")
			if err := os.WriteFile(fake, []byte("#!/bin/sh\n"+tc.body+"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			s := jjSession{ctx: context.Background(), sandbox: fake, env: jjEnvironment(root)}
			got, code, err := s.releaseNonempty(root, "operation", "commit", "evidence")
			if tc.limit {
				if !errors.Is(err, errJJOutput) {
					t.Fatalf("%v", err)
				}
				return
			}
			if err != nil || code != tc.code || got != tc.want {
				t.Fatalf("%t %d %v", got, code, err)
			}
		})
	}
	root := t.TempDir()
	fake := filepath.Join(root, "sandbox")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nprintf x\nwhile :; do :; done\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	s := jjSession{ctx: ctx, sandbox: fake, env: jjEnvironment(root)}
	got, _, err := s.releaseNonempty(root, "op", "commit", "evidence")
	if got || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancellation passed after prefix: %t %v", got, err)
	}
}

func TestJJReleaseReaderNormalizesOutputLimit(t *testing.T) {
	root := t.TempDir()
	fake := filepath.Join(root, "sandbox")
	body := "#!/bin/sh\ni=0\nwhile [ \"$i\" -lt 2048 ]; do printf xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx >&2; i=$((i+1)); done\n"
	if err := os.WriteFile(fake, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	reader := &JJReleaseReader{session: jjSession{ctx: context.Background(), sandbox: fake, env: jjEnvironment(root)}, path: root, op: "operation", commit: "commit", gitdir: root, entries: map[string]string{"evidence": "file"}, active: true}
	for name, read := range map[string]func() error{
		"readiness": func() error { _, err := reader.Nonempty("evidence"); return err },
		"blob":      func() error { _, err := reader.Read("evidence"); return err },
		"time":      func() error { _, err := reader.CommitTime(); return err },
		"main":      func() error { _, _, _, err := reader.Main("origin"); return err },
	} {
		t.Run(name, func(t *testing.T) {
			if err := read(); !errors.Is(err, ErrJJReleaseLimit) {
				t.Fatalf("public fixed read leaked private output error: %v", err)
			}
		})
	}
}

func TestJJReleaseMainRetainsObservedRefWhenAncestryCannotRun(t *testing.T) {
	root := t.TempDir()
	fake := filepath.Join(root, "sandbox")
	id := strings.Repeat("a", 40)
	records := "\"main\"\n\"origin\"\nfalse\n\"" + id + "\"\n\"" + strings.Repeat("k", 32) + "\"\ntrue\n[]\n[]\n"
	if err := os.WriteFile(fake, []byte("#!/bin/sh\ncat <<'STW_RECORDS'\n"+records+"STW_RECORDS\n"), 0700); err != nil {
		t.Fatal(err)
	}
	reader := &JJReleaseReader{session: jjSession{ctx: context.Background(), sandbox: fake, env: jjEnvironment(root)}, path: root, op: "operation", commit: id, gitdir: root, active: true}
	if err := os.Mkdir(filepath.Join(root, "info"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ path, reason string }{{"info/grafts", "grafts-present"}, {"shallow", "shallow-clone"}} {
		t.Run(tc.reason, func(t *testing.T) {
			path := filepath.Join(root, tc.path)
			if err := os.WriteFile(path, nil, 0600); err != nil {
				t.Fatal(err)
			}
			defer os.Remove(path)
			commit, reason, ancestor, err := reader.Main("origin")
			if err != nil || ancestor || reason != tc.reason || commit != id {
				t.Fatalf("recorded main lost: %s %s %t %v", commit, reason, ancestor, err)
			}
		})
	}
}

func TestJJReleaseBlobOverflowRequiresSuccessfulStdout(t *testing.T) {
	large := "i=0; while [ \"$i\" -lt 2048 ]; do printf xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx; i=$((i+1)); done"
	for _, tc := range []struct {
		name, body        string
		blob, operational bool
	}{
		{"stdout", large, true, false},
		{"stderr", "(" + large + ") >&2", false, true},
		{"late failure", large + "; exit 7", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			fake := filepath.Join(root, "sandbox")
			if err := os.WriteFile(fake, []byte("#!/bin/sh\n"+tc.body+"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			reader := &JJReleaseReader{session: jjSession{ctx: context.Background(), sandbox: fake, env: jjEnvironment(root)}, path: root, op: "operation", commit: "commit", entries: map[string]string{"contract": "file"}, active: true}
			data, err := reader.Read("contract")
			if err == nil || len(data) != 0 || errors.Is(err, ErrJJReleaseBlobTooLarge) != tc.blob || errors.Is(err, ErrJJReleaseLimit) != tc.operational {
				t.Fatalf("data=%d err=%v", len(data), err)
			}
		})
	}
	t.Run("cancel after oversized stdout", func(t *testing.T) {
		root := t.TempDir()
		fake := filepath.Join(root, "sandbox")
		if err := os.WriteFile(fake, []byte("#!/bin/sh\n"+large+"\nwhile :; do :; done\n"), 0700); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		reader := &JJReleaseReader{session: jjSession{ctx: ctx, sandbox: fake, env: jjEnvironment(root)}, path: root, op: "operation", commit: "commit", entries: map[string]string{"contract": "file"}, active: true}
		data, err := reader.Read("contract")
		if len(data) != 0 || !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrJJReleaseBlobTooLarge) {
			t.Fatalf("data=%d err=%v", len(data), err)
		}
	})
}

func TestJJReleaseReaderNeverShowsSymlinkAsBlob(t *testing.T) {
	root := t.TempDir()
	fake := filepath.Join(root, "sandbox")
	marker := filepath.Join(root, "invoked")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nprintf invoked > '"+marker+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	reader := &JJReleaseReader{session: jjSession{ctx: context.Background(), sandbox: fake, env: jjEnvironment(root)}, path: root, op: "operation", commit: "commit", entries: map[string]string{"link": "symlink"}, active: true}
	if data, err := reader.Read("link"); err == nil || len(data) != 0 {
		t.Fatalf("symlink blob accepted: %q %v", data, err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("native file show ran for a symlink: %v", err)
	}
}
