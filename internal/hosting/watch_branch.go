// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"errors"
	"net/url"
	"strings"
	"unicode/utf8"
)

// EncodeBranchSegment is the single function that turns a branch name into the
// percent-encoded path segment of branches/BASE (Spec 035 amendment of Spec 022
// FR-005). The name is validated first; the result is url.PathEscape of the whole
// name, so a slash becomes %2F and the segment never adds a path component.
func EncodeBranchSegment(name string) (string, error) {
	if !validBranchName(name) {
		return "", errors.New("invalid branch name for a hosting read")
	}
	return url.PathEscape(name), nil
}

// validBranchName applies the git ref-name rules that matter for a path segment
// plus the Spec 035 rules. A literal percent sign is refused so that no encoded
// text can pass as a name (double encoding such as %252F stays impossible).
func validBranchName(n string) bool {
	if n == "" || len(n) > 255 || !utf8.ValidString(n) || n == "@" || n[0] == '-' || n[0] == '/' || strings.HasSuffix(n, "/") || strings.HasSuffix(n, ".") || strings.HasSuffix(n, ".lock") || strings.Contains(n, "..") || strings.Contains(n, "@{") || strings.Contains(n, "//") {
		return false
	}
	for _, r := range n {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(" ~^:?*[\\%", r) {
			return false
		}
	}
	for _, part := range strings.Split(n, "/") {
		if strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}

// branchSegmentValid reports whether raw is exactly the output of
// EncodeBranchSegment: it is decoded once, the decoded name must be valid and
// re-encoding must reproduce the same text. This refuses %2E, %00, %5C, %252F,
// lowercase hex and every other alternative spelling.
func branchSegmentValid(raw string) bool {
	if raw == "" {
		return false
	}
	name, err := url.PathUnescape(raw)
	if err != nil || !validBranchName(name) {
		return false
	}
	return url.PathEscape(name) == raw
}
