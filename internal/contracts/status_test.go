// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/wstein/sourcesteward/internal/cli"
)

func statusFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root(t), "internal/cli/testdata/status-"+name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestStatusSchemaFixtures(t *testing.T) {
	s := schema(t, "status")
	for _, name := range []string{"ok", "refused"} {
		valid(t, s, statusFixture(t, name))
	}
}

func TestStatusSchemaRejectsBrokenResults(t *testing.T) {
	s := schema(t, "status")
	repoAt := func(m map[string]any, i int) map[string]any { return m["repos"].([]any)[i].(map[string]any) }
	for name, fn := range map[string]func(m map[string]any){
		"missing key":           func(m map[string]any) { delete(m, "error") },
		"missing repo key":      func(m map[string]any) { delete(repoAt(m, 0), "gitlink") },
		"wrong command":         func(m map[string]any) { m["command"] = "sync" },
		"null repos":            func(m map[string]any) { m["repos"] = nil },
		"unknown kind":          func(m map[string]any) { repoAt(m, 0)["kind"] = "worktree" },
		"unknown state":         func(m map[string]any) { repoAt(m, 0)["state"] = "dirty" },
		"unknown operation":     func(m map[string]any) { repoAt(m, 1)["operation"] = "pull" },
		"unknown attention":     func(m map[string]any) { repoAt(m, 1)["attention"] = []any{"stash"} },
		"duplicate attention":   func(m map[string]any) { repoAt(m, 1)["attention"] = []any{"ahead", "ahead"} },
		"negative ahead":        func(m map[string]any) { repoAt(m, 0)["ahead"] = -1 },
		"short oid":             func(m map[string]any) { repoAt(m, 0)["head"] = "abc" },
		"root with gitlink":     func(m map[string]any) { repoAt(m, 0)["gitlink"] = repoAt(m, 1)["gitlink"] },
		"submodule no gitlink":  func(m map[string]any) { repoAt(m, 1)["gitlink"] = nil },
		"unknown gitlink state": func(m map[string]any) { repoAt(m, 1)["gitlink"].(map[string]any)["state"] = "moved" },
		"error state no error":  func(m map[string]any) { repoAt(m, 4)["error"] = nil },
		"error on an ok repo":   func(m map[string]any) { repoAt(m, 0)["error"] = map[string]any{"code": "status-x", "message": "m"} },
		"bad error code":        func(m map[string]any) { repoAt(m, 4)["error"].(map[string]any)["code"] = "sync-x" },
		"string option":         func(m map[string]any) { m["options"].(map[string]any)["fetch"] = "yes" },
		"unknown relation":      func(m map[string]any) { repoAt(m, 1)["gitlink"].(map[string]any)["relation"] = "sideways" },
		"worktree without missing": func(m map[string]any) {
			delete(repoAt(m, 1)["worktrees"].(map[string]any)["list"].([]any)[0].(map[string]any), "missing")
		},
		"worktree without dirty": func(m map[string]any) {
			delete(repoAt(m, 1)["worktrees"].(map[string]any)["list"].([]any)[0].(map[string]any), "dirty")
		},
		"negative summary": func(m map[string]any) { m["summary"].(map[string]any)["attention"] = -1 },
	} {
		var m map[string]any
		if err := json.Unmarshal(statusFixture(t, "ok"), &m); err != nil {
			t.Fatal(err)
		}
		fn(m)
		raw, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Validate(value(t, raw)); err == nil {
			t.Errorf("%s: the schema accepted a broken result", name)
		}
	}
}

// TestStatusRealOutputValidates runs the real command against a temporary workspace with a submodule.
func TestStatusRealOutputValidates(t *testing.T) {
	dir := t.TempDir()
	git := func(d string, args ...string) { t.Helper(); reposRealGit(t, d, args...) }
	mk := func(name string) (bare, work string) {
		bare, work = filepath.Join(dir, name+".git"), filepath.Join(dir, name)
		git(dir, "init", "-q", "--bare", "-b", "main", bare)
		git(dir, "clone", "-q", bare, work)
		git(work, "checkout", "-q", "-b", "main")
		if err := os.WriteFile(filepath.Join(work, "f"), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		git(work, "add", ".")
		git(work, "commit", "-qm", "init")
		git(work, "push", "-q", "origin", "main")
		return
	}
	subBare, _ := mk("sub")
	_, ws := mk("ws")
	git(ws, "-c", "protocol.file.allow=always", "submodule", "add", "-q", "-b", "main", subBare, "sub")
	git(ws, "commit", "-qm", "sub")
	git(ws, "push", "-q", "origin", "main")
	s := schema(t, "status")
	run := func(want int, args ...string) map[string]any {
		t.Helper()
		var out, diag bytes.Buffer
		code := cli.Run(context.Background(), "test", append([]string{"status"}, args...), &out, &diag)
		if code != want {
			t.Fatalf("%v: exit %d want %d: %s %s", args, code, want, out.String(), diag.String())
		}
		if want == 2 {
			if out.Len() != 0 {
				t.Fatalf("usage error printed output: %s", out.String())
			}
			return nil
		}
		valid(t, s, out.Bytes())
		var v map[string]any
		if err := json.Unmarshal(out.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	v := run(0, "--json", "--worktrees", filepath.Join(ws, "sub"))
	if len(v["repos"].([]any)) != 2 || v["summary"].(map[string]any)["attention"].(float64) != 0 {
		t.Fatalf("%v", v)
	}
	if err := os.WriteFile(filepath.Join(ws, "new"), []byte("n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if v = run(0, "--json", ws); v["summary"].(map[string]any)["attention"].(float64) != 1 {
		t.Fatalf("attention is not a failure: %v", v)
	}
	if v = run(1, "--json", t.TempDir()); v["error"] == nil {
		t.Fatalf("%v", v)
	}
	run(2, "--bogus", ws)
	run(2, "a", "b")
}
