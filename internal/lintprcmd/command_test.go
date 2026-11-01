// SPDX-License-Identifier: EUPL-1.2
package lintprcmd

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

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/lintpr"
	"github.com/wstein/sourcesteward/internal/stwsync"
	"github.com/wstein/sourcesteward/internal/watchcmd"
)

const (
	ownerName  = "Octo Owner"
	ownerEmail = "octo@example.com"
	ownerFlag  = "Octo Owner <octo@example.com>"
)

var sessionURL = "https://claude" + ".ai/code/session_" + "01XYZ"

func sha(c string) string { return strings.Repeat(c, 40) }

// fakeSource serves canned documents and records every call; it can only read.
type fakeSource struct {
	detail   string
	commits  map[int]string // page -> body
	comments map[int]string
	links    map[string]string // "commits/1", "comments/2" -> Link header
	errAt    map[string]error
	calls    []string
}

func (f *fakeSource) PullDetail(_ context.Context, repo string, n int, _ string) (hosting.WatchResponse, error) {
	f.calls = append(f.calls, fmt.Sprintf("detail/%s#%d", repo, n))
	if err := f.errAt["detail"]; err != nil {
		return hosting.WatchResponse{}, err
	}
	return hosting.WatchResponse{Status: 200, Body: []byte(f.detail)}, nil
}

func (f *fakeSource) page(kind string, bodies map[int]string, page int) (hosting.WatchResponse, error) {
	f.calls = append(f.calls, fmt.Sprintf("%s/%d", kind, page))
	if err := f.errAt[fmt.Sprintf("%s/%d", kind, page)]; err != nil {
		return hosting.WatchResponse{}, err
	}
	body, ok := bodies[page]
	if !ok {
		body = "[]"
	}
	return hosting.WatchResponse{Status: 200, Body: []byte(body), Link: f.links[fmt.Sprintf("%s/%d", kind, page)]}, nil
}

func (f *fakeSource) PullCommits(_ context.Context, _ string, _ int, page int, _ string) (hosting.WatchResponse, error) {
	return f.page("commits", f.commits, page)
}

func (f *fakeSource) LintComments(_ context.Context, _ string, _ int, page int) (hosting.WatchResponse, error) {
	return f.page("comments", f.comments, page)
}

func nextLink(page int) string {
	return fmt.Sprintf(`<https://api.github.com/x?page=%d>; rel="next"`, page)
}

func jsonString(s string) string { b, _ := json.Marshal(s); return string(b) }

func commitJSON(sh, msg, an, ae, cn, ce string) string {
	return fmt.Sprintf(`{"sha":%q,"commit":{"message":%s,"author":{"name":%s,"email":%s},"committer":{"name":%s,"email":%s}}}`,
		sh, jsonString(msg), jsonString(an), jsonString(ae), jsonString(cn), jsonString(ce))
}

func goodCommitJSON(sh string) string {
	return commitJSON(sh, "feat(x): y\n\nCo-Authored-By: Claude <noreply@anthropic.com>", ownerName, ownerEmail, ownerName, ownerEmail)
}

const goodBody = "Purpose.\n\nBlocker: Independent review pending\nCloses #1\n\nAuthor agent: Claude"

func detailJSON(title, body string) string {
	return detailWithCommits(title, body, 1)
}

func detailWithCommits(title, body string, commits int) string {
	return fmt.Sprintf(`{"number":7,"title":%s,"body":%s,"commits":%d,"head":{"sha":%q}}`, jsonString(title), jsonString(body), commits, sha("a"))
}

func cleanSource() *fakeSource {
	return &fakeSource{
		detail:   detailJSON("feat: y", goodBody),
		commits:  map[int]string{1: "[" + goodCommitJSON(sha("1")) + "]"},
		comments: map[int]string{1: `[{"id":11,"body":"Thanks"}]`},
		links:    map[string]string{}, errAt: map[string]error{},
	}
}

func params(src *fakeSource, out *bytes.Buffer) Params {
	return Params{Host: "github.com", Repo: "o/r", PR: 7, Owners: []lintpr.Identity{{Name: ownerName, Email: ownerEmail}}, OwnerSource: OwnerSourceFlag,
		Out: out, Open: func(context.Context, string, string) (Source, func(), error) { return src, func() {}, nil }}
}

func run(t *testing.T, src *fakeSource) (map[string]any, string, error) {
	t.Helper()
	var out bytes.Buffer
	err := Execute(context.Background(), params(src, &out))
	if out.Len() == 0 {
		return nil, "", err
	}
	return decode(t, out.String()), out.String(), err
}

func lintSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "schemas", "v1", "lint-pr.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	id := "https://wstein.github.io/sourcesteward/schemas/v1/lint-pr.schema.json"
	if err := c.AddResource(id, doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile(id)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func decode(t *testing.T, out string) map[string]any {
	t.Helper()
	if strings.Count(out, "\n") != 1 {
		t.Fatalf("not exactly one line: %q", out)
	}
	v, err := jsonschema.UnmarshalJSON(strings.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if err := lintSchema(t).Validate(v); err != nil {
		t.Fatalf("schema: %v\n%s", err, out)
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(out), &m)
	return m
}

func findingRules(m map[string]any) []string {
	var out []string
	for _, f := range m["findings"].([]any) {
		out = append(out, f.(map[string]any)["rule"].(string))
	}
	return out
}

func exitCode(err error) int {
	var f *watchcmd.Failure
	if errors.As(err, &f) {
		return f.Code
	}
	if err != nil {
		return -1
	}
	return 0
}

func TestCleanPullRequestPasses(t *testing.T) {
	m, _, err := run(t, cleanSource())
	if err != nil {
		t.Fatal(err)
	}
	s := m["summary"].(map[string]any)
	if s["passed"] != true || s["errors"] != 0.0 || s["commits"] != 1.0 || s["comments"] != 1.0 || len(m["findings"].([]any)) != 0 {
		t.Fatalf("%v", m)
	}
	if m["head"] != sha("a") || m["pull_request"] != 7.0 || m["repo"] != "o/r" || len(m["rules"].([]any)) != len(lintpr.Rules) {
		t.Fatalf("%v", m)
	}
}

func TestViolationsExitOneAndAreLocated(t *testing.T) {
	src := cleanSource()
	src.detail = detailJSON("feat: y "+sessionURL, "no structure\n"+sessionURL)
	src.commits = map[int]string{1: "[" + commitJSON(sha("2"), "bad subject "+sessionURL, "Other", "o@x.y", ownerName, ownerEmail) + "]"}
	src.comments = map[int]string{1: `[{"id":42,"body":"Review: CLEAR\nrest ` + sessionURL + `"}]`}
	m, out, err := run(t, src)
	if exitCode(err) != 3 || out == "" {
		t.Fatalf("exit %d, out %q", exitCode(err), out)
	}
	want := map[string]bool{"session-link": false, "author-identity": false, "co-authored-by": false, "conventional-commit": false, "pr-blocker-line": false, "pr-author-agent-line": false, "review-line-format": false}
	for _, f := range m["findings"].([]any) {
		fm := f.(map[string]any)
		loc := fm["location"].(map[string]any)
		rule := fm["rule"].(string)
		want[rule] = true
		switch rule {
		case "author-identity", "co-authored-by", "conventional-commit":
			if loc["kind"] != "commit" || loc["id"] != sha("2") {
				t.Errorf("%s: %v", rule, loc)
			}
		case "review-line-format":
			if loc["kind"] != "comment" || loc["id"] != "42" || loc["line"] != 1.0 {
				t.Errorf("%s: %v", rule, loc)
			}
		case "pr-blocker-line":
			if loc["kind"] != "pr-body" || loc["id"] != nil {
				t.Errorf("%s: %v", rule, loc)
			}
		}
	}
	for r, ok := range want {
		if !ok {
			t.Errorf("no %s finding", r)
		}
	}
	kinds := map[string]bool{}
	for _, f := range m["findings"].([]any) {
		fm := f.(map[string]any)
		if fm["rule"] == "session-link" {
			kinds[fm["location"].(map[string]any)["kind"].(string)] = true
		}
	}
	if len(kinds) != 4 {
		t.Errorf("session link locations %v", kinds)
	}
	if m["summary"].(map[string]any)["passed"] != false {
		t.Errorf("passed")
	}
}

func TestWarningOnlyExitsZero(t *testing.T) {
	src := cleanSource()
	var lines []string
	for i := 0; i < 24; i++ {
		lines = append(lines, "x")
	}
	src.detail = detailJSON("feat: y", goodBody+"\n"+strings.Join(lines, "\n"))
	m, _, err := run(t, src)
	if err != nil {
		t.Fatal(err)
	}
	if got := findingRules(m); len(got) != 1 || got[0] != "pr-body-length" {
		t.Fatalf("%v", got)
	}
	s := m["summary"].(map[string]any)
	if s["warnings"] != 1.0 || s["errors"] != 0.0 || s["passed"] != true {
		t.Fatalf("%v", s)
	}
}

func TestNullBodyIsEmpty(t *testing.T) {
	src := cleanSource()
	src.detail = fmt.Sprintf(`{"number":7,"title":"t","body":null,"head":{"sha":%q}}`, sha("a"))
	m, _, err := run(t, src)
	if exitCode(err) != 3 || len(findingRules(m)) == 0 {
		t.Fatalf("%v %v", err, m)
	}
}

func TestPagingCommitsAndComments(t *testing.T) {
	src := cleanSource()
	src.detail = detailWithCommits("feat: y", goodBody, 2)
	src.commits = map[int]string{1: "[" + goodCommitJSON(sha("1")) + "]", 2: "[" + goodCommitJSON(sha("3")) + "]"}
	src.links["commits/1"] = nextLink(2)
	src.comments = map[int]string{1: `[{"id":1,"body":"a"}]`, 2: `[{"id":2,"body":"b"}]`, 3: `[{"id":3,"body":"c"}]`}
	src.links["comments/1"], src.links["comments/2"] = nextLink(2), nextLink(3)
	m, _, err := run(t, src)
	if err != nil {
		t.Fatal(err)
	}
	s := m["summary"].(map[string]any)
	if s["commits"] != 2.0 || s["comments"] != 3.0 || m["truncated"].(map[string]any)["commits"] != false {
		t.Fatalf("%v", m)
	}
	if strings.Join(src.calls, " ") != "detail/o/r#7 commits/1 commits/2 comments/1 comments/2 comments/3" {
		t.Fatalf("calls %v", src.calls)
	}
}

func TestTruncationAtThePageBounds(t *testing.T) {
	src := cleanSource()
	src.commits, src.comments = map[int]string{}, map[int]string{}
	for p := 1; p <= 10; p++ {
		src.comments[p] = fmt.Sprintf(`[{"id":%d,"body":"x"}]`, p)
		src.links[fmt.Sprintf("comments/%d", p)] = nextLink(p + 1)
		if p <= 3 {
			src.commits[p] = "[" + goodCommitJSON(sha(string(rune('0'+p)))) + "]"
			src.links[fmt.Sprintf("commits/%d", p)] = nextLink(p + 1)
		}
	}
	m, _, err := run(t, src)
	if exitCode(err) != 3 || !strings.Contains(err.Error(), "truncated") {
		t.Fatalf("a truncated read must fail closed: %v", err)
	}
	if sm := m["summary"].(map[string]any); sm["passed"] != false || sm["errors"] != 0.0 {
		t.Fatalf("%v", sm)
	}
	tr := m["truncated"].(map[string]any)
	if tr["commits"] != true || tr["comments"] != true || m["summary"].(map[string]any)["comments"] != 10.0 {
		t.Fatalf("%v", m)
	}
	for _, c := range src.calls {
		if c == "commits/4" || c == "comments/11" {
			t.Fatalf("read past the bound: %v", src.calls)
		}
	}
}

func TestEachTruncationAloneFailsClosed(t *testing.T) {
	// Comments only: ten full pages.
	src := cleanSource()
	src.comments = map[int]string{}
	for p := 1; p <= 10; p++ {
		src.comments[p] = fmt.Sprintf(`[{"id":%d,"body":"x"}]`, p)
		src.links[fmt.Sprintf("comments/%d", p)] = nextLink(p + 1)
	}
	m, _, err := run(t, src)
	if exitCode(err) != 3 || m["summary"].(map[string]any)["passed"] != false || m["truncated"].(map[string]any)["commits"] != false {
		t.Errorf("comments: %v %v", err, m)
	}
	// Commits only: GitHub lists at most 250 commits, the pull request says 300.
	src = cleanSource()
	src.detail = detailWithCommits("feat: y", goodBody, 300)
	m, _, err = run(t, src)
	if exitCode(err) != 3 || m["summary"].(map[string]any)["passed"] != false || m["truncated"].(map[string]any)["commits"] != true || m["truncated"].(map[string]any)["comments"] != false {
		t.Errorf("commit count: %v %v", err, m)
	}
	// A pull request without a commits field is not judged by it.
	src = cleanSource()
	src.detail = fmt.Sprintf(`{"number":7,"title":"t","body":%s,"head":{"sha":%q}}`, jsonString(goodBody), sha("a"))
	if _, _, err = run(t, src); err != nil {
		t.Errorf("no commits field: %v", err)
	}
	// Human output says so.
	src = cleanSource()
	src.detail = detailWithCommits("feat: y", goodBody, 300)
	var out bytes.Buffer
	p := params(src, &out)
	p.Human = true
	_ = Execute(context.Background(), p)
	if !strings.Contains(out.String(), "incomplete") {
		t.Errorf("human output: %s", out.String())
	}
}

func TestExactlyOneErrorExitsThree(t *testing.T) {
	src := cleanSource()
	src.detail = detailJSON("feat: y", "Purpose.\nAuthor agent: Claude")
	m, _, err := run(t, src)
	if exitCode(err) != 3 || m["summary"].(map[string]any)["errors"] != 1.0 {
		t.Fatalf("%v %v", err, m["summary"])
	}
}

func TestReadFailuresExitOneWithoutDocument(t *testing.T) {
	for _, at := range []string{"detail", "commits/1", "comments/1"} {
		src := cleanSource()
		src.errAt[at] = &hosting.WatchError{Code: hosting.CodeRateLimited, Message: "rate limited"}
		var out, diag bytes.Buffer
		p := params(src, &out)
		p.Diag = &diag
		err := Execute(context.Background(), p)
		if exitCode(err) != 1 || out.Len() != 0 || !strings.Contains(err.Error(), "rate-limited") {
			t.Errorf("%s: exit %d out %q err %v", at, exitCode(err), out.String(), err)
		}
	}
	for name, detail := range map[string]string{"not json": "nope", "no head": `{"number":7,"title":"t","body":""}`, "bad sha": `{"number":7,"title":"t","body":"","head":{"sha":"abc"}}`} {
		src := cleanSource()
		src.detail = detail
		var out bytes.Buffer
		if err := Execute(context.Background(), params(src, &out)); exitCode(err) != 1 || out.Len() != 0 {
			t.Errorf("%s: %v", name, err)
		}
	}
	src := cleanSource()
	src.commits = map[int]string{1: `[{"sha":"short","commit":{"message":"m"}}]`}
	var out bytes.Buffer
	if err := Execute(context.Background(), params(src, &out)); exitCode(err) != 1 {
		t.Errorf("bad commit sha: %v", err)
	}
}

func TestLinkNextThatIsNotTheFollowingPageIsAReadFailure(t *testing.T) {
	for _, kind := range []string{"commits", "comments"} {
		src := cleanSource()
		src.links[kind+"/1"] = nextLink(5)
		var out bytes.Buffer
		err := Execute(context.Background(), params(src, &out))
		if exitCode(err) != 1 || out.Len() != 0 || !strings.Contains(err.Error(), "Link next") {
			t.Errorf("%s: exit %d out %q err %v", kind, exitCode(err), out.String(), err)
		}
	}
}

func TestOpenFailureExitsOne(t *testing.T) {
	var out bytes.Buffer
	p := params(cleanSource(), &out)
	p.Open = func(context.Context, string, string) (Source, func(), error) {
		return nil, func() {}, errors.New("no gh")
	}
	if err := Execute(context.Background(), p); exitCode(err) != 1 || !strings.Contains(err.Error(), "hosting-api-unavailable") {
		t.Fatal(err)
	}
}

func TestHumanOutputListsEachFinding(t *testing.T) {
	src := cleanSource()
	src.detail = detailJSON("t", "x")
	var out bytes.Buffer
	p := params(src, &out)
	p.Human = true
	err := Execute(context.Background(), p)
	text := out.String()
	if exitCode(err) != 3 || !strings.Contains(text, "pr-blocker-line") || !strings.Contains(text, "error") || !strings.Contains(text, "pr-body") || !strings.Contains(text, "o/r#7") {
		t.Fatalf("%v\n%s", err, text)
	}
}

func TestHumanOutputEscapesControlCharacters(t *testing.T) {
	src := cleanSource()
	src.commits = map[int]string{1: "[" + commitJSON(sha("1"), "feat: y\n\nCo-Authored-By: C <c@d.e>", ownerName, "bad\u001b[31m@x.y", ownerName, ownerEmail) + "]"}
	var out bytes.Buffer
	p := params(src, &out)
	p.Human = true
	_ = Execute(context.Background(), p)
	if strings.ContainsRune(out.String(), 0x1b) {
		t.Fatalf("raw escape in %q", out.String())
	}
}

func flags() Flags { return Flags{Repo: "o/r", PR: 7, Owners: []string{ownerFlag}} }

func prep(t *testing.T, f Flags) (Params, error) {
	t.Helper()
	return prepare(context.Background(), f, &bytes.Buffer{}, Deps{})
}

func TestPrepare(t *testing.T) {
	p, err := prep(t, flags())
	if err != nil || p.Repo != "o/r" || p.PR != 7 || p.Host != "github.com" || p.OwnerSource != OwnerSourceFlag || len(p.Owners) != 1 || p.Owners[0] != (lintpr.Identity{Name: ownerName, Email: ownerEmail}) {
		t.Fatalf("%+v %v", p, err)
	}
	bad := map[string]func(*Flags){
		"pr zero":        func(f *Flags) { f.PR = 0 },
		"pr negative":    func(f *Flags) { f.PR = -3 },
		"repo":           func(f *Flags) { f.Repo = "nope" },
		"host":           func(f *Flags) { f.Host = "bad host" },
		"identity":       func(f *Flags) { f.Owners = []string{"Name only"} },
		"identity email": func(f *Flags) { f.Owners = []string{"N <not-an-email>"} },
		"identity empty": func(f *Flags) { f.Owners = []string{" <a@b.c>"} },
		"both outputs":   func(f *Flags) { f.Human, f.JSON = true, true },
		"junk before":    func(f *Flags) { f.Owners = []string{"x <y Name <a@b.c>"} },
		"junk after":     func(f *Flags) { f.Owners = []string{"Name <a@b.c> tail"} },
		"path and repo":  func(f *Flags) { f.WorkspacePath = "/w" },
	}
	for name, mod := range bad {
		f := flags()
		mod(&f)
		if _, err := prep(t, f); !errors.Is(err, watchcmd.ErrUsage) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestPrepareDefaultIdentityFromGitConfig(t *testing.T) {
	f := flags()
	f.Owners = nil
	var asked string
	d := Deps{GitIdentity: func(_ context.Context, dir string) (lintpr.Identity, bool) {
		asked = dir
		return lintpr.Identity{Name: ownerName, Email: ownerEmail}, true
	}}
	p, err := prepare(context.Background(), f, &bytes.Buffer{}, d)
	if err != nil || p.OwnerSource != OwnerSourceGitConfig || len(p.Owners) != 1 || asked == "" {
		t.Fatalf("%+v %v %q", p, err, asked)
	}
	d.GitIdentity = func(context.Context, string) (lintpr.Identity, bool) { return lintpr.Identity{}, false }
	if _, err := prepare(context.Background(), f, &bytes.Buffer{}, d); !errors.Is(err, watchcmd.ErrUsage) || !strings.Contains(err.Error(), "--owner-identity") {
		t.Fatalf("%v", err)
	}
	// An explicit flag wins and the config is not read.
	f.Owners = []string{ownerFlag}
	asked = ""
	if _, err := prepare(context.Background(), f, &bytes.Buffer{}, d); err != nil || asked != "" {
		t.Fatalf("%v %q", err, asked)
	}
}

func TestPrepareRepoFromWorkspace(t *testing.T) {
	f := flags()
	f.Repo = ""
	one := func(context.Context, string) (stwsync.Workspace, error) {
		return stwsync.Workspace{Root: "/w", Members: []stwsync.Member{{Name: ".", Initialized: true, Remote: "https://github.com/o/r.git"}}}, nil
	}
	p, err := prepare(context.Background(), f, &bytes.Buffer{}, Deps{Discover: one})
	if err != nil || p.Repo != "o/r" || p.WorkspaceRoot != "/w" {
		t.Fatalf("%+v %v", p, err)
	}
	two := func(context.Context, string) (stwsync.Workspace, error) {
		return stwsync.Workspace{Root: "/w", Members: []stwsync.Member{
			{Name: "a", Initialized: true, Remote: "https://github.com/o/a.git"}, {Name: "b", Initialized: true, Remote: "https://github.com/o/b.git"}}}, nil
	}
	if _, err := prepare(context.Background(), f, &bytes.Buffer{}, Deps{Discover: two}); !errors.Is(err, watchcmd.ErrUsage) || !strings.Contains(err.Error(), "--repo") {
		t.Fatalf("%v", err)
	}
}
