// SPDX-License-Identifier: EUPL-1.2

package land

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// Plan evaluates the chain read-only (FR-060 to FR-068, FR-081) and returns the report. It reads
// the forge, asks Git and fetches once into the private namespace; it writes nothing else and
// never prompts. Every failing precondition is listed (FR-019).
func Plan(ctx context.Context, o Options, mode Mode, pol Policy, f Forge, g Git) Report {
	p := &planner{ctx: ctx, o: o, pol: pol, f: f, g: g, pulls: map[int]PullFacts{}, bad: map[int]bool{}, evidence: map[int]string{}, checks: map[int]string{}, native: map[int]string{}}
	if p.o.RangeBound <= 0 {
		p.o.RangeBound = DefaultRangeBound
	}
	if p.o.Shapes == nil {
		p.o.Shapes = EmbeddedShapes()
	}
	p.r = Report{
		SchemaVersion: SchemaVersion, Command: "land", Operation: OperationPlan, Mode: mode, Repository: o.Repo,
		Preconditions: []Precondition{}, Chain: []ChainEntry{}, Effects: []Effect{}, Children: []int{}, Reasons: []Reason{}, Notes: []string{},
		Inputs: Inputs{Reviewed: map[string]string{}, AckDraft: o.AckDraft, Remote: o.Remote, PlanOnly: true, RangeMax: p.o.RangeBound},
	}
	if mode.Markers == nil {
		p.r.Mode.Markers = []string{}
	}
	for _, it := range o.Chain {
		p.r.Inputs.Stack = append(p.r.Inputs.Stack, it.Number)
		p.r.Inputs.Reviewed[fmt.Sprint(it.Number)] = it.Reviewed
	}
	p.run()
	p.finish()
	return p.r
}

type planner struct {
	ctx      context.Context
	o        Options
	pol      Policy
	f        Forge
	g        Git
	r        Report
	repo     RepoFacts
	pulls    map[int]PullFacts
	bad      map[int]bool // pull requests that are refused or unreadable; they number in reasons
	evidence map[int]string
	checks   map[int]string
	native   map[int]string // native items: number to the old base ref
	tip      string         // reviewed head of the top pull request
	m        string         // remote default branch tip
	replay   bool
	merged   map[int]string // outcome per number in a replay
}

// clean escapes control characters and bounds untrusted text (SI6).
func clean(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == utf8.RuneError, r < 0x20, r == 0x7f, r >= 0x80 && r < 0xa0, r == 0x202e, r == 0x2066, r == 0x2067, r == 0x2068, r == 0x2069:
			fmt.Fprintf(&b, "\\u%04x", r)
		default:
			b.WriteRune(r)
		}
		if b.Len() > maxDetailBytes {
			return b.String()[:maxDetailBytes] + "..."
		}
	}
	return b.String()
}

// check records one precondition; a failing one also becomes a typed reason.
func (p *planner) check(id string, n int, ok bool, value, code, detail string) bool {
	pre := Precondition{ID: id, Number: n, OK: ok, Value: clean(value)}
	if !ok {
		pre.Reason = code
		p.r.Reasons = append(p.r.Reasons, Reason{Code: code, Number: n, Detail: clean(detail)})
		if n != 0 {
			p.bad[n] = true
		}
	}
	p.r.Preconditions = append(p.r.Preconditions, pre)
	return ok
}

func (p *planner) readFailed(id string, n int, err error) {
	p.check(id, n, false, "unreadable", ReasonReadFailed, err.Error())
}

func (p *planner) numbers() []int {
	out := make([]int, 0, len(p.o.Chain))
	for _, it := range p.o.Chain {
		out = append(out, it.Number)
	}
	return out
}

func (p *planner) run() {
	repo, err := p.f.Repository(p.ctx)
	if err != nil {
		p.readFailed("repository", 0, err)
		return
	}
	if !RefName(repo.DefaultBranch) || !RefName(p.o.Remote) {
		p.check("repository", 0, false, "invalid default branch or remote name", ReasonReadFailed, "default branch "+repo.DefaultBranch+", remote "+p.o.Remote)
		return
	}
	p.repo = repo
	p.check("repository", 0, true, p.o.Repo+" default branch "+repo.DefaultBranch, "", "")
	p.tip = p.o.Chain[len(p.o.Chain)-1].Reviewed

	gitOK := p.remote()
	for _, it := range p.o.Chain {
		p.readPull(it)
	}
	if gitOK {
		gitOK = p.gitFacts()
	}
	if gitOK && p.replayCheck() {
		return
	}
	for i, it := range p.o.Chain {
		p.pullChecks(i, it)
	}
	p.sideChildren()
	if gitOK {
		p.ancestry()
	} else {
		p.r.Notes = append(p.r.Notes, "git evidence not gathered: the remote does not match the repository or Git could not be read")
	}
	p.checkCI()
	p.rules()
	p.reviews()
}

// remote verifies that the remote is the repository the pull requests were read from (FR-016).
func (p *planner) remote() bool {
	got, err := p.g.RemoteRepo(p.ctx, p.o.Remote)
	if err != nil {
		p.readFailed("remote", 0, err)
		return false
	}
	if got == "" || !strings.EqualFold(got, p.o.Repo) {
		shown := got
		if shown == "" {
			shown = "not a recognized GitHub URL"
		}
		return p.check("remote", 0, false, shown, ReasonRemoteMismatch, "remote "+p.o.Remote+" is "+shown+", expected "+p.o.Repo+"; run in that checkout, pass its absolute path as positional PATH, or use --remote NAME")
	}
	return p.check("remote", 0, true, p.o.Remote+" is "+got, "", "")
}

func (p *planner) readPull(it Item) {
	pf, err := p.f.Pull(p.ctx, it.Number)
	if err == nil && pf.Mergeable == nil && pf.State == "open" && !pf.Merged {
		// FR-014: one bounded re-read; GitHub computes mergeability lazily.
		if again, err2 := p.f.Pull(p.ctx, it.Number); err2 == nil {
			pf = again
		}
	}
	if err != nil {
		p.readFailed("pull-request", it.Number, err)
		return
	}
	p.pulls[it.Number] = pf
}

// gitFacts reads the remote tip and fetches the heads into the private namespace.
func (p *planner) gitFacts() bool {
	tip, err := p.g.DefaultTip(p.ctx, p.o.Remote, p.repo.DefaultBranch)
	if err != nil || !ValidSHA(tip) {
		if err == nil {
			err = fmt.Errorf("ls-remote returned no default branch tip")
		}
		p.readFailed("default-tip", 0, err)
		return false
	}
	p.m = tip
	p.r.DefaultTip = tip
	p.r.Effects = append(p.r.Effects, Effect{ID: "fetch", Kind: fetchKind, Argv: Argv(FetchArgs(p.o.Remote, p.repo.DefaultBranch, p.numbers())), Outcome: effectPlanned,
		Detail: "read-only fetch into " + PrivateRefPrefix + " (objects and that namespace only)"})
	if err := p.g.Fetch(p.ctx, p.o.Remote, p.repo.DefaultBranch, p.numbers()); err != nil {
		p.readFailed("fetch", 0, err)
		return false
	}
	p.r.Effects[len(p.r.Effects)-1].Outcome = effectOK
	for _, it := range p.o.Chain {
		pf, ok := p.pulls[it.Number]
		if !ok {
			continue
		}
		got, err := p.g.Resolve(p.ctx, it.Number)
		switch {
		case err != nil:
			p.readFailed("local-head", it.Number, err)
		case got == "":
			p.check("local-head", it.Number, false, "absent", ReasonObjectMissing, "the head of the pull request is not available after the fetch")
		case pf.HeadSHA != "" && got != pf.HeadSHA:
			p.check("local-head", it.Number, false, got, ReasonHeadChanged, "the head fetched differs from the head GitHub reported: "+pf.HeadSHA)
		}
	}
	return true
}

// replayCheck classifies a chain whose tip the default branch already contains (FR-074); it is
// evaluated before the preconditions that need open pull requests. It returns true when it ended
// the plan.
func (p *planner) replayCheck() bool {
	if p.m != p.tip {
		v, err := p.g.IsAncestor(p.ctx, p.tip, p.m)
		if err != nil || v != AncestorYes {
			return false
		}
	}
	p.replay = true
	all := true
	for _, it := range p.o.Chain {
		pf, ok := p.pulls[it.Number]
		outcome := "landed-not-marked"
		switch {
		case !ok:
			outcome = "unknown"
			all = false
		case pf.Merged && pf.MergeCommit == it.Reviewed:
			outcome = "landed"
		case pf.Merged:
			outcome = "merged-other-sha"
			all = false
		default:
			all = false
		}
		p.r.Chain = append(p.r.Chain, ChainEntry{Number: it.Number, Head: it.Reviewed, BaseRef: pf.BaseRef, ReviewEvidence: ReviewUnknown, Checks: "not-read", State: prState(pf, ok), Outcome: outcome})
	}
	p.r.Result = ResultLandedNotMarked
	if all {
		p.r.Result = ResultAlreadyLanded
	}
	p.r.Notes = append(p.r.Notes, "the default branch already contains the tip; the push is skipped and the pull request states are reported")
	return true
}

func prState(pf PullFacts, ok bool) string {
	switch {
	case !ok:
		return "unknown"
	case pf.Merged:
		return "merged"
	}
	return pf.State
}

// pullChecks is FR-060, FR-061, FR-063 (bases), FR-068 and the facts of FR-014.
func (p *planner) pullChecks(i int, it Item) {
	pf, ok := p.pulls[it.Number]
	if !ok {
		return
	}
	n := it.Number
	switch {
	case pf.Merged:
		p.check("open", n, false, "merged", ReasonAlreadyMerged, fmt.Sprintf("merged with merge commit %s; nothing is changed, name the rest", pf.MergeCommit))
	case pf.State != "open":
		detail := "the pull request is " + pf.State
		if pf.BaseRef != p.repo.DefaultBranch {
			detail += "; GitHub can close a pull request whose base branch was deleted (research.md R3); stw does not reopen or recreate it"
		}
		p.check("open", n, false, pf.State, ReasonNotOpen, detail)
	default:
		p.check("open", n, true, "open", "", "")
	}
	p.check("same-repository", n, strings.EqualFold(pf.HeadRepo, p.o.Repo) && strings.EqualFold(pf.BaseRepo, p.o.Repo), pf.HeadRepo, ReasonFork, "head repository "+pf.HeadRepo+", base repository "+pf.BaseRepo)
	p.check("head-reviewed", n, pf.HeadSHA == it.Reviewed, pf.HeadSHA, ReasonHeadChanged, "head is "+pf.HeadSHA+", reviewed "+it.Reviewed)
	switch {
	case !pf.Draft:
		p.check("draft", n, true, "false", "", "")
	case p.o.AckDraft:
		p.check("draft", n, true, "true (acknowledged by --ack-draft)", "", "")
	default:
		p.check("draft", n, false, "true", ReasonDraft, "pass --ack-draft to acknowledge a Draft")
	}
	switch {
	case pf.Mergeable == nil:
		p.check("mergeable", n, false, "null", ReasonMergeabilityNone, "GitHub has not computed mergeability after one re-read; run again")
	case !*pf.Mergeable || pf.MergeableState == "dirty":
		p.check("mergeable", n, false, fmt.Sprintf("%t/%s", *pf.Mergeable, pf.MergeableState), ReasonConflicting, "GitHub reports conflicts")
	default:
		p.check("mergeable", n, true, fmt.Sprintf("true/%s", pf.MergeableState), "", "")
	}
	rebase := "unknown"
	if pf.Rebaseable != nil {
		rebase = fmt.Sprint(*pf.Rebaseable)
	}
	p.check("rebaseable", n, true, rebase, "", "") // recorded, not a precondition of a fast-forward (FR-014)
	p.baseShape(i, it.Number, pf)
}

// baseShape is FR-063 and FR-084: the bottom targets the default branch; any other item targets the
// default branch (flat) or the head branch of its immediate predecessor (native). The evidence gate
// of FR-085 runs with the shape in rules().
func (p *planner) baseShape(i, n int, pf PullFacts) {
	switch {
	case pf.BaseRef == p.repo.DefaultBranch:
		p.check("base", n, true, pf.BaseRef, "", "")
	case i > 0 && p.pulls[p.o.Chain[i-1].Number].HeadRef != "" && pf.BaseRef == p.pulls[p.o.Chain[i-1].Number].HeadRef:
		p.native[n] = pf.BaseRef
		p.check("base", n, true, fmt.Sprintf("%s (native: head branch of #%d)", pf.BaseRef, p.o.Chain[i-1].Number), "", "")
	default:
		p.check("base", n, false, pf.BaseRef, ReasonBaseNotInChain, "the base is neither the default branch nor, for a pull request above the bottom, the head branch of the previous pull request of the chain")
	}
}

// sideChildren is FR-066.
func (p *planner) sideChildren() {
	open, truncated, err := p.f.OpenPulls(p.ctx)
	if err != nil {
		p.readFailed("side-children", 0, err)
		return
	}
	if truncated {
		p.check("side-children", 0, false, "list truncated", ReasonSideChildUnknown, fmt.Sprintf("more than %d open pull requests; a side child cannot be excluded", MaxOpenPulls))
		return
	}
	listed := map[int]bool{}
	heads := map[string]int{}
	for _, it := range p.o.Chain {
		listed[it.Number] = true
		if pf, ok := p.pulls[it.Number]; ok {
			heads[pf.HeadRef] = it.Number
		}
	}
	found := false
	for _, op := range open {
		if parent, ok := heads[op.BaseRef]; ok && !listed[op.Number] && op.BaseRef != p.repo.DefaultBranch {
			found = true
			p.r.Children = append(p.r.Children, op.Number)
			p.check("side-child", op.Number, false, "base "+op.BaseRef, ReasonSideChild, fmt.Sprintf("its base is the head branch of #%d; land it first or retarget it by hand (stw retargets only the pull requests of the chain)", parent))
			delete(p.bad, op.Number) // the side child is not part of the chain
		}
	}
	if !found {
		p.check("side-children", 0, true, "none", "", "")
	}
}

// ancestry is FR-063 (linearity), FR-065, FR-018, FR-081 and FR-017.
func (p *planner) ancestry() {
	chain := p.o.Chain
	ff := false
	switch v, _ := p.g.IsAncestor(p.ctx, p.m, chain[0].Reviewed); v {
	case AncestorYes:
		ff = p.check("fast-forward", chain[0].Number, true, "default tip is an ancestor of the bottom", "", "")
	case AncestorNo:
		p.check("fast-forward", chain[0].Number, false, "diverged", ReasonNotFastForward, DetailRebaseNeeded+": the default branch has commits the bottom does not contain (Spec 015/016)")
	default:
		p.check("fast-forward", chain[0].Number, false, "unknown", ReasonObjectMissing, "an object is absent after the fetch; ancestry is unproven")
	}
	linear := true
	for i := 0; i+1 < len(chain); i++ {
		lo, hi := chain[i], chain[i+1]
		v := AncestorNo
		if lo.Reviewed != hi.Reviewed {
			v, _ = p.g.IsAncestor(p.ctx, lo.Reviewed, hi.Reviewed)
		}
		switch v {
		case AncestorYes:
			p.check("linear", hi.Number, true, fmt.Sprintf("#%d is an ancestor of #%d", lo.Number, hi.Number), "", "")
		case AncestorNo:
			linear = false
			p.check("linear", hi.Number, false, "diverged", ReasonChainNotLinear, fmt.Sprintf("the head of #%d is not an ancestor of the head of #%d (Git decides)", lo.Number, hi.Number))
		default:
			linear = false
			p.check("linear", hi.Number, false, "unknown", ReasonObjectMissing, "an object is absent after the fetch; ancestry is unproven")
		}
	}
	if !ff || !linear {
		p.r.Notes = append(p.r.Notes, "commit range, identity and range invariant not evaluated: the chain does not fast-forward linearly")
		return
	}
	commits, err := p.g.Range(p.ctx, p.m, p.tip, p.o.RangeBound)
	if err != nil {
		p.readFailed("range", 0, err)
		return
	}
	if len(commits) > p.o.RangeBound {
		p.r.Truncated = true
		p.check("range", 0, false, fmt.Sprintf("more than %d commits", p.o.RangeBound), ReasonRangeTooLarge, fmt.Sprintf("the range default..tip exceeds the bound of %d commits", p.o.RangeBound))
		return
	}
	p.check("range", 0, true, fmt.Sprintf("%d commits", len(commits)), "", "")
	p.rangeInvariant(commits)
	p.identity(commits)
}

// rangeInvariant is FR-081: M..tip equals the union of the listed ranges.
func (p *planner) rangeInvariant(all []Commit) {
	want := map[string]bool{}
	for _, c := range all {
		want[c.SHA] = true
	}
	got := map[string]bool{}
	prev := p.m
	for _, it := range p.o.Chain {
		part, err := p.g.Range(p.ctx, prev, it.Reviewed, p.o.RangeBound)
		if err != nil {
			p.readFailed("range-invariant", it.Number, err)
			return
		}
		for _, c := range part {
			got[c.SHA] = true
		}
		prev = it.Reviewed
	}
	var extra []string
	for s := range want {
		if !got[s] {
			extra = append(extra, s)
		}
	}
	for s := range got {
		if !want[s] {
			extra = append(extra, s)
		}
	}
	sort.Strings(extra)
	if len(extra) > 0 {
		p.check("range-invariant", 0, false, fmt.Sprintf("%d commits differ", len(extra)), ReasonRangeMismatch, "commits in only one of default..tip and the listed ranges: "+strings.Join(firstN(extra, 3), ", "))
		return
	}
	p.check("range-invariant", 0, true, "default..tip equals the union of the listed ranges", "", "")
}

func firstN(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// identity is FR-017: author and committer of every commit equal the trusted policy owner.
func (p *planner) identity(commits []Commit) {
	if !p.pol.Loaded || p.pol.OwnerName == "" || p.pol.OwnerEmail == "" {
		p.check("identity", 0, false, "no trusted workspace policy", ReasonIdentityPolicy, "the trusted workspace policy with the owner identity is missing or unreadable; repository text and Git config are not authority")
		return
	}
	var bad []string
	for _, c := range commits {
		if c.AuthorName != p.pol.OwnerName || c.AuthorEmail != p.pol.OwnerEmail || c.CommitterName != p.pol.OwnerName || c.CommitterEmail != p.pol.OwnerEmail {
			bad = append(bad, fmt.Sprintf("%s author %q <%s> committer %q <%s>", shortSHA(c.SHA), c.AuthorName, c.AuthorEmail, c.CommitterName, c.CommitterEmail))
		}
	}
	if len(bad) == 0 {
		p.check("identity", 0, true, "author and committer match the policy owner", "", "")
		return
	}
	detail := fmt.Sprintf("%d commits; first %s", len(bad), strings.Join(firstN(bad, maxOffendingCommits), "; "))
	p.check("identity", 0, false, fmt.Sprintf("%d commits differ", len(bad)), ReasonIdentityMismatch, detail)
}

func shortSHA(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

// checkCI is FR-012 and FR-064.
func (p *planner) checkCI() {
	req, err := p.f.Required(p.ctx, p.repo.DefaultBranch)
	if err != nil {
		req = RequiredFacts{}
	}
	var viewer string
	viewerRead := false
	for i, it := range p.o.Chain {
		n := it.Number
		pf, ok := p.pulls[n]
		if !ok {
			p.checks[n] = "unknown"
			continue
		}
		tip := i == len(p.o.Chain)-1
		facts, err := p.f.Checks(p.ctx, it.Reviewed)
		if err != nil {
			p.checks[n] = "unknown"
			p.readFailed("checks", n, err)
			continue
		}
		if len(facts.Contexts) == 0 {
			p.checks[n] = "missing"
			if !viewerRead {
				viewerRead = true
				viewer, _ = p.f.Viewer(p.ctx)
			}
			p.emptyRollup(n, pf, viewer)
			continue
		}
		failing, pending := states(facts.Contexts)
		if !tip {
			switch {
			case len(failing) > 0:
				p.checks[n] = "failing"
				p.check("checks", n, false, strings.Join(failing, ", "), ReasonChecksNotGreen, "failing: "+strings.Join(failing, ", "))
			case len(pending) > 0:
				p.checks[n] = "pending"
				p.check("checks", n, false, strings.Join(pending, ", "), ReasonChecksPending, "pending: "+strings.Join(pending, ", "))
			default:
				p.checks[n] = "green"
				p.check("checks", n, true, "green", "", "")
			}
			continue
		}
		if !req.Readable {
			p.checks[n] = "unknown"
			p.check("checks", n, false, "required checks unreadable", ReasonChecksUnknown, "the rulesets or branch protection could not be read; required checks are unknown (no owner override in v1)")
			continue
		}
		var missing, bad, wait []string
		for _, c := range req.Contexts {
			switch facts.Contexts[c] {
			case CheckSuccess:
			case CheckPending:
				wait = append(wait, c)
			case CheckFailure:
				bad = append(bad, c)
			default:
				missing = append(missing, c)
			}
		}
		switch {
		case len(bad) > 0 || len(missing) > 0:
			p.checks[n] = "failing"
			p.check("checks", n, false, "required not green", ReasonChecksNotGreen, "failing: "+strings.Join(bad, ", ")+" missing: "+strings.Join(missing, ", "))
		case len(wait) > 0:
			p.checks[n] = "pending"
			p.check("checks", n, false, strings.Join(wait, ", "), ReasonTipChecksPending, "pending required checks: "+strings.Join(wait, ", "))
		case len(req.Contexts) == 0:
			p.checks[n] = "green"
			p.check("checks", n, true, "no-required-checks", "", "")
		default:
			p.checks[n] = "green"
			p.check("checks", n, true, "required checks green", "", "")
		}
	}
}

func states(m map[string]CheckState) (failing, pending []string) {
	for name, s := range m {
		switch s {
		case CheckFailure:
			failing = append(failing, name)
		case CheckPending:
			pending = append(pending, name)
		}
	}
	sort.Strings(failing)
	sort.Strings(pending)
	return
}

// emptyRollup distinguishes a head GitHub changed itself from a run that never started (FR-064).
func (p *planner) emptyRollup(n int, pf PullFacts, viewer string) {
	events, err := p.f.Events(p.ctx, n)
	if err != nil {
		p.readFailed("events", n, err)
		return
	}
	for _, e := range events {
		if (e.Name == "head_ref_force_pushed" || e.Name == "automatic_base_change_succeeded") && viewer != "" && e.Actor != viewer {
			p.check("checks", n, false, "empty rollup after a head change by "+e.Actor, ReasonHeadChangedNoChk,
				fmt.Sprintf("GitHub changed the head and no run started. The owner may run `gh pr close %d && gh pr reopen %d` (printed only, stw never runs it), then re-review", n, n))
			return
		}
	}
	p.check("checks", n, false, "empty rollup", ReasonChecksMissing, "no check run or status is reported for the head (not started or never triggered)")
}

// rules is FR-067 and the allowlist gate of decision 9.
func (p *planner) rules() {
	rs, err := p.f.Rulesets(p.ctx, p.repo.DefaultBranch)
	if err != nil {
		rs = RulesetFacts{Protection: "unreadable"}
	}
	shape := ShapeOf(p.repo, rs, true)
	p.nativeGate(shape)
	if !shape.Readable {
		p.check("ruleset", 0, false, "unreadable", ReasonLandUnmeasured, "the ruleset, protection or delete_branch_on_merge could not be read (a 403 or a plan limit); an unreadable shape never matches")
		return
	}
	blocking := false
	for _, t := range shape.RuleTypes {
		if t == "pull_request" || t == "required_status_checks" {
			blocking = true
		}
	}
	always := false
	for _, m := range shape.BypassModes {
		if m == "always" {
			always = true
		}
	}
	if blocking && !always {
		p.check("ruleset", 0, false, shape.Key(), ReasonRulesetForbids, "the rules require a pull request or checks for the default branch and no bypass of mode always exists; only the owner changes a ruleset")
		return
	}
	if !p.o.Shapes.Has(shape) {
		p.check("ruleset", 0, false, shape.Key(), ReasonLandUnmeasured, "this repository shape is not recorded in the measured allowlist (internal/land/shapes.json)")
		return
	}
	p.check("ruleset", 0, true, shape.Key(), "", "")
}

// reviews fills review_evidence (FR-062): information only, never a precondition.
func (p *planner) reviews() {
	for _, it := range p.o.Chain {
		list, err := p.f.Reviews(p.ctx, it.Number)
		ev := ReviewNone
		switch {
		case err != nil:
			ev = ReviewUnknown
		default:
			for _, rv := range list {
				if rv.CommitID == it.Reviewed && rv.State == "APPROVED" && p.reviewerAllowed(rv.Reviewer) {
					ev = ReviewFound
				}
			}
		}
		p.evidence[it.Number] = ev
	}
}

func (p *planner) reviewerAllowed(login string) bool {
	if len(p.pol.Reviewers) == 0 {
		return true
	}
	for _, r := range p.pol.Reviewers {
		if r == login {
			return true
		}
	}
	return false
}

func (p *planner) digest() string {
	if p.m == "" {
		return ""
	}
	h := sha256.New()
	fmt.Fprintf(h, "stw-land-stack-v1\n%s\n%s\n%s\n", p.o.Repo, p.m, p.tip)
	for _, it := range p.o.Chain {
		fmt.Fprintf(h, "%d %s\n", it.Number, it.Reviewed)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (p *planner) finish() {
	r := &p.r
	r.PlanDigest = p.digest()
	if p.replay {
		return // the chain table was built by the replay classification
	}
	failing := map[int]bool{}
	for _, x := range r.Reasons {
		failing[x.Number] = true
	}
	for _, it := range p.o.Chain {
		pf, ok := p.pulls[it.Number]
		head := it.Reviewed
		if ok && pf.HeadSHA != "" {
			head = pf.HeadSHA
		}
		ev, ck := p.evidence[it.Number], p.checks[it.Number]
		if ev == "" {
			ev = ReviewUnknown
		}
		if ck == "" {
			ck = "not-read"
		}
		out := "planned"
		if failing[it.Number] {
			out = "refused"
		}
		r.Chain = append(r.Chain, ChainEntry{Number: it.Number, Head: head, BaseRef: pf.BaseRef, ReviewEvidence: ev, Checks: ck, State: prState(pf, ok), Outcome: out})
	}
	r.Landable = len(r.Reasons) == 0
	r.Result = ResultRefused
	if r.Landable {
		r.Result = ResultLandable
	}
	out0 := "blocked"
	if r.Landable {
		out0 = effectPlanned
	}
	for _, it := range p.o.Chain {
		if old, ok := p.native[it.Number]; ok {
			r.Effects = append(r.Effects, Effect{ID: fmt.Sprintf("retarget-%d", it.Number), Kind: retargetKind, Argv: RetargetArgv(p.o.Repo, it.Number, p.repo.DefaultBranch), Outcome: out0,
				Detail: fmt.Sprintf("base %s becomes %s before the push (head unchanged); on a failure stw prints, and never runs, the restore command", old, p.repo.DefaultBranch)})
		}
	}
	if p.m != "" && p.tip != "" {
		out := "blocked"
		if r.Landable {
			out = effectPlanned
		}
		r.Effects = append(r.Effects, Effect{ID: "push", Kind: pushKind, Argv: Argv(PushArgs(p.o.Remote, p.tip, p.repo.DefaultBranch)), Outcome: out,
			Detail: fmt.Sprintf("one fast-forward push of the tip #%d; GitHub marks the other pull requests merged", p.o.Chain[len(p.o.Chain)-1].Number)})
	}
	r.ApplyAllowed = r.Landable && !r.Mode.Agent()
}

// nativeGate is FR-085: a native chain needs an allowlist entry measured for the native sequence.
// Without it every native item is land-unmeasured, and the plan says what the claim rests on.
func (p *planner) nativeGate(shape Shape) {
	if len(p.native) == 0 || (shape.Readable && p.o.Shapes.HasNative(shape)) {
		return
	}
	for _, it := range p.o.Chain {
		if _, ok := p.native[it.Number]; ok {
			p.check("native-shape", it.Number, false, "not measured", ReasonLandUnmeasured,
				DetailNativeStackShape+": the base is the head branch of the previous pull request; the retarget sequence is not recorded as measured for this repository shape")
		}
	}
	p.r.Notes = append(p.r.Notes, "native stack: the sequence (retarget, re-read, one push) is verified by fixtures only and not measured against GitHub for this repository shape; "+
		"run scripts/measure-native-stack.sh in a sandbox repository and record the evidence in research.md and internal/land/shapes.json")
}
