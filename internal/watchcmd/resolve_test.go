// SPDX-License-Identifier: EUPL-1.2
package watchcmd

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/stwsync"
)

func TestParseRemoteAcceptsTheGitHubSpellings(t *testing.T) {
	for url, want := range map[string]string{
		"https://github.com/wstein/anchorage.git": "wstein/anchorage",
		"https://github.com/wstein/anchorage":     "wstein/anchorage",
		"https://github.com/wstein/anchorage/":    "wstein/anchorage",
		"https://user@github.com/wstein/a.b.git":  "wstein/a.b",
		"git@github.com:wstein/a.git":             "wstein/a",
		"ssh://git@github.com/wstein/a.git":       "wstein/a",
		"ssh://git@github.com:22/wstein/a.git":    "wstein/a",
		"https://GitHub.com/wstein/a":             "wstein/a",
		"https://github.com/wstein/a.git/":        "wstein/a",
		"  https://github.com/wstein/a.git\n":     "wstein/a",
	} {
		got, ok := ParseRemote(url, "github.com")
		if !ok || got != want {
			t.Errorf("%q: %q %v", url, got, ok)
		}
	}
	for _, url := range []string{"", "/srv/git/a.git", "../a", "https://example.org/wstein/a", "https://github.com/wstein", "https://github.com/wstein/a/b",
		"git@gitlab.com:wstein/a.git", "https://github.com/wstein/../a", "https://github.com.evil.example/wstein/a", "file:///github.com/wstein/a"} {
		if got, ok := ParseRemote(url, "github.com"); ok {
			t.Errorf("%q accepted as %q", url, got)
		}
	}
	if got, ok := ParseRemote("git@ghe.example.com:team/x.git", "ghe.example.com"); !ok || got != "team/x" {
		t.Fatalf("enterprise host: %q %v", got, ok)
	}
}

func TestSplitRemoteReturnsHostAndPathWithoutJudgingThem(t *testing.T) {
	for remote, want := range map[string][2]string{
		"https://github.com/wstein/a.git":     {"github.com", "wstein/a.git"},
		"git@gitlab.com:team/x":               {"gitlab.com", "team/x"},
		"ssh://git@ghe.example.com:22/t/x":    {"ghe.example.com", "t/x"},
		"https://github.com/wstein/../a":      {"github.com", "wstein/../a"},
		"  https://GitHub.com/wstein/a/\n":    {"GitHub.com", "wstein/a/"},
		"https://github.com/wstein/a/b/c.git": {"github.com", "wstein/a/b/c.git"},
	} {
		host, path, ok := SplitRemote(remote)
		if !ok || host != want[0] || path != want[1] {
			t.Errorf("%q: %q %q %v", remote, host, path, ok)
		}
	}
	for _, remote := range []string{"", "/srv/git/a.git", "../a", "file:///github.com/wstein/a", "ftp://github.com/a/b"} {
		if host, path, ok := SplitRemote(remote); ok {
			t.Errorf("%q split into %q %q", remote, host, path)
		}
	}
}

func fakeDiscover(root string, members ...stwsync.Member) func(context.Context, string) (stwsync.Workspace, error) {
	return func(context.Context, string) (stwsync.Workspace, error) {
		return stwsync.Workspace{Root: root, Members: members}, nil
	}
}

func TestResolveUnionsFlagsWorkspaceAndConfigInOrder(t *testing.T) {
	disc := fakeDiscover("/ws",
		stwsync.Member{Name: ".", Initialized: true, Remote: "https://github.com/o/root.git"},
		stwsync.Member{Name: "a", Initialized: true, Remote: "git@github.com:o/a.git"},
		stwsync.Member{Name: "gl", Initialized: true, Remote: "https://gitlab.com/o/gl"},
		stwsync.Member{Name: "loc", Initialized: true, Remote: "/srv/git/loc"},
		stwsync.Member{Name: "bare", Initialized: true},
		stwsync.Member{Name: "off", Initialized: false})
	got, err := Resolve(context.Background(), ResolveInput{
		Host: "github.com", Repos: []string{"x/first", "O/A"}, Workspace: true, WorkspacePath: "/ws/sub",
		Config: &Config{Repos: []string{"c/one", "x/FIRST"}}, Discover: disc})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"x/first", "O/A", "o/root", "c/one"}
	if !reflect.DeepEqual(got.Repos, want) {
		t.Fatalf("repos %v", got.Repos)
	}
	if got.WorkspaceRoot != "/ws" {
		t.Fatalf("root %q", got.WorkspaceRoot)
	}
	skipped := map[string]string{}
	for _, s := range got.Skipped {
		skipped[s.Path] = s.Reason
	}
	if len(got.Skipped) != 4 || !strings.Contains(skipped["gl"], "GitHub") || !strings.Contains(skipped["loc"], "GitHub") ||
		!strings.Contains(skipped["bare"], "remote") || !strings.Contains(skipped["off"], "not initialized") {
		t.Fatalf("skipped %+v", got.Skipped)
	}
	// o/a came from the workspace after the flag O/A and is a duplicate
	if len(got.Repos) != 4 {
		t.Fatal("duplicates must be removed")
	}
}

func TestResolveUsageErrors(t *testing.T) {
	in := ResolveInput{Host: "github.com", Discover: fakeDiscover("/ws")}
	if _, err := Resolve(context.Background(), in); !errors.Is(err, ErrUsage) {
		t.Fatalf("no source: %v", err)
	}
	in.Repos = []string{"nonsense"}
	if _, err := Resolve(context.Background(), in); !errors.Is(err, ErrUsage) {
		t.Fatalf("bad repo: %v", err)
	}
	in = ResolveInput{Host: "github.com", Workspace: true, Discover: func(context.Context, string) (stwsync.Workspace, error) {
		return stwsync.Workspace{}, errors.New("not a repository")
	}}
	if _, err := Resolve(context.Background(), in); !errors.Is(err, ErrUsage) {
		t.Fatalf("discovery failure: %v", err)
	}
	in = ResolveInput{Host: "github.com", Workspace: true, Discover: fakeDiscover("/ws", stwsync.Member{Name: ".", Initialized: true, Remote: "/x"})}
	if _, err := Resolve(context.Background(), in); !errors.Is(err, ErrUsage) {
		t.Fatalf("a workspace without any GitHub repository: %v", err)
	}
	var many []string
	for i := 0; i < 257; i++ {
		many = append(many, "o/r"+strings.Repeat("x", i%7)+string(rune('a'+i%26))+string(rune('a'+i/26)))
	}
	if _, err := Resolve(context.Background(), ResolveInput{Host: "github.com", Repos: many}); !errors.Is(err, ErrUsage) {
		t.Fatalf("more than 256 repositories: %v", err)
	}
}
