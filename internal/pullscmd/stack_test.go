// SPDX-License-Identifier: EUPL-1.2
package pullscmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/watchcmd"
)

func TestPrepareStackConflicts(t *testing.T) {
	for name, mutate := range map[string]func(f *Flags){
		"with mergeable": func(f *Flags) { f.Mergeable = true },
		"with review":    func(f *Flags) { f.ReviewStates = []string{"clear"} },
		"with hint":      func(f *Flags) { f.Hints = []string{"ready-mergeable"} },
		"with no-review": func(f *Flags) { f.NoReview = true },
	} {
		f := baseFlags()
		f.Repos, f.Stack = []string{"o/a"}, true
		mutate(&f)
		if _, err := prepare(context.Background(), f, &bytes.Buffer{}, nil); !errors.Is(err, watchcmd.ErrUsage) {
			t.Errorf("%s: %v", name, err)
		}
	}
	f := baseFlags()
	f.Repos, f.Stack = []string{"o/a"}, true
	if p, err := prepare(context.Background(), f, &bytes.Buffer{}, nil); err != nil || !p.Stack || !p.Options.Review {
		t.Fatalf("%v %+v", err, p.Options)
	}
}

func TestExecuteStackJSON(t *testing.T) {
	f := baseFlags()
	f.Repos, f.JSON, f.Stack = []string{"o/a"}, true, true
	out, diag, err := execReview(t, stackSource(), f)
	if err != nil || diag != "" {
		t.Fatalf("%v %q", err, diag)
	}
	m := decode(t, out)
	if m["options"].(map[string]any)["stack"] != true || m["owner_queue"] != nil {
		t.Fatalf("%v", m["options"])
	}
	st := m["stack"].(map[string]any)
	trees := st["trees"].([]any)
	if len(trees) != 1 || st["singles"] != 2.0 {
		t.Fatalf("%v", st)
	}
	root := trees[0].(map[string]any)
	if root["number"] != 1.0 || root["review"] != "clear" || root["checks"] != "green" || root["group"] != "ready" || root["base"] != "main" || root["head_ref"] != "stack-1" {
		t.Fatalf("%v", root)
	}
	kid := root["children"].([]any)[0].(map[string]any)
	grand := kid["children"].([]any)[0].(map[string]any)
	if kid["number"] != 2.0 || grand["number"] != 3.0 || len(grand["children"].([]any)) != 0 || fmt.Sprint(kid["hints"]) != "[base-not-default]" {
		t.Fatalf("%v %v", kid, grand)
	}
}

func TestExecuteWithoutStackHasNoStackMember(t *testing.T) {
	f := baseFlags()
	f.Repos, f.JSON = []string{"o/a"}, true
	out, _, err := execReview(t, stackSource(), f)
	if err != nil {
		t.Fatal(err)
	}
	m := decode(t, out)
	if v, ok := m["stack"]; !ok || v != nil || m["options"].(map[string]any)["stack"] != false {
		t.Fatalf("stack must be present and null: %v %v", v, ok)
	}
}

func TestExecuteStackHuman(t *testing.T) {
	f := baseFlags()
	f.Repos, f.Human, f.Stack = []string{"o/a"}, true, true
	out, _, err := execReview(t, stackSource(), f)
	if err != nil {
		t.Fatal(err)
	}
	want := "o/a#1  ready  checks green  review clear  title 1  {ready-mergeable}\n" +
		"`- o/a#2  ready  checks green  review clear  title 2  {base-not-default}\n" +
		"   `- o/a#3  ready  checks green  review clear  title 3  {base-not-default}\n" +
		"\n2 single pull requests not shown\n"
	if out != want {
		t.Fatalf("%q\nwant %q", out, want)
	}
}

func TestExecuteStackHumanDrawsSiblingsAndOrphans(t *testing.T) {
	src := stackSource()
	// o/a#4 gets a second child of #1 and an orphan: base branch with no open pull request
	extra := okPR("o/a", 6)
	extra.base, extra.headRef = "stack-1", "side"
	src.add(extra)
	src.commits[extra.head] = t0.Add(-1000 * 3600e9)
	orphan := okPR("o/a", 7)
	orphan.base, orphan.headRef = "gone-parent", "orphan"
	src.add(orphan)
	f := baseFlags()
	f.Repos, f.Human, f.Stack = []string{"o/a"}, true, true
	out, _, err := execReview(t, src, f)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"o/a#1  ready",
		"|- o/a#2  ",
		"|  `- o/a#3  ",
		"`- o/a#6  ",
		"o/a#7  ready  checks green  review none  title 7  (base gone-parent)  {no-final-review, base-not-default}\n",
		"2 single pull requests not shown\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}

func TestExecuteStackHumanWhenThereAreNoStacks(t *testing.T) {
	src := newSource()
	src.add(okPR("o/a", 1))
	f := baseFlags()
	f.Repos, f.Human, f.Stack = []string{"o/a"}, true, true
	out, _, err := execReview(t, reviewForge{src}, f)
	if err != nil || out != "no stacked pull requests\n\n1 single pull request not shown\n" {
		t.Fatalf("%v %q", err, out)
	}
}

func TestExecuteStackEscapesUntrustedText(t *testing.T) {
	src := stackSource()
	src.lists["o/a"][1].Title = "evil\x1b[31m\ntitle"
	src.lists["o/a"][0].BaseRef = "x\x1b[1m"
	f := baseFlags()
	f.Repos, f.Human, f.Stack = []string{"o/a"}, true, true
	out, _, err := execReview(t, src, f)
	if err != nil || strings.ContainsRune(out, 0x1b) || !strings.Contains(out, "evil\\x1b[31m\\ntitle") {
		t.Fatalf("%v %q", err, out)
	}
}

func TestExecuteStackHumanSeparatesTrees(t *testing.T) {
	src := stackSource()
	for n, base := range map[int]string{7: "main", 8: "t7"} {
		p := okPR("o/b", n)
		p.base = base
		p.headRef = fmt.Sprintf("t%d", n)
		src.add(p)
		src.commits[p.head] = t0
	}
	f := baseFlags()
	f.Repos, f.Human, f.Stack = []string{"o/a", "o/b"}, true, true
	out, _, err := execReview(t, src, f)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "   `- o/a#3  ") || !strings.Contains(out, "\n\no/b#7  ") || !strings.Contains(out, "`- o/b#8  ") {
		t.Fatalf("trees are separated by an empty line:\n%s", out)
	}
}

func TestExecuteStackShowsNoStrangersCommentAsAReview(t *testing.T) {
	src := stackSource()
	src.fakeSource.comments["o/a#1"][0].Association = "NONE" // the CLEAR of the root is a stranger's
	f := baseFlags()
	f.Repos, f.JSON, f.Stack = []string{"o/a"}, true, true
	out, _, err := execReview(t, src, f)
	if err != nil {
		t.Fatal(err)
	}
	root := decode(t, out)["stack"].(map[string]any)["trees"].([]any)[0].(map[string]any)
	if root["number"] != 1.0 || root["review"] != "none" || fmt.Sprint(root["hints"]) != "[no-final-review foreign-review-ignored]" {
		t.Fatalf("a stranger's CLEAR must show as none, with the ignored hint: %v", root)
	}
	f.JSON, f.Human = false, true
	out, _, err = execReview(t, src, f)
	if err != nil || !strings.Contains(out, "o/a#1") || strings.Contains(strings.SplitN(out, "o/a#2", 2)[0], "clear") {
		t.Fatalf("%v\n%s", err, out)
	}
}
