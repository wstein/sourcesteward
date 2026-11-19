// SPDX-License-Identifier: EUPL-1.2
package watchhost

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/watch"
	"github.com/wstein/sourcesteward/internal/watchexport"
)

type commitsIn struct {
	Body  string `json:"body"`
	Error string `json:"transport_error"` // "", "malformed" or "other"
}

type commitsOut struct {
	SHAs      []string `json:"shas"`
	More      bool     `json:"more"`
	ErrorKind string   `json:"error_kind"` // "", "malformed" or "other"
	Request   string   `json:"request"`
}

func TestExportScenarios(t *testing.T) {
	dir, err := watchexport.OutDir()
	if err != nil {
		t.Skip("STW_EXPORT_DIR not set: " + err.Error())
	}
	a, b := strings.Repeat("a", 40), strings.Repeat("b", 40)
	run := func(in commitsIn) commitsOut {
		f := &fakePullCommits{body: in.Body}
		switch in.Error {
		case "malformed":
			f.err = &hosting.WatchError{Code: hosting.CodeMalformed, Message: "x"}
		case "other":
			f.err = &hosting.WatchError{Code: hosting.CodeNetwork, Message: "x"}
		}
		shas, more, err := NewHostingCommits(f).PullCommits(context.Background(), "o/r", 12, 2)
		out := commitsOut{SHAs: shas, More: more}
		if out.SHAs == nil {
			out.SHAs = []string{}
		}
		switch {
		case err == nil:
		case errors.Is(err, watch.ErrMalformed):
			out.ErrorKind = "malformed"
		default:
			out.ErrorKind = "other"
		}
		if len(f.got) > 0 {
			out.Request = f.got[0]
		}
		return out
	}
	v := watchexport.NewVectors("commit-list-decode", "decode_pull_commits", "The page of pulls/N/commits as the restacker reads it: the 40-hex SHAs in order, `more` when the page holds 100 items, a malformed body or a malformed transport error as `malformed`, any other transport error as `other`; the page is read unconditionally (no validator).",
		nil, "watchhost/commits_test.go:TestHostingCommitsDecodes", "watchhost/commits_test.go:TestHostingCommitsFullPageHasMore", "watchhost/commits_test.go:TestHostingCommitsMalformed")
	add := func(name string, in commitsIn) { v.Add(name, in, run(in)) }
	add("two commits, extra fields ignored", commitsIn{Body: `[{"sha":"` + a + `","commit":{"message":"x"}},{"sha":"` + b + `"}]`})
	var parts []string
	for i := 0; i < 100; i++ {
		parts = append(parts, `{"sha":"`+strings.Repeat("ab", 20)+`"}`)
	}
	add("a full page of 100 has more", commitsIn{Body: "[" + strings.Join(parts, ",") + "]"})
	add("not json", commitsIn{Body: `{`})
	add("not an array", commitsIn{Body: `{}`})
	add("sha that is not 40 hex", commitsIn{Body: `[{"sha":"XYZ"}]`})
	add("uppercase sha", commitsIn{Body: `[{"sha":"` + strings.ToUpper(a) + `"}]`})
	add("empty list", commitsIn{Body: `[]`})
	add("transport error malformed", commitsIn{Body: `[]`, Error: "malformed"})
	add("transport error other", commitsIn{Body: `[]`, Error: "other"})
	if err := watchexport.Write(dir, v.Name, v); err != nil {
		t.Fatal(err)
	}

	// the hosting name check of the watch restacker
	type restIn struct {
		HeadRefs []string `json:"head_refs_on_the_merged_branch_and_on_main"`
	}
	old := a
	w := watchexport.NewVectors("restack-hosting-name-check", "restack_with_hosting_branch_check", "The restacker of `stw watch` refuses a head branch that the hosting reader cannot put into a URL segment (a percent sign, for example) with a diagnostic, and goes on with the others.",
		nil, "watchhost/commits_test.go:TestNewRestackerUsesHostingNameCheck")
	f := &fakePullCommits{body: `[{"sha":"` + old + `"}]`}
	var diag []string
	rs := NewRestacker(f, func(s string) { diag = append(diag, s) })
	due := watch.RestackDue{MergedPR: 10, OldParent: old, Base: "main", HeadRef: "p"}
	open := []watch.OpenPR{{Number: 11, Base: "p", HeadRef: "bad%20name"}, {Number: 12, Base: "main", HeadRef: "feat/ok"}}
	got, err := rs.Candidates(context.Background(), "o/r", due, open)
	if err != nil {
		t.Fatal(err)
	}
	var prs []int
	for _, g := range got {
		prs = append(prs, g.PR)
	}
	_ = restIn{}
	w.Add("percent in a head branch is skipped, a plain name is kept", map[string]any{
		"due":      map[string]any{"merged_pr": 10, "old_parent": old, "base": "main", "head_ref": "p"},
		"open":     []map[string]any{{"number": 11, "base": "p", "head_ref": "bad%20name"}, {"number": 12, "base": "main", "head_ref": "feat/ok"}},
		"commit_page_body": f.body,
	}, map[string]any{"candidate_prs": prs, "diagnostics": len(diag), "commit_reads": len(f.got)})
	if err := watchexport.Write(dir, w.Name, w); err != nil {
		t.Fatal(err)
	}
}
