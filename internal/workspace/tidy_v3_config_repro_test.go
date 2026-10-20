// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Reproduction of issue #79: a normal workspace (the Anchorage checkout) carries
// submodule.* and core.hooksPath and planning was refused with
// git-unsafe-configuration. Both keys are neutralized for planning (Spec 031).
func TestTidyV3PlanReproducesIssue79(t *testing.T) {
	for _, key := range []string{"submodule.member.url", "core.hooksPath"} {
		t.Run(key, func(t *testing.T) {
			dir, _ := fixtureTidyV3Git(t)
			value := "https://example.invalid/member.git"
			if key == "core.hooksPath" {
				value = filepath.Join(gitTempDir(t), "hooks")
				if err := os.MkdirAll(value, 0700); err != nil {
					t.Fatal(err)
				}
			}
			gitFixture(t, dir, "config", key, value)
			base := gitFixture(t, dir, "symbolic-ref", "HEAD")
			r := defaultTidyV3RefsCoordinator().plan(context.Background(), V3PlanOptions{Path: dir, Base: base, ProtectWorktrees: []string{}})
			if r.Status != "ok" || r.Plan == nil {
				t.Fatalf("plan refused: status=%s diagnostics=%+v", r.Status, r.Diagnostics)
			}
		})
	}
}
