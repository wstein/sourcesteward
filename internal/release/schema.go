// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	sourcesteward "github.com/wstein/sourcesteward"
)

// The contract schema is the source of truth of the model. This file interprets the keyword
// subset that schemas/v1/release-config.schema.json uses, so the linter and the published
// schema cannot drift; the repository's contract tests compare it with the test-only JSON
// Schema library on the whole fixture corpus.

var supportedKeywords = map[string]bool{
	"$schema": true, "$id": true, "title": true, "description": true, "$defs": true, "$ref": true,
	"type": true, "const": true, "enum": true, "pattern": true, "minLength": true, "maxLength": true,
	"minimum": true, "maximum": true, "minItems": true, "maxItems": true, "items": true,
	"required": true, "properties": true, "additionalProperties": true,
	"allOf": true, "anyOf": true, "not": true, "if": true, "then": true, "else": true,
}

var (
	configSchemaOnce sync.Once
	configSchema     map[string]any
	patternCache     sync.Map
)

func loadConfigSchema() map[string]any {
	configSchemaOnce.Do(func() {
		if err := json.Unmarshal(sourcesteward.ReleaseConfigSchema, &configSchema); err != nil {
			panic("embedded release-config schema is invalid: " + err.Error())
		}
	})
	return configSchema
}

// checkSchemaKeywords reports a keyword of the embedded schema that the interpreter ignores.
func checkSchemaKeywords() error {
	var walk func(node any, path string) error
	walk = func(node any, path string) error {
		m, ok := node.(map[string]any)
		if !ok {
			return nil
		}
		for key := range m {
			if !supportedKeywords[key] {
				return fmt.Errorf("unsupported schema keyword %q at %s", key, path)
			}
		}
		for _, key := range []string{"properties", "$defs"} {
			if children, ok := m[key].(map[string]any); ok {
				for name, child := range children {
					if err := walk(child, path+"/"+key+"/"+name); err != nil {
						return err
					}
				}
			}
		}
		for _, key := range []string{"items", "additionalProperties", "not", "if", "then", "else"} {
			if err := walk(m[key], path+"/"+key); err != nil {
				return err
			}
		}
		for _, key := range []string{"allOf", "anyOf"} {
			if list, ok := m[key].([]any); ok {
				for i, child := range list {
					if err := walk(child, fmt.Sprintf("%s/%s/%d", path, key, i)); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	return walk(loadConfigSchema(), "")
}

// validateConfigSchema validates a decoded contract document (strings, integers, booleans,
// arrays and objects) against the schema and returns the findings ordered by JSON pointer.
// Exceeding a maximum item or length count is limit-exceeded; every other violation is
// schema-violation.
func validateConfigSchema(doc any) []Finding {
	w := &schemaWalker{root: loadConfigSchema(), seen: map[string]bool{}}
	w.check(w.root, doc, "")
	sortFindings(w.out)
	return w.out
}

type schemaWalker struct {
	root map[string]any
	out  []Finding
	seen map[string]bool
}

func (w *schemaWalker) add(code, pointer, message string) {
	key := code + "\x00" + pointer + "\x00" + message
	if w.seen[key] {
		return
	}
	w.seen[key] = true
	w.out = append(w.out, errorFinding(code, pointer, message))
}

func pointerJoin(pointer, key string) string {
	return pointer + "/" + strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}

func (w *schemaWalker) valid(schema map[string]any, v any) bool {
	scratch := &schemaWalker{root: w.root, seen: map[string]bool{}}
	scratch.check(schema, v, "")
	return len(scratch.out) == 0
}

func (w *schemaWalker) resolve(ref string) map[string]any {
	name, ok := strings.CutPrefix(ref, "#/$defs/")
	if !ok {
		panic("unsupported schema reference " + ref)
	}
	defs, _ := w.root["$defs"].(map[string]any)
	target, ok := defs[name].(map[string]any)
	if !ok {
		panic("unresolved schema reference " + ref)
	}
	return target
}

func (w *schemaWalker) check(schema map[string]any, v any, pointer string) {
	if ref, ok := schema["$ref"].(string); ok {
		w.check(w.resolve(ref), v, pointer)
	}
	if want, ok := schema["type"].(string); ok && !typeMatches(want, v) {
		w.add("schema-violation", pointer, fmt.Sprintf("expected %s, got %s", want, typeName(v)))
		return
	}
	if c, ok := schema["const"]; ok && !jsonEqual(c, v) {
		w.add("schema-violation", pointer, fmt.Sprintf("must be %s", describe(c)))
	}
	if e, ok := schema["enum"].([]any); ok {
		found := false
		for _, candidate := range e {
			found = found || jsonEqual(candidate, v)
		}
		if !found {
			names := make([]string, len(e))
			for i, candidate := range e {
				names[i] = describe(candidate)
			}
			w.add("schema-violation", pointer, "must be one of "+strings.Join(names, ", "))
		}
	}
	switch v := v.(type) {
	case string:
		w.checkString(schema, v, pointer)
	case int64, int, float64:
		n := toFloat(v)
		if limit, ok := schema["minimum"].(float64); ok && n < limit {
			w.add("schema-violation", pointer, fmt.Sprintf("must be at least %v", limit))
		}
		if limit, ok := schema["maximum"].(float64); ok && n > limit {
			w.add("schema-violation", pointer, fmt.Sprintf("must be at most %v", limit))
		}
	case []any:
		w.checkArray(schema, v, pointer)
	case map[string]any:
		w.checkObject(schema, v, pointer)
	}
	w.checkCombinators(schema, v, pointer)
}

func (w *schemaWalker) checkString(schema map[string]any, v, pointer string) {
	length := utf8.RuneCountInString(v)
	if limit, ok := schema["minLength"].(float64); ok && float64(length) < limit {
		w.add("schema-violation", pointer, fmt.Sprintf("must have at least %v characters", limit))
	}
	if limit, ok := schema["maxLength"].(float64); ok && float64(length) > limit {
		w.add("limit-exceeded", pointer, fmt.Sprintf("must have at most %v characters, has %d", limit, length))
	}
	if pattern, ok := schema["pattern"].(string); ok && !compiled(pattern).MatchString(v) {
		w.add("schema-violation", pointer, fmt.Sprintf("%s does not match the pattern %s", describe(v), pattern))
	}
}

func (w *schemaWalker) checkArray(schema map[string]any, v []any, pointer string) {
	if limit, ok := schema["minItems"].(float64); ok && float64(len(v)) < limit {
		w.add("schema-violation", pointer, fmt.Sprintf("must have at least %v items", limit))
	}
	if limit, ok := schema["maxItems"].(float64); ok && float64(len(v)) > limit {
		w.add("limit-exceeded", pointer, fmt.Sprintf("must have at most %v items, has %d", limit, len(v)))
	}
	if items, ok := schema["items"].(map[string]any); ok {
		for i, item := range v {
			w.check(items, item, fmt.Sprintf("%s/%d", pointer, i))
		}
	}
}

func (w *schemaWalker) checkObject(schema map[string]any, v map[string]any, pointer string) {
	if required, ok := schema["required"].([]any); ok {
		for _, name := range required {
			if _, present := v[name.(string)]; !present {
				w.add("schema-violation", pointer, fmt.Sprintf("missing property %s", name))
			}
		}
	}
	properties, _ := schema["properties"].(map[string]any)
	names := make([]string, 0, len(v))
	for name := range v {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if sub, ok := properties[name].(map[string]any); ok {
			w.check(sub, v[name], pointerJoin(pointer, name))
		} else if schema["additionalProperties"] == false {
			w.add("schema-violation", pointerJoin(pointer, name), "unknown property")
		}
	}
}

func (w *schemaWalker) checkCombinators(schema map[string]any, v any, pointer string) {
	if all, ok := schema["allOf"].([]any); ok {
		for _, sub := range all {
			w.check(sub.(map[string]any), v, pointer)
		}
	}
	if options, ok := schema["anyOf"].([]any); ok {
		matched := false
		for _, sub := range options {
			matched = matched || w.valid(sub.(map[string]any), v)
		}
		if !matched {
			w.add("schema-violation", pointer, "matches none of the allowed forms")
		}
	}
	if cond, ok := schema["if"].(map[string]any); ok {
		if w.valid(cond, v) {
			if then, ok := schema["then"].(map[string]any); ok {
				w.check(then, v, pointer)
			}
		} else if otherwise, ok := schema["else"].(map[string]any); ok {
			w.check(otherwise, v, pointer)
		}
	}
	if not, ok := schema["not"].(map[string]any); ok && w.valid(not, v) {
		w.reportForbidden(not, v, pointer)
	}
}

// reportForbidden explains a failed "not": the properties that may not be present together
// with the others are named individually, at their own pointer.
func (w *schemaWalker) reportForbidden(not map[string]any, v any, pointer string) {
	object, isObject := v.(map[string]any)
	var names []string
	collect := func(schema map[string]any) {
		if required, ok := schema["required"].([]any); ok {
			for _, name := range required {
				names = append(names, name.(string))
			}
		}
	}
	collect(not)
	if options, ok := not["anyOf"].([]any); ok {
		for _, option := range options {
			collect(option.(map[string]any))
		}
	}
	reported := false
	if isObject {
		sort.Strings(names)
		for _, name := range names {
			if _, present := object[name]; present {
				w.add("schema-violation", pointerJoin(pointer, name), "property is not allowed with this configuration")
				reported = true
			}
		}
	}
	if !reported {
		w.add("schema-violation", pointer, "the value is not allowed in this configuration")
	}
}

func compiled(pattern string) *regexp.Regexp {
	if re, ok := patternCache.Load(pattern); ok {
		return re.(*regexp.Regexp)
	}
	re := regexp.MustCompile(pattern)
	patternCache.Store(pattern, re)
	return re
}

func typeName(v any) string {
	switch v := v.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case bool:
		return "boolean"
	case int64, int:
		return "integer"
	case float64:
		if v == math.Trunc(v) && math.Abs(v) < 1e15 {
			return "integer"
		}
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return fmt.Sprintf("%T", v)
}

func typeMatches(want string, v any) bool {
	have := typeName(v)
	return have == want
}

func toFloat(v any) float64 {
	switch v := v.(type) {
	case int64:
		return float64(v)
	case int:
		return float64(v)
	case float64:
		return v
	}
	return math.NaN()
}

func jsonEqual(a, b any) bool {
	if typeName(a) == "integer" && typeName(b) == "integer" {
		return toFloat(a) == toFloat(b)
	}
	return reflect.DeepEqual(a, b)
}

// describe renders a value for a message; long strings are cut so a hostile value is not echoed whole.
func describe(v any) string {
	if s, ok := v.(string); ok {
		runes := []rune(s)
		if len(runes) > 48 {
			return fmt.Sprintf("%q...", string(runes[:48]))
		}
		return fmt.Sprintf("%q", s)
	}
	out, err := json.Marshal(v)
	if err != nil {
		return typeName(v)
	}
	return string(out)
}

// ValidateSchema validates a decoded contract document against the contract schema.
func ValidateSchema(doc any) []Finding { return validateConfigSchema(doc) }
