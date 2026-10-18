// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestNativeCommittedSymlinkInputsRefuseUnsupportedCapability(t *testing.T) {
	for lane, binary := range jjLanes(t) {
		t.Run(lane, func(t *testing.T) {
			useJJ(t, binary)
			for _, tc := range []struct {
				name                      string
				links                     map[string]string
				emptyAllowlist, unrelated bool
			}{
				{name: "direct", links: map[string]string{"scripts/release-package.sh": "../docs/run.sh"}},
				{name: "chain", links: map[string]string{"scripts/release-package.sh": "next", "scripts/next": "../docs/run.sh"}},
				{name: "directory component", links: map[string]string{"scripts": "docs"}},
				{name: "escape", links: map[string]string{"scripts/release-package.sh": "../../outside"}},
				{name: "loop", links: map[string]string{"scripts/release-package.sh": "release-package.sh"}},
				{name: "empty allowlist", links: map[string]string{"scripts/release-package.sh": "../docs/run.sh"}, emptyAllowlist: true},
				{name: "unrelated symlink", links: map[string]string{"docs/link": "run.sh"}, unrelated: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					home := t.TempDir()
					dir, ok := jjLayouts()[1].setup(t, home)
					if !ok {
						t.Skip("standalone unavailable")
					}
					config := string(exampleBytes(t, "go-cli"))
					if tc.emptyAllowlist {
						config = strings.Replace(config, `paths = ["docs/**", "README.md", "llms.txt"]`, `paths = []`, 1)
					}
					write(t, dir, ".stw/release.toml", []byte(config))
					write(t, dir, "docs/run.sh", []byte("build input"))
					write(t, dir, "docs/release-package.sh", []byte("build input"))
					if _, ok := tc.links["scripts"]; !ok {
						if err := os.MkdirAll(dir+"/scripts", 0755); err != nil {
							t.Fatal(err)
						}
					}
					if tc.unrelated {
						write(t, dir, "scripts/release-package.sh", []byte("build input"))
					}
					for path, target := range tc.links {
						if err := os.Symlink(target, dir+"/"+path); err != nil {
							t.Fatal(err)
						}
					}
					if out, err := jjRun(t, dir, home, "commit", "-m", "symlink input"); err != nil {
						t.Fatalf("%s %v", out, err)
					}
					commit, err := jjRun(t, dir, home, "--ignore-working-copy", "log", "--no-graph", "-r", "@-", "-T", "commit_id")
					if err != nil {
						t.Fatal(err)
					}
					if out, err := jjRun(t, dir, home, "git", "remote", "add", "origin", "https://github.com/example/test.git"); err != nil {
						t.Fatalf("%s %v", out, err)
					}
					before, state := snapshot(t, dir), jjState(t, dir, home)
					checked := Check(context.Background(), CheckOptions{Path: dir, Commit: strings.TrimSpace(commit)})
					planned := Plan(context.Background(), PlanOptions{Path: dir, Subject: strings.TrimSpace(commit), Version: "v1.2.3"})
					if tc.unrelated {
						if checked.Status != "ok" || planned.Status != "ok" || len(planned.AllowlistMatches) == 0 || planned.AllowlistMatches[0].TrackedFiles != 3 {
							t.Fatalf("unrelated link rejected or omitted: check=%+v plan=%+v", checked, planned)
						}
					} else {
						if checked.Status != "refused" || checked.Error == nil || checked.Error.Code != "unsupported-backend" || checked.Boundary.Complete {
							t.Errorf("check must refuse unavailable capability: %+v", checked)
						}
						if planned.Status != "refused" || planned.Error == nil || planned.Error.Code != "unsupported-backend" || planned.Boundary.Complete {
							t.Errorf("plan must refuse unavailable capability: %+v", planned)
						}
						if checked.Error != nil && !strings.Contains(checked.Error.Message, "native jj committed symlink input resolution unavailable") {
							t.Errorf("unactionable check refusal: %+v", checked.Error)
						}
						for _, gate := range planned.Gates {
							if gate.Code == "config-invalid" && gate.Outcome != "not-run" {
								t.Errorf("unknown capability judged valid config: %+v", gate)
							}
						}
					}
					if diff := differences(before, snapshot(t, dir)); len(diff) != 0 {
						t.Fatal(diff)
					}
					if jjState(t, dir, home) != state {
						t.Fatal("symlink capability read changed native state")
					}
				})
			}
		})
	}
}

func TestEmptyKnownSymlinkTargetCannotResolveAsParent(t *testing.T) {
	_, _, err := resolveCandidates(fakeTree{"scripts": "dir", "scripts/run": "symlink:"}, []string{"scripts/run"})
	if err == nil {
		t.Fatal("empty symlink target interpreted as parent directory")
	}
}
