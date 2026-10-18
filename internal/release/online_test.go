// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"github.com/wstein/sourcesteward/internal/hosting"
	"strings"
	"testing"
)

func TestOnlineGateDecisions(t *testing.T) {
	c := &Contract{Version: Version{Scheme: "semver"}, CI: CI{Workflows: []string{"ci.yml"}}}
	sha := strings.Repeat("a", 40)
	yes := true
	no := false
	success := "success"
	e := hosting.ReleaseEvidence{Runs: []hosting.ReleaseRun{{Workflow: "ci.yml", HeadSHA: sha, Event: "push", Branch: "main", Status: "completed", Conclusion: &success}}, Tags: []string{"v0.9.0", "other/v9.0.0"}}
	gates, err := onlineGates(PlanOptions{Version: "v0.10.0", Subject: sha}, c, nil, e, false)
	if err != nil || Verdict(gates) != "pass" {
		t.Fatalf("%+v %v", gates, err)
	}
	if gates[1].Reason == nil || *gates[1].Reason != "drafts-not-checked" {
		t.Fatal(gates)
	}
	for _, run := range []hosting.ReleaseRun{{Workflow: "ci.yml", HeadSHA: sha, Event: "pull_request", Branch: "main", Status: "completed", Conclusion: &success}, {Workflow: "ci.yml", HeadSHA: sha, Event: "push", Branch: "topic", Status: "completed", Conclusion: &success}, {Workflow: "ci.yml", HeadSHA: sha, Event: "push", Branch: "main", Status: "queued"}} {
		e.Runs = []hosting.ReleaseRun{run}
		g, _ := onlineGates(PlanOptions{Version: "v0.10.0", Subject: sha}, c, nil, e, false)
		if Verdict(g) != "fail" {
			t.Fatal(g)
		}
	}
	e.Releases = []hosting.ReleaseEntry{{Tag: "v0.10.0", Draft: &yes}}
	g, _ := onlineGates(PlanOptions{Version: "v0.10.0", Subject: sha, WithDrafts: true}, c, nil, e, false)
	if Verdict(g) != "fail" {
		t.Fatal(g)
	}
	e.Releases = []hosting.ReleaseEntry{{Tag: "v0.10.0", Draft: &no}}
	record := &ReleaseRecord{Tag: "v0.10.0", Version: "v0.10.0", Commit: sha}
	g, _ = onlineGates(PlanOptions{Version: record.Version, Subject: record.Tag, Mode: "publish"}, c, record, e, true)
	for _, gate := range g {
		if gate.Code == "version-not-monotonic" || gate.Code == "already-published" || gate.Code == "draft-missing" {
			if gate.Outcome != "not-applicable" {
				t.Fatal(g)
			}
		}
	}
}

func TestVisibleDraftAlwaysContributesToMonotonicity(t *testing.T) {
	draft := true
	c := &Contract{Version: Version{Scheme: "semver"}}
	e := hosting.ReleaseEvidence{Releases: []hosting.ReleaseEntry{{Tag: "v9.0.0", Draft: &draft}}}
	gates, err := onlineGates(PlanOptions{Version: "v1.0.0", Subject: strings.Repeat("a", 40)}, c, nil, e, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range gates {
		if g.Code == "version-not-monotonic" && g.Outcome != "fail" {
			t.Fatal("visible newer draft omitted from monotonicity", gates)
		}
		if g.Code == "release-exists" && g.Outcome != "pass" {
			t.Fatal("different-tag draft blocked existence", gates)
		}
	}
}
