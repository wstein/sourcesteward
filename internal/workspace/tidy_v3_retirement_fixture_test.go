// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"bytes"
	"context"
	"fmt"
	"github.com/wstein/sourcesteward/internal/hosting"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Execute this retirement→fresh-reference sequence only on the reviewed
// integrated D source revision; earlier retirement-only receipts are separate.
func TestTidyV3SixRealisticLegacyRetirementsThenFreshReferences(t *testing.T) {
	for i, count := range []int{315, 323, 332, 341, 350, 358} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			o, preserved := tidyV3CommittedLegacyFixture(t, i%2 == 1, count)
			o.StateDir = privateDir(t)
			tracked := strings.Split(gitFixture(t, o.CompletedWorktree, "ls-files"), "\n")
			svg, e := os.Stat(filepath.Join(o.CompletedWorktree, "brand.svg"))
			if e != nil || len(tracked) != count || svg.Size() != 301117 {
				t.Fatal("realistic native fixture incomplete")
			}
			c := defaultTidyV3RetirementCoordinator()
			selectedRef := "refs/heads/completed"
			if i%2 == 1 {
				selectedRef = fmt.Sprintf("refs/heads/completed-detached-%d", count)
				gitFixture(t, o.Path, "update-ref", selectedRef, gitFixture(t, o.CompletedWorktree, "rev-parse", "HEAD"))
			}
			providerSessions := 0
			if i >= 2 {
				var repo hosting.TidyRepositoryIdentity
				var row hosting.TidyHeadEvidence
				o, repo, row = fixtureTidyV3RetirementMergedHead(t, o)
				fixtureTidyV3RetirementHosting(&c, repo, row, func(*fixtureTidyV3Hosting) { providerSessions++ })
			}
			// A later chosen base preserves the task head as an integrated candidate;
			// exact base/head equality would correctly retain a subsequent branch plan.
			gitFixture(t, o.Path, "commit", "--allow-empty", "-qm", "later chosen cleanup base")
			observe := c.observe
			var ledger *tidyV3ProofBudget
			c.observe = func(ctx context.Context, o V3PlanOptions, b *tidyV3ProofBudget) (tidyV3LegacyObservation, error) {
				ledger = b
				return observe(ctx, o, b)
			}
			started := time.Now()
			planned := c.plan(context.Background(), o)
			planElapsed := time.Since(started)
			if planned.Status != "ok" || planned.Plan == nil {
				t.Fatalf("realistic planning %+v", planned.Diagnostics)
			}
			p := *planned.Plan
			pc := ledger.snapshot()
			planDeadline := planObservationTimeout
			if o.Online {
				planDeadline = planObservationTimeout + planOnlineTimeout
			}
			if pc.Observations != 3 || pc.RawProcesses != 3 || pc.UserPasses != 9 || pc.MetadataPasses != 12 || pc.MaxReadBuffer > 64<<10 || planElapsed >= planDeadline {
				t.Fatalf("plan proof %+v elapsed%s", pc, planElapsed)
			}
			var user, pointer, metadata int64
			for _, entry := range p.Retirement.Disk.Entries {
				if entry.Identity.Kind == "regular" {
					if entry.Identity.Path == filepath.Join(o.CompletedWorktree, ".git") {
						pointer += entry.Identity.Size
					} else {
						user += entry.Identity.Size
					}
				}
			}
			for _, entry := range p.Retirement.Metadata.Entries {
				if entry.Identity.Kind == "regular" {
					metadata += entry.Identity.Size
				}
			}
			if pc.UserBytes != 9*user || pc.MetadataBytes != 6*(pointer+metadata) {
				t.Fatalf("plan actual-byte accounting %+v", pc)
			}
			started = time.Now()
			applied := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, Online: o.Online, Owner: o.Owner, CompletedWorktree: o.CompletedWorktree, ProtectWorktrees: o.ProtectWorktrees})
			applyElapsed := time.Since(started)
			if applied.Status != "ok" || applied.Plan == nil {
				t.Fatalf("realistic native removal %+v", applied.Diagnostics)
			}
			ac := ledger.snapshot()
			if ac.Observations != 8 || ac.RawProcesses != 8 || ac.UserPasses != 24 || ac.MetadataPasses != 32 || ac.UserBytes != 24*user || ac.MetadataBytes != 16*(pointer+metadata) || ac.MaxReadBuffer > 64<<10 || applyElapsed >= 60*time.Second {
				t.Fatalf("apply proof %+v elapsed%s", ac, applyElapsed)
			}
			for _, path := range []string{o.CompletedWorktree, p.Retirement.Registration.Path} {
				if _, e := os.Lstat(path); !os.IsNotExist(e) {
					t.Fatal("authorized native path remains", path, e)
				}
			}
			for path, want := range preserved {
				got, e := os.ReadFile(path)
				if e != nil || !bytes.Equal(got, want) {
					t.Fatalf("protected primary/active bytes changed: %s", path)
				}
			}
			if p.Retirement.Target.HeadRef != "" {
				if got := gitFixture(t, o.Path, "rev-parse", p.Retirement.Target.HeadRef); got != p.Retirement.Target.CommitID {
					t.Fatal("attached branch changed")
				}
			}
			recovery := v3RecoveryFor(p, p.Retirement.ActionID, p.Retirement.Target.CommitID)
			if got := gitFixture(t, o.Path, "rev-parse", recovery.Name); got != recovery.CommitID {
				t.Fatal("history recovery missing")
			}
			// Whole acceptance requires a new immutable reference plan and a new
			// operation-scoped provider session after physical registration removal.
			refsOptions := V3PlanOptions{Path: o.Path, Base: o.Base, StateDir: privateDir(t), Refs: []string{selectedRef}, ProtectWorktrees: []string{}, Online: o.Online, Remote: o.Remote, Repo: o.Repo, PRHints: append([]V3PRHint{}, o.PRHints...)}
			refsPlan := c.refs.plan(context.Background(), refsOptions)
			if refsPlan.Status != "ok" || refsPlan.Plan == nil || len(refsPlan.Plan.Candidates) != 1 || refsPlan.Plan.Candidates[0].Decision != "delete" {
				t.Fatal("fresh post-retirement reference plan", refsPlan.Diagnostics, refsPlan.Plan)
			}
			refsApply := defaultTidyV3RefsApplyCoordinator()
			refsApply.refs = c.refs
			refsResult := refsApply.apply(context.Background(), V3ApplyOptions{StateDir: refsOptions.StateDir, PlanID: refsPlan.Plan.PlanID, Authorize: true, Online: o.Online, ProtectWorktrees: []string{}})
			if refsResult.Status != "ok" || refsResult.Plan == nil {
				t.Fatal("fresh post-retirement reference apply", refsResult.Diagnostics)
			}
			if got := gitFixture(t, o.Path, "for-each-ref", "--format=%(refname)", selectedRef); got != "" {
				t.Fatal("fresh selected branch remains", got)
			}
			if got := gitFixture(t, o.Path, "rev-parse", recovery.Name); got != recovery.CommitID {
				t.Fatal("reference cleanup lost retirement recovery")
			}
			for path, want := range preserved {
				got, e := os.ReadFile(path)
				if e != nil || !bytes.Equal(got, want) {
					t.Fatal("fresh reference cleanup changed protected bytes", path)
				}
			}
			if o.Online && providerSessions != 4 {
				t.Fatalf("expected four fresh operation-scoped typed provider sessions, got%d", providerSessions)
			}
			t.Logf("tracked=%d svg=301117 native-in-place=true detached=%t clean=tracked+staged+untracked+ignored plan=%+v planElapsed=%s apply=%+v applyElapsed=%s; protected primary user-plan/cache and active bytes exact; raw-process channel excludes other bounded native controls and native internal removal IO; fresh reference plan/apply complete; typed provider sessions=%d online=%t (inert native fixture evidence, no live HTTP)", count, i%2 == 1, pc, planElapsed, ac, applyElapsed, providerSessions, o.Online)
		})
	}
}
