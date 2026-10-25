// SPDX-License-Identifier: EUPL-1.2

package stackpublish

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// fakeEdge records every call in order and returns scripted answers. reads are consumed in order;
// the last one repeats.
type fakeEdge struct {
	log       []string
	d5        Guard
	reads     []PullRequestState
	readErrs  []error
	evidence  ReadyInput
	evErr     error
	intentErr error
	markErr   error
	intents   []ReadyIntent
	marked    []string
	onMark    func(*fakeEdge)
}

func (f *fakeEdge) Attribution() Guard {
	f.log = append(f.log, "d5")
	return f.d5
}

func (f *fakeEdge) ReadPullRequest() (PullRequestState, error) {
	n := 0
	for _, l := range f.log {
		if l == "read" {
			n++
		}
	}
	f.log = append(f.log, "read")
	i := n
	if i >= len(f.reads) {
		i = len(f.reads) - 1
	}
	var err error
	if n < len(f.readErrs) {
		err = f.readErrs[n]
	}
	return f.reads[i], err
}

func (f *fakeEdge) Evidence(pr PullRequestState) (ReadyInput, error) {
	f.log = append(f.log, "evidence")
	return f.evidence, f.evErr
}

func (f *fakeEdge) RecordIntent(in ReadyIntent) error {
	f.log = append(f.log, "intent")
	f.intents = append(f.intents, in)
	return f.intentErr
}

func (f *fakeEdge) MarkReady(nodeID string) error {
	f.log = append(f.log, "mark")
	f.marked = append(f.marked, nodeID)
	if f.onMark != nil {
		f.onMark(f)
	}
	return f.markErr
}

const goodNodeID = "PR_kwDOABCD1234"

func draftPR() PullRequestState {
	return PullRequestState{Number: 7, State: "open", Draft: true, NodeID: goodNodeID, BaseRepository: "owner/project", HeadRepository: "owner/project", HeadSHA: sha('a'), BaseSHA: sha('b')}
}

func readyPR() PullRequestState {
	pr := draftPR()
	pr.Draft = false
	return pr
}

func newEdge() *fakeEdge {
	return &fakeEdge{d5: Guard{Name: GuardAttribution, Result: GuardPass}, reads: []PullRequestState{draftPR(), readyPR()}, evidence: goodReady()}
}

func readyRequest() ReadyRequest {
	return ReadyRequest{Repository: "owner/project", Number: 7, Receipt: EvaluateReady(goodReady())}
}

func wantRefused(t *testing.T, r ReadyResult, reason Reason, detail string) {
	t.Helper()
	if r.State != StateRefused || r.Reason == nil || *r.Reason != reason || r.Detail != detail || r.Attempted {
		t.Fatalf("want refused %s/%s without an attempt, got %+v (reason %v)", reason, detail, r, r.Reason)
	}
	if r.Status() != StatusRefused || ExitCode(r.Status()) != 1 {
		t.Fatalf("status %v", r.Status())
	}
}

func TestTheReadySequenceIsD5ReadEvaluateIntentMutationReread(t *testing.T) {
	e := newEdge()
	r := ExecuteReady(e, readyRequest())
	want := []string{"d5", "read", "evidence", "intent", "mark", "read"}
	if !reflect.DeepEqual(e.log, want) {
		t.Fatalf("order %v want %v", e.log, want)
	}
	if r.State != StateCompleted || !r.Attempted || r.AlreadyReady || r.Reason != nil || r.Observed == nil || r.Observed.Draft || r.Status() != StatusComplete {
		t.Fatalf("%+v", r)
	}
	if !reflect.DeepEqual(e.marked, []string{goodNodeID}) {
		t.Fatalf("the node id of the fresh read is the only mutation input: %v", e.marked)
	}
	if len(e.intents) != 1 || e.intents[0] != (ReadyIntent{Repository: "owner/project", Number: 7, HeadSHA: sha('a'), BaseSHA: sha('b')}) {
		t.Fatalf("%+v", e.intents)
	}
}

func TestD5IsAppliedBeforeEveryAttemptAndBeforeAnyRead(t *testing.T) {
	for _, g := range []Guard{
		{Name: GuardAttribution, Result: GuardFail, Reason: ReasonAttributionMismatch},
		{Name: GuardAttribution, Result: GuardUnknown, Reason: ReasonAttributionUnknown},
		{Name: GuardAttribution, Result: GuardNotApplicable},
		{Name: GuardOwnership, Result: GuardPass},
		{Name: GuardAttribution, Result: "maybe"},
		{},
	} {
		e := newEdge()
		e.d5 = g
		r := ExecuteReady(e, readyRequest())
		if r.State != StateRefused || r.Attempted || !reflect.DeepEqual(e.log, []string{"d5"}) {
			t.Errorf("%+v: %+v log %v", g, r, e.log)
		}
	}
	e := newEdge()
	e.d5 = Guard{Name: GuardAttribution, Result: GuardFail, Reason: ReasonAttributionMismatch}
	wantRefused(t, ExecuteReady(e, readyRequest()), ReasonAttributionMismatch, "d5")
	// A second attempt applies D5 again: nothing is cached between attempts.
	e = newEdge()
	ExecuteReady(e, readyRequest())
	ExecuteReady(e, readyRequest())
	n := 0
	for _, l := range e.log {
		if l == "d5" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("D5 ran %d times for two attempts: %v", n, e.log)
	}
}

func TestAnUnreadablePullRequestRefusesWithoutAWrite(t *testing.T) {
	e := newEdge()
	e.readErrs = []error{errors.New("boom")}
	wantRefused(t, ExecuteReady(e, readyRequest()), ReasonPlanStale, "pr-unreadable")
	if reflect.DeepEqual(e.log, nil) || len(e.marked) != 0 {
		t.Fatal(e.log)
	}
}

func TestOnlyAnOpenUnmergedPullRequestInTheSameRepositoryAtTheReceiptIsReadied(t *testing.T) {
	cases := map[string]struct {
		mutate func(*PullRequestState)
		reason Reason
		detail string
	}{
		"closed":             {func(p *PullRequestState) { p.State = "closed" }, ReasonPlanStale, "pr-not-open"},
		"merged":             {func(p *PullRequestState) { p.State = "closed"; p.Merged = true }, ReasonPlanStale, "pr-not-open"},
		"state unknown":      {func(p *PullRequestState) { p.State = "" }, ReasonPlanStale, "pr-not-open"},
		"merged but open":    {func(p *PullRequestState) { p.Merged = true }, ReasonPlanStale, "pr-not-open"},
		"another number":     {func(p *PullRequestState) { p.Number = 8 }, ReasonPlanStale, "pr-number"},
		"fork head":          {func(p *PullRequestState) { p.HeadRepository = "someone/project" }, ReasonNotOwned, "repository"},
		"head repo unknown":  {func(p *PullRequestState) { p.HeadRepository = "" }, ReasonNotOwned, "repository"},
		"other base repo":    {func(p *PullRequestState) { p.BaseRepository = "owner/other" }, ReasonNotOwned, "repository"},
		"repo in wrong case": {func(p *PullRequestState) { p.BaseRepository = "Owner/project" }, ReasonNotOwned, "repository"},
		"head moved":         {func(p *PullRequestState) { p.HeadSHA = sha('9') }, ReasonHeadChanged, "head"},
		"head empty":         {func(p *PullRequestState) { p.HeadSHA = "" }, ReasonHeadChanged, "head"},
		"base moved":         {func(p *PullRequestState) { p.BaseSHA = sha('9') }, ReasonHeadChanged, "base"},
		"base empty":         {func(p *PullRequestState) { p.BaseSHA = "" }, ReasonHeadChanged, "base"},
		"head upper case":    {func(p *PullRequestState) { p.HeadSHA = strings.ToUpper(sha('a')) }, ReasonHeadChanged, "head"},
	}
	for name, tc := range cases {
		e := newEdge()
		pr := draftPR()
		tc.mutate(&pr)
		e.reads = []PullRequestState{pr}
		r := ExecuteReady(e, readyRequest())
		if len(e.marked) != 0 || len(e.intents) != 0 {
			t.Errorf("%s: wrote: %v", name, e.log)
		}
		if r.State != StateRefused || r.Reason == nil || *r.Reason != tc.reason || r.Detail != tc.detail || r.Attempted {
			t.Errorf("%s: %+v reason %v", name, r, r.Reason)
		}
	}
}

func TestAnAlreadyReadyPullRequestIsProvenCompleteWithoutAMutation(t *testing.T) {
	e := newEdge()
	e.reads = []PullRequestState{readyPR()}
	r := ExecuteReady(e, readyRequest())
	if r.State != StateCompleted || !r.AlreadyReady || r.Attempted || r.Status() != StatusComplete {
		t.Fatalf("%+v", r)
	}
	if len(e.marked) != 0 || len(e.intents) != 0 || !reflect.DeepEqual(e.log, []string{"d5", "read"}) {
		t.Fatalf("%v", e.log)
	}
	// Already Ready is still a state of the same PR at the receipt: a moved head is refused first.
	e = newEdge()
	moved := readyPR()
	moved.HeadSHA = sha('9')
	e.reads = []PullRequestState{moved}
	wantRefused(t, ExecuteReady(e, readyRequest()), ReasonHeadChanged, "head")
	// A closed PR that is not a Draft is not a proof either.
	e = newEdge()
	closed := readyPR()
	closed.State = "closed"
	e.reads = []PullRequestState{closed}
	wantRefused(t, ExecuteReady(e, readyRequest()), ReasonPlanStale, "pr-not-open")
}

func TestTheReceiptMustBeReadyAndWellFormed(t *testing.T) {
	e := newEdge()
	in := goodReady()
	in.Review = nil
	req := readyRequest()
	req.Receipt = EvaluateReady(in)
	wantRefused(t, ExecuteReady(e, req), ReasonPlanStale, "receipt-not-ready")
	if len(e.log) != 0 {
		t.Fatalf("nothing is read or evaluated for a refused receipt: %v", e.log)
	}
	for name, mutate := range map[string]func(*ReadyRequest){
		"repository empty":    func(r *ReadyRequest) { r.Repository = "" },
		"repository no slash": func(r *ReadyRequest) { r.Repository = "project" },
		"number zero":         func(r *ReadyRequest) { r.Number = 0 },
		"number negative":     func(r *ReadyRequest) { r.Number = -1 },
	} {
		e := newEdge()
		req := readyRequest()
		mutate(&req)
		r := ExecuteReady(e, req)
		if r.State != StateRefused || len(e.log) != 0 {
			t.Errorf("%s: %+v %v", name, r, e.log)
		}
	}
	if r := ExecuteReady(nil, readyRequest()); r.State != StateRefused {
		t.Fatalf("a nil edge: %+v", r)
	}
}

func TestTheFreshEvaluationBlocksTheWrite(t *testing.T) {
	e := newEdge()
	e.evidence.Review.Verdict = VerdictNotClear
	r := ExecuteReady(e, readyRequest())
	wantRefused(t, r, ReasonReviewMissing, "review-not-clear")
	if len(r.Blockers) != 1 || len(e.intents) != 0 || len(e.marked) != 0 {
		t.Fatalf("%+v %v", r, e.log)
	}
	// Evidence for another head than the fresh read is stale: the head of the read, not the
	// caller's, is what is evaluated.
	e = newEdge()
	e.evidence.Head = sha('9')
	e.evidence.Review.SubjectSHA = sha('9')
	wantRefused(t, ExecuteReady(e, readyRequest()), ReasonReviewMissing, "review-wrong-head")
	// An evidence read that fails refuses.
	e = newEdge()
	e.evErr = errors.New("rate limited")
	wantRefused(t, ExecuteReady(e, readyRequest()), ReasonPlanStale, "evidence-unreadable")
	// A failing attribution guard inside the evidence still refuses: the edge's D5 pass cannot hide it.
	e = newEdge()
	e.evidence.Guards = append(append([]Guard{}, e.evidence.Guards...), Guard{Name: GuardAttribution, Result: GuardFail, Reason: ReasonAttributionMismatch})
	wantRefused(t, ExecuteReady(e, readyRequest()), ReasonAttributionMismatch, "guard")
	// The edge's own D5 pass supplies the attribution guard: evidence without one still passes.
	e = newEdge()
	e.evidence.Guards = []Guard{{Name: GuardOwnership, Result: GuardPass}}
	if r := ExecuteReady(e, readyRequest()); r.State != StateCompleted {
		t.Fatalf("%+v", r)
	}
	// An empty PR is refused by the gate itself.
	e = newEdge()
	empty := draftPR()
	empty.BaseSHA = empty.HeadSHA
	e.reads = []PullRequestState{empty}
	e.evidence.Checks.ObservedBase, e.evidence.Rebase.EvaluatedBase = empty.HeadSHA, empty.HeadSHA
	req := readyRequest()
	req.Receipt = ReadyReceipt{Head: empty.HeadSHA, BaseSHA: empty.HeadSHA}
	wantRefused(t, ExecuteReady(e, req), ReasonScopeNotAccepted, "head-equals-base")
}

func TestAMissingOrMalformedNodeIdRefusesBeforeAnyIntent(t *testing.T) {
	for _, id := range []string{"", "x", "PR kwDO", "PR_kw\nDO", `PR_"}){x`, "PR_kw{DO}", "PR_kw$(id)", "PR_kw;DO", "PR/kw", strings.Repeat("A", 129), "PR_kwéDO", "PR_kw DO\t"} {
		e := newEdge()
		pr := draftPR()
		pr.NodeID = id
		e.reads = []PullRequestState{pr}
		r := ExecuteReady(e, readyRequest())
		wantRefused(t, r, ReasonCapabilityUnavailable, "node-id-invalid")
		if len(e.intents) != 0 || len(e.marked) != 0 {
			t.Errorf("%q: %v", id, e.log)
		}
	}
	for _, id := range []string{goodNodeID, "MDExOlB1bGxSZXF1ZXN0MQ==", strings.Repeat("A", 128), "PR_kwDO-ab_9"} {
		e := newEdge()
		pr := draftPR()
		pr.NodeID = id
		e.reads = []PullRequestState{pr, readyPR()}
		if r := ExecuteReady(e, readyRequest()); r.State != StateCompleted {
			t.Errorf("%q: %+v", id, r)
		}
	}
}

func TestNoWriteWhenTheIntentCannotBeMadeDurable(t *testing.T) {
	e := newEdge()
	e.intentErr = errors.New("disk full")
	r := ExecuteReady(e, readyRequest())
	wantRefused(t, r, ReasonJournalFailed, "intent-not-durable")
	if len(e.marked) != 0 || reflect.DeepEqual(e.log[len(e.log)-1], "mark") {
		t.Fatal(e.log)
	}
}

func TestALostResponseIsUnknownWithAFreshReread(t *testing.T) {
	e := newEdge()
	e.markErr = errors.New("connection reset")
	e.reads = []PullRequestState{draftPR(), readyPR()}
	r := ExecuteReady(e, readyRequest())
	if r.State != StateUnknown || !r.Attempted || r.Reason == nil || *r.Reason != ReasonOutcomeUnknown || r.Status() != StatusUnknown || ExitCode(r.Status()) != 1 {
		t.Fatalf("%+v", r)
	}
	if e.log[len(e.log)-1] != "read" || r.Observed == nil || r.Observed.Draft {
		t.Fatalf("a fresh re-read follows the lost response: %v %+v", e.log, r.Observed)
	}
	if n := len(e.marked); n != 1 {
		t.Fatalf("one attempt, never an automatic retry: %d", n)
	}
	// The re-read may itself fail: the outcome stays unknown, with nothing observed.
	e = newEdge()
	e.markErr = errors.New("timeout")
	e.reads = []PullRequestState{draftPR(), {}}
	e.readErrs = []error{nil, errors.New("down")}
	r = ExecuteReady(e, readyRequest())
	if r.State != StateUnknown || r.Observed != nil || !r.Attempted {
		t.Fatalf("%+v", r)
	}
}

func TestASuccessReplyIsOnlyCompleteWhenTheRereadProvesIt(t *testing.T) {
	cases := map[string]struct {
		reread PullRequestState
		err    error
	}{
		"still a draft":     {draftPR(), nil},
		"reread fails":      {PullRequestState{}, errors.New("down")},
		"head moved":        {func() PullRequestState { p := readyPR(); p.HeadSHA = sha('9'); return p }(), nil},
		"base moved":        {func() PullRequestState { p := readyPR(); p.BaseSHA = sha('9'); return p }(), nil},
		"closed meanwhile":  {func() PullRequestState { p := readyPR(); p.State = "closed"; return p }(), nil},
		"merged meanwhile":  {func() PullRequestState { p := readyPR(); p.Merged = true; return p }(), nil},
		"other repo":        {func() PullRequestState { p := readyPR(); p.BaseRepository = "x/y"; return p }(), nil},
		"another pr number": {func() PullRequestState { p := readyPR(); p.Number = 9; return p }(), nil},
	}
	for name, tc := range cases {
		e := newEdge()
		e.reads = []PullRequestState{draftPR(), tc.reread}
		e.readErrs = []error{nil, tc.err}
		r := ExecuteReady(e, readyRequest())
		if r.State != StateUnknown || !r.Attempted || r.Reason == nil || *r.Reason != ReasonOutcomeUnknown {
			t.Errorf("%s: %+v", name, r)
		}
	}
}

func TestADifferentExecutorDoesNotChangeTheRequest(t *testing.T) {
	// The request is a value: running twice with the same request gives the same intent.
	e := newEdge()
	req := readyRequest()
	ExecuteReady(e, req)
	e2 := newEdge()
	ExecuteReady(e2, req)
	if !reflect.DeepEqual(e.intents, e2.intents) {
		t.Fatal("intents differ")
	}
}
