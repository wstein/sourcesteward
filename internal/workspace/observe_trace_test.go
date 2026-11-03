// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"os/exec"
	"reflect"
	"testing"
)

// The trace records the Git arguments only, whichever sandbox launcher carries them:
// bwrap puts them after "--", sandbox-exec after "-p <profile> <git>".
func TestTraceCommandRecordsGitArgumentsUnderEachSandbox(t *testing.T) {
	want := []string{"rev-list", "--count", "HEAD"}
	git := []string{"/opt/homebrew/bin/git", "--no-replace-objects", "-c", "core.x=y", "rev-list", "--count", "HEAD"}
	cases := map[string]*exec.Cmd{
		"direct":       {Path: "/opt/homebrew/bin/git", Args: git},
		"bwrap":        {Path: "/usr/bin/bwrap", Args: append([]string{"bwrap", "--ro-bind", "/", "/", "--"}, git...)},
		"sandbox-exec": {Path: "/usr/bin/sandbox-exec", Args: append([]string{"sandbox-exec", "-p", `(version 1)(allow default)(deny network*)`}, git...)},
	}
	for name, cmd := range cases {
		t.Run(name, func(t *testing.T) {
			var got []string
			ctx := context.WithValue(context.Background(), gitTraceKey{}, func(c GitCall) { got = c.Argv })
			traceCommand(ctx, cmd)()
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("argv %q, want %q", got, want)
			}
		})
	}
}
