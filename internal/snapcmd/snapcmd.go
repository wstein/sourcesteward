// SPDX-License-Identifier: EUPL-1.2

// Package snapcmd holds what the snapshot commands `stw pulls` and `stw issues` share: the usage and
// failure errors, the output mode, bounded text helpers and the owner discovery errors of their documents.
package snapcmd

import (
	"fmt"
	"io"
	"os"
	"unicode/utf8"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/watchcmd"
)

// MaxMessage bounds an error message of a document.
const MaxMessage = 512

// Usagef is a usage error (exit 2).
func Usagef(format string, args ...any) error {
	return fmt.Errorf("%w: %s", watchcmd.ErrUsage, fmt.Sprintf(format, args...))
}

// Failure is a terminal error with the exit code it maps to.
func Failure(code int, format string, args ...any) *watchcmd.Failure {
	return &watchcmd.Failure{Code: code, Err: fmt.Errorf(format, args...)}
}

// TerminalHuman: --human, else --json, else human exactly when stdout is a terminal.
func TerminalHuman(human, json bool, out io.Writer) bool {
	switch {
	case human:
		return true
	case json:
		return false
	}
	file, ok := out.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// Cut shortens s to at most n bytes on a UTF-8 boundary.
func Cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// StrPtr is nil for the empty string.
func StrPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Strings0 turns nil into an empty slice so that a JSON array is never null.
func Strings0(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// Plural writes a count with its noun.
func Plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// DocError is one failed repository (Repo set), owner discovery (Owner set) or, with both nil, run.
type DocError struct {
	Repo    *string `json:"repo"`
	Owner   *string `json:"owner"`
	Code    string  `json:"code"`
	Message string  `json:"message"`
}

// OwnerErrorCount counts the errors of owner discoveries.
func OwnerErrorCount(errs []DocError) int {
	n := 0
	for _, e := range errs {
		if e.Owner != nil {
			n++
		}
	}
	return n
}

// OwnerErrors turns one discovery into error entries. Results with an unusable repository_url
// are always reported as malformed-response, also when the search was incomplete or too large.
func OwnerErrors(d hosting.OwnerDiscovery) []DocError {
	var out []DocError
	owner := d.Owner
	add := func(code, msg string) {
		out = append(out, DocError{Owner: &owner, Code: code, Message: Cut(msg, MaxMessage)})
	}
	if d.Code != "" {
		add(d.Code, d.Message)
	}
	if d.Ignored > 0 && !(d.Code == hosting.CodeMalformed && d.Message == hosting.MessageIgnoredResults) {
		add(hosting.CodeMalformed, fmt.Sprintf("%s ignored: unusable repository", Plural(d.Ignored, "search result", "search results")))
	}
	return out
}
