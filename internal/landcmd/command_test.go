// SPDX-License-Identifier: EUPL-1.2

package landcmd

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
	"github.com/wstein/sourcesteward/internal/land"
	"github.com/wstein/sourcesteward/internal/watchcmd"
)

const (
	ownerName = "Werner Stein"
	ownerMail = "werner.stein@gmail.com"
)

func sha(c byte) string { return strings.Repeat(string(c), 40) }

// chainReader serves a complete GitHub world for the chain 11, 12, 13 on main.
func chainReader() *fakeReader {
	b := map[string]string{
		"repos/o/r": `{"default_branch":"main","delete_branch_on_merge":true}`,
		"user":      `{"login":"wstein"}`,
		"repos/o/r/pulls?state=open&per_page=100&page=1":    `[]`,
		"repos/o/r/rules/branches/main?per_page=100&page=1": `[{"type":"required_status_checks","ruleset_id":7,"parameters":{"required_status_checks":[{"context":"ci/all"}]}},{"type":"pull_request","ruleset_id":7}]`,
		"repos/o/r/rulesets/7":                              `{"bypass_actors":[{"bypass_mode":"always"}]}`,
	}
	for i, n := range []int{11, 12, 13} {
		head := sha(byte('1' + i))
		b[fmt.Sprintf("repos/o/r/pulls/%d", n)] = fmt.Sprintf(`{"number":%d,"state":"open","head":{"sha":%q,"ref":"feat-%d","repo":{"full_name":"o/r"}},"base":{"ref":"main","repo":{"full_name":"o/r"}},"mergeable":true,"mergeable_state":"blocked","rebaseable":true}`, n, head, n)
		b[fmt.Sprintf("repos/o/r/commits/%s/check-runs?per_page=100&page=1", head)] = `{"check_runs":[{"name":"ci/all","status":"completed","conclusion":"success"}]}`
		b[fmt.Sprintf("repos/o/r/commits/%s/statuses?per_page=100&page=1", head)] = `[]`
		b[fmt.Sprintf("repos/o/r/pulls/%d/reviews?per_page=100&page=1", n)] = fmt.Sprintf(`[{"user":{"login":"rev"},"state":"APPROVED","commit_id":%q}]`, head)
		b[fmt.Sprintf("repos/o/r/issues/%d/events?per_page=100&page=1", n)] = `[]`
	}
	return &fakeReader{bodies: b, status: map[string]int{"repos/o/r/branches/main/protection": 404}}
}

// stubGit is a land.Git over a linear history M, h1, h2, h3.
type stubGit struct{ calls []string }

func (g *stubGit) RemoteRepo(context.Context, string) (string, error) { return "o/r", nil }
func (g *stubGit) DefaultTip(context.Context, string, string) (string, error) {
	g.calls = append(g.calls, "ls-remote")
	return sha('0'), nil
}
func (g *stubGit) Fetch(context.Context, string, string, []int) error {
	g.calls = append(g.calls, "fetch")
	return nil
}
func (g *stubGit) Resolve(_ context.Context, n int) (string, error) {
	return sha(byte('1' + n - 11)), nil
}
func (g *stubGit) IsAncestor(_ context.Context, a, b string) (land.Ancestry, error) {
	if a <= b {
		return land.AncestorYes, nil
	}
	return land.AncestorNo, nil
}
func (g *stubGit) Range(_ context.Context, from, to string, _ int) ([]land.Commit, error) {
	var out []land.Commit
	for c := from[0] + 1; c <= to[0]; c++ {
		out = append(out, land.Commit{SHA: sha(c), AuthorName: ownerName, AuthorEmail: ownerMail, CommitterName: ownerName, CommitterEmail: ownerMail})
	}
	return out, nil
}

type opened struct{ n int }

func baseFlags() Flags {
	return Flags{Stack: "11,12,13", Reviewed: []string{"11:" + sha('1'), "12:" + sha('2'), "13:" + sha('3')}, Plan: true, Repo: "o/r", Remote: "origin", JSON: true}
}

// run prepares and executes with fakes; env is the environment of the process.
func run(t *testing.T, f Flags, env []string, tty bool, mutate func(*Params)) (stdout string, err error, opens *opened, git *stubGit) {
	t.Helper()
	var out bytes.Buffer
	p, err := Prepare(context.Background(), f, &out)
	if err != nil {
		return "", err, nil, nil
	}
	opens, git = &opened{}, &stubGit{}
	reader := chainReader()
	p.Open = func(context.Context, string, string) (Reader, func(), error) {
		opens.n++
		return reader, func() {}, nil
	}
	p.Git = git
	p.Environ = append([]string{}, env...) // never the real environment: this test run is itself an agent session
	p.TTY = func() (bool, bool) { return tty, tty }
	p.Policy = func(context.Context, string) land.Policy {
		return land.Policy{Loaded: true, OwnerName: ownerName, OwnerEmail: ownerMail}
	}
	p.Shapes = nil
	if mutate != nil {
		mutate(&p)
	}
	err = Execute(context.Background(), p)
	return out.String(), err, opens, git
}

// measuredShapes records the shape of chainReader so that a plan is landable.
func measuredShapes(t *testing.T) *land.ShapeSet {
	t.Helper()
	g := NewGitHubForge(chainReader(), "o/r")
	repo, _ := g.Repository(context.Background())
	rs, _ := g.Rulesets(context.Background(), "main")
	return land.NewShapeSet(land.ShapeOf(repo, rs, true).Key())
}

func TestPrepareUsageErrors(t *testing.T) {
	cases := map[string]func(*Flags){
		"yes":                func(f *Flags) { f.Yes = true },
		"no stack":           func(f *Flags) { f.Stack = "" },
		"single number":      func(f *Flags) { f.Stack = "11"; f.Reviewed = f.Reviewed[:1] },
		"pr with stack":      func(f *Flags) { f.Pr = 11 },
		"head with stack":    func(f *Flags) { f.Head = sha('1') },
		"pr alone":           func(f *Flags) { f.Stack = ""; f.Pr = 11; f.Head = sha('1') },
		"json with human":    func(f *Flags) { f.Human = true },
		"bad wait":           func(f *Flags) { f.Wait = -1; f.WaitSet = true },
		"wait too long":      func(f *Flags) { f.Wait = 3 * 60 * 1e9; f.WaitSet = true },
		"bad repo":           func(f *Flags) { f.Repo = "../x" },
		"bad remote":         func(f *Flags) { f.Remote = "-x" },
		"reviewed mismatch":  func(f *Flags) { f.Reviewed = f.Reviewed[:2] },
		"reviewed short sha": func(f *Flags) { f.Reviewed[0] = "11:abc" },
		"bad host":           func(f *Flags) { f.Host = "bad host" },
		"nine in the chain":  func(f *Flags) { f.Stack = "1,2,3,4,5,6,7,8,9"; f.Reviewed = nil },
		"duplicate in stack": func(f *Flags) { f.Stack = "11,11" },
		"not a number":       func(f *Flags) { f.Stack = "11,x" },
	}
	for name, edit := range cases {
		f := baseFlags()
		edit(&f)
		_, err := Prepare(context.Background(), f, &bytes.Buffer{})
		if !errors.Is(err, watchcmd.ErrUsage) {
			t.Errorf("%s: %v is not a usage error", name, err)
		}
	}
	if _, err := Prepare(context.Background(), baseFlags(), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
}

func TestPlanJSONValidatesAgainstTheSchemaAndAgentModeBlocksApply(t *testing.T) {
	out, err, opens, git := run(t, baseFlags(), []string{"STW_AGENT=1"}, true, func(p *Params) { p.Shapes = measuredShapes(t) })
	if err != nil {
		t.Fatalf("a landable plan exits 0: %v", err)
	}
	doc := validate(t, out)
	if doc["landable"] != true || doc["apply_allowed"] != false || doc["mode"].(map[string]any)["mode"] != "agent" {
		t.Fatalf("%s", out)
	}
	if opens.n != 1 || len(git.calls) == 0 {
		t.Fatalf("the plan reads: opens=%d git=%v", opens.n, git.calls)
	}
}

func TestPlanInHumanModeAllowsApply(t *testing.T) {
	out, err, _, _ := run(t, baseFlags(), nil, true, func(p *Params) { p.Shapes = measuredShapes(t) })
	if err != nil {
		t.Fatal(err)
	}
	doc := validate(t, out)
	if doc["apply_allowed"] != true || doc["mode"].(map[string]any)["mode"] != "human" {
		t.Fatalf("%s", out)
	}
}

func TestRefusedPlanPrintsTheDocumentAndExitsOne(t *testing.T) {
	out, err, _, _ := run(t, baseFlags(), nil, true, nil) // no recorded shape: land-unmeasured
	var fail *watchcmd.Failure
	if !errors.As(err, &fail) || fail.Code != 1 {
		t.Fatalf("exit: %v", err)
	}
	doc := validate(t, out)
	if doc["landable"] != false || doc["result"] != "refused" {
		t.Fatalf("%s", out)
	}
	if !strings.Contains(out, `"land-unmeasured"`) {
		t.Fatalf("land-unmeasured missing: %s", out)
	}
}

// The owner-only rule: without --plan the command applies, and an agent never reaches a read.
func TestApplyInAgentModeIsRefusedBeforeAnyRead(t *testing.T) {
	for _, env := range [][]string{{"STW_AGENT=1"}, {"CLAUDECODE=1"}, {"CLAUDE_CODE_ENTRYPOINT=cli"}, {"CODEX_HOME=x"}, {"AGY_X=1"}} {
		f := baseFlags()
		f.Plan = false
		out, err, opens, git := run(t, f, env, true, func(p *Params) { p.Shapes = measuredShapes(t) })
		var fail *watchcmd.Failure
		if !errors.As(err, &fail) || fail.Code != 1 {
			t.Fatalf("%v: %v", env, err)
		}
		doc := validate(t, out)
		if doc["operation"] != "apply" || doc["result"] != "refused" || !strings.Contains(out, `"agent-mode"`) {
			t.Fatalf("%v: %s", env, out)
		}
		if opens.n != 0 || len(git.calls) != 0 {
			t.Fatalf("%v: an agent apply reached a read: opens=%d git=%v", env, opens.n, git.calls)
		}
	}
}

func TestApplyWithoutTerminalIsAgentMode(t *testing.T) {
	f := baseFlags()
	f.Plan = false
	out, err, opens, _ := run(t, f, nil, false, nil)
	if err == nil || !strings.Contains(out, `"agent-mode"`) || opens.n != 0 {
		t.Fatalf("%v %s %d", err, out, opens.n)
	}
}

func TestApplyIsUnavailableInThisBuild(t *testing.T) {
	f := baseFlags()
	f.Plan = false
	out, err, opens, _ := run(t, f, nil, true, nil)
	if err == nil || !strings.Contains(out, `"apply-unavailable"`) || opens.n != 0 {
		t.Fatalf("%v %s %d", err, out, opens.n)
	}
}

func TestHumanOutputEscapesUntrustedText(t *testing.T) {
	f := baseFlags()
	f.JSON, f.Human = false, true
	out, _, _, _ := run(t, f, nil, true, func(p *Params) {
		r := chainReader()
		r.bodies["repos/o/r/pulls/12"] = strings.Replace(r.bodies["repos/o/r/pulls/12"], `"ref":"feat-12"`, `"ref":"evil\u001b[31mred"`, 1)
		r.bodies["repos/o/r/pulls/12"] = strings.Replace(r.bodies["repos/o/r/pulls/12"], `"ref":"main"`, `"ref":"ma\u001b]0;x\u0007in"`, 1)
		p.Open = func(context.Context, string, string) (Reader, func(), error) { return r, func() {}, nil }
	})
	if strings.ContainsAny(out, "\x1b\x07") {
		t.Fatalf("control characters in the output: %q", out)
	}
	for _, want := range []string{"land --stack", "#11", "#13", "land-unmeasured", "o/r"} {
		if !strings.Contains(out, want) {
			t.Errorf("human output lacks %q:\n%s", want, out)
		}
	}
}

func TestInvalidDocumentsFailTheSchema(t *testing.T) {
	out, _, _, _ := run(t, baseFlags(), nil, true, func(p *Params) { p.Shapes = measuredShapes(t) })
	var base map[string]any
	if err := json.Unmarshal([]byte(out), &base); err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(map[string]any){
		"missing landable": func(m map[string]any) { delete(m, "landable") },
		"extra key":        func(m map[string]any) { m["extra"] = 1 },
		"bad operation":    func(m map[string]any) { m["operation"] = "land" },
		"bad mode":         func(m map[string]any) { m["mode"].(map[string]any)["mode"] = "robot" },
		"one pull request": func(m map[string]any) { m["inputs"].(map[string]any)["stack"] = []any{11} },
		"bad digest":       func(m map[string]any) { m["plan_digest"] = "xyz" },
		"bad effect kind":  func(m map[string]any) { m["effects"].([]any)[0].(map[string]any)["kind"] = "force-push" },
		"failing without code": func(m map[string]any) {
			m["preconditions"] = []any{map[string]any{"id": "x", "ok": false, "value": ""}}
		},
		"bad review evidence": func(m map[string]any) { m["chain"].([]any)[0].(map[string]any)["review_evidence"] = "clear" },
		"bad result":          func(m map[string]any) { m["result"] = "merged" },
		"uppercase code":      func(m map[string]any) { m["reasons"] = []any{map[string]any{"code": "Head-Changed"}} },
	}
	for name, mut := range mutations {
		clone := map[string]any{}
		raw, _ := json.Marshal(base)
		_ = json.Unmarshal(raw, &clone)
		mut(clone)
		bad, _ := json.Marshal(clone)
		if err := landSchema(t).Validate(mustParse(t, string(bad))); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func landSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "schemas", "v1", "land.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	id := "https://wstein.github.io/sourcesteward/schemas/v1/land.schema.json"
	if err := c.AddResource(id, doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile(id)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func mustParse(t *testing.T, s string) any {
	t.Helper()
	v, err := jsonschema.UnmarshalJSON(strings.NewReader(s))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func validate(t *testing.T, out string) map[string]any {
	t.Helper()
	if strings.Count(strings.TrimSuffix(out, "\n"), "\n") != 0 || !strings.HasSuffix(out, "\n") {
		t.Fatalf("not exactly one line: %q", out)
	}
	if err := landSchema(t).Validate(mustParse(t, out)); err != nil {
		t.Fatalf("schema: %v\n%s", err, out)
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(out), &m)
	return m
}

func TestOpenFailureIsExitOne(t *testing.T) {
	_, err, _, _ := run(t, baseFlags(), nil, true, func(p *Params) {
		p.Open = func(context.Context, string, string) (Reader, func(), error) {
			return nil, func() {}, errors.New("no gh")
		}
	})
	var fail *watchcmd.Failure
	if !errors.As(err, &fail) || fail.Code != 1 {
		t.Fatalf("%v", err)
	}
}

var _ = hosting.CodeForbidden

// cubistryReader is the world of the observed failure: a native stack 11 (main), 12 (head branch of 11),
// no pull_request rule, the classic protection answers 403 and delete_branch_on_merge is not visible.
func cubistryReader() *fakeReader {
	r := chainReader()
	r.bodies["repos/o/r"] = `{"default_branch":"main","delete_branch_on_merge":null}`
	r.bodies["repos/o/r/rules/branches/main?per_page=100&page=1"] = `[{"type":"required_linear_history","ruleset_id":7}]`
	r.bodies["repos/o/r/rulesets/7"] = `{"bypass_actors":[]}`
	r.bodies["repos/o/r/branches/main"] = `{"protected":true,"protection":{"enabled":false,"required_status_checks":{"contexts":[],"checks":[],"enforcement_level":"off"}}}`
	r.status["repos/o/r/branches/main/protection"] = 403
	r.bodies["repos/o/r/pulls/12"] = strings.Replace(r.bodies["repos/o/r/pulls/12"], `"base":{"ref":"main"`, `"base":{"ref":"feat-11"`, 1)
	r.bodies["repos/o/r/pulls/11"] = strings.Replace(r.bodies["repos/o/r/pulls/11"], `"ref":"feat-11"`, `"ref":"feat-11"`, 1)
	return r
}

func TestNativeStackPlanOnATokenWithoutAdministrationRead(t *testing.T) {
	f := baseFlags()
	f.Stack, f.Reviewed = "11,12", []string{"11:" + sha('1'), "12:" + sha('2')}
	open := func(p *Params) {
		p.Open = func(context.Context, string, string) (Reader, func(), error) { return cubistryReader(), func() {}, nil }
	}
	// without evidence: refused as unmeasured, but the shape and the checks are readable
	out, err, _, _ := run(t, f, nil, true, open)
	if err == nil {
		t.Fatal("an unmeasured native shape must not be landable")
	}
	doc := validate(t, out)
	if strings.Contains(out, `"checks-unknown"`) || strings.Contains(out, `unreadable`) {
		t.Fatalf("the reads must work without administration rights: %s", out)
	}
	if !strings.Contains(out, "native-stack-shape") || !strings.Contains(out, "scripts/measure-native-stack.sh") || doc["landable"] != false {
		t.Fatalf("%s", out)
	}
	// with the recorded native entry: landable, with the retarget before the push
	out, err, _, _ = run(t, f, nil, true, func(p *Params) {
		open(p)
		g := NewGitHubForge(cubistryReader(), "o/r")
		repo, _ := g.Repository(context.Background())
		rs, _ := g.Rulesets(context.Background(), "main")
		p.Shapes = land.NewNativeShapeSet(land.ShapeOf(repo, rs, true).Key())
	})
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	doc = validate(t, out)
	if doc["landable"] != true || !strings.Contains(out, `"kind":"retarget"`) || !strings.Contains(out, `"gh","pr","edit","12","--base","main"`) {
		t.Fatalf("%s", out)
	}
}

func TestRemoteMismatchSaysHowToProceed(t *testing.T) {
	f := baseFlags()
	f.JSON, f.Human = false, true
	out, err, _, _ := run(t, f, nil, true, func(p *Params) { p.Git = &mismatchGit{} })
	if err == nil {
		t.Fatal("a wrong checkout must be refused")
	}
	for _, want := range []string{"remote-repo-mismatch", "x/other", "o/r", "positional PATH", "--remote"} {
		if !strings.Contains(out, want) {
			t.Errorf("message lacks %q:\n%s", want, out)
		}
	}
}

type mismatchGit struct{ stubGit }

func (g *mismatchGit) RemoteRepo(context.Context, string) (string, error) { return "x/other", nil }
