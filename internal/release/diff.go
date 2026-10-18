// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

const maxDiffBytes = 4 << 20
const maxDiffEntries = 20000

type DiffResult struct {
	Entries   int      `json:"entries"`
	Offending []string `json:"offending"`
	Truncated bool     `json:"truncated"`
}

// parseRawDiff consumes only raw metadata. No blob content, attributes or executable input
// participates in deciding whether the net diff can be published.
func parseRawDiff(input io.Reader, patterns []string, recordPath string) (r DiffResult, err error) {
	r.Offending = []string{}
	remaining := &io.LimitedReader{R: input, N: maxDiffBytes + 1}
	reader := bufio.NewReader(remaining)
	token := func() (string, error) {
		b, e := reader.ReadBytes(0)
		if remaining.N == 0 {
			return "", errReleaseLimit
		}
		if e != nil {
			if len(b) > 0 && e == io.EOF {
				return "", io.ErrUnexpectedEOF
			}
			return "", e
		}
		return string(b[:len(b)-1]), nil
	}
	for {
		meta, e := token()
		if e == io.EOF {
			return r, nil
		}
		if e != nil {
			return r, e
		}
		path, e := token()
		if e != nil {
			return r, fmt.Errorf("raw diff path: %w", e)
		}
		r.Entries++
		if r.Entries > maxDiffEntries {
			return r, errReleaseLimit
		}
		f := strings.Split(meta, " ")
		if len(f) != 5 || !strings.HasPrefix(f[0], ":") || !IsFullCommitID(f[2]) || !IsFullCommitID(f[3]) {
			return r, fmt.Errorf("invalid raw diff metadata")
		}
		old, new := strings.TrimPrefix(f[0], ":"), f[1]
		regular := func(s string) bool { return s == "100644" || s == "100755" }
		allowed := false
		switch f[4] {
		case "A":
			allowed = old == "000000" && regular(new)
		case "D":
			allowed = regular(old) && new == "000000"
		case "M":
			allowed = regular(old) && new == old
		}
		match := path == recordPath
		for _, p := range patterns {
			match = match || AllowedPath(p, path)
		}
		forbidden := path == ".stw" || strings.HasPrefix(path, ".stw/") || path == ".github" || strings.HasPrefix(path, ".github/")
		if !allowed || !match || !ValidRepoPath(path) || forbidden {
			if len(r.Offending) < 20 {
				r.Offending = append(r.Offending, Escape(path))
			} else {
				r.Truncated = true
			}
		}
	}
}
