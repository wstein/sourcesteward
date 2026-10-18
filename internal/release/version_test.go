// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"reflect"
	"strings"
	"testing"
)

func mustVersionScheme(t *testing.T, v Version) *VersionScheme {
	t.Helper()
	s, err := CompileVersionScheme(v)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestVersionValidityAndRoundTrip(t *testing.T) {
	cases := []struct {
		scheme, prefix string
		valid, invalid []string
	}{
		{"semver", "bdock/", []string{"v0.0.0", "v0.10.0", "v999999999.999999999.999999999"}, []string{"0.1.0", "v01.0.0", "v1.00.0", "v1.0.00", "v1.0", "v1.0.0-rc1", "v1.0.0+meta", "v1000000000.0.0", "v0.1000000000.0", "v0.0.1000000000", "v1.0.0\n", "xv1.0.0", "v١.0.0"}},
		{"calver", "toolbox/", []string{"2026.10.15", "2026.10.15-b98", "2026.10.15-b98.1", "2000.02.29", "2024.02.29", "2026.10.15-b999999999.999999999"}, []string{"2026.02.31", "1900.02.29", "2025.02.29", "2026.04.31", "2026.00.15", "2026.13.15", "2026.10.00", "2026.10.32", "2026.1.15", "2026.10.15-b0", "2026.10.15-b01", "2026.10.15-b1.0", "2026.10.15-b1.01", "2026.10.15-b1000000000", "2026.10.15-b1.1000000000", "2026.10.15\n"}},
	}
	for _, tc := range cases {
		t.Run(tc.scheme, func(t *testing.T) {
			s := mustVersionScheme(t, Version{Scheme: tc.scheme, TagPrefix: tc.prefix})
			for _, v := range tc.valid {
				if !s.Valid(v) {
					t.Errorf("valid %q refused", v)
				}
				tag, err := s.Tag(v)
				if err != nil || tag != tc.prefix+v {
					t.Fatalf("Tag(%q) = %q, %v", v, tag, err)
				}
				got, tuple, ok := s.ParseTag(tag)
				if !ok || got != v || len(tuple) == 0 {
					t.Errorf("ParseTag(%q) = %q, %v, %v", tag, got, tuple, ok)
				}
				for _, bad := range []string{tag + "\n", "x" + tag, v} {
					if _, _, ok := s.ParseTag(bad); ok {
						t.Errorf("foreign tag %q accepted", bad)
					}
				}
			}
			for _, v := range tc.invalid {
				if s.Valid(v) {
					t.Errorf("invalid %q accepted", v)
				}
				if _, err := s.Tag(v); err == nil {
					t.Errorf("Tag(%q) succeeded", v)
				}
				if _, _, ok := s.ParseTag(tc.prefix + v); ok {
					t.Errorf("invalid tag %q accepted", v)
				}
			}
		})
	}
}

func TestPatternVersionRoundTrip(t *testing.T) {
	s := mustVersionScheme(t, product(t, "mirror-zlib", "zlib", nil).Version)
	for _, tc := range []struct {
		v     string
		tuple VersionTuple
	}{{"1.3", VersionTuple{1, 3, -1}}, {"1.3.1", VersionTuple{1, 3, 1}}, {"01.03.001", VersionTuple{1, 3, 1}}} {
		tag, err := s.Tag(tc.v)
		v, tuple, ok := s.ParseTag(tag)
		if err != nil || tag != "zlib-v"+tc.v || !ok || v != tc.v || !reflect.DeepEqual(tuple, tc.tuple) {
			t.Errorf("round trip %q: %q %q %v %v %v", tc.v, tag, v, tuple, ok, err)
		}
	}
	for _, bad := range []string{"zlib-v1.3.1\n", "busybox-v9.0.0", "zlib-v1.3.1x", "zlib-v1.3.1000000000"} {
		if _, _, ok := s.ParseTag(bad); ok {
			t.Errorf("accepted %q", bad)
		}
	}
	v := Version{Scheme: "pattern", TagTemplate: "pkg/{version}-src", TagPattern: `^pkg/(?P<version>(?P<n>[0-9]{1,9}))-src$`, Order: []string{"n"}}
	suffix := mustVersionScheme(t, v)
	tag, err := suffix.Tag("001")
	got, _, ok := suffix.ParseTag(tag)
	if err != nil || tag != "pkg/001-src" || got != "001" || !ok {
		t.Fatalf("suffix round trip: %s %s %v %v", tag, got, ok, err)
	}
}

func TestVersionTupleNumericOrder(t *testing.T) {
	for _, tc := range []struct{ scheme, a, b string }{{"semver", "v0.10.0", "v0.9.0"}, {"semver", "v999999999.0.0", "v999999998.999999999.999999999"}, {"calver", "2026.10.15-b98.1", "2026.10.15-b98"}, {"calver", "2026.10.15-b98", "2026.10.15"}} {
		s := mustVersionScheme(t, Version{Scheme: tc.scheme})
		_, a, _ := s.ParseTag(tc.a)
		_, b, _ := s.ParseTag(tc.b)
		if CompareVersionTuples(a, b) != 1 || CompareVersionTuples(b, a) != -1 || CompareVersionTuples(a, a) != 0 {
			t.Errorf("comparison %s %s: %v %v", tc.a, tc.b, a, b)
		}
	}
}

func TestVersionMonotonicity(t *testing.T) {
	toolbox := product(t, "oci-bundle", "toolbox", nil).Version
	strictToolbox := toolbox
	strictToolbox.Monotonicity = ""
	zlib := product(t, "mirror-zlib", "zlib", nil).Version
	cases := []struct {
		name              string
		v                 Version
		version           string
		names             []string
		subject           string
		monotonic, exists bool
		ignored           int
	}{
		{"semver numeric", Version{Scheme: "semver"}, "v0.10.0", []string{"v0.9.0"}, "", true, false, 0},
		{"maximum not last", Version{Scheme: "semver"}, "v0.9.0", []string{"v0.10.0", "v0.1.0"}, "", false, false, 0},
		{"empty set", Version{Scheme: "semver"}, "v0.0.0", nil, "", true, false, 0},
		{"subject excluded", Version{Scheme: "semver"}, "v1.0.0", []string{"v1.0.0", "v0.9.0"}, "v1.0.0", true, true, 0},
		{"zlib greater", zlib, "1.3.2", []string{"zlib-v1.3.1", "zlib-v1.3", "busybox-v9.0.0", "zlib-v1.3.2\n"}, "", true, false, 2},
		{"zlib optional absent", zlib, "1.3", []string{"zlib-v1.3.1"}, "", false, false, 0},
		{"strict equal zeros", zlib, "01.03.01", []string{"zlib-v1.3.1"}, "", false, false, 0},
		{"restart accepted", toolbox, "2026.10.15-b1", []string{"toolbox/2026.10.15-b98.1"}, "", true, false, 0},
		{"bare date accepted", toolbox, "2026.10.15", []string{"toolbox/2026.10.15-b98.1"}, "", true, false, 0},
		{"date regression", toolbox, "2026.10.14-b5", []string{"toolbox/2026.10.15-b98.1"}, "", false, false, 0},
		{"bare date regression", toolbox, "2026.10.14", []string{"toolbox/2026.10.15-b98.1"}, "", false, false, 0},
		{"existing full tag", toolbox, "2026.10.15-b98.1", []string{"toolbox/2026.10.15-b98.1"}, "", true, true, 0},
		{"strict default", strictToolbox, "2026.10.15-b1", []string{"toolbox/2026.10.15-b98.1"}, "", false, false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := mustVersionScheme(t, tc.v)
			got, err := s.CheckMonotonicity(tc.version, tc.names, tc.subject)
			if err != nil || got.Monotonic != tc.monotonic || got.Exists != tc.exists || got.Ignored != tc.ignored || got.Matched != len(tc.names)-tc.ignored {
				t.Fatalf("got %+v %v", got, err)
			}
		})
	}
	zlib.Monotonicity = "non-decreasing"
	s := mustVersionScheme(t, zlib)
	got, err := s.CheckMonotonicity("01.03.01", []string{"zlib-v1.3.1"}, "")
	if err != nil || !got.Monotonic || got.Exists {
		t.Fatalf("equal tuple distinct tag: %+v %v", got, err)
	}
	if _, err := s.CheckMonotonicity("invalid", nil, ""); err == nil {
		t.Fatal("invalid candidate accepted")
	}
}

func TestToolboxPatternDoesNotValidateCalendar(t *testing.T) {
	s := mustVersionScheme(t, product(t, "oci-bundle", "toolbox", nil).Version)
	if !s.Valid("2026.02.31") {
		t.Fatal("pattern must not apply calver calendar validation")
	}
	for _, bad := range []string{"2026.13.01", "2026.02.32", "2026.02.01-b0", "2026.02.01-b1000000000"} {
		if s.Valid(bad) {
			t.Errorf("fixture accepted %q", bad)
		}
	}
}

func TestCompileVersionSchemeRejectsInvalidConfig(t *testing.T) {
	for _, v := range []Version{{Scheme: "unknown"}, {Scheme: "semver", Monotonicity: "non-decreasing"}, {Scheme: "calver", Monotonicity: "non-decreasing"}, {Scheme: "semver", TagPrefix: "../"}, {Scheme: "pattern"}, {Scheme: "pattern", TagTemplate: "x{version}", TagPattern: `^x(?P<version>(?P<n>[0-9]{1,9}))$`, Order: []string{"n"}, Monotonicity: "unknown"}} {
		if _, err := CompileVersionScheme(v); err == nil {
			t.Errorf("invalid config accepted: %+v", v)
		}
	}
	v := Version{Scheme: "pattern", TagTemplate: "x{version}", TagPattern: `^x(?P<version>(?P<n>[0-9]{1,9}))$`, Order: []string{"n"}}
	for _, pattern := range []string{strings.TrimPrefix(v.TagPattern, "^"), strings.Replace(v.TagPattern, "{1,9}", "{1,10}", 1), strings.Replace(v.TagPattern, "?P<n>", "?P<other>", 1), "("} {
		v.TagPattern = pattern
		if _, err := CompileVersionScheme(v); err == nil {
			t.Errorf("invalid pattern accepted %q", pattern)
		}
	}
}

// Reuse every one-rule tree mutant of the contract linter to keep runtime
// compilation from becoming a second, weaker interpretation of pattern rules.
func TestVersionCompileUsesContractTreeRules(t *testing.T) {
	for _, tc := range schemeCases() {
		t.Run(tc.name, func(t *testing.T) {
			doc := loadExample(t, tc.example)
			if tc.mutate != nil {
				tc.mutate(t, doc)
			}
			v := modelFrom(doc).Version
			_, err := CompileVersionScheme(v)
			wantInvalid := false
			for _, finding := range tc.want {
				if strings.HasPrefix(finding, "scheme-invalid ") {
					wantInvalid = true
				}
			}
			if (err != nil) != wantInvalid {
				t.Fatalf("compile error = %v, want invalid %v", err, wantInvalid)
			}
		})
	}
}

func TestVersionMonotonicityMaximumAndDuplicateNames(t *testing.T) {
	s := mustVersionScheme(t, Version{Scheme: "calver", TagPrefix: "toolbox/"})
	got, err := s.CheckMonotonicity("2026.10.16", []string{"toolbox/2026.10.15-b98.1", "toolbox/2026.10.15", "toolbox/2026.10.15-b98.1", "toolbox/2026.02.31", "2026.10.15"}, "")
	if err != nil || !got.Monotonic || got.Exists || got.Matched != 3 || got.Ignored != 2 || !reflect.DeepEqual(got.Maximum, VersionTuple{2026, 10, 15, 98, 1}) {
		t.Fatalf("maximum/counts: %+v %v", got, err)
	}
}
