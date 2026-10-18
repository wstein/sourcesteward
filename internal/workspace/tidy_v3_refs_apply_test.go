// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/wstein/sourcesteward/internal/hosting"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func fixtureTidyV3NativeApply(t *testing.T) (string, string, V3Plan) {
	t.Helper()
	dir, bin := fixtureTidyV3Git(t)
	gitFixture(t, dir, "branch", "integrated", "HEAD")
	gitFixture(t, dir, "commit", "--allow-empty", "-qm", "advance source")
	base := gitFixture(t, dir, "symbolic-ref", "HEAD")
	r := defaultTidyV3RefsCoordinator().plan(context.Background(), V3PlanOptions{Path: dir, Base: base, StateDir: privateDir(t), Refs: []string{"refs/heads/integrated"}, ProtectWorktrees: []string{}})
	if r.Status != "ok" || r.Plan == nil || len(r.Plan.Candidates) != 1 || r.Plan.Candidates[0].Decision != "delete" {
		t.Fatalf("fixture plan failed: %+v", r)
	}
	return dir, bin, *r.Plan
}
func readTidyV3ApplyJournal(t *testing.T, p V3Plan) V3Journal {
	t.Helper()
	observation, err := observeTidyV3Git(context.Background(), p.SourcePath, p.Expected.BaseSelector)
	if err != nil {
		t.Fatal(err)
	}
	s, err := openV3Store(p.StateDir, false, v3NativeRoots(observation.Native), []string{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	j, err := s.readJournal(p)
	if err != nil {
		t.Fatal(err)
	}
	return j
}
func TestTidyV3ApplyCreatesExactRecoveryDeletesAndRetriesReadOnly(t *testing.T) {
	dir, bin, p := fixtureTidyV3NativeApply(t)
	protected := layoutProtectedFiles(t, dir, dir, p.Expected.Identity.RepositoryDir)
	apply := defaultTidyV3RefsApplyCoordinator()
	o := V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, Online: p.Online != nil, ProtectWorktrees: []string{}}
	r := apply.apply(context.Background(), o)
	if r.Status != "ok" || r.Plan == nil || !r.Boundary.Complete || validateV3Result(r) != nil {
		t.Fatalf("complete apply failed: status=%s diagnostics=%+v", r.Status, r.Diagnostics)
	}
	target := p.Candidates[0]
	recovery := v3RecoveryFor(p, target.ActionID, target.OID)
	if err := exec.Command(bin, "-C", dir, "show-ref", "--verify", "--quiet", target.Name).Run(); err == nil {
		t.Fatal("selected target still exists")
	}
	if got := gitFixture(t, dir, "rev-parse", recovery.Name); got != target.OID {
		t.Fatalf("wrong recovery %s", got)
	}
	requireLayoutProtectedFiles(t, protected)
	j := readTidyV3ApplyJournal(t, p)
	if j.Phase != "complete" || len(j.Entries) != 1 || j.Entries[0].Transaction != "confirmed" || j.Entries[0].Recovery == nil || *j.Entries[0].Recovery != recovery {
		t.Fatalf("wrong completed journal %+v", j)
	}
	journalPath := filepath.Join(p.StateDir, "tidy-v3-operations", p.PlanID+".json")
	before := layoutProtectedFileAt(t, journalPath)
	refs := gitFixture(t, dir, "for-each-ref", "--format=%(refname) %(objectname) %(symref)")
	retry := apply.apply(context.Background(), o)
	if retry.Status != "ok" || retry.Plan == nil {
		t.Fatalf("completed retry failed %+v", retry)
	}
	for _, action := range retry.Actions {
		if action.Status == "unknown" {
			t.Fatalf("completed read-only retry invented uncertain effect: %+v", action)
		}
	}

	if after := layoutProtectedFileAt(t, journalPath); !reflect.DeepEqual(after, before) {
		t.Fatal("completed retry rewrote journal")
	}
	if after := gitFixture(t, dir, "for-each-ref", "--format=%(refname) %(objectname) %(symref)"); after != refs {
		t.Fatal("completed retry changed refs")
	}
	if _, err := os.Stat(filepath.Join(p.Expected.Identity.RepositoryDir, "refs", "heads", "integrated.lock")); !os.IsNotExist(err) {
		t.Fatal("completed apply left a target lock", err)
	}
}

// The constructor keeps the original live operation context. Only Commit's
// tighter caller context is canceled, after its callback durably wrote intent.
// Cancellation may prevent abort acknowledgment; preserve A's uncertainty.
type fixtureTidyV3CancelAfterIntent struct {
	tidyV3ApplyRefTransaction
	observed gitRefTransactionOutcome
}

func (t *fixtureTidyV3CancelAfterIntent) Commit(ctx context.Context, validate func(context.Context) error) (gitRefTransactionOutcome, error) {
	caller, cancel := context.WithCancel(ctx)
	defer cancel()
	out, err := t.tidyV3ApplyRefTransaction.Commit(caller, func(callback context.Context) error {
		err := validate(callback)
		if err == nil {
			cancel()
			<-callback.Done()
		}
		return err
	})
	t.observed = out
	return out, err
}
func TestTidyV3ApplyCancellationAfterDurableCommitIntentIsUnknown(t *testing.T) {
	dir, _, p := fixtureTidyV3NativeApply(t)
	c := defaultTidyV3RefsApplyCoordinator()
	prepare := c.prepare
	var tx *fixtureTidyV3CancelAfterIntent
	c.prepare = func(ctx context.Context, bin string, id NativeIdentity, ref, oid string, write, ro []string) (tidyV3ApplyRefTransaction, error) {
		inner, err := prepare(ctx, bin, id, ref, oid, write, ro)
		if err != nil {
			return inner, err
		}
		tx = &fixtureTidyV3CancelAfterIntent{tidyV3ApplyRefTransaction: inner}
		return tx, nil
	}
	r := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, Online: p.Online != nil, ProtectWorktrees: []string{}})
	if tx == nil {
		t.Fatal("actual native prepared transaction was not exercised")
	}
	if !tx.observed.Prepared || tx.observed.CommitAttempted || tx.observed.Committed || !tx.observed.Uncertain {
		t.Fatalf("expected unconfirmed cancellation cleanup with no COMMIT send, got %+v", tx.observed)
	}
	if r.Status != "partial" || r.Plan != nil {
		t.Fatalf("canceled action became successful %+v", r)
	}
	target := p.Candidates[0]
	if got := gitFixture(t, dir, "rev-parse", target.Name); got != target.OID {
		t.Fatal("confirmed abort changed target")
	}
	recovery := v3RecoveryFor(p, target.ActionID, target.OID)
	if got := gitFixture(t, dir, "rev-parse", recovery.Name); got != target.OID {
		t.Fatal("recovery missing after abort")
	}
	if _, err := os.Stat(filepath.Join(p.Expected.Identity.RepositoryDir, "refs", "heads", "integrated.lock")); !os.IsNotExist(err) {
		t.Fatal("confirmed native abort left target lock", err)
	}
	j := readTidyV3ApplyJournal(t, p)
	if j.Entries[0].Transaction != "unknown" || j.Phase != "incomplete" {
		t.Fatalf("actual uncertain native outcome=%+v but persisted phase=%s transaction=%s", tx.observed, j.Phase, j.Entries[0].Transaction)
	}
}

type fixtureTidyV3RefuseAfterIntent struct {
	tidyV3ApplyRefTransaction
	observed gitRefTransactionOutcome
}

func (t *fixtureTidyV3RefuseAfterIntent) Commit(ctx context.Context, validate func(context.Context) error) (gitRefTransactionOutcome, error) {
	out, err := t.tidyV3ApplyRefTransaction.Commit(ctx, func(callback context.Context) error {
		if err := validate(callback); err != nil {
			return err
		}
		return failure("post-intent-validation-refused", "Final prepared validation refused after durable commit intent; do not send COMMIT.")
	})
	t.observed = out
	return out, err
}
func TestTidyV3ApplyConfirmedAbortAfterDurableCommitIntent(t *testing.T) {
	dir, _, p := fixtureTidyV3NativeApply(t)
	c := defaultTidyV3RefsApplyCoordinator()
	prepare := c.prepare
	var tx *fixtureTidyV3RefuseAfterIntent
	c.prepare = func(ctx context.Context, bin string, id NativeIdentity, ref, oid string, write, ro []string) (tidyV3ApplyRefTransaction, error) {
		inner, err := prepare(ctx, bin, id, ref, oid, write, ro)
		if err != nil {
			return inner, err
		}
		tx = &fixtureTidyV3RefuseAfterIntent{tidyV3ApplyRefTransaction: inner}
		return tx, nil
	}
	r := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, Online: p.Online != nil, ProtectWorktrees: []string{}})
	if tx == nil || !tx.observed.Prepared || tx.observed.CommitAttempted || tx.observed.Committed || tx.observed.Uncertain {
		t.Fatalf("expected actual acknowledged abort/no COMMIT, got %+v", tx)
	}
	if r.Status != "partial" || r.Plan != nil {
		t.Fatalf("refused final guard became successful %+v", r)
	}
	target := p.Candidates[0]
	if got := gitFixture(t, dir, "rev-parse", target.Name); got != target.OID {
		t.Fatal("acknowledged abort changed target")
	}
	recovery := v3RecoveryFor(p, target.ActionID, target.OID)
	if got := gitFixture(t, dir, "rev-parse", recovery.Name); got != target.OID {
		t.Fatal("recovery missing after acknowledged abort")
	}
	if _, err := os.Stat(filepath.Join(p.Expected.Identity.RepositoryDir, "refs", "heads", "integrated.lock")); !os.IsNotExist(err) {
		t.Fatal("acknowledged abort left target lock", err)
	}
	j := readTidyV3ApplyJournal(t, p)
	if j.Entries[0].Transaction != "aborted" {
		t.Fatalf("actual native outcome=%+v but persisted phase=%s transaction=%s known_effects=%v; acknowledged abort/no-send is not an unknown deletion", tx.observed, j.Phase, j.Entries[0].Transaction, j.KnownEffects)
	}
	if j.Phase != "incomplete" || len(j.Entries[0].Deltas) != 1 || j.Entries[0].Recovery == nil {
		t.Fatalf("abort lost recovery history: %+v", j)
	}
	for _, effect := range j.KnownEffects {
		if strings.HasPrefix(effect, "possible-reference-deletion:") {
			t.Fatal("confirmed no-send abort invented possible deletion", effect)
		}
	}
	if len(j.Diagnostics) != 1 || j.Diagnostics[0].Code != "native-commit-aborted:"+target.ActionID {
		t.Fatalf("missing exact abort diagnostic: %+v", j.Diagnostics)
	}
	requireTidyV3NoDiagnostic(t, r, "journal-update-failed")
	journalPath := filepath.Join(p.StateDir, "tidy-v3-operations", p.PlanID+".json")
	before := layoutProtectedFileAt(t, journalPath)
	c.prepare = func(context.Context, string, NativeIdentity, string, string, []string, []string) (tidyV3ApplyRefTransaction, error) {
		t.Fatal("confirmed aborted retry attempted native mutation")
		return nil, nil
	}
	retry := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, ProtectWorktrees: []string{}})
	found := false
	for _, d := range retry.Diagnostics {
		found = found || d.Code == "native-commit-aborted:"+target.ActionID
	}
	if !found {
		t.Fatalf("retry lost durable no-send/acknowledged-abort diagnostic: %+v", retry.Diagnostics)
	}
	for _, action := range retry.Actions {
		if strings.HasPrefix(action.Name, "native-target-lock:") && action.Status == "unknown" {
			t.Fatalf("confirmed aborted retry invented uncertain lock: %+v", action)
		}
	}
	if after := layoutProtectedFileAt(t, journalPath); !reflect.DeepEqual(before, after) {
		t.Fatal("confirmed aborted retry rewrote terminal journal")
	}
}

func TestTidyV3ApplyNativeThreeActionWriteAhead(t *testing.T) {
	dir, _ := fixtureTidyV3Git(t)
	refs := []string{"refs/heads/integrated-a", "refs/heads/integrated-b", "refs/heads/integrated-c"}
	for _, ref := range refs {
		gitFixture(t, dir, "branch", ref[len("refs/heads/"):], "HEAD")
	}
	gitFixture(t, dir, "commit", "--allow-empty", "-qm", "advance source")
	planned := defaultTidyV3RefsCoordinator().plan(context.Background(), V3PlanOptions{Path: dir, Base: gitFixture(t, dir, "symbolic-ref", "HEAD"), StateDir: privateDir(t), Refs: refs, ProtectWorktrees: []string{}})
	if planned.Status != "ok" || planned.Plan == nil || len(v3Eligible(*planned.Plan)) != 3 {
		t.Fatalf("three-action plan failed: %+v", planned)
	}
	p := *planned.Plan
	c := defaultTidyV3RefsApplyCoordinator()
	pin := c.pin
	calls := 0
	c.pin = func(ctx context.Context, bin string, id NativeIdentity, recovery TidyRecovery, ro []string) (bool, error) {
		j := readTidyV3ApplyJournal(t, p)
		if j.Phase != "prepared" {
			t.Fatalf("recovery %d lacks prepared write-ahead: %s", calls, j.Phase)
		}
		for i, e := range j.Entries {
			if i < calls && e.Phase != "action-complete" {
				t.Fatalf("earlier action incomplete: %+v", e)
			}
			if i == calls && (!e.RecoveryAttempted || e.PlannedRecovery != recovery) {
				t.Fatalf("current recovery lacks exact durable intent: %+v", e)
			}
			if i > calls && (e.RecoveryAttempted || e.Recovery != nil || e.Transaction != "none") {
				t.Fatalf("future action already attempted: %+v", e)
			}
		}
		calls++
		return pin(ctx, bin, id, recovery, ro)
	}
	r := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, Online: p.Online != nil, ProtectWorktrees: []string{}})
	if r.Status != "ok" || calls != 3 {
		t.Fatalf("three-action apply failed: calls=%d status=%s diagnostics=%+v", calls, r.Status, r.Diagnostics)
	}
	j := readTidyV3ApplyJournal(t, p)
	if j.Phase != "complete" {
		t.Fatalf("incomplete sequence: %+v", j)
	}
	for _, target := range v3Eligible(p) {
		if got := gitFixture(t, dir, "rev-parse", v3RecoveryFor(p, target.ActionID, target.OID).Name); got != target.OID {
			t.Fatal("wrong recovery", got)
		}
	}
}

func requireTidyV3Ref(t *testing.T, dir, ref, oid string) {
	t.Helper()
	if got := gitFixture(t, dir, "rev-parse", ref); got != oid {
		t.Fatalf("%s = %s, want %s", ref, got, oid)
	}
}
func requireTidyV3NoDiagnostic(t *testing.T, r V3Result, code string) {
	t.Helper()
	for _, d := range r.Diagnostics {
		if d.Code == code {
			t.Fatalf("unexpected %s: %+v", code, r.Diagnostics)
		}
	}
}
func TestTidyV3ApplyPreEffectRefusals(t *testing.T) {
	for _, kind := range []string{"unauthorized", "changed-tip", "changed-base", "symbolic-target", "registered-target", "changed-config", "tampered-plan", "canceled"} {
		t.Run(kind, func(t *testing.T) {
			dir, _, p := fixtureTidyV3NativeApply(t)
			target := p.Candidates[0]
			c := defaultTidyV3RefsApplyCoordinator()
			c.pin = func(context.Context, string, NativeIdentity, TidyRecovery, []string) (bool, error) {
				t.Fatal("native recovery reached on refused apply")
				return false, nil
			}
			c.prepare = func(context.Context, string, NativeIdentity, string, string, []string, []string) (tidyV3ApplyRefTransaction, error) {
				t.Fatal("native prepare reached on refused apply")
				return nil, nil
			}
			ctx := context.Background()
			authorized := true
			switch kind {
			case "unauthorized":
				authorized = false
			case "changed-tip":
				gitFixture(t, dir, "update-ref", target.Name, p.Expected.BaseCommit)
			case "changed-base":
				gitFixture(t, dir, "commit", "--allow-empty", "-qm", "changed base")
			case "symbolic-target":
				gitFixture(t, dir, "symbolic-ref", target.Name, p.Expected.BaseSelector)
			case "registered-target":
				gitFixture(t, dir, "worktree", "add", "--quiet", filepath.Join(t.TempDir(), "linked"), strings.TrimPrefix(target.Name, "refs/heads/"))
			case "changed-config":
				gitFixture(t, dir, "config", "core.filemode", "false")
			case "tampered-plan":
				path := filepath.Join(p.StateDir, "tidy-v3-plans", p.PlanID+".json")
				b, e := os.ReadFile(path)
				if e != nil {
					t.Fatal(e)
				}
				if e = os.WriteFile(path, append(b, []byte(" {}")...), 0600); e != nil {
					t.Fatal(e)
				}
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			before := gitFixture(t, dir, "for-each-ref", "--format=%(refname) %(objectname) %(symref)")
			r := c.apply(ctx, V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: authorized, ProtectWorktrees: []string{}})
			if r.Status == "ok" || r.Plan != nil || r.Boundary.Complete {
				t.Fatalf("unsafe apply admitted: %+v", r)
			}
			if after := gitFixture(t, dir, "for-each-ref", "--format=%(refname) %(objectname) %(symref)"); after != before {
				t.Fatal("refused apply changed refs")
			}
			if _, err := os.Stat(filepath.Join(p.StateDir, "tidy-v3-operations", p.PlanID+".json")); !os.IsNotExist(err) {
				t.Fatal("preflight refusal wrote journal", err)
			}
		})
	}
}
func fixtureTidyV3ProviderApply(t *testing.T, all bool) (string, V3Plan, *fixtureTidyV3Hosting, tidyV3RefsApplyCoordinator) {
	t.Helper()
	dir, _, expected, repo, rows := fixtureTidyV3Merged(t)
	refs, session := fixtureTidyV3Online(t, dir, repo, rows)
	selected := []string{}
	if !all {
		selected = []string{"refs/heads/topic-xxx"}
	}
	r := refs.plan(context.Background(), V3PlanOptions{Path: dir, Base: expected.BaseSelector, Online: true, Remote: "origin", StateDir: privateDir(t), Refs: selected, ProtectWorktrees: []string{}})
	if r.Status != "ok" || r.Plan == nil {
		t.Fatalf("online plan failed %+v", r)
	}
	session.closed = false
	session.rechecks = nil
	c := defaultTidyV3RefsApplyCoordinator()
	c.refs = refs
	return dir, *r.Plan, session, c
}
func TestTidyV3ApplyNineProviderHeadsIncludingSevenStacked(t *testing.T) {
	dir, p, s, c := fixtureTidyV3ProviderApply(t, true)
	protected := layoutProtectedFiles(t, dir, dir, p.Expected.Identity.RepositoryDir)
	r := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, Online: p.Online != nil, ProtectWorktrees: []string{}})
	if r.Status != "ok" || r.Plan == nil || len(v3Eligible(p)) != 9 || !s.closed || !r.Boundary.NetworkAttempted || validateV3Result(r) != nil {
		t.Fatalf("nine provider heads failed %+v", r)
	}
	for _, target := range v3Eligible(p) {
		requireTidyV3Ref(t, dir, v3RecoveryFor(p, target.ActionID, target.OID).Name, target.OID)
	}
	requireLayoutProtectedFiles(t, protected)
}
func TestTidyV3ApplyProviderFailureBoundaries(t *testing.T) {
	for _, boundary := range []string{"before-journal", "after-recovery", "while-prepared", "native-change-during-provider"} {
		t.Run(boundary, func(t *testing.T) {
			dir, p, s, c := fixtureTidyV3ProviderApply(t, false)
			target := v3Eligible(p)[0]
			pins, prepares := 0, 0
			pin := c.pin
			c.pin = func(ctx context.Context, bin string, id NativeIdentity, recovery TidyRecovery, ro []string) (bool, error) {
				pins++
				sent, err := pin(ctx, bin, id, recovery, ro)
				if boundary == "after-recovery" {
					s.errorOnRecheck = hosting.ErrTidyChanged
				}
				return sent, err
			}
			prepare := c.prepare
			c.prepare = func(ctx context.Context, bin string, id NativeIdentity, ref, oid string, write, ro []string) (tidyV3ApplyRefTransaction, error) {
				prepares++
				tx, err := prepare(ctx, bin, id, ref, oid, write, ro)
				if boundary == "while-prepared" {
					s.errorOnRecheck = hosting.ErrTidyChanged
				}
				return tx, err
			}
			if boundary == "before-journal" {
				s.errorOnRecheck = hosting.ErrTidyChanged
			}
			if boundary == "native-change-during-provider" {
				c.refs.openHosting = func(context.Context, string, hosting.TidySessionOptions) (tidyV3Hosting, error) {
					return &fixtureTidyV3ApplyHostingHook{fixtureTidyV3Hosting: s, onRecheck: func() { gitFixture(t, dir, "config", "core.filemode", "false") }}, nil
				}
			}
			r := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, Online: p.Online != nil, ProtectWorktrees: []string{}})
			if r.Status == "ok" || r.Plan != nil || r.Boundary.Complete || validateV3Result(r) != nil {
				t.Fatalf("provider failure admitted %+v", r)
			}
			requireTidyV3Ref(t, dir, target.Name, target.OID)
			if boundary == "before-journal" || boundary == "native-change-during-provider" {
				if pins != 0 || prepares != 0 {
					t.Fatal("effects before complete stable proof")
				}
				return
			}
			if pins != 1 {
				t.Fatal("wrong pin count", pins)
			}
			if boundary == "after-recovery" && prepares != 0 {
				t.Fatal("provider changed after recovery but native preparation was reached")
			}

			requireTidyV3Ref(t, dir, v3RecoveryFor(p, target.ActionID, target.OID).Name, target.OID)
			j := readTidyV3ApplyJournal(t, p)
			if j.Phase != "incomplete" {
				t.Fatal("failure not terminal", j.Phase)
			}
			if boundary == "while-prepared" && (prepares != 1 || j.Entries[0].Transaction != "aborted") {
				t.Fatalf("prepared failure lacked confirmed abort %+v", j.Entries[0])
			}
			before := layoutProtectedFileAt(t, filepath.Join(p.StateDir, "tidy-v3-operations", p.PlanID+".json"))
			s.errorOnRecheck = nil
			c.prepare = func(context.Context, string, NativeIdentity, string, string, []string, []string) (tidyV3ApplyRefTransaction, error) {
				t.Fatal("incomplete replay reached prepare")
				return nil, nil
			}
			c.refs.openHosting = func(context.Context, string, hosting.TidySessionOptions) (tidyV3Hosting, error) {
				t.Fatal("incomplete replay reached credentials")
				return nil, nil
			}
			retry := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, Online: p.Online != nil, ProtectWorktrees: []string{}})
			if retry.Status != "partial" || retry.Plan != nil {
				t.Fatalf("incomplete replay admitted %+v", retry)
			}
			if after := layoutProtectedFileAt(t, filepath.Join(p.StateDir, "tidy-v3-operations", p.PlanID+".json")); !reflect.DeepEqual(before, after) {
				t.Fatal("incomplete replay rewrote journal")
			}
		})
	}
}

type fixtureTidyV3ApplyHostingHook struct {
	*fixtureTidyV3Hosting
	onRecheck func()
}

func (s *fixtureTidyV3ApplyHostingHook) RevalidatePullRequest(ctx context.Context, pr hosting.TidyPullRequest) error {
	s.onRecheck()
	return s.fixtureTidyV3Hosting.RevalidatePullRequest(ctx, pr)
}
func TestTidyV3ApplyDurabilityBeforeNativeEffects(t *testing.T) {
	for _, point := range []string{"initial", "recovery-intent", "recovery-confirmed", "prepared", "commit-intent", "action-complete", "terminal-abort"} {
		t.Run(point, func(t *testing.T) {
			dir, _, p := fixtureTidyV3NativeApply(t)
			target := v3Eligible(p)[0]
			c := defaultTidyV3RefsApplyCoordinator()
			open := c.openStore
			injected := false
			writesAfterTerminal := 0
			c.openStore = func(path string, write bool, registered, protected []string) (*v3Store, error) {
				s, err := open(path, write, registered, protected)
				if err != nil {
					return nil, err
				}
				original := s.writeRecord
				s.writeRecord = func(kind, id string, value any, exclusive bool) error {
					j, ok := value.(V3Journal)
					if ok {
						if injected && point == "terminal-abort" {
							writesAfterTerminal++
						}
						hit := point == "initial" && exclusive || point == "recovery-intent" && j.Entries[0].RecoveryAttempted && j.Entries[0].Recovery == nil || point == "recovery-confirmed" && j.Phase == "recovery-confirmed" || point == "prepared" && j.Phase == "target-prepared" && j.Entries[0].Transaction == "prepared" || point == "commit-intent" && j.Entries[0].Transaction == "commit-attempted" || point == "action-complete" && j.Phase == "action-complete" || point == "terminal-abort" && j.Phase == "incomplete" && j.Entries[0].Transaction == "aborted"
						if !injected && hit {
							injected = true
							return errors.New("injected durable journal failure")
						}
					}
					return original(kind, id, value, exclusive)
				}
				return s, nil
			}
			if point == "terminal-abort" {
				prepare := c.prepare
				c.prepare = func(ctx context.Context, bin string, id NativeIdentity, ref, oid string, write, ro []string) (tidyV3ApplyRefTransaction, error) {
					tx, err := prepare(ctx, bin, id, ref, oid, write, ro)
					if err != nil {
						return tx, err
					}
					return &fixtureTidyV3RefuseAfterIntent{tidyV3ApplyRefTransaction: tx}, nil
				}
			}
			r := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, Online: p.Online != nil, ProtectWorktrees: []string{}})
			if !injected || r.Status == "ok" || r.Plan != nil || validateV3Result(r) != nil {
				t.Fatalf("durability failure admitted: injected=%v result=%+v", injected, r)
			}
			if point != "action-complete" {
				requireTidyV3Ref(t, dir, target.Name, target.OID)
			}
			if point == "initial" || point == "recovery-intent" {
				if _, err := exec.Command("git", "-C", dir, "rev-parse", "--verify", v3RecoveryFor(p, target.ActionID, target.OID).Name).Output(); err == nil {
					t.Fatal("recovery before durable intent")
				}
			}
			if point == "terminal-abort" && writesAfterTerminal != 0 {
				t.Fatal("terminal abort persistence retried", writesAfterTerminal)
			}
		})
	}
}

type fixtureTidyV3LostAck struct {
	*gitRefTransaction
	observed gitRefTransactionOutcome
}

func (tx *fixtureTidyV3LostAck) Commit(ctx context.Context, validate func(context.Context) error) (gitRefTransactionOutcome, error) {
	out, err := tx.gitRefTransaction.Commit(ctx, func(callback context.Context) error {
		if err := validate(callback); err != nil {
			return err
		}
		return tx.stdout.Close()
	})
	tx.observed = out
	return out, err
}
func TestTidyV3ApplyActualLostCommitAcknowledgment(t *testing.T) {
	dir, _, p := fixtureTidyV3NativeApply(t)
	c := defaultTidyV3RefsApplyCoordinator()
	prepare := c.prepare
	var tx *fixtureTidyV3LostAck
	c.prepare = func(ctx context.Context, bin string, id NativeIdentity, ref, oid string, write, ro []string) (tidyV3ApplyRefTransaction, error) {
		inner, err := prepare(ctx, bin, id, ref, oid, write, ro)
		if err != nil {
			return inner, err
		}
		tx = &fixtureTidyV3LostAck{gitRefTransaction: inner.(*gitRefTransaction)}
		return tx, nil
	}
	r := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, ProtectWorktrees: []string{}})
	if tx == nil || !tx.observed.Prepared || !tx.observed.CommitAttempted || tx.observed.Committed || !tx.observed.Uncertain || tx.cmd.ProcessState == nil {
		t.Fatalf("lost ACK not exercised %+v", tx)
	}
	if r.Status != "partial" || r.Plan != nil || validateV3Result(r) != nil {
		t.Fatalf("lost ACK became success %+v", r)
	}
	target := v3Eligible(p)[0]
	requireTidyV3Ref(t, dir, v3RecoveryFor(p, target.ActionID, target.OID).Name, target.OID)
	j := readTidyV3ApplyJournal(t, p)
	if j.Phase != "incomplete" || j.Entries[0].Transaction != "unknown" {
		t.Fatalf("lost ACK understated: %+v", j)
	}
	found := false
	for _, effect := range j.KnownEffects {
		found = found || effect == "possible-reference-deletion:"+target.ActionID
	}
	if !found {
		t.Fatal("possible deletion omitted")
	}
}
func TestTidyV3ApplyPreparedDirectRefRaces(t *testing.T) {
	for _, kind := range []string{"oid", "symbolic", "recovery"} {
		t.Run(kind, func(t *testing.T) {
			dir, _, p := fixtureTidyV3NativeApply(t)
			target := v3Eligible(p)[0]
			c := defaultTidyV3RefsApplyCoordinator()
			prepare := c.prepare
			c.prepare = func(ctx context.Context, bin string, id NativeIdentity, ref, oid string, write, ro []string) (tidyV3ApplyRefTransaction, error) {
				tx, err := prepare(ctx, bin, id, ref, oid, write, ro)
				if err != nil {
					return tx, err
				}
				if kind == "recovery" {
					gitFixture(t, dir, "update-ref", v3RecoveryFor(p, target.ActionID, target.OID).Name, p.Expected.BaseCommit)
					return tx, nil
				}
				value := p.Expected.BaseCommit + "\n"
				if kind == "symbolic" {
					value = "ref: " + p.Expected.BaseSelector + "\n"
				}
				if err := os.WriteFile(filepath.Join(id.RepositoryDir, filepath.FromSlash(ref)), []byte(value), 0644); err != nil {
					t.Fatal(err)
				}
				return tx, nil
			}
			r := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, ProtectWorktrees: []string{}})
			if r.Status != "partial" || r.Plan != nil || validateV3Result(r) != nil {
				t.Fatalf("prepared race admitted %+v", r)
			}
			requireTidyV3HistoricalTerminal(t, r)
			if kind == "recovery" {
				requireTidyV3Ref(t, dir, target.Name, target.OID)
				requireTidyV3Ref(t, dir, v3RecoveryFor(p, target.ActionID, target.OID).Name, p.Expected.BaseCommit)
			} else {
				requireTidyV3Ref(t, dir, target.Name, p.Expected.BaseCommit)
				requireTidyV3Ref(t, dir, v3RecoveryFor(p, target.ActionID, target.OID).Name, target.OID)
			}
			j := readTidyV3ApplyJournal(t, p)
			if j.Phase != "incomplete" || j.Entries[0].Transaction != "aborted" {
				t.Fatalf("refused race not confirmed abort: %+v", j.Entries[0])
			}
		})
	}
}
func TestTidyV3ApplyCompletedRetryRejectsRecreatedRef(t *testing.T) {
	dir, _, p := fixtureTidyV3NativeApply(t)
	c := defaultTidyV3RefsApplyCoordinator()
	o := V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, ProtectWorktrees: []string{}}
	if r := c.apply(context.Background(), o); r.Status != "ok" {
		t.Fatal(r)
	}
	target := v3Eligible(p)[0]
	gitFixture(t, dir, "update-ref", target.Name, target.OID)
	journalPath := filepath.Join(p.StateDir, "tidy-v3-operations", p.PlanID+".json")
	before := layoutProtectedFileAt(t, journalPath)
	c.pin = func(context.Context, string, NativeIdentity, TidyRecovery, []string) (bool, error) {
		t.Fatal("completed retry reached native writer")
		return false, nil
	}
	r := c.apply(context.Background(), o)
	if r.Status == "ok" || r.Plan != nil {
		t.Fatalf("recreated reference considered completed %+v", r)
	}
	requireTidyV3Ref(t, dir, target.Name, target.OID)
	if after := layoutProtectedFileAt(t, journalPath); !reflect.DeepEqual(before, after) {
		t.Fatal("completed stale retry rewrote journal")
	}
}

func TestTidyV3ApplyPartialBatchPreservesCompletedPrefix(t *testing.T) {
	dir, _, p := fixtureTidyV3NativeApply(t)
	// Build a separate three-target plan at the same admitted source baseline.
	for _, name := range []string{"integrated-b", "integrated-c"} {
		gitFixture(t, dir, "branch", name, p.Candidates[0].OID)
	}
	planned := defaultTidyV3RefsCoordinator().plan(context.Background(), V3PlanOptions{Path: dir, Base: p.Expected.BaseSelector, StateDir: privateDir(t), Refs: []string{"refs/heads/integrated", "refs/heads/integrated-b", "refs/heads/integrated-c"}, ProtectWorktrees: []string{}})
	if planned.Status != "ok" || planned.Plan == nil {
		t.Fatal(planned)
	}
	p = *planned.Plan
	c := defaultTidyV3RefsApplyCoordinator()
	prepare := c.prepare
	calls := 0
	c.prepare = func(ctx context.Context, bin string, id NativeIdentity, ref, oid string, write, ro []string) (tidyV3ApplyRefTransaction, error) {
		calls++
		tx, err := prepare(ctx, bin, id, ref, oid, write, ro)
		if err != nil {
			return tx, err
		}
		if calls == 2 {
			return &fixtureTidyV3RefuseAfterIntent{tidyV3ApplyRefTransaction: tx}, nil
		}
		return tx, nil
	}
	r := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, ProtectWorktrees: []string{}})
	if r.Status != "partial" || r.Plan != nil || calls != 2 || validateV3Result(r) != nil {
		t.Fatalf("partial batch wrong: calls=%d result=%+v", calls, r)
	}
	j := readTidyV3ApplyJournal(t, p)
	if j.Phase != "incomplete" || j.Entries[0].Phase != "action-complete" || j.Entries[0].Transaction != "confirmed" || j.Entries[1].Transaction != "aborted" || j.Entries[2].RecoveryAttempted {
		t.Fatalf("partial prefix or untouched suffix corrupted %+v", j.Entries)
	}
	eligible := v3Eligible(p)
	for _, target := range eligible[1:] {
		requireTidyV3Ref(t, dir, target.Name, target.OID)
	}
	for _, target := range eligible[:2] {
		requireTidyV3Ref(t, dir, v3RecoveryFor(p, target.ActionID, target.OID).Name, target.OID)
	}
	if _, err := exec.Command("git", "-C", dir, "show-ref", "--verify", "--quiet", eligible[0].Name).Output(); err == nil {
		t.Fatal("completed prefix target restored")
	}
	requireTidyV3NoDiagnostic(t, r, "journal-update-failed")
}

func TestTidyV3ApplyPrepareFailureIdentifiesTargetWithoutReplay(t *testing.T) {
	for _, kind := range []string{"short", "native-3901-byte-ref"} {
		t.Run(kind, func(t *testing.T) {
			var dir string
			var p V3Plan
			if kind == "short" {
				dir, _, p = fixtureTidyV3NativeApply(t)
			} else {
				dir, _ = fixtureTidyV3Git(t)
				ref := "refs/heads/" + strings.Repeat(strings.Repeat("a", 99)+"/", 38) + strings.Repeat("b", 90)
				if len(ref) != 3901 {
					t.Fatal("wrong long-ref fixture length", len(ref))
				}
				gitFixture(t, dir, "update-ref", ref, "HEAD")
				gitFixture(t, dir, "commit", "--allow-empty", "-qm", "advance source")
				planned := defaultTidyV3RefsCoordinator().plan(context.Background(), V3PlanOptions{Path: dir, Base: "HEAD", StateDir: privateDir(t), Refs: []string{ref}, ProtectWorktrees: []string{}})
				if planned.Status != "ok" || planned.Plan == nil || len(v3Eligible(*planned.Plan)) != 1 {
					t.Fatalf("native long ref not admitted: %+v", planned)
				}
				p = *planned.Plan
			}
			requireTidyV3PrepareFailureWithoutReplay(t, dir, p)
		})
	}
}
func requireTidyV3PrepareFailureWithoutReplay(t *testing.T, dir string, p V3Plan) {
	t.Helper()
	target := v3Eligible(p)[0]
	c := defaultTidyV3RefsApplyCoordinator()
	prepares := 0
	c.prepare = func(context.Context, string, NativeIdentity, string, string, []string, []string) (tidyV3ApplyRefTransaction, error) {
		prepares++
		return nil, errors.New("injected native prepare session start failure")
	}
	options := V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, ProtectWorktrees: []string{}}
	var before layoutProtectedFile
	journalPath := filepath.Join(p.StateDir, "tidy-v3-operations", p.PlanID+".json")
	for _, phase := range []string{"initial", "retry"} {
		r := c.apply(context.Background(), options)
		if r.Status != "partial" || r.Plan != nil || r.Boundary.Complete || validateV3Result(r) != nil {
			t.Fatalf("%s unconfirmed prepare became authority: %+v", phase, r)
		}
		found, identified := false, false
		for _, d := range r.Diagnostics {
			if d.Code == "native-prepare-target:"+target.ActionID && d.Message == target.Name {
				identified = true
			}
			if d.Code == "native-prepare-unconfirmed" {
				found = true
				for _, text := range []string{target.ActionID, "acknowledgment", "abort", "process completion", "possible locks", "COMMIT was not sent", "Inspect manually", "no automatic replay"} {
					if !strings.Contains(d.Message, text) {
						t.Errorf("%s diagnostic lacks %q: %s", phase, text, d.Message)
					}
				}
			}
		}
		if !found || !identified {
			t.Errorf("%s lost linked durable prepare diagnostics: %+v", phase, r.Diagnostics)
		}
		requireTidyV3Ref(t, dir, target.Name, target.OID)
		requireTidyV3Ref(t, dir, v3RecoveryFor(p, target.ActionID, target.OID).Name, target.OID)
		j := readTidyV3ApplyJournal(t, p)
		if j.Phase != "incomplete" || j.Entries[0].Phase != "recovery-confirmed" || j.Entries[0].Transaction != "none" || j.Entries[0].Recovery == nil || len(j.Entries[0].Deltas) != 1 {
			t.Fatalf("%s invented acknowledged prepare or lost recovery: %+v", phase, j)
		}
		for _, effect := range j.KnownEffects {
			if strings.HasPrefix(effect, "possible-reference-deletion:") {
				t.Fatal("COMMIT not sent but possible deletion invented")
			}
		}
		if phase == "initial" {
			if len(j.Diagnostics) != 2 || j.Diagnostics[1].Code != "native-prepare-target:"+target.ActionID || j.Diagnostics[1].Message != target.Name {
				t.Fatalf("paired target diagnostic was not persisted atomically: %+v", j.Diagnostics)
			}
			if len(target.Name) == 3901 {
				requireTidyV3PrepareDiagnosticBounds(t, p, j, r, target.ActionID)
			}
			before = layoutProtectedFileAt(t, journalPath)
			c.pin = func(context.Context, string, NativeIdentity, TidyRecovery, []string) (bool, error) {
				t.Fatal("incomplete retry replayed recovery")
				return false, nil
			}
			c.prepare = func(context.Context, string, NativeIdentity, string, string, []string, []string) (tidyV3ApplyRefTransaction, error) {
				t.Fatal("incomplete retry replayed preparation")
				return nil, nil
			}
			c.openStore = func(string, bool, []string, []string) (*v3Store, error) {
				t.Fatal("incomplete retry opened writable coordinator store")
				return nil, nil
			}
		} else if after := layoutProtectedFileAt(t, journalPath); !reflect.DeepEqual(before, after) {
			t.Fatal("incomplete retry rewrote terminal journal")
		}
	}
	if prepares != 1 {
		t.Fatal("prepare was replayed", prepares)
	}
}

// Exercise the newly combined representation at the existing maximum admitted
// ID/text and whole-record context budgets; these checks never change the caps.
func requireTidyV3PrepareDiagnosticBounds(t *testing.T, p V3Plan, j V3Journal, r V3Result, action string) {
	t.Helper()
	maxID := strings.Repeat("a", 128)
	maxRef := "refs/heads/" + strings.Repeat("<", 4096-len("refs/heads/"))
	if !validID(maxID) || !v3Ref(maxRef) {
		t.Fatal("maximum admitted identifier fixtures invalid")
	}
	pair := append([]Diagnostic{}, j.Diagnostics...)
	pair[0].Message = strings.ReplaceAll(pair[0].Message, action, maxID)
	pair[1] = Diagnostic{Code: "native-prepare-target:" + maxID, Message: maxRef}
	r.Diagnostics = pair
	j.Diagnostics = pair
	if validateV3Result(r) != nil || validateV3Journal(j, p) != nil {
		t.Fatal("maximum paired diagnostic fields lost bounded result/journal")
	}
	maxFit := func(fits func(int) bool) int {
		low, high := 0, 1025
		for low+1 < high {
			mid := (low + high) / 2
			if fits(mid) {
				low = mid
			} else {
				high = mid
			}
		}
		if !fits(low) || fits(low+1) {
			t.Fatal("context boundary not enforced")
		}
		return low
	}
	resultContext := func(n int) V3Result {
		next := r
		next.Limitations = append([]string{}, r.Limitations...)
		for i := 0; i < n; i++ {
			next.Limitations = append(next.Limitations, "prior bounded context")
		}
		return next
	}
	countLimit := maxFit(func(n int) bool { return validateV3Result(resultContext(n)) == nil })
	journalContext := func(n int, large bool) V3Journal {
		next := cloneTidyV3Journal(j)
		message := "prior bounded context"
		if large {
			message = strings.Repeat("<", 4096)
		}
		for i := 0; i < n; i++ {
			next.Diagnostics = append(next.Diagnostics, Diagnostic{Code: "prior-context", Message: message})
		}
		return next
	}
	journalCountLimit := maxFit(func(n int) bool { return validateV3Journal(journalContext(n, false), p) == nil })
	byteLimit := maxFit(func(n int) bool { return validateV3Journal(journalContext(n, true), p) == nil })
	encoded, err := json.Marshal(journalContext(byteLimit, true))
	if err != nil || len(encoded) > recordLimit {
		t.Fatal("admitted encoded context invalid", err)
	}
	overflow, err := json.Marshal(journalContext(byteLimit+1, true))
	if err != nil || len(overflow) <= recordLimit {
		t.Fatal("expected existing one-MiB encoded boundary", err)
	}
	t.Logf("paired fields: maxID=%d code=%d summary=%d fullRef=%d; result extra-entry boundary=%d journal extra-entry boundary=%d; encoded journal=%d bytes with %d large-context diagnostics, next=%d bytes refused", len(maxID), len(pair[1].Code), len(pair[0].Message), len(maxRef), countLimit, journalCountLimit, len(encoded), byteLimit, len(overflow))
}

func requireTidyV3HistoricalTerminal(t *testing.T, r V3Result) {
	t.Helper()
	messages := []string{}
	for _, d := range r.Diagnostics {
		if strings.HasPrefix(d.Code, "native-commit-aborted:") {
			messages = append(messages, d.Message)
		}
	}
	for _, a := range r.Actions {
		if strings.HasPrefix(a.Name, "delete-reference:") {
			messages = append(messages, a.Detail)
		}
	}
	if len(messages) == 0 {
		t.Fatal("no terminal transaction disclosure")
	}
	for _, message := range messages {
		for _, claim := range []string{"exact recovery remains", "The target was retained", "Recovery remains retained"} {
			if strings.Contains(message, claim) {
				t.Errorf("unsupported current-state claim after failed final evidence: %s", message)
			}
		}
		for _, fact := range []string{"journal", "current target", "recovery", "manually"} {
			if !strings.Contains(message, fact) {
				t.Errorf("terminal failure lacks historical/current distinction %q: %s", fact, message)
			}
		}
	}
}

type fixtureTidyV3LostAbortAck struct {
	*gitRefTransaction
	observed gitRefTransactionOutcome
}

func (tx *fixtureTidyV3LostAbortAck) Commit(ctx context.Context, validate func(context.Context) error) (gitRefTransactionOutcome, error) {
	out, err := tx.gitRefTransaction.Commit(ctx, func(callback context.Context) error {
		err := validate(callback)
		if err != nil {
			tx.stdout.Close()
		}
		return err
	})
	tx.observed = out
	return out, err
}
func TestTidyV3ApplyStaleRecoveryUnknownAbortAcknowledgment(t *testing.T) {
	dir, _, p := fixtureTidyV3NativeApply(t)
	target := v3Eligible(p)[0]
	recovery := v3RecoveryFor(p, target.ActionID, target.OID)
	c := defaultTidyV3RefsApplyCoordinator()
	prepare := c.prepare
	var tx *fixtureTidyV3LostAbortAck
	c.prepare = func(ctx context.Context, bin string, id NativeIdentity, ref, oid string, write, ro []string) (tidyV3ApplyRefTransaction, error) {
		inner, err := prepare(ctx, bin, id, ref, oid, write, ro)
		if err != nil {
			return inner, err
		}
		gitFixture(t, dir, "update-ref", recovery.Name, p.Expected.BaseCommit)
		tx = &fixtureTidyV3LostAbortAck{gitRefTransaction: inner.(*gitRefTransaction)}
		return tx, nil
	}
	r := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, ProtectWorktrees: []string{}})
	if tx == nil || !tx.observed.Prepared || tx.observed.CommitAttempted || tx.observed.Committed || !tx.observed.Uncertain || tx.cmd.ProcessState == nil {
		t.Fatalf("actual failed abort acknowledgment was not uncertain: %+v", tx)
	}
	if r.Status != "partial" || r.Plan != nil || r.Boundary.Complete || validateV3Result(r) != nil {
		t.Fatalf("stale recovery/abort ACK loss became authority: %+v", r)
	}
	requireTidyV3HistoricalTerminal(t, r)
	requireTidyV3Ref(t, dir, target.Name, target.OID)
	requireTidyV3Ref(t, dir, recovery.Name, p.Expected.BaseCommit)
	j := readTidyV3ApplyJournal(t, p)
	if j.Phase != "incomplete" || j.Entries[0].Transaction != "prepared" || j.Entries[0].Recovery == nil || j.Entries[0].Recovery.CommitID != target.OID {
		t.Fatalf("last acknowledged prepared/recovery evidence was invented or lost: %+v", j.Entries[0])
	}
}
