// SPDX-License-Identifier: EUPL-1.2
package tidywizard

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/tidywizard/derive"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/workspace"
)

func pruneMode() owner.Evidence {
	return owner.Evidence{Mode: owner.ModeAgent, Caller: "codex", CallerSource: "explicit", Markers: []string{}, ByOwner: []string{}}
}

func TestRegistrationPruneAdapterUnprovenStoreNeverObserves(t *testing.T) {
	for _, kind := range []string{"none", "duplicate", "unreadable", "unsafe", "mismatch", "wrong-task", "wrong-gitdir", "empty-owner"} {
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
					switch kind {
					case "wrong-task":
						rec.TaskID = "other"
					case "wrong-gitdir":
						rec.Native.GitDir += "-other"
					case "empty-owner":
						rec.Owner = ""
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

func TestRegistrationPruneAdapterAdmissionGates(t *testing.T) {
	cases := []struct {
		name   string
		repos  []RepoSpec
		stores []derive.Store
		cfg    Config
		calls  int
	}{
		{name: "repository-count", repos: make([]RepoSpec, MaxCandidates+1)},
		{name: "store-count", repos: []RepoSpec{{Name: "r"}}, stores: make([]derive.Store, MaxCandidates+1)},
		{name: "mode-disagreement", repos: []RepoSpec{{Name: "r"}}, cfg: Config{Caller: "codex"}},
		{name: "duplicate-repository", repos: []RepoSpec{{Name: "r"}, {Name: "r"}}, calls: 1},
		{name: "skipped-repository", repos: []RepoSpec{{Name: "r", Skip: "secret"}}},
		{name: "failed-repository", repos: []RepoSpec{{Name: "r", Error: "secret"}}},
		{name: "empty-name", repos: []RepoSpec{{Name: ""}}},
		{name: "invalid-name", repos: []RepoSpec{{Name: string([]byte{255})}}},
		{name: "long-name", repos: []RepoSpec{{Name: strings.Repeat("n", 4097)}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfg
			if tc.name != "mode-disagreement" {
				cfg = Config{Caller: "codex", Agent: true}
			}
			calls := 0
			a := registrationPruneAdapter{inventory: func(context.Context, string) workspace.RegistrationPruneV4Inventory {
				calls++
				return workspace.RegistrationPruneV4Inventory{Status: "ok"}
			}}
			got := a.run(context.Background(), tc.repos, tc.stores, pruneMode(), cfg)
			if !got.Incomplete || calls != tc.calls || len(got.Rows) != 0 || len(got.Notices) != 1 || got.Notices[0] != "registration-prune-inventory-incomplete" {
				t.Fatalf("inventory calls=%d want %d result=%+v", calls, tc.calls, got)
			}
		})
	}
}

func TestRegistrationPruneAdapterCandidateBounds(t *testing.T) {
	cfg := Config{Caller: "codex", Agent: true}
	for _, count := range []int{MaxCandidates, MaxCandidates + 1} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			candidates := make([]workspace.RegistrationPruneV4Candidate, count)
			for i := range candidates {
				candidates[i] = workspace.RegistrationPruneV4Candidate{Name: fmt.Sprint(i), GitDir: "/r/admin/" + fmt.Sprint(i), Destination: "/gone/" + fmt.Sprint(i)}
			}
			a := registrationPruneAdapter{inventory: func(context.Context, string) workspace.RegistrationPruneV4Inventory {
				return workspace.RegistrationPruneV4Inventory{Status: "ok", Candidates: candidates}
			}}
			got := a.run(context.Background(), []RepoSpec{{Name: "r"}}, nil, pruneMode(), cfg)
			if len(got.Rows) != MaxCandidates || got.Incomplete != (count > MaxCandidates) {
				t.Fatalf("count=%d rows=%d incomplete=%v", count, len(got.Rows), got.Incomplete)
			}
		})
	}
	for _, field := range []string{"name", "gitdir", "destination"} {
		for _, value := range []string{string([]byte{255}), strings.Repeat("x", 4097)} {
			t.Run(fmt.Sprintf("%s/%d", field, len(value)), func(t *testing.T) {
				candidate := workspace.RegistrationPruneV4Candidate{Name: "n", GitDir: "/r/admin/n", Destination: "/gone"}
				switch field {
				case "name":
					candidate.Name = value
				case "gitdir":
					candidate.GitDir = value
				case "destination":
					candidate.Destination = value
				}
				a := registrationPruneAdapter{inventory: func(context.Context, string) workspace.RegistrationPruneV4Inventory {
					return workspace.RegistrationPruneV4Inventory{Status: "ok", Candidates: []workspace.RegistrationPruneV4Candidate{candidate}}
				}}
				got := a.run(context.Background(), []RepoSpec{{Name: "r"}}, nil, pruneMode(), cfg)
				if !got.Incomplete || len(got.Rows) != 0 {
					t.Fatalf("unbounded candidate accepted: rows=%d incomplete=%v", len(got.Rows), got.Incomplete)
				}
			})
		}
	}
}

func TestRegistrationPruneAdapterDeadlineIsBounded(t *testing.T) {
	for _, parentLimit := range []time.Duration{0, time.Second} {
		t.Run(parentLimit.String(), func(t *testing.T) {
			ctx := context.Background()
			var parentDeadline time.Time
			if parentLimit > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, parentLimit)
				defer cancel()
				parentDeadline, _ = ctx.Deadline()
			}
			started := time.Now()
			calls := 0
			a := registrationPruneAdapter{inventory: func(observed context.Context, _ string) workspace.RegistrationPruneV4Inventory {
				calls++
				deadline, ok := observed.Deadline()
				if !ok || deadline.Before(started) || deadline.After(started.Add(time.Minute+time.Second)) {
					t.Fatalf("unbounded observation deadline: %v", deadline)
				}
				if parentLimit > 0 && deadline != parentDeadline {
					t.Fatalf("parent deadline changed: got %v want %v", deadline, parentDeadline)
				}
				return workspace.RegistrationPruneV4Inventory{Status: "ok"}
			}}
			got := a.run(ctx, []RepoSpec{{Name: "r"}}, nil, pruneMode(), Config{Caller: "codex", Agent: true})
			if calls != 1 || got.Incomplete {
				t.Fatalf("calls=%d result=%+v", calls, got)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := registrationPruneAdapter{inventory: func(context.Context, string) workspace.RegistrationPruneV4Inventory {
		cancel()
		return workspace.RegistrationPruneV4Inventory{Status: "ok"}
	}}
	if got := a.run(ctx, []RepoSpec{{Name: "r"}}, nil, pruneMode(), Config{Caller: "codex", Agent: true}); !got.Incomplete {
		t.Fatal("cancellation after inventory was lost")
	}
}

func TestRegistrationPruneAdapterStoreMatchingControl(t *testing.T) {
	candidate := workspace.RegistrationPruneV4Candidate{Name: "n", Task: "task", GitDir: "/r/admin/n", Destination: "/gone"}
	reads := 0
	a := registrationPruneAdapter{readTask: func(state, task string) (workspace.TaskRecord, bool, error) {
		reads++
		return workspace.TaskRecord{TaskID: task, Owner: "codex", Destination: candidate.Destination, Native: workspace.NativeIdentity{GitDir: candidate.GitDir}}, true, nil
	}}
	got, ok := a.uniqueStore(context.Background(), []derive.Store{{Path: "/store"}, {Path: "/store"}}, candidate)
	if !ok || got != "/store" || reads != 1 {
		t.Fatalf("matching store=%q ok=%v reads=%d", got, ok, reads)
	}
}
