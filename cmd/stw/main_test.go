// SPDX-License-Identifier: EUPL-1.2
package main

import (
	"os"
	"testing"
)

// The private native route (spec 026 T031/T034) is entered only when the process is
// invoked as "git" with the private role variable, before any command parsing. Every
// other invocation reaches the public CLI unchanged.
func runWith(t *testing.T, role string, args ...string) int {
	t.Helper()
	saved := os.Args
	t.Cleanup(func() { os.Args = saved })
	os.Args = args
	t.Setenv("STW_BUILD_VIEW_ROLE", role)
	t.Setenv("STW_BUILD_VIEW_MANIFEST", "") // an invalid manifest: a taken route refuses with status 1
	return run()
}

func TestRunEntersThePrivateRouteOnlyAsGitWithTheRole(t *testing.T) {
	if code := runWith(t, "route", "/private/route/git", "status", "--porcelain=2"); code != 1 {
		t.Fatalf("private route not taken: status %d, want the route's refusal 1", code)
	}
	// Public command parsing: an unknown command is a usage error (2), not a route refusal.
	for name, c := range map[string]struct {
		role string
		args []string
	}{
		"git without the role":   {"", []string{"/usr/bin/git", "status", "--porcelain=2"}},
		"role under argv0 stw":   {"route", []string{"/usr/bin/stw", "status", "--porcelain=2"}},
		"wrong role value":       {"other", []string{"/private/route/git", "status", "--porcelain=2"}},
		"stw with ordinary args": {"", []string{"stw", "status", "--porcelain=2"}},
	} {
		if code := runWith(t, c.role, c.args...); code != 2 {
			t.Errorf("%s: status %d, want the public CLI usage status 2", name, code)
		}
	}
}
