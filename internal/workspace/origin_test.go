// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/attribution"
)

const originSession = "sess-7f3a-sentinel"

func originFixture(t *testing.T, origin *attribution.Origin) (source, dest, state string, applied Result) {
	t.Helper()
	source = fixtureGit(t)
	ctx := context.Background()
	state = gitTempDir(t)
	if e := os.Chmod(state, 0700); e != nil {
		t.Fatal(e)
	}
	dest = filepath.Join(gitTempDir(t), "task")
	plan := PlanCreate(ctx, PlanOptions{Path: source, Base: "HEAD", Destination: dest, Task: "attributed", Owner: "fixture", StateDir: state})
	if plan.Status != "ok" {
		t.Fatalf("plan: %+v", plan.Diagnostics)
	}
	applied = Apply(ctx, ApplyOptions{StateDir: state, PlanID: plan.Plan.PlanID, Authorize: true, Origin: origin})
	if applied.Status != "ok" || applied.Task == nil {
		t.Fatalf("apply: %+v", applied.Diagnostics)
	}
	return source, dest, state, applied
}

func TestApplyRecordsOriginInStoredTask(t *testing.T) {
	want := attribution.Origin{Kind: "claude", SessionID: originSession, CreatedAt: "2026-10-24T12:00:00Z"}
	_, _, state, applied := originFixture(t, &want)
	shown := attribution.Origin{Kind: "claude", CreatedAt: want.CreatedAt}
	if applied.Task.Origin == nil || *applied.Task.Origin != shown {
		t.Fatalf("result origin %+v", applied.Task.Origin)
	}
	stored, found, err := ReadTask(state, "attributed")
	if err != nil || !found || stored.Origin == nil || *stored.Origin != want {
		t.Fatalf("stored origin %+v %v %v", stored.Origin, found, err)
	}
}

func TestApplyWithoutOriginRecordsNone(t *testing.T) {
	_, _, state, _ := originFixture(t, nil)
	stored, _, _ := ReadTask(state, "attributed")
	if stored.Origin != nil {
		t.Fatalf("origin invented: %+v", stored.Origin)
	}
}

func TestSecondApplyDoesNotOverwriteOrigin(t *testing.T) {
	first := attribution.Origin{Kind: "claude", SessionID: originSession, CreatedAt: "2026-10-24T12:00:00Z"}
	_, _, state, applied := originFixture(t, &first)
	other := attribution.Origin{Kind: "claude", SessionID: "other-session", CreatedAt: "2026-10-25T12:00:00Z"}
	again := Apply(context.Background(), ApplyOptions{StateDir: state, PlanID: applied.Plan.PlanID, Authorize: true, Origin: &other})
	if again.Status != "ok" || again.Task == nil || again.Task.Origin == nil || again.Task.Origin.CreatedAt != first.CreatedAt || again.Task.Origin.Kind != first.Kind {
		t.Fatalf("retry changed the origin: %+v %+v", again.Diagnostics, again.Task)
	}
	stored, _, _ := ReadTask(state, "attributed")
	if stored.Origin == nil || *stored.Origin != first {
		t.Fatalf("stored origin overwritten: %+v", stored.Origin)
	}
}

// The session id lives in stw state only: not in the source repository (objects, refs, config,
// logs), not in the new checkout or its native registration.
func TestOriginSessionAppearsInNoGitArtifact(t *testing.T) {
	origin := attribution.Origin{Kind: "claude", SessionID: originSession, CreatedAt: "2026-10-24T12:00:00Z"}
	source, dest, state, _ := originFixture(t, &origin)
	gitFixture(t, dest, "-c", "user.name=F", "-c", "user.email=f@example.invalid", "commit", "-q", "--allow-empty", "-m", "task commit")
	for _, root := range []string{source, dest} {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if b, e := os.ReadFile(p); e == nil && bytes.Contains(b, []byte(originSession)) {
				t.Errorf("session id found in %s", p)
			}
			return nil
		})
	}
	log := gitFixture(t, source, "log", "--all", "--format=%H %an %ae %cn %ce %s%n%b")
	if bytes.Contains([]byte(log), []byte(originSession)) {
		t.Error("session id in a commit")
	}
	// The state store holds it, in the task record only.
	found := 0
	_ = filepath.WalkDir(state, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if b, e := os.ReadFile(p); e == nil && bytes.Contains(b, []byte(originSession)) {
				found++
				if filepath.Base(filepath.Dir(p)) != "tasks" {
					t.Errorf("session id outside the task record: %s", p)
				}
			}
		}
		return nil
	})
	if found != 1 {
		t.Errorf("session id in %d state files, want 1", found)
	}
}

func TestSecretLookingVariablesReachNoStateFile(t *testing.T) {
	const sentinel = "MSG-TOKEN-SENTINEL-93b1"
	environ := []string{"CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID=" + originSession, "CLAUDE_CODE_MESSAGING_TOKEN=" + sentinel, "CODEX_API_KEY=" + sentinel}
	origin := attribution.FromEnviron(environ, []string{"CLAUDECODE", "CLAUDE_CODE_SESSION_ID", "CLAUDE_CODE_MESSAGING_TOKEN", "CODEX_API_KEY"}, false, time.Now())
	source, dest, state, _ := originFixture(t, &origin)
	for _, root := range []string{state, source, dest} {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				if b, e := os.ReadFile(p); e == nil && bytes.Contains(b, []byte(sentinel)) {
					t.Errorf("secret-looking value found in %s", p)
				}
			}
			return nil
		})
	}
}

func TestRetirementOwnershipDoesNotCarryTheOrigin(t *testing.T) {
	origin := attribution.Origin{Kind: "claude", SessionID: originSession, CreatedAt: "2026-10-24T12:00:00Z"}
	_, _, state, _ := originFixture(t, &origin)
	s, err := openStore(state, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, task, _, err := readRetirementOwnership(s, "attributed", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if task.Origin != nil {
		t.Fatalf("origin embedded in a closed-schema record: %+v", task.Origin)
	}
}

// No output shows the session id: the JSON of apply (first and retry) and handoff carries kind and time only.
func TestSessionIDAppearsInNoOutput(t *testing.T) {
	origin := attribution.Origin{Kind: "claude", SessionID: originSession, CreatedAt: "2026-10-24T12:00:00Z"}
	_, dest, state, applied := originFixture(t, &origin)
	retry := Apply(context.Background(), ApplyOptions{StateDir: state, PlanID: applied.Plan.PlanID, Authorize: true, Origin: &origin})
	handoff := Handoff(context.Background(), Options{Path: dest, StateDir: state})
	for name, r := range map[string]Result{"apply": applied, "retry": retry, "handoff": handoff} {
		b, err := json.Marshal(r)
		if err != nil || bytes.Contains(b, []byte(originSession)) {
			t.Errorf("%s output shows the session id or fails: %v", name, err)
		}
		if r.Task == nil || r.Task.Origin == nil || r.Task.Origin.Kind != "claude" || r.Task.Origin.CreatedAt == "" {
			t.Errorf("%s: kind and time missing: %+v", name, r.Task)
		}
	}
	if stored, _, _ := ReadTask(state, "attributed"); stored.Origin == nil || stored.Origin.SessionID != originSession {
		t.Error("the stored record lost the session id")
	}
}

// rewindJournal puts the operation journal back to native-created, the state after a first apply that
// stopped before the task record was finalized.
func rewindJournal(t *testing.T, state, planID string) {
	t.Helper()
	s, err := openStore(state, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var j journal
	if err = s.read("operations", planID, &j); err != nil {
		t.Fatal(err)
	}
	j.Phase = "native-created"
	if err = s.write("operations", planID, j, false); err != nil {
		t.Fatal(err)
	}
}

// With a task record already present and the journal at native-created, finalize takes its
// existing-record branch: the stored origin is kept and is what the result shows.
func TestFinalizeWithExistingRecordKeepsTheFirstOrigin(t *testing.T) {
	first := attribution.Origin{Kind: "claude", SessionID: originSession, CreatedAt: "2026-10-24T12:00:00Z"}
	_, _, state, applied := originFixture(t, &first)
	rewindJournal(t, state, applied.Plan.PlanID)
	other := attribution.Origin{Kind: "claude", SessionID: "other-session", CreatedAt: "2026-10-25T12:00:00Z"}
	again := Apply(context.Background(), ApplyOptions{StateDir: state, PlanID: applied.Plan.PlanID, Authorize: true, Origin: &other})
	if again.Status != "ok" || again.Task == nil || again.Task.Origin == nil || again.Task.Origin.CreatedAt != first.CreatedAt {
		t.Fatalf("finalize showed another origin: %+v %+v", again.Diagnostics, again.Task)
	}
	if stored, _, _ := ReadTask(state, "attributed"); stored.Origin == nil || *stored.Origin != first {
		t.Fatalf("stored origin changed: %+v", stored.Origin)
	}
}

// Known residual risk (Spec 042 D4): the origin is recorded at the first successful task-record write.
// When the first apply stopped before that write, the session that completes the creation is recorded.
func TestRetryAfterPartialFirstApplyRecordsTheRetryingSession(t *testing.T) {
	first := attribution.Origin{Kind: "claude", SessionID: originSession, CreatedAt: "2026-10-24T12:00:00Z"}
	_, _, state, applied := originFixture(t, &first)
	rewindJournal(t, state, applied.Plan.PlanID)
	if err := os.Remove(filepath.Join(state, "tasks", "attributed.json")); err != nil {
		t.Fatal(err)
	}
	retrying := attribution.Origin{Kind: "claude", SessionID: "retrying-session", CreatedAt: "2026-10-25T12:00:00Z"}
	again := Apply(context.Background(), ApplyOptions{StateDir: state, PlanID: applied.Plan.PlanID, Authorize: true, Origin: &retrying})
	if again.Status != "ok" {
		t.Fatalf("retry: %+v", again.Diagnostics)
	}
	if stored, _, _ := ReadTask(state, "attributed"); stored.Origin == nil || *stored.Origin != retrying {
		t.Fatalf("stored origin %+v", stored.Origin)
	}
}
