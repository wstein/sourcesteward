// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/tidywizard"
)

// runTidyBoth is runTidy that also returns what went to stderr.
func runTidyBoth(t *testing.T, stdin string, args ...string) (out, errOut string, err error) {
	t.Helper()
	cmd := tidyCommand()
	var o, e bytes.Buffer
	cmd.SetOut(&o)
	cmd.SetErr(&e)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err = cmd.Execute()
	return o.String(), e.String(), err
}

func markOnline(w *wizardWorld) {
	for i := range w.prep.Specs {
		w.prep.Specs[i].Online = true
	}
}

// Spec 044 T005a (D13): the table of the decision, flag by flag and context by context.
func TestResolveTidyOnline(t *testing.T) {
	for name, tc := range map[string]struct {
		online, offline, human bool
		want                   tidyOnlineChoice
		err                    bool
	}{
		"human wizard, no flag":    {false, false, true, tidyOnlineChoice{On: true, Value: "default", Source: "human wizard"}, false},
		"human wizard, --online":   {true, false, true, tidyOnlineChoice{On: true, Value: "flag", Source: "--online"}, false},
		"human wizard, --offline":  {false, true, true, tidyOnlineChoice{Value: "off", Source: "--offline"}, false},
		"both flags":               {true, true, true, tidyOnlineChoice{}, true},
		"both flags, not human":    {true, true, false, tidyOnlineChoice{}, true},
		"plan or agent, no flag":   {false, false, false, tidyOnlineChoice{}, false},
		"plan or agent, --online":  {true, false, false, tidyOnlineChoice{On: true}, false},
		"plan or agent, --offline": {false, true, false, tidyOnlineChoice{}, false},
	} {
		got, err := resolveTidyOnline(tc.online, tc.offline, tc.human)
		if (err != nil) != tc.err || got != tc.want {
			t.Errorf("%s: %+v %v, want %+v (error %v)", name, got, err, tc.want, tc.err)
		}
	}
}

// Online is the default of the human wizard only; --offline turns it off.
func TestHumanWizardIsOnlineByDefaultAndOfflineTurnsItOff(t *testing.T) {
	w := newInteractive(t, true, true)
	markOnline(w.wizardWorld)
	out, errOut, err := runTidyBoth(t, "\n")
	if err != nil || !w.gotOpt.Online || !w.gotOpt.OnlineDefault || w.fetches != 1 {
		t.Fatalf("%v %+v fetches %d", err, w.gotOpt, w.fetches)
	}
	if !strings.Contains(out, "online default (human wizard)") {
		t.Errorf("the derived online entry is missing:\n%s", out)
	}
	if got := len(w.fetchMembers); got != 2 || w.fetchMembers[0] != (tidywizard.OnlineMember{Name: "a", Path: "/ws/a", Remote: "origin"}) {
		t.Errorf("members %+v", w.fetchMembers)
	}
	if errOut != "GitHub: checked 2 of 2 repositories\n" {
		t.Errorf("stderr %q", errOut)
	}

	w = newInteractive(t, true, true)
	markOnline(w.wizardWorld)
	out, errOut, err = runTidyBoth(t, "\n", "--offline")
	if err != nil || w.gotOpt.Online || w.gotOpt.OnlineDefault || w.fetches != 0 || errOut != "" {
		t.Fatalf("%v %+v fetches %d stderr %q", err, w.gotOpt, w.fetches, errOut)
	}
	if !strings.Contains(out, "online off (--offline)") {
		t.Errorf("the derived entry of --offline is missing:\n%s", out)
	}
}

// An explicit --online keeps today's behavior: it is a flag, not the implicit default.
func TestExplicitOnlineIsAFlagNotTheDefault(t *testing.T) {
	w := newInteractive(t, true, true)
	markOnline(w.wizardWorld)
	out, _, err := runTidyBoth(t, "\n", "--online")
	if err != nil || !w.gotOpt.Online || w.gotOpt.OnlineDefault || w.fetches != 1 || !strings.Contains(out, "online flag (--online)") {
		t.Fatalf("%v %+v fetches %d\n%s", err, w.gotOpt, w.fetches, out)
	}
}

// plan, --json, --save, agent mode and the expert layer stay offline by default, call no
// network, and --offline changes nothing there, byte for byte.
func TestEverythingButTheHumanWizardStaysOffline(t *testing.T) {
	for name, tc := range map[string]struct {
		stdinTTY, stdoutTTY bool
		env                 []string
		args                []string
	}{
		"plan":               {true, true, nil, []string{"plan"}},
		"plan json":          {true, true, nil, []string{"plan", "--json"}},
		"plan human":         {true, true, nil, []string{"plan", "--human"}},
		"bare json":          {true, true, nil, []string{"--json"}},
		"bare, no terminal":  {false, false, nil, nil},
		"bare, piped stdin":  {false, true, nil, nil},
		"bare, piped stdout": {true, false, nil, nil},
		"bare --mine":        {true, true, []string{"STW_OWNER=claude"}, []string{"--mine"}},
		"agent marker":       {true, true, []string{"CODEX_SANDBOX=1"}, nil},
	} {
		w := newWizardWorld(t, tc.stdinTTY, tc.stdoutTTY, tc.env...)
		markOnline(w)
		plain, errPlain, err := runTidyBoth(t, "", tc.args...)
		if err != nil || w.gotOpt.Online || w.gotOpt.OnlineDefault || w.fetches != 0 || errPlain != "" {
			t.Errorf("%s: %v online %v fetches %d stderr %q", name, err, w.gotOpt.Online, w.fetches, errPlain)
		}
		if strings.Contains(plain, "\"online\"") || strings.Contains(plain, "online default") || strings.Contains(plain, "online off") {
			t.Errorf("%s: an online entry appears:\n%s", name, plain)
		}
		withFlag, _, err := runTidyBoth(t, "", append(append([]string{}, tc.args...), "--offline")...)
		if err != nil || withFlag != plain || w.fetches != 0 || w.gotOpt.Online {
			t.Errorf("%s: --offline changed something: %v\n%s\nversus\n%s", name, err, withFlag, plain)
		}
	}
	// --online is the opt-in everywhere else, as today: the observer reads, the pull requests are not fetched.
	w := newWizardWorld(t, true, true)
	if _, _, err := runTidyBoth(t, "", "plan", "--online"); err != nil || !w.gotOpt.Online || w.gotOpt.OnlineDefault || w.fetches != 0 {
		t.Errorf("plan --online: %v %+v fetches %d", err, w.gotOpt, w.fetches)
	}
}

// --online and --offline together are a usage error (exit 2) before anything is analysed; --offline
// is a wizard flag: refused with a PATH and with an expert flag.
func TestOfflineUsageErrors(t *testing.T) {
	for name, args := range map[string][]string{
		"bare":             {"--online", "--offline"},
		"bare json":        {"--online", "--offline", "--json"},
		"plan":             {"plan", "--online", "--offline"},
		"plan json":        {"plan", "--json", "--online", "--offline"},
		"plan with a path": {"plan", "--offline", "/some/path"},
		"plan expert flag": {"plan", "--offline", "--base", "refs/heads/main"},
		"plan contract 3":  {"plan", "--offline", "--contract", "3"},
		"plan and a task":  {"plan", "--offline", "--task", "T"},
		"plan contract 4":  {"plan", "--offline", "--contract", "4"},
		"bare with a path": {"--offline", "/some/path"},
		"bare expert flag": {"--offline", "--base", "refs/heads/main"},
		"apply":            {"apply", "--offline"},
	} {
		w := newInteractive(t, true, true)
		_, _, err := runTidyBoth(t, "", args...)
		if !isUsage(err) || w.prepared != 0 || w.fetches != 0 || len(w.applied) != 0 {
			t.Errorf("%s: err %v prepared %d fetches %d", name, err, w.prepared, w.fetches)
		}
	}
}

func TestOfflineFlagIsClassifiedAsAWizardFlag(t *testing.T) {
	if tidyFlagClasses["offline"] != flagWizard {
		t.Fatalf("--offline is %v", tidyFlagClasses["offline"])
	}
	for _, args := range [][]string{{"plan"}, nil} {
		cmd, _, err := tidyCommand().Find(args)
		if err != nil || cmd.Flags().Lookup("offline") == nil {
			t.Errorf("%v: --offline is not registered (%v)", args, err)
		}
	}
	if apply, _, _ := tidyCommand().Find([]string{"apply"}); apply.Flags().Lookup("offline") != nil {
		t.Error("apply must not take --offline")
	}
}

// A repository that could not be read online is judged offline and says so once; the exit code and
// the rest of the run are as offline.
func TestFallbackSaysSoOnceAndKeepsTheExitCode(t *testing.T) {
	w := newInteractive(t, true, true)
	markOnline(w.wizardWorld)
	w.res.Repositories[0].OnlineFallback = tidywizard.ReasonRemoteUnsupported
	w.online = tidywizard.OnlineOutcome{Members: []tidywizard.MemberOutcome{
		{Name: "a", State: tidywizard.MemberChecked}, {Name: "b", State: tidywizard.MemberOffline, Reason: tidywizard.ReasonNotAuthenticated},
	}}
	out, errOut, err := runTidyBoth(t, "\n")
	if err != nil {
		t.Fatal(err)
	}
	want := "GitHub: checked 0 of 2 repositories; the others are judged offline (gh is not authenticated; the remote is not a supported github.com repository)\n"
	if errOut != want {
		t.Errorf("stderr %q\nwant   %q", errOut, want)
	}
	if !strings.Contains(out, "online off (fallback: not-authenticated, remote-unsupported)") {
		t.Errorf("the derived entry names the reasons:\n%s", out)
	}
	if strings.Contains(out, "GitHub: checked") {
		t.Error("the notice belongs on stderr")
	}

	// A partial fallback keeps the online value and counts the others as checked.
	w = newInteractive(t, true, true)
	markOnline(w.wizardWorld)
	w.online = tidywizard.OnlineOutcome{Members: []tidywizard.MemberOutcome{
		{Name: "a", State: tidywizard.MemberChecked}, {Name: "b", State: tidywizard.MemberOffline, Reason: tidywizard.ReasonRateLimit},
	}}
	out, errOut, err = runTidyBoth(t, "\n")
	if err != nil || errOut != "GitHub: checked 1 of 2 repositories; the others are judged offline (GitHub rate limit reached)\n" || !strings.Contains(out, "online default (human wizard; offline for 1 of 2 repositories") {
		t.Errorf("%v %q\n%s", err, errOut, out)
	}

	// Everything checked: the one line without a reason.
	w = newInteractive(t, true, true)
	markOnline(w.wizardWorld)
	w.online = tidywizard.OnlineOutcome{Members: []tidywizard.MemberOutcome{{Name: "a", State: tidywizard.MemberChecked}, {Name: "b", State: tidywizard.MemberChecked}}}
	if _, errOut, err = runTidyBoth(t, "\n"); err != nil || errOut != "GitHub: checked 2 of 2 repositories\n" {
		t.Errorf("%v %q", err, errOut)
	}
}

// The expert layer of contract 4 refuses --offline with the wizard-flag message, not by accident.
func TestOfflineIsRefusedByNameInTheContract4Layer(t *testing.T) {
	newInteractive(t, true, true)
	for _, args := range [][]string{{"plan", "--offline", "--contract", "4"}, {"plan", "--offline", "--prune-registration", "x"}} {
		_, _, err := runTidyBoth(t, "", args...)
		if !isUsage(err) || err == nil || !strings.Contains(err.Error(), "--offline is a wizard flag and cannot be used with the expert layer") {
			t.Errorf("%v: %v", args, err)
		}
	}
}

// Spec 044 D13: one run budget serves the whole human wizard run. The observation (which makes
// the tracking reads) and the pull request reads get the same budget; a run that stays offline has none.
func TestOneRunBudgetServesTheObservationAndThePullRequestReads(t *testing.T) {
	w := newInteractive(t, true, true)
	markOnline(w.wizardWorld)
	if _, _, err := runTidyBoth(t, "\n"); err != nil {
		t.Fatal(err)
	}
	if w.prepBudget == nil || w.prepBudget != w.fetchBudget {
		t.Fatalf("the observation and the fetch must share one budget: %p %p", w.prepBudget, w.fetchBudget)
	}
	for name, args := range map[string][]string{"offline": {"--offline"}, "json": {"--json"}} {
		w = newInteractive(t, true, true)
		if _, _, err := runTidyBoth(t, "\n", args...); err != nil || w.prepBudget != nil || w.fetchBudget != nil {
			t.Errorf("%s: %v %p %p", name, err, w.prepBudget, w.fetchBudget)
		}
	}
	pw := newWizardWorld(t, true, true)
	if _, _, err := runTidyBoth(t, "", "plan", "--online"); err != nil || pw.prepBudget != nil {
		t.Errorf("plan --online has no run budget: %v %p", err, pw.prepBudget)
	}
}

// A stop of the run seen by an earlier read names the cause once on the notice line.
func TestTheStopOfTheRunIsTheReasonOfTheFallback(t *testing.T) {
	w := newInteractive(t, true, true)
	markOnline(w.wizardWorld)
	w.res.Repositories[0].OnlineFallback = tidywizard.ReasonError
	w.online = tidywizard.OnlineOutcome{Stopped: hosting.TidyStopRateLimit, Members: []tidywizard.MemberOutcome{
		{Name: "a", State: tidywizard.MemberChecked}, {Name: "b", State: tidywizard.MemberOffline, Reason: tidywizard.ReasonRateLimit}}}
	_, errOut, err := runTidyBoth(t, "\n")
	if err != nil || errOut != "GitHub: checked 0 of 2 repositories; the others are judged offline (GitHub rate limit reached)\n" {
		t.Errorf("%v %q", err, errOut)
	}
}

// A stop of the run replaces a generic failure as the named reason even when every pull request
// read finished.
func TestAStopOfTheRunReplacesAGenericFallbackReason(t *testing.T) {
	w := newInteractive(t, true, true)
	markOnline(w.wizardWorld)
	w.res.Repositories[0].OnlineFallback = tidywizard.ReasonError
	w.online = tidywizard.OnlineOutcome{Stopped: hosting.TidyStopRunCap, Members: []tidywizard.MemberOutcome{{Name: "a", State: tidywizard.MemberChecked}, {Name: "b", State: tidywizard.MemberChecked}}}
	_, errOut, err := runTidyBoth(t, "\n")
	if err != nil || errOut != "GitHub: checked 1 of 2 repositories; the others are judged offline (per-run request cap reached)\n" {
		t.Errorf("%v %q", err, errOut)
	}
}

// The production fetch hands the run budget of the wizard to the pull request reads: a budget that
// already saw a rate limit judges the repository offline for that reason, without any session.
func TestProductionFetchUsesTheSharedBudget(t *testing.T) {
	stopped := hosting.NewTidyRunBudget(roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 429, Header: http.Header{}, Body: http.NoBody}, nil
	}), 10)
	defer stopped.Close()
	req, _ := http.NewRequest(http.MethodGet, "https://api.github.com/repos/o/r", nil)
	if res, err := stopped.RoundTrip(req); err != nil || res.StatusCode != 429 {
		t.Fatal(err)
	}
	cands := []tidywizard.Candidate{{ID: "branch:a:x", Repo: "a", Kind: tidywizard.KindBranch, Verdict: tidywizard.VerdictRetain, Reason: "unmerged", Proof: tidywizard.ProofNone, Tip: strings.Repeat("a", 40)}}
	out := tidyOnlineFetch(context.Background(), []tidywizard.OnlineMember{{Name: "a", Path: "/nonexistent/a", Remote: "origin"}}, cands, stopped)
	if len(out.Members) != 1 || out.Members[0].Reason != tidywizard.ReasonRateLimit || out.Stopped != hosting.TidyStopRateLimit || out.Requests != 1 {
		t.Fatalf("%+v", out)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// The merged pull request of a tip is on the candidate the wizard goes on with (Spec 044 C2): the
// planner sees the number of the asked tip and none for a tip without a merged answer, and an
// offline run (--offline) attaches nothing.
func TestMergedPullRequestNumberReachesTheCandidates(t *testing.T) {
	prs := func(w *interactive) map[string]int {
		got := map[string]int{}
		for _, s := range w.planner.specs {
			for _, c := range s.Items {
				got[c.ID] = c.PR
			}
		}
		return got
	}
	w := newInteractive(t, true, true)
	markOnline(w.wizardWorld)
	for i := range w.res.Candidates {
		switch w.res.Candidates[i].ID {
		case "branch:a:claude/old":
			w.res.Candidates[i].Tip = "tipOld"
		case "worktree:a:own1":
			w.res.Candidates[i].Tip = "tipOwn"
		}
	}
	w.online = tidywizard.OnlineOutcome{Facts: map[tidywizard.TipKey]tidywizard.PREvidence{
		{Repo: "a", Tip: "tipOld"}: {State: tidywizard.PRMerged, Number: 42},
		{Repo: "a", Tip: "tipOwn"}: {State: tidywizard.PRUnknown},
	}}
	if _, _, err := runTidyBoth(t, "a\nn\n", "--rows"); err != nil {
		t.Fatal(err)
	}
	got := prs(w)
	if got["branch:a:claude/old"] != 42 || got["worktree:a:own1"] != 0 || len(got) == 0 {
		t.Errorf("pull request numbers %v", got)
	}

	w = newInteractive(t, true, true)
	markOnline(w.wizardWorld)
	for i := range w.res.Candidates {
		if w.res.Candidates[i].ID == "branch:a:claude/old" {
			w.res.Candidates[i].Tip = "tipOld"
		}
	}
	w.online = tidywizard.OnlineOutcome{Facts: map[tidywizard.TipKey]tidywizard.PREvidence{{Repo: "a", Tip: "tipOld"}: {State: tidywizard.PRMerged, Number: 42}}}
	if _, _, err := runTidyBoth(t, "a\nn\n", "--rows", "--offline"); err != nil {
		t.Fatal(err)
	}
	if got := prs(w); got["branch:a:claude/old"] != 0 || len(got) == 0 {
		t.Errorf("--offline attached %v", got)
	}
}
