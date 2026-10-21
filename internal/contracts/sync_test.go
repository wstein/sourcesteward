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
	for _, name := range []string{"plan", "refused"} {
		valid(t, s, syncFixture(t, name))
	}
}

// mutateSync applies fn to a copy of the plan fixture and returns the JSON.
func mutateSync(t *testing.T, fn func(m map[string]any, first map[string]any)) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(syncFixture(t, "plan"), &m); err != nil {
		t.Fatal(err)
	}
	fn(m, m["items"].([]any)[0].(map[string]any))
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestSyncSchemaRejectsBrokenResults(t *testing.T) {
	s := schema(t, "sync")
	for name, fn := range map[string]func(map[string]any, map[string]any){
		"missing key":              func(m, _ map[string]any) { delete(m, "error") },
		"missing item key":         func(_, it map[string]any) { delete(it, "reason") },
		"wrong command":            func(m, _ map[string]any) { m["command"] = "repos" },
		"run id on a plan":         func(m, _ map[string]any) { m["run_id"] = "r1" },
		"unknown mode":             func(m, _ map[string]any) { m["mode"] = "sync" },
		"null items":               func(m, _ map[string]any) { m["items"] = nil },
		"unknown result":           func(_, it map[string]any) { it["result"] = "merged" },
		"short oid":                func(_, it map[string]any) { it["old_oid"] = "abc" },
		"null old oid":             func(_, it map[string]any) { it["old_oid"] = nil },
		"new oid on ahead":         func(_, it map[string]any) { it["new_oid"] = "1111111111111111111111111111111111111111" },
		"fast-forward without new": func(m, _ map[string]any) { m["items"].([]any)[4].(map[string]any)["new_oid"] = nil },
		"skipped-dirty no reason":  func(m, _ map[string]any) { m["items"].([]any)[1].(map[string]any)["reason"] = nil },
		"diverged no reason":       func(m, _ map[string]any) { m["items"].([]any)[2].(map[string]any)["reason"] = nil },
		"submodule entries":        func(m, _ map[string]any) { m["submodules"] = []any{map[string]any{}} },
		"unknown summary result":   func(m, _ map[string]any) { m["summary"].(map[string]any)["by_result"].(map[string]any)["merged"] = 1 },
		"negative untracked":       func(m, _ map[string]any) { m["summary"].(map[string]any)["untracked"] = -1 },
		"error without code":       func(m, _ map[string]any) { m["error"] = map[string]any{"message": "x"} },
		"null pruned refs":         func(m, _ map[string]any) { m["fetch"].(map[string]any)["pruned_refs"] = nil },
		"string option":            func(m, _ map[string]any) { m["options"].(map[string]any)["prune"] = "yes" },
	} {
		if err := s.Validate(value(t, mutateSync(t, fn))); err == nil {
			t.Errorf("%s: the schema accepted a broken result", name)
		}
	}
}

// TestSyncRealOutputValidates runs the real command against temporary repositories with a bare remote
// and checks schema validity, the exit code and the JSON/human agreement for each outcome.
func TestSyncRealOutputValidates(t *testing.T) {
	f := newReposRealFixture(t)
	bare := filepath.Join(f.dir, "origin.git")
	reposRealGit(t, f.dir, "init", "-q", "--bare", "-b", "main", bare)
	reposRealGit(t, f.clean, "remote", "add", "origin", bare)
	reposRealGit(t, f.clean, "push", "-q", "-u", "origin", "main")
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
	// up to date: exit 0
	v := run(0, "--plan", "--json", f.clean)
	if v["error"] != nil || v["fetch"].(map[string]any)["performed"] != true || v["summary"].(map[string]any)["total"] != float64(1) {
		t.Fatalf("%v", v)
	}
	// a remote move and a dirty tracked file: skipped-dirty, exit 1
	other := filepath.Join(f.dir, "other")
	reposRealGit(t, f.dir, "clone", "-q", bare, other)
	if err := os.WriteFile(filepath.Join(other, "g"), []byte("y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reposRealGit(t, other, "add", ".")
	reposRealGit(t, other, "commit", "-qm", "remote")
	reposRealGit(t, other, "push", "-q", "origin", "main")
	v = run(0, "--dry-run", "--json", f.clean)
	if v["items"].([]any)[0].(map[string]any)["result"] != "fast-forward" {
		t.Fatalf("%v", v["items"])
	}
	if err := os.WriteFile(filepath.Join(f.clean, "f"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v = run(1, "--plan", "--json", f.clean)
	if v["items"].([]any)[0].(map[string]any)["result"] != "skipped-dirty" {
		t.Fatalf("%v", v["items"])
	}
	// refusals: a plain directory (JSON with error, exit 1) and usage errors (exit 2, no JSON)
	v = run(1, "--plan", "--json", f.plain)
	if v["error"] == nil {
		t.Fatalf("%v", v)
	}
	run(2, f.clean)
	run(2, "--plan", "--remote", "-x", f.clean)
}
