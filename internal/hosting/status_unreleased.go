// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"sort"
	"strconv"
	"strings"
)

// DefaultReleaseTagPattern is the release tag pattern of a repository that declares no product: a
// semantic version with an optional leading `v`, so that 1.2.3 and v1.2.3 are one version scheme.
const DefaultReleaseTagPattern = "v*.*.*"

// MaxUnreleasedTags is the number of unreleased tags status reports per repository.
const MaxUnreleasedTags = 5

// PatternTag is one tag as one release tag pattern sees it: Latest when it has the highest version
// of the pattern, Unreleased when it waits for a release. A tag that two patterns match has one
// entry per pattern.
type PatternTag struct {
	Pattern string
	StatusTag
	Latest     bool
	Unreleased bool
}

// FindTags applies the unreleased-tag rule of Spec 037 to the tags and the releases (drafts and
// published ones) of one repository. patterns are the release tag patterns of the products the
// repository declares, chosen by the caller; none means DefaultReleaseTagPattern. Per pattern:
//
//   - a tag belongs to the pattern when it matches the glob (`*` matches any text) and the text
//     between the pattern's literal head and tail is a semantic version; any other tag is ignored,
//     and only the version decides what is newer, never the name or a date;
//   - the latest tag is the one with the highest version;
//   - a tag is unreleased when its version is higher than that of the latest published release of
//     the pattern (a draft is no published release; with none published only the latest tag is a
//     candidate) and no release, draft or published, carries its name.
//
// Prerelease versions follow semantic versioning: 1.0.0-rc.1 is lower than 1.0.0. Older tags
// without release are history and never listed. The result has one entry per pattern and tag that
// is latest or unreleased (a tag that is both has one entry with both flags), the patterns in the
// order given and each newest first. At most MaxUnreleasedTags entries are unreleased, filled in
// that order; the latest entries are not capped.
func FindTags(tags []StatusTag, releases []StatusRelease, patterns []string) []PatternTag {
	if len(patterns) == 0 {
		patterns = []string{DefaultReleaseTagPattern}
	}
	carried := map[string]bool{}
	for _, rel := range releases {
		carried[rel.Tag] = true
	}
	var out []PatternTag
	seen := map[string]bool{}
	unreleased := 0
	for _, pattern := range patterns {
		if seen[pattern] {
			continue
		}
		seen[pattern] = true
		matched := versioned(pattern, tags)
		sort.SliceStable(matched, func(i, j int) bool { return newer(matched[i], matched[j]) })
		var floor *semver
		for _, p := range versioned(pattern, publishedTags(releases)) {
			if floor == nil || p.version.compare(*floor) > 0 {
				v := p.version
				floor = &v
			}
		}
		for i, t := range matched {
			newerThanRelease := i == 0 // without a published release only the latest tag is a candidate
			if floor != nil {
				newerThanRelease = t.version.compare(*floor) > 0
			}
			entry := PatternTag{Pattern: pattern, StatusTag: t.StatusTag, Latest: i == 0}
			if newerThanRelease && !carried[t.Name] && unreleased < MaxUnreleasedTags {
				entry.Unreleased = true
				unreleased++
			}
			if entry.Latest || entry.Unreleased {
				out = append(out, entry)
			}
		}
	}
	return out
}

// publishedTags lists the tags of the published releases.
func publishedTags(releases []StatusRelease) []StatusTag {
	var out []StatusTag
	for _, rel := range releases {
		if !rel.Draft {
			out = append(out, StatusTag{Name: rel.Tag})
		}
	}
	return out
}

// versionedTag is a tag that belongs to a pattern, with its parsed version.
type versionedTag struct {
	StatusTag
	version semver
}

// versioned returns the tags of the list that belong to the pattern.
func versioned(pattern string, tags []StatusTag) []versionedTag {
	var out []versionedTag
	for _, t := range tags {
		if v, ok := patternVersion(pattern, t.Name); ok {
			out = append(out, versionedTag{t, v})
		}
	}
	return out
}

// newer orders by version, highest first, and by name where versions are equal (build metadata
// does not count), so that the result never depends on the order of the input.
func newer(a, b versionedTag) bool {
	if c := a.version.compare(b.version); c != 0 {
		return c > 0
	}
	return a.Name < b.Name
}

// patternVersion reports whether name matches the glob pattern and returns the version between the
// literal text before the first `*` and the literal text after the last one. A pattern without `*`
// matches nothing: it names no version. The default pattern is the exception that gives it its
// meaning: the version with or without one leading `v`.
func patternVersion(pattern, name string) (semver, bool) {
	if pattern == DefaultReleaseTagPattern {
		return parseSemver(strings.TrimPrefix(name, "v"))
	}
	parts := strings.Split(pattern, "*")
	if len(parts) < 2 {
		return semver{}, false
	}
	head, tail := parts[0], parts[len(parts)-1]
	if len(name) < len(head)+len(tail) || !strings.HasPrefix(name, head) || !strings.HasSuffix(name, tail) {
		return semver{}, false
	}
	middle := name[len(head) : len(name)-len(tail)]
	rest := middle
	for _, literal := range parts[1 : len(parts)-1] {
		i := strings.Index(rest, literal)
		if i < 0 {
			return semver{}, false
		}
		rest = rest[i+len(literal):]
	}
	return parseSemver(middle)
}

// semver is a parsed semantic version 2.0.0 without its build metadata.
type semver struct {
	major, minor, patch uint64
	pre                 []string
}

func parseSemver(s string) (semver, bool) {
	s, build, hasBuild := strings.Cut(s, "+")
	if hasBuild && !validIdentifiers(build, false) {
		return semver{}, false
	}
	core, pre, hasPre := strings.Cut(s, "-")
	nums := strings.Split(core, ".")
	if len(nums) != 3 {
		return semver{}, false
	}
	var v semver
	for i, field := range []*uint64{&v.major, &v.minor, &v.patch} {
		n, ok := parseNumeric(nums[i])
		if !ok {
			return semver{}, false
		}
		*field = n
	}
	if hasPre {
		if !validIdentifiers(pre, true) {
			return semver{}, false
		}
		v.pre = strings.Split(pre, ".")
	}
	return v, true
}

// parseNumeric accepts digits without a leading zero.
func parseNumeric(s string) (uint64, bool) {
	if s == "" || (len(s) > 1 && s[0] == '0') || strings.Trim(s, "0123456789") != "" {
		return 0, false
	}
	n, err := strconv.ParseUint(s, 10, 64)
	return n, err == nil
}

// validIdentifiers reports whether s is a non-empty, dot-separated list of identifiers of
// [0-9A-Za-z-]; a numeric prerelease identifier has no leading zero.
func validIdentifiers(s string, prerelease bool) bool {
	for _, id := range strings.Split(s, ".") {
		if id == "" || strings.Trim(id, "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz-") != "" {
			return false
		}
		if _, numeric := parseNumeric(id); prerelease && !numeric && strings.Trim(id, "0123456789") == "" {
			return false
		}
	}
	return true
}

// compare is the precedence of semantic versioning: -1, 0 or 1.
func (a semver) compare(b semver) int {
	for _, p := range [][2]uint64{{a.major, b.major}, {a.minor, b.minor}, {a.patch, b.patch}} {
		if p[0] != p[1] {
			if p[0] < p[1] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(a.pre) == 0 && len(b.pre) == 0:
		return 0
	case len(a.pre) == 0:
		return 1
	case len(b.pre) == 0:
		return -1
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		if c := comparePre(a.pre[i], b.pre[i]); c != 0 {
			return c
		}
	}
	switch {
	case len(a.pre) < len(b.pre):
		return -1
	case len(a.pre) > len(b.pre):
		return 1
	}
	return 0
}

func comparePre(a, b string) int {
	an, aNum := parseNumeric(a)
	bn, bNum := parseNumeric(b)
	switch {
	case aNum && bNum:
		if an < bn {
			return -1
		} else if an > bn {
			return 1
		}
		return 0
	case aNum:
		return -1
	case bNum:
		return 1
	}
	return strings.Compare(a, b)
}
