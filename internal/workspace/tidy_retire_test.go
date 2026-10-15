// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRetirementRequiresExactCompletedCreationEvidence(t *testing.T) {
	c, _, o := fixtureCoordinator(t)
	planned := c.plan(context.Background(), o)
	if planned.Status != "ok" {
		t.Fatal(planned)
	}
	result := c.apply(context.Background(), ApplyOptions{StateDir: o.StateDir, PlanID: planned.Plan.PlanID, Authorize: true})
	if result.Status != "ok" {
		t.Fatal(result)
	}
	s, err := openRetirementStore(o.StateDir, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, task, digest, err := readRetirementOwnership(s, o.Task, o.Owner)
	if err != nil || task.TaskID != o.Task || p.PlanID != planned.Plan.PlanID || len(digest) != 64 {
		t.Fatalf("ownership %v", err)
	}
	if _, _, _, err = readRetirementOwnership(s, o.Task, "foreign-owner"); err == nil {
		t.Fatal("wrong owner accepted")
	}
	var j journal
	if err = s.read("operations", p.PlanID, &j); err != nil {
		t.Fatal(err)
	}
	j.Phase = "prepared"
	if err = s.write("operations", p.PlanID, j, false); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = readRetirementOwnership(s, o.Task, o.Owner); err == nil {
		t.Fatal("incomplete creation journal accepted")
	}
}
func TestRetirementCurrentWorkspaceProtectionUsesActualCWD(t *testing.T) {
	parent := privateDir(t)
	source := filepath.Join(parent, "source")
	target := filepath.Join(parent, "target")
	os.Mkdir(source, 0700)
	os.Mkdir(target, 0700)
	if err := protectRetirementWorkspace(source, target, target); err == nil {
		t.Fatal("explicit alternate source permitted deleting actual current directory")
	}
	if err := protectRetirementWorkspace(source, source, parent); err == nil {
		t.Fatal("source permitted as retirement target")
	}
	if err := protectRetirementWorkspace(source, target, source); err != nil {
		t.Fatal(err)
	}
}
func TestRetirementRecoveryPinsTargetIdentity(t *testing.T) {
	p := RetirementPlanRecord{PlanID: strings.Repeat("a", 32), Backend: "git", Task: TaskRecord{TaskID: "task"}, Expected: RetirementObservation{Target: NativeIdentity{CommitID: strings.Repeat("b", 40)}, Expected: Snapshot{Identity: NativeIdentity{CommitID: strings.Repeat("c", 40)}}}}
	if retirementRecoveryFor(p).CommitID != p.Expected.Target.CommitID {
		t.Fatal("source commit pinned instead of retired target")
	}
}
