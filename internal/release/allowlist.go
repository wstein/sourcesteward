// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"strings"
	"unicode/utf8"
)

// Allowlist patterns are globs over '/'-separated repository paths: '*' matches any characters
// within one path segment, '**' as a whole segment matches zero or more whole segments, and
// there is no other metacharacter. A pattern must not hide a build input, so the linter asks
// whether a pattern could match the declared inputs; classification of a diff entry (a later
// work item) asks whether a validated path is matched.

const maxPatternBytes = 256

// maxPathBytes bounds a repository path the matcher accepts.
const maxPathBytes = 4096

func patternSegments(pattern string) []string { return strings.Split(pattern, "/") }

// PatternProblem returns why a pattern is not a valid allowlist glob, or "".
func PatternProblem(pattern string) string {
	if len(pattern) > maxPatternBytes {
		return "the pattern is longer than 256 bytes"
	}
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-' || c == '*' || c == '/'
		if !ok {
			return "the pattern uses a character that is not allowed; only A-Z, a-z, 0-9, . _ - * and / are"
		}
	}
	for _, segment := range patternSegments(pattern) {
		switch {
		case segment == "":
			return "the pattern is empty or has an empty segment, a leading slash or a trailing slash"
		case segment == "." || segment == "..":
			return "the pattern has a dot or dot-dot segment"
		case strings.EqualFold(segment, ".git"):
			return "the pattern names a .git directory"
		case strings.Contains(segment, "**") && segment != "**":
			return "** is allowed only as a whole segment"
		}
	}
	return ""
}

// ValidRepoPath reports whether p is a plain repository-relative path a diff entry or a declared
// input may have: UTF-8, no control character or backslash, no empty, dot, dot-dot or .git
// segment, at most 4096 bytes.
func ValidRepoPath(p string) bool {
	if len(p) > maxPathBytes || !utf8.ValidString(p) {
		return false
	}
	for i := 0; i < len(p); i++ {
		if c := p[i]; c < 0x20 || c == 0x7f || c == '\\' {
			return false
		}
	}
	for _, segment := range strings.Split(p, "/") {
		if segment == "" || segment == "." || segment == ".." || strings.EqualFold(segment, ".git") {
			return false
		}
	}
	return true
}

// MatchPattern reports whether the glob matches the path exactly, case-sensitively and without
// validating either; use AllowedPath to classify a path.
func MatchPattern(pattern, path string) bool {
	return matchSegments(patternSegments(pattern), strings.Split(path, "/"))
}

// AllowedPath is the fail-closed classification: a path that is not a valid repository path is
// never allowed, whatever the pattern.
func AllowedPath(pattern, path string) bool {
	return ValidRepoPath(path) && MatchPattern(pattern, path)
}

func matchSegments(pattern, path []string) bool {
	// reachable[j] means the first j path segments are matched by the pattern consumed so far.
	reachable := make([]bool, len(path)+1)
	reachable[0] = true
	for _, segment := range pattern {
		next := make([]bool, len(path)+1)
		if segment == "**" {
			seen := false
			for j := 0; j <= len(path); j++ {
				seen = seen || reachable[j]
				next[j] = seen
			}
		} else {
			for j := 0; j < len(path); j++ {
				next[j+1] = reachable[j] && matchSegment(segment, path[j])
			}
		}
		reachable = next
	}
	return reachable[len(path)]
}

// matchSegment matches one path segment against a pattern segment whose only metacharacter is '*'.
func matchSegment(pattern, name string) bool {
	p, n := 0, 0
	star, mark := -1, 0
	for n < len(name) {
		switch {
		case p < len(pattern) && pattern[p] == '*':
			star, mark = p, n
			p++
		case p < len(pattern) && pattern[p] == name[n]:
			p++
			n++
		case star >= 0:
			p = star + 1
			mark++
			n = mark
		default:
			return false
		}
	}
	for p < len(pattern) && pattern[p] == '*' {
		p++
	}
	return p == len(pattern)
}

// PatternOverlapsFile reports whether the pattern matches the declared input file, ignoring case
// so that a case-insensitive file system cannot hide an input.
func PatternOverlapsFile(pattern, file string) bool {
	return MatchPattern(strings.ToLower(pattern), strings.ToLower(file))
}

// PatternOverlapsDirectory reports whether the pattern matches the directory itself or could
// match some path below it, ignoring case.
func PatternOverlapsDirectory(pattern, dir string) bool {
	segments := patternSegments(strings.ToLower(pattern))
	// states holds the pattern positions still open after the directory segments consumed so far;
	// only whether any remains matters, so a ** that could also match nothing needs no extra state.
	states := map[int]bool{0: true}
	for _, part := range strings.Split(strings.ToLower(dir), "/") {
		next := map[int]bool{}
		for i := range states {
			if i >= len(segments) {
				continue
			}
			if segments[i] == "**" {
				// ** can match this segment and any number more, so the pattern stays open.
				next[i] = true
			} else if matchSegment(segments[i], part) {
				next[i+1] = true
			}
		}
		states = next
	}
	// Any unfinished pattern can still match a name below the directory; a finished one matches it.
	return len(states) > 0
}
