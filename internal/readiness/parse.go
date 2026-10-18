// SPDX-License-Identifier: EUPL-1.2
package readiness

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

type document[T any] struct {
	raw        string
	normalized string
	digest     string
}

// RawJSON returns a new copy of the exact input bytes, including whitespace.
func (d document[T]) RawJSON() []byte { return []byte(d.raw) }

// SHA256 returns lowercase hex of the complete raw input, without normalization.
func (d document[T]) SHA256() string { return d.digest }

// Value returns a fresh typed snapshot, including all nested arrays and pointers.
func (d document[T]) Value() (v T) { _ = json.Unmarshal([]byte(d.normalized), &v); return v }
func parseDocument[T any](raw []byte, limit int, s *shape) (document[T], error) {
	v, err := strictJSON(raw, limit)
	if err != nil {
		return document[T]{}, err
	}
	if err = s.validate(v); err != nil {
		return document[T]{}, err
	}
	normalized, err := json.Marshal(v)
	if err != nil {
		return document[T]{}, fmt.Errorf("input normalization failed")
	}
	var typed T
	if err = json.Unmarshal(normalized, &typed); err != nil {
		return document[T]{}, fmt.Errorf("input type mismatch")
	}
	return document[T]{raw: string(raw), normalized: string(normalized), digest: fmt.Sprintf("%x", sha256.Sum256(raw))}, nil
}

// Size and encoding are checked before decoding; depth is checked before a
// nested container is allocated. Error messages never echo untrusted input.
func strictJSON(raw []byte, limit int) (any, error) {
	if len(raw) > limit {
		return nil, fmt.Errorf("input byte limit exceeded")
	}
	if !utf8.Valid(raw) || bytes.HasPrefix(raw, []byte{0xef, 0xbb, 0xbf}) || !validSurrogates(raw) {
		return nil, fmt.Errorf("invalid input encoding")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	v, err := readValue(d, 0)
	if err != nil {
		return nil, err
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing input")
	}
	return v, nil
}
func readValue(d *json.Decoder, depth int) (any, error) {
	token, err := d.Token()
	if err != nil {
		return nil, fmt.Errorf("malformed JSON")
	}
	switch v := token.(type) {
	case json.Delim:
		if depth >= 16 {
			return nil, fmt.Errorf("input depth limit exceeded")
		}
		switch v {
		case '{':
			m := map[string]any{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return nil, fmt.Errorf("malformed object")
				}
				k, ok := key.(string)
				if !ok || len(k) > 2048 {
					return nil, fmt.Errorf("invalid object key")
				}
				if _, exists := m[k]; exists {
					return nil, fmt.Errorf("duplicate object key")
				}
				child, err := readValue(d, depth+1)
				if err != nil {
					return nil, err
				}
				m[k] = child
			}
			close, err := d.Token()
			if err != nil || close != json.Delim('}') {
				return nil, fmt.Errorf("malformed object")
			}
			return m, nil
		case '[':
			a := []any{}
			for d.More() {
				child, err := readValue(d, depth+1)
				if err != nil {
					return nil, err
				}
				a = append(a, child)
			}
			close, err := d.Token()
			if err != nil || close != json.Delim(']') {
				return nil, fmt.Errorf("malformed array")
			}
			return a, nil
		}
		return nil, fmt.Errorf("unexpected delimiter")
	case string:
		if len(v) > 2048 {
			return nil, fmt.Errorf("decoded string byte limit exceeded")
		}
		return v, nil
	case json.Number:
		return integer(v.String())
	default:
		return v, nil
	}
}

// Accept the JSON Schema integer value space (including 1.0 and 1e0) without
// floating-point rounding or unbounded exponent expansion.
func integer(s string) (int64, error) {
	invalid := func() (int64, error) { return 0, fmt.Errorf("integer outside supported range") }
	negative := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	exponent := 0
	if at := strings.IndexAny(s, "eE"); at >= 0 {
		var err error
		exponent, err = strconv.Atoi(s[at+1:])
		if err != nil || exponent > 65536 || exponent < -65536 {
			return invalid()
		}
		s = s[:at]
	}
	if dot := strings.IndexByte(s, '.'); dot >= 0 {
		exponent -= len(s) - dot - 1
		s = s[:dot] + s[dot+1:]
	}
	s = strings.TrimLeft(s, "0")
	if s == "" {
		return 0, nil
	}
	if exponent < 0 {
		cut := -exponent
		if cut > len(s) || strings.Trim(s[len(s)-cut:], "0") != "" {
			return invalid()
		}
		s = s[:len(s)-cut]
	} else {
		if len(s)+exponent > 16 {
			return invalid()
		}
		s += strings.Repeat("0", exponent)
	}
	if len(s) > 16 {
		return invalid()
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n > 9007199254740991 {
		return invalid()
	}
	if negative {
		n = -n
	}
	return n, nil
}

// encoding/json replaces unpaired UTF-16 escapes with U+FFFD; strict inputs
// reject them rather than silently changing the declared string value.
func validSurrogates(raw []byte) bool {
	for i := 0; i < len(raw); i++ {
		if raw[i] != '"' {
			continue
		}
		i++
		for ; i < len(raw) && raw[i] != '"'; i++ {
			if raw[i] != '\\' {
				continue
			}
			i++
			if i >= len(raw) {
				return false
			}
			if raw[i] != 'u' {
				continue
			}
			if i+4 >= len(raw) {
				return false
			}
			n, e := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
			if e != nil {
				return false
			}
			i += 4
			if n >= 0xdc00 && n <= 0xdfff {
				return false
			}
			if n < 0xd800 || n > 0xdbff {
				continue
			}
			if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
				return false
			}
			low, e := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
			if e != nil || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return true
}

type shape struct {
	kind     byte
	fields   map[string]*shape
	item     *shape
	min, max int64
	pattern  *regexp.Regexp
	choices  []string
	nullable bool
	trueOnly bool
}

func obj(fields map[string]*shape) *shape { return &shape{kind: 'o', fields: fields} }
func array(item *shape, min, max int64) *shape {
	return &shape{kind: 'a', item: item, min: min, max: max}
}
func text(max int64, pattern string) *shape {
	s := &shape{kind: 's', min: 1, max: max}
	if pattern != "" {
		s.pattern = regexp.MustCompile(pattern)
	}
	return s
}
func numeric(max int64) *shape       { return &shape{kind: 'n', min: 1, max: max} }
func choice(values ...string) *shape { return &shape{kind: 's', choices: values} }
func nullable(s *shape) *shape       { copy := *s; copy.nullable = true; return &copy }
func boolean() *shape                { return &shape{kind: 'b'} }
func (s *shape) validate(v any) error {
	invalid := func() error { return fmt.Errorf("input violates closed schema") }
	if v == nil {
		if s.nullable {
			return nil
		}
		return invalid()
	}
	switch s.kind {
	case 'o':
		m, ok := v.(map[string]any)
		if !ok || len(m) != len(s.fields) {
			return invalid()
		}
		for k, child := range s.fields {
			value, ok := m[k]
			if !ok {
				return invalid()
			}
			if e := child.validate(value); e != nil {
				return e
			}
		}
	case 'a':
		a, ok := v.([]any)
		if !ok || int64(len(a)) < s.min || int64(len(a)) > s.max {
			return invalid()
		}
		seen := map[string]bool{}
		for _, v := range a {
			if e := s.item.validate(v); e != nil {
				return e
			}
			b, _ := json.Marshal(v)
			key := string(b)
			if seen[key] {
				return invalid()
			}
			seen[key] = true
		}
	case 's':
		str, ok := v.(string)
		if !ok {
			return invalid()
		}
		if len(s.choices) > 0 {
			for _, c := range s.choices {
				if str == c {
					return nil
				}
			}
			return invalid()
		}
		length := int64(utf8.RuneCountInString(str))
		if length < s.min || length > s.max || (s.pattern != nil && !s.pattern.MatchString(str)) {
			return invalid()
		}
	case 'n':
		n, ok := v.(int64)
		if !ok || n < s.min || n > s.max {
			return invalid()
		}
	case 'b':
		b, ok := v.(bool)
		if !ok || (s.trueOnly && !b) {
			return invalid()
		}
	default:
		return invalid()
	}
	return nil
}
