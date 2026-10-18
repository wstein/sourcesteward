// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"github.com/wstein/sourcesteward/internal/hosting"
	"strings"
	"testing"
)

func TestOnlineQualificationAndOfflineZeroReads(t *testing.T) {
	d := repoWith(t, map[string][]byte{".stw/release.toml": exampleBytes(t, "go-cli")})
	sha := commitAll(t, d)
	git(t, d, "remote", "add", "origin", "https://github.com/example/test.git")
	git(t, d, "update-ref", "refs/remotes/origin/main", sha)
	calls := 0
	success := "success"
	reader := func(_ context.Context, root string, repo hosting.Repository, commit string, workflows []string) (hosting.ReleaseEvidence, error) {
		calls++
		if root != d || repo.Owner != "example" || repo.Name != "test" || commit != sha || len(workflows) != 1 || workflows[0] != "ci.yml" {
			t.Fatalf("wrong selection %s %+v %s %v", root, repo, commit, workflows)
		}
		return hosting.ReleaseEvidence{Runs: []hosting.ReleaseRun{{Workflow: "ci.yml", HeadSHA: sha, Event: "push", Branch: "main", Status: "completed", Conclusion: &success}}, Tags: []string{"v0.1.0"}, Network: []hosting.ReleaseRequest{{Method: "GET", Endpoint: "repos/example/test/tags?per_page=100&page=1"}}}, nil
	}
	o := PlanOptions{Path: d, Subject: sha, Version: "v0.2.0"}
	offline := planWithReader(context.Background(), o, reader)
	if calls != 0 || len(offline.Network) != 0 || offline.Boundary.Effects.Network {
		t.Fatal("offline invoked reader")
	}
	o.Online = true
	r := planWithReader(context.Background(), o, reader)
	if calls != 1 || r.Status != "ok" || r.Verdict != "pass" || !r.Boundary.Effects.Network || len(r.Network) != 1 || strings.Contains(r.Text(), "nothing built; network: none") {
		t.Fatalf("calls %d %+v", calls, r)
	}
	o.Path = t.TempDir()
	r = planWithReader(context.Background(), o, reader)
	if calls != 1 || r.Status == "ok" {
		t.Fatal("unowned path acquired online evidence")
	}
}
func TestOnlineExistingTagBuildAndDraftCoverage(t *testing.T) {
	c := &Contract{Version: Version{Scheme: "semver"}}
	f := false
	yes := true
	e := hosting.ReleaseEvidence{Tags: []string{"v1.0.0"}}
	gates, err := onlineGates(PlanOptions{Mode: "build", Subject: "v1.0.0", Version: "v1.0.0"}, c, nil, e, false)
	if err != nil || Verdict(gates) != "pass" {
		t.Fatalf("%+v %v", gates, err)
	}
	record := &ReleaseRecord{Version: "v1.0.0", Tag: "v1.0.0", Checksums: FileRef{Name: "SHA256SUMS"}, Manifest: FileRef{Name: "build-manifest.json"}}
	for _, draft := range []bool{false, true} {
		e.Releases = []hosting.ReleaseEntry{{Tag: record.Tag, Draft: &f}}
		if draft {
			e.Releases = []hosting.ReleaseEntry{{Tag: record.Tag, Draft: &yes, Assets: []hosting.ReleaseAsset{{Name: "SHA256SUMS"}}}}
		}
		g, _ := onlineGates(PlanOptions{Mode: "publish", Subject: record.Tag, Version: record.Version}, c, record, e, false)
		for _, gate := range g {
			if gate.Code == "draft-missing" && gate.Outcome != "fail" {
				t.Fatal(g)
			}
		}
	}
}

func TestOnlineExistingTagBuildChecksResolvedCommitCI(t *testing.T) {
	d := repoWith(t, map[string][]byte{".stw/release.toml": exampleBytes(t, "go-cli")})
	sha := commitAll(t, d)
	git(t, d, "remote", "add", "origin", "https://github.com/example/test.git")
	git(t, d, "update-ref", "refs/remotes/origin/main", sha)
	git(t, d, "tag", "-a", "v0.2.0", "-m", "test release")
	success := "success"
	reader := func(_ context.Context, _ string, _ hosting.Repository, commit string, _ []string) (hosting.ReleaseEvidence, error) {
		if commit != sha {
			t.Fatal("reader not bound to tag commit")
		}
		return hosting.ReleaseEvidence{Tags: []string{"v0.2.0"}, Runs: []hosting.ReleaseRun{{Workflow: "ci.yml", HeadSHA: sha, Event: "push", Branch: "main", Status: "completed", Conclusion: &success}}}, nil
	}
	r := planWithReader(context.Background(), PlanOptions{Path: d, Subject: "v0.2.0", Mode: "build", Online: true}, reader)
	if r.Status != "ok" || r.Verdict != "pass" {
		t.Fatalf("existing-tag build CI not bound to commit: %+v", r.Gates)
	}
}
