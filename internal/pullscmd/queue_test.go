// SPDX-License-Identifier: EUPL-1.2
package pullscmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/watchcmd"
)

// stackSource has a CLEAR bottom o/a#1 (head branch stack-1), a CLEAR child #2 based on it, a grandchild #3, an
// independent CLEAR pull request o/a#4 that is older than #1, and a Draft CLEAR #5.
func stackSource() reviewForge {
	s := newSource()
	rl := func(head string, n int) string {
		return fmt.Sprintf("Review: CLEAR head=%s patch-id=%s pr=%d", head, strings.Repeat("1", 40), n)
	}
	spec := []struct {
		n             int
		age           time.Duration
		base, headRef string
		draft         bool
	}{
		{1, 3 * 24 * time.Hour, "main", "stack-1", false},
		{2, 2 * 24 * time.Hour, "stack-1", "stack-2", false},
		{3, 24 * time.Hour, "stack-2", "stack-3", false},
		{4, 5 * 24 * time.Hour, "main", "solo", false},
		{5, time.Hour, "main", "drafty", true},
	}
	for _, c := range spec {
		p := okPR("o/a", c.n)
		p.age, p.base, p.headRef, p.draft = c.age, c.base, c.headRef, c.draft
		s.add(p)
		s.commits[p.head] = t0.Add(-1000 * time.Hour)
		s.say("o/a", c.n, 1, 5, rl(p.head, c.n))
	}
	return reviewForge{s}
}

func TestPrepareMergeableConflicts(t *testing.T) {
	for name, mutate := range map[string]func(f *Flags){
		"with review":    func(f *Flags) { f.ReviewStates = []string{"clear"} },
		"with hint":      func(f *Flags) { f.Hints = []string{"ready-mergeable"} },
		"with no-review": func(f *Flags) { f.NoReview = true },
	} {
		f := baseFlags()
		f.Repos, f.Mergeable = []string{"o/a"}, true
		mutate(&f)
		if _, err := prepare(context.Background(), f, &bytes.Buffer{}, nil); !errors.Is(err, watchcmd.ErrUsage) {
			t.Errorf("%s: %v", name, err)
		}
	}
	f := baseFlags()
	f.Repos, f.Mergeable = []string{"o/a"}, true
	p, err := prepare(context.Background(), f, &bytes.Buffer{}, nil)
	if err != nil || !p.Options.Review || !p.Mergeable {
		t.Fatalf("%+v %v", p.Options, err)
	}
}

func TestExecuteMergeableJSONHasTheOwnerQueue(t *testing.T) {
	f := baseFlags()
	f.Repos, f.JSON, f.Mergeable = []string{"o/a"}, true, true
	out, diag, err := execReview(t, stackSource(), f)
	if err != nil || diag != "" {
		t.Fatalf("%v %q", err, diag)
	}
	m := decode(t, out)
	if m["options"].(map[string]any)["mergeable"] != true {
		t.Fatalf("%v", m["options"])
	}
	q := m["owner_queue"].([]any)
	if len(q) != 2 {
		t.Fatalf("%v", q)
	}
	first, second := q[0].(map[string]any), q[1].(map[string]any)
	if first["number"] != 4.0 || first["position"] != 1.0 || second["number"] != 1.0 || second["position"] != 2.0 {
		t.Fatalf("oldest first: %v", q)
	}
	if len(first["head"].(string)) != 40 || first["url"] != "https://github.com/o/a/pull/4" {
		t.Fatalf("%v", first)
	}
	if fmt.Sprint(second["then"]) != "[2 3]" || fmt.Sprint(first["then"]) != "[]" || second["review"] != "clear" ||
		!strings.HasPrefix(second["reason"].(string), "CLEAR for head ") || !strings.HasSuffix(second["reason"].(string), ", base main") {
		t.Fatalf("%v", second)
	}
	if m["summary"].(map[string]any)["waiting_for_parent"] != 2.0 {
		t.Fatalf("%v", m["summary"])
	}
	if len(m["groups"].(map[string]any)["ready"].([]any)) != 4 || m["summary"].(map[string]any)["total"] != 5.0 {
		t.Fatalf("the groups stay those of the list: %v", m["groups"])
	}
}

func TestExecuteWithoutMergeableHasNoOwnerQueue(t *testing.T) {
	f := baseFlags()
	f.Repos, f.JSON = []string{"o/a"}, true
	out, _, err := execReview(t, stackSource(), f)
	if err != nil {
		t.Fatal(err)
	}
	m := decode(t, out)
	if v, ok := m["owner_queue"]; !ok || v != nil {
		t.Fatalf("owner_queue must be present and null: %v %v", v, ok)
	}
	if m["options"].(map[string]any)["mergeable"] != false || m["summary"].(map[string]any)["waiting_for_parent"] != 0.0 {
		t.Fatalf("%v %v", m["options"], m["summary"])
	}
}

func TestExecuteMergeableHuman(t *testing.T) {
	f := baseFlags()
	f.Repos, f.Human, f.Mergeable = []string{"o/a"}, true, true
	out, _, err := execReview(t, stackSource(), f)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"merge now (2)\n",
		"1. o/a#4  title 4\n   CLEAR for head ",
		", base main\n",
		"2. o/a#1  title 1\n",
		"   then: #2, #3\n",
		"2 can be merged now, 2 waiting for a parent\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "ready (") || strings.Contains(out, "draft (") {
		t.Fatalf("the list groups are replaced:\n%s", out)
	}
}

func TestExecuteMergeableHumanWhenNothingCanBeMerged(t *testing.T) {
	src := stackSource()
	for i := range src.lists["o/a"] {
		src.lists["o/a"][i].Draft = true // every pull request is a Draft
	}
	f := baseFlags()
	f.Repos, f.Human, f.Mergeable = []string{"o/a"}, true, true
	out, _, err := execReview(t, src, f)
	if err != nil || out != "nothing can be merged now\n" {
		t.Fatalf("%v %q", err, out)
	}
}

func TestExecuteMergeableEscapesUntrustedTitles(t *testing.T) {
	src := stackSource()
	src.lists["o/a"][3].Title = "evil\x1b[31m\ntitle"
	f := baseFlags()
	f.Repos, f.Human, f.Mergeable = []string{"o/a"}, true, true
	out, _, err := execReview(t, src, f)
	if err != nil || strings.ContainsRune(out, 0x1b) || !strings.Contains(out, "evil\\x1b[31m\\ntitle") {
		t.Fatalf("%v %q", err, out)
	}
}
