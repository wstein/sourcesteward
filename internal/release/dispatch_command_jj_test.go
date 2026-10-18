// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/hosting"
)

func TestDispatchCommandGitButlerRefusesBeforeOnline(t *testing.T) {
	d, sha := dispatchNativeFixture(t)
	if err := os.MkdirAll(filepath.Join(d, ".git", "gitbutler"), 0755); err != nil {
		t.Fatal(err)
	}
	r := dispatchWithAdapters(context.Background(), DispatchOptions{Path: d, Commit: sha, Version: "v0.2.0"}, func(context.Context, string, hosting.Repository, string, []string) (hosting.ReleaseEvidence, error) {
		t.Fatal("managed refusal reached online reads")
		return hosting.ReleaseEvidence{}, nil
	}, func(context.Context, string, hosting.Repository, string, hosting.WorkflowDispatchInputs) (hosting.DispatchResponse, error) {
		t.Fatal("managed refusal wrote")
		return hosting.DispatchResponse{}, nil
	})
	if r.Error == nil || r.Error.Code != "unsupported-backend" || r.Boundary.Effects.Network || r.Boundary.Effects.Writes {
		t.Fatalf("GitButler refusal=%+v", r)
	}
}

func TestDispatchCommandNativeJJPlansAndAppliesWithoutStateChanges(t *testing.T) {
	for lane, binary := range jjLanes(t) {
		for _, layout := range jjLayouts() {
			t.Run(lane+"/"+layout.name, func(t *testing.T) {
				useJJ(t, binary)
				home := t.TempDir()
				d, ok := layout.setup(t, home)
				if !ok {
					t.Skip("native layout unavailable")
				}
				// The seed is a separate Git-owned temporary repository. All writes inside
				// the jj workspace use native jj, including a fetch from this local fixture.
				seed := repoWith(t, map[string][]byte{".stw/release.toml": exampleBytes(t, "go-cli")})
				sha := commitAll(t, seed)
				tags := []string{"deadbeef1", strings.Repeat("a", 39) + "1", strings.Repeat("a", 63) + "1"}
				for _, tag := range tags {
					git(t, seed, "tag", "-a", tag, "-m", "annotated seed tag")
				}
				for _, args := range [][]string{{"git", "remote", "add", "origin", seed}, {"git", "fetch", "--remote", "origin"}, {"git", "remote", "set-url", "origin", "https://github.com/example/test.git"}} {
					if out, err := jjRun(t, d, home, args...); err != nil {
						t.Fatalf("native fixture %q: %v %s", args, err, out)
					}
				}
				write(t, d, "unsnapshotted", []byte("dirty native state"))
				before, state := snapshot(t, d), jjState(t, d, home)
				reads, writes := 0, 0
				reader := func(_ context.Context, root string, repo hosting.Repository, commit string, _ []string) (hosting.ReleaseEvidence, error) {
					reads++
					if root != d || repo.Owner != "example" || commit != sha {
						t.Fatalf("native identity lost: %s %+v %s", root, repo, commit)
					}
					return dispatchOnlineFixture(sha, sha), nil
				}
				writer := func(_ context.Context, root string, _ hosting.Repository, _ string, inputs hosting.WorkflowDispatchInputs) (hosting.DispatchResponse, error) {
					writes++
					if root != d || inputs.Commit != sha {
						t.Fatal("write lost native identity")
					}
					return hosting.DispatchResponse{Requests: 1, StatusCode: 204}, nil
				}
				o := DispatchOptions{Path: d, Commit: sha, Version: "v0.2.0"}
				plan := dispatchWithAdapters(context.Background(), o, reader, writer)
				if plan.Status != "ok" || plan.Verdict != "pass" || plan.Backend != "jj" || len(plan.PlanID) != 64 || writes != 0 {
					t.Fatalf("native plan=%+v writes=%d", plan, writes)
				}
				o.Apply = plan.PlanID
				applied := dispatchWithAdapters(context.Background(), o, reader, writer)
				if applied.Status != "ok" || !applied.Applied || writes != 1 || reads != 2 {
					t.Fatalf("native apply=%+v writes=%d reads=%d", applied, writes, reads)
				}
				for _, name := range append(tags, "v0.2.0") {
					tag := dispatchWithAdapters(context.Background(), DispatchOptions{Path: d, Tag: name}, reader, writer)
					if tag.Error == nil || tag.Error.Code != "unsupported-backend" || reads != 2 || writes != 1 {
						t.Fatalf("native tag %q refusal=%+v", name, tag)
					}
				}
				if after := jjState(t, d, home); after != state {
					t.Fatal("native dispatch changed jj operation or working-copy state")
				}
				if changed := differences(before, snapshot(t, d)); len(changed) != 0 {
					t.Fatalf("native dispatch changed files: %v", changed)
				}
			})
		}
	}
}
