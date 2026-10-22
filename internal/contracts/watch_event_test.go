// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	wA = "3f2a9c1d0b7e4a5f6c8d9e0a1b2c3d4e5f607182"
	wB = "9d8c7b6a5f4e3d2c1b0a99887766554433221100"
)

// watchStatusSchema compiles the second top-level definition of the watch schema.
func watchStatusSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root(t), "schemas/v1/watch-event.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	id := "https://wstein.github.io/sourcesteward/schemas/v1/watch-event.schema.json"
	c := compiler()
	if err := c.AddResource(id, doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile(id + "#/$defs/status")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func watchLine(kind, repo, pr, details string) string {
	return fmt.Sprintf(`{"schema_version":1,"seq":1,"id":"x","time":"2026-10-22T09:00:07Z","repo":%s,"pr":%s,"kind":"%s","details":%s}`, repo, pr, kind, details)
}

// watchFixtures holds one valid line per kind (plus the null variants) with its event id.
func watchFixtures() map[string]string {
	r := `"wstein/sourcesteward"`
	f := map[string]string{
		"new-pr":                 watchLine("new-pr", r, "41", `{"title":"t","author":"a","draft":true,"base":"main","head_ref":"x","head":"`+wA+`","fork":false}`),
		"head-changed":           watchLine("head-changed", r, "41", `{"old":"`+wA+`","new":"`+wB+`"}`),
		"checks-green":           watchLine("checks-green", r, "41", `{"head":"`+wA+`","draft":false,"base":"main","total":3}`),
		"checks-red":             watchLine("checks-red", r, "41", `{"head":"`+wA+`","failing":["docs-check"],"failing_total":1,"shape":"real","base":"main","draft":true}`),
		"checks-pending-timeout": watchLine("checks-pending-timeout", r, "41", `{"head":"`+wA+`","pending":["ci"],"since":"2026-10-22T08:00:00Z"}`),
		"mergeable-changed":      watchLine("mergeable-changed", r, "41", `{"mergeable":null,"state":"dirty","conflicting":true,"behind":false,"base":"main"}`),
		"ready-state-changed":    watchLine("ready-state-changed", r, "41", `{"from":"draft","to":"ready"}`),
		"base-changed":           watchLine("base-changed", r, "41", `{"old":"feat/a","new":"main"}`),
		"merged":                 watchLine("merged", r, "41", `{"merge_commit":"`+wB+`","base":"main","head":"`+wA+`","head_ref":"x","merged_at":"2026-10-22T10:29:58Z","method_hint":"rebase-or-squash"}`),
		"merged null commit":     watchLine("merged", r, "41", `{"merge_commit":null,"base":"main","head":"`+wA+`","head_ref":"x","merged_at":"2026-10-22T10:29:58Z","method_hint":"unknown"}`),
		"closed":                 watchLine("closed", r, "41", `{"head":"`+wA+`","base":"main"}`),
		"restack-needed":         watchLine("restack-needed", r, "42", `{"merged_pr":41,"base":"main","reason":"contains-merged-commits","old_parent":"`+wA+`","new_parent":"`+wB+`","new_parent_source":"merge_commit_sha","branch":"b","argv":["git","rebase"]}`),
		"restack-needed null":    watchLine("restack-needed", r, "42", `{"merged_pr":41,"base":"main","reason":"child-of-merged-branch","old_parent":"`+wA+`","new_parent":null,"new_parent_source":"merge_commit_sha","branch":"b","argv":null}`),
		"repos-changed":          watchLine("repos-changed", "null", "null", `{"added":["o/a"],"removed":[],"skipped":[{"path":"app/lib","reason":"not-github"}]}`),
		"watch-error run":        watchLine("watch-error", "null", "null", `{"code":"rate-limited","message":"m","failures":1,"retry_in_seconds":1620,"repo":null}`),
		"watch-error repo":       watchLine("watch-error", r, "null", `{"code":"network","message":"m","failures":2,"retry_in_seconds":60,"repo":"wstein/sourcesteward"}`),
		"watch-recovered":        watchLine("watch-recovered", r, "null", `{"after_failures":2,"repo":"wstein/sourcesteward"}`),
		"watch-stalled":          watchLine("watch-stalled", r, "null", `{"since":"2026-10-22T09:00:00Z","threshold_seconds":600,"repo":"wstein/sourcesteward"}`),
	}
	for k, v := range f {
		f[k] = strings.Replace(v, `"id":"x"`, `"id":"`+idFor(k)+`"`, 1)
	}
	return f
}

func idFor(name string) string {
	kind := strings.Fields(name)[0]
	switch {
	case kind == "repos-changed" || name == "watch-error run":
		return "-:" + kind + ":p1"
	case strings.HasPrefix(kind, "watch-"):
		return "wstein/sourcesteward#-:" + kind + ":e1"
	}
	return "wstein/sourcesteward#41:" + kind + ":g0"
}

func TestWatchEventSchemaAcceptsEveryKind(t *testing.T) {
	s := schema(t, "watch-event")
	seen := map[string]bool{}
	for name, line := range watchFixtures() {
		valid(t, s, []byte(line))
		var m map[string]any
		_ = json.Unmarshal([]byte(line), &m)
		seen[m["kind"].(string)] = true
		_ = name
	}
	if len(seen) != 15 {
		t.Fatalf("fixtures cover %d kinds", len(seen))
	}
}

func TestWatchEventSchemaRejectsBrokenEvents(t *testing.T) {
	s := schema(t, "watch-event")
	fx := watchFixtures()
	mut := func(base string, fn func(m map[string]any)) string {
		var m map[string]any
		if err := json.Unmarshal([]byte(fx[base]), &m); err != nil {
			t.Fatal(err)
		}
		fn(m)
		b, _ := json.Marshal(m)
		return string(b)
	}
	d := func(m map[string]any) map[string]any { return m["details"].(map[string]any) }
	for name, line := range map[string]string{
		"missing envelope key":       mut("closed", func(m map[string]any) { delete(m, "pr") }),
		"extra envelope key":         mut("closed", func(m map[string]any) { m["extra"] = 1 }),
		"schema version 2":           mut("closed", func(m map[string]any) { m["schema_version"] = 2 }),
		"seq zero":                   mut("closed", func(m map[string]any) { m["seq"] = 0 }),
		"unknown kind":               mut("closed", func(m map[string]any) { m["kind"] = "review-state-changed" }),
		"bad time":                   mut("closed", func(m map[string]any) { m["time"] = "2026-10-22 09:00:00" }),
		"non-UTC time":               mut("closed", func(m map[string]any) { m["time"] = "2026-10-22T09:00:00+02:00" }),
		"bad id":                     mut("closed", func(m map[string]any) { m["id"] = "no id" }),
		"bad repo":                   mut("closed", func(m map[string]any) { m["repo"] = "not-a-repo" }),
		"pr kind without repo":       mut("closed", func(m map[string]any) { m["repo"] = nil }),
		"pr kind without pr":         mut("closed", func(m map[string]any) { m["pr"] = nil }),
		"pr zero":                    mut("closed", func(m map[string]any) { m["pr"] = 0 }),
		"repos-changed with a repo":  mut("repos-changed", func(m map[string]any) { m["repo"] = "o/r" }),
		"watch-stalled without repo": mut("watch-stalled", func(m map[string]any) { m["repo"] = nil }),
		"watch-error with a pr":      mut("watch-error repo", func(m map[string]any) { m["pr"] = 3 }),
		"extra detail key":           mut("closed", func(m map[string]any) { d(m)["extra"] = 1 }),
		"missing detail key":         mut("closed", func(m map[string]any) { delete(d(m), "base") }),
		"details of another kind": mut("closed", func(m map[string]any) {
			m["details"] = d(map[string]any{"details": map[string]any{"old": wA, "new": wB}})
		}),
		"details not an object":   mut("closed", func(m map[string]any) { m["details"] = []any{} }),
		"short sha":               mut("closed", func(m map[string]any) { d(m)["head"] = "abc" }),
		"uppercase sha":           mut("closed", func(m map[string]any) { d(m)["head"] = strings.ToUpper(wA) }),
		"new-pr without fork":     mut("new-pr", func(m map[string]any) { delete(d(m), "fork") }),
		"new-pr fork as string":   mut("new-pr", func(m map[string]any) { d(m)["fork"] = "no" }),
		"too many names":          mut("checks-red", func(m map[string]any) { d(m)["failing"] = make([]any, 21) }),
		"unknown shape":           mut("checks-red", func(m map[string]any) { d(m)["shape"] = "flaky" }),
		"negative total":          mut("checks-green", func(m map[string]any) { d(m)["total"] = -1 }),
		"ready from equals to":    mut("ready-state-changed", func(m map[string]any) { d(m)["to"] = "draft" }),
		"ready unknown state":     mut("ready-state-changed", func(m map[string]any) { d(m)["to"] = "open" }),
		"unknown method hint":     mut("merged", func(m map[string]any) { d(m)["method_hint"] = "squash" }),
		"merged_at not a time":    mut("merged", func(m map[string]any) { d(m)["merged_at"] = "yesterday" }),
		"unknown restack reason":  mut("restack-needed", func(m map[string]any) { d(m)["reason"] = "other" }),
		"wrong parent source":     mut("restack-needed", func(m map[string]any) { d(m)["new_parent_source"] = "head" }),
		"argv with a null parent": mut("restack-needed null", func(m map[string]any) { d(m)["argv"] = []any{"git"} }),
		"argv missing":            mut("restack-needed", func(m map[string]any) { delete(d(m), "argv") }),
		"skipped without reason":  mut("repos-changed", func(m map[string]any) { d(m)["skipped"] = []any{map[string]any{"path": "a"}} }),
		"unknown error code":      mut("watch-error run", func(m map[string]any) { d(m)["code"] = "boom" }),
		"error message too long":  mut("watch-error run", func(m map[string]any) { d(m)["message"] = strings.Repeat("m", 513) }),
		"negative retry":          mut("watch-error run", func(m map[string]any) { d(m)["retry_in_seconds"] = -1 }),
		"mergeable as string":     mut("mergeable-changed", func(m map[string]any) { d(m)["mergeable"] = "yes" }),
	} {
		if e := s.Validate(value(t, []byte(line))); e == nil {
			t.Errorf("%s: accepted\n%s", name, line)
		}
	}
}

func TestWatchStatusDefinition(t *testing.T) {
	s := watchStatusSchema(t)
	good := `{"schema_version":1,"state_dir":"/home/u/.local/state/stw/watch/abc","heartbeat":{"time":"2026-10-22T09:00:00Z","age_seconds":4,"pid":123},"fresh":true,"interval_seconds":60,"seq":7,"lock_held":true,"repos":[{"repo":"o/r","last_ok":"2026-10-22T09:00:00Z","age_seconds":4,"failures":0,"open_error":null},{"repo":"o/s","last_ok":null,"age_seconds":null,"failures":2,"open_error":"forbidden"}]}`
	valid(t, s, []byte(good))
	valid(t, s, []byte(strings.Replace(good, `{"time":"2026-10-22T09:00:00Z","age_seconds":4,"pid":123}`, "null", 1)))
	for name, bad := range map[string]string{
		"missing lock_held": strings.Replace(good, `"lock_held":true,`, "", 1),
		"extra key":         strings.Replace(good, `"fresh":true,`, `"fresh":true,"extra":1,`, 1),
		"fresh as string":   strings.Replace(good, `"fresh":true`, `"fresh":"yes"`, 1),
		"unknown code":      strings.Replace(good, `"forbidden"`, `"boom"`, 1),
		"negative age":      strings.Replace(good, `"age_seconds":4,"failures"`, `"age_seconds":-4,"failures"`, 1),
		"version 2":         strings.Replace(good, `"schema_version":1`, `"schema_version":2`, 1),
	} {
		if e := s.Validate(value(t, []byte(bad))); e == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// Every kind rejects an extra and a missing detail key (an open details object must fail a test, not only
// the freeze hash).
func TestWatchEventSchemaClosesDetailsOfEveryKind(t *testing.T) {
	s := schema(t, "watch-event")
	for name, line := range watchFixtures() {
		var base map[string]any
		if err := json.Unmarshal([]byte(line), &base); err != nil {
			t.Fatal(err)
		}
		details := base["details"].(map[string]any)
		withExtra := func() string {
			var m map[string]any
			_ = json.Unmarshal([]byte(line), &m)
			m["details"].(map[string]any)["extra"] = 1
			b, _ := json.Marshal(m)
			return string(b)
		}()
		if s.Validate(value(t, []byte(withExtra))) == nil {
			t.Errorf("%s: an extra details key is accepted", name)
		}
		for key := range details {
			var m map[string]any
			_ = json.Unmarshal([]byte(line), &m)
			delete(m["details"].(map[string]any), key)
			b, _ := json.Marshal(m)
			if s.Validate(value(t, b)) == nil {
				t.Errorf("%s: missing details key %s is accepted", name, key)
			}
		}
	}
}
