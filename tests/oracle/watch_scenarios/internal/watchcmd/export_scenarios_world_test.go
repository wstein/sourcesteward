// SPDX-License-Identifier: EUPL-1.2
package watchcmd

// The "world" is what the fake forge answers: the data model of the observer-session and run-loop scenarios.
// A scenario is built from a *fakeReader of the Go tests, serialised to JSON, and then rebuilt from that JSON
// for the actual run, so the file provably carries everything the run needs.

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/watch"
)

type wItem struct {
	Number  int      `json:"number"`
	Title   string   `json:"title"`
	User    string   `json:"user"`
	Labels  []string `json:"labels"`
	Draft   bool     `json:"draft"`
	HeadSHA string   `json:"head_sha"`
	HeadRef string   `json:"head_ref"`
	BaseRef string   `json:"base_ref"`
}

// wErr is a read failure: kind "watch" carries a hosting code, kind "plain" any other error.
type wErr struct {
	Kind              string `json:"kind"`
	Code              string `json:"code"`
	Message           string `json:"message"`
	RetryAfterSeconds int    `json:"retry_after_seconds"`
}

type wRate struct {
	Known           bool `json:"known"`
	Limit           int  `json:"limit"`
	Remaining       int  `json:"remaining"`
	ResetOffsetSecs int  `json:"reset_in_seconds"` // relative to the scenario clock at the start of the step
}

type wWorld struct {
	Items       []wItem           `json:"list_items"`
	ListError   *wErr             `json:"list_error"`
	Details     map[string]string `json:"pull_details"`
	DetailETags map[string]string `json:"pull_detail_etags"`
	Tips        map[string]string `json:"branch_tips"`
	CheckRuns   map[string]string `json:"check_runs"`
	Statuses    map[string]string `json:"commit_statuses"`
	Commits     map[string]string `json:"commits"`
	PullCommits map[string]string `json:"pull_commits"`
	Rate        *wRate            `json:"rate_limit"`
	CallErrors  map[string]wErr   `json:"call_errors"`
}

func wErrOf(err error) *wErr {
	if err == nil {
		return nil
	}
	var we *hosting.WatchError
	if errors.As(err, &we) {
		return &wErr{Kind: "watch", Code: we.Code, Message: we.Message, RetryAfterSeconds: int(we.RetryAfter / time.Second)}
	}
	return &wErr{Kind: "plain", Message: err.Error()}
}

func (e wErr) err() error {
	if e.Kind == "watch" {
		return &hosting.WatchError{Code: e.Code, Message: e.Message, RetryAfter: time.Duration(e.RetryAfterSeconds) * time.Second}
	}
	return errors.New(e.Message)
}

func strMap[V any](m map[int]V, f func(V) string) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		out[strconv.Itoa(k)] = f(v)
	}
	return out
}

func nonNilS(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// worldOf snapshots a fake reader.
func worldOf(f *fakeReader, now time.Time) wWorld {
	w := wWorld{Items: []wItem{}, Details: map[string]string{}, DetailETags: map[string]string{}, Tips: f.tips, CheckRuns: f.runs, Statuses: f.stats, Commits: f.commits,
		PullCommits: map[string]string{}, CallErrors: map[string]wErr{}}
	for _, it := range f.items {
		w.Items = append(w.Items, wItem{it.Number, it.Title, it.User, nonNilS(it.Labels), it.Draft, it.HeadSHA, it.HeadRef, it.BaseRef})
	}
	w.ListError = wErrOf(f.listErr)
	w.Details = strMap(f.details, func(s string) string { return s })
	w.DetailETags = strMap(f.detailET, func(s string) string { return s })
	w.PullCommits = strMap(f.pulls, func(s string) string { return s })
	if f.rate.Known {
		w.Rate = &wRate{Known: true, Limit: f.rate.Limit, Remaining: f.rate.Remaining, ResetOffsetSecs: int(f.rate.Reset.Sub(now) / time.Second)}
	}
	for k, e := range f.errAt {
		w.CallErrors[k] = *wErrOf(e)
	}
	// copies, so that later edits of the test fake do not alias into earlier steps
	w.Tips, w.CheckRuns, w.Statuses, w.Commits = cp(f.tips), cp(f.runs), cp(f.stats), cp(f.commits)
	return w
}

func cp(m map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

// fakeOf rebuilds a fake reader from a world.
func fakeOf(w wWorld, now time.Time) *fakeReader {
	f := newFake()
	for _, it := range w.Items {
		f.items = append(f.items, hosting.PullItem{Number: it.Number, Title: it.Title, User: it.User, Draft: it.Draft, HeadSHA: it.HeadSHA, HeadRef: it.HeadRef, BaseRef: it.BaseRef, Labels: it.Labels})
	}
	if w.ListError != nil {
		f.listErr = w.ListError.err()
	}
	for k, v := range w.Details {
		n, _ := strconv.Atoi(k)
		f.details[n] = v
	}
	for k, v := range w.DetailETags {
		n, _ := strconv.Atoi(k)
		f.detailET[n] = v
	}
	for k, v := range w.PullCommits {
		n, _ := strconv.Atoi(k)
		f.pulls[n] = v
	}
	f.tips, f.runs, f.stats, f.commits = cp(w.Tips), cp(w.CheckRuns), cp(w.Statuses), cp(w.Commits)
	if w.Rate != nil {
		f.rate = hosting.RateInfo{Known: w.Rate.Known, Limit: w.Rate.Limit, Remaining: w.Rate.Remaining, Reset: now.Add(time.Duration(w.Rate.ResetOffsetSecs) * time.Second)}
	}
	for k, e := range w.CallErrors {
		f.errAt[k] = e.err()
	}
	return f
}

// ---- observation view ----

type oRun struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	Title      string `json:"title"`
	Summary    string `json:"summary"`
	Text       string `json:"text"`
}
type oStat struct {
	Context     string `json:"context"`
	State       string `json:"state"`
	Description string `json:"description"`
}
type oChecks struct {
	Runs     []oRun  `json:"runs"`
	Statuses []oStat `json:"statuses"`
}
type oDetail struct {
	Mergeable *bool  `json:"mergeable"`
	State     string `json:"state"`
}
type oPR struct {
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
	Detail    *oDetail `json:"detail"`
	Checks    *oChecks `json:"checks"`
}
type oGone struct {
	Merged      bool    `json:"merged"`
	MergeCommit *string `json:"merge_commit"`
	MergedAt    string  `json:"merged_at"`
	Base        string  `json:"base"`
	Head        string  `json:"head"`
	HeadRef     string  `json:"head_ref"`
	MethodHint  string  `json:"method_hint"`
}
type oError struct {
	Code           string `json:"code"`
	RunWide        bool   `json:"run_wide"`
	RetryInSeconds int    `json:"retry_in_seconds"`
	Canceled       bool   `json:"canceled"`
}
type oObs struct {
	DefaultBranch string           `json:"default_branch"`
	PRs           []oPR            `json:"prs"`
	Gone          map[string]oGone `json:"gone"`
}

func oObsOf(o watch.RepoObservation) oObs {
	out := oObs{DefaultBranch: o.DefaultBranch, PRs: []oPR{}, Gone: map[string]oGone{}}
	for _, p := range o.PRs {
		x := oPR{Number: p.Number, Title: p.Title, Author: p.Author, Labels: nonNilS(p.Labels), Draft: p.Draft, Head: p.Head, HeadRef: p.HeadRef, Base: p.Base, Fork: p.Fork, ForkKnown: p.ForkKnown}
		if p.Detail != nil {
			x.Detail = &oDetail{Mergeable: p.Detail.Mergeable, State: p.Detail.State}
		}
		if p.Checks != nil {
			c := &oChecks{Runs: []oRun{}, Statuses: []oStat{}}
			for _, r := range p.Checks.Runs {
				c.Runs = append(c.Runs, oRun{r.Name, r.Status, r.Conclusion, r.Title, r.Summary, r.Text})
			}
			for _, s := range p.Checks.Statuses {
				c.Statuses = append(c.Statuses, oStat{s.Context, s.State, s.Description})
			}
			x.Checks = c
		}
		out.PRs = append(out.PRs, x)
	}
	for n, g := range o.Gone {
		out.Gone[strconv.Itoa(n)] = oGone{g.Merged, g.MergeCommit, g.MergedAt, g.Base, g.Head, g.HeadRef, g.MethodHint}
	}
	return out
}

func oErrOf(err error) *oError {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return &oError{Canceled: true}
	}
	var oe *watch.ObserveError
	if errors.As(err, &oe) {
		return &oError{Code: oe.Code, RunWide: oe.RunWide, RetryInSeconds: int(oe.RetryIn / time.Second)}
	}
	return &oError{Code: "other"}
}

type oKnown struct {
	Number         int    `json:"number"`
	Head           string `json:"head"`
	Class          string `json:"class"`
	ClassCandidate string `json:"class_candidate"`
	DetailDue      bool   `json:"detail_due"`
}

type oHints struct {
	Cycle       int      `json:"cycle"`
	DetailEvery int      `json:"detail_every"`
	Known       []oKnown `json:"known"`
}

func (h oHints) hints() watch.Hints {
	out := watch.Hints{Cycle: h.Cycle, DetailEvery: h.DetailEvery}
	for _, k := range h.Known {
		out.Known = append(out.Known, watch.KnownPR{Number: k.Number, Head: k.Head, Class: k.Class, ClassCand: k.ClassCandidate, DetailDue: k.DetailDue})
	}
	return out
}

type oCall struct {
	Call        string `json:"call"`
	Conditional bool   `json:"conditional"`
}

// callsOf lists the calls of the fake in order; the validator sent says whether a read was conditional.
func callsOf(f *fakeReader) []oCall {
	out := []oCall{}
	for _, c := range f.calls {
		out = append(out, oCall{Call: c, Conditional: f.etags[c] != ""})
	}
	return out
}

func sortedKeysOf[V any](m map[string]V) []string {
	var k []string
	for x := range m {
		k = append(k, x)
	}
	sort.Strings(k)
	return k
}

var _ = fmt.Sprint
var _ = strings.Join
