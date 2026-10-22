// SPDX-License-Identifier: EUPL-1.2
package watch

import (
	"os"
	"os/exec"
	"testing"
)

// TestCompilesOnEveryPlatformItClaimsOrStubs builds the package for the Unix systems of the flock
// implementation and for Windows (stubs), so a build tag slip fails here and not in a release.
func TestCompilesOnEveryPlatformItClaimsOrStubs(t *testing.T) {
	if testing.Short() {
		t.Skip("cross builds are skipped in -short")
	}
	for _, target := range [][2]string{{"windows", "amd64"}, {"freebsd", "amd64"}, {"openbsd", "amd64"}, {"darwin", "arm64"}} {
		cmd := exec.Command("go", "build", ".")
		cmd.Env = append(os.Environ(), "GOOS="+target[0], "GOARCH="+target[1], "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("%s/%s: %v\n%s", target[0], target[1], err, out)
		}
	}
}
