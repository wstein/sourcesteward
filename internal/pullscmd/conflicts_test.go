// SPDX-License-Identifier: EUPL-1.2
package pullscmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	osexec "os/exec"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/conflicts"
	"github.com/wstein/sourcesteward/internal/pulls"
	"github.com/wstein/sourcesteward/internal/watchcmd"
)

// stubProber answers by number and counts its calls.
type stubProber struct {
	findings map[int]conflicts.Finding
	calls    int
	req      ProberRequest // what the command asked the prober factory for
}

func (s *stubProber) Probe(_ context.Context, _ string, prs []conflicts.PR) []conflicts.Finding {
	s.calls++
	var out []conflicts.Finding
	for _, p := range prs {
		if f, ok := s.findings[p.Number]; ok {
			f.Number = p.Number
			out = append(out, f)
		}
	}
	return out
}

// conflictSource: o/a#1 CLEAR and conflicting with main, #2 above it (base "one"), #3 CLEAR and clean.
func conflictSource() reviewForge {
	s := newSource()
	rl := func(head string, n int) string {
		return "Review: CLEAR head=" + head + " patch-id=" + strings.Repeat("1", 40) + " pr=" + itoa(n) + "\nrest"
	}
	for n, bh := range map[int][2]string{1: {"main", "one"}, 2: {"one", "two"}, 3: {"main", "three"}} {
		p := okPR("o/a", n)
		p.age = time.Duration(n) * 24 * time.Hour
		p.base, p.headRef = bh[0], bh[1]
		s.add(p)
		s.commits[p.head] = t0.Add(-100 * time.Hour)
		s.say("o/a", n, 1, 5, rl(p.head, n))
	}
	return reviewForge{s}
}

func itoa(n int) string { return string(rune('0' + n)) }

func conflictFindings() map[int]conflicts.Finding {
	return map[int]conflicts.Finding{
		1: {State: conflicts.StateConflicts, Base: "main", BaseSHA: strings.Repeat("f", 40), MergeBase: strings.Repeat("e", 40),
			Files: []conflicts.FileConflict{{Path: "rules.md", BaseCommit: strings.Repeat("b", 40)}}},
		2: {State: conflicts.StateClean, Base: "one"},
		3: {State: conflicts.StateClean, Base: "main", Overlap: []string{"notes.md"}},
	}
}

func execConflicts(t *testing.T, src Source, f Flags, prober *stubProber) (string, string, error) {
	t.Helper()
	var out, diag bytes.Buffer
	p, err := prepare(context.Background(), f, &out, nil)
	if err != nil {
		t.Fatal(err)
	}
	p.Diag, p.Now = &diag, func() time.Time { return t0 }
	p.Open = func(context.Context, string, string) (Source, func(), error) { return src, func() {}, nil }
	p.NewProber = func(_ context.Context, req ProberRequest) (pulls.ConflictProber, func(), error) {
		prober.req = req
		return prober, func() {}, nil
	}
	err = Execute(context.Background(), p)
	return out.String(), diag.String(), err
}

func conflictFlags() Flags {
	f := baseFlags()
	f.Repos, f.JSON, f.Conflicts = []string{"o/a"}, true, true
	return f
}

func TestPrepareConflictFlags(t *testing.T) {
	p, err := prepare(context.Background(), conflictFlags(), &bytes.Buffer{}, nil)
	if err != nil || !p.Conflicts || p.RestackPlan || p.Remote != "origin" || p.Checkout != "" {
		t.Fatalf("%+v %v", p, err)
	}
	f := conflictFlags()
	f.RestackPlan, f.Remote, f.Checkout = true, "up", "/tmp/x"
	if p, err = prepare(context.Background(), f, &bytes.Buffer{}, nil); err != nil || !p.Conflicts || !p.RestackPlan || p.Remote != "up" || p.Checkout != "/tmp/x" {
		t.Fatalf("%+v %v", p, err)
	}
	f = baseFlags()
	f.Repos, f.RestackPlan = []string{"o/a"}, true
	if p, err = prepare(context.Background(), f, &bytes.Buffer{}, nil); err != nil || !p.Conflicts {
		t.Fatalf("--restack-plan implies --conflicts: %+v %v", p, err)
	}
	for name, mutate := range map[string]func(f *Flags){
		"checkout without conflicts": func(f *Flags) { f.Conflicts, f.Checkout = false, "/tmp/x" },
		"remote without conflicts":   func(f *Flags) { f.Conflicts, f.Remote = false, "up" },
		"bad remote":                 func(f *Flags) { f.Remote = "-u" },
		"two repositories":           func(f *Flags) { f.Repos = []string{"o/a", "o/b"} },
		"owner discovery":            func(f *Flags) { f.Owners = []string{"o"} },
		"json and human":             func(f *Flags) { f.Human = true },
	} {
		f := conflictFlags()
		mutate(&f)
		if _, err := prepare(context.Background(), f, &bytes.Buffer{}, nil); !errors.Is(err, watchcmd.ErrUsage) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestExecuteJSONCarriesConflictsAndStacks(t *testing.T) {
	prober := &stubProber{findings: conflictFindings()}
	f := conflictFlags()
	out, diag, err := execConflicts(t, conflictSource(), f, prober)
	if err != nil || diag != "" {
		t.Fatalf("%v %q", err, diag)
	}
	m := decode(t, out)
	rows := map[int]map[string]any{}
	groups := map[int]string{}
	for _, g := range []string{"ready", "checks", "conflict", "draft", "blocked"} {
		for _, r := range m["groups"].(map[string]any)[g].([]any) {
			row := r.(map[string]any)
			rows[int(row["number"].(float64))] = row
			groups[int(row["number"].(float64))] = g
		}
	}
	if groups[1] != "conflict" || groups[2] != "blocked" || groups[3] != "ready" {
		t.Fatalf("groups %v", groups)
	}
	if rows[1]["ready"] != false || rows[2]["reason"] != "parent-conflict" || rows[2]["ready"] != false {
		t.Fatalf("%v %v", rows[1], rows[2])
	}
	bc := rows[1]["base_conflict"].(map[string]any)
	file := bc["files"].([]any)[0].(map[string]any)
	if bc["state"] != "conflicts-with-base" || bc["reason"] != nil || bc["base"] != "main" || file["path"] != "rules.md" || file["base_commit"] != strings.Repeat("b", 40) ||
		bc["merge_base"] != strings.Repeat("e", 40) || bc["blocked_by"] != nil || bc["truncated"] != false {
		t.Fatalf("%v", bc)
	}
	if rows[2]["base_conflict"].(map[string]any)["blocked_by"] != 1.0 {
		t.Fatalf("%v", rows[2]["base_conflict"])
	}
	if ov := rows[3]["base_conflict"].(map[string]any)["overlap"].([]any); len(ov) != 1 || ov[0] != "notes.md" {
		t.Fatalf("early warning: %v", ov)
	}
	if o := m["options"].(map[string]any); o["conflicts"] != true || o["restack_plan"] != false {
		t.Fatalf("%v", o)
	}
	if m["summary"].(map[string]any)["base_conflicts"] != 1.0 || m["summary"].(map[string]any)["ready"] != 1.0 {
		t.Fatalf("%v", m["summary"])
	}
	c := m["conflicts"].(map[string]any)
	if c["remote"] != "origin" || len(c["plans"].([]any)) != 0 {
		t.Fatalf("%v", c)
	}
	st := c["stacks"].([]any)[0].(map[string]any)
	if st["repo"] != "o/a" || st["root"] != 1.0 {
		t.Fatalf("%v", st)
	}
	lv := st["levels"].([]any)
	l1, l2 := lv[0].(map[string]any), lv[1].(map[string]any)
	if l1["number"] != 1.0 || l1["parent"] != nil || l1["clear"] != "valid" || l1["ci"] != "green" || l1["conflict"] != "conflicts-with-base" || l1["status"] != "blocked: restack needed" || l1["depth"] != 0.0 {
		t.Fatalf("%v", l1)
	}
	if l2["parent"] != 1.0 || l2["status"] != "blocked by #1" || l2["depth"] != 1.0 {
		t.Fatalf("%v", l2)
	}
	wd, _ := os.Getwd()
	if prober.calls != 1 || prober.req != (ProberRequest{Dir: wd, Remote: "origin", Repo: "o/a", Host: "github.com"}) {
		t.Fatalf("probed %d times with %+v", prober.calls, prober.req)
	}
}

func TestExecuteUsesTheNamedCheckoutAndRemote(t *testing.T) {
	prober := &stubProber{findings: conflictFindings()}
	f := conflictFlags()
	f.Checkout, f.Remote, f.RestackPlan = "/some/checkout", "upstream", true
	out, _, err := execConflicts(t, conflictSource(), f, prober)
	if err != nil {
		t.Fatal(err)
	}
	if prober.req != (ProberRequest{Dir: "/some/checkout", Remote: "upstream", Repo: "o/a", Host: "github.com"}) {
		t.Fatalf("%+v", prober.req)
	}
	m := decode(t, out)
	c := m["conflicts"].(map[string]any)
	pl := c["plans"].([]any)[0].(map[string]any)
	if c["remote"] != "upstream" || pl["remote"] != "upstream" || m["options"].(map[string]any)["restack_plan"] != true ||
		pl["steps"].([]any)[0].(map[string]any)["rebase"].([]any)[0] != "git fetch upstream" {
		t.Fatalf("%v", c)
	}
}

func TestExecuteHumanListsConflictsInNumberOrderAndMarksTruncation(t *testing.T) {
	src := conflictSource()
	for i := range src.lists["o/a"] { // #3 is the newest, so the conflict group lists it before #1
		if src.lists["o/a"][i].Number == 3 {
			src.lists["o/a"][i].CreatedAt = t0.Add(-time.Hour)
		}
	}
	fs := conflictFindings()
	f1, f3 := fs[1], fs[3]
	f1.Truncated = true
	f3.State, f3.Files, f3.Overlap = conflicts.StateConflicts, []conflicts.FileConflict{{Path: "z.md"}}, nil
	fs[1], fs[3] = f1, f3
	f := conflictFlags()
	f.JSON, f.Human = false, true
	out, _, err := execConflicts(t, src, f, &stubProber{findings: fs})
	if err != nil {
		t.Fatal(err)
	}
	i1, i3 := strings.Index(out, "o/a#1 conflicts with main"), strings.Index(out, "o/a#3 conflicts with main: z.md\n")
	if i1 < 0 || i3 < 0 || i1 > i3 {
		t.Fatalf("conflicts are listed by number (%d, %d):\n%s", i1, i3, out)
	}
	if !strings.Contains(out, "(last changed on main by "+strings.Repeat("b", 12)+"), more not listed\n") {
		t.Fatalf("a cut list says so:\n%s", out)
	}
	if !strings.Contains(out, "2 pull requests conflict with their base\n") {
		t.Fatalf("plural:\n%s", out)
	}
}

func TestExecuteHumanDoesNotMarkUnknownAsConflict(t *testing.T) {
	fs := conflictFindings()
	fs[1] = conflicts.Finding{State: conflicts.StateUnknown, Reason: conflicts.ReasonFetchFailed, Base: "main"}
	f := conflictFlags()
	f.JSON, f.Human = false, true
	out, _, err := execConflicts(t, conflictSource(), f, &stubProber{findings: fs})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "conflicts-with-base") || strings.Contains(out, "base conflicts (") || strings.Contains(out, "conflicts with its base") {
		t.Fatalf("unknown is not a conflict:\n%s", out)
	}
	if !strings.Contains(out, "clear valid  ci green  unknown\n") {
		t.Fatalf("the stack line says unknown:\n%s", out)
	}
}

func TestExecuteWithoutConflictsProbesNothing(t *testing.T) {
	prober := &stubProber{findings: conflictFindings()}
	f := conflictFlags()
	f.Conflicts = false
	out, _, err := execConflicts(t, conflictSource(), f, prober)
	if err != nil || prober.calls != 0 {
		t.Fatalf("%v %d", err, prober.calls)
	}
	m := decode(t, out)
	if m["conflicts"] != nil || m["options"].(map[string]any)["conflicts"] != false || m["summary"].(map[string]any)["base_conflicts"] != 0.0 {
		t.Fatalf("%v", m)
	}
	for _, r := range m["groups"].(map[string]any)["ready"].([]any) {
		if r.(map[string]any)["base_conflict"] != nil {
			t.Fatalf("%v", r)
		}
	}
}

func TestExecuteRestackPlanInJSONAndHuman(t *testing.T) {
	f := conflictFlags()
	f.RestackPlan = true
	out, _, err := execConflicts(t, conflictSource(), f, &stubProber{findings: conflictFindings()})
	if err != nil {
		t.Fatal(err)
	}
	m := decode(t, out)
	plans := m["conflicts"].(map[string]any)["plans"].([]any)
	if len(plans) != 1 {
		t.Fatalf("%v", plans)
	}
	pl := plans[0].(map[string]any)
	steps := pl["steps"].([]any)
	s1 := steps[0].(map[string]any)
	if len(steps) != 2 || s1["number"] != 1.0 || s1["head_ref"] != "one" || s1["why"] != "conflicts-with-base" || s1["skip"] != nil ||
		!strings.HasPrefix(s1["push"].(string), "git push --force-with-lease=one:") || len(s1["rebase"].([]any)) != 3 || len(s1["verify"].([]any)) != 1 {
		t.Fatalf("%v", s1)
	}
	if !strings.HasPrefix(pl["brief"].(string), "# Restack brief: o/a #1, #2\n") || pl["root"] != 1.0 || pl["remote"] != "origin" || len(pl["gh_stack"].([]any)) != 3 {
		t.Fatalf("%v", pl)
	}

	f = conflictFlags()
	f.JSON, f.Human, f.RestackPlan = false, true, true
	hum, _, err := execConflicts(t, conflictSource(), f, &stubProber{findings: conflictFindings()})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# Restack brief: o/a #1, #2", "git rebase --onto origin/main " + strings.Repeat("e", 40) + " one", "--force-with-lease=one:"} {
		if !strings.Contains(hum, want) {
			t.Errorf("human output lacks %q:\n%s", want, hum)
		}
	}
}

func TestExecuteHumanShowsConflictsStacksAndWarnings(t *testing.T) {
	f := conflictFlags()
	f.JSON, f.Human = false, true
	out, _, err := execConflicts(t, conflictSource(), f, &stubProber{findings: conflictFindings()})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"o/a#1  1d  alice  green  clean  clear  title 1  conflicts-with-base\n",
		"base conflicts (1)\n",
		"o/a#1 conflicts with main: rules.md (last changed on main by " + strings.Repeat("b", 12) + ")\n",
		"early warning (1)\n",
		"o/a#3: main also changed notes.md\n",
		"stacks\n",
		"\n  #1  ",
		"  clear valid  ci green  conflicts-with-base  blocked: restack needed\n",
		"\n    #2  ",
		"  clear valid  ci green  clean  blocked by #1\n",
		"1 pull request conflicts with its base\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("human output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "# Restack brief") {
		t.Error("the brief is printed without --restack-plan")
	}
	if i := strings.Index(out, "\nready ("); i >= 0 && strings.Contains(out[i:strings.Index(out[i:], "\n\n")+i], "title 1") {
		t.Errorf("a conflicting pull request is listed as ready:\n%s", out)
	}
}

func TestExecuteEscapesUntrustedPaths(t *testing.T) {
	fs := conflictFindings()
	f1 := fs[1]
	f1.Files = []conflicts.FileConflict{{Path: "evil\x1b[31m\nrules.md"}}
	fs[1] = f1
	f := conflictFlags()
	f.JSON, f.Human = false, true
	out, _, err := execConflicts(t, conflictSource(), f, &stubProber{findings: fs})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "\x1b") || strings.Contains(out, "evil\n") {
		t.Fatalf("control characters reached the terminal: %q", out)
	}
}

// The brief of the restack plan is Markdown that goes to the terminal in the human output, so no
// control character may survive in it: C0, C1 (U+009B is a CSI), format characters such as the bidi
// controls, and the line and paragraph separators.
func TestExecuteEscapesUntrustedPathsInTheRestackBrief(t *testing.T) {
	fs := conflictFindings()
	f1 := fs[1]
	f1.Files = []conflicts.FileConflict{{Path: "c1\u009b31m bidi\u202e line\u2028sep para\u2029sep esc\x1b[31m rules.md"}}
	fs[1] = f1
	f := conflictFlags()
	f.JSON, f.Human, f.RestackPlan = false, true, true
	out, _, err := execConflicts(t, conflictSource(), f, &stubProber{findings: fs})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "# Restack brief") {
		t.Fatalf("the brief is missing:\n%s", out)
	}
	if strings.ContainsAny(out, "\x1b\u009b\u202e\u2028\u2029") {
		t.Fatalf("a control character reached the terminal: %q", out)
	}
}

func TestDefaultProberChecksTheCheckoutsRemote(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := osexec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("remote", "add", "origin", "https://github.com/O/A.git")
	ctx := context.Background()
	pr, cleanup, err := defaultProber(ctx, ProberRequest{Dir: dir, Remote: "origin", Repo: "o/a", Host: "github.com"})
	if err != nil || pr == nil {
		t.Fatalf("%v", err)
	}
	cleanup()
	if _, _, err := defaultProber(ctx, ProberRequest{Dir: dir, Remote: "origin", Repo: "o/b", Host: "github.com"}); !errors.Is(err, watchcmd.ErrUsage) {
		t.Fatalf("a checkout of another repository: %v", err)
	}
	if _, _, err := defaultProber(ctx, ProberRequest{Dir: dir, Remote: "nope", Repo: "o/a", Host: "github.com"}); !errors.Is(err, watchcmd.ErrUsage) || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("a missing remote: %v", err)
	}
	if _, _, err := defaultProber(ctx, ProberRequest{Dir: dir + "/missing", Remote: "origin", Repo: "o/a", Host: "github.com"}); !errors.Is(err, watchcmd.ErrUsage) {
		t.Fatalf("a missing checkout: %v", err)
	}
}

func TestEveryUnknownReasonValidatesAgainstTheSchema(t *testing.T) {
	for _, reason := range []string{conflicts.ReasonFetchFailed, conflicts.ReasonHeadMoved, conflicts.ReasonRefInvalid, conflicts.ReasonPastLimit, conflicts.ReasonProbeFailed, conflicts.ReasonNoBase} {
		fs := conflictFindings()
		fs[3] = conflicts.Finding{State: conflicts.StateUnknown, Reason: reason, Base: "main"}
		out, _, err := execConflicts(t, conflictSource(), conflictFlags(), &stubProber{findings: fs})
		if err != nil {
			t.Fatal(err)
		}
		m := decode(t, out) // validates against schemas/v1/pulls.schema.json
		for _, r := range m["groups"].(map[string]any)["ready"].([]any) {
			if bc := r.(map[string]any)["base_conflict"].(map[string]any); bc["state"] == "unknown" && bc["reason"] != reason {
				t.Errorf("%s: %v", reason, bc)
			}
		}
	}
}
