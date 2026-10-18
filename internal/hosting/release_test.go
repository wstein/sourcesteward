// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestReleaseReadsTypedAndFixed(t *testing.T) {
	var paths []string
	api := apiReaderFunc(func(_ context.Context, path string) ([]byte, error) {
		paths = append(paths, path)
		switch {
		case strings.Contains(path, "/runs?"):
			return []byte(`{"total_count":1,"workflow_runs":[{"head_sha":"` + strings.Repeat("a", 40) + `","event":"push","head_branch":"main","status":"completed","conclusion":"success"}]}`), nil
		case strings.Contains(path, "/releases?"):
			return []byte(`[{"tag_name":"v1.0.0","draft":true,"assets":[{"name":"SHA256SUMS"}]}]`), nil
		case strings.Contains(path, "/tags?"):
			return []byte(`[{"name":"v1.0.0"}]`), nil
		case strings.HasSuffix(path, "/git/ref/heads/main"):
			return []byte(`{"ref":"refs/heads/main","object":{"type":"commit","sha":"` + strings.Repeat("b", 40) + `"}}`), nil
		default:
			return nil, fmt.Errorf("unexpected endpoint")
		}
	})
	got, err := readRelease(context.Background(), api, "owner/repo", strings.Repeat("a", 40), []string{"ci.yml"})
	if err != nil || len(got.Runs) != 1 || len(got.Releases) != 1 || len(got.Tags) != 1 || got.MainSHA != strings.Repeat("b", 40) {
		t.Fatalf("%+v %v", got, err)
	}
	if len(paths) != 5 || paths[0] != "repos/owner/repo/actions/workflows/ci.yml/runs?head_sha="+strings.Repeat("a", 40)+"&per_page=100&page=1" {
		t.Fatal(paths)
	}
	for _, p := range paths {
		if !releaseEndpoint(p) {
			t.Fatalf("guard refused %q", p)
		}
	}
}

func TestReleaseReadsRefuseTruncationAndMalformedEvidence(t *testing.T) {
	for _, body := range []string{`null`, `{}`, `[{"name":null}]`, strings.Repeat(" ", maxBytes+1)} {
		_, err := releaseTags(context.Background(), apiReaderFunc(func(context.Context, string) ([]byte, error) { return []byte(body), nil }), "owner/repo", nil)
		if err == nil {
			t.Fatalf("accepted malformed tags %q", body[:min(30, len(body))])
		}
	}
	n := 0
	_, err := releaseTags(context.Background(), apiReaderFunc(func(context.Context, string) ([]byte, error) {
		n++
		return []byte("[" + strings.Repeat(`{"name":"v1"},`, 99) + `{"name":"v1"}]`), nil
	}), "owner/repo", nil)
	if err == nil || n != 10 {
		t.Fatalf("pages=%d err=%v", n, err)
	}
}

func TestReleaseEndpointClosed(t *testing.T) {
	for _, p := range []string{"repos/o/r/releases?per_page=100&page=11", "repos/o/r/releases?per_page=101&page=1", "repos/o/r/actions/workflows/../runs?head_sha=" + strings.Repeat("a", 40) + "&per_page=100&page=1", "repos/o/r/git/ref/heads/other", "repos/o/r/rulesets/1", "repos/o/r/releases/1"} {
		if releaseEndpoint(p) {
			t.Fatal(p)
		}
	}
}
