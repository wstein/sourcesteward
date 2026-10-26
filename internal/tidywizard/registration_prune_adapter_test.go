// SPDX-License-Identifier: EUPL-1.2
package tidywizard

import (
	"context"
	"errors"
	"testing"

	"github.com/wstein/sourcesteward/internal/tidywizard/derive"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/workspace"
)

func pruneMode() owner.Evidence {
	return owner.Evidence{Mode: owner.ModeAgent, Caller: "codex", CallerSource: "explicit", Markers: []string{}, ByOwner: []string{}}
}

func TestRegistrationPruneAdapterUnprovenStoreNeverObserves(t *testing.T) {
	for _, kind := range []string{"none", "duplicate", "unreadable", "unsafe", "mismatch"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			a := registrationPruneAdapter{
				inventory: func(context.Context, string) workspace.RegistrationPruneV4Inventory {
					return workspace.RegistrationPruneV4Inventory{Status: "ok", Candidates: []workspace.RegistrationPruneV4Candidate{{Name: "n", GitDir: "/r/.git/worktrees/n", Destination: "/gone", Task: "task"}}}
				},
				readTask: func(store, task string) (workspace.TaskRecord, bool, error) {
					if kind == "unreadable" {
						return workspace.TaskRecord{}, false, errors.New("private secret")
					}
					if kind == "none" {
						return workspace.TaskRecord{}, false, nil
					}
					rec := workspace.TaskRecord{TaskID: task, Owner: "codex", Destination: "/gone", Native: workspace.NativeIdentity{GitDir: "/r/.git/worktrees/n"}}
					if kind == "mismatch" {
						rec.Destination = "/other"
					}
					return rec, true, nil
				},
				observe: func(context.Context, workspace.RegistrationPruneV4Options) workspace.RegistrationPruneV4Observation {
					calls++
					return workspace.RegistrationPruneV4Observation{Status: "ok"}
				},
			}
			stores := []derive.Store{{Path: "/store"}}
			if kind == "duplicate" {
				stores = append(stores, derive.Store{Path: "/second"})
			}
			if kind == "unsafe" {
				stores[0].Issue = &derive.Issue{Code: "unsafe-state-dir"}
			}
			got := a.run(context.Background(), []RepoSpec{{Name: "r", Path: "/r"}}, stores, pruneMode(), Config{Caller: "codex", Agent: true})
			if calls != 0 || len(got.Rows) != 1 || got.Rows[0].Reason != "registration-prune-ownership-unproven" || got.Rows[0].Verdict != RegistrationPruneRetain || len(SelectRegistrationPrunes(got.Rows, Config{Caller: "codex", Agent: true})) != 0 {
				t.Fatalf("calls=%d result=%+v", calls, got)
			}
		})
	}
}

func TestRegistrationPruneAdapterDeadlineAndInventoryFailure(t *testing.T) {
	calls := 0
	a := registrationPruneAdapter{inventory: func(context.Context, string) workspace.RegistrationPruneV4Inventory {
		calls++
		return workspace.RegistrationPruneV4Inventory{Status: "refused", Diagnostic: workspace.Diagnostic{Code: "malicious\x1b", Message: "secret payload"}}
	}}
	cfg := Config{Caller: "codex", Agent: true}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := a.run(ctx, []RepoSpec{{Name: "r", Path: "/r"}}, nil, pruneMode(), cfg)
	if calls != 0 || !got.Incomplete {
		t.Fatalf("canceled calls=%d result=%+v", calls, got)
	}
	got = a.run(context.Background(), []RepoSpec{{Name: "r", Path: "/r"}}, nil, pruneMode(), cfg)
	if calls != 1 || !got.Incomplete || len(got.Notices) != 1 || got.Notices[0] != "registration-prune-inventory-incomplete" {
		t.Fatalf("inventory failure=%+v", got)
	}
	wrong := pruneMode()
	wrong.Caller = "else"
	got = a.run(context.Background(), []RepoSpec{{Name: "r", Path: "/r"}}, nil, wrong, cfg)
	if calls != 1 || !got.Incomplete {
		t.Fatal("inconsistent mode observed repository")
	}
}
