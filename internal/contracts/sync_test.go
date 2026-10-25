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

func syncFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root(t), "internal/cli/testdata/sync-"+name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestSyncSchemaFixtures(t *testing.T) {
	s := schema(t, "sync")
	for _, name := range []string{"plan", "run", "refused", "restore"} {
		valid(t, s, syncFixture(t, name))
	}
}

// Run IDs with and without microseconds validate; the retention release writes the longer form.
func TestSyncSchemaAcceptsBothRunIDForms(t *testing.T) {
	s := schema(t, "sync")
	for _, id := range []string{"20261021T101500Z-0a1b2c3d", "20261021T101500123456Z-0a1b2c3d"} {
		valid(t, s, mutateSync(t, "run", func(m map[string]any, _ []any) { m["run_id"] = id }))
	}
	for _, id := range []string{"20261021T10150012Z-0a1b2c3d", "20261021T101500123456-0a1b2c3d"} {
		if err := s.Validate(value(t, mutateSync(t, "run", func(m map[string]any, _ []any) { m["run_id"] = id }))); err == nil {
			t.Errorf("%s accepted", id)
		}
	}
}

// An autostash-conflict whose entry could not be identified without doubt has a null stash_oid.
func TestSyncSchemaAcceptsAnUnidentifiedStash(t *testing.T) {
	valid(t, schema(t, "sync"), mutateSync(t, "run", func(_ map[string]any, r []any) { syncRepoOf(r, "autostash-conflict")["stash_oid"] = nil }))
}

// mutateSync applies fn to a copy of a fixture and returns the JSON.
func mutateSync(t *testing.T, fixture string, fn func(m map[string]any, repos []any)) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(syncFixture(t, fixture), &m); err != nil {
		t.Fatal(err)
	}
	fn(m, m["repos"].([]any))
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func syncRepoOf(repos []any, result string) map[string]any {
	for _, r := range repos {
		if m := r.(map[string]any); m["result"] == result {
			return m
		}
	}
	panic("no repo with result " + result)
}

func TestSyncSchemaRejectsBrokenResults(t *testing.T) {
	s := schema(t, "sync")
	for name, tc := range map[string]struct {
		fixture string
		fn      func(m map[string]any, repos []any)
	}{
		"missing key":                {"plan", func(m map[string]any, _ []any) { delete(m, "error") }},
		"missing repo key":           {"plan", func(_ map[string]any, r []any) { delete(r[0].(map[string]any), "detail") }},
		"wrong command":              {"plan", func(m map[string]any, _ []any) { m["command"] = "repos" }},
		"run id on a plan":           {"plan", func(m map[string]any, _ []any) { m["run_id"] = "20261021T101500Z-0a1b2c3d" }},
		"bad run id":                 {"run", func(m map[string]any, _ []any) { m["run_id"] = "r1" }},
		"unknown mode":               {"plan", func(m map[string]any, _ []any) { m["mode"] = "undo" }},
		"run result in a plan":       {"plan", func(_ map[string]any, r []any) { syncRepoOf(r, "fast-forward")["result"] = "fast-forwarded" }},
		"plan result in a run":       {"run", func(_ map[string]any, r []any) { syncRepoOf(r, "rebased")["result"] = "rebase" }},
		"unknown result":             {"plan", func(_ map[string]any, r []any) { r[0].(map[string]any)["result"] = "merged" }},
		"unknown kind":               {"plan", func(_ map[string]any, r []any) { r[0].(map[string]any)["kind"] = "worktree" }},
		"short oid":                  {"plan", func(_ map[string]any, r []any) { r[0].(map[string]any)["old_oid"] = "abc" }},
		"fast-forward without new":   {"plan", func(_ map[string]any, r []any) { syncRepoOf(r, "fast-forward")["new_oid"] = nil }},
		"fast-forwarded without new": {"run", func(_ map[string]any, r []any) { syncRepoOf(r, "fast-forwarded")["new_oid"] = nil }},
		"new oid on up-to-date": {"plan", func(_ map[string]any, r []any) {
			syncRepoOf(r, "up-to-date")["new_oid"] = "1111111111111111111111111111111111111111"
		}},
		"new oid on failed": {"run", func(_ map[string]any, r []any) {
			syncRepoOf(r, "failed")["new_oid"] = "1111111111111111111111111111111111111111"
		}},
		"failed without reason":   {"run", func(_ map[string]any, r []any) { syncRepoOf(r, "failed")["reason"] = nil }},
		"diverged without reason": {"plan", func(_ map[string]any, r []any) { syncRepoOf(r, "diverged")["reason"] = nil }},
		"dirty without reason":    {"plan", func(_ map[string]any, r []any) { syncRepoOf(r, "skipped-dirty")["reason"] = nil }},
		"conflict without detail": {"run", func(_ map[string]any, r []any) { syncRepoOf(r, "conflict")["detail"] = nil }},
		"detail on a success":     {"run", func(_ map[string]any, r []any) { syncRepoOf(r, "fast-forwarded")["detail"] = "x" }},
		"negative ahead":          {"plan", func(_ map[string]any, r []any) { r[0].(map[string]any)["ahead"] = -1 }},
		"unknown summary result": {"plan", func(m map[string]any, _ []any) {
			m["summary"].(map[string]any)["by_result"].(map[string]any)["bogus"] = 1
		}},
		"error without code":   {"refused", func(m map[string]any, _ []any) { m["error"] = map[string]any{"message": "x"} }},
		"missing merge option": {"plan", func(m map[string]any, _ []any) { delete(m["options"].(map[string]any), "merge") }},
		"string merge option":  {"plan", func(m map[string]any, _ []any) { m["options"].(map[string]any)["merge"] = "yes" }},
		"stash oid on a success": {"run", func(_ map[string]any, r []any) {
			syncRepoOf(r, "fast-forwarded")["stash_oid"] = "3333333333333333333333333333333333333333"
		}},
		"short stash oid":           {"run", func(_ map[string]any, r []any) { syncRepoOf(r, "autostash-conflict")["stash_oid"] = "abc" }},
		"missing stash oid":         {"run", func(_ map[string]any, r []any) { delete(syncRepoOf(r, "autostash-conflict"), "stash_oid") }},
		"string option":             {"plan", func(m map[string]any, _ []any) { m["options"].(map[string]any)["prune"] = "yes" }},
		"sync result in a restore":  {"restore", func(_ map[string]any, r []any) { r[0].(map[string]any)["result"] = "fast-forwarded" }},
		"restore result in a run":   {"run", func(_ map[string]any, r []any) { r[0].(map[string]any)["result"] = "restored" }},
		"restore without run id":    {"restore", func(m map[string]any, _ []any) { m["run_id"] = nil }},
		"restored without new":      {"restore", func(_ map[string]any, r []any) { syncRepoOf(r, "restored")["new_oid"] = nil }},
		"changed-since no reason":   {"restore", func(_ map[string]any, r []any) { syncRepoOf(r, "changed-since")["reason"] = nil }},
		"incomplete without reason": {"restore", func(_ map[string]any, r []any) { syncRepoOf(r, "journal-incomplete")["reason"] = nil }},
		"null repos":                {"plan", func(m map[string]any, _ []any) { m["repos"] = nil }},
	} {
		if err := s.Validate(value(t, mutateSync(t, tc.fixture, tc.fn))); err == nil {
			t.Errorf("%s: the schema accepted a broken result", name)
		}
	}
}

// TestSyncRealOutputValidates runs the real command against a temporary workspace with a submodule and
// bare remotes and checks schema validity, the exit code and the JSON for each outcome.
func TestSyncRealOutputValidates(t *testing.T) {
	root := t.TempDir()
	// the product's own git commands (the rebase) need an identity on runners that have none
	for _, k := range []string{"GIT_AUTHOR", "GIT_COMMITTER"} {
		t.Setenv(k+"_NAME", "Tester")
		t.Setenv(k+"_EMAIL", "test@example.org")
	}
	git := func(dir string, args ...string) {
		t.Helper()
		reposRealGit(t, dir, args...)
	}
	mk := func(name string) (bare, work string) {
		bare, work = filepath.Join(root, name+".git"), filepath.Join(root, name)
		git(root, "init", "-q", "--bare", "-b", "main", bare)
		git(root, "clone", "-q", bare, work)
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
	bare, ws := mk("ws")
	git(ws, "-c", "protocol.file.allow=always", "submodule", "add", "-q", "-b", "main", subBare, "sub")
	git(ws, "commit", "-qm", "sub")
	git(ws, "push", "-q", "origin", "main")
	s := schema(t, "sync")
	run := func(want int, args ...string) map[string]any {
		t.Helper()
		var out, diag bytes.Buffer
		code := cli.Run(context.Background(), "test", append([]string{"sync"}, args...), &out, &diag)
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
	results := func(v map[string]any) map[string]string {
		m := map[string]string{}
		for _, r := range v["repos"].([]any) {
			m[r.(map[string]any)["name"].(string)] = r.(map[string]any)["result"].(string)
		}
		return m
	}
	if r := results(run(0, "--json", ws)); r["."] != "up-to-date" || r["sub"] != "up-to-date" {
		t.Fatalf("%v", r)
	}
	other := filepath.Join(root, "other")
	git(root, "clone", "-q", bare, other)
	if err := os.WriteFile(filepath.Join(other, "g"), []byte("y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(other, "add", ".")
	git(other, "commit", "-qm", "remote")
	git(other, "push", "-q", "origin", "main")
	v := run(0, "--plan", "--json", ws)
	if v["mode"] != "plan" || v["run_id"] != nil || results(v)["."] != "fast-forward" {
		t.Fatalf("%v", v)
	}
	v = run(0, "--json", ws)
	if v["mode"] != "sync" || v["run_id"] == nil || results(v)["."] != "fast-forwarded" {
		t.Fatalf("%v", v)
	}
	// --ff-only reports a diverged branch (exit 0, independent of the user's global configuration); --rebase resolves it
	if err := os.WriteFile(filepath.Join(ws, "local"), []byte("l\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(ws, "add", "local")
	git(ws, "commit", "-qm", "local")
	if err := os.WriteFile(filepath.Join(other, "h"), []byte("z\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(other, "add", ".")
	git(other, "commit", "-qm", "remote 2")
	git(other, "push", "-q", "origin", "main")
	if r := results(run(0, "--json", "--ff-only", ws)); r["."] != "diverged" {
		t.Fatalf("%v", r)
	}
	if r := results(run(0, "--json", "--rebase", ws)); r["."] != "rebased" {
		t.Fatalf("%v", r)
	}
	// restore undoes the latest run: the real output validates, and a second restore is already-restored
	v = run(0, "restore", "--json", ws)
	if v["mode"] != "restore" || v["run_id"] == nil || results(v)["."] != "restored" {
		t.Fatalf("%v", v)
	}
	if r := results(run(0, "restore", "--json", ws)); r["."] != "already-restored" {
		t.Fatalf("%v", r)
	}
	// refusals: a plain directory (JSON with error, exit 1) and usage errors (exit 2, no JSON)
	if v = run(1, "--json", t.TempDir()); v["error"] == nil {
		t.Fatalf("%v", v)
	}
	run(2, "--rebase", "--ff-only", ws)
	run(2, "restore", "--rebase", ws)
	run(2, "--bogus", ws)
}
