// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestJJInventoryMachineRecords(t *testing.T) {
	for _, value := range []string{"null\n", "\"x\"", "\"x\"\n\n", "\"x\"\ntrue\n"} {
		if _, err := jjInventoryRecords(value, 3); err == nil {
			t.Fatalf("accepted malformed records %q", value)
		}
	}
	if _, err := jjInventoryRecords("\""+string([]byte{255})+"\"\n", 1); err == nil {
		t.Fatal("accepted non-UTF8 records")
	}
	if _, err := jjInventoryRecords(strings.Repeat("null\n", (inventoryRecordLimit+1)*3), 3); err == nil {
		t.Fatal("record budget ignored")
	}
}

func TestJJInventoryPredicatesAndOperationPin(t *testing.T) {
	op := strings.Repeat("a", 128)
	commit := strings.Repeat("b", 40)
	change := strings.Repeat("k", 32)
	identity := Result{Status: "ok", Backend: "jj", InputPath: "/repo", JJRepository: &JJRepository{WorkspaceRoot: "/repo", CommitID: commit, ChangeID: change, OperationID: op}}
	for _, kind := range []string{"false", "true", "concurrent", "malformed", "canceled"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			q := func(operation string, args ...string) (string, error) {
				calls++
				if args[0] == "operation" {
					if kind == "concurrent" && calls > 1 {
						return `"` + strings.Repeat("c", 128) + "\"\n", nil
					}
					return `"` + op + "\"\n", nil
				}
				if operation != op {
					t.Fatal("query unpinned", operation)
				}
				if kind == "canceled" {
					cancel()
					return "", ctx.Err()
				}
				if kind == "malformed" {
					return "not-json\n", nil
				}
				if args[0] == "bookmark" && kind == "true" {
					return `"branch"` + "\nnull\nfalse\n\"" + commit + "\"\n\"" + change + "\"\nfalse\n[\"" + commit + "\"]\n[]\n", nil
				}
				return "", nil
			}
			r := observeJJInventory(ctx, identity, InventoryOptions{Query: "ref-exists", Ref: "branch"}, q)
			switch kind {
			case "false", "true":
				if r.Status != "ok" || r.Query == nil || r.Query.Value != kind {
					t.Fatalf("%#v", r)
				}
			default:
				if r.Status != "error" {
					t.Fatalf("accepted %s: %#v", kind, r)
				}
			}
		})
	}
}

func TestJJInventoryConformance(t *testing.T) {
	requireJJ(t)
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	repo := filepath.Join(root, "repo")
	jjFixture(t, root, "git", "init", repo)
	requireJJSandbox(t, repo)
	file := "--<>&\n日本語"
	if err := os.WriteFile(filepath.Join(repo, file), []byte("stored\n"), 0600); err != nil {
		t.Fatal(err)
	}
	jjFixture(t, repo, "describe", "-m", "stored")
	jjFixture(t, repo, "bookmark", "create", "topic")
	secondary := filepath.Join(root, "second")
	jjFixture(t, repo, "workspace", "add", secondary, "--name", "second")
	if err := os.WriteFile(filepath.Join(repo, "unsnapshotted"), []byte("disk\n"), 0600); err != nil {
		t.Fatal(err)
	}
	identity := Inspect(context.Background(), repo)
	before := snapshot(t, root)
	r := inventoryJJ(context.Background(), identity, InventoryOptions{Scopes: []string{"stored"}})
	if r.Status != "ok" || !r.Boundary.Complete || len(r.Workspaces) != 2 || len(r.Files) != 1 || r.Files[0].Path != file {
		t.Fatalf("stored inventory: %#v", r)
	}
	if !reflect.DeepEqual(before, snapshot(t, root)) {
		t.Fatal("inventory mutated target")
	}
	for _, w := range r.Workspaces {
		if w.TaskOwner != "unknown" || w.Tracked.Value != "unknown" || w.OperationID == nil {
			t.Fatalf("fabricated disk/owner state: %#v", w)
		}
	}
	r = inventoryJJ(context.Background(), identity, InventoryOptions{Query: "clean", Scopes: []string{"disk", "untracked"}})
	if r.Status != "partial" || r.Query.Value != "unknown" {
		t.Fatalf("unsnapshotted declared clean: %#v", r)
	}
	r = inventoryJJ(context.Background(), identity, InventoryOptions{Query: "ref-exists", Ref: "absent"})
	if r.Status != "ok" || r.Query.Value != "false" {
		t.Fatalf("false conflated with error: %#v", r)
	}
	r = inventoryJJ(context.Background(), identity, InventoryOptions{Query: "ancestor", From: strings.Repeat("0", 40), To: identity.JJRepository.CommitID})
	if r.Status != "ok" || r.Query.Value != "true" {
		t.Fatalf("native ancestry: %#v", r)
	}
	if !reflect.DeepEqual(before, snapshot(t, root)) {
		t.Fatal("predicate mutated target")
	}
	data, err := json.Marshal(r)
	if err != nil || strings.Contains(string(data), `"references":null`) {
		t.Fatal("invalid envelope", err)
	}
}

func TestJJInventoryCanceledSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := observeJJInventory(ctx, Result{JJRepository: &JJRepository{OperationID: strings.Repeat("a", 128)}}, InventoryOptions{}, func(string, ...string) (string, error) { return "", ctx.Err() })
	if r.Status != "error" || len(r.Diagnostics) == 0 {
		t.Fatal(r)
	}
}

func TestJJInventoryPreservesNativeTargetsAndUnicode(t *testing.T) {
	commit, other := strings.Repeat("a", 40), strings.Repeat("b", 40)
	change := strings.Repeat("k", 32)
	row := func(name, remote string, conflict bool, target, changeID string, tracked bool, added, removed []string) string {
		fields := []any{name, nil, conflict, nil, nil, tracked, added, removed}
		if remote != "" {
			fields[1] = remote
		}
		if target != "" {
			fields[3] = target
			fields[4] = changeID
		}
		var out strings.Builder
		for _, field := range fields {
			b, _ := json.Marshal(field)
			out.Write(b)
			out.WriteByte('\n')
		}
		return out.String()
	}
	refs, e := jjInventoryReferences(row("topic", "", true, "", "", false, []string{commit, other}, []string{strings.Repeat("c", 40)}) + row("topic", "first", false, commit, change, true, []string{commit}, []string{}) + row("topic", "second", false, other, change, true, []string{other}, []string{}))
	if e != nil || len(refs) != 3 || refs[0].CommitID != nil || len(refs[0].AddedTargets) != 2 || len(refs[0].RemovedTargets) != 1 || len(refs[0].Upstreams) != 2 || refs[0].Upstream != nil {
		t.Fatalf("lost native targets: %#v %v", refs, e)
	}
	for _, bad := range []string{`"\ud800"`, `"\udfff"`, `"x\ud800y"`} {
		if _, e := jjString(json.RawMessage(bad), false); e == nil {
			t.Fatalf("accepted lossy unicode %s", bad)
		}
	}
	for _, good := range []string{`"\ud83d\ude00"`, `"日本語"`, `"\\ud800"`} {
		if _, e := jjString(json.RawMessage(good), false); e != nil {
			t.Fatalf("rejected valid unicode %s %v", good, e)
		}
	}
}

func TestJJInventoryStoredRevisionRenameAndDeletion(t *testing.T) {
	requireJJ(t)
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	repo := filepath.Join(root, "repo")
	jjFixture(t, root, "git", "init", repo)
	requireJJSandbox(t, repo)
	os.WriteFile(filepath.Join(repo, "old"), []byte("rename contents\n"), 0600)
	os.WriteFile(filepath.Join(repo, "removed"), []byte("delete contents\n"), 0600)
	jjFixture(t, repo, "describe", "-m", "base")
	jjFixture(t, repo, "new")
	os.Rename(filepath.Join(repo, "old"), filepath.Join(repo, "--new\n日本語"))
	os.Remove(filepath.Join(repo, "removed"))
	jjFixture(t, repo, "describe", "-m", "changed")
	jjFixture(t, repo, "bookmark", "create", "changed")
	jjFixture(t, repo, "new")
	identity := Inspect(context.Background(), repo)
	before := snapshot(t, root)
	r := inventoryJJ(context.Background(), identity, InventoryOptions{Scopes: []string{"stored"}, Revision: "changed", Paths: []string{"old"}})
	if r.Status != "ok" || len(r.Files) != 1 || r.Files[0].Change != "renamed" || r.Files[0].OldPath == nil || *r.Files[0].OldPath != "old" || r.Files[0].Path != "--new\n日本語" {
		t.Fatalf("rename selection: %#v", r)
	}
	r = inventoryJJ(context.Background(), identity, InventoryOptions{Scopes: []string{"stored"}, Revision: "changed", Paths: []string{"removed"}})
	if r.Status != "ok" || len(r.Files) != 1 || r.Files[0].Change != "deleted" {
		t.Fatalf("deletion: %#v", r)
	}
	if !reflect.DeepEqual(before, snapshot(t, root)) {
		t.Fatal("stored revision query mutated target")
	}
}

func TestJJInventoryMissingWorkspaceAndHostileConfiguration(t *testing.T) {
	requireJJ(t)
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	config := filepath.Join(root, "config")
	os.Mkdir(config, 0700)
	t.Setenv("XDG_CONFIG_HOME", config)
	repo := filepath.Join(root, "repo")
	jjFixture(t, root, "git", "init", repo)
	requireJJSandbox(t, repo)
	second := filepath.Join(root, "second")
	jjFixture(t, repo, "workspace", "add", second, "--name", "second")
	os.RemoveAll(second)
	jjFixture(t, repo, "config", "set", "--repo", `template-aliases."json(x)"`, `"hijacked"`)
	sentinel := filepath.Join(root, "helper-ran")
	helper := filepath.Join(root, "helper")
	os.WriteFile(helper, []byte("#!/bin/sh\nprintf ran > '"+sentinel+"'\n"), 0700)
	jjFixture(t, repo, "config", "set", "--repo", "ui.pager", helper)
	t.Setenv("JJ_CONFIG", helper)
	t.Setenv("GIT_TRACE", sentinel)
	identity := Inspect(context.Background(), repo)
	before := snapshot(t, root)
	r := inventoryJJ(context.Background(), identity, InventoryOptions{Scopes: []string{"stored"}})
	if r.Status != "partial" || len(r.Workspaces) != 2 || (r.Workspaces[1].Registration != "missing" && (r.Workspaces[1].Registration != "unknown" || r.Workspaces[1].Path != nil)) {
		t.Fatalf("missing registration: %#v", r)
	}
	if !reflect.DeepEqual(before, snapshot(t, root)) {
		t.Fatal("inventory repaired registration or ran helper")
	}
}
