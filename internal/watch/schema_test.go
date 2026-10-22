// SPDX-License-Identifier: EUPL-1.2
package watch

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func watchEventSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "schemas", "v1", "watch-event.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	const id = "https://wstein.github.io/sourcesteward/schemas/v1/watch-event.schema.json"
	c := jsonschema.NewCompiler()
	if err := c.AddResource(id, doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile(id)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Every line the crash-matrix scenarios print or journal, for every kind, validates against the published
// schema (Spec 035 US2 and US4).
func TestEveryGeneratedEventValidatesAgainstTheSchema(t *testing.T) {
	s := watchEventSchema(t)
	kinds := map[string]bool{}
	lines := 0
	check := func(where, raw string) {
		for _, l := range strings.Split(strings.TrimSpace(raw), "\n") {
			if l == "" {
				continue
			}
			v, err := jsonschema.UnmarshalJSON(strings.NewReader(l))
			if err != nil {
				t.Fatalf("%s: %q: %v", where, l, err)
			}
			if err := s.Validate(v); err != nil {
				t.Fatalf("%s: %s\n%v", where, l, err)
			}
			lines++
			var e Event
			if err := e.UnmarshalJSON([]byte(l)); err != nil {
				t.Fatal(err)
			}
			kinds[e.Kind] = true
		}
	}
	for _, c := range crashCases() {
		for _, steps := range [][]step{c.still, c.flapped} {
			r, _ := c.play(t, steps, "", 0)
			raw, err := os.ReadFile(filepath.Join(r.dir, "journal.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			check(c.kind+" journal", string(raw))
			check(c.kind+" stdout", r.out.String())
			r.crashed()
		}
	}
	for _, k := range Kinds {
		if !kinds[k] {
			t.Errorf("no generated event of kind %s", k)
		}
	}
	t.Logf("validated %d lines", lines)
}
