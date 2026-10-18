// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// VersionTuple is a scheme's fixed-length numeric order. An absent group is -1.
// Values are at most nine decimal digits, including on 32-bit hosts.
type VersionTuple []int

// VersionScheme is an immutable, validated tag grammar and ordering policy.
// CompileVersionScheme accepts version settings directly; contract loading also
// validates the complete configuration schema and the source-file settings.
type VersionScheme struct {
	kind, prefix, suffix, monotonicity string
	re                                 *regexp.Regexp
	versionGroup                       int
	orderGroups                        []int
}

// CompileVersionScheme compiles the version and tag operations. Invalid pattern
// declarations use the same tree checks as the scheme-invalid contract findings.
func CompileVersionScheme(v Version) (*VersionScheme, error) {
	if findings := checkVersion(v); len(findings) > 0 {
		return nil, fmt.Errorf("scheme-invalid: %s: %s", findings[0].Pointer, findings[0].Message)
	}
	s := &VersionScheme{kind: v.Scheme, prefix: v.TagPrefix, monotonicity: "strict"}
	expression := ""
	switch v.Scheme {
	case "semver", "calver":
		if v.Monotonicity != "" || v.TagPattern != "" || v.TagTemplate != "" || len(v.Order) > 0 {
			return nil, fmt.Errorf("scheme-invalid: pattern settings require the pattern scheme")
		}
		if !regexp.MustCompile(`\A(?:[a-z0-9._/-]*/)?\z`).MatchString(v.TagPrefix) || !prefixSegmentsOK(v.TagPrefix) || !refTextOK(v.TagPrefix, "") {
			return nil, fmt.Errorf("scheme-invalid: invalid tag_prefix")
		}
		body := semverBody
		s.orderGroups = []int{1, 2, 3}
		if v.Scheme == "calver" {
			body = calverBody
			s.orderGroups = []int{1, 2, 3, 4, 5}
		}
		expression = `\A` + regexp.QuoteMeta(s.prefix) + `(?P<version>` + body + `)\z`
		s.versionGroup = 1
		// The enclosing version capture shifts every built-in numeric group.
		for i := range s.orderGroups {
			s.orderGroups[i]++
		}
	case "pattern":
		prefix, suffix, ok := splitTemplate(v.TagTemplate)
		if !ok || v.TagPattern == "" || len(v.Order) < 1 || len(v.Order) > 8 || v.TagPrefix != "" {
			return nil, fmt.Errorf("scheme-invalid: pattern requires tag_template, tag_pattern and 1 to 8 order groups")
		}
		if v.Monotonicity != "" && v.Monotonicity != "strict" && v.Monotonicity != "non-decreasing" {
			return nil, fmt.Errorf("scheme-invalid: unknown monotonicity")
		}
		if v.Monotonicity != "" {
			s.monotonicity = v.Monotonicity
		}
		s.prefix, s.suffix = prefix, suffix
		expression = v.TagPattern
	default:
		return nil, fmt.Errorf("scheme-invalid: unknown version scheme")
	}
	re, err := regexp.Compile(expression)
	if err != nil {
		return nil, fmt.Errorf("scheme-invalid: %w", err)
	}
	s.re = re
	if v.Scheme == "pattern" {
		s.versionGroup = re.SubexpIndex("version")
		for _, name := range v.Order {
			s.orderGroups = append(s.orderGroups, re.SubexpIndex(name))
		}
	}
	return s, nil
}

// Valid reports whether the complete version belongs to this scheme.
func (s *VersionScheme) Valid(version string) bool {
	if s == nil {
		return false
	}
	parsed, _, ok := s.ParseTag(s.prefix + version + s.suffix)
	return ok && parsed == version
}

// Tag derives a tag without normalizing the version's spelling.
func (s *VersionScheme) Tag(version string) (string, error) {
	if !s.Valid(version) {
		return "", fmt.Errorf("invalid release version")
	}
	return s.prefix + version + s.suffix, nil
}

// ParseTag parses a complete tag and checks the exact tag/version round trip.
// Calendar validation is exclusive to calver; pattern products apply only their
// declared expression, even when their order groups happen to name a date.
func (s *VersionScheme) ParseTag(tag string) (version string, tuple VersionTuple, ok bool) {
	if s == nil || s.re == nil {
		return "", nil, false
	}
	indices := s.re.FindStringSubmatchIndex(tag)
	if len(indices) == 0 || indices[0] != 0 || indices[1] != len(tag) {
		return "", nil, false
	}
	start, end := indices[2*s.versionGroup], indices[2*s.versionGroup+1]
	if start < 0 {
		return "", nil, false
	}
	version = tag[start:end]
	if tag != s.prefix+version+s.suffix {
		return "", nil, false
	}
	if s.kind == "calver" {
		date := strings.SplitN(version, "-", 2)[0]
		if _, err := time.Parse("2006.01.02", date); err != nil {
			return "", nil, false
		}
	}
	tuple = make(VersionTuple, len(s.orderGroups))
	for i, group := range s.orderGroups {
		start, end := indices[2*group], indices[2*group+1]
		tuple[i] = -1
		if start < 0 {
			continue
		}
		digits := tag[start:end]
		if len(digits) < 1 || len(digits) > maxOrderDigits {
			return "", nil, false
		}
		value, err := strconv.Atoi(digits)
		if err != nil {
			return "", nil, false
		}
		tuple[i] = value
	}
	return version, tuple, true
}

// CompareVersionTuples returns -1, 0 or 1 in numeric lexicographic order.
// Tuples from one scheme always have the same length.
func CompareVersionTuples(a, b VersionTuple) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return 0
}

// MonotonicityResult separates the ordering decision from full tag existence.
// Callers combine names from tags and releases (published and visible drafts).
// They must separately enforce release-exists, even for an equal tuple accepted
// by non-decreasing. Matched and Ignored count supplied names, including repeats.
type MonotonicityResult struct {
	Monotonic bool
	Exists    bool
	Matched   int
	Ignored   int
	Maximum   VersionTuple
}

// CheckMonotonicity checks the candidate against all matching names, excluding
// subjectTag only from ordering. The subject still contributes to Exists and
// Matched. Under non-decreasing, the order within an equal tuple is undefined:
// consumers must not infer recency from counters omitted from order.
func (s *VersionScheme) CheckMonotonicity(version string, names []string, subjectTag string) (MonotonicityResult, error) {
	var result MonotonicityResult
	tag, err := s.Tag(version)
	if err != nil {
		return result, err
	}
	_, candidate, _ := s.ParseTag(tag)
	for _, name := range names {
		_, tuple, ok := s.ParseTag(name)
		if !ok {
			result.Ignored++
			continue
		}
		result.Matched++
		if name == tag {
			result.Exists = true
		}
		if name == subjectTag {
			continue
		}
		if result.Maximum == nil || CompareVersionTuples(tuple, result.Maximum) > 0 {
			result.Maximum = tuple
		}
	}
	result.Monotonic = true
	if result.Maximum != nil {
		comparison := CompareVersionTuples(candidate, result.Maximum)
		result.Monotonic = comparison > 0 || comparison == 0 && s.monotonicity == "non-decreasing"
	}
	return result, nil
}
