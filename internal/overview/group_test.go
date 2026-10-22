// SPDX-License-Identifier: EUPL-1.2

package overview

import (
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

func prItem(c Category, repo string, n int) Item {
	it := mk(c, repo, Subject{Kind: KindPR, Number: n})
	it.URL = fmt.Sprintf("https://github.com/%s/pull/%d", repo, n)
	it.Command, it.Effect = buildCommand(c, target{Slug: repo, Number: n})
	return it
}

func TestGroupJoinsPriorityCategoryRepo(t *testing.T) {
	items := []Item{
		prItem(PRBot, "w/h", 3), prItem(PRBot, "w/h", 4), // grouped
		prItem(PRBot, "w/other", 5), // other repository: own line
		prItem(PRReady, "w/h", 1),   // other category and priority: own line
		prItem(PRReady, "w/h", 2),
	}
	sections := Group(items)
	if len(sections) != 2 || sections[0].Priority != Green || sections[1].Priority != Violet {
		t.Fatalf("%+v", sections)
	}
	green := sections[0].Lines
	if len(green) != 1 || green[0].Count != 2 || green[0].Category != PRReady {
		t.Fatalf("%+v", green)
	}
	violet := sections[1].Lines
	if len(violet) != 2 || violet[0].Repo != "w/h" || violet[0].Count != 2 || violet[1].Repo != "w/other" || violet[1].Count != 1 {
		t.Fatalf("%+v", violet)
	}
	if got := violet[0].Labels; !reflect.DeepEqual(got, []string{"#3", "#4"}) || violet[0].More != 0 {
		t.Errorf("labels %v more %d", got, violet[0].More)
	}
}

func TestGroupedLineHasNoURLAndASharedOrDrillDownCommand(t *testing.T) {
	sections := Group([]Item{prItem(PRBot, "w/h", 3), prItem(PRBot, "w/h", 4), prItem(PRReady, "w/a", 1), prItem(PRReady, "w/a", 2), prItem(PRReady, "w/b", 7)})
	bot := sections[1].Lines[0]
	if bot.URL != "" || bot.Command != "stw pulls --repo w/h --bots-only" || bot.Effect != Read {
		t.Errorf("bot line %+v", bot)
	}
	grouped := sections[0].Lines[0] // two ready PRs of w/a: merge commands differ, so the drill-down
	if grouped.URL != "" || grouped.Command != "stw pulls --repo w/a" || grouped.Effect != Read {
		t.Errorf("ready group %+v", grouped)
	}
	single := sections[0].Lines[1]
	if single.URL != "https://github.com/w/b/pull/7" || single.Command != "gh pr merge 7 -R w/b --rebase" || single.Effect != Owner || single.Count != 1 {
		t.Errorf("single %+v", single)
	}
}

func TestGroupDrillDownOfDraftsAndFallback(t *testing.T) {
	d1 := mk(ReleaseDraft, "w/r", Subject{Kind: KindRelease, Tag: "v1.0.0"})
	d2 := mk(ReleaseDraft, "w/r", Subject{Kind: KindRelease, Tag: "v1.1.0"})
	d1.Command, d1.Effect = buildCommand(ReleaseDraft, target{Slug: "w/r", Tag: "v1.0.0"})
	d2.Command, d2.Effect = buildCommand(ReleaseDraft, target{Slug: "w/r", Tag: "v1.1.0"})
	if l := Group([]Item{d1, d2})[0].Lines[0]; l.Command != "gh release list -R w/r" || l.Effect != Read {
		t.Errorf("drafts %+v", l)
	}
	// A category without a drill-down, with different commands, has none.
	w1 := mk(WorktreeDirty, "r", Subject{Kind: KindWorktree, Name: "/a"})
	w2 := mk(WorktreeDirty, "r", Subject{Kind: KindWorktree, Name: "/b"})
	w1.Command, w1.Effect = "git -C /a status", Read
	w2.Command, w2.Effect = "git -C /b status", Read
	if l := Group([]Item{w1, w2})[0].Lines[0]; l.Command != "" || l.Effect != "" {
		t.Errorf("worktrees %+v", l)
	}
	// Same command everywhere is kept.
	s1 := mk(LocalStash, "r", Subject{Kind: KindFact, Name: "stash"})
	s2 := s1
	s2.Subject.Name = "x"
	s1.Command, s1.Effect, s2.Command, s2.Effect = "same", Read, "same", Read
	if l := Group([]Item{s1, s2})[0].Lines[0]; l.Command != "same" || l.Effect != Read {
		t.Errorf("shared %+v", l)
	}
}

func TestGroupLabelsAreTheFirstThreeWithMore(t *testing.T) {
	var items []Item
	for n := 1; n <= 7; n++ {
		items = append(items, prItem(PRBot, "w/h", n))
	}
	l := Group(items)[0].Lines[0]
	if l.Count != 7 || !reflect.DeepEqual(l.Labels, []string{"#1", "#2", "#3"}) || l.More != 4 {
		t.Fatalf("%+v", l)
	}
	tags := []Item{mk(ReleaseDraft, "w/r", Subject{Kind: KindRelease, Tag: "v1\x1b[31m"}), mk(LocalStash, "r", Subject{Kind: KindFact, Name: "stash"})}
	if got := Group(tags)[0].Lines[0].Labels[0]; strings.ContainsAny(got, "\x1b") {
		t.Errorf("label %q", got)
	}
}

func TestGroupCapsLinesPerPriorityAndKeepsTheCounts(t *testing.T) {
	var items []Item
	for i := 0; i < 13; i++ {
		repo := fmt.Sprintf("w/r%02d", i)
		items = append(items, prItem(PRConflict, repo, 1), prItem(PRConflict, repo, 2)) // 13 yellow lines, 26 items
	}
	items = append(items, prItem(PRReady, "w/ok", 1))
	sections := Group(items)
	y := sections[0]
	if y.Priority != Yellow || len(y.Lines) != MaxLinesPerPriority || y.MoreLines != 3 || y.MoreItems != 6 {
		t.Fatalf("%d lines more %d/%d", len(y.Lines), y.MoreLines, y.MoreItems)
	}
	if y.MoreCommand != "stw pulls --repo w/r10" {
		t.Errorf("more command %q", y.MoreCommand)
	}
	if g := sections[1]; g.Priority != Green || len(g.Lines) != 1 || g.MoreLines != 0 || g.MoreCommand != "" {
		t.Errorf("green %+v", g)
	}
	// the cap is per priority and never merges across priorities
	for _, s := range sections {
		for _, l := range s.Lines {
			if l.Priority != s.Priority {
				t.Errorf("line of %s in section %s", l.Priority, s.Priority)
			}
		}
	}
}

func TestGroupCountsEqualItemCounts(t *testing.T) {
	ov := collect(richInput())
	perPriority := map[Priority]int{}
	for _, it := range ov {
		perPriority[it.Priority]++
	}
	sections := Group(ov)
	for _, s := range sections {
		sum := s.MoreItems
		for _, l := range s.Lines {
			sum += l.Count
		}
		if sum != perPriority[s.Priority] {
			t.Errorf("%s: %d in lines, %d items", s.Priority, sum, perPriority[s.Priority])
		}
		delete(perPriority, s.Priority)
	}
	if len(perPriority) != 0 {
		t.Errorf("priorities without a section: %v", perPriority)
	}
}

func TestGroupIsInvariantUnderShuffles(t *testing.T) {
	items := collect(richInput())
	want := Group(items)
	for seed := int64(0); seed < 50; seed++ {
		in := append([]Item(nil), items...)
		rand.New(rand.NewSource(seed)).Shuffle(len(in), func(i, j int) { in[i], in[j] = in[j], in[i] })
		if got := Group(in); !reflect.DeepEqual(got, want) {
			t.Fatalf("seed %d differs", seed)
		}
	}
	if Group(nil) != nil {
		t.Error("no items must give no sections")
	}
}

// The "and N more" command is the read-only drill-down of the first omitted line's category and repository,
// never the owner command of one of its items.
func TestMoreLineCarriesTheDrillDownNotAnOwnerCommand(t *testing.T) {
	var items []Item
	for i := 0; i < MaxLinesPerPriority+1; i++ {
		items = append(items, prItem(PRReady, fmt.Sprintf("w/r%02d", i), 7)) // 11 ready PRs, 11 lines
	}
	s := Group(items)[0]
	if len(s.Lines) != MaxLinesPerPriority || s.MoreLines != 1 || s.MoreItems != 1 {
		t.Fatalf("%d lines, more %d/%d", len(s.Lines), s.MoreLines, s.MoreItems)
	}
	if s.MoreCommand != "stw pulls --repo w/r10" || s.MoreEffect != Read {
		t.Errorf("more command %q (%q)", s.MoreCommand, s.MoreEffect)
	}
	if strings.Contains(s.MoreCommand, "gh pr merge") {
		t.Error("an owner command leaked into the more line")
	}
	// bots and drafts drill down too; a category without one has no command and no effect.
	bots := []Item{}
	for i := 0; i < 11; i++ {
		bots = append(bots, prItem(PRBot, fmt.Sprintf("w/b%02d", i), 1))
	}
	if b := Group(bots)[0]; b.MoreCommand != "stw pulls --repo w/b10 --bots-only" || b.MoreEffect != Read {
		t.Errorf("bots %q (%q)", b.MoreCommand, b.MoreEffect)
	}
	var stash []Item
	for i := 0; i < 11; i++ {
		it := mk(LocalStash, fmt.Sprintf("r%02d", i), Subject{Kind: KindFact, Name: "stash"})
		it.Command, it.Effect = "git -C x stash list", Read
		stash = append(stash, it)
	}
	if st := Group(stash)[0]; st.MoreLines != 1 || st.MoreCommand != "" || st.MoreEffect != "" {
		t.Errorf("stash %q (%q)", st.MoreCommand, st.MoreEffect)
	}
}
