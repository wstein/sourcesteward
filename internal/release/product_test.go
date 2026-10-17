// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"fmt"
	"strings"
	"testing"
)

func discover(names ...string) ([]ContractFile, []string) {
	files, findings := DiscoverProducts(names)
	var got []string
	for _, f := range findings {
		got = append(got, f.Code+" "+f.File)
	}
	return files, got
}

func TestDiscoverProducts(t *testing.T) {
	cases := []struct {
		name     string
		entries  []string
		products string // product:name pairs in order
		findings []string
	}{
		{"one default toml file", []string{"release.toml"}, ":release.toml", nil},
		{"one default json file", []string{"release.json"}, ":release.json", nil},
		{"toml and json for the default product", []string{"release.toml", "release.json"}, ":release.toml", []string{"config-ambiguous .stw/release.json"}},
		{"default and a named product", []string{"release.toml", "release.zlib.toml"}, ":release.toml zlib:release.zlib.toml", []string{"config-ambiguous .stw/release.toml"}},
		{"default json and a named product", []string{"release.json", "release.zlib.toml"}, ":release.json zlib:release.zlib.toml", []string{"config-ambiguous .stw/release.json"}},
		{"two products", []string{"release.zlib.toml", "release.busybox.json"}, "busybox:release.busybox.json zlib:release.zlib.toml", nil},
		{"two files for one product", []string{"release.zlib.toml", "release.zlib.json"}, "zlib:release.zlib.toml", []string{"config-ambiguous .stw/release.zlib.json"}},
		{"yaml is not accepted", []string{"release.yaml"}, "", []string{"profile-violation .stw/release.yaml"}},
		{"yml is not accepted", []string{"release.yml"}, "", []string{"profile-violation .stw/release.yml"}},
		{"named yaml is not accepted", []string{"release.zlib.yaml", "release.toml"}, ":release.toml", []string{"profile-violation .stw/release.zlib.yaml"}},
		{"yaml beside toml", []string{"release.toml", "release.yml"}, ":release.toml", []string{"profile-violation .stw/release.yml"}},
		{"unrelated files are ignored", []string{"notes.md", "release.toml.orig", "release.txt", "release", "release.zlib.toml.bak", "other.toml", "release.toml"}, ":release.toml", nil},
		{"uppercase product id", []string{"release.Zlib.toml"}, "", []string{"schema-violation .stw/release.Zlib.toml"}},
		{"empty product id", []string{"release..toml"}, "", []string{"schema-violation .stw/release..toml"}},
		{"product id with a dot", []string{"release.a.b.toml"}, "", []string{"schema-violation .stw/release.a.b.toml"}},
		{"product id that starts with a dash", []string{"release.-a.toml"}, "", []string{"schema-violation .stw/release.-a.toml"}},
		{"product id of 32 characters", []string{"release." + strings.Repeat("a", 32) + ".toml"}, strings.Repeat("a", 32) + ":release." + strings.Repeat("a", 32) + ".toml", nil},
		{"product id of 33 characters", []string{"release." + strings.Repeat("a", 33) + ".toml"}, "", []string{"schema-violation .stw/release." + strings.Repeat("a", 33) + ".toml"}},
		{"no files", nil, "", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			files, findings := discover(c.entries...)
			var got []string
			for _, f := range files {
				got = append(got, f.Product+":"+strings.TrimPrefix(f.Path, ".stw/"))
			}
			if strings.Join(got, " ") != c.products {
				t.Fatalf("products %q, want %q", got, c.products)
			}
			if strings.Join(findings, ";") != strings.Join(c.findings, ";") {
				t.Fatalf("findings %q, want %q", findings, c.findings)
			}
		})
	}
}

func TestDiscoverProductsLimit(t *testing.T) {
	var names []string
	for i := 0; i < 64; i++ {
		names = append(names, fmt.Sprintf("release.p%02d.toml", i))
	}
	files, findings := DiscoverProducts(names)
	if len(files) != 64 || len(findings) != 0 {
		t.Fatalf("64 files: %d %+v", len(files), findings)
	}
	files, findings = DiscoverProducts(append(names, "release.p64.toml"))
	if len(files) != 64 || len(findings) != 1 || findings[0].Code != "limit-exceeded" {
		t.Fatalf("65 files: %d %+v", len(files), findings)
	}
}

func TestDiscoverProductsIsDeterministic(t *testing.T) {
	a, _ := DiscoverProducts([]string{"release.b.toml", "release.a.toml", "release.c.json"})
	b, _ := DiscoverProducts([]string{"release.c.json", "release.a.toml", "release.b.toml"})
	if fmt.Sprint(a) != fmt.Sprint(b) {
		t.Fatalf("%v != %v", a, b)
	}
}

func TestSelectProduct(t *testing.T) {
	files := []ContractFile{{Product: "busybox", Path: ".stw/release.busybox.toml"}, {Product: "zlib", Path: ".stw/release.zlib.toml"}}
	single := []ContractFile{{Product: "", Path: ".stw/release.toml"}}
	cases := []struct {
		name     string
		files    []ContractFile
		product  string
		required bool
		want     string
		code     string
	}{
		{"all products when not required", files, "", false, "busybox zlib", ""},
		{"named product", files, "zlib", false, "zlib", ""},
		{"named product when required", files, "busybox", true, "busybox", ""},
		{"unknown product", files, "nope", false, "", "product-unknown"},
		{"unknown product when required", files, "nope", true, "", "product-unknown"},
		{"required with several products", files, "", true, "", "product-required"},
		{"required with one product", single, "", true, "", ""},
		{"a name for the default product is unknown", single, "x", false, "", "product-unknown"},
		{"empty name is absent", single, "", false, "", ""},
	}
	for _, c := range cases {
		got, code := SelectProduct(c.files, c.product, c.required)
		var names []string
		for _, f := range got {
			names = append(names, f.Product)
		}
		if code != c.code || strings.Join(names, " ") != c.want {
			t.Errorf("%s: got %q %q, want %q %q", c.name, names, code, c.want, c.code)
		}
	}
}

func product(t *testing.T, example, name string, mutate func(*testing.T, map[string]any)) *Contract {
	t.Helper()
	doc := loadExample(t, example)
	if mutate != nil {
		mutate(t, doc)
	}
	c, findings, err := ValidateContract(doc, ValidateOptions{Product: name})
	if err != nil || c == nil {
		t.Fatalf("%s: %+v %v", example, findings, err)
	}
	return c
}

func mirror(t *testing.T) []*Contract {
	return []*Contract{product(t, "mirror-busybox", "busybox", nil), product(t, "mirror-zlib", "zlib", nil)}
}

func TestTwoProductMirrorSelectsByTagGrammar(t *testing.T) {
	contracts := mirror(t)
	cases := []struct {
		tag  string
		want string
		code string
	}{
		{"zlib-v1.3.2", "zlib", ""},
		{"zlib-v1.3", "zlib", ""},
		{"busybox-v1.36.1", "busybox", ""},
		{"busybox-v9.0", "busybox", ""},
		{"zlib-v", "", "product-unknown"},
		{"zlib-v1.3.2.1", "", "product-unknown"},
		{"zlib-v1.3.2\n", "", "product-unknown"},
		{"v1.3.2", "", "product-unknown"},
		{"xzlib-v1.3.2", "", "product-unknown"},
		{"zlib-v01.3", "zlib", ""},
	}
	for _, c := range cases {
		idx, code := ProductForTag(contracts, c.tag)
		got := ""
		if idx >= 0 {
			got = contracts[idx].Project.Product
		}
		if got != c.want || code != c.code {
			t.Errorf("%q: got %q %q, want %q %q", c.tag, got, code, c.want, c.code)
		}
	}
}

func TestSemverAndPatternProductsSelectTheirTags(t *testing.T) {
	cli := product(t, "go-cli", "bdock", both(set("bdock", "project", "product"), set("bdock/", "version", "tag_prefix")))
	toolbox := product(t, "oci-bundle", "toolbox", nil)
	contracts := []*Contract{cli, toolbox}
	for tag, want := range map[string]string{"bdock/v0.1.0": "bdock", "bdock/v10.20.30": "bdock", "toolbox/2026.10.15-b1": "toolbox", "toolbox/2026.10.15": "toolbox", "toolbox/2026.10.15-b98.1": "toolbox"} {
		idx, code := ProductForTag(contracts, tag)
		if code != "" || contracts[idx].Project.Product != want {
			t.Errorf("%q: %d %q", tag, idx, code)
		}
	}
	for _, tag := range []string{"bdock/0.1.0", "bdock/v01.0.0", "bdock/v1.0", "toolbox/2026.13.01", "toolbox/2026.10.15-b0", "other/v1.0.0"} {
		if idx, code := ProductForTag(contracts, tag); idx >= 0 || code != "product-unknown" {
			t.Errorf("%q matched %d %q", tag, idx, code)
		}
	}
	calver := product(t, "go-cli", "cal", both(set("cal", "project", "product"), set("calver", "version", "scheme"), set("cal/", "version", "tag_prefix")))
	if idx, code := ProductForTag([]*Contract{calver}, "cal/2026.10.15-b98.1"); idx != 0 || code != "" {
		t.Errorf("calver: %d %q", idx, code)
	}
	for _, tag := range []string{"cal/2026.10.15-b0", "cal/26.10.15", "cal/2026.13.01", "cal/2026.10.15-b1.0"} {
		if idx, _ := ProductForTag([]*Contract{calver}, tag); idx >= 0 {
			t.Errorf("calver matched %q", tag)
		}
	}
	// The grammar is the regular expression; a real calendar date is a version rule (WI2).
	if idx, _ := ProductForTag([]*Contract{calver}, "cal/2026.02.31"); idx != 0 {
		t.Error("the calver grammar does not check the calendar")
	}
}

func TestAmbiguousTagGrammarsAreRefused(t *testing.T) {
	a := product(t, "go-cli", "a", both(set("a", "project", "product"), set("x/", "version", "tag_prefix")))
	b := product(t, "go-cli", "b", both(set("b", "project", "product"), set("x/", "version", "tag_prefix")))
	if idx, code := ProductForTag([]*Contract{a, b}, "x/v1.0.0"); idx >= 0 || code != "product-required" {
		t.Fatalf("%d %q", idx, code)
	}
}

func TestProductTagOverlap(t *testing.T) {
	zlib := product(t, "mirror-zlib", "zlib", nil)
	mk := func(name, template string) *Contract {
		pattern := "^" + strings.NewReplacer(".", `\.`).Replace(strings.TrimSuffix(template, "{version}")) + `(?P<version>(?P<major>[0-9]{1,9})\.(?P<minor>[0-9]{1,9}))$`
		return product(t, "mirror-zlib", name, both(set(name, "project", "product"), set(template, "version", "tag_template"), set(pattern, "version", "tag_pattern"), set(strs("major", "minor"), "version", "order")))
	}
	cases := []struct {
		name string
		a, b *Contract
		want bool
	}{
		{"unrelated prefixes", zlib, mk("busybox", "busybox-v{version}"), false},
		{"equal prefixes", zlib, mk("zlibtwo", "zlib-v{version}"), true},
		{"one prefix extends the other", zlib, mk("zlibtwo", "zlib-v2-v{version}"), true},
		{"the shorter prefix is second", mk("zlibtwo", "zlib-v2-v{version}"), zlib, true},
		{"sibling directories", mk("one", "one/{version}"), mk("two", "two/{version}"), false},
		{"directory nesting", mk("one", "one/{version}"), mk("onetwo", "one/two/{version}"), true},
		{"prefixes that only share letters", mk("one", "ab-{version}"), mk("two", "ac-{version}"), false},
	}
	for _, c := range cases {
		findings := ProductTagOverlaps([]*Contract{c.a, c.b}, []string{".stw/release.a.toml", ".stw/release.b.toml"})
		if (len(findings) == 1) != c.want || len(findings) > 1 {
			t.Errorf("%s: %+v", c.name, findings)
		}
		for _, f := range findings {
			if f.Code != "product-tag-overlap" || f.Severity != "error" || f.File == "" {
				t.Errorf("%s: %+v", c.name, f)
			}
		}
	}
	cli := product(t, "go-cli", "bdock", both(set("bdock", "project", "product"), set("bdock/", "version", "tag_prefix")))
	if f := ProductTagOverlaps([]*Contract{cli, product(t, "oci-bundle", "toolbox", nil)}, []string{"a", "b"}); len(f) != 0 {
		t.Errorf("semver and pattern products: %+v", f)
	}
	empty := product(t, "go-cli", "plain", both(set("plain", "project", "product"), set("calver", "version", "scheme"), set("", "version", "tag_prefix")))
	if f := ProductTagOverlaps([]*Contract{empty, zlib}, []string{"a", "b"}); len(f) != 1 {
		t.Errorf("an empty prefix overlaps every product: %+v", f)
	}
}

func TestTagGrammarBoundaries(t *testing.T) {
	cli := product(t, "go-cli", "dotted", both(set("dotted", "project", "product"), set("a.b/", "version", "tag_prefix")))
	cases := map[string]bool{
		"a.b/v1.2.3":                         true,
		"a.b/v999999999.999999999.999999999": true,
		"a.b/v1000000000.0.0":                false,
		"a.b/v0.1000000000.0":                false,
		"a.b/v0.0.1000000000":                false,
		"axb/v1.2.3":                         false,
		"xa.b/v1.2.3":                        false,
		"a.b/v1.2.3x":                        false,
		"a.b/v1.2.3-rc1":                     false,
		"a.b/v1.2.3\n":                       false,
		"a.b/v01.2.3":                        false,
		"a.b/v1.02.3":                        false,
		"a.b/v1.2.03":                        false,
		"a.b/v0.0.0":                         true,
		"a.b/1.2.3":                          false,
	}
	for tag, want := range cases {
		idx, _ := ProductForTag([]*Contract{cli}, tag)
		if (idx == 0) != want {
			t.Errorf("%q: matched=%v, want %v", tag, idx == 0, want)
		}
	}
}

func TestCalverGrammarBoundaries(t *testing.T) {
	cal := product(t, "go-cli", "cal", both(set("cal", "project", "product"), set("calver", "version", "scheme"), set("cal/", "version", "tag_prefix")))
	cases := map[string]bool{
		"cal/2026.10.15": true, "cal/2026.10.15-b1": true, "cal/2026.10.15-b999999999.999999999": true,
		"cal/2026.10.15-b1000000000": false, "cal/2026.10.15-b1.1000000000": false, "cal/2026.10.15-b01": false, "cal/2026.10.15-b1.0": false,
		"cal/2026.10.15-b1.": false, "cal/2026.12.31": true, "cal/2026.00.15": false, "cal/2026.13.15": false, "cal/2026.10.00": false, "cal/2026.10.32": false,
		"cal/26.10.15": false, "cal/2026.10.15-": false, "cal/2026.10.15\n": false, "xcal/2026.10.15": false, "cal/2026.10.15x": false,
	}
	for tag, want := range cases {
		idx, _ := ProductForTag([]*Contract{cal}, tag)
		if (idx == 0) != want {
			t.Errorf("%q: matched=%v, want %v", tag, idx == 0, want)
		}
	}
}

func TestOverlapUsesTheLiteralPrefixOfEachScheme(t *testing.T) {
	semver := product(t, "go-cli", "sv", both(set("sv", "project", "product"), set("x/", "version", "tag_prefix")))
	mk := func(name, template string) *Contract {
		pattern := "^" + template[:len(template)-len("{version}")] + `(?P<version>(?P<major>[0-9]{1,9})\.(?P<minor>[0-9]{1,9}))$`
		return product(t, "mirror-zlib", name, both(set(name, "project", "product"), set(template, "version", "tag_template"), set(pattern, "version", "tag_pattern"), set(strs("major", "minor"), "version", "order")))
	}
	// The semver tag always continues with "v", so x/w... cannot be one of its tags.
	if f := ProductTagOverlaps([]*Contract{semver, mk("w", "x/w{version}")}, []string{"a", "b"}); len(f) != 0 {
		t.Errorf("x/v... and x/w... do not overlap: %+v", f)
	}
	if f := ProductTagOverlaps([]*Contract{semver, mk("v", "x/v{version}")}, []string{"a", "b"}); len(f) != 1 {
		t.Errorf("x/v... overlaps x/v...: %+v", f)
	}
}
