// SPDX-License-Identifier: EUPL-1.2
package issuescmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/stwsync"
	"github.com/wstein/sourcesteward/internal/watchcmd"
)

func ws(root string, members ...stwsync.Member) func(context.Context, string) (stwsync.Workspace, error) {
	return func(context.Context, string) (stwsync.Workspace, error) {
		return stwsync.Workspace{Root: root, Members: members}, nil
	}
}

func member(name, remote string) stwsync.Member {
	return stwsync.Member{Name: name, Initialized: true, Remote: remote}
}

func never(t *testing.T) func(context.Context, string) (stwsync.Workspace, error) {
	return func(context.Context, string) (stwsync.Workspace, error) {
		t.Fatal("workspace discovery must not run")
		return stwsync.Workspace{}, nil
	}
}

func baseFlags() Flags { return Flags{Limit: 200, Host: "github.com"} }

func TestPrepareWorkspaceDiscoveryIsTheDefault(t *testing.T) {
	var asked []string
	discover := func(_ context.Context, p string) (stwsync.Workspace, error) {
		asked = append(asked, p)
		return stwsync.Workspace{Root: "/w", Members: []stwsync.Member{
			member(".", "git@github.com:wstein/anchorage-workspace.git"),
			member("sourcesteward", "https://github.com/wstein/sourcesteward.git"),
			{Name: "cold", Initialized: false},
			member("lab", "https://gitlab.com/x/y.git"),
		}}, nil
	}
	p, err := prepare(context.Background(), baseFlags(), &bytes.Buffer{}, discover)
	if err != nil {
		t.Fatal(err)
	}
	if len(asked) != 1 || asked[0] != "." {
		t.Fatalf("discovery asked %v", asked)
	}
	if got := strings.Join(p.Repos, ","); got != "wstein/anchorage-workspace,wstein/sourcesteward" {
		t.Fatalf("repos %s", got)
	}
	if p.Scope != ScopeWorkspace || p.Human || p.Host != "github.com" || p.WorkspaceRoot != "/w" || len(p.Skipped) != 2 || p.Options.Limit != 200 {
		t.Fatalf("%+v", p)
	}
	if p.Sources["wstein/sourcesteward"] != SourceWorkspace {
		t.Fatalf("sources %v", p.Sources)
	}
}

func TestPrepareScopesAndSources(t *testing.T) {
	f := baseFlags()
	f.Repos = []string{"o/B", "o/a", "O/b"}
	p, err := prepare(context.Background(), f, &bytes.Buffer{}, never(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(p.Repos, ","); got != "o/a,o/B" || p.Scope != ScopeRepos || p.Sources["o/a"] != SourceRepo {
		t.Fatalf("%v %s %v", p.Repos, p.Scope, p.Sources)
	}
	f = baseFlags()
	f.Owners = []string{"wstein", "Wstein", "org-1"}
	p, err = prepare(context.Background(), f, &bytes.Buffer{}, never(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Repos) != 0 || p.Scope != ScopeRepos || strings.Join(p.Owners, ",") != "wstein,org-1" {
		t.Fatalf("%+v", p)
	}
	f = baseFlags()
	f.Workspace, f.WorkspacePath, f.Repos, f.Owners = true, "/w", []string{"wstein/sourcesteward"}, []string{"wstein"}
	p, err = prepare(context.Background(), f, &bytes.Buffer{}, ws("/w", member("s", "https://github.com/wstein/sourcesteward.git"), member("c", "https://github.com/wstein/crewbook.git")))
	if err != nil {
		t.Fatal(err)
	}
	if p.Scope != ScopeMixed || len(p.Repos) != 2 || p.Sources["wstein/sourcesteward"] != SourceRepo || p.Sources["wstein/crewbook"] != SourceWorkspace {
		t.Fatalf("%+v", p)
	}
}

func TestPrepareOutputModeDefaultsFollowTheTerminal(t *testing.T) {
	f := baseFlags()
	f.Repos = []string{"o/a"}
	for name, c := range map[string]struct{ human, json, want bool }{
		"default on a buffer is JSON": {false, false, false}, "--human": {true, false, true}, "--json": {false, true, false},
	} {
		f.Human, f.JSON = c.human, c.json
		p, err := prepare(context.Background(), f, &bytes.Buffer{}, nil)
		if err != nil || p.Human != c.want {
			t.Fatalf("%s: %v %+v", name, err, p.Human)
		}
	}
}

func TestPrepareKeepsTheFiltersAsGiven(t *testing.T) {
	f := baseFlags()
	f.Repos = []string{"o/a"}
	f.Labels, f.Assignees, f.Authors = []string{"Bug", "type: bug"}, []string{"@me", "@none", "copilot[bot]"}, []string{"dependabot[bot]"}
	p, err := prepare(context.Background(), f, &bytes.Buffer{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	o := p.Options
	if strings.Join(o.Labels, "|") != "Bug|type: bug" || strings.Join(o.Assignees, "|") != "@me|@none|copilot[bot]" || strings.Join(o.Authors, "|") != "dependabot[bot]" {
		t.Fatalf("%+v", o)
	}
}

func TestPrepareRefusesUnusableInputWithAUsageError(t *testing.T) {
	long := strings.Repeat("x", 51)
	cases := map[string]func(f *Flags){
		"bad host":                  func(f *Flags) { f.Host = "not a host" },
		"human and json":            func(f *Flags) { f.Human, f.JSON = true, true },
		"limit 0":                   func(f *Flags) { f.Limit = 0 },
		"limit 501":                 func(f *Flags) { f.Limit = 501 },
		"path without workspace":    func(f *Flags) { f.WorkspacePath = "/w" },
		"bad owner":                 func(f *Flags) { f.Owners = []string{"a--b"} },
		"bad repo":                  func(f *Flags) { f.Repos = []string{"nope"} },
		"empty label":               func(f *Flags) { f.Labels = []string{""} },
		"label of 51 characters":    func(f *Flags) { f.Labels = []string{long} },
		"label with a control":      func(f *Flags) { f.Labels = []string{"a\x1b[31mb"} },
		"label with a newline":      func(f *Flags) { f.Labels = []string{"a\nb"} },
		"label with leading space":  func(f *Flags) { f.Labels = []string{" bug"} },
		"label with trailing space": func(f *Flags) { f.Labels = []string{"bug "} },
		"label of invalid UTF-8":    func(f *Flags) { f.Labels = []string{"a\xffb"} },
		"assignee with a space":     func(f *Flags) { f.Assignees = []string{"a b"} },
		"assignee @you":             func(f *Flags) { f.Assignees = []string{"@you"} },
		"assignee of 40 characters": func(f *Flags) { f.Assignees = []string{strings.Repeat("a", 40)} },
		"assignee with a query":     func(f *Flags) { f.Assignees = []string{"a&b=c"} },
		"author @me":                func(f *Flags) { f.Authors = []string{"@me"} },
		"author @none":              func(f *Flags) { f.Authors = []string{"@none"} },
		"author with two bot marks": func(f *Flags) { f.Authors = []string{"a[bot][bot]"} },
		"too many repositories":     func(f *Flags) { f.Repos = manyRepos(257) },
	}
	for name, mutate := range cases {
		f := baseFlags()
		f.Repos = []string{"o/a"}
		mutate(&f)
		_, err := prepare(context.Background(), f, &bytes.Buffer{}, ws("/w"))
		if !errors.Is(err, watchcmd.ErrUsage) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func manyRepos(n int) []string {
	var out []string
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf("o/r%d", i))
	}
	return out
}

func TestPrepareAcceptsTheEdgesOfTheFilterRules(t *testing.T) {
	f := baseFlags()
	f.Repos = []string{"o/a"}
	f.Labels = []string{strings.Repeat("x", 50), "ü", "a b", "x"}
	f.Assignees = []string{strings.Repeat("a", 39), "Bot-1[bot]", "@none"}
	if _, err := prepare(context.Background(), f, &bytes.Buffer{}, nil); err != nil {
		t.Fatal(err)
	}
}
