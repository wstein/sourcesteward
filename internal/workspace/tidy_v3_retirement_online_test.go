// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/wstein/sourcesteward/internal/hosting"
)

func fixtureTidyV3RetirementMergedHead(t *testing.T, o V3PlanOptions) (V3PlanOptions, hosting.TidyRepositoryIdentity, hosting.TidyHeadEvidence) {
	t.Helper()
	head := gitFixture(t, o.CompletedWorktree, "rev-parse", "HEAD")
	root := gitFixture(t, o.CompletedWorktree, "rev-parse", "HEAD^")
	tree := gitFixture(t, o.CompletedWorktree, "rev-parse", "HEAD^{tree}")
	result := gitFixture(t, o.Path, "commit-tree", tree, "-p", root, "-m", "independent integrated result")
	sourceRef := gitFixture(t, o.Path, "symbolic-ref", "HEAD")
	gitFixture(t, o.Path, "update-ref", sourceRef, result)
	gitFixture(t, o.Path, "remote", "add", "origin", "https://github.com/owner/repo.git")
	repo := hosting.TidyRepositoryIdentity{Host: "github.com", TidyRepositoryID: hosting.TidyRepositoryID{ID: 1, NodeID: "R_fixture", FullName: "owner/repo"}, DefaultBranch: "main"}
	row := tidyV3PR(repo, head, root, result, "deleted-intermediate-stack-base", 1)
	// The provider proof is under test: keep the local forge-free classes out of it.
	o.IntegrationProofs = []string{"ancestor"}
	o.Online = true
	o.Remote = "origin"
	o.PRHints = []V3PRHint{{OID: head, Number: 1}}
	return o, repo, row
}

// Per-operation typed sessions avoid reusing a closed session or old proof as a
// new observation. They never contact HTTP or mutate provider state.
func fixtureTidyV3RetirementHosting(c *tidyV3RetirementCoordinator, repo hosting.TidyRepositoryIdentity, row hosting.TidyHeadEvidence, configure func(*fixtureTidyV3Hosting)) {
	c.refs.openHosting = func(context.Context, string, hosting.TidySessionOptions) (tidyV3Hosting, error) {
		s := &fixtureTidyV3Hosting{repo: repo, rows: map[string]hosting.TidyHeadEvidence{row.HeadOID: row}, budget: hosting.TidyHTTPBudget{Requests: 1, RawBytes: 1, NetworkAttempted: true}}
		if configure != nil {
			configure(s)
		}
		return s, nil
	}
}

func TestTidyV3LegacyOnlineMergedHeadRetirement(t *testing.T) {
	o, _ := tidyV3CommittedLegacyFixture(t, false, 315)
	o.StateDir = privateDir(t)
	o, repo, row := fixtureTidyV3RetirementMergedHead(t, o)
	c := defaultTidyV3RetirementCoordinator()
	fixtureTidyV3RetirementHosting(&c, repo, row, nil)
	planned := c.plan(context.Background(), o)
	if planned.Status != "ok" || planned.Plan == nil {
		t.Fatal("online retirement plan", planned.Diagnostics)
	}
	p := *planned.Plan
	if p.Retirement.Integration.Proof != "github-merged-head" {
		t.Fatal("original intermediate PR evidence unused", p.Retirement.Integration)
	}
	r := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, Online: true, CompletedWorktree: o.CompletedWorktree, Owner: o.Owner, ProtectWorktrees: o.ProtectWorktrees})
	if r.Status != "ok" || r.Plan == nil {
		t.Fatal("online retirement apply", r.Diagnostics)
	}
}

func TestTidyV3LegacyChangedProviderAfterRecoveryRetainsTarget(t *testing.T) {
	o, _ := tidyV3CommittedLegacyFixture(t, false, 315)
	o.StateDir = privateDir(t)
	o, repo, row := fixtureTidyV3RetirementMergedHead(t, o)
	c := defaultTidyV3RetirementCoordinator()
	fixtureTidyV3RetirementHosting(&c, repo, row, nil)
	planned := c.plan(context.Background(), o)
	if planned.Plan == nil {
		t.Fatal(planned.Diagnostics)
	}
	p := *planned.Plan
	var live *fixtureTidyV3Hosting
	fixtureTidyV3RetirementHosting(&c, repo, row, func(s *fixtureTidyV3Hosting) { live = s })
	pin := c.pin
	c.pin = func(ctx context.Context, bin string, id NativeIdentity, r TidyRecovery, ro []string) (bool, error) {
		attempted, e := pin(ctx, bin, id, r, ro)
		if e == nil {
			live.errorOnRecheck = hosting.ErrTidyChanged
		}
		return attempted, e
	}
	removes := 0
	c.remove = func(context.Context, string, []string, string, []string, []string) (string, int, error) {
		removes++
		t.Fatal("changed provider reached removal")
		return "", 1, nil
	}
	r := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, Online: true, CompletedWorktree: o.CompletedWorktree, Owner: o.Owner, ProtectWorktrees: o.ProtectWorktrees})
	if r.Status != "partial" || r.Plan != nil || removes != 0 {
		t.Fatal("changed provider accepted", r)
	}
	if got := gitFixture(t, o.CompletedWorktree, "rev-parse", "HEAD"); got != p.Retirement.Target.CommitID {
		t.Fatal("target changed")
	}
	recovery := v3RecoveryFor(p, p.Retirement.ActionID, p.Retirement.Target.CommitID)
	if got := gitFixture(t, o.Path, "rev-parse", recovery.Name); got != recovery.CommitID {
		t.Fatal("recovery lost")
	}
}

func TestTidyV3LegacyProviderChangesAtRemovalGrantBoundaries(t *testing.T) {
	for _, boundary := range []int{6, 7} {
		t.Run(fmt.Sprint(boundary), func(t *testing.T) {
			o, _ := tidyV3CommittedLegacyFixture(t, false, 315)
			o.StateDir = privateDir(t)
			o, repo, row := fixtureTidyV3RetirementMergedHead(t, o)
			c := defaultTidyV3RetirementCoordinator()
			fixtureTidyV3RetirementHosting(&c, repo, row, nil)
			planned := c.plan(context.Background(), o)
			if planned.Plan == nil {
				t.Fatal(planned.Diagnostics)
			}
			p := *planned.Plan
			var live *fixtureTidyV3Hosting
			fixtureTidyV3RetirementHosting(&c, repo, row, func(s *fixtureTidyV3Hosting) { live = s })
			observe, samples := c.observe, 0
			c.observe = func(ctx context.Context, options V3PlanOptions, budget *tidyV3ProofBudget) (tidyV3LegacyObservation, error) {
				out, err := observe(ctx, options, budget)
				samples++
				if err == nil && samples == boundary {
					live.errorOnRecheck = hosting.ErrTidyChanged
				}
				return out, err
			}
			removes := 0
			c.remove = func(context.Context, string, []string, string, []string, []string) (string, int, error) {
				removes++
				return "", 1, errors.New("changed provider must not reach native removal")
			}
			r := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, Online: true, CompletedWorktree: o.CompletedWorktree, Owner: o.Owner, ProtectWorktrees: o.ProtectWorktrees})
			if r.Status != "partial" || r.Plan != nil || removes != 0 || samples != boundary {
				t.Fatal("provider change crossed grants", r.Diagnostics, removes, samples)
			}
			if got := gitFixture(t, o.CompletedWorktree, "rev-parse", "HEAD"); got != p.Retirement.Target.CommitID {
				t.Fatal("actor target changed")
			}
			recovery := v3RecoveryFor(p, p.Retirement.ActionID, p.Retirement.Target.CommitID)
			if got := gitFixture(t, o.Path, "rev-parse", recovery.Name); got != recovery.CommitID {
				t.Fatal("current-head recovery lost")
			}
		})
	}
}
