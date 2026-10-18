// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"errors"
	"strings"
	"testing"
)

func rawDiff(old, new, status, path string) string {
	return ":" + old + " " + new + " " + strings.Repeat("a", 40) + " " + strings.Repeat("b", 40) + " " + status + "\x00" + path + "\x00"
}
func TestDiffMetadataClassification(t *testing.T) {
	for _, tc := range []struct {
		old, new, status, path string
		allowed                bool
	}{
		{"100644", "100644", "M", "docs/a.md", true}, {"000000", "100755", "A", "docs/new", true}, {"100755", "000000", "D", "docs/gone", true},
		{"100644", "100755", "M", "docs/mode", false}, {"120000", "120000", "M", "docs/link", false}, {"160000", "160000", "M", "docs/sub", false}, {"100644", "100644", "T", "docs/type", false},
		{"100644", "100644", "M", ".stw/release.toml", false}, {"100644", "100644", "M", ".github/a", false}, {"100644", "100644", "M", "docs/\xff", false}, {"100644", "100644", "M", "src/a", false},
		{"100644", "100644", "M", "docs/releases/v1.json", true},
		{"100644", "100644", "R100", "docs/renamed", false}, {"100644", "100644", "C100", "docs/copied", false}, {"100644", "100644", "U", "docs/conflict", false}, {"100644", "100644", "X", "docs/unknown", false},
	} {
		t.Run(tc.path+tc.status, func(t *testing.T) {
			r, e := parseRawDiff(strings.NewReader(rawDiff(tc.old, tc.new, tc.status, tc.path)), []string{"docs/**"}, "docs/releases/v1.json")
			if e != nil || (len(r.Offending) == 0) != tc.allowed || r.Entries != 1 {
				t.Fatalf("%+v %v", r, e)
			}
		})
	}
}
func TestDiffLimitsAndMalformed(t *testing.T) {
	good := rawDiff("100644", "100644", "M", "docs/a")
	for _, n := range []int{20000, 20001} {
		r, e := parseRawDiff(strings.NewReader(strings.Repeat(good, n)), []string{"docs/**"}, "")
		if (errors.Is(e, errReleaseLimit)) != (n > 20000) || r.Entries != min(n, 20001) {
			t.Fatalf("n=%d: %+v %v", n, r, e)
		}
	}
	for _, data := range []string{":broken\x00a\x00", strings.TrimSuffix(good, "\x00"), strings.Repeat("x", 4<<20+1)} {
		if _, e := parseRawDiff(strings.NewReader(data), nil, ""); e == nil {
			t.Fatal("accepted malformed or oversized diff")
		}
	}
	r, e := parseRawDiff(strings.NewReader(strings.Repeat(rawDiff("100644", "100644", "M", "src/no"), 21)), nil, "")
	if e != nil || len(r.Offending) != 20 || !r.Truncated {
		t.Fatalf("%+v %v", r, e)
	}
}

func TestDiffForbiddenPathsOverrideMatchingAllowlist(t *testing.T) {
	for _, path := range []string{".stw/release.toml", ".github/workflows/release.yml"} {
		pattern := strings.Split(path, "/")[0] + "/**"
		if !AllowedPath(pattern, path) {
			t.Fatal("fixture does not match allowlist")
		}
		r, e := parseRawDiff(strings.NewReader(rawDiff("100644", "100644", "M", path)), []string{pattern}, path)
		if e != nil || len(r.Offending) != 1 || r.Offending[0] != path {
			t.Fatalf("forbidden path accepted despite allow match: %+v %v", r, e)
		}
	}
}
