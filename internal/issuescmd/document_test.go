// SPDX-License-Identifier: EUPL-1.2
package issuescmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/wstein/sourcesteward/internal/issues"
	"github.com/wstein/sourcesteward/internal/snapcmd"
)

var t0 = time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)

func issuesSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "schemas", "v1", "issues.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	id := "https://wstein.github.io/sourcesteward/schemas/v1/issues.schema.json"
	if err := c.AddResource(id, doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile(id)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// decode validates one JSON line against the schema and returns it.
func decode(t *testing.T, out string) map[string]any {
	t.Helper()
	if strings.Count(strings.TrimSuffix(out, "\n"), "\n") != 0 || !strings.HasSuffix(out, "\n") {
		t.Fatalf("not exactly one line: %q", out)
	}
	v, err := jsonschema.UnmarshalJSON(strings.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if err := issuesSchema(t).Validate(v); err != nil {
		t.Fatalf("schema: %v\n%s", err, out)
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(out), &m)
	return m
}

func ptr[T any](v T) *T { return &v }

func row(repo string, n int, priority, reason, author string, mods ...func(*issues.Row)) issues.Row {
	r := issues.Row{Repo: repo, Number: n, Title: "title", URL: "https://github.com/" + repo + "/issues/" + string(rune('0'+n%10)), Author: author,
		CreatedAt: t0.Add(-48 * time.Hour), AgeDays: ptr(2), Labels: []string{}, Assignees: []string{}, Comments: ptr(0), Priority: priority, Reason: reason}
	if priority == issues.Red || priority == issues.Yellow {
		r.Command, r.Effect = "gh issue view 1 -R "+repo, "read"
	}
	for _, m := range mods {
		m(&r)
	}
	return r
}

func sampleResult() *issues.Result {
	return &issues.Result{
		Time: t0, Limit: 200,
		Viewer: issues.Viewer{State: issues.ViewerOK, Login: "wstein"},
		Rows: []issues.Row{
			row("o/a", 1, issues.Red, "security-label", "alice", func(r *issues.Row) { r.Labels, r.LabelsTotal = []string{"security", "bug"}, 2 }),
			row("o/a", 2, issues.Yellow, "assigned-to-you", "bob", func(r *issues.Row) { r.Assignees = []string{"wstein"} }),
			row("o/b", 3, issues.Blue, "none", "carol"),
			row("o/b", 4, issues.Violet, "bot", "dependabot[bot]", func(r *issues.Row) { r.Bot = true }),
			row("o/b", 5, issues.Violet, "bot", "renovate", func(r *issues.Row) { r.Bot = true }),
		},
		Repos: []issues.RepoStatus{
			{Repo: "o/a", State: issues.StateOK, Open: ptr(2), PullsDropped: ptr(1)},
			{Repo: "o/b", State: issues.StateOK, Open: ptr(3), PullsDropped: ptr(0)},
		},
		Summary: issues.Summary{Total: 5, Red: 1, Yellow: 1, Blue: 1, Violet: 2, Counts: issues.Counts{Red: 1, Yellow: 1, Blue: 1, Violet: 2}, Bots: 2, ReposRead: 2},
	}
}

func sampleParams() Params {
	return Params{Host: "github.com", Scope: ScopeRepos, Options: issues.Options{Limit: 200}}
}

func TestDocumentIsOneSchemaValidLineInThePriorityOrder(t *testing.T) {
	p := sampleParams()
	d := buildDocument(p, sampleResult(), map[string]string{"o/a": SourceRepo}, nil)
	var b bytes.Buffer
	if err := writeJSON(&b, d); err != nil {
		t.Fatal(err)
	}
	m := decode(t, b.String())
	g := m["issues"].(map[string]any)
	if len(g["red"].([]any)) != 1 || len(g["yellow"].([]any)) != 1 || len(g["blue"].([]any)) != 1 || len(g["violet"].([]any)) != 2 {
		t.Fatalf("%v", g)
	}
	repos := m["repos"].([]any)
	if repos[0].(map[string]any)["source"] != SourceRepo || repos[1].(map[string]any)["source"] != SourceRepo {
		t.Fatalf("a repository without a recorded source is a --repo one: %v", repos)
	}
	if !strings.HasPrefix(b.String(), `{"schema_version":1,"command":"issues","time":"2026-10-22T09:00:00Z","host":"github.com","viewer":{"state":"ok","login":"wstein","reason":null,"error":null},"options":`) {
		t.Fatalf("key order: %s", b.String())
	}
}

func TestDocumentEmitsNullAndEmptyArraysForWhatIsAbsent(t *testing.T) {
	res := sampleResult()
	res.Rows = []issues.Row{row("o/a", 1, issues.Blue, "none", "ghost", func(r *issues.Row) {
		r.URL, r.CreatedAt, r.AgeDays, r.Comments, r.Labels, r.Assignees = "", time.Time{}, nil, nil, nil, nil
	})}
	res.Viewer = issues.Viewer{State: issues.ViewerSkipped, Reason: issues.ReasonNoIdentity}
	d := buildDocument(sampleParams(), res, nil, nil)
	var b bytes.Buffer
	_ = writeJSON(&b, d)
	m := decode(t, b.String())
	r := m["issues"].(map[string]any)["blue"].([]any)[0].(map[string]any)
	for _, k := range []string{"url", "created_at", "age_days", "comments", "command", "effect"} {
		if v, ok := r[k]; !ok || v != nil {
			t.Errorf("%s = %v", k, v)
		}
	}
	if l, ok := r["labels"].([]any); !ok || len(l) != 0 {
		t.Errorf("labels %v", r["labels"])
	}
	v := m["viewer"].(map[string]any)
	if v["state"] != "skipped" || v["reason"] != "no-identity" || v["login"] != nil || v["error"] != nil {
		t.Errorf("%v", v)
	}
}

func TestDocumentRepositoryStatesAndErrors(t *testing.T) {
	res := sampleResult()
	res.Rows = nil
	res.Viewer = issues.Viewer{State: issues.ViewerError, Error: "network"}
	res.Repos = []issues.RepoStatus{
		{Repo: "o/a", State: issues.StateSkipped, Reason: "issues-disabled", Open: ptr(0), PullsDropped: ptr(0)},
		{Repo: "o/b", State: issues.StateSkipped, Reason: issues.CodeViewerUnknown},
		{Repo: "o/c", State: issues.StatePartial, Open: ptr(1000), PullsDropped: ptr(3), Error: issues.CodeTooManyOpenIssues},
		{Repo: "o/d", State: issues.StateError, Error: "network"},
	}
	res.Errors = []issues.Error{{Code: "network", Message: "read failed"}, {Repo: "o/c", Code: issues.CodeTooManyOpenIssues, Message: "m"}, {Repo: "o/d", Code: "network", Message: "m"}}
	res.Summary = issues.Summary{ReposRead: 1, ReposFailed: 2}
	owner := []snapcmd.DocError{{Owner: ptr("wstein"), Code: "search-incomplete", Message: "m"}}
	d := buildDocument(sampleParams(), res, map[string]string{"o/a": SourceOwner}, owner)
	var b bytes.Buffer
	_ = writeJSON(&b, d)
	m := decode(t, b.String())
	errs := m["errors"].([]any)
	if len(errs) != 4 {
		t.Fatalf("%v", errs)
	}
	run, owned := errs[0].(map[string]any), errs[3].(map[string]any)
	if run["repo"] != nil || run["owner"] != nil || run["code"] != "network" || owned["owner"] != "wstein" || owned["repo"] != nil {
		t.Fatalf("%v %v", run, owned)
	}
	if m["repos"].([]any)[0].(map[string]any)["source"] != SourceOwner {
		t.Fatalf("%v", m["repos"])
	}
}

func TestDocumentCutsMessagesAndHostOnAUTF8Boundary(t *testing.T) {
	long := strings.Repeat("é", 400) // 800 bytes
	res := sampleResult()
	res.Errors = []issues.Error{{Repo: "o/a", Code: "network", Message: long}}
	d := buildDocument(Params{Host: strings.Repeat("a", 2047) + "é", Scope: ScopeRepos}, res, nil, []snapcmd.DocError{{Owner: ptr("w"), Code: "search-incomplete", Message: long}})
	for what, s := range map[string]string{"message": d.Errors[0].Message, "owner message": d.Errors[1].Message} {
		if len(s) > 512 || len(s) < 511 || !utf8.ValidString(s) {
			t.Errorf("%s: len %d", what, len(s))
		}
	}
	if len(d.Host) != 2047 || !utf8.ValidString(d.Host) {
		t.Errorf("host len %d", len(d.Host))
	}
}

func human(t *testing.T, res *issues.Result, p Params) string {
	t.Helper()
	var b bytes.Buffer
	if err := writeHuman(&b, buildDocument(p, res, nil, nil)); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestHumanOutputGroupsByPriorityWithTheRunHintUnderActions(t *testing.T) {
	want := "RED (1)\n" +
		"o/a#1  2d  alice  [labels: security, bug]  title  (security-label)\n" +
		"  run: gh issue view 1 -R o/a\n\n" +
		"YELLOW (1)\n" +
		"o/a#2  2d  bob  [assigned: wstein]  title  (assigned-to-you)\n" +
		"  run: gh issue view 1 -R o/a\n\n" +
		"BLUE (1)\n" +
		"o/b#3  2d  carol  title  (none)\n\n" +
		"VIOLET (2)\n" +
		"o/b#4  2d  dependabot[bot]  title  (bot)\n" +
		"o/b#5  2d  renovate[bot]  title  (bot)\n\n" +
		"5 open issues in 2 repositories, 1 red, 1 yellow\n"
	if got := human(t, sampleResult(), sampleParams()); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestHumanOutputReportsOmittedFailuresAndAnEmptyList(t *testing.T) {
	res := sampleResult()
	res.Rows, res.Summary = nil, issues.Summary{Omitted: 7, ReposRead: 1, ReposFailed: 2}
	out := human(t, res, sampleParams())
	for _, w := range []string{"0 open issues in 1 repository, 0 red, 0 yellow\n", "7 omitted (past --limit 200)\n", "2 repositories could not be read\n"} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in %q", w, out)
		}
	}
	d := buildDocument(sampleParams(), res, nil, []snapcmd.DocError{{Owner: ptr("w"), Code: "search-incomplete", Message: "m"}})
	var b bytes.Buffer
	_ = writeHuman(&b, d)
	if !strings.Contains(b.String(), "1 problem with owner discovery\n") {
		t.Errorf("%q", b.String())
	}
}

func TestHumanOutputEscapesEveryUntrustedColumn(t *testing.T) {
	res := sampleResult()
	res.Rows = []issues.Row{row("o/a", 1, issues.Red, "security-label", "ev\x1b[31mil", func(r *issues.Row) {
		r.Title = "t\r\nitle‮" + strings.Repeat("x", 10000)
		r.Labels, r.LabelsTotal, r.Assignees = []string{"la\x00bel"}, 1, []string{"as\x1bg"}
	})}
	out := human(t, res, sampleParams())
	for _, bad := range []rune{0x1b, 0x202e, 0, '\r'} {
		if strings.ContainsRune(out, bad) {
			t.Fatalf("raw %q in %q", bad, out)
		}
	}
	for _, want := range []string{`ev\x1b[31mil`, `la\x00bel`, `as\x1bg`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
	if len(strings.Split(out, "\n")[0]) > 400 {
		t.Errorf("title not bounded: %d", len(out))
	}
}

func TestHumanOutputShowsACutLabelList(t *testing.T) {
	res := sampleResult()
	res.Rows = []issues.Row{row("o/a", 1, issues.Blue, "none", "alice", func(r *issues.Row) { r.Labels, r.LabelsTotal = []string{"a", "b"}, 30 })}
	if out := human(t, res, sampleParams()); !strings.Contains(out, "[labels: a, b, +28 more]") {
		t.Fatalf("%q", out)
	}
}
