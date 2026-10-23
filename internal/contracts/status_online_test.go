// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/overview"
)

// onlineStatus is the document `stw status --json` prints for the fixture workspace of internal/statuscmd
// (four repositories: everything read, drafts not visible, a deadline skip, a repository not read locally).
func onlineStatus(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root(t), "internal/statuscmd/testdata/document.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func jobj(v any) map[string]any { return v.(map[string]any) }

func actionOf(t *testing.T, m map[string]any, category string) map[string]any {
	t.Helper()
	for _, a := range m["actions"].([]any) {
		if jobj(a)["category"] == category {
			return jobj(a)
		}
	}
	t.Fatalf("the fixture has no %s item", category)
	return nil
}

func sourceOf(m map[string]any, repo int, name string) map[string]any {
	return jobj(jobj(jobj(jobj(m["repos"].([]any)[repo])["remote"])["sources"])[name])
}

func TestOnlineStatusSchemaAcceptsTheDocumentOfTheCommand(t *testing.T) {
	s := schemaAt(t, "v2", "status")
	raw, err := json.Marshal(onlineStatus(t))
	if err != nil {
		t.Fatal(err)
	}
	valid(t, s, raw)
}

// The schema lists the same categories, priorities and sources as the action model: a category added
// there without a schema rule is caught here.
func TestOnlineStatusSchemaFollowsTheActionModel(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(root(t), "schemas/v2/status.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	defs := jobj(doc["$defs"])
	strs := func(v any) []string {
		var out []string
		for _, e := range v.([]any) {
			out = append(out, e.(string))
		}
		return out
	}
	var cats []string
	for _, c := range overview.Categories() {
		cats = append(cats, string(c))
	}
	action := jobj(jobj(defs["action"])["properties"])
	if got := strs(jobj(action["category"])["enum"]); !reflect.DeepEqual(got, cats) {
		t.Errorf("categories %v, want %v", got, cats)
	}
	ruled := map[string]string{} // category -> priority required by the schema
	for _, r := range jobj(defs["action"])["allOf"].([]any) {
		cond, ok := jobj(jobj(jobj(r)["if"])["properties"])["category"]
		if !ok {
			continue
		}
		if p, ok := jobj(jobj(jobj(r)["then"])["properties"])["priority"]; ok {
			for _, c := range strs(jobj(cond)["enum"]) {
				ruled[c] = jobj(p)["const"].(string)
			}
		}
	}
	for _, c := range overview.Categories() {
		want, _ := overview.PriorityOf(c)
		if ruled[string(c)] != string(want) {
			t.Errorf("%s: the schema requires priority %q, the model says %q", c, ruled[string(c)], want)
		}
	}
	var ids []string
	for _, src := range overview.Sources {
		ids = append(ids, string(src))
	}
	ids = append(ids, "viewer") // the one `user` read of the run is a source of the document, not of a repository
	if got := strs(jobj(jobj(jobj(defs["source"])["properties"])["id"])["enum"]); !reflect.DeepEqual(got, append([]string{"local"}, ids...)) {
		t.Errorf("source ids %v", got)
	}
}

func TestOnlineStatusSchemaRejectsBrokenDocuments(t *testing.T) {
	s := schemaAt(t, "v2", "status")
	many := func(n int, item func(i int) any) []any {
		out := make([]any, n)
		for i := range out {
			out[i] = item(i)
		}
		return out
	}
	run := func() any {
		return map[string]any{"workflow": "release.yml", "conclusion": nil, "status": "queued", "url": nil}
	}
	for name, fn := range map[string]func(m map[string]any){
		"extra top key":                func(m map[string]any) { m["extra"] = 1 },
		"missing top key":              func(m map[string]any) { delete(m, "sources") },
		"missing host":                 func(m map[string]any) { delete(m, "host") },
		"source without a reason key":  func(m map[string]any) { delete(sourceOf(m, 0, "pulls"), "reason") },
		"source without an error key":  func(m map[string]any) { delete(sourceOf(m, 0, "pulls"), "error") },
		"schema 1":                     func(m map[string]any) { m["schema_version"] = 1 },
		"icons in the JSON":            func(m map[string]any) { jobj(m["options"])["icons"] = "unicode" },
		"time with an offset":          func(m map[string]any) { m["time"] = "2026-10-22T09:30:00+02:00" },
		"other host":                   func(m map[string]any) { m["host"] = "gitlab.com" },
		"extra item key":               func(m map[string]any) { actionOf(t, m, "pr-ready")["extra"] = 1 },
		"missing item key":             func(m map[string]any) { delete(actionOf(t, m, "pr-ready"), "hint") },
		"unknown category":             func(m map[string]any) { actionOf(t, m, "pr-ready")["category"] = "pr-merged" },
		"priority of another category": func(m map[string]any) { actionOf(t, m, "pr-ready")["priority"] = "red" },
		"subject kind of another one":  func(m map[string]any) { jobj(actionOf(t, m, "pr-ready")["subject"])["kind"] = "tag" },
		"pr without number":            func(m map[string]any) { jobj(actionOf(t, m, "pr-ready")["subject"])["number"] = nil },
		"number on a tag":              func(m map[string]any) { jobj(actionOf(t, m, "release-run-failed")["subject"])["number"] = 1 },
		"fact without name":            func(m map[string]any) { jobj(actionOf(t, m, "local-changes")["subject"])["name"] = nil },
		"name on a pr":                 func(m map[string]any) { jobj(actionOf(t, m, "pr-ready")["subject"])["name"] = "x" },
		"short sha":                    func(m map[string]any) { jobj(actionOf(t, m, "main-red")["subject"])["sha"] = "abc" },
		"effect without command":       func(m map[string]any) { actionOf(t, m, "pr-ready")["command"] = nil },
		"command without effect":       func(m map[string]any) { actionOf(t, m, "pr-ready")["effect"] = nil },
		"unknown effect":               func(m map[string]any) { actionOf(t, m, "pr-ready")["effect"] = "write" },
		"http url":                     func(m map[string]any) { actionOf(t, m, "pr-ready")["url"] = "http://github.com/o/r/pull/1" },
		"unknown evidence key":         func(m map[string]any) { jobj(actionOf(t, m, "local-changes")["evidence"])["extra"] = 1 },
		"missing evidence key":         func(m map[string]any) { delete(jobj(actionOf(t, m, "local-changes")["evidence"]), "staged") },
		"evidence of a pr group":       func(m map[string]any) { jobj(actionOf(t, m, "pr-ready")["evidence"])["group"] = "merged" },
		"no evidence for main-red":     func(m map[string]any) { actionOf(t, m, "main-red")["evidence"] = nil },
		"evidence on a null category":  func(m map[string]any) { actionOf(t, m, "main-behind-origin")["evidence"] = map[string]any{"x": 1} },
		"draft visibility not writable": func(m map[string]any) {
			jobj(actionOf(t, m, "release-draft")["evidence"])["drafts_visibility"] = "unknown"
		},
		"repo without remote":        func(m map[string]any) { delete(jobj(m["repos"].([]any)[0]), "remote") },
		"repo with an extra key":     func(m map[string]any) { jobj(m["repos"].([]any)[0])["extra"] = 1 },
		"repo without a v1 member":   func(m map[string]any) { delete(jobj(m["repos"].([]any)[0]), "reasons") },
		"unknown remote state":       func(m map[string]any) { jobj(jobj(m["repos"].([]any)[0])["remote"])["state"] = "partial" },
		"skipped without reason":     func(m map[string]any) { jobj(jobj(m["repos"].([]any)[3])["remote"])["reason"] = nil },
		"reason on an ok repository": func(m map[string]any) { jobj(jobj(m["repos"].([]any)[0])["remote"])["reason"] = "offline" },
		"unknown skip reason":        func(m map[string]any) { jobj(jobj(m["repos"].([]any)[3])["remote"])["reason"] = "tired" },
		"bad repo name":              func(m map[string]any) { jobj(jobj(m["repos"].([]any)[0])["remote"])["repo"] = "no-slash" },
		"extra source": func(m map[string]any) {
			jobj(jobj(jobj(m["repos"].([]any)[0])["remote"])["sources"])["wiki"] = sourceOf(m, 0, "repo")
		},
		"missing issues source": func(m map[string]any) {
			delete(jobj(jobj(jobj(m["repos"].([]any)[0])["remote"])["sources"]), "issues")
		},
		"issues data with an extra key":   func(m map[string]any) { jobj(sourceOf(m, 0, "issues")["data"])["x"] = 1 },
		"issues data without truncated":   func(m map[string]any) { delete(jobj(sourceOf(m, 0, "issues")["data"]), "truncated") },
		"issues data negative":            func(m map[string]any) { jobj(sourceOf(m, 0, "issues")["data"])["red"] = -1 },
		"issues data truncated as number": func(m map[string]any) { jobj(sourceOf(m, 0, "issues")["data"])["truncated"] = 1 },
		"issues-disabled on another source": func(m map[string]any) {
			s := sourceOf(m, 2, "ci_main")
			s["reason"] = "issues-disabled"
		},
		"issue without a number":     func(m map[string]any) { jobj(actionOf(t, m, "issue-security")["subject"])["number"] = nil },
		"issue kind on a pr":         func(m map[string]any) { jobj(actionOf(t, m, "pr-ready")["subject"])["kind"] = "issue" },
		"pr kind on an issue":        func(m map[string]any) { jobj(actionOf(t, m, "issue-security")["subject"])["kind"] = "pr" },
		"yellow security issue":      func(m map[string]any) { actionOf(t, m, "issue-security")["priority"] = "yellow" },
		"red assigned issue":         func(m map[string]any) { actionOf(t, m, "issue-assigned")["priority"] = "red" },
		"issue evidence unknown key": func(m map[string]any) { jobj(actionOf(t, m, "issue-security")["evidence"])["x"] = 1 },
		"issue evidence reason of a pr": func(m map[string]any) {
			jobj(actionOf(t, m, "issue-security")["evidence"])["reason"] = "checks-red"
		},
		"issue evidence without labels": func(m map[string]any) { delete(jobj(actionOf(t, m, "issue-security")["evidence"]), "labels") },
		"issue with 21 labels": func(m map[string]any) {
			jobj(actionOf(t, m, "issue-security")["evidence"])["labels"] = many(21, func(i int) any { return fmt.Sprintf("l%d", i) })
		},
		"issue with 21 assignees": func(m map[string]any) {
			jobj(actionOf(t, m, "issue-security")["evidence"])["assignees"] = many(21, func(i int) any { return fmt.Sprintf("a%d", i) })
		},
		"no evidence for an issue": func(m map[string]any) { actionOf(t, m, "issue-security")["evidence"] = nil },
		"eight sources":            func(m map[string]any) { m["sources"] = m["sources"].([]any)[:8] },
		"viewer before issues": func(m map[string]any) {
			s := m["sources"].([]any)
			s[7], s[8] = s[8], s[7]
		},
		"missing source": func(m map[string]any) {
			delete(jobj(jobj(jobj(m["repos"].([]any)[0])["remote"])["sources"]), "ci_main")
		},
		"ok source without data":        func(m map[string]any) { sourceOf(m, 0, "pulls")["data"] = nil },
		"skipped source with data":      func(m map[string]any) { sourceOf(m, 2, "ci_main")["data"] = sourceOf(m, 0, "ci_main")["data"] },
		"skipped source without reason": func(m map[string]any) { sourceOf(m, 2, "ci_main")["reason"] = nil },
		"error source without code":     func(m map[string]any) { s := sourceOf(m, 0, "tags"); s["state"], s["data"] = "error", nil },
		"unknown error code": func(m map[string]any) {
			s := sourceOf(m, 0, "tags")
			s["state"], s["data"], s["error"] = "error", nil, "boom"
		},
		"error source with a reason": func(m map[string]any) {
			s := sourceOf(m, 0, "tags")
			s["state"], s["data"], s["error"], s["reason"] = "error", nil, "network", "deadline"
		},
		"pulls data with an extra key": func(m map[string]any) { jobj(sourceOf(m, 0, "pulls")["data"])["x"] = 1 },
		"pulls data negative":          func(m map[string]any) { jobj(sourceOf(m, 0, "pulls")["data"])["open"] = -1 },
		"repo data http page":          func(m map[string]any) { jobj(sourceOf(m, 0, "repo")["data"])["html_url"] = "http://github.com/o/r" },
		"101 drafts": func(m map[string]any) {
			jobj(sourceOf(m, 0, "releases")["data"])["drafts"] = many(101, func(int) any {
				return map[string]any{"name": nil, "tag": "v1", "created_at": nil, "assets": 0, "url": nil}
			})
		},
		"6 unreleased tags": func(m map[string]any) {
			jobj(sourceOf(m, 0, "tags")["data"])["unreleased"] = many(6, func(i int) any {
				return map[string]any{"pattern": "v*", "tag": fmt.Sprintf("v%d", i), "sha": strings.Repeat("a", 40), "run": nil}
			})
		},
		"unreleased tag with a short sha": func(m map[string]any) {
			jobj(jobj(sourceOf(m, 0, "tags")["data"])["unreleased"].([]any)[0])["sha"] = "abc"
		},
		"main class unknown": func(m map[string]any) { jobj(sourceOf(m, 0, "ci_main")["data"])["class"] = "blue" },
		"main head short":    func(m map[string]any) { jobj(sourceOf(m, 0, "ci_main")["data"])["head"] = "abc" },
		"21 runs": func(m map[string]any) {
			jobj(jobj(sourceOf(m, 0, "ci_release")["data"])["tags"].([]any)[0])["runs"] = many(21, func(int) any { return run() })
		},
		"unknown run conclusion": func(m map[string]any) {
			jobj(jobj(jobj(sourceOf(m, 0, "tags")["data"])["unreleased"].([]any)[0])["run"])["conclusion"] = "exploded"
		},
		"release tag without workflow": func(m map[string]any) {
			delete(jobj(jobj(sourceOf(m, 0, "ci_release")["data"])["tags"].([]any)[0]), "workflow")
		},
		"six sources":           func(m map[string]any) { m["sources"] = m["sources"].([]any)[:6] },
		"sources out of order":  func(m map[string]any) { s := m["sources"].([]any); s[0], s[1] = s[1], s[0] },
		"unknown source state":  func(m map[string]any) { jobj(m["sources"].([]any)[1])["state"] = "fine" },
		"negative source count": func(m map[string]any) { jobj(m["sources"].([]any)[1])["read"] = -1 },
		"notice message too long": func(m map[string]any) {
			jobj(m["notices"].([]any)[0])["message"] = strings.Repeat("x", 513)
		},
		"notice with 257 repos": func(m map[string]any) {
			jobj(m["notices"].([]any)[0])["repos"] = many(257, func(i int) any { return fmt.Sprintf("r%d", i) })
		},
		"notice without repos": func(m map[string]any) { delete(jobj(m["notices"].([]any)[0]), "repos") },
		"missing priority":     func(m map[string]any) { delete(jobj(jobj(m["summary"])["priorities"]), "violet") },
		"negative summary":     func(m map[string]any) { jobj(m["summary"])["errors"] = -1 },
		"summary failed text":  func(m map[string]any) { jobj(m["summary"])["failed"] = "yes" },
		"error without status code": func(m map[string]any) {
			m["error"] = map[string]any{"code": "sync-x", "message": "m"}
		},
	} {
		m := onlineStatus(t)
		fn(m)
		raw, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Validate(value(t, raw)); err == nil {
			t.Errorf("%s: the schema accepted a broken document", name)
		}
	}
}

// The offline document is the schema 1 document, byte for byte what it was; the two schemas never accept each other's.
func TestOnlineAndOfflineStatusDocumentsStayApart(t *testing.T) {
	v1, v2 := schema(t, "status"), schemaAt(t, "v2", "status")
	valid(t, v1, statusFixture(t, "ok"))
	if err := v2.Validate(value(t, statusFixture(t, "ok"))); err == nil {
		t.Error("the schema 2 accepted a schema 1 document")
	}
	raw, _ := json.Marshal(onlineStatus(t))
	if err := v1.Validate(value(t, raw)); err == nil {
		t.Error("the schema 1 accepted a schema 2 document")
	}
}
