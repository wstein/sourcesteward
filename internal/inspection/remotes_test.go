// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRemoteListStrictParsing(t *testing.T) {
	good := "origin https://github.com/owner/repo.git\nupstream git@github.com:other/repo.git (push: git@github.com:owner/repo.git)\n"
	got, err := parseJJRemotes(good)
	if err != nil || len(got) != 2 || got[1].PushURLs[0] != "git@github.com:owner/repo.git" {
		t.Fatalf("%#v %v", got, err)
	}
	for _, raw := range []string{"origin URL", "origin <no URL>\n", "origin https://example.com/a b\n", "origin URL\norigin URL\n", "a\tURL\n", "a URL\r\n", "a \xff\n", "origin URL (push: URL) trailing\n", strings.Repeat("a URL\n", 1025)} {
		if _, err := parseJJRemotes(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}

func TestGitRemotesConformance(t *testing.T) {
	d := repo(t)
	git(t, d, "remote", "add", "origin", "https://github.com/owner/repo.git")
	git(t, d, "config", "--add", "remote.origin.url", "https://github.com/owner/other.git")
	git(t, d, "config", "--add", "remote.origin.pushurl", "git@github.com:owner/repo.git")
	identity := Inspect(context.Background(), d)
	before := snapshot(t, d)
	got := Remotes(context.Background(), identity)
	if got.Status != "ok" || len(got.Remotes) != 1 || len(got.Remotes[0].FetchURLs) != 2 || len(got.Remotes[0].PushURLs) != 1 {
		t.Fatalf("%#v", got)
	}
	if !reflect.DeepEqual(before, snapshot(t, d)) {
		t.Fatal("remote inspection changed repository")
	}
	git(t, d, "commit", "--allow-empty", "-qm", "new")
	got = Remotes(context.Background(), identity)
	if got.Status != "error" || got.Diagnostics[0].Code != "changed-repository" {
		t.Fatalf("stale identity accepted: %#v", got)
	}
}

func TestJJRemotesConformance(t *testing.T) {
	requireJJ(t)
	root := t.TempDir()
	d := filepath.Join(root, "repo")
	jjFixture(t, root, "git", "init", d)
	requireJJSandbox(t, d)
	jjFixture(t, d, "git", "remote", "add", "origin", "https://github.com/owner/repo.git")
	if err := os.WriteFile(filepath.Join(d, "unsnapshotted"), []byte("unsnapshotted\n"), 0600); err != nil {
		t.Fatal(err)
	}
	identity := Inspect(context.Background(), d)
	before := snapshot(t, root)
	got := Remotes(context.Background(), identity)
	if got.Status != "ok" || len(got.Remotes) != 1 || got.Remotes[0].FetchURLs[0] != "https://github.com/owner/repo.git" {
		t.Fatalf("%#v", got)
	}
	if !reflect.DeepEqual(before, snapshot(t, root)) {
		t.Fatal("native jj remote inspection changed repository or snapshotted edits")
	}
}

func TestRemotesRejectInvalidIdentityAndCancellation(t *testing.T) {
	for _, id := range []Result{{}, {Status: "ok", Backend: "ambiguous"}, {Status: "ok", Backend: "git"}} {
		if got := Remotes(context.Background(), id); got.Status != "error" || len(got.Remotes) != 0 {
			t.Fatalf("%#v", got)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := Remotes(ctx, Result{}); got.Status != "error" || got.Diagnostics[0].Code != "canceled" {
		t.Fatalf("%#v", got)
	}
}

func TestRemotesRacesFailuresAndBounds(t *testing.T) {
	id := Result{Status: "ok", Backend: "git", InputPath: "/repo", Repository: &Repository{}}
	for _, kind := range []string{"stable", "remote-changed", "identity-changed", "read-failed", "canceled"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			inspections, reads := 0, 0
			inspect := func(context.Context, string) Result {
				inspections++
				r := id
				if kind == "identity-changed" && inspections == 2 {
					r.Backend = "jj"
				}
				return r
			}
			read := func(context.Context, Result) ([]Remote, error) {
				reads++
				if kind == "read-failed" {
					return nil, errors.New("sensitive detail")
				}
				if kind == "canceled" {
					cancel()
				}
				name := "origin"
				if kind == "remote-changed" && reads == 2 {
					name = "upstream"
				}
				return []Remote{{Name: name, FetchURLs: []string{"https://github.com/o/r"}, PushURLs: []string{"https://github.com/o/r"}}}, nil
			}
			got := remotes(ctx, id, inspect, read)
			if kind == "stable" {
				if got.Status != "ok" {
					t.Fatal(got)
				}
				return
			}
			if got.Status != "error" || len(got.Remotes) != 0 || len(got.Diagnostics) != 1 || strings.Contains(got.Diagnostics[0].Message, "sensitive") {
				t.Fatal(got)
			}
		})
	}
	for _, kind := range []string{"duplicate", "unterminated", "empty-url", "whitespace", "count", "bytes", "error"} {
		t.Run(kind, func(t *testing.T) {
			run := func(_ context.Context, _ string, args ...string) (string, int, error) {
				if len(args) == 1 {
					if kind == "duplicate" {
						return "origin\norigin\n", 0, nil
					}
					return "origin\n", 0, nil
				}
				switch kind {
				case "unterminated":
					return "URL", 0, nil
				case "empty-url":
					return "", 0, nil
				case "whitespace":
					return "URL other\n", 0, nil
				case "count":
					return strings.Repeat("URL\n", 1025), 0, nil
				case "bytes":
					return strings.Repeat("x", outputLimit) + "\n", 0, nil
				case "error":
					return "secret", 1, nil
				}
				return "URL\n", 0, nil
			}
			if _, err := readGitRemotes(context.Background(), id, run); err == nil {
				t.Fatal("malformed Git remotes accepted")
			}
		})
	}
}
