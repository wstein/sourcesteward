// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"encoding/json"
	"strings"
	"testing"
)

func carryOverExample(yes bool) map[string]any {
	s := func(c string) string { return strings.Repeat(c, 40) }
	doc := map[string]any{
		"schema_version": 1, "command": "review carry-over", "pr": 12,
		"from":          map[string]any{"base": s("1"), "head": s("2")},
		"to":            map[string]any{"base": s("3"), "head": s("4")},
		"patch_id":      map[string]any{"state": "equal", "old": s("9"), "new": s("9")},
		"merge_commits": map[string]any{"state": "none", "count": 0, "commits": []any{}},
		"range_diff":    map[string]any{"state": "equal", "total": 2, "not_equal": 0, "deviations": []any{}, "truncated": false},
		"pr_head":       map[string]any{"state": "not-checked"},
		"carry_over":    true, "reasons": []any{},
		"line": "Carried-over: from=" + s("2") + " to=" + s("4") + " patch-id=" + s("9") + " pr=12",
	}
	if !yes {
		doc["carry_over"], doc["line"], doc["reasons"] = false, nil, []any{"merge-commits"}
		doc["merge_commits"] = map[string]any{"state": "found", "count": 1, "commits": []any{s("7")}}
	}
	return doc
}

func carryOverJSON(t *testing.T, m map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestReviewCarryOverSchemaAcceptsTheYesAndNoDocuments(t *testing.T) {
	s := schema(t, "review-carry-over")
	valid(t, s, carryOverJSON(t, carryOverExample(true)))
	valid(t, s, carryOverJSON(t, carryOverExample(false)))
	extra := carryOverExample(true)
	extra["added_later"] = 1
	valid(t, s, carryOverJSON(t, extra))
	dev := carryOverExample(false)
	dev["range_diff"] = map[string]any{"state": "differs", "total": 2, "not_equal": 1, "truncated": false,
		"deviations": []any{map[string]any{"old": nil, "new": 3, "mark": ">"}}}
	valid(t, s, carryOverJSON(t, dev))
}

func TestReviewCarryOverSchemaRejectsBrokenDocuments(t *testing.T) {
	s := schema(t, "review-carry-over")
	for name, mutate := range map[string]func(m map[string]any){
		"yes without line":          func(m map[string]any) { m["line"] = nil },
		"no with a line":            func(m map[string]any) { m["carry_over"] = false; m["reasons"] = []any{"merge-commits"} },
		"yes with a reason":         func(m map[string]any) { m["reasons"] = []any{"merge-commits"} },
		"no without a reason":       func(m map[string]any) { m["carry_over"], m["line"] = false, nil },
		"yes with differing patch":  func(m map[string]any) { m["patch_id"].(map[string]any)["state"] = "differs" },
		"yes with merge commits":    func(m map[string]any) { m["merge_commits"].(map[string]any)["state"] = "found" },
		"yes with range-diff diff":  func(m map[string]any) { m["range_diff"].(map[string]any)["state"] = "differs" },
		"short sha":                 func(m map[string]any) { m["from"].(map[string]any)["head"] = "abc1234" },
		"uppercase line sha":        func(m map[string]any) { m["line"] = strings.ToUpper(m["line"].(string)) },
		"line with a trailing text": func(m map[string]any) { m["line"] = m["line"].(string) + " ok" },
		"unknown reason":            func(m map[string]any) { m["carry_over"], m["line"], m["reasons"] = false, nil, []any{"because"} },
		"unknown state":             func(m map[string]any) { m["pr_head"].(map[string]any)["state"] = "maybe" },
		"wrong command":             func(m map[string]any) { m["command"] = "carry-over" },
		"wrong version":             func(m map[string]any) { m["schema_version"] = 2 },
		"pr zero":                   func(m map[string]any) { m["pr"] = 0 },
		"missing key":               func(m map[string]any) { delete(m, "range_diff") },
	} {
		m := carryOverExample(true)
		mutate(m)
		if err := s.Validate(value(t, carryOverJSON(t, m))); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
