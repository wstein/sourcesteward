// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/readycheck"
	"github.com/wstein/sourcesteward/internal/watchcmd"
)

// readyCheckExample is the document of specs/048-stw-ready-check/contracts/ready-check.md.
const readyCheckExample = `{"schema_version":1,"command":"ready-check","time":"2026-11-01T09:00:00Z","host":"github.com","repo":"wstein/sourcesteward","number":343,"head":"3f2a9c1000000000000000000000000000000000","draft":true,"ready":true,"conditions":[{"id":"open","status":"pass","evidence":"state open"},{"id":"review","status":"pass","evidence":"CLEAR for head 3f2a9c1 per comment"},{"id":"checks","status":"pass","evidence":"all check runs and statuses of head 3f2a9c1 are green"},{"id":"base","status":"pass","evidence":"base main is the default branch"},{"id":"blocker","status":"pass","evidence":"no Blocker line names a blocker"},{"id":"conflict","status":"pass","evidence":"no conflict with the base (state draft)"},{"id":"rebaseable","status":"pass","evidence":"rebaseable is true"}],"errors":[]}`

func mutateReadyCheck(t *testing.T, fn func(m map[string]any)) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(readyCheckExample), &m); err != nil {
		t.Fatal(err)
	}
	fn(m)
	b, _ := json.Marshal(m)
	return string(b)
}

func readyCond(m map[string]any, i int) map[string]any {
	return m["conditions"].([]any)[i].(map[string]any)
}

func TestReadyCheckSchemaAcceptsTheContractExampleAndItsVariants(t *testing.T) {
	s := schema(t, "ready-check")
	valid(t, s, []byte(readyCheckExample))
	valid(t, s, []byte(mutateReadyCheck(t, func(m map[string]any) {
		m["ready"] = false
		readyCond(m, 1)["status"] = "fail"
		readyCond(m, 2)["status"] = "unknown"
		m["draft"] = false
	})))
	// Nothing could be read: no head, no draft, every condition unknown, one error per failed read.
	valid(t, s, []byte(mutateReadyCheck(t, func(m map[string]any) {
		m["ready"], m["head"], m["draft"] = false, nil, nil
		for i := range m["conditions"].([]any) {
			readyCond(m, i)["status"] = "unknown"
		}
		m["errors"] = []any{map[string]any{"source": "pull", "code": "rate-limited", "message": "m"}, map[string]any{"source": "repo", "code": "x", "message": "m"}}
	})))
}

func TestReadyCheckSchemaRejectsBrokenDocuments(t *testing.T) {
	s := schema(t, "ready-check")
	top := func(fn func(m map[string]any)) string { return mutateReadyCheck(t, fn) }
	for name, doc := range map[string]string{
		"extra top key":        top(func(m map[string]any) { m["extra"] = 1 }),
		"missing top key":      top(func(m map[string]any) { delete(m, "draft") }),
		"schema version 2":     top(func(m map[string]any) { m["schema_version"] = 2 }),
		"other command":        top(func(m map[string]any) { m["command"] = "pulls" }),
		"time not UTC":         top(func(m map[string]any) { m["time"] = "2026-11-01T09:00:00+01:00" }),
		"number zero":          top(func(m map[string]any) { m["number"] = 0 }),
		"bad repo":             top(func(m map[string]any) { m["repo"] = "o" }),
		"short head":           top(func(m map[string]any) { m["head"] = "3f2a9c1" }),
		"six conditions":       top(func(m map[string]any) { m["conditions"] = m["conditions"].([]any)[:6] }),
		"eight conditions":     top(func(m map[string]any) { m["conditions"] = append(m["conditions"].([]any), readyCond(m, 0)) }),
		"conditions reordered": top(func(m map[string]any) { c := m["conditions"].([]any); c[0], c[1] = c[1], c[0] }),
		"unknown status":       top(func(m map[string]any) { m["ready"] = false; readyCond(m, 0)["status"] = "maybe" }),
		"missing evidence":     top(func(m map[string]any) { delete(readyCond(m, 0), "evidence") }),
		"long evidence":        top(func(m map[string]any) { readyCond(m, 0)["evidence"] = strings.Repeat("x", 401) }),
		"extra condition key":  top(func(m map[string]any) { readyCond(m, 0)["x"] = 1 }),
		"ready with a fail":    top(func(m map[string]any) { readyCond(m, 3)["status"] = "fail" }),
		"ready with unknown":   top(func(m map[string]any) { readyCond(m, 3)["status"] = "unknown" }),
		"ready without head":   top(func(m map[string]any) { m["head"] = nil }),
		"ready with error": top(func(m map[string]any) {
			m["errors"] = []any{map[string]any{"source": "pull", "code": "x", "message": "m"}}
		}),
		"unknown error source": top(func(m map[string]any) {
			m["ready"] = false
			m["errors"] = []any{map[string]any{"source": "comments", "code": "x", "message": "m"}}
		}),
	} {
		var v any
		if err := json.Unmarshal([]byte(doc), &v); err != nil {
			t.Fatal(name, err)
		}
		if err := s.Validate(v); err == nil {
			t.Errorf("%s: accepted %s", name, doc)
		}
	}
}

// rcSource is a forge with one pull request; the nil Reader panics on any read it must not make.
type rcSource struct {
	watchcmd.Reader
	pull string
	fail bool
}

func (f rcSource) PullDetail(context.Context, string, int, string) (hosting.WatchResponse, error) {
	if f.fail {
		return hosting.WatchResponse{}, errors.New("boom")
	}
	return hosting.WatchResponse{Status: 200, Body: []byte(f.pull)}, nil
}
func (f rcSource) CheckRuns(context.Context, string, string, int, string) (hosting.WatchResponse, error) {
	return hosting.WatchResponse{Status: 200, Body: []byte(`{"total_count":1,"check_runs":[{"name":"ci","status":"completed","conclusion":"success","output":{}}]}`)}, nil
}
func (f rcSource) Statuses(context.Context, string, string, int, string) (hosting.WatchResponse, error) {
	return hosting.WatchResponse{Status: 200, Body: []byte(`[]`)}, nil
}
func (f rcSource) RepoInfo(context.Context, string) (hosting.RepoInfo, error) {
	return hosting.RepoInfo{DefaultBranch: "main", HTMLURL: "https://github.com/o/r"}, nil
}

func TestReadyCheckSchemaValidatesRealOutput(t *testing.T) {
	s := schema(t, "ready-check")
	head := strings.Repeat("3", 40)
	pull := `{"state":"open","draft":true,"body":"Blocker: none","mergeable":true,"mergeable_state":"draft","rebaseable":true,"head":{"sha":"` + head + `"},"base":{"ref":"main"}}`
	clear := func(_ context.Context, _ string, _ int, h string) (readycheck.Review, error) {
		return readycheck.Review{State: "clear", Head: h}, nil
	}
	now := func() time.Time { return time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC) }
	in := readycheck.Input{Repo: "o/r", Number: 7, Host: "github.com"}
	for name, src := range map[string]rcSource{"ready": {pull: pull}, "unreadable": {fail: true}} {
		d := readycheck.Run(context.Background(), src, clear, in, now)
		if d.Ready != (name == "ready") {
			t.Fatalf("%s: %+v", name, d)
		}
		raw, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		valid(t, s, raw)
	}
}
