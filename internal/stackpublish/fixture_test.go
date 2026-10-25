// SPDX-License-Identifier: EUPL-1.2

package stackpublish

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var fixtureNow = time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

func str(s string) *string { return &s }
func num(n int) *int       { return &n }

func sha(c byte) string { return strings.Repeat(string(c), 40) }

func allCapabilities() CapabilitySet {
	return CapabilitySet{KindPushTopic: true, KindCreatePR: true, KindEditBody: true, KindRetarget: true, KindReady: true, KindNativeLink: true, KindNativeUnstack: true}
}

func fixtureRepository() *Repository {
	return &Repository{Backend: "git", LayoutIdentity: "fixture-layout-1", Forge: "github", Host: "github.com", Owner: "owner", Name: "project",
		SelectionProvenance: "explicit-repo-flag", DefaultRef: "refs/heads/main", DefaultCommitID: sha('1'), DefaultSource: "local-remote-tracking-ref"}
}

func pushAction(id, ref string) Action {
	return Action{ID: id, Kind: KindPushTopic, Target: Target{Ref: str(ref)}, Expected: Expected{OldSHA: str(sha('a')), NewSHA: str(sha('b'))},
		Effects: []string{"push " + ref}, DependsOn: []string{}, Guards: []Guard{}, State: StatePlanned}
}

func retargetAction(id string, pr int) Action {
	return Action{ID: id, Kind: KindRetarget, Target: Target{PullRequest: num(pr)}, Expected: Expected{HeadSHA: str(sha('c')), BaseRef: str("refs/heads/parent"), PlannedBase: str("refs/heads/main")},
		Effects: []string{"base refs/heads/parent -> refs/heads/main"}, DependsOn: []string{}, Guards: []Guard{}, State: StatePlanned}
}

func fixtureInput(actions ...Action) PlanInput {
	selected := []string{}
	for _, a := range actions {
		selected = append(selected, a.ID)
	}
	return PlanInput{CreatedAt: fixtureNow, Repository: fixtureRepository(), Task: TaskBinding{TaskID: "spec016-slice1"},
		Mode: ModeSummary{Derived: "agent", Markers: []string{"CLAUDECODE"}}, Limits: DefaultLimits(), Options: PlanOptions{Selected: selected},
		Actions: actions, Blockers: []Blocker{}, Boundary: Boundary{Complete: true}, Capabilities: allCapabilities()}
}

func mustPlan(t *testing.T, in PlanInput) Plan {
	t.Helper()
	p, err := NewPlan(in)
	if err != nil {
		t.Fatalf("NewPlan: %v", err)
	}
	return p
}

// wantCode asserts a typed ValidationError with the code (and usage flag).
func wantCode(t *testing.T, err error, code string, usage bool) {
	t.Helper()
	var v *ValidationError
	if !errors.As(err, &v) {
		t.Fatalf("want ValidationError %q, got %v", code, err)
	}
	if v.Code != code || v.Usage != usage {
		t.Fatalf("want code %q usage %v, got %q usage %v (field %q)", code, usage, v.Code, v.Usage, v.Field)
	}
}

// testPlan is the validated plan of the fixture task in the fixture repository (owner/project).
func testPlan() Plan {
	p, err := NewPlan(fixtureInput())
	if err != nil {
		panic(err)
	}
	return p
}
