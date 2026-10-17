// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"sort"
	"strings"
)

// Finding is one result of linting a release contract (contracts/check.md). Pointer is an RFC 6901
// JSON pointer into the contract document ("" is the document itself). File names the contract
// file when a repository holds several products.
type Finding struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Pointer  string `json:"pointer"`
	Message  string `json:"message"`
	File     string `json:"file,omitempty"`
	line     int
	// scope lists the product ids a discovery finding is about, each as |id| (the default product
	// is ||); empty means every product.
	scope string
	// partial marks the note that a parser stopped early; it is replaced by one final cut message.
	partial bool
}

const (
	severityError = "error"
	severityInfo  = "info"
)

// MaxFindings is the number of findings one check reports before limit-exceeded.
const MaxFindings = 100

func errorFinding(code, pointer, message string) Finding {
	return Finding{Code: code, Severity: severityError, Pointer: pointer, Message: message}
}

// sortFindings orders findings by file, JSON pointer, source line, code and message, so the
// output is independent of the order checks ran in.
func sortFindings(findings []Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Pointer != b.Pointer {
			return pointerLess(a.Pointer, b.Pointer)
		}
		if a.line != b.line {
			return a.line < b.line
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Message < b.Message
	})
}

// pointerLess orders JSON pointers segment by segment, with array indexes compared as numbers so
// /build/2 comes before /build/10.
func pointerLess(a, b string) bool {
	as, bs := strings.Split(a, "/"), strings.Split(b, "/")
	for i := 0; i < len(as) && i < len(bs); i++ {
		if as[i] == bs[i] {
			continue
		}
		if isIndex(as[i]) && isIndex(bs[i]) {
			x, y := strings.TrimLeft(as[i], "0"), strings.TrimLeft(bs[i], "0")
			if x != y {
				if len(x) != len(y) {
					return len(x) < len(y)
				}
				return x < y
			}
			if len(as[i]) != len(bs[i]) {
				return len(as[i]) < len(bs[i])
			}
		}
		return as[i] < bs[i]
	}
	return len(as) < len(bs)
}

func isIndex(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
