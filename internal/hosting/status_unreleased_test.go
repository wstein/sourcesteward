// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func tagsOf(names ...string) []StatusTag {
	out := make([]StatusTag, len(names))
	for i, n := range names {
		out[i] = StatusTag{Name: n, SHA: strings.Repeat(fmt.Sprintf("%x", i%16), 40)}
	}
	return out
}

func names(tags []StatusTag) []string {
	out := []string{}
	for _, t := range tags {
		out = append(out, t.Name)
	}
	return out
}

func published(tags ...string) []StatusRelease {
	var out []StatusRelease
	for _, t := range tags {
		out = append(out, StatusRelease{Tag: t})
	}
	return out
}

func drafts(tags ...string) []StatusRelease {
	var out []StatusRelease
	for _, t := range tags {
		out = append(out, StatusRelease{Tag: t, Draft: true})
	}
	return out
}

func TestFindTagsUnreleasedRule(t *testing.T) {
	for _, tc := range []struct {
		name       string
		tags       []string
		releases   []StatusRelease
		patterns   []string
		unreleased []string
		latest     []string
	}{
		{"newer than the latest release only", []string{"v1.0.0", "v1.1.0", "v1.2.0", "v1.3.0"}, published("v1.1.0", "v1.0.0"), nil, []string{"v1.3.0", "v1.2.0"}, []string{"v1.3.0"}},
		{"older tags without release are history", []string{"v0.9.0", "v1.0.0"}, published("v1.0.0"), nil, nil, []string{"v1.0.0"}},
		{"nothing newer", []string{"v1.0.0"}, published("v1.0.0"), nil, nil, []string{"v1.0.0"}},
		{"no release at all: only the newest tag", []string{"v1.0.0", "v1.1.0", "v0.1.0"}, nil, nil, []string{"v1.1.0"}, []string{"v1.1.0"}},
		{"a draft carries the tag", []string{"v1.0.0", "v1.1.0"}, append(published("v1.0.0"), drafts("v1.1.0")...), nil, nil, []string{"v1.1.0"}},
		{"a draft is no published release", []string{"v1.0.0", "v1.1.0"}, drafts("v1.1.0"), nil, nil, []string{"v1.1.0"}},
		{"the floor is the highest release whatever their order", []string{"v1.0.0", "v1.1.0", "v1.2.0"}, published("v1.0.0", "v1.1.0"), nil, []string{"v1.2.0"}, []string{"v1.2.0"}},
		{"a draft does not raise the floor", []string{"v1.0.0", "v1.1.0", "v1.2.0"}, append(published("v1.0.0"), drafts("v1.2.0")...), nil, []string{"v1.1.0"}, []string{"v1.2.0"}},
		{"version order, not text order", []string{"v1.9.0", "v1.10.0", "v1.2.0"}, published("v1.9.0"), nil, []string{"v1.10.0"}, []string{"v1.10.0"}},
		{"tag order and names do not matter", []string{"v1.3.0", "v1.1.0", "v1.2.0"}, published("v1.1.0"), nil, []string{"v1.3.0", "v1.2.0"}, []string{"v1.3.0"}},
		{"prerelease after the release is newer", []string{"v1.0.0", "v1.1.0-rc.1"}, published("v1.0.0"), nil, []string{"v1.1.0-rc.1"}, []string{"v1.1.0-rc.1"}},
		{"prerelease of the released version is older", []string{"v1.0.0", "v1.0.0-rc.1"}, published("v1.0.0"), nil, nil, []string{"v1.0.0"}},
		{"prerelease order", []string{"v2.0.0-alpha", "v2.0.0-alpha.1", "v2.0.0-beta.2", "v2.0.0-beta.11", "v2.0.0-rc.1", "v2.0.0"}, published("v1.0.0"), nil, []string{"v2.0.0", "v2.0.0-rc.1", "v2.0.0-beta.11", "v2.0.0-beta.2", "v2.0.0-alpha.1"}, []string{"v2.0.0"}},
		{"non-semver tags are ignored", []string{"v1.0.0", "v1.1", "v1.1.x", "vnext", "v01.2.3", "v1.2.3.4", "latest", "v1.0.0-", "v1.0.0-01"}, published("v1.0.0"), nil, nil, []string{"v1.0.0"}},
		{"build metadata is ignored for the order", []string{"v1.0.0+a", "v1.0.0"}, published("v1.0.0"), nil, nil, []string{"v1.0.0"}},
		{"the v is optional in the default pattern", []string{"1.0.0", "1.1.0", "v1.2.0"}, published("1.0.0"), nil, []string{"v1.2.0", "1.1.0"}, []string{"v1.2.0"}},
		{"one floor for tags with and without v", []string{"1.0.0", "v1.0.0", "v0.9.0", "v1.1.0"}, published("v1.0.0"), nil, []string{"v1.1.0"}, []string{"v1.1.0"}},
		{"a release without v is the floor of a tag with v", []string{"v1.0.0", "v1.1.0"}, published("1.1.0"), nil, nil, []string{"v1.1.0"}},
		{"the same version with and without v: both listed, by name", []string{"v1.1.0", "1.1.0"}, published("v1.0.0"), nil, []string{"1.1.0", "v1.1.0"}, []string{"1.1.0"}},
		{"only one v is optional", []string{"vv1.0.0", "V1.0.0", "v-1.0.0"}, nil, nil, nil, nil},
		{"build metadata must be well formed", []string{"v1.0.0+", "v1.0.0+a..b", "v1.0.0+a_b", "v1.0.0+.a", "v1.0.1+a.b-c.01"}, published("v1.0.0"), nil, []string{"v1.0.1+a.b-c.01"}, []string{"v1.0.1+a.b-c.01"}},
		{"no tags", nil, published("v1.0.0"), nil, nil, nil},
		{"release without a tag name", []string{"v1.0.0"}, []StatusRelease{{Tag: ""}}, nil, []string{"v1.0.0"}, []string{"v1.0.0"}},
	} {
		got := FindTags(tagsOf(tc.tags...), tc.releases, tc.patterns)
		if !reflect.DeepEqual(names(got.Unreleased), orEmpty(tc.unreleased)) || !reflect.DeepEqual(names(got.Latest), orEmpty(tc.latest)) {
			t.Errorf("%s: unreleased %v latest %v, want %v %v", tc.name, names(got.Unreleased), names(got.Latest), tc.unreleased, tc.latest)
		}
	}
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func TestFindTagsAtMostFiveNewestFirst(t *testing.T) {
	var all []string
	for i := 1; i <= 9; i++ {
		all = append(all, fmt.Sprintf("v1.%d.0", i))
	}
	got := FindTags(tagsOf(all...), published("v1.0.0"), nil)
	want := []string{"v1.9.0", "v1.8.0", "v1.7.0", "v1.6.0", "v1.5.0"}
	if !reflect.DeepEqual(names(got.Unreleased), want) {
		t.Fatalf("%v", names(got.Unreleased))
	}
}

func TestFindTagsKeepsTheCommit(t *testing.T) {
	tags := []StatusTag{{"v1.0.0", strings.Repeat("a", 40)}, {"v1.1.0", strings.Repeat("b", 40)}}
	got := FindTags(tags, published("v1.0.0"), nil)
	if len(got.Unreleased) != 1 || got.Unreleased[0] != tags[1] || got.Latest[0] != tags[1] {
		t.Fatalf("%+v", got)
	}
}

func TestFindTagsPerDeclaredPattern(t *testing.T) {
	tags := tagsOf("bdock/1.0.0", "bdock/1.1.0", "claude/2.0.0", "claude/2.1.0", "claude/2.2.0", "v9.9.9", "other/3.0.0")
	releases := published("bdock/1.0.0", "claude/2.1.0")
	got := FindTags(tags, releases, []string{"bdock/*", "claude/*"})
	if !reflect.DeepEqual(names(got.Unreleased), []string{"bdock/1.1.0", "claude/2.2.0"}) || !reflect.DeepEqual(names(got.Latest), []string{"bdock/1.1.0", "claude/2.2.0"}) {
		t.Fatalf("%+v", got)
	}
	// The default pattern is not added to declared ones.
	for _, n := range names(got.Latest) {
		if n == "v9.9.9" || n == "other/3.0.0" {
			t.Fatalf("undeclared tag %s", n)
		}
	}
}

func TestFindTagsPatternShapes(t *testing.T) {
	for _, tc := range []struct {
		pattern, tag string
		want         bool
	}{
		{"v*.*.*", "v1.2.3", true},
		{"v*.*.*", "v1.2.3-rc.1", true},
		{"v*.*.*", "1.2.3", true},
		{"v*.*.*", "v1.2.3", true},
		{"v*.*.*", "vv1.2.3", false},
		{"v*.*.*", "v1.2.3+", false},
		{"v*.*.*", "v1.2.3+a..b", false},
		{"v*.*.*", "v1.2.3+a.b-c", true},
		{"bdock/*", "bdock/1.2.3+", false},
		{"v*.*.*", "v1.2", false},
		{"bdock/*", "bdock/1.2.3", true},
		{"bdock/*", "bdock/v1.2.3", false},
		{"bdock/*", "other/1.2.3", false},
		{"*", "1.2.3", true},
		{"rel-*-lts", "rel-1.2.3-lts", true},
		{"rel-*-lts", "rel-1.2.3", false},
		{"v1.0.0", "v1.0.0", false},
		{"x*.*", "x1", false},
		{"", "", false},
		{"v*", "v18446744073709551616.0.0", false},
	} {
		_, got := patternVersion(tc.pattern, tc.tag)
		if got != tc.want {
			t.Errorf("%q %q: %v", tc.pattern, tc.tag, got)
		}
	}
}

func TestFindTagsOverlappingPatternsListATagOnce(t *testing.T) {
	got := FindTags(tagsOf("v1.0.0", "v1.1.0"), published("v1.0.0"), []string{"v*.*.*", "*"})
	if !reflect.DeepEqual(names(got.Unreleased), []string{"v1.1.0"}) || !reflect.DeepEqual(names(got.Latest), []string{"v1.1.0"}) {
		t.Fatalf("%+v", got)
	}
}

func TestFindTagsCapIsPerRepositoryAcrossPatterns(t *testing.T) {
	var a, b []string
	for i := 1; i <= 4; i++ {
		a = append(a, fmt.Sprintf("a/1.%d.0", i))
		b = append(b, fmt.Sprintf("b/1.%d.0", i))
	}
	got := FindTags(tagsOf(append(a, b...)...), published("a/1.0.0", "b/1.0.0"), []string{"a/*", "b/*"})
	if len(got.Unreleased) != MaxUnreleasedTags || got.Unreleased[4].Name != "b/1.4.0" {
		t.Fatalf("%v", names(got.Unreleased))
	}
}

func TestFindTagsIsIndependentOfInputOrder(t *testing.T) {
	in := tagsOf("v1.3.0", "v1.1.0", "v1.2.0", "v1.2.0+x")
	rev := []StatusTag{in[3], in[2], in[1], in[0]}
	a := FindTags(in, published("v1.1.0"), nil)
	b := FindTags(rev, published("v1.1.0"), nil)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("%+v vs %+v", a, b)
	}
}

func TestSemverPrecedence(t *testing.T) {
	order := []string{"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc.1", "1.0.0", "1.0.1", "1.1.0", "2.0.0", "10.0.0"}
	for i := range order {
		for j := range order {
			a, _ := parseSemver(order[i])
			b, _ := parseSemver(order[j])
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			if got := a.compare(b); got != want {
				t.Errorf("%s vs %s: %d", order[i], order[j], got)
			}
		}
	}
}
