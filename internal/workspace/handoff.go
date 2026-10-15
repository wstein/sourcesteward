// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"github.com/wstein/sourcesteward/internal/inspection"
)

func (c coordinator) handoff(ctx context.Context, o Options, retire bool) Result {
	command := "workspace-handoff"
	if retire {
		command = "workspace-retire-plan"
	}
	r := newResult(command)
	path, e := canonicalSource(o.Path)
	if e != nil {
		return failResult(r, e)
	}
	s, e := openStore(o.StateDir, false)
	if e != nil {
		return failResult(r, e)
	}
	defer s.Close()
	ids, e := s.list("tasks")
	if e != nil {
		return failResult(r, e)
	}
	var found *TaskRecord
	for _, id := range ids {
		var task TaskRecord
		if e = s.read("tasks", id, &task); e != nil {
			return failResult(r, e)
		}
		if task.TaskID != id {
			return refuse(r, "invalid-ownership", "Task record identity does not match its state filename.")
		}
		if task.Destination == path {
			if found != nil {
				return refuse(r, "ambiguous-ownership", "Multiple task records claim this destination.")
			}
			copy := task
			found = &copy
		}
	}
	if found == nil {
		return refuse(r, "unowned-workspace", "No durable task ownership record matches this exact workspace root.")
	}
	var p MutationPlan
	if e = s.read("plans", found.PlanID, &p); e != nil {
		return failResult(r, e)
	}
	if e = validatePlan(p, o.StateDir, found.PlanID); e != nil {
		return failResult(r, e)
	}
	r.Backend = p.Backend
	r.Plan = &p
	var j journal
	if e = s.read("operations", p.PlanID, &j); e != nil {
		return failResult(r, e)
	}
	if j.Phase != "complete" || j.Native == nil || j.DestinationIdentity == nil || !validTask(*found, p) || !sameOwner(j.Native.Identity, found.Native) || found.DestinationIdentity != *j.DestinationIdentity {
		return refuse(r, "invalid-ownership", "Task ownership is not supported by a complete creation journal.")
	}
	current, e := currentIdentity(path)
	if e != nil || current != found.DestinationIdentity {
		return refuse(r, "changed-destination", "Task destination identity changed; retain it.")
	}
	p.DestinationIdentity = &current
	a, e := c.adapter(p.Backend)
	if e != nil {
		return failResult(r, e)
	}
	native, e := a.Verify(ctx, p)
	if e != nil || !native.Complete || !sameOwner(native.Identity, found.Native) {
		return refuse(r, "changed-ownership", "Task native registration is unavailable or foreign; retain it.")
	}
	inventory := c.inventory(ctx, path, inspection.InventoryOptions{})
	if e = ctx.Err(); e != nil {
		return failResult(r, e)
	}
	found.Native = native.Identity
	found.Inventory = &inventory
	found.State = "observed"
	found.RemainingWork = []string{"Caller must supply task context; SourceSteward does not infer an agent session or unfinished intent.", "Retain all dirty, untracked, stale or unknown state; no retirement execution is authorized."}
	if retire {
		found.State = "retained"
		found.RemainingWork = append(found.RemainingWork, "Retirement execution is delegated to Spec011; no files, references or registrations were deleted.")
	}
	r.Task = found
	r.Status = "ok"
	r.Actions = []ActionOutcome{{"observe-owned-task", "completed", "Stable native ownership and destination identity revalidated without mutation."}}
	r.Limitations = append(r.Limitations, found.RecoveryLimits...)
	r.Limitations = append(r.Limitations, native.Limitations...)
	if inventory.Status != "ok" {
		r.Limitations = append(r.Limitations, "Inventory includes unknown or incomplete state; retain the workspace.")
	}
	return r
}
