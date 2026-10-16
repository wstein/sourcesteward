// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/wstein/sourcesteward/internal/inspection"
)

type pinsWorld struct {
	t    *testing.T
	root string
}

func newPinsWorld(t *testing.T) pinsWorld {
	t.Helper()
	// The audit refuses world-writable submodule directories, which Git creates under umask 000.
	old := syscall.Umask(0o022)
	t.Cleanup(func() { syscall.Umask(old) })
	root := t.TempDir()
	t.Setenv("HOME", root)
	return pinsWorld{t, root}
}

func (w pinsWorld) git(dir string, args ...string) string {
	w.t.Helper()
	c := exec.Command("git", append([]string{"-c", "protocol.file.allow=always", "-c", "user.name=T", "-c", "user.email=t@e.invalid", "-c", "commit.gpgsign=false"}, args...)...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := c.CombinedOutput()
	if err != nil {
		w.t.Fatalf("git %v: %s", args, out)
	}
	return strings.TrimSpace(string(out))
}

func (w pinsWorld) repo(name string) string {
	d := filepath.Join(w.root, name)
	if err := os.MkdirAll(d, 0o755); err != nil {
		w.t.Fatal(err)
	}
	w.git(d, "init", "-q", "-b", "main")
	w.git(d, "commit", "-q", "--allow-empty", "-m", "one")
	return d
}

// super returns a superproject with one submodule, "member", cloned from a local repository.
func (w pinsWorld) super() (super, sub string) {
	sub, super = w.repo("sub"), w.repo("super")
	w.git(super, "submodule", "add", "-q", sub, "member")
	w.git(super, "commit", "-qm", "add")
	return super, sub
}

func (w pinsWorld) write(path, content string) {
	w.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		w.t.Fatal(err)
	}
}

func (w pinsWorld) check(super, wantStatus string) inspection.PinsResult {
	w.t.Helper()
	r := inspection.Pins(context.Background(), inspection.PinsOptions{Path: super})
	raw, err := json.Marshal(r)
	if err != nil {
		w.t.Fatal(err)
	}
	valid(w.t, schema(w.t, "pins"), raw)
	if r.Status != wantStatus {
		w.t.Fatalf("status %s, want %s: %+v %+v", r.Status, wantStatus, r.Error, r.Findings)
	}
	if len(raw) > 1<<20 {
		w.t.Fatalf("result is %d bytes", len(raw))
	}
	return r
}

func gitlinks(w pinsWorld, dir string, paths []string) {
	var in bytes.Buffer
	oid := w.git(dir, "rev-parse", "HEAD")
	for _, p := range paths {
		fmt.Fprintf(&in, "160000 %s\t%s\x00", oid, p)
	}
	c := exec.Command("git", "update-index", "--add", "-z", "--index-info")
	c.Dir, c.Stdin = dir, &in
	c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	if out, err := c.CombinedOutput(); err != nil {
		w.t.Fatalf("%s %v", out, err)
	}
	w.git(dir, "commit", "-qm", "gitlinks")
}

func longPath(i, comps int) string {
	parts := make([]string, comps)
	for j := range parts {
		parts[j] = fmt.Sprintf("%s%03d%03d", strings.Repeat("x", 190), i, j)
	}
	return strings.Join(parts, "/")
}

func TestPinsBoundedResultsValidateAgainstSchema(t *testing.T) {
	for name, tc := range map[string]struct {
		comps, n int
		status   string
		maxSubs  int
	}{
		"more than 256 submodules":   {1, 257, "incomplete", 256},
		"early bound in enumeration": {19, 256, "incomplete", 256},
		"result beyond one MiB":      {7, 256, "incomplete", 255},
	} {
		t.Run(name, func(t *testing.T) {
			w := newPinsWorld(t)
			dir := w.repo("many")
			paths := make([]string, tc.n)
			for i := range paths {
				paths[i] = longPath(i, tc.comps)
				if tc.comps == 1 {
					paths[i] = fmt.Sprintf("m%03d", i)
				}
			}
			gitlinks(w, dir, paths)
			r := w.check(dir, tc.status)
			if r.Identity == nil || len(r.Submodules) > tc.maxSubs {
				t.Fatalf("identity=%v subs=%d", r.Identity, len(r.Submodules))
			}
			var limit bool
			for _, f := range r.Findings {
				limit = limit || f.Code == "pins-limit"
			}
			if !limit {
				t.Fatal("no pins-limit")
			}
		})
	}
}

func TestPinsHostileBranchAndRemoteValidateAgainstSchema(t *testing.T) {
	w := newPinsWorld(t)
	super, _ := w.super()
	member := filepath.Join(super, "member")
	w.git(member, "checkout", "-q", "-b", "br\xff")
	w.git(member, "remote", "add", "caf\xe9", "https://example.invalid/x.git")
	r := w.check(super, "ok")
	if s := r.Submodules[0]; s.Checkout.Branch == nil || *s.Checkout.Branch != `br\xff` {
		t.Fatalf("%+v", s.Checkout)
	}
}

func TestPinsHostileSubmoduleNameIsRefusedAndEscapedPerField(t *testing.T) {
	w := newPinsWorld(t)
	super, sub := w.super()
	w.write(filepath.Join(super, ".gitmodules"), "[submodule \"n\xff\"]\n\tpath = member\n\turl = "+sub+"\n")
	w.git(super, "add", ".gitmodules")
	w.git(super, "commit", "-qm", "odd name")
	r := w.check(super, "ok")
	s := r.Submodules[0]
	if s.Name == nil || *s.Name != `n\xff` || s.Refusal == nil || s.Refusal.Code != "submodule-path-unsafe" {
		t.Fatalf("%v %+v", s.Name, s.Refusal)
	}
}

func TestPinsFindingScenariosValidateAgainstSchema(t *testing.T) {
	for name, mutate := range map[string]func(w pinsWorld, super, sub string){
		"clean":      func(w pinsWorld, super, sub string) {},
		"untracked":  func(w pinsWorld, super, sub string) { w.write(filepath.Join(super, "member", "new.txt"), "x") },
		"jj managed": func(w pinsWorld, super, sub string) { _ = os.MkdirAll(filepath.Join(super, "member", ".jj"), 0o755) },
		"unsafe pointer": func(w pinsWorld, super, sub string) {
			w.write(filepath.Join(super, "member", ".git"), "gitdir: /tmp/elsewhere\n")
		},
		"credentials": func(w pinsWorld, super, sub string) {
			w.write(filepath.Join(super, ".gitmodules"), "[submodule \"member\"]\n\tpath = member\n\turl = https://user:secret@example.invalid/r.git\n")
			w.git(super, "add", ".gitmodules")
			w.git(super, "commit", "-qm", "creds")
		},
		"staged pin": func(w pinsWorld, super, sub string) {
			w.git(filepath.Join(super, "member"), "commit", "-q", "--allow-empty", "-m", "two")
			w.git(super, "add", "member")
		},
		"uninitialized": func(w pinsWorld, super, sub string) { gitlinks(w, super, []string{"ghost"}) },
		"detached superproject": func(w pinsWorld, super, sub string) {
			w.git(super, "checkout", "-q", "--detach")
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := newPinsWorld(t)
			super, sub := w.super()
			mutate(w, super, sub)
			w.check(super, "ok")
		})
	}
}
