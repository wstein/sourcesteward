// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestReposOrchestrationMaxWorkers(t *testing.T) {
	tempDir := reposTempDir(t)
	var paths []string
	for i := 0; i < 8; i++ {
		p := filepath.Join(tempDir, fmt.Sprintf("repo%d", i))
		if err := os.Mkdir(p, 0o755); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}

	// Each stage holds its first four callers until all four are inside, so the peak must be exactly the
	// bound: fewer workers would never gather four, more would exceed it.
	var inspectArrived, invArrived int32
	gather := func(arrived *int32) {
		atomic.AddInt32(arrived, 1)
		for deadline := time.Now().Add(3 * time.Second); atomic.LoadInt32(arrived) < int32(ReposParallelism) && time.Now().Before(deadline); {
			time.Sleep(time.Millisecond)
		}
	}
	var inspectCurrent, inspectMax int32
	fakeInspect := func(ctx context.Context, path string) Result {
		c := atomic.AddInt32(&inspectCurrent, 1)
		for {
			m := atomic.LoadInt32(&inspectMax)
			if c <= m || atomic.CompareAndSwapInt32(&inspectMax, m, c) {
				break
			}
		}
		gather(&inspectArrived)
		time.Sleep(5 * time.Millisecond)
		atomic.AddInt32(&inspectCurrent, -1)
		wt := path
		return Result{
			Status:  "ok",
			Backend: "git",
			Repository: &Repository{
				GitDir:    filepath.Join(wt, ".git"),
				CommonDir: filepath.Join(wt, ".git"),
				Worktree:  &wt,
			},
		}
	}

	var invCurrent, invMax int32
	fakeInventory := func(ctx context.Context, path string, o InventoryOptions) InventoryResult {
		c := atomic.AddInt32(&invCurrent, 1)
		for {
			m := atomic.LoadInt32(&invMax)
			if c <= m || atomic.CompareAndSwapInt32(&invMax, m, c) {
				break
			}
		}
		gather(&invArrived)
		time.Sleep(5 * time.Millisecond)
		atomic.AddInt32(&invCurrent, -1)
		wt := path
		return InventoryResult{
			Status:  "ok",
			Backend: "git",
			Identity: Result{
				Status:  "ok",
				Backend: "git",
				Repository: &Repository{
					GitDir:    filepath.Join(wt, ".git"),
					CommonDir: filepath.Join(wt, ".git"),
					Worktree:  &wt,
				},
			},
		}
	}

	res := reposWithRunner(context.Background(), ReposOptions{
		Paths: paths,
	}, fakeInspect, fakeInventory, newReposRunner)

	if len(res.Entries) != 8 {
		t.Fatalf("expected 8 entries, got %d", len(res.Entries))
	}
	if got := atomic.LoadInt32(&inspectMax); got != 4 {
		t.Errorf("identity inspection peak concurrency %d, want exactly 4", got)
	}
	if got := atomic.LoadInt32(&invMax); got != 4 {
		t.Errorf("inventory peak concurrency %d, want exactly 4", got)
	}
	limits := res.Boundary.Limits
	if limits.Parallelism != 4 || limits.TotalSeconds != 60 || limits.PerRepositorySeconds != 10 || limits.MaxOutputBytes != 8388608 || limits.MaxEntries != 16 || limits.MaxDiscoveryCandidates != 64 || limits.GitmodulesBytes != 65536 || limits.MaxPathBytes != 4096 {
		t.Errorf("boundary limits are not the normative table: %+v", limits)
	}
}

func TestReposOrchestrationOrderStability(t *testing.T) {
	tempDir := reposTempDir(t)
	var paths []string
	for i := 0; i < 6; i++ {
		p := filepath.Join(tempDir, fmt.Sprintf("repo%d", i))
		if err := os.Mkdir(p, 0o755); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}

	fakeInspect := func(ctx context.Context, path string) Result {
		// Randomized delay
		time.Sleep(time.Duration(rand.Intn(20)) * time.Millisecond)
		wt := path
		return Result{
			Status:  "ok",
			Backend: "git",
			Repository: &Repository{
				GitDir:    filepath.Join(wt, ".git"),
				CommonDir: filepath.Join(wt, ".git"),
				Worktree:  &wt,
			},
		}
	}

	fakeInventory := func(ctx context.Context, path string, o InventoryOptions) InventoryResult {
		time.Sleep(time.Duration(rand.Intn(20)) * time.Millisecond)
		wt := path
		return InventoryResult{
			Status:  "ok",
			Backend: "git",
			Identity: Result{
				Status:  "ok",
				Backend: "git",
				Repository: &Repository{
					GitDir:    filepath.Join(wt, ".git"),
					CommonDir: filepath.Join(wt, ".git"),
					Worktree:  &wt,
				},
			},
		}
	}

	res := reposWithRunner(context.Background(), ReposOptions{
		Paths: paths,
	}, fakeInspect, fakeInventory, newReposRunner)

	for i, entry := range res.Entries {
		if entry.Path != paths[i] {
			t.Errorf("entry index %d path mismatch: expected %q, got %q", i, paths[i], entry.Path)
		}
		if entry.Index != i {
			t.Errorf("entry index mismatch: expected %d, got %d", i, entry.Index)
		}
	}
}

func TestReposOrchestrationPanicRecovery(t *testing.T) {
	tempDir := reposTempDir(t)
	p0 := filepath.Join(tempDir, "repo0")
	p1 := filepath.Join(tempDir, "repo1")
	p2 := filepath.Join(tempDir, "repo2")
	for _, p := range []string{p0, p1, p2} {
		if err := os.Mkdir(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	fakeInspect := func(ctx context.Context, path string) Result {
		if path == p1 {
			panic("simulated inspect panic")
		}
		wt := path
		return Result{
			Status:  "ok",
			Backend: "git",
			Repository: &Repository{
				GitDir:    filepath.Join(wt, ".git"),
				CommonDir: filepath.Join(wt, ".git"),
				Worktree:  &wt,
			},
		}
	}

	fakeInventory := func(ctx context.Context, path string, o InventoryOptions) InventoryResult {
		if path == p2 {
			panic("simulated inventory panic")
		}
		wt := path
		return InventoryResult{
			Status:  "ok",
			Backend: "git",
			Identity: Result{
				Status:  "ok",
				Backend: "git",
				Repository: &Repository{
					GitDir:    filepath.Join(wt, ".git"),
					CommonDir: filepath.Join(wt, ".git"),
					Worktree:  &wt,
				},
			},
		}
	}

	res := reposWithRunner(context.Background(), ReposOptions{
		Paths: []string{p0, p1, p2},
	}, fakeInspect, fakeInventory, newReposRunner)

	if len(res.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(res.Entries))
	}

	// entry 0 should be ok
	if res.Entries[0].Status != "ok" {
		t.Errorf("entry 0 expected ok, got %s", res.Entries[0].Status)
	}

	// entry 1 panicked in inspect -> member-failed
	if res.Entries[1].Status != "error" {
		t.Errorf("entry 1 expected error, got %s", res.Entries[1].Status)
	}
	if res.Entries[1].Error == nil || res.Entries[1].Error.Code != ReposCodeMemberFailed {
		t.Errorf("entry 1 expected code %s, got %+v", ReposCodeMemberFailed, res.Entries[1].Error)
	}

	// entry 2 panicked in inventory -> member-failed
	if res.Entries[2].Status != "error" {
		t.Errorf("entry 2 expected error, got %s", res.Entries[2].Status)
	}
	if res.Entries[2].Error == nil || res.Entries[2].Error.Code != ReposCodeMemberFailed {
		t.Errorf("entry 2 expected code %s, got %+v", ReposCodeMemberFailed, res.Entries[2].Error)
	}
}

func TestReposOrchestrationChangedRepository(t *testing.T) {
	tempDir := reposTempDir(t)
	p0 := filepath.Join(tempDir, "repo0")
	if err := os.Mkdir(p0, 0o755); err != nil {
		t.Fatal(err)
	}

	fakeInspect := func(ctx context.Context, path string) Result {
		wt := path
		headID := "1111111111111111111111111111111111111111"
		return Result{
			Status:  "ok",
			Backend: "git",
			Repository: &Repository{
				GitDir:    filepath.Join(wt, ".git"),
				CommonDir: filepath.Join(wt, ".git"),
				Worktree:  &wt,
				Head: Head{
					OID: &headID,
				},
			},
		}
	}

	fakeInventory := func(ctx context.Context, path string, o InventoryOptions) InventoryResult {
		wt := path
		// Identity changed between inspect and inventory!
		headID := "2222222222222222222222222222222222222222"
		return InventoryResult{
			Status:  "ok",
			Backend: "git",
			Identity: Result{
				Status:  "ok",
				Backend: "git",
				Repository: &Repository{
					GitDir:    filepath.Join(wt, ".git"),
					CommonDir: filepath.Join(wt, ".git"),
					Worktree:  &wt,
					Head: Head{
						OID: &headID,
					},
				},
			},
		}
	}

	res := reposWithRunner(context.Background(), ReposOptions{
		Paths: []string{p0},
	}, fakeInspect, fakeInventory, newReposRunner)

	if len(res.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(res.Entries))
	}
	entry := res.Entries[0]
	if entry.Status != "error" {
		t.Errorf("expected status error, got %s", entry.Status)
	}
	if entry.Error == nil || entry.Error.Code != ReposCodeChangedRepository {
		t.Errorf("expected error changed-repository, got %+v", entry.Error)
	}
	if entry.Inventory != nil {
		t.Errorf("expected inventory to be nil on changed-repository, got %+v", entry.Inventory)
	}
	if entry.Facts != nil {
		t.Errorf("expected facts to be nil on changed-repository, got %+v", entry.Facts)
	}
}

func TestReposOrchestrationTimeoutsAndMapping(t *testing.T) {
	tempDir := reposTempDir(t)
	p0 := filepath.Join(tempDir, "repo0")
	p1 := filepath.Join(tempDir, "repo1")
	for _, p := range []string{p0, p1} {
		if err := os.Mkdir(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// 1. Caller context canceled
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()

	resCancel := reposWithRunner(cancelCtx, ReposOptions{
		Paths: []string{p0},
	}, func(ctx context.Context, path string) Result {
		return Result{Status: "error", Diagnostics: []Diagnostic{{Code: "canceled", Message: "context canceled"}}}
	}, func(ctx context.Context, path string, o InventoryOptions) InventoryResult {
		return InventoryResult{Status: "error"}
	}, newReposRunner)

	if len(resCancel.Entries) > 0 {
		entry := resCancel.Entries[0]
		if entry.Error == nil || (entry.Error.Code != "canceled" && entry.Error.Code != ReposCodeInventoryNotStarted) {
			t.Errorf("expected canceled or inventory-not-started on caller cancel, got %+v", entry.Error)
		}
	}

	// 2. Underlying timeout mapping: canceled -> inventory-timeout with bound message, when the repos
	// total deadline (not the caller's) fired.
	setReposTimeouts(t, 20*time.Millisecond, time.Minute)
	deadlineCtx := context.Background()

	var startedCount int32
	fakeInspectSlow := func(ctx context.Context, path string) Result {
		atomic.AddInt32(&startedCount, 1)
		<-ctx.Done()
		return Result{
			Status:      "error",
			Diagnostics: []Diagnostic{{Code: "canceled", Message: "operation canceled"}},
		}
	}

	resTimeout := reposWithRunner(deadlineCtx, ReposOptions{
		Paths: []string{p0, p1},
	}, fakeInspectSlow, func(ctx context.Context, path string, o InventoryOptions) InventoryResult {
		return InventoryResult{Status: "ok"}
	}, newReposRunner)

	if len(resTimeout.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(resTimeout.Entries))
	}

	for _, entry := range resTimeout.Entries {
		if entry.Status != "error" {
			t.Errorf("expected error status on timeout, got %s", entry.Status)
		}
		if entry.Error == nil || entry.Error.Code != ReposCodeInventoryTimeout || !strings.Contains(entry.Error.Message, "total deadline of 60 seconds") {
			t.Errorf("expected inventory-timeout naming the total deadline, got %+v", entry.Error)
		}
	}
}

// setReposTimeouts shortens the total and per-call bounds for one test.
func setReposTimeouts(t *testing.T, total, call time.Duration) {
	t.Helper()
	oldTotal, oldCall := reposTotalTimeout, reposCallTimeout
	reposTotalTimeout, reposCallTimeout = total, call
	t.Cleanup(func() { reposTotalTimeout, reposCallTimeout = oldTotal, oldCall })
}

// L3: the caller's own deadline or cancellation is `canceled`, with a message that does not claim the
// 60-second bound; only the repos deadline (or a per-call deadline) is an inventory-timeout.
func TestReposCallerDeadlineIsCanceledNotTimeout(t *testing.T) {
	p0 := reposTempDir(t)
	slow := func(ctx context.Context, path string) Result {
		<-ctx.Done()
		return Result{Status: "error", Diagnostics: []Diagnostic{{Code: "canceled", Message: "operation canceled"}}}
	}
	inv := func(ctx context.Context, path string, o InventoryOptions) InventoryResult {
		return InventoryResult{Status: "ok"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	e := reposWithRunner(ctx, ReposOptions{Paths: []string{p0}}, slow, inv, newReposRunner).Entries[0]
	if e.Error == nil || e.Error.Code != ReposCodeCanceled || strings.Contains(e.Error.Message, "60 seconds") {
		t.Fatalf("caller deadline: %+v", e.Error)
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel2() }()
	e = reposWithRunner(ctx2, ReposOptions{Paths: []string{p0}}, slow, inv, newReposRunner).Entries[0]
	if e.Error == nil || e.Error.Code != ReposCodeCanceled {
		t.Fatalf("caller cancel: %+v", e.Error)
	}
	// A per-call deadline inside the total is an inventory-timeout naming the 10-second bound.
	setReposTimeouts(t, time.Minute, 20*time.Millisecond)
	e = reposWithRunner(context.Background(), ReposOptions{Paths: []string{p0}}, slow, inv, newReposRunner).Entries[0]
	if e.Error == nil || e.Error.Code != ReposCodeInventoryTimeout || !strings.Contains(e.Error.Message, "per-call deadline of 10 seconds") {
		t.Fatalf("per-call deadline: %+v", e.Error)
	}
}

func TestReposOrchestrationDeadlineInsideInventoryInspect(t *testing.T) {
	tempDir := reposTempDir(t)
	p0 := filepath.Join(tempDir, "repo0")
	if err := os.Mkdir(p0, 0o755); err != nil {
		t.Fatal(err)
	}

	fakeInspect := func(ctx context.Context, path string) Result {
		wt := path
		return Result{
			Status:  "ok",
			Backend: "git",
			Repository: &Repository{
				GitDir:    filepath.Join(wt, ".git"),
				CommonDir: filepath.Join(wt, ".git"),
				Worktree:  &wt,
			},
		}
	}

	// Inventory failed during its own inspect due to timeout/canceled without identity
	fakeInventory := func(ctx context.Context, path string, o InventoryOptions) InventoryResult {
		return InventoryResult{
			Status:  "error",
			Backend: "git",
			Identity: Result{
				Status: "error", // No usable identity
			},
			Diagnostics: []Diagnostic{
				{Code: "canceled", Message: "inspect timed out"},
			},
		}
	}

	res := reposWithRunner(context.Background(), ReposOptions{
		Paths: []string{p0},
	}, fakeInspect, fakeInventory, newReposRunner)

	if len(res.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(res.Entries))
	}
	entry := res.Entries[0]
	if entry.Status != "error" {
		t.Errorf("expected status error, got %s", entry.Status)
	}
	// Must be mapped to inventory-timeout (per-call bound exceeded) and NOT changed-repository
	if entry.Error == nil || entry.Error.Code != ReposCodeInventoryTimeout {
		t.Errorf("expected code inventory-timeout, got %+v", entry.Error)
	}
}

func TestReposOutputBudgetDropsLaterInventories(t *testing.T) {
	tempDir := reposTempDir(t)
	var paths []string
	for i := 0; i < 3; i++ {
		p := filepath.Join(tempDir, fmt.Sprintf("repo%d", i))
		if err := os.Mkdir(p, 0o755); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}

	fakeInspect := func(ctx context.Context, path string) Result {
		wt := path
		return Result{
			Status:  "ok",
			Backend: "git",
			Repository: &Repository{
				GitDir:    filepath.Join(wt, ".git"),
				CommonDir: filepath.Join(wt, ".git"),
				Worktree:  &wt,
			},
		}
	}

	// Inventory 0 is normal (small), Inventory 1 and 2 are huge (4.5 MiB each)
	fakeInventory := func(ctx context.Context, path string, o InventoryOptions) InventoryResult {
		wt := path
		hugeLim := ""
		if path != paths[0] {
			hugeLim = string(make([]byte, 4500000))
		}
		return InventoryResult{
			Status:  "ok",
			Backend: "git",
			Identity: Result{
				Status:  "ok",
				Backend: "git",
				Repository: &Repository{
					GitDir:    filepath.Join(wt, ".git"),
					CommonDir: filepath.Join(wt, ".git"),
					Worktree:  &wt,
				},
			},
			Workspaces: []InventoryWorkspace{
				{
					Path:         &wt,
					Manager:      "git",
					Registration: "ok",
					Tracked:      InventoryObservation{Value: "false"},
					Untracked:    InventoryObservation{Value: "false"},
					Limitations:  []string{hugeLim},
				},
			},
		}
	}

	res := reposWithRunner(context.Background(), ReposOptions{
		Paths: paths,
	}, fakeInspect, fakeInventory, newReposRunner)

	if len(res.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(res.Entries))
	}

	// The overall result must respect the 8 MiB limit
	if err := ReposCheckJSONBudget(res); err != nil {
		t.Fatalf("result exceeded 8 MiB budget: %v", err)
	}

	// First entry should retain inventory
	if res.Entries[0].Inventory == nil {
		t.Errorf("entry 0 should retain inventory, but got nil")
	}

	// Later entries should have inventory omitted, status error with output-limit, but facts kept
	omittedFound := false
	for i := 1; i < 3; i++ {
		entry := res.Entries[i]
		if entry.Inventory == nil && entry.Status == "error" && entry.Error != nil && entry.Error.Code == ReposCodeOutputLimit {
			omittedFound = true
			if entry.Facts == nil {
				t.Errorf("entry %d must keep facts even when inventory omitted", i)
			}
		}
	}
	if !omittedFound {
		t.Errorf("expected at least one omitted inventory with output-limit")
	}
	if res.Summary.InventoriesOmitted == 0 {
		t.Errorf("expected InventoriesOmitted > 0 in summary, got %d", res.Summary.InventoriesOmitted)
	}
}

func TestReposOrchestrationRace(t *testing.T) {
	tempDir := reposTempDir(t)
	var paths []string
	for i := 0; i < 8; i++ {
		p := filepath.Join(tempDir, fmt.Sprintf("race%d", i))
		if err := os.Mkdir(p, 0o755); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}

	fakeInspect := func(ctx context.Context, path string) Result {
		wt := path
		return Result{
			Status:  "ok",
			Backend: "git",
			Repository: &Repository{
				GitDir:    filepath.Join(wt, ".git"),
				CommonDir: filepath.Join(wt, ".git"),
				Worktree:  &wt,
			},
		}
	}

	fakeInventory := func(ctx context.Context, path string, o InventoryOptions) InventoryResult {
		wt := path
		return InventoryResult{
			Status:  "ok",
			Backend: "git",
			Identity: Result{
				Status:  "ok",
				Backend: "git",
				Repository: &Repository{
					GitDir:    filepath.Join(wt, ".git"),
					CommonDir: filepath.Join(wt, ".git"),
					Worktree:  &wt,
				},
			},
		}
	}

	res := reposWithRunner(context.Background(), ReposOptions{
		Paths: paths,
	}, fakeInspect, fakeInventory, newReposRunner)

	if len(res.Entries) != 8 {
		t.Fatalf("expected 8 entries, got %d", len(res.Entries))
	}
}

func TestReposOutputBudgetFailingFinalRecheck(t *testing.T) {
	tempDir := reposTempDir(t)
	p0 := filepath.Join(tempDir, "repo0")
	if err := os.Mkdir(p0, 0o755); err != nil {
		t.Fatal(err)
	}

	fakeInspect := func(ctx context.Context, path string) Result {
		wt := path
		return Result{
			Status:  "ok",
			Backend: "git",
			Repository: &Repository{
				GitDir:    filepath.Join(wt, ".git"),
				CommonDir: filepath.Join(wt, ".git"),
				Worktree:  &wt,
			},
		}
	}

	// Single inventory that alone exceeds 8 MiB (9 MiB string)
	fakeInventory := func(ctx context.Context, path string, o InventoryOptions) InventoryResult {
		wt := path
		return InventoryResult{
			Status:  "ok",
			Backend: "git",
			Identity: Result{
				Status:  "ok",
				Backend: "git",
				Repository: &Repository{
					GitDir:    filepath.Join(wt, ".git"),
					CommonDir: filepath.Join(wt, ".git"),
					Worktree:  &wt,
				},
			},
			Workspaces: []InventoryWorkspace{
				{
					Path:         &wt,
					Manager:      "git",
					Registration: "ok",
					Limitations:  []string{string(make([]byte, 9000000))},
				},
			},
		}
	}

	res := reposWithRunner(context.Background(), ReposOptions{
		Paths: []string{p0},
	}, fakeInspect, fakeInventory, newReposRunner)

	if len(res.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(res.Entries))
	}
	entry := res.Entries[0]
	if entry.Inventory != nil {
		t.Errorf("expected inventory to be omitted, got %+v", entry.Inventory)
	}
	if entry.Status != "error" || entry.Error == nil || entry.Error.Code != ReposCodeOutputLimit {
		t.Errorf("expected status error with code output-limit, got status %s, error %+v", entry.Status, entry.Error)
	}
	if res.Status != "error" {
		t.Errorf("expected overall status error when all inventories omitted, got %s", res.Status)
	}
	if err := ReposCheckJSONBudget(res); err != nil {
		t.Fatalf("result must be within 8 MiB, got error: %v", err)
	}
}

// The timeout mapping against real repositories: a per-call deadline that fires inside the real identity
// inspection is an inventory-timeout naming the per-call bound, and the other members are unaffected.
func TestReposRealRepositoryPerCallDeadlineIsInventoryTimeout(t *testing.T) {
	reposHermetic(t)
	base := reposTempDir(t)
	repo := reposNewRepo(t, filepath.Join(base, "repo"))
	setReposTimeouts(t, time.Minute, time.Millisecond)
	res := Repos(context.Background(), ReposOptions{Paths: []string{repo}})
	e := res.Entries[0]
	if e.Status != "error" || e.Error == nil || e.Error.Code != ReposCodeInventoryTimeout || !strings.Contains(e.Error.Message, "per-call deadline of 10 seconds") {
		t.Fatalf("%+v %+v", e, e.Error)
	}
}
