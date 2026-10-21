// SPDX-License-Identifier: EUPL-1.2
package restack

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// snapshot records every file under root (the repository, its worktrees and everything beside
// them): kind, mode and content digest or link target. Timestamps are not part of it.
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			target, _ := os.Readlink(path)
			out[rel] = "l:" + target
		case d.IsDir():
			out[rel] = fmt.Sprintf("d:%v", info.Mode().Perm())
		default:
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(b)
			out[rel] = fmt.Sprintf("f:%v:%s", info.Mode().Perm(), hex.EncodeToString(sum[:]))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func assertUnchanged(t *testing.T, name, root string, before map[string]string) {
	t.Helper()
	after := snapshot(t, root)
	if reflect.DeepEqual(before, after) {
		return
	}
	for path, v := range after {
		if before[path] != v {
			t.Errorf("%s: %s changed or appeared: %q -> %q", name, path, before[path], v)
		}
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			t.Errorf("%s: %s disappeared", name, path)
		}
	}
}

// plant installs helpers that would leave a marker if anything ran them: every hook that a
// porcelain command could start, a signing program and a merge driver in the configuration.
func plant(t *testing.T, s *scenario) string {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "executed")
	script := "#!/bin/sh\necho \"$0\" >> '" + marker + "'\nexit 0\n"
	for _, name := range []string{"pre-rebase", "post-rewrite", "reference-transaction", "post-checkout", "post-commit", "post-merge", "pre-commit", "commit-msg", "prepare-commit-msg", "post-index-change", "fsmonitor-watchman", "pre-auto-gc", "pre-push"} {
		if err := os.WriteFile(filepath.Join(s.dir, ".git", "hooks", name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	program := filepath.Join(filepath.Dir(marker), "gpg-program")
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	s.git("config", "commit.gpgsign", "true")
	s.git("config", "tag.gpgsign", "true")
	s.git("config", "gpg.program", program)
	s.git("config", "gc.auto", "1")
	return marker
}

func TestAPlanWritesNothingToTheRepositoryOnAnyPath(t *testing.T) {
	type tc struct {
		name  string
		build func(*testing.T) *scenario
		opts  func(*scenario) Options
		ctx   func() (context.Context, context.CancelFunc)
	}
	same := func(s *scenario) Options { return s.options() }
	live := func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) }
	conflict := func(t *testing.T) *scenario {
		return rebaseMergedWith(t, func(r *repo) { r.commit("B1", map[string]string{"parent1.txt": "p1\n"}) },
			func(r *repo) { r.commit("C1", map[string]string{"shared.txt": "line1\nchild\nline3\n"}) },
			func(r *repo) { r.commit("M", map[string]string{"shared.txt": "line1\nmain\nline3\n"}) })
	}
	cases := []tc{
		{"clean", func(t *testing.T) *scenario { return rebaseMerged(t) }, same, live},
		{"conflict", conflict, same, live},
		{"refusal missing branch", func(t *testing.T) *scenario { return rebaseMerged(t) }, func(s *scenario) Options { o := s.options(); o.Branch = "refs/heads/none"; return o }, live},
		{"refusal hazard", func(t *testing.T) *scenario {
			s := rebaseMerged(t)
			s.git("config", "merge.custom.driver", "true")
			return s
		}, same, live},
		{"refusal dirty", func(t *testing.T) *scenario {
			s := rebaseMerged(t)
			s.write("README", "dirty\n")
			return s
		}, same, live},
		{"canceled", func(t *testing.T) *scenario { return rebaseMerged(t) }, same, func() (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return ctx, cancel
		}},
		{"bound", oversizeRange, same, live},
	}
	for _, c := range cases {
		s := c.build(t)
		marker := plant(t, s)
		root := filepath.Dir(s.dir)
		before := snapshot(t, root)
		ctx, cancel := c.ctx()
		res := Plan(ctx, c.opts(s))
		cancel()
		assertUnchanged(t, c.name, root, before)
		if _, err := os.Stat(marker); err == nil {
			b, _ := os.ReadFile(marker)
			t.Errorf("%s: a hook, signing program or helper ran:\n%s", c.name, b)
		}
		if res.Status == "" {
			t.Errorf("%s: no status", c.name)
		}
	}
}

func TestTheSentinelNoticesAnObjectWrite(t *testing.T) {
	s := rebaseMerged(t)
	root := filepath.Dir(s.dir)
	before := snapshot(t, root)
	// A plain merge-tree without the quarantine writes objects into the repository: the control.
	s.git("merge-tree", "--write-tree", "--merge-base="+s.oldParent, s.newParent, s.oldHead)
	if reflect.DeepEqual(before, snapshot(t, root)) {
		t.Fatal("the sentinel cannot see a merge-tree object write")
	}
}

func TestACancelledPlanIsIncompleteAndDeadlineExpiryToo(t *testing.T) {
	s := rebaseMerged(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := Plan(ctx, s.options())
	if res.Status != StatusIncomplete || firstCode(res) != "canceled" || res.Evidence != nil {
		t.Fatalf("%s %+v", res.Status, res.Diagnostics)
	}
	ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	res = Plan(ctx, s.options())
	if res.Status != StatusIncomplete || firstCode(res) != "timeout" {
		t.Fatalf("%s %+v", res.Status, res.Diagnostics)
	}
}
