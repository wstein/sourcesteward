// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestReleaseBuildParserRefusesInvalidCombinations(t *testing.T) {
	for _, args := range [][]string{{}, {"--mode", "ci"}, {"--mode", "local-verify", "--commit", strings.Repeat("a", 40), "--version", "v1.2.3"}, {"--mode", "promote", "--tag", "v1.2.3", "--record", "file", "--out", "outside"}, {"--mode", "ci", "--commit", strings.Repeat("a", 40), "--version", "v1.2.3", "--out", "unused", "--smoke", "target", "--files-only"}} {
		cmd := releaseBuildCommand()
		cmd.SetArgs(args)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		if err := cmd.Execute(); err == nil {
			t.Errorf("accepted %q", args)
		}
	}
}
func TestReleaseBuildCIRefusalRendersJSON(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "false")
	cmd := releaseBuildCommand()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--mode", "ci", "--commit", strings.Repeat("a", 40), "--version", "v1.2.3", "--out", "unused", "--json"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("outside CI succeeded")
	}
	if !strings.Contains(output.String(), `"code":"ci-only"`) {
		t.Fatal(output.String())
	}
}
func TestReleaseBuildMismatchExitStatus(t *testing.T) {
	if code := exitStatus(&bytes.Buffer{}, releaseArtifactMismatch{}); code != 3 {
		t.Fatalf("artifact mismatch exit %d, want3", code)
	}
}
func TestReleaseBuildRegisteredAndGroupStaysHidden(t *testing.T) {
	cmd := releaseCommand()
	if !cmd.Hidden {
		t.Fatal("release group advertised before WI9")
	}
	found := false
	for _, child := range cmd.Commands() {
		if child.Name() == "build" {
			found = true
		}
	}
	if !found {
		t.Fatal("build command not registered")
	}
}
