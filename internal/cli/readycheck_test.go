// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/readycheck"
)

// readyForge serves o/r#7 (a draft with green checks) over the guarded reader.
func readyForge(t *testing.T, body, base string) *watchForge {
	t.Helper()
	head := strings.Repeat("3", 40)
	f := &watchForge{t: t, docs: map[string]string{}}
	pull, _ := json.Marshal(map[string]any{"state": "open", "draft": true, "body": body, "mergeable": true, "mergeable_state": "draft", "rebaseable": true,
		"head": map[string]any{"sha": head, "repo": map[string]any{"full_name": "o/r"}}, "base": map[string]any{"ref": base, "repo": map[string]any{"full_name": "o/r"}}})
	f.docs["repos/o/r/pulls/7"] = string(pull)
	f.docs["repos/o/r"] = `{"default_branch":"main","html_url":"https://github.com/o/r"}`
	f.docs["repos/o/r/commits/"+head+"/check-runs?per_page=100&page=1"] = `{"total_count":1,"check_runs":[{"name":"ci","status":"completed","conclusion":"success","output":{}}]}`
	f.docs["repos/o/r/commits/"+head+"/statuses?per_page=100&page=1"] = `[]`
	return f
}

func readyRun(t *testing.T, f *watchForge, clear bool, args ...string) (int, string, string) {
	t.Helper()
	readyCheckTransport = f
	readyCheckHook = func(p *readycheck.Params) {
		if clear {
			p.Review = func(_ context.Context, _ string, _ int, head string) (readycheck.Review, error) {
				return readycheck.Review{State: "clear", Head: head}, nil
			}
		}
	}
	t.Cleanup(func() { readyCheckTransport, readyCheckHook = nil, nil })
	var stdout, stderr bytes.Buffer
	status := Run(context.Background(), "test", append([]string{"ready-check"}, args...), &stdout, &stderr)
	return status, stdout.String(), stderr.String()
}

func TestReadyCheckEndToEndOverTheGuardedReaderIsGETOnly(t *testing.T) {
	watchEnv(t, true)
	f := readyForge(t, "Purpose.\n\nBlocker: none\n", "main")
	status, out, errOut := readyRun(t, f, true, "7", "--repo", "o/r", "--json")
	if status != 0 || errOut != "" {
		t.Fatalf("%d %q %s", status, errOut, out)
	}
	var d readycheck.Document
	if err := json.Unmarshal([]byte(out), &d); err != nil || !d.Ready {
		t.Fatalf("%v %s", err, out)
	}
	// pulls/7, the repository, check runs and statuses of the head; the review is the hook. The
	// forge fails the test on any request that is not a GET.
	if len(f.log) != 4 {
		t.Fatalf("requests: %v", f.log)
	}
}

func TestReadyCheckExitsOneWithAnUnmetConditionAndStillPrints(t *testing.T) {
	watchEnv(t, true)
	status, out, errOut := readyRun(t, readyForge(t, "Blocker: Independent review pending\n", "main"), true, "7", "--repo", "o/r", "--json")
	if status != 1 || !strings.Contains(errOut, "blocker fail") || !strings.Contains(out, `"ready":false`) {
		t.Fatalf("%d %q %s", status, errOut, out)
	}
}

func TestReadyCheckWithoutTheReviewParserNeverPasses(t *testing.T) {
	watchEnv(t, true)
	status, out, errOut := readyRun(t, readyForge(t, "Blocker: none\n", "main"), false, "7", "--repo", "o/r", "--json")
	if status != 1 || !strings.Contains(errOut, "review unknown") || !strings.Contains(out, "Spec 046") {
		t.Fatalf("%d %q %s", status, errOut, out)
	}
}

func TestReadyCheckUsageErrorsExitTwo(t *testing.T) {
	for _, args := range [][]string{{}, {"0", "--repo", "o/r"}, {"7", "--repo", "bad"}, {"7", "extra", "--repo", "o/r"}, {"7", "--repo", "o/r", "--human", "--json"}} {
		var stdout, stderr bytes.Buffer
		if status := Run(context.Background(), "test", append([]string{"ready-check"}, args...), &stdout, &stderr); status != 2 {
			t.Errorf("%v: status %d %q", args, status, stderr.String())
		}
	}
}
