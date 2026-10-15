// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestJJWorkspaceNativeCreationPreservesSource(t *testing.T) {
	if os.Getenv("STW_TEST_JJ_WORKSPACES") != "1" && os.Getenv("STW_REQUIRE_JJ") != "1" {
		t.Skip("explicit native jj workspace test")
	}
	bin, err := exec.LookPath("jj")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "home")
	if err = os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("JJ_CONFIG", "")
	source := filepath.Join(root, "source")
	if err = os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	native := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Dir = dir
		out, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("jj %v: %v %s", args, e, out)
		}
		return strings.TrimSpace(string(out))
	}
	native(source, "git", "init", "--colocate")
	if err = os.WriteFile(filepath.Join(source, "tracked"), []byte("stored\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(source, ".gitattributes"), []byte("tracked filter=evil\n"), 0600); err != nil {
		t.Fatal(err)
	}
	native(source, "--config", `user.name="Fixture"`, "--config", `user.email="fixture@localhost"`, "describe", "-m", "base")
	sentinel := filepath.Join(home, "undeclared-helper-ran")
	helper := filepath.Join(home, "helper")
	if err = os.WriteFile(helper, []byte("#!/bin/sh\ntouch '"+sentinel+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	gitConfig := exec.Command("git", "-C", source, "config", "filter.evil.smudge", helper)
	if out, e := gitConfig.CombinedOutput(); e != nil {
		t.Fatalf("fixture git config: %v %s", e, out)
	}
	base := native(source, "--ignore-working-copy", "log", "-r", "@", "--no-graph", "-T", "commit_id")
	if err = os.WriteFile(filepath.Join(source, "tracked"), []byte("dirty\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(source, "untracked"), []byte("keep\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"all()", "@-", "trunk()", "root()"} {
		if _, e := ObserveJJWorkspace(context.Background(), source, invalid); e == nil {
			t.Fatalf("accepted revset program %q", invalid)
		}
	}
	observed, err := ObserveJJWorkspace(context.Background(), source, base)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "task")
	if err = os.Mkdir(destination, 0700); err != nil {
		t.Fatal(err)
	}
	native(source, "bookmark", "create", "moved", "--revision", base)
	stale, e := CreateJJWorkspace(context.Background(), source, observed, "stw-task", destination)
	if e == nil || stale.Phase != "not-started" {
		t.Fatalf("accepted stale operation: %+v %v", stale, e)
	}
	entries, e := os.ReadDir(destination)
	if e != nil || len(entries) != 0 {
		t.Fatalf("stale destination effects: %v %v", entries, e)
	}
	observed, err = ObserveJJWorkspace(context.Background(), source, base)
	if err != nil {
		t.Fatal(err)
	}
	if e := CheckJJWorkspaceTaskAvailable(context.Background(), source, "default"); e == nil {
		t.Fatal("accepted occupied native name")
	}
	collision, e := CreateJJWorkspace(context.Background(), source, observed, "default", destination)
	if e == nil || collision.Phase != "not-started" {
		t.Fatalf("native name collision mutated: %+v %v", collision, e)
	}
	created, err := CreateJJWorkspace(context.Background(), source, observed, "stw-task", destination)
	if err != nil {
		t.Fatalf("creation: %+v %v", created, err)
	}
	if created.Phase != "materialized" || created.Workspace == nil || !created.SourceUnchanged {
		t.Fatalf("incomplete: %+v", created)
	}
	b, err := os.ReadFile(filepath.Join(destination, "tracked"))
	if err != nil || string(b) != "stored\n" {
		t.Fatalf("destination: %q %v", b, err)
	}
	for p, want := range map[string]string{"tracked": "dirty\n", "untracked": "keep\n"} {
		b, e := os.ReadFile(filepath.Join(source, p))
		if e != nil || string(b) != want {
			t.Fatalf("source %s: %q %v", p, b, e)
		}
	}
	if _, e := os.Lstat(sentinel); !os.IsNotExist(e) {
		t.Fatalf("undeclared helper ran: %v", e)
	}
	if _, err = os.Lstat(filepath.Join(destination, ".git")); !os.IsNotExist(err) {
		t.Fatalf("unexpected colocated Git metadata: %v", err)
	}
	if _, err = CreateJJWorkspace(context.Background(), source, observed, "stw-task", destination); err == nil {
		t.Fatal("reused existing destination")
	}
	verified, err := VerifyJJWorkspace(context.Background(), source, "stw-task", destination, observed.Source.RepositoryDir)
	if err != nil || verified.WorkspaceRoot != destination {
		t.Fatalf("verify: %+v %v", verified, err)
	}
}

func TestJJWorkspaceWriteScopeValidation(t *testing.T) {
	for _, name := range []string{"", "-x", "a/b", "a\x00b"} {
		if validJJTaskName(name) {
			t.Fatalf("accepted task name %q", name)
		}
	}
	if !validJJTaskName("stw-task-12") {
		t.Fatal("valid name rejected")
	}
}
