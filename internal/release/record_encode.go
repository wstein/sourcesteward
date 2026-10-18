// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"bytes"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// EncodeReleaseDocument returns the project-specific canonical form of a typed
// record, manifest or OCI result. It does not mutate the caller's slices.
func EncodeReleaseDocument(document any) ([]byte, error) {
	switch document.(type) {
	case ReleaseRecord, *ReleaseRecord, BuildManifest, *BuildManifest, OCIResult, *OCIResult:
	default:
		return nil, fmt.Errorf("unsupported release document %T", document)
	}
	return canonicalValue(document)
}
func canonicalValue(value any) ([]byte, error) {
	node, err := recordNode(reflect.ValueOf(value))
	if err != nil {
		return nil, err
	}
	sortRecordArrays(node)
	var out bytes.Buffer
	if err := writeCanonical(&out, node, 0); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}
func recordNode(v reflect.Value) (any, error) {
	if !v.IsValid() {
		return nil, nil
	}
	if v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil, nil
		}
		return recordNode(v.Elem())
	}
	if a, ok := v.Interface().(RecordAsset); ok {
		if (a.File == nil) == (a.Image == nil) {
			return nil, fmt.Errorf("asset needs exactly one kind")
		}
		if a.File != nil {
			return recordNode(reflect.ValueOf(a.File))
		}
		return recordNode(reflect.ValueOf(a.Image))
	}
	switch v.Kind() {
	case reflect.Struct:
		m := map[string]any{}
		typ := v.Type()
		for i := 0; i < v.NumField(); i++ {
			tag := strings.Split(typ.Field(i).Tag.Get("json"), ",")
			if tag[0] == "" || tag[0] == "-" {
				return nil, fmt.Errorf("unmodeled field")
			}
			if len(tag) > 1 && v.Field(i).IsZero() {
				continue
			}
			n, e := recordNode(v.Field(i))
			if e != nil {
				return nil, e
			}
			m[tag[0]] = n
		}
		return m, nil
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return nil, fmt.Errorf("non-string key")
		}
		m := map[string]any{}
		for _, k := range v.MapKeys() {
			n, e := recordNode(v.MapIndex(k))
			if e != nil {
				return nil, e
			}
			m[k.String()] = n
		}
		return m, nil
	case reflect.Slice, reflect.Array:
		a := make([]any, v.Len())
		for i := range a {
			n, e := recordNode(v.Index(i))
			if e != nil {
				return nil, e
			}
			a[i] = n
		}
		return a, nil
	case reflect.String:
		if !utf8.ValidString(v.String()) {
			return nil, fmt.Errorf("invalid UTF-8")
		}
		return v.String(), nil
	case reflect.Int, reflect.Int64, reflect.Int32:
		return v.Int(), nil
	case reflect.Bool:
		return v.Bool(), nil
	}
	return nil, fmt.Errorf("unsupported canonical value %s", v.Kind())
}
func sortRecordArrays(node any) {
	switch n := node.(type) {
	case map[string]any:
		for k, v := range n {
			if a, ok := v.([]any); ok {
				switch k {
				case "assets":
					sort.SliceStable(a, func(i, j int) bool {
						x, y := a[i].(map[string]any), a[j].(map[string]any)
						if x["target"] != y["target"] {
							return x["target"].(string) < y["target"].(string)
						}
						return getString(x, "name") < getString(y, "name")
					})
				case "platforms":
					sort.SliceStable(a, func(i, j int) bool {
						x, y := a[i].(map[string]any), a[j].(map[string]any)
						for _, key := range []string{"os", "arch", "variant"} {
							if getString(x, key) != getString(y, key) {
								return getString(x, key) < getString(y, key)
							}
						}
						return false
					})
				case "tags":
					sort.SliceStable(a, func(i, j int) bool { return a[i].(string) < a[j].(string) })
				}
			}
			sortRecordArrays(v)
		}
	case []any:
		for _, v := range n {
			sortRecordArrays(v)
		}
	}
}
func canonicalString(out *bytes.Buffer, s string) {
	out.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			out.WriteString(`\"`)
		case '\\':
			out.WriteString(`\\`)
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\t':
			out.WriteString(`\t`)
		default:
			if r < 32 {
				fmt.Fprintf(out, `\u%04x`, r)
			} else {
				out.WriteRune(r)
			}
		}
	}
	out.WriteByte('"')
}
func writeCanonical(out *bytes.Buffer, v any, depth int) error {
	indent := func(d int) { out.WriteString(strings.Repeat("  ", d)) }
	switch n := v.(type) {
	case nil:
		out.WriteString("null")
	case string:
		canonicalString(out, n)
	case int64:
		out.WriteString(strconv.FormatInt(n, 10))
	case bool:
		out.WriteString(strconv.FormatBool(n))
	case map[string]any:
		keys := make([]string, 0, len(n))
		for k := range n {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out.WriteByte('{')
		for i, k := range keys {
			out.WriteByte('\n')
			indent(depth + 1)
			canonicalString(out, k)
			out.WriteString(": ")
			if e := writeCanonical(out, n[k], depth+1); e != nil {
				return e
			}
			if i < len(keys)-1 {
				out.WriteByte(',')
			}
		}
		if len(keys) > 0 {
			out.WriteByte('\n')
			indent(depth)
		}
		out.WriteByte('}')
	case []any:
		out.WriteByte('[')
		for i, x := range n {
			out.WriteByte('\n')
			indent(depth + 1)
			if e := writeCanonical(out, x, depth+1); e != nil {
				return e
			}
			if i < len(n)-1 {
				out.WriteByte(',')
			}
		}
		if len(n) > 0 {
			out.WriteByte('\n')
			indent(depth)
		}
		out.WriteByte(']')
	default:
		return fmt.Errorf("unsupported canonical node %T", v)
	}
	return nil
}
