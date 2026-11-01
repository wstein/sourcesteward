// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"encoding/json"
	"strings"
	"testing"
)

// pullsExample is the document of specs/036-stw-pulls/contracts/pulls.md.
const pullsExample = `{"schema_version":1,"command":"pulls","time":"2026-10-22T09:00:00Z","host":"github.com","options":{"scope":"workspace","owners":[],"no_bots":false,"bots_only":false,"authors":[],"detail_drafts":false,"limit":200},"repos":[{"repo":"wstein/sourcesteward","source":"workspace","open":2,"listed":2,"error":null}],"groups":{"ready":[{"repo":"wstein/sourcesteward","number":57,"title":"build(deps): bump cobra","url":"https://github.com/wstein/sourcesteward/pull/57","author":"dependabot[bot]","bot":true,"draft":false,"created_at":"2026-10-19T04:12:00Z","age_days":3,"base":"main","head_ref":"dependabot/go_modules/cobra","head":"3f2a9c1d0b7e4a5f6c8d9e0a1b2c3d4e5f607182","fork":false,"labels":["dependencies"],"checks":"green","failing":[],"mergeable":true,"mergeable_state":"clean","rebaseable":true,"reviewers":[],"group":"ready","reason":null,"ready":true,"detail":true}],"checks":[],"conflict":[],"draft":[],"blocked":[]},"summary":{"total":1,"ready":1,"checks":0,"conflict":0,"draft":0,"blocked":0,"bots":1,"not_read":0,"repos_read":1,"repos_failed":0},"errors":[]}`

func mutatePulls(t *testing.T, fn func(m map[string]any)) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(pullsExample), &m); err != nil {
		t.Fatal(err)
	}
	fn(m)
	b, _ := json.Marshal(m)
	return string(b)
}

func pullRow(m map[string]any) map[string]any {
	return m["groups"].(map[string]any)["ready"].([]any)[0].(map[string]any)
}

func TestPullsSchemaAcceptsTheContractExampleAndItsVariants(t *testing.T) {
	s := schema(t, "pulls")
	valid(t, s, []byte(pullsExample))
	// A blocked row without detail, a draft, an owner row and every error shape.
	valid(t, s, []byte(mutatePulls(t, func(m map[string]any) {
		r := pullRow(m)
		g := m["groups"].(map[string]any)
		g["ready"] = []any{}
		r["group"], r["reason"], r["ready"], r["detail"] = "blocked", "not-read", false, false
		r["checks"], r["mergeable"], r["mergeable_state"], r["rebaseable"] = nil, nil, nil, nil
		g["blocked"] = []any{r}
		m["summary"] = map[string]any{"total": 1, "ready": 0, "checks": 0, "conflict": 0, "draft": 0, "blocked": 1, "bots": 1, "not_read": 1, "repos_read": 1, "repos_failed": 0}
	})))
	valid(t, s, []byte(mutatePulls(t, func(m map[string]any) {
		m["options"].(map[string]any)["scope"] = "mixed"
		m["options"].(map[string]any)["owners"] = []any{"wstein"}
		m["repos"] = []any{
			map[string]any{"repo": "wstein/other", "source": "owner", "open": nil, "listed": 0, "error": "rate-limited"},
		}
		m["errors"] = []any{
			map[string]any{"repo": "wstein/other", "owner": nil, "code": "rate-limited", "message": "m"},
			map[string]any{"repo": nil, "owner": "wstein", "code": "search-incomplete", "message": "m"},
			map[string]any{"repo": nil, "owner": "wstein", "code": "malformed-response", "message": "m"},
		}
		m["summary"].(map[string]any)["repos_failed"] = 1
	})))
	valid(t, s, []byte(mutatePulls(t, func(m map[string]any) {
		r := pullRow(m)
		r["created_at"], r["age_days"], r["url"], r["head"] = nil, nil, nil, nil
	})))
}

func TestPullsSchemaRejectsBrokenDocuments(t *testing.T) {
	s := schema(t, "pulls")
	row := func(fn func(r map[string]any)) string {
		return mutatePulls(t, func(m map[string]any) { fn(pullRow(m)) })
	}
	top := func(fn func(m map[string]any)) string { return mutatePulls(t, fn) }
	opt := func(fn func(o map[string]any)) string {
		return mutatePulls(t, func(m map[string]any) { fn(m["options"].(map[string]any)) })
	}
	for name, doc := range map[string]string{
		"extra top key":          top(func(m map[string]any) { m["extra"] = 1 }),
		"missing top key":        top(func(m map[string]any) { delete(m, "errors") }),
		"schema version 2":       top(func(m map[string]any) { m["schema_version"] = 2 }),
		"other command":          top(func(m map[string]any) { m["command"] = "watch" }),
		"time not UTC":           top(func(m map[string]any) { m["time"] = "2026-10-22T09:00:00+02:00" }),
		"unknown scope":          opt(func(o map[string]any) { o["scope"] = "owner" }),
		"limit zero":             opt(func(o map[string]any) { o["limit"] = 0 }),
		"limit 501":              opt(func(o map[string]any) { o["limit"] = 501 }),
		"bad owner":              opt(func(o map[string]any) { o["owners"] = []any{"-bad"} }),
		"extra option":           opt(func(o map[string]any) { o["x"] = true }),
		"unknown source":         top(func(m map[string]any) { m["repos"].([]any)[0].(map[string]any)["source"] = "org" }),
		"unknown repo error":     top(func(m map[string]any) { m["repos"].([]any)[0].(map[string]any)["error"] = "boom" }),
		"missing group key":      top(func(m map[string]any) { delete(m["groups"].(map[string]any), "blocked") }),
		"extra group key":        top(func(m map[string]any) { m["groups"].(map[string]any)["other"] = []any{} }),
		"row in the wrong group": top(func(m map[string]any) { pullRow(m)["group"] = "draft" }),
		"draft row listed under ready": row(func(r map[string]any) {
			r["group"], r["ready"], r["draft"] = "draft", false, true
		}),
		"ready flag false":      row(func(r map[string]any) { r["ready"] = false }),
		"ready row with reason": row(func(r map[string]any) { r["reason"] = "unknown" }),
		"blocked without reason": top(func(m map[string]any) {
			r := pullRow(m)
			g := m["groups"].(map[string]any)
			g["blocked"] = []any{r}
			g["ready"] = []any{}
			r["group"], r["ready"] = "blocked", false
		}),
		"unknown reason": top(func(m map[string]any) {
			r := pullRow(m)
			g := m["groups"].(map[string]any)
			g["blocked"] = []any{r}
			g["ready"] = []any{}
			r["group"], r["ready"], r["reason"] = "blocked", false, "other"
		}),
		"no detail but checks": row(func(r map[string]any) { r["detail"] = false; r["ready"] = false; r["group"] = "ready" }),
		"http url":             row(func(r map[string]any) { r["url"] = "http://github.com/o/r/pull/1" }),
		"short head":           row(func(r map[string]any) { r["head"] = "abc" }),
		"upper case head":      row(func(r map[string]any) { r["head"] = strings.Repeat("A", 40) }),
		"number zero":          row(func(r map[string]any) { r["number"] = 0 }),
		"negative age":         row(func(r map[string]any) { r["age_days"] = -1 }),
		"unknown checks class": row(func(r map[string]any) { r["checks"] = "yellow" }),
		"too many labels":      row(func(r map[string]any) { r["labels"] = make([]any, 21) }),
		"too many reviewers":   row(func(r map[string]any) { r["reviewers"] = make([]any, 21) }),
		"extra row key":        row(func(r map[string]any) { r["extra"] = 1 }),
		"missing row key":      row(func(r map[string]any) { delete(r, "detail") }),
		"bad repo":             row(func(r map[string]any) { r["repo"] = "no-slash" }),
		"unknown error code": top(func(m map[string]any) {
			m["errors"] = []any{map[string]any{"repo": "o/r", "owner": nil, "code": "boom", "message": "m"}}
		}),
		"error without subject": top(func(m map[string]any) {
			m["errors"] = []any{map[string]any{"repo": nil, "owner": nil, "code": "network", "message": "m"}}
		}),
		"error with both subject": top(func(m map[string]any) {
			m["errors"] = []any{map[string]any{"repo": "o/r", "owner": "o", "code": "network", "message": "m"}}
		}),
		"error message too long": top(func(m map[string]any) {
			m["errors"] = []any{map[string]any{"repo": "o/r", "owner": nil, "code": "network", "message": strings.Repeat("x", 513)}}
		}),
		"negative summary":    top(func(m map[string]any) { m["summary"].(map[string]any)["not_read"] = -1 }),
		"missing summary key": top(func(m map[string]any) { delete(m["summary"].(map[string]any), "bots") }),
	} {
		if err := s.Validate(value(t, []byte(doc))); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// reviewMembers adds the Spec 046 members to the contract example.
func reviewMembers(t *testing.T, state string, fn func(r, rv, m map[string]any)) string {
	t.Helper()
	return mutatePulls(t, func(m map[string]any) {
		r := pullRow(m)
		rv := map[string]any{"basis": "comment", "state": state, "verdict": nil, "head": nil, "patch_id": nil, "at": nil, "url": nil, "carried": false, "edited": false, "truncated": false}
		switch state {
		case "clear", "clear-carried", "clear-stale", "findings":
			rv["verdict"] = "CLEAR"
			if state == "findings" {
				rv["verdict"] = "FINDINGS"
			}
			rv["head"], rv["patch_id"], rv["at"], rv["url"] = strings.Repeat("a", 40), strings.Repeat("1", 40), "2026-11-01T09:00:00Z", "https://github.com/o/r/pull/1#issuecomment-1"
			rv["carried"] = state == "clear-carried"
		}
		r["review"], r["hints"] = rv, []any{"ready-mergeable"}
		m["options"].(map[string]any)["review"] = true
		m["options"].(map[string]any)["review_states"] = []any{}
		m["options"].(map[string]any)["hints"] = []any{}
		m["summary"].(map[string]any)["review"] = map[string]any{"none": 0, "findings": 0, "clear": 1, "clear_carried": 0, "clear_stale": 0, "unparsed": 0, "not_read": 0}
		m["summary"].(map[string]any)["hints"] = map[string]any{"clear_not_ready": 0, "no_final_review": 0, "base_not_default": 0, "parent_merged_retarget": 0, "ready_mergeable": 1}
		if fn != nil {
			fn(r, rv, m)
		}
	})
}

func TestPullsSchemaAcceptsReviewMembers(t *testing.T) {
	s := schema(t, "pulls")
	for _, state := range []string{"none", "unparsed", "clear", "clear-carried", "clear-stale", "findings"} {
		valid(t, s, []byte(reviewMembers(t, state, nil)))
	}
	valid(t, s, []byte(mutatePulls(t, func(m map[string]any) {
		pullRow(m)["review"], pullRow(m)["hints"] = nil, []any{}
	})))
	valid(t, s, []byte(reviewMembers(t, "clear", func(r, rv, m map[string]any) {
		m["options"].(map[string]any)["review_states"] = []any{"clear", "findings"}
		m["options"].(map[string]any)["hints"] = []any{"ready-mergeable"}
		r["hints"] = []any{"clear-not-ready", "no-final-review", "base-not-default", "parent-merged-retarget", "ready-mergeable"}
	})))
}

func TestPullsSchemaRejectsBrokenReviewMembers(t *testing.T) {
	s := schema(t, "pulls")
	type mod = func(r, rv, m map[string]any)
	for name, c := range map[string]struct {
		state string
		fn    mod
	}{
		"unknown state":            {"clear", func(r, rv, m map[string]any) { rv["state"] = "approved" }},
		"basis not comment":        {"clear", func(r, rv, m map[string]any) { rv["basis"] = "verified" }},
		"verdict lower case":       {"clear", func(r, rv, m map[string]any) { rv["verdict"] = "clear" }},
		"clear with FINDINGS":      {"clear", func(r, rv, m map[string]any) { rv["verdict"] = "FINDINGS" }},
		"findings with CLEAR":      {"findings", func(r, rv, m map[string]any) { rv["verdict"] = "CLEAR" }},
		"none with a verdict":      {"none", func(r, rv, m map[string]any) { rv["verdict"] = "CLEAR" }},
		"unparsed with a url":      {"unparsed", func(r, rv, m map[string]any) { rv["url"] = "https://github.com/x" }},
		"none edited":              {"none", func(r, rv, m map[string]any) { rv["edited"] = true }},
		"clear without head":       {"clear", func(r, rv, m map[string]any) { rv["head"] = nil }},
		"clear without time":       {"clear", func(r, rv, m map[string]any) { rv["at"] = nil }},
		"clear carried flag":       {"clear", func(r, rv, m map[string]any) { rv["carried"] = true }},
		"carried without the flag": {"clear-carried", func(r, rv, m map[string]any) { rv["carried"] = false }},
		"stale with FINDINGS":      {"clear-stale", func(r, rv, m map[string]any) { rv["verdict"] = "FINDINGS" }},
		"short head":               {"clear", func(r, rv, m map[string]any) { rv["head"] = "abc" }},
		"patch id of 20 digits":    {"clear", func(r, rv, m map[string]any) { rv["patch_id"] = strings.Repeat("1", 20) }},
		"http url":                 {"clear", func(r, rv, m map[string]any) { rv["url"] = "http://github.com/x" }},
		"extra review member":      {"clear", func(r, rv, m map[string]any) { rv["extra"] = 1 }},
		"missing review member":    {"clear", func(r, rv, m map[string]any) { delete(rv, "edited") }},
		"unknown hint":             {"clear", func(r, rv, m map[string]any) { r["hints"] = []any{"approved"} }},
		"duplicate hint":           {"clear", func(r, rv, m map[string]any) { r["hints"] = []any{"ready-mergeable", "ready-mergeable"} }},
		"hints not an array":       {"clear", func(r, rv, m map[string]any) { r["hints"] = "ready-mergeable" }},
		"unknown filter state":     {"clear", func(r, rv, m map[string]any) { m["options"].(map[string]any)["review_states"] = []any{"x"} }},
		"unknown filter hint":      {"clear", func(r, rv, m map[string]any) { m["options"].(map[string]any)["hints"] = []any{"x"} }},
		"negative review count": {"clear", func(r, rv, m map[string]any) {
			m["summary"].(map[string]any)["review"].(map[string]any)["clear"] = -1
		}},
		"extra review count": {"clear", func(r, rv, m map[string]any) {
			m["summary"].(map[string]any)["review"].(map[string]any)["approved"] = 1
		}},
		"missing hint count": {"clear", func(r, rv, m map[string]any) {
			delete(m["summary"].(map[string]any)["hints"].(map[string]any), "ready_mergeable")
		}},
	} {
		if err := s.Validate(value(t, []byte(reviewMembers(t, c.state, c.fn)))); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
