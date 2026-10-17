// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

// encodeTOML writes a parsed document back in the profile; a document the parser accepted
// must survive the round trip unchanged.
func encodeTOML(doc map[string]any) string {
	var out strings.Builder
	keys := make([]string, 0, len(doc))
	for k := range doc {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var tables []string
	for _, k := range keys {
		switch v := doc[k].(type) {
		case map[string]any:
			tables = append(tables, k)
		case []any:
			if len(v) > 0 {
				if _, isTable := v[0].(map[string]any); isTable {
					tables = append(tables, k)
					continue
				}
			}
			out.WriteString(k + " = " + encodeValue(v) + "\n")
		default:
			out.WriteString(k + " = " + encodeValue(v) + "\n")
		}
	}
	for _, k := range tables {
		switch v := doc[k].(type) {
		case map[string]any:
			out.WriteString("[" + k + "]\n" + encodeTOML(v))
		case []any:
			for _, entry := range v {
				out.WriteString("[[" + k + "]]\n" + encodeTOML(entry.(map[string]any)))
			}
		}
	}
	return out.String()
}

func encodeValue(v any) string {
	switch v := v.(type) {
	case string:
		var b strings.Builder
		b.WriteByte('"')
		for _, r := range v {
			switch {
			case r == '"' || r == '\\':
				b.WriteByte('\\')
				b.WriteRune(r)
			case r < 0x20 || r == 0x7f:
				fmt.Fprintf(&b, "\\u%04x", r)
			default:
				b.WriteRune(r)
			}
		}
		b.WriteByte('"')
		return b.String()
	case int64:
		return fmt.Sprint(v)
	case bool:
		return fmt.Sprint(v)
	case []any:
		parts := make([]string, len(v))
		for i, e := range v {
			parts[i] = encodeValue(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	}
	panic(fmt.Sprintf("value outside the profile: %T", v))
}

// checkProfile asserts that an accepted document lies inside the profile.
func checkProfile(t *testing.T, data []byte, doc map[string]any) {
	t.Helper()
	if len(data) > MaxContractBytes || strings.HasPrefix(string(data), "\xef\xbb\xbf") || strings.IndexByte(string(data), 0) >= 0 || !utf8.Valid(data) {
		t.Fatalf("accepted input outside the encoding rules: %q", data)
	}
	scalar := func(v any) {
		switch v := v.(type) {
		case string, int64, bool:
		case []any:
			for _, e := range v {
				if _, ok := e.(string); !ok {
					t.Fatalf("array holds %T", e)
				}
			}
		default:
			t.Fatalf("value %T is outside the profile", v)
		}
	}
	for k, v := range doc {
		if k == "" || strings.Trim(k, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-") != "" {
			t.Fatalf("non-bare key %q", k)
		}
		switch v := v.(type) {
		case map[string]any:
			for _, inner := range v {
				scalar(inner)
			}
		case []any:
			if len(v) > 0 {
				if _, isTable := v[0].(map[string]any); isTable {
					for _, e := range v {
						for _, inner := range e.(map[string]any) {
							scalar(inner)
						}
					}
					continue
				}
			}
			scalar(v)
		default:
			scalar(v)
		}
	}
	again, findings := ParseTOML([]byte(encodeTOML(doc)))
	if len(findings) != 0 || !reflect.DeepEqual(again, doc) {
		t.Fatalf("accepted document does not round trip: %v %+v", doc, findings)
	}
}

func FuzzParseTOML(f *testing.F) {
	for _, c := range tomlCases() {
		f.Add([]byte(c.input))
	}
	files, _ := filepath.Glob("../../specs/026-release-process/examples/*.release.toml")
	for _, name := range files {
		raw, err := os.ReadFile(name)
		if err == nil {
			f.Add(raw)
		}
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		doc, findings := ParseTOML(data)
		if (doc == nil) == (len(findings) == 0) {
			t.Fatalf("document and findings disagree: %v %v", doc, findings)
		}
		if doc != nil {
			checkProfile(t, data, doc)
		}
		for _, finding := range findings {
			if finding.Code != "profile-violation" && finding.Code != "limit-exceeded" || finding.Severity != "error" {
				t.Fatalf("unexpected finding %+v", finding)
			}
		}
	})
}

func TestFuzzSeedsHoldTheProfileProperties(t *testing.T) {
	for _, c := range tomlCases() {
		doc, findings := ParseTOML([]byte(c.input))
		if (doc == nil) == (len(findings) == 0) {
			t.Fatalf("%s: document and findings disagree", c.name)
		}
		if doc != nil {
			checkProfile(t, []byte(c.input), doc)
		}
	}
}
