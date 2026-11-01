// SPDX-License-Identifier: EUPL-1.2
package pullscmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/watchcmd"
)

// reviewSource has o/a#1 CLEAR and ready, o/a#2 with FINDINGS, o/a#3 a draft with CLEAR, o/a#4 without any comment,
// o/a#5 with an old free-form verdict.
func reviewSource() reviewForge {
	s := newSource()
	rl := func(v, head string, n int) string {
		return fmt.Sprintf("Review: %s head=%s patch-id=%s pr=%d\nrest", v, head, strings.Repeat("1", 40), n)
	}
	for n := 1; n <= 5; n++ {
		p := okPR("o/a", n)
		p.age = time.Duration(n) * 24 * time.Hour
		p.draft = n == 3
		s.add(p)
		s.commits[p.head] = t0.Add(-time.Hour * 100)
	}
	h := func(n int) string { return s.lists["o/a"][n-1].HeadSHA }
	s.say("o/a", 1, 1, 5, rl("CLEAR", h(1), 1))
	s.say("o/a", 2, 1, 5, rl("FINDINGS", h(2), 2))
	s.say("o/a", 3, 1, 5, rl("CLEAR", h(3), 3))
	s.say("o/a", 5, 1, 5, "Verdict: CLEAR")
	return reviewForge{s}
}

func execReview(t *testing.T, src Source, f Flags) (string, string, error) {
	t.Helper()
	var out, diag bytes.Buffer
	p, err := prepare(context.Background(), f, &out, nil)
	if err != nil {
		t.Fatal(err)
	}
	p.Diag, p.Now = &diag, func() time.Time { return t0 }
	p.Open = func(context.Context, string, string) (Source, func(), error) { return src, func() {}, nil }
	err = Execute(context.Background(), p)
	return out.String(), diag.String(), err
}

func TestPrepareReviewFlags(t *testing.T) {
	f := baseFlags()
	f.Repos = []string{"o/a"}
	f.ReviewStates, f.Hints = []string{"clear", "findings"}, []string{"clear-not-ready"}
	p, err := prepare(context.Background(), f, &bytes.Buffer{}, nil)
	if err != nil || !p.Options.Review || strings.Join(p.Options.ReviewStates, ",") != "clear,findings" || strings.Join(p.Options.Hints, ",") != "clear-not-ready" {
		t.Fatalf("%+v %v", p.Options, err)
	}
	f = baseFlags()
	f.Repos, f.NoReview = []string{"o/a"}, true
	if p, err = prepare(context.Background(), f, &bytes.Buffer{}, nil); err != nil || p.Options.Review {
		t.Fatalf("--no-review: %+v %v", p.Options, err)
	}
	for name, mutate := range map[string]func(f *Flags){
		"unknown state":          func(f *Flags) { f.ReviewStates = []string{"clearish"} },
		"unknown hint":           func(f *Flags) { f.Hints = []string{"nope"} },
		"state with no-review":   func(f *Flags) { f.NoReview, f.ReviewStates = true, []string{"clear"} },
		"hint with no-review":    func(f *Flags) { f.NoReview, f.Hints = true, []string{"ready-mergeable"} },
		"empty state":            func(f *Flags) { f.ReviewStates = []string{""} },
		"upper-case state":       func(f *Flags) { f.ReviewStates = []string{"CLEAR"} },
		"state with a separator": func(f *Flags) { f.ReviewStates = []string{"clear,findings"} },
	} {
		f := baseFlags()
		f.Repos = []string{"o/a"}
		mutate(&f)
		if _, err := prepare(context.Background(), f, &bytes.Buffer{}, nil); !errors.Is(err, watchcmd.ErrUsage) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestExecuteJSONCarriesReviewAndHints(t *testing.T) {
	f := baseFlags()
	f.Repos, f.JSON = []string{"o/a"}, true
	out, diag, err := execReview(t, reviewSource(), f)
	if err != nil || diag != "" {
		t.Fatalf("%v %q", err, diag)
	}
	m := decode(t, out)
	rows := map[int]map[string]any{}
	for _, g := range []string{"ready", "checks", "conflict", "draft", "blocked"} {
		for _, r := range m["groups"].(map[string]any)[g].([]any) {
			row := r.(map[string]any)
			rows[int(row["number"].(float64))] = row
		}
	}
	state := func(n int) string { return rows[n]["review"].(map[string]any)["state"].(string) }
	if state(1) != "clear" || state(2) != "findings" || state(3) != "clear" || state(4) != "none" || state(5) != "unparsed" {
		t.Fatalf("%v", rows)
	}
	r1 := rows[1]["review"].(map[string]any)
	if r1["basis"] != "comment" || r1["verdict"] != "CLEAR" || r1["head"] != rows[1]["head"] || r1["carried"] != false || r1["edited"] != false || r1["truncated"] != false ||
		r1["at"] != "2026-10-22T09:05:00Z" || !strings.HasPrefix(r1["url"].(string), "https://") || r1["patch_id"] != strings.Repeat("1", 40) {
		t.Fatalf("%v", r1)
	}
	r4 := rows[4]["review"].(map[string]any)
	if r4["verdict"] != nil || r4["head"] != nil || r4["at"] != nil || r4["url"] != nil || r4["patch_id"] != nil {
		t.Fatalf("a state without a decisive comment has null evidence: %v", r4)
	}
	hints := func(n int) string {
		var out []string
		for _, h := range rows[n]["hints"].([]any) {
			out = append(out, h.(string))
		}
		return strings.Join(out, ",")
	}
	if hints(1) != "ready-mergeable" || hints(3) != "clear-not-ready" || hints(2) != "" || hints(4) != "no-final-review" || hints(5) != "no-final-review" {
		t.Fatalf("%v %v %v %v %v", hints(1), hints(2), hints(3), hints(4), hints(5))
	}
	o := m["options"].(map[string]any)
	if o["review"] != true || len(o["review_states"].([]any)) != 0 || len(o["hints"].([]any)) != 0 {
		t.Fatalf("%v", o)
	}
	s := m["summary"].(map[string]any)
	rv, hs := s["review"].(map[string]any), s["hints"].(map[string]any)
	if rv["clear"] != 2.0 || rv["findings"] != 1.0 || rv["none"] != 1.0 || rv["unparsed"] != 1.0 || rv["clear_carried"] != 0.0 || rv["clear_stale"] != 0.0 || rv["not_read"] != 0.0 {
		t.Fatalf("%v", rv)
	}
	if hs["ready_mergeable"] != 1.0 || hs["clear_not_ready"] != 1.0 || hs["no_final_review"] != 2.0 || hs["base_not_default"] != 0.0 || hs["parent_merged_retarget"] != 0.0 {
		t.Fatalf("%v", hs)
	}
}

func TestExecuteNoReviewReadsNoCommentsAndSaysSo(t *testing.T) {
	src := reviewSource()
	f := baseFlags()
	f.Repos, f.JSON, f.NoReview = []string{"o/a"}, true, true
	out, _, err := execReview(t, src, f)
	if err != nil {
		t.Fatal(err)
	}
	m := decode(t, out)
	if m["options"].(map[string]any)["review"] != false {
		t.Fatalf("%v", m["options"])
	}
	if src.count("comments/") != 0 {
		t.Fatalf("calls %v", src.calls)
	}
	for _, r := range m["groups"].(map[string]any)["ready"].([]any) {
		row := r.(map[string]any)
		if row["review"] != nil || len(row["hints"].([]any)) != 0 {
			t.Fatalf("%v", row)
		}
	}
}

func TestExecuteReviewAndHintFiltersSelectRows(t *testing.T) {
	cases := map[string]struct {
		mutate func(f *Flags)
		want   []int
	}{
		"clear":             {func(f *Flags) { f.ReviewStates = []string{"clear"} }, []int{1, 3}},
		"clear or findings": {func(f *Flags) { f.ReviewStates = []string{"clear", "findings"} }, []int{1, 2, 3}},
		"clear-not-ready":   {func(f *Flags) { f.Hints = []string{"clear-not-ready"} }, []int{3}},
		"no-final-review":   {func(f *Flags) { f.Hints = []string{"no-final-review"} }, []int{4, 5}},
		"ready-mergeable":   {func(f *Flags) { f.Hints = []string{"ready-mergeable"} }, []int{1}},
		"state and hint":    {func(f *Flags) { f.ReviewStates, f.Hints = []string{"clear"}, []string{"clear-not-ready"} }, []int{3}},
		"nothing matches":   {func(f *Flags) { f.ReviewStates = []string{"clear-stale"} }, nil},
	}
	for name, c := range cases {
		f := baseFlags()
		f.Repos, f.JSON = []string{"o/a"}, true
		c.mutate(&f)
		out, _, err := execReview(t, reviewSource(), f)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		m := decode(t, out)
		var got []int
		for _, g := range []string{"ready", "checks", "conflict", "draft", "blocked"} {
			got = append(got, groupNumbers(m, g)...)
		}
		sort.Ints(got)
		if fmt.Sprint(got) != fmt.Sprint(c.want) && !(len(got) == 0 && len(c.want) == 0) {
			t.Errorf("%s: %v want %v", name, got, c.want)
		}
	}
}

func TestExecuteHumanShowsReviewColumnHintsAndCounts(t *testing.T) {
	f := baseFlags()
	f.Repos, f.Human = []string{"o/a"}, true
	out, _, err := execReview(t, reviewSource(), f)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"o/a#1  1d  alice  green  clean  clear  title 1  {ready-mergeable}\n",
		"o/a#2  2d  alice  green  clean  findings  title 2\n",
		"o/a#3  3d  alice  -  -  clear  title 3  {clear-not-ready}\n",
		"o/a#4  4d  alice  green  clean  none  title 4  {no-final-review}\n",
		"o/a#5  5d  alice  green  clean  unparsed  title 5  {no-final-review}\n",
		"review: 2 clear, 1 findings, 1 none, 1 unparsed\n",
		"hints: 1 clear-not-ready, 2 no-final-review, 1 ready-mergeable\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}

func TestExecuteHumanWithoutReviewKeepsTheOldLines(t *testing.T) {
	f := baseFlags()
	f.Repos, f.Human, f.NoReview = []string{"o/a"}, true, true
	out, _, err := execReview(t, reviewSource(), f)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "o/a#1  1d  alice  green  clean  title 1\n") || strings.Contains(out, "review:") || strings.Contains(out, "{") {
		t.Fatalf("%s", out)
	}
}

func TestExecuteReviewReadFailureIsExitOne(t *testing.T) {
	src := reviewSource()
	src.errAt["comments/o/a#1"] = errors.New("boom")
	f := baseFlags()
	f.Repos, f.JSON = []string{"o/a"}, true
	out, diag, err := execReview(t, src, f)
	var fail *watchcmd.Failure
	if !errors.As(err, &fail) || fail.Code != watchcmd.ExitFailure {
		t.Fatalf("%v", err)
	}
	m := decode(t, out)
	if m["summary"].(map[string]any)["review"].(map[string]any)["not_read"] != 1.0 || diag == "" {
		t.Fatalf("%v %q", m["summary"], diag)
	}
}

func TestExecuteHumanAndJSONShowACarriedReview(t *testing.T) {
	src := reviewSource()
	p := okPR("o/a", 6)
	p.age = 6 * 24 * time.Hour
	src.add(p)
	src.commits[p.head] = t0.Add(-100 * time.Hour)
	old := sha("e")
	src.say("o/a", 6, 1, -1000, fmt.Sprintf("Review: CLEAR head=%s patch-id=%s pr=6", old, strings.Repeat("2", 40)))
	src.say("o/a", 6, 2, 3, fmt.Sprintf("Carried-over: from=%s to=%s patch-id=%s pr=6", old, p.head, strings.Repeat("2", 40)))
	f := baseFlags()
	f.Repos, f.Human = []string{"o/a"}, true
	out, _, err := execReview(t, src, f)
	if err != nil || !strings.Contains(out, "o/a#6  6d  alice  green  clean  clear(carried)  title 6  {ready-mergeable}\n") {
		t.Fatalf("%v\n%s", err, out)
	}
	f.Human, f.JSON = false, true
	out, _, err = execReview(t, src, f)
	if err != nil {
		t.Fatal(err)
	}
	m := decode(t, out)
	for _, r := range m["groups"].(map[string]any)["ready"].([]any) {
		row := r.(map[string]any)
		if row["number"] == 6.0 {
			rv := row["review"].(map[string]any)
			if rv["state"] != "clear-carried" || rv["carried"] != true || rv["head"] != old || rv["at"] != "2026-10-22T09:03:00Z" {
				t.Fatalf("%v", rv)
			}
			return
		}
	}
	t.Fatal("row 6 missing")
}
