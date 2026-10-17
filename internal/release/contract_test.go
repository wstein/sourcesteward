// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Each case is one rule of the strict TOML profile (contracts/config.md). Every violation
// case must produce exactly one finding with the stated code on the stated line.
type tomlCase struct {
	name  string
	input string
	code  string // "" means accepted
	line  int    // 1-based line named in the message when rejected
}

func tomlCases() []tomlCase {
	return []tomlCase{
		// accepted forms
		{name: "empty document", input: ""},
		{name: "comments and blank lines", input: "# c\n\n  # indented\nschema_version = 1 # trailing\n"},
		{name: "crlf line ends", input: "schema_version = 1\r\n[project]\r\nname = \"x\"\r\n"},
		{name: "literal string", input: "a = 'C:\\path\\{x}'\n"},
		{name: "basic string escapes", input: "a = \"q\\\"b\\\\t\\t\\n\\r\\b\\f\\u00e9\\U0001F600\"\n"},
		{name: "integers", input: "a = 0\nb = -0\nc = -5\nd = 9223372036854775807\ne = -9223372036854775808\n"},
		{name: "booleans", input: "a = true\nb = false\n"},
		{name: "empty array", input: "a = []\n"},
		{name: "array of strings multi-line with comments and trailing comma", input: "a = [\n  \"x\", # one\n  'y',\n\n  # between\n  \"z\",\n]\n"},
		{name: "array single line", input: "a = [\"x\" , 'y']\n"},
		{name: "tabs as whitespace", input: "a\t=\t1\n"},
		{name: "table and contiguous array of tables", input: "[t]\nx = 1\n[[b]]\ny = 1\n[[b]]\ny = 2\n[u]\nz = 1\n"},
		{name: "spaces inside table brackets", input: "[ t ]\nx = 1\n[[ b ]]\n"},
		{name: "bare keys", input: "A_b-1 = 1\n"},
		{name: "no final newline", input: "a = 1"},
		// rule: dotted keys and tables
		{name: "dotted key", input: "a.b = 1\n", code: "profile-violation", line: 1},
		{name: "dotted table header", input: "[a.b]\nx = 1\n", code: "profile-violation", line: 1},
		{name: "dotted array-of-tables header", input: "[[a.b]]\n", code: "profile-violation", line: 1},
		{name: "quoted key", input: "\"a\" = 1\n", code: "profile-violation", line: 1},
		{name: "literal quoted key", input: "'a' = 1\n", code: "profile-violation", line: 1},
		{name: "quoted table header", input: "[\"a\"]\n", code: "profile-violation", line: 1},
		{name: "non-ascii key", input: "é = 1\n", code: "profile-violation", line: 1},
		{name: "empty key", input: "= 1\n", code: "profile-violation", line: 1},
		// rule: inline tables
		{name: "inline table", input: "a = { b = 1 }\n", code: "profile-violation", line: 1},
		{name: "inline table in array", input: "a = [{ b = 1 }]\n", code: "profile-violation", line: 1},
		// rule: split tables and interleaved arrays
		{name: "split table", input: "[a]\nx = 1\n[b]\ny = 1\n[a]\nz = 1\n", code: "profile-violation", line: 5},
		{name: "interleaved arrays of tables", input: "[[t]]\nx = 1\n[u]\ny = 1\n[[t]]\nx = 2\n", code: "profile-violation", line: 5},
		{name: "table redefined as array", input: "[a]\n[[a]]\n", code: "profile-violation", line: 2},
		{name: "array redefined as table", input: "[[a]]\n[a]\n", code: "profile-violation", line: 2},
		// rule: duplicate keys
		{name: "duplicate key", input: "a = 1\na = 2\n", code: "profile-violation", line: 2},
		{name: "duplicate key in table", input: "[t]\na = 1\na = 2\n", code: "profile-violation", line: 3},
		{name: "same key in different tables", input: "[t]\na = 1\n[u]\na = 2\n"},
		{name: "same key in array entries", input: "[[t]]\na = 1\n[[t]]\na = 2\n"},
		{name: "key named like a table", input: "[t]\n[u]\n", code: ""},
		{name: "root key then table of same name", input: "t = 1\n[t]\n", code: "profile-violation", line: 2},
		// rule: values
		{name: "float", input: "a = 1.5\n", code: "profile-violation", line: 1},
		{name: "exponent float", input: "a = 1e3\n", code: "profile-violation", line: 1},
		{name: "inf", input: "a = inf\n", code: "profile-violation", line: 1},
		{name: "signed inf", input: "a = -inf\n", code: "profile-violation", line: 1},
		{name: "nan", input: "a = nan\n", code: "profile-violation", line: 1},
		{name: "date", input: "a = 1979-05-27\n", code: "profile-violation", line: 1},
		{name: "datetime", input: "a = 1979-05-27T07:32:00Z\n", code: "profile-violation", line: 1},
		{name: "time", input: "a = 07:32:00\n", code: "profile-violation", line: 1},
		{name: "multi-line basic string", input: "a = \"\"\"x\"\"\"\n", code: "profile-violation", line: 1},
		{name: "multi-line literal string", input: "a = '''x'''\n", code: "profile-violation", line: 1},
		{name: "hexadecimal", input: "a = 0xFF\n", code: "profile-violation", line: 1},
		{name: "octal", input: "a = 0o7\n", code: "profile-violation", line: 1},
		{name: "binary", input: "a = 0b1\n", code: "profile-violation", line: 1},
		{name: "plus sign", input: "a = +5\n", code: "profile-violation", line: 1},
		{name: "underscore", input: "a = 1_000\n", code: "profile-violation", line: 1},
		{name: "leading zero", input: "a = 01\n", code: "profile-violation", line: 1},
		{name: "integer overflow", input: "a = 9223372036854775808\n", code: "profile-violation", line: 1},
		{name: "integer underflow", input: "a = -9223372036854775809\n", code: "profile-violation", line: 1},
		{name: "bare word value", input: "a = yes\n", code: "profile-violation", line: 1},
		{name: "capitalised boolean", input: "a = True\n", code: "profile-violation", line: 1},
		{name: "missing value", input: "a =\n", code: "profile-violation", line: 1},
		{name: "missing equals", input: "a 1\n", code: "profile-violation", line: 1},
		// rule: arrays hold only strings
		{name: "array of integers", input: "a = [1, 2]\n", code: "profile-violation", line: 1},
		{name: "array of booleans", input: "a = [true]\n", code: "profile-violation", line: 1},
		{name: "mixed array", input: "a = [\"x\", 1]\n", code: "profile-violation", line: 1},
		{name: "nested array", input: "a = [[\"x\"]]\n", code: "profile-violation", line: 1},
		{name: "array of arrays multi-line", input: "a = [\n  [\"x\"],\n]\n", code: "profile-violation", line: 2},
		{name: "unterminated array", input: "a = [\"x\"\n", code: "profile-violation", line: 1},
		{name: "array missing comma", input: "a = [\"x\" \"y\"]\n", code: "profile-violation", line: 1},
		{name: "array double comma", input: "a = [\"x\",,]\n", code: "profile-violation", line: 1},
		{name: "array leading comma", input: "a = [,\"x\"]\n", code: "profile-violation", line: 1},
		// strings
		{name: "unterminated string", input: "a = \"x\n", code: "profile-violation", line: 1},
		{name: "unterminated literal string", input: "a = 'x\n", code: "profile-violation", line: 1},
		{name: "invalid escape", input: "a = \"\\q\"\n", code: "profile-violation", line: 1},
		{name: "toml 1.1 hex escape", input: "a = \"\\x41\"\n", code: "profile-violation", line: 1},
		{name: "toml 1.1 e escape", input: "a = \"\\e\"\n", code: "profile-violation", line: 1},
		{name: "short unicode escape", input: "a = \"\\u00e\"\n", code: "profile-violation", line: 1},
		{name: "surrogate escape", input: "a = \"\\ud800\"\n", code: "profile-violation", line: 1},
		{name: "out of range escape", input: "a = \"\\U00110000\"\n", code: "profile-violation", line: 1},
		{name: "control character in string", input: "a = \"x\x01y\"\n", code: "profile-violation", line: 1},
		{name: "delete character in string", input: "a = \"x\x7fy\"\n", code: "profile-violation", line: 1},
		{name: "control character in literal string", input: "a = 'x\x01y'\n", code: "profile-violation", line: 1},
		{name: "trailing characters after value", input: "a = 1 2\n", code: "profile-violation", line: 1},
		{name: "trailing characters after string", input: "a = \"x\" y\n", code: "profile-violation", line: 1},
		{name: "trailing characters after table header", input: "[t] x\n", code: "profile-violation", line: 1},
		{name: "unterminated table header", input: "[t\n", code: "profile-violation", line: 1},
		{name: "array table header unbalanced", input: "[[t]\n", code: "profile-violation", line: 1},
		{name: "empty table name", input: "[]\n", code: "profile-violation", line: 1},
		// encoding rules
		{name: "byte order mark", input: "\ufeffa = 1\n", code: "profile-violation", line: 1},
		{name: "NUL byte", input: "a = 1\n# x\x00y\n", code: "profile-violation", line: 2},
		{name: "NUL inside string", input: "a = \"x\x00y\"\n", code: "profile-violation", line: 1},
		{name: "invalid UTF-8", input: "a = \"\xff\"\n", code: "profile-violation", line: 1},
		{name: "lone carriage return", input: "a = 1\rb = 2\n", code: "profile-violation", line: 1},
		{name: "control character in comment", input: "# x\x01y\na = 1\n", code: "profile-violation", line: 1},
		{name: "delete character in comment", input: "# x\x7fy\na = 1\n", code: "profile-violation", line: 1},
		{name: "quote after a bare key", input: "a\"b\" = 1\n", code: "profile-violation", line: 1},
		{name: "literal quote after a bare key", input: "a'b' = 1\n", code: "profile-violation", line: 1},
		{name: "more than 64 KiB", input: "# " + strings.Repeat("x", 64*1024) + "\n", code: "limit-exceeded", line: 0},
		{name: "exactly 64 KiB", input: "#" + strings.Repeat("x", 64*1024-2) + "\n"},
	}
}

// whyRejected names the rule each rejection must report, so removing one rule is not hidden by
// another rule rejecting the same input.
var whyRejected = map[string]string{
	"dotted key": "dotted", "dotted table header": "dotted", "dotted array-of-tables header": "dotted",
	"quoted key": "quoted", "literal quoted key": "quoted", "quoted table header": "quoted", "non-ascii key": "must use only",
	"empty key": "must use only", "inline table": "inline tables", "inline table in array": "inline tables",
	"split table": "defined twice", "interleaved arrays of tables": "contiguous", "table redefined as array": "redefines the table",
	"array redefined as table": "redefines the array of tables", "duplicate key": "duplicate key", "duplicate key in table": "duplicate key",
	"root key then table of same name": "already a key",
	"float":                            "floats", "exponent float": "floats", "inf": "floats", "signed inf": "floats", "nan": "floats",
	"date": "dates and times", "datetime": "dates and times", "time": "dates and times",
	"multi-line basic string": "multi-line", "multi-line literal string": "multi-line",
	"hexadecimal": "hexadecimal", "octal": "hexadecimal", "binary": "hexadecimal", "plus sign": "plus sign", "underscore": "digit separators",
	"leading zero": "leading zeros", "integer overflow": "64-bit", "integer underflow": "64-bit", "bare word value": "unsupported value",
	"capitalised boolean": "unsupported value", "missing value": "missing value", "missing equals": "expected =",
	"array of integers": "only strings", "array of booleans": "only strings", "mixed array": "only strings", "nested array": "nested arrays",
	"array of arrays multi-line": "nested arrays", "unterminated array": "not closed", "array missing comma": "comma or ]",
	"array double comma": "missing before the comma", "array leading comma": "missing before the comma",
	"unterminated string": "not terminated", "unterminated literal string": "not terminated", "invalid escape": "invalid escape",
	"toml 1.1 hex escape": "invalid escape", "toml 1.1 e escape": "invalid escape", "short unicode escape": "unicode escape",
	"surrogate escape": "unicode escape", "out of range escape": "unicode escape", "control character in string": "control character",
	"delete character in string": "control character", "control character in literal string": "control character",
	"trailing characters after value": "after the value", "trailing characters after string": "after the value",
	"trailing characters after table header": "after the value", "unterminated table header": "not closed", "array table header unbalanced": "not closed",
	"empty table name": "must use only", "byte order mark": "byte order mark", "NUL byte": "NUL", "NUL inside string": "NUL",
	"invalid UTF-8": "UTF-8", "delete character in comment": "comment", "quote after a bare key": "quoted", "literal quote after a bare key": "quoted", "lone carriage return": "carriage return", "control character in comment": "comment",
	"more than 64 KiB": "byte limit",
}

func TestStrictTOMLProfile(t *testing.T) {
	for _, c := range tomlCases() {
		t.Run(c.name, func(t *testing.T) {
			doc, findings := ParseTOML([]byte(c.input))
			if c.code == "" {
				if len(findings) != 0 || doc == nil {
					t.Fatalf("rejected: %+v", findings)
				}
				return
			}
			if len(findings) != 1 || findings[0].Code != c.code || findings[0].Severity != "error" {
				t.Fatalf("want one %s finding, got %+v", c.code, findings)
			}
			if c.line > 0 && !strings.HasPrefix(findings[0].Message, "line "+itoa(c.line)+":") {
				t.Fatalf("finding names the wrong line: %q", findings[0].Message)
			}
			if doc != nil {
				t.Fatalf("a rejected document returned data: %v", doc)
			}
			why, ok := whyRejected[c.name]
			if !ok {
				t.Fatalf("no expected reason for %q", c.name)
			}
			if !strings.Contains(findings[0].Message, why) {
				t.Fatalf("message %q does not name the rule %q", findings[0].Message, why)
			}
		})
	}
}

func TestTOMLStopsAtTheFindingLimit(t *testing.T) {
	_, findings := ParseTOML([]byte(strings.Repeat("a.b = 1\n", 500)))
	if len(findings) != MaxFindings+1 {
		t.Fatalf("got %d findings", len(findings))
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func TestTOMLParsesTheModelShape(t *testing.T) {
	doc, findings := ParseTOML([]byte(`schema_version = 1
[project]
name = "x y"
[[build]]
id = "a"
argv = ["sh", 'b']
timeout_seconds = 900
[[build]]
id = "b"
network = "fetch"
[outputs]
checksums = "SHA256SUMS"
flag = true
`))
	if len(findings) != 0 {
		t.Fatal(findings)
	}
	want := map[string]any{
		"schema_version": int64(1),
		"project":        map[string]any{"name": "x y"},
		"build": []any{
			map[string]any{"id": "a", "argv": []any{"sh", "b"}, "timeout_seconds": int64(900)},
			map[string]any{"id": "b", "network": "fetch"},
		},
		"outputs": map[string]any{"checksums": "SHA256SUMS", "flag": true},
	}
	if !reflect.DeepEqual(doc, want) {
		t.Fatalf("got %#v", doc)
	}
}

func TestTOMLReportsEveryViolationOnce(t *testing.T) {
	_, findings := ParseTOML([]byte("a.b = 1\nc = { d = 1 }\ne = 1.5\n[x]\n[x]\nf = 1\nf = 2\n"))
	if len(findings) != 5 {
		t.Fatalf("want 5 findings, got %+v", findings)
	}
}

func TestTOMLArrayErrorDoesNotCascade(t *testing.T) {
	_, findings := ParseTOML([]byte("a = [\n  \"x\",\n  1,\n  \"y\",\n]\nb = 2\n"))
	if len(findings) != 1 {
		t.Fatalf("want one finding, got %+v", findings)
	}
}

func TestTOMLWorkedExamplesParse(t *testing.T) {
	files, err := filepath.Glob("../../specs/026-release-process/examples/*.release.toml")
	if err != nil || len(files) != 6 {
		t.Fatalf("examples: %v %v", files, err)
	}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if _, findings := ParseTOML(raw); len(findings) != 0 {
			t.Errorf("%s: %+v", f, findings)
		}
	}
}
