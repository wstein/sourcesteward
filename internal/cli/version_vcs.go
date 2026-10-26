// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"regexp"
	"runtime/debug"
)

// readBuildInfo is a variable so tests can supply build settings.
var readBuildInfo = debug.ReadBuildInfo

var vcsRevisionPattern = regexp.MustCompile(`^[0-9a-f]{4,64}$`)

// vcsSuffix is the build identity the Go toolchain stamps into a binary built from a
// version-control checkout: " (REVISION)" or " (REVISION, modified)" with the revision
// shortened to 12 characters. It is empty when the binary carries no vcs.revision (a
// -buildvcs=false build such as the release, a test binary) or an unexpected value.
func vcsSuffix() string {
	info, ok := readBuildInfo()
	if !ok || info == nil {
		return ""
	}
	revision, modified := "", false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if !vcsRevisionPattern.MatchString(revision) {
		return ""
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	if modified {
		return " (" + revision + ", modified)"
	}
	return " (" + revision + ")"
}
