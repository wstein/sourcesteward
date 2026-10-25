// SPDX-License-Identifier: EUPL-1.2

package stackpublish

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/attribution"
)

const sessionSentinel = "SESSION-SENTINEL-4f1c"

func human() attribution.Origin { return attribution.Origin{Kind: attribution.KindHuman} }
func claude(session string) attribution.Origin {
	return attribution.Origin{Kind: "claude", SessionID: session}
}

func ownTask() TaskEvidence {
	return TaskEvidence{Found: true, TaskID: "spec016-slice1", Complete: true, HeadRef: "refs/heads/feat/own", Origin: &attribution.Origin{Kind: "claude", SessionID: sessionSentinel}}
}

func pushInput(task TaskEvidence, current attribution.Origin) OwnershipInput {
	return OwnershipInput{PlanTask: "spec016-slice1", Kind: KindPushTopic, Subject: Subject{Ref: str("refs/heads/feat/own")}, Task: task, Current: current}
}

func byName(gs []Guard, name string) (Guard, bool) {
	for _, g := range gs {
		if g.Name == name {
			return g, true
		}
	}
	return Guard{}, false
}

func want(t *testing.T, gs []Guard, name string, result GuardResult, reason Reason) {
	t.Helper()
	g, ok := byName(gs, name)
	if !ok || g.Result != result || g.Reason != reason {
		t.Errorf("%s: got %+v (present %v), want %s/%s in %+v", name, g, ok, result, reason, gs)
	}
}

func TestOwnTaskOwnsOnlyItsRecordedBranch(t *testing.T) {
	gs := OwnershipGuards(pushInput(ownTask(), claude(sessionSentinel)))
	want(t, gs, GuardOwnership, GuardPass, "")
	want(t, gs, GuardAttribution, GuardPass, "")
	for name, mutate := range map[string]func(*OwnershipInput){
		"other branch":         func(in *OwnershipInput) { in.Subject.Ref = str("refs/heads/feat/other") },
		"prefix of own branch": func(in *OwnershipInput) { in.Subject.Ref = str("refs/heads/feat/own-two") },
		"no subject ref":       func(in *OwnershipInput) { in.Subject.Ref = nil },
		"other task id":        func(in *OwnershipInput) { in.Task.TaskID = "someone-else" },
		"empty recorded head":  func(in *OwnershipInput) { in.Task.HeadRef = "" },
	} {
		in := pushInput(ownTask(), claude(sessionSentinel))
		mutate(&in)
		g, _ := byName(OwnershipGuards(in), GuardOwnership)
		if g.Result == GuardPass {
			t.Errorf("%s: ownership passed", name)
		}
	}
	in := pushInput(ownTask(), claude(sessionSentinel))
	in.Task.TaskID = "someone-else"
	want(t, OwnershipGuards(in), GuardOwnership, GuardFail, ReasonNotOwned)
	in = pushInput(ownTask(), claude(sessionSentinel))
	in.Task.Complete = false
	want(t, OwnershipGuards(in), GuardOwnership, GuardUnknown, ReasonOwnershipUnknown)
}

func TestUnreadableRecordIsNotTheSameAsAnAbsentRecord(t *testing.T) {
	absent := pushInput(TaskEvidence{}, human())
	want(t, OwnershipGuards(absent), GuardOwnership, GuardFail, ReasonNotOwned)
	unreadable := pushInput(TaskEvidence{Unreadable: true}, human())
	want(t, OwnershipGuards(unreadable), GuardOwnership, GuardUnknown, ReasonOwnershipUnknown)
	// Unreadable and absent are never ownership, for a human or an agent.
	for _, cur := range []attribution.Origin{human(), claude("x")} {
		for _, task := range []TaskEvidence{{}, {Unreadable: true}} {
			if g, _ := byName(OwnershipGuards(pushInput(task, cur)), GuardOwnership); g.Result == GuardPass {
				t.Errorf("ownership passed for %+v by %+v", task, cur)
			}
		}
	}
}

// The Spec 042 D5 rows, as the guards must report them for a push or Ready.
func TestEveryRealD5Row(t *testing.T) {
	recorded := &attribution.Origin{Kind: "claude", SessionID: sessionSentinel}
	cases := []struct {
		name    string
		task    TaskEvidence
		current attribution.Origin
		result  GuardResult
		reason  Reason
	}{
		{"same session", withOrigin(recorded), claude(sessionSentinel), GuardPass, ""},
		{"different session", withOrigin(recorded), claude("other"), GuardFail, ReasonAttributionMismatch},
		{"recorded session, current absent", withOrigin(recorded), attribution.Origin{Kind: "claude"}, GuardFail, ReasonAttributionUnknown},
		{"codex session source is unmeasured and absent", withOrigin(recorded), attribution.Origin{Kind: "codex"}, GuardFail, ReasonAttributionUnknown},
		{"unknown agent without a session", withOrigin(recorded), attribution.Origin{Kind: attribution.KindUnknown}, GuardFail, ReasonAttributionUnknown},
		{"human run is never gated", withOrigin(recorded), human(), GuardPass, ""},
		{"task without a recorded session", withOrigin(&attribution.Origin{Kind: "claude"}), claude("other"), GuardPass, ""},
		{"task record without an origin", withOrigin(nil), claude("other"), GuardPass, ""},
		{"codex current, task without a session", withOrigin(&attribution.Origin{Kind: "claude"}), attribution.Origin{Kind: "codex"}, GuardPass, ""},
	}
	for _, tc := range cases {
		for _, kind := range []ActionKind{KindPushTopic, KindReady} {
			in := pushInput(tc.task, tc.current)
			in.Kind = kind
			if kind == KindReady {
				in.PlanRepo = RepoRef{Host: "github.com", Owner: "owner", Name: "project"}
				in.Subject = Subject{PRHeadRef: str("refs/heads/feat/own"), HeadRepo: repoRef("owner", "project"), BaseRepo: repoRef("owner", "project")}
			}
			gs := OwnershipGuards(in)
			want(t, gs, GuardAttribution, tc.result, tc.reason)
			want(t, gs, GuardOwnership, GuardPass, "")
		}
	}
	// An absent record is never gated by D5, even if a stale origin value is attached to it.
	absent := pushInput(TaskEvidence{Origin: recorded}, claude("other"))
	want(t, OwnershipGuards(absent), GuardAttribution, GuardPass, "")
	want(t, OwnershipGuards(absent), GuardOwnership, GuardFail, ReasonNotOwned)
	// An unreadable record refuses an agent run but never gates a human by D5.
	want(t, OwnershipGuards(pushInput(TaskEvidence{Unreadable: true}, claude("x"))), GuardAttribution, GuardFail, ReasonAttributionUnknown)
	want(t, OwnershipGuards(pushInput(TaskEvidence{Unreadable: true}, human())), GuardAttribution, GuardPass, "")
}

func withOrigin(o *attribution.Origin) TaskEvidence {
	t := ownTask()
	t.Origin = o
	return t
}

func TestD5AppliesToPushAndReadyOnly(t *testing.T) {
	recorded := withOrigin(&attribution.Origin{Kind: "claude", SessionID: sessionSentinel})
	for _, kind := range []ActionKind{KindCreatePR, KindEditBody, KindRetarget, KindNativeLink, KindNativeUnstack} {
		in := OwnershipInput{PlanTask: "spec016-slice1", Kind: kind, Task: recorded, Current: claude("other"),
			Subject: Subject{Ref: str("refs/heads/feat/own"), PRHeadRef: str("refs/heads/feat/own")}}
		want(t, OwnershipGuards(in), GuardAttribution, GuardNotApplicable, "")
	}
}

func TestPullRequestActionsAreOwnedThroughTheirHeadRef(t *testing.T) {
	for _, kind := range []ActionKind{KindEditBody, KindRetarget, KindReady} {
		in := prInput(kind, repoRef("owner", "project"), repoRef("owner", "project"))
		want(t, OwnershipGuards(in), GuardOwnership, GuardPass, "")
		in.Subject.PRHeadRef = str("refs/heads/someone/else")
		want(t, OwnershipGuards(in), GuardOwnership, GuardFail, ReasonNotOwned)
		in.Subject.PRHeadRef = nil
		want(t, OwnershipGuards(in), GuardOwnership, GuardUnknown, ReasonOwnershipUnknown)
		// A ref subject cannot stand in for the PR's head.
		in.Subject = Subject{Ref: str("refs/heads/feat/own"), HeadRepo: repoRef("owner", "project"), BaseRepo: repoRef("owner", "project")}
		want(t, OwnershipGuards(in), GuardOwnership, GuardUnknown, ReasonOwnershipUnknown)
	}
	create := OwnershipInput{PlanTask: "spec016-slice1", Kind: KindCreatePR, Task: ownTask(), Current: human(), Subject: Subject{Ref: str("refs/heads/feat/own")}}
	want(t, OwnershipGuards(create), GuardOwnership, GuardPass, "")
	// Native stack membership across several PRs is not established by one task record.
	link := OwnershipInput{PlanTask: "spec016-slice1", Kind: KindNativeLink, Task: ownTask(), Current: human(), Subject: Subject{PRHeadRef: str("refs/heads/feat/own")}}
	want(t, OwnershipGuards(link), GuardOwnership, GuardUnknown, ReasonOwnershipUnknown)
}

func TestNoLabelPrefixOrAuthorHintWidensOwnership(t *testing.T) {
	task := ownTask()
	task.HeadRef = "refs/heads/feat/own"
	base := pushInput(task, claude(sessionSentinel))
	base.Subject.Ref = str("refs/heads/feat/someone-elses")
	for _, hints := range []Hints{
		{}, {CallerLabel: "claude"}, {CallerLabel: "owner-label-of-the-task"}, {BranchPrefix: "feat/"}, {BranchPrefix: "refs/heads/feat/someone-elses"},
		{AuthorEmail: "owner@example.org"}, {CallerLabel: "x", BranchPrefix: "feat/", AuthorEmail: "y@z"},
	} {
		in := base
		in.Hints = hints
		want(t, OwnershipGuards(in), GuardOwnership, GuardFail, ReasonNotOwned)
	}
	// And hints never remove a real refusal or add a pass for an absent record.
	absent := pushInput(TaskEvidence{}, human())
	absent.Hints = Hints{CallerLabel: "claude", BranchPrefix: "feat/", AuthorEmail: "owner@example.org"}
	want(t, OwnershipGuards(absent), GuardOwnership, GuardFail, ReasonNotOwned)
}

func TestNoSessionIdAppearsInAnyGuard(t *testing.T) {
	for _, current := range []attribution.Origin{claude("other-" + sessionSentinel), claude(sessionSentinel), {Kind: "claude"}, human()} {
		raw, err := json.Marshal(OwnershipGuards(pushInput(ownTask(), current)))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "SESSION-SENTINEL") {
			t.Fatalf("a guard carries a session id: %s", raw)
		}
	}
}

func TestGuardsFeedApplyEndToEnd(t *testing.T) {
	p := savedPlan(t, pushAction("p1", "refs/heads/feat/own"))
	run := func(task TaskEvidence, current attribution.Origin) []string {
		var asked []string
		e, err := drive(p, applyRequest(p, "p1"), allCapabilities(), newBudget(), matching(&asked, func(a Action, f *Fresh) {
			f.Guards = OwnershipGuards(OwnershipInput{PlanTask: p.Task().TaskID, Kind: a.Kind, Subject: Subject{Ref: a.Target.Ref}, Task: task, Current: current})
		}))
		if err != nil {
			t.Fatal(err)
		}
		return states(e)
	}
	task := ownTask()
	task.TaskID = p.Task().TaskID
	if got := run(task, claude(sessionSentinel)); !reflect.DeepEqual(got, []string{"completed"}) {
		t.Fatalf("own task, same session: %v", got)
	}
	if got := run(task, claude("other")); !reflect.DeepEqual(got, []string{"refused:attribution-mismatch"}) {
		t.Fatalf("other session: %v", got)
	}
	if got := run(TaskEvidence{Unreadable: true}, claude("x")); !reflect.DeepEqual(got, []string{"refused:ownership-unknown"}) {
		t.Fatalf("unreadable record: %v", got)
	}
	if got := run(TaskEvidence{}, human()); !reflect.DeepEqual(got, []string{"refused:not-owned"}) {
		t.Fatalf("absent record: %v", got)
	}
}

func TestModeSummaryKeepsMarkerNamesOnly(t *testing.T) {
	m := NewModeSummary([]string{"CODEX_HOME", "CLAUDECODE", "CLAUDECODE", "CLAUDE_CODE_SESSION_ID=" + sessionSentinel, "", "lower", "TOKEN=abc"}, false, true, false)
	if m.Derived != "agent" || m.StdinTerminal || !m.StdoutTerminal {
		t.Fatalf("%+v", m)
	}
	if !reflect.DeepEqual(m.Markers, []string{"CLAUDECODE", "CODEX_HOME"}) {
		t.Fatalf("names must be sorted, unique and values dropped: %v", m.Markers)
	}
	raw, _ := json.Marshal(m)
	if strings.Contains(string(raw), "SENTINEL") || strings.Contains(string(raw), "abc") {
		t.Fatalf("a marker value leaked: %s", raw)
	}
	if a := NewModeSummary(nil, false, false, false); a.Derived != "agent" {
		t.Fatalf("%+v", a)
	}
	if h := NewModeSummary(nil, true, true, true); h.Derived != "human" || h.Markers == nil || len(h.Markers) != 0 {
		t.Fatalf("%+v", h)
	}
	if err := validMode(m); err != nil {
		t.Fatalf("the derived summary must validate: %v", err)
	}
}

func repoRef(owner, name string) *RepoRef {
	return &RepoRef{Host: "github.com", Owner: owner, Name: name}
}

func prInput(kind ActionKind, head, base *RepoRef) OwnershipInput {
	return OwnershipInput{PlanTask: "spec016-slice1", Kind: kind, Task: ownTask(), Current: human(), PlanRepo: RepoRef{Host: "github.com", Owner: "owner", Name: "project"},
		Subject: Subject{PRHeadRef: str("refs/heads/feat/own"), HeadRepo: head, BaseRepo: base}}
}

func TestAForkPRWithTheSameBranchNameIsNotOwned(t *testing.T) {
	own := repoRef("owner", "project")
	for _, kind := range []ActionKind{KindEditBody, KindRetarget, KindReady} {
		want(t, OwnershipGuards(prInput(kind, own, own)), GuardOwnership, GuardPass, "")
		// Same branch name, but the head lives in someone else's fork.
		want(t, OwnershipGuards(prInput(kind, repoRef("mallory", "project"), own)), GuardOwnership, GuardFail, ReasonNotOwned)
		want(t, OwnershipGuards(prInput(kind, repoRef("owner", "project-fork"), own)), GuardOwnership, GuardFail, ReasonNotOwned)
		want(t, OwnershipGuards(prInput(kind, &RepoRef{Host: "ghe.example.org", Owner: "owner", Name: "project"}, own)), GuardOwnership, GuardFail, ReasonNotOwned)
		// A cross-repository base is not the plan repository either.
		want(t, OwnershipGuards(prInput(kind, own, repoRef("elsewhere", "project"))), GuardOwnership, GuardFail, ReasonNotOwned)
		// Unknown head or base repository evidence refuses as unknown, never passes.
		want(t, OwnershipGuards(prInput(kind, nil, own)), GuardOwnership, GuardUnknown, ReasonOwnershipUnknown)
		want(t, OwnershipGuards(prInput(kind, own, nil)), GuardOwnership, GuardUnknown, ReasonOwnershipUnknown)
		want(t, OwnershipGuards(prInput(kind, &RepoRef{}, own)), GuardOwnership, GuardUnknown, ReasonOwnershipUnknown)
		want(t, OwnershipGuards(prInput(kind, &RepoRef{Owner: "owner", Name: "project"}, own)), GuardOwnership, GuardUnknown, ReasonOwnershipUnknown)
	}
	// GitHub owner and repository names compare case-insensitively.
	want(t, OwnershipGuards(prInput(KindReady, repoRef("OWNER", "Project"), repoRef("Owner", "PROJECT"))), GuardOwnership, GuardPass, "")
}
