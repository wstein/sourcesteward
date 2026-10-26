// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"runtime/debug"
	"testing"
)

func buildInfoWith(settings ...debug.BuildSetting) func() (*debug.BuildInfo, bool) {
	return func() (*debug.BuildInfo, bool) { return &debug.BuildInfo{Settings: settings}, true }
}

func TestVersionPrintsTheVCSRevision(t *testing.T) {
	full := "6cdaf75afba83d75dbf7ced2d80a8f018fb5bde4"
	for _, tc := range []struct {
		name string
		read func() (*debug.BuildInfo, bool)
		want string
	}{
		{"clean", buildInfoWith(debug.BuildSetting{Key: "vcs.revision", Value: full}, debug.BuildSetting{Key: "vcs.modified", Value: "false"}), "SourceSteward v1.2.3 (6cdaf75afba8)\n"},
		{"modified", buildInfoWith(debug.BuildSetting{Key: "vcs.revision", Value: full}, debug.BuildSetting{Key: "vcs.modified", Value: "true"}), "SourceSteward v1.2.3 (6cdaf75afba8, modified)\n"},
		{"short revision kept whole", buildInfoWith(debug.BuildSetting{Key: "vcs.revision", Value: "abc123"}), "SourceSteward v1.2.3 (abc123)\n"},
		{"no vcs settings (-buildvcs=false, go test)", buildInfoWith(), "SourceSteward v1.2.3\n"},
		{"build info unavailable", func() (*debug.BuildInfo, bool) { return nil, false }, "SourceSteward v1.2.3\n"},
		{"unsafe revision ignored", buildInfoWith(debug.BuildSetting{Key: "vcs.revision", Value: "ab\ncd;rm"}), "SourceSteward v1.2.3\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := readBuildInfo
			readBuildInfo = tc.read
			t.Cleanup(func() { readBuildInfo = old })
			var out, diagnostic bytes.Buffer
			if code := Run(context.Background(), "v1.2.3", []string{"version"}, &out, &diagnostic); code != 0 || diagnostic.Len() != 0 {
				t.Fatalf("code=%d stderr=%q", code, diagnostic.String())
			}
			if out.String() != tc.want {
				t.Fatalf("got %q want %q", out.String(), tc.want)
			}
			out.Reset()
			if Run(context.Background(), "v1.2.3", []string{"version", "--json"}, &out, &diagnostic) != 0 || out.String() != "{\"schema_version\":1,\"name\":\"SourceSteward\",\"version\":\"v1.2.3\"}\n" {
				t.Fatalf("JSON must stay unchanged: %q", out.String())
			}
		})
	}
}
