// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/analysis"
	"github.com/wstein/sourcesteward/internal/tidywizard/derive"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/tidywizard/planset"
	"github.com/wstein/sourcesteward/internal/tidywizard/render"
	"github.com/wstein/sourcesteward/internal/workspace"
)

// wizardWorld replaces the analysis seams of the wizard-layer `tidy plan`.
type wizardWorld struct {
	prep       analysis.Prepared
	prepErr    error
	res        tidywizard.Result
	gotOpt     analysis.Options
	gotCfg     tidywizard.Config
	hadDline   bool
	analyseCtx context.Context
	prepared   int
	planner    *spyPlanner
	// The online pull request evidence seam (Spec 044 D13): every call is recorded, the outcome is set by the test.
	fetches      int
	fetchBudget  *hosting.TidyRunBudget
	prepBudget   *hosting.TidyRunBudget
	fetchMembers []tidywizard.OnlineMember
	online       tidywizard.OnlineOutcome
}

type spyPlanner struct {
	specs []planset.SubSpec
	err   error
	// admit, when set, is the read-only admission of a sub-plan (planset.Admitter).
	admit func(planset.SubSpec) error
}

func (p *spyPlanner) Admit(_ context.Context, s planset.SubSpec) error {
	if p.admit != nil {
		return p.admit(s)
	}
	return nil
}

func (p *spyPlanner) Plan(_ context.Context, s planset.SubSpec) (planset.Planned, error) {
	p.specs = append(p.specs, s)
	if p.err != nil {
		return planset.Planned{}, p.err
	}
	return planset.Planned{ID: "a1b2c3d4e5f" + string(rune('0'+len(p.specs))), StateDir: "/state/plans"}, nil
}

func wcand(repo string, kind tidywizard.Kind, subject string, v tidywizard.Verdict, ownerLabel string, src owner.Source, scope owner.Scope) tidywizard.Candidate {
	c := tidywizard.Candidate{ID: string(kind) + ":" + repo + ":" + subject, Repo: repo, Kind: kind, Verdict: v, Owner: ownerLabel, OwnerSource: src, Scope: scope, Proof: tidywizard.ProofNone, Effects: []string{}}
	if kind == tidywizard.KindWorktree || kind == tidywizard.KindStaleWorktree {
		c.Path = "/w/" + subject
	} else {
		c.Branch = subject
	}
	if v == tidywizard.VerdictSafe {
		c.Proof, c.Effects = tidywizard.ProofAncestor, []string{"remove-registration", "remove-files"}
	}
	return c
}

// privateStore is a private (0700) state directory with the record directories.
func privateStore(t *testing.T) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"plans", "operations", "tasks"} {
		if err := os.Mkdir(filepath.Join(p, k), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func newWizardWorld(t *testing.T, stdinTTY, stdoutTTY bool, environ ...string) *wizardWorld {
	t.Helper()
	w := &wizardWorld{planner: &spyPlanner{}}
	own := wcand("a", tidywizard.KindWorktree, "own1", tidywizard.VerdictSafe, "claude", owner.SourceTaskRecord, owner.ScopeOwn)
	own.Task = "t1"
	ownBranch := wcand("a", tidywizard.KindBranch, "claude/old", tidywizard.VerdictSafe, "claude", owner.SourceTaskRecord, owner.ScopeOwn)
	ownBranch.Effects = []string{"delete-branch:claude/old"}
	other := wcand("a", tidywizard.KindWorktree, "whr1", tidywizard.VerdictSafe, "codex", owner.SourcePathConvention, owner.ScopeOther)
	dec := wcand("b", tidywizard.KindWorktree, "dec", tidywizard.VerdictDecision, "", owner.SourceUnknown, owner.ScopeUnknown)
	ret := wcand("b", tidywizard.KindWorktree, "dirty", tidywizard.VerdictRetain, "claude", owner.SourceTaskRecord, owner.ScopeOwn)
	track := wcand("b", tidywizard.KindTrackingRef, "refs/remotes/origin/x", tidywizard.VerdictSafe, "claude", owner.SourceTaskRecord, owner.ScopeOwn)
	w.prep = analysis.Prepared{
		Workspace: "/ws", Derived: []derive.Derived{{Name: "workspace", Value: "/ws", Source: "discovery"}, {Name: "remote:a", Value: "origin", Source: "origin"}, {Name: "remote:b", Value: "origin", Source: "origin"}},
		Specs: []tidywizard.RepoSpec{{Name: "a", Path: "/ws/a", Base: "refs/heads/main", Remote: "origin"}, {Name: "b", Path: "/ws/b", Base: "refs/heads/main", Remote: "origin"}},
		Meta:  map[string]render.RepoMeta{"a": {Path: "/ws/a", Backend: "git", Base: "refs/heads/main", Remote: "origin"}, "b": {Path: "/ws/b", Backend: "git", Base: "refs/heads/main", Remote: "origin"}},
		Names: []string{"a", "b"}, Notices: []string{},
		Stores: nil, PlanStore: privateStore(t),
	}
	w.res = tidywizard.Result{
		Repositories: []tidywizard.RepoResult{{Name: "a", State: "analysed", Notices: []string{}}, {Name: "b", State: "analysed", Notices: []string{}}},
		Candidates:   []tidywizard.Candidate{own, ownBranch, other, dec, ret, track},
	}
	oldP, oldA, oldW, oldN, oldPl, oldT, oldE, oldF := tidyPrepare, tidyAnalyse, tidyGetwd, tidyNow, tidyPlannerFor, tidyTerminalFacts, tidyEnviron, tidyOnlineFetch
	t.Cleanup(func() {
		tidyPrepare, tidyAnalyse, tidyGetwd, tidyNow, tidyPlannerFor, tidyTerminalFacts, tidyEnviron, tidyOnlineFetch = oldP, oldA, oldW, oldN, oldPl, oldT, oldE, oldF
	})
	tidyOnlineFetch = func(_ context.Context, members []tidywizard.OnlineMember, _ []tidywizard.Candidate, b *hosting.TidyRunBudget) tidywizard.OnlineOutcome {
		w.fetches++
		w.fetchBudget = b
		w.fetchMembers = members
		return w.online
	}
	tidyPrepare = func(ctx context.Context, o analysis.Options) (analysis.Prepared, error) {
		w.gotOpt = o
		w.prepBudget = hosting.TidyRunBudgetFrom(ctx)
		_, w.hadDline = ctx.Deadline()
		w.prepared++
		return w.prep, w.prepErr
	}
	tidyAnalyse = func(ctx context.Context, _ analysis.Prepared, cfg tidywizard.Config) tidywizard.Result {
		w.analyseCtx = ctx
		w.gotCfg = cfg
		return w.res
	}
	tidyGetwd = func() (string, error) { return "/cwd", nil }
	tidyNow = func() time.Time { return time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC) }
	tidyPlannerFor = func(analysis.Prepared) planset.Planner { return w.planner }
	tidyTerminalFacts = func(any, any) (bool, bool) { return stdinTTY, stdoutTTY }
	tidyEnviron = func() []string { return environ }
	return w
}

func docOf(t *testing.T, out string) render.Document {
	t.Helper()
	var d render.Document
	// Cobra may print the usage after the document of a failed run: read the first value only.
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&d); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	return d
}

func verdicts(d render.Document) map[string]bool {
	out := map[string]bool{}
	for _, c := range d.Candidates {
		out[c.ID] = true
	}
	return out
}

// The overview is printed in text, nothing is stored, the exit is 0 for a complete analysis.
func TestWizardPlanPrintsTheOverviewAndStoresNothing(t *testing.T) {
	w := newWizardWorld(t, true, true)
	out, err := runTidy(t, "", "plan", "--rows")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Workspace: /ws", "Analysed 2 repositories", "own1", "[-] retain", "summary: 4 safe"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q missing:\n%s", want, out)
		}
	}
	if len(w.planner.specs) != 0 {
		t.Error("planning stored something without --save")
	}
	_ = filepath.WalkDir(w.prep.PlanStore, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			t.Errorf("the plan store was written: %s", path)
		}
		return nil
	})
	if w.gotOpt.CWD != "/cwd" || w.gotOpt.Online || w.gotCfg.CWD != "/cwd" || w.gotCfg.Agent {
		t.Errorf("options %+v config %+v", w.gotOpt, w.gotCfg)
	}
}

func TestWizardPlanJSONIsOneDocument(t *testing.T) {
	newWizardWorld(t, false, false)
	out, err := runTidy(t, "", "plan", "--json")
	if err != nil {
		t.Fatal(err)
	}
	d := docOf(t, out)
	if d.Command != "tidy-overview" || d.PlanID != "" || len(d.Candidates) != 6 || d.Mode.Mode != owner.ModeAgent || d.Mode.StdinTTY {
		t.Errorf("%+v", d)
	}
	if out2, err := runTidy(t, "", "plan", "--human"); err != nil || strings.HasPrefix(strings.TrimSpace(out2), "{") {
		t.Errorf("--human prints text: %v\n%s", err, out2)
	}
}

// A usage error of the preparation is exit 2 and names the reason; another failure is exit 1.
func TestWizardPlanPreparationErrors(t *testing.T) {
	w := newWizardWorld(t, true, true)
	w.prepErr = &analysis.UsageError{Code: "workspace-not-found", Message: "no Git workspace was found"}
	_, err := runTidy(t, "", "plan")
	if !isUsage(err) || !strings.Contains(err.Error(), "workspace-not-found") {
		t.Errorf("%v", err)
	}
	w.prepErr = errors.New("boom")
	if _, err = runTidy(t, "", "plan"); err == nil || isUsage(err) {
		t.Errorf("a failure of the preparation is a runtime failure: %v", err)
	}
}

// The flags reach the analysis: workspace, members, member bases, every --state-dir in order,
// --online, and --timeout as a deadline over the whole analysis.
func TestWizardPlanFlagsReachTheAnalysis(t *testing.T) {
	w := newWizardWorld(t, true, true)
	_, err := runTidy(t, "", "plan", "--workspace", "/other", "--member", "a", "--member", "b", "--member-base", "a=refs/heads/trunk",
		"--state-dir", "/s/one", "--state-dir", "/s/two", "--online", "--timeout", "30s")
	if err != nil {
		t.Fatal(err)
	}
	want := analysis.Options{Workspace: "/other", CWD: "/cwd", Members: []string{"a", "b"}, MemberBases: []string{"a=refs/heads/trunk"}, StateDirs: []string{"/s/one", "/s/two"}, Online: true}
	if !reflect.DeepEqual(w.gotOpt, want) {
		t.Errorf("got %+v\nwant %+v", w.gotOpt, want)
	}
	if !w.hadDline {
		t.Error("--timeout did not become a deadline")
	}
	w = newWizardWorld(t, true, true)
	if _, err = runTidy(t, "", "plan"); err != nil || w.hadDline {
		t.Errorf("no deadline without --timeout: %v %v", err, w.hadDline)
	}
	if _, err = runTidy(t, "", "plan", "--timeout", "0s"); !isUsage(err) {
		t.Errorf("a non-positive timeout is a usage error: %v", err)
	}
}

// Exit 1 after printing the whole overview: a repository error, a skipped repository whose base or
// remote could not be derived, an unsafe state directory.
func TestWizardPlanExitsOneWhenEvidenceIsIncomplete(t *testing.T) {
	for name, mutate := range map[string]func(*wizardWorld){
		"repository error": func(w *wizardWorld) {
			w.res.Repositories[1] = tidywizard.RepoResult{Name: "b", State: "error", Reason: "boom", Notices: []string{}}
		},
		"base ambiguous": func(w *wizardWorld) {
			w.res.Repositories[1] = tidywizard.RepoResult{Name: "b", State: "skipped", Reason: "base-ambiguous", Notices: []string{}}
		},
		"remote ambiguous": func(w *wizardWorld) {
			w.res.Repositories[1] = tidywizard.RepoResult{Name: "b", State: "skipped", Reason: "remote-ambiguous", Notices: []string{}}
		},
		"base missing": func(w *wizardWorld) {
			w.res.Repositories[1] = tidywizard.RepoResult{Name: "b", State: "skipped", Reason: "base-missing", Notices: []string{}}
		},
		"unsafe state dir": func(w *wizardWorld) {
			w.prep.Stores = []derive.Store{{Path: "/s", Issue: &derive.Issue{Code: "state-dir-unsafe", Reason: "mode"}}}
		},
		"result truncated": func(w *wizardWorld) { w.res.Truncated = true },
	} {
		w := newWizardWorld(t, true, true)
		mutate(w)
		out, err := runTidy(t, "", "plan")
		var failure runtimeFailure
		if !errors.As(err, &failure) || !strings.Contains(out, "Workspace /ws") {
			t.Errorf("%s: err %v, output %q", name, err, out)
		}
	}
	// A skip that is not an error of the evidence (nothing to analyse in a member) stays exit 0.
	w := newWizardWorld(t, true, true)
	w.res.Repositories[1] = tidywizard.RepoResult{Name: "b", State: "skipped", Reason: "something-else", Notices: []string{}}
	if _, err := runTidy(t, "", "plan"); err != nil {
		t.Errorf("%v", err)
	}
}

// A document over 4 MiB is cut, flagged, and the run ends with exit 1 (output-limit).
func TestWizardPlanOutputLimit(t *testing.T) {
	w := newWizardWorld(t, false, false)
	w.res.Candidates = nil
	for i := 0; i < tidywizard.MaxCandidates; i++ {
		c := wcand("a", tidywizard.KindBranch, strings.Repeat("b", 900)+string(rune(0x4e00+i)), tidywizard.VerdictRetain, "claude", owner.SourceTaskRecord, owner.ScopeOwn)
		c.Evidence = strings.Repeat("e", 512)
		w.res.Candidates = append(w.res.Candidates, c)
	}
	out, err := runTidy(t, "", "plan", "--json")
	var failure runtimeFailure
	if !errors.As(err, &failure) || !strings.Contains(err.Error(), "output-limit") {
		t.Fatalf("%v", err)
	}
	if d := docOf(t, out); !d.Truncated {
		t.Error("the document is not flagged truncated")
	}
}

// --mine needs the caller label ($STW_OWNER), narrows the view to it, and forces agent mode;
// --by-owner only narrows the view and does not select the mode.
func TestWizardPlanMineAndByOwnerNarrowOnlyTheView(t *testing.T) {
	w := newWizardWorld(t, true, true)
	if _, err := runTidy(t, "", "plan", "--mine"); !isUsage(err) || !strings.Contains(err.Error(), "STW_OWNER") {
		t.Errorf("--mine without a caller label is a usage error: %v", err)
	}
	w = newWizardWorld(t, true, true, "STW_OWNER=claude")
	out, err := runTidy(t, "", "plan", "--mine", "--json")
	if err != nil {
		t.Fatal(err)
	}
	d := docOf(t, out)
	ids := verdicts(d)
	if ids["worktree:a:whr1"] || ids["worktree:b:dec"] || !ids["worktree:a:own1"] || d.Mode.Mode != owner.ModeAgent || !d.Mode.Mine || !w.gotCfg.Agent || w.gotCfg.Caller != "claude" {
		t.Errorf("mine: ids %v mode %+v config %+v", ids, d.Mode, w.gotCfg)
	}
	w = newWizardWorld(t, true, true)
	out, err = runTidy(t, "", "plan", "--by-owner", "codex", "--json")
	if err != nil {
		t.Fatal(err)
	}
	d = docOf(t, out)
	if ids = verdicts(d); len(ids) != 1 || !ids["worktree:a:whr1"] || d.Mode.Mode != owner.ModeHuman || !reflect.DeepEqual(d.Mode.ByOwner, []string{"codex"}) || w.gotCfg.Agent {
		t.Errorf("by-owner: ids %v mode %+v", ids, d.Mode)
	}
	if d.Summary.Safe != 1 || d.Summary.Retain != 0 {
		t.Errorf("the summary follows the view: %+v", d.Summary)
	}
}

func TestWizardPlanOnlyNarrowsTheKinds(t *testing.T) {
	newWizardWorld(t, true, true)
	for only, want := range map[string][]string{
		"retire":          {"worktree:a:own1", "worktree:a:whr1", "worktree:b:dec", "worktree:b:dirty"},
		"branches":        {"branch:a:claude/old"},
		"tracking":        {"tracking-ref:b:refs/remotes/origin/x"},
		"retire,branches": {"branch:a:claude/old", "worktree:a:own1", "worktree:a:whr1", "worktree:b:dec", "worktree:b:dirty"},
	} {
		out, err := runTidy(t, "", "plan", "--only", only, "--json")
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for id := range verdicts(docOf(t, out)) {
			got = append(got, id)
		}
		sortStrings(got)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("--only %s: %v, want %v", only, got, want)
		}
	}
}

func sortStrings(s []string) {
	for i := range s {
		for j := i + 1; j < len(s); j++ {
			if s[j] < s[i] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

// --save stores the plan set of the safe items of scope own in the plan store (the first valid
// state directory), prints its id, and still applies nothing. A human takes own items of any
// owner source; an agent only those whose owner came from a task record.
func TestWizardPlanSaveStoresTheOwnSafePlanSet(t *testing.T) {
	w := newWizardWorld(t, true, true)
	out, err := runTidy(t, "", "plan", "--save", "--json")
	if err != nil {
		t.Fatal(err)
	}
	d := docOf(t, out)
	if len(d.PlanID) != 12 {
		t.Fatalf("plan id %q", d.PlanID)
	}
	set, err := planset.Load(w.prep.PlanStore, d.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, it := range set.Items {
		ids = append(ids, it.ID)
	}
	sortStrings(ids)
	want := []string{"branch:a:claude/old", "tracking-ref:b:refs/remotes/origin/x", "worktree:a:own1"}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("stored items %v, want %v (never other, unknown, retain or decision)", ids, want)
	}
	if len(set.AuthorizedScopes) != 0 {
		t.Errorf("a non-interactive save authorizes no foreign scope: %+v", set.AuthorizedScopes)
	}
	// Text mode says where it is and that nothing was changed.
	w = newWizardWorld(t, true, true)
	text, err := runTidy(t, "", "plan", "--save")
	if err != nil || !strings.Contains(text, "Stored plan set ") || !strings.Contains(text, "Nothing has been changed") || !strings.Contains(text, "stw tidy apply ") {
		t.Errorf("%v\n%s", err, text)
	}
}

func TestWizardPlanSaveInAgentModeNeedsATaskRecordOwner(t *testing.T) {
	w := newWizardWorld(t, false, false, "STW_OWNER=claude")
	guess := wcand("a", tidywizard.KindBranch, "claude/guess", tidywizard.VerdictSafe, "claude", owner.SourcePathConvention, owner.ScopeOwn)
	w.res.Candidates = append(w.res.Candidates, guess)
	out, err := runTidy(t, "", "plan", "--save", "--json")
	if err != nil {
		t.Fatal(err)
	}
	set, err := planset.Load(w.prep.PlanStore, docOf(t, out).PlanID)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range set.Items {
		if it.ID == guess.ID {
			t.Errorf("an owner guessed from a convention was stored in agent mode: %+v", it)
		}
	}
	if set.Mode.Mode != owner.ModeAgent {
		t.Errorf("the plan set records the mode evidence: %+v", set.Mode)
	}
}

func TestWizardPlanSaveEdgeCases(t *testing.T) {
	// Nothing safe and own: nothing is stored, no plan id, exit 0.
	w := newWizardWorld(t, true, true)
	w.res.Candidates = []tidywizard.Candidate{w.res.Candidates[2], w.res.Candidates[4]}
	out, err := runTidy(t, "", "plan", "--save", "--json")
	if err != nil || docOf(t, out).PlanID != "" || len(w.planner.specs) != 0 {
		t.Errorf("%v %q %d", err, out, len(w.planner.specs))
	}
	// No valid plan store: exit 1, nothing stored.
	w = newWizardWorld(t, true, true)
	w.prep.PlanStore = ""
	_, err = runTidy(t, "", "plan", "--save")
	var failure runtimeFailure
	if !errors.As(err, &failure) || !strings.Contains(err.Error(), "state") {
		t.Errorf("%v", err)
	}
}

// The message of a preparation failure carries paths and error text from outside: it reaches the
// terminal only through esc.
func TestWizardPlanEscapesTheFailureMessages(t *testing.T) {
	w := newWizardWorld(t, true, true)
	w.prepErr = &analysis.UsageError{Code: "workspace-not-found", Message: "no workspace from /tmp/\x1b[2Jevil\r\n‮x"}
	_, err := runTidy(t, "", "plan")
	if !isUsage(err) || strings.ContainsAny(err.Error(), "\x1b\r\n‮") || !strings.Contains(err.Error(), `\x1b[2Jevil\r\n`) {
		t.Errorf("usage: %q", err)
	}
	w.prepErr = errors.New("boom \x1b[31m\nnow")
	_, err = runTidy(t, "", "plan")
	if err == nil || isUsage(err) || strings.ContainsAny(err.Error(), "\x1b\n") {
		t.Errorf("runtime: %q", err)
	}
}

// A deadline or an interrupt during the preparation is exit 1 naming the cause, whatever error the
// preparation made of it (discovery fails when its context ends and reports workspace-not-found).
func TestWizardPlanTimeoutAndInterruptAreNotAWorkspaceError(t *testing.T) {
	w := newWizardWorld(t, true, true)
	tidyPrepare = func(ctx context.Context, _ analysis.Options) (analysis.Prepared, error) {
		<-ctx.Done()
		return analysis.Prepared{}, &analysis.UsageError{Code: "workspace-not-found", Message: "no Git workspace was found (" + ctx.Err().Error() + ")"}
	}
	_, err := runTidy(t, "", "plan", "--timeout", "1ns")
	var failure runtimeFailure
	if !errors.As(err, &failure) || strings.Contains(err.Error(), "workspace-not-found") || !strings.Contains(err.Error(), "--timeout") || !strings.Contains(err.Error(), "nothing was changed") {
		t.Errorf("timeout: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd := tidyCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"plan"})
	err = cmd.ExecuteContext(ctx)
	if !errors.As(err, &failure) || strings.Contains(err.Error(), "workspace-not-found") || !strings.Contains(err.Error(), "interrupted") {
		t.Errorf("interrupt: %v", err)
	}
	_ = w
}

// A very long path in the message does not cut the fixed hint away; only the outside parts are
// bounded by esc, and nothing raw reaches the message.
func TestWizardPlanUsageMessageKeepsTheHint(t *testing.T) {
	w := newWizardWorld(t, true, true)
	w.prepErr = &analysis.UsageError{Code: "workspace-not-found", Message: "no Git workspace was found from /" + strings.Repeat("p\x1b", 300) + "; give --workspace PATH, or use the expert layer"}
	_, err := runTidy(t, "", "plan")
	if !isUsage(err) || !strings.Contains(err.Error(), "give --workspace PATH, or use the expert layer") || strings.Contains(err.Error(), "\x1b") {
		t.Errorf("%q", err)
	}
}

// Failures of --save are reported after the overview, with every message escaped; an unusable plan
// store does not hide the overview (text and JSON), the run ends with exit 1.
func TestWizardPlanSaveFailuresPrintTheOverviewFirst(t *testing.T) {
	for name, setup := range map[string]func(*wizardWorld){
		"no plan store": func(w *wizardWorld) { w.prep.PlanStore = "" },
		"planner fails": func(w *wizardWorld) { w.planner.err = errors.New("planner \x1b]0;pwned\x07 failed\nnow") },
	} {
		for _, flags := range [][]string{{"plan", "--save"}, {"plan", "--save", "--json"}} {
			w := newWizardWorld(t, true, true)
			setup(w)
			out, err := runTidy(t, "", flags...)
			var failure runtimeFailure
			if !errors.As(err, &failure) || strings.ContainsAny(err.Error(), "\x1b\x07\n") {
				t.Errorf("%s %v: error %q", name, flags, err)
			}
			if len(flags) == 3 {
				if d := docOf(t, out); d.Command != "tidy-overview" || d.PlanID != "" {
					t.Errorf("%s: %+v", name, d)
				}
			} else if !strings.Contains(out, "Workspace /ws") {
				t.Errorf("%s: no overview before the failure:\n%s", name, out)
			}
		}
	}
}

func TestWizardPlanOnlyStaleWorktrees(t *testing.T) {
	w := newWizardWorld(t, true, true)
	stale := wcand("a", tidywizard.KindStaleWorktree, "gone", tidywizard.VerdictRetain, "claude", owner.SourceTaskRecord, owner.ScopeOwn)
	w.res.Candidates = append(w.res.Candidates, stale)
	out, err := runTidy(t, "", "plan", "--only", "stale-worktrees", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if ids := verdicts(docOf(t, out)); len(ids) != 1 || !ids[stale.ID] {
		t.Errorf("%v", ids)
	}
}

// The stored plan set line says how much it covers, as in the specification.
func TestWizardPlanSaveSaysHowMuchWasStored(t *testing.T) {
	newWizardWorld(t, true, true)
	out, err := runTidy(t, "", "plan", "--save")
	if err != nil || !strings.Contains(out, "(5 effects in 2 repositories)") {
		t.Errorf("%v\n%s", err, out)
	}
}

// A deadline that ends the analysis itself still prints what was analysed and ends with exit 1
// naming the deadline.
func TestWizardPlanDeadlineDuringTheAnalysis(t *testing.T) {
	w := newWizardWorld(t, true, true)
	tidyAnalyse = func(ctx context.Context, _ analysis.Prepared, _ tidywizard.Config) tidywizard.Result {
		<-ctx.Done()
		return w.res
	}
	out, err := runTidy(t, "", "plan", "--timeout", "1ns")
	var failure runtimeFailure
	if !errors.As(err, &failure) || !strings.Contains(err.Error(), "--timeout") || !strings.Contains(out, "Workspace /ws") {
		t.Errorf("%v\n%s", err, out)
	}
}

// escFull must not cut an ordinary message: 250 plain characters come back as they are, whatever
// the size of the pieces it escapes in (a piece over esc's 120-character bound would be cut).
func TestEscFullDoesNotCutOrdinaryMessages(t *testing.T) {
	for _, n := range []int{1, 99, 100, 101, 119, 120, 121, 130, 200, 250, 1000} {
		msg := strings.Repeat("x", n)
		if got := escFull(msg); got != msg {
			t.Errorf("%d characters: cut to %d (%q...)", n, len(got), got[max(0, len(got)-8):])
		}
	}
	// Multibyte text is counted in characters, not bytes.
	if got := escFull(strings.Repeat("ä", 250)); got != strings.Repeat("ä", 250) {
		t.Errorf("multibyte message was cut: %d bytes", len(got))
	}
	// Escapes lengthen a piece but never trigger the cut either: 100 ESC characters per piece.
	if got := escFull(strings.Repeat("\x1b", 250)); got != strings.Repeat(`\x1b`, 250) {
		t.Errorf("escaped message was cut: %d bytes", len(got))
	}
}

// The escaped message is bounded as a whole, so an enormous error cannot flood the terminal.
func TestEscFullBoundsTheWholeMessage(t *testing.T) {
	got := escFull(strings.Repeat("e", 100000))
	// The bound is 4096 characters (the literal, not the constant under test).
	if got != strings.Repeat("e", 4096)+"..." {
		t.Errorf("%d bytes, tail %q", len(got), got[max(0, len(got)-8):])
	}
	if maxEscapedMessage != 4096 {
		t.Errorf("maxEscapedMessage is %d", maxEscapedMessage)
	}
	hostile := escFull(strings.Repeat("\x1b", 100000))
	if len(hostile) > maxEscapedMessage*4+8 {
		t.Errorf("hostile message of %d bytes", len(hostile))
	}
	if short := escFull("short"); short != "short" {
		t.Errorf("%q", short)
	}
}

// A store or planner error that follows a timeout does not hide the timeout: the cause comes
// first and the save error is shown as detail.
func TestWizardPlanSaveErrorAfterATimeoutKeepsTheCause(t *testing.T) {
	w := newWizardWorld(t, true, true)
	w.planner.err = errors.New("planner failed: context deadline exceeded")
	tidyAnalyse = func(ctx context.Context, _ analysis.Prepared, _ tidywizard.Config) tidywizard.Result {
		<-ctx.Done()
		return w.res
	}
	_, err := runTidy(t, "", "plan", "--save", "--timeout", "1ns")
	var failure runtimeFailure
	if !errors.As(err, &failure) || !strings.Contains(err.Error(), "--timeout of 1ns elapsed") || !strings.Contains(err.Error(), "planner failed") {
		t.Errorf("%v", err)
	}
	// Without a timeout the save error stands alone.
	w = newWizardWorld(t, true, true)
	w.planner.err = errors.New("planner failed")
	_, err = runTidy(t, "", "plan", "--save")
	if !errors.As(err, &failure) || strings.Contains(err.Error(), "--timeout") || !strings.Contains(err.Error(), "planner failed") {
		t.Errorf("%v", err)
	}
}

// --timeout is also the observation window of every repository (it may exceed the old fixed 60 s),
// up to the documented maximum; without it the observation window is the scaled default.
func TestWizardTimeoutIsTheObservationWindowUpToTheMaximum(t *testing.T) {
	w := newWizardWorld(t, true, true)
	if _, err := runTidy(t, "", "plan", "--timeout", "15m"); err != nil {
		t.Fatal(err)
	}
	if d, ok := workspace.ObservationTimeout(w.analyseCtx); !ok || d != 15*time.Minute {
		t.Errorf("observation window %v %v, want 15m", d, ok)
	}
	w = newWizardWorld(t, true, true)
	if _, err := runTidy(t, "", "plan"); err != nil {
		t.Fatal(err)
	}
	if _, ok := workspace.ObservationTimeout(w.analyseCtx); ok {
		t.Error("an observation window without --timeout")
	}
	w = newWizardWorld(t, true, true)
	if _, err := runTidy(t, "", "plan", "--timeout", "1h"); err != nil {
		t.Errorf("the maximum itself is allowed: %v", err)
	}
	if _, err := runTidy(t, "", "plan", "--timeout", "61m"); !isUsage(err) || !strings.Contains(err.Error(), "1h") {
		t.Errorf("above the maximum is a usage error naming it: %v", err)
	}
}

func TestWizardTimeoutHelpNamesTheWindowAndTheMaximum(t *testing.T) {
	newWizardWorld(t, true, true)
	out, _ := runTidy(t, "", "--help")
	if !strings.Contains(out, "observation window") || !strings.Contains(out, "at most 1h") {
		t.Errorf("--timeout help:\n%s", out)
	}
}
