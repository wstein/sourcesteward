// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"sort"
	"strconv"
	"strings"
)

// DefaultReleaseTagPattern is the release tag pattern of a repository that declares no product.
const DefaultReleaseTagPattern = "v*.*.*"

// MaxUnreleasedTags is the number of unreleased tags status reports per repository.
const MaxUnreleasedTags = 5

// TagFindings is what the tags and releases of a repository say about the release state.
// Latest holds the newest tag of each pattern that has one (the tags whose release workflow run
// status reads), Unreleased the tags that wait for a release, newest first.
type TagFindings struct {
	Latest     []StatusTag
	Unreleased []StatusTag
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
// without release are history and never listed; the result has at most MaxUnreleasedTags entries,
// taken pattern by pattern in the order given, each newest first.
func FindTags(tags []StatusTag, releases []StatusRelease, patterns []string) TagFindings {
	if len(patterns) == 0 {
		patterns = []string{DefaultReleaseTagPattern}
	}
	carried := map[string]bool{}
	for _, rel := range releases {
		carried[rel.Tag] = true
	}
	var out TagFindings
	seenLatest, seenUnreleased := map[string]bool{}, map[string]bool{}
	for _, pattern := range patterns {
		matched := versioned(pattern, tags)
		if len(matched) == 0 {
			continue
		}
		sort.SliceStable(matched, func(i, j int) bool { return newer(matched[i], matched[j]) })
		if latest := matched[0].StatusTag; !seenLatest[latest.Name] {
			seenLatest[latest.Name] = true
			out.Latest = append(out.Latest, latest)
		}
		candidates := matched[:1]
		if published := versioned(pattern, publishedTags(releases)); len(published) > 0 {
			floor := published[0]
			for _, p := range published[1:] {
				if p.version.compare(floor.version) > 0 {
					floor = p
				}
			}
			candidates = nil
			for _, c := range matched {
				if c.version.compare(floor.version) > 0 {
					candidates = append(candidates, c)
				}
			}
		}
		for _, c := range candidates {
			if len(out.Unreleased) == MaxUnreleasedTags {
				break
			}
			if !carried[c.Name] && !seenUnreleased[c.Name] {
				seenUnreleased[c.Name] = true
				out.Unreleased = append(out.Unreleased, c.StatusTag)
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
// matches nothing: it names no version.
func patternVersion(pattern, name string) (semver, bool) {
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
	s, _, _ = strings.Cut(s, "+")
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
		v.pre = strings.Split(pre, ".")
		for _, id := range v.pre {
			if !validPreIdentifier(id) {
				return semver{}, false
			}
		}
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

func validPreIdentifier(id string) bool {
	if id == "" || strings.Trim(id, "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz-") != "" {
		return false
	}
	if strings.Trim(id, "0123456789") == "" {
		_, ok := parseNumeric(id)
		return ok
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
