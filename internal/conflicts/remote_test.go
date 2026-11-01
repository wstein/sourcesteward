// SPDX-License-Identifier: EUPL-1.2
package conflicts

import (
	"context"
	"strings"
	"testing"
)

func TestParseGitHubURL(t *testing.T) {
	for url, want := range map[string]string{
		"https://github.com/wstein/sourcesteward.git":   "wstein/sourcesteward",
		"https://github.com/wstein/sourcesteward":       "wstein/sourcesteward",
		"https://github.com/wstein/sourcesteward/":      "wstein/sourcesteward",
		"https://github.com/wstein/source_steward.git":  "wstein/source_steward",
		"git@github.com:wstein/sourcesteward.git":       "wstein/sourcesteward",
		"ssh://git@github.com/wstein/sourcesteward.git": "wstein/sourcesteward",
		"https://gitlab.com/wstein/sourcesteward.git":   "",
		"https://github.com/wstein":                     "",
		"https://github.com/wstein/a/b":                 "",
		"https://user:tok@github.com/wstein/x.git":      "",
		"/some/local/path.git":                          "",
		"https://github.com.evil.example/wstein/x.git":  "",
		"https://github.com/wstein/x y.git":             "",
	} {
		if got := ParseGitHubURL(url, "github.com"); got != want {
			t.Errorf("%q: %q, want %q", url, got, want)
		}
	}
}

func TestRunnerRemoteRepo(t *testing.T) {
	f := newRepoFixture(t)
	r := NewRunner(f.dir)
	defer r.Close()
	ctx := context.Background()
	git(t, f.dir, "remote", "add", "up", "https://github.com/Wstein/SourceSteward.git")
	if got, err := r.RemoteRepo(ctx, "up", "github.com"); err != nil || got != "Wstein/SourceSteward" {
		t.Fatalf("%q %v", got, err)
	}
	if got, err := r.RemoteRepo(ctx, "origin", "github.com"); err != nil || got != "" {
		t.Fatalf("a local path is no GitHub repository: %q %v", got, err)
	}
	if _, err := r.RemoteRepo(ctx, "missing", "github.com"); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("%v", err)
	}
	if _, err := r.RemoteRepo(ctx, "-x", "github.com"); err == nil || !strings.Contains(err.Error(), "invalid remote name") {
		t.Fatalf("a remote name starting with a dash must be refused by name: %v", err)
	}
}

func TestProberAnswersOnlyForItsRepository(t *testing.T) {
	g := newFake()
	p := Prober{Repo: "O/A", Remote: "origin", Git: g}
	prs := []PR{{Number: 1, Base: "main", HeadRef: "x", Head: sh("a")}}
	if got := p.Probe(context.Background(), "o/b", prs); got != nil || len(g.calls) != 0 {
		t.Fatalf("another repository was probed: %v %v", got, g.calls)
	}
	if got := p.Probe(context.Background(), "o/a", prs); len(got) != 1 || len(g.calls) != 1 {
		t.Fatalf("%v %v", got, g.calls)
	}
}
