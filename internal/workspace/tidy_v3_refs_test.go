// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/inspection"
)

// Isolate before the first Git command: fixture setup is outside the native
// production sandbox and must never inherit another repository's controls.
func fixtureTidyV3Git(t *testing.T) (string, string) {
	t.Helper()
	var bin string
	if _, shared := parallelFixtures.Load(t.Name()); shared {
		// A parallel test cannot change the environment: its serial parent already
		// called isolateTidyV3Environment.
		var err error
		if bin, err = gitExecutable(); err != nil {
			t.Fatal(err)
		}
	} else {
		bin = isolateTidyV3Environment(t)
	}
	return fixtureGit(t), bin
}

// isolateTidyV3Environment changes the process environment for the test and its
// cleanup, so only a serial test or the serial parent of parallel subtests may call it.
func isolateTidyV3Environment(t *testing.T) string {
	t.Helper()
	for _, item := range os.Environ() {
		key, value, _ := strings.Cut(item, "=")
		if strings.HasPrefix(key, "GIT_") {
			if err := os.Unsetenv(key); err != nil {
				t.Fatal(err)
			}
			key, value := key, value
			t.Cleanup(func() {
				if err := os.Setenv(key, value); err != nil {
					t.Error(err)
				}
			})
		}
	}
	t.Setenv("HOME", gitTempDir(t))
	t.Setenv("XDG_CONFIG_HOME", gitTempDir(t))
	t.Setenv("GH_CONFIG_DIR", gitTempDir(t))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("native Git unavailable: ", err)
	}
	bin, err := gitExecutable()
	if err != nil {
		t.Skip("supported native Git unavailable: ", err)
	}
	t.Setenv("PATH", filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))
	return bin
}

func tidyV3AncestryAt(bin, dir string) tidyV3Ancestry {
	return func(ctx context.Context, target, base string) (bool, error) {
		_, code, err := runSandboxed(ctx, bin, []string{"--no-replace-objects", "merge-base", "--is-ancestor", target, base}, dir, nil, nil)
		if err != nil {
			return false, err
		}
		if code > 1 {
			return false, failure("ancestry-unavailable", "Complete native ancestry is unavailable.")
		}
		return code == 0, nil
	}
}

func tidyV3PR(repo hosting.TidyRepositoryIdentity, head, base, result, originalBase string, number int) hosting.TidyHeadEvidence {
	merged := "2026-10-18T00:00:00Z"
	id := repo.TidyRepositoryID
	pr := hosting.TidyPullRequest{Repository: repo, ID: int64(100 + number), NodeID: "PR_" + strings.Repeat("x", number), Number: number, State: "closed", Merged: true, MergedAt: &merged, HeadRepository: &id, HeadRef: "topic", HeadOID: head, BaseRepository: &id, BaseRef: originalBase, BaseOID: base, MergeOID: &result}
	return hosting.TidyHeadEvidence{HeadOID: head, Reason: "github-merged-head", PullRequest: &pr}
}

func fixtureTidyV3Merged(t *testing.T) (string, string, Snapshot, hosting.TidyRepositoryIdentity, []hosting.TidyHeadEvidence) {
	t.Helper()
	dir, bin := fixtureTidyV3Git(t)
	root := gitFixture(t, dir, "rev-parse", "HEAD")
	tree := gitFixture(t, dir, "rev-parse", "HEAD^{tree}")
	repo := hosting.TidyRepositoryIdentity{Host: "github.com", TidyRepositoryID: hosting.TidyRepositoryID{ID: 1, NodeID: "R_fixture", FullName: "owner/repo"}, DefaultBranch: "main"}
	rows := []hosting.TidyHeadEvidence{}
	parent := root
	for i := 0; i < 9; i++ {
		head := gitFixture(t, dir, "commit-tree", tree, "-p", root, "-m", "original "+strings.Repeat("x", i+1))
		result := gitFixture(t, dir, "commit-tree", tree, "-p", parent, "-m", "integrated "+strings.Repeat("x", i+1))
		parent = result
		gitFixture(t, dir, "update-ref", "refs/heads/topic-"+strings.Repeat("x", i+1), head)
		originalBase := "main"
		if i >= 2 {
			originalBase = "deleted-stack-" + strings.Repeat("x", i)
		}
		rows = append(rows, tidyV3PR(repo, head, root, result, originalBase, i+1))
	}
	gitFixture(t, dir, "update-ref", "refs/heads/selected-base", parent)
	expected, err := (gitAdapter{}).Observe(context.Background(), dir, "refs/heads/selected-base")
	if err != nil {
		t.Fatal(err)
	}
	return dir, bin, expected, repo, rows
}
func TestTidyV3NineExactMergedHeadsWithSevenStackedBases(t *testing.T) {
	dir, bin, expected, repo, rows := fixtureTidyV3Merged(t)
	for i, evidence := range rows {
		proof, err := proveTidyV3Commit(context.Background(), expected, evidence.HeadOID, []string{}, &repo, &evidence, tidyV3AncestryAt(bin, dir))
		if err != nil || proof.Decision != "delete" || proof.Proof != "github-merged-head" || proof.Evidence == nil || proof.Evidence.GitHub.Base.Ref != evidence.PullRequest.BaseRef || proof.Evidence.GitHub.ChosenBaseOID != expected.BaseCommit {
			t.Fatalf("exact merged head %d failed: proof=%+v err=%v", i+1, proof, err)
		}
	}
}

func TestTidyV3StatelessNativePlanAndProtection(t *testing.T) {
	dir, _ := fixtureTidyV3Git(t)
	root := gitFixture(t, dir, "rev-parse", "HEAD")
	gitFixture(t, dir, "branch", "integrated", root)
	gitFixture(t, dir, "commit", "--allow-empty", "-qm", "advance base")
	base := gitFixture(t, dir, "symbolic-ref", "HEAD")
	gitFixture(t, dir, "branch", "base-alias", "HEAD")
	gitFixture(t, dir, "symbolic-ref", "refs/heads/alias", "refs/heads/integrated")
	before := gitFixture(t, dir, "for-each-ref", "--format=%(refname) %(objectname) %(symref)")
	c := defaultTidyV3RefsCoordinator()
	r := c.plan(context.Background(), V3PlanOptions{Path: dir, Base: base, ProtectWorktrees: []string{}})
	if r.Status != "ok" || r.Plan == nil || !r.Boundary.Complete || r.Boundary.NetworkAttempted || r.Plan.PlanID != "" || validateV3Result(r) != nil {
		t.Fatalf("stateless plan incomplete: %+v", r)
	}
	got := map[string]V3Candidate{}
	for _, v := range r.Plan.Candidates {
		got[v.Name] = v
	}
	for _, name := range []string{base, "refs/heads/base-alias", "refs/heads/alias", "refs/heads/integrated"} {
		if got[name].Decision != "retain" {
			t.Fatalf("unprotected %s: %+v", name, got[name])
		}
	}
	if after := gitFixture(t, dir, "for-each-ref", "--format=%(refname) %(objectname) %(symref)"); after != before {
		t.Fatal("planning changed native references")
	}
	selected := c.plan(context.Background(), V3PlanOptions{Path: dir, Base: base, Refs: []string{"refs/heads/not-observed"}, ProtectWorktrees: []string{}})
	if selected.Status == "ok" || selected.Plan != nil {
		t.Fatal("unobserved selector became an executable plan")
	}
}

type fixtureTidyV3Hosting struct {
	repo                        hosting.TidyRepositoryIdentity
	rows                        map[string]hosting.TidyHeadEvidence
	targets                     [][]string
	hintCalls                   []map[string]int
	rechecks                    []hosting.TidyPullRequest
	discoverErr, errorOnRecheck error
	budget                      hosting.TidyHTTPBudget
	closed                      bool
	onDiscover                  func()
}

func (s *fixtureTidyV3Hosting) Repository() hosting.TidyRepositoryIdentity     { return s.repo }
func (s *fixtureTidyV3Hosting) Budget() hosting.TidyHTTPBudget                 { return s.budget }
func (s *fixtureTidyV3Hosting) Close()                                         { s.closed = true }
func (s *fixtureTidyV3Hosting) RevalidateRepository(ctx context.Context) error { return ctx.Err() }
func (s *fixtureTidyV3Hosting) DiscoverPullRequests(ctx context.Context, targets []string, hints map[string]int) ([]hosting.TidyHeadEvidence, error) {
	s.targets = append(s.targets, append([]string{}, targets...))
	s.hintCalls = append(s.hintCalls, hints)
	if s.onDiscover != nil {
		s.onDiscover()
	}
	if s.discoverErr != nil {
		return nil, s.discoverErr
	}
	rows := []hosting.TidyHeadEvidence{}
	for _, oid := range targets {
		row, ok := s.rows[oid]
		if !ok {
			row = hosting.TidyHeadEvidence{HeadOID: oid, Reason: "integration-not-proven"}
		}
		rows = append(rows, row)
	}
	return rows, ctx.Err()
}
func (s *fixtureTidyV3Hosting) RevalidatePullRequest(ctx context.Context, pr hosting.TidyPullRequest) error {
	s.rechecks = append(s.rechecks, pr)
	if s.errorOnRecheck != nil {
		return s.errorOnRecheck
	}
	if row, ok := s.rows[pr.HeadOID]; !ok || row.PullRequest == nil || !reflect.DeepEqual(*row.PullRequest, pr) {
		return hosting.ErrTidyChanged
	}
	return ctx.Err()
}
func fixtureTidyV3Online(t *testing.T, dir string, repo hosting.TidyRepositoryIdentity, rows []hosting.TidyHeadEvidence) (tidyV3RefsCoordinator, *fixtureTidyV3Hosting) {
	t.Helper()
	gitFixture(t, dir, "remote", "add", "origin", "https://github.com/owner/repo.git")
	session := &fixtureTidyV3Hosting{repo: repo, rows: map[string]hosting.TidyHeadEvidence{}, budget: hosting.TidyHTTPBudget{Requests: 1, RawBytes: 1, NetworkAttempted: true}}
	for _, row := range rows {
		session.rows[row.HeadOID] = row
	}
	c := defaultTidyV3RefsCoordinator()
	c.openHosting = func(context.Context, string, hosting.TidySessionOptions) (tidyV3Hosting, error) { return session, nil }
	return c, session
}
func TestTidyV3CoordinatorNineProviderHeadsAndStoredPlan(t *testing.T) {
	dir, _, expected, repo, rows := fixtureTidyV3Merged(t)
	c, session := fixtureTidyV3Online(t, dir, repo, rows)
	o := V3PlanOptions{Path: dir, Base: expected.BaseSelector, Online: true, Remote: "origin", IntegrationProofs: []string{"ancestor"}, StateDir: privateDir(t), ProtectWorktrees: []string{}}
	before := gitFixture(t, dir, "for-each-ref", "--format=%(refname) %(objectname) %(symref)")
	r := c.plan(context.Background(), o)
	if r.Status != "ok" || r.Plan == nil || validateV3Result(r) != nil {
		t.Fatalf("provider plan failed: %+v diagnostics=%+v", r, r.Diagnostics)
	}
	count := 0
	for _, candidate := range r.Plan.Candidates {
		if candidate.Decision == "delete" {
			count++
			if candidate.Proof != "github-merged-head" {
				t.Fatalf("wrong proof %+v", candidate)
			}
		}
	}
	if count != 9 || len(session.targets) != 1 || len(session.targets[0]) != 9 || len(session.rechecks) != 0 || !session.closed || !r.Boundary.NetworkAttempted {
		t.Fatalf("incomplete provider work: count=%d session=%+v boundary=%+v", count, session, r.Boundary)
	}
	native, err := observeTidyV3Git(context.Background(), dir, o.Base)
	if err != nil {
		t.Fatal(err)
	}
	store, err := openV3Store(o.StateDir, false, v3NativeRoots(native.Native), []string{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	stored, baseline, err := store.readPlan(r.Plan.PlanID)
	if err != nil || !reflect.DeepEqual(stored, *r.Plan) || !reflect.DeepEqual(baseline, native.Native) {
		t.Fatalf("wrong stored authority: %v", err)
	}
	if after := gitFixture(t, dir, "for-each-ref", "--format=%(refname) %(objectname) %(symref)"); after != before {
		t.Fatal("provider planning changed refs")
	}
}
func TestTidyV3CompleteNegativeAndUnavailableAreDistinct(t *testing.T) {
	for _, kind := range []string{"negative", "unavailable", "missing-object", "nonreachable", "wrong-repository", "amended-tip", "patch-only"} {
		t.Run(kind, func(t *testing.T) {
			dir, _, expected, repo, rows := fixtureTidyV3Merged(t)
			row := rows[0]
			rows = rows[:1]
			switch kind {
			case "negative", "patch-only":
				row.PullRequest = nil
				row.Reason = "integration-not-proven"
			case "missing-object":
				missing := strings.Repeat("a", 40)
				row.PullRequest.MergeOID = &missing
			case "nonreachable":
				row.PullRequest.MergeOID = &rows[0].HeadOID
			case "wrong-repository":
				wrong := repo.TidyRepositoryID
				wrong.ID++
				row.PullRequest.HeadRepository = &wrong
			case "amended-tip":
				row.PullRequest.HeadOID = rows[0].PullRequest.BaseOID
			}
			c, s := fixtureTidyV3Online(t, dir, repo, []hosting.TidyHeadEvidence{row})
			if kind == "unavailable" {
				s.discoverErr = errors.New("provider unavailable")
			}
			o := V3PlanOptions{Path: dir, Base: expected.BaseSelector, Online: true, Remote: "origin", IntegrationProofs: []string{"ancestor"}, Refs: []string{"refs/heads/topic-x"}, ProtectWorktrees: []string{}}
			r := c.plan(context.Background(), o)
			incomplete := kind == "unavailable" || kind == "missing-object"
			if incomplete {
				if r.Status == "ok" || r.Plan != nil || r.Boundary.Complete {
					t.Fatalf("incomplete provider evidence authorized %+v", r)
				}
			} else {
				if r.Status != "ok" || r.Plan == nil || r.Plan.Candidates[0].Decision != "retain" || !r.Boundary.Complete {
					t.Fatalf("complete negative not retained %+v", r)
				}
			}
			if !r.Boundary.NetworkAttempted || validateV3Result(r) != nil {
				t.Fatalf("untruthful boundary %+v", r)
			}
		})
	}
}
func TestTidyV3CommitProofPreservesWorkspacePathsAndRefusesCancellation(t *testing.T) {
	dir, _, expected, repo, rows := fixtureTidyV3Merged(t)
	row := rows[0]
	paths := []string{dir}
	proof, err := proveTidyV3Commit(context.Background(), expected, row.HeadOID, paths, &repo, &row, func(context.Context, string, string) (bool, error) { return true, nil })
	if err != nil || !reflect.DeepEqual(proof.WorkspacePaths, paths) || proof.Proof != "native-ancestry" {
		t.Fatalf("commit evidence invented or lost workspace identity: %+v %v", proof, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	_, err = proveTidyV3Commit(ctx, expected, row.HeadOID, []string{}, nil, nil, func(context.Context, string, string) (bool, error) { cancel(); return true, nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation became a successful integration proof: %v", err)
	}
}

func TestTidyV3AllRegisteredWorkspacesProtectBeforeProviderLookup(t *testing.T) {
	dir, _, expected, repo, rows := fixtureTidyV3Merged(t)
	detached := filepath.Join(gitTempDir(t), "detached")
	attached := filepath.Join(gitTempDir(t), "attached")
	gitFixture(t, dir, "worktree", "add", "--detach", detached, rows[0].HeadOID)
	gitFixture(t, dir, "worktree", "add", attached, "topic-xx")
	gitFixture(t, dir, "worktree", "lock", "--reason", "active fixture", attached)
	c, s := fixtureTidyV3Online(t, dir, repo, rows)
	r := c.plan(context.Background(), V3PlanOptions{Path: dir, Base: expected.BaseSelector, Online: true, Remote: "origin", IntegrationProofs: []string{"ancestor"}, ProtectWorktrees: []string{}})
	if r.Status != "ok" || r.Plan == nil {
		t.Fatalf("workspace-protected plan failed %+v", r)
	}
	for _, candidate := range r.Plan.Candidates {
		if candidate.OID == rows[0].HeadOID || candidate.OID == rows[1].HeadOID {
			if candidate.Decision != "retain" || candidate.Reason != "workspace-used" || len(candidate.WorkspacePaths) != 1 {
				t.Fatalf("workspace use lost %+v", candidate)
			}
		}
	}
	for _, target := range s.targets[0] {
		if target == rows[0].HeadOID || target == rows[1].HeadOID {
			t.Fatal("provider discovery attempted for a workspace-protected head")
		}
	}
	// A registration whose directory went missing (prunable, unlocked, real parent) no longer blocks
	// the plan (issue 284): its native registration keeps protecting its head and ref.
	if err := os.RemoveAll(detached); err != nil {
		t.Fatal(err)
	}
	calls := 0
	c.openHosting = func(context.Context, string, hosting.TidySessionOptions) (tidyV3Hosting, error) {
		calls++
		return s, nil
	}
	r = c.plan(context.Background(), V3PlanOptions{Path: dir, Base: expected.BaseSelector, Online: true, Remote: "origin", IntegrationProofs: []string{"ancestor"}, ProtectWorktrees: []string{}})
	if r.Plan == nil || r.Status != "ok" || calls != 1 {
		t.Fatalf("stale registration blocked the plan %+v calls=%d", r, calls)
	}
	protectedHeads := 0
	for _, candidate := range r.Plan.Candidates {
		if candidate.OID == rows[0].HeadOID || candidate.OID == rows[1].HeadOID {
			protectedHeads++
			if candidate.Decision != "retain" || candidate.Reason != "workspace-used" || len(candidate.WorkspacePaths) != 1 {
				t.Fatalf("stale registration lost its protection %+v", candidate)
			}
		}
	}
	if protectedHeads == 0 {
		t.Fatal("vacuous: no workspace-protected candidate")
	}
	for _, target := range s.targets[len(s.targets)-1] {
		if target == rows[0].HeadOID || target == rows[1].HeadOID {
			t.Fatal("provider discovery attempted for a workspace-protected head")
		}
	}
	// A registered root that is replaced by an alias is an unavailable root: no auth, no plan.
	if err := os.Symlink(dir, detached); err != nil {
		t.Fatal(err)
	}
	calls = 0
	r = c.plan(context.Background(), V3PlanOptions{Path: dir, Base: expected.BaseSelector, Online: true, Remote: "origin", IntegrationProofs: []string{"ancestor"}, ProtectWorktrees: []string{}})
	if r.Plan != nil || r.Status == "ok" || calls != 0 {
		t.Fatalf("aliased workspace observation reached auth/plan %+v calls=%d", r, calls)
	}
}

func TestTidyV3OfflineAndUnsafePreflightHaveNoHostingCalls(t *testing.T) {
	for _, kind := range []string{"offline", "cancelled", "rewrite", "duplicate-url", "wrong-override", "helper", "state-overlap"} {
		t.Run(kind, func(t *testing.T) {
			dir, _, expected, repo, rows := fixtureTidyV3Merged(t)
			c, s := fixtureTidyV3Online(t, dir, repo, rows)
			calls := 0
			c.openHosting = func(context.Context, string, hosting.TidySessionOptions) (tidyV3Hosting, error) {
				calls++
				return s, nil
			}
			o := V3PlanOptions{Path: dir, Base: expected.BaseSelector, Online: true, Remote: "origin", IntegrationProofs: []string{"ancestor"}, ProtectWorktrees: []string{}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch kind {
			case "offline":
				o.Online = false
				o.Remote = ""
			case "cancelled":
				cancel()
			case "rewrite":
				gitFixture(t, dir, "config", "url.https://github.com/.insteadOf", "alias:")
			case "duplicate-url":
				gitFixture(t, dir, "config", "--add", "remote.origin.url", "https://github.com/owner/repo.git")
			case "wrong-override":
				o.Repo = "other/repo"
			case "helper":
				gitFixture(t, dir, "config", "hook.fixture.command", "must-never-run")
			case "state-overlap":
				o.StateDir = filepath.Join(dir, "private-state")
			}
			r := c.plan(ctx, o)
			if calls != 0 || r.Boundary.NetworkAttempted {
				t.Fatalf("unqualified plan reached hosting %+v calls=%d", r, calls)
			}
			if kind == "offline" {
				if r.Status != "ok" || r.Plan == nil {
					t.Fatalf("offline plan failed %+v", r)
				}
				for _, candidate := range r.Plan.Candidates {
					if strings.HasPrefix(candidate.Name, "refs/heads/topic-") && candidate.Decision != "retain" {
						t.Fatal("offline rewritten head was deleted")
					}
				}
			} else if r.Status == "ok" || r.Plan != nil {
				t.Fatalf("unsafe preflight yielded plan %+v", r)
			}
		})
	}
}
func TestTidyV3ChangedNativeEvidenceNeverStoresExecutablePlan(t *testing.T) {
	for _, kind := range []string{"base", "reference", "config"} {
		t.Run(kind, func(t *testing.T) {
			dir, _, expected, repo, rows := fixtureTidyV3Merged(t)
			c, s := fixtureTidyV3Online(t, dir, repo, rows)
			s.onDiscover = func() {
				switch kind {
				case "base":
					gitFixture(t, dir, "update-ref", expected.BaseSelector, rows[0].HeadOID)
				case "reference":
					gitFixture(t, dir, "update-ref", "refs/heads/topic-x", rows[1].HeadOID)
				case "config":
					gitFixture(t, dir, "config", "safe.fixture", "changed")
				}
			}
			state := privateDir(t)
			r := c.plan(context.Background(), V3PlanOptions{Path: dir, Base: expected.BaseSelector, StateDir: state, Online: true, Remote: "origin", IntegrationProofs: []string{"ancestor"}, ProtectWorktrees: []string{}})
			if r.Plan != nil || r.Boundary.Complete || r.Status == "ok" {
				t.Fatalf("changed native authority stored %+v", r)
			}
			files, err := os.ReadDir(state)
			if err != nil {
				t.Fatal(err)
			}
			if len(files) != 0 {
				t.Fatal("stale planning persisted a private record")
			}
		})
	}
}
func TestTidyV3MalformedCompleteNativeRecordsRefuse(t *testing.T) {
	oid := strings.Repeat("a", 40)
	for _, raw := range []string{"refs/heads/a\x00" + oid + "\x00\x00commit\x00", "refs/heads/a\x00" + oid + "\x00\x00blob\x00\n", "refs/heads/b\x00" + oid + "\x00\x00commit\x00\nrefs/heads/a\x00" + oid + "\x00\x00commit\x00\n", "refs/heads/a\x00" + strings.Repeat("0", 40) + "\x00\x00commit\x00\n"} {
		if _, err := parseTidyV3References(raw, "sha1"); err == nil {
			t.Fatalf("incomplete reference observation admitted %q", raw)
		}
	}
}

func TestTidyV3StoredCommitProofRevalidatesNativeAndProviderAuthority(t *testing.T) {
	dir, bin, expected, repo, rows := fixtureTidyV3Merged(t)
	row := rows[0]
	proof, err := proveTidyV3Commit(context.Background(), expected, row.HeadOID, []string{dir}, &repo, &row, tidyV3AncestryAt(bin, dir))
	if err != nil {
		t.Fatal(err)
	}
	s := &fixtureTidyV3Hosting{repo: repo, rows: map[string]hosting.TidyHeadEvidence{row.HeadOID: row}}
	if err = revalidateTidyV3Commit(context.Background(), s, expected, proof, func(context.Context, string, string) (bool, error) { return false, nil }); err == nil {
		t.Fatal("changed native result ancestry accepted during stored proof resampling")
	}
	s.errorOnRecheck = hosting.ErrTidyChanged
	if err = revalidateTidyV3Commit(context.Background(), s, expected, proof, func(context.Context, string, string) (bool, error) { return true, nil }); !errors.Is(err, hosting.ErrTidyChanged) {
		t.Fatalf("changed exact provider tuple accepted: %v", err)
	}
	s.errorOnRecheck = nil
	if err = revalidateTidyV3Commit(context.Background(), s, expected, proof, tidyV3AncestryAt(bin, dir)); err != nil {
		t.Fatal(err)
	}
	native, err := proveTidyV3Commit(context.Background(), expected, expected.Identity.CommitID, []string{dir}, nil, nil, tidyV3AncestryAt(bin, dir))
	if err != nil {
		t.Fatal(err)
	}
	if err = revalidateTidyV3Commit(context.Background(), nil, expected, native, func(context.Context, string, string) (bool, error) { return false, nil }); err == nil {
		t.Fatal("changed stored native proof accepted")
	}
}
func TestTidyV3ConflictingPRHintsRefuseBeforeAuthentication(t *testing.T) {
	dir, _, expected, repo, rows := fixtureTidyV3Merged(t)
	c, s := fixtureTidyV3Online(t, dir, repo, rows)
	calls := 0
	c.openHosting = func(context.Context, string, hosting.TidySessionOptions) (tidyV3Hosting, error) {
		calls++
		return s, nil
	}
	r := c.plan(context.Background(), V3PlanOptions{Path: dir, Base: expected.BaseSelector, Online: true, Remote: "origin", IntegrationProofs: []string{"ancestor"}, ProtectWorktrees: []string{}, PRHints: []V3PRHint{{OID: rows[0].HeadOID, Number: 1}, {OID: rows[1].HeadOID, Number: 1}}})
	if r.Status == "ok" || r.Plan != nil || calls != 0 {
		t.Fatalf("conflicting hint mappings admitted before auth refusal: result=%+v calls=%d", r, calls)
	}
}

func TestTidyV3FinalWorkspaceRootQualificationBeforePersistence(t *testing.T) {
	dir, _ := fixtureTidyV3Git(t)
	root := gitFixture(t, dir, "rev-parse", "HEAD")
	linked := filepath.Join(gitTempDir(t), "linked")
	gitFixture(t, dir, "worktree", "add", "--detach", linked, root)
	c := defaultTidyV3RefsCoordinator()
	observe := c.observe
	observations := 0
	c.observe = func(ctx context.Context, path, base string) (tidyV3NativeObservation, error) {
		o, err := observe(ctx, path, base)
		observations++
		if err == nil && observations == 2 {
			if err = os.Rename(linked, linked+"-saved"); err != nil {
				t.Fatal(err)
			}
			if err = os.Symlink(dir, linked); err != nil {
				t.Fatal(err)
			}
		}
		return o, err
	}
	r := c.plan(context.Background(), V3PlanOptions{Path: dir, Base: "HEAD", StateDir: privateDir(t), ProtectWorktrees: []string{}})
	if r.Status == "ok" || r.Plan != nil {
		t.Fatal("replaced registered workspace root became stored authority")
	}
	if observations != 2 {
		t.Fatalf("late root replacement was not refused before opening the state store: observed=%d", observations)
	}
	if data, err := os.ReadFile(filepath.Join(linked+"-saved", "file")); err != nil || string(data) != "stored\n" {
		t.Fatal("actor-preserved workspace data changed", err)
	}
}

func TestTidyV3UnsupportedOwnershipAndChangedProviderHavePreciseRefusals(t *testing.T) {
	dir, _, expected, repo, rows := fixtureTidyV3Merged(t)
	c, s := fixtureTidyV3Online(t, dir, repo, rows)
	o := V3PlanOptions{Path: dir, Base: expected.BaseSelector, Online: true, Remote: "origin", IntegrationProofs: []string{"ancestor"}, ProtectWorktrees: []string{}}
	originalInspect := c.inspect
	c.inspect = func(context.Context, string) inspection.Result { return inspection.Result{Status: "ok", Backend: "jj"} }
	r := c.plan(context.Background(), o)
	if r.Status != "refused" || r.Plan != nil || r.Backend != "jj" || r.Diagnostics[0].Code != "unsupported-backend" {
		t.Fatalf("unsupported ownership was not explicit refusal: %+v", r)
	}
	c.inspect = originalInspect
	// Provider re-reads belong to apply (Spec 031 FR-018): a plan makes none, so
	// provider drift is refused at apply, never by spending plan budget.
	s.errorOnRecheck = hosting.ErrTidyChanged
	r = c.plan(context.Background(), o)
	if r.Status != "ok" || r.Plan == nil || len(s.rechecks) != 0 {
		t.Fatalf("plan re-read the provider: %+v rechecks=%d", r, len(s.rechecks))
	}
}

// This is an integrated dependency regression: C counts its one detection byte
// on failure. The error result must retain that actual observation, not fall
// through B's bounded-result fallback and claim zero measured HTTP bytes.
func TestTidyV3FailedHTTPBoundPreservesActualDetectionCounter(t *testing.T) {
	dir, _, expected, repo, rows := fixtureTidyV3Merged(t)
	c, s := fixtureTidyV3Online(t, dir, repo, rows)
	s.budget = hosting.TidyHTTPBudget{Requests: 128, RawBytes: (32 << 20) + 1, NetworkAttempted: true}
	c.openHosting = func(context.Context, string, hosting.TidySessionOptions) (tidyV3Hosting, error) {
		return s, hosting.ErrTidyLimit
	}
	r := c.plan(context.Background(), V3PlanOptions{Path: dir, Base: expected.BaseSelector, Online: true, Remote: "origin", IntegrationProofs: []string{"ancestor"}, ProtectWorktrees: []string{}})
	if r.Status != "error" || r.Plan != nil || r.Boundary.Complete || r.Boundary.Budget.HTTPBytes != (32<<20)+1 || r.Boundary.Budget.HTTPRequests != 128 || !r.Boundary.NetworkAttempted || validateV3Result(r) != nil {
		t.Fatalf("failed HTTP bound lost actual counters or became executable: status=%s boundary=%+v diagnostics=%+v", r.Status, r.Boundary, r.Diagnostics)
	}
}
