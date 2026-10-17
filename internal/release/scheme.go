// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"fmt"
	"regexp"
	"regexp/syntax"
	"unicode/utf8"
)

// maxVersionLength bounds the version group of a pattern product (spec version table).
const maxVersionLength = 64

// maxOrderDigits bounds a numeric order group; nine digits keep a tuple comparison inside an int.
const maxOrderDigits = 9

// checkVersion returns the scheme-invalid findings of the version table.
func checkVersion(v Version) []Finding {
	var out []Finding
	invalid := func(pointer, format string, args ...any) {
		out = append(out, errorFinding("scheme-invalid", pointer, fmt.Sprintf(format, args...)))
	}
	if v.Source == "file" && v.FilePattern != "" {
		re, err := regexp.Compile(v.FilePattern)
		switch {
		case err != nil:
			invalid("/version/file_pattern", "file_pattern does not compile as an RE2 expression: %s", clip(err.Error()))
		case re.NumSubexp() != 1:
			invalid("/version/file_pattern", "file_pattern must have exactly one capture group, has %d", re.NumSubexp())
		}
	}
	if v.Scheme != "pattern" {
		return out
	}
	prefix, suffix, templateOK := splitTemplate(v.TagTemplate)
	if v.TagTemplate != "" && !templateOK {
		invalid("/version/tag_template", "tag_template must contain {version} exactly once and no other brace")
	}
	if v.TagPattern == "" {
		return out
	}
	if _, err := regexp.Compile(v.TagPattern); err != nil {
		invalid("/version/tag_pattern", "tag_pattern does not compile as an RE2 expression: %s", clip(err.Error()))
		return out
	}
	tree, err := syntax.Parse(v.TagPattern, syntax.Perl)
	if err != nil {
		invalid("/version/tag_pattern", "tag_pattern does not parse as an RE2 expression: %s", clip(err.Error()))
		return out
	}
	if name := duplicateCaptureName(tree, map[string]bool{}); name != "" {
		invalid("/version/tag_pattern", "the capture group name %q is used more than once; group names must be unique", clip(name))
		return out
	}
	group, litPrefix, litSuffix, problem := splitTagPattern(tree)
	if problem != "" {
		invalid("/version/tag_pattern", "%s", problem)
		return out
	}
	if templateOK && (litPrefix != prefix || litSuffix != suffix) {
		invalid("/version/tag_pattern", "the literal prefix and suffix of tag_pattern must equal those of tag_template (%q and %q)", clip(prefix), clip(suffix))
	}
	if problem := versionGroupProblem(group); problem != "" {
		invalid("/version/tag_pattern", "%s", problem)
	}
	seen := map[string]bool{}
	for i, name := range v.Order {
		pointer := fmt.Sprintf("/version/order/%d", i)
		switch {
		case seen[name]:
			invalid(pointer, "order group %q is listed twice", clip(name))
		case !seenGroup(group, name):
			invalid(pointer, "order names %q, which is not a named group inside the version group", clip(name))
		default:
			if problem := orderGroupProblem(namedGroup(group, name)); problem != "" {
				invalid(pointer, "order group %q %s", clip(name), problem)
			}
		}
		seen[name] = true
	}
	return out
}

// duplicateCaptureName returns a capture group name that occurs twice (Go accepts that).
func duplicateCaptureName(re *syntax.Regexp, seen map[string]bool) string {
	if re.Op == syntax.OpCapture && re.Name != "" {
		if seen[re.Name] {
			return re.Name
		}
		seen[re.Name] = true
	}
	for _, sub := range re.Sub {
		if name := duplicateCaptureName(sub, seen); name != "" {
			return name
		}
	}
	return ""
}

func seenGroup(group *syntax.Regexp, name string) bool { return namedGroup(group, name) != nil }

// splitTagPattern requires ^ literal (?P<version>...) literal $ and returns the version capture
// and the literal text around it. The literal parts are checked on the parsed syntax tree, so an
// unescaped metacharacter is not literal.
func splitTagPattern(tree *syntax.Regexp) (group *syntax.Regexp, prefix, suffix, problem string) {
	const shape = "tag_pattern must have the form ^<literal prefix>(?P<version>...)<literal suffix>$"
	if tree.Op != syntax.OpConcat || len(tree.Sub) < 3 {
		return nil, "", "", shape
	}
	parts := tree.Sub
	if parts[0].Op != syntax.OpBeginText || parts[len(parts)-1].Op != syntax.OpEndText {
		return nil, "", "", shape
	}
	var current *string = &prefix
	for _, part := range parts[1 : len(parts)-1] {
		switch part.Op {
		case syntax.OpLiteral:
			if part.Flags&syntax.FoldCase != 0 {
				return nil, "", "", "case-insensitive matching is not allowed in tag_pattern"
			}
			for _, r := range part.Rune {
				if !literalRuneOK(r) {
					return nil, "", "", "the literal prefix and suffix of tag_pattern may use only A-Z, a-z, 0-9, . _ / and -"
				}
			}
			*current += string(part.Rune)
		case syntax.OpCapture:
			if part.Name != "version" {
				return nil, "", "", "tag_pattern must have exactly one top-level capture group, named version"
			}
			group = part
			current = &suffix
		default:
			return nil, "", "", "the prefix and suffix of tag_pattern must be literal text; escape metacharacters such as the dot"
		}
	}
	if group == nil {
		return nil, "", "", "tag_pattern has no top-level (?P<version>...) group"
	}
	return group, prefix, suffix, ""
}

func literalRuneOK(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '/' || r == '-'
}

func versionRuneOK(r rune) bool { return literalRuneOK(r) && r != '/' }

func namedGroup(re *syntax.Regexp, name string) *syntax.Regexp {
	for _, sub := range re.Sub {
		if sub.Op == syntax.OpCapture && sub.Name == name {
			return sub
		}
		if found := namedGroup(sub, name); found != nil {
			return found
		}
	}
	return nil
}

func versionGroupProblem(group *syntax.Regexp) string {
	min, max := lengthBounds(group)
	switch {
	case !runesWithin(group, versionRuneOK):
		return "the version group may match characters outside A-Z, a-z, 0-9, . _ and -"
	case min < 1:
		return "the version group may match the empty string"
	case max < 0 || max > maxVersionLength:
		return fmt.Sprintf("the version group may match more than %d characters", maxVersionLength)
	}
	return ""
}

func orderGroupProblem(group *syntax.Regexp) string {
	min, max := lengthBounds(group)
	switch {
	case !runesWithin(group, func(r rune) bool { return r >= '0' && r <= '9' }):
		return "may match something other than decimal digits"
	case min < 1:
		return "may match the empty string"
	case max < 0 || max > maxOrderDigits:
		return fmt.Sprintf("may match more than %d digits", maxOrderDigits)
	}
	return ""
}

// runesWithin reports whether every string the expression can match consists of allowed runes.
func runesWithin(re *syntax.Regexp, allowed func(rune) bool) bool {
	switch re.Op {
	case syntax.OpLiteral:
		if re.Flags&syntax.FoldCase != 0 {
			return false
		}
		for _, r := range re.Rune {
			if !allowed(r) {
				return false
			}
		}
		return true
	case syntax.OpCharClass:
		for i := 0; i+1 < len(re.Rune); i += 2 {
			// The loop stops at the first disallowed rune, which is within a few hundred steps
			// for the ASCII sets allowed here.
			for r := re.Rune[i]; r <= re.Rune[i+1]; r++ {
				if !allowed(r) {
					return false
				}
			}
		}
		return true
	case syntax.OpAnyChar, syntax.OpAnyCharNotNL, syntax.OpNoMatch:
		return re.Op == syntax.OpNoMatch
	}
	for _, sub := range re.Sub {
		if !runesWithin(sub, allowed) {
			return false
		}
	}
	return true
}

// lengthBounds returns the shortest and longest match length in characters; max is -1 when unbounded.
func lengthBounds(re *syntax.Regexp) (min, max int) {
	add := func(a, b int) int {
		if a < 0 || b < 0 {
			return -1
		}
		return a + b
	}
	switch re.Op {
	case syntax.OpLiteral:
		n := utf8.RuneCountInString(string(re.Rune))
		return n, n
	case syntax.OpCharClass, syntax.OpAnyChar, syntax.OpAnyCharNotNL:
		return 1, 1
	case syntax.OpCapture:
		return lengthBounds(re.Sub[0])
	case syntax.OpConcat:
		for _, sub := range re.Sub {
			lo, hi := lengthBounds(sub)
			min, max = min+lo, add(max, hi)
		}
		return min, max
	case syntax.OpAlternate:
		min, max = -2, 0
		for _, sub := range re.Sub {
			lo, hi := lengthBounds(sub)
			if min == -2 || lo < min {
				min = lo
			}
			if max >= 0 && (hi < 0 || hi > max) {
				max = hi
			}
		}
		if min == -2 {
			min = 0
		}
		return min, max
	case syntax.OpStar, syntax.OpPlus, syntax.OpQuest, syntax.OpRepeat:
		lo, hi := lengthBounds(re.Sub[0])
		repeatMin, repeatMax := 0, -1
		switch re.Op {
		case syntax.OpPlus:
			repeatMin = 1
		case syntax.OpQuest:
			repeatMax = 1
		case syntax.OpRepeat:
			repeatMin, repeatMax = re.Min, re.Max
		}
		max = -1
		if hi == 0 {
			max = 0
		} else if hi > 0 && repeatMax >= 0 {
			max = hi * repeatMax
		}
		return lo * repeatMin, max
	}
	return 0, 0
}
