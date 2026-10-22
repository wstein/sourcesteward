// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"encoding/json"
	"strings"
	"testing"
)

// issuesExample is the document of specs/038-stw-issues/contracts/issues.md.
const issuesExample = `{"schema_version":1,"command":"issues","time":"2026-10-22T09:00:00Z","host":"github.com","viewer":{"state":"ok","login":"wstein","reason":null,"error":null},"options":{"scope":"workspace","owners":[],"labels":[],"assignees":[],"authors":[],"limit":200},"repos":[{"repo":"wstein/sourcesteward","source":"workspace","state":"ok","reason":null,"open":2,"pulls_dropped":1,"error":null}],"issues":{"red":[{"repo":"wstein/sourcesteward","number":41,"title":"token leaks into an error string","url":"https://github.com/wstein/sourcesteward/issues/41","author":"alice","bot":false,"created_at":"2026-10-20T08:00:00Z","age_days":2,"labels":["security","bug"],"labels_total":2,"assignees":[],"comments":0,"priority":"red","reason":"security-label","command":"gh issue view 41 -R wstein/sourcesteward","effect":"read"}],"yellow":[],"blue":[{"repo":"wstein/sourcesteward","number":12,"title":"docs typo","url":"https://github.com/wstein/sourcesteward/issues/12","author":"bob","bot":false,"created_at":"2026-09-01T08:00:00Z","age_days":51,"labels":[],"labels_total":0,"assignees":[],"comments":3,"priority":"blue","reason":"none","command":null,"effect":null}],"violet":[]},"summary":{"total":2,"red":1,"yellow":0,"blue":1,"violet":0,"omitted":0,"counts":{"red":1,"yellow":0,"blue":1,"violet":0},"bots":0,"repos_read":1,"repos_failed":0},"errors":[]}`

func mutateIssues(t *testing.T, fn func(m map[string]any)) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(issuesExample), &m); err != nil {
		t.Fatal(err)
	}
	fn(m)
	b, _ := json.Marshal(m)
	return string(b)
}

func issueRow(m map[string]any, group string) map[string]any {
	return m["issues"].(map[string]any)[group].([]any)[0].(map[string]any)
}

func TestIssuesSchemaAcceptsTheContractExampleAndItsVariants(t *testing.T) {
	s := schema(t, "issues")
	valid(t, s, []byte(issuesExample))
	// A yellow assigned row, a violet bot row, a row without time, URL and comments.
	valid(t, s, []byte(mutateIssues(t, func(m map[string]any) {
		g := m["issues"].(map[string]any)
		y, v := issueRow(m, "red"), issueRow(m, "blue")
		y["priority"], y["reason"], y["assignees"] = "yellow", "assigned-to-you", []any{"wstein"}
		v["priority"], v["reason"], v["author"], v["bot"] = "violet", "bot", "dependabot[bot]", true
		v["created_at"], v["age_days"], v["url"], v["comments"] = nil, nil, nil, nil
		g["red"], g["blue"], g["yellow"], g["violet"] = []any{}, []any{}, []any{y}, []any{v}
	})))
	// Every viewer outcome and every repository state.
	for _, viewer := range []map[string]any{
		{"state": "skipped", "login": nil, "reason": "no-identity", "error": nil},
		{"state": "skipped", "login": nil, "reason": "not-github-origin", "error": nil},
		{"state": "error", "login": nil, "reason": nil, "error": "rate-limited"},
	} {
		valid(t, s, []byte(mutateIssues(t, func(m map[string]any) { m["viewer"] = viewer })))
	}
	valid(t, s, []byte(mutateIssues(t, func(m map[string]any) {
		m["options"] = map[string]any{"scope": "mixed", "owners": []any{"wstein"}, "labels": []any{"bug"}, "assignees": []any{"@me", "@none", "copilot[bot]"}, "authors": []any{"dependabot[bot]"}, "limit": 500}
		m["repos"] = []any{
			map[string]any{"repo": "wstein/a", "source": "owner", "state": "skipped", "reason": "issues-disabled", "open": 0, "pulls_dropped": 0, "error": nil},
			map[string]any{"repo": "wstein/b", "source": "repo", "state": "skipped", "reason": "viewer-unknown", "open": nil, "pulls_dropped": nil, "error": nil},
			map[string]any{"repo": "wstein/c", "source": "workspace", "state": "partial", "reason": nil, "open": 1000, "pulls_dropped": 12, "error": "too-many-open-issues"},
			map[string]any{"repo": "wstein/d", "source": "workspace", "state": "error", "reason": nil, "open": nil, "pulls_dropped": nil, "error": "network"},
		}
		m["errors"] = []any{
			map[string]any{"repo": "wstein/d", "owner": nil, "code": "network", "message": "m"},
			map[string]any{"repo": nil, "owner": "wstein", "code": "search-incomplete", "message": "m"},
			map[string]any{"repo": nil, "owner": nil, "code": "viewer-unknown", "message": "m"},
		}
	})))
}

func TestIssuesSchemaRejectsBrokenDocuments(t *testing.T) {
	s := schema(t, "issues")
	top := func(fn func(m map[string]any)) string { return mutateIssues(t, fn) }
	red := func(fn func(r map[string]any)) string {
		return mutateIssues(t, func(m map[string]any) { fn(issueRow(m, "red")) })
	}
	blue := func(fn func(r map[string]any)) string {
		return mutateIssues(t, func(m map[string]any) { fn(issueRow(m, "blue")) })
	}
	opt := func(fn func(o map[string]any)) string {
		return mutateIssues(t, func(m map[string]any) { fn(m["options"].(map[string]any)) })
	}
	repo := func(fn func(r map[string]any)) string {
		return mutateIssues(t, func(m map[string]any) { fn(m["repos"].([]any)[0].(map[string]any)) })
	}
	moved := func(from, to string, fn func(r map[string]any)) string {
		return mutateIssues(t, func(m map[string]any) {
			g := m["issues"].(map[string]any)
			r := issueRow(m, from)
			fn(r)
			g[from], g[to] = []any{}, []any{r}
		})
	}
	for name, doc := range map[string]string{
		"extra top key":           top(func(m map[string]any) { m["extra"] = 1 }),
		"missing top key":         top(func(m map[string]any) { delete(m, "viewer") }),
		"schema version 2":        top(func(m map[string]any) { m["schema_version"] = 2 }),
		"other command":           top(func(m map[string]any) { m["command"] = "pulls" }),
		"time not UTC":            top(func(m map[string]any) { m["time"] = "2026-10-22T09:00:00+02:00" }),
		"viewer ok without login": top(func(m map[string]any) { m["viewer"].(map[string]any)["login"] = nil }),
		"viewer ok with error":    top(func(m map[string]any) { m["viewer"].(map[string]any)["error"] = "network" }),
		"viewer unknown state":    top(func(m map[string]any) { m["viewer"].(map[string]any)["state"] = "maybe" }),
		"viewer skipped no reason": top(func(m map[string]any) {
			m["viewer"] = map[string]any{"state": "skipped", "login": nil, "reason": nil, "error": nil}
		}),
		"viewer error no code": top(func(m map[string]any) {
			m["viewer"] = map[string]any{"state": "error", "login": nil, "reason": nil, "error": nil}
		}),
		"unknown scope":          opt(func(o map[string]any) { o["scope"] = "owner" }),
		"limit zero":             opt(func(o map[string]any) { o["limit"] = 0 }),
		"limit 501":              opt(func(o map[string]any) { o["limit"] = 501 }),
		"empty label":            opt(func(o map[string]any) { o["labels"] = []any{""} }),
		"label of 51":            opt(func(o map[string]any) { o["labels"] = []any{strings.Repeat("x", 51)} }),
		"bad assignee":           opt(func(o map[string]any) { o["assignees"] = []any{"@you"} }),
		"@me as author":          opt(func(o map[string]any) { o["authors"] = []any{"@me"} }),
		"extra option":           opt(func(o map[string]any) { o["x"] = true }),
		"unknown source":         repo(func(r map[string]any) { r["source"] = "org" }),
		"unknown state":          repo(func(r map[string]any) { r["state"] = "skip" }),
		"unknown repo error":     repo(func(r map[string]any) { r["error"] = "boom" }),
		"skipped without reason": repo(func(r map[string]any) { r["state"] = "skipped" }),
		"reason on a read repo":  repo(func(r map[string]any) { r["reason"] = "issues-disabled" }),
		"unknown reason":         repo(func(r map[string]any) { r["state"], r["reason"] = "skipped", "other" }),
		"negative open":          repo(func(r map[string]any) { r["open"] = -1 }),
		"missing issue group":    top(func(m map[string]any) { delete(m["issues"].(map[string]any), "violet") }),
		"extra issue group":      top(func(m map[string]any) { m["issues"].(map[string]any)["green"] = []any{} }),
		"row in the wrong group": top(func(m map[string]any) { issueRow(m, "red")["priority"] = "yellow" }),
		"red with other reason":  red(func(r map[string]any) { r["reason"] = "bug-label" }),
		"red without command":    red(func(r map[string]any) { r["command"], r["effect"] = nil, nil }),
		"red without effect":     red(func(r map[string]any) { r["effect"] = nil }),
		"red with write effect":  red(func(r map[string]any) { r["effect"] = "owner" }),
		"yellow with none reason": moved("red", "yellow", func(r map[string]any) {
			r["priority"], r["reason"] = "yellow", "none"
		}),
		"violet with security": moved("blue", "violet", func(r map[string]any) {
			r["priority"], r["reason"] = "violet", "security-label"
		}),
		"blue with command":     blue(func(r map[string]any) { r["command"], r["effect"] = "gh issue view 12 -R wstein/sourcesteward", "read" }),
		"blue with effect":      blue(func(r map[string]any) { r["effect"] = "read" }),
		"command from a title":  red(func(r map[string]any) { r["command"] = "gh issue view 41 -R wstein/sourcesteward; rm -rf /" }),
		"http url":              red(func(r map[string]any) { r["url"] = "http://github.com/o/r/issues/1" }),
		"number zero":           red(func(r map[string]any) { r["number"] = 0 }),
		"negative age":          red(func(r map[string]any) { r["age_days"] = -1 }),
		"too many labels":       red(func(r map[string]any) { r["labels"] = make([]any, 21) }),
		"too many assignees":    red(func(r map[string]any) { r["assignees"] = make([]any, 21) }),
		"negative labels total": red(func(r map[string]any) { r["labels_total"] = -1 }),
		"extra row key":         red(func(r map[string]any) { r["extra"] = 1 }),
		"missing row key":       red(func(r map[string]any) { delete(r, "labels_total") }),
		"bad row repo":          red(func(r map[string]any) { r["repo"] = "no-slash" }),
		"negative summary":      top(func(m map[string]any) { m["summary"].(map[string]any)["omitted"] = -1 }),
		"missing summary key":   top(func(m map[string]any) { delete(m["summary"].(map[string]any), "counts") }),
		"extra counts key":      top(func(m map[string]any) { m["summary"].(map[string]any)["counts"].(map[string]any)["green"] = 0 }),
		"unknown error code": top(func(m map[string]any) {
			m["errors"] = []any{map[string]any{"repo": "o/r", "owner": nil, "code": "boom", "message": "m"}}
		}),
		"old pulls error code": top(func(m map[string]any) {
			m["errors"] = []any{map[string]any{"repo": "o/r", "owner": nil, "code": "too-many-open-prs", "message": "m"}}
		}),
		"error with both subject": top(func(m map[string]any) {
			m["errors"] = []any{map[string]any{"repo": "o/r", "owner": "o", "code": "network", "message": "m"}}
		}),
		"error message too long": top(func(m map[string]any) {
			m["errors"] = []any{map[string]any{"repo": "o/r", "owner": nil, "code": "network", "message": strings.Repeat("x", 513)}}
		}),
	} {
		if err := s.Validate(value(t, []byte(doc))); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
