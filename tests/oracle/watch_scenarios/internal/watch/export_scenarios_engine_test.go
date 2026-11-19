// SPDX-License-Identifier: EUPL-1.2
package watch

// Scenario engine of the exporter: runs a scripted sequence of cycles through the real Go watcher (the rig of
// rig_test.go) and records inputs and outputs in the format-independent scenario format (FORMAT.md). The
// expectations are never typed by hand: they are what the Go code produced. The `want` calls only guard the
// translation of a Go test into a scenario (the export fails when a scenario does not show what the test asserts).

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/watchexport"
)

// ---- JSON views ----

type xRun struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Conclude string `json:"conclusion"`
	Title    string `json:"title"`
	Summary  string `json:"summary"`
	Text     string `json:"text"`
}

type xStat struct {
	Context     string `json:"context"`
	State       string `json:"state"`
	Description string `json:"description"`
}

type xChecks struct {
	Runs     []xRun  `json:"runs"`
	Statuses []xStat `json:"statuses"`
}

type xDetail struct {
	Mergeable *bool  `json:"mergeable"`
	State     string `json:"state"`
}

type xPR struct {
	Number    int      `json:"number"`
	Title     string   `json:"title"`
	Author    string   `json:"author"`
	Labels    []string `json:"labels"`
	Draft     bool     `json:"draft"`
	Head      string   `json:"head"`
	HeadRef   string   `json:"head_ref"`
	Base      string   `json:"base"`
	Fork      bool     `json:"fork"`
	ForkKnown bool     `json:"fork_known"`
	Detail    *xDetail `json:"detail"`
	Checks    *xChecks `json:"checks"`
}

type xGone struct {
	Merged      bool    `json:"merged"`
	MergeCommit *string `json:"merge_commit"`
	MergedAt    string  `json:"merged_at"`
	Base        string  `json:"base"`
	Head        string  `json:"head"`
	HeadRef     string  `json:"head_ref"`
	MethodHint  string  `json:"method_hint"`
}

type xErr struct {
	Code           string `json:"code"`
	Message        string `json:"message"`
	RunWide        bool   `json:"run_wide"`
	RetryInSeconds int    `json:"retry_in_seconds"`
}

// xObs is the observation of one repository in one cycle: either an error or a snapshot.
type xObs struct {
	Error         *xErr            `json:"error"`
	DefaultBranch string           `json:"default_branch"`
	PRs           []xPR            `json:"prs"`
	Gone          map[string]xGone `json:"gone"`
}

func xObsOf(s snap) xObs {
	if s.err != nil {
		var oe *ObserveError
		if errors.As(s.err, &oe) {
			return xObs{Error: &xErr{Code: oe.Code, Message: oe.Message, RunWide: oe.RunWide, RetryInSeconds: int(oe.RetryIn / time.Second)}, PRs: []xPR{}, Gone: map[string]xGone{}}
		}
		return xObs{Error: &xErr{Code: "network", Message: s.err.Error()}, PRs: []xPR{}, Gone: map[string]xGone{}}
	}
	o := xObs{DefaultBranch: s.obs.DefaultBranch, PRs: []xPR{}, Gone: map[string]xGone{}}
	for _, p := range s.obs.PRs {
		xp := xPR{Number: p.Number, Title: p.Title, Author: p.Author, Labels: nonNil(p.Labels), Draft: p.Draft, Head: p.Head, HeadRef: p.HeadRef, Base: p.Base,
			Fork: p.Fork, ForkKnown: p.ForkKnown}
		if p.Detail != nil {
			xp.Detail = &xDetail{Mergeable: p.Detail.Mergeable, State: p.Detail.State}
		}
		if p.Checks != nil {
			c := &xChecks{Runs: []xRun{}, Statuses: []xStat{}}
			for _, r := range p.Checks.Runs {
				c.Runs = append(c.Runs, xRun{Name: r.Name, Status: r.Status, Conclude: r.Conclusion, Title: r.Title, Summary: r.Summary, Text: r.Text})
			}
			for _, st := range p.Checks.Statuses {
				c.Statuses = append(c.Statuses, xStat{Context: st.Context, State: st.State, Description: st.Description})
			}
			xp.Checks = c
		}
		o.PRs = append(o.PRs, xp)
	}
	for n, g := range s.obs.Gone {
		o.Gone[strconv.Itoa(n)] = xGone{Merged: g.Merged, MergeCommit: g.MergeCommit, MergedAt: g.MergedAt, Base: g.Base, Head: g.Head, HeadRef: g.HeadRef, MethodHint: g.MethodHint}
	}
	return o
}

type xGoRef struct {
	ID  string `json:"id"`
	Seq int64  `json:"seq"`
}

type xEvent struct {
	Ordinal int     `json:"ordinal"`
	Step    int     `json:"step"`
	Time    string  `json:"time"`
	Kind    string  `json:"kind"`
	Repo    *string `json:"repo"`
	PR      *int    `json:"pr"`
	Details any     `json:"details"`
	Go      xGoRef  `json:"go"`
}

type xKnown struct {
	Number         int    `json:"number"`
	Head           string `json:"head"`
	Class          string `json:"class"`
	ClassCandidate string `json:"class_candidate"`
	DetailDue      bool   `json:"detail_due"`
}

type xHints struct {
	Cycle       int      `json:"cycle"`
	DetailEvery int      `json:"detail_every"`
	Known       []xKnown `json:"known"`
}

type xEpochs struct {
	Head      int `json:"head"`
	Checks    int `json:"checks"`
	Mergeable int `json:"mergeable"`
	Ready     int `json:"ready"`
	Base      int `json:"base"`
}

type xMergeCand struct {
	Mergeable bool   `json:"mergeable"`
	Behind    bool   `json:"behind"`
	State     string `json:"state"`
}

type xPRState struct {
	Number         int         `json:"number"`
	Title          string      `json:"title"`
	Author         string      `json:"author"`
	Labels         []string    `json:"labels"`
	Draft          bool        `json:"draft"`
	Head           string      `json:"head"`
	HeadRef        string      `json:"head_ref"`
	Base           string      `json:"base"`
	Fork           bool        `json:"fork"`
	Generation     int         `json:"generation"`
	Epochs         xEpochs     `json:"epochs"`
	Class          string      `json:"class"`
	ClassCandidate string      `json:"class_candidate"`
	PendingSince   string      `json:"pending_since"`
	PendingNames   []string    `json:"pending_names"`
	TimeoutFired   bool        `json:"timeout_fired"`
	Mergeable      *bool       `json:"mergeable"`
	MergeableState string      `json:"mergeable_state"`
	Conflicting    bool        `json:"conflicting"`
	Behind         bool        `json:"behind"`
	MergeCandidate *xMergeCand `json:"mergeable_candidate"`
	DetailDue      bool        `json:"detail_due"`
	Gone           bool        `json:"gone"`
}

type xClosed struct {
	Number     int     `json:"number"`
	Generation int     `json:"generation"`
	Epochs     xEpochs `json:"epochs"`
}

type xDue struct {
	MergedPR    int     `json:"merged_pr"`
	MergeCommit *string `json:"merge_commit"`
	OldParent   string  `json:"old_parent"`
	Base        string  `json:"base"`
	HeadRef     string  `json:"head_ref"`
	HeadFork    bool    `json:"head_fork"`
}

type xDone struct {
	Child       int    `json:"child"`
	MergedPR    int    `json:"merged_pr"`
	MergeCommit string `json:"merge_commit"`
}

type xHealth struct {
	Failures     int    `json:"failures"`
	Episode      int    `json:"episode"`
	ErrorOpen    bool   `json:"error_open"`
	LastCode     string `json:"last_code"`
	Stalled      bool   `json:"stalled"`
	StallEpisode int    `json:"stall_episode"`
	NextTry      string `json:"next_try"`
}

type xRepoState struct {
	Baselined     bool                 `json:"baselined"`
	Since         string               `json:"since"`
	LastOK        string               `json:"last_ok"`
	DefaultBranch string               `json:"default_branch"`
	PRs           map[string]*xPRState `json:"prs"`
	Closed        []xClosed            `json:"closed"`
	RestackDue    []xDue               `json:"restack_due"`
	RestackDone   []xDone              `json:"restack_done"`
	Health        xHealth              `json:"health"`
}

type xState struct {
	Cycle           int                    `json:"cycle"`
	ReposEpoch      int                    `json:"repos_epoch"`
	IntervalSeconds int                    `json:"interval_seconds"`
	Repos           []string               `json:"repos"`
	Skipped         []Skipped              `json:"skipped"`
	Repo            map[string]*xRepoState `json:"repo"`
	RunHealth       xHealth                `json:"run_health"`
}

func xHealthOf(h *Health) xHealth {
	if h == nil {
		return xHealth{}
	}
	return xHealth{Failures: h.Failures, Episode: h.Episode, ErrorOpen: h.ErrOpen, LastCode: h.LastCode, Stalled: h.Stalled, StallEpisode: h.StallEpisode, NextTry: h.NextTry}
}

func xEpochsOf(e Epochs) xEpochs {
	return xEpochs{Head: e.Head, Checks: e.Checks, Mergeable: e.Mergeable, Ready: e.Ready, Base: e.Base}
}

// xStateOf is the semantic view of a state: no file layout, no ids, no conditional-request store.
func xStateOf(s *State) *xState {
	v := &xState{Cycle: s.Cycle, ReposEpoch: s.ReposEpoch, IntervalSeconds: s.IntervalSeconds, Repos: nonNil(s.Repos), Skipped: append([]Skipped{}, s.Skipped...),
		Repo: map[string]*xRepoState{}, RunHealth: xHealthOf(s.Health[""])}
	for name, rs := range s.Repo {
		r := &xRepoState{Baselined: rs.Baselined, Since: rs.Since, LastOK: rs.LastOK, DefaultBranch: rs.DefaultBranch, PRs: map[string]*xPRState{},
			Closed: []xClosed{}, RestackDue: []xDue{}, RestackDone: []xDone{}, Health: xHealthOf(s.Health[name])}
		for n, p := range rs.PRs {
			x := &xPRState{Number: p.Number, Title: p.Title, Author: p.Author, Labels: nonNil(p.Labels), Draft: p.Draft, Head: p.Head, HeadRef: p.HeadRef, Base: p.Base,
				Fork: p.Fork, Generation: p.Generation, Epochs: xEpochsOf(p.Epochs), Class: p.Class, ClassCandidate: p.ClassCand, PendingSince: p.PendingSince,
				PendingNames: nonNil(p.PendingNames), TimeoutFired: p.TimeoutFired, Mergeable: p.Mergeable, MergeableState: p.MState, Conflicting: p.Conflicting,
				Behind: p.Behind, DetailDue: p.DetailDue, Gone: p.Gone}
			if p.MergeCand != nil {
				x.MergeCandidate = &xMergeCand{Mergeable: p.MergeCand.Mergeable, Behind: p.MergeCand.Behind, State: p.MergeCand.State}
			}
			r.PRs[strconv.Itoa(n)] = x
		}
		for _, c := range rs.Closed {
			r.Closed = append(r.Closed, xClosed{Number: c.Number, Generation: c.Generation, Epochs: xEpochsOf(c.Epochs)})
		}
		sort.Slice(r.Closed, func(i, j int) bool { return r.Closed[i].Number < r.Closed[j].Number })
		for _, d := range rs.RestackDue {
			r.RestackDue = append(r.RestackDue, xDue{MergedPR: d.MergedPR, MergeCommit: d.MergeCommit, OldParent: d.OldParent, Base: d.Base, HeadRef: d.HeadRef, HeadFork: d.HeadFork})
		}
		for _, id := range rs.RestackDone {
			// id: repo#child:restack-needed:<merged pr>:<merge commit|unknown>
			i := strings.Index(id, "#")
			parts := strings.Split(id[i+1:], ":")
			child, _ := strconv.Atoi(parts[0])
			merged, _ := strconv.Atoi(parts[2])
			r.RestackDone = append(r.RestackDone, xDone{Child: child, MergedPR: merged, MergeCommit: parts[3]})
		}
		sort.Slice(r.RestackDone, func(i, j int) bool {
			a, b := r.RestackDone[i], r.RestackDone[j]
			if a.MergedPR != b.MergedPR {
				return a.MergedPR < b.MergedPR
			}
			return a.Child < b.Child
		})
		v.Repo[name] = r
	}
	return v
}

// ---- scripted steps ----

type xcrash struct {
	Point string `json:"point"`
	N     int    `json:"n"`
}

type xcand struct {
	PR     int      `json:"pr"`
	Reason string   `json:"reason"`
	Branch string   `json:"branch"`
	Argv   []string `json:"argv"`
}

type xstep struct {
	action   string // cycle (default) or open
	adv      *time.Duration
	obs      map[string]snap
	repos    []string
	skipped  *[]Skipped
	baseline *bool
	restart  bool
	fileOp   string // delete-state, corrupt-state
	crash    *xcrash
	note     string
	want     []string // kinds the step must have journalled, in order (translation guard)
	guard    bool     // check want (an empty want then means: nothing journalled)
}

type xsc struct {
	name, desc string
	goTests    []string
	repos      []string
	skipped    []Skipped
	timeout    time.Duration
	timeoutSet bool
	ignoreA    []string
	ignoreL    []string
	patterns   []string
	byMerge    map[int][]RestackCandidate
	fuzzRule   bool
	steps      []xstep
	status     []time.Duration // offsets from the last heartbeat, read after the last step with the watcher closed
	ignoreView []string
	lean       bool // no per-step state view and hints (long scenarios)
	tailIdx    int
	tail       *xstep                                 // repeated after the last step until `until` holds
	until      func(step []xEvent, all []xEvent) bool // evaluated after each step while tail is set
	total      map[string]int                         // journalled events per kind that the Go test asserts at the end
}

func newXSC(name, desc string, goTests ...string) *xsc {
	return &xsc{name: name, desc: desc, goTests: goTests, repos: []string{"o/r"}, byMerge: map[int][]RestackCandidate{}}
}

// repeatUntil appends st and repeats it right after itself until pred holds (pred sees the events of the last repetition and all so far).
func (s *xsc) repeatUntil(st xstep, pred func(step []xEvent, all []xEvent) bool) *xsc {
	s.add(st)
	s.tail, s.tailIdx, s.until = &st, len(s.steps)-1, pred
	return s
}

func dur(d time.Duration) *time.Duration { return &d }

func (s *xsc) add(st xstep) *xsc {
	s.steps = append(s.steps, st)
	return s
}

// cyc appends a cycle of the single repository o/r (one minute after the previous one) that must journal exactly the kinds.
func (s *xsc) cyc(o snap, kinds ...string) *xsc {
	st := xstep{obs: map[string]snap{"o/r": o}, want: kinds, guard: true}
	return s.add(st)
}

// cycAfter is cyc with an explicit pause before the cycle.
func (s *xsc) cycAfter(d time.Duration, o snap, kinds ...string) *xsc {
	st := xstep{adv: dur(d), obs: map[string]snap{"o/r": o}, want: kinds, guard: true}
	return s.add(st)
}

// any appends a cycle without a guard on its events.
func (s *xsc) any(o snap) *xsc {
	return s.add(xstep{obs: map[string]snap{"o/r": o}})
}

func (s *xsc) times(n int, o snap) *xsc {
	for i := 0; i < n; i++ {
		s.any(o)
	}
	return s
}

type xcycleRec struct {
	Step        int             `json:"step"`
	Action      string          `json:"action"`
	Time        string          `json:"time"`
	Note        string          `json:"note"`
	Config      map[string]any  `json:"config_change"`
	Restart     bool            `json:"restart_before"`
	FileOp      string          `json:"file_op"`
	Crash       *xcrash         `json:"crash"`
	Observation map[string]xObs `json:"observation"`
}

type xexpect struct {
	Step       int               `json:"step"`
	Result     string            `json:"result"`
	ErrorKind  string            `json:"error_kind"`
	RetryIn    int               `json:"retry_in_seconds"`
	Events     []xEvent          `json:"events"`
	Printed    []int             `json:"printed"`
	Hints      map[string]xHints `json:"hints"`
	StateAfter *xState           `json:"state_after"`
	GoMessage  string            `json:"go_message"`
}

type xprobe struct {
	OffsetSeconds int `json:"offset_seconds"`
	Expected      any `json:"expected"`
}

type xStatusView struct {
	Fresh           bool          `json:"fresh"`
	HeartbeatAge    *int          `json:"heartbeat_age_seconds"`
	IntervalSeconds int           `json:"interval_seconds"`
	LockHeld        bool          `json:"lock_held"`
	Repos           []xStatusRepo `json:"repos"`
}

type xStatusRepo struct {
	Repo      string  `json:"repo"`
	AgeSecs   *int    `json:"last_ok_age_seconds"`
	Failures  int     `json:"failures"`
	OpenError *string `json:"open_error"`
}

type xConfig struct {
	Repos                 []string  `json:"repos"`
	Skipped               []Skipped `json:"skipped"`
	Host                  string    `json:"host"`
	StartTime             string    `json:"start_time"`
	IntervalSeconds       int       `json:"interval_seconds"`
	PendingTimeoutSeconds int       `json:"pending_timeout_seconds"`
	DetailEvery           int       `json:"detail_every"`
	IgnoreAuthors         []string  `json:"ignore_authors"`
	IgnoreLabels          []string  `json:"ignore_labels"`
	StackPatterns         []string  `json:"stack_patterns"`
	Mode                  string    `json:"mode"`
}

type xDoc struct {
	watchexport.Common
	Config               xConfig     `json:"config"`
	Restacker            any         `json:"restacker"`
	Cycles               []xcycleRec `json:"cycles"`
	ExpectedEvents       []xexpect   `json:"expected_events"`
	ExpectedStateView    *xState     `json:"expected_state_view"`
	StateViewIgnore      []string    `json:"state_view_ignore"`
	ExpectedJournal      []int       `json:"expected_journal_ordinals"`
	ExpectUniqueEventIDs bool        `json:"expect_unique_event_ids"`
	StatusProbes         []xprobe    `json:"status_probes"`
}

type xRestackRuleFuzz struct {
	Rule   string   `json:"rule"`
	Reason string   `json:"reason"`
	Argv   []string `json:"argv"`
}

func (s *xsc) run(t *testing.T, dir string) {
	t.Helper()
	doc := s.exec(t)
	if err := watchexport.Write(dir, s.name, doc); err != nil {
		t.Fatal(err)
	}
}

func (s *xsc) exec(t *testing.T) *xDoc {
	t.Helper()
	r := newRig(t, s.repos...)
	r.skipped = s.skipped
	r.ignoreA, r.ignoreL = s.ignoreA, s.ignoreL
	if s.timeoutSet {
		r.timeout = s.timeout
	}
	for k, v := range s.byMerge {
		r.rs.byMerge[k] = v
	}
	open := func() error {
		if r.w != nil {
			r.w.Close()
			r.w = nil
		}
		o := r.options()
		o.StackPatterns = s.patterns
		if s.fuzzRule {
			o.Restacker = fuzzRestacker{}
		}
		w, err := Open(o)
		if err != nil {
			return err
		}
		r.w = w
		return nil
	}
	doc := &xDoc{Common: watchexport.NewCommon(s.name, watchexport.KindCycles, s.desc, s.goTests...), ExpectUniqueEventIDs: true}
	doc.Common.FormatDependent = []string{"go"}
	doc.ExpectedEvents = []xexpect{}
	doc.Cycles = []xcycleRec{}
	doc.StateViewIgnore = append([]string{}, s.ignoreView...)
	doc.Config = xConfig{Repos: append([]string{}, s.repos...), Skipped: append([]Skipped{}, s.skipped...), Host: "github.com", StartTime: rigStart.Format(time.RFC3339),
		IntervalSeconds: 60, PendingTimeoutSeconds: int(r.timeout / time.Second), IgnoreAuthors: nonNil(s.ignoreA), IgnoreLabels: nonNil(s.ignoreL),
		StackPatterns: nonNil(s.patterns), Mode: "once"}
	switch {
	case s.fuzzRule:
		doc.Restacker = xRestackRuleFuzz{Rule: "every-open-pr-above-merged", Reason: ReasonContains, Argv: []string{"git", "rebase"}}
	default:
		m := map[string][]xcand{}
		for k, cs := range s.byMerge {
			for _, c := range cs {
				m[strconv.Itoa(k)] = append(m[strconv.Itoa(k)], xcand{PR: c.PR, Reason: c.Reason, Branch: c.Branch, Argv: c.Argv})
			}
		}
		doc.Restacker = map[string]any{"rule": "by-merged-pr", "candidates": m}
	}
	crashed := false
	outSeen := 0
	var allEvents []xEvent
	for i := 0; i < len(s.steps); i++ {
		st := s.steps[i]
		n := i + 1
		rec := xcycleRec{Step: n, Action: orStr(st.action, "cycle"), Note: st.note, Observation: map[string]xObs{}, FileOp: st.fileOp, Crash: st.crash}
		cfg := map[string]any{}
		if st.fileOp != "" {
			if r.w != nil {
				r.w.Close()
				r.w = nil
			}
			p := filepath.Join(r.dir, "state.json")
			switch st.fileOp {
			case "delete-state":
				os.Remove(p)
			case "corrupt-state":
				os.WriteFile(p, []byte("{broken"), 0o600)
			default:
				t.Fatalf("file op %q", st.fileOp)
			}
		}
		restart := st.restart
		if st.repos != nil {
			r.repos = st.repos
			cfg["repos"] = st.repos
			restart = true
		}
		if st.skipped != nil {
			r.skipped = *st.skipped
			cfg["skipped"] = *st.skipped
			restart = true
		}
		if st.baseline != nil {
			r.base = *st.baseline
			cfg["baseline"] = *st.baseline
		}
		if len(cfg) > 0 {
			rec.Config = cfg
		}
		if crashed {
			restart = true // a crash is a restart before whatever comes next
			crashed = false
		}
		rec.Restart = restart
		if restart && r.w != nil {
			r.w.Close()
			r.w = nil
		}
		if st.crash != nil {
			r.faults = crashAt(st.crash.Point, st.crash.N)
		}
		for repo, sn := range st.obs {
			r.set(repo, sn)
		}
		r.obs.hints = nil
		jBefore := len(r.journal())
		exp := xexpect{Step: n, Events: []xEvent{}, Printed: []int{}}
		var err error
		if rec.Action == "open" {
			if r.w == nil {
				err = open()
			}
			rec.Time = r.now.Format(time.RFC3339)
		} else {
			if st.adv != nil {
				r.now = r.now.Add(*st.adv)
			} else {
				r.now = r.now.Add(time.Minute)
			}
			rec.Time = r.now.Format(time.RFC3339)
			if r.w == nil {
				err = open()
			}
			if err == nil {
				var res CycleResult
				res, err = r.w.Cycle(context.Background())
				exp.RetryIn = int(res.RetryIn / time.Second)
			}
		}
		// the observation the cycle saw: every configured repository, an absent answer is the rig's "no fixture" failure
		for _, repo := range r.repos {
			if sn, ok := r.obs.cur[repo]; ok {
				rec.Observation[repo] = xObsOf(sn)
			} else {
				rec.Observation[repo] = xObsOf(failWith("network"))
				o := rec.Observation[repo]
				o.Error.Message = "no fixture"
				rec.Observation[repo] = o
			}
		}
		switch {
		case err == nil:
			exp.Result = "ok"
		case errors.Is(err, errCrash):
			exp.Result = "crashed"
			r.crashed()
			crashed = true
		case strings.Contains(err.Error(), "corrupt"):
			exp.Result, exp.ErrorKind, exp.GoMessage = "error", "corrupt-state", err.Error()
		default:
			exp.Result, exp.ErrorKind, exp.GoMessage = "error", "other", err.Error()
		}
		if st.crash != nil && exp.Result != "crashed" {
			t.Fatalf("%s step %d: the planned crash %v did not happen: %v", s.name, n, st.crash, err)
		}
		if st.crash != nil {
			// crashed() cleared the faults
		} else if r.faults != nil {
			r.faults = nil
		}
		journal := r.journal()
		byID := map[string]int{}
		for _, e := range journal {
			byID[e.ID] = int(e.Seq)
		}
		for _, e := range journal[jBefore:] {
			exp.Events = append(exp.Events, xEvent{Ordinal: int(e.Seq), Step: n, Time: e.Time, Kind: e.Kind, Repo: e.Repo, PR: e.PR, Details: e.Details, Go: xGoRef{ID: e.ID, Seq: e.Seq}})
		}
		if st.guard {
			var got []string
			for _, e := range exp.Events {
				got = append(got, e.Kind)
			}
			if strings.Join(got, ",") != strings.Join(st.want, ",") {
				t.Fatalf("%s step %d: translation guard: journalled %v, the Go test says %v", s.name, n, got, st.want)
			}
		}
		outLines := nonEmptyLines(r.out.String())
		for _, l := range outLines[outSeen:] {
			var e Event
			if jerr := e.UnmarshalJSON([]byte(l)); jerr != nil {
				t.Fatal(jerr)
			}
			exp.Printed = append(exp.Printed, byID[e.ID])
		}
		outSeen = len(outLines)
		if len(r.obs.hints) > 0 {
			exp.Hints = map[string]xHints{}
			for repo, h := range r.obs.hints {
				xh := xHints{Cycle: h.Cycle, DetailEvery: h.DetailEvery, Known: []xKnown{}}
				for _, k := range h.Known {
					xh.Known = append(xh.Known, xKnown{Number: k.Number, Head: k.Head, Class: k.Class, ClassCandidate: k.ClassCand, DetailDue: k.DetailDue})
				}
				exp.Hints[repo] = xh
			}
		}
		if exp.Result == "ok" && !s.lean {
			if s, lerr := loadState(r.dir); lerr == nil && s != nil {
				exp.StateAfter = xStateOf(s)
			}
		}
		if s.lean {
			exp.Hints = nil
		}
		allEvents = append(allEvents, exp.Events...)
		if s.tail != nil && i == s.tailIdx && !s.until(exp.Events, allEvents) {
			if len(s.steps) > 600 {
				t.Fatalf("%s: the stop condition was not reached", s.name)
			}
			s.steps = append(s.steps[:i+1], append([]xstep{*s.tail}, s.steps[i+1:]...)...)
			s.tailIdx = i + 1
		}
		doc.Cycles = append(doc.Cycles, rec)
		doc.ExpectedEvents = append(doc.ExpectedEvents, exp)
	}
	if r.w != nil {
		r.w.Close()
		r.w = nil
	}
	final, lerr := loadState(r.dir)
	if lerr != nil || final == nil {
		t.Fatalf("%s: no final state: %v", s.name, lerr)
	}
	doc.ExpectedStateView = xStateOf(final)
	ids := []string{}
	doc.ExpectedJournal = []int{}
	for _, e := range r.journal() {
		ids = append(ids, e.ID)
		doc.ExpectedJournal = append(doc.ExpectedJournal, int(e.Seq))
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("%s: duplicate event id %s", s.name, id)
		}
		seen[id] = true
	}
	doc.StatusProbes = []xprobe{}
	if len(s.status) > 0 {
		probe, err := ReadStatus(r.dir, r.now)
		if err != nil || probe.Heartbeat == nil {
			t.Fatalf("%s: status: %v", s.name, err)
		}
		hb, _ := time.Parse(time.RFC3339, probe.Heartbeat.Time)
		for _, off := range s.status {
			st, err := ReadStatus(r.dir, hb.Add(off))
			if err != nil {
				t.Fatal(err)
			}
			v := xStatusView{Fresh: st.Fresh, IntervalSeconds: st.IntervalSeconds, LockHeld: st.LockHeld, Repos: []xStatusRepo{}}
			if st.Heartbeat != nil {
				a := st.Heartbeat.AgeSeconds
				v.HeartbeatAge = &a
			}
			for _, rp := range st.Repos {
				v.Repos = append(v.Repos, xStatusRepo{Repo: rp.Repo, AgeSecs: rp.AgeSeconds, Failures: rp.Failures, OpenError: rp.OpenError})
			}
			doc.StatusProbes = append(doc.StatusProbes, xprobe{OffsetSeconds: int(off / time.Second), Expected: v})
		}
	}
	counts := map[string]int{}
	for _, e := range r.journal() {
		counts[e.Kind]++
	}
	for k, want := range s.total {
		if counts[k] != want {
			t.Fatalf("%s: %d journal lines of kind %s, the Go test says %d", s.name, counts[k], k, want)
		}
	}
	return doc
}

var _ = fmt.Sprint

func orStr(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}
