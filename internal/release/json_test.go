// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseJSONAcceptsTheModelShape(t *testing.T) {
	doc, findings := ParseJSON([]byte(`{"schema_version":1,"a":{"b":[1,"x",true,null,2.0]},"n":-3,"f":1.5,"big":12345678901234567890}`))
	if len(findings) != 0 {
		t.Fatal(findings)
	}
	want := map[string]any{
		"schema_version": int64(1),
		"a":              map[string]any{"b": []any{int64(1), "x", true, nil, int64(2)}},
		"n":              int64(-3),
		"f":              1.5,
		"big":            1.2345678901234567e19,
	}
	if !reflect.DeepEqual(doc, want) {
		t.Fatalf("got %#v", doc)
	}
}

func TestParseJSONRefusals(t *testing.T) {
	cases := []struct {
		name  string
		input string
		code  string
		ptr   string
	}{
		{"duplicate key", `{"a":1,"a":2}`, "profile-violation", "/a"},
		{"nested duplicate key", `{"a":{"b":1,"b":2}}`, "profile-violation", "/a/b"},
		{"duplicate key inside array", `{"a":[{"b":1,"b":2}]}`, "profile-violation", "/a/0/b"},
		{"duplicate through an escape", `{"a":1,"a":2}`, "profile-violation", "/a"},
		{"pointer escaping", `{"a/b":1,"a/b":2}`, "profile-violation", "/a~1b"},
		{"syntax error", `{"a":}`, "profile-violation", ""},
		{"trailing comma", `{"a":1,}`, "profile-violation", ""},
		{"trailing data", `{"a":1} {}`, "profile-violation", ""},
		{"empty input", ``, "profile-violation", ""},
		{"byte order mark", "\xef\xbb\xbf{}", "profile-violation", ""},
		{"invalid UTF-8", "{\"a\":\"\xff\"}", "profile-violation", ""},
		{"raw NUL", "{\"a\":\"\x00\"}", "profile-violation", ""},
		{"too deep", strings.Repeat("[", 100) + strings.Repeat("]", 100), "profile-violation", ""},
		{"too large", `{"a":"` + strings.Repeat("x", 64*1024) + `"}`, "limit-exceeded", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc, findings := ParseJSON([]byte(c.input))
			if doc != nil || len(findings) != 1 || findings[0].Code != c.code || findings[0].Pointer != c.ptr {
				t.Fatalf("got %#v %+v", doc, findings)
			}
		})
	}
}

func TestParseJSONKeepsANonObjectRoot(t *testing.T) {
	doc, findings := ParseJSON([]byte(`[1]`))
	if len(findings) != 0 || !reflect.DeepEqual(doc, []any{int64(1)}) {
		t.Fatalf("%#v %+v", doc, findings)
	}
}

func TestParseJSONSizeBoundaryAndMessages(t *testing.T) {
	exact := `{"a":"` + strings.Repeat("x", 64*1024-8) + `"}`
	if len(exact) != 64*1024 {
		t.Fatalf("fixture has %d bytes", len(exact))
	}
	if doc, findings := ParseJSON([]byte(exact)); doc == nil || len(findings) != 0 {
		t.Fatalf("a file of exactly 64 KiB is within the limit: %+v", findings)
	}
	why := map[string]string{
		"\xef\xbb\xbf{}": "byte order mark", "{\"a\":\"\xff\"}": "UTF-8", `{"a":1} {}`: "after the top-level value",
		strings.Repeat("[", 100) + strings.Repeat("]", 100): "too deep", `{"a":1,"a":2}`: "duplicate key",
	}
	for input, want := range why {
		_, findings := ParseJSON([]byte(input))
		if len(findings) != 1 || !strings.Contains(findings[0].Message, want) {
			t.Errorf("%q: %+v, want %q", input, findings, want)
		}
	}
}
