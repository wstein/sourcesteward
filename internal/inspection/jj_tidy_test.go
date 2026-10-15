// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestJJTidyUnavailableWorkspaceRoot(t *testing.T) {
	paths, complete := jjTidyWorkspacePaths([]InventoryWorkspace{{Path: ptr("/repo")}, {Name: "unknown"}})
	if complete || !reflect.DeepEqual(paths, []string{"/repo"}) {
		t.Fatalf("unknown workspace root became complete: %v %v", paths, complete)
	}
}

func TestJJTidyCandidateSafety(t *testing.T) {
	commit, change := strings.Repeat("a", 40), strings.Repeat("k", 32)
	base := InventoryTarget{strings.Repeat("b", 40), ptr(strings.Repeat("l", 32))}
	ref := InventoryReference{Name: "topic", CommitID: &commit, ChangeID: &change}
	for _, kind := range []string{"integrated", "not-integrated", "base", "workspace-commit", "workspace-change", "remote", "git-remote", "conflicted", "recovery"} {
		t.Run(kind, func(t *testing.T) {
			refs := []InventoryReference{ref}
			ws := []InventoryWorkspace{}
			if kind == "base" {
				refs[0].CommitID, refs[0].ChangeID = &base.CommitID, base.ChangeID
			}
			if kind == "workspace-commit" {
				ws = append(ws, InventoryWorkspace{CommitID: &commit, ChangeID: base.ChangeID, Path: ptr("/task")})
			}
			if kind == "workspace-change" {
				ws = append(ws, InventoryWorkspace{CommitID: &base.CommitID, ChangeID: &change})
			}
			if kind == "remote" || kind == "git-remote" {
				remote := "origin"
				if kind == "git-remote" {
					remote = "git"
				}
				r := ref
				r.Remote = &remote
				refs = append(refs, r)
			}
			if kind == "conflicted" {
				refs[0].CommitID, refs[0].ChangeID = nil, nil
			}
			if kind == "recovery" {
				refs[0].Name = "stw-recovery-old"
			}
			got, err := jjTidyCandidates(context.Background(), base, refs, ws, func(string) (bool, error) { return kind != "not-integrated", nil })
			if err != nil || len(got) != 1 {
				t.Fatalf("%+v %v", got, err)
			}
			want := "retain"
			if kind == "integrated" {
				want = "delete"
			}
			if got[0].Decision != want || got[0].WorkspacePaths == nil || got[0].Limitations == nil {
				t.Fatalf("%+v", got[0])
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := jjTidyCandidates(ctx, base, []InventoryReference{ref}, nil, func(string) (bool, error) { t.Fatal("query after cancellation"); return true, nil }); err == nil {
		t.Fatal("cancellation ignored")
	}
}

func TestJJTidyNativeReadOnly(t *testing.T) {
	requireJJ(t)
	for _, shape := range []string{"standalone", "colocated"} {
		t.Run(shape, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			repo := filepath.Join(root, "repo")
			jjFixture(t, root, "--config", "git.colocate="+map[string]string{"standalone": "false", "colocated": "true"}[shape], "git", "init", repo)
			requireJJSandbox(t, repo)
			if err := os.WriteFile(filepath.Join(repo, "tracked"), []byte("stored\n"), 0600); err != nil {
				t.Fatal(err)
			}
			jjFixture(t, repo, "describe", "-m", "stored")
			jjFixture(t, repo, "bookmark", "create", `"topic?"`)
			jjFixture(t, repo, "bookmark", "create", "foobar")
			jjFixture(t, repo, "bookmark", "create", "stw-recovery-keep")
			jjFixture(t, repo, "new")
			jjFixture(t, repo, "bookmark", "create", "main")
			secondary := filepath.Join(root, "secondary")
			jjFixture(t, repo, "workspace", "add", "--name", "unreferenced", secondary)
			if err := os.WriteFile(filepath.Join(repo, "unsnapshotted"), []byte("preserve\n"), 0600); err != nil {
				t.Fatal(err)
			}
			before := snapshot(t, root)
			got, err := ObserveJJTidy(context.Background(), repo, "main")
			if err != nil {
				t.Fatal(err)
			}
			if !got.WorkspacePathsComplete || !reflect.DeepEqual(got.WorkspacePaths, []string{repo, secondary}) {
				t.Fatalf("incomplete native workspace roots: %+v", got)
			}
			decisions := map[string]string{}
			for _, c := range got.Candidates {
				decisions[c.Name] = c.Decision
			}
			wantFoobar := "delete"
			if shape == "colocated" {
				wantFoobar = "retain"
			}
			if decisions["topic?"] != "delete" || decisions["foobar"] != wantFoobar || decisions["main"] != "retain" || decisions["stw-recovery-keep"] != "retain" {
				t.Fatalf("%+v", got)
			}
			if !reflect.DeepEqual(before, snapshot(t, root)) {
				t.Fatal("tidy observation changed repository")
			}
			if _, err := ObserveJJTidy(context.Background(), repo, "all()"); err == nil {
				t.Fatal("accepted revset program")
			}
		})
	}
}
