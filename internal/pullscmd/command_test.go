// SPDX-License-Identifier: EUPL-1.2
package pullscmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/stwsync"
	"github.com/wstein/sourcesteward/internal/watchcmd"
)

// fakeSource is the fake forge plus a canned owner search.
type fakeSource struct {
	*fakeForge
	owners   map[string]hosting.OwnerDiscovery
	searched []string
}

func (s *fakeSource) DiscoverOwners(_ context.Context, owners []string) []hosting.OwnerDiscovery {
	var out []hosting.OwnerDiscovery
	for _, o := range owners {
		s.searched = append(s.searched, o)
		d, ok := s.owners[strings.ToLower(o)]
		if !ok {
			d = hosting.OwnerDiscovery{Owner: o}
		}
		out = append(out, d)
	}
	return out
}

func newSource() *fakeSource {
	return &fakeSource{fakeForge: newForge(), owners: map[string]hosting.OwnerDiscovery{}}
}

func ws(root string, members ...stwsync.Member) func(context.Context, string) (stwsync.Workspace, error) {
	return func(context.Context, string) (stwsync.Workspace, error) {
		return stwsync.Workspace{Root: root, Members: members}, nil
	}
}

func member(name, remote string) stwsync.Member {
	return stwsync.Member{Name: name, Initialized: true, Remote: remote}
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
	if p.Scope != "workspace" || p.Human || p.Host != "github.com" || p.WorkspaceRoot != "/w" || len(p.Skipped) != 2 {
		t.Fatalf("%+v", p)
	}
	if p.Sources["wstein/sourcesteward"] != "workspace" {
		t.Fatalf("sources %v", p.Sources)
	}
}

func TestPrepareScopesAndSources(t *testing.T) {
	never := func(context.Context, string) (stwsync.Workspace, error) {
		t.Fatal("workspace discovery must not run")
		return stwsync.Workspace{}, nil
	}
	f := baseFlags()
	f.Repos = []string{"o/B", "o/a", "O/b"}
	p, err := prepare(context.Background(), f, &bytes.Buffer{}, never)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(p.Repos, ","); got != "o/a,o/B" || p.Scope != "repos" || p.Sources["o/a"] != "repo" {
		t.Fatalf("%v %s %v", p.Repos, p.Scope, p.Sources)
	}
	f = baseFlags()
	f.Owners = []string{"wstein", "Wstein", "org-1"}
	p, err = prepare(context.Background(), f, &bytes.Buffer{}, never)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Repos) != 0 || p.Scope != "repos" || strings.Join(p.Owners, ",") != "wstein,org-1" {
		t.Fatalf("%+v", p)
	}
	f = baseFlags()
	f.Workspace, f.WorkspacePath, f.Repos, f.Owners = true, "/w", []string{"wstein/sourcesteward"}, []string{"wstein"}
	p, err = prepare(context.Background(), f, &bytes.Buffer{}, ws("/w", member("s", "https://github.com/wstein/sourcesteward.git"), member("c", "https://github.com/wstein/crewbook.git")))
	if err != nil {
		t.Fatal(err)
	}
	if p.Scope != "mixed" || len(p.Repos) != 2 || p.Sources["wstein/sourcesteward"] != "repo" || p.Sources["wstein/crewbook"] != "workspace" {
		t.Fatalf("%+v", p)
	}
	f = baseFlags()
	f.Workspace = true
	if p, err = prepare(context.Background(), f, &bytes.Buffer{}, ws("/w", member("s", "https://github.com/wstein/s.git"))); err != nil || p.Scope != "workspace" {
		t.Fatalf("%v %+v", err, p)
	}
}

func TestPrepareOutputModeDefaultsFollowTheTerminal(t *testing.T) {
	f := baseFlags()
	f.Repos = []string{"o/a"}
	for name, c := range map[string]struct {
		human, json bool
		want        bool
	}{"default on a buffer is JSON": {false, false, false}, "--human": {true, false, true}, "--json": {false, true, false}} {
		f.Human, f.JSON = c.human, c.json
		p, err := prepare(context.Background(), f, &bytes.Buffer{}, nil)
		if err != nil || p.Human != c.want {
			t.Fatalf("%s: %v %+v", name, err, p.Human)
		}
	}
	// a character device is a terminal
	tty, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer tty.Close()
	f.Human, f.JSON = false, false
	if p, err := prepare(context.Background(), f, tty, nil); err != nil || !p.Human {
		t.Fatalf("%v %v", err, p.Human)
	}
}

func TestPrepareFilterOptionsReachTheCycle(t *testing.T) {
	f := baseFlags()
	f.Repos = []string{"o/a"}
	f.BotsOnly, f.Authors, f.DetailDrafts, f.Limit = true, []string{"alice", "dependabot[bot]"}, true, 7
	p, err := prepare(context.Background(), f, &bytes.Buffer{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	o := p.Options
	if !o.BotsOnly || o.NoBots || !o.DetailDrafts || o.Limit != 7 || len(o.Authors) != 2 || strings.Join(o.Repos, "") != "" {
		t.Fatalf("%+v", o)
	}
}

func TestPrepareRejectsUsageErrors(t *testing.T) {
	many := make([]string, 257)
	for i := range many {
		many[i] = fmt.Sprintf("o/r%d", i)
	}
	cases := map[string]func(f *Flags){
		"no-bots with bots-only": func(f *Flags) { f.NoBots, f.BotsOnly = true, true },
		"human with json":        func(f *Flags) { f.Human, f.JSON = true, true },
		"limit 0":                func(f *Flags) { f.Limit = 0 },
		"limit 501":              func(f *Flags) { f.Limit = 501 },
		"limit negative":         func(f *Flags) { f.Limit = -1 },
		"author with a space":    func(f *Flags) { f.Authors = []string{"a b"} },
		"author too long":        func(f *Flags) { f.Authors = []string{strings.Repeat("a", 40)} },
		"author bot suffix twice": func(f *Flags) {
			f.Authors = []string{"x[bot][bot]"}
		},
		"empty author":           func(f *Flags) { f.Authors = []string{""} },
		"owner leading hyphen":   func(f *Flags) { f.Owners = []string{"-x"} },
		"owner doubled hyphen":   func(f *Flags) { f.Owners = []string{"a--b"} },
		"owner with a plus":      func(f *Flags) { f.Owners = []string{"a+b"} },
		"owner with a colon":     func(f *Flags) { f.Owners = []string{"a:b"} },
		"owner too long":         func(f *Flags) { f.Owners = []string{strings.Repeat("a", 40)} },
		"empty owner":            func(f *Flags) { f.Owners = []string{""} },
		"repo without a slash":   func(f *Flags) { f.Repos = []string{"nope"} },
		"repo with a query":      func(f *Flags) { f.Repos = []string{"o/r?x=1"} },
		"host with a path":       func(f *Flags) { f.Host = "github.com/x" },
		"path without workspace": func(f *Flags) { f.Repos, f.WorkspacePath = []string{"o/r"}, "/w" },
		"too many repositories":  func(f *Flags) { f.Repos = many },
	}
	for name, mutate := range cases {
		f := baseFlags()
		f.Repos = []string{"o/r"}
		mutate(&f)
		_, err := prepare(context.Background(), f, &bytes.Buffer{}, ws("/w"))
		if !errors.Is(err, watchcmd.ErrUsage) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// outside a workspace the default discovery is a usage error
	outside := func(context.Context, string) (stwsync.Workspace, error) {
		return stwsync.Workspace{}, errors.New("not a workspace")
	}
	if _, err := prepare(context.Background(), baseFlags(), &bytes.Buffer{}, outside); !errors.Is(err, watchcmd.ErrUsage) {
		t.Errorf("outside a workspace: %v", err)
	}
	// a workspace without a GitHub remote is a usage error too
	if _, err := prepare(context.Background(), baseFlags(), &bytes.Buffer{}, ws("/w", member("x", "https://gitlab.com/a/b.git"))); !errors.Is(err, watchcmd.ErrUsage) {
		t.Errorf("no GitHub remote: %v", err)
	}
}

func TestPrepareAcceptsFilterCombinationsOfTheSpec(t *testing.T) {
	for name, mutate := range map[string]func(f *Flags){
		"bots-only with an author": func(f *Flags) { f.BotsOnly, f.Authors = true, []string{"alice"} },
		"no-bots with an author":   func(f *Flags) { f.NoBots, f.Authors = true, []string{"alice"} },
		"bot author suffix":        func(f *Flags) { f.Authors = []string{"dependabot[bot]"} },
		"limit 1":                  func(f *Flags) { f.Limit = 1 },
		"limit 500":                func(f *Flags) { f.Limit = 500 },
		"enterprise host":          func(f *Flags) { f.Host = "ghe.example.com" },
		"256 repositories": func(f *Flags) {
			f.Repos = nil
			for i := 0; i < 256; i++ {
				f.Repos = append(f.Repos, fmt.Sprintf("o/r%d", i))
			}
		},
		"39 character owner": func(f *Flags) { f.Owners = []string{strings.Repeat("a", 39)} },
	} {
		f := baseFlags()
		f.Repos = []string{"o/r"}
		mutate(&f)
		if _, err := prepare(context.Background(), f, &bytes.Buffer{}, nil); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// exec runs the command over a fake source and returns stdout, stderr and the error.
func exec(t *testing.T, src *fakeSource, f Flags, discover func(context.Context, string) (stwsync.Workspace, error)) (string, string, error) {
	t.Helper()
	var out, diag bytes.Buffer
	p, err := prepare(context.Background(), f, &out, discover)
	if err != nil {
		t.Fatal(err)
	}
	p.Diag, p.Now = &diag, func() time.Time { return t0 }
	p.Open = func(context.Context, string, string) (Source, func(), error) { return src, func() {}, nil }
	err = Execute(context.Background(), p)
	return out.String(), diag.String(), err
}

func pullsSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "schemas", "v1", "pulls.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	id := "https://wstein.github.io/sourcesteward/schemas/v1/pulls.schema.json"
	if err := c.AddResource(id, doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile(id)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// decode validates one JSON document against the schema and returns it.
func decode(t *testing.T, out string) map[string]any {
	t.Helper()
	if strings.Count(strings.TrimSuffix(out, "\n"), "\n") != 0 || !strings.HasSuffix(out, "\n") {
		t.Fatalf("not exactly one line: %q", out)
	}
	v, err := jsonschema.UnmarshalJSON(strings.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if err := pullsSchema(t).Validate(v); err != nil {
		t.Fatalf("schema: %v\n%s", err, out)
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(out), &m)
	return m
}

func groupNumbers(m map[string]any, group string) []int {
	var out []int
	for _, r := range m["groups"].(map[string]any)[group].([]any) {
		out = append(out, int(r.(map[string]any)["number"].(float64)))
	}
	return out
}

func sampleSource() *fakeSource {
	s := newSource()
	s.add(okPR("o/a", 1))
	pend := okPR("o/a", 2)
	pend.class, pend.age = "pending", 2*24*time.Hour
	s.add(pend)
	dirty := okPR("o/a", 3)
	dirty.state, dirty.mergeable, dirty.age = "dirty", "false", 3*24*time.Hour
	s.add(dirty)
	draft := okPR("o/b", 4)
	draft.draft, draft.age = true, 4*24*time.Hour
	s.add(draft)
	red := okPR("o/b", 5)
	red.class, red.user, red.utype, red.age = "red", "dependabot[bot]", "Bot", 5*24*time.Hour
	s.add(red)
	return s
}

func TestExecuteJSONDocumentIsOneSchemaValidLine(t *testing.T) {
	f := baseFlags()
	f.Repos, f.JSON = []string{"o/b", "o/a"}, true
	out, diag, err := exec(t, sampleSource(), f, nil)
	if err != nil || diag != "" {
		t.Fatalf("%v %q", err, diag)
	}
	m := decode(t, out)
	if m["command"] != "pulls" || m["host"] != "github.com" || m["time"] != "2026-10-22T09:00:00Z" {
		t.Fatalf("%v", m)
	}
	want := map[string][]int{"ready": {1}, "checks": {2}, "conflict": {3}, "draft": {4}, "blocked": {5}}
	for g, ns := range want {
		if got := groupNumbers(m, g); fmt.Sprint(got) != fmt.Sprint(ns) {
			t.Errorf("%s: %v", g, got)
		}
	}
	s := m["summary"].(map[string]any)
	if s["total"] != 5.0 || s["bots"] != 1.0 || s["ready"] != 1.0 || s["repos_read"] != 2.0 || s["repos_failed"] != 0.0 || s["not_read"] != 0.0 {
		t.Fatalf("%v", s)
	}
	o := m["options"].(map[string]any)
	if o["scope"] != "repos" || o["limit"] != 200.0 || o["no_bots"] != false || len(o["owners"].([]any)) != 0 {
		t.Fatalf("%v", o)
	}
	repos := m["repos"].([]any)
	if len(repos) != 2 || repos[0].(map[string]any)["repo"] != "o/a" || repos[0].(map[string]any)["source"] != "repo" || repos[0].(map[string]any)["open"] != 3.0 {
		t.Fatalf("%v", repos)
	}
	if len(m["errors"].([]any)) != 0 {
		t.Fatalf("%v", m["errors"])
	}
}

func TestExecuteFlagsFilterAndBudgetTheRows(t *testing.T) {
	cases := map[string]struct {
		mutate func(f *Flags)
		group  string
		want   string
	}{
		"no-bots":                       {func(f *Flags) { f.NoBots = true }, "blocked", "[]"},
		"bots-only":                     {func(f *Flags) { f.BotsOnly = true }, "blocked", "[5]"},
		"bots-only ready":               {func(f *Flags) { f.BotsOnly = true }, "ready", "[]"},
		"author":                        {func(f *Flags) { f.Authors = []string{"DEPENDABOT[bot]"} }, "blocked", "[5]"},
		"bots-only with a human author": {func(f *Flags) { f.BotsOnly, f.Authors = true, []string{"alice"} }, "ready", "[]"},
		"limit 1 keeps the newest only": {func(f *Flags) { f.Limit = 1 }, "blocked", "[2 3 5]"},
	}
	for name, c := range cases {
		f := baseFlags()
		f.Repos, f.JSON = []string{"o/a", "o/b"}, true
		c.mutate(&f)
		out, _, err := exec(t, sampleSource(), f, nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		m := decode(t, out)
		if got := fmt.Sprint(groupNumbers(m, c.group)); got != c.want {
			t.Errorf("%s: %s %s", name, c.group, got)
		}
	}
	// limit 1: the newest non-draft (#1) is read, the others are not-read; summary counts them
	f := baseFlags()
	f.Repos, f.JSON, f.Limit = []string{"o/a", "o/b"}, true, 1
	out, _, _ := exec(t, sampleSource(), f, nil)
	m := decode(t, out)
	if s := m["summary"].(map[string]any); s["not_read"] != 3.0 {
		t.Fatalf("not_read %v", s)
	}
	if fmt.Sprint(groupNumbers(m, "ready")) != "[1]" {
		t.Fatalf("%v", groupNumbers(m, "ready"))
	}
	// detail-drafts spends budget on the draft
	src := sampleSource()
	f = baseFlags()
	f.Repos, f.JSON, f.DetailDrafts = []string{"o/a", "o/b"}, true, true
	if _, _, err := exec(t, src, f, nil); err != nil {
		t.Fatal(err)
	}
	if src.count("detail/o/b#4") != 1 {
		t.Fatalf("draft detail not read: %v", src.calls)
	}
	src = sampleSource()
	f.DetailDrafts = false
	_, _, _ = exec(t, src, f, nil)
	if src.count("detail/o/b#4") != 0 {
		t.Fatalf("draft detail read without --detail-drafts")
	}
}

func TestExecuteHumanOutput(t *testing.T) {
	f := baseFlags()
	f.Repos, f.Human = []string{"o/a", "o/b"}, true
	src := sampleSource()
	// an untrusted title and reviewer
	src.lists["o/a"][0].Title = "evil\x1b[31m\ntitle " + strings.Repeat("x", 200)
	src.lists["o/a"][0].Reviewers = []string{"bob", "carol"}
	out, diag, err := exec(t, src, f, nil)
	if err != nil || diag != "" {
		t.Fatalf("%v %q", err, diag)
	}
	if strings.ContainsRune(out, 0x1b) || strings.Count(out, "\n") != len(strings.Split(strings.TrimSuffix(out, "\n"), "\n")) {
		t.Fatalf("control characters leaked: %q", out)
	}
	for _, want := range []string{
		"ready (1)\n", "checks (1)\n", "conflict (1)\n", "draft (1)\n", "blocked (1)\n",
		"o/a#1  1d  alice  green  clean  [reviewers: bob, carol]  evil\\x1b[31m\\ntitle ",
		"o/b#5  5d  dependabot[bot]  red  clean  title 5  (checks-red)\n",
		"o/b#4  4d  alice  -  -  title 4\n",
		"5 open pull requests in 2 repositories, 1 ready\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Index(out, "ready (1)") > strings.Index(out, "checks (1)") || strings.Index(out, "draft (1)") > strings.Index(out, "blocked (1)") {
		t.Fatalf("group order\n%s", out)
	}
	if strings.Contains(out, "evil") && strings.Contains(out, strings.Repeat("x", 130)) {
		t.Fatalf("title not bounded")
	}
}

func TestExecuteHumanSkipsEmptyGroupsAndNothingOpen(t *testing.T) {
	f := baseFlags()
	f.Repos, f.Human = []string{"o/a"}, true
	out, _, err := exec(t, newSource(), f, nil)
	if err != nil || out != "0 open pull requests in 1 repository, 0 ready\n" {
		t.Fatalf("%v %q", err, out)
	}
	f.Human, f.JSON = false, true
	out, _, err = exec(t, newSource(), f, nil)
	m := decode(t, out)
	if err != nil || m["summary"].(map[string]any)["total"] != 0.0 {
		t.Fatalf("%v %v", err, m)
	}
}

func TestExecutePartialFailureIsExitOneWithOutput(t *testing.T) {
	src := sampleSource()
	src.listErr["o/b"] = &hosting.WatchError{Code: hosting.CodeForbidden, Message: "token\nlacks access"}
	f := baseFlags()
	f.Repos, f.JSON = []string{"o/a", "o/b"}, true
	out, diag, err := exec(t, src, f, nil)
	var fail *watchcmd.Failure
	if !errors.As(err, &fail) || fail.Code != watchcmd.ExitFailure {
		t.Fatalf("%v", err)
	}
	m := decode(t, out)
	errs := m["errors"].([]any)
	if len(errs) != 1 || errs[0].(map[string]any)["code"] != "forbidden" || errs[0].(map[string]any)["repo"] != "o/b" || errs[0].(map[string]any)["owner"] != nil {
		t.Fatalf("%v", errs)
	}
	if s := m["summary"].(map[string]any); s["repos_failed"] != 1.0 || s["repos_read"] != 1.0 {
		t.Fatalf("%v", s)
	}
	if !strings.Contains(diag, "o/b") || !strings.Contains(diag, "forbidden") || strings.Count(diag, "\n") != 1 {
		t.Fatalf("stderr %q", diag)
	}
	// human run names the failure in the last line
	f.JSON, f.Human = false, true
	out, _, err = exec(t, src, f, nil)
	if !errors.As(err, &fail) || !strings.Contains(out, "1 repository could not be read\n") {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestExecuteRateLimitStopsAndMarksTheRest(t *testing.T) {
	src := sampleSource()
	src.errAt["detail/o/a#1"] = &hosting.WatchError{Code: hosting.CodeRateLimited, Message: "slow down"}
	f := baseFlags()
	f.Repos, f.JSON = []string{"o/a", "o/b"}, true
	out, _, err := exec(t, src, f, nil)
	var fail *watchcmd.Failure
	if !errors.As(err, &fail) {
		t.Fatalf("%v", err)
	}
	m := decode(t, out)
	if s := m["summary"].(map[string]any); s["repos_failed"] == 0.0 {
		t.Fatalf("%v", s)
	}
}

func TestExecuteOwnerModeMergesDedupesAndLabels(t *testing.T) {
	src := newSource()
	src.add(okPR("wstein/sourcesteward", 1))
	src.add(okPR("wstein/lab", 2))
	src.add(okPR("wstein/more", 3))
	src.owners["wstein"] = hosting.OwnerDiscovery{Owner: "wstein", Repos: []string{"Wstein/SourceSteward", "wstein/lab", "wstein/more"}}
	f := baseFlags()
	f.Owners, f.Repos, f.JSON = []string{"wstein"}, []string{"wstein/more"}, true
	f.Workspace = true
	discover := ws("/w", member("s", "https://github.com/wstein/sourcesteward.git"))
	out, diag, err := exec(t, src, f, discover)
	if err != nil || diag != "" {
		t.Fatalf("%v %q", err, diag)
	}
	m := decode(t, out)
	got := map[string]string{}
	for _, r := range m["repos"].([]any) {
		r := r.(map[string]any)
		got[r["repo"].(string)] = r["source"].(string)
	}
	want := map[string]string{"wstein/sourcesteward": "workspace", "wstein/lab": "owner", "wstein/more": "repo"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("%v", got)
	}
	if m["options"].(map[string]any)["scope"] != "mixed" || fmt.Sprint(m["options"].(map[string]any)["owners"]) != "[wstein]" {
		t.Fatalf("%v", m["options"])
	}
	if src.count("list/wstein/sourcesteward") != 1 || src.count("list/Wstein/SourceSteward") != 0 {
		t.Fatalf("a discovered duplicate was read twice: %v", src.calls)
	}
	if fmt.Sprint(src.searched) != "[wstein]" {
		t.Fatalf("%v", src.searched)
	}
}

func TestExecuteOwnerFailuresAreErrorsAndExitOne(t *testing.T) {
	for name, c := range map[string]struct {
		d     hosting.OwnerDiscovery
		codes []string
	}{
		"incomplete":              {hosting.OwnerDiscovery{Owner: "wstein", Repos: []string{"wstein/lab"}, Code: hosting.CodeSearchIncomplete, Message: "search results are incomplete"}, []string{"search-incomplete"}},
		"too many":                {hosting.OwnerDiscovery{Owner: "wstein", Repos: []string{"wstein/lab"}, Code: hosting.CodeSearchTooMany, Message: "m"}, []string{"search-too-many"}},
		"rate limited":            {hosting.OwnerDiscovery{Owner: "wstein", Code: hosting.CodeRateLimited, Message: "m"}, []string{"rate-limited"}},
		"ignored alone":           {hosting.OwnerDiscovery{Owner: "wstein", Repos: []string{"wstein/lab"}, Ignored: 2, Code: hosting.CodeMalformed, Message: "search results with an unusable repository were ignored"}, []string{"malformed-response"}},
		"ignored with incomplete": {hosting.OwnerDiscovery{Owner: "wstein", Repos: []string{"wstein/lab"}, Ignored: 2, Code: hosting.CodeSearchIncomplete, Message: "m"}, []string{"search-incomplete", "malformed-response"}},
		"ignored with too many":   {hosting.OwnerDiscovery{Owner: "wstein", Repos: []string{"wstein/lab"}, Ignored: 1, Code: hosting.CodeSearchTooMany, Message: "m"}, []string{"search-too-many", "malformed-response"}},
		"malformed and ignored":   {hosting.OwnerDiscovery{Owner: "wstein", Ignored: 1, Code: hosting.CodeMalformed, Message: "unusable Link header"}, []string{"malformed-response", "malformed-response"}},
	} {
		src := newSource()
		src.add(okPR("wstein/lab", 2))
		src.owners["wstein"] = c.d
		f := baseFlags()
		f.Owners, f.JSON = []string{"wstein"}, true
		out, diag, err := exec(t, src, f, nil)
		var fail *watchcmd.Failure
		if !errors.As(err, &fail) || fail.Code != 1 {
			t.Fatalf("%s: %v", name, err)
		}
		m := decode(t, out)
		var codes []string
		for _, e := range m["errors"].([]any) {
			e := e.(map[string]any)
			if e["owner"] != "wstein" || e["repo"] != nil {
				t.Fatalf("%s: %v", name, e)
			}
			codes = append(codes, e["code"].(string))
		}
		if fmt.Sprint(codes) != fmt.Sprint(c.codes) {
			t.Errorf("%s: %v", name, codes)
		}
		if !strings.Contains(diag, "wstein") {
			t.Errorf("%s: stderr %q", name, diag)
		}
		if len(c.d.Repos) > 0 {
			// the repositories seen are still read
			if src.count("list/wstein/lab") != 1 || len(groupNumbers(m, "ready")) != 1 {
				t.Errorf("%s: repositories seen were not read", name)
			}
		}
	}
}

func TestExecuteOwnerEmptyIsNotAnError(t *testing.T) {
	src := newSource()
	f := baseFlags()
	f.Owners, f.JSON = []string{"wstein"}, true
	out, _, err := exec(t, src, f, nil)
	if err != nil {
		t.Fatal(err)
	}
	m := decode(t, out)
	if len(m["repos"].([]any)) != 0 || len(m["errors"].([]any)) != 0 {
		t.Fatalf("%v", m)
	}
}

func TestExecuteOwnerResultsCountAgainstTheRepositoryCap(t *testing.T) {
	src := newSource()
	var repos []string
	for i := 0; i < 250; i++ {
		repos = append(repos, fmt.Sprintf("o/w%d", i))
	}
	var found []string
	for i := 0; i < 7; i++ {
		found = append(found, fmt.Sprintf("big/r%d", i))
	}
	src.owners["big"] = hosting.OwnerDiscovery{Owner: "big", Repos: found}
	f := baseFlags()
	f.Repos, f.Owners, f.JSON = repos, []string{"big"}, true
	var out, diag bytes.Buffer
	p, err := prepare(context.Background(), f, &out, nil)
	if err != nil {
		t.Fatal(err)
	}
	p.Diag = &diag
	p.Open = func(context.Context, string, string) (Source, func(), error) { return src, func() {}, nil }
	err = Execute(context.Background(), p)
	if !errors.Is(err, watchcmd.ErrUsage) || !strings.Contains(err.Error(), "257") || out.Len() != 0 || src.count("list/") != 0 {
		t.Fatalf("%v %q %v", err, out.String(), src.calls)
	}
}

func TestExecuteReaderFailureIsExitOne(t *testing.T) {
	var out bytes.Buffer
	f := baseFlags()
	f.Repos = []string{"o/a"}
	p, err := prepare(context.Background(), f, &out, nil)
	if err != nil {
		t.Fatal(err)
	}
	p.Open = func(context.Context, string, string) (Source, func(), error) {
		return nil, func() {}, errors.New("gh is missing")
	}
	err = Execute(context.Background(), p)
	var fail *watchcmd.Failure
	if !errors.As(err, &fail) || fail.Code != 1 || out.Len() != 0 || !strings.Contains(err.Error(), "gh is missing") {
		t.Fatalf("%v", err)
	}
}

func TestExecuteReportsSkippedMembersOnStderr(t *testing.T) {
	f := baseFlags()
	f.JSON = true
	discover := ws("/w", member("s", "https://github.com/wstein/s.git"), stwsync.Member{Name: "cold"}, member("lab", "https://gitlab.com/a/b.git"))
	_, diag, err := exec(t, newSource(), f, discover)
	if err != nil || !strings.Contains(diag, "cold") || !strings.Contains(diag, "lab") {
		t.Fatalf("%v %q", err, diag)
	}
}
