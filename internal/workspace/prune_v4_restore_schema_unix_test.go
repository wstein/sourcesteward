//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
//
package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"os"
	"path/filepath"
	"testing"
)

func pruneV4RestoreSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	raw, e := os.ReadFile("../../schemas/v4/registration-prune.schema.json")
	pruneMust(t, e)
	doc, e := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	pruneMust(t, e)
	c := jsonschema.NewCompiler()
	const u = "https://wstein.github.io/sourcesteward/schemas/v4/registration-prune.schema.json"
	pruneMust(t, c.AddResource(u, doc))
	s, e := c.Compile(u)
	pruneMust(t, e)
	return s
}
func TestRegistrationPruneV4RestoreSchemaNativeRecords(t *testing.T) {
	p := restoreV4Fixture(t)
	s := pruneV4RestoreSchema(t)
	o := restoreV4Options(p)
	inspect := RestoreRegistrationPruneV4(context.Background(), o)
	if inspect.Status != "ok" {
		t.Fatalf("inspect: %+v", inspect)
	}
	o.Apply, o.Authorize = true, true
	apply := RestoreRegistrationPruneV4(context.Background(), o)
	if apply.Status != "ok" {
		t.Fatalf("restore: %+v", apply)
	}
	dir := filepath.Join(p.StateDir, pruneV4Namespace, "restores")
	names, e := os.ReadDir(dir)
	pruneMust(t, e)
	if len(names) != 1 {
		t.Fatalf("journal count %d", len(names))
	}
	raw, e := os.ReadFile(filepath.Join(dir, names[0].Name()))
	pruneMust(t, e)
	values := []any{inspect, apply, json.RawMessage(raw)}
	for _, v := range values {
		b, e := json.Marshal(v)
		pruneMust(t, e)
		doc, e := jsonschema.UnmarshalJSON(bytes.NewReader(b))
		pruneMust(t, e)
		if e = s.Validate(doc); e != nil {
			t.Fatalf("schema rejects native restore: %v", e)
		}
	}
	doc, e := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	pruneMust(t, e)
	m := doc.(map[string]any)
	for _, key := range []string{"restore_id", "prune_journal_digest", "effects", "remaining"} {
		copy := map[string]any{}
		for k, v := range m {
			copy[k] = v
		}
		delete(copy, key)
		if s.Validate(copy) == nil {
			t.Fatalf("schema allows missing %s", key)
		}
	}
}
