// SPDX-License-Identifier: EUPL-1.2

package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A dry run of a tracking plan judges the plan and stores nothing.
func TestTidyV3TrackingDryRunStoresNothing(t *testing.T) {
	o, c, _, _ := fixtureTidyV3TrackingPlanner(t)
	dir := o.Expected.Identity.WorkspaceRoot
	stale := gitFixture(t, dir, "rev-parse", "refs/remotes/origin/stale")
	gitFixture(t, dir, "update-ref", "refs/remotes/origin/live", stale)
	state := privateDir(t)
	before := listFiles(t, state)
	opts := V3PlanOptions{Path: dir, Base: "HEAD", Online: true, TrackingRemote: "origin", StateDir: state, ProtectWorktrees: []string{}, DryRun: true}
	r := c.plan(context.Background(), opts)
	if r.Status != "ok" || r.Plan == nil {
		t.Fatalf("%s %+v", r.Status, r.Diagnostics)
	}
	if after := listFiles(t, state); strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Errorf("a dry run stored a tracking plan: %v", after)
	}
	opts.DryRun = false
	if real := c.plan(context.Background(), opts); real.Status != "ok" || len(listFiles(t, state)) <= len(before) {
		t.Errorf("the real tracking plan must store: %s", real.Status)
	}
}

// A dry run writes no review report either.
func TestPlanTidyV3DryRunWritesNoReport(t *testing.T) {
	f := reportRepo(t)
	dir := gitTempDir(t)
	r := f.planReport(V3PlanOptions{ReportDir: dir, DryRun: true})
	if r.Status != "ok" || r.Plan == nil {
		t.Fatalf("%s %+v", r.Status, r.Diagnostics)
	}
	if _, err := os.Stat(filepath.Join(dir, "tidy-report.json")); !os.IsNotExist(err) {
		t.Errorf("a dry run wrote the report: %v", err)
	}
	if real := f.planReport(V3PlanOptions{ReportDir: dir}); real.Status != "ok" {
		t.Fatal(real.Diagnostics)
	}
	if _, err := os.Stat(filepath.Join(dir, "tidy-report.json")); err != nil {
		t.Errorf("the real plan must write the report: %v", err)
	}
}
