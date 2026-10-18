// SPDX-License-Identifier: EUPL-1.2
package readiness

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

type inputFixture struct {
	name  string
	limit int
	parse func([]byte) error
}

func inputs() []inputFixture {
	return []inputFixture{
		{"policy", 64 * 1024, func(b []byte) error { _, e := ParsePolicy(b); return e }},
		{"lock", 16 * 1024, func(b []byte) error { _, e := ParseLock(b); return e }},
		{"report", 64 * 1024, func(b []byte) error { _, e := ParseReport(b); return e }},
	}
}
func designFile(t *testing.T, name string) []byte {
	t.Helper()
	b, e := os.ReadFile(filepath.Join("../../specs/028-ready-check/contracts", name))
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func jsonBytes(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func object(t *testing.T, b []byte) map[string]any {
	t.Helper()
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var v map[string]any
	if e := d.Decode(&v); e != nil {
		t.Fatal(e)
	}
	return v
}

func TestClosedInputSyntaxAndBounds(t *testing.T) {
	for _, in := range inputs() {
		t.Run(in.name, func(t *testing.T) {
			raw := designFile(t, "examples/"+in.name+".json")
			if e := in.parse(raw); e != nil {
				t.Fatal(e)
			}
			compact := jsonBytes(t, object(t, raw))
			cases := map[string][]byte{
				"bom":             append([]byte{0xef, 0xbb, 0xbf}, raw...),
				"utf8":            bytes.Replace(compact, []byte(`"github.com"`), []byte{'"', 0xff, '"'}, 1),
				"trailing-object": append(append([]byte{}, raw...), []byte(`{}`)...),
				"trailing-scalar": append(append([]byte{}, raw...), []byte(`true`)...),
				"root-array":      []byte(`[]`), "root-null": []byte(`null`),
				"duplicate":              bytes.Replace(compact, []byte(`"schema_version":1`), []byte(`"schema_version":1,"schema_version":1`), 1),
				"escaped-duplicate":      bytes.Replace(compact, []byte(`"schema_version":1`), []byte(`"schema_version":1,"schema_vers\u0069on":1`), 1),
				"nested-duplicate":       bytes.Replace(compact, []byte(`"host":"github.com"`), []byte(`"host":"github.com","host":"github.com"`), 1),
				"overflow":               bytes.Replace(compact, []byte(`"schema_version":1`), []byte(`"schema_version":9223372036854775808`), 1),
				"fraction":               bytes.Replace(compact, []byte(`"schema_version":1`), []byte(`"schema_version":1.1`), 1),
				"huge-exponent":          bytes.Replace(compact, []byte(`"schema_version":1`), []byte(`"schema_version":1e999999999`), 1),
				"unpaired-surrogate":     bytes.Replace(compact, []byte(`"github.com"`), []byte(`"\ud800"`), 1),
				"unpaired-low-surrogate": bytes.Replace(compact, []byte(`"github.com"`), []byte(`"\udc00"`), 1),
				"depth":                  []byte(`{"x":` + strings.Repeat(`[`, 16) + `0` + strings.Repeat(`]`, 16) + `}`),
			}
			// Report has no repository host; malformed bytes/strings target its reviewer.
			if in.name == "report" {
				for _, name := range []string{"utf8", "unpaired-surrogate", "unpaired-low-surrogate"} {
					replacement := []byte(`"\ud800"`)
					if name == "utf8" {
						replacement = []byte{'"', 0xff, '"'}
					}
					if name == "unpaired-low-surrogate" {
						replacement = []byte(`"\udc00"`)
					}
					cases[name] = bytes.Replace(compact, []byte(`"independent reviewer"`), replacement, 1)
				}
				cases["nested-duplicate"] = bytes.Replace(compact, []byte(`"evidence":"host-configured"`), []byte(`"evidence":"host-configured","evidence":"host-configured"`), 1)
			}
			for name, b := range cases {
				t.Run(name, func(t *testing.T) {
					if e := in.parse(b); e == nil {
						t.Fatal("accepted malformed input")
					}
				})
			}
			atLimit := append(append([]byte{}, compact...), bytes.Repeat([]byte(" "), in.limit-len(compact))...)
			if e := in.parse(atLimit); e != nil {
				t.Fatalf("exact byte limit refused: %v", e)
			}
			if e := in.parse(append(atLimit, ' ')); e == nil {
				t.Fatal("accepted over byte limit")
			}
		})
	}
}

// Traverse the approved schemas, independently exercising every required field,
// closed object, type, bound, enum, pattern and unique-items constraint.
func TestInputsMatchEveryApprovedShapeConstraint(t *testing.T) {
	maxItemsCases := 0
	for _, in := range inputs() {
		t.Run(in.name, func(t *testing.T) {
			raw := designFile(t, in.name+".schema.json")
			s := object(t, raw)
			c := jsonschema.NewCompiler()
			c.UseLoader(noSchemaNetwork{})
			if e := c.AddResource("urn:ready:"+in.name, s); e != nil {
				t.Fatal(e)
			}
			oracle, e := c.Compile("urn:ready:" + in.name)
			if e != nil {
				t.Fatal(e)
			}
			seed := shapeSeed(s)
			if e = oracle.Validate(seed); e != nil {
				t.Fatalf("bad independent seed: %v", e)
			}
			if e = in.parse(jsonBytes(t, seed)); e != nil {
				t.Fatalf("schema-valid seed refused: %v", e)
			}
			count := 0
			var walk func(map[string]any, any, func(any) any, string)
			walk = func(s map[string]any, v any, replace func(any) any, path string) {
				test := func(label string, bad any) {
					count++
					t.Run(path+"/"+label, func(t *testing.T) {
						candidate := replace(bad)
						oracleErr := oracle.Validate(candidate)
						if oracleErr == nil {
							t.Fatal("negative fixture is not schema-invalid")
						}
						if label == "max-items" {
							assertOnlyMaxItems(t, oracleErr)
						}
						if e := in.parse(jsonBytes(t, candidate)); e == nil {
							t.Fatal("accepted schema-invalid input")
						}
					})
				}
				if variants, ok := s["anyOf"].([]any); ok {
					walk(variants[0].(map[string]any), v, replace, path)
					return
				}
				if oracle.Validate(replace(nil)) != nil {
					test("null", nil)
				}
				if s["type"] != "null" {
					if s["type"] == "boolean" {
						test("wrong-type", 0)
					} else {
						test("wrong-type", false)
					}
				}
				if _, ok := s["const"]; ok {
					test("const", "unsupported")
				}
				if _, ok := s["enum"]; ok {
					test("enum", "unsupported")
				}
				switch s["type"] {
				case "object":
					m := v.(map[string]any)
					extra := copyObject(m)
					extra["unknown"] = true
					test("unknown", extra)
					for k, child := range s["properties"].(map[string]any) {
						missing := copyObject(m)
						delete(missing, k)
						test("missing-"+k, missing)
						walk(child.(map[string]any), m[k], func(x any) any { n := copyObject(m); n[k] = x; return replace(n) }, path+"/"+k)
					}
				case "array":
					min := number(s["minItems"])
					max := number(s["maxItems"])
					if min > 0 {
						test("min-items", []any{})
					}
					tooMany := make([]any, max+1)
					for i := range tooMany {
						tooMany[i] = variedSeed(s["items"].(map[string]any), i)
					}
					// The max-sized control and max+1 negative differ only
					// in cardinality; uniqueness must not mask this guard.
					control := replace(tooMany[:max])
					if err := oracle.Validate(control); err != nil {
						t.Fatalf("invalid maxItems control: %v", err)
					}
					if err := in.parse(jsonBytes(t, control)); err != nil {
						t.Fatalf("maximum array refused: %v", err)
					}
					maxItemsCases++
					test("max-items", tooMany)
					test("unique", []any{v.([]any)[0], v.([]any)[0]})
					walk(s["items"].(map[string]any), v.([]any)[0], func(x any) any { return replace([]any{x}) }, path+"/item")
				case "string":
					test("min-length", "")
					test("max-length", strings.Repeat("x", number(s["maxLength"])+1))
					if _, ok := s["pattern"]; ok {
						test("pattern", " !")
					}
				case "integer":
					test("min", 0)
					test("max", json.Number(fmt.Sprint(int64(number(s["maximum"]))+1)))
					test("fraction", 1.5)
				case "boolean":
					test("boolean", 0)
				}
			}
			walk(s, seed, func(v any) any { return v }, "root")
			if count < 50 {
				t.Fatalf("incomplete traversal: %d", count)
			}
			t.Logf("%d schema-derived negative cases", count)
		})
	}
	if maxItemsCases != 12 {
		t.Fatalf("exercised %d array maxima, want all 12 approved constraints", maxItemsCases)
	}
}

// The independent oracle must reject each distinct max+1 fixture for maxItems
// alone, rather than uniqueness, item validity or another masked constraint.
func assertOnlyMaxItems(t *testing.T, err error) {
	t.Helper()
	root, ok := err.(*jsonschema.ValidationError)
	if !ok {
		t.Fatalf("unexpected schema error: %v", err)
	}
	leaves := 0
	var walk func(*jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) != 0 {
			for _, child := range e.Causes {
				walk(child)
			}
			return
		}
		leaves++
		path := e.ErrorKind.KeywordPath()
		if len(path) != 1 || path[0] != "maxItems" {
			t.Fatalf("max+1 fixture fails another constraint: %v", err)
		}
	}
	walk(root)
	if leaves != 1 {
		t.Fatalf("max+1 fixture has %d oracle failures, want exactly one maxItems failure: %v", leaves, err)
	}
}
func copyObject(v map[string]any) map[string]any {
	n := make(map[string]any, len(v))
	for k, x := range v {
		n[k] = x
	}
	return n
}
func number(v any) int {
	switch n := v.(type) {
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	case float64:
		return int(n)
	}
	panic("not a number")
}
func shapeSeed(s map[string]any) any {
	if v, ok := s["const"]; ok {
		return v
	}
	if v, ok := s["enum"].([]any); ok {
		return v[0]
	}
	if v, ok := s["anyOf"].([]any); ok {
		return shapeSeed(v[0].(map[string]any))
	}
	switch s["type"] {
	case "object":
		m := map[string]any{}
		for k, v := range s["properties"].(map[string]any) {
			m[k] = shapeSeed(v.(map[string]any))
		}
		return m
	case "array":
		return []any{shapeSeed(s["items"].(map[string]any))}
	case "integer":
		return s["minimum"]
	case "boolean":
		return true
	case "null":
		return nil
	case "string":
		if s["pattern"] == "^[0-9a-f]{40}$" {
			return strings.Repeat("a", 40)
		}
		if s["pattern"] == "^[0-9a-f]{64}$" {
			return strings.Repeat("b", 64)
		}
		return "x"
	}
	panic("unknown shape")
}

type noSchemaNetwork struct{}

func (noSchemaNetwork) Load(s string) (any, error) {
	return nil, fmt.Errorf("no external schema: %s", s)
}

func TestPolicyAndLockImmutableExactBytes(t *testing.T) {
	raw := designFile(t, "examples/policy.json")
	original := append([]byte{}, raw...)
	p, e := ParsePolicy(raw)
	if e != nil {
		t.Fatal(e)
	}
	raw[0] = '!'
	if !bytes.Equal(p.RawJSON(), original) || p.SHA256() != fmt.Sprintf("%x", sha256.Sum256(original)) {
		t.Fatal("policy failed raw-byte ownership/digest")
	}
	copy := p.RawJSON()
	copy[0] = '!'
	v := p.Value()
	v.Review.Tiers[0].Models[0].ID = "mutated"
	v.Review.StatusCreatorIDs[0] = 1
	v.TrustRoots[0] = "changed"
	*v.CI[0].AppID = 1
	if !bytes.Equal(p.RawJSON(), original) || p.Value().Review.Tiers[0].Models[0].ID == "mutated" || p.Value().Review.StatusCreatorIDs[0] == 1 || p.Value().TrustRoots[0] == "changed" || *p.Value().CI[0].AppID == 1 {
		t.Fatal("mutable policy alias")
	}
	spaced, e := ParsePolicy(append(original, ' '))
	if e != nil || spaced.SHA256() == p.SHA256() {
		t.Fatal("digest normalized raw bytes")
	}
	lockBytes := designFile(t, "examples/lock-with-transition.json")
	l, e := ParseLock(lockBytes)
	if e != nil {
		t.Fatal(e)
	}
	lv := l.Value()
	lv.Transition.Number = 1
	*lv.Deployment.Environment = "changed"
	*lv.Deployment.PublisherAppID = 1
	lockBytes[0] = '!'
	if l.Value().Transition.Number == 1 || *l.Value().Deployment.Environment == "changed" || *l.Value().Deployment.PublisherAppID == 1 || l.RawJSON()[0] != '{' {
		t.Fatal("mutable lock alias")
	}
	if l.SHA256() != fmt.Sprintf("%x", sha256.Sum256(l.RawJSON())) {
		t.Fatal("wrong lock digest")
	}
}

func TestStrictJSONDepthBoundary(t *testing.T) {
	for _, depth := range []int{16, 17} {
		raw := []byte(strings.Repeat("[", depth) + "0" + strings.Repeat("]", depth))
		_, err := strictJSON(raw, 64*1024)
		if (err == nil) != (depth == 16) {
			t.Fatalf("depth %d error=%v", depth, err)
		}
	}
}

func TestApprovedShapePositiveBoundaries(t *testing.T) {
	for _, in := range inputs() {
		t.Run(in.name, func(t *testing.T) {
			s := object(t, designFile(t, in.name+".schema.json"))
			seed := shapeSeed(s)
			c := jsonschema.NewCompiler()
			c.UseLoader(noSchemaNetwork{})
			if e := c.AddResource("urn:positive:"+in.name, s); e != nil {
				t.Fatal(e)
			}
			oracle, e := c.Compile("urn:positive:" + in.name)
			if e != nil {
				t.Fatal(e)
			}
			var walk func(map[string]any, any, func(any) any, string)
			walk = func(s map[string]any, v any, replace func(any) any, path string) {
				accept := func(label string, value any) {
					candidate := replace(value)
					if oracle.Validate(candidate) != nil {
						if label == "maximum-items" {
							t.Fatal("maximum-cardinality fixture is not schema-valid")
						}
						return
					}
					t.Run(path+"/"+label, func(t *testing.T) {
						if e := in.parse(jsonBytes(t, candidate)); e != nil {
							t.Fatalf("schema-valid boundary refused: %v", e)
						}
					})
				}
				if variants, ok := s["anyOf"].([]any); ok {
					accept("explicit-null", nil)
					walk(variants[0].(map[string]any), v, replace, path)
					return
				}
				if choices, ok := s["enum"].([]any); ok {
					for i, x := range choices {
						accept(fmt.Sprintf("choice-%d", i), x)
					}
				}
				switch s["type"] {
				case "object":
					m := v.(map[string]any)
					for k, child := range s["properties"].(map[string]any) {
						walk(child.(map[string]any), m[k], func(x any) any { n := copyObject(m); n[k] = x; return replace(n) }, path+"/"+k)
					}
				case "array":
					maximum := make([]any, number(s["maxItems"]))
					for i := range maximum {
						maximum[i] = variedSeed(s["items"].(map[string]any), i)
					}
					accept("maximum-items", maximum)
					if number(s["minItems"]) == 0 {
						accept("empty-array", []any{})
					}
					walk(s["items"].(map[string]any), v.([]any)[0], func(x any) any { return replace([]any{x}) }, path+"/item")
				case "integer":
					accept("minimum", s["minimum"])
					accept("maximum", s["maximum"])
				case "string":
					accept("minimum-length", "x")
					accept("maximum-length", strings.Repeat("x", number(s["maxLength"])))
					if number(s["maxLength"]) <= 1024 {
						accept("multibyte-maximum", strings.Repeat("é", number(s["maxLength"])))
					}
				case "boolean":
					accept("false", false)
					accept("true", true)
				}
			}
			walk(s, seed, func(v any) any { return v }, "root")
			raw := designFile(t, "examples/"+in.name+".json")
			versionTwo := bytes.Replace(raw, []byte(`"schema_version": 1`), []byte(`"schema_version": 2`), 1)
			if in.parse(versionTwo) == nil {
				t.Fatal("unsupported version accepted")
			}
			integral := bytes.Replace(raw, []byte(`"schema_version": 1`), []byte(`"schema_version": 1.0e0`), 1)
			if e := in.parse(integral); e != nil {
				t.Fatalf("JSON Schema integral number refused: %v", e)
			}
		})
	}
}

// Distinct shape-valid items permit positive maximum-cardinality fixtures,
// independently of production's uniqueness implementation.
func variedSeed(s map[string]any, i int) any {
	if _, ok := s["const"]; ok {
		return shapeSeed(s)
	}
	if values, ok := s["enum"].([]any); ok {
		return values[i%len(values)]
	}
	if values, ok := s["anyOf"].([]any); ok {
		return variedSeed(values[0].(map[string]any), i)
	}
	switch s["type"] {
	case "object":
		m := map[string]any{}
		for k, v := range s["properties"].(map[string]any) {
			m[k] = variedSeed(v.(map[string]any), i)
		}
		return m
	case "array":
		return []any{variedSeed(s["items"].(map[string]any), i)}
	case "string":
		if s["pattern"] == "^[0-9a-f]{40}$" {
			return fmt.Sprintf("%040x", i)
		}
		if s["pattern"] == "^[0-9a-f]{64}$" {
			return fmt.Sprintf("%064x", i)
		}
		return fmt.Sprintf("item-%d", i)
	case "integer":
		min, max := number(s["minimum"]), number(s["maximum"])
		return min + i%(max-min+1)
	case "boolean":
		return i%2 == 0
	}
	return shapeSeed(s)
}
