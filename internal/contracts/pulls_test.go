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
