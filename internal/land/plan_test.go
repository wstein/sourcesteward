// SPDX-License-Identifier: EUPL-1.2

package land

import (
	"errors"
	"strings"
	"testing"
)

// measured makes the shape of the scenario a recorded one, so that only the tested reason fails.
func measured(s *scenario) {
	shape := ShapeOf(s.forge.repo, s.forge.rulesets, true)
	s.opts.Shapes = NewShapeSet(shape.Key())
}

func happy() *scenario {
	s := newScenario()
	measured(s)
	return s
}

func TestPlanHappyChain(t *testing.T) {
	s := happy()
	r := s.plan()
	if !r.Landable || r.Result != ResultLandable || len(r.Reasons) != 0 {
		t.Fatalf("landable=%v result=%s reasons=%v", r.Landable, r.Result, reasonCodes(r))
	}
	if !r.ApplyAllowed {
		t.Fatal("apply must be allowed in human mode when every precondition holds")
	}
	if len(r.Chain) != 3 || r.Chain[0].Number != 11 || r.Chain[2].Number != 13 {
		t.Fatalf("chain %+v", r.Chain)
	}
	if r.DefaultTip != sha('0') || r.PlanDigest == "" || r.Repository != "o/r" || r.Command != "land" || r.Operation != "plan" {
		t.Fatalf("report %+v", r)
	}
	var push []string
	for _, e := range r.Effects {
		if e.Kind == "push" {
			push = e.Argv
		}
	}
	want := []string{"git", "push", "--porcelain", "origin", sha('3') + ":refs/heads/main"}
	if strings.Join(push, " ") != strings.Join(want, " ") {
		t.Fatalf("push argv %v", push)
	}
}

func TestPlanAgentModeNeverAllowsApply(t *testing.T) {
	s := happy()
	s.mode = agentMode
	r := s.plan()
	if !r.Landable {
		t.Fatalf("an agent may plan: %v", reasonCodes(r))
	}
	if r.ApplyAllowed || r.Mode.Mode != "agent" {
		t.Fatalf("apply_allowed=%v mode=%s", r.ApplyAllowed, r.Mode.Mode)
	}
}

func TestPlanDigestBindsTheChain(t *testing.T) {
	a, b := happy().plan(), happy()
	b.git.tip = sha('0')
	b.opts.Chain[1].Reviewed = sha('9') // a different reviewed head changes the digest even when refused
	b.forge.pulls[12] = pull(12, sha('9'), "feat-2")
	if a.PlanDigest == b.plan().PlanDigest {
		t.Fatal("digest equal for different heads")
	}
	if a.PlanDigest != happy().plan().PlanDigest {
		t.Fatal("digest not deterministic")
	}
}

func TestPlanWritesNothing(t *testing.T) {
	s := happy()
	s.plan()
	for _, c := range s.git.calls {
		switch c {
		case "remote-repo", "ls-remote", "fetch", "is-ancestor", "range":
		default:
			if !strings.HasPrefix(c, "resolve ") {
				t.Errorf("unexpected git call %q", c)
			}
		}
	}
	if s.git.fetched != 1 {
		t.Errorf("fetch ran %d times, want 1 (the one disclosed local effect)", s.git.fetched)
	}
}

type want struct {
	code string
	n    int
}

func TestPlanRefusals(t *testing.T) {
	h1, h2, h3 := sha('1'), sha('2'), sha('3')
	cases := []struct {
		name string
		edit func(*scenario)
		want []want
	}{
		{"head changed", func(s *scenario) { s.forge.pulls[12] = pull(12, sha('9'), "feat-2") }, []want{{ReasonHeadChanged, 12}}},
		{"head changed and fetched head agrees with the API", func(s *scenario) {
			s.forge.pulls[12] = pull(12, sha('9'), "feat-2")
			s.git.refs[12] = sha('9')
			s.git.lin = []string{sha('0'), h1, h2, h3, sha('9')}
		}, []want{{ReasonHeadChanged, 12}}},
		{"fork", func(s *scenario) { p := s.forge.pulls[12]; p.HeadRepo = "evil/r"; s.forge.pulls[12] = p }, []want{{ReasonFork, 12}}},
		{"closed", func(s *scenario) { p := s.forge.pulls[12]; p.State = "closed"; s.forge.pulls[12] = p }, []want{{ReasonNotOpen, 12}}},
		{"merged other sha", func(s *scenario) {
			p := s.forge.pulls[12]
			p.State, p.Merged, p.MergeCommit = "closed", true, sha('8')
			s.forge.pulls[12] = p
		}, []want{{ReasonAlreadyMerged, 12}}},
		{"draft", func(s *scenario) { p := s.forge.pulls[12]; p.Draft = true; s.forge.pulls[12] = p }, []want{{ReasonDraft, 12}}},
		{"conflicting", func(s *scenario) {
			p := s.forge.pulls[12]
			p.Mergeable = boolp(false)
			p.MergeableState = "dirty"
			s.forge.pulls[12] = p
		}, []want{{ReasonConflicting, 12}}},
		{"mergeable null", func(s *scenario) { p := s.forge.pulls[12]; p.Mergeable = nil; s.forge.pulls[12] = p }, []want{{ReasonMergeabilityNone, 12}}},
		{"not linear", func(s *scenario) { s.git.lin = []string{sha('0'), h1, h3, h2} }, []want{{ReasonChainNotLinear, 0}}},
		{"object missing", func(s *scenario) { s.git.lin = []string{sha('0'), h1, h3} }, []want{{ReasonObjectMissing, 0}}},
		{"base not in chain", func(s *scenario) { p := s.forge.pulls[12]; p.BaseRef = "other"; s.forge.pulls[12] = p }, []want{{ReasonBaseNotInChain, 12}}},
		{"native stack shape", func(s *scenario) { p := s.forge.pulls[12]; p.BaseRef = "feat-1"; s.forge.pulls[12] = p }, []want{{ReasonLandUnmeasured, 12}}},
		{"default branch moved", func(s *scenario) {
			s.git.tip = sha('c')
			s.git.lin = []string{sha('0'), h1, h2, h3, sha('c')}
			s.git.diverged = map[[2]string]bool{{sha('c'), h1}: true, {h3, sha('c')}: true}
		}, []want{{ReasonNotFastForward, 0}}},
		{"side child", func(s *scenario) { s.forge.open = []OpenPull{{Number: 20, BaseRef: "feat-2", HeadRef: "x"}} }, []want{{ReasonSideChild, 20}}},
		{"side children unknown", func(s *scenario) { s.forge.openTrunc = true }, []want{{ReasonSideChildUnknown, 0}}},
		{"tip checks failing", func(s *scenario) {
			s.forge.checks[h3] = CheckFacts{Contexts: map[string]CheckState{"ci/all": CheckFailure}}
		}, []want{{ReasonChecksNotGreen, 13}}},
		{"tip required missing", func(s *scenario) {
			s.forge.checks[h3] = CheckFacts{Contexts: map[string]CheckState{"light": CheckSuccess}}
		}, []want{{ReasonChecksNotGreen, 13}}},
		{"tip checks pending", func(s *scenario) {
			s.forge.checks[h3] = CheckFacts{Contexts: map[string]CheckState{"ci/all": CheckPending}}
		}, []want{{ReasonTipChecksPending, 13}}},
		{"lower checks pending", func(s *scenario) {
			s.forge.checks[h2] = CheckFacts{Contexts: map[string]CheckState{"light": CheckPending}}
		}, []want{{ReasonChecksPending, 12}}},
		{"lower checks failing", func(s *scenario) {
			s.forge.checks[h1] = CheckFacts{Contexts: map[string]CheckState{"light": CheckFailure}}
		}, []want{{ReasonChecksNotGreen, 11}}},
		{"empty rollup after github head change", func(s *scenario) {
			s.forge.checks[h2] = CheckFacts{}
			s.forge.events[12] = []Event{{Name: "head_ref_force_pushed", Actor: "github-actions"}}
		}, []want{{ReasonHeadChangedNoChk, 12}}},
		{"empty rollup after owner force push", func(s *scenario) {
			s.forge.checks[h2] = CheckFacts{}
			s.forge.events[12] = []Event{{Name: "head_ref_force_pushed", Actor: "wstein"}}
		}, []want{{ReasonChecksMissing, 12}}},
		{"empty rollup no event", func(s *scenario) { s.forge.checks[h1] = CheckFacts{} }, []want{{ReasonChecksMissing, 11}}},
		{"required unreadable", func(s *scenario) { s.forge.required = RequiredFacts{} }, []want{{ReasonChecksUnknown, 13}}},
		{"range too large", func(s *scenario) { s.git.extra = 5; s.opts.RangeBound = 3 }, []want{{ReasonRangeTooLarge, 0}}},
		{"range mismatch", func(s *scenario) {
			s.git.rangeHook = func(from, to string, out []Commit) []Commit {
				if from == sha('0') && to == sha('3') {
					return append(out, Commit{SHA: sha('e'), AuthorName: ownerName, AuthorEmail: ownerEmail, CommitterName: ownerName, CommitterEmail: ownerEmail})
				}
				return out
			}
		}, []want{{ReasonRangeMismatch, 0}}},
		{"foreign author", func(s *scenario) { s.git.ident[h2] = [4]string{"Mallory", "m@example.org", ownerName, ownerEmail} }, []want{{ReasonIdentityMismatch, 0}}},
		{"foreign committer", func(s *scenario) { s.git.ident[h1] = [4]string{ownerName, ownerEmail, "GitHub", "noreply@github.com"} }, []want{{ReasonIdentityMismatch, 0}}},
		{"foreign author email only", func(s *scenario) { s.git.ident[h3] = [4]string{ownerName, "other@example.org", ownerName, ownerEmail} }, []want{{ReasonIdentityMismatch, 0}}},
		{"foreign author name only", func(s *scenario) { s.git.ident[h3] = [4]string{"Other", ownerEmail, ownerName, ownerEmail} }, []want{{ReasonIdentityMismatch, 0}}},
		{"foreign committer email only", func(s *scenario) { s.git.ident[h3] = [4]string{ownerName, ownerEmail, ownerName, "other@example.org"} }, []want{{ReasonIdentityMismatch, 0}}},
		{"policy missing", func(s *scenario) { s.policy = Policy{} }, []want{{ReasonIdentityPolicy, 0}}},
		{"remote is another repository", func(s *scenario) { s.git.remoteRepo = "x/y" }, []want{{ReasonRemoteMismatch, 0}}},
		{"remote unrecognized", func(s *scenario) { s.git.remoteRepo = "" }, []want{{ReasonRemoteMismatch, 0}}},
		{"pull read fails", func(s *scenario) { s.forge.pullErr = map[int]error{12: errors.New("boom")} }, []want{{ReasonReadFailed, 12}}},
		{"ruleset forbids", func(s *scenario) {
			s.forge.rulesets.BypassModes = []string{"pull_request"}
		}, []want{{ReasonRulesetForbids, 0}}},
		{"ruleset without bypass", func(s *scenario) { s.forge.rulesets.BypassModes = nil }, []want{{ReasonRulesetForbids, 0}}},
		{"ruleset unreadable", func(s *scenario) { s.forge.rulesets = RulesetFacts{Protection: "unreadable"} }, []want{{ReasonLandUnmeasured, 0}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := happy()
			c.edit(s)
			r := s.plan()
			if r.Landable || r.ApplyAllowed || r.Result != ResultRefused {
				t.Fatalf("landable=%v apply=%v result=%s", r.Landable, r.ApplyAllowed, r.Result)
			}
			for _, w := range c.want {
				if !has(r, w.code, w.n) {
					t.Errorf("missing %s#%d in %v", w.code, w.n, reasonCodes(r))
				}
			}
		})
	}
}

func TestPlanUnrecordedShapeIsUnmeasuredAndListedOnce(t *testing.T) {
	s := newScenario() // no recorded shape: the allowlist is empty
	r := s.plan()
	if r.Landable || r.ApplyAllowed || !has(r, ReasonLandUnmeasured, 0) {
		t.Fatalf("reasons %v", reasonCodes(r))
	}
	n := 0
	for _, x := range r.Reasons {
		if x.Code == ReasonLandUnmeasured {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("land-unmeasured listed %d times", n)
	}
	// the shape recorded for another stack setting does not match
	s.opts.Shapes = NewShapeSet(ShapeOf(s.forge.repo, s.forge.rulesets, false).Key())
	if has2 := has(s.plan(), ReasonLandUnmeasured, 0); !has2 {
		t.Fatal("a shape measured for the single landing must not allow the chain")
	}
}

func TestPlanListsEveryFailingPrecondition(t *testing.T) {
	s := happy()
	s.forge.pulls[12] = pull(12, sha('9'), "feat-2")
	s.forge.checks[sha('3')] = CheckFacts{Contexts: map[string]CheckState{"ci/all": CheckFailure}}
	s.policy = Policy{}
	r := s.plan()
	for _, w := range []want{{ReasonHeadChanged, 12}, {ReasonChecksNotGreen, 13}, {ReasonIdentityPolicy, 0}} {
		if !has(r, w.code, w.n) {
			t.Errorf("missing %s#%d in %v", w.code, w.n, reasonCodes(r))
		}
	}
}

func TestPlanDraftNeedsAckDraft(t *testing.T) {
	s := happy()
	p := s.forge.pulls[12]
	p.Draft = true
	s.forge.pulls[12] = p
	s.opts.AckDraft = true
	if r := s.plan(); !r.Landable {
		t.Fatalf("acknowledged draft refused: %v", reasonCodes(r))
	}
}

func TestPlanRebaseableIsRecordedNotRequired(t *testing.T) {
	s := happy()
	p := s.forge.pulls[12]
	p.Rebaseable = boolp(false)
	s.forge.pulls[12] = p
	r := s.plan()
	if !r.Landable {
		t.Fatalf("rebaseable false must not refuse a fast-forward push: %v", reasonCodes(r))
	}
	found := false
	for _, p := range r.Preconditions {
		if p.ID == "rebaseable" && p.Number == 12 && p.Value == "false" && p.OK {
			found = true
		}
	}
	if !found {
		t.Fatal("rebaseable not recorded")
	}
}

func TestPlanReviewEvidenceIsInformationOnly(t *testing.T) {
	s := happy()
	s.forge.reviews[11] = nil                                                                // none
	s.forge.revErr = map[int]error{12: errors.New("forbidden")}                              // unknown
	s.forge.reviews[13] = []Review{{Reviewer: "rev", State: "APPROVED", CommitID: sha('7')}} // other commit: none
	r := s.plan()
	if !r.Landable {
		t.Fatalf("review evidence must never refuse: %v", reasonCodes(r))
	}
	got := map[int]string{}
	for _, c := range r.Chain {
		got[c.Number] = c.ReviewEvidence
	}
	if got[11] != ReviewNone || got[12] != ReviewUnknown || got[13] != ReviewNone {
		t.Fatalf("evidence %v", got)
	}
	s = happy()
	if r := s.plan(); r.Chain[0].ReviewEvidence != ReviewFound {
		t.Fatalf("found: %v", r.Chain[0].ReviewEvidence)
	}
	s.policy.Reviewers = []string{"someone-else"}
	if r := s.plan(); r.Chain[0].ReviewEvidence != ReviewNone {
		t.Fatalf("a reviewer outside the policy list must not count: %v", r.Chain[0].ReviewEvidence)
	}
}

func TestPlanReplayAlreadyLanded(t *testing.T) {
	s := happy()
	// the default branch already is the tip; GitHub marked every pull request merged with its own head
	s.git.tip = sha('3')
	for _, n := range []int{11, 12, 13} {
		p := s.forge.pulls[n]
		p.State, p.Merged, p.MergeCommit = "closed", true, p.HeadSHA
		s.forge.pulls[n] = p
	}
	r := s.plan()
	if r.Result != ResultAlreadyLanded || r.Landable {
		t.Fatalf("result %s reasons %v", r.Result, reasonCodes(r))
	}
	if r.ApplyAllowed {
		t.Fatal("nothing to apply after a landing")
	}
	for _, e := range r.Effects {
		if e.Kind == "push" {
			t.Fatal("replay must plan no push")
		}
	}
}

func TestPlanReplayLandedNotMarked(t *testing.T) {
	s := happy()
	s.git.tip = sha('3')
	r := s.plan()
	if r.Result != ResultLandedNotMarked || r.ApplyAllowed {
		t.Fatalf("result %s apply=%v", r.Result, r.ApplyAllowed)
	}
	for _, e := range r.Effects {
		if e.Kind == "push" {
			t.Fatal("replay must plan no push")
		}
	}
}

func TestPlanRepositoryReadFailureRefuses(t *testing.T) {
	s := happy()
	s.forge.repoErr = errors.New("boom")
	r := s.plan()
	if r.Landable || !has(r, ReasonReadFailed, 0) {
		t.Fatalf("%v", reasonCodes(r))
	}
}

func TestPlanDetailIsBounded(t *testing.T) {
	s := happy()
	s.forge.pullErr = map[int]error{12: errors.New(strings.Repeat("x", 5000) + "\x1b[31m")}
	r := s.plan()
	for _, x := range r.Reasons {
		if len(x.Detail) > 400 || strings.ContainsRune(x.Detail, 0x1b) {
			t.Fatalf("unbounded or unescaped detail %q", x.Detail)
		}
	}
}

func TestPlanUnreadableRulesetsAreNamedUnreadable(t *testing.T) {
	s := happy()
	s.forge.rulesets = RulesetFacts{Protection: "unreadable"}
	r := s.plan()
	for _, p := range r.Preconditions {
		if p.ID == "ruleset" && p.OK == false && p.Value == "unreadable" {
			return
		}
	}
	t.Fatalf("the unreadable shape must be reported as unreadable, not as an unrecorded key: %+v", r.Preconditions)
}
