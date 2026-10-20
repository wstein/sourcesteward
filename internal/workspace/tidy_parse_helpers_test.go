// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"strings"
)

// textReader backs a production reader function with a fixed stream so tests
// reach the same parsing and error mapping as a real Git read.
func textReader(raw string) gitCollectionReader {
	return func(kind gitRecordKind, _ []string, accept func([]byte) error) error {
		return parseGitRecords(strings.NewReader(raw), kind, accept)
	}
}

func parseGitTidyRefs(raw, format string) ([]gitTidyRef, error) {
	return readGitTidyRefs(textReader(raw), format)
}

func parseGitTidyWorktrees(raw, format string) ([]gitTidyWorktree, error) {
	return readGitTidyWorktrees(textReader(raw), format)
}

func parseTidyV3References(raw, format string) ([]V3Reference, error) {
	return readTidyV3References(textReader(raw), format)
}
