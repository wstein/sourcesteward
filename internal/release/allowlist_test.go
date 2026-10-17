// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"regexp"
	"strings"
	"testing"
)

func TestPatternSyntax(t *testing.T) {
	good := []string{"docs/**", "README.md", "*.md", "docs/*/x", "**", "**/x", "a/**/b", "docs/releases/*.json", "a*b", "*", ".github-docs/x"}
	for _, p := range good {
		if reason := PatternProblem(p); reason != "" {
			t.Errorf("%q rejected: %s", p, reason)
		}
	}
	bad := map[string]string{
		"":          "empty",
		"/docs":     "leading slash",
		"docs/":     "trailing slash",
		"docs//x":   "empty segment",
		"../x":      "dot-dot segment",
		"a/../x":    "dot-dot segment",
		"./x":       "dot segment",
		"a**":       "double star inside a segment",
		"**a":       "double star inside a segment",
		"a/***":     "triple star",
		"a/**b/c":   "double star inside a segment",
		"a?b":       "other metacharacter",
		"a[b]":      "other metacharacter",
		"a{b,c}":    "other metacharacter",
		"a\\b":      "backslash",
		"a\nb":      "control character",
		"é":         "non-ASCII",
		"a b":       "space",
		"!negated":  "negation",
		"a/.git/**": "git directory",
	}
	bad[strings.Repeat("a", 257)] = "longer than 256 bytes"
	bad["a/.Git/x"] = "git directory in another case"
	bad["a/.GIT"] = "git directory in another case"
	if reason := PatternProblem(strings.Repeat("a", 256)); reason != "" {
		t.Errorf("256 bytes rejected: %s", reason)
	}
	for p, why := range bad {
		if PatternProblem(p) == "" {
			t.Errorf("%q (%s) accepted", p, why)
		}
	}
}

func TestPatternMatching(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"docs/**", "docs/a.md", true},
		{"docs/**", "docs/a/b/c.md", true},
		{"docs/**", "docs", true},
		{"docs/**", "docsx/a.md", false},
		{"docs/**", "x/docs/a.md", false},
		{"docs/*", "docs/a.md", true},
		{"docs/*", "docs/a/b.md", false},
		{"docs/*", "docs", false},
		{"*.md", "README.md", true},
		{"*.md", "docs/README.md", false},
		{"**/*.md", "README.md", true},
		{"**/*.md", "a/b/README.md", true},
		{"**/*.md", "a/b/README.txt", false},
		{"a/**/b", "a/b", true},
		{"a/**/b", "a/x/y/b", true},
		{"a/**/b", "a/x/y/c", false},
		{"**", "any/thing/at/all", true},
		{"**", "x", true},
		{"a*b", "ab", true},
		{"a*b", "axxb", true},
		{"a*b", "a/b", false},
		{"a*b*c", "abc", true},
		{"a*b*c", "aXbYc", true},
		{"a*b*c", "aXbY", false},
		{"README.md", "README.md", true},
		{"README.md", "readme.md", false},
		{"a*", "a", true},
		{"a*", "abc", true},
		{"README*", "README", true},
		{"*a", "a", true},
		{"a**b", "ab", true},
		{"docs/a*", "docs/a", true},
		{"docs/a*", "docs/ba", false},
		{"docs/**", "Docs/a.md", false},
		{"*", "-rf", true},
		{"**", ".stw/release.toml", true},
		{"docs/**/**/x", "docs/x", true},
		{"**/**", "a/b", true},
	}
	for _, c := range cases {
		if got := MatchPattern(c.pattern, c.path); got != c.want {
			t.Errorf("MatchPattern(%q, %q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

func TestRepositoryPathValidity(t *testing.T) {
	good := []string{"a", "docs/a.md", "-leading-dash", "A.MD", ".github/x", "a b", "é", strings.Repeat("a", 255), strings.Repeat("a", 4096)}
	for _, p := range good {
		if !ValidRepoPath(p) {
			t.Errorf("%q rejected", p)
		}
	}
	bad := []string{"", ".", "..", "a/..", "../a", "a/../b", "/abs", "a//b", "a/", "a\nb", "a\x00b", "a\rb", "a\tb", "a\\b", "\xff", "a/\xfe", ".git/config", "a/.GIT/x", "a/.Git", "a\x7fb", strings.Repeat("a/", 2999) + "a", strings.Repeat("a", 4097)}
	for _, p := range bad {
		if ValidRepoPath(p) {
			t.Errorf("%q accepted", p)
		}
	}
}

// AllowedPath fails closed: a hostile name is never allowlisted, whatever the pattern.
func TestAllowedPathRefusesHostileNames(t *testing.T) {
	for _, p := range []string{"a\nb", "a\x00b", "\xff.md", "../x", "a/../b", "/abs", ".git/hooks/x", "a//b", "a\\b"} {
		if AllowedPath("**", p) {
			t.Errorf("** allowed %q", p)
		}
	}
	if !AllowedPath("**", "-rf") || !AllowedPath("docs/*", "docs/-x") {
		t.Error("a leading dash is an ordinary name")
	}
	if AllowedPath("docs/readme.md", "docs/README.md") {
		t.Error("matching is case-sensitive for allowlisting")
	}
}

func TestPatternOverlapsADirectory(t *testing.T) {
	cases := []struct {
		pattern, dir string
		want         bool
	}{
		{"**", ".stw", true},
		{".stw/**", ".stw", true},
		{".stw/*", ".stw", true},
		{".stw/release.toml", ".stw", true},
		{"docs/**", ".stw", false},
		{"*/**", ".github", true},
		{"*", ".github", true},
		{"**/x", ".github", true},
		{".github/workflows/*.yml", ".github", true},
		{"docs/a.md", "docs", true},
		{"docs/a.md", "docs/a", false},
		{"docsx/**", "docs", false},
		{"a/b/**", "a/b/c", true},
		{"a/b/c/d", "a/b/c", true},
		{"a/b", "a/b/c", false},
		{"a/**/z", "a/b/c", true},
		{".STW/**", ".stw", true},
		{".stw/**", ".STW", true},
	}
	for _, c := range cases {
		if got := PatternOverlapsDirectory(c.pattern, c.dir); got != c.want {
			t.Errorf("PatternOverlapsDirectory(%q, %q) = %v, want %v", c.pattern, c.dir, got, c.want)
		}
	}
}

func TestPatternOverlapsAFile(t *testing.T) {
	cases := []struct {
		pattern, file string
		want          bool
	}{
		{"scripts/**", "scripts/release-package.sh", true},
		{"scripts/*.sh", "scripts/release-package.sh", true},
		{"scripts/*.py", "scripts/release-package.sh", false},
		{"SCRIPTS/**", "scripts/x.sh", true},
		{"docs/**", "scripts/x.sh", false},
		{"web/mix.exs", "web/mix.exs", true},
		{"web/**", "web/mix.exs", true},
		{"**/mix.exs", "web/mix.exs", true},
	}
	for _, c := range cases {
		if got := PatternOverlapsFile(c.pattern, c.file); got != c.want {
			t.Errorf("PatternOverlapsFile(%q, %q) = %v, want %v", c.pattern, c.file, got, c.want)
		}
	}
}

// The matcher agrees with a regular-expression translation of the same rules.
func referenceMatch(pattern, path string) bool {
	var b strings.Builder
	b.WriteString("^")
	for _, segment := range strings.Split(pattern, "/") {
		if segment == "**" {
			b.WriteString("(?:/[^/]+)*")
			continue
		}
		b.WriteString("/")
		for _, r := range segment {
			if r == '*' {
				b.WriteString("[^/]*")
			} else {
				b.WriteString(regexp.QuoteMeta(string(r)))
			}
		}
	}
	b.WriteString("$")
	return regexp.MustCompile(b.String()).MatchString("/" + path)
}

func FuzzMatchPattern(f *testing.F) {
	f.Add("docs/**", "docs/a/b.md")
	f.Add("a*b*c", "aXbYc")
	f.Add("**/*.md", "x/y.md")
	f.Add("**", "")
	f.Fuzz(func(t *testing.T, pattern, path string) {
		if PatternProblem(pattern) != "" || !ValidRepoPath(path) {
			// Arbitrary input must still not panic.
			MatchPattern(pattern, path)
			PatternOverlapsDirectory(pattern, path)
			return
		}
		got, want := MatchPattern(pattern, path), referenceMatch(pattern, path)
		if got != want {
			t.Fatalf("MatchPattern(%q, %q) = %v, reference %v", pattern, path, got, want)
		}
		if got && !PatternOverlapsFile(pattern, path) {
			t.Fatalf("a matching pattern %q must overlap its file %q", pattern, path)
		}
	})
}

func TestMatcherAgreesWithTheReferenceOnTheTable(t *testing.T) {
	patterns := []string{"docs/**", "*.md", "**/*.md", "a/**/b", "**", "a*b*c", "docs/*", "a/**/**/x", "**/**"}
	paths := []string{"docs", "docs/a.md", "a/b", "a/x/y/b", "x", "README.md", "a/b/README.md", "aXbYc", "a/x", "x/x"}
	for _, p := range patterns {
		for _, path := range paths {
			if got, want := MatchPattern(p, path), referenceMatch(p, path); got != want {
				t.Errorf("%q vs %q: %v, reference %v", p, path, got, want)
			}
		}
	}
}
