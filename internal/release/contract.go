// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// MaxContractBytes is the size limit of a contract file (spec limits table).
const MaxContractBytes = 64 * 1024

// maxTOMLFindings stops the parser once more findings than a report can hold exist.
const maxTOMLFindings = MaxFindings + 1

// ParseTOML parses the strict TOML profile of contracts/config.md: top-level tables and
// contiguous top-level arrays of tables whose keys hold basic or literal strings, decimal
// integers, booleans and arrays of strings. It returns the document (tables as
// map[string]any, arrays of tables as []any of map[string]any, arrays of strings as []any of
// string, integers as int64) or, when anything violates the profile, nil and findings.
func ParseTOML(data []byte) (map[string]any, []Finding) {
	if len(data) > MaxContractBytes {
		return nil, []Finding{errorFinding("limit-exceeded", "", fmt.Sprintf("contract file exceeds the %d byte limit", MaxContractBytes))}
	}
	if finding, ok := checkEncoding(data); !ok {
		return nil, []Finding{finding}
	}
	p := &tomlParser{src: data, doc: map[string]any{}, kinds: map[string]string{}}
	p.cur = p.doc
	p.run()
	if len(p.findings) > 0 {
		return nil, p.findings
	}
	return p.doc, nil
}

func lineAt(data []byte, offset int) int { return 1 + bytes.Count(data[:offset], []byte{'\n'}) }

func profileFinding(data []byte, offset int, message string) Finding {
	line := lineAt(data, offset)
	f := errorFinding("profile-violation", "", fmt.Sprintf("line %d: %s", line, message))
	f.line = line
	return f
}

// checkEncoding enforces the whole-file rules: no BOM, NUL, invalid UTF-8 or bare carriage return.
func checkEncoding(data []byte) (Finding, bool) {
	if bytes.HasPrefix(data, []byte("\xef\xbb\xbf")) {
		return profileFinding(data, 0, "a byte order mark is not allowed"), false
	}
	if i := bytes.IndexByte(data, 0); i >= 0 {
		return profileFinding(data, i, "a NUL byte is not allowed"), false
	}
	if !utf8.Valid(data) {
		for i := 0; i < len(data); {
			r, size := utf8.DecodeRune(data[i:])
			if r == utf8.RuneError && size == 1 {
				return profileFinding(data, i, "the file is not valid UTF-8"), false
			}
			i += size
		}
	}
	for i, b := range data {
		if b == '\r' && (i+1 >= len(data) || data[i+1] != '\n') {
			return profileFinding(data, i, "a carriage return must be part of a CRLF line end"), false
		}
	}
	return Finding{}, true
}

type tomlParser struct {
	src        []byte
	pos        int
	doc        map[string]any
	cur        map[string]any
	kinds      map[string]string // top-level table name -> "table" or "array"
	lastHeader string
	findings   []Finding
}

// tomlError carries the offset a violation is reported at.
type tomlError struct {
	offset  int
	message string
}

func (p *tomlParser) errAt(offset int, format string, args ...any) *tomlError {
	return &tomlError{offset, fmt.Sprintf(format, args...)}
}

func (p *tomlParser) report(e *tomlError) {
	p.findings = append(p.findings, profileFinding(p.src, e.offset, e.message))
}

func (p *tomlParser) run() {
	for p.pos < len(p.src) && len(p.findings) < maxTOMLFindings {
		start := p.pos
		if err := p.statement(); err != nil {
			p.report(err)
			p.recover(start)
		}
	}
}

func (p *tomlParser) eof() bool  { return p.pos >= len(p.src) }
func (p *tomlParser) peek() byte { return p.src[p.pos] }

func (p *tomlParser) skipSpace() {
	for !p.eof() && (p.peek() == ' ' || p.peek() == '\t') {
		p.pos++
	}
}

// endOfLine consumes an optional comment and the line end.
func (p *tomlParser) endOfLine() *tomlError {
	p.skipSpace()
	if !p.eof() && p.peek() == '#' {
		if err := p.comment(); err != nil {
			return err
		}
	}
	if p.eof() {
		return nil
	}
	if p.peek() == '\n' {
		p.pos++
		return nil
	}
	if p.peek() == '\r' {
		p.pos += 2
		return nil
	}
	return p.errAt(p.pos, "unexpected characters after the value")
}

func (p *tomlParser) comment() *tomlError {
	for !p.eof() && p.peek() != '\n' && p.peek() != '\r' {
		if c := p.peek(); c < 0x20 && c != '\t' || c == 0x7f {
			return p.errAt(p.pos, "a control character is not allowed in a comment")
		}
		p.pos++
	}
	return nil
}

// recover skips the rest of the failed statement: its line and any continuation lines that do
// not start a new statement (the remainder of a multi-line array).
func (p *tomlParser) recover(start int) {
	if p.pos <= start {
		p.pos = start
	}
	// A statement that failed after consuming its line end (a semantic header error) is already done.
	if p.pos == start || p.src[p.pos-1] != '\n' {
		p.toNextLine()
	}
	for !p.eof() {
		trimmed := strings.TrimLeft(p.currentLine(), " \t")
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") && startsStatement(trimmed) {
			return
		}
		p.toNextLine()
	}
}

func (p *tomlParser) currentLine() string {
	end := bytes.IndexByte(p.src[p.pos:], '\n')
	if end < 0 {
		return string(p.src[p.pos:])
	}
	return string(p.src[p.pos : p.pos+end])
}

func (p *tomlParser) toNextLine() {
	end := bytes.IndexByte(p.src[p.pos:], '\n')
	if end < 0 {
		p.pos = len(p.src)
		return
	}
	p.pos += end + 1
}

func startsStatement(line string) bool {
	if strings.HasPrefix(line, "[") {
		rest := strings.TrimLeft(strings.TrimPrefix(strings.TrimPrefix(line, "["), "["), " \t")
		return rest != "" && (isBare(rest[0]) || rest[0] == ']')
	}
	i := 0
	for i < len(line) && (isBare(line[i]) || line[i] == '.' || line[i] == '"' || line[i] == '\'') {
		i++
	}
	rest := strings.TrimLeft(line[i:], " \t")
	return i > 0 && strings.HasPrefix(rest, "=")
}

func isBare(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-'
}

func (p *tomlParser) statement() *tomlError {
	p.skipSpace()
	if p.eof() {
		return nil
	}
	switch p.peek() {
	case '\n':
		p.pos++
		return nil
	case '\r':
		p.pos += 2
		return nil
	case '#':
		if err := p.comment(); err != nil {
			return err
		}
		return p.endOfLine()
	case '[':
		return p.header()
	}
	return p.keyValue()
}

// bareKey reads one bare key and rejects dotted and quoted forms.
func (p *tomlParser) bareKey(what string) (string, *tomlError) {
	p.skipSpace()
	start := p.pos
	for !p.eof() && isBare(p.peek()) {
		p.pos++
	}
	key := string(p.src[start:p.pos])
	if !p.eof() {
		switch p.peek() {
		case '.':
			return "", p.errAt(p.pos, "dotted keys and tables are not allowed")
		case '"', '\'':
			return "", p.errAt(p.pos, "quoted keys are not allowed; use a bare key")
		}
	}
	if key == "" {
		if p.eof() || p.peek() == '\n' || p.peek() == '\r' {
			return "", p.errAt(p.pos, "missing %s", what)
		}
		if p.peek() == '"' || p.peek() == '\'' {
			return "", p.errAt(p.pos, "quoted keys are not allowed; use a bare key")
		}
		return "", p.errAt(p.pos, "a %s must use only A-Z, a-z, 0-9, _ and -", what)
	}
	return key, nil
}

func (p *tomlParser) header() *tomlError {
	open := p.pos
	p.pos++
	array := !p.eof() && p.peek() == '['
	if array {
		p.pos++
	}
	name, err := p.bareKey("table name")
	if err != nil {
		return err
	}
	p.skipSpace()
	closing := 1
	if array {
		closing = 2
	}
	for i := 0; i < closing; i++ {
		if p.eof() || p.peek() != ']' {
			return p.errAt(p.pos, "the table header is not closed")
		}
		p.pos++
	}
	if err := p.endOfLine(); err != nil {
		return err
	}
	kind, seen := p.kinds[name]
	switch {
	case !array && seen && kind == "table":
		return p.errAt(open, "table [%s] is defined twice; a table appears once", name)
	case !array && seen:
		return p.errAt(open, "[%s] redefines the array of tables [[%s]]", name, name)
	case array && seen && kind == "table":
		return p.errAt(open, "[[%s]] redefines the table [%s]", name, name)
	case array && seen && p.lastHeader != name:
		return p.errAt(open, "the entries of [[%s]] must be contiguous", name)
	}
	if _, clash := p.doc[name]; clash && !seen {
		return p.errAt(open, "%s is already a key of the document", name)
	}
	entry := map[string]any{}
	if array {
		list, _ := p.doc[name].([]any)
		p.doc[name] = append(list, entry)
		p.kinds[name] = "array"
	} else {
		p.doc[name] = entry
		p.kinds[name] = "table"
	}
	p.cur = entry
	p.lastHeader = name
	return nil
}

func (p *tomlParser) keyValue() *tomlError {
	key, err := p.bareKey("key")
	if err != nil {
		return err
	}
	keyOffset := p.pos
	p.skipSpace()
	if p.eof() || p.peek() != '=' {
		return p.errAt(p.pos, "expected = after the key %s", key)
	}
	p.pos++
	p.skipSpace()
	value, err := p.value()
	if err != nil {
		return err
	}
	if err := p.endOfLine(); err != nil {
		return err
	}
	if _, dup := p.cur[key]; dup {
		return p.errAt(keyOffset, "duplicate key %s", key)
	}
	p.cur[key] = value
	return nil
}

func (p *tomlParser) value() (any, *tomlError) {
	if p.eof() || p.peek() == '\n' || p.peek() == '\r' || p.peek() == '#' {
		return nil, p.errAt(p.pos, "missing value")
	}
	switch p.peek() {
	case '"', '\'':
		return p.str()
	case '[':
		return p.array()
	case '{':
		return nil, p.errAt(p.pos, "inline tables are not allowed")
	}
	return p.scalar()
}

func (p *tomlParser) str() (string, *tomlError) {
	quote := p.peek()
	if bytes.HasPrefix(p.src[p.pos:], bytes.Repeat([]byte{quote}, 3)) {
		return "", p.errAt(p.pos, "multi-line strings are not allowed")
	}
	p.pos++
	var out strings.Builder
	for {
		if p.eof() || p.peek() == '\n' || p.peek() == '\r' {
			return "", p.errAt(p.pos, "the string is not terminated on its line")
		}
		c := p.peek()
		switch {
		case c == quote:
			p.pos++
			return out.String(), nil
		case c < 0x20 && c != '\t' || c == 0x7f:
			return "", p.errAt(p.pos, "a control character is not allowed in a string")
		case c == '\\' && quote == '"':
			if err := p.escape(&out); err != nil {
				return "", err
			}
		default:
			r, size := utf8.DecodeRune(p.src[p.pos:])
			out.WriteRune(r)
			p.pos += size
		}
	}
}

func (p *tomlParser) escape(out *strings.Builder) *tomlError {
	at := p.pos
	p.pos++
	if p.eof() {
		return p.errAt(at, "the string is not terminated on its line")
	}
	c := p.peek()
	p.pos++
	simple := map[byte]string{'b': "\b", 't': "\t", 'n': "\n", 'f': "\f", 'r': "\r", '"': "\"", '\\': "\\"}
	if s, ok := simple[c]; ok {
		out.WriteString(s)
		return nil
	}
	digits := 0
	switch c {
	case 'u':
		digits = 4
	case 'U':
		digits = 8
	default:
		return p.errAt(at, "invalid escape sequence in a string")
	}
	if p.pos+digits > len(p.src) {
		return p.errAt(at, "invalid unicode escape in a string")
	}
	n, err := strconv.ParseUint(string(p.src[p.pos:p.pos+digits]), 16, 32)
	if err != nil || !utf8.ValidRune(rune(n)) {
		return p.errAt(at, "invalid unicode escape in a string")
	}
	p.pos += digits
	out.WriteRune(rune(n))
	return nil
}

func (p *tomlParser) array() (any, *tomlError) {
	open := p.pos
	p.pos++
	items := []any{}
	for {
		if err := p.arraySpace(); err != nil {
			return nil, err
		}
		if p.eof() {
			return nil, p.errAt(open, "the array is not closed")
		}
		switch p.peek() {
		case ']':
			p.pos++
			return items, nil
		case ',':
			return nil, p.errAt(p.pos, "an array element is missing before the comma")
		case '"', '\'':
			s, err := p.str()
			if err != nil {
				return nil, err
			}
			items = append(items, s)
		case '[':
			return nil, p.errAt(p.pos, "nested arrays are not allowed")
		case '{':
			return nil, p.errAt(p.pos, "inline tables are not allowed")
		default:
			return nil, p.errAt(p.pos, "arrays may hold only strings")
		}
		if err := p.arraySpace(); err != nil {
			return nil, err
		}
		if p.eof() {
			return nil, p.errAt(open, "the array is not closed")
		}
		switch p.peek() {
		case ',':
			p.pos++
		case ']':
		default:
			return nil, p.errAt(p.pos, "expected a comma or ] after the array element")
		}
	}
}

// arraySpace skips whitespace, line ends and comments between array elements.
func (p *tomlParser) arraySpace() *tomlError {
	for !p.eof() {
		switch p.peek() {
		case ' ', '\t', '\n':
			p.pos++
		case '\r':
			p.pos += 2
		case '#':
			if err := p.comment(); err != nil {
				return err
			}
		default:
			return nil
		}
	}
	return nil
}

// scalar classifies a bare token: booleans and decimal integers are accepted, every other TOML
// scalar is a named violation.
func (p *tomlParser) scalar() (any, *tomlError) {
	start := p.pos
	for !p.eof() {
		c := p.peek()
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ',' || c == ']' || c == '#' || c == '}' {
			break
		}
		p.pos++
	}
	token := string(p.src[start:p.pos])
	switch token {
	case "":
		return nil, p.errAt(start, "missing value")
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	body := strings.TrimLeft(token, "+-")
	switch {
	case body == "inf" || body == "nan":
		return nil, p.errAt(start, "floats are not allowed")
	case allDigits(token[signLength(token):]) && token[signLength(token):] != "":
		return p.integer(start, token)
	case allDigitsOrUnderscore(body) && body != "":
		return nil, p.errAt(start, "digit separators in integers are not allowed")
	case len(body) > 1 && body[0] == '0' && (body[1] == 'x' || body[1] == 'o' || body[1] == 'b'):
		return nil, p.errAt(start, "hexadecimal, octal and binary integers are not allowed")
	case strings.ContainsRune(token, ':') || len(token) >= 8 && token[4] == '-' && allDigits(token[:4]):
		return nil, p.errAt(start, "dates and times are not allowed")
	case body != "" && body[0] >= '0' && body[0] <= '9':
		return nil, p.errAt(start, "floats are not allowed")
	}
	return nil, p.errAt(start, "unsupported value; use a string, a decimal integer, a boolean or an array of strings")
}

func signLength(token string) int {
	if token != "" && (token[0] == '+' || token[0] == '-') {
		return 1
	}
	return 0
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func allDigitsOrUnderscore(s string) bool {
	return strings.Trim(s, "0123456789_") == ""
}

func (p *tomlParser) integer(offset int, token string) (any, *tomlError) {
	if token[0] == '+' {
		return nil, p.errAt(offset, "a plus sign in an integer is not allowed")
	}
	digits := token[signLength(token):]
	if len(digits) > 1 && digits[0] == '0' {
		return nil, p.errAt(offset, "integers must not have leading zeros")
	}
	n, err := strconv.ParseInt(token, 10, 64)
	if err != nil {
		return nil, p.errAt(offset, "the integer is outside the 64-bit range")
	}
	return n, nil
}
