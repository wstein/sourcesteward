// SPDX-License-Identifier: EUPL-1.2

package stackpublish

import (
	"reflect"
	"strings"
	"testing"
)

func owner() Identity { return goodPolicy().Owner }

func commitOf(c byte, paths ...string) Commit {
	return Commit{ID: sha(c), Author: owner(), Committer: owner(), Paths: paths, PathsKnown: true}
}

func goodPublication() PublicationInput {
	return PublicationInput{Policy: goodPolicy(), Backend: "git", Repository: "owner/project", Task: "spec016-slice2", Rewrite: true, NewHead: sha('f'), AuthorID: "claude-author",
		Range:     &RangeEvidence{Default: sha('1'), Complete: true, Commits: []Commit{commitOf('e', "docs/a.md"), commitOf('f', "docs/b.md")}},
		Moved:     &MovedScopeSource{Spec: goodPolicy().RestackReceipts[0], Backend: "git", Repository: "owner/project", Task: "spec016-slice2", ReviewGateHead: sha('f'), TargetNewHead: sha('f'), ChangeClass: "docs-only", Complete: true},
		RangeDiff: &RangeDiff{Complete: true, OldCount: 2, NewCount: 2, Rows: []string{"=", "="}},
		Checks:    &LocalChecks{Known: true, Green: true},
		Config:    &ConfigEvidence{Complete: true, Origins: []ConfigOrigin{{"global", "user.name", owner().Name}, {"global", "user.email", owner().Email}}},
		Signing:   &SigningEvidence{Mode: SigningUnsigned, SourceKnown: true, FinalHead: sha('f')}}
}

func codePublication() PublicationInput {
	in := goodPublication()
	in.Range.Commits = []Commit{commitOf('e', "internal/x.go"), commitOf('f', "internal/y_test.go")}
	in.Moved.ChangeClass = "code"
	in.Review = &ReviewEvidence{Source: "pr-review", ReviewerID: "opus-reviewer", Verdict: VerdictClear, SubjectSHA: sha('f'), EvidenceDigest: strings.Repeat("e", 64), Locator: "pull/2#review-1"}
	return in
}

func pubBlockers(r PublicationReceipt) []string {
	return refusals(r.Blockers)
}

func TestClassifyPathsFollowsTheProseAllowList(t *testing.T) {
	docs := []string{"NOTES.md", "CHANGES.md", "docs/fixture-page.md", "specs/016-fixture/spec-text.md", "specs/016-x/checklists/fixture-list.md"}
	if ClassifyPaths(docs, true) != ClassDocsOnly {
		t.Fatal("prose paths are docs-only")
	}
	for _, p := range docs {
		if ClassifyPaths([]string{p}, true) != ClassDocsOnly {
			t.Errorf("%s", p)
		}
	}
	for _, p := range []string{"internal/x.go", "specs/016-x/contracts/a.md", "specs/016-x/examples/a.md", "specs/016-x/tasks/a.md", "docs/sub/a.md", "docs/a.json", "docs/index.html", "scripts/x.py",
		".github/workflows/ci.yml", "NOTES.MD", "a.md.go", "go.mod", "../NOTES.md", "/NOTES.md", "./NOTES.md", "docs//a.md", "a\\b.md", "docs/a\x00.md", "docs/a\x7f.md", "specs/../x.md", "specs/./x.md", "", "specs/a.md", "docs/.md"} {
		if ClassifyPaths([]string{p}, true) != ClassCode {
			t.Errorf("%q must be code", p)
		}
		if ClassifyPaths([]string{"NOTES.md", p}, true) != ClassCode {
			t.Errorf("a docs path plus %q must be code", p)
		}
	}
	if ClassifyPaths(nil, true) != ClassCode || ClassifyPaths([]string{}, true) != ClassCode {
		t.Error("an empty path set proves nothing")
	}
	if ClassifyPaths(docs, false) != ClassCode {
		t.Error("unknown path evidence is code")
	}
}

func TestPublicationReceiptForADocsRewriteNeedsNoReview(t *testing.T) {
	for name, mutate := range map[string]func(*PublicationInput){
		"pure rebase":   func(in *PublicationInput) {},
		"other rewrite": func(in *PublicationInput) { in.RangeDiff.Rows = []string{"=", "!"} },
	} {
		in := goodPublication()
		mutate(&in)
		r := EvaluatePublication(in)
		if !r.Allowed() || r.Class != ClassDocsOnly || r.ReviewRequired || r.Status() != StatusComplete || len(r.Blockers) != 0 {
			t.Errorf("%s: %+v", name, r)
		}
	}
}

func TestEffectiveClassIsCodeUnlessBothAreProvedDocsOnly(t *testing.T) {
	cases := map[string]struct {
		mutate func(*PublicationInput)
		moved  ChangeClass
		rng    ChangeClass
		want   ChangeClass
	}{
		"docs child above an open code parent": {func(in *PublicationInput) { in.Range.Commits[0] = commitOf('e', "internal/parent.go") }, ClassDocsOnly, ClassCode, ClassCode},
		"parent code removed again by a later commit": {func(in *PublicationInput) {
			in.Range.Commits = []Commit{commitOf('d', "internal/gone.go"), commitOf('e', "internal/gone.go", "docs/a.md"), commitOf('f', "docs/b.md")}
		}, ClassDocsOnly, ClassCode, ClassCode},
		"unknown parent path evidence":         {func(in *PublicationInput) { in.Range.Commits[0].PathsKnown = false }, ClassDocsOnly, ClassCode, ClassCode},
		"moved scope is code, range is docs":   {func(in *PublicationInput) { in.Moved.ChangeClass = "code" }, ClassCode, ClassDocsOnly, ClassCode},
		"moved scope class unrecognized":       {func(in *PublicationInput) { in.Moved.ChangeClass = "mostly-docs" }, ClassCode, ClassDocsOnly, ClassCode},
		"moved scope receipt for another head": {func(in *PublicationInput) { in.Moved.ReviewGateHead = sha('9') }, ClassCode, ClassDocsOnly, ClassCode},
		"moved scope absent for a rewrite":     {func(in *PublicationInput) { in.Moved = nil }, ClassCode, ClassDocsOnly, ClassCode},
		"both docs":                            {func(in *PublicationInput) {}, ClassDocsOnly, ClassDocsOnly, ClassDocsOnly},
		"new topic has no moved scope": {func(in *PublicationInput) {
			in.Rewrite, in.Moved, in.RangeDiff = false, nil, nil
			in.Review = codePublication().Review
		}, "", ClassDocsOnly, ClassDocsOnly},
	}
	for name, c := range cases {
		in := goodPublication()
		c.mutate(&in)
		r := EvaluatePublication(in)
		if r.MovedScopeClass != c.moved || r.RangeClass != c.rng || r.Class != c.want {
			t.Errorf("%s: moved %q range %q class %q, want %q %q %q", name, r.MovedScopeClass, r.RangeClass, r.Class, c.moved, c.rng, c.want)
		}
	}
}

func TestCodeRewriteTierMatrix(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*PublicationInput)
		tier   ReviewTier
		pure   bool
		want   []string
	}{
		{"pure rebase, strongest reviewer", func(in *PublicationInput) {}, TierMiddle, true, nil},
		{"pure rebase, middle reviewer", func(in *PublicationInput) { in.Review.ReviewerID = "sonnet-reviewer" }, TierMiddle, true, nil},
		{"pure rebase, helper reviewer", func(in *PublicationInput) { in.Review.ReviewerID = "haiku-helper" }, TierMiddle, true, []string{"review-missing/review-tier-too-low"}},
		{"other rewrite, strongest reviewer", func(in *PublicationInput) { in.RangeDiff.Rows = []string{"=", "!"} }, TierStrongest, false, nil},
		{"other rewrite, middle reviewer", func(in *PublicationInput) {
			in.RangeDiff.Rows = []string{"=", "!"}
			in.Review.ReviewerID = "sonnet-reviewer"
		}, TierStrongest, false, []string{"review-missing/review-tier-too-low"}},
		{"unknown class is never the cheap row", func(in *PublicationInput) {
			in.Range.Commits[0].PathsKnown = false
			in.Review.ReviewerID = "sonnet-reviewer"
		}, TierStrongest, true, []string{"review-missing/review-tier-too-low"}},
		{"no review", func(in *PublicationInput) { in.Review = nil }, TierMiddle, true, []string{"review-missing/review-absent"}},
		{"review of the predicted or older sha", func(in *PublicationInput) { in.Review.SubjectSHA = sha('9') }, TierMiddle, true, []string{"review-missing/review-wrong-head"}},
		{"review not clear", func(in *PublicationInput) { in.Review.Verdict = VerdictNotClear }, TierMiddle, true, []string{"review-missing/review-not-clear"}},
		{"author reviewing itself", func(in *PublicationInput) { in.AuthorID = "opus-reviewer" }, TierMiddle, true, []string{"review-missing/review-not-independent"}},
		{"range diff has a changed row", func(in *PublicationInput) {
			in.RangeDiff.Rows = []string{"=", "!"}
			in.Review.ReviewerID = "sonnet-reviewer"
		}, TierStrongest, false, []string{"review-missing/review-tier-too-low"}},
	} {
		in := codePublication()
		tc.mutate(&in)
		r := EvaluatePublication(in)
		if r.Class != ClassCode || !r.ReviewRequired || r.MinTier != tc.tier || r.PureRebase != tc.pure || !reflect.DeepEqual(pubBlockers(r), append([]string{}, tc.want...)) {
			t.Errorf("%s: class %s required %v tier %s pure %v blockers %v", tc.name, r.Class, r.ReviewRequired, r.MinTier, r.PureRebase, pubBlockers(r))
		}
	}
}

// Review F3: AGENTS.md "Push early": a new topic goes up as a Draft once the author's checks pass; the
// review belongs to Ready. Only a REWRITE of a pushed branch needs its review before the push.
func TestAFirstPushOfANewTopicNeedsNoReviewBeforeThePush(t *testing.T) {
	for _, class := range []string{"docs", "code"} {
		in := codePublication() // this PR's own first push: a new topic with code
		in.Rewrite, in.Moved, in.RangeDiff, in.Review = false, nil, nil, nil
		if class == "docs" {
			in.Range.Commits = []Commit{commitOf('f', "docs/a.md")}
		}
		r := EvaluatePublication(in)
		if !r.Allowed() || r.ReviewRequired || r.MinTier != "" || r.MovedScopeClass != "" {
			t.Errorf("%s: %+v %v", class, r, pubBlockers(r))
		}
		// The other pre-push gates still apply to a new topic.
		in.Checks = &LocalChecks{Known: true}
		if got := pubBlockers(EvaluatePublication(in)); !reflect.DeepEqual(got, []string{"checks-not-green/local-checks-red"}) {
			t.Errorf("%s: %v", class, got)
		}
		in.Checks = goodPublication().Checks
		in.Range.Commits[0].Author = Identity{Name: "x", Email: "y"}
		if got := pubBlockers(EvaluatePublication(in)); !reflect.DeepEqual(got, []string{"identity-mismatch/foreign-identity"}) {
			t.Errorf("%s: %v", class, got)
		}
	}
}

func TestPureRebaseNeedsEveryRowEqual(t *testing.T) {
	for name, c := range map[string]struct {
		d    RangeDiff
		want bool
	}{
		"all equal":                  {RangeDiff{Complete: true, OldCount: 3, NewCount: 3, Rows: []string{"=", "=", "="}}, true},
		"one changed":                {RangeDiff{Complete: true, OldCount: 3, NewCount: 3, Rows: []string{"=", "!", "="}}, false},
		"unknown symbol":             {RangeDiff{Complete: true, OldCount: 2, NewCount: 2, Rows: []string{"=", "?"}}, false},
		"added commit":               {RangeDiff{Complete: true, OldCount: 2, NewCount: 3, Rows: []string{"=", "=", ">"}}, false},
		"dropped commit":             {RangeDiff{Complete: true, OldCount: 3, NewCount: 2, Rows: []string{"=", "="}}, false},
		"added commit without a row": {RangeDiff{Complete: true, OldCount: 2, NewCount: 3, Rows: []string{"=", "="}}, false},
		"missing row":                {RangeDiff{Complete: true, OldCount: 3, NewCount: 3, Rows: []string{"=", "="}}, false},
		"extra row":                  {RangeDiff{Complete: true, OldCount: 2, NewCount: 2, Rows: []string{"=", "=", "="}}, false},
		"incomplete":                 {RangeDiff{Complete: false, OldCount: 2, NewCount: 2, Rows: []string{"=", "="}}, false},
		"empty":                      {RangeDiff{Complete: true}, false},
		"empty string row":           {RangeDiff{Complete: true, OldCount: 1, NewCount: 1, Rows: []string{""}}, false},
	} {
		in := codePublication()
		in.RangeDiff = &RangeDiff{Complete: c.d.Complete, OldCount: c.d.OldCount, NewCount: c.d.NewCount, Rows: c.d.Rows}
		if got := EvaluatePublication(in).PureRebase; got != c.want {
			t.Errorf("%s: %v want %v", name, got, c.want)
		}
	}
	in := codePublication()
	in.RangeDiff = nil
	if EvaluatePublication(in).PureRebase {
		t.Error("absent range-diff evidence is not a pure rebase")
	}
	in = codePublication()
	in.Rewrite = false
	if EvaluatePublication(in).PureRebase {
		t.Error("a non-rewrite is not a pure rebase")
	}
}

func TestLocalChecksMustBeKnownAndGreen(t *testing.T) {
	for name, c := range map[string]struct {
		checks *LocalChecks
		want   string
	}{"absent": {nil, "checks-unknown/local-checks-unknown"}, "unknown": {&LocalChecks{Known: false, Green: true}, "checks-unknown/local-checks-unknown"}, "red": {&LocalChecks{Known: true}, "checks-not-green/local-checks-red"}} {
		in := goodPublication()
		in.Checks = c.checks
		if got := pubBlockers(EvaluatePublication(in)); !reflect.DeepEqual(got, []string{c.want}) {
			t.Errorf("%s: %v", name, got)
		}
	}
}

func TestRangeAndIdentityGates(t *testing.T) {
	foreign := Identity{Name: "Mallory", Email: "m@example.org"}
	cases := map[string]struct {
		mutate func(*PublicationInput)
		want   []string
	}{
		"policy not loaded":              {func(in *PublicationInput) { in.Policy.Loaded = false }, []string{"identity-mismatch/policy-missing"}},
		"owner identity missing":         {func(in *PublicationInput) { in.Policy.Owner = Identity{} }, []string{"identity-mismatch/owner-missing"}},
		"range absent":                   {func(in *PublicationInput) { in.Range = nil }, []string{"identity-mismatch/range-absent"}},
		"range incomplete":               {func(in *PublicationInput) { in.Range.Complete = false }, []string{"identity-mismatch/range-incomplete"}},
		"range empty":                    {func(in *PublicationInput) { in.Range.Commits = nil }, []string{"identity-mismatch/range-empty"}},
		"default not a full sha":         {func(in *PublicationInput) { in.Range.Default = "main" }, []string{"identity-mismatch/default-invalid"}},
		"tip is not the new head":        {func(in *PublicationInput) { in.Range.Commits[1].ID = sha('9') }, []string{"identity-mismatch/range-head-mismatch"}},
		"new head not a full sha":        {func(in *PublicationInput) { in.NewHead = "abc" }, []string{"identity-mismatch/range-head-mismatch"}},
		"commit id not a full sha":       {func(in *PublicationInput) { in.Range.Commits[0].ID = "abc" }, []string{"identity-mismatch/range-commit-invalid"}},
		"foreign author in the parent":   {func(in *PublicationInput) { in.Range.Commits[0].Author = foreign }, []string{"identity-mismatch/foreign-identity"}},
		"foreign committer in the child": {func(in *PublicationInput) { in.Range.Commits[1].Committer = foreign }, []string{"identity-mismatch/foreign-identity"}},
		"owner name differs by case":     {func(in *PublicationInput) { in.Range.Commits[1].Author.Name = strings.ToUpper(owner().Name) }, []string{"identity-mismatch/foreign-identity"}},
		"owner email missing":            {func(in *PublicationInput) { in.Policy.Owner.Email = "" }, []string{"identity-mismatch/owner-missing"}},
		"owner email differs":            {func(in *PublicationInput) { in.Range.Commits[1].Author.Email = "x" + owner().Email }, []string{"identity-mismatch/foreign-identity"}},
		"empty identity":                 {func(in *PublicationInput) { in.Range.Commits[0].Committer = Identity{} }, []string{"identity-mismatch/foreign-identity"}},
		"co-author does not count": {func(in *PublicationInput) {
			in.Range.Commits[1].Author = Identity{Name: "Claude", Email: "noreply@anthropic.com"}
		}, []string{"identity-mismatch/foreign-identity"}},
		"config absent":     {func(in *PublicationInput) { in.Config = nil }, []string{"identity-config-foreign/config-origin-unknown"}},
		"config incomplete": {func(in *PublicationInput) { in.Config.Complete = false }, []string{"identity-config-foreign/config-origin-unknown"}},
		"foreign user.name in a worktree": {func(in *PublicationInput) {
			in.Config.Origins = append(in.Config.Origins, ConfigOrigin{"worktree", "user.name", "t"})
		}, []string{"identity-config-foreign/config-foreign"}},
		"foreign user.email local": {func(in *PublicationInput) {
			in.Config.Origins = append(in.Config.Origins, ConfigOrigin{"local", "user.email", "t@t"})
		}, []string{"identity-config-foreign/config-foreign"}},
		"other key in the evidence": {func(in *PublicationInput) {
			in.Config.Origins = append(in.Config.Origins, ConfigOrigin{"local", "user.signingkey", "k"})
		}, []string{"identity-config-foreign/config-key-not-allowed"}},
		"origin without a scope": {func(in *PublicationInput) {
			in.Config.Origins = append(in.Config.Origins, ConfigOrigin{"", "user.name", owner().Name})
		}, []string{"identity-config-foreign/config-origin-unknown"}},
		"signing evidence absent":  {func(in *PublicationInput) { in.Signing = nil }, []string{"signature-policy-unknown/signing-evidence-absent"}},
		"source signature dropped": {func(in *PublicationInput) { in.Signing.SourceSigned = []string{sha('1')} }, []string{"signature-policy-unknown/signature-dropped"}},
	}
	for name, c := range cases {
		in := goodPublication()
		c.mutate(&in)
		r := EvaluatePublication(in)
		if r.Allowed() || !reflect.DeepEqual(pubBlockers(r), c.want) || r.Status() != StatusRefused {
			t.Errorf("%s: allowed=%v %v want %v", name, r.Allowed(), pubBlockers(r), c.want)
		}
	}
}

func TestEvidenceListsAreValidatedAndBounded(t *testing.T) {
	cases := map[string]struct {
		mutate func(*PublicationInput)
		want   []string
	}{
		"duplicate commit id": {func(in *PublicationInput) {
			in.Range.Commits = append([]Commit{in.Range.Commits[1]}, in.Range.Commits...)
		}, []string{"identity-mismatch/range-commit-duplicate"}},
		"too many paths in one commit": {func(in *PublicationInput) { in.Range.Commits[0].Paths = manyPaths(1001) }, []string{"bound-exceeded/paths-too-many"}},
		"too many origins":             {func(in *PublicationInput) { in.Config.Origins = manyOrigins(1001) }, []string{"bound-exceeded/config-too-many"}},
	}
	for name, c := range cases {
		in := goodPublication()
		c.mutate(&in)
		if got := pubBlockers(EvaluatePublication(in)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %v want %v", name, got, c.want)
		}
	}
	in := goodPublication()
	in.Range.Commits[0].Paths = manyPaths(1000)
	in.Config.Origins = manyOrigins(1000)
	if r := EvaluatePublication(in); r.RangeClass != ClassDocsOnly || !r.Allowed() {
		t.Errorf("the limits themselves are allowed: %v", pubBlockers(r))
	}
}

func manyPaths(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "docs/p" + strings.Repeat("x", i%5) + string(rune('a'+i%26)) + ".md"
	}
	return out
}

func manyOrigins(n int) []ConfigOrigin {
	out := make([]ConfigOrigin, n)
	for i := range out {
		out[i] = ConfigOrigin{"global", "user.name", owner().Name}
	}
	return out
}

func TestMoreThanTwoHundredCommitsAreRefused(t *testing.T) {
	in := goodPublication()
	in.Range.Commits = nil
	for i := 0; i < 201; i++ {
		c := commitOf('a', "docs/a.md")
		in.Range.Commits = append(in.Range.Commits, c)
	}
	in.Range.Commits[200].ID = sha('f')
	if got := pubBlockers(EvaluatePublication(in)); !reflect.DeepEqual(got, []string{"bound-exceeded/identity-range-too-long"}) {
		t.Fatalf("%v", got)
	}
	in.Range.Commits = in.Range.Commits[1:]
	if r := EvaluatePublication(in); !r.Allowed() {
		t.Fatalf("200 commits are within the bound: %v", pubBlockers(r))
	}
}

func TestMovedScopeAdapterValidatesProducerKindVersionScopeAndHead(t *testing.T) {
	p, head := goodPolicy(), sha('f')
	src := func() *MovedScopeSource { return goodPublication().Moved }
	if c, r := AdaptMovedScope(p, "git", "owner/project", "spec016-slice2", head, src()); c != ClassDocsOnly || r != nil {
		t.Fatalf("%v %v", c, r)
	}
	for name, mutate := range map[string]func(*MovedScopeSource){
		"fabricated producer":    func(s *MovedScopeSource) { s.Spec.Producer = "someone" },
		"wrong command":          func(s *MovedScopeSource) { s.Spec.Command = "restack plan" },
		"hidden v1 kind":         func(s *MovedScopeSource) { s.Spec.Kind = "restack-plan" },
		"wrong version":          func(s *MovedScopeSource) { s.Spec.SchemaVersion = 1 },
		"other backend":          func(s *MovedScopeSource) { s.Backend = "jj" },
		"other repository":       func(s *MovedScopeSource) { s.Repository = "owner/other" },
		"other task":             func(s *MovedScopeSource) { s.Task = "someone-elses" },
		"review gate head":       func(s *MovedScopeSource) { s.ReviewGateHead = sha('9') },
		"target new head":        func(s *MovedScopeSource) { s.TargetNewHead = sha('9') },
		"short head":             func(s *MovedScopeSource) { s.ReviewGateHead, s.TargetNewHead = "ffffff", "ffffff" },
		"incomplete moved scope": func(s *MovedScopeSource) { s.Complete = false },
		"top-level class value":  func(s *MovedScopeSource) { s.ChangeClass = "effective" },
		"empty class":            func(s *MovedScopeSource) { s.ChangeClass = "" },
	} {
		s := src()
		mutate(s)
		c, r := AdaptMovedScope(p, "git", "owner/project", "spec016-slice2", head, s)
		if c != ClassCode || r == nil || r.Reason != ReasonIdentityMismatch || !strings.HasPrefix(r.Detail, "moved-scope-") {
			t.Errorf("%s: %q %v", name, c, r)
		}
	}
	if c, r := AdaptMovedScope(p, "git", "owner/project", "spec016-slice2", head, nil); c != ClassCode || r == nil || r.Detail != "moved-scope-absent" {
		t.Errorf("absent: %q %v", c, r)
	}
	// With no accepted restack receipt contract in the policy, no source can be admitted.
	none := goodPolicy()
	none.RestackReceipts = nil
	if c, r := AdaptMovedScope(none, "git", "owner/project", "spec016-slice2", head, src()); c != ClassCode || r == nil {
		t.Errorf("unaccepted contract: %q %v", c, r)
	}
	// A code class passes through as code.
	s := src()
	s.ChangeClass = "code"
	if c, r := AdaptMovedScope(p, "git", "owner/project", "spec016-slice2", head, s); c != ClassCode || r != nil {
		t.Errorf("%q %v", c, r)
	}
}

func TestWordingOnlyFixOfAnEffectiveCodePushStillNeedsTheCodeGate(t *testing.T) {
	in := codePublication()
	in.Review.SubjectSHA = sha('9') // CLEAR was at the previous SHA; the later fix is wording only
	if got := pubBlockers(EvaluatePublication(in)); !reflect.DeepEqual(got, []string{"review-missing/review-wrong-head"}) {
		t.Fatalf("%v", got)
	}
}

func TestPublicationWithAHelperReSignedChainNeedsTheReviewOfTheFinalSignedHead(t *testing.T) {
	in := codePublication()
	in.Policy = helperPolicy()
	in.Signing = signedEvidence()
	in.Review.SubjectSHA = sha('f')
	in.RangeDiff = &RangeDiff{Complete: true, OldCount: 2, NewCount: 2, Rows: []string{"=", "="}}
	if r := EvaluatePublication(in); !r.Allowed() {
		t.Fatalf("%v", pubBlockers(r))
	}
	in.Review.SubjectSHA = sha('3') // the review covered the unsigned candidate
	if got := pubBlockers(EvaluatePublication(in)); !reflect.DeepEqual(got, []string{"review-missing/review-wrong-head"}) {
		t.Fatalf("%v", got)
	}
}

func TestAnAbsentOrInvalidMovedScopeNeverGetsTheCheapReviewRow(t *testing.T) {
	in := codePublication()
	in.Moved = nil // class is code from the range, but the rewrite receipt is missing
	in.Review.ReviewerID = "sonnet-reviewer"
	r := EvaluatePublication(in)
	if r.MinTier != TierStrongest || !r.ClassUnknown || !reflect.DeepEqual(pubBlockers(r), []string{"identity-mismatch/moved-scope-absent", "review-missing/review-tier-too-low"}) {
		t.Fatalf("%+v", r)
	}
}
