// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureTidyV3Tracking(t *testing.T) tidyV3NativeObservation {
	t.Helper()
	dir, _ := fixtureTidyV3Git(t)
	gitFixture(t, dir, "remote", "add", "origin", "https://github.com/owner/repo.git")
	gitFixture(t, dir, "update-ref", "refs/remotes/origin/stale", "HEAD")
	gitFixture(t, dir, "commit", "--allow-empty", "-qm", "advance source")
	o, err := observeTidyV3Git(context.Background(), dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	return o
}
func tidyV3TrackingConfig(o tidyV3NativeObservation, key, value string, appendRow bool) tidyV3NativeObservation {
	fields := strings.Split(strings.TrimSuffix(o.Config, "\x00"), "\x00")
	if appendRow {
		o.Config += fields[0] + "\x00" + key + "\n" + value + "\x00"
		return o
	}
	for i := 1; i < len(fields); i += 2 {
		old, _, _ := strings.Cut(fields[i], "\n")
		if old == key {
			fields[i] = key + "\n" + value
		}
	}
	o.Config = strings.Join(fields, "\x00") + "\x00"
	return o
}
func TestTidyV3TrackingStandardMappingAndLiteralBinding(t *testing.T) {
	original := fixtureTidyV3Tracking(t)
	var previous string
	for _, refspec := range []string{"+refs/heads/*:refs/remotes/origin/*", "refs/heads/*:refs/remotes/origin/*"} {
		o := tidyV3TrackingConfig(original, "remote.origin.fetch", refspec, false)
		mapping, err := qualifyTidyV3TrackingMapping(o, "origin", "owner/repo")
		if err != nil {
			t.Fatalf("standard mapping %q refused: %v", refspec, err)
		}
		if mapping.Remote != "origin" || mapping.FetchURL != "https://github.com/owner/repo.git" || mapping.Refspec != "+refs/heads/*:refs/remotes/origin/*" || mapping.ConfigDigest != gitDigest(o.Config) {
			t.Fatalf("mapping did not bind exact literal config and normalized refspec: %+v", mapping)
		}
		if previous == mapping.ConfigDigest {
			t.Fatal("literal optional-plus configuration change did not change digest")
		}
		previous = mapping.ConfigDigest
	}
	// Same semantic configuration from a different literal origin remains distinct.
	alt := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(alt, []byte("# distinct origin\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fields := strings.Split(strings.TrimSuffix(original.Config, "\x00"), "\x00")
	for i := 0; i < len(fields); i += 2 {
		fields[i] = "file:" + alt
	}
	changed := original
	changed.Config = strings.Join(fields, "\x00") + "\x00"
	a, err := qualifyTidyV3TrackingMapping(original, "origin", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := qualifyTidyV3TrackingMapping(changed, "origin", "")
	if err != nil {
		t.Fatal(err)
	}
	if a.ConfigDigest == b.ConfigDigest {
		t.Fatal("origin change was not bound")
	}
}
func TestTidyV3TrackingUnsupportedMappings(t *testing.T) {
	baseline := fixtureTidyV3Tracking(t)
	tests := []struct {
		name, key, value string
		add              bool
	}{
		{"negative", "remote.origin.fetch", "^refs/heads/private", false},
		{"custom-source", "remote.origin.fetch", "+refs/tags/*:refs/remotes/origin/*", false},
		{"custom-destination", "remote.origin.fetch", "+refs/heads/*:refs/custom/origin/*", false},
		{"fixed-source", "remote.origin.fetch", "+refs/heads/main:refs/remotes/origin/main", false},
		{"other-remote-namespace", "remote.origin.fetch", "+refs/heads/*:refs/remotes/upstream/*", false},
		{"duplicate", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*", true},
		{"extra-negative", "remote.origin.fetch", "^refs/heads/private", true},
		{"mirror", "remote.origin.mirror", "true", true},
		{"custom-overlap", "remote.other.fetch", "+refs/heads/topic:refs/remotes/origin/topic", true},
		{"wildcard-overlap", "remote.other.fetch", "+refs/*:refs/*", true},
		{"duplicate-fetch-url", "remote.origin.url", "https://github.com/owner/repo.git", true},
		{"rewrite", "url.https://github.com/.insteadof", "alias:", true},
		{"push-rewrite", "url.https://github.com/.pushinsteadof", "alias:", true},
		{"wrong-fetch-url", "remote.origin.url", "https://github.com/other/repo.git", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := tidyV3TrackingConfig(baseline, tc.key, tc.value, tc.add)
			if _, err := qualifyTidyV3TrackingMapping(o, "origin", "owner/repo"); err == nil {
				t.Fatal("unsupported mapping became authority")
			}
		})
	}
}
func TestTidyV3TrackingSelectionAndProtection(t *testing.T) {
	original := fixtureTidyV3Tracking(t)
	var target V3Reference
	for _, ref := range original.Native.References {
		if ref.Name == "refs/remotes/origin/stale" {
			target = ref
		}
	}
	for _, kind := range []string{"stale", "symbolic", "symbolic-chain-target", "selected-base-name", "selected-base-oid", "workspace-name", "workspace-oid", "separate-namespaces", "invalid-symbolic-cycle", "invalid-symbolic-missing"} {
		t.Run(kind, func(t *testing.T) {
			o := original
			o.Native = cloneV3Native(original.Native)
			want := ""
			switch kind {
			case "symbolic":
				for i := range o.Native.References {
					if o.Native.References[i].Name == target.Name {
						o.Native.References[i].Symbolic = o.Expected.Identity.HeadRef
					}
				}
				want = "symbolic-reference"
			case "symbolic-chain-target":
				o.Native.References = append(o.Native.References, V3Reference{Name: "refs/remotes/origin/HEAD", OID: target.OID, Symbolic: "refs/remotes/origin/alias"}, V3Reference{Name: "refs/remotes/origin/alias", OID: target.OID, Symbolic: target.Name})
				want = "symbolic-reference"
			case "selected-base-name":
				o.Expected.BaseSelector = target.Name
				want = "selected-base"
			case "selected-base-oid":
				o.Expected.BaseCommit = target.OID
				want = "selected-base"
			case "workspace-name":
				o.Native.Registrations = append(o.Native.Registrations, V3Registration{Path: filepath.Join(t.TempDir(), "used"), HeadOID: o.Expected.BaseCommit, HeadRef: target.Name})
				want = "workspace-used"
			case "workspace-oid":
				o.Native.Registrations = append(o.Native.Registrations, V3Registration{Path: filepath.Join(t.TempDir(), "used"), HeadOID: target.OID})
				want = "workspace-used"
			case "separate-namespaces":
				o.Native.References = append(o.Native.References, V3Reference{Name: "refs/remotes/elsewhere/stale", OID: target.OID}, V3Reference{Name: "refs/heads/stale", OID: target.OID})
			case "invalid-symbolic-cycle":
				o.Native.References = append(o.Native.References, V3Reference{Name: "refs/remotes/origin/HEAD", OID: target.OID, Symbolic: "refs/remotes/origin/alias"}, V3Reference{Name: "refs/remotes/origin/alias", OID: target.OID, Symbolic: "refs/remotes/origin/HEAD"})
			case "invalid-symbolic-missing":
				o.Native.References = append(o.Native.References, V3Reference{Name: "refs/remotes/origin/HEAD", OID: target.OID, Symbolic: "refs/remotes/origin/unobserved"})
			}
			_, selected, err := selectTidyV3Tracking(o, "origin", "", []string{target.Name})
			if strings.HasPrefix(kind, "invalid-") {
				if err == nil {
					t.Fatal("unresolved symbolic inventory admitted")
				}
				return
			}
			if err != nil || len(selected) != 1 {
				t.Fatalf("complete selection failed: selected=%+v err=%v", selected, err)
			}
			if selected[0].Reference.Name != target.Name || selected[0].SourceRef != "refs/heads/stale" || selected[0].ProtectedReason != want {
				t.Fatalf("wrong exact mapping/protection: %+v want=%q", selected[0], want)
			}
			if want == "workspace-used" && len(selected[0].WorkspacePaths) != 1 {
				t.Fatal("workspace evidence dropped")
			}
		})
	}
}

func TestTidyV3TrackingCanonicalFetchAndDisjointMapping(t *testing.T) {
	baseline := fixtureTidyV3Tracking(t)
	for _, url := range []string{"https://github.com/owner/repo.git", "https://github.com/owner/repo", "git@github.com:owner/repo.git"} {
		o := tidyV3TrackingConfig(baseline, "remote.origin.url", url, false)
		o = tidyV3TrackingConfig(o, "remote.other.fetch", "+refs/heads/*:refs/remotes/other/*", true)
		o = tidyV3TrackingConfig(o, "remote.origin.mirror", "false", true)
		got, err := qualifyTidyV3TrackingMapping(o, "origin", "")
		if err != nil || got.FetchURL != url {
			t.Fatalf("canonical fetch/disjoint mapping refused: %+v %v", got, err)
		}
	}
}
func TestTidyV3TrackingIncompleteOriginsAndLiteralRemote(t *testing.T) {
	baseline := fixtureTidyV3Tracking(t)
	for _, kind := range []string{"missing-refspec", "missing-url", "wrong-subsection-case", "malformed-origin-pair", "non-file-origin", "unresolved-origin"} {
		t.Run(kind, func(t *testing.T) {
			fields := strings.Split(strings.TrimSuffix(baseline.Config, "\x00"), "\x00")
			out := []string{}
			for i := 0; i < len(fields); i += 2 {
				origin, row := fields[i], fields[i+1]
				key, _, _ := strings.Cut(row, "\n")
				if kind == "missing-refspec" && key == "remote.origin.fetch" || kind == "missing-url" && key == "remote.origin.url" {
					continue
				}
				if kind == "wrong-subsection-case" {
					row = strings.ReplaceAll(row, "remote.origin.url", "remote.Origin.url")
				}
				if kind == "non-file-origin" {
					origin = "command line:"
				}
				if kind == "unresolved-origin" {
					origin = "file:" + filepath.Join(t.TempDir(), "missing-config")
				}
				out = append(out, origin, row)
			}
			o := baseline
			o.Config = strings.Join(out, "\x00") + "\x00"
			if kind == "malformed-origin-pair" {
				o.Config += "file:unpaired\x00"
			}
			if _, err := qualifyTidyV3TrackingMapping(o, "origin", ""); err == nil {
				t.Fatal("incomplete or foreign origin/remote admitted")
			}
		})
	}
}
func TestTidyV3TrackingCompleteNamespaceAndExactSelection(t *testing.T) {
	o := fixtureTidyV3Tracking(t)
	oid := o.Native.References[0].OID
	o.Native.References = append(o.Native.References, V3Reference{Name: "refs/remotes/origin/nested/topic", OID: oid}, V3Reference{Name: "refs/remotes/originate/stale", OID: oid}, V3Reference{Name: "refs/remotes/other/stale", OID: oid}, V3Reference{Name: "refs/heads/stale", OID: oid})
	_, refs, err := selectTidyV3Tracking(o, "origin", "", []string{})
	if err != nil || len(refs) != 2 {
		t.Fatalf("namespace selection failed: %+v %v", refs, err)
	}
	for _, ref := range refs {
		if !strings.HasPrefix(ref.Reference.Name, "refs/remotes/origin/") || ref.SourceRef != "refs/heads/"+strings.TrimPrefix(ref.Reference.Name, "refs/remotes/origin/") {
			t.Fatal("literal namespace/source mapping changed", ref)
		}
	}
	for _, selected := range [][]string{{"refs/remotes/origin/stale", "refs/remotes/origin/stale"}, {"refs/remotes/originate/stale"}, {"refs/heads/stale"}, {"refs/remotes/origin/missing"}} {
		if _, _, err := selectTidyV3Tracking(o, "origin", "", selected); err == nil {
			t.Fatal("inexact or unobserved selection admitted", selected)
		}
	}
}
func TestTidyV3TrackingLiteralConfigByteChangeStalesNativeProof(t *testing.T) {
	before := fixtureTidyV3Tracking(t)
	path := filepath.Join(before.Expected.Identity.RepositoryDir, "config")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, append(data, []byte("\n# changed literal config bytes\n")...), 0644); err != nil {
		t.Fatal(err)
	}
	after, err := observeTidyV3Git(context.Background(), before.Expected.Identity.WorkspaceRoot, before.Expected.BaseSelector)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = qualifyTidyV3TrackingMapping(after, "origin", ""); err != nil {
		t.Fatal("same semantic mapping no longer qualified", err)
	}
	if after.Expected.SafetyDigest == before.Expected.SafetyDigest {
		t.Fatal("literal-byte config change escaped preserved full native proof")
	}
}

func fixtureTidyV3Advertisement(repo hosting.TidyRepositoryIdentity, branches []hosting.TidyBranch) hosting.TidyBranchAdvertisement {
	rows := append([]hosting.TidyBranch{}, branches...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	data, _ := json.Marshal(struct {
		Repository hosting.TidyRepositoryIdentity `json:"repository"`
		Branches   []hosting.TidyBranch           `json:"branches"`
	}{repo, rows})
	return hosting.TidyBranchAdvertisement{Repository: repo, Branches: rows, Fingerprint: gitDigest(string(data))}
}

type fixtureTidyV3TrackingHosting struct {
	*fixtureTidyV3Hosting
	advertisements []hosting.TidyBranchAdvertisement
	reads          int
	errorAt        map[int]error
	onRead         func(int)
	deadline       time.Time
}

func (s *fixtureTidyV3TrackingHosting) ReadBranches(ctx context.Context) (hosting.TidyBranchAdvertisement, error) {
	if err := ctx.Err(); err != nil {
		return hosting.TidyBranchAdvertisement{}, err
	}
	deadline, ok := ctx.Deadline()
	if !ok || !deadline.Equal(s.deadline) {
		return hosting.TidyBranchAdvertisement{}, failure("fixture-deadline-reset", "Tracking sample reset the operation deadline.")
	}
	s.reads++
	s.budget.Requests++
	s.budget.RawBytes++
	s.budget.NetworkAttempted = true
	if s.onRead != nil {
		s.onRead(s.reads)
	}
	if err := s.errorAt[s.reads]; err != nil {
		return hosting.TidyBranchAdvertisement{}, err
	}
	index := s.reads - 1
	if index >= len(s.advertisements) {
		index = len(s.advertisements) - 1
	}
	if index < 0 {
		return hosting.TidyBranchAdvertisement{}, failure("fixture-no-advertisement", "Missing typed advertisement.")
	}
	ad := s.advertisements[index]
	if ad.Branches != nil {
		ad.Branches = append([]hosting.TidyBranch{}, ad.Branches...)
	}
	return ad, ctx.Err()
}
func (s *fixtureTidyV3TrackingHosting) RevalidateBranches(ctx context.Context, expected hosting.TidyBranchAdvertisement) error {
	current, err := s.ReadBranches(ctx)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current, expected) {
		return hosting.ErrTidyChanged
	}
	return nil
}
func fixtureTidyV3TrackingPlanner(t *testing.T) (tidyV3NativeObservation, tidyV3TrackingCoordinator, *fixtureTidyV3TrackingHosting, *int) {
	t.Helper()
	o := fixtureTidyV3Tracking(t)
	repo := hosting.TidyRepositoryIdentity{Host: "github.com", TidyRepositoryID: hosting.TidyRepositoryID{ID: 1, NodeID: "R_fixture", FullName: "owner/repo"}, DefaultBranch: "main"}
	ad := fixtureTidyV3Advertisement(repo, []hosting.TidyBranch{{Name: "live", OID: o.Expected.BaseCommit}, {Name: "unrelated", OID: o.Expected.BaseCommit}})
	s := &fixtureTidyV3TrackingHosting{fixtureTidyV3Hosting: &fixtureTidyV3Hosting{repo: repo, rows: map[string]hosting.TidyHeadEvidence{}}, advertisements: []hosting.TidyBranchAdvertisement{ad}, errorAt: map[int]error{}}
	c := defaultTidyV3TrackingCoordinator()
	opens := new(int)
	c.openHosting = func(ctx context.Context, path string, options hosting.TidySessionOptions) (tidyV3TrackingHosting, error) {
		*opens++
		if !options.Online || options.Remote != "origin" || path != o.Expected.Identity.WorkspaceRoot {
			t.Fatal("wrong explicit selected session", options, path)
		}
		s.deadline, _ = ctx.Deadline()
		return s, nil
	}
	return o, c, s, opens
}
func TestTidyV3TrackingPlanCompleteAbsenceAndLiveChangedOID(t *testing.T) {
	o, c, s, opens := fixtureTidyV3TrackingPlanner(t)
	dir := o.Expected.Identity.WorkspaceRoot
	stale := gitFixture(t, dir, "rev-parse", "refs/remotes/origin/stale")
	gitFixture(t, dir, "update-ref", "refs/remotes/origin/live", stale)
	gitFixture(t, dir, "update-ref", "refs/remotes/elsewhere/stale", stale)
	before := gitFixture(t, dir, "for-each-ref", "--format=%(refname) %(objectname) %(symref)")
	r := c.plan(context.Background(), V3PlanOptions{Path: dir, Base: "HEAD", Online: true, TrackingRemote: "origin", StateDir: privateDir(t), ProtectWorktrees: []string{}})
	if r.Status != "ok" || r.Plan == nil || !r.Boundary.Complete || validateV3Result(r) != nil {
		t.Fatalf("complete tracking plan failed: status=%s diagnostics=%+v", r.Status, r.Diagnostics)
	}
	if *opens != 1 || s.reads < 2 || !s.closed || r.Boundary.Budget.HTTPRequests != int64(s.budget.Requests) || !r.Boundary.NetworkAttempted {
		t.Fatalf("session/deadline/budget was not shared: opens=%d reads=%d result=%+v", *opens, s.reads, r.Boundary)
	}
	p := *r.Plan
	if p.Mode != "tracking-refs" || p.Online == nil || len(p.Candidates) != 2 || len(v3Eligible(p)) != 1 || len(p.Online.ObservationDigests) != 1 {
		t.Fatalf("wrong tracking inventory/selection: %+v", p)
	}
	for _, target := range p.Candidates {
		switch target.Name {
		case "refs/remotes/origin/stale":
			if target.Decision != "delete" || target.Proof != "github-branch-absent" || target.Evidence == nil || target.Evidence.Absence == nil || target.Evidence.Absence.SourceRef != "refs/heads/stale" || target.Evidence.Absence.Refspec != "+refs/heads/*:refs/remotes/origin/*" {
				t.Fatalf("incomplete exact absence proof: %+v", target)
			}
		case "refs/remotes/origin/live":
			if target.Decision != "retain" || target.Reason != "remote-source-present" || target.OID == o.Expected.BaseCommit {
				t.Fatalf("live source at changed OID was not retained: %+v", target)
			}
		default:
			t.Fatal("separate namespace entered tracking plan", target.Name)
		}
	}
	if after := gitFixture(t, dir, "for-each-ref", "--format=%(refname) %(objectname) %(symref)"); after != before {
		t.Fatal("tracking planning synchronized native references")
	}
	store, err := openV3Store(p.StateDir, false, v3NativeRoots(o.Native), o.Native.ProtectedRoots)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	stored, _, err := store.readPlan(p.PlanID)
	if err != nil || !reflect.DeepEqual(stored, p) {
		t.Fatal("tracking plan not bound to immutable store", err)
	}
}

func TestTidyV3TrackingPlanIncompleteEvidenceNeverAuthorizes(t *testing.T) {
	for _, kind := range []string{"first-read-error", "second-read-error", "changed-unrelated-oid", "changed-repository", "nil-inventory", "wrong-fingerprint", "wrong-ad-repository", "canceled-read", "config-bytes-changed", "native-tip-changed", "http-detection-byte"} {
		t.Run(kind, func(t *testing.T) {
			observed, c, session, opens := fixtureTidyV3TrackingPlanner(t)
			dir := observed.Expected.Identity.WorkspaceRoot
			target := "refs/remotes/origin/stale"
			old := gitFixture(t, dir, "rev-parse", target)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch kind {
			case "first-read-error":
				session.errorAt[1] = errors.New("provider permission failure")
			case "second-read-error":
				session.errorAt[2] = errors.New("provider final page unavailable")
			case "changed-unrelated-oid":
				ad := session.advertisements[0]
				ad = fixtureTidyV3Advertisement(ad.Repository, []hosting.TidyBranch{{Name: "live", OID: observed.Expected.BaseCommit}, {Name: "unrelated", OID: old}})
				session.advertisements = append(session.advertisements, ad)
			case "changed-repository":
				ad := session.advertisements[0]
				repo := ad.Repository
				repo.ID++
				session.advertisements = append(session.advertisements, fixtureTidyV3Advertisement(repo, ad.Branches))
			case "nil-inventory":
				session.advertisements[0].Branches = nil
			case "wrong-fingerprint":
				session.advertisements[0].Fingerprint = strings.Repeat("a", 64)
			case "wrong-ad-repository":
				repo := session.repo
				repo.ID++
				session.advertisements[0] = fixtureTidyV3Advertisement(repo, session.advertisements[0].Branches)
			case "canceled-read":
				session.onRead = func(n int) {
					if n == 1 {
						cancel()
					}
				}
			case "config-bytes-changed":
				session.onRead = func(n int) {
					if n == 2 {
						path := filepath.Join(observed.Expected.Identity.RepositoryDir, "config")
						data, e := os.ReadFile(path)
						if e != nil {
							t.Fatal(e)
						}
						if e = os.WriteFile(path, append(data, []byte("\n# concurrent literal config change\n")...), 0644); e != nil {
							t.Fatal(e)
						}
					}
				}
			case "native-tip-changed":
				session.onRead = func(n int) {
					if n == 2 {
						gitFixture(t, dir, "update-ref", target, observed.Expected.BaseCommit)
					}
				}
			case "http-detection-byte":
				session.onRead = func(n int) {
					if n == 2 {
						session.budget.RawBytes = (32 << 20) + 1
					}
				}
				session.errorAt[2] = hosting.ErrTidyLimit
			}
			state := privateDir(t)
			r := c.plan(ctx, V3PlanOptions{Path: dir, Base: "HEAD", Online: true, TrackingRemote: "origin", StateDir: state, ProtectWorktrees: []string{}})
			if r.Status == "ok" || r.Plan != nil || r.Boundary.Complete || validateV3Result(r) != nil {
				t.Fatalf("incomplete evidence became executable: %+v", r)
			}
			if *opens != 1 || !session.closed || r.Boundary.Budget.HTTPRequests != int64(session.budget.Requests) || r.Boundary.Budget.HTTPBytes != session.budget.RawBytes {
				t.Fatalf("session budget/evidence was reset or dropped: %+v", r.Boundary)
			}
			if kind != "native-tip-changed" {
				requireTidyV3Ref(t, dir, target, old)
			}
			if kind == "http-detection-byte" {
				if r.Boundary.Budget.HTTPBytes != (32<<20)+1 {
					t.Fatal("detection byte lost")
				}
				found := false
				for _, d := range r.Diagnostics {
					found = found || d.Code == "hosting-evidence-limit"
				}
				if !found {
					t.Fatal("typed budget diagnostic lost", r.Diagnostics)
				}
			}
			entries, err := os.ReadDir(filepath.Join(state, "tidy-v3-plans"))
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if len(entries) > 0 {
				t.Fatal("incomplete evidence persisted executable plan")
			}
			if roots := gitFixture(t, dir, "for-each-ref", "--format=%(refname)", "refs/stw/recovery/"); roots != "" {
				t.Fatal("planning created recovery", roots)
			}
		})
	}
}
func TestTidyV3TrackingPlanPreflightPrecedesCredentials(t *testing.T) {
	for _, kind := range []string{"offline", "missing-selector", "wrong-namespace", "custom-mapping", "state-overlap", "already-canceled"} {
		t.Run(kind, func(t *testing.T) {
			observed, c, session, opens := fixtureTidyV3TrackingPlanner(t)
			dir := observed.Expected.Identity.WorkspaceRoot
			o := V3PlanOptions{Path: dir, Base: "HEAD", Online: true, TrackingRemote: "origin", ProtectWorktrees: []string{}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch kind {
			case "offline":
				o.Online = false
			case "missing-selector":
				o.TrackingRemote = ""
			case "wrong-namespace":
				o.Refs = []string{"refs/remotes/other/stale"}
			case "custom-mapping":
				gitFixture(t, dir, "config", "remote.origin.fetch", "+refs/tags/*:refs/remotes/origin/*")
			case "state-overlap":
				o.StateDir = filepath.Join(dir, "private-state")
			case "already-canceled":
				cancel()
			}
			before := gitFixture(t, dir, "for-each-ref", "--format=%(refname) %(objectname) %(symref)")
			r := c.plan(ctx, o)
			if r.Status == "ok" || r.Plan != nil || r.Boundary.Complete || *opens != 0 || session.reads != 0 || r.Boundary.NetworkAttempted {
				t.Fatalf("unsafe preflight reached credentials/authority: opens=%d reads=%d %+v", *opens, session.reads, r)
			}
			if after := gitFixture(t, dir, "for-each-ref", "--format=%(refname) %(objectname) %(symref)"); after != before {
				t.Fatal("refused preflight mutated native refs")
			}
		})
	}
}
func TestTidyV3TrackingPlanCannotOverrideNativeProtections(t *testing.T) {
	for _, kind := range []string{"symbolic-head-chain", "chosen-base", "workspace-oid"} {
		t.Run(kind, func(t *testing.T) {
			observed, c, session, _ := fixtureTidyV3TrackingPlanner(t)
			dir := observed.Expected.Identity.WorkspaceRoot
			target := "refs/remotes/origin/stale"
			o := V3PlanOptions{Path: dir, Base: "HEAD", Online: true, TrackingRemote: "origin", ProtectWorktrees: []string{}}
			reason := ""
			switch kind {
			case "symbolic-head-chain":
				gitFixture(t, dir, "symbolic-ref", "refs/remotes/origin/HEAD", target)
				reason = "symbolic-reference"
			case "chosen-base":
				o.Base = target
				reason = "selected-base"
			case "workspace-oid":
				gitFixture(t, dir, "worktree", "add", "--quiet", "--detach", filepath.Join(t.TempDir(), "active"), target)
				reason = "workspace-used"
			}
			// Both complete advertisements omit the mapped source; protection still wins.
			session.advertisements = []hosting.TidyBranchAdvertisement{fixtureTidyV3Advertisement(session.repo, []hosting.TidyBranch{})}
			r := c.plan(context.Background(), o)
			if r.Status != "ok" || r.Plan == nil || len(v3Eligible(*r.Plan)) != 0 {
				t.Fatalf("provider absence overrode native protection: %+v", r)
			}
			found := false
			for _, candidate := range r.Plan.Candidates {
				if candidate.Name == target {
					found = true
					if candidate.Decision != "retain" || candidate.Reason != reason || candidate.Proof != "none" || candidate.Evidence != nil {
						t.Fatalf("wrong protection: %+v", candidate)
					}
				}
			}
			if !found {
				t.Fatal("protected target disappeared")
			}
		})
	}
}

func TestTidyV3TrackingPlanStatelessTwoEqualSamples(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "equal", true: "changed"}[changed], func(t *testing.T) {
			observed, c, session, opens := fixtureTidyV3TrackingPlanner(t)
			if changed {
				old := gitFixture(t, observed.Expected.Identity.WorkspaceRoot, "rev-parse", "refs/remotes/origin/stale")
				session.advertisements = append(session.advertisements, fixtureTidyV3Advertisement(session.repo, []hosting.TidyBranch{{Name: "live", OID: old}}))
			}
			r := c.plan(context.Background(), V3PlanOptions{Path: observed.Expected.Identity.WorkspaceRoot, Base: "HEAD", Online: true, TrackingRemote: "origin", ProtectWorktrees: []string{}})
			if *opens != 1 || session.reads != 2 || !session.closed {
				t.Fatalf("stateless planning did not take exactly two shared samples: opens=%d reads=%d", *opens, session.reads)
			}
			if changed {
				if r.Status == "ok" || r.Plan != nil || r.Boundary.Complete {
					t.Fatal("single/changed advertisement authorized a plan", r)
				}
			} else if r.Status != "ok" || r.Plan == nil || r.Plan.PlanID != "" || r.Plan.StateDir != "" || validateV3Result(r) != nil {
				t.Fatal("equal complete stateless observation failed", r)
			}
		})
	}
}
func TestTidyV3TrackingPlanStatelessFinalNativeSample(t *testing.T) {
	observed, c, session, _ := fixtureTidyV3TrackingPlanner(t)
	dir := observed.Expected.Identity.WorkspaceRoot
	session.onRead = func(n int) {
		if n == 2 {
			gitFixture(t, dir, "config", "tracking.actor", "changed")
		}
	}
	r := c.plan(context.Background(), V3PlanOptions{Path: dir, Base: "HEAD", Online: true, TrackingRemote: "origin", ProtectWorktrees: []string{}})
	if r.Status == "ok" || r.Plan != nil || r.Boundary.Complete {
		t.Fatal("native evidence change after provider sample became a plan", r)
	}
}
