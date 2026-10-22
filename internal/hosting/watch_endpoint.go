// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"regexp"
	"strconv"
	"strings"
)

// Page bounds of Spec 035: 2000 open PRs per repository at 25 or 12 per page.
const (
	watchMaxPages25 = 80
	watchMaxPages12 = 167
	// WatchMaxOpenPRs is the sanity bound for max_open_prs (Spec 035).
	WatchMaxOpenPRs = 2000
)

// watchRoute lists the fixed GET endpoints added for the watch reader (Spec 035
// amendment of Spec 022 FR-005). pulls/N and commits/SHA/check-runs|statuses are
// already part of the one-shot allowlist and stay there. The path is matched in
// its escaped form, so a branch segment arrives exactly as EncodeBranchSegment
// wrote it.
var watchRoute = regexp.MustCompile(`\A(?:rate_limit|repos/[A-Za-z0-9_-][A-Za-z0-9_.-]*/[A-Za-z0-9_-][A-Za-z0-9_.-]*/(?:pulls\?state=open&per_page=(25|12)&page=([1-9][0-9]*)|pulls/[1-9][0-9]*/commits\?per_page=100&page=[1-3]|branches/([^/?#]+)|commits/[0-9a-f]{40}))\z`)

func watchReadEndpoint(p string) bool {
	m := watchRoute.FindStringSubmatch(p)
	if m == nil {
		return false
	}
	if p != "rate_limit" {
		parts := strings.SplitN(p, "/", 4)
		if !validRepo(parts[1] + "/" + parts[2]) {
			return false
		}
	}
	if m[1] != "" {
		page, err := strconv.Atoi(m[2])
		limit := watchMaxPages25
		if m[1] == "12" {
			limit = watchMaxPages12
		}
		return err == nil && page <= limit
	}
	if m[3] != "" {
		return branchSegmentValid(m[3])
	}
	return true
}
