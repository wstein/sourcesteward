// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
)

type fixtureTidyV3TrackingApplication struct {
	dir            string
	plan           V3Plan
	coordinator    tidyV3TrackingCoordinator
	repository     hosting.TidyRepositoryIdentity
	advertisements []hosting.TidyBranchAdvertisement
	errorAt        map[int]error
	onRead         func(int)
	sessions       []*fixtureTidyV3TrackingHosting
}

func fixtureTidyV3TrackingApply(t *testing.T, count int) *fixtureTidyV3TrackingApplication {
	t.Helper()
	observed, c, plannedSession, _ := fixtureTidyV3TrackingPlanner(t)
	dir := observed.Expected.Identity.WorkspaceRoot
	oid := gitFixture(t, dir, "rev-parse", "refs/remotes/origin/stale")
	selected := []string{"refs/remotes/origin/stale"}
	for i := 1; i < count; i++ {
		ref := "refs/remotes/origin/stale-" + strconv.Itoa(i)
		gitFixture(t, dir, "update-ref", ref, oid)
		selected = append(selected, ref)
	}
	for _, ref := range []string{"refs/remotes/origin/live", "refs/remotes/elsewhere/stale", "refs/heads/keep"} {
		gitFixture(t, dir, "update-ref", ref, oid)
	}

	gitFixture(t, dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/live")
	gitFixture(t, dir, "update-ref", "refs/remotes/origin/selected-base", "HEAD")
	tree := gitFixture(t, dir, "rev-parse", "HEAD^{tree}")
	active := gitFixture(t, dir, "commit-tree", tree, "-p", "HEAD", "-m", "registered tracking workspace")
	gitFixture(t, dir, "update-ref", "refs/remotes/origin/in-use", active)
	gitFixture(t, dir, "worktree", "add", "--detach", filepath.Join(gitTempDir(t), "active"), active)
	selected = append(selected, "refs/remotes/origin/HEAD", "refs/remotes/origin/live", "refs/remotes/origin/selected-base", "refs/remotes/origin/in-use")
	// The primary's unrelated user files remain outside every write grant.
	if err := os.WriteFile(filepath.Join(dir, "user-plan.md"), []byte("preserve primary plan\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "cache"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cache", "ignored"), []byte("preserve ignored cache\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(observed.Expected.Identity.RepositoryDir, "info", "exclude"), []byte("cache/\n"), 0600); err != nil {
		t.Fatal(err)
	}
	planned := c.plan(context.Background(), V3PlanOptions{Path: dir, Base: "HEAD", Online: true, TrackingRemote: "origin", Refs: selected, StateDir: privateDir(t), ProtectWorktrees: []string{}})
	if planned.Status != "ok" || planned.Plan == nil || len(v3Eligible(*planned.Plan)) != count {
		t.Fatalf("tracking apply fixture plan failed %+v", planned)
	}
	f := &fixtureTidyV3TrackingApplication{dir: dir, plan: *planned.Plan, coordinator: c, repository: plannedSession.repo, advertisements: plannedSession.advertisements, errorAt: map[int]error{}}
	f.coordinator.openHosting = func(ctx context.Context, path string, o hosting.TidySessionOptions) (tidyV3TrackingHosting, error) {
		if path != dir || !o.Online || !o.Apply || o.Remote != "origin" || o.Repo != "owner/repo" {
			t.Fatal("wrong acknowledged tracking apply session", o, path)
		}
		session := &fixtureTidyV3TrackingHosting{fixtureTidyV3Hosting: &fixtureTidyV3Hosting{repo: f.repository, rows: map[string]hosting.TidyHeadEvidence{}}, advertisements: f.advertisements, errorAt: f.errorAt, onRead: f.onRead}
		session.deadline, _ = ctx.Deadline()
		f.sessions = append(f.sessions, session)
		return session, nil
	}
	return f
}
func TestTidyV3TrackingApplyOneAndThreeExactRecoveries(t *testing.T) {
	for _, count := range []int{1, 3} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			f := fixtureTidyV3TrackingApply(t, count)
			p := f.plan

			for _, name := range []string{"refs/remotes/origin/HEAD", "refs/remotes/origin/live", "refs/remotes/origin/selected-base", "refs/remotes/origin/in-use"} {
				found := false
				for _, candidate := range p.Candidates {
					if candidate.Name == name {
						found = true
						if candidate.Decision != "retain" {
							t.Fatal("protected native tracking candidate admitted", candidate)
						}
					}
				}
				if !found {
					t.Fatal("protected ref missing from complete planned selection", name)
				}
			}
			special := map[string]string{}
			for _, name := range []string{"refs/remotes/origin/HEAD", "refs/remotes/origin/selected-base", "refs/remotes/origin/in-use"} {
				special[name] = gitFixture(t, f.dir, "rev-parse", name)
			}
			protected := layoutProtectedFiles(t, f.dir, f.dir, p.Expected.Identity.RepositoryDir)
			user := layoutProtectedFileAt(t, filepath.Join(f.dir, "user-plan.md"))
			cache := layoutProtectedFileAt(t, filepath.Join(f.dir, "cache", "ignored"))
			keep := gitFixture(t, f.dir, "rev-parse", "refs/remotes/origin/live")
			options := V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, Online: true, ProtectWorktrees: []string{}}
			r := f.coordinator.apply(context.Background(), options)
			if r.Status != "ok" || r.Plan == nil || !r.Boundary.Complete || validateV3Result(r) != nil {
				t.Fatalf("exact tracking apply failed: status=%s diagnostics=%+v", r.Status, r.Diagnostics)
			}
			if len(f.sessions) != 1 || !f.sessions[0].closed || r.Boundary.Budget.HTTPRequests != int64(f.sessions[0].budget.Requests) {
				t.Fatal("tracking apply reset its session or lost consumed counters", r.Boundary)
			}
			j := readTidyV3ApplyJournal(t, p)
			if j.Phase != "complete" || len(j.Entries) != count {
				t.Fatalf("wrong tracking journal %+v", j)
			}
			current, err := observeTidyV3Git(context.Background(), f.dir, p.Expected.BaseSelector)
			if err != nil {
				t.Fatal(err)
			}
			for _, candidate := range v3Eligible(p) {
				for _, ref := range current.Native.References {
					if ref.Name == candidate.Name {
						t.Fatal("selected stale tracking ref remains", ref)
					}
				}
				requireTidyV3Ref(t, f.dir, v3RecoveryFor(p, candidate.ActionID, candidate.OID).Name, candidate.OID)
			}
			for _, ref := range []string{"refs/remotes/origin/live", "refs/remotes/elsewhere/stale", "refs/heads/keep"} {
				requireTidyV3Ref(t, f.dir, ref, keep)
			}
			for name, oid := range special {
				requireTidyV3Ref(t, f.dir, name, oid)
			}
			requireLayoutProtectedFiles(t, protected)
			if !reflect.DeepEqual(user, layoutProtectedFileAt(t, filepath.Join(f.dir, "user-plan.md"))) || !reflect.DeepEqual(cache, layoutProtectedFileAt(t, filepath.Join(f.dir, "cache", "ignored"))) {
				t.Fatal("tracking cleanup changed primary user data")
			}
			journalPath := filepath.Join(p.StateDir, "tidy-v3-operations", p.PlanID+".json")
			before := layoutProtectedFileAt(t, journalPath)
			retry := f.coordinator.apply(context.Background(), options)
			if retry.Status != "ok" || retry.Plan == nil || len(f.sessions) != 2 || !f.sessions[1].closed {
				t.Fatal("completed tracking retry was not read-only", retry)
			}
			if !reflect.DeepEqual(before, layoutProtectedFileAt(t, journalPath)) {
				t.Fatal("completed tracking retry rewrote journal")
			}
		})
	}
}

func trackingApplyOptions(p V3Plan) V3ApplyOptions {
	return V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, Online: true, ProtectWorktrees: []string{}}
}
func requireTrackingRefAbsent(t *testing.T, dir, ref string) {
	t.Helper()
	o, err := observeTidyV3Git(context.Background(), dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range o.Native.References {
		if row.Name == ref {
			t.Fatal("unexpected local reference", row)
		}
	}
}
func TestTidyV3TrackingApplyResamplesBeforeEffects(t *testing.T) {
	isolateTidyV3Environment(t)
	for _, at := range []int{1, 2, 3} {
		for _, kind := range []string{"unrelated-oid", "source-reappeared", "provider-error", "wrong-repository", "bad-fingerprint", "http-detection-byte"} {
			t.Run(strconv.Itoa(at)+"/"+kind, func(t *testing.T) {
				parallelFixture(t)
				f := fixtureTidyV3TrackingApply(t, 1)
				p := f.plan
				target := v3Eligible(p)[0]
				before := f.advertisements[0]
				changed := before
				switch kind {
				case "unrelated-oid":
					changed = fixtureTidyV3Advertisement(f.repository, []hosting.TidyBranch{{Name: "live", OID: p.Expected.BaseCommit}, {Name: "unrelated", OID: target.OID}})
				case "source-reappeared":
					changed = fixtureTidyV3Advertisement(f.repository, []hosting.TidyBranch{{Name: "live", OID: p.Expected.BaseCommit}, {Name: "stale", OID: p.Expected.BaseCommit}})
				case "provider-error":
					f.errorAt[at] = errors.New("last provider page unavailable")
				case "wrong-repository":
					repo := f.repository
					repo.ID++
					changed = fixtureTidyV3Advertisement(repo, before.Branches)
				case "bad-fingerprint":
					changed.Fingerprint = gitDigest("wrong full inventory")
				case "http-detection-byte":
					f.errorAt[at] = hosting.ErrTidyLimit
					f.onRead = func(n int) {
						if n == at {
							f.sessions[0].budget.RawBytes = (32 << 20) + 1
						}
					}
				}
				if kind != "provider-error" && kind != "http-detection-byte" {
					f.advertisements = nil
					for i := 1; i < at; i++ {
						f.advertisements = append(f.advertisements, before)
					}
					f.advertisements = append(f.advertisements, changed)
				}
				r := f.coordinator.apply(context.Background(), trackingApplyOptions(p))
				if r.Status == "ok" || r.Plan != nil || r.Boundary.Complete || validateV3Result(r) != nil {
					t.Fatalf("incomplete tracking proof authorized %+v", r)
				}
				requireTidyV3Ref(t, f.dir, target.Name, target.OID)
				requireTrackingRefAbsent(t, f.dir, v3RecoveryFor(p, target.ActionID, target.OID).Name)
				if len(f.sessions) != 1 || !f.sessions[0].closed || f.sessions[0].reads != at || r.Boundary.Budget.HTTPRequests != int64(at) || r.Boundary.Budget.HTTPBytes != f.sessions[0].budget.RawBytes {
					t.Fatal("failed sample lost/reset its shared ledger", r.Boundary)
				}
				if at > 1 {
					j := readTidyV3ApplyJournal(t, p)
					if j.Phase != "incomplete" || j.Entries[0].Transaction != "none" || j.Entries[0].Recovery != nil {
						t.Fatal("pre-recovery failure invented native effect", j)
					}
					retry := f.coordinator.apply(context.Background(), trackingApplyOptions(p))
					if retry.Status == "ok" || len(f.sessions) != 1 {
						t.Fatal("incomplete tracking operation replayed provider/native work", retry)
					}
				}
			})
		}
	}
}
func TestTidyV3TrackingApplyPreparedAdvertisementChangeAborts(t *testing.T) {
	for _, kind := range []string{"unrelated-oid", "source-reappeared"} {
		t.Run(kind, func(t *testing.T) {
			f := fixtureTidyV3TrackingApply(t, 1)
			p := f.plan
			target := v3Eligible(p)[0]
			apply := f.coordinator.referenceApply()
			prepare := apply.prepare
			var transaction tidyV3ApplyRefTransaction
			apply.prepare = func(ctx context.Context, bin string, id NativeIdentity, ref, oid string, write, ro []string) (tidyV3ApplyRefTransaction, error) {
				tx, err := prepare(ctx, bin, id, ref, oid, write, ro)
				if err != nil {
					return tx, err
				}
				transaction = tx
				rows := []hosting.TidyBranch{{Name: "live", OID: p.Expected.BaseCommit}, {Name: "unrelated", OID: target.OID}}
				if kind == "source-reappeared" {
					rows[1].Name = "stale"
				}
				f.sessions[0].advertisements = []hosting.TidyBranchAdvertisement{fixtureTidyV3Advertisement(f.repository, rows)}
				return tx, nil
			}
			r := apply.apply(context.Background(), trackingApplyOptions(p))
			if transaction == nil || transaction.outcome().CommitAttempted || transaction.outcome().Committed || transaction.outcome().Uncertain || !transaction.outcome().Prepared {
				t.Fatal("prepared provider change did not confirm no-send abort", transaction)
			}
			if r.Status != "partial" || r.Plan != nil || validateV3Result(r) != nil {
				t.Fatal("prepared remote change became success", r)
			}
			requireTidyV3HistoricalTerminal(t, r)
			requireTidyV3Ref(t, f.dir, target.Name, target.OID)
			requireTidyV3Ref(t, f.dir, v3RecoveryFor(p, target.ActionID, target.OID).Name, target.OID)
			j := readTidyV3ApplyJournal(t, p)
			if j.Phase != "incomplete" || j.Entries[0].Transaction != "aborted" || j.Entries[0].Recovery == nil {
				t.Fatal("prepared absence failure lost acknowledged history", j)
			}
			for _, effect := range j.KnownEffects {
				if effect == "possible-reference-deletion:"+target.ActionID {
					t.Fatal("confirmed no-send abort invented possible deletion")
				}
			}
			retry := apply.apply(context.Background(), trackingApplyOptions(p))
			if retry.Status == "ok" || len(f.sessions) != 1 {
				t.Fatal("prepared incomplete retry replayed native/provider work", retry)
			}
		})
	}
}

func TestTidyV3TrackingApplyNativeChangesDuringProvider(t *testing.T) {
	for _, kind := range []string{"config-bytes", "mapping", "target-oid", "symbolic", "workspace"} {
		t.Run(kind, func(t *testing.T) {
			f := fixtureTidyV3TrackingApply(t, 1)
			p := f.plan
			target := v3Eligible(p)[0]
			f.onRead = func(n int) {
				if n != 1 {
					return
				}
				switch kind {
				case "config-bytes":
					path := filepath.Join(p.Expected.Identity.RepositoryDir, "config")
					data, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, append(data, []byte("\n# literal actor change\n")...), 0644); err != nil {
						t.Fatal(err)
					}
				case "mapping":
					gitFixture(t, f.dir, "config", "remote.origin.fetch", "+refs/heads/topic/*:refs/remotes/origin/*")
				case "target-oid":
					gitFixture(t, f.dir, "update-ref", target.Name, p.Expected.BaseCommit)
				case "symbolic":
					gitFixture(t, f.dir, "symbolic-ref", target.Name, p.Expected.Identity.HeadRef)
				case "workspace":
					gitFixture(t, f.dir, "worktree", "add", "--detach", filepath.Join(gitTempDir(t), "new-active"), target.OID)
				}
			}
			r := f.coordinator.apply(context.Background(), trackingApplyOptions(p))
			if r.Status == "ok" || r.Plan != nil || validateV3Result(r) != nil {
				t.Fatal("native change during complete provider sample admitted", r)
			}
			requireTrackingRefAbsent(t, f.dir, v3RecoveryFor(p, target.ActionID, target.OID).Name)
			oid := target.OID
			if kind == "target-oid" || kind == "symbolic" {
				oid = p.Expected.BaseCommit
			}
			requireTidyV3Ref(t, f.dir, target.Name, oid)
		})
	}
}
func TestTidyV3TrackingApplyPreparedNativeChangesAbort(t *testing.T) {
	for _, kind := range []string{"target-oid", "symbolic", "recovery"} {
		t.Run(kind, func(t *testing.T) {
			f := fixtureTidyV3TrackingApply(t, 1)
			p := f.plan
			target := v3Eligible(p)[0]
			recovery := v3RecoveryFor(p, target.ActionID, target.OID)
			apply := f.coordinator.referenceApply()
			prepare := apply.prepare
			var transaction tidyV3ApplyRefTransaction
			apply.prepare = func(ctx context.Context, bin string, id NativeIdentity, ref, oid string, write, ro []string) (tidyV3ApplyRefTransaction, error) {
				tx, err := prepare(ctx, bin, id, ref, oid, write, ro)
				if err != nil {
					return tx, err
				}
				transaction = tx
				if kind == "recovery" {
					gitFixture(t, f.dir, "update-ref", recovery.Name, p.Expected.BaseCommit)
				} else {
					value := p.Expected.BaseCommit + "\n"
					if kind == "symbolic" {
						value = "ref: " + p.Expected.Identity.HeadRef + "\n"
					}
					if err := os.WriteFile(filepath.Join(id.RepositoryDir, filepath.FromSlash(ref)), []byte(value), 0644); err != nil {
						t.Fatal(err)
					}
				}
				return tx, nil
			}
			r := apply.apply(context.Background(), trackingApplyOptions(p))
			if transaction == nil || !transaction.outcome().Prepared || transaction.outcome().CommitAttempted || transaction.outcome().Committed || transaction.outcome().Uncertain {
				t.Fatal("native race did not confirm no-send abort", transaction)
			}
			if r.Status != "partial" || r.Plan != nil || validateV3Result(r) != nil {
				t.Fatal("prepared native race admitted", r)
			}
			requireTidyV3HistoricalTerminal(t, r)
			currentTarget, currentRecovery := target.OID, target.OID
			if kind == "recovery" {
				currentRecovery = p.Expected.BaseCommit
			} else {
				currentTarget = p.Expected.BaseCommit
			}
			requireTidyV3Ref(t, f.dir, target.Name, currentTarget)
			requireTidyV3Ref(t, f.dir, recovery.Name, currentRecovery)
			j := readTidyV3ApplyJournal(t, p)
			if j.Phase != "incomplete" || j.Entries[0].Transaction != "aborted" {
				t.Fatal("native race journal misstates ACK", j)
			}
		})
	}
}
func TestTidyV3TrackingApplyPartialPrefixKeepsUntouchedSuffix(t *testing.T) {
	f := fixtureTidyV3TrackingApply(t, 3)
	p := f.plan
	targets := v3Eligible(p)
	f.onRead = func(n int) {
		if n == 8 {
			f.sessions[0].advertisements = []hosting.TidyBranchAdvertisement{fixtureTidyV3Advertisement(f.repository, []hosting.TidyBranch{{Name: "live", OID: p.Expected.BaseCommit}, {Name: "new-unrelated", OID: targets[0].OID}})}
		}
	}
	r := f.coordinator.apply(context.Background(), trackingApplyOptions(p))
	if r.Status != "partial" || r.Plan != nil || validateV3Result(r) != nil {
		t.Fatal("changed batch advertisement admitted", r)
	}
	j := readTidyV3ApplyJournal(t, p)
	if j.Phase != "incomplete" || j.Entries[0].Phase != "action-complete" || j.Entries[0].Transaction != "confirmed" || j.Entries[1].RecoveryAttempted || j.Entries[2].RecoveryAttempted {
		t.Fatal("acknowledged prefix/untouched suffix lost", j)
	}
	requireTrackingRefAbsent(t, f.dir, targets[0].Name)
	requireTidyV3Ref(t, f.dir, v3RecoveryFor(p, targets[0].ActionID, targets[0].OID).Name, targets[0].OID)
	for _, target := range targets[1:] {
		requireTidyV3Ref(t, f.dir, target.Name, target.OID)
		requireTrackingRefAbsent(t, f.dir, v3RecoveryFor(p, target.ActionID, target.OID).Name)
	}
	before := layoutProtectedFileAt(t, filepath.Join(p.StateDir, "tidy-v3-operations", p.PlanID+".json"))
	retry := f.coordinator.apply(context.Background(), trackingApplyOptions(p))
	if retry.Status == "ok" || len(f.sessions) != 1 || !reflect.DeepEqual(before, layoutProtectedFileAt(t, filepath.Join(p.StateDir, "tidy-v3-operations", p.PlanID+".json"))) {
		t.Fatal("partial prefix replayed or journal rewritten", retry)
	}
}
func TestTidyV3TrackingApplyCompletedRetryRequiresFreshAdvertisement(t *testing.T) {
	f := fixtureTidyV3TrackingApply(t, 1)
	p := f.plan
	if r := f.coordinator.apply(context.Background(), trackingApplyOptions(p)); r.Status != "ok" {
		t.Fatal(r)
	}
	before := layoutProtectedFileAt(t, filepath.Join(p.StateDir, "tidy-v3-operations", p.PlanID+".json"))
	f.advertisements = []hosting.TidyBranchAdvertisement{fixtureTidyV3Advertisement(f.repository, []hosting.TidyBranch{{Name: "stale", OID: p.Expected.BaseCommit}})}
	apply := f.coordinator.referenceApply()
	apply.pin = func(context.Context, string, NativeIdentity, TidyRecovery, []string) (bool, error) {
		t.Fatal("completed retry reached pin")
		return false, nil
	}
	apply.prepare = func(context.Context, string, NativeIdentity, string, string, []string, []string) (tidyV3ApplyRefTransaction, error) {
		t.Fatal("completed retry reached native writer")
		return nil, nil
	}
	r := apply.apply(context.Background(), trackingApplyOptions(p))
	if r.Status == "ok" || r.Plan != nil || len(f.sessions) != 2 || !reflect.DeepEqual(before, layoutProtectedFileAt(t, filepath.Join(p.StateDir, "tidy-v3-operations", p.PlanID+".json"))) {
		t.Fatal("completed retry trusted stale advertisement or changed journal", r)
	}
}

func TestTidyV3TrackingApplyActualUncertainNativeOutcomes(t *testing.T) {
	for _, kind := range []string{"cancel-after-intent", "lost-commit-ack"} {
		t.Run(kind, func(t *testing.T) {
			f := fixtureTidyV3TrackingApply(t, 1)
			p := f.plan
			target := v3Eligible(p)[0]
			apply := f.coordinator.referenceApply()
			prepare := apply.prepare
			var observed func() gitRefTransactionOutcome
			apply.prepare = func(ctx context.Context, bin string, id NativeIdentity, ref, oid string, write, ro []string) (tidyV3ApplyRefTransaction, error) {
				inner, err := prepare(ctx, bin, id, ref, oid, write, ro)
				if err != nil {
					return inner, err
				}
				if kind == "cancel-after-intent" {
					tx := &fixtureTidyV3CancelAfterIntent{tidyV3ApplyRefTransaction: inner}
					observed = func() gitRefTransactionOutcome { return tx.observed }
					return tx, nil
				}
				tx := &fixtureTidyV3LostAck{gitRefTransaction: inner.(*gitRefTransaction)}
				observed = func() gitRefTransactionOutcome { return tx.observed }
				return tx, nil
			}
			r := apply.apply(context.Background(), trackingApplyOptions(p))
			if observed == nil {
				t.Fatal("native uncertainty fixture not reached")
			}
			out := observed()
			if !out.Prepared || out.Committed || !out.Uncertain || out.CommitAttempted != (kind == "lost-commit-ack") {
				t.Fatal("native observed outcome was misrepresented", out)
			}
			if r.Status != "partial" || r.Plan != nil || validateV3Result(r) != nil {
				t.Fatal("uncertain tracking deletion became success", r)
			}
			requireTidyV3HistoricalTerminal(t, r)
			requireTidyV3Ref(t, f.dir, v3RecoveryFor(p, target.ActionID, target.OID).Name, target.OID)
			if kind == "cancel-after-intent" {
				requireTidyV3Ref(t, f.dir, target.Name, target.OID)
			}
			j := readTidyV3ApplyJournal(t, p)
			if j.Phase != "incomplete" || j.Entries[0].Transaction != "unknown" || !containsString(j.KnownEffects, "possible-reference-deletion:"+target.ActionID) {
				t.Fatal("uncertain intent lost durable possible effect", j)
			}
			before := layoutProtectedFileAt(t, filepath.Join(p.StateDir, "tidy-v3-operations", p.PlanID+".json"))
			retry := apply.apply(context.Background(), trackingApplyOptions(p))
			if retry.Status != "partial" || retry.Plan != nil || len(f.sessions) != 1 || !reflect.DeepEqual(before, layoutProtectedFileAt(t, filepath.Join(p.StateDir, "tidy-v3-operations", p.PlanID+".json"))) {
				t.Fatal("unknown tracking operation replayed", retry)
			}
		})
	}
}
func TestTidyV3TrackingApplyDurableIntentBeforeEffects(t *testing.T) {
	for _, point := range []string{"recovery-intent", "commit-intent", "action-complete"} {
		t.Run(point, func(t *testing.T) {
			f := fixtureTidyV3TrackingApply(t, 1)
			p := f.plan
			target := v3Eligible(p)[0]
			apply := f.coordinator.referenceApply()
			open := apply.openStore
			injected := false
			apply.openStore = func(path string, write bool, registered, protected []string) (*v3Store, error) {
				s, err := open(path, write, registered, protected)
				if err != nil {
					return nil, err
				}
				original := s.writeRecord
				s.writeRecord = func(kind, id string, value any, exclusive bool) error {
					if j, ok := value.(V3Journal); ok && !injected {
						hit := point == "recovery-intent" && j.Entries[0].RecoveryAttempted && j.Entries[0].Recovery == nil || point == "commit-intent" && j.Entries[0].Transaction == "commit-attempted" || point == "action-complete" && j.Phase == "action-complete"
						if hit {
							injected = true
							return errors.New("injected durable tracking journal failure")
						}
					}
					return original(kind, id, value, exclusive)
				}
				return s, nil
			}
			r := apply.apply(context.Background(), trackingApplyOptions(p))
			if !injected || r.Status != "partial" || r.Plan != nil || validateV3Result(r) != nil {
				t.Fatal("unacknowledged durable state authorized tracking effect", r, injected)
			}
			if point == "action-complete" {
				requireTrackingRefAbsent(t, f.dir, target.Name)
			} else {
				requireTidyV3Ref(t, f.dir, target.Name, target.OID)
			}
			if point == "recovery-intent" {
				requireTrackingRefAbsent(t, f.dir, v3RecoveryFor(p, target.ActionID, target.OID).Name)
			} else {
				requireTidyV3Ref(t, f.dir, v3RecoveryFor(p, target.ActionID, target.OID).Name, target.OID)
			}
			j := readTidyV3ApplyJournal(t, p)
			if j.Phase != "incomplete" {
				t.Fatal("durable failure left executable state", j)
			}
			before := layoutProtectedFileAt(t, filepath.Join(p.StateDir, "tidy-v3-operations", p.PlanID+".json"))
			retry := apply.apply(context.Background(), trackingApplyOptions(p))
			if retry.Status == "ok" || retry.Plan != nil || len(f.sessions) != 1 || !reflect.DeepEqual(before, layoutProtectedFileAt(t, filepath.Join(p.StateDir, "tidy-v3-operations", p.PlanID+".json"))) {
				t.Fatal("durable incomplete tracking operation replayed", retry)
			}
		})
	}
}

// Even an internally consistent stored fingerprint cannot turn a present mapped
// source into absence. The typed proof boundary checks that fact explicitly.
func TestTidyV3TrackingApplyProofRejectsPresentSourceWithMatchingHash(t *testing.T) {
	f := fixtureTidyV3TrackingApply(t, 1)
	p := f.plan
	target := v3Eligible(p)[0]
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := f.coordinator.openHosting(ctx, f.dir, hosting.TidySessionOptions{Online: true, Apply: true, Remote: "origin", Repo: "owner/repo"})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if err := revalidateTidyV3TrackingProof(ctx, p, target, session); err != nil {
		t.Fatal("unchanged complete absence rejected", err)
	}
	for _, oid := range []string{target.OID, p.Expected.BaseCommit} {
		live := fixtureTidyV3Advertisement(f.repository, []hosting.TidyBranch{{Name: "stale", OID: oid}})
		f.sessions[0].advertisements = []hosting.TidyBranchAdvertisement{live}
		p.Online.ObservationDigests = []string{live.Fingerprint}
		target.Evidence.Absence.AdvertisementDigest = live.Fingerprint
		if !validV3Candidate(target, p) {
			t.Fatal("matching-hash counterexample must reach semantic absence guard")
		}
		if err := revalidateTidyV3TrackingProof(ctx, p, target, session); err == nil {
			t.Fatal("present remote source mistaken for absence", oid)
		}
	}
	if err := revalidateTidyV3TrackingProof(ctx, p, target, &fixtureTidyV3Hosting{repo: f.repository}); err == nil {
		t.Fatal("missing complete-advertisement port accepted")
	}
}
