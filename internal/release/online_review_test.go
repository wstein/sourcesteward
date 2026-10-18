// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"github.com/wstein/sourcesteward/internal/hosting"
	"testing"
)

func TestCanceledOnlineReaderCannotEstablishPass(t *testing.T) {
	d := repoWith(t, map[string][]byte{".stw/release.toml": exampleBytes(t, "go-cli")})
	sha := commitAll(t, d)
	git(t, d, "remote", "add", "origin", "https://github.com/example/test.git")
	git(t, d, "update-ref", "refs/remotes/origin/main", sha)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	success := "success"
	reader := func(context.Context, string, hosting.Repository, string, []string) (hosting.ReleaseEvidence, error) {
		cancel()
		return hosting.ReleaseEvidence{Runs: []hosting.ReleaseRun{{Workflow: "ci.yml", HeadSHA: sha, Event: "push", Branch: "main", Status: "completed", Conclusion: &success}}}, nil
	}
	r := planWithReader(ctx, PlanOptions{Path: d, Subject: sha, Version: "v0.2.0", Online: true}, reader)
	if r.Status != "refused" || r.Error == nil || r.Error.Code != "canceled" || r.Boundary.Complete {
		t.Fatalf("canceled evidence accepted: %+v", r)
	}
}
