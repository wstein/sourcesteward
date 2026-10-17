// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"encoding/json"
	"strings"
	"testing"
)

func decodeJSON(t *testing.T, raw string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func schemaCodes(findings []Finding) string {
	var parts []string
	for _, f := range findings {
		parts = append(parts, f.Code+" "+f.Pointer)
	}
	return strings.Join(parts, "; ")
}

func TestSchemaKeywordsAreAllInterpreted(t *testing.T) {
	if err := checkSchemaKeywords(); err != nil {
		t.Fatal(err)
	}
}

func TestSchemaFindingsCarryPointersAndCodes(t *testing.T) {
	cases := []struct {
		name string
		doc  string
		want string
	}{
		{"unknown top-level key", `{"extra":1}`, "schema-violation /extra"},
		{"wrong type", `{"schema_version":"1"}`, "schema-violation /schema_version"},
		{"const", `{"schema_version":2}`, "schema-violation /schema_version"},
		{"missing property", `{"schema_version":1}`, "schema-violation "},
		{"max items is a limit", `{"allowlist":{"paths":[` + strings.Repeat(`"a",`, 256) + `"a"]}}`, "limit-exceeded /allowlist/paths"},
		{"max length is a limit", `{"version":{"tag_pattern":"` + strings.Repeat("a", 257) + `"}}`, "limit-exceeded /version/tag_pattern"},
		{"pattern", `{"project":{"name":"bad/name"}}`, "schema-violation /project/name"},
		{"pointer escaping", `{"a/b~c":1}`, "schema-violation /a~1b~0c"},
		{"conditional forbidden property", `{"version":{"scheme":"semver","source":"input","order":["a"]}}`, "schema-violation /version/order"},
		{"conditional required property", `{"version":{"scheme":"pattern","source":"input"}}`, "schema-violation /version"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := schemaCodes(validateConfigSchema(decodeJSON(t, c.doc)))
			if !strings.Contains(got, c.want) {
				t.Fatalf("want %q in %q", c.want, got)
			}
		})
	}
}

func TestSchemaFindingsAreOrderedAndUnique(t *testing.T) {
	findings := validateConfigSchema(decodeJSON(t, `{"z":1,"a":2,"schema_version":3}`))
	for i := 1; i < len(findings); i++ {
		if findings[i-1].Pointer > findings[i].Pointer {
			t.Fatalf("not ordered by pointer: %s", schemaCodes(findings))
		}
		if findings[i-1] == findings[i] {
			t.Fatalf("duplicate finding: %s", schemaCodes(findings))
		}
	}
}

func TestSchemaIntegerSemantics(t *testing.T) {
	for _, raw := range []string{"900", "900.0"} {
		doc := decodeJSON(t, `{"build":[{"timeout_seconds":`+raw+`}]}`)
		for _, f := range validateConfigSchema(doc) {
			if f.Pointer == "/build/0/timeout_seconds" {
				t.Fatalf("%s rejected: %+v", raw, f)
			}
		}
	}
	for _, raw := range []string{"1.5", "true", `"9"`, "null"} {
		doc := decodeJSON(t, `{"build":[{"timeout_seconds":`+raw+`}]}`)
		found := false
		for _, f := range validateConfigSchema(doc) {
			found = found || f.Pointer == "/build/0/timeout_seconds"
		}
		if !found {
			t.Fatalf("%s accepted", raw)
		}
	}
}

func TestSchemaMessagesTruncateHostileValues(t *testing.T) {
	long := strings.Repeat("x", 5000)
	for _, f := range validateConfigSchema(decodeJSON(t, `{"project":{"name":"`+long+`"}}`)) {
		if len(f.Message) > 400 {
			t.Fatalf("message of %d bytes echoes the value", len(f.Message))
		}
	}
}

func TestSchemaBoundaries(t *testing.T) {
	rep := func(item string, n int) string { return strings.TrimSuffix(strings.Repeat(item+",", n), ",") }
	cases := []struct {
		name string
		doc  string
		path string
		ok   bool
	}{
		{"name of 64 characters", `{"project":{"name":"` + strings.Repeat("a", 64) + `"}}`, "/project/name", true},
		{"name of 65 characters", `{"project":{"name":"` + strings.Repeat("a", 65) + `"}}`, "/project/name", false},
		{"256 allowlist patterns", `{"allowlist":{"paths":[` + rep(`"a"`, 256) + `]}}`, "/allowlist/paths", true},
		{"257 allowlist patterns", `{"allowlist":{"paths":[` + rep(`"a"`, 257) + `]}}`, "/allowlist/paths", false},
		{"one build argv element", `{"build":[{"argv":["a"]}]}`, "/build/0/argv", true},
		{"no build argv element", `{"build":[{"argv":[]}]}`, "/build/0/argv", false},
		{"timeout 1", `{"build":[{"timeout_seconds":1}]}`, "/build/0/timeout_seconds", true},
		{"timeout 0", `{"build":[{"timeout_seconds":0}]}`, "/build/0/timeout_seconds", false},
		{"timeout 7200", `{"build":[{"timeout_seconds":7200}]}`, "/build/0/timeout_seconds", true},
		{"timeout 7201", `{"build":[{"timeout_seconds":7201}]}`, "/build/0/timeout_seconds", false},
		{"empty source offer", `{"target":[{"kind":"oci-image","source_offer":""}]}`, "/target/0/source_offer", false},
		{"one-character source offer", `{"target":[{"kind":"oci-image","source_offer":"a"}]}`, "/target/0/source_offer", true},
		{"16 builds", `{"build":[` + rep(`{}`, 16) + `]}`, "/build", true},
		{"17 builds", `{"build":[` + rep(`{}`, 17) + `]}`, "/build", false},
		{"8 workflows", `{"ci":{"workflows":[` + rep(`"a.yml"`, 8) + `]}}`, "/ci/workflows", true},
		{"9 workflows", `{"ci":{"workflows":[` + rep(`"a.yml"`, 9) + `]}}`, "/ci/workflows", false},
	}
	for _, c := range cases {
		found := false
		for _, f := range validateConfigSchema(decodeJSON(t, c.doc)) {
			found = found || f.Pointer == c.path
		}
		if found == c.ok {
			t.Errorf("%s: finding at %s present=%v, valid=%v", c.name, c.path, found, c.ok)
		}
	}
}

func TestSchemaConditionalsAndAnyOf(t *testing.T) {
	// anyOf inside not: either forbidden property alone triggers the finding.
	for _, doc := range []string{`{"version":{"scheme":"semver","tag_pattern":"x"}}`, `{"version":{"scheme":"semver","tag_template":"x"}}`, `{"version":{"scheme":"semver","order":["a"]}}`, `{"version":{"scheme":"semver","monotonicity":"strict"}}`} {
		found := false
		for _, f := range validateConfigSchema(decodeJSON(t, doc)) {
			found = found || strings.HasPrefix(f.Pointer, "/version/") && f.Code == "schema-violation" && f.Message == "property is not allowed with this configuration"
		}
		if !found {
			t.Errorf("%s: forbidden property not reported", doc)
		}
	}
	// A pattern product with all its properties is clean at the version table.
	clean := `{"version":{"scheme":"pattern","source":"input","tag_pattern":"x","tag_template":"x","order":["a"]}}`
	for _, f := range validateConfigSchema(decodeJSON(t, clean)) {
		if strings.HasPrefix(f.Pointer, "/version") && f.Pointer != "/version/tag_template" && f.Pointer != "/version/tag_pattern" {
			t.Errorf("unexpected %+v", f)
		}
	}
}

func TestSchemaFindingsAreInNumericIndexOrder(t *testing.T) {
	builds := make([]string, 12)
	for i := range builds {
		builds[i] = `{"bogus":1}`
	}
	findings := validateConfigSchema(decodeJSON(t, `{"build":[`+strings.Join(builds, ",")+`]}`))
	var pointers []string
	for _, f := range findings {
		if strings.HasSuffix(f.Pointer, "/bogus") {
			pointers = append(pointers, f.Pointer)
		}
	}
	if len(pointers) != 12 {
		t.Fatalf("got %d", len(pointers))
	}
	if pointers[0] != "/build/0/bogus" || pointers[2] != "/build/2/bogus" || pointers[11] != "/build/11/bogus" {
		t.Fatalf("array indexes are not in numeric order: %v", pointers)
	}
}

// Two branches of a schema may report the same problem; it is reported once.
func TestSchemaFindingsAreSortedEvenWhenVisitedOutOfOrder(t *testing.T) {
	findings := validateConfigSchema(decodeJSON(t, `{"version":{"scheme":"pattern","source":"nope"}}`))
	var versionAt, sourceAt = -1, -1
	for i, f := range findings {
		switch f.Pointer {
		case "/version":
			versionAt = i
		case "/version/source":
			sourceAt = i
		}
	}
	if versionAt < 0 || sourceAt < 0 || versionAt > sourceAt {
		t.Fatalf("the missing-property finding at /version is visited after /version/source but sorts before it: %s", schemaCodes(findings))
	}
}

func TestSchemaWalkerReportsAFindingOnce(t *testing.T) {
	root := map[string]any{"allOf": []any{
		map[string]any{"required": []any{"a"}},
		map[string]any{"required": []any{"a"}},
	}}
	w := &schemaWalker{root: root, seen: map[string]bool{}}
	w.check(root, map[string]any{}, "")
	if len(w.out) != 1 {
		t.Fatalf("got %+v", w.out)
	}
}

func TestSchemaMaxLengthBoundary(t *testing.T) {
	ok := validateConfigSchema(decodeJSON(t, `{"version":{"tag_pattern":"`+strings.Repeat("a", 256)+`"}}`))
	for _, f := range ok {
		if f.Pointer == "/version/tag_pattern" {
			t.Fatalf("256 characters rejected: %+v", f)
		}
	}
	over := validateConfigSchema(decodeJSON(t, `{"version":{"tag_pattern":"`+strings.Repeat("é", 257)+`"}}`))
	found := false
	for _, f := range over {
		found = found || f.Pointer == "/version/tag_pattern" && f.Code == "limit-exceeded"
	}
	if !found {
		t.Fatal("257 characters (counted as characters, not bytes) accepted")
	}
}

func TestFindingsSortByFileThenPointer(t *testing.T) {
	findings := []Finding{
		{Code: "b", Pointer: "/a", File: ".stw/release.z.toml"},
		{Code: "b", Pointer: "/b", File: ".stw/release.a.toml"},
		{Code: "b", Pointer: "/a", File: ".stw/release.a.toml"},
		{Code: "a", Pointer: "/a", File: ".stw/release.a.toml"},
	}
	sortFindings(findings)
	got := ""
	for _, f := range findings {
		got += f.File[len(".stw/release."):len(".stw/release.")+1] + f.Pointer + f.Code + " "
	}
	if got != "a/aa a/ab a/bb z/ab " {
		t.Fatalf("%q", got)
	}
}

func TestPointerOrderIsNumericForIndexes(t *testing.T) {
	cases := []struct{ a, b string }{
		{"/build/2", "/build/10"}, {"/build/9/argv", "/build/10/argv"}, {"", "/a"}, {"/a", "/a/b"}, {"/a/b", "/b"},
		{"/build/1/x", "/build/01/x"}, {"/a~0b", "/a~1b"}, {"/x/2", "/x/b"},
	}
	for _, c := range cases {
		if !pointerLess(c.a, c.b) || pointerLess(c.b, c.a) {
			t.Errorf("%q should sort before %q", c.a, c.b)
		}
	}
	if pointerLess("/a", "/a") {
		t.Error("equal pointers")
	}
}
