// SPDX-License-Identifier: EUPL-1.2

package tidywizard

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/wstein/sourcesteward/internal/fanout"
	"github.com/wstein/sourcesteward/internal/hosting"
)

// The online pull request evidence of the human wizard (Spec 044 T005, D13). For the tips of the
// worktree and branch rows that the local proofs leave open it asks GitHub, read only, through
// the bounded Spec 031 discovery of internal/hosting (the closed collection of closed pull
// requests, then one full read per candidate), one session per member repository. The answer is a
// fact per tip that a later step (the risk ladder, the renderer) may consume; this file never
// changes a candidate, never proves a deletion (D8) and never writes.

// PRState is the pull request evidence of one tip.
type PRState string

// The states of the pull request evidence of a tip. A tip that was not asked (beyond the cap of
// its repository, or a repository judged offline) has no fact at all; that is not PRUnknown.
const (
	// PRMerged: a pull request of the repository, closed and merged, whose head is exactly the
	// tip. Number is that pull request.
	PRMerged PRState = "merged-head"
	// PRClosedUnmerged: no merged pull request, but a pull request of this head was closed
	// without a merge.
	PRClosedUnmerged PRState = "closed-unmerged"
	// PRNone: the collection of closed pull requests was read to its authoritative end and holds
	// no pull request of this head. An open pull request is not looked up.
	PRNone PRState = "none"
	// PRUnknown: GitHub was asked and the answer is inconclusive (collection bound reached,
	// request budget exhausted, several merged pull requests).
	PRUnknown PRState = "unknown"
)

// PREvidence is the optional pull request fact of one tip. Number is set only with PRMerged.
type PREvidence struct {
	State  PRState `json:"state"`
	Number int     `json:"number,omitempty"`
}

// RaisesToStrong reports whether this fact lifts the row to strong evidence (green). Only a merged
// head does, and only for a row that is already safe: an unmerged, a decision or any other
// retained row is never lifted (Spec 044 D8: online evidence is no deletion proof by itself).
func (f PREvidence) RaisesToStrong(c Candidate) bool {
	return f.State == PRMerged && f.Number > 0 && c.Verdict == VerdictSafe && (c.Kind == KindWorktree || c.Kind == KindBranch)
}

// Note is the evidence text of a merged head for a row, or empty. A safe row reads "merged as
// #N"; a row that is retained as unmerged keeps its level and reads that the local proofs disagree.
func (f PREvidence) Note(c Candidate) string {
	if f.State != PRMerged || f.Number <= 0 {
		return ""
	}
	switch {
	case c.Verdict == VerdictSafe:
		return fmt.Sprintf("merged as #%d", f.Number)
	case c.Verdict == VerdictRetain && c.Reason == "unmerged":
		return fmt.Sprintf("merged as #%d, but the local proofs disagree: review it", f.Number)
	}
	return ""
}

// prFact maps one row of the Spec 031 discovery to the fact. A merged head wins over a closed
// unmerged pull request of the same head; absence counts only when the collection was complete.
func prFact(row hosting.TidyHeadEvidence) PREvidence {
	switch {
	case row.Result == "github-merged-head" && row.PullRequest != nil && row.PullRequest.Number > 0:
		return PREvidence{State: PRMerged, Number: row.PullRequest.Number}
	case row.Result == "github-merged-head":
		return PREvidence{State: PRUnknown}
	case len(row.ClosedUnmerged) > 0 && row.Result != "ambiguous-needs-hint":
		return PREvidence{State: PRClosedUnmerged}
	case row.Result == "no-merged-pr" && row.Coverage == "complete":
		return PREvidence{State: PRNone}
	}
	return PREvidence{State: PRUnknown}
}

// TipKey identifies the tip of a row: the repository and the full object id.
type TipKey struct{ Repo, Tip string }

// Why a repository was judged offline (the reasons of the one notice line of D13).
const (
	ReasonNotAuthenticated  = "not-authenticated"
	ReasonRemoteUnsupported = "remote-unsupported"
	ReasonRateLimit         = "rate-limit"
	ReasonServer            = "server-error"
	ReasonRunCap            = "run-cap"
	ReasonTimeout           = "timeout"
	ReasonMemberBudget      = "member-budget"
	ReasonError             = "error"
)

var reasonText = map[string]string{
	ReasonNotAuthenticated:  "gh is not authenticated",
	ReasonRemoteUnsupported: "the remote is not a supported github.com repository",
	ReasonRateLimit:         "GitHub rate limit reached",
	ReasonServer:            "GitHub reported a server error",
	ReasonRunCap:            "per-run request cap reached",
	ReasonTimeout:           "timeout reached",
	ReasonMemberBudget:      "a repository's request or time budget ran out",
	ReasonError:             "a GitHub read failed",
}

var reasonOrder = []string{ReasonNotAuthenticated, ReasonRemoteUnsupported, ReasonRateLimit, ReasonServer, ReasonRunCap, ReasonTimeout, ReasonMemberBudget, ReasonError}

// ReasonText is the human wording of a fallback reason; an unknown word reads as a failed read.
func ReasonText(reason string) string {
	if t, ok := reasonText[reason]; ok {
		return t
	}
	return reasonText[ReasonError]
}

// FormatOnlineNotice is the one notice line of the online reads: how many of the repositories were
// checked and, when some were judged offline, why (each reason once, in a fixed order).
func FormatOnlineNotice(checked, of int, reasons []string) string {
	if of <= 0 {
		return ""
	}
	line := fmt.Sprintf("GitHub: checked %d of %d repositories", checked, of)
	seen := map[string]bool{}
	for _, r := range reasons {
		if _, ok := reasonText[r]; !ok {
			r = ReasonError
		}
		seen[r] = true
	}
	var texts []string
	for _, r := range reasonOrder {
		if seen[r] {
			texts = append(texts, reasonText[r])
		}
	}
	if len(texts) == 0 {
		return line
	}
	return line + "; the others are judged offline (" + strings.Join(texts, "; ") + ")"
}

// The state of one repository after the online reads.
const (
	MemberChecked = "checked"
	MemberOffline = "offline"
)

// MemberOutcome reports one repository. Eligible is the number of distinct tips that could have
// been asked, Asked the number that were (at most MaxOnlineTips), NotChecked the rest.
type MemberOutcome struct {
	Name       string
	State      string
	Reason     string // set when State is MemberOffline
	Eligible   int
	Asked      int
	NotChecked int
}

// OnlineOutcome is the result of FetchPREvidence.
type OnlineOutcome struct {
	// Facts holds one fact per tip that was asked of a repository that finished.
	Facts map[TipKey]PREvidence
	// Members is one outcome per repository, sorted by name.
	Members []MemberOutcome
	// Requests is the number of requests the run counted (each one a read).
	Requests int
	// Stopped is why the run's online reads stopped, or empty.
	Stopped hosting.TidyStopReason
}

// Lookup returns the fact of a row's tip; false when the tip was not asked.
func (o OnlineOutcome) Lookup(c Candidate) (PREvidence, bool) {
	if c.Tip == "" {
		return PREvidence{}, false
	}
	f, ok := o.Facts[TipKey{Repo: c.Repo, Tip: c.Tip}]
	return f, ok
}

// Checked is the number of repositories that finished, Offline the reasons of the others.
func (o OnlineOutcome) Checked() (checked, of int, reasons []string) {
	for _, m := range o.Members {
		of++
		if m.State == MemberChecked {
			checked++
		} else {
			reasons = append(reasons, m.Reason)
		}
	}
	return checked, of, reasons
}

// Notice is the one notice line of the pull request reads, empty when there was nothing to read.
func (o OnlineOutcome) Notice() string {
	checked, of, reasons := o.Checked()
	return FormatOnlineNotice(checked, of, reasons)
}

// MaxOnlineTips is the number of distinct tips asked of one repository (Spec 044 D13).
const MaxOnlineTips = 64

// OnlineMember is one repository whose tips may be asked: its path and the remote the observation
// already selected.
type OnlineMember struct{ Name, Path, Remote string }

// PRSession is the part of a hosting tidy session that the fetch uses; *hosting.TidySession is one.
type PRSession interface {
	DiscoverPullRequests(ctx context.Context, targets []string, hints map[string]int) ([]hosting.TidyHeadEvidence, error)
	Close()
}

// PROpener opens the session of one repository. The transport rt is the run's shared budget and
// must be the inner transport of the session.
type PROpener func(ctx context.Context, path, remote string, rt http.RoundTripper) (PRSession, error)

// DefaultPROpener opens the production session: native preflight, github.com only, the fixed
// bounded transport, the stored gh credential.
func DefaultPROpener(ctx context.Context, path, remote string, rt http.RoundTripper) (PRSession, error) {
	s, err := hosting.OpenTidySessionWithTransport(ctx, path, hosting.TidySessionOptions{Online: true, Remote: remote}, rt)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// OnlineOptions are the seams and bounds of FetchPREvidence; the zero value is production.
type OnlineOptions struct {
	Open PROpener
	// Transport is the transport below the shared run budget; nil is the production HTTPS one.
	Transport http.RoundTripper
	// Width is the number of repositories read at the same time (default 4).
	Width int
	// RunCap is the request cap of the run (default and maximum hosting.TidyRunMaxRequests).
	RunCap int
	// MaxTips is the number of tips asked per repository (default and maximum MaxOnlineTips).
	MaxTips int
}

const onlineWidth = 4

// tipSelection is the ordered tips asked of one repository.
type tipSelection struct {
	Tips     []string
	Eligible int
}

func isFullTip(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range []byte(s) {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func equivalentProof(p Proof) bool { return p == ProofPatchID || p == ProofMergeNoop }

// selectTips picks the tips of one repository in the order of the specification: the safe rows
// whose proof is only equivalent (by row id), then the rows retained as unmerged (by row id), each
// distinct tip once, at most limit. Strong proofs are green already and are not asked.
func selectTips(repo string, cands []Candidate, limit int) tipSelection {
	var first, second []Candidate
	for _, c := range cands {
		if c.Repo != repo || (c.Kind != KindWorktree && c.Kind != KindBranch) || !isFullTip(c.Tip) {
			continue
		}
		switch {
		case c.Verdict == VerdictSafe && equivalentProof(c.Proof):
			first = append(first, c)
		case c.Verdict == VerdictRetain && c.Reason == "unmerged":
			second = append(second, c)
		}
	}
	byID := func(s []Candidate) { sort.SliceStable(s, func(i, j int) bool { return s[i].ID < s[j].ID }) }
	byID(first)
	byID(second)
	var sel tipSelection
	seen := map[string]bool{}
	for _, c := range append(first, second...) {
		if seen[c.Tip] {
			continue
		}
		seen[c.Tip] = true
		sel.Eligible++
		if len(sel.Tips) < limit {
			sel.Tips = append(sel.Tips, c.Tip)
		}
	}
	return sel
}

// stopReason maps the stop reason of the run budget to the reason of a repository.
func stopReason(r hosting.TidyStopReason) string {
	switch r {
	case hosting.TidyStopRateLimit:
		return ReasonRateLimit
	case hosting.TidyStopServer:
		return ReasonServer
	case hosting.TidyStopRunCap:
		return ReasonRunCap
	}
	return ""
}

// failureReason names why one repository's reads failed. A stop of the run comes first: every
// repository that has not finished is judged offline for that reason.
func failureReason(ctx context.Context, budget *hosting.TidyRunBudget, err error) string {
	switch {
	case stopReason(budget.Stopped()) != "":
		return stopReason(budget.Stopped())
	case ctx.Err() != nil:
		return ReasonTimeout
	case errors.Is(err, hosting.ErrTidyAuthentication):
		return ReasonNotAuthenticated
	case errors.Is(err, hosting.ErrTidyUnsupported):
		return ReasonRemoteUnsupported
	case errors.Is(err, hosting.ErrTidyLimit), errors.Is(err, context.DeadlineExceeded):
		return ReasonMemberBudget
	}
	return ReasonError
}

// FetchPREvidence reads the pull request evidence of the open tips of the members (Spec 044
// T005). The members are read through the existing fan-out, each with its own session and its
// own 128-request, 32 MiB and 30-second budget; all sessions share one run budget (512 requests),
// whose stop (rate limit, server error, cap) and the end of ctx judge every repository that has
// not finished offline, nothing retried. A repository whose session fails is judged offline on
// its own. The candidates are only read.
func FetchPREvidence(ctx context.Context, members []OnlineMember, cands []Candidate, o OnlineOptions) OnlineOutcome {
	if o.Open == nil {
		o.Open = DefaultPROpener
	}
	if o.Width < 1 {
		o.Width = onlineWidth
	}
	if o.RunCap < 1 || o.RunCap > hosting.TidyRunMaxRequests {
		o.RunCap = hosting.TidyRunMaxRequests
	}
	if o.MaxTips < 1 || o.MaxTips > MaxOnlineTips {
		o.MaxTips = MaxOnlineTips
	}
	list := append([]OnlineMember{}, members...)
	sort.SliceStable(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	out := OnlineOutcome{Facts: map[TipKey]PREvidence{}, Members: []MemberOutcome{}}
	budget := hosting.NewTidyRunBudget(o.Transport, o.RunCap)
	defer budget.Close()
	results := make([]MemberOutcome, len(list))
	facts := make([]map[string]PREvidence, len(list))
	_ = fanout.Do(ctx, len(list), o.Width, func(ctx context.Context, i int) {
		results[i], facts[i] = fetchMember(ctx, list[i], cands, o, budget)
	})
	for i, m := range list {
		if results[i].State == "" {
			// Never started: the run's context ended first.
			sel := selectTips(m.Name, cands, o.MaxTips)
			results[i] = MemberOutcome{Name: m.Name, State: MemberOffline, Reason: ReasonTimeout, Eligible: sel.Eligible, NotChecked: sel.Eligible}
		}
		for tip, f := range facts[i] {
			out.Facts[TipKey{Repo: m.Name, Tip: tip}] = f
		}
		out.Members = append(out.Members, results[i])
	}
	out.Requests, out.Stopped = budget.Requests(), budget.Stopped()
	return out
}

func fetchMember(ctx context.Context, m OnlineMember, cands []Candidate, o OnlineOptions, budget *hosting.TidyRunBudget) (MemberOutcome, map[string]PREvidence) {
	sel := selectTips(m.Name, cands, o.MaxTips)
	res := MemberOutcome{Name: m.Name, State: MemberChecked, Eligible: sel.Eligible, Asked: len(sel.Tips), NotChecked: sel.Eligible - len(sel.Tips)}
	if len(sel.Tips) == 0 {
		return res, nil
	}
	offline := func(reason string) (MemberOutcome, map[string]PREvidence) {
		res.State, res.Reason, res.Asked, res.NotChecked = MemberOffline, reason, 0, sel.Eligible
		return res, nil
	}
	if r := stopReason(budget.Stopped()); r != "" {
		return offline(r)
	}
	if ctx.Err() != nil {
		return offline(ReasonTimeout)
	}
	session, err := o.Open(ctx, m.Path, m.Remote, budget)
	if err != nil || session == nil {
		return offline(failureReason(ctx, budget, err))
	}
	defer session.Close()
	rows, err := session.DiscoverPullRequests(ctx, sel.Tips, nil)
	if err != nil {
		return offline(failureReason(ctx, budget, err))
	}
	asked := map[string]bool{}
	for _, tip := range sel.Tips {
		asked[tip] = true
	}
	got := map[string]PREvidence{}
	for _, row := range rows {
		if !asked[row.HeadOID] {
			return offline(ReasonError)
		}
		got[row.HeadOID] = prFact(row)
	}
	if len(got) != len(sel.Tips) {
		return offline(ReasonError)
	}
	return res, got
}
